package chunk

import (
	"errors"
	"testing"
	"time"
)

type fakeStrategy struct {
	onFileChangeCalls int
	forceChunks       map[string]*Chunk
	staleChunks       []Chunk
}

func (f *fakeStrategy) OnFileChange(event FileChangeEvent) {
	f.onFileChangeCalls++
}

func (f *fakeStrategy) FlushStaleChunks(now time.Time) []Chunk {
	out := f.staleChunks
	f.staleChunks = nil
	return out
}

func (f *fakeStrategy) ForceFlush(filePath string) *Chunk {
	return f.forceChunks[filePath]
}

type fakeStrategyWithFlushAll struct {
	*fakeStrategy
	allChunks []Chunk
}

func (f *fakeStrategyWithFlushAll) FlushAll() []Chunk {
	out := f.allChunks
	f.allChunks = nil
	return out
}

type fakeStore struct {
	saved  []Chunk
	failID ChunkID
}

func (f *fakeStore) SaveChunk(c Chunk) error {
	if c.ID == f.failID {
		return errors.New("save failed")
	}
	f.saved = append(f.saved, c)
	return nil
}

func (f *fakeStore) FindChunks(filePath string) ([]Chunk, error) { return nil, nil }
func (f *fakeStore) GetRecentChunks(limit int) ([]Chunk, error)  { return nil, nil }

type fakeEmitter struct {
	created []Chunk
	flushed [][]Chunk
}

func (f *fakeEmitter) EmitChunkCreated(c Chunk) {
	f.created = append(f.created, c)
}

func (f *fakeEmitter) EmitChunkFlushed(chunks []Chunk) {
	cp := append([]Chunk(nil), chunks...)
	f.flushed = append(f.flushed, cp)
}

func TestManagerOnFileChangeResetsBackoffInterval(t *testing.T) {
	strategy := &fakeStrategy{}
	store := &fakeStore{}
	m := NewManager(strategy, store, nil)
	t.Cleanup(m.Stop)

	m.currentInterval = 10 * time.Minute
	m.ticker.Reset(m.currentInterval)

	m.OnFileChange(FileChangeEvent{Path: "a.txt", Contents: []byte("x"), Time: time.Now()})

	if strategy.onFileChangeCalls != 1 {
		t.Fatalf("expected strategy OnFileChange call count 1, got %d", strategy.onFileChangeCalls)
	}
	if m.currentInterval != m.baseInterval {
		t.Fatalf("expected interval reset to base (%s), got %s", m.baseInterval, m.currentInterval)
	}
}

func TestManagerForceFlushSavesAndEmits(t *testing.T) {
	chunk := &Chunk{ID: "c1", FilePath: "a.txt", Diff: "diff"}
	strategy := &fakeStrategy{forceChunks: map[string]*Chunk{"a.txt": chunk}}
	store := &fakeStore{}
	emitter := &fakeEmitter{}
	m := NewManager(strategy, store, emitter)
	t.Cleanup(m.Stop)

	if err := m.ForceFlush("a.txt"); err != nil {
		t.Fatalf("force flush: %v", err)
	}

	if len(store.saved) != 1 || store.saved[0].ID != "c1" {
		t.Fatalf("expected chunk c1 saved once, got %+v", store.saved)
	}
	if len(emitter.created) != 1 || emitter.created[0].ID != "c1" {
		t.Fatalf("expected chunk c1 emitted once, got %+v", emitter.created)
	}
}

func TestManagerFlushStaleChunksLockedPersistsAndEmits(t *testing.T) {
	strategy := &fakeStrategy{staleChunks: []Chunk{{ID: "ok1"}, {ID: "bad"}, {ID: "ok2"}}}
	store := &fakeStore{failID: "bad"}
	emitter := &fakeEmitter{}
	m := NewManager(strategy, store, emitter)
	t.Cleanup(m.Stop)

	m.flushStaleChunksLocked()

	if len(store.saved) != 2 {
		t.Fatalf("expected 2 successfully saved chunks, got %d", len(store.saved))
	}
	if len(emitter.flushed) != 1 || len(emitter.flushed[0]) != 3 {
		t.Fatalf("expected single flushed emit with 3 chunks, got %+v", emitter.flushed)
	}
}

func TestManagerFlushAllUsesOptionalInterface(t *testing.T) {
	t.Run("no flush-all interface", func(t *testing.T) {
		strategy := &fakeStrategy{}
		store := &fakeStore{}
		emitter := &fakeEmitter{}
		m := NewManager(strategy, store, emitter)
		t.Cleanup(m.Stop)

		if err := m.FlushAll(); err != nil {
			t.Fatalf("flush all: %v", err)
		}
		if len(store.saved) != 0 {
			t.Fatalf("expected no saved chunks, got %d", len(store.saved))
		}
	})

	t.Run("with flush-all interface", func(t *testing.T) {
		strategy := &fakeStrategyWithFlushAll{fakeStrategy: &fakeStrategy{}, allChunks: []Chunk{{ID: "c1"}, {ID: "c2"}}}
		store := &fakeStore{}
		emitter := &fakeEmitter{}
		m := NewManager(strategy, store, emitter)
		t.Cleanup(m.Stop)

		if err := m.FlushAll(); err != nil {
			t.Fatalf("flush all: %v", err)
		}
		if len(store.saved) != 2 {
			t.Fatalf("expected 2 saved chunks, got %d", len(store.saved))
		}
		if len(emitter.flushed) != 1 || len(emitter.flushed[0]) != 2 {
			t.Fatalf("expected one flush emit with 2 chunks, got %+v", emitter.flushed)
		}
	})
}

func TestManagerBackoffCapsAtMaximum(t *testing.T) {
	strategy := &fakeStrategy{}
	store := &fakeStore{}
	m := NewManager(strategy, store, nil)
	t.Cleanup(m.Stop)

	m.currentInterval = 25 * time.Minute
	m.backoff()
	if m.currentInterval != m.maxInterval {
		t.Fatalf("expected interval capped at max %s, got %s", m.maxInterval, m.currentInterval)
	}

	before := m.currentInterval
	m.backoff()
	if m.currentInterval != before {
		t.Fatalf("expected interval to remain capped at %s, got %s", before, m.currentInterval)
	}
}
