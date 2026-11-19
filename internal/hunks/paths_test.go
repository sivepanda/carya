package hunks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pathsRepo has changes at the root and in internal/, a README.md in both,
// an unchanged directory and a file deleted with its directory.
func pathsRepo(t *testing.T) *testRepo {
	r := newRepo(t)
	r.write("a.txt", "a\n")
	r.write("README.md", "top\n")
	r.write("internal/README.md", "inside\n")
	r.write("internal/store/store.go", "package store\n")
	r.write("internal/unchanged/keep.txt", "keep\n")
	r.write("docs/gone.md", "gone\n")
	r.commitAll("base")
	r.write("a.txt", "A\n")
	r.write("README.md", "TOP\n")
	r.write("internal/README.md", "INSIDE\n")
	r.write("internal/store/store.go", "package store\n\nvar x = 1\n")
	r.write("internal/store/store_test.go", "package store\n")
	r.remove("docs/gone.md")
	if err := os.Remove(filepath.Join(r.dir, "docs")); err != nil {
		t.Fatal(err)
	}
	return r
}

func paths(hs []Hunk) string {
	var out []string
	for _, h := range hs {
		out = append(out, h.Path)
	}
	return strings.Join(out, " ")
}

func checkSelect(t *testing.T, hs []Hunk, want map[string]string) {
	t.Helper()
	for ref, w := range want {
		got, err := Select(hs, []string{ref})
		if err != nil {
			t.Errorf("Select(%q): %v", ref, err)
			continue
		}
		if paths(got) != w {
			t.Errorf("Select(%q) = %q, want %q", ref, paths(got), w)
		}
	}
}

func checkSelectFails(t *testing.T, hs []Hunk, refs ...string) {
	t.Helper()
	for _, ref := range refs {
		got, err := Select(hs, []string{ref})
		if err == nil {
			t.Errorf("Select(%q) = %q, want an error", ref, paths(got))
		} else if !strings.Contains(err.Error(), ref) {
			t.Errorf("Select(%q) error %q does not name it", ref, err)
		}
	}
}

func TestSelectDirectoriesFromTheRoot(t *testing.T) {
	r := pathsRepo(t)
	hs := r.list()
	t.Chdir(r.dir)
	all := "README.md a.txt docs/gone.md internal/README.md internal/store/store.go internal/store/store_test.go"
	checkSelect(t, hs, map[string]string{
		".":                       all,
		"./":                      all,
		"internal":                "internal/README.md internal/store/store.go internal/store/store_test.go",
		"internal/":               "internal/README.md internal/store/store.go internal/store/store_test.go",
		"./internal/store":        "internal/store/store.go internal/store/store_test.go",
		"internal/store/store.go": "internal/store/store.go",
		"docs":                    "docs/gone.md",
		"./a.txt":                 "a.txt",
		filepath.Join(r.dir, "internal", "store"): "internal/store/store.go internal/store/store_test.go",
		r.dir: all,
	})
	checkSelectFails(t, hs, "internal/unchanged", "intern", "internal/store/store", "..", "../a.txt", t.TempDir())
}

func TestSelectPathsFromASubdirectory(t *testing.T) {
	r := pathsRepo(t)
	hs := r.list()
	t.Chdir(filepath.Join(r.dir, "internal"))
	checkSelect(t, hs, map[string]string{
		// From here.
		"store":          "internal/store/store.go internal/store/store_test.go",
		"store/store.go": "internal/store/store.go",
		".":              "internal/README.md internal/store/store.go internal/store/store_test.go",
		"./README.md":    "internal/README.md",
		"../a.txt":       "a.txt",
		"../docs":        "docs/gone.md",
		"..":             "README.md a.txt docs/gone.md internal/README.md internal/store/store.go internal/store/store_test.go",
		// From the root, which comes first: it is how carya hunks prints paths.
		"internal/store/store.go":               "internal/store/store.go",
		"README.md":                             "README.md",
		"a.txt":                                 "a.txt",
		filepath.Join(r.dir, "docs", "gone.md"): "docs/gone.md",
	})
	// ./ and ../ paths are only read from here.
	checkSelectFails(t, hs, "nope.go", "unchanged", "./a.txt", "../internal/nope", "../..", "../../x")

	// Errors say how a path was read.
	outside := t.TempDir()
	_, err := Select(hs, []string{"nope.go", "./a.txt", "deadbee", outside})
	want := "no hunk or changed file matches nope.go (tried nope.go, internal/nope.go), ./a.txt (tried internal/a.txt), " +
		"deadbee (tried deadbee, internal/deadbee), " + outside + " (outside the checkout); run `carya hunks` for current IDs"
	if err == nil || err.Error() != want {
		t.Errorf("error:\n %v\nwant:\n %s", err, want)
	}

	// IDs still come first.
	sel, err := Select(hs, []string{hs[0].ID, "store"})
	if err != nil || paths(sel) != hs[0].Path+" internal/store/store.go internal/store/store_test.go" {
		t.Errorf("Select(ID, store) = %q, %v", paths(sel), err)
	}
}

func TestSelectFromASymlinkedDirectory(t *testing.T) {
	skipOnWindows(t)
	r := pathsRepo(t)
	hs := r.list()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(filepath.Join(r.dir, "internal"), link); err != nil {
		t.Fatal(err)
	}
	t.Chdir(link)
	checkSelect(t, hs, map[string]string{
		"store":                          "internal/store/store.go internal/store/store_test.go",
		"../a.txt":                       "a.txt",
		filepath.Join(link, "store"):     "internal/store/store.go internal/store/store_test.go",
		filepath.Join(link, "README.md"): "internal/README.md",
	})
}

func TestSelectWithoutACheckoutReadsPathsFromTheRoot(t *testing.T) {
	// Hunks that did not come from List have no checkout to read paths
	// against: paths are the checkout's, whatever the current directory.
	hs := []Hunk{{ID: "1111111", Path: "a/b.go"}, {ID: "2222222", Path: "c.go"}}
	checkSelect(t, hs, map[string]string{"a": "a/b.go", "./c.go": "c.go", ".": "a/b.go c.go"})
	checkSelectFails(t, hs, "b.go", "/a/b.go")
}

func TestCommitDirectoryFromASubdirectory(t *testing.T) {
	r := pathsRepo(t)
	t.Chdir(filepath.Join(r.dir, "internal"))
	r.commit("store", "store")
	if got := r.git("show", "--name-only", "--format=", "HEAD"); got != "internal/store/store.go\ninternal/store/store_test.go\n" {
		t.Errorf("committed files:\n%s", got)
	}
	if got := paths(r.list()); got != "README.md a.txt docs/gone.md internal/README.md" {
		t.Errorf("left uncommitted: %q", got)
	}
}
