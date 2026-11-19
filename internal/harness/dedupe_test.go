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

	// The peer changes the file again: that is news.
	if err := os.WriteFile(filepath.Join(other, "shared.txt"), []byte("from b, again\n"), 0644); err != nil {
		t.Fatal(err)
	}
	hook(t, EventPostToolUse, map[string]any{"cwd": other, "tool_name": "Bash"}, HookOptions{})
	if out := edit("s1"); !strings.Contains(out, "+from b, again") {
		t.Fatalf("a new peer change should be reported:\n%s", out)
	}

	// Without a session ID there is nothing to remember against.
	if a, b := edit(""), edit(""); a == "" || b == "" {
		t.Fatal("without a session ID every edit should get the note")
	}
}
