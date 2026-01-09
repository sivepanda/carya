package chunk

import (
	"strings"
	"testing"
	"time"
)

func TestDiffGeneration(t *testing.T) {
	strategy := NewUnifiedStrategy()

	// Test 1: Simple text addition
	t.Run("SimpleAddition", func(t *testing.T) {
		// Simulate initial file state
		initialContent := []byte("line 1\nline 2\nline 3\n")
		event1 := FileChangeEvent{
			Path:     "test.txt",
			Contents: initialContent,
			Time:     time.Now(),
		}

		// Manually set git content (simulating HEAD)
		active := &activeChunk{
			chunk: &Chunk{
				ID:        "test-1",
				FilePath:  "test.txt",
				StartTime: event1.Time,
				EndTime:   event1.Time,
			},
			lastUpdate:     event1.Time,
			initialContent: []byte("line 1\nline 2\n"),
			initialHash:    strategy.hashContent([]byte("line 1\nline 2\n")),
			latestContent:  initialContent,
		}

		active.chunk.Hash = ChunkHash(strategy.hashContent(initialContent))

		// Generate diff
		diff := strategy.generateDiff(active)

		// Verify diff is not empty
		if diff == "" {
			t.Error("Expected non-empty diff, got empty string")
		}

		// Verify diff contains the added line
		if !strings.Contains(diff, "+line 3") {
			t.Errorf("Expected diff to contain '+line 3', got:\n%s", diff)
		}

		// Verify diff has proper header
		if !strings.Contains(diff, "diff --git") {
			t.Errorf("Expected diff to contain git header, got:\n%s", diff)
		}

		t.Logf("Generated diff:\n%s", diff)
	})

	// Test 2: Text removal
	t.Run("SimpleRemoval", func(t *testing.T) {
		initialContent := []byte("line 1\nline 2\nline 3\n")
		newContent := []byte("line 1\nline 3\n")

		active := &activeChunk{
			chunk: &Chunk{
				ID:        "test-2",
				FilePath:  "test.txt",
				StartTime: time.Now(),
				EndTime:   time.Now(),
			},
			lastUpdate:     time.Now(),
			initialContent: initialContent,
			initialHash:    strategy.hashContent(initialContent),
			latestContent:  newContent,
		}

		active.chunk.Hash = ChunkHash(strategy.hashContent(newContent))

		// Generate diff
		diff := strategy.generateDiff(active)

		// Verify diff contains the removed line
		if !strings.Contains(diff, "-line 2") {
			t.Errorf("Expected diff to contain '-line 2', got:\n%s", diff)
		}

		t.Logf("Generated diff:\n%s", diff)
	})

	// Test 3: Binary file detection
	t.Run("BinaryFile", func(t *testing.T) {
		// Content with null bytes (binary)
		binaryContent := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}

		active := &activeChunk{
			chunk: &Chunk{
				ID:        "test-3",
				FilePath:  "test.bin",
				StartTime: time.Now(),
				EndTime:   time.Now(),
			},
			lastUpdate:     time.Now(),
			initialContent: []byte{0x00, 0x01},
			initialHash:    strategy.hashContent([]byte{0x00, 0x01}),
			latestContent:  binaryContent,
		}

		active.chunk.Hash = ChunkHash(strategy.hashContent(binaryContent))

		// Generate diff
		diff := strategy.generateDiff(active)

		// Verify binary file message
		if !strings.Contains(diff, "Binary file") {
			t.Errorf("Expected 'Binary file' message, got:\n%s", diff)
		}

		t.Logf("Generated diff:\n%s", diff)
	})

	// Test 4: Empty file
	t.Run("EmptyToContent", func(t *testing.T) {
		newContent := []byte("new line\n")

		active := &activeChunk{
			chunk: &Chunk{
				ID:        "test-4",
				FilePath:  "newfile.txt",
				StartTime: time.Now(),
				EndTime:   time.Now(),
			},
			lastUpdate:     time.Now(),
			initialContent: []byte{},
			initialHash:    strategy.hashContent([]byte{}),
			latestContent:  newContent,
		}

		active.chunk.Hash = ChunkHash(strategy.hashContent(newContent))

		// Generate diff
		diff := strategy.generateDiff(active)

		// Verify diff shows addition
		if !strings.Contains(diff, "+new line") {
			t.Errorf("Expected diff to contain '+new line', got:\n%s", diff)
		}

		t.Logf("Generated diff:\n%s", diff)
	})
}

func TestFlushGeneratesDiff(t *testing.T) {
	strategy := NewUnifiedStrategy()

	// Simulate file change
	initialContent := []byte("initial content\n")
	event1 := FileChangeEvent{
		Path:     "test.txt",
		Contents: initialContent,
		Time:     time.Now(),
	}

	// Create initial chunk (simulating file in git HEAD is empty)
	strategy.OnFileChange(event1)

	// Simulate another change
	updatedContent := []byte("initial content\nupdated line\n")
	event2 := FileChangeEvent{
		Path:     "test.txt",
		Contents: updatedContent,
		Time:     time.Now().Add(time.Second),
	}

	strategy.OnFileChange(event2)

	// Force flush to generate diff
	chunk := strategy.ForceFlush("test.txt")

	if chunk == nil {
		t.Fatal("Expected chunk to be created, got nil")
	}

	if chunk.Diff == "" {
		t.Error("Expected non-empty diff after flush")
	}

	// Verify diff contains expected content
	if !strings.Contains(chunk.Diff, "+updated line") {
		t.Errorf("Expected diff to contain '+updated line', got:\n%s", chunk.Diff)
	}

	t.Logf("Flushed chunk diff:\n%s", chunk.Diff)
}
