package store

import (
	"context"

	datasources "github.com/tinywideclouds/go-data-sources/pkg/v1"
	"github.com/tinywideclouds/go-github-store/internal/github"
	urn "github.com/tinywideclouds/go-platform/pkg/net/v1"
)

// DataSourceStore handles the ingestion and retrieval of GitHub repo bundles and files.
type DataSourceStore interface {
	SaveSync(ctx context.Context, dsID urn.URN, repo, branch, commitSHA string, files []github.SyncFile, sendEvent func(stage string, details map[string]any)) error
	CreateDataSource(ctx context.Context, meta *datasources.DataSourceMetadata) error
	GetDataSource(ctx context.Context, dsID urn.URN) (*datasources.DataSourceMetadata, error)
	ListDataSources(ctx context.Context) ([]datasources.DataSourceMetadata, error)
	ListFilesMetadata(ctx context.Context, dsID urn.URN) ([]datasources.FileMetadata, error)
	GetFileContent(ctx context.Context, dsID urn.URN, docID string) (string, error)
}

// ProfileStore handles the filter profiles attached to specific Data Sources.
type ProfileStore interface {
	ListProfiles(ctx context.Context, dsID urn.URN) ([]datasources.Profile, error)
	CreateProfile(ctx context.Context, dsID urn.URN, profile *datasources.Profile) error
	UpdateProfile(ctx context.Context, dsID urn.URN, profile *datasources.Profile) error
	DeleteProfile(ctx context.Context, dsID, profileID urn.URN) error
}

// DataGroupStore handles the Context Blueprints (logical groupings).
type DataGroupStore interface {
	CreateDataGroup(ctx context.Context, group *datasources.DataGroup) error
	GetDataGroup(ctx context.Context, id string) (*datasources.DataGroup, error)
	UpdateDataGroup(ctx context.Context, group *datasources.DataGroup) error
	DeleteDataGroup(ctx context.Context, id string) error
	ListDataGroups(ctx context.Context) ([]datasources.DataGroup, error)
}
