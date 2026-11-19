package hunks

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Line is one line of a hunk's diff. N numbers the lines of the hunk's body
// from 1, so that part of a hunk can be selected as "<id>:<ranges>"; it is 0
// for the lines before the body (the @@ line and, for a whole file, its diff
// header) and for "\ No newline at end of file" markers.
type Line struct {
	N    int
	Text string
}

// Lines returns h's diff line by line with the body's lines numbered. A hunk
// that Select cut down to some of its lines keeps the numbers those lines
// have in the whole hunk, so they mean the same in both.
func (h Hunk) Lines() []Line {
	texts := strings.Split(strings.TrimSuffix(h.Diff, "\n"), "\n")
	out := make([]Line, len(texts))
	for i, t := range texts {
		out[i].Text = t
	}
	if h.numbers != nil {
		for i := range out {
			if i < len(h.numbers) {
				out[i].N = h.numbers[i]
			}
		}
		return out
	}
	head, body, ok := splitBody(h.Diff)
	if !ok {
		return out
	}
	i := len(head)
	for _, b := range body {
		if i < len(out) {
			out[i].N = b.n
		}
		i++
		if b.marker != "" {
			i++
		}
	}
	return out
}

// bodyLine is a line of a hunk's body: its text, newline included, its
// number, and the "\ No newline at end of file" line after it, if any.
type bodyLine struct {
	text, marker string
	n            int
}

func (b bodyLine) op() byte {
	if b.text == "" {
		return ' '
	}
	return b.text[0]
}

// splitBody splits a text hunk's diff into its lines up to and including
// the @@ line, and its body. It reports false for a diff without an @@ line:
// a binary file, a mode change or an empty file.
func splitBody(diff string) (head []string, body []bodyLine, ok bool) {
	lines := strings.SplitAfter(diff, "\n")
	at := -1
	for i, l := range lines {
		if hunkHeader.MatchString(l) {
			at = i
			break
		}
	}
	if at < 0 {
		return nil, nil, false
	}
	n := 0
	for _, l := range lines[at+1:] {
		switch {
		case l == "":
		case strings.HasPrefix(l, `\`) && len(body) > 0:
			body[len(body)-1].marker = l
		default:
			n++
			body = append(body, bodyLine{text: l, n: n})
		}
	}
	return lines[:at+1], body, true
}

// cutRanges splits "<id>:<ranges>" at its last colon. IDs have no colons
// in them, and ranges have none either.
func cutRanges(ref string) (id, ranges string, ok bool) {
	i := strings.LastIndexByte(ref, ':')
	if i < 0 {
		return "", "", false
	}
	return ref[:i], ref[i+1:], true
}

// idsOfPath returns the IDs of the hunks of the changed file at p, to point
// out that line ranges go after one of those instead.
func idsOfPath(all []Hunk, p string) []string {
	clean := path.Clean(filepath.ToSlash(p))
	var is []int
	for i, h := range all {
		if h.Path == p || h.Path == clean {
			is = append(is, i)
		}
	}
	return idsOf(all, is)
}

func idsOf(all []Hunk, is []int) []string {
	var ids []string
	for _, i := range is {
		ids = append(ids, all[i].ID)
	}
	return ids
}

// lineRanges reads line ranges such as "1-40,52" for hunk h.
func lineRanges(h Hunk, ranges string) (map[int]bool, error) {
	_, body, ok := splitBody(h.Diff)
	if !ok || len(body) == 0 {
		what := map[string]string{"binary": "a binary file", "mode": "a mode change"}[h.Kind]
		if what == "" {
			what = "a binary or empty file"
		}
		return nil, fmt.Errorf("hunk %s is %s, with no lines to choose from; commit it whole", h.ID, what)
	}
	if ranges == "" {
		return nil, errors.New("no lines after the colon; write them as in 1-40,52")
	}
	digits := func(s string) bool {
		for _, c := range s {
			if c < '0' || c > '9' {
				return false
			}
		}
		return s != ""
	}
	keep := map[int]bool{}
	for _, item := range strings.Split(ranges, ",") {
		lo, hi, isRange := strings.Cut(item, "-")
		if !isRange {
			hi = lo
		}
		if item == "" {
			return nil, errors.New("empty range between commas; write lines as in 1-40,52")
		}
		if !digits(lo) || !digits(hi) {
			return nil, fmt.Errorf("%q is not a line number or range; write lines as in 1-40,52", item)
		}
		a, errA := strconv.Atoi(lo)
		b, errB := strconv.Atoi(hi)
		tooFar := errA != nil || errB != nil // too big for an int
		switch {
		case !tooFar && a == 0:
			return nil, errors.New("lines are numbered from 1")
		case !tooFar && b < a:
			return nil, fmt.Errorf("range %s runs backwards", item)
		case tooFar || b > len(body):
			return nil, fmt.Errorf("%s is past the end: hunk %s has lines 1-%d (see `carya hunks --diff %s`)", item, h.ID, len(body), h.ID)
		}
		for i := a; i <= b; i++ {
			keep[i] = true
		}
	}
	return keep, nil
}

// rangesOf writes a set of line numbers as ranges, as in "1-40,52".
func rangesOf(keep map[int]bool) string {
	var ns []int
	for n := range keep {
		ns = append(ns, n)
	}
	sort.Ints(ns)
	var parts []string
	for i := 0; i < len(ns); {
		j := i
		for j+1 < len(ns) && ns[j+1] == ns[j]+1 {
			j++
		}
		if j == i {
			parts = append(parts, strconv.Itoa(ns[i]))
		} else {
			parts = append(parts, strconv.Itoa(ns[i])+"-"+strconv.Itoa(ns[j]))
		}
		i = j + 1
	}
	return strings.Join(parts, ",")
}

var rangeHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(.*)$`)

// part cuts h down to the body lines in keep: kept added lines stay and
// the other added lines go; the other removed lines become context, so
// their removal is not committed; context stays. The result is h itself
// when keep has every added and removed line, and an error when it has
// none. A part of a new file still creates the file; a part of a deletion
// changes the file instead of deleting it.
func part(h Hunk, keep map[int]bool) (Hunk, error) {
	head, body, ok := splitBody(h.Diff)
	if !ok {
		return Hunk{}, fmt.Errorf("hunk %s has no lines to choose from", h.ID)
	}
	at := strings.TrimSuffix(head[len(head)-1], "\n")
	m := rangeHeader.FindStringSubmatch(at)
	if m == nil {
		return Hunk{}, fmt.Errorf("cannot read the @@ line %q of hunk %s", at, h.ID)
	}
	oldStart, _ := strconv.Atoi(m[1])
	newStart, _ := strconv.Atoi(m[3])
	newCountBefore := 1
	if m[4] != "" {
		newCountBefore, _ = strconv.Atoi(m[4])
	}

	var lines []string
	var numbers []int
	emit := func(text string, n int, marker string) {
		lines = append(lines, text)
		numbers = append(numbers, n)
		if marker != "" {
			lines = append(lines, marker)
			numbers = append(numbers, 0)
		}
	}
	// addsAfter reports whether a kept added line comes after body[k].
	addsAfter := func(k int) bool {
		for _, b := range body[k+1:] {
			if b.op() == '+' && keep[b.n] {
				return true
			}
		}
		return false
	}
	var oldCount, newCount, added, removed int
	chosen, left := false, false
	for k, b := range body {
		switch op, content := b.op(), b.text[min(1, len(b.text)):]; {
		case op == '+' && keep[b.n]:
			emit(b.text, b.n, b.marker)
			newCount++
			added++
			chosen = true
		case op == '+':
			left = true
		case op == '-' && keep[b.n]:
			emit(b.text, b.n, b.marker)
			oldCount++
			removed++
			chosen = true
		case op == '-' && b.marker != "" && addsAfter(k):
			// The file's last line, which has no newline at its end, stays
			// and kept lines are added after it, so it needs a newline now.
			left = true
			emit(b.text, b.n, b.marker)
			emit("+"+content, 0, "")
			oldCount++
			newCount++
			removed++
			added++
		case op == '-':
			left = true
			emit(" "+content, b.n, b.marker)
			oldCount++
			newCount++
		default:
			emit(b.text, b.n, b.marker)
			oldCount++
			newCount++
		}
	}
	if !chosen {
		return Hunk{}, fmt.Errorf("%s:%s selects no added or removed line, only context", h.ID, rangesOf(keep))
	}
	if !left {
		return h, nil
	}
	// With no lines on a side, the @@ line gives the line before the hunk
	// instead of its first line.
	switch {
	case newCountBefore == 0 && newCount > 0:
		newStart++
	case newCountBefore > 0 && newCount == 0:
		newStart--
	}
	header := "@@ -" + span(oldStart, oldCount) + " +" + span(newStart, newCount) + " @@" + m[5]

	p := h
	p.ID = h.ID + ":" + rangesOf(keep)
	p.Added, p.Removed = added, removed
	newBody := strings.Join(lines, "")
	switch h.Kind {
	case "add":
		// The index line names the whole file's blob, which this is not.
		var fh strings.Builder
		for _, l := range head[:len(head)-1] {
			if !strings.HasPrefix(l, "index ") {
				fh.WriteString(l)
			}
		}
		p.Diff = fh.String() + header + "\n" + newBody
		p.numbers = append(make([]int, strings.Count(fh.String(), "\n")+1), numbers...)
	case "delete":
		p.Kind = "change"
		p.fileHeader = asChange(head[:len(head)-1])
		fallthrough
	default:
		p.Header = header
		p.Diff = header + "\n" + newBody
		p.numbers = append([]int{0}, numbers...)
	}
	return p, nil
}

// span writes a side of an @@ line, leaving out a count of 1 as git does.
func span(start, count int) string {
	if count == 1 {
		return strconv.Itoa(start)
	}
	return strconv.Itoa(start) + "," + strconv.Itoa(count)
}

// asChange turns a deleted file's diff header into one that changes the
// file, for committing part of the deletion.
func asChange(header []string) string {
	var b strings.Builder
	for _, l := range header {
		switch {
		case strings.HasPrefix(l, "deleted file mode "), strings.HasPrefix(l, "index "), strings.HasPrefix(l, "+++ "):
		case strings.HasPrefix(l, "--- "):
			b.WriteString(l)
			name := l[len("--- "):]
			if strings.HasPrefix(name, `"a/`) {
				name = `"b/` + name[len(`"a/`):]
			} else {
				name = "b/" + strings.TrimPrefix(name, "a/")
			}
			b.WriteString("+++ " + name)
		default:
			b.WriteString(l)
		}
	}
	return b.String()
}
