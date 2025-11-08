package config

import (
	"fmt"
	"os"
	"strconv"
)

// DiffConfig holds configuration for binary diff operations
type DiffConfig struct {
	// Library specifies the diff algorithm to use ("bsdiff" or "xdelta")
	Library string

	// ChunkSizeMB is the chunk size in megabytes for large file processing
	ChunkSizeMB int

	// HashAlgo specifies the hash algorithm for CAS ("sha256" or "blake3")
	HashAlgo string

	// DedupScope defines deduplication scope ("container" or "cluster")
	DedupScope string

	// EnableDiff enables binary diff mode (vs legacy full-file compression)
	EnableDiff bool

	// SnapshotInterval defines how often to create full snapshots (version count)
	SnapshotInterval int

	// ChunkThresholdBytes is the file size threshold for chunking
	ChunkThresholdBytes int64
}

// DefaultConfig returns the default configuration
func DefaultConfig() *DiffConfig {
	return &DiffConfig{
		Library:             "bsdiff",
		ChunkSizeMB:         4,
		HashAlgo:            "sha256",
		DedupScope:          "container",
		EnableDiff:          true,
		SnapshotInterval:    10, // Full snapshot every 10 versions
		ChunkThresholdBytes: 1 * 1024 * 1024 * 1024, // 1GB
	}
}

// LoadFromEnv loads configuration from environment variables
func LoadFromEnv() *DiffConfig {
	cfg := DefaultConfig()

	if lib := os.Getenv("DIFFKEEPER_DIFF_LIBRARY"); lib != "" {
		cfg.Library = lib
	}

	if chunkSize := os.Getenv("DIFFKEEPER_CHUNK_SIZE_MB"); chunkSize != "" {
		if size, err := strconv.Atoi(chunkSize); err == nil {
			cfg.ChunkSizeMB = size
		}
	}

	if hashAlgo := os.Getenv("DIFFKEEPER_HASH_ALGO"); hashAlgo != "" {
		cfg.HashAlgo = hashAlgo
	}

	if dedupScope := os.Getenv("DIFFKEEPER_DEDUP_SCOPE"); dedupScope != "" {
		cfg.DedupScope = dedupScope
	}

	if enableDiff := os.Getenv("DIFFKEEPER_ENABLE_DIFF"); enableDiff != "" {
		cfg.EnableDiff = enableDiff == "true" || enableDiff == "1"
	}

	if interval := os.Getenv("DIFFKEEPER_SNAPSHOT_INTERVAL"); interval != "" {
		if i, err := strconv.Atoi(interval); err == nil {
			cfg.SnapshotInterval = i
		}
	}

	if threshold := os.Getenv("DIFFKEEPER_CHUNK_THRESHOLD_MB"); threshold != "" {
		if t, err := strconv.Atoi(threshold); err == nil {
			cfg.ChunkThresholdBytes = int64(t) * 1024 * 1024
		}
	}

	return cfg
}

// Validate checks if the configuration is valid
func (c *DiffConfig) Validate() error {
	if c.Library != "bsdiff" && c.Library != "xdelta" {
		return fmt.Errorf("invalid diff library: %s (must be 'bsdiff' or 'xdelta')", c.Library)
	}

	if c.ChunkSizeMB <= 0 {
		return fmt.Errorf("chunk size must be positive, got: %d", c.ChunkSizeMB)
	}

	if c.HashAlgo != "sha256" && c.HashAlgo != "blake3" {
		return fmt.Errorf("invalid hash algorithm: %s (must be 'sha256' or 'blake3')", c.HashAlgo)
	}

	if c.DedupScope != "container" && c.DedupScope != "cluster" {
		return fmt.Errorf("invalid dedup scope: %s (must be 'container' or 'cluster')", c.DedupScope)
	}

	if c.SnapshotInterval <= 0 {
		return fmt.Errorf("snapshot interval must be positive, got: %d", c.SnapshotInterval)
	}

	if c.ChunkThresholdBytes <= 0 {
		return fmt.Errorf("chunk threshold must be positive, got: %d", c.ChunkThresholdBytes)
	}

	return nil
}

// GetChunkSizeBytes returns chunk size in bytes
func (c *DiffConfig) GetChunkSizeBytes() int {
	return c.ChunkSizeMB * 1024 * 1024
}

// ShouldChunk returns true if a file of the given size should be chunked
func (c *DiffConfig) ShouldChunk(fileSize int64) bool {
	return fileSize > c.ChunkThresholdBytes
}
