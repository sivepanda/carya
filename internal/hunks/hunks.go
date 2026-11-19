// Package hunks splits an agent's uncommitted work into hunks it can commit
// in any grouping, without interactive staging. Agents cannot drive
// `git add -p`; this gives them stable hunk IDs, suggestions for which files
// belong in one commit, and a way to commit exactly the hunks they choose.
package hunks

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"carya/internal/git"
)

// Hunk is one independently committable piece of the working tree's changes:
// a diff hunk, or a whole file for additions, deletions, binary files and
// mode changes.
type Hunk struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Kind    string `json:"kind"`             // "change", "add", "delete", "binary" or "mode"
	Header  string `json:"header,omitempty"` // the @@ line for a change
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Diff    string `json:"diff"` // a change's hunk without the file header; the whole file's patch otherwise

	fileHeader string // the file's diff header, for a change
	order      int    // position in List's result
	digest     string // the content hash an ID starts with
	line       int    // the line a change starts at in the working tree file
}

// List returns the working tree's changes against HEAD as hunks, in file
// and then line order. It sees what `carya snapshot` sees: tracked changes,
// deletions and untracked files that are not ignored.
func List(repoRoot string) ([]Hunk, error) {
	tree, err := git.WorkingStateTree(repoRoot)
	if err != nil {
		return nil, err
	}
	base, err := run(repoRoot, nil, "", "rev-parse", "--verify", "--quiet", "HEAD^{tree}")
	if err != nil {
		if base, err = emptyTree(repoRoot); err != nil {
			return nil, err
		}
	}
	diff, err := run(repoRoot, nil, "", "-c", "core.quotePath=false", "diff-tree", "-p", "--binary", "--no-renames", "--full-index", "--no-color", strings.TrimSpace(base), tree)
	if err != nil {
		return nil, fmt.Errorf("diff working tree: %w", err)
	}
	return parse(diff)
}

// emptyTree returns the ID of the empty tree, which differs between SHA-1
// and SHA-256 repositories.
func emptyTree(repoRoot string) (string, error) {
	out, err := run(repoRoot, nil, "", "hash-object", "-t", "tree", "--stdin")
	return strings.TrimSpace(out), err
}

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

func parse(diff string) ([]Hunk, error) {
	var out []Hunk
	lines := strings.SplitAfter(diff, "\n")
	for i := 0; i < len(lines); {
		if !strings.HasPrefix(lines[i], "diff --git ") {
			i++
			continue
		}
		path, err := diffPath(strings.TrimSuffix(lines[i], "\n"))
		if err != nil {
			return nil, err
		}
		j := i + 1
		for j < len(lines) && !strings.HasPrefix(lines[j], "diff --git ") {
			j++
		}
		out = append(out, parseFile(path, lines[i:j])...)
		i = j
	}
	assignIDs(out)
	return out, nil
}

// diffPath reads the path from a "diff --git a/<path> b/<path>" line. Without
// renames both names are the same path, which is how a path that itself has
// " b/" in it is read correctly. Git quotes a path C-style when it has a
// double quote, backslash or control character in it.
func diffPath(line string) (string, error) {
	names := strings.TrimPrefix(line, "diff --git ")
	if strings.HasPrefix(names, `"`) {
		a, rest, ok := unquote(names)
		if ok && strings.HasPrefix(a, "a/") && strings.HasPrefix(rest, " ") {
			if b, rest, ok := unquote(rest[1:]); ok && rest == "" && b == "b/"+a[2:] {
				return a[2:], nil
			}
		}
	} else if n := (len(names) - len("a/ b/")) / 2; n > 0 && len(names) == 2*n+len("a/ b/") {
		if p := names[2 : 2+n]; names[:2] == "a/" && names[2+n:5+n] == " b/" && names[5+n:] == p {
			return p, nil
		}
	}
	return "", fmt.Errorf("cannot read the path from %q", line)
}

// unquote reads a C-style quoted string, as git writes unusual paths, from
// the start of s, and returns it and the rest of s. Bytes git leaves alone
// pass through as they are, so non-UTF-8 names survive.
func unquote(s string) (string, string, bool) {
	if !strings.HasPrefix(s, `"`) {
		return "", s, false
	}
	escapes := map[byte]byte{'a': '\a', 'b': '\b', 't': '\t', 'n': '\n', 'v': '\v', 'f': '\f', 'r': '\r', '"': '"', '\\': '\\'}
	octal := func(c byte) bool { return '0' <= c && c <= '7' }
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		switch c := s[i]; c {
		case '"':
			return b.String(), s[i+1:], true
		case '\\':
			if i+1 >= len(s) {
				return "", s, false
			}
			e := s[i+1]
			if r, ok := escapes[e]; ok {
				b.WriteByte(r)
				i++
				continue
			}
			if i+3 >= len(s) || e > '3' || !octal(e) || !octal(s[i+2]) || !octal(s[i+3]) {
				return "", s, false
			}
			b.WriteByte((e-'0')<<6 | (s[i+2]-'0')<<3 | (s[i+3] - '0'))
			i += 3
		default:
			b.WriteByte(c)
		}
	}
	return "", s, false
}

// parseFile splits one file's section into hunks.
func parseFile(path string, section []string) []Hunk {
	h := 0
	for h < len(section) && !hunkHeader.MatchString(section[h]) {
		h++
	}
	header := strings.Join(section[:h], "")
	kind := "change"
	switch {
	case strings.Contains(header, "\nnew file mode "):
		kind = "add"
	case strings.Contains(header, "\ndeleted file mode "):
		kind = "delete"
	case strings.Contains(header, "\nGIT binary patch") || strings.Contains(header, "\nBinary files "):
		kind = "binary"
	case h == len(section):
		kind = "mode"
	}

	// Whole-file changes cannot be split: they are one hunk.
	if kind != "change" {
		added, removed := count(section[h:])
		return []Hunk{{Path: path, Kind: kind, Added: added, Removed: removed, Diff: strings.Join(section, "")}}
	}

	var hunks []Hunk
	for h < len(section) {
		end := h + 1
		for end < len(section) && !hunkHeader.MatchString(section[end]) {
			end++
		}
		body := section[h:end]
		added, removed := count(body[1:])
		line, _ := strconv.Atoi(hunkHeader.FindStringSubmatch(body[0])[1])
		hunks = append(hunks, Hunk{
			Path:       path,
			Kind:       "change",
			Header:     strings.TrimRight(body[0], "\n"),
			Added:      added,
			Removed:    removed,
			Diff:       strings.Join(body, ""),
			fileHeader: header,
			line:       line,
		})
		h = end
	}
	return hunks
}

// count counts the added and removed lines of hunk bodies. Every body line
// starts with ' ', '+', '-' or '\', so "--- x" is a removed "-- x", not a
// file header: those come before the first @@ line and are never counted.
func count(lines []string) (added, removed int) {
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "+"):
			added++
		case strings.HasPrefix(l, "-"):
			removed++
		}
	}
	return added, removed
}

// assignIDs gives each hunk a short ID from its path and content, leaving
// out line numbers, so a hunk keeps its ID while other hunks are committed
// around it. Hunks with identical content get the line they start at in the
// working tree file added ("abc1234@40"): committing other hunks never moves
// that line, and Select still takes the form once the hunk is unique.
func assignIDs(hunks []Hunk) {
	copies := map[string]int{}
	for i := range hunks {
		h := &hunks[i]
		h.order = i
		body := h.Diff
		if h.Kind == "change" {
			_, body, _ = strings.Cut(h.Diff, "\n") // drop the @@ line
		}
		sum := sha1.Sum([]byte(h.Path + "\x00" + body))
		h.digest = hex.EncodeToString(sum[:])[:7]
		copies[h.digest]++
	}
	seen := map[string]int{}
	for i := range hunks {
		h := &hunks[i]
		id := h.digest
		if copies[h.digest] > 1 && h.line > 0 {
			id += "@" + strconv.Itoa(h.line)
		}
		// Whole files have no line to tell them apart; a short hash can
		// also collide. Number what is left over.
		seen[id]++
		if n := seen[id]; n > 1 {
			id += "-" + strconv.Itoa(n)
		}
		h.ID = id
	}
}

// Select resolves hunk IDs and file paths to hunks. A path selects every
// hunk of that file. It fails on anything that matches nothing, which is
// usually an ID from before the working tree changed, and on an ID that now
// matches several identical hunks.
func Select(all []Hunk, refs []string) ([]Hunk, error) {
	byID := map[string]int{}
	byDigest := map[string][]int{}
	byPath := map[string][]int{}
	for i, h := range all {
		byID[h.ID] = i
		byDigest[h.digest] = append(byDigest[h.digest], i)
		byPath[h.Path] = append(byPath[h.Path], i)
	}
	picked := map[int]bool{}
	var unknown, ambiguous []string
	for _, ref := range refs {
		if i, ok := byID[ref]; ok {
			picked[i] = true
			continue
		}
		if i, ok := atLine(all, byDigest, ref); ok {
			picked[i] = true
			continue
		}
		is, ok := byPath[ref]
		if !ok {
			is, ok = byPath[path.Clean(filepath.ToSlash(ref))]
		}
		if ok {
			for _, i := range is {
				picked[i] = true
			}
			continue
		}
		if len(byDigest[ref]) > 1 {
			var copies []string
			for _, i := range byDigest[ref] {
				copies = append(copies, all[i].ID)
			}
			ambiguous = append(ambiguous, fmt.Sprintf("%s (now %s)", ref, strings.Join(copies, ", ")))
			continue
		}
		unknown = append(unknown, ref)
	}
	var problems []string
	if len(unknown) > 0 {
		problems = append(problems, "no hunk or changed file matches "+strings.Join(unknown, ", "))
	}
	if len(ambiguous) > 0 {
		problems = append(problems, "identical hunks share the ID "+strings.Join(ambiguous, "; "))
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%s; run `carya hunks` for current IDs", strings.Join(problems, "; "))
	}
	out := make([]Hunk, 0, len(picked))
	for i := range all {
		if picked[i] {
			out = append(out, all[i])
		}
	}
	return out, nil
}

// atLine resolves an ID of the form digest@line to the change with that
// content starting at that line of the working tree file.
func atLine(all []Hunk, byDigest map[string][]int, ref string) (int, bool) {
	digest, at, ok := strings.Cut(ref, "@")
	line, err := strconv.Atoi(at)
	if !ok || err != nil {
		return 0, false
	}
	for _, i := range byDigest[digest] {
		if all[i].line == line {
			return i, true
		}
	}
	return 0, false
}

// Patch joins hunks into one patch that applies to HEAD.
func Patch(hunks []Hunk) string {
	// git apply wants each file's hunks together and in order.
	hunks = append([]Hunk(nil), hunks...)
	sort.SliceStable(hunks, func(i, j int) bool { return hunks[i].order < hunks[j].order })
	var b strings.Builder
	lastHeader := ""
	for _, h := range hunks {
		if h.fileHeader == "" {
			b.WriteString(h.Diff)
			lastHeader = ""
			continue
		}
		if h.fileHeader != lastHeader {
			b.WriteString(h.fileHeader)
			lastHeader = h.fileHeader
		}
		b.WriteString(h.Diff)
	}
	return b.String()
}

// Commit commits exactly the hunks refs names, with message, through a
// temporary index, so other changes (staged or not) stay as they are. It
// runs `git commit`, so the repository's commit hooks and the user's
// identity apply. It returns git's output.
func Commit(repoRoot string, refs []string, message string) (string, []Hunk, error) {
	if strings.TrimSpace(message) == "" {
		return "", nil, errors.New("a commit message is required")
	}
	if len(refs) == 0 {
		return "", nil, errors.New("name the hunk IDs or files to commit")
	}
	if err := checkNoOperation(repoRoot); err != nil {
		return "", nil, err
	}
	all, err := List(repoRoot)
	if err != nil {
		return "", nil, err
	}
	if len(all) == 0 {
		return "", nil, errors.New("nothing to commit")
	}
	picked, err := Select(all, refs)
	if err != nil {
		return "", nil, err
	}
	oldHead := ""
	if out, err := run(repoRoot, nil, "", "rev-parse", "--verify", "--quiet", "HEAD^{commit}"); err == nil {
		oldHead = strings.TrimSpace(out)
	}
	out, err := commitPatch(repoRoot, oldHead, Patch(picked), message)
	if err != nil {
		return out, picked, err
	}
	if err := updateIndex(repoRoot, oldHead); err != nil {
		return out, picked, fmt.Errorf("committed, but could not update the index to match: %w", err)
	}
	return out, picked, nil
}

// checkNoOperation refuses to commit while git is in the middle of a merge,
// cherry-pick or revert: the commit would become that operation's, recording
// its other parent or taking its author, with only some of its changes.
func checkNoOperation(repoRoot string) error {
	out, err := run(repoRoot, nil, "", "rev-parse", "--git-path", "MERGE_HEAD", "--git-path", "CHERRY_PICK_HEAD", "--git-path", "REVERT_HEAD")
	if err != nil {
		return err
	}
	paths := strings.Split(strings.TrimSpace(out), "\n")
	for i, op := range []string{"merge", "cherry-pick", "revert"} {
		if i >= len(paths) {
			break
		}
		p := paths[i]
		if !filepath.IsAbs(p) {
			p = filepath.Join(repoRoot, p)
		}
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf("a %s is in progress; finish or abort it before committing hunks", op)
		}
	}
	return nil
}

// commitPatch commits HEAD (or nothing, on an unborn branch) with patch
// applied, building the commit in a temporary index.
func commitPatch(repoRoot, oldHead, patch, message string) (string, error) {
	dir, err := os.MkdirTemp("", "carya-hunks-*")
	if err != nil {
		return "", fmt.Errorf("create temporary index: %w", err)
	}
	defer os.RemoveAll(dir)
	env := append(os.Environ(), "GIT_INDEX_FILE="+filepath.Join(dir, "index"))

	seed := []string{"read-tree", "--empty"}
	if oldHead != "" {
		seed = []string{"read-tree", oldHead}
	}
	if _, err := run(repoRoot, env, "", seed...); err != nil {
		return "", fmt.Errorf("seed temporary index: %w", err)
	}
	// The hunks are committed exactly as they are in the working tree:
	// apply.whitespace=fix would otherwise change them and =error refuse them.
	if _, err := run(repoRoot, env, patch, "apply", "--cached", "--whitespace=nowarn", "-"); err != nil {
		return "", fmt.Errorf("apply hunks: %w", err)
	}
	cmd := exec.Command("git", "commit", "-m", message)
	cmd.Dir = repoRoot
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git commit: %w\n%s", err, out)
	}
	return string(out), nil
}

// entry is a path's mode and object in a tree or the index; an absent path
// has mode "000000".
type entry struct{ mode, oid string }

func (e entry) regular() bool { return e.mode == "100644" || e.mode == "100755" }

// updateIndex brings the checkout's index up to date with the commit just
// made from a temporary index, for the paths it changed. A path the user had
// nothing staged for gets the committed version. A path they had staged
// keeps their version with the committed change merged in, or as it is when
// the two overlap, so the index never shows the commit being undone. It
// reads and writes index entries directly: paths are never pathspecs, so a
// file named "*.go" stands for itself.
func updateIndex(repoRoot, oldHead string) error {
	base := oldHead
	if base == "" {
		var err error
		if base, err = emptyTree(repoRoot); err != nil {
			return err
		}
	}
	raw, err := run(repoRoot, nil, "", "diff-tree", "-r", "-z", "--no-renames", "--no-abbrev", base, "HEAD")
	if err != nil {
		return fmt.Errorf("list committed paths: %w", err)
	}
	staged, err := run(repoRoot, nil, "", "ls-files", "--stage", "-z")
	if err != nil {
		return fmt.Errorf("read index: %w", err)
	}
	index := map[string]entry{}
	conflicted := map[string]bool{}
	for _, rec := range strings.Split(staged, "\x00") {
		meta, p, ok := strings.Cut(rec, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 3 {
			continue
		}
		if f[2] != "0" {
			conflicted[p] = true
		}
		index[p] = entry{f[0], f[1]}
	}

	var info strings.Builder
	fields := strings.Split(raw, "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		f := strings.Fields(strings.TrimPrefix(fields[i], ":"))
		if len(f) < 5 {
			return fmt.Errorf("unexpected diff-tree output %q", fields[i])
		}
		p, old, committed := fields[i+1], entry{f[0], f[2]}, entry{f[1], f[3]}
		if conflicted[p] {
			continue
		}
		cur, ok := index[p]
		if !ok {
			cur = entry{"000000", strings.Repeat("0", len(old.oid))}
		}
		switch {
		case cur == committed:
		case cur == old:
			writeEntry(&info, committed, p)
		default:
			if merged, ok := mergeStaged(repoRoot, old, cur, committed); ok {
				writeEntry(&info, merged, p)
			}
		}
	}
	if info.Len() == 0 {
		return nil
	}
	if _, err := run(repoRoot, nil, info.String(), "update-index", "-z", "--index-info"); err != nil {
		return err
	}
	// Entries written that way have no stat data; refresh it so plumbing
	// such as diff-files does not report the files as changed.
	_, _ = run(repoRoot, nil, "", "update-index", "-q", "--refresh")
	return nil
}

func writeEntry(b *strings.Builder, e entry, path string) {
	if e.mode == "000000" {
		fmt.Fprintf(b, "0 %s\t%s\x00", e.oid, path) // mode 0 removes the path
		return
	}
	fmt.Fprintf(b, "%s %s\t%s\x00", e.mode, e.oid, path)
}

// mergeStaged merges the committed change to a file (old to committed) into
// the version the user has staged. It reports false unless all three are
// regular files and they merge cleanly.
func mergeStaged(repoRoot string, old, staged, committed entry) (entry, bool) {
	if !old.regular() || !staged.regular() || !committed.regular() {
		return entry{}, false
	}
	dir, err := os.MkdirTemp("", "carya-hunks-merge-*")
	if err != nil {
		return entry{}, false
	}
	defer os.RemoveAll(dir)
	files := []string{filepath.Join(dir, "staged"), filepath.Join(dir, "old"), filepath.Join(dir, "committed")}
	for i, e := range []entry{staged, old, committed} {
		content, err := run(repoRoot, nil, "", "cat-file", "blob", e.oid)
		if err != nil || os.WriteFile(files[i], []byte(content), 0o600) != nil {
			return entry{}, false
		}
	}
	merged, err := run(repoRoot, nil, "", "merge-file", "-p", "--quiet", files[0], files[1], files[2])
	if err != nil { // conflicts, or a binary file
		return entry{}, false
	}
	oid, err := run(repoRoot, nil, merged, "hash-object", "-w", "--stdin")
	if err != nil {
		return entry{}, false
	}
	mode := staged.mode
	if staged.mode == old.mode {
		mode = committed.mode
	}
	return entry{mode, strings.TrimSpace(oid)}, true
}

// run runs git in dir, with env (nil for this process's) and stdin, and
// returns its standard output; an error carries git's standard error.
func run(dir string, env []string, stdin string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			err = fmt.Errorf("%w: %s", err, msg)
		}
		name := args[0]
		if name == "-c" && len(args) > 2 {
			name = args[2]
		}
		return string(out), fmt.Errorf("git %s: %w", name, err)
	}
	return string(out), nil
}
