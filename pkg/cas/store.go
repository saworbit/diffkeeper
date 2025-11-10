package cas

import (
	"encoding/json"
	"fmt"

	"github.com/multiformats/go-multihash"
	"go.etcd.io/bbolt"
)

const (
	BucketCAS     = "cas"
	BucketCASRefs = "cas_refs"
)

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

// Put stores data in CAS and returns its CID
// If the data already exists (same CID), it's deduplicated
func (c *CASStore) Put(data []byte) (string, error) {
	cid, err := c.computeCID(data)
	if err != nil {
		return "", err
	}

	// Check if already exists (deduplication)
	exists, err := c.Has(cid)
	if err != nil {
		return "", err
	}

	if exists {
		// Already exists, no need to store again
		return cid, nil
	}

	// Store in CAS bucket
	err = c.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(BucketCAS))
		return bucket.Put([]byte(cid), data)
	})

	if err != nil {
		return "", fmt.Errorf("failed to store in CAS: %w", err)
	}

	return cid, nil
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

		// Copy data since it's only valid during transaction
		data = make([]byte, len(value))
		copy(data, value)

		return nil
	})

	return data, err
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
