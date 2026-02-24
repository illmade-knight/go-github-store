package store

import (
	"context"
	"time"

	"github.com/tinywideclouds/go-github-store/internal/github"
)

const (
	BundleCollection   = "CacheBundles"
	FilesCollection    = "Files"
	ProfilesCollection = "FilterProfiles"
	MaxBatchSize       = 500 // Firestore hard limit
)

// FilterRules represents the parsed YAML structure for inclusion/exclusion logic.
type FilterRules struct {
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
}

// Profile represents a saved filter configuration for a specific cache bundle.
type Profile struct {
	ID        string    `json:"id" firestore:"-"` // Firestore Document ID
	Name      string    `json:"name" firestore:"name"`
	RulesYaml string    `json:"rulesYaml" firestore:"rulesYaml"`
	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`
}

// CacheMetadata represents the summary of a synced repository.
type CacheMetadata struct {
	ID           string `json:"id" firestore:"-"`
	Repo         string `json:"repo" firestore:"repo"`
	Branch       string `json:"branch" firestore:"branch"`
	LastSyncedAt int64  `json:"lastSyncedAt" firestore:"lastSyncedAt"`
	FileCount    int    `json:"fileCount" firestore:"fileCount"`
	Status       string `json:"status" firestore:"status"`
}

// FileMetadata represents a file without its heavy content payload.
type FileMetadata struct {
	Path      string `json:"path" firestore:"path"`
	SizeBytes int    `json:"sizeBytes" firestore:"sizeBytes"`
	Extension string `json:"extension" firestore:"extension"`
}

type BundleMetadata struct {
	Repo         string `firestore:"repo"`
	Branch       string `firestore:"branch"`
	LastSyncedAt int64  `firestore:"lastSyncedAt"`
	FileCount    int    `firestore:"fileCount"`
	Status       string `firestore:"status"`
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
	// Write Operations (Existing)
	SaveSync(ctx context.Context, cacheID, repo, branch string, files []github.SyncFile) error

	// Read Operations (New)
	ListCaches(ctx context.Context) ([]CacheMetadata, error)
	ListFilesMetadata(ctx context.Context, cacheID string) ([]FileMetadata, error)

	// Profile Management (New)
	ListProfiles(ctx context.Context, cacheID string) ([]Profile, error)
	CreateProfile(ctx context.Context, cacheID string, profile *Profile) error
	UpdateProfile(ctx context.Context, cacheID string, profile *Profile) error
	DeleteProfile(ctx context.Context, cacheID, profileID string) error

	// Lifecycle Management
	Close() error
}
