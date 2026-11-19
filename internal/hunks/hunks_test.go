package hunks

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// TestMain keeps the developer's git configuration, and any GIT_* variables
// from a git process the tests run under (a hook, say), out of the tests.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "GIT_") {
			os.Unsetenv(name)
		}
	}
	dir, err := os.MkdirTemp("", "hunks-test-config-*")
	if err != nil {
		panic(err)
	}
	global := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(global, nil, 0o644); err != nil {
		panic(err)
	}
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL":   global,
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME":     "Test",
		"GIT_AUTHOR_EMAIL":    "test@example.com",
		"GIT_COMMITTER_NAME":  "Test",
		"GIT_COMMITTER_EMAIL": "test@example.com",
	} {
		os.Setenv(k, v)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type testRepo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *testRepo {
	t.Helper()
	r := &testRepo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	return r
}

// git runs git in the repository and returns its standard output.
func (r *testRepo) git(args ...string) string {
	r.t.Helper()
	out, err := r.tryGit(args...)
	if err != nil {
		r.t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return out
}

func (r *testRepo) tryGit(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return string(out), fmt.Errorf("%w: %s", err, stderr.String())
	}
	return string(out), nil
}

func (r *testRepo) write(name, content string) {
	r.t.Helper()
	p := filepath.Join(r.dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *testRepo) read(name string) string {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join(r.dir, name))
	if err != nil {
		r.t.Fatal(err)
	}
	return string(b)
}

func (r *testRepo) remove(name string) {
	r.t.Helper()
	if err := os.Remove(filepath.Join(r.dir, name)); err != nil {
		r.t.Fatal(err)
	}
}

func (r *testRepo) commitAll(message string) {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "-m", message)
}

func (r *testRepo) head() string {
	r.t.Helper()
	out, err := r.tryGit("rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// blob returns a file's content in a commit (or ":" for the index).
func (r *testRepo) blob(rev, name string) string {
	r.t.Helper()
	return r.git("cat-file", "blob", rev+":"+name)
}

func (r *testRepo) status() string {
	r.t.Helper()
	return r.git("status", "--porcelain", "--untracked-files=all")
}

func (r *testRepo) list() []Hunk {
	r.t.Helper()
	hs, err := List(r.dir)
	if err != nil {
		r.t.Fatalf("List: %v", err)
	}
	return hs
}

func (r *testRepo) commit(message string, refs ...string) string {
	r.t.Helper()
	out, _, err := Commit(r.dir, refs, message)
	if err != nil {
		r.t.Fatalf("Commit(%q): %v\n%s", refs, err, out)
	}
	return out
}

// numbered returns lines "line 1" to "line n".
func numbered(n int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	return lines
}

// edited returns numbered(n) with line i replaced by change[i] (an empty
// slice deletes it).
func edited(n int, change map[int][]string) string {
	var out []string
	for i, l := range numbered(n) {
		if c, ok := change[i+1]; ok {
			out = append(out, c...)
			continue
		}
		out = append(out, l)
	}
	return text(out)
}

func text(lines []string) string { return strings.Join(lines, "\n") + "\n" }

func ids(hs []Hunk) []string {
	var out []string
	for _, h := range hs {
		out = append(out, h.ID)
	}
	return out
}

func ofPath(hs []Hunk, path string) []Hunk {
	var out []Hunk
	for _, h := range hs {
		if h.Path == path {
			out = append(out, h)
		}
	}
	return out
}

func one(t *testing.T, hs []Hunk, path string) Hunk {
	t.Helper()
	got := ofPath(hs, path)
	if len(got) != 1 {
		t.Fatalf("want 1 hunk for %q, got %d: %+v", path, len(got), hs)
	}
	return got[0]
}

func skipOnWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs POSIX file names and modes")
	}
}

// Three separate hunks in f.txt: line 3 changed, two lines added after
// line 15, line 27 removed.
var threeEdits = map[int][]string{
	3:  {"changed 3"},
	15: {"line 15", "inserted a", "inserted b"},
	27: {},
}

func subset(keys ...int) map[int][]string {
	m := map[int][]string{}
	for _, k := range keys {
		m[k] = threeEdits[k]
	}
	return m
}

func threeHunkRepo(t *testing.T) *testRepo {
	r := newRepo(t)
	r.write("f.txt", edited(30, nil))
	r.commitAll("base")
	r.write("f.txt", edited(30, threeEdits))
	return r
}

func TestListSeveralHunksInOneFile(t *testing.T) {
	r := threeHunkRepo(t)
	hs := r.list()
	if len(hs) != 3 {
		t.Fatalf("want 3 hunks, got %d: %+v", len(hs), hs)
	}
	counts := [][2]int{{1, 1}, {2, 0}, {0, 1}}
	seen := map[string]bool{}
	for i, h := range hs {
		if h.Path != "f.txt" || h.Kind != "change" {
			t.Errorf("hunk %d: path %q kind %q", i, h.Path, h.Kind)
		}
		if h.Added != counts[i][0] || h.Removed != counts[i][1] {
			t.Errorf("hunk %d: +%d -%d, want +%d -%d", i, h.Added, h.Removed, counts[i][0], counts[i][1])
		}
		if !strings.HasPrefix(h.Header, "@@ -") || !strings.HasPrefix(h.Diff, h.Header+"\n") {
			t.Errorf("hunk %d: header %q, diff %q", i, h.Header, h.Diff)
		}
		if strings.Contains(h.Diff, "diff --git") {
			t.Errorf("hunk %d: diff has the file header: %q", i, h.Diff)
		}
		if h.ID == "" || seen[h.ID] {
			t.Errorf("hunk %d: ID %q empty or repeated", i, h.ID)
		}
		seen[h.ID] = true
	}
	// The hunks put back together are the diff git itself shows.
	if got, want := Patch(hs), r.git("diff", "--full-index", "HEAD"); got != want {
		t.Errorf("Patch(all) differs from git diff:\n%s\nwant:\n%s", got, want)
	}
	// Patch puts hunks back in file order whatever order it gets them in.
	if got := Patch([]Hunk{hs[2], hs[0], hs[1]}); got != Patch(hs) {
		t.Errorf("Patch of reordered hunks:\n%s", got)
	}
}

func TestListUntrackedFile(t *testing.T) {
	r := newRepo(t)
	r.write(".gitignore", "*.log\n")
	r.commitAll("base")
	r.write("new.txt", "a\nb\nc\n")
	r.write("dir/deeper.txt", "x\n")
	r.write("ignored.log", "noise\n")

	hs := r.list()
	if len(hs) != 2 {
		t.Fatalf("want 2 hunks (ignored file left out), got %+v", hs)
	}
	h := one(t, hs, "new.txt")
	if h.Kind != "add" || h.Added != 3 || h.Removed != 0 || h.Header != "" {
		t.Errorf("new.txt: %+v", h)
	}
	if !strings.Contains(h.Diff, "new file mode 100644") {
		t.Errorf("new.txt diff: %q", h.Diff)
	}
	one(t, hs, "dir/deeper.txt")
}

func TestListDeletedFile(t *testing.T) {
	r := newRepo(t)
	r.write("old.txt", "1\n2\n3\n4\n")
	r.write("keep.txt", "keep\n")
	r.commitAll("base")
	r.remove("old.txt")

	h := one(t, r.list(), "old.txt")
	if h.Kind != "delete" || h.Added != 0 || h.Removed != 4 {
		t.Errorf("old.txt: %+v", h)
	}
}

func TestListAndCommitBinaryFiles(t *testing.T) {
	r := newRepo(t)
	r.write("img.bin", "\x00\x01\x02binary\x00\n")
	r.commitAll("base")
	changed := "\x00\x01\x02changed\x00\xff\n"
	added := "\x89PNG\r\n\x1a\n\x00\x00"
	r.write("img.bin", changed)
	r.write("new.bin", added)

	hs := r.list()
	if h := one(t, hs, "img.bin"); h.Kind != "binary" || h.Added != 0 || h.Removed != 0 || !strings.Contains(h.Diff, "GIT binary patch") {
		t.Errorf("img.bin: %+v", h)
	}
	if h := one(t, hs, "new.bin"); h.Kind != "add" || !strings.Contains(h.Diff, "GIT binary patch") {
		t.Errorf("new.bin: %+v", h)
	}

	r.commit("binaries", "img.bin", "new.bin")
	if got := r.blob("HEAD", "img.bin"); got != changed {
		t.Errorf("committed img.bin = %q", got)
	}
	if got := r.blob("HEAD", "new.bin"); got != added {
		t.Errorf("committed new.bin = %q", got)
	}
	if s := r.status(); s != "" {
		t.Errorf("status after commit:\n%s", s)
	}
}

func TestListAndCommitModeChange(t *testing.T) {
	skipOnWindows(t)
	r := newRepo(t)
	r.write("run.sh", "echo hi\n")
	r.write("both.sh", "echo one\n")
	r.commitAll("base")
	for _, name := range []string{"run.sh", "both.sh"} {
		if err := os.Chmod(filepath.Join(r.dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	r.write("both.sh", "echo two\n")

	hs := r.list()
	h := one(t, hs, "run.sh")
	if h.Kind != "mode" || h.Added != 0 || h.Removed != 0 ||
		!strings.Contains(h.Diff, "old mode 100644\nnew mode 100755\n") {
		t.Errorf("run.sh: %+v", h)
	}
	if h := one(t, hs, "both.sh"); h.Kind != "change" || h.Added != 1 || h.Removed != 1 {
		t.Errorf("both.sh: %+v", h)
	}

	r.commit("chmod", h.ID)
	if got := r.git("ls-tree", "HEAD", "run.sh"); !strings.HasPrefix(got, "100755 ") {
		t.Errorf("run.sh in HEAD: %q", got)
	}
	if s := r.status(); s != " M both.sh\n" {
		t.Errorf("status after committing the mode change:\n%s", s)
	}
	r.commit("chmod and change", "both.sh")
	if got := r.git("ls-tree", "HEAD", "both.sh"); !strings.HasPrefix(got, "100755 ") {
		t.Errorf("both.sh in HEAD: %q", got)
	}
	if s := r.status(); s != "" {
		t.Errorf("status after commit:\n%s", s)
	}
}

func TestListAndCommitUnusualPaths(t *testing.T) {
	skipOnWindows(t)
	r := newRepo(t)
	tracked := []string{
		"with space.txt",
		"dir b/sub b/file.txt", // " b/" inside the path itself
		`quote"and\backslash.txt`,
		"ünïcødé ✓.txt",
	}
	untracked := []string{
		"new ünïcødé.txt",
		"tab\tand\nnewline.txt",
		`"quoted".txt`,
		"latin-1 caf\xe9.txt",   // not UTF-8
		"latin-1 \xe9\ttab.txt", // not UTF-8, and quoted
	}
	for _, name := range tracked {
		r.write(name, "old\n")
	}
	r.commitAll("base")
	for _, name := range tracked {
		r.write(name, "new\n")
	}
	for _, name := range untracked {
		r.write(name, "fresh\n")
	}

	hs := r.list()
	var got []string
	for _, h := range hs {
		got = append(got, h.Path)
	}
	want := append(append([]string(nil), tracked...), untracked...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("paths:\n got %q\nwant %q", got, want)
	}
	for _, name := range want {
		sel, err := Select(hs, []string{name})
		if err != nil || len(sel) != 1 || sel[0].Path != name {
			t.Errorf("Select(%q) = %+v, %v", name, sel, err)
		}
	}

	r.commit("unusual names", want...)
	if s := r.git("status", "--porcelain", "-z"); s != "" {
		t.Errorf("status after commit: %q", s)
	}
	for _, name := range tracked {
		if got := r.blob("HEAD", name); got != "new\n" {
			t.Errorf("%q in HEAD = %q", name, got)
		}
	}
}

func TestListAndCommitWithoutTrailingNewline(t *testing.T) {
	r := newRepo(t)
	base := numbered(20)
	r.write("a.txt", strings.Join(base, "\n")) // no newline at the end
	r.write("c.txt", "x")
	r.commitAll("base")

	work := numbered(20)
	work[1] = "changed 2"
	work[19] = "changed 20"
	r.write("a.txt", strings.Join(work, "\n"))
	r.write("b.txt", "only line")
	r.write("c.txt", "x\n")

	hs := r.list()
	a := ofPath(hs, "a.txt")
	if len(a) != 2 {
		t.Fatalf("a.txt: want 2 hunks, got %+v", a)
	}
	if !strings.Contains(a[1].Diff, "\\ No newline at end of file") || a[1].Added != 1 || a[1].Removed != 1 {
		t.Errorf("last hunk of a.txt: %+v", a[1])
	}
	if h := one(t, hs, "b.txt"); h.Kind != "add" || h.Added != 1 {
		t.Errorf("b.txt: %+v", h)
	}
	if h := one(t, hs, "c.txt"); h.Kind != "change" || h.Added != 1 || h.Removed != 1 {
		t.Errorf("c.txt: %+v", h)
	}

	// Commit only the hunk at the end of the file.
	r.commit("last line", a[1].ID)
	half := numbered(20)
	half[19] = "changed 20"
	if got, want := r.blob("HEAD", "a.txt"), strings.Join(half, "\n"); got != want {
		t.Errorf("a.txt in HEAD = %q, want %q", got, want)
	}
	if d := r.git("diff", "--", "a.txt"); !strings.Contains(d, "+changed 2") || strings.Contains(d, "changed 20") {
		t.Errorf("a.txt left uncommitted:\n%s", d)
	}

	r.commit("the rest", "a.txt", "b.txt", "c.txt")
	if got := r.blob("HEAD", "a.txt"); got != strings.Join(work, "\n") {
		t.Errorf("a.txt in HEAD = %q", got)
	}
	if got := r.blob("HEAD", "b.txt"); got != "only line" {
		t.Errorf("b.txt in HEAD = %q", got)
	}
	if got := r.blob("HEAD", "c.txt"); got != "x\n" {
		t.Errorf("c.txt in HEAD = %q", got)
	}
	if s := r.status(); s != "" {
		t.Errorf("status after commit:\n%s", s)
	}
}

// Lines whose content starts with "-- " or "++ " show up in a diff as
// "--- ..." and "+++ ...", like file headers, but they are changed lines.
func TestListCountsLinesThatLookLikeFileHeaders(t *testing.T) {
	r := newRepo(t)
	r.write("q.sql", "-- comment\nselect 1;\n++ counter\n")
	r.write("gone.sql", "-- gone\n")
	r.commitAll("base")
	r.write("q.sql", "-- new comment\nselect 1;\n++ x\n")
	r.write("n.sql", "-- a\n++ b\n")
	r.remove("gone.sql")

	hs := r.list()
	if h := one(t, hs, "q.sql"); h.Added != 2 || h.Removed != 2 {
		t.Errorf("q.sql: +%d -%d, want +2 -2", h.Added, h.Removed)
	}
	if h := one(t, hs, "n.sql"); h.Added != 2 || h.Removed != 0 {
		t.Errorf("n.sql: +%d -%d, want +2 -0", h.Added, h.Removed)
	}
	if h := one(t, hs, "gone.sql"); h.Added != 0 || h.Removed != 1 {
		t.Errorf("gone.sql: +%d -%d, want +0 -1", h.Added, h.Removed)
	}
}

func TestListTypeChange(t *testing.T) {
	skipOnWindows(t)
	r := newRepo(t)
	r.write("target.txt", "target\n")
	r.write("link", "a file for now\n")
	r.commitAll("base")
	r.remove("link")
	if err := os.Symlink("target.txt", filepath.Join(r.dir, "link")); err != nil {
		t.Fatal(err)
	}

	hs := ofPath(r.list(), "link")
	if len(hs) == 0 {
		t.Fatal("no hunks for link")
	}
	r.commit("make link a symlink", "link")
	if got := r.git("ls-tree", "HEAD", "link"); !strings.HasPrefix(got, "120000 ") {
		t.Errorf("link in HEAD: %q", got)
	}
	if s := r.status(); s != "" {
		t.Errorf("status after commit:\n%s", s)
	}
}

func TestIDsStayTheSameWhenOtherHunksAreCommitted(t *testing.T) {
	r := threeHunkRepo(t)
	r.write("g.txt", "other file\n")
	before := r.list()
	if len(before) != 4 {
		t.Fatalf("want 4 hunks, got %+v", before)
	}
	again := r.list()
	if strings.Join(ids(again), " ") != strings.Join(ids(before), " ") {
		t.Fatalf("IDs changed between two listings: %v then %v", ids(before), ids(again))
	}

	// Committing the middle hunk adds two lines above the last hunk, so its
	// @@ line changes; its ID must not.
	r.commit("middle", before[1].ID)
	after := r.list()
	if got, want := ids(after), []string{before[0].ID, before[2].ID, before[3].ID}; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("IDs after committing %s: %v, want %v", before[1].ID, got, want)
	}
	if after[1].Header == before[2].Header {
		t.Fatalf("test does not move the last hunk: header %q", after[1].Header)
	}

	r.commit("first", before[0].ID)
	if got, want := ids(r.list()), []string{before[2].ID, before[3].ID}; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("IDs after committing %s: %v, want %v", before[0].ID, got, want)
	}
}

func TestIDsDifferForTheSameEditInDifferentFiles(t *testing.T) {
	r := newRepo(t)
	r.write("a.txt", "same\n")
	r.write("b.txt", "same\n")
	r.commitAll("base")
	r.write("a.txt", "edit\n")
	r.write("b.txt", "edit\n")
	hs := r.list()
	if len(hs) != 2 || hs[0].ID == hs[1].ID {
		t.Fatalf("IDs: %v", ids(hs))
	}
}

// identicalHunksRepo has two hunks in f.txt that are the same, context
// included, followed by a different one.
func identicalHunksRepo(t *testing.T) *testRepo {
	block := []string{"alpha", "beta", "gamma", "TARGET", "delta", "epsilon", "zeta"}
	var lines []string
	lines = append(lines, "top")
	lines = append(lines, block...)
	lines = append(lines, "sep 1", "sep 2", "sep 3")
	lines = append(lines, block...)
	lines = append(lines, "fill 1", "fill 2", "fill 3", "fill 4", "fill 5", "other", "fill 6", "fill 7", "fill 8")
	r := newRepo(t)
	r.write("f.txt", text(lines))
	r.commitAll("base")
	content := strings.ReplaceAll(text(lines), "TARGET", "CHANGED")
	r.write("f.txt", strings.Replace(content, "other", "OTHER", 1))
	return r
}

func TestIDsUniqueForIdenticalHunks(t *testing.T) {
	r := identicalHunksRepo(t)
	hs := r.list()
	if len(hs) != 3 {
		t.Fatalf("want 3 hunks, got %+v", hs)
	}
	_, body0, _ := strings.Cut(hs[0].Diff, "\n")
	_, body1, _ := strings.Cut(hs[1].Diff, "\n")
	if body0 != body1 {
		t.Fatalf("test hunks are not identical:\n%s\n%s", hs[0].Diff, hs[1].Diff)
	}
	if hs[0].ID == hs[1].ID || hs[0].ID == hs[2].ID || hs[1].ID == hs[2].ID {
		t.Fatalf("IDs not unique: %v", ids(hs))
	}
	for _, h := range hs {
		sel, err := Select(hs, []string{h.ID})
		if err != nil || len(sel) != 1 || sel[0].Header != h.Header {
			t.Errorf("Select(%s) = %+v, %v", h.ID, sel, err)
		}
	}
	// An ID listed while one copy was unique now matches both: refuse it
	// rather than guess.
	digest, _, _ := strings.Cut(hs[0].ID, "@")
	if digest == hs[0].ID {
		t.Fatalf("identical hunks got plain IDs: %v", ids(hs))
	}
	if sel, err := Select(hs, []string{digest}); err == nil || !strings.Contains(err.Error(), hs[1].ID) {
		t.Errorf("Select(%s) = %v, %v; want an error naming both copies", digest, ids(sel), err)
	}
}

func TestIDsOfIdenticalHunksStayValidWhenOneIsCommitted(t *testing.T) {
	r := identicalHunksRepo(t)
	hs := r.list()
	first, second, other := hs[0], hs[1], hs[2]

	r.commit("first copy", first.ID)
	after := r.list()
	if len(after) != 2 {
		t.Fatalf("want 2 hunks left, got %+v", after)
	}
	// The second copy's ID from before still names it.
	sel, err := Select(after, []string{second.ID})
	if err != nil || len(sel) != 1 || sel[0].Diff != after[0].Diff {
		t.Fatalf("Select(%s) after committing %s = %+v, %v", second.ID, first.ID, sel, err)
	}
	if after[1].ID != other.ID {
		t.Errorf("other hunk's ID changed from %s to %s", other.ID, after[1].ID)
	}
	r.commit("second copy", second.ID)
	if got := r.blob("HEAD", "f.txt"); strings.Count(got, "CHANGED") != 2 || strings.Contains(got, "OTHER") {
		t.Errorf("f.txt in HEAD:\n%s", got)
	}
}

func TestSelect(t *testing.T) {
	r := newRepo(t)
	r.write("a.txt", edited(30, nil))
	r.write("b.txt", "b\n")
	r.write("same.txt", "unchanged\n")
	r.commitAll("base")
	r.write("a.txt", edited(30, subset(3, 27)))
	r.write("b.txt", "B\n")
	r.write("c.txt", "new\n")
	hs := r.list()
	if len(hs) != 4 {
		t.Fatalf("want 4 hunks, got %+v", hs)
	}
	a1, a2, b, c := hs[0], hs[1], hs[2], hs[3]

	check := func(refs []string, want ...Hunk) {
		t.Helper()
		got, err := Select(hs, refs)
		if err != nil {
			t.Errorf("Select(%q): %v", refs, err)
			return
		}
		if strings.Join(ids(got), " ") != strings.Join(ids(want), " ") {
			t.Errorf("Select(%q) = %v, want %v", refs, ids(got), ids(want))
		}
	}
	check([]string{a2.ID}, a2)
	check([]string{"a.txt"}, a1, a2)
	check([]string{"./a.txt"}, a1, a2)
	check([]string{"c.txt"}, c)
	// Mixed, out of order and repeated: the result is in list order, once each.
	check([]string{"c.txt", a2.ID, "b.txt", a2.ID, a1.ID, "a.txt"}, a1, a2, b, c)
	check(nil)

	for _, refs := range [][]string{
		{"deadbee"},
		{"nope.txt"},
		{"same.txt"}, // tracked but unchanged
		{a1.ID, "nope.txt"},
	} {
		got, err := Select(hs, refs)
		if err == nil {
			t.Errorf("Select(%q) = %v, want an error", refs, ids(got))
			continue
		}
		if bad := refs[len(refs)-1]; !strings.Contains(err.Error(), bad) {
			t.Errorf("Select(%q) error %q does not name %s", refs, err, bad)
		}
	}
}

func TestCommitSubsetOfOneFile(t *testing.T) {
	r := threeHunkRepo(t)
	work := r.read("f.txt")
	hs := r.list()

	out := r.commit("first and last", hs[0].ID, hs[2].ID)
	if !strings.Contains(out, "first and last") {
		t.Errorf("git output: %q", out)
	}
	if got := strings.TrimSpace(r.git("log", "-1", "--format=%s")); got != "first and last" {
		t.Errorf("commit message %q", got)
	}
	if got, want := r.blob("HEAD", "f.txt"), edited(30, subset(3, 27)); got != want {
		t.Errorf("f.txt in HEAD:\n%s\nwant:\n%s", got, want)
	}
	shown := r.git("show", "--format=", "HEAD")
	if !strings.Contains(shown, "+changed 3") || !strings.Contains(shown, "-line 27") || strings.Contains(shown, "inserted") {
		t.Errorf("git show HEAD:\n%s", shown)
	}
	diff := r.git("diff")
	if !strings.Contains(diff, "+inserted a") || strings.Contains(diff, "changed 3") || strings.Contains(diff, "line 27") {
		t.Errorf("git diff after commit:\n%s", diff)
	}
	if staged := r.git("diff", "--cached"); staged != "" {
		t.Errorf("index differs from HEAD:\n%s", staged)
	}
	if r.read("f.txt") != work {
		t.Error("Commit changed the working tree")
	}
	if got := ids(r.list()); len(got) != 1 || got[0] != hs[1].ID {
		t.Errorf("left uncommitted: %v, want [%s]", got, hs[1].ID)
	}
}

func TestCommitNewFile(t *testing.T) {
	r := newRepo(t)
	r.write("old.txt", "old\n")
	r.commitAll("base")
	r.write("new.txt", "brand new\n")
	r.write("old.txt", "changed\n")

	r.commit("add new.txt", "new.txt")
	if got := r.blob("HEAD", "new.txt"); got != "brand new\n" {
		t.Errorf("new.txt in HEAD = %q", got)
	}
	if got := r.git("show", "--name-only", "--format=", "HEAD"); got != "new.txt\n" {
		t.Errorf("commit touched %q", got)
	}
	if s := r.status(); s != " M old.txt\n" {
		t.Errorf("status:\n%s", s)
	}
	// The index's stat data is fresh, so plumbing agrees with git status.
	if got := r.git("diff-files", "--name-only"); got != "old.txt\n" {
		t.Errorf("diff-files: %q", got)
	}
}

func TestCommitDeletion(t *testing.T) {
	r := newRepo(t)
	r.write("gone.txt", "bye\n")
	r.write("old.txt", "old\n")
	r.commitAll("base")
	r.remove("gone.txt")
	r.write("old.txt", "changed\n")

	r.commit("remove gone.txt", "gone.txt")
	if got := r.git("ls-tree", "--name-only", "HEAD"); got != "old.txt\n" {
		t.Errorf("HEAD has %q", got)
	}
	if s := r.status(); s != " M old.txt\n" {
		t.Errorf("status:\n%s", s)
	}
}

func TestCommitKeepsOtherStagedChanges(t *testing.T) {
	r := newRepo(t)
	r.write("f.txt", "f\n")
	r.write("staged.txt", "staged\n")
	r.write("gone.txt", "gone\n")
	r.commitAll("base")
	r.write("staged.txt", "staged change\n")
	r.write("staged-new.txt", "staged new\n")
	r.git("add", "staged.txt", "staged-new.txt")
	r.git("rm", "-q", "gone.txt")
	stagedBefore := r.git("diff", "--cached")
	r.write("f.txt", "agent change\n")

	r.commit("agent work", "f.txt")
	if got := r.git("show", "--name-only", "--format=", "HEAD"); got != "f.txt\n" {
		t.Errorf("commit touched %q", got)
	}
	if got := r.git("diff", "--cached"); got != stagedBefore {
		t.Errorf("staged changes after commit:\n%s\nwant:\n%s", got, stagedBefore)
	}
	if s := r.status(); s != "D  gone.txt\nA  staged-new.txt\nM  staged.txt\n" {
		t.Errorf("status:\n%s", s)
	}
}

func TestCommitKeepsStagedHunkOfTheSameFile(t *testing.T) {
	t.Run("other hunk staged", func(t *testing.T) {
		r := threeHunkRepo(t)
		hs := r.list()
		// Stage the first hunk, as `git add -p` would.
		cmd := exec.Command("git", "apply", "--cached", "-")
		cmd.Dir = r.dir
		cmd.Stdin = strings.NewReader(Patch(hs[:1]))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("stage first hunk: %v\n%s", err, out)
		}

		r.commit("last hunk", hs[2].ID)
		if got, want := r.blob("HEAD", "f.txt"), edited(30, subset(27)); got != want {
			t.Errorf("f.txt in HEAD:\n%s", got)
		}
		// The index still has the staged hunk, and has the committed one too
		// rather than undoing it.
		if got, want := r.blob("", "f.txt"), edited(30, subset(3, 27)); got != want {
			t.Errorf("f.txt in the index:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("whole file staged", func(t *testing.T) {
		r := threeHunkRepo(t)
		hs := r.list()
		r.git("add", "f.txt")

		r.commit("middle hunk", hs[1].ID)
		if got := r.blob("", "f.txt"); got != r.read("f.txt") {
			t.Errorf("f.txt in the index:\n%s", got)
		}
		if s := r.status(); s != "M  f.txt\n" {
			t.Errorf("status:\n%s", s)
		}
	})
	t.Run("staged version overlaps", func(t *testing.T) {
		r := threeHunkRepo(t)
		r.write("f.txt", edited(30, map[int][]string{3: {"staged 3"}}))
		r.git("add", "f.txt")
		staged := r.blob("", "f.txt")
		r.write("f.txt", edited(30, threeEdits))
		hs := r.list()

		// The staged line 3 conflicts with the committed one: leave the
		// user's staged version as it is.
		r.commit("first hunk", hs[0].ID)
		if got := r.blob("", "f.txt"); got != staged {
			t.Errorf("f.txt in the index:\n%s\nwant:\n%s", got, staged)
		}
	})
}

// A file named like a glob must not be treated as a pathspec that also
// matches the user's staged files.
func TestCommitPathThatLooksLikeAGlob(t *testing.T) {
	skipOnWindows(t)
	r := newRepo(t)
	r.write("a.txt", "a\n")
	r.write("*.txt", "star\n")
	r.commitAll("base")
	r.write("a.txt", "a staged\n")
	r.git("add", "a.txt")
	r.write("*.txt", "star changed\n")

	r.commit("star", "*.txt")
	if got := r.blob("HEAD", "*.txt"); got != "star changed\n" {
		t.Errorf("*.txt in HEAD = %q", got)
	}
	if got := r.git("diff", "--cached", "--name-only"); got != "a.txt\n" {
		t.Errorf("staged after commit: %q, want only a.txt", got)
	}
	if got := r.git("diff", "--name-only"); got != "" {
		t.Errorf("unstaged after commit: %q", got)
	}
}

// apply.whitespace=fix would strip the trailing blanks from what is
// committed, and =error would refuse the hunk.
func TestCommitIgnoresApplyWhitespaceConfig(t *testing.T) {
	for _, setting := range []string{"fix", "error"} {
		t.Run(setting, func(t *testing.T) {
			r := newRepo(t)
			r.write("f.txt", "a\n")
			r.commitAll("base")
			r.git("config", "apply.whitespace", setting)
			work := "a  \nb\t\n"
			r.write("f.txt", work)

			r.commit("trailing blanks", "f.txt")
			if got := r.blob("HEAD", "f.txt"); got != work {
				t.Errorf("f.txt in HEAD = %q, want %q", got, work)
			}
			if s := r.status(); s != "" {
				t.Errorf("status:\n%s", s)
			}
		})
	}
}

func TestCommitRunsPreCommitHook(t *testing.T) {
	skipOnWindows(t)
	hook := func(r *testRepo, script string) {
		hooks := filepath.Join(r.dir, strings.TrimSpace(r.git("rev-parse", "--git-path", "hooks")))
		if err := os.MkdirAll(hooks, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("passing", func(t *testing.T) {
		r := threeHunkRepo(t)
		r.write("other.txt", "not committed\n")
		hook(r, `git diff --cached --name-only > "$(git rev-parse --git-dir)/hook-saw"`+"\n")
		hs := r.list()

		r.commit("with hook", hs[0].ID)
		saw, err := os.ReadFile(filepath.Join(r.dir, ".git", "hook-saw"))
		if err != nil {
			t.Fatalf("hook did not run: %v", err)
		}
		// The hook sees what is being committed, not the rest of the work.
		if string(saw) != "f.txt\n" {
			t.Errorf("hook saw %q staged, want only f.txt", saw)
		}
	})

	t.Run("failing", func(t *testing.T) {
		r := threeHunkRepo(t)
		hook(r, "echo 'hook says no' >&2\nexit 1\n")
		head, status, work := r.head(), r.status(), r.read("f.txt")
		hs := r.list()

		out, _, err := Commit(r.dir, []string{hs[0].ID}, "blocked")
		if err == nil {
			t.Fatalf("Commit succeeded despite the failing hook:\n%s", out)
		}
		if !strings.Contains(err.Error()+out, "hook says no") {
			t.Errorf("error does not carry the hook's output: %v\n%s", err, out)
		}
		if r.head() != head {
			t.Error("HEAD moved")
		}
		if r.status() != status || r.read("f.txt") != work {
			t.Error("index or working tree changed")
		}
		if got := ids(r.list()); strings.Join(got, " ") != strings.Join(ids(hs), " ") {
			t.Errorf("hunks after failed commit: %v, want %v", got, ids(hs))
		}
	})
}

func TestCommitInRepositoryWithoutCommits(t *testing.T) {
	r := newRepo(t)
	r.write("a.txt", "a\n")
	r.write("b.txt", "b\n")
	r.write("c.txt", "c\n")
	r.git("add", "c.txt")

	hs := r.list()
	if len(hs) != 3 {
		t.Fatalf("want 3 hunks, got %+v", hs)
	}
	for _, h := range hs {
		if h.Kind != "add" {
			t.Errorf("%s: kind %q", h.Path, h.Kind)
		}
	}

	r.commit("first", "a.txt")
	if got := r.git("ls-tree", "--name-only", "HEAD"); got != "a.txt\n" {
		t.Errorf("first commit has %q", got)
	}
	if s := r.status(); s != "A  c.txt\n?? b.txt\n" {
		t.Errorf("status:\n%s", s)
	}

	r.commit("second", "b.txt")
	if got := r.git("rev-list", "--count", "HEAD"); got != "2\n" {
		t.Errorf("%s commits", strings.TrimSpace(got))
	}
	if s := r.status(); s != "A  c.txt\n" {
		t.Errorf("status:\n%s", s)
	}
}

// The empty tree has a different ID under SHA-256.
func TestCommitInSHA256RepositoryWithoutCommits(t *testing.T) {
	r := &testRepo{t: t, dir: t.TempDir()}
	if _, err := r.tryGit("init", "-q", "-b", "main", "--object-format=sha256"); err != nil {
		t.Skipf("git cannot make SHA-256 repositories: %v", err)
	}
	r.write("a.txt", "a\n")
	r.write("b.txt", "b\n")

	if hs := r.list(); len(hs) != 2 || hs[0].Kind != "add" || hs[1].Kind != "add" {
		t.Fatalf("hunks: %+v", hs)
	}
	r.commit("first", "a.txt")
	if got := r.git("ls-tree", "--name-only", "HEAD"); got != "a.txt\n" {
		t.Errorf("first commit has %q", got)
	}
	if s := r.status(); s != "?? b.txt\n" {
		t.Errorf("status:\n%s", s)
	}
}

func TestCommitRefusals(t *testing.T) {
	r := threeHunkRepo(t)
	hs := r.list()
	head := r.head()
	for name, c := range map[string]struct {
		refs    []string
		message string
	}{
		"empty message":      {[]string{hs[0].ID}, ""},
		"blank message":      {[]string{hs[0].ID}, " \n\t"},
		"no hunks named":     {nil, "message"},
		"unknown hunk":       {[]string{"deadbee"}, "message"},
		"one unknown of two": {[]string{hs[0].ID, "missing.txt"}, "message"},
	} {
		if _, _, err := Commit(r.dir, c.refs, c.message); err == nil {
			t.Errorf("%s: Commit succeeded", name)
		}
	}
	if r.head() != head {
		t.Error("HEAD moved")
	}

	clean := newRepo(t)
	clean.write("a.txt", "a\n")
	clean.commitAll("base")
	if _, _, err := Commit(clean.dir, []string{"a.txt"}, "nothing"); err == nil {
		t.Error("Commit with nothing to commit succeeded")
	}
}
