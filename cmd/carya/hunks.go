package main

import (
	"fmt"
	"strings"

	"carya/internal/hunks"

	"github.com/spf13/cobra"
)

var (
	hunksDiff    bool
	hunksJSON    bool
	commitMsg    string
	commitDryRun bool
)

var hunksCmd = &cobra.Command{
	Use:   "hunks [file...]",
	Short: "List uncommitted changes as hunks you can commit separately",
	Long: `List the working tree's changes against HEAD, untracked files included, as
hunks with short IDs. Commit any grouping of them with
'carya commit -m <message> <id or file>...'; the rest stays uncommitted.
IDs come from each hunk's content, so they stay the same while you commit
other hunks.`,
	Run: func(cmd *cobra.Command, args []string) {
		root := mustRepo().RootPath()
		all, err := hunks.List(root)
		if err != nil {
			fail(err)
		}
		shown := all
		if len(args) > 0 {
			if shown, err = hunks.Select(all, args); err != nil {
				fail(err)
			}
		}
		if hunksJSON {
			if shown == nil {
				shown = []hunks.Hunk{}
			}
			printJSON(shown)
			return
		}
		if len(shown) == 0 {
			fmt.Println("Nothing to commit.")
			return
		}
		printHunks(shown, hunksDiff)
		fmt.Println(`Commit some with: carya commit -m "<message>" <id or file>...`)
	},
}

func printHunks(hs []hunks.Hunk, withDiff bool) {
	files := 0
	for i, h := range hs {
		if i == 0 || hs[i-1].Path != h.Path {
			files++
		}
	}
	fmt.Printf("%d hunk(s) in %d file(s):\n", len(hs), files)
	for i, h := range hs {
		if i == 0 || hs[i-1].Path != h.Path {
			note := map[string]string{"add": " (new file)", "delete": " (deleted)", "binary": " (binary)", "mode": " (mode change)"}[h.Kind]
			fmt.Printf("  %s%s\n", h.Path, note)
		}
		label := h.Header
		if label == "" {
			label = h.Kind
		}
		if len(label) > 70 {
			label = label[:70] + "…"
		}
		fmt.Printf("    %s  %-72s +%d -%d\n", h.ID, label, h.Added, h.Removed)
		if withDiff && h.Kind != "binary" {
			for _, line := range strings.Split(strings.TrimRight(h.Diff, "\n"), "\n") {
				fmt.Printf("        %s\n", line)
			}
		}
	}
}

var commitCmd = &cobra.Command{
	Use:   "commit -m <message> <hunk id or file>...",
	Short: "Commit exactly the chosen hunks",
	Long: `Commit the hunks (IDs from 'carya hunks') or whole files given, and nothing
else: other changes, staged or not, stay as they are. It runs git commit, so
your identity and the repository's commit hooks apply.`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		root := mustRepo().RootPath()
		if commitDryRun {
			all, err := hunks.List(root)
			if err != nil {
				fail(err)
			}
			picked, err := hunks.Select(all, args)
			if err != nil {
				fail(err)
			}
			fmt.Println("Would commit:")
			printHunks(picked, false)
			return
		}
		out, picked, err := hunks.Commit(root, args, commitMsg)
		if err != nil {
			fail(err)
		}
		fmt.Print(out)
		rest, err := hunks.List(root)
		if err == nil {
			fmt.Printf("Committed %d hunk(s); %d left uncommitted.\n", len(picked), len(rest))
		}
	},
}

func init() {
	hunksCmd.Flags().BoolVar(&hunksDiff, "diff", false, "show each hunk's diff")
	hunksCmd.Flags().BoolVar(&hunksJSON, "json", false, "print the hunks as JSON")
	commitCmd.Flags().StringVarP(&commitMsg, "message", "m", "", "commit message")
	commitCmd.Flags().BoolVar(&commitDryRun, "dry-run", false, "show what would be committed")
	rootCmd.AddCommand(hunksCmd, commitCmd)
}
