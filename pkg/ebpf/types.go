package ebpf

import (
	"context"
	"errors"
	"time"
)

// ErrUnsupported is returned when the current platform cannot host eBPF programs
var ErrUnsupported = errors.New("eBPF monitoring is only supported on Linux kernels >= 4.18")

// Event represents a captured syscall write targeting a given path
type Event struct {
	PID       uint32
	Path      string
	Bytes     uint64
	Timestamp time.Time
}

// LifecycleEvent represents a container lifecycle transition (create/start/stop)
type LifecycleEvent struct {
	Runtime     string
	Namespace   string
	ContainerID string
	PID         uint32
	State       string
	Timestamp   time.Time
}

// HotPathSink consumes adaptive profiler hints to refine kernel filters
type HotPathSink interface {
	ApplyHotPathHints(map[string]float64) error
}

// Manager exposes kernel-level monitoring primitives regardless of platform
type Manager interface {
	Start(ctx context.Context) error
	Close() error
	Events() <-chan Event
	LifecycleEvents() <-chan LifecycleEvent
	ApplyHotPathHints(map[string]float64) error
}
