package chunk

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// ChunkMetadata contains metadata about a file chunk
type ChunkMetadata struct {
	FileHash    string // SHA256 hash of the entire file
	ChunkIndex  int    // Index of this chunk (0-based)
	TotalChunks int    // Total number of chunks in the file
	ChunkSize   int    // Size of this chunk in bytes
	ChunkHash   string // SHA256 hash of this chunk's data
}

// ChunkDiff represents a diff for a single chunk
type ChunkDiff struct {
	Index int    // Chunk index
	Data  []byte // Diff data for this chunk
	Hash  string // Hash of the diff data
}

// SplitFile splits data into fixed-size chunks
func SplitFile(data []byte, chunkSizeBytes int) [][]byte {
	if len(data) == 0 {
		return [][]byte{}
	}

	if chunkSizeBytes <= 0 {
		// Invalid chunk size, return entire file as single chunk
		return [][]byte{data}
	}

	var chunks [][]byte
	for i := 0; i < len(data); i += chunkSizeBytes {
		end := i + chunkSizeBytes
		if end > len(data) {
			end = len(data)
		}
		chunks = append(chunks, data[i:end])
	}

	return chunks
}

// ReassembleChunks combines chunks back into a single file
func ReassembleChunks(chunks [][]byte) []byte {
	if len(chunks) == 0 {
		return []byte{}
	}

	// Calculate total size
	totalSize := 0
	for _, chunk := range chunks {
		totalSize += len(chunk)
	}

	// Allocate buffer and copy chunks
	result := make([]byte, 0, totalSize)
	for _, chunk := range chunks {
		result = append(result, chunk...)
	}

	return result
}

// ShouldChunk returns true if a file should be chunked based on threshold
func ShouldChunk(fileSize int64, thresholdBytes int64) bool {
	return fileSize > thresholdBytes
}

// ComputeChunkHash computes SHA256 hash of chunk data
func ComputeChunkHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// CreateMetadata creates metadata for chunks
func CreateMetadata(fileData []byte, chunks [][]byte) []ChunkMetadata {
	fileHash := ComputeChunkHash(fileData)
	totalChunks := len(chunks)

	metadata := make([]ChunkMetadata, totalChunks)
	for i, chunk := range chunks {
		metadata[i] = ChunkMetadata{
			FileHash:    fileHash,
			ChunkIndex:  i,
			TotalChunks: totalChunks,
			ChunkSize:   len(chunk),
			ChunkHash:   ComputeChunkHash(chunk),
		}
	}

	return metadata
}

// VerifyChunks verifies that chunks reassemble to the expected file hash
func VerifyChunks(chunks [][]byte, expectedFileHash string) error {
	reassembled := ReassembleChunks(chunks)
	actualHash := ComputeChunkHash(reassembled)

	if actualHash != expectedFileHash {
		return fmt.Errorf("chunk verification failed: expected hash %s, got %s", expectedFileHash, actualHash)
	}

	return nil
}

// VerifyChunkIntegrity verifies a single chunk against its metadata
func VerifyChunkIntegrity(chunk []byte, meta ChunkMetadata) error {
	actualHash := ComputeChunkHash(chunk)

	if actualHash != meta.ChunkHash {
		return fmt.Errorf("chunk %d integrity check failed: expected hash %s, got %s",
			meta.ChunkIndex, meta.ChunkHash, actualHash)
	}

	if len(chunk) != meta.ChunkSize {
		return fmt.Errorf("chunk %d size mismatch: expected %d bytes, got %d bytes",
			meta.ChunkIndex, meta.ChunkSize, len(chunk))
	}

	return nil
}
