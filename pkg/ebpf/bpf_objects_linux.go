//go:build linux

package ebpf

import (
	"bytes"
	_ "embed"
	"fmt"

	"github.com/cilium/ebpf"
)

//go:embed diffkeeper.bpf.o
var diffkeeperObject []byte

// bpfObjects mirrors the maps and programs compiled into diffkeeper.bpf.o.
type bpfObjects struct {
	Events          *ebpf.Map     `ebpf:"events"`
	LifecycleEvents *ebpf.Map     `ebpf:"lifecycle_events"`
	FentryVfsWrite  *ebpf.Program `ebpf:"fentry_vfs_write"`
	FentryVfsWritev *ebpf.Program `ebpf:"fentry_vfs_writev"`
	HandleSchedExec *ebpf.Program `ebpf:"handle_sched_exec"`
}

func (o *bpfObjects) Close() error {
	if o == nil {
		return nil
	}

	if o.Events != nil {
		o.Events.Close()
	}
	if o.LifecycleEvents != nil {
		o.LifecycleEvents.Close()
	}
	if o.FentryVfsWrite != nil {
		o.FentryVfsWrite.Close()
	}
	if o.FentryVfsWritev != nil {
		o.FentryVfsWritev.Close()
	}
	if o.HandleSchedExec != nil {
		o.HandleSchedExec.Close()
	}
	return nil
}

func loadEmbeddedSpec() (*ebpf.CollectionSpec, error) {
	if len(diffkeeperObject) == 0 {
		return nil, fmt.Errorf("embedded diffkeeper object is empty")
	}
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(diffkeeperObject))
	if err != nil {
		return nil, fmt.Errorf("load embedded spec: %w", err)
	}
	return spec, nil
}

func loadBpfObjects(objs *bpfObjects, opts *ebpf.CollectionOptions) error {
	spec, err := loadEmbeddedSpec()
	if err != nil {
		return err
	}
	if err := spec.LoadAndAssign(objs, opts); err != nil {
		return fmt.Errorf("load diffkeeper objects: %w", err)
	}
	return nil
}
