package harness

import "strings"

// applyPatchHeaders are the file headers of the apply_patch format Codex and
// OpenCode use:
//
//	*** Begin Patch
//	*** Update File: src/app.go
//	*** Move to: src/main.go
//	@@ ...
//	*** Add File: docs/new.md
//	*** Delete File: old.txt
//	*** End Patch
var applyPatchHeaders = []string{"*** Add File: ", "*** Update File: ", "*** Delete File: ", "*** Move to: "}

// ApplyPatchPaths returns the paths an apply_patch patch touches, in order
// and without duplicates. A move reports both the old and the new path.
func ApplyPatchPaths(patch string) []string {
	seen := make(map[string]bool)
	var paths []string
	for _, line := range strings.Split(patch, "\n") {
		line = strings.TrimRight(line, "\r")
		for _, h := range applyPatchHeaders {
			if p, ok := strings.CutPrefix(line, h); ok {
				p = strings.TrimSpace(p)
				if p != "" && !seen[p] {
					seen[p] = true
					paths = append(paths, p)
				}
				break
			}
		}
	}
	return paths
}
