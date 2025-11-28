package cas

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/cockroachdb/pebble"
	"github.com/klauspost/compress/zstd"
	"github.com/multiformats/go-multihash"
)

const (
	PrefixCAS  = "c:" // Stores compressed file chunks
	PrefixMeta = "m:" // Stores file metadata
	PrefixLog  = "l:" // Stores raw incoming events (The "Journal")
)

const (
	metaRefPrefix = PrefixMeta + "ref:"
)

const compressionMagic = "DKZ1"

// CASStore implements content-addressable storage
type CASStore struct {
	db       *pebble.DB
	hashAlgo string
}

// CASObject represents a stored object in CAS
type CASObject struct {
	CID  string // Content identifier (multihash)
	Data []byte // Object data
	Size int    // Size in bytes
}

// CASRefCount tracks references to a CAS object
type CASRefCount struct {
	CID   string   `json:"cid"`
	Refs  int      `json:"refs"`
	Files []string `json:"files"` // Which files reference this CID
}

// NewCASStore creates a new content-addressable storage instance
func NewCASStore(db *pebble.DB, hashAlgo string) (*CASStore, error) {
	if db == nil {
		return nil, fmt.Errorf("pebble DB is nil")
	}

	return &CASStore{
		db:       db,
		hashAlgo: hashAlgo,
	}, nil
}

// computeCID computes a content identifier for the given data
func (c *CASStore) computeCID(data []byte) (string, error) {
	var hashType uint64

	switch c.hashAlgo {
	case "sha256":
		hashType = multihash.SHA2_256
	case "blake3":
		hashType = multihash.BLAKE3
	default:
		return "", fmt.Errorf("unsupported hash algorithm: %s", c.hashAlgo)
	}

	mh, err := multihash.Sum(data, hashType, -1)
	if err != nil {
		return "", fmt.Errorf("failed to compute multihash: %w", err)
	}

	return mh.B58String(), nil
}

// PutWithSize stores data in CAS and returns its CID along with the compressed bytes written.
// If the CID already exists, the storedBytes value will be zero.
func (c *CASStore) PutWithSize(data []byte) (string, int, error) {
	cid, err := c.computeCID(data)
	if err != nil {
		return "", 0, err
	}

	exists, err := c.Has(cid)
	if err != nil {
		return "", 0, err
	}

	if exists {
		return cid, 0, nil
	}

	compressed, err := compressForStorage(data)
	if err != nil {
		return "", 0, fmt.Errorf("failed to compress object: %w", err)
	}

	if err := c.db.Set(casKey(cid), compressed, pebble.Sync); err != nil {
		return "", 0, fmt.Errorf("failed to store in CAS: %w", err)
	}

	return cid, len(compressed), nil
}

// Put stores data in CAS and returns its CID
// If the data already exists (same CID), it's deduplicated
func (c *CASStore) Put(data []byte) (string, error) {
	cid, _, err := c.PutWithSize(data)
	return cid, err
}

// PutChunkWithHash stores data keyed by a pre-computed SHA256 hash, returning the CID and compressed bytes written.
func (c *CASStore) PutChunkWithHash(hash [32]byte, data []byte) (string, int, error) {
	cid := hex.EncodeToString(hash[:])

	exists, err := c.Has(cid)
	if err != nil {
		return "", 0, err
	}

	if exists {
		return cid, 0, nil
	}

	compressed, err := compressForStorage(data)
	if err != nil {
		return "", 0, fmt.Errorf("failed to compress chunk: %w", err)
	}

	if err := c.db.Set(casKey(cid), compressed, pebble.Sync); err != nil {
		return "", 0, fmt.Errorf("failed to store chunk in CAS: %w", err)
	}

	return cid, len(compressed), nil
}

// PutChunk stores chunk data using its SHA256 hash.
func (c *CASStore) PutChunk(hash [32]byte, data []byte) (string, error) {
	cid, _, err := c.PutChunkWithHash(hash, data)
	return cid, err
}

// Get retrieves data from CAS by CID
func (c *CASStore) Get(cid string) ([]byte, error) {
	val, closer, err := c.db.Get(casKey(cid))
	if errors.Is(err, pebble.ErrNotFound) {
		return nil, fmt.Errorf("CID not found: %s", cid)
	}
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	copied := append([]byte(nil), val...)
	return decompressFromStorage(copied)
}

// GetChunk retrieves data using a pre-computed SHA256 hash.
func (c *CASStore) GetChunk(hash [32]byte) ([]byte, error) {
	return c.Get(hex.EncodeToString(hash[:]))
}

// Has checks if a CID exists in CAS
func (c *CASStore) Has(cid string) (bool, error) {
	_, closer, err := c.db.Get(casKey(cid))
	if errors.Is(err, pebble.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	closer.Close()
	return true, nil
}

// Delete removes a CID from CAS
// WARNING: This should only be called after verifying no references exist
func (c *CASStore) Delete(cid string) error {
	return c.db.Delete(casKey(cid), pebble.Sync)
}

// AddReference adds a reference from a file to a CID
func (c *CASStore) AddReference(cid, filePath string) error {
	key := refKey(cid)
	refCount := CASRefCount{
		CID:   cid,
		Refs:  0,
		Files: []string{},
	}

	if val, closer, err := c.db.Get(key); err == nil {
		defer closer.Close()
		if err := json.Unmarshal(val, &refCount); err != nil {
			return fmt.Errorf("failed to unmarshal ref count: %w", err)
		}
	} else if !errors.Is(err, pebble.ErrNotFound) {
		return err
	}

	for _, f := range refCount.Files {
		if f == filePath {
			return nil
		}
	}

	refCount.Refs++
	refCount.Files = append(refCount.Files, filePath)

	data, err := json.Marshal(refCount)
	if err != nil {
		return fmt.Errorf("failed to marshal ref count: %w", err)
	}

	return c.db.Set(key, data, pebble.Sync)
}

// RemoveReference removes a reference from a file to a CID
func (c *CASStore) RemoveReference(cid, filePath string) error {
	key := refKey(cid)
	refCount := CASRefCount{}

	val, closer, err := c.db.Get(key)
	if errors.Is(err, pebble.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	defer closer.Close()

	if err := json.Unmarshal(val, &refCount); err != nil {
		return fmt.Errorf("failed to unmarshal ref count: %w", err)
	}

	newFiles := []string{}
	found := false
	for _, f := range refCount.Files {
		if f != filePath {
			newFiles = append(newFiles, f)
		} else {
			found = true
		}
	}

	if !found {
		return nil
	}

	refCount.Files = newFiles
	refCount.Refs--

	if refCount.Refs <= 0 {
		return c.db.Delete(key, pebble.Sync)
	}

	data, err := json.Marshal(refCount)
	if err != nil {
		return fmt.Errorf("failed to marshal ref count: %w", err)
	}

	return c.db.Set(key, data, pebble.Sync)
}

// GetRefCount returns the reference count for a CID
func (c *CASStore) GetRefCount(cid string) (int, error) {
	key := refKey(cid)
	val, closer, err := c.db.Get(key)
	if errors.Is(err, pebble.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer closer.Close()

	var refCount CASRefCount
	if err := json.Unmarshal(val, &refCount); err != nil {
		return 0, fmt.Errorf("failed to unmarshal ref count: %w", err)
	}

	return refCount.Refs, nil
}

// GarbageCollect removes unreferenced CAS objects
func (c *CASStore) GarbageCollect() (int, error) {
	iter, err := newPrefixIter(c.db, PrefixCAS)
	if err != nil {
		return 0, err
	}
	defer iter.Close()

	deleted := 0

	for iter.First(); iter.Valid(); iter.Next() {
		cid := stripPrefix(iter.Key(), PrefixCAS)

		refs, err := c.GetRefCount(cid)
		if err != nil {
			return deleted, fmt.Errorf("failed to get ref count for %s: %w", cid, err)
		}

		if refs <= 0 {
			if err := c.db.Delete(casKey(cid), pebble.Sync); err != nil {
				return deleted, fmt.Errorf("failed to delete CID %s: %w", cid, err)
			}
			deleted++
		}
	}

	if err := iter.Error(); err != nil {
		return deleted, err
	}

	return deleted, nil
}

// Stats returns statistics about the CAS store
type CASStats struct {
	TotalObjects     int
	TotalSize        int64
	TotalRefs        int
	UniqueFiles      int
	UnreferencedObjs int
}

// GetStats returns statistics about the CAS store
func (c *CASStore) GetStats() (CASStats, error) {
	var stats CASStats

	referencedCIDs := make(map[string]bool)
	fileSet := make(map[string]bool)

	refsIter, err := newPrefixIter(c.db, metaRefPrefix)
	if err != nil {
		return stats, err
	}
	defer refsIter.Close()

	for refsIter.First(); refsIter.Valid(); refsIter.Next() {
		var refCount CASRefCount
		if err := json.Unmarshal(refsIter.Value(), &refCount); err != nil {
			return stats, err
		}

		if refCount.Refs > 0 {
			referencedCIDs[refCount.CID] = true
			stats.TotalRefs += refCount.Refs
			for _, f := range refCount.Files {
				fileSet[f] = true
			}
		}
	}

	if err := refsIter.Error(); err != nil {
		return stats, err
	}

	stats.UniqueFiles = len(fileSet)

	casIter, err := newPrefixIter(c.db, PrefixCAS)
	if err != nil {
		return stats, err
	}
	defer casIter.Close()

	for casIter.First(); casIter.Valid(); casIter.Next() {
		stats.TotalObjects++
		stats.TotalSize += int64(len(casIter.Value()))

		cid := stripPrefix(casIter.Key(), PrefixCAS)
		if !referencedCIDs[cid] {
			stats.UnreferencedObjs++
		}
	}

	if err := casIter.Error(); err != nil {
		return stats, err
	}

	return stats, nil
}

var (
	zstdEncoderOnce sync.Once
	zstdDecoderOnce sync.Once
	zstdEncoder     *zstd.Encoder
	zstdDecoder     *zstd.Decoder
	zstdInitErr     error
)

func getZstdEncoder() (*zstd.Encoder, error) {
	zstdEncoderOnce.Do(func() {
		enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
		if err != nil {
			zstdInitErr = err
			return
		}
		zstdEncoder = enc
	})
	return zstdEncoder, zstdInitErr
}

func getZstdDecoder() (*zstd.Decoder, error) {
	zstdDecoderOnce.Do(func() {
		dec, err := zstd.NewReader(nil)
		if err != nil {
			zstdInitErr = err
			return
		}
		zstdDecoder = dec
	})
	return zstdDecoder, zstdInitErr
}

func compressForStorage(data []byte) ([]byte, error) {
	enc, err := getZstdEncoder()
	if err != nil {
		return nil, err
	}
	dst := enc.EncodeAll(data, nil)
	return append([]byte(compressionMagic), dst...), nil
}

func decompressFromStorage(data []byte) ([]byte, error) {
	if len(data) < len(compressionMagic) || !bytes.Equal(data[:len(compressionMagic)], []byte(compressionMagic)) {
		out := make([]byte, len(data))
		copy(out, data)
		return out, nil
	}

	dec, err := getZstdDecoder()
	if err != nil {
		return nil, err
	}
	return dec.DecodeAll(data[len(compressionMagic):], nil)
}

func casKey(cid string) []byte {
	return []byte(PrefixCAS + cid)
}

func refKey(cid string) []byte {
	return []byte(metaRefPrefix + cid)
}

func newPrefixIter(db *pebble.DB, prefix string) (*pebble.Iterator, error) {
	upper := append([]byte(prefix), 0xff)
	return db.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: upper,
	})
}

func stripPrefix(key []byte, prefix string) string {
	// Copy to avoid iterator buffer reuse across steps
	k := append([]byte(nil), key...)
	return strings.TrimPrefix(string(k), prefix)
}
