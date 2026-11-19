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
