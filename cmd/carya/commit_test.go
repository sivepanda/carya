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
		{nil, "nothing left uncommitted."},
		{hs("a.go", "a.go", "b.go"), "3 left in a.go, b.go."},
		{hs("a", "b", "c", "d", "e", "f", "g", "g"), "8 left in a, b, c, d, e and 2 more."},
	} {
		if got := leftover(c.rest); got != c.want {
			t.Errorf("leftover(%d hunks) = %q, want %q", len(c.rest), got, c.want)
		}
	}
}
