package file_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tinywideclouds/go-github-store/internal/file"
	"github.com/tinywideclouds/go-github-store/internal/github"
)

func TestCalculateDiff(t *testing.T) {
	existing := map[string]file.ExistingFile{
		"unchanged.go": {Path: "unchanged.go", Hash: file.GenerateHash("A")},
		"changed.go":   {Path: "changed.go", Hash: file.GenerateHash("B")},
		"deleted.go":   {Path: "deleted.go", Hash: file.GenerateHash("C")},
	}

	incoming := []github.SyncFile{
		{Path: "unchanged.go", Content: "A"},   // Exists, content same -> Ignore
		{Path: "changed.go", Content: "B_NEW"}, // Exists, content diff -> Update
		{Path: "new_file.go", Content: "D"},    // Not exists -> Create
	}

	diff := file.CalculateDiff(existing, incoming)

	// --- Assertions ---

	// Should have 2 writes (1 update, 1 create)
	assert.Len(t, diff.ToWrite, 2)

	var wroteChanged, wroteNew bool
	for _, f := range diff.ToWrite {
		if f.Path == "changed.go" {
			assert.Equal(t, "B_NEW", f.Content)
			wroteChanged = true
		}
		if f.Path == "new_file.go" {
			assert.Equal(t, "D", f.Content)
			wroteNew = true
		}
	}
	assert.True(t, wroteChanged, "Should write changed.go")
	assert.True(t, wroteNew, "Should write new_file.go")

	// Should have 1 delete ("deleted.go" was not in incoming)
	assert.Len(t, diff.ToDelete, 1)
	assert.Equal(t, "deleted.go", diff.ToDelete[0])
}
