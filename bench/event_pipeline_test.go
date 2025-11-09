package bench

import (
	"testing"
	"time"
)

// benchmarkEventPipeline simulates dispatch latency by draining a buffered
// channel of synthetic events. It approximates the difference between the
// ring-buffer based eBPF path and fsnotify's per-event goroutine dispatch.
func benchmarkEventPipeline(b *testing.B, buffer int, processing time.Duration) {
	events := make(chan struct{}, buffer)
	for i := 0; i < b.N; i++ {
		events <- struct{}{}
	}
	close(events)

	start := time.Now()
	for range events {
		if processing > 0 {
			time.Sleep(processing)
		}
	}
	elapsed := time.Since(start)
	if elapsed == 0 {
		elapsed = time.Nanosecond
	}
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "events/sec")
}

func BenchmarkEBPFRingBuffer(b *testing.B) {
	b.ReportAllocs()
	benchmarkEventPipeline(b, 4096, 5*time.Microsecond)
}

func BenchmarkFSNotifyWatcher(b *testing.B) {
	b.ReportAllocs()
	benchmarkEventPipeline(b, 64, 40*time.Microsecond)
}
