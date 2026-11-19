package store

import (
	"carya/internal/chunk"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "chunks.db")
	s, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func TestSQLiteStoreSaveFindRecentAndDelete(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()

	c1 := chunk.Chunk{ID: "c1", FilePath: "a.txt", Diff: "d1", StartTime: now, EndTime: now, Hash: "h1", Manual: false}
	c2 := chunk.Chunk{ID: "c2", FilePath: "a.txt", Diff: "d2", StartTime: now.Add(time.Second), EndTime: now.Add(time.Second), Hash: "h2", Manual: true}
	c3 := chunk.Chunk{ID: "c3", FilePath: "b.txt", Diff: "d3", StartTime: now.Add(2 * time.Second), EndTime: now.Add(2 * time.Second), Hash: "h3", Manual: false}

	for _, c := range []chunk.Chunk{c1, c2, c3} {
		if err := s.SaveChunk(c); err != nil {
			t.Fatalf("save chunk %s: %v", c.ID, err)
		}
	}

	byFile, err := s.FindChunks("a.txt")
	if err != nil {
		t.Fatalf("find chunks by file: %v", err)
	}
	if len(byFile) != 2 {
		t.Fatalf("expected 2 chunks for a.txt, got %d", len(byFile))
	}

	recent, err := s.GetRecentChunks(2)
	if err != nil {
		t.Fatalf("get recent chunks: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent chunks, got %d", len(recent))
	}

	if err := s.DeleteChunks([]chunk.ChunkID{"c1", "c3"}); err != nil {
		t.Fatalf("delete chunks: %v", err)
	}
	all, err := s.GetAllChunks()
	if err != nil {
		t.Fatalf("get all chunks: %v", err)
	}
	if len(all) != 1 || all[0].ID != "c2" {
		t.Fatalf("expected only c2 remaining, got %+v", all)
	}
}

func TestSQLiteStoreGetChunksOfRecentFiles(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()

	// Oldest first; created_at is set explicitly since SQLite records it at
	// one-second resolution.
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, c := range []chunk.Chunk{
		{ID: "a1", FilePath: "a.txt", Diff: "d", StartTime: now, EndTime: now, Hash: "h"},
		{ID: "c1", FilePath: "c.txt", Diff: "d", StartTime: now, EndTime: now, Hash: "h"},
		{ID: "b1", FilePath: "b.txt", Diff: "d", StartTime: now, EndTime: now, Hash: "h"},
		{ID: "a2", FilePath: "a.txt", Diff: "d", StartTime: now, EndTime: now, Hash: "h"},
	} {
		if err := s.SaveChunk(c); err != nil {
			t.Fatalf("save chunk %s: %v", c.ID, err)
		}
		at := created.Add(time.Duration(i) * time.Second).Format("2006-01-02 15:04:05")
		if _, err := s.db.Exec("UPDATE chunks SET created_at = ? WHERE id = ?", at, string(c.ID)); err != nil {
			t.Fatal(err)
		}
	}

	// The two most recent chunks are a2 and b1: all of a.txt's and b.txt's
	// chunks are returned, c.txt's are not.
	got, err := s.GetChunksOfRecentFiles(2)
	if err != nil {
		t.Fatalf("get chunks of recent files: %v", err)
	}
	var ids []chunk.ChunkID
	for _, c := range got {
		ids = append(ids, c.ID)
	}
	if want := []chunk.ChunkID{"a2", "b1", "a1"}; len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] || ids[2] != want[2] {
		t.Fatalf("got %v, want %v", ids, want)
	}
}

func TestSQLiteStoreFeatureLabelOperations(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()

	for _, c := range []chunk.Chunk{
		{ID: "c1", FilePath: "a.txt", Diff: "d1", StartTime: now, EndTime: now, Hash: "h1", Manual: false},
		{ID: "c2", FilePath: "b.txt", Diff: "d2", StartTime: now, EndTime: now, Hash: "h2", Manual: false},
	} {
		if err := s.SaveChunk(c); err != nil {
			t.Fatalf("save chunk %s: %v", c.ID, err)
		}
	}

	if err := s.UpdateChunkFeatureLabel([]chunk.ChunkID{"c1", "c2"}, "search"); err != nil {
		t.Fatalf("update feature labels: %v", err)
	}

	labels, err := s.ListFeatureLabels()
	if err != nil {
		t.Fatalf("list feature labels: %v", err)
	}
	if len(labels) != 1 || labels[0] != "search" {
		t.Fatalf("expected labels [search], got %v", labels)
	}

	if err := s.ClearChunkFeatureLabel([]chunk.ChunkID{"c1"}); err != nil {
		t.Fatalf("clear feature label: %v", err)
	}
	labels, err = s.ListFeatureLabels()
	if err != nil {
		t.Fatalf("list feature labels after clear: %v", err)
	}
	if len(labels) != 1 || labels[0] != "search" {
		t.Fatalf("expected remaining label [search], got %v", labels)
	}

	if err := s.ClearChunkFeatureLabel([]chunk.ChunkID{"c2"}); err != nil {
		t.Fatalf("clear second feature label: %v", err)
	}
	labels, err = s.ListFeatureLabels()
	if err != nil {
		t.Fatalf("list feature labels after clearing all: %v", err)
	}
	if len(labels) != 0 {
		t.Fatalf("expected no labels, got %v", labels)
	}
}
