package ebpf

import (
	"context"
	"sync"
	"time"

	"github.com/yourorg/diffkeeper/pkg/config"
)

// Profiler performs lightweight EMA calculations to predict frequently modified paths
type Profiler struct {
	sink      HotPathSink
	interval  time.Duration
	alpha     float64
	threshold float64

	mu        sync.Mutex
	samples   map[string]uint64
	estimates map[string]float64
}

// NewProfiler builds a profiler using EBPFConfig settings
func NewProfiler(cfg *config.EBPFConfig, sink HotPathSink) *Profiler {
	if cfg == nil || !cfg.EnableProfiler {
		return nil
	}
	return &Profiler{
		sink:      sink,
		interval:  cfg.ProfilerInterval,
		alpha:     cfg.ProfilerAlpha,
		threshold: cfg.HotPathThreshold,
		samples:   make(map[string]uint64),
		estimates: make(map[string]float64),
	}
}

// Record increments sample counts for a path
func (p *Profiler) Record(path string) {
	if p == nil || path == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.samples[path]++
}

// Run begins periodic prediction loop until ctx is cancelled
func (p *Profiler) Run(ctx context.Context) {
	if p == nil {
		return
	}
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.flush()
		}
	}
}

// Snapshot returns a copy of current EMA map (for testing/diagnostics)
func (p *Profiler) Snapshot() map[string]float64 {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	cp := make(map[string]float64, len(p.estimates))
	for k, v := range p.estimates {
		cp[k] = v
	}
	return cp
}

// Flush forces an immediate EMA update (used for tests and manual tuning)
func (p *Profiler) Flush() {
	if p == nil {
		return
	}
	p.flush()
}

func (p *Profiler) flush() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.samples) == 0 {
		return
	}

	hints := make(map[string]float64)
	for path, count := range p.samples {
		prev := p.estimates[path]
		current := p.alpha*float64(count) + (1-p.alpha)*prev
		p.estimates[path] = current

		if current >= p.threshold {
			hints[path] = current
		}
	}

	// Reset sample window
	p.samples = make(map[string]uint64)

	if len(hints) > 0 && p.sink != nil {
		_ = p.sink.ApplyHotPathHints(hints)
	}
}
