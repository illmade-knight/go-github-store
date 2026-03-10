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

// ProfileDoc is the internal representation for Firestore persistence
type ProfileDoc struct {
	Name      string    `firestore:"name"`
	RulesYaml string    `firestore:"rulesYaml"`
	CreatedAt time.Time `firestore:"createdAt"`
	UpdatedAt time.Time `firestore:"updatedAt"`
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

		var pDoc ProfileDoc
		if err := doc.DataTo(&pDoc); err != nil {
			s.logger.Warn("Failed to unmarshal profile", "docID", doc.Ref.ID, "error", err)
			continue
		}

		profID, err := urn.Parse(doc.Ref.ID)
		if err != nil {
			s.logger.Warn("Invalid URN found in profile document ID", "docID", doc.Ref.ID, "error", err)
			continue
		}

		profiles = append(profiles, datasources.Profile{
			ID:        profID,
			Name:      pDoc.Name,
			RulesYaml: pDoc.RulesYaml,
			CreatedAt: pDoc.CreatedAt,
			UpdatedAt: pDoc.UpdatedAt,
		})
	}
	return profiles, nil
}

func (s *FirestoreClient) CreateProfile(ctx context.Context, dsID urn.URN, profile *datasources.Profile) error {
	ref := s.client.Collection(s.c.BundleCollection).Doc(dsID.String()).Collection(s.c.ProfilesCollection).Doc(profile.ID.String())

	pDoc := ProfileDoc{
		Name:      profile.Name,
		RulesYaml: profile.RulesYaml,
		CreatedAt: profile.CreatedAt,
		UpdatedAt: profile.UpdatedAt,
	}

	_, err := ref.Set(ctx, pDoc)
	return err
}

func (s *FirestoreClient) UpdateProfile(ctx context.Context, dsID urn.URN, profile *datasources.Profile) error {
	ref := s.client.Collection(s.c.BundleCollection).Doc(dsID.String()).Collection(s.c.ProfilesCollection).Doc(profile.ID.String())

	pDoc := ProfileDoc{
		Name:      profile.Name,
		RulesYaml: profile.RulesYaml,
		CreatedAt: profile.CreatedAt, // Note: You might want to omit this if only updating specific fields, but assuming full overwrite for now
		UpdatedAt: profile.UpdatedAt,
	}

	_, err := ref.Set(ctx, pDoc, firestore.MergeAll)
	return err
}

func (s *FirestoreClient) DeleteProfile(ctx context.Context, dsID, profileID urn.URN) error {
	ref := s.client.Collection(s.c.BundleCollection).Doc(dsID.String()).Collection(s.c.ProfilesCollection).Doc(profileID.String())
	_, err := ref.Delete(ctx)
	return err
}
