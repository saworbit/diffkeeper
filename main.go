package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/fsnotify/fsnotify"
	"github.com/saworbit/diffkeeper/internal/version"
	"github.com/saworbit/diffkeeper/pkg/cas"
	"github.com/saworbit/diffkeeper/pkg/config"
	"github.com/saworbit/diffkeeper/pkg/ebpf"
	"github.com/saworbit/diffkeeper/pkg/recorder"
	"github.com/spf13/cobra"
)

const sessionMetaKey = cas.PrefixMeta + "session:start"

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		log.Fatal(err)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "diffkeeper",
		Short:   "DiffKeeper - CI/CD flight recorder",
		Version: version.Version,
	}

	root.AddCommand(newRecordCmd(), newExportCmd())
	return root
}

func newRecordCmd() *cobra.Command {
	var stateDir string
	var watchDir string

	cmd := &cobra.Command{
		Use:   "record -- <command>",
		Short: "Record raw filesystem events into the Pebble journal",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if stateDir == "" {
				return fmt.Errorf("state-dir is required")
			}
			if watchDir == "" {
				watchDir = "."
			}
			return runRecord(stateDir, watchDir, args)
		},
	}

	cmd.Flags().StringVar(&stateDir, "state-dir", "", "Directory where Pebble state is stored")
	cmd.Flags().StringVar(&watchDir, "watch", ".", "Directory to watch for changes")
	return cmd
}

func newExportCmd() *cobra.Command {
	var stateDir string
	var outDir string
	var atTime string

	cmd := &cobra.Command{
		Use:   "export --out <dir> --time <timestamp>",
		Short: "Reconstruct files from CAS metadata at a given point in time",
		RunE: func(cmd *cobra.Command, args []string) error {
			if stateDir == "" {
				return fmt.Errorf("state-dir is required")
			}
			if outDir == "" {
				return fmt.Errorf("out directory is required")
			}
			return runExport(stateDir, outDir, atTime)
		},
	}

	cmd.Flags().StringVar(&stateDir, "state-dir", "", "Directory where Pebble state is stored")
	cmd.Flags().StringVar(&outDir, "out", "", "Destination directory for restored files")
	cmd.Flags().StringVar(&atTime, "time", "latest", "Timestamp or duration (e.g. 2s, 2025-01-02T15:04:05Z)")
	return cmd
}

func runRecord(stateDir, watchDir string, args []string) error {
	cfg := config.DefaultConfig()

	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	db, err := pebble.Open(stateDir, &pebble.Options{})
	if err != nil {
		return fmt.Errorf("open pebble: %w", err)
	}
	defer db.Close()

	casStore, err := cas.NewCASStore(db, cfg.HashAlgo)
	if err != nil {
		return fmt.Errorf("init CAS: %w", err)
	}

	journal := recorder.NewJournal(db)
	stopProcessor := recorder.StartProcessor(db, casStore)
	defer stopProcessor()

	recordSessionStart(db, time.Now())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := startFSRecorder(ctx, watchDir, journal); err != nil {
		return fmt.Errorf("start fs recorder: %w", err)
	}

	mgr, err := ebpf.NewManager(stateDir, &cfg.EBPF)
	if err != nil && !errors.Is(err, ebpf.ErrUnsupported) {
		return fmt.Errorf("start ebpf manager: %w", err)
	}

	if mgr != nil {
		go func() {
			if err := mgr.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("[eBPF] manager stopped: %v", err)
			}
		}()
		defer mgr.Close()
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Dir = watchDir

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start command: %w", err)
	}

	runErr := cmd.Wait()

	// Give the processor a short window to drain the journal before closing.
	time.Sleep(200 * time.Millisecond)

	if flushErr := db.Flush(); flushErr != nil && runErr == nil {
		runErr = flushErr
	}

	return runErr
}

func runExport(stateDir, outDir, atTime string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create out dir: %w", err)
	}

	db, err := pebble.Open(stateDir, &pebble.Options{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("open pebble: %w", err)
	}
	defer db.Close()

	cfg := config.DefaultConfig()
	casStore, err := cas.NewCASStore(db, cfg.HashAlgo)
	if err != nil {
		return fmt.Errorf("init CAS: %w", err)
	}

	sessionStart := loadSessionStart(db)
	targetTime, err := parseTargetTime(atTime, sessionStart)
	if err != nil {
		return err
	}

	records, err := loadMetadataAt(db, targetTime)
	if err != nil {
		return err
	}

	for path, meta := range records {
		data, err := casStore.Get(meta.CID)
		if err != nil {
			return fmt.Errorf("load CAS object %s: %w", meta.CID, err)
		}

		relPath := cleanPath(path)
		dest := filepath.Join(outDir, relPath)

		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("create parent for %s: %w", dest, err)
		}

		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dest, err)
		}
	}

	return nil
}

func loadMetadataAt(db *pebble.DB, target time.Time) (map[string]recorder.MetadataRecord, error) {
	iter, err := newPrefixIter(db, cas.PrefixMeta)
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	records := make(map[string]recorder.MetadataRecord)
	cutoff := target.UnixNano()

	for iter.First(); iter.Valid(); iter.Next() {
		key := string(iter.Key())
		if key == sessionMetaKey {
			continue
		}

		val := append([]byte(nil), iter.Value()...)
		var meta recorder.MetadataRecord
		if err := json.Unmarshal(val, &meta); err != nil {
			log.Printf("[export] skip corrupt metadata %s: %v", key, err)
			continue
		}

		if meta.Timestamp > cutoff {
			continue
		}

		if prev, ok := records[meta.Path]; !ok || meta.Timestamp > prev.Timestamp {
			records[meta.Path] = meta
		}
	}

	if err := iter.Error(); err != nil {
		return nil, err
	}

	return records, nil
}

func recordSessionStart(db *pebble.DB, start time.Time) {
	if db == nil {
		return
	}

	val := []byte(fmt.Sprintf("%020d", start.UnixNano()))

	if _, closer, err := db.Get([]byte(sessionMetaKey)); err == nil {
		closer.Close()
		return
	}

	if err := db.Set([]byte(sessionMetaKey), val, pebble.Sync); err != nil {
		log.Printf("[record] failed to record session start: %v", err)
	}
}

func loadSessionStart(db *pebble.DB) time.Time {
	val, closer, err := db.Get([]byte(sessionMetaKey))
	if err != nil {
		return time.Time{}
	}
	defer closer.Close()

	ts, err := strconv.ParseInt(strings.TrimSpace(string(val)), 10, 64)
	if err != nil {
		return time.Time{}
	}

	return time.Unix(0, ts)
}

func parseTargetTime(raw string, sessionStart time.Time) (time.Time, error) {
	if raw == "" || raw == "latest" {
		return time.Now(), nil
	}

	if dur, err := time.ParseDuration(raw); err == nil {
		if sessionStart.IsZero() {
			return time.Time{}, fmt.Errorf("session start unknown; cannot apply duration %s", raw)
		}
		return sessionStart.Add(dur), nil
	}

	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts, nil
	}

	return time.Time{}, fmt.Errorf("invalid time value %q", raw)
}

func startFSRecorder(ctx context.Context, root string, journal *recorder.Journal) error {
	if journal == nil {
		return fmt.Errorf("journal is not initialized")
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		return err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	if err := addWatchRecursive(watcher, absRoot); err != nil {
		watcher.Close()
		return err
	}

	go func() {
		defer watcher.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case evt := <-watcher.Events:
				if evt.Op&(fsnotify.Create|fsnotify.Write) != 0 {
					info, err := os.Stat(evt.Name)
					if err == nil && info.IsDir() && evt.Op&fsnotify.Create != 0 {
						_ = watcher.Add(evt.Name)
						continue
					}

					data, err := os.ReadFile(evt.Name)
					if err != nil {
						continue
					}

					path := evt.Name
					if rel, relErr := filepath.Rel(absRoot, evt.Name); relErr == nil {
						path = rel
					}

					_ = journal.LogEvent(path, data)
				}
			case err := <-watcher.Errors:
				if err != nil {
					log.Printf("[record] watcher error: %v", err)
				}
			}
		}
	}()

	return nil
}

func addWatchRecursive(watcher *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		return watcher.Add(path)
	})
}

func newPrefixIter(db *pebble.DB, prefix string) (*pebble.Iterator, error) {
	upper := append([]byte(prefix), 0xff)
	return db.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: upper,
	})
}

func cleanPath(path string) string {
	clean := filepath.Clean(path)
	clean = strings.TrimPrefix(clean, string(filepath.Separator))
	for strings.HasPrefix(clean, "..") {
		clean = strings.TrimPrefix(clean, "..")
		clean = strings.TrimPrefix(clean, string(filepath.Separator))
	}
	if clean == "." {
		return "root"
	}
	return clean
}
