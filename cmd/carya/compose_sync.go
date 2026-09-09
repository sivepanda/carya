package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	composeChunkSyncInterval = 30 * time.Minute
	composeChunkSyncStamp    = "chunk-sync.last"
)

func maybeAutoChunkSync(repoPath, caryaPath, dbPath string) (syncResult, bool, error) {
	now := time.Now()
	stampPath := filepath.Join(caryaPath, composeChunkSyncStamp)

	if lastRun, ok := readChunkSyncStamp(stampPath); ok {
		if now.Sub(lastRun) < composeChunkSyncInterval {
			return syncResult{}, false, nil
		}
	}

	res, err := runChunkSync(repoPath, dbPath)
	if err != nil {
		return syncResult{}, true, err
	}

	if err := writeChunkSyncStamp(stampPath, now); err != nil {
		log.Printf("failed to update chunk sync stamp: %v", err)
	}

	return res, true, nil
}

func readChunkSyncStamp(path string) (time.Time, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, false
	}

	ts := strings.TrimSpace(string(data))
	if ts == "" {
		return time.Time{}, false
	}

	parsed, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}, false
	}

	return parsed, true
}

func writeChunkSyncStamp(path string, ts time.Time) error {
	return os.WriteFile(path, []byte(ts.Format(time.RFC3339Nano)+"\n"), 0644)
}
