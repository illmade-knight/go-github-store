package store

import (
	datasources "github.com/tinywideclouds/go-data-sources/pkg/v1"
)

// StoreCollections holds Firestore configuration
type StoreCollections struct {
	BundleCollection     string
	FilesCollection      string
	ProfilesCollection   string
	DatagroupsCollection string
}

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

// DataGroupDoc is the internal representation for Firestore persistence
type DataGroupDoc struct {
	Name        string               `firestore:"name"`
	Description *string              `firestore:"description,omitempty"`
	Sources     []DataGroupSourceDoc `firestore:"sources"`
	Metadata    map[string]string    `firestore:"metadata,omitempty"` // Opaque K/V store for LLM domain
	CreatedAt   int64                `firestore:"createdAt"`          // Unix timestamp for consistency
	UpdatedAt   int64                `firestore:"updatedAt"`
}

type DataGroupSourceDoc struct {
	DataSourceID string  `firestore:"dataSourceId"`
	ProfileID    *string `firestore:"profileId,omitempty"`
}
