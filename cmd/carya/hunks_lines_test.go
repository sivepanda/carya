package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"carya/internal/hunks"
)

func TestPrintDiffNumbersTheBody(t *testing.T) {
	var body strings.Builder
	for range 10 {
		body.WriteString(" same\n")
	}
	h := hunks.Hunk{
		Kind:   "change",
		Header: "@@ -1,12 +1,12 @@",
		Diff:   "@@ -1,12 +1,12 @@\n" + body.String() + "-old\n+new\n\\ No newline at end of file\n",
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	printDiff(h)
	os.Stdout = stdout
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	want := map[int]string{
		0:  "         @@ -1,12 +1,12 @@",
		1:  "       1  same",
		10: "      10  same",
		11: "      11 -old",
		12: "      12 +new",
		13: `         \ No newline at end of file`,
	}
	if len(lines) != 14 {
		t.Fatalf("printed %d lines:\n%s", len(lines), out)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d = %q, want %q", i, lines[i], w)
		}
	}
}
