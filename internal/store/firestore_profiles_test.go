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

type MockProfileStore struct {
	profiles map[string]map[string]*datasources.Profile
}

func NewMockProfileStore() *MockProfileStore {
	return &MockProfileStore{
		profiles: make(map[string]map[string]*datasources.Profile),
	}
}

func (m *MockProfileStore) CreateProfile(ctx context.Context, dsID urn.URN, profile *datasources.Profile) error {
	dsKey := dsID.String()
	if _, exists := m.profiles[dsKey]; !exists {
		m.profiles[dsKey] = make(map[string]*datasources.Profile)
	}
	m.profiles[dsKey][profile.ID.String()] = profile
	return nil
}

func (m *MockProfileStore) ListProfiles(ctx context.Context, dsID urn.URN) ([]datasources.Profile, error) {
	dsKey := dsID.String()
	var results []datasources.Profile
	if dsProfiles, exists := m.profiles[dsKey]; exists {
		for _, p := range dsProfiles {
			results = append(results, *p)
		}
	}
	return results, nil
}

func TestProfileStore_URNMapping(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockProfileStore()

	dsURN, _ := urn.Parse("urn:data-source:999")
	profURN, _ := urn.Parse("urn:profile:abc")
	now := time.Now().Truncate(time.Second).UTC()

	profile := &datasources.Profile{
		ID:        profURN,
		Name:      "Test Profile",
		RulesYaml: "include: []",
		CreatedAt: now,
		UpdatedAt: now,
	}

	err := mockStore.CreateProfile(ctx, dsURN, profile)
	require.NoError(t, err)

	profiles, err := mockStore.ListProfiles(ctx, dsURN)
	require.NoError(t, err)
	require.Len(t, profiles, 1)

	assert.Equal(t, profURN, profiles[0].ID)
	assert.Equal(t, "Test Profile", profiles[0].Name)
}
