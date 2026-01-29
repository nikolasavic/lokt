package demo

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ReadNextIndex reads the next tile index from the state file.
// Returns 0 if the file doesn't exist.
func ReadNextIndex(path string) (int, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is controlled
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read next-index: %w", err)
	}
	s := strings.TrimSpace(string(data))
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("parse next-index: %w", err)
	}
	return n, nil
}

// WriteNextIndex writes the next tile index to the state file.
// Uses direct overwrite for simplicity (safe under lock, racy without).
func WriteNextIndex(path string, index int) error {
	data := []byte(strconv.Itoa(index) + "\n")
	if err := os.WriteFile(path, data, 0644); err != nil { //nolint:gosec // path is controlled
		return fmt.Errorf("write next-index: %w", err)
	}
	return nil
}

// MosaicConfig holds the configuration for a mosaic demo run.
type MosaicConfig struct {
	Name       string
	GridX      int
	GridY      int
	Workers    int
	TTL        int // seconds
	Wait       bool
	Timeout    int // seconds
	FPS        int
	TileDelay  int // milliseconds
	ChunkSize  int
	ChunkDelay int // milliseconds
	Seed       uint64
	Mode       string // "lock" or "nolock"
}

// TotalTiles returns the total number of tiles in the mosaic.
func (c *MosaicConfig) TotalTiles() int {
	return c.GridX * c.GridY
}
