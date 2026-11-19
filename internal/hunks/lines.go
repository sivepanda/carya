package hunks

import (
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
