// Package harness adapts Carya's agent operations to each coding-agent
// harness's hook protocol.
package harness

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"carya/internal/agent"
	"carya/internal/peers"
)

// Claude Code hook events Carya handles, as passed to `carya hook claude`.
const (
	EventSessionStart = "session-start"
	EventPreToolUse   = "pre-tool-use"
	EventPostToolUse  = "post-tool-use"
)

// Tools whose input names a file the model is about to change.
var claudeEditTools = map[string]bool{"Edit": true, "Write": true, "NotebookEdit": true}

// maxContext caps what one hook adds to the model's context.
const maxContext = 6000

// syncInterval is how often hooks start a background push/fetch.
const syncInterval = time.Minute

type claudeHookInput struct {
	CWD           string          `json:"cwd"`
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	Source        string          `json:"source"`
}

type claudeToolInput struct {
	FilePath     string `json:"file_path"`
	NotebookPath string `json:"notebook_path"`
}

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

// RunClaudeHook handles one Claude Code hook invocation: it reads the hook
// input from stdin and writes any response to stdout. It never blocks the
// agent: failures are logged and the hook exits quietly.
func RunClaudeHook(event string, stdin io.Reader, stdout io.Writer, opts HookOptions) error {
	if opts.Now == nil {
		opts.Now = time.Now
	}

	var in claudeHookInput
	if err := json.NewDecoder(stdin).Decode(&in); err != nil && err != io.EOF {
		return fmt.Errorf("read hook input: %w", err)
	}
	dir := in.CWD
	if dir == "" {
		dir, _ = os.Getwd()
	}
	if opts.RequireOptIn && !OptedIn(dir) {
		return nil
	}

	s, err := agent.Bootstrap(dir, "claude")
	if err != nil {
		return err
	}
	if f, err := os.OpenFile(s.Repo.LogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
		defer f.Close()
		prev := log.Writer()
		log.SetOutput(f)
		defer log.SetOutput(prev)
	}

	defer func() {
		if opts.StartSync != nil && s.ClaimSync(syncInterval) {
			opts.StartSync(s.Repo.RootPath())
		}
	}()

	switch event {
	case EventSessionStart:
		return claudeSessionStart(s, stdout, opts.Now())
	case EventPreToolUse:
		return claudePreToolUse(s, in, stdout, opts.Now())
	case EventPostToolUse:
		if _, err := s.Snapshot(); err != nil {
			log.Printf("carya hook: snapshot after %s: %v", in.ToolName, err)
		}
		return nil
	default:
		return fmt.Errorf("unknown Claude Code hook event %q", event)
	}
}

func claudeSessionStart(s *agent.Session, stdout io.Writer, now time.Time) error {
	if _, err := s.Snapshot(); err != nil {
		log.Printf("carya hook: snapshot at session start: %v", err)
	}
	states, err := peers.NewChecker(s.Repo.RootPath()).Overview(s.UserID)
	if err != nil || len(states) == 0 {
		return err
	}

	// Plain stdout from a SessionStart hook becomes context. Keep it short:
	// the per-file detail arrives only when the agent goes to edit a file.
	var b strings.Builder
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
	b.WriteString("Before you edit a file one of them is changing, Carya adds their change to your context. Run `carya check <path>` to look yourself.\n")
	_, err = io.WriteString(stdout, b.String())
	return err
}

func claudePreToolUse(s *agent.Session, in claudeHookInput, stdout io.Writer, now time.Time) error {
	if !claudeEditTools[in.ToolName] {
		return nil
	}
	var ti claudeToolInput
	if len(in.ToolInput) > 0 {
		if err := json.Unmarshal(in.ToolInput, &ti); err != nil {
			return fmt.Errorf("read %s input: %w", in.ToolName, err)
		}
	}
	path := ti.FilePath
	if path == "" {
		path = ti.NotebookPath
	}
	changes, err := s.Check([]string{path})
	if err != nil || len(changes) == 0 {
		return err
	}

	context := "Carya: " + peers.Summary(changes, now)
	if len(context) > maxContext {
		context = strings.ToValidUTF8(context[:maxContext], "") + "\n… (truncated; run `carya check " + changes[0].Path + "` for the full diff)\n"
	}
	return json.NewEncoder(stdout).Encode(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "PreToolUse",
			"additionalContext": context,
		},
	})
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
