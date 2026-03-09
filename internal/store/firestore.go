package store

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"time"

	"cloud.google.com/go/firestore"

	datasources "github.com/tinywideclouds/go-data-sources/pkg/v1"
	"github.com/tinywideclouds/go-github-store/internal/file"
	"github.com/tinywideclouds/go-github-store/internal/github"
	urn "github.com/tinywideclouds/go-platform/pkg/net/v1"
	"google.golang.org/api/iterator"
)

type FirestoreClient struct {
	client *firestore.Client
	c      StoreCollections
	logger *slog.Logger
}

const MaxBatchSize = 500

func NewFirestoreClient(client *firestore.Client, collections StoreCollections, logger *slog.Logger) *FirestoreClient {
	return &FirestoreClient{client: client, c: collections, logger: logger}
}

func (s *FirestoreClient) Close() error {
	s.logger.Info("Closing Firestore connection")
	return s.client.Close()
}

func GenerateDocID(path string) string {
	return base64.URLEncoding.EncodeToString([]byte(path))
}

// --- DATA SOURCE METADATA ---

func (s *FirestoreClient) GetDataSource(ctx context.Context, dsID urn.URN) (*datasources.DataSourceMetadata, error) {
	doc, err := s.client.Collection(s.c.BundleCollection).Doc(dsID.String()).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get data source document: %w", err)
	}

	var meta BundleMetadata
	if err := doc.DataTo(&meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal internal metadata: %w", err)
	}

	lastSynced := time.Time{}
	if meta.LastSyncedAt > 0 {
		lastSynced = time.Unix(meta.LastSyncedAt, 0)
	}

	domainMeta := &datasources.DataSourceMetadata{
		ID:              dsID.String(),
		Repo:            meta.Repo,
		Branch:          meta.Branch,
		SyncedCommitSha: meta.SyncedCommitSHA,
		FileCount:       meta.FileCount,
		Status:          meta.Status,
		Analysis:        &meta.Analysis,
		LastSyncedAt:    lastSynced,
	}

	return domainMeta, nil
}

func (s *FirestoreClient) CreateDataSource(ctx context.Context, meta *datasources.DataSourceMetadata) error {
	ref := s.client.Collection(s.c.BundleCollection).Doc(meta.ID)

	dbMeta := BundleMetadata{
		Repo:      meta.Repo,
		Branch:    meta.Branch,
		Status:    meta.Status,
		FileCount: meta.FileCount,
	}
	if meta.Analysis != nil {
		dbMeta.Analysis = *meta.Analysis
	}

	_, err := ref.Set(ctx, dbMeta)
	return err
}

func (s *FirestoreClient) SaveSync(ctx context.Context, dsID urn.URN, repo, branch, commitSHA string, files []github.SyncFile, sendEvent func(stage string, details map[string]any)) error {
	s.logger.Info("Starting Firestore sync", "dsID", dsID.String(), "incoming_files", len(files))

	if sendEvent != nil {
		sendEvent("db_sync_start", map[string]any{
			"message":        "Starting Firestore sync",
			"incoming_files": len(files),
		})
	}

	bundleRef := s.client.Collection(s.c.BundleCollection).Doc(dsID.String())
	filesRef := bundleRef.Collection(s.c.FilesCollection)

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

	diff := file.CalculateDiff(existingFiles, files)
	s.logger.Info("Diff calculated", "to_write", len(diff.ToWrite), "to_delete", len(diff.ToDelete))

	if sendEvent != nil {
		sendEvent("diff_calculated", map[string]any{
			"message":   "Diff calculated",
			"to_write":  len(diff.ToWrite),
			"to_delete": len(diff.ToDelete),
		})
	}

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
			SizeBytes: int32(f.SizeBytes),
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

func (s *FirestoreClient) ListDataSources(ctx context.Context) ([]datasources.DataSourceMetadata, error) {
	s.logger.Debug("Executing Firestore query for Data Sources", "collection", s.c.BundleCollection)

	var sources []datasources.DataSourceMetadata
	iter := s.client.Collection(s.c.BundleCollection).OrderBy("lastSyncedAt", firestore.Desc).Documents(ctx)

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			s.logger.Error("Firestore iteration failed", "error", err)
			return nil, fmt.Errorf("failed to iterate data sources: %w", err)
		}

		var meta BundleMetadata
		if err := doc.DataTo(&meta); err != nil {
			s.logger.Warn("Failed to unmarshal data source metadata", "docID", doc.Ref.ID, "error", err)
			continue
		}

		lastSynced := time.Time{}
		if meta.LastSyncedAt > 0 {
			lastSynced = time.Unix(meta.LastSyncedAt, 0)
		}

		sources = append(sources, datasources.DataSourceMetadata{
			ID:              doc.Ref.ID,
			Repo:            meta.Repo,
			Branch:          meta.Branch,
			SyncedCommitSha: meta.SyncedCommitSHA,
			FileCount:       meta.FileCount,
			Status:          meta.Status,
			Analysis:        &meta.Analysis,
			LastSyncedAt:    lastSynced,
		})
	}

	s.logger.Info("Successfully retrieved data sources", "count", len(sources))

	if sources == nil {
		s.logger.Debug("No data sources found, returning empty array")
		return []datasources.DataSourceMetadata{}, nil
	}

	return sources, nil
}

func (s *FirestoreClient) ListFilesMetadata(ctx context.Context, dsID urn.URN) ([]datasources.FileMetadata, error) {
	var files []datasources.FileMetadata

	iter := s.client.Collection(s.c.BundleCollection).Doc(dsID.String()).
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

		var meta datasources.FileMetadata
		if err := doc.DataTo(&meta); err != nil {
			s.logger.Warn("Failed to unmarshal file metadata", "docID", doc.Ref.ID, "error", err)
			continue
		}
		files = append(files, meta)
	}
	return files, nil
}

// --- Profile Management ---

func (s *FirestoreClient) ListProfiles(ctx context.Context, dsID urn.URN) ([]datasources.Profile, error) {
	var profiles []datasources.Profile
	iter := s.client.Collection(s.c.BundleCollection).Doc(dsID.String()).Collection(s.c.ProfilesCollection).Documents(ctx)

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list profiles: %w", err)
		}

		var p datasources.Profile
		if err := doc.DataTo(&p); err != nil {
			s.logger.Warn("Failed to unmarshal profile", "docID", doc.Ref.ID, "error", err)
			continue
		}
		p.ID = doc.Ref.ID
		profiles = append(profiles, p)
	}
	return profiles, nil
}

func (s *FirestoreClient) CreateProfile(ctx context.Context, dsID urn.URN, profile *datasources.Profile) error {
	ref := s.client.Collection(s.c.BundleCollection).Doc(dsID.String()).Collection(s.c.ProfilesCollection).Doc(profile.ID)
	_, err := ref.Set(ctx, profile)
	return err
}

func (s *FirestoreClient) UpdateProfile(ctx context.Context, dsID urn.URN, profile *datasources.Profile) error {
	ref := s.client.Collection(s.c.BundleCollection).Doc(dsID.String()).Collection(s.c.ProfilesCollection).Doc(profile.ID)
	_, err := ref.Set(ctx, profile, firestore.MergeAll)
	return err
}

func (s *FirestoreClient) DeleteProfile(ctx context.Context, dsID, profileID urn.URN) error {
	ref := s.client.Collection(s.c.BundleCollection).Doc(dsID.String()).Collection(s.c.ProfilesCollection).Doc(profileID.String())
	_, err := ref.Delete(ctx)
	return err
}

func (s *FirestoreClient) GetFileContent(ctx context.Context, dsID urn.URN, docID string) (string, error) {
	doc, err := s.client.Collection(s.c.BundleCollection).
		Doc(dsID.String()).
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
