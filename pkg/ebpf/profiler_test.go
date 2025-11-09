package ebpf

import (
	"testing"
	"time"

	"github.com/yourorg/diffkeeper/pkg/config"
)

type captureSink struct {
	hints map[string]float64
}

func (c *captureSink) ApplyHotPathHints(hints map[string]float64) error {
	c.hints = make(map[string]float64, len(hints))
	for k, v := range hints {
		c.hints[k] = v
	}
	return nil
}

func TestProfilerEMA(t *testing.T) {
	cfg := config.EBPFConfig{
		EnableProfiler:   true,
		ProfilerInterval: 10 * time.Millisecond,
		ProfilerAlpha:    0.5,
		HotPathThreshold: 1,
	}

	sink := &captureSink{}
	prof := NewProfiler(&cfg, sink)

	prof.Record("/data/hot")
	prof.Record("/data/hot")
	prof.Record("/data/cold")
	prof.Flush()

	if len(sink.hints) == 0 {
		t.Fatalf("expected hints to be emitted")
	}

	if _, ok := sink.hints["/data/hot"]; !ok {
		t.Fatalf("hot path missing in hints: %#v", sink.hints)
	}

	if _, ok := sink.hints["/data/cold"]; ok {
		t.Fatalf("cold path should not exceed threshold: %#v", sink.hints)
	}

	if est := prof.Snapshot()["/data/hot"]; est <= 0 {
		t.Fatalf("snapshot missing EMA for hot path")
	}
}
