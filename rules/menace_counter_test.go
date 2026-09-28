package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// butchMenaceCounterEngine seats the REAL Butch DeLoria, Tunnel Snake (the
// deck carrier the brief names) on seat 0's battlefield with a Bear to
// receive its menace counter, and two Memnites on seat 1 to block with. Seat
// 0's pool is funded with {1}{B} for Butch's activated ability. It returns
// the engine, Butch's id and the Bear's id after the ability has resolved and
// the stack is empty.
func butchMenaceCounterEngine(t *testing.T) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	butch, ok := reg.Lookup("Butch DeLoria, Tunnel Snake")
	if !ok {
		t.Fatal("Butch DeLoria, Tunnel Snake missing from corpus")
	}
	if d := butch.Link(); len(d) != 0 {
		t.Fatalf("link Butch DeLoria: %v", d)
	}
	e := layerEngine(t)
	e.G.Active = 0
	e.G.Step = state.StepMain1
	e.G.Turn = 1

	bo := e.G.AddObject(butch, 0)
	bo.Zone = state.ZBattlefield
	bo.SummonSick = false
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), bo.ID))
	e.G.Clock++
	bo.Timestamp = e.G.Clock

	bear := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	// The two blockers are placed for seat 1, already past summoning sickness
	// (blocking never needs it, but a real board would be settled).
	onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")

	// Fund {1}{B}: one generic and one black.
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "B", Amount: 1})
	e.pending = nil
	e.Advance()

	d := e.Pending()
	ability := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == bo.ID {
			ability = o.Index
		}
	}
	if ability < 0 {
		t.Fatalf("Butch DeLoria menace-counter ability not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{ability}}); err != nil {
		t.Fatalf("activate Butch DeLoria: %v", err)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Butch target = %+v, want a creature target", d)
	}
	target := -1
	for _, o := range d.Options {
		if o.Obj == bear {
			target = o.Index
		}
	}
	if target < 0 {
		t.Fatalf("Butch did not offer the Bear: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{target}}); err != nil {
		t.Fatalf("target the Bear with Butch: %v", err)
	}
	passUntilStackEmpty(t, e, 40)
	return e, bo.ID, bear
}

// TestButchDeLoriaMenaceCounterGrantsMenaceEndToEnd drives the real corpus
// card's `DB$ PutCounter | CounterType$ Menace` leg and proves CR 702.109's
// menace is live from a MARKER COUNTER, not just from a printed `K:Menace`:
// the counter is on the creature and the engine reads the keyword, and the
// declare-blockers gate then rejects a single blocker for the countered
// creature while accepting two. Before the fix only the card's Animate leg
// did anything and a menace-countered creature could be blocked by one
// creature.
func TestButchDeLoriaMenaceCounterGrantsMenaceEndToEnd(t *testing.T) {
	e, _, bear := butchMenaceCounterEngine(t)

	if n := e.G.Obj(bear).Counter("Menace"); n != 1 {
		t.Fatalf("Bear has %d menace counters, want 1", n)
	}
	if !e.HasKeyword(bear, "Menace") {
		t.Fatal("a creature with a menace counter does not have menace")
	}

	// Attack with the countered Bear; seat 1 declares blockers. The engine's
	// real Menace gate (validateBlockers) must reject one blocker and admit
	// two -- the exact behaviour a single printed K:Menace attacker gets.
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{bear}})
	e.G.Step = state.StepDeclareBlockers
	e.askBlockers()

	d := e.Pending()
	if d == nil || d.Kind != decision.KBlockers || len(d.Options) != 2 {
		t.Fatalf("expected two block options, got %+v", d)
	}
	// A single blocker must be rejected.
	beforeIntents, beforeEvents := len(e.L.Intents), len(e.L.Events)
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err == nil {
		t.Fatal("a menace-countered attacker was blocked by exactly one creature")
	}
	if e.Pending() != d || len(e.L.Intents) != beforeIntents || len(e.L.Events) != beforeEvents {
		t.Fatal("rejected menace declaration consumed the decision or changed the log")
	}
	// Two blockers must be accepted.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index, d.Options[1].Index}}); err != nil {
		t.Fatalf("legal two-blocker declaration rejected: %v", err)
	}
}

// TestMenaceCounterKeywordClassifier pins cards.CounterKeyword's contract
// directly: the CR 122.1b keyword counters grant their keyword, the mapping is
// case-insensitive and canonicalising, parameterised forms keep their
// parameter, and a non-keyword marker counter grants nothing. This is the one
// classifier every counter-to-keyword read goes through, so its edges are the
// edges of the feature.
func TestMenaceCounterKeywordClassifier(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind string
		want string
		ok   bool
	}{
		{"Menace", "Menace", true},
		{"menace", "Menace", true},
		{"MENACE", "Menace", true},
		{"First Strike", "First Strike", true},
		{"first strike", "First Strike", true},
		{"Double Strike", "Double Strike", true},
		{"Deathtouch", "Deathtouch", true},
		{"Decayed", "Decayed", true},
		{"Exalted", "Exalted", true},
		{"Haste", "Haste", true},
		{"Hexproof", "Hexproof", true},
		{"Hexproof:Black", "Hexproof:Black", true},
		{"Indestructible", "Indestructible", true},
		{"Lifelink", "Lifelink", true},
		{"Reach", "Reach", true},
		{"Shadow", "Shadow", true},
		{"Trample", "Trample", true},
		{"Trample:2", "Trample:2", true},
		{"Vigilance", "Vigilance", true},
		{"Flying", "Flying", true},
		{"P1P1", "", false},
		{"M1M1", "", false},
		{"CHARGE", "", false},
		{"LOYALTY", "", false},
		{"", "", false},
	} {
		got, ok := cards.CounterKeyword(tc.kind)
		if ok != tc.ok || got != tc.want {
			t.Errorf("CounterKeyword(%q) = %q,%v want %q,%v", tc.kind, got, ok, tc.want, tc.ok)
		}
	}
}
