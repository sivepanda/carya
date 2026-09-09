package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryPathsAndEnsureExists(t *testing.T) {
	root := t.TempDir()
	r := &Repository{rootPath: root, caryaPath: filepath.Join(root, ".carya")}

	if r.Exists() {
		t.Fatal("repository should not exist before EnsureExists")
	}
	if err := r.EnsureExists(); err != nil {
		t.Fatalf("ensure exists: %v", err)
	}
	if !r.Exists() {
		t.Fatal("repository should exist after EnsureExists")
	}

	if got := r.DBPath(); got != filepath.Join(root, ".carya", "chunks.db") {
		t.Fatalf("unexpected DBPath: %s", got)
	}
	if got := r.ShadowPath(); got != filepath.Join(root, ".carya", "shadow") {
		t.Fatalf("unexpected ShadowPath: %s", got)
	}
}

func TestEnsureGitignoreWritesEntryOnce(t *testing.T) {
	root := t.TempDir()
	r := &Repository{rootPath: root, caryaPath: filepath.Join(root, ".carya")}
	gitignore := filepath.Join(root, ".gitignore")

	if err := r.EnsureGitignore(); err != nil {
		t.Fatalf("ensure gitignore first time: %v", err)
	}
	if err := r.EnsureGitignore(); err != nil {
		t.Fatalf("ensure gitignore second time: %v", err)
	}

	b, err := os.ReadFile(gitignore)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	content := string(b)
	if !strings.Contains(content, "# Carya directory") {
		t.Fatalf("expected carya header in .gitignore, got: %q", content)
	}
	if strings.Count(content, ".carya/") != 1 {
		t.Fatalf("expected single .carya/ entry, got: %q", content)
	}
}

func TestEnsureGitignoreRespectsExistingEntryForms(t *testing.T) {
	t.Run("existing .carya line", func(t *testing.T) {
		root := t.TempDir()
		r := &Repository{rootPath: root, caryaPath: filepath.Join(root, ".carya")}
		gitignore := filepath.Join(root, ".gitignore")
		if err := os.WriteFile(gitignore, []byte("node_modules\n.carya\n"), 0644); err != nil {
			t.Fatalf("write .gitignore: %v", err)
		}

		if err := r.EnsureGitignore(); err != nil {
			t.Fatalf("ensure gitignore: %v", err)
		}

		b, err := os.ReadFile(gitignore)
		if err != nil {
			t.Fatalf("read .gitignore: %v", err)
		}
		content := string(b)
		if strings.Count(content, ".carya") != 1 {
			t.Fatalf("expected unchanged .carya entry, got: %q", content)
		}
	})

	t.Run("existing .carya/ line", func(t *testing.T) {
		root := t.TempDir()
		r := &Repository{rootPath: root, caryaPath: filepath.Join(root, ".carya")}
		gitignore := filepath.Join(root, ".gitignore")
		if err := os.WriteFile(gitignore, []byte("dist\n.carya/\n"), 0644); err != nil {
			t.Fatalf("write .gitignore: %v", err)
		}

		if err := r.EnsureGitignore(); err != nil {
			t.Fatalf("ensure gitignore: %v", err)
		}

		b, err := os.ReadFile(gitignore)
		if err != nil {
			t.Fatalf("read .gitignore: %v", err)
		}
		content := string(b)
		if strings.Count(content, ".carya/") != 1 {
			t.Fatalf("expected unchanged .carya/ entry, got: %q", content)
		}
	})
}
