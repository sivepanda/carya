package main

import (
	"strings"
	"testing"
	"unicode/utf8"

	"carya/internal/hunks"
)

func TestHunkLabelCutsLongHeadersOnCharacterBoundaries(t *testing.T) {
	h := hunks.Hunk{Kind: "change", Header: "@@ -1,3 +1,3 @@ func " + strings.Repeat("é", 80)}
	label := hunkLabel(h)
	if !utf8.ValidString(label) || utf8.RuneCountInString(label) != 71 || !strings.HasSuffix(label, "…") {
		t.Errorf("label %q", label)
	}
	if got := hunkLabel(hunks.Hunk{Kind: "add"}); got != "add" {
		t.Errorf("label of a new file %q", got)
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
