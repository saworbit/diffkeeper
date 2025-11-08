package chunk

import (
	"bytes"
	"testing"
)

func TestSplitFile(t *testing.T) {
	tests := []struct {
		name          string
		data          []byte
		chunkSize     int
		expectedChunks int
	}{
		{
			name:          "empty file",
			data:          []byte{},
			chunkSize:     100,
			expectedChunks: 0,
		},
		{
			name:          "file smaller than chunk size",
			data:          []byte("hello"),
			chunkSize:     100,
			expectedChunks: 1,
		},
		{
			name:          "file exactly chunk size",
			data:          bytes.Repeat([]byte("A"), 100),
			chunkSize:     100,
			expectedChunks: 1,
		},
		{
			name:          "file larger than chunk size",
			data:          bytes.Repeat([]byte("A"), 250),
			chunkSize:     100,
			expectedChunks: 3,
		},
		{
			name:          "invalid chunk size",
			data:          []byte("hello"),
			chunkSize:     -1,
			expectedChunks: 1, // Should return single chunk
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := SplitFile(tt.data, tt.chunkSize)

			if len(chunks) != tt.expectedChunks {
				t.Errorf("SplitFile() returned %d chunks, want %d", len(chunks), tt.expectedChunks)
				return
			}

			// Verify chunks can be reassembled
			if len(tt.data) > 0 {
				reassembled := ReassembleChunks(chunks)
				if !bytes.Equal(reassembled, tt.data) {
					t.Error("Reassembled data doesn't match original")
				}
			}
		})
	}
}

func TestReassembleChunks(t *testing.T) {
	tests := []struct {
		name     string
		chunks   [][]byte
		expected []byte
	}{
		{
			name:     "empty chunks",
			chunks:   [][]byte{},
			expected: []byte{},
		},
		{
			name:     "single chunk",
			chunks:   [][]byte{[]byte("hello")},
			expected: []byte("hello"),
		},
		{
			name:     "multiple chunks",
			chunks:   [][]byte{[]byte("hello "), []byte("world")},
			expected: []byte("hello world"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ReassembleChunks(tt.chunks)
			if !bytes.Equal(result, tt.expected) {
				t.Errorf("ReassembleChunks() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestShouldChunk(t *testing.T) {
	threshold := int64(1024 * 1024 * 1024) // 1GB

	tests := []struct {
		name     string
		fileSize int64
		want     bool
	}{
		{"small file", 100 * 1024, false},               // 100KB
		{"medium file", 500 * 1024 * 1024, false},       // 500MB
		{"exactly threshold", threshold, false},
		{"large file", 2 * 1024 * 1024 * 1024, true},    // 2GB
		{"very large file", 10 * 1024 * 1024 * 1024, true}, // 10GB
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldChunk(tt.fileSize, threshold)
			if got != tt.want {
				t.Errorf("ShouldChunk(%d, %d) = %v, want %v", tt.fileSize, threshold, got, tt.want)
			}
		})
	}
}

func TestComputeChunkHash(t *testing.T) {
	data := []byte("hello world")
	hash := ComputeChunkHash(data)

	// Verify hash is valid hex string of correct length (SHA256 = 64 hex chars)
	if len(hash) != 64 {
		t.Errorf("ComputeChunkHash() returned hash of length %d, want 64", len(hash))
	}

	// Verify deterministic
	hash2 := ComputeChunkHash(data)
	if hash != hash2 {
		t.Error("ComputeChunkHash() is not deterministic")
	}

	// Verify different data produces different hash
	hash3 := ComputeChunkHash([]byte("different data"))
	if hash == hash3 {
		t.Error("ComputeChunkHash() returned same hash for different data")
	}
}

func TestCreateMetadata(t *testing.T) {
	fileData := bytes.Repeat([]byte("A"), 1000)
	chunks := SplitFile(fileData, 300)

	metadata := CreateMetadata(fileData, chunks)

	if len(metadata) != len(chunks) {
		t.Errorf("CreateMetadata() returned %d metadata entries, want %d", len(metadata), len(chunks))
		return
	}

	// Verify all metadata has same file hash
	expectedFileHash := ComputeChunkHash(fileData)
	for i, meta := range metadata {
		if meta.FileHash != expectedFileHash {
			t.Errorf("Metadata[%d].FileHash = %s, want %s", i, meta.FileHash, expectedFileHash)
		}

		if meta.ChunkIndex != i {
			t.Errorf("Metadata[%d].ChunkIndex = %d, want %d", i, meta.ChunkIndex, i)
		}

		if meta.TotalChunks != len(chunks) {
			t.Errorf("Metadata[%d].TotalChunks = %d, want %d", i, meta.TotalChunks, len(chunks))
		}

		if meta.ChunkSize != len(chunks[i]) {
			t.Errorf("Metadata[%d].ChunkSize = %d, want %d", i, meta.ChunkSize, len(chunks[i]))
		}

		expectedChunkHash := ComputeChunkHash(chunks[i])
		if meta.ChunkHash != expectedChunkHash {
			t.Errorf("Metadata[%d].ChunkHash = %s, want %s", i, meta.ChunkHash, expectedChunkHash)
		}
	}
}

func TestVerifyChunks(t *testing.T) {
	fileData := []byte("hello world, this is a test file")
	chunks := SplitFile(fileData, 10)
	fileHash := ComputeChunkHash(fileData)

	// Test valid chunks
	err := VerifyChunks(chunks, fileHash)
	if err != nil {
		t.Errorf("VerifyChunks() failed for valid chunks: %v", err)
	}

	// Test invalid chunks (corrupted data)
	corruptedChunks := make([][]byte, len(chunks))
	copy(corruptedChunks, chunks)
	corruptedChunks[0] = []byte("corrupted")

	err = VerifyChunks(corruptedChunks, fileHash)
	if err == nil {
		t.Error("VerifyChunks() should fail for corrupted chunks")
	}
}

func TestVerifyChunkIntegrity(t *testing.T) {
	chunk := []byte("test chunk data")
	meta := ChunkMetadata{
		ChunkIndex: 0,
		ChunkSize:  len(chunk),
		ChunkHash:  ComputeChunkHash(chunk),
	}

	// Test valid chunk
	err := VerifyChunkIntegrity(chunk, meta)
	if err != nil {
		t.Errorf("VerifyChunkIntegrity() failed for valid chunk: %v", err)
	}

	// Test corrupted chunk
	corruptedChunk := []byte("corrupted data")
	err = VerifyChunkIntegrity(corruptedChunk, meta)
	if err == nil {
		t.Error("VerifyChunkIntegrity() should fail for corrupted chunk")
	}

	// Test size mismatch
	wrongSizeMeta := meta
	wrongSizeMeta.ChunkSize = 999
	err = VerifyChunkIntegrity(chunk, wrongSizeMeta)
	if err == nil {
		t.Error("VerifyChunkIntegrity() should fail for size mismatch")
	}
}

// Benchmark tests
func BenchmarkSplitFile_1MB(b *testing.B) {
	data := bytes.Repeat([]byte("A"), 1024*1024) // 1MB
	chunkSize := 4 * 1024 * 1024                  // 4MB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SplitFile(data, chunkSize)
	}
}

func BenchmarkSplitFile_100MB(b *testing.B) {
	data := bytes.Repeat([]byte("A"), 100*1024*1024) // 100MB
	chunkSize := 4 * 1024 * 1024                      // 4MB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SplitFile(data, chunkSize)
	}
}

func BenchmarkReassembleChunks(b *testing.B) {
	data := bytes.Repeat([]byte("A"), 100*1024*1024) // 100MB
	chunks := SplitFile(data, 4*1024*1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ReassembleChunks(chunks)
	}
}
