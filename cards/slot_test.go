package cards

import (
	"sync"
	"testing"
)

// TestSlotFirstStoreWinsAndKeys: concurrent first stores publish exactly one
// entry every reader sees, and an entry never answers a different key.
func TestSlotFirstStoreWinsAndKeys(t *testing.T) {
	var s Slot
	var wg sync.WaitGroup
	got := make([]any, 16)
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v := new(int)
			*v = i
			got[i] = s.Store("{1}{U}", v)
		}(i)
	}
	wg.Wait()
	first, ok := s.Load("{1}{U}")
	if !ok {
		t.Fatal("no entry published")
	}
	for i, g := range got {
		if g != first {
			t.Fatalf("store %d saw %v, published %v", i, g, first)
		}
	}
	if _, ok := s.Load("{2}{U}"); ok {
		t.Fatal("entry answered a different key")
	}
	if v := s.Store("{2}{U}", 7); v != 7 {
		t.Fatal("a different key's store must hand back its own value")
	}
	var nilSlot *Slot
	if _, ok := nilSlot.Load("x"); ok || nilSlot.Store("x", 1) != 1 {
		t.Fatal("nil slot must be a no-op")
	}
}
