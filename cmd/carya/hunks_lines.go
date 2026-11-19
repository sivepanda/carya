package main

import (
	"fmt"
	"strconv"

	"carya/internal/hunks"
)

// printDiff prints a hunk's diff with the lines of its body numbered, so
// some of them can be committed as <id>:<lines>.
func printDiff(h hunks.Hunk) {
	lines := h.Lines()
	width := 2
	for _, l := range lines {
		width = max(width, len(strconv.Itoa(l.N)))
	}
	for _, l := range lines {
		n := ""
		if l.N > 0 {
			n = strconv.Itoa(l.N)
		}
		fmt.Printf("      %*s %s\n", width, n, l.Text)
	}
}

func init() {
	hunksCmd.Example = `  carya hunks --diff            show each hunk's diff, its lines numbered
  carya hunks 3b04596:1-40,52   show just lines 1 to 40 and 52 of hunk 3b04596`
	commitCmd.Example = `  carya commit -m "feat: parser" 3b04596 lexer.go
  carya commit -m "feat: parser" 3b04596:1-40,52

<id>:<lines> commits only those lines of a hunk, numbered as in
'carya hunks --diff': chosen added lines are committed and the others left
out, removed lines not chosen stay in the file, and context lines do not
matter. The rest of the hunk stays uncommitted, under a new ID.`
}
