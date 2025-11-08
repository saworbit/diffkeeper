package merkle

import (
	"crypto/sha256"
	"fmt"

	"github.com/cbergoon/merkletree"
)

// MerkleManager manages Merkle trees for file integrity verification
type MerkleManager struct {
	treeCache map[string]*merkletree.MerkleTree
}

// NewMerkleManager creates a new Merkle tree manager
func NewMerkleManager() *MerkleManager {
	return &MerkleManager{
		treeCache: make(map[string]*merkletree.MerkleTree),
	}
}

// Content implements merkletree.Content interface for CID-based content
type Content struct {
	cid string
}

// CalculateHash implements the Content interface
func (c Content) CalculateHash() ([]byte, error) {
	h := sha256.New()
	if _, err := h.Write([]byte(c.cid)); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// Equals implements the Content interface
func (c Content) Equals(other merkletree.Content) (bool, error) {
	otherContent, ok := other.(Content)
	if !ok {
		return false, fmt.Errorf("type mismatch")
	}
	return c.cid == otherContent.cid, nil
}

// NewContent creates a new Content from a CID
func NewContent(cid string) Content {
	return Content{cid: cid}
}

// BuildTree builds a Merkle tree from a list of CIDs
func (m *MerkleManager) BuildTree(cids []string) (*merkletree.MerkleTree, error) {
	if len(cids) == 0 {
		return nil, fmt.Errorf("cannot build tree from empty CID list")
	}

	// Convert CIDs to Content objects
	var contents []merkletree.Content
	for _, cid := range cids {
		contents = append(contents, NewContent(cid))
	}

	// Build tree
	tree, err := merkletree.NewTree(contents)
	if err != nil {
		return nil, fmt.Errorf("failed to build Merkle tree: %w", err)
	}

	return tree, nil
}

// GetRoot returns the Merkle root hash for a tree
func GetRoot(tree *merkletree.MerkleTree) []byte {
	if tree == nil {
		return nil
	}
	return tree.MerkleRoot()
}

// VerifyTree verifies the entire Merkle tree
func VerifyTree(tree *merkletree.MerkleTree) (bool, error) {
	if tree == nil {
		return false, fmt.Errorf("cannot verify nil tree")
	}
	return tree.VerifyTree()
}

// VerifyContent verifies a specific content item in the tree
func (m *MerkleManager) VerifyContent(tree *merkletree.MerkleTree, cid string) (bool, error) {
	if tree == nil {
		return false, fmt.Errorf("cannot verify content in nil tree")
	}

	content := NewContent(cid)
	verified, err := tree.VerifyContent(content)
	if err != nil {
		return false, fmt.Errorf("failed to verify content: %w", err)
	}

	return verified, nil
}

// BuildAndCache builds a tree and caches it with a key
func (m *MerkleManager) BuildAndCache(key string, cids []string) (*merkletree.MerkleTree, error) {
	tree, err := m.BuildTree(cids)
	if err != nil {
		return nil, err
	}

	m.treeCache[key] = tree
	return tree, nil
}

// GetCachedTree retrieves a cached tree
func (m *MerkleManager) GetCachedTree(key string) (*merkletree.MerkleTree, bool) {
	tree, ok := m.treeCache[key]
	return tree, ok
}

// ClearCache clears the tree cache
func (m *MerkleManager) ClearCache() {
	m.treeCache = make(map[string]*merkletree.MerkleTree)
}

// RemoveFromCache removes a specific tree from cache
func (m *MerkleManager) RemoveFromCache(key string) {
	delete(m.treeCache, key)
}

// FileIntegrity represents the integrity information for a file
type FileIntegrity struct {
	FilePath   string   // Path to the file
	CIDs       []string // List of CIDs (chunks or diffs)
	MerkleRoot []byte   // Merkle root hash
	IsValid    bool     // Validation result
}

// VerifyFileIntegrity verifies the integrity of a file using its CIDs and expected root
func (m *MerkleManager) VerifyFileIntegrity(cids []string, expectedRoot []byte) error {
	if len(cids) == 0 {
		return fmt.Errorf("cannot verify integrity with empty CID list")
	}

	// Build tree from CIDs
	tree, err := m.BuildTree(cids)
	if err != nil {
		return fmt.Errorf("failed to build tree for verification: %w", err)
	}

	// Verify tree structure
	valid, err := VerifyTree(tree)
	if err != nil {
		return fmt.Errorf("tree verification failed: %w", err)
	}

	if !valid {
		return fmt.Errorf("tree structure is invalid")
	}

	// Compare roots
	actualRoot := GetRoot(tree)
	if !bytesEqual(actualRoot, expectedRoot) {
		return fmt.Errorf("merkle root mismatch: expected %x, got %x", expectedRoot, actualRoot)
	}

	return nil
}

// bytesEqual compares two byte slices
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// GenerateProof generates a Merkle proof for a specific CID in the tree
func (m *MerkleManager) GenerateProof(tree *merkletree.MerkleTree, cid string) ([][]byte, error) {
	if tree == nil {
		return nil, fmt.Errorf("cannot generate proof from nil tree")
	}

	content := NewContent(cid)
	path, indices, err := tree.GetMerklePath(content)
	if err != nil {
		return nil, fmt.Errorf("failed to generate proof: %w", err)
	}

	// Combine path and indices into proof
	// Note: This is a simplified version. In production, you might want to
	// include more information like the indices
	_ = indices // Currently unused but available for extended proof format
	return path, nil
}

// VerifyProof verifies a Merkle proof for a specific content
func (m *MerkleManager) VerifyProof(tree *merkletree.MerkleTree, cid string, proof [][]byte) (bool, error) {
	if tree == nil {
		return false, fmt.Errorf("cannot verify proof against nil tree")
	}

	content := NewContent(cid)

	// Calculate content hash
	contentHash, err := content.CalculateHash()
	if err != nil {
		return false, fmt.Errorf("failed to calculate content hash: %w", err)
	}

	// Reconstruct root from proof
	currentHash := contentHash
	for _, proofElement := range proof {
		h := sha256.New()
		// Concatenate hashes (simplified - production version should handle ordering)
		h.Write(currentHash)
		h.Write(proofElement)
		currentHash = h.Sum(nil)
	}

	// Compare with tree root
	treeRoot := GetRoot(tree)
	return bytesEqual(currentHash, treeRoot), nil
}
