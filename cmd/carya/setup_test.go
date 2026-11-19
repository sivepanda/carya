package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"carya/internal/harness"
	"carya/internal/repository"
)

func setupFor(t *testing.T, name string) harnessSetup {
	t.Helper()
	for _, h := range harnessSetups {
		if h.name == name {
			return h
		}
	}
	t.Fatalf("no harness %q", name)
	return harnessSetup{}
}

func TestSetupTargetFromALinkedWorktree(t *testing.T) {
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	main := t.TempDir()
	run(main, "init", "-q", "-b", "trunk")
	if err := os.WriteFile(filepath.Join(main, "f.txt"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run(main, "add", "f.txt")
	run(main, "-c", "user.name=T", "-c", "user.email=t@e", "commit", "-q", "-m", "init")
	wt := filepath.Join(t.TempDir(), "wt")
	run(main, "worktree", "add", "-q", "-b", "feature", wt)

	resolve := func(p string) string {
		r, err := filepath.EvalSymlinks(filepath.Dir(filepath.Dir(p)))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	mainResolved, _ := filepath.EvalSymlinks(main)
	wtResolved, _ := filepath.EvalSymlinks(wt)

	wtRepo, err := repository.NewAt(wt)
	if err != nil {
		t.Fatal(err)
	}

	// Codex reads project hooks only from the main checkout.
	path, notes, err := setupTarget(setupFor(t, "codex"), harness.ScopeLocal, wtRepo)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "hooks.json" || resolve(path) != mainResolved {
		t.Fatalf("codex hooks from a worktree should go in the main checkout, got %s", path)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "linked worktree") {
		t.Fatalf("setup should say why it wrote the main checkout's file: %q", notes)
	}

	_, notes, err = setupTarget(setupFor(t, "codex"), harness.ScopeProject, wtRepo)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 || !strings.Contains(notes[1], "(trunk)") {
		t.Fatalf("project scope should name the main checkout's branch: %q", notes)
	}

	// Claude Code reads settings from the worktree itself.
	path, notes, err = setupTarget(setupFor(t, "claude"), harness.ScopeLocal, wtRepo)
	if err != nil {
		t.Fatal(err)
	}
	if resolve(path) != wtResolved || len(notes) != 0 {
		t.Fatalf("claude settings belong in the worktree, got %s %q", path, notes)
	}

	// In the main checkout nothing changes.
	mainRepo, err := repository.NewAt(main)
	if err != nil {
		t.Fatal(err)
	}
	path, notes, err = setupTarget(setupFor(t, "codex"), harness.ScopeLocal, mainRepo)
	if err != nil {
		t.Fatal(err)
	}
	if resolve(path) != mainResolved || len(notes) != 0 {
		t.Fatalf("main checkout: got %s %q", path, notes)
	}
}
