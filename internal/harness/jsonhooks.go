package harness

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
)

// Hook events Carya handles for JSON-hook harnesses, as passed to
// `carya hook <harness> <event>`.
const (
	EventSessionStart = "session-start"
	EventPreToolUse   = "pre-tool-use"
	EventPostToolUse  = "post-tool-use"
)

// Claude Code and Codex share a hook protocol: one JSON object on stdin with
// cwd, tool_name and tool_input; plain stdout from SessionStart becomes
// context; PreToolUse may answer {"hookSpecificOutput": {"hookEventName":
// "PreToolUse", "additionalContext": ...}} to add context without blocking.
type jsonHookInput struct {
	SessionID string          `json:"session_id"`
	AgentID   string          `json:"agent_id"`
	CWD       string          `json:"cwd"`
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
}

type jsonAdapter struct {
	harness string // agent name, and the <harness> in `carya hook <harness>`
	// editPaths returns the files a tool call is about to change, or nil
	// when the call is not an edit.
	editPaths func(tool string, input json.RawMessage) ([]string, error)
}

var claudeAdapter = jsonAdapter{harness: "claude", editPaths: claudeEditPaths}
var codexAdapter = jsonAdapter{harness: "codex", editPaths: codexEditPaths}

// RunClaudeHook handles one Claude Code hook invocation: it reads the hook
// input from stdin and writes any response to stdout. It never blocks the
// agent: callers log failures and exit quietly.
func RunClaudeHook(event string, stdin io.Reader, stdout io.Writer, opts HookOptions) error {
	return claudeAdapter.run(event, stdin, stdout, opts)
}

// RunCodexHook is RunClaudeHook for Codex.
func RunCodexHook(event string, stdin io.Reader, stdout io.Writer, opts HookOptions) error {
	return codexAdapter.run(event, stdin, stdout, opts)
}

func (a jsonAdapter) run(event string, stdin io.Reader, stdout io.Writer, opts HookOptions) error {
	var in jsonHookInput
	if err := json.NewDecoder(stdin).Decode(&in); err != nil && err != io.EOF {
		return fmt.Errorf("read hook input: %w", err)
	}
	s, done, err := begin(in.CWD, a.harness, opts)
	if err != nil || s == nil {
		return err
	}
	defer done()

	switch event {
	case EventSessionStart:
		if _, err := s.Snapshot(); err != nil {
			log.Printf("carya hook: snapshot at session start: %v", err)
		}
		overview, err := sessionOverview(s, opts.now())
		if err != nil || overview == "" {
			return err
		}
		_, err = io.WriteString(stdout, overview)
		return err

	case EventPreToolUse:
		paths, err := a.editPaths(in.ToolName, in.ToolInput)
		if err != nil || len(paths) == 0 {
			return err
		}
		report, err := editReport(s, absPaths(in.CWD, paths), opts.now(), sessionKey(in.SessionID, in.AgentID))
		if err != nil || report == "" {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]any{
			"hookSpecificOutput": map[string]any{
				"hookEventName":     "PreToolUse",
				"additionalContext": report,
			},
		})

	case EventPostToolUse:
		if _, err := s.Snapshot(); err != nil {
			log.Printf("carya hook: snapshot after %s: %v", in.ToolName, err)
		}
		return nil

	default:
		return fmt.Errorf("unknown %s hook event %q", a.harness, event)
	}
}

func claudeEditPaths(tool string, input json.RawMessage) ([]string, error) {
	switch tool {
	case "Edit", "Write", "NotebookEdit":
	default:
		return nil, nil
	}
	var ti struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
	}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &ti); err != nil {
			return nil, fmt.Errorf("read %s input: %w", tool, err)
		}
	}
	if ti.FilePath != "" {
		return []string{ti.FilePath}, nil
	}
	if ti.NotebookPath != "" {
		return []string{ti.NotebookPath}, nil
	}
	return nil, nil
}

// codexEditPaths reads the files a Codex apply_patch call touches from the
// patch text in tool_input.command.
func codexEditPaths(tool string, input json.RawMessage) ([]string, error) {
	switch tool {
	case "apply_patch", "Edit", "Write":
	default:
		return nil, nil
	}
	var ti struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
	}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &ti); err != nil {
			return nil, fmt.Errorf("read %s input: %w", tool, err)
		}
	}
	if ti.FilePath != "" {
		return []string{ti.FilePath}, nil
	}
	return ApplyPatchPaths(ti.Command), nil
}
