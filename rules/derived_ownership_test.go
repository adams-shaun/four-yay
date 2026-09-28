package rules

import (
	"fmt"
	"slices"
	"sync"
	"testing"
)

// TestDerivedScratchNotSharedWithClone pins the ownership invariant that
// rules/layers.go's Derived buffer reuse (derivedKW / derivedTypes) depends
// on: Engine.Clone must NOT copy those scratch buffers (rules/clone.go), so
// a clone grows its own, and a Derived call on one engine can never overwrite
// or alias the buffer the other engine is reading. If Clone were to share the
// buffer fields, the two engines below would write into the same backing
// array and corrupt each other's Derived result. This runs both engines'
// Derived in parallel goroutines so a shared buffer races, not just aliases.
func TestDerivedScratchNotSharedWithClone(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:SerraAngel\nManaCost:3 WW\nTypes:Creature Angel\nPT:4/4\nK:Flying\nOracle:x\n")
	want := []string{"Flying"}
	// Grow the original's scratch buffers before cloning so a clone that
	// aliases them (rather than growing its own on first use) fails here.
	if kw := e.Derived(id).Keywords; !slices.Equal(kw, want) {
		t.Fatalf("set up: derived keywords = %v, want %v", kw, want)
	}

	c := e.Clone()
	if kw := c.Derived(id).Keywords; !slices.Equal(kw, want) {
		t.Fatalf("clone derived keywords = %v, want %v", kw, want)
	}

	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	run := func(eng *Engine) {
		defer wg.Done()
		for i := 0; i < 20000; i++ {
			// A full Derived rewrites derivedKW/derivedTypes; a shared buffer
			// between the two engines would make one's rewrite clobber the
			// other's in-flight result and trip here (and race under -race).
			if kw := eng.Derived(id); !slices.Equal(kw.Keywords, want) {
				errCh <- fmt.Errorf("engine %p derived keywords corrupted at iter %d: %v", eng, i, kw.Keywords)
				return
			}
			// The scalar fast path must stay independent of the buffers too.
			if p, tn := eng.Power(id), eng.Toughness(id); p != 4 || tn != 4 {
				errCh <- fmt.Errorf("engine %p PT corrupted at iter %d: %d/%d", eng, i, p, tn)
				return
			}
			// A second object works the same reusable scratch concurrently.
			if kw := eng.Derived(id); !slices.Equal(kw.Keywords, want) {
				errCh <- fmt.Errorf("engine %p re-derived keywords corrupted at iter %d: %v", eng, i, kw.Keywords)
				return
			}
		}
	}
	wg.Add(2)
	go run(e)
	go run(c)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

// TestDerivedMatchesClone is a sequential cross-engine double-check: the
// full Derived result (P/T + keywords) on an engine and on its clone must be
// identical and read-only-stable, even though each owns separate scratch.
func TestDerivedMatchesClone(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nK:Trample\nOracle:x\n")
	e.G.Obj(id).AddCounter("P1P1", 1)
	c := e.Clone()
	a := e.Derived(id)
	b := c.Derived(id)
	if a.Power != b.Power || a.Toughness != b.Toughness || !slices.Equal(a.Keywords, b.Keywords) {
		t.Fatalf("original %+v vs clone %+v", a, b)
	}
	if a.Power != 3 {
		t.Fatalf("expected 2/2 +1 counter = 3 power, got %+v", a)
	}
}
