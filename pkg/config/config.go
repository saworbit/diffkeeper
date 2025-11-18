package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// DiffConfig holds configuration for binary diff operations
type DiffConfig struct {
	// Library specifies the diff algorithm to use ("bsdiff" or "xdelta")
	Library string

	// ChunkSizeMB is the chunk size in megabytes for large file processing
	ChunkSizeMB int

	// EnableChunking toggles streaming content-defined chunking for large files
	EnableChunking bool

	// ChunkMinBytes is the minimum chunk size (bytes) when chunking is enabled
	ChunkMinBytes int

	// ChunkAvgBytes is the target average chunk size (bytes)
	ChunkAvgBytes int

	// ChunkMaxBytes is the maximum chunk size (bytes)
	ChunkMaxBytes int

	// ChunkHashWindow controls the rolling hash window size in bytes
	ChunkHashWindow int

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

	// EBPF holds configuration for kernel-level monitoring, profiler, and lifecycle tracing
	EBPF EBPFConfig
}

// EBPFConfig captures settings for eBPF-based monitoring and adaptive profiling
type EBPFConfig struct {
	Enable           bool
	ProgramPath      string
	ProfilerInterval time.Duration
	ProfilerAlpha    float64
	HotPathThreshold float64
	EnableProfiler   bool
	AutoInject       bool
	InjectorCommand  string
	LifecycleTracing bool
	FallbackFSNotify bool
	CollectLifecycle bool
	EventBufferSize  int
	LifecycleBufSize int
	BTF              BTFConfig
}

// BTFConfig controls CO-RE relocations and BTFHub downloads
type BTFConfig struct {
	CacheDir      string
	AllowDownload bool
	HubMirror     string
}

// DefaultConfig returns the default configuration
func DefaultConfig() *DiffConfig {
	return &DiffConfig{
		Library:             "bsdiff",
		ChunkSizeMB:         4,
		EnableChunking:      true,
		ChunkMinBytes:       1 * 1024 * 1024,  // 1MiB
		ChunkAvgBytes:       8 * 1024 * 1024,  // 8MiB
		ChunkMaxBytes:       64 * 1024 * 1024, // 64MiB
		ChunkHashWindow:     64,               // 64 bytes rolling window
		HashAlgo:            "sha256",
		DedupScope:          "container",
		EnableDiff:          true,
		SnapshotInterval:    10,                     // Full snapshot every 10 versions
		ChunkThresholdBytes: 1 * 1024 * 1024 * 1024, // 1GB
		EBPF:                defaultEBPFConfig(),
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
	if chunkMin := os.Getenv("DIFFKEEPER_CHUNK_MIN_BYTES"); chunkMin != "" {
		if size, err := strconv.Atoi(chunkMin); err == nil {
			cfg.ChunkMinBytes = size
		}
	}
	if chunkAvg := os.Getenv("DIFFKEEPER_CHUNK_AVG_BYTES"); chunkAvg != "" {
		if size, err := strconv.Atoi(chunkAvg); err == nil {
			cfg.ChunkAvgBytes = size
		}
	}
	if chunkMax := os.Getenv("DIFFKEEPER_CHUNK_MAX_BYTES"); chunkMax != "" {
		if size, err := strconv.Atoi(chunkMax); err == nil {
			cfg.ChunkMaxBytes = size
		}
	}
	if window := os.Getenv("DIFFKEEPER_CHUNK_HASH_WINDOW"); window != "" {
		if size, err := strconv.Atoi(window); err == nil {
			cfg.ChunkHashWindow = size
		}
	}
	if enableChunking := os.Getenv("DIFFKEEPER_ENABLE_CHUNKING"); enableChunking != "" {
		cfg.EnableChunking = enableChunking == "1" || enableChunking == "true" || enableChunking == "TRUE"
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

	cfg.EBPF = loadEBPFConfigFromEnv(cfg.EBPF)

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

	if c.ChunkMinBytes <= 0 || c.ChunkAvgBytes <= 0 || c.ChunkMaxBytes <= 0 {
		return fmt.Errorf("chunk byte sizes must be positive (min=%d avg=%d max=%d)", c.ChunkMinBytes, c.ChunkAvgBytes, c.ChunkMaxBytes)
	}

	if c.ChunkMinBytes > c.ChunkAvgBytes {
		return fmt.Errorf("chunk min size cannot exceed average (min=%d avg=%d)", c.ChunkMinBytes, c.ChunkAvgBytes)
	}

	if c.ChunkAvgBytes > c.ChunkMaxBytes {
		return fmt.Errorf("chunk average size cannot exceed max (avg=%d max=%d)", c.ChunkAvgBytes, c.ChunkMaxBytes)
	}

	if c.ChunkHashWindow <= 0 {
		return fmt.Errorf("chunk hash window must be positive, got: %d", c.ChunkHashWindow)
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

	if err := c.EBPF.Validate(); err != nil {
		return fmt.Errorf("ebpf config invalid: %w", err)
	}

	return nil
}

// GetChunkSizeBytes returns chunk size in bytes
func (c *DiffConfig) GetChunkSizeBytes() int {
	return c.ChunkSizeMB * 1024 * 1024
}

// ShouldChunk returns true if a file of the given size should be chunked
func (c *DiffConfig) ShouldChunk(fileSize int64) bool {
	if !c.EnableChunking {
		return false
	}
	return fileSize > c.ChunkThresholdBytes
}

// ChunkingConfig models the normalized chunking knobs in byte units.
type ChunkingConfig struct {
	MinBytes   int
	AvgBytes   int
	MaxBytes   int
	HashWindow int
}

// GetChunkingConfig returns chunking parameters in a single struct.
func (c *DiffConfig) GetChunkingConfig() ChunkingConfig {
	return ChunkingConfig{
		MinBytes:   c.ChunkMinBytes,
		AvgBytes:   c.ChunkAvgBytes,
		MaxBytes:   c.ChunkMaxBytes,
		HashWindow: c.ChunkHashWindow,
	}
}

func defaultEBPFConfig() EBPFConfig {
	return EBPFConfig{
		Enable:           true,
		ProgramPath:      "",
		ProfilerInterval: 100 * time.Millisecond,
		ProfilerAlpha:    0.1,
		HotPathThreshold: 10.0,
		EnableProfiler:   true,
		AutoInject:       true,
		InjectorCommand:  "",
		LifecycleTracing: true,
		FallbackFSNotify: true,
		CollectLifecycle: true,
		EventBufferSize:  4096,
		LifecycleBufSize: 256,
		BTF: BTFConfig{
			CacheDir:      defaultBTFCacheDir(),
			AllowDownload: true,
			HubMirror:     "https://github.com/aquasecurity/btfhub-archive/raw/main",
		},
	}
}

func defaultBTFCacheDir() string {
	if _, err := os.Stat("/var/cache"); err == nil || os.IsPermission(err) {
		return "/var/cache/diffkeeper/btf"
	}
	return filepath.Join(os.TempDir(), "diffkeeper", "btf")
}

func loadEBPFConfigFromEnv(cfg EBPFConfig) EBPFConfig {
	if v := os.Getenv("DIFFKEEPER_ENABLE_EBPF"); v != "" {
		cfg.Enable = v == "1" || v == "true" || v == "TRUE"
	}
	if v := os.Getenv("DIFFKEEPER_EBPF_PROGRAM"); v != "" {
		cfg.ProgramPath = v
	}
	if v := os.Getenv("DIFFKEEPER_EBPF_PROFILER_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.ProfilerInterval = d
		}
	}
	if v := os.Getenv("DIFFKEEPER_EBPF_PROFILER_ALPHA"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.ProfilerAlpha = f
		}
	}
	if v := os.Getenv("DIFFKEEPER_EBPF_HOT_PATH_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.HotPathThreshold = f
		}
	}
	if v := os.Getenv("DIFFKEEPER_EBPF_ENABLE_PROFILER"); v != "" {
		cfg.EnableProfiler = v == "1" || v == "true" || v == "TRUE"
	}
	if v := os.Getenv("DIFFKEEPER_EBPF_AUTO_INJECT"); v != "" {
		cfg.AutoInject = v == "1" || v == "true" || v == "TRUE"
	}
	if v := os.Getenv("DIFFKEEPER_EBPF_INJECTOR_CMD"); v != "" {
		cfg.InjectorCommand = v
	}
	if v := os.Getenv("DIFFKEEPER_EBPF_LIFECYCLE_TRACING"); v != "" {
		cfg.LifecycleTracing = v == "1" || v == "true" || v == "TRUE"
	}
	if v := os.Getenv("DIFFKEEPER_EBPF_FALLBACK_FSNOTIFY"); v != "" {
		cfg.FallbackFSNotify = v == "1" || v == "true" || v == "TRUE"
	}
	if v := os.Getenv("DIFFKEEPER_EBPF_EVENT_BUFFER"); v != "" {
		if size, err := strconv.Atoi(v); err == nil && size > 0 {
			cfg.EventBufferSize = size
		}
	}
	if v := os.Getenv("DIFFKEEPER_EBPF_LIFECYCLE_BUFFER"); v != "" {
		if size, err := strconv.Atoi(v); err == nil && size > 0 {
			cfg.LifecycleBufSize = size
		}
	}

	if cacheDir := os.Getenv("DIFFKEEPER_BTF_CACHE_DIR"); cacheDir != "" {
		cfg.BTF.CacheDir = cacheDir
	}
	if allow := os.Getenv("DIFFKEEPER_BTF_ALLOW_DOWNLOAD"); allow != "" {
		cfg.BTF.AllowDownload = allow == "1" || allow == "true" || allow == "TRUE"
	}
	if mirror := os.Getenv("DIFFKEEPER_BTF_MIRROR"); mirror != "" {
		cfg.BTF.HubMirror = mirror
	}

	return cfg
}

// Validate ensures eBPF configuration values make sense for the running kernel
func (c EBPFConfig) Validate() error {
	if !c.Enable {
		return nil
	}
	if c.ProfilerInterval <= 0 {
		return fmt.Errorf("profiler interval must be > 0")
	}
	if c.ProfilerAlpha <= 0 || c.ProfilerAlpha >= 1 {
		return fmt.Errorf("profiler alpha must be between 0 and 1 (exclusive)")
	}
	if c.HotPathThreshold < 0 {
		return fmt.Errorf("hot path threshold must be >= 0")
	}
	if c.EventBufferSize <= 0 {
		return fmt.Errorf("event buffer size must be positive")
	}
	if c.LifecycleBufSize <= 0 {
		return fmt.Errorf("lifecycle buffer size must be positive")
	}
	if err := c.BTF.Validate(); err != nil {
		return err
	}
	return nil
}

// Validate ensures BTF config is usable for CO-RE relocations
func (c BTFConfig) Validate() error {
	if c.CacheDir == "" {
		return fmt.Errorf("btf cache directory must be provided")
	}
	if c.HubMirror == "" {
		return fmt.Errorf("btfhub mirror must be provided")
	}
	return nil
}
