package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"carya/internal/agent"
	"carya/internal/git"
	"carya/internal/history"
	"carya/internal/peers"

	"github.com/spf13/cobra"
)

var (
	historyLimit int
	historyJSON  bool
)

var relatedCmd = &cobra.Command{
	Use:   "related <file>",
	Short: "Show files that usually change together with a file",
	Long: `Show the files most often changed in the same commit as <file>, from the
repository's history. Use it before editing to find the tests, callers and
docs that usually need to change too. Bulk commits (over 40 files) are not
counted, and files deleted since are left out.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		_, path, ix := openHistory(args[0])
		defer ix.Close()
		related, total, err := ix.RelatedFiles(path, historyLimit)
		if err != nil {
			fail(err)
		}
		if historyJSON {
			if related == nil {
				related = []history.Related{}
			}
			printJSON(map[string]any{"path": path, "commits": total, "related": related})
			return
		}
		switch {
		case total == 0:
			fmt.Printf("%s has no indexed history.\n", path)
		case len(related) == 0:
			fmt.Printf("%s changed in %d commit(s), never together with files that still exist.\n", path, total)
		default:
			fmt.Printf("Files that change with %s (%d commit(s) touch it):\n", path, total)
			width := 0
			for _, r := range related {
				width = max(width, len(r.Path))
			}
			for _, r := range related {
				fmt.Printf("  %-*s  %d of %d (%d%%)\n", width, r.Path, r.Together, total, int(r.Share*100+0.5))
			}
		}
	},
}

var whyCmd = &cobra.Command{
	Use:   "why <file>[:<line>]",
	Short: "Show why a file (or line) changed, and who is changing it now",
	Long: `Show the most recent commits that changed <file>, with their reasons (the
commit subject, plus a Carya-Reason trailer when a commit has one), and any
unfinished changes other agents have published for it. With <file>:<line>,
show the commit that last changed that line.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		arg, line := splitLine(args[0])
		root, path, ix := openHistory(arg)
		defer ix.Close()

		var lineChange *history.Change
		if line > 0 {
			c, err := ix.LineChange(path, line)
			if err != nil {
				fail(err)
			}
			lineChange = c
		}
		changes, total, err := ix.Why(path, historyLimit)
		if err != nil {
			fail(err)
		}
		inProgress := peerChanges(root, path)

		if historyJSON {
			if changes == nil {
				changes = []history.Change{}
			}
			if inProgress == nil {
				inProgress = []peers.Change{}
			}
			out := map[string]any{"path": path, "commits": total, "recent": changes, "in_progress": inProgress}
			if lineChange != nil {
				out["line"] = line
				out["line_change"] = lineChange
			}
			printJSON(out)
			return
		}

		if lineChange != nil {
			fmt.Printf("%s:%d was last changed by %s:\n", path, line, shortHash(lineChange.Commit))
			printChange(*lineChange)
			fmt.Println()
		}
		if total == 0 {
			fmt.Printf("%s has no indexed history.\n", path)
		} else {
			fmt.Printf("Recent changes to %s (%d of %d):\n", path, len(changes), total)
			for _, c := range changes {
				printChange(c)
			}
		}
		if len(inProgress) > 0 {
			fmt.Println("\nIn progress:")
			now := time.Now()
			for _, p := range inProgress {
				note := ""
				if p.Conflict {
					note = "; would conflict with your changes"
				}
				fmt.Printf("  %s has unfinished changes here (published %s ago%s). Run 'carya check %s' to see them.\n",
					p.UserID, now.Sub(p.Published).Round(time.Second), note, path)
			}
		}
	},
}

func printChange(c history.Change) {
	short := c.Commit
	if len(short) > 8 {
		short = short[:8]
	}
	fmt.Printf("  %s  %s  %s (%s)\n", c.Time.Format("2006-01-02"), short, c.Subject, c.Author)
	if c.Reason != "" {
		fmt.Printf("      reason: %s\n", c.Reason)
	}
}

// openHistory finds the checkout, turns arg into a checkout-relative path
// and opens an up-to-date history index.
func openHistory(arg string) (root, path string, ix *history.Index) {
	wd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	s, err := agent.Open(wd)
	if err != nil {
		repo := mustRepo()
		s = &agent.Session{Repo: repo}
	}
	rel := s.RelPaths([]string{absFromCwd(wd, arg)})
	if len(rel) == 0 {
		fail(fmt.Errorf("%s is outside this repository", arg))
	}
	if err := s.Repo.EnsureExists(); err != nil {
		fail(err)
	}
	if err := s.Repo.EnsureLocallyIgnored(); err != nil {
		fail(err)
	}
	ix, err = history.Open(s.Repo.DBPath(), s.Repo.RootPath())
	if err != nil {
		fail(err)
	}
	if n, err := ix.Update(); err != nil {
		fail(err)
	} else if n > 500 {
		fmt.Fprintf(os.Stderr, "Indexed %d commits.\n", n)
	}
	return s.Repo.RootPath(), rel[0], ix
}

func absFromCwd(wd, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(wd, p)
}

// splitLine splits "file:42" into the file and the line; anything else is a
// plain path.
func splitLine(arg string) (string, int) {
	i := strings.LastIndex(arg, ":")
	if i <= 0 {
		return arg, 0
	}
	n, err := strconv.Atoi(arg[i+1:])
	if err != nil || n <= 0 {
		return arg, 0
	}
	return arg[:i], n
}

// peerChanges lists other agents' unfinished changes to path, measured
// against your current state when Carya is set up here.
func peerChanges(root, path string) []peers.Change {
	if s, err := agent.Open(root); err == nil {
		changes, _ := s.Check([]string{path})
		return changes
	}
	changes, _ := peers.NewChecker(root).ForPaths("", git.UserRef{}, []string{path})
	return changes
}

func init() {
	for _, c := range []*cobra.Command{relatedCmd, whyCmd} {
		c.Flags().IntVarP(&historyLimit, "limit", "n", 10, "how many results to show")
		c.Flags().BoolVar(&historyJSON, "json", false, "print the result as JSON")
		rootCmd.AddCommand(c)
	}
}
