package hunks

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// pathRefs resolves the paths given to Select to the changed files they
// name. Hunk paths are relative to the checkout root, and that is how
// `carya hunks` prints them, so a path is read from the root first. The
// command may run in a subdirectory, though, so a path that names nothing
// from the root is read from the current directory, and one written as
// ./x or ../x, which can only mean a path from here, is read only from
// there. An absolute path names what it names inside the checkout.
type pathRefs struct {
	all  []Hunk
	root string // the checkout, or "" for hunks that did not come from List
	here string // the current directory from root, "." at the root; "" outside it
}

func newPathRefs(all []Hunk) pathRefs {
	p := pathRefs{all: all}
	if len(all) == 0 || all[0].root == "" {
		return p
	}
	p.root = all[0].root
	if wd, err := os.Getwd(); err == nil {
		var ok bool
		if p.here, ok = within(p.root, wd); !ok {
			p.here, _ = within(p.root, realDir(wd)) // wd itself can be a symlink
		}
	}
	return p
}

// match returns the hunks of the changed file or under the directory that
// ref, slash-separated, names, from the first reading of it that matches any.
func (p pathRefs) match(ref string) []int {
	for _, c := range p.readings(ref) {
		if is := p.under(c); len(is) > 0 {
			return is
		}
	}
	return nil
}

// readings returns the checkout paths ref, slash-separated, could mean, in
// the order they are tried.
func (p pathRefs) readings(ref string) []string {
	if ref == "" {
		return nil
	}
	if filepath.IsAbs(ref) {
		if rel, ok := within(p.root, ref); ok {
			return []string{rel}
		}
		return nil
	}
	fromHere := ref == "." || ref == ".." || strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../")
	var out []string
	if !fromHere || p.here == "" {
		out = append(out, ref, path.Clean(ref))
	}
	if p.here != "" {
		out = append(out, path.Join(p.here, ref))
	}
	return out
}

// under returns the hunks of the file at checkout path dir, or of every
// file under it; "." is the whole checkout.
func (p pathRefs) under(dir string) []int {
	var is []int
	for i, h := range p.all {
		if dir == "." || h.Path == dir || strings.HasPrefix(h.Path, dir+"/") {
			is = append(is, i)
		}
	}
	return is
}

// explain lists refs that matched nothing, for an error, saying of each
// which checkout paths it was read as, unless that is just the ref itself.
func (p pathRefs) explain(refs []string) string {
	out := make([]string, len(refs))
	for i, ref := range refs {
		slashed := filepath.ToSlash(ref)
		var tried []string
		for _, r := range p.readings(slashed) {
			if !slices.Contains(tried, r) {
				tried = append(tried, r)
			}
		}
		out[i] = ref
		switch {
		case filepath.IsAbs(ref) && p.root != "" && len(tried) == 0:
			out[i] += " (outside the checkout)"
		case len(tried) > 1 || len(tried) == 1 && tried[0] != slashed:
			out[i] += " (tried " + strings.Join(tried, ", ") + ")"
		}
	}
	return strings.Join(out, ", ")
}

// within returns p relative to root, slash-separated, when p is root or a
// path inside it. It compares the paths as given and then with symlinks
// resolved in root and in p's directories, but not in p's last element: a
// changed file can itself be a symlink, and its path still names it.
func within(root, p string) (string, bool) {
	if root == "" {
		return "", false
	}
	p = filepath.Clean(p)
	if rel, ok := relative(root, p); ok {
		return rel, true
	}
	return relative(realDir(root), filepath.Join(realDir(filepath.Dir(p)), filepath.Base(p)))
}

func relative(root, p string) (string, bool) {
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// realDir resolves the symlinks in directory dir, as far up as it exists:
// a deleted file's directory can be gone too.
func realDir(dir string) string {
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		return r
	}
	parent := filepath.Dir(dir)
	if parent == dir {
		return dir
	}
	return filepath.Join(realDir(parent), filepath.Base(dir))
}
