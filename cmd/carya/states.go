package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"carya/internal/agent"
	"carya/internal/git"
	"carya/internal/states"

	"github.com/spf13/cobra"
)

var (
	statesJSON      bool
	statesNoFetch   bool
	pruneOlderThan  time.Duration
	pruneNoLanded   bool
	pruneUsers      []string
	pruneRemote     bool
	pruneDryRun     bool
	pruneAssumeOnto string
)

var statesCmd = &cobra.Command{
	Use:   "states",
	Short: "List published working states",
	Long: `List every published working state: who, when it was last published, how
many files it changes against its own base, where it lives (local refs, origin,
or both) and whether its work has already landed on the default branch.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		root, me := statesContext()
		all, err := states.List(root, me, git.NewRefManager(root).DefaultBranch())
		if err != nil {
			fail(err)
		}
		if statesJSON {
			printJSON(all)
			return
		}
		if len(all) == 0 {
			fmt.Println("No published states.")
			return
		}
		now := time.Now()
		fmt.Printf("  %-24s %-10s %-6s %-14s %s\n", "STATE", "PUBLISHED", "FILES", "WHERE", "")
		for _, st := range all {
			mark := " "
			if st.Mine {
				mark = "*"
			}
			fmt.Printf("%s %-24s %-10s %-6d %-14s %s\n", mark, st.UserID, ago(st, now), st.Files, where(st), landedNote(st))
		}
		fmt.Println("\n* = you. Remove stale ones with 'carya states prune'.")
	},
}

var statesPruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Delete stale published states",
	Long: `Delete published states that are stale: those whose work has already landed
on the default branch, and those not republished for --older-than (7 days by
default). --user prunes exactly the named states instead. Your own state is
kept unless you name it.

By default only your copies are deleted: local refs and the copies fetched
from origin. A state still on origin comes back on the next fetch, so
--remote also deletes it there, for everyone. Use --dry-run to see first.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		root, me := statesContext()
		onto := pruneAssumeOnto
		if onto == "" {
			onto = git.NewRefManager(root).DefaultBranch()
		}
		all, err := states.List(root, me, onto)
		if err != nil {
			fail(err)
		}
		picked := states.Select(all, states.PruneOptions{
			OlderThan: pruneOlderThan,
			Landed:    !pruneNoLanded,
			Users:     pruneUsers,
		})
		if len(picked) == 0 {
			fmt.Println("Nothing to prune.")
			return
		}

		now := time.Now()
		verb := "Pruning"
		if pruneDryRun {
			verb = "Would prune"
		}
		fmt.Printf("%s %d state(s):\n", verb, len(picked))
		for _, c := range picked {
			fmt.Printf("  %-24s %-10s %-14s %s\n", c.UserID, ago(c.State, now), where(c.State), c.Reason)
		}
		if pruneDryRun {
			return
		}

		left, err := states.Delete(root, picked, pruneRemote)
		if err != nil {
			fail(err)
		}
		if pruneRemote {
			fmt.Println("Deleted them locally and on origin.")
		} else if left > 0 {
			fmt.Printf("Deleted them locally. %d are still on origin and will come back on the next fetch; run with --remote to delete them there for everyone.\n", left)
		} else {
			fmt.Println("Deleted them.")
		}
	},
}

// statesContext returns the checkout root and the caller's identity (empty
// when Carya isn't set up here), after fetching others' states if possible.
func statesContext() (string, string) {
	wd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	if s, err := agent.Open(wd); err == nil {
		if !statesNoFetch {
			if ok, _ := s.Sharing(); ok {
				if err := s.FetchStates(); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not fetch states: %v\n", err)
				}
			}
		}
		return s.Repo.RootPath(), s.UserID
	}
	repo := mustRepo()
	if !statesNoFetch && git.NewRefManager(repo.RootPath()).HasRemote("origin") {
		_ = git.NewRefManager(repo.RootPath()).FetchCaryaRefs("origin")
	}
	return repo.RootPath(), ""
}

func ago(st states.State, now time.Time) string {
	if st.Published.IsZero() {
		return "unknown"
	}
	return states.FormatAge(st.Age(now)) + " ago"
}

func where(st states.State) string {
	var w []string
	if st.Local {
		w = append(w, "local")
	}
	if st.OnRemote {
		w = append(w, "origin")
	}
	return strings.Join(w, "+")
}

func landedNote(st states.State) string {
	switch {
	case st.Landed:
		return "landed"
	case st.Base == "":
		return "no base (older carya)"
	}
	return ""
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fail(err)
	}
}

func init() {
	statesCmd.PersistentFlags().BoolVar(&statesNoFetch, "no-fetch", false, "don't fetch states from origin first")
	statesCmd.Flags().BoolVar(&statesJSON, "json", false, "print the states as JSON")
	p := statesPruneCmd.Flags()
	p.DurationVar(&pruneOlderThan, "older-than", 7*24*time.Hour, "prune states not republished for this long (0 to disable)")
	p.BoolVar(&pruneNoLanded, "no-landed", false, "keep states whose work has landed")
	p.StringSliceVar(&pruneUsers, "user", nil, "prune exactly these states instead")
	p.BoolVar(&pruneRemote, "remote", false, "also delete the states on origin, for everyone")
	p.BoolVar(&pruneDryRun, "dry-run", false, "show what would be pruned without deleting anything")
	p.StringVar(&pruneAssumeOnto, "onto", "", "branch to check for landed work (default: the default branch)")
	statesCmd.AddCommand(statesPruneCmd)
	rootCmd.AddCommand(statesCmd)
}
