package harness

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyPatchPaths(t *testing.T) {
	patch := strings.Join([]string{
		"*** Begin Patch",
		"*** Update File: src/app.go",
		"*** Move to: src/main.go",
		"@@ func main()",
		"-old",
		"+new",
		"*** Add File: docs/new file.md",
		"+hello",
		"*** Delete File: old.txt\r",
		"*** Update File: src/app.go",
		"*** End Patch",
	}, "\n")
	got := ApplyPatchPaths(patch)
	want := []string{"src/app.go", "src/main.go", "docs/new file.md", "old.txt"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("ApplyPatchPaths = %q, want %q", got, want)
	}
	if got := ApplyPatchPaths("echo hi"); len(got) != 0 {
		t.Fatalf("no headers, want no paths, got %q", got)
	}
}

// twoAgents sets up a repository with a second worktree whose agent has
// changed shared.txt and published it.
func twoAgents(t *testing.T) (repo, other string) {
	t.Helper()
	isolate(t)
	repo = newRepo(t)
	other = filepath.Join(t.TempDir(), "other")
	gitRun(t, repo, "worktree", "add", "-q", other)
	if err := os.WriteFile(filepath.Join(other, "shared.txt"), []byte("from b\n"), 0644); err != nil {
		t.Fatal(err)
	}
	hook(t, EventPostToolUse, map[string]any{"cwd": other, "tool_name": "Bash"}, HookOptions{})
	return repo, other
}

func TestCodexPreToolUseReadsPatchPaths(t *testing.T) {
	repo, _ := twoAgents(t)
	sub := filepath.Join(repo, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}

	run := func(input map[string]any) string {
		t.Helper()
		data, _ := json.Marshal(input)
		var out bytes.Buffer
		if err := RunCodexHook(EventPreToolUse, bytes.NewReader(data), &out, HookOptions{}); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	// Codex sessions can run in a subdirectory; patch paths are relative to it.
	out := run(map[string]any{
		"cwd":        sub,
		"tool_name":  "apply_patch",
		"tool_input": map[string]any{"command": "*** Begin Patch\n*** Update File: ../shared.txt\n@@\n-shared\n+c\n*** End Patch\n"},
	})
	var resp struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if resp.HookSpecificOutput.HookEventName != "PreToolUse" || !strings.Contains(resp.HookSpecificOutput.AdditionalContext, "+from b") {
		t.Fatalf("unexpected response: %+v", resp)
	}

	if out := run(map[string]any{"cwd": repo, "tool_name": "Bash", "tool_input": map[string]any{"command": "cat shared.txt"}}); out != "" {
		t.Fatalf("shell commands are not checked before running, got %s", out)
	}
	id, _ := os.ReadFile(filepath.Join(repo, ".carya", "user-id"))
	if !strings.HasPrefix(string(id), "codex-") {
		t.Fatalf("Codex checkout should get a codex identity, got %q", id)
	}
}

func TestOpenCodePostToolReportsAfterEdits(t *testing.T) {
	repo, _ := twoAgents(t)
	run := func(input map[string]any) string {
		t.Helper()
		data, _ := json.Marshal(input)
		var out bytes.Buffer
		if err := RunOpenCodeHook(EventOpenCodePostTool, bytes.NewReader(data), &out, HookOptions{}); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	if err := os.WriteFile(filepath.Join(repo, "shared.txt"), []byte("from a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range []map[string]any{
		{"filePath": filepath.Join(repo, "shared.txt")}, // 1.x edit/write
		{"path": "shared.txt"},                          // 2.x edit/write
		{"patchText": "*** Begin Patch\n*** Update File: shared.txt\n*** End Patch"},
	} {
		out := run(map[string]any{"cwd": repo, "tool": "edit", "args": args})
		if !strings.Contains(out, "CONFLICT") || !strings.Contains(out, "+from b") {
			t.Fatalf("args %v: expected a conflict report, got:\n%s", args, out)
		}
	}
	if out := run(map[string]any{"cwd": repo, "tool": "bash", "args": map[string]any{"command": "ls"}}); out != "" {
		t.Fatalf("bash should only snapshot, got %s", out)
	}
}

func TestOpenCodePluginInstall(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".opencode", "plugins", "carya.ts")
	changed, err := InstallOpenCodePlugin(path, `/opt/my "tools"/carya`, true)
	if err != nil || !changed {
		t.Fatalf("install: %v %v", changed, err)
	}
	src, _ := os.ReadFile(path)
	for _, want := range []string{openCodeMarker, `const CARYA = "/opt/my \"tools\"/carya"`, `"--require-opt-in"`, `"tool.execute.after"`, `ctx.tool.hook("execute.after"`} {
		if !strings.Contains(string(src), want) {
			t.Errorf("plugin is missing %s", want)
		}
	}
	if changed, err := InstallOpenCodePlugin(path, `/opt/my "tools"/carya`, true); err != nil || changed {
		t.Fatalf("reinstall should be a no-op: %v %v", changed, err)
	}
	if changed, err := RemoveOpenCodePlugin(path); err != nil || !changed {
		t.Fatalf("remove: %v %v", changed, err)
	}

	if err := os.WriteFile(path, []byte("export default {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallOpenCodePlugin(path, "carya", false); err == nil {
		t.Fatal("must not overwrite someone else's plugin")
	}
	if _, err := RemoveOpenCodePlugin(path); err == nil {
		t.Fatal("must not delete someone else's plugin")
	}
}

func TestCodexHooksFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".codex", "hooks.json")
	if _, err := InstallCodexHooks(path, "carya", false); err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid hooks.json: %v\n%s", err, data)
	}
	if pre := parsed.Hooks["PreToolUse"]; len(pre) != 1 || pre[0].Matcher != "^apply_patch$" || pre[0].Hooks[0].Command != "carya hook codex pre-tool-use" {
		t.Fatalf("unexpected PreToolUse: %+v", pre)
	}
	if post := parsed.Hooks["PostToolUse"]; len(post) != 1 || post[0].Matcher != "" {
		t.Fatalf("PostToolUse should match every tool: %+v", post)
	}
	// Claude's hooks in the same file would be left alone, and vice versa.
	if _, err := InstallClaudeHooks(path, "carya", false); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveCodexHooks(path); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "hook codex") || !strings.Contains(string(data), "hook claude") {
		t.Fatalf("removing Codex hooks should keep Claude's:\n%s", data)
	}
}
