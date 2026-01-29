// Package demo implements the terminal mosaic demo for Lokt.
package demo

// TileColor computes a deterministic RGB color for tile index i with the given seed.
// Uses splitmix64 to produce well-distributed colors.
func TileColor(i int, seed uint64) [3]byte {
	h := splitmix64(seed ^ uint64(i)) //nolint:gosec // G115: index is always non-negative
	r := byte(h)
	g := byte(h >> 8)
	b := byte(h >> 16)

	// Boost brightness: ensure no channel is below 40
	if r < 40 {
		r += 40
	}
	if g < 40 {
		g += 40
	}
	if b < 40 {
		b += 40
	}
	return [3]byte{r, g, b}
}

// splitmix64 is a fast, high-quality 64-bit hash function.
func splitmix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}
