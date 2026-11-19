// Package integrate merges every agent's unfinished work into one tree and
// checks it the way CI would, before anyone opens a pull request.
package integrate

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"carya/internal/git"
)

// IntegrationRef holds the last merged result so it can be inspected or
// checked out afterwards.
const IntegrationRef = "refs/carya/integration"

// Options controls an integration run.
type Options struct {
	Onto    string        // ref to integrate onto; empty means the default branch
	Only    []string      // user IDs to include; empty means everyone
	Exclude []string      // user IDs to leave out
	Since   time.Duration // leave out states published longer ago than this; zero keeps all
	Run     []string      // build and test commands; empty means detect them
	Setup   bool          // run the repository's Mycelia post-checkout setup first
	Keep    bool          // keep the worktree afterwards
	Timeout time.Duration // per step; zero means 15 minutes
}

// Merged is a state included in the result.
type Merged struct {
	UserID    string    `json:"user_id"`
	Commit    string    `json:"commit"`
	Published time.Time `json:"published"`
}

// Conflict is a state left out because it does not merge cleanly.
type Conflict struct {
	UserID string   `json:"user_id"`
	Files  []string `json:"files"`
	// With lists the already-merged states that change the same files, or
	// the onto ref when only it does.
	With []string `json:"with"`
}

// Skipped is a state left out for another reason.
type Skipped struct {
	UserID string `json:"user_id"`
	Reason string `json:"reason"`
}

// Step is one setup, build or test command run on the merged tree.
type Step struct {
	Kind     string        `json:"kind"` // "setup" or "run"
	Command  string        `json:"command"`
	ExitCode int           `json:"exit_code"`
	Duration time.Duration `json:"duration_ns"`
	Output   string        `json:"output"` // the tail; the full output is in LogPath
}

// Result is the outcome of an integration.
type Result struct {
	Onto       string     `json:"onto"`
	OntoCommit string     `json:"onto_commit"`
	Merged     []Merged   `json:"merged"`
	Conflicts  []Conflict `json:"conflicts"`
	Skipped    []Skipped  `json:"skipped"`
	Commit     string     `json:"commit"` // also at refs/carya/integration
	Worktree   string     `json:"worktree,omitempty"`
	Steps      []Step     `json:"steps"`
	LogPath    string     `json:"log_path,omitempty"`
	// OK is true when every state merged and every step passed.
	OK bool `json:"ok"`
}

// Failed returns the first failing step, or nil.
func (r *Result) Failed() *Step {
	for i := range r.Steps {
		if r.Steps[i].ExitCode != 0 {
			return &r.Steps[i]
		}
	}
	return nil
}

// Merge folds every published state into the onto ref, oldest publish
// first, entirely in memory. Each merge is recorded as a commit whose
// parents are the result so far and the state, so git finds the true merge
// base for every later state. A state that conflicts is left out and
// reported, and the rest still merge.
func Merge(repoRoot string, opts Options) (*Result, error) {
	refs := git.NewRefManager(repoRoot)
	predictor := git.NewConflictPredictor(repoRoot)

	res := &Result{Onto: opts.Onto, Merged: []Merged{}, Conflicts: []Conflict{}, Skipped: []Skipped{}, Steps: []Step{}}
	if res.Onto == "" {
		res.Onto = refs.IntegrationBranch()
	}
	onto, err := refs.ResolveRef(res.Onto + "^{commit}")
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", res.Onto, err)
	}
	res.OntoCommit = onto

	states, err := refs.ListUserRefs()
	if err != nil {
		return nil, err
	}
	only := set(opts.Only)
	exclude := set(opts.Exclude)

	type candidate struct {
		state     git.UserRef
		published time.Time
	}
	var candidates []candidate
	for _, st := range states {
		if (len(only) > 0 && !only[st.UserID]) || exclude[st.UserID] {
			continue
		}
		if !st.HasBase() {
			res.Skipped = append(res.Skipped, Skipped{st.UserID, "published by an older carya without a base; republish it"})
			continue
		}
		published := commitTime(repoRoot, st.Commit)
		if opts.Since > 0 && !published.IsZero() && time.Since(published) > opts.Since {
			age := time.Since(published).Round(time.Minute)
			res.Skipped = append(res.Skipped, Skipped{st.UserID, fmt.Sprintf("published %s ago, older than --since", age)})
			continue
		}
		candidates = append(candidates, candidate{st, published})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if !candidates[i].published.Equal(candidates[j].published) {
			return candidates[i].published.Before(candidates[j].published)
		}
		return candidates[i].state.UserID < candidates[j].state.UserID
	})

	current := onto
	currentTree, err := treeOf(repoRoot, current)
	if err != nil {
		return nil, err
	}
	changed := map[string][]string{} // merged user -> files their own work changes

	for _, c := range candidates {
		st := c.state
		report, err := predictor.MergeCommits(current, st.Commit)
		if err != nil {
			res.Skipped = append(res.Skipped, Skipped{st.UserID, "could not merge: " + firstLine(err.Error())})
			continue
		}
		if report.HasConflicts {
			conflict := Conflict{UserID: st.UserID, Files: []string{}, With: []string{}}
			for _, f := range report.ConflictedFiles {
				conflict.Files = append(conflict.Files, f.Path)
			}
			conflict.With = attribute(conflict.Files, res.Merged, changed, res.Onto)
			res.Conflicts = append(res.Conflicts, conflict)
			continue
		}
		if report.MergedTreeHash == currentTree {
			res.Skipped = append(res.Skipped, Skipped{st.UserID, "nothing that is not already in " + res.Onto})
			continue
		}

		next, err := refs.CommitTree(report.MergedTreeHash, "carya integration: "+st.UserID, current, st.Commit)
		if err != nil {
			return nil, err
		}
		current, currentTree = next, report.MergedTreeHash
		res.Merged = append(res.Merged, Merged{UserID: st.UserID, Commit: st.Commit, Published: c.published})
		changed[st.UserID], _ = changedFiles(repoRoot, st.BaseCommit, st.TreeHash)
	}

	res.Commit = current
	if err := refs.UpdateRef(IntegrationRef, current); err != nil {
		return nil, err
	}
	return res, nil
}

// attribute names who else changes the conflicted files: merged states
// whose own work touches them, or the onto ref when none does.
func attribute(files []string, merged []Merged, changed map[string][]string, onto string) []string {
	want := set(files)
	var with []string
	for _, m := range merged {
		for _, f := range changed[m.UserID] {
			if want[f] {
				with = append(with, m.UserID)
				break
			}
		}
	}
	if len(with) == 0 {
		with = []string{onto}
	}
	return with
}

func changedFiles(repoRoot, base, tree string) ([]string, error) {
	cmd := exec.Command("git", "diff-tree", "-r", "-z", "--no-renames", "--name-only", base, tree)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

func treeOf(repoRoot, commit string) (string, error) {
	cmd := exec.Command("git", "rev-parse", commit+"^{tree}")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("resolve tree of %s: %w", commit, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func commitTime(repoRoot, commit string) time.Time {
	cmd := exec.Command("git", "show", "-s", "--format=%ct", commit)
	cmd.Dir = repoRoot
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

func set(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, it := range items {
		m[it] = true
	}
	return m
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
