package host

import (
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/view"
)

// retentionMatches is how many matches the retention tests play on one
// perpetual table.
const retentionMatches = 20

// retentionHeapGrowthBound is the most HeapInuse may grow between the end of
// match 5 and the end of match retentionMatches in persistence mode. A
// retained live engine costs ~9 MB (the 80k-slot reserved log alone), so
// fifteen retained matches would be >100 MB; the bound leaves room for GC
// noise, the single-slot t.loaded cache and the sidecar index.
const retentionHeapGrowthBound = 48 << 20

// retentionCooldown marks the between-matches Sleep call: Sleep is also
// called after every decision with the table's Pace (0 here), and the hook
// is a no-op either way, so the value is only a discriminator.
const retentionCooldown = time.Hour

func twoSeatPerpetual(id TableID) TableConfig {
	return TableConfig{ID: id, Name: "Table " + string(id), Seats: 2, Decks: []string{"a", "b"},
		Seed: 7, Pace: 0, Spectator: view.Omniscient, Perpetual: true}
}

func heapInuse() uint64 {
	runtime.GC()
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapInuse
}

// TestFinishedMatchesReleaseEngine is the persistence-mode retention
// regression: a long-running table must not keep every finished match's
// engine alive (the demo's heap reached ~6 GB at ~535 matches, 4.88 GB of it
// reserved event logs reached through t.history). Finished matches are
// served from disk, the listing still names every match once, and a
// feedback snapshot between matches still captures the just-finished one.
func TestFinishedMatchesReleaseEngine(t *testing.T) {
	dir := t.TempDir()
	o := diskOptions(t, dir)
	var r *Registry
	var (
		ended     int
		heap5     uint64
		heapN     uint64
		retained  []int // len(t.history) after each match
		withE     []int // retained matches still holding an engine
		live1     []protocol.EventBody
		fbMatch   []int
		fbHeadErr []string
	)
	o.OnMatchEnd = func(_ TableID, k int, _ protocol.MatchInfo) error {
		if k == 1 {
			// t.cur is still match 1 here: this is the live engine's own
			// projection, the reference the disk-served one must equal.
			evs, err := r.Events("t1", 1, 0)
			if err != nil {
				t.Errorf("live Events(1): %v", err)
			}
			live1 = evs
		}
		return nil
	}
	o.Cooldown = retentionCooldown
	o.Sleep = func(d time.Duration, stop <-chan struct{}) {
		if d != retentionCooldown {
			return // a per-decision Pace sleep, not the between-matches cooldown
		}
		ended++
		r.mu.RLock()
		tb := r.tables["t1"]
		r.mu.RUnlock()
		tb.mu.RLock()
		retained = append(retained, len(tb.history))
		n := 0
		for _, m := range tb.history {
			m.mu.RLock()
			if m.e != nil {
				n++
			}
			m.mu.RUnlock()
		}
		tb.mu.RUnlock()
		withE = append(withE, n)

		// Between matches: t.cur is nil, so feedback must reach the match
		// that just finished.
		snap, err := r.SnapshotForFeedback("t1", nil)
		if err != nil {
			fbHeadErr = append(fbHeadErr, err.Error())
		} else {
			fbMatch = append(fbMatch, snap.Match.Match)
			if snap.Log.Head != snap.Match.Head {
				fbHeadErr = append(fbHeadErr, "snapshot log head "+snap.Log.Head+" != sidecar head "+snap.Match.Head)
			}
		}

		switch ended {
		case 5:
			heap5 = heapInuse()
		case retentionMatches:
			heapN = heapInuse()
			go r.Close()
			<-r.Done()
		}
	}
	var err error
	r, err = New(o)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(twoSeatPerpetual("t1")); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	r.Wait("t1")

	if ended < retentionMatches {
		t.Fatalf("only %d matches finished, want %d", ended, retentionMatches)
	}
	for i, n := range retained {
		if n > memoryHistoryLimit || withE[i] != 0 {
			t.Errorf("after match %d t.history holds %d matches (limit %d), %d of them with a live engine",
				i+1, n, memoryHistoryLimit, withE[i])
			break
		}
	}
	if n := len(withE); n > 0 && withE[n-1] != 0 {
		t.Errorf("after match %d, %d finished matches still hold a live engine", n, withE[n-1])
	}
	t.Logf("HeapInuse after match 5: %d MB, after match %d: %d MB", heap5>>20, retentionMatches, heapN>>20)
	if growth := int64(heapN) - int64(heap5); growth > retentionHeapGrowthBound {
		t.Errorf("HeapInuse grew %d MB between match 5 and match %d (bound %d MB)",
			growth>>20, retentionMatches, retentionHeapGrowthBound>>20)
	}
	if len(fbHeadErr) != 0 {
		t.Fatalf("feedback between matches: %v", fbHeadErr)
	}
	for i, k := range fbMatch {
		if k != i+1 {
			t.Fatalf("feedback after match %d snapshotted match %d", i+1, k)
		}
	}

	// The listing names every match exactly once, ascending.
	ms, err := r.Matches("t1")
	if err != nil {
		t.Fatal(err)
	}
	for i, mi := range ms {
		if mi.Match != i+1 {
			t.Fatalf("listing[%d] is match %d: %+v", i, mi.Match, ms)
		}
	}
	if len(ms) < retentionMatches {
		t.Fatalf("listing has %d matches, want >= %d", len(ms), retentionMatches)
	}

	// An early finished match is still served, from disk, exactly.
	got, err := r.Events("t1", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(live1) == 0 || !reflect.DeepEqual(got, live1) {
		t.Fatalf("Events(1) from disk (%d bodies) differs from the live projection (%d bodies)", len(got), len(live1))
	}
	if len(got) != ms[0].Events {
		t.Fatalf("Events(1) served %d bodies, listing records %d", len(got), ms[0].Events)
	}
	if _, err := r.ViewAt("t1", 1, uint64(ms[0].Events-1)); err != nil {
		t.Fatalf("ViewAt(1, head): %v", err)
	}
	if _, err := r.ViewAt("t1", 1, 0); err != nil {
		t.Fatalf("ViewAt(1, 0): %v", err)
	}
}

// TestArchivedIndexHoldsNoNameUniverse: the in-memory sidecar index must not
// carry the ~24k-entry name list (both after archive() and after a restart
// reload), while a match rebuilt from disk still gets the exact names.
func TestArchivedIndexHoldsNoNameUniverse(t *testing.T) {
	takeMatchSlot(t)
	reg := testutil.CorpusRegistry(t)
	if reg.Len() == 0 {
		t.Fatal("precondition: corpus universe is empty")
	}
	dir := t.TempDir()
	opts := Options{
		Dir:          dir,
		LoadDeck:     nameLandLoader(t),
		NameUniverse: reg.AllCards(),
		Sleep:        func(time.Duration, <-chan struct{}) {},
	}
	r, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.AddTable(nameUniverseTable("t1")); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	r.Wait("t1")

	onDisk, err := readSidecar(dir, "t1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !onDisk.NameUniverse || len(onDisk.NameUniverseNames) == 0 {
		t.Fatalf("precondition: the on-disk sidecar records no name universe (mode %v, %d names)",
			onDisk.NameUniverse, len(onDisk.NameUniverseNames))
	}

	check := func(r *Registry, when string) {
		t.Helper()
		tb := r.tables["t1"]
		tb.mu.RLock()
		for _, sc := range tb.archived {
			if len(sc.NameUniverseNames) != 0 {
				tb.mu.RUnlock()
				t.Fatalf("%s: t.archived[%d] holds %d NameUniverseNames", when, sc.Match, len(sc.NameUniverseNames))
			}
		}
		tb.mu.RUnlock()
		_, m, err := r.lookup("t1", 1)
		if err != nil {
			t.Fatalf("%s: lookup: %v", when, err)
		}
		m.mu.RLock()
		names := m.e.G.NameUniverseNames
		m.mu.RUnlock()
		if !reflect.DeepEqual(names, onDisk.NameUniverseNames) {
			t.Fatalf("%s: rebuilt match has %d names, disk has %d", when, len(names), len(onDisk.NameUniverseNames))
		}
	}
	check(r, "after archive")
	r.Close()

	r2, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	check(r2, "after restart")
}

// TestMemoryModeHistoryIsBounded: without a Dir the engines are the only
// copy of a finished match, so they are kept for ViewAt — but only the last
// memoryHistoryLimit of them, each with its log trimmed to length.
func TestMemoryModeHistoryIsBounded(t *testing.T) {
	o := testOptions(t)
	var r *Registry
	ended := 0
	total := memoryHistoryLimit + 3
	o.Cooldown = retentionCooldown
	o.Sleep = func(d time.Duration, stop <-chan struct{}) {
		if d != retentionCooldown {
			return
		}
		ended++
		if ended == total {
			go r.Close()
			<-r.Done()
		}
	}
	var err error
	r, err = New(o)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(twoSeatPerpetual("t1")); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	r.Wait("t1")

	tb := r.tables["t1"]
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	if len(tb.history) != memoryHistoryLimit {
		t.Fatalf("memory-mode history holds %d matches, want %d", len(tb.history), memoryHistoryLimit)
	}
	last := tb.history[len(tb.history)-1].k
	for i, m := range tb.history {
		if want := last - len(tb.history) + 1 + i; m.k != want {
			t.Fatalf("history[%d] is match %d, want %d", i, m.k, want)
		}
		m.mu.RLock()
		if m.e == nil {
			t.Fatalf("memory-mode match %d lost its engine", m.k)
		}
		if l, c := len(m.e.L.Events), cap(m.e.L.Events); c != l {
			t.Fatalf("finished match %d keeps log capacity %d for %d events", m.k, c, l)
		}
		// Snapshots cloned from the live log shared its reserved backing
		// array; they must now share the trimmed one instead, or the
		// reserved array stays reachable through them.
		for j, sn := range m.snaps {
			if se := sn.e.L.Events; len(se) > 0 && &se[0] != &m.e.L.Events[0] {
				t.Fatalf("match %d snapshot %d still holds the untrimmed log", m.k, j)
			}
		}
		m.mu.RUnlock()
	}
	if _, err := r.ViewAt("t1", last, 0); err != nil {
		t.Fatalf("ViewAt on a retained memory-mode match: %v", err)
	}
}
