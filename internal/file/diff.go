package file

import (
	"crypto/sha256"
	"fmt"

	"github.com/tinywideclouds/go-github-store/internal/github"
)

// ExistingFile represents the minimal metadata needed from the database to perform a diff.
type ExistingFile struct {
	Path string
	Hash string
}

// DiffResult holds the exact files to write and the paths to delete.
type DiffResult struct {
	ToWrite  []github.SyncFile
	ToDelete []string // Just the generic paths to delete
}

// GenerateHash creates a simple content hash to detect changes without comparing massive strings.
func GenerateHash(content string) string {
	h := sha256.New()
	h.Write([]byte(content))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// CalculateDiff determines which files are new, changed, or deleted by comparing paths and hashes.
// existing: a map where the key is the file Path.
func CalculateDiff(existing map[string]ExistingFile, incoming []github.SyncFile) DiffResult {
	result := DiffResult{
		ToWrite:  make([]github.SyncFile, 0),
		ToDelete: make([]string, 0),
	}

	incomingPaths := make(map[string]bool)

	// 1. Find Creates and Updates
	for _, f := range incoming {
		incomingPaths[f.Path] = true

		newHash := GenerateHash(f.Content)
		existingFile, exists := existing[f.Path]

		// If it doesn't exist, or the content has changed, we must write it
		if !exists || existingFile.Hash != newHash {
			result.ToWrite = append(result.ToWrite, f)
		}
	}

	// 2. Find Deletes (exists in DB, but no longer in incoming tree)
	for path := range existing {
		if !incomingPaths[path] {
			result.ToDelete = append(result.ToDelete, path)
		}
	}

	return result
}
