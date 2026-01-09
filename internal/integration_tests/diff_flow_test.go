package integration_tests

import (
	"carya/internal/chunk"
	"carya/internal/store"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestEndToEndDiffFlow tests the complete flow from file changes to diff storage and retrieval
func TestEndToEndDiffFlow(t *testing.T) {
	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Initialize store
	chunkStore, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer chunkStore.Close()

	// Create strategy
	strategy := chunk.NewUnifiedStrategy()

	// Simulate file changes
	t.Run("CompleteFlow", func(t *testing.T) {
		// Initial content
		event1 := chunk.FileChangeEvent{
			Path:     "example.go",
			Contents: []byte("package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"),
			Time:     time.Now(),
		}
		strategy.OnFileChange(event1)

		// Wait a bit
		time.Sleep(10 * time.Millisecond)

		// Modified content
		event2 := chunk.FileChangeEvent{
			Path:     "example.go",
			Contents: []byte("package main\n\nfunc main() {\n\tprintln(\"hello world\")\n\tprintln(\"goodbye\")\n}\n"),
			Time:     time.Now(),
		}
		strategy.OnFileChange(event2)

		// Force flush to get the chunk
		chunkObj := strategy.ForceFlush("example.go")
		if chunkObj == nil {
			t.Fatal("Expected chunk, got nil")
		}

		// Verify diff is generated
		if chunkObj.Diff == "" {
			t.Error("Expected non-empty diff")
		}

		t.Logf("Generated diff:\n%s", chunkObj.Diff)

		// Verify diff contains expected changes
		if !strings.Contains(chunkObj.Diff, "+") {
			t.Error("Expected diff to contain additions")
		}

		// Save chunk to store
		if err := chunkStore.SaveChunk(*chunkObj); err != nil {
			t.Fatalf("Failed to save chunk: %v", err)
		}

		// Retrieve chunks from store
		chunks, err := chunkStore.FindChunks("example.go")
		if err != nil {
			t.Fatalf("Failed to retrieve chunks: %v", err)
		}

		if len(chunks) == 0 {
			t.Fatal("Expected at least one chunk, got none")
		}

		// Verify the retrieved chunk has the diff
		retrievedChunk := chunks[0]
		if retrievedChunk.Diff == "" {
			t.Error("Retrieved chunk has empty diff")
		}

		if retrievedChunk.Diff != chunkObj.Diff {
			t.Errorf("Retrieved diff doesn't match saved diff\nExpected:\n%s\nGot:\n%s",
				chunkObj.Diff, retrievedChunk.Diff)
		}

		t.Logf("Retrieved diff:\n%s", retrievedChunk.Diff)
		t.Logf("✅ End-to-end diff flow successful")
	})
}

// TestManagerFlushFlow tests the flow through the Manager
func TestManagerFlushFlow(t *testing.T) {
	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Initialize store
	chunkStore, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer chunkStore.Close()

	// Create strategy and manager
	strategy := chunk.NewUnifiedStrategy()

	// Create a simple event emitter for testing
	emitter := &testEmitter{}

	manager := chunk.NewManager(strategy, chunkStore, emitter)

	// Simulate file changes through the manager
	t.Run("ManagerFlow", func(t *testing.T) {
		// First change
		event1 := chunk.FileChangeEvent{
			Path:     "test.go",
			Contents: []byte("package main\n"),
			Time:     time.Now(),
		}
		manager.OnFileChange(event1)

		// Second change
		event2 := chunk.FileChangeEvent{
			Path:     "test.go",
			Contents: []byte("package main\n\nfunc main() {}\n"),
			Time:     time.Now(),
		}
		manager.OnFileChange(event2)

		// Force flush through manager
		if err := manager.ForceFlush("test.go"); err != nil {
			t.Fatalf("Failed to force flush: %v", err)
		}

		// Retrieve from store
		chunks, err := chunkStore.GetRecentChunks(10)
		if err != nil {
			t.Fatalf("Failed to get recent chunks: %v", err)
		}

		if len(chunks) == 0 {
			t.Fatal("Expected at least one chunk")
		}

		// Find the chunk for our file
		var foundChunk *chunk.Chunk
		for _, c := range chunks {
			if c.FilePath == "test.go" {
				foundChunk = &c
				break
			}
		}

		if foundChunk == nil {
			t.Fatal("Could not find chunk for test.go")
		}

		// Verify diff exists
		if foundChunk.Diff == "" {
			t.Error("Chunk has empty diff after manager flush")
		}

		// Verify diff has proper structure
		if !strings.Contains(foundChunk.Diff, "diff --git") {
			t.Error("Diff doesn't contain git header")
		}

		t.Logf("Manager flushed chunk diff:\n%s", foundChunk.Diff)
		t.Logf("✅ Manager flush flow successful")
	})
}

// testEmitter is a simple event emitter for testing
type testEmitter struct{}

func (e *testEmitter) EmitChunkCreated(chunk chunk.Chunk) {
	// No-op for testing
}

func (e *testEmitter) EmitChunkFlushed(chunks []chunk.Chunk) {
	// No-op for testing
}

// TestMultipleFilesFlow tests diff generation for multiple files
func TestMultipleFilesFlow(t *testing.T) {
	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Initialize store
	chunkStore, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer chunkStore.Close()

	// Create strategy
	strategy := chunk.NewUnifiedStrategy()

	t.Run("MultipleFiles", func(t *testing.T) {
		// File 1
		event1 := chunk.FileChangeEvent{
			Path:     "file1.go",
			Contents: []byte("package main\n\nfunc foo() {}\n"),
			Time:     time.Now(),
		}
		strategy.OnFileChange(event1)

		// File 2
		event2 := chunk.FileChangeEvent{
			Path:     "file2.go",
			Contents: []byte("package main\n\nfunc bar() {}\n"),
			Time:     time.Now(),
		}
		strategy.OnFileChange(event2)

		// Update both files
		event3 := chunk.FileChangeEvent{
			Path:     "file1.go",
			Contents: []byte("package main\n\nfunc foo() {\n\treturn 42\n}\n"),
			Time:     time.Now().Add(time.Millisecond),
		}
		strategy.OnFileChange(event3)

		event4 := chunk.FileChangeEvent{
			Path:     "file2.go",
			Contents: []byte("package main\n\nfunc bar() {\n\treturn \"hello\"\n}\n"),
			Time:     time.Now().Add(time.Millisecond),
		}
		strategy.OnFileChange(event4)

		// Flush all
		chunks := strategy.FlushAll()
		if len(chunks) != 2 {
			t.Errorf("Expected 2 chunks, got %d", len(chunks))
		}

		// Verify both chunks have diffs
		for _, c := range chunks {
			if c.Diff == "" {
				t.Errorf("Chunk for %s has empty diff", c.FilePath)
			}
			if !strings.Contains(c.Diff, "diff --git") {
				t.Errorf("Chunk for %s missing git header", c.FilePath)
			}
			t.Logf("Chunk for %s has diff (length: %d)", c.FilePath, len(c.Diff))
		}

		t.Logf("✅ Multiple files diff generation successful")
	})
}
