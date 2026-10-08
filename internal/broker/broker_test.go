//go:build broker

package broker

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/view"
)

// fakeSeat is a seat that returns a fixed intent and counts its Decide calls.
type fakeSeat struct {
	calls atomic.Int64
}

func (f *fakeSeat) Decide(context.Context, view.View, decision.Decision) (decision.Intent, error) {
	f.calls.Add(1)
	return decision.Intent{}, nil
}

// countingScorer records the widths it is handed.
type countingScorer struct {
	mu     sync.Mutex
	widths []int
	delay  time.Duration
}

func (c *countingScorer) Serve(batch []*Request) error {
	if c.delay > 0 {
		time.Sleep(c.delay)
	}
	c.mu.Lock()
	c.widths = append(c.widths, len(batch))
	c.mu.Unlock()
	return nil
}

func (c *countingScorer) Name() string { return "counting" }

// TestWrapPreservesIntent pins the wrapper's contract: the wrapped seat still
// decides, so wrapping changes only WHEN a decision is served, never WHAT.
func TestWrapPreservesIntent(t *testing.T) {
	br, err := New(Options{MaxBatch: 8}, PassThrough{})
	if err != nil {
		t.Fatal(err)
	}
	defer br.Stop()
	inner := &fakeSeat{}
	w := br.Wrap(inner, 0)
	if _, err := w.Decide(context.Background(), view.View{}, decision.Decision{}); err != nil {
		t.Fatal(err)
	}
	if got := inner.calls.Load(); got != 1 {
		t.Fatalf("inner seat saw %d Decide calls, want 1", got)
	}
}

// TestServesEveryDecision pins that no request is dropped: the number of
// requests the broker serves equals the number submitted, across many
// concurrent submitters.
func TestServesEveryDecision(t *testing.T) {
	const n = 2000
	sc := &countingScorer{}
	br, err := New(Options{MaxBatch: 32}, sc)
	if err != nil {
		t.Fatal(err)
	}
	inner := &fakeSeat{}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := br.Wrap(inner, 0)
			_, _ = w.Decide(context.Background(), view.View{}, decision.Decision{})
		}()
	}
	wg.Wait()
	st := br.Stop()
	if got := st.States; got != n {
		t.Fatalf("served %d states, want %d", got, n)
	}
	if got := inner.calls.Load(); got != n {
		t.Fatalf("inner seat saw %d decisions, want %d", got, n)
	}
	var sum int
	for _, w := range sc.widths {
		if w <= 0 || w > 32 {
			t.Fatalf("served a batch of width %d, want 1..32", w)
		}
		sum += w
	}
	if sum != n {
		t.Fatalf("batch widths sum to %d, want %d", sum, n)
	}
}

// TestWaitWidensBatches pins the documented no-barrier envelope: with Wait=0
// the ready set is served immediately (narrow), and a positive Wait lets
// stragglers join (wider). This is the latency-for-width trade the report
// quotes, asserted rather than assumed.
func TestWaitWidensBatches(t *testing.T) {
	run := func(wait time.Duration) float64 {
		sc := &countingScorer{delay: 200 * time.Microsecond}
		br, err := New(Options{MaxBatch: 64, Wait: wait}, sc)
		if err != nil {
			t.Fatal(err)
		}
		const n = 512
		inner := &fakeSeat{}
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				// Stagger arrivals so a Wait window can actually collect.
				time.Sleep(time.Duration(i%64) * 20 * time.Microsecond)
				w := br.Wrap(inner, 0)
				_, _ = w.Decide(context.Background(), view.View{}, decision.Decision{})
			}(i)
		}
		wg.Wait()
		return br.Stop().MeanWidth()
	}
	narrow := run(0)
	// A generous wait relative to the scorer delay must collect wider batches
	// than serving the ready set immediately.
	wide := run(5 * time.Millisecond)
	if !(wide > narrow) {
		t.Fatalf("mean width did not widen with a wait: wait=0 -> %.2f, wait=5ms -> %.2f", narrow, wide)
	}
}

// TestStatsBucketsAreConsistent pins that the width histogram accounts for
// every served call.
func TestStatsBucketsAreConsistent(t *testing.T) {
	sc := &countingScorer{}
	br, err := New(Options{MaxBatch: 16}, sc)
	if err != nil {
		t.Fatal(err)
	}
	inner := &fakeSeat{}
	for i := 0; i < 100; i++ {
		w := br.Wrap(inner, 0)
		_, _ = w.Decide(context.Background(), view.View{}, decision.Decision{})
	}
	st := br.Stop()
	var bucketTotal int64
	for _, b := range st.Buckets {
		bucketTotal += b
	}
	if bucketTotal != st.Served {
		t.Fatalf("bucket total %d != served %d", bucketTotal, st.Served)
	}
}
