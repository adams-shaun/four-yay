package state

import (
	"testing"
	"unsafe"
)

// TestObjectCacheLinePadded pins Object to a whole number of 64-byte cache
// lines: the Objs arena is page-aligned, so a whole-line Object keeps every
// object's hot head (its first fields) on one line instead of straddling two.
func TestObjectCacheLinePadded(t *testing.T) {
	if n := unsafe.Sizeof(Object{}); n%64 != 0 {
		t.Fatalf("state.Object is %d bytes, not a whole number of 64-byte lines: re-pad its trailing _ field", n)
	}
}
