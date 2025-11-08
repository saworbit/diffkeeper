package diff

import (
	"fmt"
)

// DiffEngine defines the interface for binary diff operations
type DiffEngine interface {
	// ComputeDiff computes the binary diff between old and new data
	ComputeDiff(oldData, newData []byte) ([]byte, error)

	// ApplyPatch applies a diff patch to base data to produce new data
	ApplyPatch(baseData, patchData []byte) ([]byte, error)

	// Name returns the name of the diff engine
	Name() string
}

// NewDiffEngine creates a new diff engine based on the specified library
func NewDiffEngine(library string) (DiffEngine, error) {
	switch library {
	case "bsdiff":
		return NewBsdiffEngine(), nil
	case "xdelta":
		return nil, fmt.Errorf("xdelta support not yet implemented (planned for future release)")
	default:
		return nil, fmt.Errorf("unsupported diff library: %s (must be 'bsdiff' or 'xdelta')", library)
	}
}

// Stats holds statistics about a diff operation
type Stats struct {
	OldSize         int     // Size of old data
	NewSize         int     // Size of new data
	PatchSize       int     // Size of patch data
	CompressionRate float64 // Patch size / new size (lower is better)
}

// ComputeStats calculates statistics for a diff operation
func ComputeStats(oldData, newData, patchData []byte) Stats {
	stats := Stats{
		OldSize:   len(oldData),
		NewSize:   len(newData),
		PatchSize: len(patchData),
	}

	if len(newData) > 0 {
		stats.CompressionRate = float64(len(patchData)) / float64(len(newData))
	}

	return stats
}
