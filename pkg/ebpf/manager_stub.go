//go:build !linux

package ebpf

import (
	"context"
	"fmt"

	"github.com/saworbit/diffkeeper/pkg/config"
)

type stubManager struct{}

// NewManager reports unsupported platforms when Linux eBPF is unavailable.
func NewManager(_ string, _ *config.EBPFConfig) (Manager, error) {
	return nil, ErrUnsupported
}

func (stubManager) Start(context.Context) error                { return fmt.Errorf("ebpf unavailable") }
func (stubManager) Close() error                               { return nil }
func (stubManager) Events() <-chan Event                       { return nil }
func (stubManager) LifecycleEvents() <-chan LifecycleEvent     { return nil }
func (stubManager) ApplyHotPathHints(map[string]float64) error { return nil }
