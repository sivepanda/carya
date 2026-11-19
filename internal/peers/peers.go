// Package peers answers "is anyone else changing this file?" from the
// working states other agents and teammates have published.
package peers

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"carya/internal/git"
)

// Change is one peer's unfinished change to a path.
type Change struct {
	UserID    string    `json:"user_id"`
	Path      string    `json:"path"`
	Status    string    `json:"status"` // A, M or D, against the peer's own base
	Base      string    `json:"base,omitempty"`
	Published time.Time `json:"published,omitempty"`
	Conflict  bool      `json:"conflict"` // merging their state with yours conflicts here
	Patch     string    `json:"patch"`
}

// Checker looks up peer changes in one repository.
type Checker struct {
	repoRoot  string
	refs      *git.RefManager
	predictor *git.ConflictPredictor
}

func NewChecker(repoRoot string) *Checker {
	return &Checker{
		repoRoot:  repoRoot,
		refs:      git.NewRefManager(repoRoot),
		predictor: git.NewConflictPredictor(repoRoot),
	}
}

// ForPaths returns every peer change to the given repo-relative paths,
// ordered by path then user. Each peer is measured against the base its own
// state records, so upstream commits a peer lacks are not reported as their
// edits. mine is the caller's current state; when it is empty no conflicts
// are predicted.
func (c *Checker) ForPaths(me string, mine git.UserRef, paths []string) ([]Change, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	head, err := c.refs.ResolveRef("HEAD")
	if err != nil {
		head = ""
	}

	states, err := c.refs.ListUserRefs()
	if err != nil {
		return nil, err
	}

	var changes []Change
	for _, peer := range states {
		if peer.UserID == me {
			continue
		}
		base := peer.Base(head)
		if base == "" {
			continue
		}
		touched, err := c.changedPaths(base, peer.TreeHash, paths)
		if err != nil || len(touched) == 0 {
			continue
		}

		conflicted := map[string]bool{}
		if mine.TreeHash != "" {
			if report, err := c.predictor.PredictStateConflicts(mine, peer, head); err == nil {
				for _, f := range report.ConflictedFiles {
					conflicted[f.Path] = true
				}
			}
		}
		published := c.publishedAt(peer)

		for _, t := range touched {
			patch, _ := c.patch(base, peer.TreeHash, t.path)
			changes = append(changes, Change{
				UserID:    peer.UserID,
				Path:      t.path,
				Status:    t.status,
				Base:      peer.BaseCommit,
				Published: published,
				Conflict:  conflicted[t.path],
				Patch:     patch,
			})
		}
	}

	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Path != changes[j].Path {
			return changes[i].Path < changes[j].Path
		}
		return changes[i].UserID < changes[j].UserID
	})
	return changes, nil
}

type touchedPath struct{ status, path string }

func (c *Checker) changedPaths(base, tree string, paths []string) ([]touchedPath, error) {
	args := append([]string{"diff-tree", "-r", "-z", "--no-renames", "--name-status", base, tree, "--"}, paths...)
	cmd := exec.Command("git", args...)
	cmd.Dir = c.repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("diff %s..%s: %w", base, tree, err)
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	var touched []touchedPath
	for i := 0; i+1 < len(fields); i += 2 {
		touched = append(touched, touchedPath{status: fields[i], path: fields[i+1]})
	}
	return touched, nil
}

func (c *Checker) patch(base, tree, path string) (string, error) {
	cmd := exec.Command("git", "diff-tree", "-p", "--no-renames", base, tree, "--", path)
	cmd.Dir = c.repoRoot
	out, err := cmd.Output()
	return string(out), err
}

func (c *Checker) publishedAt(peer git.UserRef) time.Time {
	if peer.Commit == "" {
		return time.Time{}
	}
	cmd := exec.Command("git", "show", "-s", "--format=%ct", peer.Commit)
	cmd.Dir = c.repoRoot
	out, err := cmd.Output()
	if err != nil {
		return time.Time{}
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(secs, 0)
}

// Summary renders changes as short plain text for a model or a terminal.
// It returns "" when there is nothing to report.
func Summary(changes []Change, now time.Time) string {
	if len(changes) == 0 {
		return ""
	}
	var b strings.Builder
	for _, ch := range changes {
		verb := map[string]string{"A": "added", "D": "deleted"}[ch.Status]
		if verb == "" {
			verb = "changed"
		}
		fmt.Fprintf(&b, "%s has unfinished work that %s %s", ch.UserID, verb, ch.Path)
		if !ch.Published.IsZero() {
			fmt.Fprintf(&b, " (published %s ago)", now.Sub(ch.Published).Round(time.Second))
		}
		if ch.Conflict {
			b.WriteString(". Your current changes would CONFLICT with theirs here.")
		} else {
			b.WriteString(". It merges cleanly with your current changes so far.")
		}
		b.WriteString("\n")
		if ch.Patch != "" {
			b.WriteString(strings.TrimRight(ch.Patch, "\n"))
			b.WriteString("\n")
		}
	}
	return b.String()
}
