package main

import (
	"os"
	"path/filepath"
	"testing"

	"go.etcd.io/bbolt"
)

func TestCompressDecompress(t *testing.T) {
	testData := []byte("Hello, DiffKeeper! This is test data that should compress well.")
	
	compressed, err := compressData(testData)
	if err != nil {
		t.Fatalf("Compression failed: %v", err)
	}
	
	if len(compressed) >= len(testData) {
		t.Logf("Warning: Compressed size (%d) >= original size (%d)", len(compressed), len(testData))
	}
	
	decompressed, err := decompressData(compressed)
	if err != nil {
		t.Fatalf("Decompression failed: %v", err)
	}
	
	if string(decompressed) != string(testData) {
		t.Errorf("Decompressed data doesn't match original.\nExpected: %s\nGot: %s", testData, decompressed)
	}
}

func TestDiffKeeperLifecycle(t *testing.T) {
	// Create temporary directories
	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, "state")
	storePath := filepath.Join(tmpDir, "test.bolt")
	
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("Failed to create state dir: %v", err)
	}
	
	// Initialize DiffKeeper
	dk, err := NewDiffKeeper(stateDir, storePath)
	if err != nil {
		t.Fatalf("Failed to create DiffKeeper: %v", err)
	}
	defer dk.Close()
	
	// Test file creation and capture
	testFile := filepath.Join(stateDir, "test.txt")
	testContent := []byte("Initial content")
	
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	
	// Capture the file
	if err := dk.BlueShift(testFile); err != nil {
		t.Fatalf("BlueShift failed: %v", err)
	}
	
	// Modify the file
	updatedContent := []byte("Updated content")
	if err := os.WriteFile(testFile, updatedContent, 0644); err != nil {
		t.Fatalf("Failed to update test file: %v", err)
	}
	
	// Capture the update
	if err := dk.BlueShift(testFile); err != nil {
		t.Fatalf("BlueShift update failed: %v", err)
	}
	
	// Remove the file to simulate loss
	if err := os.Remove(testFile); err != nil {
		t.Fatalf("Failed to remove test file: %v", err)
	}
	
	// Verify file is gone
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Fatal("Test file should not exist")
	}
	
	// RedShift should restore it
	if err := dk.RedShift(); err != nil {
		t.Fatalf("RedShift failed: %v", err)
	}
	
	// Verify file was restored with latest content
	restored, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read restored file: %v", err)
	}
	
	if string(restored) != string(updatedContent) {
		t.Errorf("Restored content doesn't match.\nExpected: %s\nGot: %s", updatedContent, restored)
	}
}

func TestMultipleFilesRedShift(t *testing.T) {
	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, "state")
	storePath := filepath.Join(tmpDir, "test.bolt")
	
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("Failed to create state dir: %v", err)
	}
	
	dk, err := NewDiffKeeper(stateDir, storePath)
	if err != nil {
		t.Fatalf("Failed to create DiffKeeper: %v", err)
	}
	defer dk.Close()
	
	// Create multiple test files
	testFiles := map[string]string{
		"config.json":  `{"setting": "value"}`,
		"data.txt":     "Some data content",
		"subdir/nested.log": "Nested file content",
	}
	
	for path, content := range testFiles {
		fullPath := filepath.Join(stateDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create dir: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write file %s: %v", path, err)
		}
		if err := dk.BlueShift(fullPath); err != nil {
			t.Fatalf("BlueShift failed for %s: %v", path, err)
		}
	}
	
	// Remove all files
	if err := os.RemoveAll(stateDir); err != nil {
		t.Fatalf("Failed to remove state dir: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("Failed to recreate state dir: %v", err)
	}
	
	// Restore all files
	if err := dk.RedShift(); err != nil {
		t.Fatalf("RedShift failed: %v", err)
	}
	
	// Verify all files were restored
	for path, expectedContent := range testFiles {
		fullPath := filepath.Join(stateDir, path)
		restored, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("Failed to read restored file %s: %v", path, err)
			continue
		}
		if string(restored) != expectedContent {
			t.Errorf("File %s content mismatch.\nExpected: %s\nGot: %s", path, expectedContent, restored)
		}
	}
}

func TestNoChangeNoDelta(t *testing.T) {
	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, "state")
	storePath := filepath.Join(tmpDir, "test.bolt")
	
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("Failed to create state dir: %v", err)
	}
	
	dk, err := NewDiffKeeper(stateDir, storePath)
	if err != nil {
		t.Fatalf("Failed to create DiffKeeper: %v", err)
	}
	defer dk.Close()
	
	testFile := filepath.Join(stateDir, "test.txt")
	testContent := []byte("Unchanging content")
	
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	
	// Capture first time
	if err := dk.BlueShift(testFile); err != nil {
		t.Fatalf("First BlueShift failed: %v", err)
	}
	
	// Capture again without changes - should be no-op
	if err := dk.BlueShift(testFile); err != nil {
		t.Fatalf("Second BlueShift failed: %v", err)
	}
	
	// Verify only one delta was stored
	count := 0
	dk.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketDeltas))
		c := b.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			count++
		}
		return nil
	})
	
	if count != 1 {
		t.Errorf("Expected 1 delta, got %d", count)
	}
}

func BenchmarkBlueShift(b *testing.B) {
	tmpDir := b.TempDir()
	stateDir := filepath.Join(tmpDir, "state")
	storePath := filepath.Join(tmpDir, "bench.bolt")
	
	os.MkdirAll(stateDir, 0755)
	
	dk, err := NewDiffKeeper(stateDir, storePath)
	if err != nil {
		b.Fatalf("Failed to create DiffKeeper: %v", err)
	}
	defer dk.Close()
	
	testFile := filepath.Join(stateDir, "bench.txt")
	testContent := []byte("Benchmark content that changes")
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		content := append(testContent, byte(i%256))
		os.WriteFile(testFile, content, 0644)
		dk.BlueShift(testFile)
	}
}

func BenchmarkRedShift(b *testing.B) {
	tmpDir := b.TempDir()
	stateDir := filepath.Join(tmpDir, "state")
	storePath := filepath.Join(tmpDir, "bench.bolt")
	
	os.MkdirAll(stateDir, 0755)
	
	dk, err := NewDiffKeeper(stateDir, storePath)
	if err != nil {
		b.Fatalf("Failed to create DiffKeeper: %v", err)
	}
	defer dk.Close()
	
	// Create 100 test files
	for i := 0; i < 100; i++ {
		testFile := filepath.Join(stateDir, "file_"+string(rune(i))+".txt")
		content := []byte("Content for file " + string(rune(i)))
		os.WriteFile(testFile, content, 0644)
		dk.BlueShift(testFile)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		os.RemoveAll(stateDir)
		os.MkdirAll(stateDir, 0755)
		dk.RedShift()
	}
}