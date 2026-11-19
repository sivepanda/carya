package compose

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"carya/internal/chunk"
	"carya/internal/store"

	_ "modernc.org/sqlite"
)

// saveChunksInOrder saves chunks with created_at one second apart, oldest
// first, so the store's recency order is fixed (SaveChunk leaves created_at
// to SQLite, at one-second resolution).
func saveChunksInOrder(t *testing.T, chunks ...chunk.Chunk) *store.SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "chunks.db")
	st, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, c := range chunks {
		if err := st.SaveChunk(c); err != nil {
			t.Fatal(err)
		}
		at := created.Add(time.Duration(i) * time.Second).Format("2006-01-02 15:04:05")
		if _, err := db.Exec("UPDATE chunks SET created_at = ? WHERE id = ?", at, string(c.ID)); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

func TestNewLoadsWholeStackOfEachFileShown(t *testing.T) {
	repo, diffs := stackRepo(t, "file.txt", "one\ntwo\nthree\n", stackedVersions...)
	now := time.Now()

	// file.txt's first chunk is older than the most recent chunks compose
	// looks at, its second one newer, so a window of recent chunks would
	// cut its stack off at the bottom.
	chunks := []chunk.Chunk{{ID: "a", FilePath: "file.txt", Diff: diffs[0], StartTime: now, EndTime: now, Hash: "a"}}
	for i := 0; i < defaultRecentChunkLimit; i++ {
		at := now.Add(time.Duration(i+1) * time.Millisecond)
		path := fmt.Sprintf("note-%03d.txt", i)
		chunks = append(chunks, chunk.Chunk{
			ID: chunk.ChunkID(path), FilePath: path, StartTime: at, EndTime: at, Hash: "n",
			Diff: fmt.Sprintf("diff --git a/%s b/%s\nnew file mode 100644\n--- /dev/null\n+++ b/%s\n@@ -0,0 +1 @@\n+note\n", path, path, path),
		})
	}
	chunks = append(chunks, chunk.Chunk{ID: "b", FilePath: "file.txt", Diff: diffs[1], StartTime: now.Add(time.Second), EndTime: now.Add(time.Second), Hash: "b"})
	st := saveChunksInOrder(t, chunks...)

	c, err := New(st, repo)
	if err != nil {
		t.Fatal(err)
	}
	stack := 0
	for i, ch := range c.Chunks() {
		if ch.FilePath == "file.txt" {
			c.SetSelected(i, true)
			stack++
		}
	}
	if stack != 2 {
		t.Fatalf("compose loaded %d of file.txt's 2 chunks", stack)
	}

	res, err := c.CreateCommit("stack")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if res.Warning != nil || res.ApplyConflict != nil {
		t.Fatalf("expected a plain commit, got warning=%v conflict=%v", res.Warning, res.ApplyConflict)
	}
	if got := gitRun(t, repo, "show", "HEAD:file.txt"); got != stackedVersions[1] {
		t.Fatalf("committed file.txt = %q, want %q", got, stackedVersions[1])
	}
}

func TestNewLeavesOutFilesWithoutRecentChunks(t *testing.T) {
	now := time.Now()
	old := chunk.Chunk{ID: "old", FilePath: "old.txt", Diff: "x", StartTime: now, EndTime: now, Hash: "o"}
	chunks := []chunk.Chunk{old}
	for i := 0; i < defaultRecentChunkLimit; i++ {
		at := now.Add(time.Duration(i+1) * time.Millisecond)
		chunks = append(chunks, chunk.Chunk{ID: chunk.ChunkID(fmt.Sprintf("r%d", i)), FilePath: "recent.txt", Diff: "x", StartTime: at, EndTime: at, Hash: "r"})
	}
	st := saveChunksInOrder(t, chunks...)

	c, err := New(st, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := len(c.Chunks()); got != defaultRecentChunkLimit {
		t.Fatalf("compose loaded %d chunks, want the %d recent ones", got, defaultRecentChunkLimit)
	}
	for _, ch := range c.Chunks() {
		if ch.ID == old.ID {
			t.Fatal("compose loaded a file none of whose chunks is recent")
		}
	}
}
