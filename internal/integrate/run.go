package integrate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sivepanda/mycelia"
)

const outputTail = 4000

// Check checks res.Commit out into a temporary worktree outside the
// checkout and runs, in order, the repository's Mycelia post-checkout setup
// (when opts.Setup) and the build and test commands, stopping at the first
// failure. Full output goes to logPath. The worktree is removed afterwards
// unless opts.Keep.
func Check(repoRoot string, res *Result, opts Options, logPath string) error {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}

	parent, err := os.MkdirTemp("", "carya-integrate-*")
	if err != nil {
		return err
	}
	tree := filepath.Join(parent, filepath.Base(repoRoot))
	add := exec.Command("git", "worktree", "add", "--detach", "--quiet", tree, res.Commit)
	add.Dir = repoRoot
	if out, err := add.CombinedOutput(); err != nil {
		os.RemoveAll(parent)
		return fmt.Errorf("check out the merged tree: %w\n%s", err, out)
	}
	if opts.Keep {
		res.Worktree = tree
	} else {
		defer func() {
			remove := exec.Command("git", "worktree", "remove", "--force", tree)
			remove.Dir = repoRoot
			_ = remove.Run()
			os.RemoveAll(parent)
			prune := exec.Command("git", "worktree", "prune")
			prune.Dir = repoRoot
			_ = prune.Run()
		}()
	}

	var log io.Writer = io.Discard
	if logPath != "" {
		if f, err := os.Create(logPath); err == nil {
			defer f.Close()
			log = f
			res.LogPath = logPath
		}
	}
	fmt.Fprintf(log, "carya integrate: %s onto %s (%s)\n", res.Commit, res.Onto, res.OntoCommit)
	for _, m := range res.Merged {
		fmt.Fprintf(log, "  merged %s (%s)\n", m.UserID, m.Commit)
	}

	type planned struct {
		kind, command, dir string
		shell              bool
	}
	var plan []planned
	if opts.Setup {
		setup, err := setupCommands(tree)
		if err != nil {
			return err
		}
		for _, c := range setup {
			dir := tree
			if c.WorkingDir != "" && c.WorkingDir != "." {
				dir = filepath.Join(tree, c.WorkingDir)
			}
			plan = append(plan, planned{kind: "setup", command: c.Command, dir: dir})
		}
	}
	run := opts.Run
	if len(run) == 0 {
		run = DetectChecks(tree)
	}
	for _, c := range run {
		plan = append(plan, planned{kind: "run", command: c, dir: tree, shell: true})
	}

	for _, p := range plan {
		step := runStep(p.kind, p.command, p.dir, p.shell, timeout, log)
		res.Steps = append(res.Steps, step)
		if step.ExitCode != 0 {
			break
		}
	}

	res.OK = len(res.Conflicts) == 0 && res.Failed() == nil
	return nil
}

// setupCommands returns the repository's post-checkout setup from its
// Mycelia config in the merged tree, or Mycelia's detected suggestions when
// the repository has no config.
func setupCommands(tree string) ([]mycelia.Command, error) {
	data, err := os.ReadFile(filepath.Join(tree, mycelia.ConfigFile))
	if errors.Is(err, os.ErrNotExist) {
		return mycelia.NewDetector(tree).GetSuggestedCommands("post-checkout")
	}
	if err != nil {
		return nil, err
	}
	var cfg mycelia.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", mycelia.ConfigFile, err)
	}
	return cfg.PostCheckout, nil
}

func runStep(kind, command, dir string, shell bool, timeout time.Duration, log io.Writer) Step {
	step := Step{Kind: kind, Command: command}
	fmt.Fprintf(log, "\n$ %s\n", command)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var cmd *exec.Cmd
	if shell {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	} else {
		// Mycelia runs setup commands split on whitespace, without a shell.
		parts := strings.Fields(command)
		if len(parts) == 0 {
			step.ExitCode = -1
			step.Output = "empty command"
			return step
		}
		cmd = exec.CommandContext(ctx, parts[0], parts[1:]...)
	}
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = io.MultiWriter(&out, log)
	cmd.Stderr = io.MultiWriter(&out, log)

	start := time.Now()
	err := cmd.Run()
	step.Duration = time.Since(start)

	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case ctx.Err() == context.DeadlineExceeded:
		step.ExitCode = -1
		fmt.Fprintf(&out, "\ntimed out after %s", timeout)
	case errors.As(err, &exitErr):
		step.ExitCode = exitErr.ExitCode()
	default:
		step.ExitCode = -1
		fmt.Fprintf(&out, "\n%v", err)
	}
	step.Output = tail(out.String(), outputTail)
	fmt.Fprintf(log, "[exit %d after %s]\n", step.ExitCode, step.Duration.Round(time.Millisecond))
	return step
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + strings.ToValidUTF8(s[len(s)-n:], "")
}

// DetectChecks guesses a repository's build and test commands from the
// files at its root.
func DetectChecks(root string) []string {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(root, name))
		return err == nil
	}
	var checks []string
	if exists("go.mod") {
		checks = append(checks, "go build ./...", "go test ./...")
	}
	if exists("package.json") {
		pm := "npm"
		switch {
		case exists("bun.lock") || exists("bun.lockb"):
			pm = "bun"
		case exists("pnpm-lock.yaml"):
			pm = "pnpm"
		case exists("yarn.lock"):
			pm = "yarn"
		}
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if data, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil {
			_ = json.Unmarshal(data, &pkg)
		}
		if _, ok := pkg.Scripts["build"]; ok {
			checks = append(checks, pm+" run build")
		}
		if _, ok := pkg.Scripts["test"]; ok {
			checks = append(checks, pm+" test")
		}
	}
	if exists("Cargo.toml") {
		checks = append(checks, "cargo build", "cargo test")
	}
	if exists("pyproject.toml") || exists("setup.py") || exists("pytest.ini") || exists("tox.ini") {
		checks = append(checks, "python -m pytest")
	}
	if len(checks) == 0 && exists("Makefile") {
		if data, err := os.ReadFile(filepath.Join(root, "Makefile")); err == nil && bytes.Contains(data, []byte("\ntest:")) {
			checks = append(checks, "make test")
		}
	}
	return checks
}
