package main

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"carya/internal/hunks"
)

// change builds a change hunk from its @@ line and body lines.
func change(id, header string, body ...string) hunks.Hunk {
	h := hunks.Hunk{ID: id, Path: "f.go", Kind: "change", Header: header, Diff: header + "\n" + strings.Join(body, "\n") + "\n"}
	for _, l := range body {
		switch {
		case strings.HasPrefix(l, "+"):
			h.Added++
		case strings.HasPrefix(l, "-"):
			h.Removed++
		}
	}
	return h
}

func TestChangedLines(t *testing.T) {
	for _, c := range []struct {
		name        string
		h           hunks.Hunk
		first, last int
	}{
		{"one changed line", change("a", "@@ -15,6 +15,6 @@ func main() {", " a", " b", " c", "-old", "+new", " d"), 18, 18},
		{"lines added", change("a", "@@ -7,7 +7,11 @@", " a", " b", " c", "-x", "+1", "+2", "+3", "+4", "+5", " d"), 10, 14},
		{"lines removed", change("a", "@@ -24,5 +24,3 @@", " a", " b", "-x", "-y", " c"), 26, 26},
		{"removed after added", change("a", "@@ -1,4 +1,4 @@", "+new", " a", " b", "-old", " c"), 1, 4},
		{"no newline at end", change("a", "@@ -3 +3 @@", "-x", `\ No newline at end of file`, "+y", `\ No newline at end of file`), 3, 3},
	} {
		first, last, ok := changedLines(c.h)
		if !ok || first != c.first || last != c.last {
			t.Errorf("%s: changedLines = %d, %d, %v; want %d, %d", c.name, first, last, ok, c.first, c.last)
		}
	}
	if _, _, ok := changedLines(hunks.Hunk{Kind: "add", Added: 3}); ok {
		t.Error("a new file has changed lines")
	}
}

func TestHunkLabel(t *testing.T) {
	for _, c := range []struct {
		name string
		h    hunks.Hunk
		want string
	}{
		{"first added line, trimmed", change("a", "@@ -1,3 +1,3 @@ x := `inside a raw string", " a", "-\told()", "+\tf(\"a  b\")\t// comment ", " b"), `f("a  b") // comment`},
		{"punctuation and blank lines skipped", change("a", "@@ -1,2 +1,5 @@", " a", "+", "+}", "+\treturn x", " b"), "return x"},
		{"only removed", change("a", "@@ -1,3 +1,2 @@ func f() {", " a", "-\tgone := 1", " b"), "removed: gone := 1"},
		{"only punctuation, near git's function context", change("a", "@@ -1,3 +1,4 @@ func main() {", " a", "+}", " b"), "} near func main() {"},
		{"only blank lines", change("a", "@@ -1,3 +1,4 @@", " a", "+", "+  ", " b"), "blank lines"},
		{"control characters and bad bytes", change("a", "@@ -1 +1 @@", "-x", "+a\x1b[31mred\r\xff"), "a [31mred \uFFFD"},
		{"new file", hunks.Hunk{Kind: "add", Added: 10}, "10 lines"},
		{"new empty file", hunks.Hunk{Kind: "add"}, "empty"},
		{"deleted file", hunks.Hunk{Kind: "delete", Removed: 1}, "1 line"},
		{"mode change", hunks.Hunk{Kind: "mode", Diff: "diff --git a/run.sh b/run.sh\nold mode 100644\nnew mode 100755\n"}, "100644 -> 100755"},
		{"binary file", hunks.Hunk{Kind: "binary"}, "binary data"},
		{"new binary file", hunks.Hunk{Kind: "add", Diff: "diff --git a/x.bin b/x.bin\nnew file mode 100644\nindex 0000000..1111111\nGIT binary patch\nliteral 3\nKcmZQzWB>pF\n\nliteral 0\nHcmV?d00001\n\n"}, "binary data"},
	} {
		if got := hunkLabel(c.h); got != c.want {
			t.Errorf("%s: hunkLabel = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestHunkRowsAlignAndFit(t *testing.T) {
	long := change("abc1234@120", "@@ -1,3 +1,3 @@", "-x", "+func "+strings.Repeat("é", 80))
	hs := []hunks.Hunk{
		change("1111111", "@@ -15,6 +15,6 @@", "-old", "+new"),
		{ID: "2222222", Path: "new.go", Kind: "add", Added: 120},
		long,
	}
	rows := hunkRows(hs, 80)
	want := []string{
		"    1111111        +1 -1  L15  new",
		"    2222222      +120 -0       120 lines",
	}
	for i, w := range want {
		if rows[i] != w {
			t.Errorf("row %d:\n got %q\nwant %q", i, rows[i], w)
		}
	}
	last := rows[2]
	if !utf8.ValidString(last) || utf8.RuneCountInString(last) != 80 || !strings.HasSuffix(last, "é…") ||
		!strings.HasPrefix(last, "    abc1234@120    +1 -1  L1   func é") {
		t.Errorf("long row %q (%d characters)", last, utf8.RuneCountInString(last))
	}

	// Without changes that have lines, there is no empty column for them.
	if rows := hunkRows([]hunks.Hunk{{ID: "3333333", Kind: "delete", Removed: 2}}, 80); rows[0] != "    3333333  +0 -2  2 lines" {
		t.Errorf("row of a deleted file %q", rows[0])
	}
}

func TestFileRows(t *testing.T) {
	hs := []hunks.Hunk{
		{ID: "1111111", Path: "a.go", Kind: "change", Added: 3, Removed: 1},
		{ID: "2222222", Path: "a.go", Kind: "change", Added: 10},
		{ID: "3333333", Path: "dir/ünï.txt", Kind: "add", Added: 1},
		{ID: "4444444", Path: strings.Repeat("long/", 10) + "x.go", Kind: "delete", Removed: 120},
	}
	files := fileTotals(hs)
	if len(files) != 3 || strings.Join(files[0].ids, " ") != "1111111 2222222" || files[0].added != 13 || files[0].removed != 1 {
		t.Fatalf("fileTotals = %+v", files)
	}
	want := []string{
		"  a.go                    +13 -1    1111111 2222222",
		"  dir/ünï.txt (new file)   +1 -0    3333333",
		"  " + strings.Repeat("long/", 10) + "x.go (deleted)   +0 -120  4444444",
	}
	if got := fileRows(files, 80); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("fileRows:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// IDs that do not fit go on further lines, under the first.
	many := fileTotal{path: "many.go", added: 12}
	for i := 1; i <= 12; i++ {
		many.ids = append(many.ids, fmt.Sprintf("a%06d", i))
	}
	want = []string{
		"  many.go  +12 -0  " + strings.Join(many.ids[:7], " "),
		strings.Repeat(" ", 19) + strings.Join(many.ids[7:], " "),
	}
	got := fileRows([]fileTotal{many}, 80)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("fileRows with many IDs:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, row := range got {
		if len(row) > 80 {
			t.Errorf("row of %d characters: %q", len(row), row)
		}
	}
}

func TestCutsOnCharacterBoundaries(t *testing.T) {
	if got := cut(strings.Repeat("é", 30), 20); !utf8.ValidString(got) || utf8.RuneCountInString(got) != 20 || !strings.HasSuffix(got, "é…") {
		t.Errorf("cut = %q", got)
	}
	if got := cut("short", 20); got != "short" {
		t.Errorf("cut = %q", got)
	}
}

func TestBinaryHunkCoversAddedAndDeletedBinaryFiles(t *testing.T) {
	added := hunks.Hunk{Kind: "add", Diff: "diff --git a/x.bin b/x.bin\nnew file mode 100644\nindex 0000000..1111111\nGIT binary patch\nliteral 3\nKcmZQzWB>pF\n\nliteral 0\nHcmV?d00001\n\n"}
	text := hunks.Hunk{Kind: "add", Diff: "diff --git a/x.txt b/x.txt\nnew file mode 100644\n--- /dev/null\n+++ b/x.txt\n@@ -0,0 +1 @@\n+GIT binary patch\n"}
	if !binaryHunk(added) || !binaryHunk(hunks.Hunk{Kind: "binary"}) {
		t.Error("binary hunk not recognised")
	}
	if binaryHunk(text) {
		t.Error("text file mentioning a binary patch taken for binary")
	}
}
