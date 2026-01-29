package demo

import "testing"

func TestTileColorDeterministic(t *testing.T) {
	seed := uint64(1337)
	// Same index + seed must produce same color.
	for i := 0; i < 100; i++ {
		c1 := TileColor(i, seed)
		c2 := TileColor(i, seed)
		if c1 != c2 {
			t.Errorf("TileColor(%d, %d) not deterministic: %v != %v", i, seed, c1, c2)
		}
	}
}

func TestTileColorDifferentIndices(t *testing.T) {
	seed := uint64(42)
	// Different indices should (almost always) produce different colors.
	colors := make(map[[3]byte]int)
	n := 1000
	for i := 0; i < n; i++ {
		c := TileColor(i, seed)
		colors[c]++
	}
	// With 1000 indices, we expect at least 900 distinct colors.
	if len(colors) < 900 {
		t.Errorf("expected at least 900 distinct colors from %d indices, got %d", n, len(colors))
	}
}

func TestTileColorMinBrightness(t *testing.T) {
	seed := uint64(0)
	for i := 0; i < 500; i++ {
		c := TileColor(i, seed)
		if c[0] < 40 || c[1] < 40 || c[2] < 40 {
			t.Errorf("TileColor(%d, %d) has channel below 40: %v", i, seed, c)
		}
	}
}

func TestTileColorDifferentSeeds(t *testing.T) {
	i := 0
	c1 := TileColor(i, 1)
	c2 := TileColor(i, 2)
	if c1 == c2 {
		t.Error("same index with different seeds should produce different colors")
	}
}
