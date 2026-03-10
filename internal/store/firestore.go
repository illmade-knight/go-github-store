package store

import (
	"encoding/base64"
	"log/slog"

	"cloud.google.com/go/firestore"
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
