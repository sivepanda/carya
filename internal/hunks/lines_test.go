package hunks

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// changes returns the added and removed lines of hunks, as "+x" and "-y".
func changes(hs ...Hunk) []string {
	var out []string
	for _, h := range hs {
		_, body, ok := splitBody(h.Diff)
		if !ok {
			continue
		}
		for _, b := range body {
			if op := b.op(); op == '+' || op == '-' {
				out = append(out, strings.TrimSuffix(b.text, "\n"))
			}
		}
	}
	return out
}

// lineOf returns the number of the line of h that reads text, "+x" say.
func lineOf(t *testing.T, h Hunk, text string) int {
	t.Helper()
	for _, l := range h.Lines() {
		if l.N > 0 && l.Text == text {
			return l.N
		}
	}
	t.Fatalf("no line %q in hunk %s:\n%s", text, h.ID, h.Diff)
	return 0
}

func sameLines(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n got %q\nwant %q", what, got, want)
	}
}

func prefixed(prefix string, lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = prefix + l
	}
	return out
}

// The hunk partHunkRepo leaves, numbered:
//
//	1  line 5      6  line 9      11  line 12
//	2  line 6      7 +ins a       12  line 13
//	3  line 7      8 +ins b       13  line 14
//	4 -line 8      9  line 10
//	5 +changed 8  10 -line 11
var partEdits = map[int][]string{8: {"changed 8"}, 9: {"line 9", "ins a", "ins b"}, 11: {}}

func partHunkRepo(t *testing.T) (*testRepo, Hunk) {
	r := newRepo(t)
	r.write("f.txt", edited(20, nil))
	r.commitAll("base")
	r.write("f.txt", edited(20, partEdits))
	return r, one(t, r.list(), "f.txt")
}

func TestLinesNumberTheBody(t *testing.T) {
	r, h := partHunkRepo(t)
	lines := h.Lines()
	if lines[0].N != 0 || lines[0].Text != h.Header {
		t.Errorf("first line %+v, want the unnumbered @@ line", lines[0])
	}
	for i, l := range lines[1:] {
		if l.N != i+1 {
			t.Errorf("line %q numbered %d, want %d", l.Text, l.N, i+1)
		}
	}
	if len(lines) != 14 || lines[4].Text != "-line 8" || lines[13].Text != " line 14" {
		t.Errorf("lines: %+v", lines)
	}

	// A whole file's diff header and "\ No newline" markers are not numbered.
	r.write("new.txt", "a\nb")
	add := one(t, r.list(), "new.txt")
	var got []string
	for _, l := range add.Lines() {
		got = append(got, fmt.Sprintf("%d %s", l.N, l.Text))
	}
	want := []string{
		"0 diff --git a/new.txt b/new.txt",
		"0 new file mode 100644",
		"0 index " + strings.Repeat("0", 40) + ".." + strings.TrimSpace(r.git("hash-object", "new.txt")),
		"0 --- /dev/null",
		"0 +++ b/new.txt",
		"0 @@ -0,0 +1,2 @@",
		"1 +a",
		"2 +b",
		`0 \ No newline at end of file`,
	}
	sameLines(t, "numbered new file", got, want)

	r.write("bin.dat", "\x00\x01")
	for _, l := range one(t, r.list(), "bin.dat").Lines() {
		if l.N != 0 {
			t.Errorf("binary file has numbered line %+v", l)
		}
	}
}

func TestCommitPartOfAChange(t *testing.T) {
	r, h := partHunkRepo(t)
	work := r.read("f.txt")
	ref := fmt.Sprintf("%s:%d,%d", h.ID, lineOf(t, h, "+ins a"), lineOf(t, h, "-line 11"))

	r.commit("part", ref)
	// "ins a" is added and line 11 removed; line 8's change and "ins b" are not.
	if got, want := r.blob("HEAD", "f.txt"), edited(20, map[int][]string{9: {"line 9", "ins a"}, 11: {}}); got != want {
		t.Errorf("f.txt in HEAD:\n%s\nwant:\n%s", got, want)
	}
	if r.read("f.txt") != work {
		t.Error("Commit changed the working tree")
	}
	if staged := r.git("diff", "--cached"); staged != "" {
		t.Errorf("index differs from HEAD:\n%s", staged)
	}
	rest := r.list()
	if len(rest) != 1 || rest[0].ID == h.ID {
		t.Fatalf("left uncommitted: %+v, want one hunk with a new ID", rest)
	}
	sameLines(t, "left uncommitted", changes(rest...), []string{"-line 8", "+changed 8", "+ins b"})

	r.commit("the rest", rest[0].ID)
	if got := r.blob("HEAD", "f.txt"); got != work {
		t.Errorf("f.txt in HEAD after committing the rest:\n%s", got)
	}
	if s := r.status(); s != "" {
		t.Errorf("status:\n%s", s)
	}
}

func TestCommitPartWithGapsAndContext(t *testing.T) {
	r, h := partHunkRepo(t)
	// Lines 1-3 and 11-13 are context, which ranges may cover.
	r.commit("part", h.ID+":1-4,7,10-13")
	if got, want := r.blob("HEAD", "f.txt"), edited(20, map[int][]string{8: {}, 9: {"line 9", "ins a"}, 11: {}}); got != want {
		t.Errorf("f.txt in HEAD:\n%s\nwant:\n%s", got, want)
	}
	sameLines(t, "left uncommitted", changes(r.list()...), []string{"+changed 8", "+ins b"})
}

func TestCommitPartOfANewFile(t *testing.T) {
	t.Run("first lines", func(t *testing.T) {
		r := newRepo(t)
		r.write("old.txt", "old\n")
		r.commitAll("base")
		r.write("new.txt", text(numbered(60)))
		h := one(t, r.list(), "new.txt")

		r.commit("start new.txt", h.ID+":1-40")
		if got := r.blob("HEAD", "new.txt"); got != text(numbered(40)) {
			t.Errorf("new.txt in HEAD:\n%s", got)
		}
		if got := r.git("show", "--name-only", "--format=", "HEAD"); got != "new.txt\n" {
			t.Errorf("commit touched %q", got)
		}
		if s := r.status(); s != " M new.txt\n" {
			t.Errorf("status:\n%s", s)
		}
		rest := r.list()
		if len(rest) != 1 || rest[0].Kind != "change" || rest[0].ID == h.ID {
			t.Fatalf("left uncommitted: %+v", rest)
		}
		sameLines(t, "left uncommitted", changes(rest...), prefixed("+", numbered(60)[40:]))
	})
	t.Run("lines with gaps", func(t *testing.T) {
		r := newRepo(t)
		r.write("new.txt", text(numbered(60)))
		h := one(t, r.list(), "new.txt")

		r.commit("some of new.txt", h.ID+":10-20,30")
		want := append(numbered(20)[9:], "line 30")
		if got := r.blob("HEAD", "new.txt"); got != text(want) {
			t.Errorf("new.txt in HEAD:\n%s", got)
		}
		var left []string
		for i, l := range numbered(60) {
			if n := i + 1; n < 10 || (n > 20 && n != 30) {
				left = append(left, "+"+l)
			}
		}
		sameLines(t, "left uncommitted", changes(r.list()...), left)
	})
}

func TestCommitPartOfADeletion(t *testing.T) {
	for _, name := range []string{"old.txt", `quote"d name.txt`} {
		t.Run(name, func(t *testing.T) {
			if strings.Contains(name, `"`) {
				skipOnWindows(t)
			}
			r := newRepo(t)
			r.write(name, text(numbered(10)))
			r.write("keep.txt", "keep\n")
			r.commitAll("base")
			r.remove(name)
			h := one(t, r.list(), name)

			r.commit("part of the deletion", h.ID+":3-5")
			kept := append(numbered(2), numbered(10)[5:]...)
			if got := r.blob("HEAD", name); got != text(kept) {
				t.Errorf("%s in HEAD:\n%s", name, got)
			}
			if s := r.git("status", "--porcelain", "-z"); s != " D "+name+"\x00" {
				t.Errorf("status %q", s)
			}
			rest := one(t, r.list(), name)
			if rest.Kind != "delete" || rest.ID == h.ID {
				t.Errorf("left uncommitted: %+v", rest)
			}
			sameLines(t, "left uncommitted", changes(rest), prefixed("-", kept))

			r.commit("the rest", rest.ID)
			if got := r.git("ls-tree", "--name-only", "HEAD"); got != "keep.txt\n" {
				t.Errorf("HEAD has %q", got)
			}
		})
	}
}

func TestCommitPartWithOtherHunks(t *testing.T) {
	r := threeHunkRepo(t)
	r.write("g.txt", "other file\n")
	r.write("h.txt", "not committed\n")
	hs := r.list()
	f := ofPath(hs, "f.txt")
	g, other := one(t, hs, "g.txt"), one(t, hs, "h.txt")

	ref := fmt.Sprintf("%s:%d", f[1].ID, lineOf(t, f[1], "+inserted b"))
	r.commit("mixed", f[0].ID, ref, "g.txt")
	if got, want := r.blob("HEAD", "f.txt"), edited(30, map[int][]string{3: {"changed 3"}, 15: {"line 15", "inserted b"}}); got != want {
		t.Errorf("f.txt in HEAD:\n%s\nwant:\n%s", got, want)
	}
	if got := r.git("show", "--name-only", "--format=", "HEAD"); got != "f.txt\ng.txt\n" {
		t.Errorf("commit touched %q", got)
	}
	rest := r.list()
	if len(rest) != 3 {
		t.Fatalf("left uncommitted: %+v", rest)
	}
	// The rest of the split hunk has a new ID; the others keep theirs.
	if rest[0].ID == f[1].ID || rest[1].ID != f[2].ID || rest[2].ID != other.ID {
		t.Errorf("IDs left: %v; before: %v", ids(rest), ids([]Hunk{f[0], f[1], f[2], g, other}))
	}
	sameLines(t, "left of the split hunk", changes(rest[0]), []string{"+inserted a"})
}

func TestSelectLineRanges(t *testing.T) {
	r, h := partHunkRepo(t)
	r.write("g.txt", "g\n")
	hs := r.list()
	sel := func(refs ...string) []Hunk {
		t.Helper()
		got, err := Select(hs, refs)
		if err != nil {
			t.Fatalf("Select(%q): %v", refs, err)
		}
		return got
	}

	p := sel(h.ID + ":10,7,1-2,2-4")
	if len(p) != 1 || p[0].ID != h.ID+":1-4,7,10" {
		t.Fatalf("Select = %v, want one hunk %s:1-4,7,10", ids(p), h.ID)
	}
	_, context, _ := strings.Cut(strings.TrimPrefix(h.Header, "@@"), "@@") // git's function context
	header := "@@ -5,10 +5,9 @@" + context
	if p[0].Added != 1 || p[0].Removed != 2 || p[0].Header != header || p[0].Path != "f.txt" {
		t.Errorf("part: %+v, want header %q", p[0], header)
	}
	// The part keeps the whole hunk's numbers: the dropped added lines leave
	// gaps, and the removal not chosen is context under its own number.
	var got []string
	for _, l := range p[0].Lines() {
		got = append(got, fmt.Sprintf("%d %s", l.N, l.Text))
	}
	sameLines(t, "numbered part", got, []string{
		"0 " + header,
		"1  line 5", "2  line 6", "3  line 7", "4 -line 8", "6  line 9", "7 +ins a",
		"9  line 10", "10 -line 11", "11  line 12", "12  line 13", "13  line 14",
	})

	// Two refs to one hunk add up; a whole hunk wins over part of it.
	if p := sel(h.ID+":4", h.ID+":7"); len(p) != 1 || p[0].ID != h.ID+":4,7" {
		t.Errorf("two parts: %v", ids(p))
	}
	if p := sel(h.ID+":4", h.ID, "g.txt"); len(p) != 2 || p[0].ID != h.ID || p[0].Diff != h.Diff {
		t.Errorf("part and whole: %v", ids(p))
	}
	// Every changed line is the whole hunk.
	if p := sel(h.ID + ":4-5,7-8,10"); len(p) != 1 || p[0].ID != h.ID || p[0].Diff != h.Diff {
		t.Errorf("all changed lines: %+v", p)
	}
	if got := Patch(sel(h.ID + ":1-13")); got != Patch([]Hunk{h}) {
		t.Errorf("all lines patch:\n%s", got)
	}
}

func TestSelectLineRangesOfIdenticalHunks(t *testing.T) {
	r := identicalHunksRepo(t)
	hs := r.list()
	n := lineOf(t, hs[1], "+CHANGED")
	got, err := Select(hs, []string{fmt.Sprintf("%s:%d", hs[1].ID, n)})
	if err != nil || len(got) != 1 || got[0].line != hs[1].line {
		t.Fatalf("Select(%s:%d) = %+v, %v", hs[1].ID, n, got, err)
	}
	digest, _, _ := strings.Cut(hs[0].ID, "@")
	if _, err := Select(hs, []string{digest + ":1"}); err == nil || !strings.Contains(err.Error(), "identical hunks") || !strings.Contains(err.Error(), hs[1].ID) {
		t.Errorf("Select(%s:1) of a digest two hunks share: %v", digest, err)
	}
}

func TestSelectLineRangeErrors(t *testing.T) {
	skipOnWindows(t)
	r, h := partHunkRepo(t)
	r.write("bin.dat", "\x00\x01")
	r.write("mode.txt", "m\n")
	r.git("add", "mode.txt")
	r.git("commit", "-q", "-m", "mode.txt")
	if err := os.Chmod(filepath.Join(r.dir, "mode.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.write("empty.txt", "")
	hs := r.list()
	bin, mode, empty := one(t, hs, "bin.dat"), one(t, hs, "mode.txt"), one(t, hs, "empty.txt")
	head := r.head()

	for ref, want := range map[string]string{
		h.ID + ":":                     "no lines after the colon",
		h.ID + ":x":                    `"x" is not a line number or range`,
		h.ID + ":1-":                   `"1-" is not a line number or range`,
		h.ID + ":-3":                   `"-3" is not a line number or range`,
		h.ID + ":1--2":                 `"1--2" is not a line number or range`,
		h.ID + ":+3":                   `"+3" is not a line number or range`,
		h.ID + ": 3":                   `" 3" is not a line number or range`,
		h.ID + ":4,,7":                 "empty range between commas",
		h.ID + ":0":                    "numbered from 1",
		h.ID + ":0-4":                  "numbered from 1",
		h.ID + ":7-4":                  "7-4 runs backwards",
		h.ID + ":14":                   "hunk " + h.ID + " has lines 1-13",
		h.ID + ":4-99":                 "4-99 is past the end",
		h.ID + ":99999999999999999999": "past the end",
		h.ID + ":1-3,11-13":            h.ID + ":1-3,11-13 selects no added or removed line",
		"f.txt:4":                      "line ranges go after a hunk ID, not a file name; f.txt has " + h.ID,
		"deadbee:4":                    "no hunk or changed file matches deadbee",
		bin.ID + ":1":                  "is a binary or empty file",
		mode.ID + ":1":                 "is a mode change",
		empty.ID + ":1":                "is a binary or empty file",
	} {
		if _, err := Select(hs, []string{ref}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Select(%q): %v; want an error with %q", ref, err, want)
		}
		if _, _, err := Commit(r.dir, []string{"bin.dat", ref}, "bad"); err == nil {
			t.Errorf("Commit(%q) succeeded", ref)
		}
	}
	if r.head() != head {
		t.Error("HEAD moved")
	}
	// Every problem is reported at once.
	_, err := Select(hs, []string{h.ID + ":0", h.ID + ":1-3", "nope:1"})
	for _, want := range []string{"numbered from 1", "only context", "nope:1"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error %v does not mention %q", err, want)
		}
	}
}

// A file without a newline at its end has its last line marked "\ No
// newline at end of file" in a diff; parts of such hunks keep the file
// well formed.
func TestCommitPartWithoutTrailingNewline(t *testing.T) {
	for _, c := range []struct {
		name, base, work, lines, want string
	}{
		// Adding after a last line that has no newline gives it one:
		//   1  a  2  b  3 -last  4 +last  5 +new1  6 +new2
		{"add after it", "a\nb\nlast", "a\nb\nlast\nnew1\nnew2", "5", "a\nb\nlast\nnew1\n"},
		{"last added line", "a\nb\nlast", "a\nb\nlast\nnew1\nnew2", "6", "a\nb\nlast\nnew2"},
		{"only the newline", "a\nb\nlast", "a\nb\nlast\nnew1\nnew2", "3-4", "a\nb\nlast\n"},
		{"remove it", "a\nb\nlast", "a\nb\nlast\nnew1\nnew2", "3", "a\nb\n"},
		// A last line whose removal is not chosen stays as it is:
		//   1 -p  2 +P  3  q  4 -r
		{"removal left out", "p\nq\nr", "P\nq\n", "1-2", "P\nq\nr"},
		{"removal chosen", "p\nq\nr", "P\nq\n", "4", "p\nq\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newRepo(t)
			r.write("f.txt", c.base)
			r.commitAll("base")
			r.write("f.txt", c.work)
			h := one(t, r.list(), "f.txt")

			r.commit("part", h.ID+":"+c.lines)
			if got := r.blob("HEAD", "f.txt"); got != c.want {
				t.Errorf("f.txt in HEAD = %q, want %q", got, c.want)
			}
			r.commit("the rest", "f.txt")
			if got := r.blob("HEAD", "f.txt"); got != c.work {
				t.Errorf("f.txt in HEAD after the rest = %q, want %q", got, c.work)
			}
		})
	}
}

// For every choice of changed lines in a few hunks, the part applies, and
// to what the selection rules say: context and removed lines not chosen
// stay, added lines stay only when chosen, and every line but the last
// ends in a newline.
func TestEveryPartAppliesAsTheRulesSay(t *testing.T) {
	for _, c := range []struct {
		name, base, work string
		added, deleted   bool
	}{
		{name: "both without newline", base: "a\nb\nc\nd", work: "a\nB\nc\nd\ne"},
		{name: "newline added", base: "a\nb", work: "a\nb\nc\n"},
		{name: "newline removed", base: "p\nq\nr\n", work: "p\nQ\nr"},
		{name: "last line removed", base: "p\nq\nr", work: "P\nq\n"},
		{name: "emptied", base: "x\ny\n", work: ""},
		{name: "new file", work: "x\ny\nz", added: true},
		{name: "deleted file", base: "x\ny\nz\n", deleted: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newRepo(t)
			r.write("keep.txt", "keep\n")
			if !c.added {
				r.write("f.txt", c.base)
			}
			r.commitAll("base")
			if c.deleted {
				r.remove("f.txt")
			} else {
				r.write("f.txt", c.work)
			}
			h := one(t, r.list(), "f.txt")
			_, body, ok := splitBody(h.Diff)
			if !ok {
				t.Fatalf("no body: %q", h.Diff)
			}
			var changed []int
			for _, b := range body {
				if b.op() != ' ' {
					changed = append(changed, b.n)
				}
			}
			for mask := 1; mask < 1<<len(changed); mask++ {
				keep := map[int]bool{}
				var nums []string
				for i, n := range changed {
					if mask&(1<<i) != 0 {
						keep[n] = true
						nums = append(nums, fmt.Sprint(n))
					}
				}
				ref := h.ID + ":" + strings.Join(nums, ",")
				sel, err := Select([]Hunk{h}, []string{ref})
				if err != nil {
					t.Errorf("Select(%s): %v", ref, err)
					continue
				}
				got, exists, err := r.applied(Patch(sel), "f.txt")
				if err != nil {
					t.Errorf("%s: %v\n%s", ref, err, Patch(sel))
					continue
				}
				want, wantExists := ruled(body, keep, h.Kind == "delete" && len(keep) == len(changed))
				if got != want || exists != wantExists {
					t.Errorf("%s gives %q (exists %v), want %q (exists %v)\n%s", ref, got, exists, want, wantExists, Patch(sel))
				}
			}
		})
	}
}

// ruled is what the selection rules make of a hunk's body with the changed
// lines in keep, worked out from the lines rather than from a diff.
func ruled(body []bodyLine, keep map[int]bool, deleted bool) (string, bool) {
	if deleted {
		return "", false
	}
	type line struct {
		text    string
		newline bool
	}
	var out []line
	for _, b := range body {
		op, content := b.op(), strings.TrimSuffix(b.text[1:], "\n")
		if op == ' ' || (op == '-' && !keep[b.n]) || (op == '+' && keep[b.n]) {
			out = append(out, line{content, b.marker == ""})
		}
	}
	var s strings.Builder
	for i, l := range out {
		s.WriteString(l.text)
		if l.newline || i < len(out)-1 {
			s.WriteString("\n")
		}
	}
	return s.String(), true
}

// applied applies patch to HEAD in a scratch index and returns what it
// makes of the file at name, and whether the file is there at all.
func (r *testRepo) applied(patch, name string) (string, bool, error) {
	r.t.Helper()
	env := append(os.Environ(), "GIT_INDEX_FILE="+filepath.Join(r.t.TempDir(), "index"))
	gitEnv := func(stdin string, args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env, cmd.Stdin = r.dir, env, strings.NewReader(stdin)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %s: %v\n%s", args[0], err, out)
		}
		return string(out), nil
	}
	if _, err := gitEnv("", "read-tree", "HEAD"); err != nil {
		return "", false, err
	}
	if _, err := gitEnv(patch, "apply", "--cached", "-"); err != nil {
		return "", false, err
	}
	files, err := gitEnv("", "ls-files", "-z")
	if err != nil {
		return "", false, err
	}
	names := strings.Split(strings.TrimSuffix(files, "\x00"), "\x00")
	sort.Strings(names)
	if i := sort.SearchStrings(names, name); i == len(names) || names[i] != name {
		return "", false, nil
	}
	content, err := gitEnv("", "cat-file", "blob", ":"+name)
	return content, true, err
}
