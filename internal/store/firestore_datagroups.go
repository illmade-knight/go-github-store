package store

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	datasources "github.com/tinywideclouds/go-data-sources/pkg/v1"
	urn "github.com/tinywideclouds/go-platform/pkg/net/v1"
	"google.golang.org/api/iterator"
)

// ListDataGroups retrieves all data groups, ordered by most recently updated.
func (s *FirestoreClient) ListDataGroups(ctx context.Context) ([]datasources.DataGroup, error) {
	var groups []datasources.DataGroup
	iter := s.client.Collection(s.c.DatagroupsCollection).OrderBy("updatedAt", firestore.Desc).Documents(ctx)

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list data groups: %w", err)
		}

		var dbGroup DataGroupDoc
		if err := doc.DataTo(&dbGroup); err != nil {
			s.logger.Warn("Failed to unmarshal data group", "docID", doc.Ref.ID, "error", err)
			continue
		}

		groups = append(groups, mapDBToDataGroup(doc.Ref.ID, dbGroup))
	}

	if groups == nil {
		return []datasources.DataGroup{}, nil
	}
	return groups, nil
}

// GetDataGroup retrieves a single data group by ID.
func (s *FirestoreClient) GetDataGroup(ctx context.Context, id string) (*datasources.DataGroup, error) {
	doc, err := s.client.Collection(s.c.DatagroupsCollection).Doc(id).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get data group: %w", err)
	}

	var dbGroup DataGroupDoc
	if err := doc.DataTo(&dbGroup); err != nil {
		return nil, fmt.Errorf("failed to unmarshal data group: %w", err)
	}

	group := mapDBToDataGroup(id, dbGroup)
	return &group, nil
}

// CreateDataGroup persists a new Data Group.
func (s *FirestoreClient) CreateDataGroup(ctx context.Context, group *datasources.DataGroup) error {
	ref := s.client.Collection(s.c.DatagroupsCollection).Doc(group.ID.String())
	dbGroup := mapDataGroupToDB(group)
	_, err := ref.Set(ctx, dbGroup)
	return err
}

// UpdateDataGroup modifies an existing Data Group.
func (s *FirestoreClient) UpdateDataGroup(ctx context.Context, group *datasources.DataGroup) error {
	ref := s.client.Collection(s.c.DatagroupsCollection).Doc(group.ID.String())
	dbGroup := mapDataGroupToDB(group)

	// Use MergeAll to only update provided fields
	_, err := ref.Set(ctx, dbGroup, firestore.MergeAll)
	return err
}

// DeleteDataGroup removes a Data Group.
// CRITICAL CONSTRAINT: Delete is explicitly non-cascading. Underlying sources are untouched.
func (s *FirestoreClient) DeleteDataGroup(ctx context.Context, id string) error {
	ref := s.client.Collection(s.c.DatagroupsCollection).Doc(id)
	_, err := ref.Delete(ctx)
	return err
}

// --- Internal Mapping Helpers ---

func mapDBToDataGroup(id string, db DataGroupDoc) datasources.DataGroup {
	dgID, _ := urn.Parse(id)

	dg := datasources.DataGroup{
		ID:          dgID,
		Name:        db.Name,
		Description: db.Description,
		Metadata:    db.Metadata,
		CreatedAt:   time.Unix(db.CreatedAt, 0),
		UpdatedAt:   time.Unix(db.UpdatedAt, 0),
	}

	for _, s := range db.Sources {
		dsID, _ := urn.Parse(s.DataSourceID)

		var profID *urn.URN
		if s.ProfileID != nil {
			if pid, err := urn.Parse(*s.ProfileID); err == nil {
				profID = &pid
			}
		}

		dg.Sources = append(dg.Sources, &datasources.DataGroupSource{
			DataSourceID: dsID,
			ProfileID:    profID,
		})
	}
	return dg
}

func mapDataGroupToDB(dg *datasources.DataGroup) DataGroupDoc {
	db := DataGroupDoc{
		Name:        dg.Name,
		Description: dg.Description,
		Metadata:    dg.Metadata,
		CreatedAt:   dg.CreatedAt.Unix(),
		UpdatedAt:   dg.UpdatedAt.Unix(),
	}

	for _, s := range dg.Sources {
		var profIDStr *string
		if s.ProfileID != nil {
			str := s.ProfileID.String()
			profIDStr = &str
		}

		db.Sources = append(db.Sources, DataGroupSourceDoc{
			DataSourceID: s.DataSourceID.String(),
			ProfileID:    profIDStr,
		})
	}
	return db
}
