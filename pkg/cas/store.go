package cas

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/klauspost/compress/zstd"
	"github.com/multiformats/go-multihash"
	"go.etcd.io/bbolt"
)

const (
	BucketCAS     = "cas"
	BucketCASRefs = "cas_refs"
)

const compressionMagic = "DKZ1"

// CASStore implements content-addressable storage
type CASStore struct {
	db       *bbolt.DB
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
func NewCASStore(db *bbolt.DB, hashAlgo string) (*CASStore, error) {
	// Initialize CAS buckets
	err := db.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(BucketCAS)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(BucketCASRefs)); err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to initialize CAS buckets: %w", err)
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

	err = c.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(BucketCAS))
		return bucket.Put([]byte(cid), compressed)
	})

	if err != nil {
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

	err = c.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(BucketCAS))
		return bucket.Put([]byte(cid), compressed)
	})

	if err != nil {
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
	var data []byte

	err := c.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(BucketCAS))
		value := bucket.Get([]byte(cid))

		if value == nil {
			return fmt.Errorf("CID not found: %s", cid)
		}

		decompressed, err := decompressFromStorage(value)
		if err != nil {
			return fmt.Errorf("failed to decompress CID %s: %w", cid, err)
		}

		data = decompressed

		return nil
	})

	return data, err
}

// GetChunk retrieves data using a pre-computed SHA256 hash.
func (c *CASStore) GetChunk(hash [32]byte) ([]byte, error) {
	return c.Get(hex.EncodeToString(hash[:]))
}

// Has checks if a CID exists in CAS
func (c *CASStore) Has(cid string) (bool, error) {
	var exists bool

	err := c.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(BucketCAS))
		value := bucket.Get([]byte(cid))
		exists = value != nil
		return nil
	})

	return exists, err
}

// Delete removes a CID from CAS
// WARNING: This should only be called after verifying no references exist
func (c *CASStore) Delete(cid string) error {
	return c.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(BucketCAS))
		return bucket.Delete([]byte(cid))
	})
}

// AddReference adds a reference from a file to a CID
func (c *CASStore) AddReference(cid, filePath string) error {
	return c.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(BucketCASRefs))

		// Get existing ref count
		var refCount CASRefCount
		value := bucket.Get([]byte(cid))

		if value != nil {
			if err := json.Unmarshal(value, &refCount); err != nil {
				return fmt.Errorf("failed to unmarshal ref count: %w", err)
			}
		} else {
			// Initialize new ref count
			refCount = CASRefCount{
				CID:   cid,
				Refs:  0,
				Files: []string{},
			}
		}

		// Check if file already referenced
		for _, f := range refCount.Files {
			if f == filePath {
				// Already referenced, no need to increment
				return nil
			}
		}

		// Add reference
		refCount.Refs++
		refCount.Files = append(refCount.Files, filePath)

		// Store updated ref count
		data, err := json.Marshal(refCount)
		if err != nil {
			return fmt.Errorf("failed to marshal ref count: %w", err)
		}

		return bucket.Put([]byte(cid), data)
	})
}

// RemoveReference removes a reference from a file to a CID
func (c *CASStore) RemoveReference(cid, filePath string) error {
	return c.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(BucketCASRefs))

		// Get existing ref count
		var refCount CASRefCount
		value := bucket.Get([]byte(cid))

		if value == nil {
			// No references exist
			return nil
		}

		if err := json.Unmarshal(value, &refCount); err != nil {
			return fmt.Errorf("failed to unmarshal ref count: %w", err)
		}

		// Remove file from references
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
			// File wasn't referenced, nothing to do
			return nil
		}

		refCount.Files = newFiles
		refCount.Refs--

		// If no more references, delete the ref count entry
		if refCount.Refs <= 0 {
			return bucket.Delete([]byte(cid))
		}

		// Store updated ref count
		data, err := json.Marshal(refCount)
		if err != nil {
			return fmt.Errorf("failed to marshal ref count: %w", err)
		}

		return bucket.Put([]byte(cid), data)
	})
}

// GetRefCount returns the reference count for a CID
func (c *CASStore) GetRefCount(cid string) (int, error) {
	var count int

	err := c.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(BucketCASRefs))
		value := bucket.Get([]byte(cid))

		if value == nil {
			count = 0
			return nil
		}

		var refCount CASRefCount
		if err := json.Unmarshal(value, &refCount); err != nil {
			return fmt.Errorf("failed to unmarshal ref count: %w", err)
		}

		count = refCount.Refs
		return nil
	})

	return count, err
}

// GarbageCollect removes unreferenced CAS objects
func (c *CASStore) GarbageCollect() (int, error) {
	var deleted int

	// Get all CIDs with no references
	var unreferencedCIDs []string

	err := c.db.View(func(tx *bbolt.Tx) error {
		casBucket := tx.Bucket([]byte(BucketCAS))
		refsBucket := tx.Bucket([]byte(BucketCASRefs))

		// Iterate all CAS objects
		return casBucket.ForEach(func(k, v []byte) error {
			cid := string(k)

			// Check if it has references
			refData := refsBucket.Get(k)
			if refData == nil {
				// No references, mark for deletion
				unreferencedCIDs = append(unreferencedCIDs, cid)
				return nil
			}

			var refCount CASRefCount
			if err := json.Unmarshal(refData, &refCount); err != nil {
				return err
			}

			if refCount.Refs <= 0 {
				unreferencedCIDs = append(unreferencedCIDs, cid)
			}

			return nil
		})
	})

	if err != nil {
		return 0, fmt.Errorf("failed to collect unreferenced CIDs: %w", err)
	}

	// Delete unreferenced CIDs
	for _, cid := range unreferencedCIDs {
		if err := c.Delete(cid); err != nil {
			return deleted, fmt.Errorf("failed to delete CID %s: %w", cid, err)
		}
		deleted++
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

	err := c.db.View(func(tx *bbolt.Tx) error {
		casBucket := tx.Bucket([]byte(BucketCAS))
		refsBucket := tx.Bucket([]byte(BucketCASRefs))

		// Track which CIDs have references
		referencedCIDs := make(map[string]bool)
		fileSet := make(map[string]bool)

		// Count total references and unique files
		if err := refsBucket.ForEach(func(k, v []byte) error {
			var refCount CASRefCount
			if err := json.Unmarshal(v, &refCount); err != nil {
				return err
			}

			if refCount.Refs > 0 {
				referencedCIDs[string(k)] = true
				stats.TotalRefs += refCount.Refs
				for _, f := range refCount.Files {
					fileSet[f] = true
				}
			}

			return nil
		}); err != nil {
			return err
		}

		stats.UniqueFiles = len(fileSet)

		// Count total objects and unreferenced objects
		if err := casBucket.ForEach(func(k, v []byte) error {
			stats.TotalObjects++
			stats.TotalSize += int64(len(v))

			if !referencedCIDs[string(k)] {
				stats.UnreferencedObjs++
			}

			return nil
		}); err != nil {
			return err
		}

		return nil
	})

	return stats, err
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
