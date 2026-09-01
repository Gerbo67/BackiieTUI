package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
)

// HashingReader wraps an io.Reader, computing SHA-256 incrementally.
type HashingReader struct {
	src    io.Reader
	hasher hash.Hash
	tee    io.Reader
}

// NewHashingReader wraps r so every Read call feeds the SHA-256 hasher.
func NewHashingReader(r io.Reader) *HashingReader {
	h := sha256.New()
	return &HashingReader{
		src:    r,
		hasher: h,
		tee:    io.TeeReader(r, h),
	}
}

func (hr *HashingReader) Read(p []byte) (int, error) {
	return hr.tee.Read(p)
}

// Sum returns the hex-encoded SHA-256 of all data read so far.
func (hr *HashingReader) Sum() string {
	return hex.EncodeToString(hr.hasher.Sum(nil))
}

// HashBytes returns the SHA-256 hex digest of b.
func HashBytes(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
