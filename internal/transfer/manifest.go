// Package transfer implements verified, resumable file-transfer primitives.
package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

const DefaultChunkSize int64 = 4 << 20

type Manifest struct {
	Size      int64    `json:"size"`
	ChunkSize int64    `json:"chunkSize"`
	Chunks    []string `json:"chunks"`
}

func BuildManifest(r io.Reader, chunkSize int64) (Manifest, error) {
	if chunkSize <= 0 {
		return Manifest{}, fmt.Errorf("invalid chunk size")
	}
	result := Manifest{ChunkSize: chunkSize}
	buf := make([]byte, chunkSize)
	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			sum := sha256.Sum256(buf[:n])
			result.Chunks = append(result.Chunks, hex.EncodeToString(sum[:]))
			result.Size += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return Manifest{}, err
		}
	}
	return result, nil
}
func (m Manifest) ChunkLength(index int) (int64, error) {
	if index < 0 || index >= len(m.Chunks) {
		return 0, fmt.Errorf("invalid chunk index")
	}
	if index == len(m.Chunks)-1 {
		return m.Size - int64(index)*m.ChunkSize, nil
	}
	return m.ChunkSize, nil
}
func (m Manifest) Verify(index int, body []byte) error {
	wantLen, err := m.ChunkLength(index)
	if err != nil {
		return err
	}
	if int64(len(body)) != wantLen {
		return fmt.Errorf("chunk %d length mismatch", index)
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != m.Chunks[index] {
		return fmt.Errorf("chunk %d hash mismatch", index)
	}
	return nil
}
