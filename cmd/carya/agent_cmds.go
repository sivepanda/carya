package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"carya/internal/agent"
	"carya/internal/harness"
	"carya/internal/peers"

	"github.com/spf13/cobra"
)

// ---- carya setup <harness> -------------------------------------------------

var (
	setupScope  string
	setupRemove bool
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Set Carya up for a coding agent harness",
	Long: `Set Carya up for a coding agent harness in one step, with no prompts.

Each agent checkout gets its own identity. Agents publish what they have on
disk after every tool call (including shell commands), and before an agent
edits a file another agent is changing, Carya adds that change to its context.`,
}

var setupClaudeCmd = &cobra.Command{
	Use:   "claude",
	Short: "Set Carya up for Claude Code",
	Long: `Set Carya up for Claude Code in this repository.

Writes Carya's SessionStart, PreToolUse and PostToolUse hooks into a Claude
Code settings file and sets up this checkout. Other worktrees set themselves
up the first time a hook runs in them. Nothing tracked changes unless you
choose --scope project.

Scopes:
  local    .claude/settings.local.json, just you, this repository (default)
  project  .claude/settings.json, committed, for everyone on the repository
  user     ~/.claude/settings.json, every repository you run setup in

Run it again at any time; it replaces its own hooks and leaves the rest of
the file alone. --remove takes the hooks out again.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		wd, err := os.Getwd()
		if err != nil {
			fail(err)
		}
		repo := mustRepo()
		path, err := harness.ClaudeSettingsPath(setupScope, repo.RootPath())
		if err != nil {
			fail(err)
		}

		if setupRemove {
			changed, err := harness.RemoveClaudeHooks(path)
			if err != nil {
				fail(err)
			}
			if changed {
				fmt.Printf("Removed Carya's hooks from %s\n", path)
			} else {
				fmt.Printf("No Carya hooks in %s\n", path)
			}
			return
		}

		s, err := agent.Bootstrap(wd, "claude")
		if err != nil {
			fail(err)
		}
		if err := harness.OptIn(wd); err != nil {
			fail(err)
		}
		// `claude --worktree` puts worktrees inside the checkout; keep them
		// (and personal settings) out of git without editing .gitignore.
		excludes := []string{".claude/worktrees/"}
		if setupScope == harness.ScopeLocal {
			excludes = append(excludes, ".claude/settings.local.json")
		}
		for _, pattern := range excludes {
			if err := s.Repo.EnsureLocallyExcluded(pattern); err != nil {
				fail(err)
			}
		}

		carya, warning := hookCommand(setupScope)
		changed, err := harness.InstallClaudeHooks(path, carya, setupScope == harness.ScopeUser)
		if err != nil {
			fail(err)
		}

		if changed {
			fmt.Printf("Installed Carya's Claude Code hooks in %s\n", path)
		} else {
			fmt.Printf("Carya's Claude Code hooks are already up to date in %s\n", path)
		}
		fmt.Printf("This checkout's agent identity: %s\n", s.UserID)
		if ok, why := s.Sharing(); ok {
			fmt.Println("States are shared through origin (refs/carya/*, which GitHub's branch list does not show).")
		} else {
			fmt.Printf("States stay on this machine: %s.\n", why)
		}
		if warning != "" {
			fmt.Fprintln(os.Stderr, "Warning: "+warning)
		}
		fmt.Println("New and running Claude Code sessions pick the hooks up automatically.")
	},
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
		if args[0] != "claude" {
			fmt.Fprintf(os.Stderr, "carya hook: unsupported harness %q\n", args[0])
			return
		}
		opts := harness.HookOptions{RequireOptIn: hookRequireOptIn, StartSync: startStateSync}
		if err := harness.RunClaudeHook(args[1], os.Stdin, os.Stdout, opts); err != nil {
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
		fmt.Fprintf(os.Stderr, "Error: %v\nRun 'carya setup claude' (or 'carya init') first.\n", err)
		os.Exit(1)
	}
	return s
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}

func init() {
	setupClaudeCmd.Flags().StringVar(&setupScope, "scope", harness.ScopeLocal, "where to write the hooks: local, project or user")
	setupClaudeCmd.Flags().BoolVar(&setupRemove, "remove", false, "remove Carya's hooks instead of installing them")
	setupCmd.AddCommand(setupClaudeCmd)
	checkCmd.Flags().BoolVar(&checkJSON, "json", false, "print the changes as JSON")
	snapshotCmd.Flags().BoolVar(&snapshotPush, "push", false, "also push your state to origin and fetch others'")
	hookCmd.Flags().BoolVar(&hookRequireOptIn, "require-opt-in", false, "do nothing unless the repository ran carya setup")
	rootCmd.AddCommand(setupCmd, checkCmd, snapshotCmd, hookCmd, syncStatesCmd)
}
