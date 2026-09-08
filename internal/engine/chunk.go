// Package engine provides the main engine and coordination logic for the Carya
// version control system, integrating chunk management, storage, and file watching.
package engine

import (
	"carya/internal/chunk"
	"carya/internal/config"
	"carya/internal/git"
	"carya/internal/identity"
	"carya/internal/store"
	"log"
	"time"
)

// Engine is the main coordination component of Carya that manages chunk creation,
// storage, and file change processing.
type Engine struct {
	chunkManager *chunk.Manager   // Manages chunk lifecycle and creation
	store        chunk.ChunkStore // Storage backend for chunks
	shadow       *git.ShadowRepo  // Shadow git repository for blob storage
	refManager   *git.RefManager  // Ref manager for user state sharing
	userID       string           // User identifier for team features
}

// SimpleEventEmitter provides basic logging-based event emission for chunk events.
type SimpleEventEmitter struct{}

// EmitChunkCreated logs when a new chunk is created.
func (e *SimpleEventEmitter) EmitChunkCreated(c chunk.Chunk) {
	log.Printf("Chunk created: %s for file %s", c.ID, c.FilePath)
}

// EmitChunkFlushed logs when chunks are flushed to storage.
func (e *SimpleEventEmitter) EmitChunkFlushed(chunks []chunk.Chunk) {
	log.Printf("Flushed %d chunks", len(chunks))
}

// NewEngineWithShadow creates a new Carya engine with shadow repository support.
func NewEngineWithShadow(storePath, repoRoot, caryaPath string) (*Engine, error) {
	chunkStore, err := store.NewSQLiteStore(storePath)
	if err != nil {
		return nil, err
	}

	shadow := git.NewShadowRepo(caryaPath, repoRoot)
	if err := shadow.Initialize(); err != nil {
		return nil, err
	}

	if err := shadow.SeedIndexFromMainHEAD(); err != nil {
		log.Printf("Warning: could not seed shadow index from HEAD: %v", err)
	}

	// Initialize ref manager
	refManager := git.NewRefManager(repoRoot)

	// Get or create user identity
	userIdentity := identity.NewUserIdentity(caryaPath)
	userID, err := userIdentity.GetOrCreate()
	if err != nil {
		log.Printf("Warning: Failed to get user identity: %v", err)
		userID = "unknown"
	}

	// Create strategy with shadow repo
	strategy := chunk.NewGitStrategy(shadow)
	emitter := &SimpleEventEmitter{}
	manager := chunk.NewManager(strategy, chunkStore, emitter, managerOptionsFromGlobalConfig())

	return &Engine{
		chunkManager: manager,
		store:        chunkStore,
		shadow:       shadow,
		refManager:   refManager,
		userID:       userID,
	}, nil
}

// Start begins the engine's background processing, including chunk management.
func (e *Engine) Start() {
	e.chunkManager.Start()
}

// Stop gracefully shuts down the engine and all its components.
func (e *Engine) Stop() {
	e.chunkManager.Stop()
}

// OnFileChange processes a file change event by creating a FileChangeEvent
// and passing it to the chunk manager for processing.
func (e *Engine) OnFileChange(path string, contents []byte) {
	event := chunk.FileChangeEvent{
		Path:     path,
		Contents: contents,
		Time:     time.Now(),
	}
	e.chunkManager.OnFileChange(event)
}

// ForceFlush immediately creates and saves a chunk for the specified file path.
// Returns an error if the chunk cannot be created or saved.
func (e *Engine) ForceFlush(filePath string) error {
	return e.chunkManager.ForceFlush(filePath)
}

// FlushAll immediately flushes all active chunks to storage.
func (e *Engine) FlushAll() error {
	return e.chunkManager.FlushAll()
}

// FlushStatus returns the current flush interval and idle state.
func (e *Engine) FlushStatus() (interval time.Duration, isIdle bool) {
	return e.chunkManager.FlushStatus()
}

// PublishState writes the current working tree to a git ref for team sharing.
func (e *Engine) PublishState() error {
	if e.shadow == nil || e.refManager == nil {
		return nil
	}

	treeHash, err := e.shadow.WriteTree()
	if err != nil {
		return err
	}

	return e.refManager.UpdateUserTreeRef(e.userID, treeHash)
}

// PublishAndPushState writes the current working tree to a git ref and pushes
// it to remote, so team members can see it without the caller having to know
// about the local ref update and the push as two separate steps.
func (e *Engine) PublishAndPushState(remote string) error {
	if err := e.PublishState(); err != nil {
		return err
	}
	if e.refManager == nil {
		return nil
	}
	return e.refManager.PushUserRef(remote, e.userID)
}

func managerOptionsFromGlobalConfig() chunk.ManagerOptions {
	opts := chunk.DefaultManagerOptions()

	globalCfg, err := config.LoadGlobalConfig()
	if err != nil {
		log.Printf("Warning: failed to load global config for flush settings: %v", err)
		return opts
	}

	hot, cold := globalCfg.FlushIntervals()
	opts.BaseInterval = hot
	opts.MaxInterval = cold
	return opts
}
