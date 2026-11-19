package main

import "testing"

func TestSplitLine(t *testing.T) {
	for _, tc := range []struct {
		in   string
		path string
		line int
	}{
		{"internal/git/refs.go:42", "internal/git/refs.go", 42},
		{"internal/git/refs.go", "internal/git/refs.go", 0},
		{"weird:name.go", "weird:name.go", 0},
		{"file.go:0", "file.go:0", 0},
		{":12", ":12", 0},
	} {
		path, line := splitLine(tc.in)
		if path != tc.path || line != tc.line {
			t.Errorf("splitLine(%q) = %q, %d; want %q, %d", tc.in, path, line, tc.path, tc.line)
		}
	}
}
