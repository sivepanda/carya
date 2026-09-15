// Package store provides storage implementations for persisting chunks in the
// Carya version control system, including SQLite and JSON-based storage.
package store

import (
	"carya/internal/chunk"
	"database/sql"
	"strings"

	_ "modernc.org/sqlite"
)

// SQLiteStore provides SQLite-based persistent storage for chunks.
type SQLiteStore struct {
	db *sql.DB // SQLite database connection
}

// NewSQLiteStore creates a new SQLite store with the specified database file path.
// It automatically initializes the required tables and indexes.
func NewSQLiteStore(dataSourceName string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dataSourceName)
	if err != nil {
		return nil, err
	}

	store := &SQLiteStore{db: db}
	if err := store.initTables(); err != nil {
		return nil, err
	}

	return store, nil
}

// initTables creates the chunks table and associated indexes if they don't exist.
func (s *SQLiteStore) initTables() error {
	query := `
		CREATE TABLE IF NOT EXISTS chunks (
			id TEXT PRIMARY KEY,
			file_path TEXT NOT NULL,
			diff TEXT NOT NULL,
			start_time TIMESTAMP NOT NULL,
			end_time TIMESTAMP NOT NULL,
			feature_label TEXT,
			hash TEXT NOT NULL,
			manual BOOLEAN NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			initial_blob_hash TEXT,
			final_blob_hash TEXT,
			tree_hash TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_chunks_file_path ON chunks(file_path);
		CREATE INDEX IF NOT EXISTS idx_chunks_created_at ON chunks(created_at);
		CREATE INDEX IF NOT EXISTS idx_chunks_tree_hash ON chunks(tree_hash);
	`
	_, err := s.db.Exec(query)
	if err != nil {
		return err
	}

	// Run migrations for existing databases
	return s.runMigrations()
}

// runMigrations adds new columns to existing tables if they don't exist.
func (s *SQLiteStore) runMigrations() error {
	// Check if columns exist and add them if not
	migrations := []string{
		"ALTER TABLE chunks ADD COLUMN initial_blob_hash TEXT",
		"ALTER TABLE chunks ADD COLUMN final_blob_hash TEXT",
		"ALTER TABLE chunks ADD COLUMN tree_hash TEXT",
		"ALTER TABLE chunks ADD COLUMN feature_label TEXT",
	}

	for _, migration := range migrations {
		// SQLite will error if column already exists, which is fine
		s.db.Exec(migration)
	}

	// Ensure index exists
	s.db.Exec("CREATE INDEX IF NOT EXISTS idx_chunks_tree_hash ON chunks(tree_hash)")

	return nil
}

// SaveChunk persists a chunk to the SQLite database, replacing any existing chunk with the same ID.
func (s *SQLiteStore) SaveChunk(c chunk.Chunk) error {
	query := `
		INSERT OR REPLACE INTO chunks (id, file_path, diff, start_time, end_time, feature_label, hash, manual)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.Exec(query, c.ID, c.FilePath, c.Diff, c.StartTime, c.EndTime, c.FeatureLabel, c.Hash, c.Manual)
	return err
}

// FindChunks retrieves all chunks for a specific file path, ordered by creation time (newest first).
func (s *SQLiteStore) FindChunks(filePath string) ([]chunk.Chunk, error) {
	query := `
		SELECT id, file_path, diff, start_time, end_time, COALESCE(feature_label, ''), hash, manual
		FROM chunks 
		WHERE file_path = ?
		ORDER BY created_at DESC
	`
	rows, err := s.db.Query(query, filePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanChunks(rows)
}

// GetRecentChunks retrieves the most recently created chunks up to the specified limit.
func (s *SQLiteStore) GetRecentChunks(limit int) ([]chunk.Chunk, error) {
	query := `
		SELECT id, file_path, diff, start_time, end_time, COALESCE(feature_label, ''), hash, manual
		FROM chunks 
		ORDER BY created_at DESC
		LIMIT ?
	`
	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanChunks(rows)
}

// GetAllChunks retrieves all chunks ordered by creation time (newest first).
func (s *SQLiteStore) GetAllChunks() ([]chunk.Chunk, error) {
	query := `
		SELECT id, file_path, diff, start_time, end_time, COALESCE(feature_label, ''), hash, manual
		FROM chunks
		ORDER BY created_at DESC
	`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanChunks(rows)
}

// DeleteChunks removes chunks by ID.
func (s *SQLiteStore) DeleteChunks(ids []chunk.ChunkID) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = string(id)
	}

	query := "DELETE FROM chunks WHERE id IN (" + strings.Join(placeholders, ",") + ")"
	_, err := s.db.Exec(query, args...)
	return err
}

// UpdateChunkFeatureLabel sets a feature label for the specified chunks.
func (s *SQLiteStore) UpdateChunkFeatureLabel(ids []chunk.ChunkID, label string) error {
	return s.updateChunkFeatureLabel(ids, label)
}

// ClearChunkFeatureLabel clears the feature label for the specified chunks.
func (s *SQLiteStore) ClearChunkFeatureLabel(ids []chunk.ChunkID) error {
	return s.updateChunkFeatureLabel(ids, "")
}

func (s *SQLiteStore) updateChunkFeatureLabel(ids []chunk.ChunkID, label string) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, label)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, string(id))
	}

	query := "UPDATE chunks SET feature_label = ? WHERE id IN (" + strings.Join(placeholders, ",") + ")"
	_, err := s.db.Exec(query, args...)
	return err
}

// ListFeatureLabels returns all distinct non-empty feature labels.
func (s *SQLiteStore) ListFeatureLabels() ([]string, error) {
	rows, err := s.db.Query(`
		SELECT DISTINCT feature_label
		FROM chunks
		WHERE feature_label IS NOT NULL AND TRIM(feature_label) != ''
		ORDER BY feature_label ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var labels []string
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}

	return labels, rows.Err()
}

// scanChunks converts SQL rows into a slice of Chunk structs.
func (s *SQLiteStore) scanChunks(rows *sql.Rows) ([]chunk.Chunk, error) {
	var chunks []chunk.Chunk
	for rows.Next() {
		var c chunk.Chunk
		err := rows.Scan(&c.ID, &c.FilePath, &c.Diff, &c.StartTime, &c.EndTime, &c.FeatureLabel, &c.Hash, &c.Manual)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

// Close closes the SQLite database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
