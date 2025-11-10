package cas

import (
	"os"
	"path/filepath"
	"testing"

	"go.etcd.io/bbolt"
)

func setupTestDB(t *testing.T) (*bbolt.DB, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := bbolt.Open(dbPath, 0600, nil)
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}

	return db, cleanup
}

func mustAddReference(tb testing.TB, store *CASStore, cid, path string) {
	tb.Helper()
	if err := store.AddReference(cid, path); err != nil {
		tb.Fatalf("AddReference(%s, %s) error: %v", cid, path, err)
	}
}

func mustPut(tb testing.TB, store *CASStore, data []byte) string {
	tb.Helper()
	cid, err := store.Put(data)
	if err != nil {
		tb.Fatalf("store.Put error: %v", err)
	}
	return cid
}

func TestNewCASStore(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewCASStore(db, "sha256")
	if err != nil {
		t.Fatalf("NewCASStore() error = %v", err)
	}

	if store == nil {
		t.Fatal("NewCASStore() returned nil store")
	}

	if store.hashAlgo != "sha256" {
		t.Errorf("Expected hash algo 'sha256', got '%s'", store.hashAlgo)
	}
}

func TestCASStore_PutAndGet(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewCASStore(db, "sha256")
	if err != nil {
		t.Fatalf("NewCASStore() error = %v", err)
	}

	data := []byte("hello world")

	// Put data
	cid, err := store.Put(data)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	if cid == "" {
		t.Fatal("Put() returned empty CID")
	}

	// Get data
	retrieved, err := store.Get(cid)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if string(retrieved) != string(data) {
		t.Errorf("Get() = %v, want %v", retrieved, data)
	}
}

func TestCASStore_Deduplication(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewCASStore(db, "sha256")
	if err != nil {
		t.Fatalf("NewCASStore() error = %v", err)
	}

	data := []byte("duplicate data")

	// Put same data twice
	cid1, err := store.Put(data)
	if err != nil {
		t.Fatalf("First Put() error = %v", err)
	}

	cid2, err := store.Put(data)
	if err != nil {
		t.Fatalf("Second Put() error = %v", err)
	}

	// Should return same CID (deduplication)
	if cid1 != cid2 {
		t.Errorf("Deduplication failed: cid1 = %s, cid2 = %s", cid1, cid2)
	}

	// Verify only one object stored
	stats, err := store.GetStats()
	if err != nil {
		t.Fatalf("GetStats() error = %v", err)
	}

	if stats.TotalObjects != 1 {
		t.Errorf("Expected 1 object after deduplication, got %d", stats.TotalObjects)
	}
}

func TestCASStore_Has(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewCASStore(db, "sha256")
	if err != nil {
		t.Fatalf("NewCASStore() error = %v", err)
	}

	data := []byte("test data")
	cid, err := store.Put(data)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	// Check existing CID
	exists, err := store.Has(cid)
	if err != nil {
		t.Fatalf("Has() error = %v", err)
	}

	if !exists {
		t.Error("Has() = false for existing CID")
	}

	// Check non-existing CID
	exists, err = store.Has("nonexistent")
	if err != nil {
		t.Fatalf("Has() error = %v", err)
	}

	if exists {
		t.Error("Has() = true for non-existing CID")
	}
}

func TestCASStore_References(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewCASStore(db, "sha256")
	if err != nil {
		t.Fatalf("NewCASStore() error = %v", err)
	}

	data := []byte("referenced data")
	cid, err := store.Put(data)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	// Add reference
	err = store.AddReference(cid, "/path/to/file1")
	if err != nil {
		t.Fatalf("AddReference() error = %v", err)
	}

	// Check ref count
	count, err := store.GetRefCount(cid)
	if err != nil {
		t.Fatalf("GetRefCount() error = %v", err)
	}

	if count != 1 {
		t.Errorf("GetRefCount() = %d, want 1", count)
	}

	// Add another reference
	err = store.AddReference(cid, "/path/to/file2")
	if err != nil {
		t.Fatalf("AddReference() error = %v", err)
	}

	count, err = store.GetRefCount(cid)
	if err != nil {
		t.Fatalf("GetRefCount() error = %v", err)
	}

	if count != 2 {
		t.Errorf("GetRefCount() = %d, want 2", count)
	}

	// Add duplicate reference (should not increment)
	err = store.AddReference(cid, "/path/to/file1")
	if err != nil {
		t.Fatalf("AddReference() error = %v", err)
	}

	count, err = store.GetRefCount(cid)
	if err != nil {
		t.Fatalf("GetRefCount() error = %v", err)
	}

	if count != 2 {
		t.Errorf("GetRefCount() after duplicate = %d, want 2", count)
	}

	// Remove reference
	err = store.RemoveReference(cid, "/path/to/file1")
	if err != nil {
		t.Fatalf("RemoveReference() error = %v", err)
	}

	count, err = store.GetRefCount(cid)
	if err != nil {
		t.Fatalf("GetRefCount() error = %v", err)
	}

	if count != 1 {
		t.Errorf("GetRefCount() after removal = %d, want 1", count)
	}
}

func TestCASStore_GarbageCollect(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewCASStore(db, "sha256")
	if err != nil {
		t.Fatalf("NewCASStore() error = %v", err)
	}

	// Add some data with references
	data1 := []byte("referenced data")
	cid1 := mustPut(t, store, data1)
	mustAddReference(t, store, cid1, "/file1")

	// Add some data without references
	data2 := []byte("unreferenced data")
	cid2 := mustPut(t, store, data2)

	// Run GC
	deleted, err := store.GarbageCollect()
	if err != nil {
		t.Fatalf("GarbageCollect() error = %v", err)
	}

	if deleted != 1 {
		t.Errorf("GarbageCollect() deleted %d objects, want 1", deleted)
	}

	// Verify referenced data still exists
	exists, err := store.Has(cid1)
	if err != nil {
		t.Fatalf("Has(%s) error: %v", cid1, err)
	}
	if !exists {
		t.Error("GarbageCollect() deleted referenced data")
	}

	// Verify unreferenced data is gone
	exists, err = store.Has(cid2)
	if err != nil {
		t.Fatalf("Has(%s) error: %v", cid2, err)
	}
	if exists {
		t.Error("GarbageCollect() did not delete unreferenced data")
	}
}

func TestCASStore_GetStats(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewCASStore(db, "sha256")
	if err != nil {
		t.Fatalf("NewCASStore() error = %v", err)
	}

	// Add data
	data1 := []byte("data 1")
	cid1 := mustPut(t, store, data1)
	mustAddReference(t, store, cid1, "/file1")
	mustAddReference(t, store, cid1, "/file2")

	data2 := []byte("data 2")
	cid2 := mustPut(t, store, data2)
	mustAddReference(t, store, cid2, "/file1")

	data3 := []byte("data 3")
	mustPut(t, store, data3) // No references

	// Get stats
	stats, err := store.GetStats()
	if err != nil {
		t.Fatalf("GetStats() error = %v", err)
	}

	if stats.TotalObjects != 3 {
		t.Errorf("TotalObjects = %d, want 3", stats.TotalObjects)
	}

	if stats.TotalRefs != 3 {
		t.Errorf("TotalRefs = %d, want 3", stats.TotalRefs)
	}

	if stats.UniqueFiles != 2 {
		t.Errorf("UniqueFiles = %d, want 2", stats.UniqueFiles)
	}

	if stats.UnreferencedObjs != 1 {
		t.Errorf("UnreferencedObjs = %d, want 1", stats.UnreferencedObjs)
	}
}

func BenchmarkCASStore_Put(b *testing.B) {
	db, cleanup := setupTestDB(&testing.T{})
	defer cleanup()

	store, err := NewCASStore(db, "sha256")
	if err != nil {
		b.Fatalf("NewCASStore() error = %v", err)
	}
	data := []byte("benchmark data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.Put(data); err != nil {
			b.Fatalf("store.Put error: %v", err)
		}
	}
}

func BenchmarkCASStore_Get(b *testing.B) {
	db, cleanup := setupTestDB(&testing.T{})
	defer cleanup()

	store, err := NewCASStore(db, "sha256")
	if err != nil {
		b.Fatalf("NewCASStore() error = %v", err)
	}
	data := []byte("benchmark data")
	cid := mustPut(b, store, data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.Get(cid); err != nil {
			b.Fatalf("store.Get error: %v", err)
		}
	}
}
