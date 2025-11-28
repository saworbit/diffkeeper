package recorder

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/saworbit/diffkeeper/pkg/cas"
)

// JournalEntry represents a raw filesystem event captured for later processing.
type JournalEntry struct {
	Timestamp int64  `json:"ts"` // Nanoseconds
	Path      string `json:"path"`
	Op        string `json:"op"`   // "write", "create", etc.
	Data      []byte `json:"data"` // The raw content written
}

// Journal appends raw events to Pebble using a time-ordered prefix.
type Journal struct {
	db *pebble.DB
}

// NewJournal creates a journal writer bound to the provided Pebble instance.
func NewJournal(db *pebble.DB) *Journal {
	return &Journal{db: db}
}

// LogEvent writes a journal entry with a default "write" operation.
func (j *Journal) LogEvent(path string, data []byte) error {
	return logEventWithOp(j.db, "write", path, data)
}

// LogEventWithOp writes a journal entry with an explicit operation string.
func (j *Journal) LogEventWithOp(op, path string, data []byte) error {
	return logEventWithOp(j.db, op, path, data)
}

func logEventWithOp(db *pebble.DB, op, path string, data []byte) error {
	if db == nil {
		return fmt.Errorf("pebble database is not initialized")
	}

	entry := JournalEntry{
		Timestamp: time.Now().UnixNano(),
		Path:      path,
		Op:        op,
		Data:      data,
	}

	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal journal entry: %w", err)
	}

	keySuffix, err := randomSuffix()
	if err != nil {
		return fmt.Errorf("generate journal key: %w", err)
	}

	key := []byte(fmt.Sprintf("%s%020d:%s", cas.PrefixLog, entry.Timestamp, keySuffix))

	batch := db.NewBatch()
	defer batch.Close()

	if err := batch.Set(key, payload, pebble.NoSync); err != nil {
		return fmt.Errorf("write journal entry: %w", err)
	}

	if err := batch.Commit(pebble.NoSync); err != nil {
		return fmt.Errorf("commit journal entry: %w", err)
	}

	return nil
}

func randomSuffix() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
