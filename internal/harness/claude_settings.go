package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Claude Code settings scopes `carya setup claude` can write to.
const (
	ScopeLocal   = "local"   // .claude/settings.local.json: this checkout, not committed
	ScopeProject = "project" // .claude/settings.json: committed, for everyone on the repo
	ScopeUser    = "user"    // ~/.claude/settings.json: every repo that opts in
)

// ClaudeSettingsPath returns the settings file for scope.
func ClaudeSettingsPath(scope, repoRoot string) (string, error) {
	switch scope {
	case ScopeLocal:
		return filepath.Join(repoRoot, ".claude", "settings.local.json"), nil
	case ScopeProject:
		return filepath.Join(repoRoot, ".claude", "settings.json"), nil
	case ScopeUser:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".claude", "settings.json"), nil
	default:
		return "", fmt.Errorf("unknown scope %q (want %s, %s or %s)", scope, ScopeLocal, ScopeProject, ScopeUser)
	}
}

// claudeHookSpec is one hook Carya installs.
type claudeHookSpec struct {
	event   string // Claude Code event name
	matcher string
	arg     string // `carya hook claude <arg>`
}

var claudeHookSpecs = []claudeHookSpec{
	{event: "SessionStart", arg: EventSessionStart},
	{event: "PreToolUse", matcher: "Edit|Write|NotebookEdit", arg: EventPreToolUse},
	{event: "PostToolUse", matcher: "Edit|Write|NotebookEdit|Bash", arg: EventPostToolUse},
}

// InstallClaudeHooks merges Carya's hooks into the Claude Code settings file
// at path, creating it if needed. carya is the command that runs Carya.
// Earlier Carya hooks are replaced and everything else in the file is kept
// in its original order. It reports whether the file changed.
func InstallClaudeHooks(path, carya string, requireOptIn bool) (bool, error) {
	return editClaudeHooks(path, func(hooks *orderedObject) error {
		for _, spec := range claudeHookSpecs {
			groups, err := withoutCaryaHooks(hooks.get(spec.event))
			if err != nil {
				return fmt.Errorf("%s hooks: %w", spec.event, err)
			}
			command := shellQuote(carya) + " hook claude " + spec.arg
			if requireOptIn {
				command += " --require-opt-in"
			}
			group := map[string]any{
				"hooks": []any{map[string]any{"type": "command", "command": command, "timeout": 30}},
			}
			if spec.matcher != "" {
				group["matcher"] = spec.matcher
			}
			groups = append(groups, group)
			raw, err := json.Marshal(groups)
			if err != nil {
				return err
			}
			hooks.set(spec.event, raw)
		}
		return nil
	})
}

// RemoveClaudeHooks takes Carya's hooks out of the settings file at path,
// leaving everything else. It reports whether the file changed.
func RemoveClaudeHooks(path string) (bool, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false, nil
	}
	return editClaudeHooks(path, func(hooks *orderedObject) error {
		for _, spec := range claudeHookSpecs {
			raw := hooks.get(spec.event)
			if raw == nil {
				continue
			}
			groups, err := withoutCaryaHooks(raw)
			if err != nil {
				return fmt.Errorf("%s hooks: %w", spec.event, err)
			}
			if len(groups) == 0 {
				hooks.remove(spec.event)
				continue
			}
			enc, err := json.Marshal(groups)
			if err != nil {
				return err
			}
			hooks.set(spec.event, enc)
		}
		return nil
	})
}

func editClaudeHooks(path string, edit func(hooks *orderedObject) error) (bool, error) {
	original, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}

	var top orderedObject
	if len(bytes.TrimSpace(original)) > 0 {
		if err := top.UnmarshalJSON(original); err != nil {
			return false, fmt.Errorf("%s is not a JSON object: %w", path, err)
		}
	}
	var hooks orderedObject
	if raw := top.get("hooks"); raw != nil {
		if err := hooks.UnmarshalJSON(raw); err != nil {
			return false, fmt.Errorf("%s: \"hooks\" is not an object: %w", path, err)
		}
	}
	if err := edit(&hooks); err != nil {
		return false, err
	}
	if len(hooks) == 0 {
		top.remove("hooks")
	} else {
		raw, err := hooks.MarshalJSON()
		if err != nil {
			return false, err
		}
		top.set("hooks", raw)
	}

	compact, err := top.MarshalJSON()
	if err != nil {
		return false, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, compact, "", "  "); err != nil {
		return false, err
	}
	out.WriteByte('\n')
	if bytes.Equal(out.Bytes(), original) {
		return false, nil
	}
	if sameJSON(out.Bytes(), original) {
		return false, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	mode := os.FileMode(0644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp := path + ".carya-tmp"
	if err := os.WriteFile(tmp, out.Bytes(), mode); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return false, err
	}
	return true, nil
}

func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	ja, _ := json.Marshal(x)
	jb, _ := json.Marshal(y)
	return bytes.Equal(ja, jb)
}

// withoutCaryaHooks decodes an event's hook groups and drops Carya's hooks
// from them, removing groups left empty.
func withoutCaryaHooks(raw json.RawMessage) ([]any, error) {
	if raw == nil {
		return nil, nil
	}
	var groups []map[string]any
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, err
	}
	var kept []any
	for _, g := range groups {
		list, _ := g["hooks"].([]any)
		var others []any
		for _, h := range list {
			if hm, ok := h.(map[string]any); ok && isCaryaHook(hm) {
				continue
			}
			others = append(others, h)
		}
		if len(others) == 0 && len(list) > 0 {
			continue
		}
		g["hooks"] = others
		kept = append(kept, g)
	}
	return kept, nil
}

func isCaryaHook(h map[string]any) bool {
	command, _ := h["command"].(string)
	first, rest := firstShellWord(strings.TrimSpace(command))
	return filepath.Base(first) == "carya" && strings.HasPrefix(strings.TrimSpace(rest), "hook claude ")
}

// firstShellWord splits off the first word of a command, unquoting a word
// written by shellQuote (single quotes, with '\” for a quote).
func firstShellWord(command string) (word, rest string) {
	if !strings.HasPrefix(command, "'") {
		word, rest, _ = strings.Cut(command, " ")
		return strings.Trim(word, `"`), rest
	}
	var b strings.Builder
	for i := 1; i < len(command); i++ {
		if command[i] != '\'' {
			b.WriteByte(command[i])
			continue
		}
		if strings.HasPrefix(command[i:], `'\''`) {
			b.WriteByte('\'')
			i += 3
			continue
		}
		return b.String(), command[i+1:]
	}
	return b.String(), ""
}

// shellQuote quotes s for a POSIX shell when it needs it.
func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r == '/' || r == '.' || r == '-' || r == '_' || r == '+' || r == ':' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// orderedObject is a JSON object that keeps its keys in file order, so
// rewriting someone's settings file only changes what Carya owns.
type orderedObject []orderedField

type orderedField struct {
	key   string
	value json.RawMessage
}

func (o orderedObject) get(key string) json.RawMessage {
	for _, f := range o {
		if f.key == key {
			return f.value
		}
	}
	return nil
}

func (o *orderedObject) set(key string, value json.RawMessage) {
	for i, f := range *o {
		if f.key == key {
			(*o)[i].value = value
			return
		}
	}
	*o = append(*o, orderedField{key, value})
}

func (o *orderedObject) remove(key string) {
	for i, f := range *o {
		if f.key == key {
			*o = append((*o)[:i], (*o)[i+1:]...)
			return
		}
	}
}

func (o *orderedObject) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("expected an object")
	}
	*o = nil
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := tok.(string)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return err
		}
		o.set(key, value)
	}
	_, err = dec.Token()
	return err
}

func (o orderedObject) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := json.Marshal(f.key)
		if err != nil {
			return nil, err
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(f.value)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
