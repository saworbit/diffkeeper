package recorder

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/saworbit/diffkeeper/pkg/cas"
)

// MetadataRecord links a logical path to a CAS object at a point in time.
type MetadataRecord struct {
	Path      string `json:"path"`
	Timestamp int64  `json:"ts"`
	CID       string `json:"cid"`
	Size      int    `json:"size"`
	Op        string `json:"op"`
}

// StartProcessor launches a background worker that drains journal entries into CAS and metadata.
func StartProcessor(db *pebble.DB, store *cas.CASStore) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	go processorLoop(ctx, db, store)
	return cancel
}

func processorLoop(ctx context.Context, db *pebble.DB, store *cas.CASStore) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		processed := false
		iter, err := newPrefixIter(db, cas.PrefixLog)
		if err != nil {
			log.Printf("[processor] iterator init error: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		for iter.First(); iter.Valid(); iter.Next() {
			processed = true

			logKey := append([]byte(nil), iter.Key()...)
			payload := append([]byte(nil), iter.Value()...)

			if err := processJournalEntry(db, store, logKey, payload); err != nil {
				log.Printf("[processor] failed to handle journal %s: %v", string(logKey), err)
			}
		}

		if err := iter.Close(); err != nil {
			log.Printf("[processor] iterator close error: %v", err)
		}
		if err := iter.Error(); err != nil {
			log.Printf("[processor] iterator error: %v", err)
		}

		if !processed {
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
}

func processJournalEntry(db *pebble.DB, store *cas.CASStore, logKey, payload []byte) error {
	if db == nil || store == nil {
		return fmt.Errorf("processor requires db and store")
	}

	var entry JournalEntry
	if err := json.Unmarshal(payload, &entry); err != nil {
		return fmt.Errorf("decode journal entry: %w", err)
	}

	if entry.Op == "" {
		entry.Op = "write"
	}

	hash := sha256.Sum256(entry.Data)

	cid, _, err := store.PutChunkWithHash(hash, entry.Data)
	if err != nil {
		return fmt.Errorf("store CAS chunk: %w", err)
	}

	meta := MetadataRecord{
		Path:      entry.Path,
		Timestamp: entry.Timestamp,
		CID:       cid,
		Size:      len(entry.Data),
		Op:        entry.Op,
	}

	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	metaKey := []byte(fmt.Sprintf("%s%s:%020d", cas.PrefixMeta, entry.Path, entry.Timestamp))

	if err := db.Set(metaKey, metaBytes, pebble.Sync); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}

	if err := db.Delete(logKey, pebble.Sync); err != nil {
		return fmt.Errorf("delete journal key: %w", err)
	}

	return nil
}

func newPrefixIter(db *pebble.DB, prefix string) (*pebble.Iterator, error) {
	upper := append([]byte(prefix), 0xff)
	return db.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: upper,
	})
}
