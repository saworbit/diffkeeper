package config

import (
	"os"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Library != "bsdiff" {
		t.Errorf("Expected default library 'bsdiff', got '%s'", cfg.Library)
	}

	if cfg.ChunkSizeMB != 4 {
		t.Errorf("Expected default chunk size 4MB, got %d", cfg.ChunkSizeMB)
	}

	if cfg.HashAlgo != "sha256" {
		t.Errorf("Expected default hash algo 'sha256', got '%s'", cfg.HashAlgo)
	}

	if cfg.DedupScope != "container" {
		t.Errorf("Expected default dedup scope 'container', got '%s'", cfg.DedupScope)
	}

	if !cfg.EnableDiff {
		t.Error("Expected EnableDiff to be true by default")
	}

	if cfg.SnapshotInterval != 10 {
		t.Errorf("Expected snapshot interval 10, got %d", cfg.SnapshotInterval)
	}

	if cfg.ChunkThresholdBytes != 1*1024*1024*1024 {
		t.Errorf("Expected chunk threshold 1GB, got %d", cfg.ChunkThresholdBytes)
	}
}

func TestLoadFromEnv(t *testing.T) {
	// Set environment variables
	os.Setenv("DIFFKEEPER_DIFF_LIBRARY", "xdelta")
	os.Setenv("DIFFKEEPER_CHUNK_SIZE_MB", "8")
	os.Setenv("DIFFKEEPER_HASH_ALGO", "blake3")
	os.Setenv("DIFFKEEPER_DEDUP_SCOPE", "cluster")
	os.Setenv("DIFFKEEPER_ENABLE_DIFF", "false")
	os.Setenv("DIFFKEEPER_SNAPSHOT_INTERVAL", "20")
	os.Setenv("DIFFKEEPER_CHUNK_THRESHOLD_MB", "2048")
	defer func() {
		os.Unsetenv("DIFFKEEPER_DIFF_LIBRARY")
		os.Unsetenv("DIFFKEEPER_CHUNK_SIZE_MB")
		os.Unsetenv("DIFFKEEPER_HASH_ALGO")
		os.Unsetenv("DIFFKEEPER_DEDUP_SCOPE")
		os.Unsetenv("DIFFKEEPER_ENABLE_DIFF")
		os.Unsetenv("DIFFKEEPER_SNAPSHOT_INTERVAL")
		os.Unsetenv("DIFFKEEPER_CHUNK_THRESHOLD_MB")
	}()

	cfg := LoadFromEnv()

	if cfg.Library != "xdelta" {
		t.Errorf("Expected library 'xdelta', got '%s'", cfg.Library)
	}

	if cfg.ChunkSizeMB != 8 {
		t.Errorf("Expected chunk size 8MB, got %d", cfg.ChunkSizeMB)
	}

	if cfg.HashAlgo != "blake3" {
		t.Errorf("Expected hash algo 'blake3', got '%s'", cfg.HashAlgo)
	}

	if cfg.DedupScope != "cluster" {
		t.Errorf("Expected dedup scope 'cluster', got '%s'", cfg.DedupScope)
	}

	if cfg.EnableDiff {
		t.Error("Expected EnableDiff to be false")
	}

	if cfg.SnapshotInterval != 20 {
		t.Errorf("Expected snapshot interval 20, got %d", cfg.SnapshotInterval)
	}

	if cfg.ChunkThresholdBytes != 2048*1024*1024 {
		t.Errorf("Expected chunk threshold 2GB, got %d", cfg.ChunkThresholdBytes)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *DiffConfig
		wantErr bool
	}{
		{
			name:    "valid default config",
			cfg:     DefaultConfig(),
			wantErr: false,
		},
		{
			name: "invalid diff library",
			cfg: &DiffConfig{
				Library:             "invalid",
				ChunkSizeMB:         4,
				HashAlgo:            "sha256",
				DedupScope:          "container",
				SnapshotInterval:    10,
				ChunkThresholdBytes: 1024,
			},
			wantErr: true,
		},
		{
			name: "invalid chunk size",
			cfg: &DiffConfig{
				Library:             "bsdiff",
				ChunkSizeMB:         -1,
				HashAlgo:            "sha256",
				DedupScope:          "container",
				SnapshotInterval:    10,
				ChunkThresholdBytes: 1024,
			},
			wantErr: true,
		},
		{
			name: "invalid hash algo",
			cfg: &DiffConfig{
				Library:             "bsdiff",
				ChunkSizeMB:         4,
				HashAlgo:            "md5",
				DedupScope:          "container",
				SnapshotInterval:    10,
				ChunkThresholdBytes: 1024,
			},
			wantErr: true,
		},
		{
			name: "invalid dedup scope",
			cfg: &DiffConfig{
				Library:             "bsdiff",
				ChunkSizeMB:         4,
				HashAlgo:            "sha256",
				DedupScope:          "global",
				SnapshotInterval:    10,
				ChunkThresholdBytes: 1024,
			},
			wantErr: true,
		},
		{
			name: "invalid snapshot interval",
			cfg: &DiffConfig{
				Library:             "bsdiff",
				ChunkSizeMB:         4,
				HashAlgo:            "sha256",
				DedupScope:          "container",
				SnapshotInterval:    0,
				ChunkThresholdBytes: 1024,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetChunkSizeBytes(t *testing.T) {
	cfg := &DiffConfig{ChunkSizeMB: 4}
	expected := 4 * 1024 * 1024

	if got := cfg.GetChunkSizeBytes(); got != expected {
		t.Errorf("GetChunkSizeBytes() = %d, want %d", got, expected)
	}
}

func TestShouldChunk(t *testing.T) {
	cfg := &DiffConfig{ChunkThresholdBytes: 1024 * 1024 * 1024} // 1GB

	tests := []struct {
		name     string
		fileSize int64
		want     bool
	}{
		{"small file", 100 * 1024 * 1024, false},         // 100MB
		{"exactly threshold", 1024 * 1024 * 1024, false}, // 1GB
		{"large file", 2 * 1024 * 1024 * 1024, true},     // 2GB
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cfg.ShouldChunk(tt.fileSize); got != tt.want {
				t.Errorf("ShouldChunk(%d) = %v, want %v", tt.fileSize, got, tt.want)
			}
		})
	}
}
