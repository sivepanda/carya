package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"carya/internal/agent"
	"carya/internal/integrate"

	"github.com/spf13/cobra"
)

var (
	integrateOnto      string
	integrateOnly      []string
	integrateExclude   []string
	integrateRun       []string
	integrateNoSetup   bool
	integrateNoFetch   bool
	integrateMergeOnly bool
	integrateKeep      bool
	integrateJSON      bool
	integrateTimeout   time.Duration
	integrateSince     time.Duration
)

var integrateCmd = &cobra.Command{
	Use:   "integrate",
	Short: "Merge everyone's unfinished work and build and test the result",
	Long: `Merge every published working state (every agent's and teammate's, plus
yours) onto the default branch, then build and test the result the way CI
would, before anyone opens a pull request.

States are merged in memory, oldest first; one that conflicts is left out and
reported with whose work it collides with, and the rest still merge. The
result is kept at refs/carya/integration. It is then checked out in a
temporary worktree outside this checkout, where the repository's Mycelia
post-checkout setup runs, followed by the build and test commands (--run, or
detected from go.mod, package.json, Cargo.toml, pyproject.toml or a Makefile).
Nothing in your checkout changes.

This runs other people's unfinished code on your machine.

Exit status is 0 when everything merged and every step passed.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		wd, err := os.Getwd()
		if err != nil {
			fail(err)
		}
		s, err := agent.Open(wd)
		if err != nil {
			if s, err = agent.Bootstrap(wd, ""); err != nil {
				fail(err)
			}
		}

		// Your own unfinished work is part of the integration too.
		if _, err := s.Snapshot(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not publish your current state: %v\n", err)
		}
		if !integrateNoFetch {
			if ok, _ := s.Sharing(); ok {
				if err := s.FetchStates(); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not fetch other states: %v\n", err)
				}
			}
		}

		opts := integrate.Options{
			Onto:    integrateOnto,
			Only:    integrateOnly,
			Exclude: integrateExclude,
			Since:   integrateSince,
			Run:     integrateRun,
			Setup:   !integrateNoSetup,
			Keep:    integrateKeep,
			Timeout: integrateTimeout,
		}
		root := s.Repo.RootPath()
		res, err := integrate.Merge(root, opts)
		if err != nil {
			fail(err)
		}
		if integrateMergeOnly {
			res.OK = len(res.Conflicts) == 0
		} else if err := integrate.Check(root, res, opts, filepath.Join(s.Repo.CaryaPath(), "integration.log")); err != nil {
			fail(err)
		}

		if integrateJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(res); err != nil {
				fail(err)
			}
		} else {
			printIntegration(res, root, integrateMergeOnly)
		}
		if !res.OK {
			os.Exit(1)
		}
	},
}

func printIntegration(res *integrate.Result, root string, mergeOnly bool) {
	now := time.Now()
	fmt.Printf("Integrating onto %s (%s)\n", res.Onto, shortHash(res.OntoCommit))
	if len(res.Merged)+len(res.Conflicts)+len(res.Skipped) == 0 {
		fmt.Println("  nobody has published unfinished work")
	}
	for _, m := range res.Merged {
		fmt.Printf("  merged    %s (published %s ago)\n", m.UserID, now.Sub(m.Published).Round(time.Second))
	}
	for _, c := range res.Conflicts {
		fmt.Printf("  CONFLICT  %s in %s, with %s; left out\n", c.UserID, strings.Join(c.Files, ", "), strings.Join(c.With, ", "))
	}
	for _, sk := range res.Skipped {
		fmt.Printf("  skipped   %s: %s\n", sk.UserID, sk.Reason)
	}
	fmt.Printf("Merged result: %s (%s)\n", integrate.IntegrationRef, shortHash(res.Commit))

	if mergeOnly {
		if res.OK {
			fmt.Println("\nEverything merges cleanly.")
		} else {
			fmt.Println("\nSome work conflicts; see above.")
		}
		return
	}

	if len(res.Steps) == 0 {
		fmt.Println("\nNo setup, build or test commands found; pass them with --run.")
	} else {
		fmt.Println()
	}
	for _, st := range res.Steps {
		status := "ok"
		if st.ExitCode != 0 {
			status = fmt.Sprintf("FAILED (exit %d)", st.ExitCode)
		}
		fmt.Printf("  %-5s  %-40s %s  %s\n", st.Kind, st.Command, status, st.Duration.Round(100*time.Millisecond))
		if st.ExitCode != 0 && strings.TrimSpace(st.Output) != "" {
			for _, line := range strings.Split(strings.TrimRight(st.Output, "\n"), "\n") {
				fmt.Printf("         %s\n", line)
			}
		}
	}
	if res.Worktree != "" {
		fmt.Printf("\nKept the merged tree at %s (remove it with: git worktree remove --force %s)\n", res.Worktree, res.Worktree)
	}

	log := res.LogPath
	if rel, err := filepath.Rel(root, log); err == nil && log != "" {
		log = rel
	}
	switch {
	case res.OK:
		fmt.Printf("\nIntegration passed: %d state(s) merge cleanly and every step passed.\n", len(res.Merged))
	case res.Failed() != nil:
		fmt.Printf("\nIntegration FAILED at `%s`. Full log: %s\n", res.Failed().Command, log)
	default:
		fmt.Printf("\nThe merged work passes, but %d state(s) were left out because they conflict.\n", len(res.Conflicts))
	}
}

func init() {
	f := integrateCmd.Flags()
	f.StringVar(&integrateOnto, "onto", "", "ref to integrate onto (default: the default branch)")
	f.StringSliceVar(&integrateOnly, "only", nil, "include only these users' states")
	f.StringSliceVar(&integrateExclude, "exclude", nil, "leave these users' states out")
	f.DurationVar(&integrateSince, "since", 0, "leave out states published longer ago than this, e.g. 24h (default: keep all)")
	f.StringArrayVar(&integrateRun, "run", nil, "build or test command to run, repeatable (default: detected)")
	f.BoolVar(&integrateNoSetup, "no-setup", false, "skip the repository's Mycelia post-checkout setup")
	f.BoolVar(&integrateNoFetch, "no-fetch", false, "use only the states already fetched")
	f.BoolVar(&integrateMergeOnly, "merge-only", false, "only merge; don't check out, build or test")
	f.BoolVar(&integrateKeep, "keep", false, "keep the temporary worktree with the merged tree")
	f.BoolVar(&integrateJSON, "json", false, "print the result as JSON")
	f.DurationVar(&integrateTimeout, "timeout", 15*time.Minute, "time limit for each setup, build or test step")
	rootCmd.AddCommand(integrateCmd)
}
