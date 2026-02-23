package store_test

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tinywideclouds/go-github-store/internal/store"
)

func TestGenerateDocID(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"Root file", "main.go"},
		{"Nested file", "pkg/server/handler.go"},
		{"Deeply nested with symbols", "internal/domain/models/user-profile.ts"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := store.GenerateDocID(tt.path)

			// 1. Ensure the ID contains no forward slashes (which breaks Firestore)
			assert.NotContains(t, id, "/")

			// 2. Ensure it can be perfectly decoded back to the original path
			decoded, err := base64.URLEncoding.DecodeString(id)
			assert.NoError(t, err, "Should be valid URL-encoded base64")
			assert.Equal(t, tt.path, string(decoded), "Decoded path should match original")
		})
	}
}
