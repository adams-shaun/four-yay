package events

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
	"sync"
)

func TestEncodingIsDeterministicAndDiscriminating(t *testing.T) {
	e := Event{Seq: 3, Kind: Damage, Player: 1, Obj: 9, Amount: 3, Text: "bolt"}
	a := e.Append(nil)
	b := e.Append(nil)
	if string(a) != string(b) {
		t.Fatal("encoding is not deterministic")
	}
	for _, mut := range []Event{
		{Seq: 4, Kind: Damage, Player: 1, Obj: 9, Amount: 3, Text: "bolt"},
		{Seq: 3, Kind: LifeChange, Player: 1, Obj: 9, Amount: 3, Text: "bolt"},
		{Seq: 3, Kind: Damage, Player: 2, Obj: 9, Amount: 3, Text: "bolt"},
		{Seq: 3, Kind: Damage, Player: 1, Obj: 10, Amount: 3, Text: "bolt"},
		{Seq: 3, Kind: Damage, Player: 1, Obj: 9, Amount: 4, Text: "bolt"},
		{Seq: 3, Kind: Damage, Player: 1, Obj: 9, Amount: 3, Text: "shock"},
	} {
		if string(mut.Append(nil)) == string(a) {
			t.Fatalf("encoding collides for %+v", mut)
		}
	}
}

func TestEncodingCoversSliceFields(t *testing.T) {
	base := Event{Kind: DeclareAttackers, IDs: []state.ObjID{1, 2}}
	other := Event{Kind: DeclareAttackers, IDs: []state.ObjID{2, 1}}
	if string(base.Append(nil)) == string(other.Append(nil)) {
		t.Fatal("IDs order must affect the encoding")
	}
	p1 := Event{Kind: DeclareBlockers, Pairs: [][2]state.ObjID{{1, 2}}}
	p2 := Event{Kind: DeclareBlockers, Pairs: [][2]state.ObjID{{2, 1}}}
	if string(p1.Append(nil)) == string(p2.Append(nil)) {
		t.Fatal("Pairs order must affect the encoding")
	}
}

func TestLogAssignsSequenceAndChains(t *testing.T) {
	l := NewLog(42)
	for i := 0; i < 5; i++ {
		got := l.Append(Event{Kind: Draw, Player: state.PlayerID(i % 2)})
		if got.Seq != uint64(i) {
			t.Fatalf("event %d got seq %d", i, got.Seq)
		}
	}
	if len(l.Events) != 5 {
		t.Fatalf("events = %d", len(l.Events))
	}
	head := l.Head()
	if head == "" || head == l.HeadAt(4) {
		t.Fatal("Head must cover every event and differ from a prefix")
	}
	if l.HeadAt(5) != head {
		t.Fatal("HeadAt(len) must equal Head")
	}
	if l.HeadAt(0) == l.HeadAt(1) {
		t.Fatal("HeadAt must advance")
	}
}

func TestIdenticalEventStreamsChainIdentically(t *testing.T) {
	build := func() *Log {
		l := NewLog(7)
		l.Append(Event{Kind: Draw, Player: 0, Obj: 1, Secret: true})
		l.Append(Event{Kind: Damage, Player: 1, Amount: 3})
		l.Append(Event{Kind: StepChange, Step: state.StepMain1})
		return l
	}
	if build().Head() != build().Head() {
		t.Fatal("identical streams produced different chains")
	}
	other := build()
	other.Append(Event{Kind: Draw, Player: 0})
	if other.Head() == build().Head() {
		t.Fatal("appending an event did not change the chain")
	}
}

func TestNoHashSkipsChainButKeepsEvents(t *testing.T) {
	l := NewLog(1)
	l.NoHash = true
	l.Append(Event{Kind: Draw})
	if len(l.Events) != 1 {
		t.Fatal("NoHash dropped events")
	}
	if l.Head() != "" {
		t.Fatal("NoHash must report an empty head, not a stale one")
	}
}

// FIX 1: Test that IDs slices are copied and not aliased
func TestIDsSliceAliasing(t *testing.T) {
	l := NewLog(42)
	originalIDs := []state.ObjID{1, 2, 3}
	e := Event{Kind: DeclareAttackers, IDs: originalIDs}
	l.Append(e)
	head1 := l.Head()

	// Mutate the caller's slice
	originalIDs[0] = 99

	// Verify that the stored event wasn't affected
	head2 := l.HeadAt(len(l.Events))
	if head1 != head2 {
		t.Fatalf("IDs aliasing: Head() %s != HeadAt(1) %s after mutation", head1, head2)
	}
}

// FIX 1: Test that Pairs slices are copied and not aliased
func TestPairsSliceAliasing(t *testing.T) {
	l := NewLog(42)
	originalPairs := [][2]state.ObjID{{1, 2}, {3, 4}}
	e := Event{Kind: DeclareBlockers, Pairs: originalPairs}
	l.Append(e)
	head1 := l.Head()

	// Mutate the caller's slice
	originalPairs[0][0] = 99

	// Verify that the stored event wasn't affected
	head2 := l.HeadAt(len(l.Events))
	if head1 != head2 {
		t.Fatalf("Pairs aliasing: Head() %s != HeadAt(1) %s after mutation", head1, head2)
	}
}

// FIX 1: Test that nil slices remain nil after copy
func TestNilSlicePreservation(t *testing.T) {
	l := NewLog(42)
	e := Event{Kind: Draw, IDs: nil, Pairs: nil}
	l.Append(e)

	if l.Events[0].IDs != nil {
		t.Fatal("nil IDs slice not preserved")
	}
	if l.Events[0].Pairs != nil {
		t.Fatal("nil Pairs slice not preserved")
	}
}

// FIX 2: Test that NoHash immutability is enforced
func TestNoHashImmutability(t *testing.T) {
	// Setting NoHash before first append should work
	l := NewLog(42)
	l.NoHash = true
	l.Append(Event{Kind: Draw})
	// This should not panic

	// Toggling NoHash after first append should panic
	l2 := NewLog(42)
	l2.NoHash = false
	l2.Append(Event{Kind: Draw})
	l2.NoHash = true

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("toggling NoHash after first append did not panic")
		} else if r.(string) != "events: NoHash changed after the log was started" {
			t.Fatalf("unexpected panic message: %v", r)
		}
	}()
	l2.Append(Event{Kind: Damage})
}

// FIX 3: Test that seed affects the chain
func TestSeedSensitivity(t *testing.T) {
	build := func(seed uint64) *Log {
		l := NewLog(seed)
		l.Append(Event{Kind: Draw})
		l.Append(Event{Kind: Damage})
		return l
	}

	l1 := build(42)
	l2 := build(43)

	if l1.Head() == l2.Head() {
		t.Fatal("different seeds produced identical chain heads")
	}
}

// FIX 3: Test that same seed and events produce same chain
func TestSeedConsistency(t *testing.T) {
	build := func() *Log {
		l := NewLog(42)
		l.Append(Event{Kind: Draw})
		l.Append(Event{Kind: Damage})
		return l
	}

	l1 := build()
	l2 := build()

	if l1.Head() != l2.Head() {
		t.Fatal("identical logs with same seed produced different heads")
	}
}

// FIX 3: Test that Head() and HeadAt(len) agree with seeded chain
func TestHeadHeadAtAgreement(t *testing.T) {
	l := NewLog(42)
	l.Append(Event{Kind: Draw})
	l.Append(Event{Kind: Damage})
	l.Append(Event{Kind: Tap})

	head := l.Head()
	headAt := l.HeadAt(len(l.Events))

	if head != headAt {
		t.Fatalf("Head() %s != HeadAt(%d) %s", head, len(l.Events), headAt)
	}
}

func TestLogCloneIsIndependentAndKeepsTheChain(t *testing.T) {
	l := NewLog(9)
	l.Append(Event{Kind: GameStart, Amount: 2})
	l.Append(Event{Kind: Shuffle, Player: 1, IDs: []state.ObjID{3, 1, 2}, Secret: true})
	l.Intents = append(l.Intents, decision.Intent{Seq: 2, Player: 0, Choices: []int{1}})

	c := l.Clone()
	if c.Head() != l.Head() || c.HeadAt(2) != l.HeadAt(2) {
		t.Fatalf("clone head %s / %s, want %s / %s", c.Head(), c.HeadAt(2), l.Head(), l.HeadAt(2))
	}
	if c.Seed != l.Seed || len(c.Events) != 2 || len(c.Intents) != 1 {
		t.Fatalf("clone did not copy seed/events/intents: %+v", c)
	}

	// Appending to the original must not move the clone, and vice versa.
	l.Append(Event{Kind: TurnChange, Player: 0, Amount: 1})
	if len(c.Events) != 2 || c.Head() == l.Head() {
		t.Fatal("appending to the original moved the clone")
	}
	c.Append(Event{Kind: Priority, Player: 1})
	if len(l.Events) != 3 {
		t.Fatal("appending to the clone moved the original")
	}
	// The clone's chain continues correctly from the copied state. This must
	// be checked before the deliberate corruption below: HeadAt recomputes
	// from the live Events slice, so corrupting a past event's IDs would
	// desync it from the cached Head on ANY log, cloned or not -- that is
	// not a Clone bug, just why the check runs on uncorrupted data.
	if c.Head() != c.HeadAt(len(c.Events)) {
		t.Fatalf("clone chain desynced: Head %s, HeadAt %s", c.Head(), c.HeadAt(len(c.Events)))
	}

	// A stored Event or Intent is append-only, hash-chained history, so a
	// clone shares the backing arrays rather than deep-copying them -- an
	// in-place edit of a stored event's IDs/Pairs (or an intent's Choices)
	// is not a sanctioned operation on a clone, exactly as it is not on the
	// original: it would desync Head from HeadAt. The only mutation the
	// contract allows is append, and the divergence guarantees that come
	// with sharing are pinned by TestLogCloneAppendsDiverge below.
}

// TestLogCloneAppendsDiverge is the named regression test for Log.Clone
// sharing its backing arrays. Stored events/intents are append-only and
// hash-chained, so a clone may share them -- but only if the two logs can
// never write into each other's storage. The full-slice form
// c.Events = l.Events[:len:len] and c.Intents = l.Intents[:len:len] set
// cap == len, forcing the first append on either side to allocate a fresh
// array. Drop the cap (Mutation 1: share the spare capacity instead) and
// the interleaved appends below land on the other log's slots, silently
// corrupting its chain -- an assertion this test catches.
func TestLogCloneAppendsDiverge(t *testing.T) {
	mk := func(seed uint64) *Log {
		// Three appends so the backing array has been grown past its length
		// (len 3, cap 4) -- the spare capacity the safety relay depends on.
		l := NewLog(seed)
		l.Append(Event{Kind: GameStart, Player: 0, IDs: []state.ObjID{1, 2}})
		l.Append(Event{Kind: Draw, Player: 1})
		l.Append(Event{Kind: ManaAdd, Player: 0})
		return l
	}
	l := mk(7)
	c := l.Clone()

	// Append to the ORIGINAL first.
	l.Append(Event{Kind: TurnChange, Player: 0})
	l.Intents = append(l.Intents, decision.Intent{Seq: 2, Player: 0, Choices: []int{1}})
	if len(c.Events) != 3 || len(c.Intents) != 0 {
		t.Fatalf("append to the original leaked into the clone: %d events / %d intents",
			len(c.Events), len(c.Intents))
	}

	// Then append to the CLONE. Under sharing this is where corruption
	// would land: unless cap was truncated to len, the clone's append writes
	// into the same backing array at the slot the original just used and
	// clobbers its TurnChange (the original grew into the spare capacity the
	// clone still holds).
	c.Append(Event{Kind: Priority, Player: 1})
	c.Intents = append(c.Intents, decision.Intent{Seq: 3, Player: 1, Choices: []int{0}})

	// The original's appended event must survive, and its chain must still
	// describe its own events -- a clobber desyncs Head from HeadAt.
	if len(l.Events) != 4 || l.Events[3].Kind != TurnChange {
		t.Fatalf("clone append clobbered the original's event: %d events, [3]=%s",
			len(l.Events), l.Events[3].Kind)
	}
	if len(l.Intents) != 1 || l.Intents[0].Seq != 2 {
		t.Fatalf("clone intent append clobbered the original's intent")
	}
	if l.HeadAt(len(l.Events)) != l.Head() {
		t.Fatalf("original chain desynced: HeadAt=%s Head=%s", l.HeadAt(len(l.Events)), l.Head())
	}
	if c.HeadAt(len(c.Events)) != c.Head() {
		t.Fatalf("clone chain desynced: HeadAt=%s Head=%s", c.HeadAt(len(c.Events)), c.Head())
	}
	// And the two logs really have diverged: independent chains now.
	if l.Head() == c.Head() {
		t.Fatal("logs did not diverge after independent appends")
	}
}

// TestGrowEventsReturnsExactlyTheNamedLength pins the invariant events.Append
// relies on with Task c3's custom growth (loggerAppend -> growEvents): for
// every append along a log's growth, Append writes the new event at index
// len-1, which is only safe if growEvents returns a slice whose length is
// exactly need. A regression that forgets the s[:need] slice on the no-realloc
// path (the growth helper is called on every append, in-place or not) leaves
// the length stuck at the pre-append value, so the new event overwrites
// element len-1 — silently dropping the previous tail and truncating the log.
// The surrounding Log.Append tests already catch that corrupts the chain, but
// this one names the exact growth-place contract: appending back-to-back must
// keep every prior event at its Seq and land each new one at its own Seq.
func TestGrowEventsReturnsExactlyTheNamedLength(t *testing.T) {
	l := NewLog(5)
	const n = 513 // > 16 and > 512 so it crosses several growth steps
	for i := 0; i < n; i++ {
		kind := Draw
		if i%2 == 0 {
			kind = LifeChange
		}
		l.Append(Event{Kind: kind, Player: state.PlayerID(i % 2), Amount: int32(i)})
		if got := len(l.Events); got != i+1 {
			t.Fatalf("after append %d, len = %d, want %d", i, got, i+1)
		}
		if l.Events[i].Seq != uint64(i) {
			t.Fatalf("event %d has Seq %d, want %d", i, l.Events[i].Seq, i)
		}
		if l.Events[i].Amount != int32(i) {
			t.Fatalf("event %d lost its Amount: got %d, want %d", i, l.Events[i].Amount, i)
		}
	}
	// Length must keep growing monotonically to the full stream; a helper
	// that never extends the no-realloc slice would stop at 1.
	if len(l.Events) != n {
		t.Fatalf("final len = %d, want %d", len(l.Events), n)
	}
	// The whole stream still chains as one log: HeadAt(n) equals Head.
	if l.HeadAt(n) != l.Head() {
		t.Fatalf("chain desynced: HeadAt=%s Head=%s", l.HeadAt(n), l.Head())
	}
}

// A clone and its parent are appended to from different goroutines: host
// snapshots an engine per turn (host/snapshot.go) and ViewAt replays into a
// clone while the match goroutine keeps appending to the live log. Append
// folds each event into the chain through a reused digest, so a clone that
// shared its parent's digest would be two goroutines writing one hash.Hash.
// Reset makes that safe when the two interleave sequentially, which is why
// this has to run under -race to mean anything: it pins the ownership, not
// the arithmetic. Clone giving each log its own digest is the enforcement
// point (events/log.go), and removing it fails this test under -race.
func TestCloneAndParentAppendConcurrentlyWithoutSharingTheDigest(t *testing.T) {
	parent := NewLog(7)
	for i := 0; i < 8; i++ {
		parent.Append(Event{Kind: Note, Text: "seed"})
	}
	clone := parent.Clone()

	var wg sync.WaitGroup
	for _, l := range []*Log{parent, clone} {
		wg.Add(1)
		go func(l *Log) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				l.Append(Event{Kind: Note, Text: "x"})
			}
		}(l)
	}
	wg.Wait()

	// Both logs must still be internally consistent: Head is the running
	// chain, HeadAt recomputes it from scratch, and they can only agree if
	// neither goroutine wrote through the other's digest.
	for name, l := range map[string]*Log{"parent": parent, "clone": clone} {
		if got, want := l.Head(), l.HeadAt(len(l.Events)); got != want {
			t.Errorf("%s: Head() = %s but HeadAt(%d) = %s — the chain was corrupted by a shared digest",
				name, got, len(l.Events), want)
		}
	}
}

// TestLogReservePreallocatesTheBacking is the naming test for the Reserve size
// hint: after a Reserve the backing array has at least the requested capacity,
// and appending up to it never touches growEvents (the backing header is not
// replaced, so the chain still holds and cap stays put). Reserve is a pure
// capacity hint and must not disturb length, contents or the chain.
func TestLogReservePreallocatesTheBacking(t *testing.T) {
	l := NewLog(11)
	l.Reserve(4096)
	if cap(l.Events) < 4096 {
		t.Fatalf("after Reserve(4096) cap = %d, want >= 4096", cap(l.Events))
	}
	first := cap(l.Events)
	for i := 0; i < 2000; i++ { // far under the reserve; must never reallocate
		l.Append(Event{Kind: Draw, Player: state.PlayerID(i % 2), Amount: int32(i)})
		if cap(l.Events) != first {
			t.Fatalf("append %d under the reserve reallocated: cap %d -> %d", i, first, cap(l.Events))
		}
		if l.Events[i].Seq != uint64(i) {
			t.Fatalf("event %d lost its Seq: %d", i, l.Events[i].Seq)
		}
	}
	if l.HeadAt(len(l.Events)) != l.Head() {
		t.Fatalf("chain desynced: HeadAt=%s Head=%s", l.HeadAt(len(l.Events)), l.Head())
	}
}

// TestLogReservePreservesExistingEventsAndChain pins that Reserve is legal to
// call after the log has already appended (host calls it right after
// rules.New has written genesis): it reallocates the backing array once and
// copies the existing events over, so length, order and the running chain are
// all untouched.
func TestLogReservePreservesExistingEventsAndChain(t *testing.T) {
	l := NewLog(12)
	l.Append(Event{Kind: GameStart, Amount: 2})
	l.Append(Event{Kind: Shuffle, Player: 1, IDs: []state.ObjID{3, 1, 2}, Secret: true})
	l.Append(Event{Kind: Draw, Player: 0, Obj: 9})
	beforeHead := l.Head()
	beforeLen := len(l.Events)

	l.Reserve(4096)
	if len(l.Events) != beforeLen {
		t.Fatalf("Reserve changed len: %d -> %d", beforeLen, len(l.Events))
	}
	if cap(l.Events) < 4096 {
		t.Fatalf("Reserve left cap %d, want >= 4096", cap(l.Events))
	}
	// Contents survived the copy: the fold over the retained events is the
	// same, so the running chain head is unchanged.
	if l.HeadAt(beforeLen) != l.Head() {
		t.Fatalf("Reserve desynced the chain: HeadAt=%s Head=%s", l.HeadAt(beforeLen), l.Head())
	}
	if l.Head() != beforeHead || l.HeadAt(beforeLen) != beforeHead {
		t.Fatal("Reserve corrupted existing events: head moved")
	}
	// The log keeps working past the reserve.
	l.Append(Event{Kind: Damage, Player: 1, Amount: 3})
	if len(l.Events) != beforeLen+1 {
		t.Fatalf("append after Reserve: len = %d, want %d", len(l.Events), beforeLen+1)
	}
	if l.HeadAt(len(l.Events)) != l.Head() {
		t.Fatalf("chain desynced after Reserve+append: HeadAt=%s Head=%s", l.HeadAt(len(l.Events)), l.Head())
	}
}

// TestLogReserveDoesNotLeakSpareCapacityToClone is the named invariant test
// for Reserve's only correctness footgun. Reserve grows the PARENT's backing
// array to hold the whole (expected) log up front, leaving lots of spare
// capacity beyond len. If a Clone inherited that spare capacity — i.e. if
// Clone shared c.Events = l.Events[:len] instead of truncating to
// [len:len:len] — then the first append on either side would land in the
// shared reserved region and clobber the other log's slot, silently
// corrupting its chain. Clone's full-slice truncation is what guarantees a
// clone always starts at cap == len regardless of how much the parent
// reserved. Drop the cap (Mutation: share the reserve) and the interleaved
// appends below write into the other log's storage, which the chain
// assertions catch.
func TestLogReserveDoesNotLeakSpareCapacityToClone(t *testing.T) {
	// Reserve BEFORE any appends, so the parent's backing has a huge empty
	// reserve the clone would (under the bug) be handed.
	l := NewLog(13)
	l.Reserve(4096)
	l.Append(Event{Kind: GameStart, Player: 0, IDs: []state.ObjID{1, 2}})
	l.Append(Event{Kind: Draw, Player: 1})
	l.Append(Event{Kind: ManaAdd, Player: 0})

	c := l.Clone()
	if cap(c.Events) != len(c.Events) {
		t.Fatalf("clone should never inherit reserved spare capacity: len=%d cap=%d",
			len(c.Events), cap(c.Events))
	}

	// Append to the ORIGINAL first.
	l.Append(Event{Kind: TurnChange, Player: 0})
	l.Intents = append(l.Intents, decision.Intent{Seq: 2, Player: 0, Choices: []int{1}})
	if len(c.Events) != 3 || len(c.Intents) != 0 {
		t.Fatalf("append to the original leaked into the clone: %d events / %d intents",
			len(c.Events), len(c.Intents))
	}

	// Then append to the CLONE. Unless cap was truncated to len, the clone's
	// append writes into the parent's reserved region and clobbers the
	// TurnChange the original just wrote there.
	c.Append(Event{Kind: Priority, Player: 1})
	c.Intents = append(c.Intents, decision.Intent{Seq: 3, Player: 1, Choices: []int{0}})

	if len(l.Events) != 4 || l.Events[3].Kind != TurnChange {
		t.Fatalf("clone append clobbered the original's event: %d events, [3]=%s",
			len(l.Events), l.Events[3].Kind)
	}
	if len(l.Intents) != 1 || l.Intents[0].Seq != 2 {
		t.Fatalf("clone intent append clobbered the original's intent")
	}
	if l.HeadAt(len(l.Events)) != l.Head() {
		t.Fatalf("original chain desynced: HeadAt=%s Head=%s", l.HeadAt(len(l.Events)), l.Head())
	}
	if c.HeadAt(len(c.Events)) != c.Head() {
		t.Fatalf("clone chain desynced: HeadAt=%s Head=%s", c.HeadAt(len(c.Events)), c.Head())
	}
	if l.Head() == c.Head() {
		t.Fatal("logs did not diverge after independent appends into separate arrays")
	}
}

// TestLogReserveCloneConcurrentAppendsAreIndependent is the concurrent
// ownership test for a reserved parent. It pins, under -race, that a clone of
// a reserved log (host: the live match log, reserved at match start) owns its
// own backing array even while the parent and the clone are appended to from
// different goroutines. A clone that shared the parent's reserved spare
// capacity would be two goroutines writing one backing array, which -race
// detects; this is the condition the brief's "if the change is about reuse
// or sharing, write the concurrent test" asks for.
func TestLogReserveCloneConcurrentAppendsAreIndependent(t *testing.T) {
	parent := NewLog(7)
	parent.Reserve(4096) // the reserved backing a buggy clone would share
	for i := 0; i < 8; i++ {
		parent.Append(Event{Kind: Note, Text: "seed"})
	}
	clone := parent.Clone()
	if cap(clone.Events) != len(clone.Events) {
		t.Fatal("clone inherited reserved spare capacity; concurrent appends would race it")
	}

	var wg sync.WaitGroup
	for _, l := range []*Log{parent, clone} {
		wg.Add(1)
		go func(l *Log) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				l.Append(Event{Kind: Note, Text: "x"})
			}
		}(l)
	}
	wg.Wait()

	for name, l := range map[string]*Log{"parent": parent, "clone": clone} {
		if got, want := l.Head(), l.HeadAt(len(l.Events)); got != want {
			t.Errorf("%s: Head() = %s but HeadAt(%d) = %s — the chain was corrupted by a shared reserved array",
				name, got, len(l.Events), want)
		}
	}
}

// TestGrowEventsTaperKeepsTheLengthAndChain pins the post-growTaperAt half of
// the growth policy: past growTaperAt the growth factor drops to 1.25x (the
// clone-heavy region of a real log), and that switch must not disturb the
// growth contract that every other grow test relies on -- growEvents returns s
// with len exactly need, each new event lands at its own Seq, and the whole
// stream still folds into one chain. A helper that, say, returned [len-1]
// capacity growth on the taper path would drop the tail exactly as
// TestGrowEventsReturnsExactlyTheNamedLength guards for the doubling path.
func TestGrowEventsTaperKeepsTheLengthAndChain(t *testing.T) {
	l := NewLog(5)
	const n = 6000 // well past growTaperAt (4096): exercises the 1.25x branch
	for i := 0; i < n; i++ {
		kind := Draw
		if i%2 == 0 {
			kind = LifeChange
		}
		l.Append(Event{Kind: kind, Player: state.PlayerID(i % 2), Amount: int32(i)})
		if len(l.Events) != i+1 {
			t.Fatalf("after append %d, len = %d, want %d", i, len(l.Events), i+1)
		}
		if l.Events[i].Seq != uint64(i) {
			t.Fatalf("event %d has Seq %d, want %d", i, l.Events[i].Seq, i)
		}
	}
	if l.HeadAt(n) != l.Head() {
		t.Fatalf("chain desynced after taper growth: HeadAt=%s Head=%s", l.HeadAt(n), l.Head())
	}
}

// TestDeferredFoldMatchesEagerChain pins the deferred chain fold (unhashed):
// a head read at any point -- before or after a Clone that carries an
// unfolded tail, interleaved with appends on both logs -- equals the
// from-scratch HeadAt, i.e. the chain an eager per-Append fold produces.
func TestDeferredFoldMatchesEagerChain(t *testing.T) {
	l := NewLog(11)
	for i := 0; i < 5; i++ {
		l.Append(Event{Kind: Note, Amount: int32(i), IDs: []state.ObjID{state.ObjID(i)}})
	}
	if l.unhashed != 5 {
		t.Fatalf("unhashed = %d, want 5 (fold deferred)", l.unhashed)
	}
	c := l.Clone() // carries the five unfolded events
	l.Append(Event{Kind: Tap, Obj: 3})
	c.Append(Event{Kind: Untap, Obj: 4})
	if got, want := c.Head(), c.HeadAt(len(c.Events)); got != want {
		t.Fatalf("clone head %s, HeadAt %s", got, want)
	}
	if got, want := l.Head(), l.HeadAt(len(l.Events)); got != want {
		t.Fatalf("parent head %s, HeadAt %s", got, want)
	}
	h := l.Head()
	if l.Head() != h || l.unhashed != 0 {
		t.Fatal("a second Head read moved the chain")
	}
	l.Append(Event{Kind: Draw, Player: 1})
	if got, want := l.Head(), l.HeadAt(len(l.Events)); got != want {
		t.Fatalf("head after fold+append %s, HeadAt %s", got, want)
	}
}

// BenchmarkLogAppend prices one Append on the search path: no hash, and no
// allocation once the backing array has room (the IDs/Pairs copies of a
// payload-free event are nil).
func BenchmarkLogAppend(b *testing.B) {
	l := NewLog(1)
	ev := Event{Kind: Priority, Player: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if len(l.Events) == cap(l.Events) {
			l.Events, l.unhashed = l.Events[:0], 0
		}
		l.Append(ev)
	}
}
