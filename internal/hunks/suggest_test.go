package hunks

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func hunksFor(files ...string) []Hunk {
	var hs []Hunk
	for _, f := range files {
		hs = append(hs, Hunk{Path: f})
	}
	return hs
}

func TestSuggestRules(t *testing.T) {
	g := func(reason string, files ...string) Group { return Group{Files: files, Reason: reason} }
	const test, lock = "code and its tests", "manifest and lock file"
	cases := []struct {
		name  string
		files []string
		want  []Group
	}{
		{"go test in the same directory", []string{"a/x.go", "a/x_test.go", "a/y.go"}, []Group{g(test, "a/x.go", "a/x_test.go")}},
		{"go test in another directory", []string{"a/x.go", "b/x_test.go"}, nil},
		{"several hunks of one file", []string{"x.go", "x.go", "x_test.go"}, []Group{g(test, "x.go", "x_test.go")}},
		{"ts test", []string{"src/app.test.ts", "src/app.ts"}, []Group{g(test, "src/app.test.ts", "src/app.ts")}},
		{"tsx spec", []string{"ui/button.spec.tsx", "ui/button.tsx"}, []Group{g(test, "ui/button.spec.tsx", "ui/button.tsx")}},
		{"js test of jsx", []string{"lib/view.jsx", "lib/view.test.js"}, []Group{g(test, "lib/view.jsx", "lib/view.test.js")}},
		{"js test and spec join through the code", []string{"a.spec.js", "a.js", "a.test.js"}, []Group{g(test, "a.spec.js", "a.js", "a.test.js")}},
		{"js test in a tests directory", []string{"src/util.ts", "test/util.test.ts"}, []Group{g(test, "src/util.ts", "test/util.test.ts")}},
		{"js test with two candidates elsewhere", []string{"a/util.ts", "b/util.ts", "test/util.test.ts"}, nil},
		{"python test_ prefix", []string{"pkg/parse.py", "pkg/test_parse.py"}, []Group{g(test, "pkg/parse.py", "pkg/test_parse.py")}},
		{"python _test suffix", []string{"pkg/parse.py", "pkg/parse_test.py"}, []Group{g(test, "pkg/parse.py", "pkg/parse_test.py")}},
		{"python test in tests directory", []string{"pkg/parse.py", "tests/test_parse.py"}, []Group{g(test, "pkg/parse.py", "tests/test_parse.py")}},
		{"no cross-language tests", []string{"x.py", "x_test.go"}, nil},
		{"go.mod and go.sum", []string{"go.mod", "go.sum", "main.go"}, []Group{g(lock, "go.mod", "go.sum")}},
		{"nested go module keeps its own go.sum", []string{"go.sum", "tools/go.mod"}, nil},
		{"package-lock.json", []string{"package-lock.json", "package.json"}, []Group{g(lock, "package-lock.json", "package.json")}},
		{"pnpm", []string{"web/package.json", "web/pnpm-lock.yaml"}, []Group{g(lock, "web/package.json", "web/pnpm-lock.yaml")}},
		{"yarn", []string{"package.json", "yarn.lock"}, []Group{g(lock, "package.json", "yarn.lock")}},
		{"bun", []string{"bun.lock", "package.json"}, []Group{g(lock, "bun.lock", "package.json")}},
		{"npm workspace member", []string{"package-lock.json", "packages/a/package.json"}, []Group{g(lock, "package-lock.json", "packages/a/package.json")}},
		{"cargo", []string{"Cargo.lock", "Cargo.toml"}, []Group{g(lock, "Cargo.lock", "Cargo.toml")}},
		{"cargo workspace", []string{"Cargo.lock", "Cargo.toml", "crates/a/Cargo.toml", "crates/a/src/lib.rs"}, []Group{g(lock, "Cargo.lock", "Cargo.toml", "crates/a/Cargo.toml")}},
		{"nearest lock file wins", []string{"package.json", "web/package.json", "web/yarn.lock", "yarn.lock"}, []Group{g(lock, "package.json", "yarn.lock"), g(lock, "web/package.json", "web/yarn.lock")}},
		{"uv", []string{"pyproject.toml", "uv.lock"}, []Group{g(lock, "pyproject.toml", "uv.lock")}},
		{"poetry", []string{"poetry.lock", "pyproject.toml"}, []Group{g(lock, "poetry.lock", "pyproject.toml")}},
		{"lock file alone", []string{"go.sum", "README.md"}, nil},
		{"separate groups", []string{"go.mod", "go.sum", "x.go", "x_test.go"}, []Group{g(lock, "go.mod", "go.sum"), g(test, "x.go", "x_test.go")}},
		{"one file", []string{"x.go"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// A directory that is not a repository: no history links.
			got := Suggest(t.TempDir(), hunksFor(c.files...))
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("Suggest(%v)\n got %v\nwant %v", c.files, got, c.want)
			}
		})
	}
}

func TestUngrouped(t *testing.T) {
	hs := hunksFor("a.go", "a_test.go", "b.go", "b.go", "go.sum")
	got := Ungrouped(hs, Suggest(t.TempDir(), hs))
	if want := []string{"b.go", "go.sum"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Ungrouped = %v, want %v", got, want)
	}
}

func TestSuggestFromHistory(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(content string, files ...string) {
		t.Helper()
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(dir, f), []byte(content+"\n"), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	commit := func(msg, content string, files ...string) {
		t.Helper()
		write(content, files...)
		git("add", "-A")
		git("-c", "user.name=Dev", "-c", "user.email=d@e", "commit", "-q", "-m", msg)
	}
	git("init", "-q", "-b", "main")
	commit("a and b", "1", "a.go", "b.go")
	commit("a and b again", "2", "a.go", "b.go")
	commit("c and d once", "1", "c.go", "d.go")
	// e and f share two commits, but big ones: too weak a link.
	commit("bulk", "1", "e.go", "f.go", "g.go", "h.go", "i.go")
	commit("bulk again", "2", "e.go", "f.go", "g.go", "h.go", "i.go")

	write("3", "a.go", "a_test.go", "b.go", "c.go", "d.go", "e.go", "f.go")
	if err := os.WriteFile(filepath.Join(dir, ".git", "info", "exclude"), []byte(".carya/\n"), 0644); err != nil {
		t.Fatal(err)
	}

	suggest := func() ([]Group, []string) {
		t.Helper()
		hs, err := List(dir)
		if err != nil {
			t.Fatal(err)
		}
		groups := Suggest(dir, hs)
		return groups, Ungrouped(hs, groups)
	}

	// Without Carya set up there is no index: only the name rules apply.
	groups, _ := suggest()
	if want := []Group{{Files: []string{"a.go", "a_test.go"}, Reason: "code and its tests"}}; !reflect.DeepEqual(groups, want) {
		t.Fatalf("without an index: got %v, want %v", groups, want)
	}

	if err := os.Mkdir(filepath.Join(dir, ".carya"), 0755); err != nil {
		t.Fatal(err)
	}
	groups, rest := suggest()
	want := []Group{{Files: []string{"a.go", "a_test.go", "b.go"}, Reason: "code and its tests; often changed together"}}
	if !reflect.DeepEqual(groups, want) {
		t.Errorf("with history: got %v, want %v", groups, want)
	}
	if got := strings.Join(rest, " "); got != "c.go d.go e.go f.go" {
		t.Errorf("ungrouped = %s", got)
	}
}
