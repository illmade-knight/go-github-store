package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	datasources "github.com/tinywideclouds/go-data-sources/pkg/v1"
	urn "github.com/tinywideclouds/go-platform/pkg/net/v1"
)

// MockDataSourceStore allows us to test handlers and logic that rely on the DB
// without needing a live Firestore connection.
type MockDataSourceStore struct {
	dataSources map[string]*datasources.DataSourceMetadata
}

func NewMockDataSourceStore() *MockDataSourceStore {
	return &MockDataSourceStore{
		dataSources: make(map[string]*datasources.DataSourceMetadata),
	}
}

func (m *MockDataSourceStore) CreateDataSource(ctx context.Context, meta *datasources.DataSourceMetadata) error {
	// Simulate the URN -> String conversion that happens in Firestore
	m.dataSources[meta.ID.String()] = meta
	return nil
}

func (m *MockDataSourceStore) GetDataSource(ctx context.Context, dsID urn.URN) (*datasources.DataSourceMetadata, error) {
	// Simulate the String -> URN retrieval
	meta, exists := m.dataSources[dsID.String()]
	if !exists {
		return nil, assert.AnError // Simulate not found
	}
	return meta, nil
}

func TestDataSourceStore_URNMapping(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockDataSourceStore()

	testURN, err := urn.Parse("urn:data-source:12345")
	require.NoError(t, err)

	now := time.Now().Truncate(time.Second).UTC()

	meta := &datasources.DataSourceMetadata{
		ID:           testURN,
		Repo:         "tinywideclouds/test-repo",
		Branch:       "main",
		Status:       "ready",
		FileCount:    10,
		LastSyncedAt: now,
	}

	// 1. Test Creation
	err = mockStore.CreateDataSource(ctx, meta)
	require.NoError(t, err)

	// Verify internal storage used the string representation
	storedMeta, exists := mockStore.dataSources["urn:data-source:12345"]
	require.True(t, exists)
	assert.Equal(t, "tinywideclouds/test-repo", storedMeta.Repo)

	// 2. Test Retrieval
	retrieved, err := mockStore.GetDataSource(ctx, testURN)
	require.NoError(t, err)

	// Verify the URN identity is preserved perfectly
	assert.Equal(t, testURN, retrieved.ID)
	assert.Equal(t, "ready", retrieved.Status)
	assert.Equal(t, int32(10), retrieved.FileCount)
	assert.True(t, retrieved.LastSyncedAt.Equal(now))
}
