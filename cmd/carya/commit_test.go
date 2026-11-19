package main

import (
	"testing"

	"carya/internal/hunks"
)

func TestLeftoverNamesTheFilesLeft(t *testing.T) {
	hs := func(paths ...string) []hunks.Hunk {
		var out []hunks.Hunk
		for _, p := range paths {
			out = append(out, hunks.Hunk{Path: p})
		}
		return out
	}
	for _, c := range []struct {
		rest []hunks.Hunk
		want string
	}{
		{nil, "Nothing left uncommitted."},
		{hs("a.go"), "Left uncommitted: 1 hunk in a.go."},
		{hs("a.go", "a.go", "b.go"), "Left uncommitted: 3 hunks in a.go, b.go."},
		{hs("a", "b", "c", "d", "e", "f", "g", "g"), "Left uncommitted: 8 hunks in a, b, c, d, e and 2 more."},
	} {
		if got := leftover(c.rest); got != c.want {
			t.Errorf("leftover(%d hunks) = %q, want %q", len(c.rest), got, c.want)
		}
	}
}
