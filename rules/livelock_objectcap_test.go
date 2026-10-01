package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
)

// TestLivelockWatcherObjectCapFires pins the watcher's mid-resolution
// object-count arm in isolation: observeFrom panics with a *LivelockError
// whose Reason is the object cap, carrying the arena population it saw and
// the cap it was armed with. The check lives on the per-event path, so the
// abort lands one event after the arena crosses the cap -- the whole point
// (no decision boundary is needed).
func TestLivelockWatcherObjectCapFires(t *testing.T) {
	t.Parallel()
	w := newLivelockWatcher(&LoopGuard{MaxObjs: 5})
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("object cap did not abort at arena 6 > cap 5")
		}
		le, ok := r.(*LivelockError)
		if !ok {
			t.Fatalf("panic value %T, want *LivelockError", r)
		}
		if le.Reason != "object cap" {
			t.Fatalf("reason %q, want object cap", le.Reason)
		}
		if le.Count != 6 || le.Cap != 5 {
			t.Fatalf("count/cap = %d/%d, want 6/5", le.Count, le.Cap)
		}
		if le.Kind != events.TokenCreate {
			t.Fatalf("aborting kind %v, want token_create", le.Kind)
		}
	}()
	w.observeFrom(&events.Event{Kind: events.TokenCreate, Seq: 1}, 0, 4)
	w.observeFrom(&events.Event{Kind: events.TokenCreate, Seq: 2}, 0, 5)
	w.observeFrom(&events.Event{Kind: events.TokenCreate, Seq: 3}, 0, 6)
}

// TestLivelockWatcherObjectCapZeroIsOff pins the zero value: an unarmed
// guard (MaxObjs 0) observes the same events without ever firing, so every
// existing Config keeps its behaviour.
func TestLivelockWatcherObjectCapZeroIsOff(t *testing.T) {
	t.Parallel()
	w := newLivelockWatcher(&LoopGuard{})
	for i := 0; i < 100; i++ {
		w.observeFrom(&events.Event{Kind: events.TokenCreate, Seq: uint64(i + 1)}, 0, 100000)
	}
}

// TestLivelockWatcherObjectCapDisabledIsOff pins that the embedder opt-out
// still wins: a Disabled guard observes nothing at all, cap armed or not.
func TestLivelockWatcherObjectCapDisabledIsOff(t *testing.T) {
	t.Parallel()
	w := newLivelockWatcher(&LoopGuard{MaxObjs: 1, Disabled: true})
	for i := 0; i < 100; i++ {
		w.observeFrom(&events.Event{Kind: events.TokenCreate, Seq: uint64(i + 1)}, 0, 100000)
	}
}
