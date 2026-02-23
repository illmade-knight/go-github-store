package store

import (
	"context"

	"github.com/tinywideclouds/go-github-store/internal/github"
)

// Store defines the agnostic contract for persisting synced repository data.
// Any database implementation (Firestore, Postgres, MemoryMock) must satisfy this interface.
type Store interface {
	SaveSync(ctx context.Context, cacheID, repo, branch string, files []github.SyncFile) error
}
