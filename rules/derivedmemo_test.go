package rules

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestDerivedMemoScopedToOneWalk pins derivedmemo.go's invalidation contract:
// inside one scope a repeated Derived is served from the memo with owned
// slices (a Derived of another object does not rewrite them); a NEW scope
// reuses an entry only while active() has not been rebuilt, so an
// event-backed change (and not a layer-inert Priority marker) is seen by the
// next walk.
func TestDerivedMemoScopedToOneWalk(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	bear := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nK:Trample\nOracle:x\n")
	angel := onBoard(t, e, 0, "Name:Angel\nManaCost:3 WW\nTypes:Creature Angel\nPT:4/4\nK:Flying\nOracle:x\n")

	e.beginDerivedMemo()
	a := e.Derived(bear)
	if a.Power != 2 || !slices.Equal(a.Keywords, []string{"Trample"}) {
		t.Fatalf("first derive = %+v", a)
	}
	// Another object's derive rewrites the shared scratch; the memoized
	// bear slices must be owned and therefore untouched.
	if kw := e.Derived(angel).Keywords; !slices.Equal(kw, []string{"Flying"}) {
		t.Fatalf("angel keywords = %v", kw)
	}
	if !slices.Equal(a.Keywords, []string{"Trample"}) || !slices.Equal(a.Types, []string{"Creature", "Bear"}) {
		t.Fatalf("memoized bear slices rewritten by another derive: %+v", a)
	}
	b := e.Derived(bear)
	if &b.Keywords[0] != &a.Keywords[0] {
		t.Fatalf("second derive in one walk was not served from the memo")
	}
	e.endDerivedMemo()

	// An event-backed change rebuilds active(), so the next walk recomputes.
	e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "P1P1", Amount: 1})
	e.beginDerivedMemo()
	if p := e.Derived(bear).Power; p != 3 {
		t.Fatalf("new walk served the previous walk's entry: power %d, want 3", p)
	}
	e.endDerivedMemo()
	m := e.derivedMemo.at(bear)
	if m == nil {
		t.Fatal("no memo entry for the derived bear")
	}
	seq := m.seq
	if seq == 0 || seq != e.activeBuildSeq {
		t.Fatalf("entry not eligible for cross-walk reuse: seq %d, active %d", seq, e.activeBuildSeq)
	}

	// A layer-inert Priority marker keeps active(), so the next walk reuses
	// the entry, re-stamped into its own scope.
	e.emit(events.Event{Kind: events.Priority, Player: 0})
	e.beginDerivedMemo()
	if p := e.Derived(bear).Power; p != 3 {
		t.Fatalf("reused entry power %d, want 3", p)
	}
	if m = e.derivedMemo.at(bear); m.gen != e.derivedMemoGen || m.seq != seq || e.activeBuildSeq != seq {
		t.Fatalf("inert event did not keep the entry: gen %d/%d seq %d/%d active %d", m.gen, e.derivedMemoGen, m.seq, seq, e.activeBuildSeq)
	}
	e.endDerivedMemo()
}

// TestDerivedMemoCrossWalkVerifyCatchesDirectWrite documents the cross-walk
// reuse's blind spot -- a direct e.G write with no event between two walks.
// Game mutation goes through events.Apply; the engine's own no-event runtime
// inputs (offerAsFace's face flip, the cost-composition exclusion) call
// retireCrossWalkMemo. Tests may still write directly, and verify mode flags it.
func TestDerivedMemoCrossWalkVerifyCatchesDirectWrite(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	bear := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.beginDerivedMemo()
	_ = e.Derived(bear)
	e.endDerivedMemo()
	e.G.Obj(bear).AddCounter("P1P1", 1)
	e.beginDerivedMemo()
	defer e.endDerivedMemo()
	defer func() {
		r := recover()
		if s, ok := r.(string); !ok || !strings.Contains(s, "derived memo stale") {
			t.Fatalf("verify did not flag the stale cross-walk hit: %v", r)
		}
	}()
	_ = e.Derived(bear)
	t.Fatal("stale cross-walk hit served without a verify panic")
}

// TestDerivedMemoVerifyCatchesStaleness proves verify mode is live in the
// rules test binary (derivedmemo_verify_test.go), so the whole suite really
// does cross-check every memo hit: a no-event write inside one walk -- the
// thing the walk contract forbids -- must trip it.
func TestDerivedMemoVerifyCatchesStaleness(t *testing.T) {
	t.Parallel()
	if !derivedMemoVerify {
		t.Fatal("verify mode is off in the rules test binary")
	}
	e := layerEngine(t)
	bear := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.beginDerivedMemo()
	defer e.endDerivedMemo()
	_ = e.Derived(bear)
	e.G.Obj(bear).AddCounter("P1P1", 1)
	defer func() {
		r := recover()
		if s, ok := r.(string); !ok || !strings.Contains(s, "derived memo stale") {
			t.Fatalf("verify did not flag the stale hit: %v", r)
		}
	}()
	_ = e.Derived(bear)
}

// TestDerivedMemoBypassesZoneOverride pins that the convoke zone-override
// read (atStack != 0) never touches the memo.
func TestDerivedMemoBypassesZoneOverride(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	bear := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.beginDerivedMemo()
	defer e.endDerivedMemo()
	_ = e.derivedWith(bear, state.ZStack)
	if m := e.derivedMemo.at(bear); m != nil && m.gen != 0 {
		t.Fatal("an atStack derive was memoized")
	}
}

// TestBeginDerivedReadsResumesThePriorityWalk plays a real game with the
// test bot, whose board build (botpolicy.BoardFromGameInto) opens a
// BeginDerivedReads scope. At a priority decision the scope must resume the
// offer walk's generation (and, in this verify-mode binary, every hit it
// serves is recomputed and compared); at any other decision it must open a
// fresh one.
func TestBeginDerivedReadsResumesThePriorityWalk(t *testing.T) {
	t.Parallel()
	names, decks := testutil.SampleDecks(t, 2)
	e := New(Config{Seed: 7, Names: names, Decks: decks})
	e.Advance()
	bot := newTestBot(7)
	resumed, fresh, hitsServed := 0, 0, 0
	for n := 0; n < 400 && !e.G.Over; n++ {
		d := e.Pending()
		if d == nil {
			break
		}
		gen := e.derivedMemoGen
		e.BeginDerivedReads()
		if e.derivedMemoAliasFrom != e.derivedMemoAliasTo {
			if d.Kind != decision.KPriority {
				t.Fatalf("intent %d: a %s decision resumed the walk memo", n, d.Kind)
			}
			if e.derivedMemoGen != gen {
				t.Fatalf("intent %d: resumed scope moved the generation", n)
			}
			resumed++
			for _, id := range e.G.Zone(state.ZBattlefield, d.Player) {
				if m := e.derivedMemo.at(id); m != nil && m.gen == gen && m.ep == e.derivedMemoAliasFrom {
					_ = e.Derived(id) // served via the alias; verify mode recomputes it
					hitsServed++
				}
			}
		} else {
			if e.derivedMemoGen == gen {
				t.Fatalf("intent %d: a non-resumed scope kept the previous generation", n)
			}
			fresh++
		}
		e.EndDerivedReads()
		if err := e.Submit(bot.answer(e, d)); err != nil {
			t.Fatalf("intent %d: %v", n, err)
		}
		e.Advance()
	}
	if resumed == 0 || fresh == 0 || hitsServed == 0 {
		t.Fatalf("resumed %d, fresh %d, alias hits %d: want all three exercised", resumed, fresh, hitsServed)
	}
}

// TestBeginDerivedReadsDoesNotSurviveSubmit: once the priority decision is
// answered the tail is dead, and a Submit inside an open scope panics.
func TestBeginDerivedReadsDoesNotSurviveSubmit(t *testing.T) {
	t.Parallel()
	names, decks := testutil.SampleDecks(t, 2)
	e := New(Config{Seed: 7, Names: names, Decks: decks})
	e.Advance()
	for e.Pending() != nil && e.Pending().Kind != decision.KPriority {
		if err := e.Submit(newTestBot(1).answer(e, e.Pending())); err != nil {
			t.Fatal(err)
		}
		e.Advance()
	}
	d := e.Pending()
	if d == nil || !e.derivedMemoTailLive() {
		t.Fatalf("no live tail at the first priority decision (%v)", d)
	}
	func() {
		e.BeginDerivedReads()
		defer e.EndDerivedReads()
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("Submit inside a BeginDerivedReads scope did not panic")
			}
		}()
		_ = e.Submit(decision.Intent{})
	}()
	if err := e.Submit(newTestBot(1).answer(e, d)); err != nil {
		t.Fatal(err)
	}
	if e.derivedMemoTail.pending == d {
		t.Fatal("tail still names the answered decision")
	}
	if e.derivedMemoTailLive() && e.Pending().Kind != decision.KPriority {
		t.Fatalf("tail live at a %s decision", e.Pending().Kind)
	}
}

// TestBeginDerivedReadsVerifyCatchesDirectWrite documents the resumed
// scope's one blind spot -- a direct e.G write with no event between the ask
// and the board build -- and proves verify mode flags it.
func TestBeginDerivedReadsVerifyCatchesDirectWrite(t *testing.T) {
	t.Parallel()
	names, decks := testutil.SampleDecks(t, 2)
	e := New(Config{Seed: 7, Names: names, Decks: decks})
	e.Advance()
	for n := 0; n < 400 && !e.G.Over; n++ {
		d := e.Pending()
		if d.Kind == decision.KPriority && e.derivedMemoTailLive() {
			for _, id := range e.G.Zone(state.ZBattlefield, d.Player) {
				if m := e.derivedMemo.at(id); m == nil || m.gen != e.derivedMemoGen || e.Derived(id).Types == nil {
					continue
				}
				if !slices.Contains(e.Derived(id).Types, "Creature") {
					continue
				}
				e.G.Obj(id).AddCounter("P1P1", 1)
				e.BeginDerivedReads()
				defer e.EndDerivedReads()
				defer func() {
					r := recover()
					if s, ok := r.(string); !ok || !strings.Contains(s, "derived memo stale") {
						t.Fatalf("verify did not flag the stale resumed hit: %v", r)
					}
				}()
				_ = e.Derived(id)
				t.Fatal("stale resumed hit served without a verify panic")
			}
		}
		if err := e.Submit(newTestBot(3).answer(e, d)); err != nil {
			t.Fatal(err)
		}
		e.Advance()
	}
	t.Skip("no priority decision with a walk-derived creature reached")
}

// TestDerivedMemoFaceProbeDoesNotLeak pins offerAsFace's isolation under
// cross-walk reuse: an entry a nested scope builds under the probed face is
// never served after the probe, and a live-face entry from before the probe
// is never served inside it.
func TestDerivedMemoFaceProbeDoesNotLeak(t *testing.T) {
	t.Parallel()
	e, _, id := newFixtureDeck(t, 7413, taxedAdventureSrc, taxWardenSrc)
	o := e.G.Obj(id)
	if o == nil || len(o.Card.Faces) < 2 {
		t.Fatalf("fixture card has no adventure face")
	}
	adv := o.Card.Faces[1]
	live := func() bool { return slices.Contains(e.Derived(id).Types, "Creature") }

	// Probed face must not leak out.
	e.offerAsFace(id, adv, func() bool {
		e.beginDerivedMemo()
		defer e.endDerivedMemo()
		if live() {
			t.Fatalf("probe derived the live face")
		}
		return true
	})
	e.beginDerivedMemo()
	if !live() {
		t.Fatalf("after the probe Derived served the probed face")
	}
	e.endDerivedMemo()

	// Live face must not leak in.
	e.offerAsFace(id, adv, func() bool {
		e.beginDerivedMemo()
		defer e.endDerivedMemo()
		if live() {
			t.Fatalf("probe served the pre-probe live-face entry")
		}
		return true
	})
}
