package diff

import (
	"fmt"

	"github.com/gabstv/go-bsdiff/pkg/bsdiff"
	"github.com/gabstv/go-bsdiff/pkg/bspatch"
)

// BsdiffEngine implements the DiffEngine interface using bsdiff
type BsdiffEngine struct{}

// NewBsdiffEngine creates a new bsdiff-based diff engine
func NewBsdiffEngine() *BsdiffEngine {
	return &BsdiffEngine{}
}

// Name returns the name of the engine
func (e *BsdiffEngine) Name() string {
	return "bsdiff"
}

// ComputeDiff computes a binary diff using bsdiff
func (e *BsdiffEngine) ComputeDiff(oldData, newData []byte) ([]byte, error) {
	if len(oldData) == 0 && len(newData) == 0 {
		return []byte{}, nil
	}

	// Handle edge case: empty old data (new file)
	if len(oldData) == 0 {
		// For new files, we can't compute a diff, so return the new data itself
		// This will be handled as a special case (snapshot)
		return newData, nil
	}

	// Compute diff using bsdiff.Bytes
	patch, err := bsdiff.Bytes(oldData, newData)
	if err != nil {
		return nil, fmt.Errorf("bsdiff computation failed: %w", err)
	}

	return patch, nil
}

// ApplyPatch applies a bsdiff patch to base data
func (e *BsdiffEngine) ApplyPatch(baseData, patchData []byte) ([]byte, error) {
	if len(patchData) == 0 {
		return baseData, nil
	}

	// Handle edge case: empty base data (this was a new file)
	if len(baseData) == 0 {
		// The "patch" is actually the full file data
		return patchData, nil
	}

	// Apply patch using bspatch.Bytes
	newData, err := bspatch.Bytes(baseData, patchData)
	if err != nil {
		return nil, fmt.Errorf("bspatch application failed: %w", err)
	}

	return newData, nil
}
