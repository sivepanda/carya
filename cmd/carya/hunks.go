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
other hunks. After the list come files that probably belong in one commit:
code with its tests, manifests with their lock files, and files the history
says change together.`,
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
		groups := hunks.Suggest(root, shown)
		if hunksJSON {
			if shown == nil {
				shown = []hunks.Hunk{}
			}
			if groups == nil {
				groups = []hunks.Group{}
			}
			printJSON(map[string]any{"hunks": shown, "groups": groups})
			return
		}
		if len(shown) == 0 {
			fmt.Println("Nothing to commit.")
			return
		}
		printHunks(shown, hunksDiff)
		printGroups(shown, groups)
		fmt.Println(`Commit some with: carya commit -m "<message>" <id or file>...`)
	},
}

func printGroups(hs []hunks.Hunk, groups []hunks.Group) {
	if len(groups) == 0 {
		return
	}
	fmt.Println("Files that probably belong in one commit:")
	for _, g := range groups {
		fmt.Printf("  %s  (%s)\n", strings.Join(g.Files, " "), g.Reason)
	}
	if rest := hunks.Ungrouped(hs, groups); len(rest) > 0 {
		fmt.Printf("  ungrouped: %s\n", strings.Join(rest, " "))
	}
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
		fmt.Printf("    %s  %-72s +%d -%d\n", h.ID, hunkLabel(h), h.Added, h.Removed)
		if withDiff && !binaryHunk(h) {
			for _, line := range strings.Split(strings.TrimRight(h.Diff, "\n"), "\n") {
				fmt.Printf("        %s\n", line)
			}
		}
	}
}

// hunkLabel is a hunk's @@ line, or its kind, cut to 70 characters; it
// cuts on a character boundary, as the @@ line can end in any source text.
func hunkLabel(h hunks.Hunk) string {
	label := h.Header
	if label == "" {
		label = h.Kind
	}
	if r := []rune(label); len(r) > 70 {
		label = string(r[:70]) + "…"
	}
	return label
}

// binaryHunk reports whether a hunk's diff is binary patch data, which an
// added or deleted binary file has too.
func binaryHunk(h hunks.Hunk) bool {
	return h.Kind == "binary" || strings.Contains(h.Diff, "\nGIT binary patch\n")
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
			fmt.Printf("Committed %d hunk(s); %s\n", len(picked), leftover(rest))
		}
	},
}

// leftover says in one line what a commit left uncommitted, naming the files
// so an agent can go on to commit the next group.
func leftover(rest []hunks.Hunk) string {
	if len(rest) == 0 {
		return "nothing left uncommitted."
	}
	var files []string
	for i, h := range rest {
		if i == 0 || rest[i-1].Path != h.Path {
			files = append(files, h.Path)
		}
	}
	more := ""
	if len(files) > 5 {
		more = fmt.Sprintf(" and %d more", len(files)-5)
		files = files[:5]
	}
	return fmt.Sprintf("%d left in %s%s.", len(rest), strings.Join(files, ", "), more)
}

func init() {
	hunksCmd.Flags().BoolVar(&hunksDiff, "diff", false, "show each hunk's diff")
	hunksCmd.Flags().BoolVar(&hunksJSON, "json", false, "print the hunks and suggested groups as JSON")
	commitCmd.Flags().StringVarP(&commitMsg, "message", "m", "", "commit message")
	commitCmd.Flags().BoolVar(&commitDryRun, "dry-run", false, "show what would be committed")
	rootCmd.AddCommand(hunksCmd, commitCmd)
}
