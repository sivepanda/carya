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
	return parse(diff)
}

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+\d+(?:,\d+)? @@`)

func parse(diff string) ([]Hunk, error) {
	var out []Hunk
	lines := strings.SplitAfter(diff, "\n")
	for i := 0; i < len(lines); {
		if !strings.HasPrefix(lines[i], "diff --git ") {
			i++
			continue
		}
		path, err := diffPath(strings.TrimSuffix(lines[i], "\n"))
		if err != nil {
			return nil, err
		}
		j := i + 1
		for j < len(lines) && !strings.HasPrefix(lines[j], "diff --git ") {
			j++
		}
		out = append(out, parseFile(path, lines[i:j])...)
		i = j
	}
	assignIDs(out)
	return out, nil
}

// diffPath reads the path from a "diff --git a/<path> b/<path>" line. Without
// renames both names are the same path, which is how a path that itself has
// " b/" in it is read correctly. Git quotes a path C-style when it has a
// double quote, backslash or control character in it.
func diffPath(line string) (string, error) {
	names := strings.TrimPrefix(line, "diff --git ")
	if strings.HasPrefix(names, `"`) {
		a, rest, ok := unquote(names)
		if ok && strings.HasPrefix(a, "a/") && strings.HasPrefix(rest, " ") {
			if b, rest, ok := unquote(rest[1:]); ok && rest == "" && b == "b/"+a[2:] {
				return a[2:], nil
			}
		}
	} else if n := (len(names) - len("a/ b/")) / 2; n > 0 && len(names) == 2*n+len("a/ b/") {
		if p := names[2 : 2+n]; names[:2] == "a/" && names[2+n:5+n] == " b/" && names[5+n:] == p {
			return p, nil
		}
	}
	return "", fmt.Errorf("cannot read the path from %q", line)
}

// unquote reads a C-style quoted string, as git writes unusual paths, from
// the start of s, and returns it and the rest of s. Bytes git leaves alone
// pass through as they are, so non-UTF-8 names survive.
func unquote(s string) (string, string, bool) {
	if !strings.HasPrefix(s, `"`) {
		return "", s, false
	}
	escapes := map[byte]byte{'a': '\a', 'b': '\b', 't': '\t', 'n': '\n', 'v': '\v', 'f': '\f', 'r': '\r', '"': '"', '\\': '\\'}
	octal := func(c byte) bool { return '0' <= c && c <= '7' }
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		switch c := s[i]; c {
		case '"':
			return b.String(), s[i+1:], true
		case '\\':
			if i+1 >= len(s) {
				return "", s, false
			}
			e := s[i+1]
			if r, ok := escapes[e]; ok {
				b.WriteByte(r)
				i++
				continue
			}
			if i+3 >= len(s) || e > '3' || !octal(e) || !octal(s[i+2]) || !octal(s[i+3]) {
				return "", s, false
			}
			b.WriteByte((e-'0')<<6 | (s[i+2]-'0')<<3 | (s[i+3] - '0'))
			i += 3
		default:
			b.WriteByte(c)
		}
	}
	return "", s, false
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

// count counts the added and removed lines of hunk bodies. Every body line
// starts with ' ', '+', '-' or '\', so "--- x" is a removed "-- x", not a
// file header: those come before the first @@ line and are never counted.
func count(lines []string) (added, removed int) {
	for _, l := range lines {
		switch {
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
