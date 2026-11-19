package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"carya/internal/hunks"

	"github.com/spf13/cobra"
)

var (
	hunksDiff    bool
	hunksFiles   bool
	hunksJSON    bool
	commitMsg    string
	commitDryRun bool
)

// commitHint ends the listing; it is the same in both forms.
const commitHint = `Commit some with: carya commit -m "<message>" <id, file or directory>...`

var hunksCmd = &cobra.Command{
	Use:   "hunks [id, file or directory...]",
	Short: "List uncommitted changes as hunks you can commit separately",
	Long: `List the working tree's changes against HEAD, untracked files included, as
hunks with short IDs. Commit any grouping of them with
'carya commit -m <message> <id, file or directory>...'; the rest stays
uncommitted. IDs come from each hunk's content, so they stay the same while
you commit other hunks. Each hunk shows its ID, lines added and removed, the
lines of the file it changes (L10-14) and its first changed line; --files
shows one line per file instead. After the list come files that probably
belong in one commit: code with its tests, manifests with their lock files,
and files the history says change together.

Paths are printed from the checkout root. Paths you give are read from the
checkout root and then from the current directory; ./ and ../ paths only
from the current directory.`,
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
		if hunksFiles {
			printFiles(shown)
		} else {
			printHunks(shown, hunksDiff)
		}
		printGroups(shown, groups)
		fmt.Println(commitHint)
	},
}

func printGroups(hs []hunks.Hunk, groups []hunks.Group) {
	if len(groups) == 0 {
		return
	}
	fmt.Println("Files that probably belong in one commit:")
	for _, g := range groups {
		fmt.Printf("  %s: %s\n", g.Reason, strings.Join(g.Files, " "))
	}
	if rest := hunks.Ungrouped(hs, groups); len(rest) > 0 {
		fmt.Printf("  ungrouped: %s\n", strings.Join(rest, " "))
	}
}

// fileTotal is one file's hunks and line counts.
type fileTotal struct {
	path, note     string
	ids            []string
	added, removed int
}

// fileTotals sums hs by file, in their order; a file's hunks are together.
func fileTotals(hs []hunks.Hunk) []fileTotal {
	var out []fileTotal
	for i, h := range hs {
		if i == 0 || hs[i-1].Path != h.Path {
			out = append(out, fileTotal{path: h.Path, note: fileNote(h)})
		}
		f := &out[len(out)-1]
		f.ids = append(f.ids, h.ID)
		f.added += h.Added
		f.removed += h.Removed
	}
	return out
}

// printFiles prints the listing with one line per file, for --files.
func printFiles(hs []hunks.Hunk) {
	files := fileTotals(hs)
	fmt.Printf("%s in %s:\n", plural(len(hs), "hunk"), plural(len(files), "file"))
	for _, row := range fileRows(files, listWidth) {
		fmt.Println(row)
	}
}

// maxPathColumn is the widest path fileRows pads others to; a longer one
// only pushes its own line's columns along.
const maxPathColumn = 44

// fileRows formats the lines for --files: each file's path, lines added
// and removed and hunk IDs, in columns. IDs that would go past width go on
// further lines, under the first.
func fileRows(files []fileTotal, width int) []string {
	pathW, plusW, minusW := 0, 0, 0
	for _, f := range files {
		if w := utf8.RuneCountInString(f.path + f.note); w <= maxPathColumn {
			pathW = max(pathW, w)
		}
		plusW = max(plusW, len(fmt.Sprint(f.added))+1)
		minusW = max(minusW, len(fmt.Sprint(f.removed))+1)
	}
	var rows []string
	for _, f := range files {
		name := f.path + f.note
		pad := strings.Repeat(" ", max(pathW-utf8.RuneCountInString(name), 0))
		head := fmt.Sprintf("  %s%s  %*s %-*s", name, pad, plusW, fmt.Sprintf("+%d", f.added), minusW, fmt.Sprintf("-%d", f.removed))
		line := head + " "
		for i, id := range f.ids {
			if i > 0 && utf8.RuneCountInString(line)+1+len(id) > width {
				rows = append(rows, line)
				line = strings.Repeat(" ", utf8.RuneCountInString(head)+1)
			}
			line += " " + id
		}
		rows = append(rows, line)
	}
	return rows
}

// listWidth is the width the listing keeps to, cutting hunk labels to fit.
const listWidth = 80

func printHunks(hs []hunks.Hunk, withDiff bool) {
	fmt.Printf("%s in %s:\n", plural(len(hs), "hunk"), plural(len(fileTotals(hs)), "file"))
	rows := hunkRows(hs, listWidth)
	for i, h := range hs {
		if i == 0 || hs[i-1].Path != h.Path {
			fmt.Printf("  %s%s\n", h.Path, fileNote(h))
		}
		fmt.Println(rows[i])
		if withDiff && !binaryHunk(h) {
			printDiff(h)
		}
	}
}

// fileNote says what kind of change a file's is, after its path.
func fileNote(h hunks.Hunk) string {
	return map[string]string{"add": " (new file)", "delete": " (deleted)", "binary": " (binary)", "mode": " (mode change)"}[h.Kind]
}

// hunkRows formats each hunk's line of the listing: its ID, line counts,
// the lines it changes and its label, in columns as wide as the widest
// entry, with labels cut so lines fit in width.
func hunkRows(hs []hunks.Hunk, width int) []string {
	where := make([]string, len(hs))
	idW, plusW, minusW, whereW := 0, 0, 0, 0
	for i, h := range hs {
		if first, last, ok := changedLines(h); ok {
			where[i] = lineRange(first, last)
		}
		idW = max(idW, len(h.ID))
		plusW = max(plusW, len(fmt.Sprint(h.Added))+1)
		minusW = max(minusW, len(fmt.Sprint(h.Removed))+1)
		whereW = max(whereW, len(where[i]))
	}
	rows := make([]string, len(hs))
	for i, h := range hs {
		row := fmt.Sprintf("    %-*s  %*s %-*s", idW, h.ID, plusW, fmt.Sprintf("+%d", h.Added), minusW, fmt.Sprintf("-%d", h.Removed))
		if whereW > 0 {
			row += fmt.Sprintf("  %-*s", whereW, where[i])
		}
		room := max(width-utf8.RuneCountInString(row)-2, 20)
		rows[i] = strings.TrimRight(row+"  "+cut(hunkLabel(h), room), " ")
	}
	return rows
}

// lineRange shows lines of the working tree file as "L10" or "L10-14"; the
// L tells them from the numbers of a hunk's own lines that --diff shows.
func lineRange(first, last int) string {
	if first == last {
		return "L" + strconv.Itoa(first)
	}
	return fmt.Sprintf("L%d-%d", first, last)
}

var hunkStart = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@ ?(.*)$`)

// changedLines returns the lines of the working tree file a change hunk
// adds, or, where it removes lines, the line now after them; ok is false
// for whole-file hunks, which have no lines to show.
func changedLines(h hunks.Hunk) (first, last int, ok bool) {
	m := hunkStart.FindStringSubmatch(h.Header)
	if h.Kind != "change" || m == nil {
		return 0, 0, false
	}
	n, _ := strconv.Atoi(m[1])
	_, body, _ := strings.Cut(h.Diff, "\n")
	for _, l := range strings.Split(body, "\n") {
		if l == "" {
			continue
		}
		switch l[0] {
		case ' ':
			n++
		case '+', '-':
			if !ok {
				first, ok = n, true
			}
			last = n
			if l[0] == '+' {
				n++
			}
		}
	}
	return first, last, ok
}

// hunkLabel says what a hunk changes. A change is named by its first
// changed line with any letter or digit in it, preferring added lines,
// which are the working tree's, to removed ones. Git's function context,
// the line above the hunk that looks like a declaration, is often the
// declaration before the one changed, or a line in a string, so it is only
// added for changes to blank lines and punctuation, which say nothing.
// A whole-file hunk is named by its size or mode change.
func hunkLabel(h hunks.Hunk) string {
	switch {
	case binaryHunk(h):
		return "binary data"
	case h.Kind == "add":
		return lineCount(h.Added)
	case h.Kind == "delete":
		return lineCount(h.Removed)
	case h.Kind == "mode":
		return modeChange(h.Diff)
	case h.Kind != "change":
		return h.Kind
	}
	var added, removed, other string
	_, body, _ := strings.Cut(h.Diff, "\n")
	for _, l := range strings.Split(body, "\n") {
		if l == "" || (l[0] != '+' && l[0] != '-') {
			continue
		}
		text := oneLine(l[1:])
		switch {
		case !strings.ContainsFunc(text, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }):
			if other == "" {
				other = text
			}
		case l[0] == '+' && added == "":
			added = text
		case l[0] == '-' && removed == "":
			removed = text
		}
	}
	switch {
	case added != "":
		return added
	case removed != "":
		return "removed: " + removed
	}
	label := other
	if label == "" {
		label = "blank lines"
	}
	if m := hunkStart.FindStringSubmatch(h.Header); m != nil && oneLine(m[2]) != "" {
		label += " near " + oneLine(m[2])
	}
	return label
}

func lineCount(n int) string {
	if n == 0 {
		return "empty"
	}
	return plural(n, "line")
}

// modeChange reads a mode change's old and new modes from its diff.
func modeChange(diff string) string {
	var from, to string
	for _, l := range strings.Split(diff, "\n") {
		if m, ok := strings.CutPrefix(l, "old mode "); ok {
			from = m
		} else if m, ok := strings.CutPrefix(l, "new mode "); ok {
			to = m
		}
	}
	if from == "" || to == "" {
		return "mode change"
	}
	return from + " -> " + to
}

// oneLine makes source text fit on one line of the listing: it trims it,
// turns tabs and other control characters into spaces and replaces bytes
// that are not UTF-8.
func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, "\uFFFD"))
	return strings.TrimSpace(s)
}

// cut shortens s to n characters, ending it with "…" when it cuts; it cuts
// on a character boundary, as labels can be any source text.
func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// plural is n and word, with an s unless n is 1.
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// binaryHunk reports whether a hunk's diff is binary patch data, which an
// added or deleted binary file has too.
func binaryHunk(h hunks.Hunk) bool {
	return h.Kind == "binary" || strings.Contains(h.Diff, "\nGIT binary patch\n")
}

var commitCmd = &cobra.Command{
	Use:   "commit -m <message> <hunk id, file or directory>...",
	Short: "Commit exactly the chosen hunks",
	Long: `Commit the hunks (IDs from 'carya hunks'), whole files and directories
given, and nothing else: other changes, staged or not, stay as they are. It
runs git commit, so your identity and the repository's commit hooks apply.
Paths are read from the checkout root and then from the current directory;
./ and ../ paths only from the current directory.`,
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
		fmt.Printf("Committed %s.\n", inFiles(picked))
		if rest, err := hunks.List(root); err == nil {
			fmt.Println(leftover(rest))
		}
	},
}

// leftover says in one line what a commit left uncommitted, naming the files
// so an agent can go on to commit the next group.
func leftover(rest []hunks.Hunk) string {
	if len(rest) == 0 {
		return "Nothing left uncommitted."
	}
	return "Left uncommitted: " + inFiles(rest) + "."
}

// inFiles counts hunks and names their files, the first five of them, from
// the checkout root as the listing does.
func inFiles(hs []hunks.Hunk) string {
	var files []string
	for _, f := range fileTotals(hs) {
		files = append(files, f.path)
	}
	more := ""
	if len(files) > 5 {
		more = fmt.Sprintf(" and %d more", len(files)-5)
		files = files[:5]
	}
	return fmt.Sprintf("%s in %s%s", plural(len(hs), "hunk"), strings.Join(files, ", "), more)
}

func init() {
	hunksCmd.Flags().BoolVar(&hunksDiff, "diff", false, "show each hunk's diff")
	hunksCmd.Flags().BoolVar(&hunksFiles, "files", false, "show one line per file: its hunk IDs and lines added and removed")
	hunksCmd.Flags().BoolVar(&hunksJSON, "json", false, "print the hunks and suggested groups as JSON")
	commitCmd.Flags().StringVarP(&commitMsg, "message", "m", "", "commit message")
	commitCmd.Flags().BoolVar(&commitDryRun, "dry-run", false, "show what would be committed")
	hunksCmd.MarkFlagsMutuallyExclusive("files", "diff")
	rootCmd.AddCommand(hunksCmd, commitCmd)
}
