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
	"google.golang.org/api/iterator"
)

const (
	BundleCollection = "CacheBundles"
	FilesCollection  = "Files"
	MaxBatchSize     = 500 // Firestore hard limit
)

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

type FirestoreClient struct {
	client *firestore.Client
	logger *slog.Logger
}

func NewFirestoreClient(client *firestore.Client, logger *slog.Logger) *FirestoreClient {
	return &FirestoreClient{client: client, logger: logger}
}

// GenerateDocID creates a safe Firestore document ID from a file path.
func GenerateDocID(path string) string {
	return base64.URLEncoding.EncodeToString([]byte(path))
}

func (s *FirestoreClient) SaveSync(ctx context.Context, cacheID, repo, branch string, files []github.SyncFile) error {
	s.logger.Info("Starting Firestore sync", "cacheID", cacheID, "incoming_files", len(files))

	bundleRef := s.client.Collection(BundleCollection).Doc(cacheID)
	filesRef := bundleRef.Collection(FilesCollection)

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

	metadata := BundleMetadata{
		Repo:         repo,
		Branch:       branch,
		LastSyncedAt: time.Now().Unix(),
		FileCount:    len(files),
		Status:       "ready",
	}

	return s.executeBatches(ctx, bundleRef, filesRef, diff, metadata)
}

func (s *FirestoreClient) executeBatches(ctx context.Context, bundleRef *firestore.DocumentRef, filesRef *firestore.CollectionRef, diff file.DiffResult, metadata BundleMetadata) error {
	batch := s.client.Batch()
	opCount := 0

	batch.Set(bundleRef, metadata)
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
