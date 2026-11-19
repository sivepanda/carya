package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreToolUseTellsEachSessionAboutAChangeOnce(t *testing.T) {
	repo, other := twoAgents(t)
	edit := func(session string) string {
		t.Helper()
		return hook(t, EventPreToolUse, map[string]any{
			"session_id": session,
			"cwd":        repo,
			"tool_name":  "Edit",
			"tool_input": map[string]any{"file_path": filepath.Join(repo, "shared.txt")},
		}, HookOptions{})
	}

	if out := edit("s1"); !strings.Contains(out, "+from b") {
		t.Fatalf("first edit should get the peer's change:\n%s", out)
	}
	if out := edit("s1"); out != "" {
		t.Fatalf("the same session should not hear it again:\n%s", out)
	}
	if out := edit("s2"); !strings.Contains(out, "+from b") {
		t.Fatalf("another session should get the note:\n%s", out)
	}

	// The peer changes the file again: s1 gets one line, not the diff again.
	if err := os.WriteFile(filepath.Join(other, "shared.txt"), []byte("from b, again\n"), 0644); err != nil {
		t.Fatal(err)
	}
	hook(t, EventPostToolUse, map[string]any{"cwd": other, "tool_name": "Bash"}, HookOptions{})
	out := edit("s1")
	if !strings.Contains(out, "updated their unfinished change to shared.txt") || strings.Contains(out, "+from b") {
		t.Fatalf("an update should be one line without the diff:\n%s", out)
	}
	if out := edit("s3"); !strings.Contains(out, "+from b, again") {
		t.Fatalf("a session new to it gets the full diff:\n%s", out)
	}

	// Without a session ID there is nothing to remember against.
	if a, b := edit(""), edit(""); a == "" || b == "" {
		t.Fatal("without a session ID every edit should get the note")
	}
}

func TestEnsureBase(t *testing.T) {
	isolate(t)
	repo := newRepo(t)
	branch := gitRun(t, repo, "symbolic-ref", "--short", "HEAD")
	if got, err := EnsureBase(repo, ""); err != nil || got != branch {
		t.Fatalf("unset: got %q %v, want the current branch %q", got, err, branch)
	}
	gitRun(t, repo, "checkout", "-q", "-b", "other")
	if got, _ := EnsureBase(repo, ""); got != branch {
		t.Fatalf("an existing setting should be kept, got %q", got)
	}
	if got, err := EnsureBase(repo, "other"); err != nil || got != "other" {
		t.Fatalf("explicit: got %q %v", got, err)
	}
	if _, err := EnsureBase(repo, "no-such-branch"); err == nil {
		t.Fatal("an unknown branch should be refused")
	}
}

// Subagents share their parent's session ID; each must get its own notes.
func TestSubagentsInOneSessionEachGetTheNote(t *testing.T) {
	repo, _ := twoAgents(t)
	edit := func(agentID string) string {
		t.Helper()
		return hook(t, EventPreToolUse, map[string]any{
			"session_id": "shared-session",
			"agent_id":   agentID,
			"cwd":        repo,
			"tool_name":  "Edit",
			"tool_input": map[string]any{"file_path": filepath.Join(repo, "shared.txt")},
		}, HookOptions{})
	}
	if out := edit("agent-1"); !strings.Contains(out, "+from b") {
		t.Fatalf("agent-1 should get the note:\n%s", out)
	}
	if out := edit("agent-2"); !strings.Contains(out, "+from b") {
		t.Fatalf("agent-2 should get its own note:\n%s", out)
	}
	if out := edit("agent-1"); out != "" {
		t.Fatalf("agent-1 already heard it:\n%s", out)
	}
}
