package history

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type repo struct {
	t   *testing.T
	dir string
}

func (r repo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit writes files (an empty content deletes the file) and commits.
func (r repo) commit(msg string, files map[string]string) {
	r.t.Helper()
	for p, c := range files {
		full := filepath.Join(r.dir, p)
		if c == "" {
			os.Remove(full)
			continue
		}
		os.MkdirAll(filepath.Dir(full), 0755)
		if err := os.WriteFile(full, []byte(c), 0644); err != nil {
			r.t.Fatal(err)
		}
	}
	r.git("add", "-A")
	r.git("-c", "user.name=Dev", "-c", "user.email=d@e", "commit", "-q", "-m", msg)
}

func newRepo(t *testing.T) repo {
	t.Helper()
	r := repo{t, t.TempDir()}
	r.git("init", "-q", "-b", "main")
	return r
}

func open(t *testing.T, r repo) *Index {
	t.Helper()
	ix, err := Open(filepath.Join(t.TempDir(), "chunks.db"), r.dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	return ix
}

func TestRelatedAndWhy(t *testing.T) {
	r := newRepo(t)
	r.commit("add a", map[string]string{"a.go": "1", "a_test.go": "1", "c.go": "1"})
	r.commit("fix a", map[string]string{"a.go": "2", "a_test.go": "2"})
	r.commit("tweak a", map[string]string{"a.go": "3", "a_test.go": "3", "c.go": "2"})
	r.commit("document a", map[string]string{"a.go": "4", "docs.md": "1"})
	bulk := map[string]string{"a.go": "5"}
	for i := 0; i < 50; i++ {
		bulk[fmt.Sprintf("gen/f%02d.go", i)] = "x"
	}
	r.commit("regenerate", bulk)
	r.commit("drop c", map[string]string{"c.go": ""})
	r.commit("handle empty input in b\n\nLonger explanation.\n\nCarya-Reason: callers pass nil when the cache is cold", map[string]string{"b.go": "1"})

	ix := open(t, r)
	if n, err := ix.Update(); err != nil || n != 7 {
		t.Fatalf("first update: n=%d err=%v", n, err)
	}

	related, total, err := ix.RelatedFiles("a.go", 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Fatalf("a.go was in 5 commits, got %d", total)
	}
	var got []string
	for _, rel := range related {
		got = append(got, fmt.Sprintf("%s:%d", rel.Path, rel.Together))
	}
	// c.go is gone from HEAD, and the bulk commit adds no pairs.
	if strings.Join(got, ",") != "a_test.go:3,docs.md:1" {
		t.Fatalf("related = %v", got)
	}
	if related[0].Share != 0.6 {
		t.Fatalf("share = %v, want 0.6", related[0].Share)
	}

	why, total, err := ix.Why("b.go", 5)
	if err != nil || total != 1 || len(why) != 1 {
		t.Fatalf("why b.go: %+v total=%d err=%v", why, total, err)
	}
	if why[0].Subject != "handle empty input in b" || why[0].Reason != "callers pass nil when the cache is cold" || why[0].Author != "Dev" {
		t.Fatalf("unexpected change: %+v", why[0])
	}
	recent, _, _ := ix.Why("a.go", 2)
	if len(recent) != 2 || recent[0].Subject != "regenerate" || recent[1].Subject != "document a" {
		t.Fatalf("why a.go should be newest first: %+v", recent)
	}

	line, err := ix.LineChange("b.go", 1)
	if err != nil || line.Subject != "handle empty input in b" {
		t.Fatalf("line change: %+v %v", line, err)
	}
}

func TestUpdateIsIncrementalAndRebuildsAfterBranchSwitch(t *testing.T) {
	r := newRepo(t)
	r.commit("one", map[string]string{"x.go": "1", "y.go": "1"})
	ix := open(t, r)
	if n, _ := ix.Update(); n != 1 {
		t.Fatalf("want 1 commit indexed, got %d", n)
	}
	if n, _ := ix.Update(); n != 0 {
		t.Fatalf("nothing new, got %d", n)
	}

	r.commit("two", map[string]string{"x.go": "2", "y.go": "2"})
	if n, err := ix.Update(); err != nil || n != 1 {
		t.Fatalf("incremental update: n=%d err=%v", n, err)
	}
	if rel, _, _ := ix.RelatedFiles("x.go", 5); len(rel) != 1 || rel[0].Together != 2 {
		t.Fatalf("pairs should accumulate: %+v", rel)
	}

	// An unrelated branch: the index starts over from its history.
	r.git("checkout", "-q", "--orphan", "other")
	r.git("rm", "-rq", "--cached", ".")
	os.Remove(filepath.Join(r.dir, "x.go"))
	os.Remove(filepath.Join(r.dir, "y.go"))
	r.commit("fresh", map[string]string{"z.go": "1", "w.go": "1"})
	if n, err := ix.Update(); err != nil || n != 1 {
		t.Fatalf("rebuild: n=%d err=%v", n, err)
	}
	if _, total, _ := ix.RelatedFiles("x.go", 5); total != 0 {
		t.Fatalf("old branch history should be gone, x.go total=%d", total)
	}
	if rel, _, _ := ix.RelatedFiles("z.go", 5); len(rel) != 1 || rel[0].Path != "w.go" {
		t.Fatalf("new history should be indexed: %+v", rel)
	}
}

func TestUpdateInEmptyRepo(t *testing.T) {
	ix := open(t, newRepo(t))
	if n, err := ix.Update(); err != nil || n != 0 {
		t.Fatalf("empty repo: n=%d err=%v", n, err)
	}
}
