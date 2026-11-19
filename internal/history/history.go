// Package history answers two questions agents ask on demand instead of
// having repository summaries loaded up front: which files tend to change
// together with this one (related), and why this file changed recently
// (why). Both come from the commit history, indexed incrementally into
// Carya's SQLite store.
package history

import (
	"bufio"
	"database/sql"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	// maxPairFiles leaves bulk commits (reformatting, renames, vendoring)
	// out of co-change counts, where they would make everything related.
	maxPairFiles = 40
	// windowCommits bounds a full (re)build to the most recent history,
	// which is also the most relevant.
	windowCommits = 5000
	schemaVersion = "3"
	// ReasonTrailer is read when present; Carya never writes it.
	ReasonTrailer = "Carya-Reason"
)

// Index is the history index for one checkout.
type Index struct {
	db       *sql.DB
	repoRoot string
}

// Open opens (creating if needed) the history tables in the SQLite file at
// dbPath for the checkout at repoRoot.
func Open(dbPath, repoRoot string) (*Index, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	ix := &Index{db: db, repoRoot: repoRoot}
	if err := ix.init(); err != nil {
		db.Close()
		return nil, err
	}
	return ix, nil
}

// Close closes the database.
func (ix *Index) Close() error { return ix.db.Close() }

func (ix *Index) init() error {
	return ix.createTables(ix.db)
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func (ix *Index) createTables(db execer) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS history_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE IF NOT EXISTS history_commits (
			hash TEXT PRIMARY KEY,
			ord INTEGER NOT NULL,
			time INTEGER NOT NULL,
			author TEXT NOT NULL,
			subject TEXT NOT NULL,
			reason TEXT NOT NULL,
			files INTEGER NOT NULL
		);
		CREATE TABLE IF NOT EXISTS history_files (
			path TEXT NOT NULL,
			hash TEXT NOT NULL,
			PRIMARY KEY (path, hash)
		);
		CREATE INDEX IF NOT EXISTS idx_history_files_hash ON history_files(hash);
		CREATE TABLE IF NOT EXISTS history_pairs (
			a TEXT NOT NULL,
			b TEXT NOT NULL,
			count INTEGER NOT NULL,
			weight REAL NOT NULL,
			PRIMARY KEY (a, b)
		);
		CREATE INDEX IF NOT EXISTS idx_history_pairs_b ON history_pairs(b);
	`)
	return err
}

func (ix *Index) meta(key string) string {
	var v string
	_ = ix.db.QueryRow(`SELECT value FROM history_meta WHERE key = ?`, key).Scan(&v)
	return v
}

// Update brings the index up to HEAD. It indexes only new commits when the
// last indexed commit is an ancestor of HEAD, and rebuilds from the recent
// history otherwise. It returns how many commits it indexed.
func (ix *Index) Update() (int, error) {
	head, err := ix.git("rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if err != nil {
		return 0, nil // no commits yet
	}
	head = strings.TrimSpace(head)
	last := ix.meta("indexed_head")
	if last == head && ix.meta("version") == schemaVersion {
		return 0, nil
	}

	tx, err := ix.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	logArgs := []string{"-c", "core.quotePath=false", "log", "--no-merges", "--no-renames", "--name-only",
		"--format=%x1e%H%x1f%ct%x1f%an%x1f%s%x1f%(trailers:key=" + ReasonTrailer + ",valueonly,separator=%x20)"}
	incremental := last != "" && ix.meta("version") == schemaVersion && ix.isAncestor(last, head)
	if incremental {
		logArgs = append(logArgs, last+".."+head)
	} else {
		// DROP rather than DELETE so a schema change takes effect.
		for _, table := range []string{"history_commits", "history_files", "history_pairs", "history_meta"} {
			if _, err := tx.Exec("DROP TABLE IF EXISTS " + table); err != nil {
				return 0, err
			}
		}
		if err := ix.createTables(tx); err != nil {
			return 0, err
		}
		logArgs = append(logArgs, "--max-count="+strconv.Itoa(windowCommits), head)
	}

	cmd := exec.Command("git", logArgs...)
	cmd.Dir = ix.repoRoot
	out, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	recs, parseErr := parseLog(out)
	waitErr := cmd.Wait()
	if parseErr != nil {
		return 0, parseErr
	}
	if waitErr != nil {
		return 0, fmt.Errorf("git log: %w", waitErr)
	}
	n, err := ix.ingest(tx, recs)
	if err != nil {
		return 0, err
	}

	for k, v := range map[string]string{"indexed_head": head, "version": schemaVersion} {
		if _, err := tx.Exec(`INSERT INTO history_meta(key, value) VALUES(?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return 0, err
		}
	}
	return n, tx.Commit()
}

type record struct {
	hash, author, subject, reason string
	time                          int64
	files                         []string
}

// parseLog reads `git log` output in the format Update asks for, newest
// commit first.
func parseLog(r io.Reader) ([]record, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var recs []record
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "\x1e") {
			fields := strings.SplitN(strings.TrimPrefix(line, "\x1e"), "\x1f", 5)
			for len(fields) < 5 {
				fields = append(fields, "")
			}
			t, _ := strconv.ParseInt(fields[1], 10, 64)
			recs = append(recs, record{hash: fields[0], time: t, author: fields[2], subject: fields[3], reason: strings.TrimSpace(fields[4])})
			continue
		}
		if line = strings.TrimSpace(line); line != "" && len(recs) > 0 {
			recs[len(recs)-1].files = append(recs[len(recs)-1].files, line)
		}
	}
	return recs, scanner.Err()
}

// ingest stores recs (newest first). Each commit gets an ord one past every
// commit indexed before it, oldest first, so ord follows git's log order even
// for commits made within the same second.
func (ix *Index) ingest(tx *sql.Tx, recs []record) (int, error) {
	insCommit, err := tx.Prepare(`INSERT OR IGNORE INTO history_commits(hash, ord, time, author, subject, reason, files) VALUES(?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	insFile, err := tx.Prepare(`INSERT OR IGNORE INTO history_files(path, hash) VALUES(?, ?)`)
	if err != nil {
		return 0, err
	}
	// A commit touching n files adds 1/(n-1) to each pair it contains, so a
	// focused two-file commit links its files strongly and a catch-all
	// commit links its files only weakly.
	insPair, err := tx.Prepare(`INSERT INTO history_pairs(a, b, count, weight) VALUES(?, ?, 1, ?)
		ON CONFLICT(a, b) DO UPDATE SET count = count + 1, weight = weight + excluded.weight`)
	if err != nil {
		return 0, err
	}
	var next int64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(ord), 0) + 1 FROM history_commits`).Scan(&next); err != nil {
		return 0, err
	}

	n := 0
	for i := len(recs) - 1; i >= 0; i-- {
		rec := recs[i]
		res, err := insCommit.Exec(rec.hash, next, rec.time, rec.author, rec.subject, rec.reason, len(rec.files))
		if err != nil {
			return n, err
		}
		if added, _ := res.RowsAffected(); added == 0 {
			continue // already indexed
		}
		next++
		n++
		for _, f := range rec.files {
			if _, err := insFile.Exec(f, rec.hash); err != nil {
				return n, err
			}
		}
		if len(rec.files) > maxPairFiles || len(rec.files) < 2 {
			continue
		}
		w := 1 / float64(len(rec.files)-1)
		for a := 0; a < len(rec.files); a++ {
			for b := a + 1; b < len(rec.files); b++ {
				x, y := rec.files[a], rec.files[b]
				if x > y {
					x, y = y, x
				}
				if _, err := insPair.Exec(x, y, w); err != nil {
					return n, err
				}
			}
		}
	}
	return n, nil
}

// Related is a file that tends to change together with another.
type Related struct {
	Path     string  `json:"path"`
	Together int     `json:"together"` // commits that changed both
	Share    float64 `json:"share"`    // Together / commits that changed the queried file
	// Strength discounts commits by size: each shared commit counts
	// 1/(files-1), divided by the commits that changed the queried file. A
	// pair only ever changed together in two-file commits scores 1.
	Strength float64 `json:"strength"`
}

// RelatedFiles returns the files most often changed in the same commit as
// path, among those still in HEAD, and how many indexed commits touched path.
func (ix *Index) RelatedFiles(path string, limit int) ([]Related, int, error) {
	total, err := ix.commitsTouching(path)
	if err != nil || total == 0 {
		return nil, total, err
	}
	rows, err := ix.db.Query(`
		SELECT other, count, weight FROM (
			SELECT b AS other, count, weight FROM history_pairs WHERE a = ?
			UNION ALL
			SELECT a AS other, count, weight FROM history_pairs WHERE b = ?
		) ORDER BY weight DESC, count DESC, other ASC`, path, path)
	if err != nil {
		return nil, total, err
	}
	defer rows.Close()

	present := ix.headPaths()
	var out []Related
	for rows.Next() {
		var r Related
		var weight float64
		if err := rows.Scan(&r.Path, &r.Together, &weight); err != nil {
			return nil, total, err
		}
		r.Strength = weight / float64(total)
		if present != nil && !present[r.Path] {
			continue // deleted since
		}
		r.Share = float64(r.Together) / float64(total)
		out = append(out, r)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out, total, rows.Err()
}

// Change is one commit that touched a file.
type Change struct {
	Commit  string    `json:"commit"`
	Time    time.Time `json:"time"`
	Author  string    `json:"author"`
	Subject string    `json:"subject"`
	Reason  string    `json:"reason,omitempty"` // from a Carya-Reason trailer, if any
	Files   int       `json:"files"`            // files the commit changed
}

// Why returns the most recent commits that touched path, newest first, and
// how many indexed commits touched it in all.
func (ix *Index) Why(path string, limit int) ([]Change, int, error) {
	total, err := ix.commitsTouching(path)
	if err != nil || total == 0 {
		return nil, total, err
	}
	rows, err := ix.db.Query(`
		SELECT c.hash, c.time, c.author, c.subject, c.reason, c.files
		FROM history_files f JOIN history_commits c ON c.hash = f.hash
		WHERE f.path = ? ORDER BY c.ord DESC LIMIT ?`, path, limit)
	if err != nil {
		return nil, total, err
	}
	defer rows.Close()
	var out []Change
	for rows.Next() {
		var c Change
		var t int64
		if err := rows.Scan(&c.Commit, &t, &c.Author, &c.Subject, &c.Reason, &c.Files); err != nil {
			return nil, total, err
		}
		c.Time = time.Unix(t, 0)
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// LineChange returns the commit that last changed line of path in HEAD.
func (ix *Index) LineChange(path string, line int) (*Change, error) {
	out, err := ix.git("blame", "--porcelain", "-L", fmt.Sprintf("%d,%d", line, line), "HEAD", "--", path)
	if err != nil {
		return nil, fmt.Errorf("blame %s:%d: %w", path, line, err)
	}
	hash, _, _ := strings.Cut(out, " ")
	if len(hash) < 40 {
		return nil, fmt.Errorf("blame %s:%d: unexpected output", path, line)
	}
	var c Change
	var t int64
	err = ix.db.QueryRow(`SELECT hash, time, author, subject, reason, files FROM history_commits WHERE hash = ?`, hash).
		Scan(&c.Commit, &t, &c.Author, &c.Subject, &c.Reason, &c.Files)
	if err == sql.ErrNoRows {
		// Older than the indexed window: ask git directly.
		show, err := ix.git("show", "-s", "--format=%H%x1f%ct%x1f%an%x1f%s%x1f%(trailers:key="+ReasonTrailer+",valueonly,separator=%x20)", hash)
		if err != nil {
			return nil, err
		}
		f := strings.SplitN(strings.TrimSpace(show), "\x1f", 5)
		for len(f) < 5 {
			f = append(f, "")
		}
		t, _ = strconv.ParseInt(f[1], 10, 64)
		c = Change{Commit: f[0], Author: f[2], Subject: f[3], Reason: strings.TrimSpace(f[4])}
	} else if err != nil {
		return nil, err
	}
	c.Time = time.Unix(t, 0)
	return &c, nil
}

func (ix *Index) commitsTouching(path string) (int, error) {
	var n int
	err := ix.db.QueryRow(`SELECT COUNT(*) FROM history_files WHERE path = ?`, path).Scan(&n)
	return n, err
}

// headPaths returns the files in HEAD, or nil if they can't be listed.
func (ix *Index) headPaths() map[string]bool {
	out, err := ix.git("-c", "core.quotePath=false", "ls-tree", "-r", "--name-only", "HEAD")
	if err != nil {
		return nil
	}
	paths := map[string]bool{}
	for _, p := range strings.Split(out, "\n") {
		if p != "" {
			paths[p] = true
		}
	}
	return paths
}

func (ix *Index) isAncestor(a, b string) bool {
	cmd := exec.Command("git", "merge-base", "--is-ancestor", a, b)
	cmd.Dir = ix.repoRoot
	return cmd.Run() == nil
}

func (ix *Index) git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = ix.repoRoot
	out, err := cmd.Output()
	return string(out), err
}
