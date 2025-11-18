package chunk

import (
	"bufio"
	"crypto/sha256"
	"errors"
	"io"
	"math/bits"
	"time"
)

// ChunkRef captures the identifying information for a content-defined chunk.
type ChunkRef struct {
	Hash   [32]byte // Strong hash used as the CAS key (SHA256)
	Offset uint64   // Byte offset within the file
	Length uint32   // Length of the chunk
}

// Manifest describes the chunk layout for a single file mutation.
type Manifest struct {
	Version   uint64     `json:"version"`
	Timestamp time.Time  `json:"timestamp"`
	Chunks    []ChunkRef `json:"chunks"`
}

// Params controls the content-defined chunker.
type Params struct {
	MinSize int // Minimum chunk size in bytes
	AvgSize int // Target average chunk size in bytes
	MaxSize int // Hard maximum chunk size in bytes
	Window  int // Rolling hash window size
}

// Chunk holds a chunk's byte data and reference metadata.
type Chunk struct {
	Ref  ChunkRef
	Data []byte
}

// RabinChunker performs content-defined chunking using a rolling Rabin-Karp hash.
type RabinChunker struct {
	r      *bufio.Reader
	params Params
	offset uint64
	mask   uint64
	hash   *rollingHash
}

// NewRabinChunker builds a streaming chunker over the provided reader.
func NewRabinChunker(r io.Reader, params Params) *RabinChunker {
	p := params.normalize()
	return &RabinChunker{
		r:      bufio.NewReaderSize(r, p.MaxSize),
		params: p,
		mask:   avgToMask(p.AvgSize),
		hash:   newRollingHash(p.Window),
	}
}

// Next returns the next content-defined chunk or io.EOF when complete.
// It never holds more than MaxSize bytes in memory for a single chunk.
func (c *RabinChunker) Next() (Chunk, error) {
	if c == nil || c.r == nil {
		return Chunk{}, errors.New("chunker not initialized")
	}

	buf := make([]byte, 0, c.params.AvgSize)
	for {
		b, err := c.r.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if len(buf) == 0 {
					return Chunk{}, io.EOF
				}
				break
			}
			return Chunk{}, err
		}

		buf = append(buf, b)
		c.hash.push(b)

		if len(buf) < c.params.MinSize {
			continue
		}

		// Cut at a boundary once min is satisfied, either via hash match or max length.
		if (c.hash.sum()&c.mask) == 0 || len(buf) >= c.params.MaxSize {
			break
		}
	}

	sum := sha256.Sum256(buf)
	ref := ChunkRef{
		Hash:   sum,
		Offset: c.offset,
		Length: uint32(len(buf)),
	}
	c.offset += uint64(len(buf))

	return Chunk{Ref: ref, Data: buf}, nil
}

// normalize ensures sane defaults and bounds for chunking parameters.
func (p Params) normalize() Params {
	if p.MinSize <= 0 {
		p.MinSize = 1 << 20 // 1 MiB
	}
	if p.AvgSize <= 0 {
		p.AvgSize = 8 << 20 // 8 MiB
	}
	if p.MaxSize <= 0 {
		p.MaxSize = 64 << 20 // 64 MiB
	}
	if p.Window <= 0 {
		p.Window = 64
	}
	if p.MinSize > p.AvgSize {
		p.AvgSize = p.MinSize
	}
	if p.AvgSize > p.MaxSize {
		p.MaxSize = p.AvgSize
	}
	return p
}

// avgToMask selects a mask that approximates the target average chunk size.
func avgToMask(avg int) uint64 {
	if avg <= 0 {
		avg = 8 << 20
	}
	// Choose the nearest power-of-two mask to the requested average.
	bitWidth := bits.Len(uint(avg))
	if bitWidth < 8 {
		bitWidth = 8
	}
	if bitWidth > 62 {
		bitWidth = 62
	}
	return (1 << (bitWidth - 1)) - 1
}

type rollingHash struct {
	window int
	base   uint64
	mod    uint64
	pow    uint64
	hash   uint64
	buf    []byte
}

func newRollingHash(window int) *rollingHash {
	if window <= 0 {
		window = 64
	}
	const (
		base = 256
		mod  = (1 << 61) - 1 // large prime for stability
	)

	pow := uint64(1)
	for i := 0; i < window-1; i++ {
		pow = (pow * base) % mod
	}

	return &rollingHash{
		window: window,
		base:   base,
		mod:    mod,
		pow:    pow,
		buf:    make([]byte, 0, window),
	}
}

func (r *rollingHash) push(b byte) {
	if len(r.buf) == r.window {
		out := r.buf[0]
		r.buf = r.buf[1:]
		r.hash = (r.hash + r.mod - (uint64(out)*r.pow)%r.mod) % r.mod
	}

	r.buf = append(r.buf, b)
	r.hash = (r.hash*r.base + uint64(b)) % r.mod
}

func (r *rollingHash) sum() uint64 {
	return r.hash
}
