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

type MockDataGroupStore struct {
	groups map[string]*datasources.DataGroup
}

func NewMockDataGroupStore() *MockDataGroupStore {
	return &MockDataGroupStore{
		groups: make(map[string]*datasources.DataGroup),
	}
}

func (m *MockDataGroupStore) CreateDataGroup(ctx context.Context, group *datasources.DataGroup) error {
	m.groups[group.ID.String()] = group
	return nil
}

func (m *MockDataGroupStore) GetDataGroup(ctx context.Context, id string) (*datasources.DataGroup, error) {
	if group, exists := m.groups[id]; exists {
		return group, nil
	}
	return nil, assert.AnError
}

func TestDataGroupStore_URNMapping(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockDataGroupStore()

	dgURN, _ := urn.Parse("urn:datagroup:111")
	dsURN, _ := urn.Parse("urn:data-source:222")
	profURN, _ := urn.Parse("urn:profile:333")
	now := time.Now().Truncate(time.Second).UTC()

	group := &datasources.DataGroup{
		ID:   dgURN,
		Name: "Test Data Group",
		Sources: []*datasources.DataGroupSource{
			{DataSourceID: dsURN, ProfileID: &profURN},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	err := mockStore.CreateDataGroup(ctx, group)
	require.NoError(t, err)

	retrieved, err := mockStore.GetDataGroup(ctx, dgURN.String())
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	assert.Equal(t, dgURN, retrieved.ID)
	assert.Equal(t, "Test Data Group", retrieved.Name)
	assert.Len(t, retrieved.Sources, 1)
	assert.Equal(t, dsURN, retrieved.Sources[0].DataSourceID)
	assert.Equal(t, &profURN, retrieved.Sources[0].ProfileID)
}
