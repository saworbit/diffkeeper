//go:build linux

package ebpf

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/btf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/saworbit/diffkeeper/pkg/config"
)

var _ Manager = (*kernelManager)(nil)

type kernelManager struct {
	cfg       *config.EBPFConfig
	stateDir  string
	objs      bpfObjects
	btfSpec   *btf.Spec
	links     []link.Link
	sysEvents *ringbuf.Reader
	lifecycle *ringbuf.Reader

	events          chan Event
	lifecycleEvents chan LifecycleEvent

	cancel context.CancelFunc
	mu     sync.Mutex

	hotPaths sync.Map
	running  bool
}

// NewManager loads a compiled eBPF program and prepares syscall/lifecycle probes.
func NewManager(stateDir string, cfg *config.EBPFConfig) (Manager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("ebpf configuration is required")
	}

	var (
		btfSpec   *btf.Spec
		btfSource string
		err       error
	)

	if loader := NewBTFLoader(cfg); loader != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		btfSpec, btfSource, err = loader.LoadSpec(ctx)
		if err != nil {
			return nil, fmt.Errorf("btf load failed: %w", err)
		}
		if btfSource != "" {
			log.Printf("[eBPF] Loaded BTF spec from %s", btfSource)
		}
	}

	m := &kernelManager{
		cfg:      cfg,
		stateDir: stateDir,
		btfSpec:  btfSpec,
		events:   make(chan Event, max(cfg.EventBufferSize, 1024)),
	}

	if cfg.LifecycleTracing && cfg.CollectLifecycle {
		m.lifecycleEvents = make(chan LifecycleEvent, max(cfg.LifecycleBufSize, 64))
	}

	if err := m.init(); err != nil {
		_ = m.Close()
		return nil, err
	}

	return m, nil
}

func (m *kernelManager) init() error {
	var opts ebpf.CollectionOptions
	if m.btfSpec != nil {
		opts.Programs = ebpf.ProgramOptions{
			KernelTypes: m.btfSpec,
		}
	}

	if err := m.loadObjects(&opts); err != nil {
		return err
	}

	if err := m.attachSyscallProbes(); err != nil {
		return err
	}

	if err := m.setupReaders(); err != nil {
		return err
	}

	if m.cfg.LifecycleTracing && m.cfg.CollectLifecycle {
		if err := m.attachLifecycleTrace(); err != nil {
			log.Printf("[eBPF] Lifecycle tracing unavailable: %v", err)
			m.closeLifecycleChan()
		}
	}

	return nil
}

func (m *kernelManager) loadObjects(opts *ebpf.CollectionOptions) error {
	if m.cfg.ProgramPath == "" {
		return loadBpfObjects(&m.objs, opts)
	}

	f, err := os.Open(m.cfg.ProgramPath)
	if err != nil {
		return fmt.Errorf("open eBPF object (%s): %w", m.cfg.ProgramPath, err)
	}
	defer f.Close()

	spec, err := ebpf.LoadCollectionSpecFromReader(f)
	if err != nil {
		return fmt.Errorf("load eBPF spec: %w", err)
	}

	if err := spec.LoadAndAssign(&m.objs, opts); err != nil {
		return fmt.Errorf("assign eBPF objects: %w", err)
	}
	return nil
}

func (m *kernelManager) attachSyscallProbes() error {
	type probeCfg struct {
		prog    *ebpf.Program
		symbols []string
	}

	probes := []probeCfg{
		{prog: m.objs.KprobeVfsWrite, symbols: []string{"vfs_write", "ksys_write", "__x64_sys_write"}},
		{prog: m.objs.KprobeVfsWritev, symbols: []string{"vfs_writev", "ksys_writev", "__x64_sys_writev"}},
		{prog: m.objs.KprobeVfsPwritev, symbols: []string{"vfs_pwritev", "ksys_pwrite64", "__x64_sys_pwrite64"}},
	}

	for _, probe := range probes {
		if probe.prog == nil {
			continue
		}

		var attached bool
		for _, symbol := range probe.symbols {
			l, err := link.Kprobe(symbol, probe.prog, nil)
			if err != nil {
				continue
			}
			m.links = append(m.links, l)
			attached = true
			break
		}

		if !attached {
			return fmt.Errorf("failed to attach probe (symbols=%v)", probe.symbols)
		}
	}

	return nil
}

func (m *kernelManager) setupReaders() error {
	if m.objs.Events == nil {
		return fmt.Errorf("eBPF object missing 'events' map for syscall captures")
	}

	reader, err := ringbuf.NewReader(m.objs.Events)
	if err != nil {
		return fmt.Errorf("create syscall ring buffer: %w", err)
	}
	m.sysEvents = reader

	if m.cfg.LifecycleTracing && m.cfg.CollectLifecycle {
		if m.objs.LifecycleEvents != nil {
			rb, err := ringbuf.NewReader(m.objs.LifecycleEvents)
			if err != nil {
				return fmt.Errorf("create lifecycle ring buffer: %w", err)
			}
			m.lifecycle = rb
		}
	}

	return nil
}

func (m *kernelManager) attachLifecycleTrace() error {
	if m.objs.HandleSchedExec == nil {
		return errors.New("missing lifecycle trace program")
	}

	tracepoint, err := link.Tracepoint("sched", "sched_process_exec", m.objs.HandleSchedExec, nil)
	if err != nil {
		return fmt.Errorf("attach lifecycle tracepoint: %w", err)
	}
	m.links = append(m.links, tracepoint)
	return nil
}

// Start begins draining perf and ring buffers into Go channels
func (m *kernelManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return nil
	}

	if m.sysEvents == nil {
		return fmt.Errorf("ring buffer reader not initialized")
	}

	runCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	go m.consumeSyscallEvents(runCtx)
	if m.lifecycle != nil && m.lifecycleEvents != nil {
		go m.consumeLifecycleEvents(runCtx)
	}

	m.running = true
	return nil
}

func (m *kernelManager) consumeSyscallEvents(ctx context.Context) {
	defer close(m.events)

	for {
		record, err := m.sysEvents.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) || ctx.Err() != nil {
				return
			}
			log.Printf("[eBPF] ringbuf read error: %v", err)
			continue
		}

		event, err := decodeSyscallEvent(record.RawSample)
		if err != nil {
			log.Printf("[eBPF] decode event failed: %v", err)
			continue
		}

		select {
		case <-ctx.Done():
			return
		case m.events <- event:
		}
	}
}

func (m *kernelManager) consumeLifecycleEvents(ctx context.Context) {
	defer m.closeLifecycleChan()

	if m.lifecycle == nil {
		return
	}

	for {
		record, err := m.lifecycle.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) || ctx.Err() != nil {
				return
			}
			log.Printf("[eBPF] lifecycle ring buffer error: %v", err)
			continue
		}

		event, err := decodeLifecycleEvent(record.RawSample)
		if err != nil {
			log.Printf("[eBPF] lifecycle decode error: %v", err)
			continue
		}

		select {
		case <-ctx.Done():
			return
		case m.lifecycleEvents <- event:
		}
	}
}

func decodeSyscallEvent(raw []byte) (Event, error) {
	var payload struct {
		PID   uint32
		_     uint32
		Bytes uint64
		Path  [256]byte
	}

	if err := binary.Read(bytes.NewReader(raw), binary.LittleEndian, &payload); err != nil {
		return Event{}, err
	}

	path := string(bytes.Trim(payload.Path[:], "\x00"))
	return Event{
		PID:       payload.PID,
		Path:      path,
		Bytes:     payload.Bytes,
		Timestamp: time.Now(),
	}, nil
}

func decodeLifecycleEvent(raw []byte) (LifecycleEvent, error) {
	var payload struct {
		PID       uint32
		State     uint32
		Runtime   [16]byte
		Namespace [64]byte
		Container [64]byte
	}

	if err := binary.Read(bytes.NewReader(raw), binary.LittleEndian, &payload); err != nil {
		return LifecycleEvent{}, err
	}

	return LifecycleEvent{
		PID:         payload.PID,
		State:       lifecycleState(payload.State),
		Runtime:     string(bytes.Trim(payload.Runtime[:], "\x00")),
		Namespace:   string(bytes.Trim(payload.Namespace[:], "\x00")),
		ContainerID: string(bytes.Trim(payload.Container[:], "\x00")),
		Timestamp:   time.Now(),
	}, nil
}

func lifecycleState(id uint32) string {
	switch id {
	case 0:
		return "unknown"
	case 1:
		return "create"
	case 2:
		return "start"
	case 3:
		return "stop"
	default:
		return fmt.Sprintf("state:%d", id)
	}
}

func (m *kernelManager) Events() <-chan Event {
	return m.events
}

func (m *kernelManager) LifecycleEvents() <-chan LifecycleEvent {
	return m.lifecycleEvents
}

func (m *kernelManager) ApplyHotPathHints(hints map[string]float64) error {
	for path, score := range hints {
		m.hotPaths.Store(path, score)
	}
	// Future: write hints into kernel BPF map. For now we log to aid tuning.
	if len(hints) > 0 {
		log.Printf("[Profiler] Updated %d hot path hint(s)", len(hints))
	}
	return nil
}

// Close detaches probes and frees kernel/user-space resources
func (m *kernelManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cancel != nil {
		m.cancel()
	}

	if m.sysEvents != nil {
		m.sysEvents.Close()
	}
	if m.lifecycle != nil {
		m.lifecycle.Close()
	}

	for _, l := range m.links {
		_ = l.Close()
	}
	m.links = nil

	if err := m.objs.Close(); err != nil {
		log.Printf("[eBPF] object close error: %v", err)
	}

	m.running = false
	return nil
}

func (m *kernelManager) closeLifecycleChan() {
	if m.lifecycleEvents != nil {
		close(m.lifecycleEvents)
		m.lifecycleEvents = nil
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
