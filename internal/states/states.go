// Package states lists published working states and prunes the stale ones:
// agents that went away, and work that has since landed.
package states

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"carya/internal/git"
)

// State is one published state, merged across its local and fetched refs.
type State struct {
	UserID    string    `json:"user_id"`
	Commit    string    `json:"commit,omitempty"`
	Base      string    `json:"base,omitempty"`
	Published time.Time `json:"published,omitempty"`
	Files     int       `json:"files"`  // files changed against its own base
	Landed    bool      `json:"landed"` // the onto ref already contains all of it
	Local     bool      `json:"local"`  // a local ref exists
	OnRemote  bool      `json:"on_remote"`
	Mine      bool      `json:"mine"`
	refs      []git.StateRef
}

// Age is how long ago the state was published.
func (s State) Age(now time.Time) time.Duration {
	if s.Published.IsZero() {
		return 0
	}
	return now.Sub(s.Published)
}

// List returns every published state, newest first. onto is the ref used to
// decide whether a state's work has landed; me marks the caller's own state.
func List(repoRoot, me, onto string) ([]State, error) {
	refs := git.NewRefManager(repoRoot)
	all, err := refs.ListStateRefs()
	if err != nil {
		return nil, err
	}
	ontoCommit, _ := refs.ResolveRef(onto + "^{commit}")
	predictor := git.NewConflictPredictor(repoRoot)

	byUser := map[string]*State{}
	var order []string
	for _, r := range all {
		st, ok := byUser[r.UserID]
		if !ok {
			st = &State{UserID: r.UserID, Mine: r.UserID == me}
			byUser[r.UserID] = st
			order = append(order, r.UserID)
		}
		st.refs = append(st.refs, r)
		if r.Remote {
			st.OnRemote = true
		} else {
			st.Local = true
		}
		// The local ref is the newest copy when there is one.
		if st.Commit == "" || !r.Remote {
			st.Commit, st.Base = r.Commit, r.BaseCommit
			st.Published = commitTime(repoRoot, r.Commit)
			st.Files = changedFiles(repoRoot, r.UserRef)
			st.Landed = r.HasBase() && ontoCommit != "" && landed(repoRoot, predictor, ontoCommit, r.Commit)
		}
	}

	out := make([]State, 0, len(order))
	for _, id := range order {
		out = append(out, *byUser[id])
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Published.Equal(out[j].Published) {
			return out[i].Published.After(out[j].Published)
		}
		return out[i].UserID < out[j].UserID
	})
	return out, nil
}

// landed reports whether merging the state onto ontoCommit adds nothing.
func landed(repoRoot string, predictor *git.ConflictPredictor, ontoCommit, state string) bool {
	report, err := predictor.MergeCommits(ontoCommit, state)
	if err != nil || report.HasConflicts {
		return false
	}
	cmd := exec.Command("git", "rev-parse", ontoCommit+"^{tree}")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == report.MergedTreeHash
}

// PruneOptions says which states are stale.
type PruneOptions struct {
	OlderThan time.Duration // states not republished for this long; zero disables age
	Landed    bool          // states whose work the onto ref already contains
	Users     []string      // prune exactly these users, regardless of age
	Now       time.Time
}

// Candidate is a state chosen for pruning, with the reason.
type Candidate struct {
	State
	Reason string `json:"reason"`
}

// Select picks the states to prune. The caller's own state is never picked
// (the next snapshot would republish it anyway) unless named in Users.
func Select(all []State, opts PruneOptions) []Candidate {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	named := map[string]bool{}
	for _, u := range opts.Users {
		named[u] = true
	}
	var out []Candidate
	for _, st := range all {
		switch {
		case named[st.UserID]:
			out = append(out, Candidate{st, "named"})
		case len(named) > 0 || st.Mine:
		case opts.Landed && st.Landed:
			out = append(out, Candidate{st, "its work has landed"})
		case opts.OlderThan > 0 && !st.Published.IsZero() && st.Age(opts.Now) > opts.OlderThan:
			out = append(out, Candidate{st, fmt.Sprintf("not republished for %s", roundAge(st.Age(opts.Now)))})
		}
	}
	return out
}

// Delete removes the candidates' local refs and fetched copies. With remote,
// it also deletes them on origin first, so the next fetch does not bring
// them back. It returns how many states had a copy left on origin.
func Delete(repoRoot string, candidates []Candidate, remote bool) (leftOnRemote int, err error) {
	if remote {
		var specs []string
		for _, c := range candidates {
			if c.OnRemote {
				specs = append(specs, ":"+git.UserTreeRefPath(c.UserID))
			}
		}
		if len(specs) > 0 {
			cmd := exec.Command("git", append([]string{"push", "--quiet", "origin"}, specs...)...)
			cmd.Dir = repoRoot
			if out, err := cmd.CombinedOutput(); err != nil {
				return 0, fmt.Errorf("delete on origin: %w\n%s", err, out)
			}
		}
	}
	for _, c := range candidates {
		if c.OnRemote && !remote {
			leftOnRemote++
		}
		for _, r := range c.refs {
			cmd := exec.Command("git", "update-ref", "-d", r.Ref)
			cmd.Dir = repoRoot
			if out, err := cmd.CombinedOutput(); err != nil {
				return leftOnRemote, fmt.Errorf("delete %s: %w\n%s", r.Ref, err, out)
			}
		}
	}
	return leftOnRemote, nil
}

func changedFiles(repoRoot string, r git.UserRef) int {
	if !r.HasBase() {
		return 0
	}
	cmd := exec.Command("git", "diff-tree", "-r", "-z", "--no-renames", "--name-only", r.BaseCommit, r.TreeHash)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	n := 0
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			n++
		}
	}
	return n
}

func commitTime(repoRoot, commit string) time.Time {
	if commit == "" {
		return time.Time{}
	}
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

func roundAge(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}

// FormatAge renders an age the way prune and list print it.
func FormatAge(d time.Duration) string { return roundAge(d) }
