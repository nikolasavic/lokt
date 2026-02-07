//go:build unix

package doctor

import "testing"

func TestFsTypeToName(t *testing.T) {
	tests := []struct {
		fsType int64
		want   string
	}{
		{0x6969, "nfs"},
		{0xff534d42, "cifs"},
		{0x517B, "cifs"},
		{0x564c, "ncp"},
		{0x5346414F, "afs"},
		{0x65735546, "fuse"},
		{0x9123683E, "btrfs"},
		{0xEF53, "ext4"},
		{0x01021994, "tmpfs"},
		{0x4244, "hfs"},
		{0x482b, "hfs+"},
		{0x1badface, "apfs"},
		{0x1234, "0x1234"},
	}

	for _, tt := range tests {
		got := fsTypeToName(tt.fsType)
		if got != tt.want {
			t.Errorf("fsTypeToName(0x%x) = %q, want %q", tt.fsType, got, tt.want)
		}
	}
}
