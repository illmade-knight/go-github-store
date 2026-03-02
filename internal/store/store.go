package store

import (
	"context"
	"time"

	"github.com/tinywideclouds/go-github-store/internal/github"
	"github.com/tinywideclouds/go-llm/pkg/yaml/filter"
)

// Profile represents a saved filter configuration for a specific cache bundle.
type Profile struct {
	ID        string    `json:"id" firestore:"-"`
	Name      string    `json:"name" firestore:"name"`
	RulesYaml string    `json:"rulesYaml" firestore:"rulesYaml"`
	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`
}

// CacheAnalysis holds the pre-sync metadata calculated from the GitHub tree.
type CacheAnalysis struct {
	TotalFiles     int            `json:"totalFiles" firestore:"totalFiles"`
	TotalSizeBytes int            `json:"totalSizeBytes" firestore:"totalSizeBytes"`
	Extensions     map[string]int `json:"extensions" firestore:"extensions"`
}

// CacheMetadata represents the summary of a synced repository.
type CacheMetadata struct {
	ID              string        `json:"id" firestore:"-"`
	Repo            string        `json:"repo" firestore:"repo"`
	Branch          string        `json:"branch" firestore:"branch"`
	SyncedCommitSHA string        `json:"syncedCommitSha" firestore:"syncedCommitSha"` // Used for drift checking
	LastSyncedAt    int64         `json:"lastSyncedAt" firestore:"lastSyncedAt"`
	FileCount       int           `json:"fileCount" firestore:"fileCount"`
	Status          string        `json:"status" firestore:"status"` // e.g., "unsynced", "syncing", "ready", "failed"
	Analysis        CacheAnalysis `json:"analysis" firestore:"analysis"`
}

// FileMetadata represents a file without its heavy content payload.
type FileMetadata struct {
	Path      string `json:"path" firestore:"path"`
	SizeBytes int    `json:"sizeBytes" firestore:"sizeBytes"`
	Extension string `json:"extension" firestore:"extension"`
}

// BundleMetadata is the internal Firestore representation of the root cache document.
type BundleMetadata struct {
	Repo            string             `firestore:"repo"`
	Branch          string             `firestore:"branch"`
	SyncedCommitSHA string             `firestore:"syncedCommitSha"`
	LastSyncedAt    int64              `firestore:"lastSyncedAt"`
	FileCount       int                `firestore:"fileCount"`
	Status          string             `firestore:"status"`
	Analysis        CacheAnalysis      `firestore:"analysis"`
	IngestionRules  filter.FilterRules `firestore:"ingestionRules"`
}

type FileDoc struct {
	Path      string `firestore:"path"`
	Content   string `firestore:"content"`
	SizeBytes int    `firestore:"sizeBytes"`
	Status    string `firestore:"status"`
	Extension string `firestore:"extension"`
	Hash      string `firestore:"hash"`
}

// Store defines the agnostic contract for persisting and reading repository data.
type Store interface {
	// Write Operations

	SaveSync(ctx context.Context, cacheID, repo, branch, commitSHA string, files []github.SyncFile, sendEvent func(stage string, details map[string]any)) error
	CreateCache(ctx context.Context, meta *CacheMetadata) error

	// Read Operations
	GetCache(ctx context.Context, cacheID string) (*CacheMetadata, error)
	ListCaches(ctx context.Context) ([]CacheMetadata, error)
	ListFilesMetadata(ctx context.Context, cacheID string) ([]FileMetadata, error)
	GetFileContent(ctx context.Context, cacheID string, docID string) (string, error)

	// Profile Management
	ListProfiles(ctx context.Context, cacheID string) ([]Profile, error)
	CreateProfile(ctx context.Context, cacheID string, profile *Profile) error
	UpdateProfile(ctx context.Context, cacheID string, profile *Profile) error
	DeleteProfile(ctx context.Context, cacheID, profileID string) error

	// Lifecycle Management
	Close() error
}
