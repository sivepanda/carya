// Package harness adapts Carya's agent operations to each coding-agent
// harness's hook protocol.
package harness

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"carya/internal/agent"
	"carya/internal/peers"
)

// maxContext caps what one hook adds to the model's context.
const maxContext = 6000

// syncInterval is how often hooks start a background push/fetch.
const syncInterval = time.Minute

// HookOptions controls one hook run.
type HookOptions struct {
	// RequireOptIn makes the hook a no-op unless the repository has opted in
	// with `git config carya.agents true`. User-level hooks run in every
	// repository, so they set it.
	RequireOptIn bool
	// StartSync starts a background push/fetch of states for the checkout at
	// root. Nil disables syncing.
	StartSync func(root string)
	// Now is the clock, for tests.
	Now func() time.Time
}

func (o HookOptions) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// begin prepares one hook run in the checkout containing dir: it honours
// opt-in, sets the checkout up for agent, and sends Carya's logs to the
// checkout's log file. It returns a nil session when the hook should do
// nothing. Call done when the hook finishes; it may start a background sync.
func begin(dir, agentName string, opts HookOptions) (*agent.Session, func(), error) {
	if dir == "" {
		dir, _ = os.Getwd()
	}
	if opts.RequireOptIn && !OptedIn(dir) {
		return nil, func() {}, nil
	}
	s, err := agent.Bootstrap(dir, agentName)
	if err != nil {
		return nil, func() {}, err
	}

	restore := func() {}
	if f, err := os.OpenFile(s.Repo.LogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
		prev := log.Writer()
		log.SetOutput(f)
		restore = func() {
			log.SetOutput(prev)
			f.Close()
		}
	}
	done := func() {
		if opts.StartSync != nil && s.ClaimSync(syncInterval) {
			opts.StartSync(s.Repo.RootPath())
		}
		restore()
	}
	return s, done, nil
}

// absPaths makes paths absolute against dir, the directory the agent's tool
// paths are relative to.
func absPaths(dir string, paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) && dir != "" {
			p = filepath.Join(dir, p)
		}
		out = append(out, p)
	}
	return out
}

// editReport is what an agent is told about peers before (or right after)
// it edits paths, or "" when nobody else is changing them.
//
// With a session key, notes are not repeated: a peer change the agent was
// already shown is left out, and a newer version of a peer's change to a file
// it already heard about is one line (with the current conflict verdict)
// rather than the whole diff again.
func editReport(s *agent.Session, paths []string, now time.Time, session string) (string, error) {
	changes, err := s.Check(paths)
	if err != nil || len(changes) == 0 {
		return "", err
	}
	fresh, updated := unseen(s, session, changes)
	if len(fresh)+len(updated) == 0 {
		return "", nil
	}
	var b strings.Builder
	b.WriteString("Carya: ")
	b.WriteString(peers.Summary(fresh, now))
	for _, ch := range updated {
		verdict := "it still merges cleanly with your changes"
		if ch.Conflict {
			verdict = "it would CONFLICT with your changes"
		}
		fmt.Fprintf(&b, "%s updated their unfinished change to %s (published %s ago); %s. Run `carya check %s` to see it.\n",
			ch.UserID, ch.Path, now.Sub(ch.Published).Round(time.Second), verdict, ch.Path)
	}
	report := b.String()
	if len(report) > maxContext {
		report = strings.ToValidUTF8(report[:maxContext], "") +
			"\n… (truncated; run `carya check " + changes[0].Path + "` for the full diff)\n"
	}
	return report, nil
}

// sessionKey identifies who is told: the session, and the subagent within
// it, since subagents share their parent's session ID.
func sessionKey(sessionID, agentID string) string {
	if sessionID == "" {
		return ""
	}
	if agentID == "" {
		return sessionID
	}
	return sessionID + "/" + agentID
}

// unseen sorts changes into ones the session has never heard of (fresh),
// newer versions of changes it already heard about (updated), and repeats,
// which are dropped. It records what it returns as seen.
func unseen(s *agent.Session, session string, changes []peers.Change) (fresh, updated []peers.Change) {
	if session == "" {
		return changes, nil
	}
	sum := sha1.Sum([]byte(session))
	path := filepath.Join(s.Repo.CaryaPath(), "notified", hex.EncodeToString(sum[:8])+".json")
	seen := map[string]bool{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &seen)
	}
	digest := func(parts ...any) string {
		k := sha1.Sum([]byte(fmt.Sprint(parts...)))
		return hex.EncodeToString(k[:])
	}
	for _, ch := range changes {
		exact := digest("exact\x00", ch.Path, "\x00", ch.UserID, "\x00", ch.Status, "\x00", ch.Conflict, "\x00", ch.Patch)
		who := digest("who\x00", ch.Path, "\x00", ch.UserID)
		switch {
		case seen[exact]:
			continue
		case seen[who]:
			updated = append(updated, ch)
		default:
			fresh = append(fresh, ch)
		}
		seen[exact], seen[who] = true, true
	}
	if len(fresh)+len(updated) > 0 {
		if data, err := json.Marshal(seen); err == nil && os.MkdirAll(filepath.Dir(path), 0755) == nil {
			_ = os.WriteFile(path, data, 0644)
		}
	}
	return fresh, updated
}

// toolsHint tells the agent which history and commit commands it can run.
// It is the only thing added to every session, so it stays one line; the
// answers come only when asked.
const toolsHint = "Carya: before changing a file, `carya related <file>` lists files that usually change with it and `carya why <file>[:line]` shows why it changed and who is changing it now; to commit in steps, `carya hunks` lists your uncommitted changes as hunks and `carya commit -m <msg> <ids|files|id:lines>` commits one group.\n"

// sessionOverview is the session-start note: the history commands, then
// who else has unfinished work in the repository, if anyone. Per-file detail
// arrives only when the agent goes to edit a file, to keep session context
// small.
func sessionOverview(s *agent.Session, now time.Time) (string, error) {
	states, err := peers.NewChecker(s.Repo.RootPath()).Overview(s.UserID)
	if err != nil {
		return toolsHint, err
	}
	if len(states) == 0 {
		return toolsHint, nil
	}
	var b strings.Builder
	b.WriteString(toolsHint)
	fmt.Fprintf(&b, "Carya: other agents have unfinished work in this repository (you are %s).\n", s.UserID)
	for i, st := range states {
		if i == 5 {
			fmt.Fprintf(&b, "- and %d more\n", len(states)-i)
			break
		}
		files := st.Files
		more := ""
		if len(files) > 5 {
			more = fmt.Sprintf(", +%d more", len(files)-5)
			files = files[:5]
		}
		fmt.Fprintf(&b, "- %s: %d file(s) (%s%s)", st.UserID, len(st.Files), strings.Join(files, ", "), more)
		if !st.Published.IsZero() {
			fmt.Fprintf(&b, ", published %s ago", now.Sub(st.Published).Round(time.Second))
		}
		b.WriteString("\n")
	}
	b.WriteString("When you edit a file one of them is changing, Carya shows you their change. Run `carya check <path>` to look yourself.\n")
	return b.String(), nil
}

// OptedIn reports whether the repository containing dir has opted in to
// agent hooks with `git config carya.agents true`. Repository config is
// shared by all worktrees.
func OptedIn(dir string) bool {
	cmd := exec.Command("git", "config", "--type=bool", "--get", "carya.agents")
	cmd.Dir = dir
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// OptIn records that the repository containing dir uses agent hooks.
func OptIn(dir string) error {
	cmd := exec.Command("git", "config", "--local", "carya.agents", "true")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git config carya.agents: %w\n%s", err, out)
	}
	return nil
}

// EnsureBase records the integration branch agents' work is measured
// against and merged into (`git config carya.base`), shared by every
// worktree. An explicit branch always wins; otherwise an existing setting is
// kept, and if there is none the branch checked out in dir is used. It
// returns the branch in effect, or "" when none could be chosen (detached
// HEAD and nothing set).
func EnsureBase(dir, branch string) (string, error) {
	current := func() string {
		cmd := exec.Command("git", "config", "--get", "carya.base")
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	if branch == "" {
		if b := current(); b != "" {
			return b, nil
		}
		cmd := exec.Command("git", "symbolic-ref", "--quiet", "--short", "HEAD")
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			return "", nil
		}
		branch = strings.TrimSpace(string(out))
	}
	check := exec.Command("git", "rev-parse", "--verify", "--quiet", branch+"^{commit}")
	check.Dir = dir
	if check.Run() != nil {
		return "", fmt.Errorf("%q is not a branch or commit here", branch)
	}
	set := exec.Command("git", "config", "--local", "carya.base", branch)
	set.Dir = dir
	if out, err := set.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git config carya.base: %w\n%s", err, out)
	}
	return branch, nil
}
