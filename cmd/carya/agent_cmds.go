package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"carya/internal/agent"
	"carya/internal/harness"
	"carya/internal/peers"
	"carya/internal/repository"

	"github.com/spf13/cobra"
)

// ---- carya setup <harness> -------------------------------------------------

var (
	setupScope  string
	setupRemove bool
	setupBase   string
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Set Carya up for a coding agent harness",
	Long: `Set Carya up for a coding agent harness in one step, with no prompts.

Each agent checkout gets its own identity. Agents publish what they have on
disk after every tool call (including shell commands), and when an agent
edits a file another agent is changing, Carya shows it that change.

Every harness takes --scope:
  local    this repository, just you, nothing tracked changes (default)
  project  this repository, a file you commit for everyone
  user     your user config, active in every repository you run setup in

Run setup again at any time; it replaces only what it wrote. --remove takes
it out again.`,
}

// harnessSetup describes how `carya setup <name>` installs one harness.
type harnessSetup struct {
	name    string // as in `carya setup <name>` and `carya hook <name>`
	display string
	what    string // what gets installed, for messages
	long    string
	path    func(scope, repoRoot string) (string, error)
	install func(path, carya string, requireOptIn bool) (bool, error)
	remove  func(path string) (bool, error)
	// excludes lists paths to keep out of git through info/exclude.
	excludes func(scope string) []string
	// mainCheckout means the harness reads repository-level config only from
	// the main worktree, so setup run in a linked worktree installs there.
	mainCheckout bool
	// next is printed after a successful install.
	next string
}

var harnessSetups = []harnessSetup{
	{
		name:    "claude",
		display: "Claude Code",
		what:    "hooks",
		long: `Writes Carya's SessionStart, PreToolUse and PostToolUse hooks into
.claude/settings.local.json (local), .claude/settings.json (project) or
~/.claude/settings.json (user). Before Claude edits a file another agent is
changing, the change is added to its context.`,
		path:    harness.ClaudeSettingsPath,
		install: harness.InstallClaudeHooks,
		remove:  harness.RemoveClaudeHooks,
		excludes: func(scope string) []string {
			// ` + "`claude --worktree`" + ` puts worktrees inside the checkout.
			ex := []string{".claude/worktrees/"}
			if scope == harness.ScopeLocal {
				ex = append(ex, ".claude/settings.local.json")
			}
			return ex
		},
		next: "New and running Claude Code sessions pick the hooks up automatically.",
	},
	{
		name:    "codex",
		display: "Codex",
		what:    "hooks",
		long: `Writes Carya's SessionStart, PreToolUse and PostToolUse hooks into
.codex/hooks.json (local and project) or ~/.codex/hooks.json (user). Codex
reads a worktree's hooks from the main checkout, so worktrees are covered.
Before Codex applies a patch to a file another agent is changing, the change
is added to its context.`,
		path:         harness.CodexHooksPath,
		install:      harness.InstallCodexHooks,
		remove:       harness.RemoveCodexHooks,
		mainCheckout: true,
		excludes: func(scope string) []string {
			if scope == harness.ScopeLocal {
				return []string{".codex/hooks.json"}
			}
			return nil
		},
		next: `Codex runs new hooks only after you approve them, once: start codex here,
trust the project if it asks, then open /hooks and approve Carya's three
hooks. Updating carya itself never needs approving again; only re-running
setup in a way that changes the hooks does.`,
	},
	{
		name:    "opencode",
		display: "OpenCode",
		what:    "plugin",
		long: `Writes a small plugin to .opencode/plugins/carya.ts (local and project) or
~/.config/opencode/plugins/carya.ts (user). It works with OpenCode 1.x and
2.x. After every tool call it publishes the checkout's state, and after an
edit it appends other agents' changes to the edited files to the tool output.
For worktrees outside this checkout, use --scope project or user.`,
		path:    harness.OpenCodePluginPath,
		install: harness.InstallOpenCodePlugin,
		remove:  harness.RemoveOpenCodePlugin,
		excludes: func(scope string) []string {
			if scope == harness.ScopeLocal {
				return []string{".opencode/plugins/carya.ts"}
			}
			return nil
		},
		next: "OpenCode loads plugins at startup; restart any running OpenCode session.",
	},
}

func newSetupCmd(h harnessSetup) *cobra.Command {
	return &cobra.Command{
		Use:   h.name,
		Short: "Set Carya up for " + h.display,
		Long:  "Set Carya up for " + h.display + " in this repository.\n\n" + h.long,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			wd, err := os.Getwd()
			if err != nil {
				fail(err)
			}
			repo := mustRepo()
			path, notes, err := setupTarget(h, setupScope, repo)
			if err != nil {
				fail(err)
			}

			if setupRemove {
				changed, err := h.remove(path)
				if err != nil {
					fail(err)
				}
				if changed {
					fmt.Printf("Removed Carya's %s %s from %s\n", h.display, h.what, path)
				} else {
					fmt.Printf("No Carya %s in %s\n", h.what, path)
				}
				return
			}

			s, err := agent.Bootstrap(wd, h.name)
			if err != nil {
				fail(err)
			}
			if err := harness.OptIn(wd); err != nil {
				fail(err)
			}
			base, err := harness.EnsureBase(wd, setupBase)
			if err != nil {
				fail(err)
			}
			for _, pattern := range h.excludes(setupScope) {
				if err := s.Repo.EnsureLocallyExcluded(pattern); err != nil {
					fail(err)
				}
			}

			carya, warning := hookCommand(setupScope)
			changed, err := h.install(path, carya, setupScope == harness.ScopeUser)
			if err != nil {
				fail(err)
			}

			if changed {
				fmt.Printf("Installed Carya's %s %s in %s\n", h.display, h.what, path)
			} else {
				fmt.Printf("Carya's %s %s are already up to date in %s\n", h.display, h.what, path)
			}
			fmt.Printf("This checkout's agent identity: %s\n", s.UserID)
			if base != "" {
				fmt.Printf("Agents' work counts from where it forks off %s (change with --base).\n", base)
			}
			if ok, why := s.Sharing(); ok {
				fmt.Println("States are shared through origin (refs/carya/*, which GitHub's branch list does not show).")
			} else {
				fmt.Printf("States stay on this machine: %s.\n", why)
			}
			for _, note := range notes {
				fmt.Println(note)
			}
			if warning != "" {
				fmt.Fprintln(os.Stderr, "Warning: "+warning)
			}
			fmt.Println(h.next)
		},
	}
}

// setupTarget returns the file setup writes for h in scope, and notes to
// print about why it is there. A harness that reads repository config only
// from the main worktree (Codex) gets its file in the main checkout even when
// setup runs in a linked worktree; the file then covers every worktree.
func setupTarget(h harnessSetup, scope string, repo *repository.Repository) (string, []string, error) {
	root := repo.RootPath()
	var notes []string
	if h.mainCheckout && scope != harness.ScopeUser {
		if main := repo.MainWorktreeRoot(); main != root {
			root = main
			path, err := h.path(scope, root)
			if err != nil {
				return "", nil, err
			}
			notes = append(notes, fmt.Sprintf("This is a linked worktree. %s reads repository %s only from the main checkout, so they go in %s and cover every worktree.", h.display, h.what, path))
			if scope == harness.ScopeProject {
				branch := repository.BranchAt(main)
				if branch == "" {
					branch = "whatever is checked out there"
				}
				notes = append(notes, fmt.Sprintf("Commit it on the branch checked out in the main checkout (%s): %s reads it there, not from this worktree's branch.", branch, h.display))
			}
			return path, notes, nil
		}
	}
	path, err := h.path(scope, root)
	return path, notes, err
}

// hookCommand picks how hooks should invoke Carya. A committed settings file
// must not hold a path from this machine, so project scope always uses the
// name on PATH; the others prefer the running binary's absolute path when
// `carya` on PATH is a different binary or missing.
func hookCommand(scope string) (string, string) {
	self, err := os.Executable()
	if err == nil {
		self, _ = filepath.EvalSymlinks(self)
	}
	onPath, lookErr := exec.LookPath("carya")
	if lookErr == nil {
		onPath, _ = filepath.EvalSymlinks(onPath)
	}

	if scope == harness.ScopeProject {
		if lookErr != nil {
			return "carya", "carya is not on your PATH, so the committed hooks will fail until it is"
		}
		return "carya", ""
	}
	if lookErr == nil && (self == "" || onPath == self) {
		return "carya", ""
	}
	if self != "" {
		return self, ""
	}
	return "carya", "could not locate the carya binary; the hooks assume it is on PATH"
}

// ---- carya check <path>... --------------------------------------------------

var checkJSON bool

var checkCmd = &cobra.Command{
	Use:   "check <path>...",
	Short: "Show other agents' unfinished changes to files",
	Long: `Show other agents' and teammates' unfinished changes to the given files,
and whether they would conflict with your current changes. Each peer is
compared against the commit their own state is built on.`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		s := mustAgentSession()
		changes, err := s.Check(args)
		if err != nil {
			fail(err)
		}
		if checkJSON {
			if changes == nil {
				changes = []peers.Change{}
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(changes); err != nil {
				fail(err)
			}
			return
		}
		if summary := peers.Summary(changes, time.Now()); summary != "" {
			fmt.Print(summary)
		} else {
			fmt.Println("Nobody else has unfinished changes to these files.")
		}
	},
}

// ---- carya snapshot ---------------------------------------------------------

var snapshotPush bool

var snapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "Publish the working tree as your state, without the daemon",
	Long: `Publish the working tree as it is on disk, including edits made by shell
commands, as your working state. Harness hooks run this after every tool call;
run it yourself after editing outside an agent. --push also pushes it to
origin and fetches everyone else's.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		s := mustAgentSession()
		state, err := s.Snapshot()
		if err != nil {
			fail(err)
		}
		fmt.Printf("Published %s's state (tree %s on %s)\n", s.UserID, shortHash(state.TreeHash), shortHash(state.BaseCommit))
		if snapshotPush {
			if err := s.Sync(); err != nil {
				fail(err)
			}
			fmt.Println("Pushed to origin and fetched other states")
		}
	},
}

// ---- hidden: carya hook <harness> <event>, carya sync-states ---------------

var hookRequireOptIn bool

var hookCmd = &cobra.Command{
	Use:    "hook <harness> <event>",
	Short:  "Run as a harness hook (written by carya setup)",
	Hidden: true,
	Args:   cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		run := map[string]func(string, io.Reader, io.Writer, harness.HookOptions) error{
			"claude":   harness.RunClaudeHook,
			"codex":    harness.RunCodexHook,
			"opencode": harness.RunOpenCodeHook,
		}[args[0]]
		if run == nil {
			fmt.Fprintf(os.Stderr, "carya hook: unsupported harness %q\n", args[0])
			return
		}
		opts := harness.HookOptions{RequireOptIn: hookRequireOptIn, StartSync: startStateSync}
		if err := run(args[1], os.Stdin, os.Stdout, opts); err != nil {
			// Never fail the agent's tool call because of Carya: exit 0 and
			// leave the reason in the log.
			log.Printf("carya hook %s %s: %v", args[0], args[1], err)
		}
	},
}

var syncStatesCmd = &cobra.Command{
	Use:    "sync-states",
	Short:  "Push your state and fetch others' (run in the background by hooks)",
	Hidden: true,
	Args:   cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		s := mustAgentSession()
		if err := s.Sync(); err != nil {
			log.Printf("sync-states: %v", err)
			os.Exit(1)
		}
	},
}

// startStateSync runs `carya sync-states` for root in the background, detached
// so the hook can return while the network round trip happens.
func startStateSync(root string) {
	self, err := os.Executable()
	if err != nil {
		log.Printf("start state sync: %v", err)
		return
	}
	c := exec.Command(self, "sync-states")
	c.Dir = root
	detach(c)
	if err := c.Start(); err != nil {
		log.Printf("start state sync: %v", err)
		return
	}
	_ = c.Process.Release()
}

func mustAgentSession() *agent.Session {
	wd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	s, err := agent.Open(wd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\nRun 'carya setup claude' (or codex, opencode, or 'carya init') first.\n", err)
		os.Exit(1)
	}
	return s
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}

func init() {
	setupCmd.PersistentFlags().StringVar(&setupScope, "scope", harness.ScopeLocal, "where to install: local, project or user")
	setupCmd.PersistentFlags().BoolVar(&setupRemove, "remove", false, "remove what setup installed instead of installing it")
	setupCmd.PersistentFlags().StringVar(&setupBase, "base", "", "branch agents' work is merged into (default: the current branch)")
	for _, h := range harnessSetups {
		setupCmd.AddCommand(newSetupCmd(h))
	}
	checkCmd.Flags().BoolVar(&checkJSON, "json", false, "print the changes as JSON")
	snapshotCmd.Flags().BoolVar(&snapshotPush, "push", false, "also push your state to origin and fetch others'")
	hookCmd.Flags().BoolVar(&hookRequireOptIn, "require-opt-in", false, "do nothing unless the repository ran carya setup")
	rootCmd.AddCommand(setupCmd, checkCmd, snapshotCmd, hookCmd, syncStatesCmd)
}
