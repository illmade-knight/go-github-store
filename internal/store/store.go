package store

import (
	"context"

	datasources "github.com/tinywideclouds/go-data-sources/pkg/v1"
	"github.com/tinywideclouds/go-github-store/internal/github"
	urn "github.com/tinywideclouds/go-platform/pkg/net/v1"
)

// --- INTERNAL FIRESTORE MODELS ---

// BundleMetadata is the strict internal representation for Firestore
type BundleMetadata struct {
	Repo            string                         `firestore:"repo"`
	Branch          string                         `firestore:"branch"`
	SyncedCommitSHA string                         `firestore:"syncedCommitSha"`
	LastSyncedAt    int64                          `firestore:"lastSyncedAt"`
	FileCount       int32                          `firestore:"fileCount"`
	Status          string                         `firestore:"status"`
	Analysis        datasources.DataSourceAnalysis `firestore:"analysis"`
}

type FileDoc struct {
	Path      string `firestore:"path"`
	Content   string `firestore:"content"`
	SizeBytes int32  `firestore:"sizeBytes"`
	Status    string `firestore:"status"`
	Extension string `firestore:"extension"`
	Hash      string `firestore:"hash"`
}

// Store defines the agnostic contract for persisting Data Sources.
type Store interface {
	// Write Operations
	SaveSync(ctx context.Context, dsID urn.URN, repo, branch, commitSHA string, files []github.SyncFile, sendEvent func(stage string, details map[string]any)) error
	CreateDataSource(ctx context.Context, meta *datasources.DataSourceMetadata) error

	// Read Operations
	GetDataSource(ctx context.Context, dsID urn.URN) (*datasources.DataSourceMetadata, error)
	ListDataSources(ctx context.Context) ([]datasources.DataSourceMetadata, error)
	ListFilesMetadata(ctx context.Context, dsID urn.URN) ([]datasources.FileMetadata, error)
	GetFileContent(ctx context.Context, dsID urn.URN, docID string) (string, error)

	// Profile Management
	ListProfiles(ctx context.Context, dsID urn.URN) ([]datasources.Profile, error)
	CreateProfile(ctx context.Context, dsID urn.URN, profile *datasources.Profile) error
	UpdateProfile(ctx context.Context, dsID urn.URN, profile *datasources.Profile) error
	DeleteProfile(ctx context.Context, dsID, profileID urn.URN) error

	// Lifecycle Management
	Close() error
}
