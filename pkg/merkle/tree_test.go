package merkle

import (
	"bytes"
	"fmt"
	"testing"
)

func TestNewMerkleManager(t *testing.T) {
	mm := NewMerkleManager()

	if mm == nil {
		t.Fatal("NewMerkleManager() returned nil")
	}

	if mm.treeCache == nil {
		t.Error("MerkleManager tree cache is nil")
	}
}

func TestContent(t *testing.T) {
	cid1 := "test-cid-1"
	cid2 := "test-cid-2"

	c1 := NewContent(cid1)
	c2 := NewContent(cid2)
	c3 := NewContent(cid1)

	// Test CalculateHash
	hash1, err := c1.CalculateHash()
	if err != nil {
		t.Fatalf("CalculateHash() error = %v", err)
	}

	hash2, err := c2.CalculateHash()
	if err != nil {
		t.Fatalf("CalculateHash() error = %v", err)
	}

	hash3, err := c3.CalculateHash()
	if err != nil {
		t.Fatalf("CalculateHash() error = %v", err)
	}

	// Same CID should produce same hash
	if !bytes.Equal(hash1, hash3) {
		t.Error("Same CID produced different hashes")
	}

	// Different CIDs should produce different hashes
	if bytes.Equal(hash1, hash2) {
		t.Error("Different CIDs produced same hash")
	}

	// Test Equals
	equal, err := c1.Equals(c3)
	if err != nil {
		t.Fatalf("Equals() error = %v", err)
	}
	if !equal {
		t.Error("Equal CIDs should return true")
	}

	equal, err = c1.Equals(c2)
	if err != nil {
		t.Fatalf("Equals() error = %v", err)
	}
	if equal {
		t.Error("Different CIDs should return false")
	}
}

func TestBuildTree(t *testing.T) {
	mm := NewMerkleManager()

	tests := []struct {
		name    string
		cids    []string
		wantErr bool
	}{
		{
			name:    "valid tree with one CID",
			cids:    []string{"cid1"},
			wantErr: false,
		},
		{
			name:    "valid tree with multiple CIDs",
			cids:    []string{"cid1", "cid2", "cid3", "cid4"},
			wantErr: false,
		},
		{
			name:    "empty CID list",
			cids:    []string{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := mm.BuildTree(tt.cids)

			if (err != nil) != tt.wantErr {
				t.Errorf("BuildTree() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tree == nil {
				t.Error("BuildTree() returned nil tree without error")
			}
		})
	}
}

func TestGetRoot(t *testing.T) {
	mm := NewMerkleManager()

	cids := []string{"cid1", "cid2", "cid3"}
	tree, err := mm.BuildTree(cids)
	if err != nil {
		t.Fatalf("BuildTree() error = %v", err)
	}

	root := GetRoot(tree)
	if root == nil {
		t.Error("GetRoot() returned nil")
	}

	// Test with nil tree
	nilRoot := GetRoot(nil)
	if nilRoot != nil {
		t.Error("GetRoot(nil) should return nil")
	}
}

func TestVerifyTree(t *testing.T) {
	mm := NewMerkleManager()

	cids := []string{"cid1", "cid2", "cid3", "cid4"}
	tree, err := mm.BuildTree(cids)
	if err != nil {
		t.Fatalf("BuildTree() error = %v", err)
	}

	valid, err := VerifyTree(tree)
	if err != nil {
		t.Fatalf("VerifyTree() error = %v", err)
	}

	if !valid {
		t.Error("VerifyTree() returned false for valid tree")
	}

	// Test with nil tree
	_, err = VerifyTree(nil)
	if err == nil {
		t.Error("VerifyTree(nil) should return error")
	}
}

func TestVerifyContent(t *testing.T) {
	mm := NewMerkleManager()

	cids := []string{"cid1", "cid2", "cid3"}
	tree, err := mm.BuildTree(cids)
	if err != nil {
		t.Fatalf("BuildTree() error = %v", err)
	}

	// Verify existing content
	verified, err := mm.VerifyContent(tree, "cid1")
	if err != nil {
		t.Fatalf("VerifyContent() error = %v", err)
	}

	if !verified {
		t.Error("VerifyContent() returned false for valid content")
	}

	// Verify non-existing content
	verified, err = mm.VerifyContent(tree, "cid-not-in-tree")
	if err != nil {
		t.Fatalf("VerifyContent() error = %v", err)
	}

	if verified {
		t.Error("VerifyContent() returned true for non-existing content")
	}
}

func TestBuildAndCache(t *testing.T) {
	mm := NewMerkleManager()

	cids := []string{"cid1", "cid2"}
	key := "test-file.txt"

	tree, err := mm.BuildAndCache(key, cids)
	if err != nil {
		t.Fatalf("BuildAndCache() error = %v", err)
	}

	if tree == nil {
		t.Fatal("BuildAndCache() returned nil tree")
	}

	// Verify it's cached
	cached, ok := mm.GetCachedTree(key)
	if !ok {
		t.Error("Tree not found in cache")
	}

	if cached != tree {
		t.Error("Cached tree is different from built tree")
	}
}

func TestGetCachedTree(t *testing.T) {
	mm := NewMerkleManager()

	cids := []string{"cid1", "cid2"}
	key := "test-file.txt"

	// Cache a tree
	if _, err := mm.BuildAndCache(key, cids); err != nil {
		t.Fatalf("BuildAndCache() error = %v", err)
	}

	// Retrieve cached tree
	tree, ok := mm.GetCachedTree(key)
	if !ok {
		t.Error("GetCachedTree() returned false for cached tree")
	}

	if tree == nil {
		t.Error("GetCachedTree() returned nil tree")
	}

	// Try to get non-existent tree
	_, ok = mm.GetCachedTree("nonexistent")
	if ok {
		t.Error("GetCachedTree() returned true for non-existent tree")
	}
}

func TestClearCache(t *testing.T) {
	mm := NewMerkleManager()

	// Cache some trees
	if _, err := mm.BuildAndCache("file1", []string{"cid1", "cid2"}); err != nil {
		t.Fatalf("BuildAndCache() error = %v", err)
	}
	if _, err := mm.BuildAndCache("file2", []string{"cid3", "cid4"}); err != nil {
		t.Fatalf("BuildAndCache() error = %v", err)
	}

	// Clear cache
	mm.ClearCache()

	// Verify cache is empty
	_, ok := mm.GetCachedTree("file1")
	if ok {
		t.Error("Cache still contains file1 after ClearCache()")
	}

	_, ok = mm.GetCachedTree("file2")
	if ok {
		t.Error("Cache still contains file2 after ClearCache()")
	}
}

func TestRemoveFromCache(t *testing.T) {
	mm := NewMerkleManager()

	if _, err := mm.BuildAndCache("file1", []string{"cid1", "cid2"}); err != nil {
		t.Fatalf("BuildAndCache() error = %v", err)
	}
	if _, err := mm.BuildAndCache("file2", []string{"cid3", "cid4"}); err != nil {
		t.Fatalf("BuildAndCache() error = %v", err)
	}

	// Remove one tree
	mm.RemoveFromCache("file1")

	// Verify file1 is removed
	_, ok := mm.GetCachedTree("file1")
	if ok {
		t.Error("file1 still in cache after removal")
	}

	// Verify file2 is still there
	_, ok = mm.GetCachedTree("file2")
	if !ok {
		t.Error("file2 removed from cache unexpectedly")
	}
}

func TestVerifyFileIntegrity(t *testing.T) {
	mm := NewMerkleManager()

	cids := []string{"cid1", "cid2", "cid3"}
	tree, err := mm.BuildTree(cids)
	if err != nil {
		t.Fatalf("BuildTree() error = %v", err)
	}

	expectedRoot := GetRoot(tree)

	// Test valid integrity
	err = mm.VerifyFileIntegrity(cids, expectedRoot)
	if err != nil {
		t.Errorf("VerifyFileIntegrity() error for valid data = %v", err)
	}

	// Test with wrong root
	wrongRoot := make([]byte, len(expectedRoot))
	copy(wrongRoot, expectedRoot)
	wrongRoot[0] ^= 0xFF // Flip bits to make it different

	err = mm.VerifyFileIntegrity(cids, wrongRoot)
	if err == nil {
		t.Error("VerifyFileIntegrity() should fail with wrong root")
	}

	// Test with empty CIDs
	err = mm.VerifyFileIntegrity([]string{}, expectedRoot)
	if err == nil {
		t.Error("VerifyFileIntegrity() should fail with empty CIDs")
	}

	// Test with different CIDs
	differentCIDs := []string{"cid-x", "cid-y", "cid-z"}
	err = mm.VerifyFileIntegrity(differentCIDs, expectedRoot)
	if err == nil {
		t.Error("VerifyFileIntegrity() should fail with different CIDs")
	}
}

func TestBytesEqual(t *testing.T) {
	tests := []struct {
		name string
		a    []byte
		b    []byte
		want bool
	}{
		{"equal bytes", []byte{1, 2, 3}, []byte{1, 2, 3}, true},
		{"different bytes", []byte{1, 2, 3}, []byte{1, 2, 4}, false},
		{"different lengths", []byte{1, 2}, []byte{1, 2, 3}, false},
		{"both empty", []byte{}, []byte{}, true},
		{"one empty", []byte{1}, []byte{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bytesEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("bytesEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Benchmark tests
func BenchmarkBuildTree_SmallTree(b *testing.B) {
	mm := NewMerkleManager()
	cids := []string{"cid1", "cid2", "cid3", "cid4"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mm.BuildTree(cids); err != nil {
			b.Fatalf("BuildTree() error = %v", err)
		}
	}
}

func BenchmarkBuildTree_LargeTree(b *testing.B) {
	mm := NewMerkleManager()

	// Generate 100 CIDs
	cids := make([]string, 100)
	for i := 0; i < 100; i++ {
		cids[i] = fmt.Sprintf("cid-%d", i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mm.BuildTree(cids); err != nil {
			b.Fatalf("BuildTree() error = %v", err)
		}
	}
}

func BenchmarkVerifyFileIntegrity(b *testing.B) {
	mm := NewMerkleManager()
	cids := []string{"cid1", "cid2", "cid3", "cid4", "cid5"}
	tree, err := mm.BuildTree(cids)
	if err != nil {
		b.Fatalf("BuildTree() error = %v", err)
	}
	root := GetRoot(tree)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := mm.VerifyFileIntegrity(cids, root); err != nil {
			b.Fatalf("VerifyFileIntegrity() error = %v", err)
		}
	}
}
