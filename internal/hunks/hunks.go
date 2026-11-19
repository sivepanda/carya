// Package hunks splits an agent's uncommitted work into hunks it can commit
// in any grouping, without interactive staging. Agents cannot drive
// `git add -p`; this gives them stable hunk IDs, suggestions for which files
// belong in one commit, and a way to commit exactly the hunks they choose.
package hunks

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"carya/internal/git"
	"carya/internal/patch"
)

// Hunk is one independently committable piece of the working tree's changes:
// a diff hunk, or a whole file for additions, deletions, binary files and
// mode changes.
type Hunk struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Kind    string `json:"kind"`             // "change", "add", "delete", "binary" or "mode"
	Header  string `json:"header,omitempty"` // the @@ line for a change
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Diff    string `json:"diff"` // the hunk alone, without the file header

	fileHeader string
	order      int
}

// List returns the working tree's changes against HEAD as hunks, in file
// and then line order. It sees what `carya snapshot` sees: tracked changes,
// deletions and untracked files that are not ignored.
func List(repoRoot string) ([]Hunk, error) {
	tree, err := git.WorkingStateTree(repoRoot)
	if err != nil {
		return nil, err
	}
	base := "4b825dc642cb6eb9a060e54bf8d69288fbee4904" // the empty tree
	if head, err := run(repoRoot, "rev-parse", "--verify", "--quiet", "HEAD^{tree}"); err == nil {
		base = strings.TrimSpace(head)
	}
	diff, err := run(repoRoot, "-c", "core.quotePath=false", "diff-tree", "-p", "--binary", "--no-renames", "--full-index", "--no-color", base, tree)
	if err != nil {
		return nil, fmt.Errorf("diff working tree: %w", err)
	}
	return parse(diff), nil
}

var diffHeader = regexp.MustCompile(`^diff --git a/(.*) b/(.*)$`)
var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+\d+(?:,\d+)? @@`)

func parse(diff string) []Hunk {
	var out []Hunk
	lines := strings.SplitAfter(diff, "\n")
	for i := 0; i < len(lines); {
		m := diffHeader.FindStringSubmatch(strings.TrimRight(lines[i], "\n"))
		if m == nil {
			i++
			continue
		}
		path := m[2]
		j := i + 1
		for j < len(lines) && !strings.HasPrefix(lines[j], "diff --git ") {
			j++
		}
		out = append(out, parseFile(path, lines[i:j])...)
		i = j
	}
	assignIDs(out)
	return out
}

// parseFile splits one file's section into hunks.
func parseFile(path string, section []string) []Hunk {
	h := 0
	for h < len(section) && !hunkHeader.MatchString(section[h]) {
		h++
	}
	header := strings.Join(section[:h], "")
	kind := "change"
	switch {
	case strings.Contains(header, "\nnew file mode "):
		kind = "add"
	case strings.Contains(header, "\ndeleted file mode "):
		kind = "delete"
	case strings.Contains(header, "\nGIT binary patch") || strings.Contains(header, "\nBinary files "):
		kind = "binary"
	case h == len(section):
		kind = "mode"
	}

	// Whole-file changes cannot be split: they are one hunk.
	if kind != "change" || h == len(section) {
		whole := strings.Join(section, "")
		added, removed := count(section[h:])
		return []Hunk{{Path: path, Kind: kind, Added: added, Removed: removed, Diff: whole, fileHeader: ""}}
	}

	var hunks []Hunk
	for h < len(section) {
		end := h + 1
		for end < len(section) && !hunkHeader.MatchString(section[end]) {
			end++
		}
		body := section[h:end]
		added, removed := count(body[1:])
		hunks = append(hunks, Hunk{
			Path:       path,
			Kind:       "change",
			Header:     strings.TrimRight(body[0], "\n"),
			Added:      added,
			Removed:    removed,
			Diff:       strings.Join(body, ""),
			fileHeader: header,
		})
		h = end
	}
	return hunks
}

func count(lines []string) (added, removed int) {
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "+++ "), strings.HasPrefix(l, "--- "):
		case strings.HasPrefix(l, "+"):
			added++
		case strings.HasPrefix(l, "-"):
			removed++
		}
	}
	return added, removed
}

// assignIDs gives each hunk a short ID from its path and content, leaving
// out line numbers, so a hunk keeps its ID while other hunks are committed
// around it.
func assignIDs(hunks []Hunk) {
	seen := map[string]int{}
	for i := range hunks {
		h := &hunks[i]
		h.order = i
		body := h.Diff
		if h.Kind == "change" {
			_, body, _ = strings.Cut(h.Diff, "\n") // drop the @@ line
		}
		sum := sha1.Sum([]byte(h.Path + "\x00" + body))
		id := hex.EncodeToString(sum[:])[:7]
		seen[id]++
		if n := seen[id]; n > 1 {
			id += "-" + strconv.Itoa(n)
		}
		h.ID = id
	}
}

// Select resolves hunk IDs and file paths to hunks. A path selects every
// hunk of that file. It fails on anything that matches nothing, which is
// usually an ID from before the working tree changed.
func Select(all []Hunk, refs []string) ([]Hunk, error) {
	byID := map[string]Hunk{}
	byPath := map[string][]Hunk{}
	for _, h := range all {
		byID[h.ID] = h
		byPath[h.Path] = append(byPath[h.Path], h)
	}
	picked := map[int]Hunk{}
	var unknown []string
	for _, ref := range refs {
		if h, ok := byID[ref]; ok {
			picked[h.order] = h
			continue
		}
		if hs, ok := byPath[strings.TrimPrefix(ref, "./")]; ok {
			for _, h := range hs {
				picked[h.order] = h
			}
			continue
		}
		unknown = append(unknown, ref)
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("no hunk or changed file matches %s; run `carya hunks` for current IDs", strings.Join(unknown, ", "))
	}
	out := make([]Hunk, 0, len(picked))
	for _, h := range picked {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].order < out[j].order })
	return out, nil
}

// Patch joins hunks into one patch that applies to HEAD.
func Patch(hunks []Hunk) string {
	var b strings.Builder
	lastHeader := ""
	for _, h := range hunks {
		if h.fileHeader == "" {
			b.WriteString(h.Diff)
			lastHeader = ""
			continue
		}
		if h.fileHeader != lastHeader {
			b.WriteString(h.fileHeader)
			lastHeader = h.fileHeader
		}
		b.WriteString(h.Diff)
	}
	return b.String()
}

// Commit commits exactly the hunks refs names, with message, through a
// temporary index, so other changes (staged or not) stay as they are. It
// runs `git commit`, so the repository's commit hooks and the user's
// identity apply. It returns git's output.
func Commit(repoRoot string, refs []string, message string) (string, []Hunk, error) {
	if strings.TrimSpace(message) == "" {
		return "", nil, fmt.Errorf("a commit message is required")
	}
	all, err := List(repoRoot)
	if err != nil {
		return "", nil, err
	}
	if len(all) == 0 {
		return "", nil, fmt.Errorf("nothing to commit")
	}
	picked, err := Select(all, refs)
	if err != nil {
		return "", nil, err
	}
	out, err := patch.ApplyAndCommit(repoRoot, Patch(picked), message)
	return out, picked, err
}

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}
