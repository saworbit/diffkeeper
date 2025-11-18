package metrics

import (
	"context"
	"errors"
	"log"
	"net/http"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const namespace = "diffkeeper"

var (
	// Registry is a dedicated Prometheus registry for all DiffKeeper metrics.
	Registry = prometheus.NewRegistry()

	// CaptureDuration measures time spent capturing file changes.
	CaptureDuration = promauto.With(Registry).NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "capture_duration_ms",
			Help:      "Duration of state capture operations in milliseconds",
			Buckets:   []float64{1, 2, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000},
		},
		[]string{"type"}, // file | diff | snapshot
	)

	// CaptureTotal counts capture operations by type and outcome.
	CaptureTotal = promauto.With(Registry).NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "capture_total",
			Help:      "Total number of capture operations",
		},
		[]string{"type", "outcome"},
	)

	// StorageSavedBytesTotal accumulates bytes saved vs full-file storage.
	StorageSavedBytesTotal = promauto.With(Registry).NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "storage_saved_bytes_total",
			Help:      "Cumulative bytes saved by using deltas instead of full files",
		},
	)

	// StorageSavedRatio tracks the current savings ratio (0.0 - 1.0).
	StorageSavedRatio = promauto.With(Registry).NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "storage_saved_ratio",
			Help:      "Current storage savings ratio (saved_bytes / total_written_bytes)",
		},
	)

	// RecoveryDuration measures recovery/restoration latency.
	RecoveryDuration = promauto.With(Registry).NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "recovery_duration_ms",
			Help:      "Duration of state recovery operations in milliseconds",
			Buckets:   []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500},
		},
		[]string{"reason"}, // startup | crash | migration
	)

	// RecoveryTotal counts recovery attempts and their outcomes.
	RecoveryTotal = promauto.With(Registry).NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "recovery_total",
			Help:      "Total number of recovery operations",
		},
		[]string{"outcome"},
	)

	// StoreSizeBytes tracks Bolt buckets and store footprint.
	StoreSizeBytes = promauto.With(Registry).NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "store_size_bytes",
			Help:      "On-disk size of the delta/metadata store",
		},
		[]string{"type"}, // deltas | metadata | store_file
	)

	// FilesTracked reports the number of files under watch/managed.
	FilesTracked = promauto.With(Registry).NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "files_tracked_total",
			Help:      "Number of files currently being tracked",
		},
	)

	// ChunkTotal counts chunk processing outcomes (new vs reuse).
	ChunkTotal = promauto.With(Registry).NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "chunk_total",
			Help:      "Total chunks processed during streaming capture",
		},
		[]string{"outcome"}, // new | reuse
	)

	// ChunkDedupRatio reports global dedup ratio across chunk captures.
	ChunkDedupRatio = promauto.With(Registry).NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "chunk_dedup_ratio",
			Help:      "Instant dedup ratio for content-defined chunking",
		},
	)

	// ChunkCaptureDuration tracks streaming chunking latency.
	ChunkCaptureDuration = promauto.With(Registry).NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "chunk_capture_duration_ms",
			Help:      "Duration of streaming chunk capture in milliseconds",
			Buckets:   []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000},
		},
	)

	// LargeFilesTracked gauges the number of large files handled via chunking.
	LargeFilesTracked = promauto.With(Registry).NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "large_file_tracked_total",
			Help:      "Gauge of files above the chunking threshold currently tracked",
		},
	)

	// DeltasTotal counts delta writes grouped by compression/diff strategy.
	DeltasTotal = promauto.With(Registry).NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "deltas_total",
			Help:      "Number of deltas written",
		},
		[]string{"compression"}, // bsdiff | gzip | none
	)

	// AgentInfo exposes static information about the running agent.
	AgentInfo = promauto.With(Registry).NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "agent_info",
			Help:      "Static information about the agent",
		},
		[]string{"os", "arch", "version", "capture_backend"},
	)

	// Up is a liveness gauge for the agent.
	Up = promauto.With(Registry).NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "up",
			Help:      "1 if the agent is running and healthy",
		},
	)
)

var (
	totalWrittenBytes atomic.Int64
	totalSavedBytes   atomic.Int64
	chunkTotalCount   atomic.Int64
	chunkReuseCount   atomic.Int64
)

func init() {
	Registry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	Registry.MustRegister(prometheus.NewGoCollector())
	Up.Set(1)
}

// SetAgentInfo publishes a single info metric for the running agent.
func SetAgentInfo(osName, arch, version, captureBackend string) {
	if osName == "" {
		osName = runtime.GOOS
	}
	if arch == "" {
		arch = runtime.GOARCH
	}
	if captureBackend == "" {
		captureBackend = "unknown"
	}
	if version == "" {
		version = "dev"
	}
	AgentInfo.WithLabelValues(osName, arch, version, captureBackend).Set(1)
}

// ObserveCapture records timing and counters for capture operations.
func ObserveCapture(start time.Time, captureType, outcome string) {
	elapsed := float64(time.Since(start)) / float64(time.Millisecond)
	CaptureDuration.WithLabelValues(captureType).Observe(elapsed)
	CaptureTotal.WithLabelValues(captureType, outcome).Inc()
}

// ObserveStorageSavings updates storage delta counters and ratios.
func ObserveStorageSavings(originalBytes, compressedBytes int64) {
	if originalBytes <= 0 || compressedBytes < 0 {
		return
	}

	saved := originalBytes - compressedBytes
	written := totalWrittenBytes.Add(originalBytes)

	if saved > 0 {
		totalSavedBytes.Add(saved)
		StorageSavedBytesTotal.Add(float64(saved))
	}

	if written > 0 {
		currentSaved := totalSavedBytes.Load()
		StorageSavedRatio.Set(float64(currentSaved) / float64(written))
	}
}

// ObserveRecovery captures recovery duration and outcomes.
func ObserveRecovery(start time.Time, reason, outcome string) {
	elapsed := float64(time.Since(start)) / float64(time.Millisecond)
	RecoveryDuration.WithLabelValues(reason).Observe(elapsed)
	RecoveryTotal.WithLabelValues(outcome).Inc()
}

// ObserveChunk records a chunk outcome and updates dedup ratio.
func ObserveChunk(outcome string) {
	if outcome != "reuse" {
		outcome = "new"
	}
	count := chunkTotalCount.Add(1)
	if outcome == "reuse" {
		reused := chunkReuseCount.Add(1)
		if count > 0 {
			ChunkDedupRatio.Set(float64(reused) / float64(count))
		}
	}
	ChunkTotal.WithLabelValues(outcome).Inc()
}

// ObserveChunkCapture tracks the latency of a streaming chunking pass.
func ObserveChunkCapture(start time.Time) {
	elapsed := float64(time.Since(start)) / float64(time.Millisecond)
	ChunkCaptureDuration.Observe(elapsed)
}

// SetStoreSize reports store footprint by category.
func SetStoreSize(bucket string, sizeBytes int64) {
	if sizeBytes < 0 {
		return
	}
	StoreSizeBytes.WithLabelValues(bucket).Set(float64(sizeBytes))
}

// SetFilesTracked reports the number of tracked files.
func SetFilesTracked(count int) {
	if count < 0 {
		count = 0
	}
	FilesTracked.Set(float64(count))
}

// SetLargeFilesTracked sets the large file gauge used by streaming chunking.
func SetLargeFilesTracked(count int) {
	if count < 0 {
		count = 0
	}
	LargeFilesTracked.Set(float64(count))
}

// AddDeltas increments the delta counter for a specific compression strategy.
func AddDeltas(compression string, count int) {
	if count <= 0 {
		return
	}
	DeltasTotal.WithLabelValues(compression).Add(float64(count))
}

// SetUp toggles the liveness gauge.
func SetUp(healthy bool) {
	if healthy {
		Up.Set(1)
		return
	}
	Up.Set(0)
}

// Serve starts the /metrics HTTP endpoint on the provided address.
func Serve(ctx context.Context, addr string, logger *log.Logger) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		logger = log.Default()
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(Registry, promhttp.HandlerOpts{EnableOpenMetrics: true}))

	srv := &http.Server{Addr: addr, Handler: mux}

	idleClosed := make(chan struct{})
	go func() {
		defer close(idleClosed)
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()

	logger.Printf("[Metrics] Prometheus endpoint listening on %s", addr)
	err := srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		<-idleClosed
		return nil
	}

	return err
}
