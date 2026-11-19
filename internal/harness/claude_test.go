package harness

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("CARYA_AGENT", "")
	t.Setenv("CLAUDE_CODE_REMOTE", "")
}

func newRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "shared.txt"), []byte("shared\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "-c", "user.name=T", "-c", "user.email=t@e", "commit", "-q", "-m", "init")
	return repo
}

func hook(t *testing.T, event string, input map[string]any, opts HookOptions) string {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := RunClaudeHook(event, bytes.NewReader(data), &out, opts); err != nil {
		t.Fatalf("%s hook: %v", event, err)
	}
	return out.String()
}

func TestToolsHintIsOneLineNamingTheCommands(t *testing.T) {
	if strings.Count(toolsHint, "\n") != 1 || !strings.HasSuffix(toolsHint, "\n") {
		t.Fatalf("the hint is added to every session, so it must be one line: %q", toolsHint)
	}
	for _, cmd := range []string{"`carya related <file>`", "`carya why <file>[:line]`", "`carya hunks`", "`carya commit -m <msg> <ids|files>`"} {
		if !strings.Contains(toolsHint, cmd) {
			t.Errorf("the hint should name %s: %q", cmd, toolsHint)
		}
	}
}

func TestClaudeHooksWarnBeforeEditingAPeersFile(t *testing.T) {
	isolate(t)
	repo := newRepo(t)
	other := filepath.Join(t.TempDir(), "other")
	gitRun(t, repo, "worktree", "add", "-q", other)

	// Agent B starts, edits shared.txt with a shell command, and its
	// PostToolUse hook publishes the result.
	if out := hook(t, EventSessionStart, map[string]any{"cwd": other, "source": "startup"}, HookOptions{}); out != toolsHint {
		t.Fatalf("nobody else is working yet, so only the tools hint is expected, got:\n%s", out)
	}
	if err := os.WriteFile(filepath.Join(other, "shared.txt"), []byte("from b\n"), 0644); err != nil {
		t.Fatal(err)
	}
	hook(t, EventPostToolUse, map[string]any{"cwd": other, "tool_name": "Bash", "tool_input": map[string]any{"command": "sed -i ..."}}, HookOptions{})

	// Agent A starts and is told B is working.
	start := hook(t, EventSessionStart, map[string]any{"cwd": repo, "source": "startup"}, HookOptions{})
	if !strings.Contains(start, "shared.txt") || !strings.Contains(start, "claude-") {
		t.Fatalf("session start should mention B's work:\n%s", start)
	}

	// A goes to edit shared.txt and gets B's change as context.
	out := hook(t, EventPreToolUse, map[string]any{
		"cwd":        repo,
		"tool_name":  "Edit",
		"tool_input": map[string]any{"file_path": filepath.Join(repo, "shared.txt"), "old_string": "shared", "new_string": "a"},
	}, HookOptions{})
	var resp struct {
		HookSpecificOutput struct {
			HookEventName      string `json:"hookEventName"`
			AdditionalContext  string `json:"additionalContext"`
			PermissionDecision string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("pre-tool-use output is not JSON: %v\n%s", err, out)
	}
	ctx := resp.HookSpecificOutput
	if ctx.HookEventName != "PreToolUse" || ctx.PermissionDecision != "" || !strings.Contains(ctx.AdditionalContext, "+from b") {
		t.Fatalf("unexpected pre-tool-use response: %+v", ctx)
	}

	// Commands and files nobody else touches add nothing.
	if out := hook(t, EventPreToolUse, map[string]any{"cwd": repo, "tool_name": "Bash", "tool_input": map[string]any{"command": "ls"}}, HookOptions{}); out != "" {
		t.Fatalf("Bash should not be checked, got %s", out)
	}
	if out := hook(t, EventPreToolUse, map[string]any{"cwd": repo, "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(repo, "fresh.txt")}}, HookOptions{}); out != "" {
		t.Fatalf("an untouched file should add nothing, got %s", out)
	}
}

func TestUserScopeHooksWaitForOptIn(t *testing.T) {
	isolate(t)
	repo := newRepo(t)
	opts := HookOptions{RequireOptIn: true}

	hook(t, EventSessionStart, map[string]any{"cwd": repo}, opts)
	if _, err := os.Stat(filepath.Join(repo, ".carya")); !os.IsNotExist(err) {
		t.Fatal("a repository that has not opted in must be left alone")
	}

	if err := OptIn(repo); err != nil {
		t.Fatal(err)
	}
	hook(t, EventSessionStart, map[string]any{"cwd": repo}, opts)
	if _, err := os.Stat(filepath.Join(repo, ".carya", "user-id")); err != nil {
		t.Fatalf("opted-in repository should be set up: %v", err)
	}
}

func TestInstallClaudeHooksMergesAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	original := `{
  "model": "opus",
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "./lint.sh"}]}
    ],
    "Stop": [{"hooks": [{"type": "command", "command": "notify"}]}]
  },
  "permissions": {"allow": ["Bash(go test:*)"]}
}
`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}

	changed, err := InstallClaudeHooks(path, "/opt/my tools/carya", false)
	if err != nil || !changed {
		t.Fatalf("install: changed=%v err=%v", changed, err)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	for _, want := range []string{`"./lint.sh"`, `"notify"`, `Bash(go test:*)`, `'/opt/my tools/carya' hook claude pre-tool-use`, `"Edit|Write|NotebookEdit"`, `hook claude session-start`, `hook claude post-tool-use`} {
		if !strings.Contains(text, want) {
			t.Errorf("settings lost or missed %s:\n%s", want, text)
		}
	}
	if !(strings.Index(text, `"model"`) < strings.Index(text, `"hooks"`) && strings.Index(text, `"hooks"`) < strings.Index(text, `"permissions"`)) {
		t.Errorf("top-level key order changed:\n%s", text)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0600 {
		t.Errorf("file mode changed to %v", info.Mode().Perm())
	}

	if changed, err := InstallClaudeHooks(path, "/opt/my tools/carya", false); err != nil || changed {
		t.Fatalf("second install should be a no-op: changed=%v err=%v", changed, err)
	}
	if _, err := InstallClaudeHooks(path, "carya", true); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if strings.Count(string(data), "hook claude pre-tool-use") != 1 || !strings.Contains(string(data), "carya hook claude pre-tool-use --require-opt-in") {
		t.Fatalf("reinstall should replace Carya's hooks, not add more:\n%s", data)
	}

	if changed, err := RemoveClaudeHooks(path); err != nil || !changed {
		t.Fatalf("remove: changed=%v err=%v", changed, err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "hook claude") || !strings.Contains(string(data), "./lint.sh") || !strings.Contains(string(data), "notify") {
		t.Fatalf("remove should take out only Carya's hooks:\n%s", data)
	}
}

func TestInstallClaudeHooksCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "settings.local.json")
	if _, err := InstallClaudeHooks(path, "carya", false); err != nil {
		t.Fatal(err)
	}
	var parsed map[string]map[string][]map[string]any
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("not valid settings JSON: %v\n%s", err, data)
	}
	if len(parsed["hooks"]) != 3 {
		t.Fatalf("want 3 hook events, got %v", parsed["hooks"])
	}
	if changed, err := RemoveClaudeHooks(path); err != nil || !changed {
		t.Fatalf("remove: %v %v", changed, err)
	}
	data, _ = os.ReadFile(path)
	if strings.TrimSpace(string(data)) != "{}" {
		t.Fatalf("removing the only hooks should leave an empty object, got %s", data)
	}
}
