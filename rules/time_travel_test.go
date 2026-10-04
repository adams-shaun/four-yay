package rules

// Real-corpus end-to-end pins for Doctor Who's Time Travel action
// (AB$/SP$/DB$ TimeTravel). The effects-package halves live in
// effects/time_travel_test.go; these drive the real corpus carriers through
// the engine's own decision loop (Engine.Submit / resumeResolution) so the
// per-object cursor is exercised on the path a seat actually uses, not on a
// hand-injected Ctx.
//
// The two carriers are Rotating Fireplace ({4}, {T}: time travel) and The
// Tenth Doctor ({7}: time travel three times, Amount$ 3, "Activate only as a
// sorcery"), the two shapes the brief names.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// timeTravelBoard builds a two-seat game whose every seat holds the real
// corpus carrier named `carrier` plus the authored `extras`, pins seat 0 as
// the turn-1 active seat, drives to Main 1, and returns the engine, its
// Config, the carrier's object id (bridged into the active seat's hand), and
// the active seat. It mirrors corpusCardConfig with room for the extras a
// board setup needs (a suspended card, extra TIME-counter permanents).
func timeTravelBoard(t *testing.T, seed uint64, carrier string, extras ...string) (*Engine, Config, state.ObjID, state.PlayerID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c := mustCorpusCard(t, reg, carrier)
	seatDeck := func() []*cards.Card {
		deck := []*cards.Card{c}
		for _, e := range extras {
			deck = append(deck, card(t, e))
		}
		return append(deck, mountainDeck(t, 40-len(deck))...)
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{seatDeck(), seatDeck()}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	caster := e.G.Active
	id := findByName(e, carrier, caster)
	if id == 0 {
		t.Fatalf("corpus %q not found for seat %d -- corpus missing?", carrier, caster)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: state.ZHand})
	e.pending = nil
	e.priorityRound()
	return e, cfg, id, caster
}

// timeTravelClockSrc is an authored permanent that carries TIME counters and
// would otherwise do nothing -- the extra affected object a Time Travel walk
// must visit.
const timeTravelClockSrc = "Name:Time Clock\nManaCost:1\nTypes:Artifact\nOracle:x\n"

// timeTravelInstantSrc is an authored instant the Tenth Doctor test puts on
// the stack so the sorcery-speed window is closed while it sits there.
const timeTravelInstantSrc = "Name:Poke\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"

// suspendedSorcerySrc is an authored sorcery seeded only so it can be moved
// to exile and marked suspend-provenanced. It has no cast relevance; it is
// the CR 702.62 suspended card the Time Travel action must offer.
const suspendedSorcerySrc = "Name:Warped Spell\nManaCost:U\nTypes:Sorcery\nOracle:x\n"

// suspendCard moves a seat's card named `name` to exile and marks it
// suspended with n TIME counters, through the event path the Suspend
// mechanic itself uses (a CastInfo carrying the FlagSuspend flag), so the
// eligibility filter sees the same provenance a real suspend creates.
func suspendCard(t *testing.T, e *Engine, p state.PlayerID, name string, n int) state.ObjID {
	t.Helper()
	id := findByName(e, name, p)
	if id == 0 {
		t.Fatalf("suspend source %q not found for seat %d", name, p)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: state.ZExile})
	e.emit(events.Event{Kind: events.CastInfo, Obj: id, Counter: events.FlagsString(state.FlagSuspend)})
	if n > 0 {
		e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "TIME", Amount: int32(n)})
	}
	return id
}

// putTimePermanent places a DECK-SEEDED authored permanent on the battlefield
// through a logged MoveZone (so a log-only replay reconstructs it -- an
// eventless onBoard placement would not) and gives it n TIME counters.
func putTimePermanent(t *testing.T, e *Engine, p state.PlayerID, src string, n int) state.ObjID {
	t.Helper()
	name := card(t, src).Faces[0].Name
	id := moveByName(t, e, p, name, state.ZBattlefield)
	if n > 0 {
		e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "TIME", Amount: int32(n)})
	}
	return id
}

// timeTravelOption locates the Time Travel activated-ability option on obj.
func timeTravelOption(t *testing.T, e *Engine, obj state.ObjID) decision.Option {
	t.Helper()
	o, ok := findAbilityOptionByLabel(e, obj, "Time travel")
	if !ok {
		t.Fatalf("no Time Travel ability option on %d: %+v", obj, e.Pending().Options)
	}
	return o
}

// answerTimeTravel answers the pending Time Travel election with the option
// of kind `kind` (time_travel_skip/add/remove) and returns the object it
// named. A fatal if the pending decision is not a Time Travel election.
func answerTimeTravel(t *testing.T, e *Engine, kind string) state.ObjID {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "time_travel" {
		t.Fatalf("no Time Travel election pending: %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == kind {
			obj := o.Obj
			submitChoices(t, e, o.Index)
			return obj
		}
	}
	t.Fatalf("Time Travel election has no %q option: %+v", kind, d.Options)
	return 0
}

// drainTimeTravel drives a resolved Time Travel activation to completion,
// answering every election with `answers[i]` and passing priority when it is
// asked. It asserts the election stream is exactly wantOrder (same objects,
// same length) -- the "each object is asked exactly once per repetition"
// contract that option-derived snapshots broke. It returns the objects asked
// in order.
func drainTimeTravel(t *testing.T, e *Engine, answers []string, wantOrder []state.ObjID) []state.ObjID {
	t.Helper()
	var seen []state.ObjID
loop:
	for i := 0; i < 200; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		switch {
		case d.Kind == decision.KChoose && d.ResumeKind == "time_travel":
			if len(seen) >= len(answers) {
				t.Fatalf("more Time Travel elections (%d so far) than answers (%d): %+v", len(seen), len(answers), d)
			}
			seen = append(seen, answerTimeTravel(t, e, answers[len(seen)]))
		case d.Kind == decision.KPriority:
			// The action is complete once the stack has emptied and the
			// engine is back to an ordinary priority ask.
			if len(e.G.Stack) == 0 {
				break loop
			}
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			submitChoices(t, e, idx)
		default:
			t.Fatalf("unexpected decision while draining Time Travel: %+v", d)
		}
	}
	if len(seen) != len(wantOrder) {
		t.Fatalf("Time Travel asked %d objects, want %d: got %v want %v", len(seen), len(wantOrder), seen, wantOrder)
	}
	for i, id := range wantOrder {
		if seen[i] != id {
			t.Fatalf("Time Travel election %d asked object %d, want %d (order %v)", i, seen[i], id, seen)
		}
	}
	return seen
}

// TestRotatingFireplaceTimeTravelAddsAndRemoves drives the real corpus
// Rotating Fireplace through its {4}, {T} Time Travel ability: a suspended
// card at ZERO TIME counters (it must still be offered, and adding gives it
// its first), Rotating Fireplace itself (its own ETB counter), and a second
// battlefield permanent. It answers add for the suspended card (counter
// 0 -> 1), remove for Rotating Fireplace (1 -> 0), and asserts each object
// was asked exactly once -- the cursor-drift regression the option-derived
// snapshot caused -- then replays the whole log.
func TestRotatingFireplaceTimeTravelAddsAndRemoves(t *testing.T) {
	t.Parallel()
	e, cfg, rf, caster := timeTravelBoard(t, 71, "Rotating Fireplace", timeTravelClockSrc, suspendedSorcerySrc)
	// Rotating Fireplace enters tapped with a TIME counter through its own
	// Moved replacement; untap it so its {4},{T} cost is payable, then assert
	// the replacement really did leave the counter the action must see.
	e.emit(events.Event{Kind: events.MoveZone, Obj: rf, From: state.ZHand, To: state.ZBattlefield, Player: caster})
	e.emit(events.Event{Kind: events.Untap, Obj: rf})
	if got := e.G.Obj(rf).Counter("TIME"); got != 1 {
		t.Fatalf("Rotating Fireplace entered with %d TIME counters, want 1 (its ETB replacement)", got)
	}
	clock := putTimePermanent(t, e, caster, timeTravelClockSrc, 2)
	susp := suspendCard(t, e, caster, "Warped Spell", 0)
	if o := e.G.Obj(susp); o.Zone != state.ZExile || o.CastFlags&state.FlagSuspend == 0 || o.Counter("TIME") != 0 {
		t.Fatalf("suspended precondition failed: %+v", o)
	}

	addMana(t, e, caster, "CCCC")
	submitChoices(t, e, timeTravelOption(t, e, rf).Index)

	// The affected order is owned suspended cards first, then battlefield
	// permanents in zone order: {suspended, Rotating Fireplace, Time Clock}.
	want := []state.ObjID{susp, rf, clock}
	seen := drainTimeTravel(t, e, []string{"time_travel_add", "time_travel_remove", "time_travel_skip"}, want)

	if got := e.G.Obj(susp).Counter("TIME"); got != 1 {
		t.Fatalf("suspended card after add = %d TIME, want 1", got)
	}
	if got := e.G.Obj(rf).Counter("TIME"); got != 0 {
		t.Fatalf("Rotating Fireplace after remove = %d TIME, want 0", got)
	}
	if got := e.G.Obj(clock).Counter("TIME"); got != 2 {
		t.Fatalf("Time Clock after skip = %d TIME, want 2", got)
	}
	if len(seen) != 3 {
		t.Fatalf("asked %d objects, want 3", len(seen))
	}
	replayCheck(t, e, cfg)
}

// TestTimeTravelMultipleObjectsRemovedToZeroEachAskedOnce is the cursor-drift
// regression on its own: three battlefield permanents at ONE TIME counter
// each. Removing all three (each to zero) must ask each exactly once; a
// cursor re-derived from the shrinking live eligible set would skip the
// third or double-ask the first.
func TestTimeTravelMultipleObjectsRemovedToZeroEachAskedOnce(t *testing.T) {
	t.Parallel()
	e, cfg, rf, caster := timeTravelBoard(t, 72, "Rotating Fireplace", timeTravelClockSrc, timeTravelClockSrc, timeTravelClockSrc, suspendedSorcerySrc)
	e.emit(events.Event{Kind: events.MoveZone, Obj: rf, From: state.ZHand, To: state.ZBattlefield, Player: caster})
	e.emit(events.Event{Kind: events.Untap, Obj: rf})
	// Move Rotating Fireplace's own ETB counter off it so the shape is
	// exactly three one-counter permanents: Clock A, Clock B, Clock C.
	e.emit(events.Event{Kind: events.CounterChange, Obj: rf, Counter: "TIME", Amount: -1})
	a := putTimePermanent(t, e, caster, timeTravelClockSrc, 1)
	b := putTimePermanent(t, e, caster, timeTravelClockSrc, 1)
	c := putTimePermanent(t, e, caster, timeTravelClockSrc, 1)
	if e.G.Obj(rf).Counter("TIME") != 0 || e.G.Obj(a).Counter("TIME") != 1 {
		t.Fatal("fixture counters wrong before the action")
	}

	addMana(t, e, caster, "CCCC")
	submitChoices(t, e, timeTravelOption(t, e, rf).Index)
	drainTimeTravel(t, e, []string{"time_travel_remove", "time_travel_remove", "time_travel_remove"},
		[]state.ObjID{a, b, c})

	if e.G.Obj(a).Counter("TIME") != 0 || e.G.Obj(b).Counter("TIME") != 0 || e.G.Obj(c).Counter("TIME") != 0 {
		t.Fatalf("removed-to-zero counters = %d/%d/%d, want 0/0/0",
			e.G.Obj(a).Counter("TIME"), e.G.Obj(b).Counter("TIME"), e.G.Obj(c).Counter("TIME"))
	}
	replayCheck(t, e, cfg)
}

// TestTenthDoctorTimeyWimeySorcerySpeedOnly pins the brief's second named
// carrier: The Tenth Doctor's {7} Time Travel, Amount$ 3, "Activate only as
// a sorcery". The ability is withheld with a spell on the stack, offered on
// an empty-stack main phase, and its three repetitions each re-enumerate the
// two affected permanents -- amount 3 times two objects, in order. Adding to
// the clock every repetition proves the repetitions really run (1 -> 4); the
// removal-to-zero re-evaluation half of the rule is
// TestTimeTravelMultipleObjectsRemovedToZeroEachAskedOnce.
func TestTenthDoctorTimeyWimeySorcerySpeedOnly(t *testing.T) {
	t.Parallel()
	e, cfg, doc, caster := timeTravelBoard(t, 73, "The Tenth Doctor", timeTravelClockSrc, timeTravelInstantSrc)
	// The Doctor is a creature, but its ability costs no {T}, so summoning
	// sickness is irrelevant; only the sorcery window gates it. Enter it on
	// the battlefield (via a logged move) and give it a TIME counter so it
	// is itself an affected object.
	e.emit(events.Event{Kind: events.MoveZone, Obj: doc, From: state.ZHand, To: state.ZBattlefield, Player: caster})
	e.emit(events.Event{Kind: events.CounterChange, Obj: doc, Counter: "TIME", Amount: 1})
	a := putTimePermanent(t, e, caster, timeTravelClockSrc, 1)
	if e.G.Obj(doc).Zone != state.ZBattlefield || e.G.Obj(doc).Counter("TIME") != 1 || e.G.Obj(a).Counter("TIME") != 1 {
		t.Fatal("precondition: the Doctor and the clock must both be affected battlefield permanents")
	}

	bolt := findByName(e, "Poke", caster)
	if bolt == 0 {
		t.Fatal("seeded instant not found")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: bolt, From: e.G.Obj(bolt).Zone, To: state.ZHand})
	e.pending = nil
	e.priorityRound()
	addMana(t, e, caster, "CCCCCCC R")

	if _, ok := findAbilityOptionByLabel(e, doc, "Time travel"); !ok {
		t.Fatalf("The Tenth Doctor's Time Travel ability not offered at sorcery speed: %+v", e.Pending().Options)
	}
	// Put an instant on the stack WITHOUT draining it: a sorcery-speed
	// ability must not be offered while a spell is on the stack.
	castIdx := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == bolt {
			castIdx = o.Index
		}
	}
	if castIdx < 0 {
		t.Fatalf("no cast option for the instant: %+v", e.Pending().Options)
	}
	submitChoices(t, e, castIdx)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		idx := -1
		for _, o := range d.Options {
			if o.Obj == doc {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("no target option for the Doctor: %+v", d)
		}
		submitChoices(t, e, idx)
	}
	if len(e.G.Stack) == 0 {
		t.Fatal("the instant never reached the stack")
	}
	if _, ok := findAbilityOptionByLabel(e, doc, "Time travel"); ok {
		t.Fatal("sorcery-speed Time Travel offered with a spell on the stack")
	}
	// Drain the instant so the window is sorcery again, then activate.
	drainToEmptyStack(t, e)
	if _, ok := findAbilityOptionByLabel(e, doc, "Time travel"); !ok {
		t.Fatalf("Time Travel not re-offered once the stack emptied: %+v", e.Pending().Options)
	}
	addMana(t, e, caster, "CCCCCCC")
	submitChoices(t, e, timeTravelOption(t, e, doc).Index)

	// Two affected permanents in zone order (the Doctor, then the clock) x
	// Amount 3 = six elections, the same order each repetition. Adding to the
	// clock every time leaves it affected for all three repetitions.
	want := []state.ObjID{doc, a, doc, a, doc, a}
	drainTimeTravel(t, e, []string{
		"time_travel_skip", "time_travel_add",
		"time_travel_skip", "time_travel_add",
		"time_travel_skip", "time_travel_add",
	}, want)

	if got := e.G.Obj(a).Counter("TIME"); got != 4 {
		t.Fatalf("clock TIME = %d, want 4 (1 + three additions from three repetitions)", got)
	}
	if got := e.G.Obj(doc).Counter("TIME"); got != 1 {
		t.Fatalf("Doctor TIME = %d, want 1 (skipped every repetition)", got)
	}
	replayCheck(t, e, cfg)
}

// drainToEmptyStack passes priority until the stack is empty, answering no
// mid-resolution asks (the instant above has none). It is passUntilStackEmpty
// without the Dig-decline carve-out this file does not need.
func drainToEmptyStack(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 50 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("non-priority decision while draining an empty-stack setup: %+v", d)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		submitChoices(t, e, idx)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack did not empty: %d deep", len(e.G.Stack))
	}
}
