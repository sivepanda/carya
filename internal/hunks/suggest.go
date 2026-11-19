package hunks

import (
	"path"
	"strings"

	"carya/internal/history"
	"carya/internal/repository"
)

// Group is a set of changed files that probably belong in one commit.
type Group struct {
	Files  []string `json:"files"`
	Reason string   `json:"reason"`
}

// A history link needs both: a strength that a focused pair reaches and a
// catch-all commit does not, and more than one commit behind it.
const (
	minStrength = 0.5
	minTogether = 2
)

type linkKind int

const (
	linkTest linkKind = iota
	linkLock
	linkHistory
)

var linkReasons = []string{
	linkTest:    "code and its tests",
	linkLock:    "manifest and lock file",
	linkHistory: "often changed together",
}

type link struct {
	a, b string
	kind linkKind
}

// Suggest groups the files changed in hs that probably belong in one
// commit: code with its tests, manifests with their lock files, and files
// the history says change together (when Carya's history index is
// available in the checkout at repoRoot). Links join transitively. Only
// groups of two or more files are returned, in the order of their first
// file in hs.
func Suggest(repoRoot string, hs []Hunk) []Group {
	files := changedFiles(hs)
	links := append(testLinks(files), lockLinks(files)...)
	links = append(links, historyLinks(repoRoot, files)...)
	return join(files, links)
}

// Ungrouped returns the files changed in hs that are in none of groups.
func Ungrouped(hs []Hunk, groups []Group) []string {
	grouped := map[string]bool{}
	for _, g := range groups {
		for _, f := range g.Files {
			grouped[f] = true
		}
	}
	var out []string
	for _, f := range changedFiles(hs) {
		if !grouped[f] {
			out = append(out, f)
		}
	}
	return out
}

func changedFiles(hs []Hunk) []string {
	var files []string
	seen := map[string]bool{}
	for _, h := range hs {
		if !seen[h.Path] {
			seen[h.Path] = true
			files = append(files, h.Path)
		}
	}
	return files
}

// join unions linked files and returns the groups of two or more.
func join(files []string, links []link) []Group {
	index := map[string]int{}
	for i, f := range files {
		index[f] = i
	}
	parent := make([]int, len(files))
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for _, l := range links {
		a, b := find(index[l.a]), find(index[l.b])
		// Keep the earlier file as the root, so groups come out in file order.
		if a > b {
			a, b = b, a
		}
		parent[b] = a
	}
	kinds := map[int]map[linkKind]bool{}
	for _, l := range links {
		r := find(index[l.a])
		if kinds[r] == nil {
			kinds[r] = map[linkKind]bool{}
		}
		kinds[r][l.kind] = true
	}
	members := map[int][]string{}
	var roots []int
	for i, f := range files {
		r := find(i)
		if members[r] == nil {
			roots = append(roots, r)
		}
		members[r] = append(members[r], f)
	}
	var groups []Group
	for _, r := range roots {
		if len(members[r]) < 2 {
			continue
		}
		var reasons []string
		for k, reason := range linkReasons {
			if kinds[r][linkKind(k)] {
				reasons = append(reasons, reason)
			}
		}
		groups = append(groups, Group{Files: members[r], Reason: strings.Join(reasons, "; ")})
	}
	return groups
}

var jsExts = []string{".ts", ".tsx", ".js", ".jsx"}

// testName reports whether file is a test by its name, and the language
// and name (without extension) of the file it tests.
func testName(file string) (lang, stem string, ok bool) {
	base := path.Base(file)
	ext := path.Ext(base)
	name := strings.TrimSuffix(base, ext)
	switch {
	case ext == ".go":
		stem, ok = strings.CutSuffix(name, "_test")
		lang = "go"
	case isJS(ext):
		if stem, ok = strings.CutSuffix(name, ".test"); !ok {
			stem, ok = strings.CutSuffix(name, ".spec")
		}
		lang = "js"
	case ext == ".py":
		if stem, ok = strings.CutPrefix(name, "test_"); !ok {
			stem, ok = strings.CutSuffix(name, "_test")
		}
		lang = "py"
	}
	return lang, stem, ok && stem != ""
}

// sourceName returns the language and name (without extension) of a file
// that could have tests, or ok false.
func sourceName(file string) (lang, stem string, ok bool) {
	if _, _, isTest := testName(file); isTest {
		return "", "", false
	}
	base := path.Base(file)
	ext := path.Ext(base)
	stem = strings.TrimSuffix(base, ext)
	switch {
	case ext == ".go":
		return "go", stem, true
	case isJS(ext):
		return "js", stem, true
	case ext == ".py":
		return "py", stem, true
	}
	return "", "", false
}

func isJS(ext string) bool {
	for _, e := range jsExts {
		if ext == e {
			return true
		}
	}
	return false
}

// testLinks links each test to the code it tests: the same name in the
// same directory or, outside Go (whose tests must sit next to the code),
// the only changed file with that name anywhere, for tests kept in a
// separate tests directory.
func testLinks(files []string) []link {
	type key struct{ lang, stem string }
	sources := map[key][]string{}
	for _, f := range files {
		if lang, stem, ok := sourceName(f); ok {
			sources[key{lang, stem}] = append(sources[key{lang, stem}], f)
		}
	}
	var links []link
	for _, f := range files {
		lang, stem, ok := testName(f)
		if !ok {
			continue
		}
		candidates := sources[key{lang, stem}]
		found := false
		for _, src := range candidates {
			if path.Dir(src) == path.Dir(f) {
				links = append(links, link{src, f, linkTest})
				found = true
			}
		}
		if !found && lang != "go" && len(candidates) == 1 {
			links = append(links, link{candidates[0], f, linkTest})
		}
	}
	return links
}

var lockFiles = []struct {
	manifest string
	locks    []string
	// workspace: one lock file in a parent directory can cover several
	// manifests (npm, pnpm, yarn and bun workspaces, Cargo and uv).
	workspace bool
}{
	{"go.mod", []string{"go.sum"}, false},
	{"package.json", []string{"package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lock", "bun.lockb"}, true},
	{"Cargo.toml", []string{"Cargo.lock"}, true},
	{"pyproject.toml", []string{"uv.lock", "poetry.lock"}, true},
}

// lockLinks links each changed manifest to its changed lock file in the
// same directory or, for ecosystems with workspaces, the nearest parent
// directory that has one.
func lockLinks(files []string) []link {
	changed := map[string]bool{}
	for _, f := range files {
		changed[f] = true
	}
	var links []link
	for _, f := range files {
		for _, eco := range lockFiles {
			if path.Base(f) != eco.manifest {
				continue
			}
			for dir := path.Dir(f); ; dir = path.Dir(dir) {
				found := false
				for _, lock := range eco.locks {
					if p := path.Join(dir, lock); changed[p] {
						links = append(links, link{f, p, linkLock})
						found = true
					}
				}
				if found || !eco.workspace || dir == "." || dir == "/" {
					break
				}
			}
		}
	}
	return links
}

// historyLinks links changed files that the history index says change
// together. It returns nothing when the index is not available.
func historyLinks(repoRoot string, files []string) []link {
	if len(files) < 2 {
		return nil
	}
	repo, err := repository.NewAt(repoRoot)
	if err != nil || !repo.Exists() {
		return nil
	}
	ix, err := history.Open(repo.DBPath(), repoRoot)
	if err != nil {
		return nil
	}
	defer ix.Close()
	if _, err := ix.Update(); err != nil {
		return nil
	}
	changed := map[string]bool{}
	for _, f := range files {
		changed[f] = true
	}
	var links []link
	for _, f := range files {
		related, _, err := ix.RelatedFiles(f, 0)
		if err != nil {
			continue
		}
		for _, r := range related {
			if changed[r.Path] && r.Path != f && r.Strength >= minStrength && r.Together >= minTogether {
				links = append(links, link{f, r.Path, linkHistory})
			}
		}
	}
	return links
}
