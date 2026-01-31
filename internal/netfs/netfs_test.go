package netfs

import "testing"

func TestCheck_LocalDir(t *testing.T) {
	dir := t.TempDir()

	network, fsName := Check(dir)
	if network {
		t.Errorf("Check(%q) = true (%s), want false for local temp dir", dir, fsName)
	}
}

func TestCheck_NonexistentPath(t *testing.T) {
	network, _ := Check("/nonexistent/path/that/does/not/exist")
	if network {
		t.Error("Check on nonexistent path should return false")
	}
}
