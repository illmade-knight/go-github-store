package store

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/tinywideclouds/go-github-store/internal/file"
	"github.com/tinywideclouds/go-github-store/internal/github"
	"github.com/tinywideclouds/go-llm/pkg/cache/v1"
	"google.golang.org/api/iterator"
)

type FirestoreClient struct {
	client *firestore.Client
	c      cache.StoreCollections
	logger *slog.Logger
}

const MaxBatchSize = 500

func NewFirestoreClient(client *firestore.Client, storeCollections cache.StoreCollections, logger *slog.Logger) *FirestoreClient {
	return &FirestoreClient{client: client, c: storeCollections, logger: logger}
}

// Close gracefully shuts down the underlying Firestore client connection.
func (s *FirestoreClient) Close() error {
	s.logger.Info("Closing Firestore connection")
	return s.client.Close()
}

// GenerateDocID creates a safe Firestore document ID from a file path.
func GenerateDocID(path string) string {
	return base64.URLEncoding.EncodeToString([]byte(path))
}

// GetCache retrieves a single cache bundle's metadata.
func (s *FirestoreClient) GetCache(ctx context.Context, cacheID string) (*CacheMetadata, error) {
	doc, err := s.client.Collection(s.c.BundleCollection).Doc(cacheID).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get cache document: %w", err)
	}

	var meta CacheMetadata
	if err := doc.DataTo(&meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cache metadata: %w", err)
	}
	meta.ID = doc.Ref.ID
	return &meta, nil
}

// CreateCache saves the initial skeleton and analysis to Firestore.
func (s *FirestoreClient) CreateCache(ctx context.Context, meta *CacheMetadata) error {
	// We explicitly omit meta.ID in the struct tags, so we use it as the path here
	ref := s.client.Collection(s.c.BundleCollection).Doc(meta.ID)
	_, err := ref.Set(ctx, meta)
	return err
}

func (s *FirestoreClient) SaveSync(ctx context.Context, cacheID, repo, branch, commitSHA string, files []github.SyncFile, sendEvent func(stage string, details map[string]any)) error {
	s.logger.Info("Starting Firestore sync", "cacheID", cacheID, "incoming_files", len(files))

	if sendEvent != nil {
		sendEvent("db_sync_start", map[string]any{
			"message":        "Starting Firestore sync",
			"incoming_files": len(files),
		})
	}

	bundleRef := s.client.Collection(s.c.BundleCollection).Doc(cacheID)
	filesRef := bundleRef.Collection(s.c.FilesCollection)

	// 1. Fetch existing metadata to compute diff
	existingFiles := make(map[string]file.ExistingFile)
	iter := filesRef.Select("path", "hash").Documents(ctx)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read existing files: %w", err)
		}

		path, _ := doc.DataAt("path")
		hash, _ := doc.DataAt("hash")

		if pathStr, ok := path.(string); ok {
			hashStr, _ := hash.(string)
			existingFiles[pathStr] = file.ExistingFile{Path: pathStr, Hash: hashStr}
		}
	}

	// 2. Delegate to the pure Diffing domain
	diff := file.CalculateDiff(existingFiles, files)
	s.logger.Info("Diff calculated", "to_write", len(diff.ToWrite), "to_delete", len(diff.ToDelete))

	if sendEvent != nil {
		sendEvent("diff_calculated", map[string]any{
			"message":   "Diff calculated",
			"to_write":  len(diff.ToWrite),
			"to_delete": len(diff.ToDelete),
		})
	}

	// We only update the sync-related fields to avoid overwriting the Analysis
	updates := map[string]interface{}{
		"syncedCommitSha": commitSHA,
		"lastSyncedAt":    time.Now().Unix(),
		"fileCount":       len(files),
		"status":          "ready",
	}

	return s.executeBatches(ctx, bundleRef, filesRef, diff, updates)
}

func (s *FirestoreClient) executeBatches(ctx context.Context, bundleRef *firestore.DocumentRef, filesRef *firestore.CollectionRef, diff file.DiffResult, updates map[string]interface{}) error {
	batch := s.client.Batch()
	opCount := 0

	// Use MergeAll to protect the existing Analysis payload from being erased
	batch.Set(bundleRef, updates, firestore.MergeAll)
	opCount++

	commitBatch := func() error {
		if _, err := batch.Commit(ctx); err != nil {
			return err
		}
		batch = s.client.Batch()
		opCount = 0
		return nil
	}

	for _, f := range diff.ToWrite {
		docID := GenerateDocID(f.Path)
		docData := FileDoc{
			Path:      f.Path,
			Content:   f.Content,
			SizeBytes: f.SizeBytes,
			Status:    f.Status,
			Extension: f.Extension,
			Hash:      file.GenerateHash(f.Content),
		}

		batch.Set(filesRef.Doc(docID), docData)
		opCount++
		if opCount == MaxBatchSize {
			if err := commitBatch(); err != nil {
				return fmt.Errorf("failed committing write batch: %w", err)
			}
		}
	}

	for _, path := range diff.ToDelete {
		docID := GenerateDocID(path)
		batch.Delete(filesRef.Doc(docID))
		opCount++
		if opCount == MaxBatchSize {
			if err := commitBatch(); err != nil {
				return fmt.Errorf("failed committing delete batch: %w", err)
			}
		}
	}

	if opCount > 0 {
		if err := commitBatch(); err != nil {
			return fmt.Errorf("failed committing final batch: %w", err)
		}
	}

	return nil
}

// ListCaches retrieves all synced repository bundles.
func (s *FirestoreClient) ListCaches(ctx context.Context) ([]CacheMetadata, error) {
	s.logger.Debug("Executing Firestore query for CacheBundles", "collection", s.c.BundleCollection)

	var caches []CacheMetadata
	iter := s.client.Collection(s.c.BundleCollection).OrderBy("lastSyncedAt", firestore.Desc).Documents(ctx)

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			s.logger.Error("Firestore iteration failed", "error", err)
			return nil, fmt.Errorf("failed to iterate caches: %w", err)
		}

		var cache CacheMetadata
		if err := doc.DataTo(&cache); err != nil {
			s.logger.Warn("Failed to unmarshal cache metadata", "docID", doc.Ref.ID, "error", err)
			continue
		}
		cache.ID = doc.Ref.ID
		caches = append(caches, cache)
	}

	s.logger.Info("Successfully retrieved caches", "count", len(caches))

	if caches == nil {
		s.logger.Debug("No caches found, returning empty array")
		return []CacheMetadata{}, nil
	}

	return caches, nil
}

// ListFilesMetadata retrieves the files for a cache WITHOUT the heavy content payload.
func (s *FirestoreClient) ListFilesMetadata(ctx context.Context, cacheID string) ([]FileMetadata, error) {
	var files []FileMetadata

	// STRICT PROJECTION: Only request the lightweight fields.
	// This prevents crashing the Go service when scanning 1,000+ files.
	iter := s.client.Collection(s.c.BundleCollection).Doc(cacheID).
		Collection(s.c.FilesCollection).
		Select("path", "sizeBytes", "extension").
		Documents(ctx)

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list file metadata: %w", err)
		}

		var meta FileMetadata
		if err := doc.DataTo(&meta); err != nil {
			s.logger.Warn("Failed to unmarshal file metadata", "docID", doc.Ref.ID, "error", err)
			continue
		}
		files = append(files, meta)
	}
	return files, nil
}

// --- Profile Management ---

func (s *FirestoreClient) ListProfiles(ctx context.Context, cacheID string) ([]Profile, error) {
	var profiles []Profile
	iter := s.client.Collection(s.c.BundleCollection).Doc(cacheID).Collection(s.c.ProfilesCollection).Documents(ctx)

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list profiles: %w", err)
		}

		var p Profile
		if err := doc.DataTo(&p); err != nil {
			s.logger.Warn("Failed to unmarshal profile", "docID", doc.Ref.ID, "error", err)
			continue
		}
		p.ID = doc.Ref.ID
		profiles = append(profiles, p)
	}
	return profiles, nil
}

func (s *FirestoreClient) CreateProfile(ctx context.Context, cacheID string, profile *Profile) error {
	ref := s.client.Collection(s.c.BundleCollection).Doc(cacheID).Collection(s.c.ProfilesCollection).Doc(profile.ID)
	_, err := ref.Set(ctx, profile)
	return err
}

func (s *FirestoreClient) UpdateProfile(ctx context.Context, cacheID string, profile *Profile) error {
	ref := s.client.Collection(s.c.BundleCollection).Doc(cacheID).Collection(s.c.ProfilesCollection).Doc(profile.ID)
	_, err := ref.Set(ctx, profile, firestore.MergeAll)
	return err
}

func (s *FirestoreClient) DeleteProfile(ctx context.Context, cacheID, profileID string) error {
	ref := s.client.Collection(s.c.BundleCollection).Doc(cacheID).Collection(s.c.ProfilesCollection).Doc(profileID)
	_, err := ref.Delete(ctx)
	return err
}

// GetFileContent performs a direct O(1) lookup of a specific file document and returns only its text content.
func (s *FirestoreClient) GetFileContent(ctx context.Context, cacheID string, docID string) (string, error) {
	doc, err := s.client.Collection(s.c.BundleCollection).
		Doc(cacheID).
		Collection(s.c.FilesCollection).
		Doc(docID).
		Get(ctx)

	if err != nil {
		return "", fmt.Errorf("failed to fetch file document: %w", err)
	}

	content, err := doc.DataAt("content")
	if err != nil {
		return "", fmt.Errorf("failed to extract content field: %w", err)
	}

	contentStr, ok := content.(string)
	if !ok {
		return "", fmt.Errorf("content field is not a string")
	}

	return contentStr, nil
}
