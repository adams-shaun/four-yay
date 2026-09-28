package rules

// The two mill-event trigger modes, end to end on the real compiled corpus
// (task agent-20260919T183731Z-085022e9): Mode$ Milled fires once PER CARD
// milled (Glowing One), and Mode$ MilledAll fires once for the WHOLE mill
// action with the number of matching cards milled this way bound to the
// trigger's TriggerCount$Amount (The Wise Mothman's X).
//
// No Forge script text is committed: both cards are fetched from the corpus
// registry.

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// millTriggerEngine builds a two-seat game with seat 0 holding want and a
// deck of plenty of nonland creatures (so ordering a nonland onto the top of
// its library is always possible), seat 1 on a Mountain deck, and seats want
// on seat 0's battlefield. It returns the engine, the registry, and the ids.
// The library is left as the shuffled deal; callers order its top themselves.
func millTriggerEngine(t *testing.T, want ...*cards.Card) (*Engine, *cards.Registry) {
	t.Helper()
	reg := searchTestRegistry(t)
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := append([]*cards.Card{}, want...)
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	cfg := seatZeroStart(Config{Seed: 7301, Names: []string{"miller", "opponent"},
		Decks: [][]*cards.Card{deck, mountainDeck(t, 40)}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, reg
}

// orderLibraryTop moves the first n objects in ids (matched by card name) to
// the top of player p's library through one real LibraryOrder event, in the
// order given, and returns their ids. It fails when the library does not hold
// enough matching cards, so a vacuous order cannot pass.
func orderLibraryTop(t *testing.T, e *Engine, p state.PlayerID, name string, n int) []state.ObjID {
	t.Helper()
	lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, p)...)
	var top, rest []state.ObjID
	for _, id := range lib {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			continue
		}
		if o.Face().Name == name && len(top) < n {
			top = append(top, id)
		} else {
			rest = append(rest, id)
		}
	}
	if len(top) != n {
		t.Fatalf("library held %d cards named %q, want %d (the deal changed)", len(top), name, n)
	}
	e.emit(events.Event{Kind: events.LibraryOrder, Player: p, IDs: append(top, rest...)})
	return top
}

// seatOnBattlefield enters a real corpus card on player p's battlefield under
// p's control without a cast. These cards have no ETB trigger, so a direct
// entry exercises the mill trigger exactly as a cast would (the inert-entry
// convention Demonic Covenant's test uses).
func seatOnBattlefield(t *testing.T, e *Engine, card *cards.Card, p state.PlayerID) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card, p)
	o.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, p, append([]state.ObjID{o.ID}, e.G.Zone(state.ZBattlefield, p)...))
	return o.ID
}

// resolveMill resolves an api:Mill primitive directly against the engine,
// milling n cards from p's library, exactly as a card's DB$ Mill would.
func resolveMill(t *testing.T, e *Engine, p state.PlayerID, n int) {
	t.Helper()
	effects.Resolve(e, &effects.Ctx{Controller: p},
		&cards.SA{Kind: "DB", API: "Mill", Params: map[string]string{"NumCards": strconv.Itoa(n)}})
}

// drainMillTrigger passes priority until both the stack and the pending-trigger
// queue are empty, so a triggered ability queued by a directly-resolved
// effect (resolveMill) is placed on the stack and resolved, and nothing
// further in the turn is driven. It is the bounded complement to
// passUntilStackEmpty, which only drains once a trigger is already placed.
func drainMillTrigger(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; i < limit && !e.G.Over; i++ {
		if len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 {
			return
		}
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining (stack %d, pending %d)", len(e.G.Stack), len(e.pendingTriggers))
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("non-priority decision %+v while draining the mill trigger", d)
		}
		passOnce(t, e)
	}
}

// TestMillTriggerGlowingOneGainsLifePerNonlandMill is the per-card arm: one
// mill of ONE nonland card fires Glowing One's Mode$ Milled trigger exactly
// once, gaining its controller 1 life. ValidPlayer$ Player matches any
// player's mill, so milling seat 0's own library still gains seat 0 life.
func TestMillTriggerGlowingOneGainsLifePerNonlandMill(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	glowing := searchCorpusCard(t, reg, "Glowing One")
	e, _ := millTriggerEngine(t, glowing)

	// PRECONDITION: Glowing One is on seat 0's battlefield, so its
	// TriggerZones$ Battlefield trigger is live for this scan.
	goID := seatOnBattlefield(t, e, glowing, 0)
	if o := e.G.Obj(goID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Glowing One id %d zone = %v, want battlefield (vacuous setup)", goID, o)
	}

	// The milled cards are nonlands, so the ValidCard$ Card.nonLand filter
	// must accept them; a land top would make this test vacuous.
	milled := orderLibraryTop(t, e, 0, "Grizzly Bears", 1)
	for _, id := range milled {
		f := e.G.Obj(id).Face()
		if f == nil || f.Name != "Grizzly Bears" {
			t.Fatalf("library top %d is %q, want a nonland", id, nameOf(f))
		}
	}

	lifeBefore := e.G.Players[0].Life
	resolveMill(t, e, 0, 1)

	// The milled card reached the graveyard.
	if o := e.G.Obj(milled[0]); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("milled card %d zone = %v, want graveyard", milled[0], o)
	}
	// Drain priority so the queued trigger goes on the stack and resolves.
	drainMillTrigger(t, e, 40)

	if got := e.G.Players[0].Life; got != lifeBefore+1 {
		t.Fatalf("seat 0 life = %d, want %d after one nonland mill (trigger did not fire)", got, lifeBefore+1)
	}
	// The trigger really fired off Glowing One: a TriggerPush naming it.
	fired := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == goID {
			fired = true
		}
	}
	if !fired {
		t.Fatalf("no TriggerPush for Glowing One id %d; the Milled trigger never queued", goID)
	}
}

// TestMillTriggerWiseMothmanBatchesAndCounts is the batch arm: one mill of
// THREE nonland cards fires Mode$ MilledAll exactly ONCE and binds X = 3 (the
// number of matching cards milled this way) to TriggerCount$Amount, so the
// "up to X target creatures" ask offers all three and each chosen target gets
// one +1/+1 counter.
func TestMillTriggerWiseMothmanBatchesAndCounts(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	mothman := searchCorpusCard(t, reg, "The Wise Mothman")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	e, _ := millTriggerEngine(t, mothman)

	// PRECONDITIONS: Mothman on the battlefield, and three distinct target
	// creatures on seat 0's battlefield (so a PutCounter over "up to X" has
	// something to count on and a vacuous board cannot pass).
	mothID := seatOnBattlefield(t, e, mothman, 0)
	if o := e.G.Obj(mothID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Mothman id %d zone = %v, want battlefield (vacuous setup)", mothID, o)
	}
	var targets []state.ObjID
	for i := 0; i < 3; i++ {
		targets = append(targets, seatOnBattlefield(t, e, bear, 0))
	}
	for _, id := range targets {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil {
			t.Fatalf("target %d = %+v, want a battlefield card with a face", id, o)
		}
		if o.Counter("P1P1") != 0 {
			t.Fatalf("target %d already has %d +1/+1 counters (vacuous setup)", id, o.Counter("P1P1"))
		}
	}

	// Three nonland cards on top of seat 0's library: exactly the batch.
	milled := orderLibraryTop(t, e, 0, "Grizzly Bears", 3)
	for _, id := range milled {
		if f := e.G.Obj(id).Face(); f == nil || f.Name != "Grizzly Bears" {
			t.Fatalf("library top %d is %q, want a nonland", id, nameOf(f))
		}
	}

	resolveMill(t, e, 0, 3)

	// The three milled cards reached the graveyard.
	for _, id := range milled {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("milled card %d zone = %v, want graveyard", id, o)
		}
	}

	// The batch trigger's own placement-time target ask: "up to X target
	// creatures", X = 3 (CR 603.3d asks the target at placement, so the ask
	// is the trigger's KTarget, not a mid-resolution KChoose).
	d := passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want the MilledAll target ask (KTarget)", d)
	}
	// X = the number of nonland cards milled this way: the ask must offer all
	// three (plus the source), not one per card and not zero.
	if d.Max != 3 {
		t.Fatalf("MilledAll target ask Max = %d, want 3 (X = nonland cards milled this way)", d.Max)
	}
	// Pin the three Mothman targets as the answers, whatever else the ask
	// offers, so the counters land where the assertions look.
	var choices []int
	for _, id := range targets {
		for _, o := range d.Options {
			if o.Obj == id {
				choices = append(choices, o.Index)
			}
		}
	}
	if len(choices) != 3 {
		t.Fatalf("ask offered %d of the 3 target creatures: %+v", len(choices), d)
	}
	submitChoices(t, e, choices...)
	drainMillTrigger(t, e, 40)

	// Exactly one trigger resolved and placed three +1/+1 counters total.
	total := 0
	for _, id := range targets {
		total += int(e.G.Obj(id).Counter("P1P1"))
	}
	if total != 3 {
		t.Fatalf("+1/+1 counters across the three targets = %d, want 3 (X counters)", total)
	}
	// And the batch fired ONCE: exactly one TriggerPush for Mothman's line.
	pushes := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == mothID {
			pushes++
		}
	}
	// Mothman's other trigger (its ETB/attacks radiation line) is not on the
	// battlefield-entry path here (a direct entry emits no ETB event), so the
	// only TriggerPush naming it is the MilledAll line.
	if pushes != 1 {
		t.Fatalf("TriggerPush count for Mothman = %d, want 1 (MilledAll fires once per mill action)", pushes)
	}
}

// TestMillTriggerModesAreRegistered pins the two primitives' registration
// (effects.RegisterNonAPI), so a revert of the registration -- not just the
// matcher -- fails loudly rather than leaving the triggers silently inert.
func TestMillTriggerModesAreRegistered(t *testing.T) {
	t.Parallel()
	sup := effects.Supported()
	for _, p := range []string{"trig:Milled", "trig:MilledAll"} {
		if !sup[p] {
			t.Fatalf("primitive %s not registered (effects.Supported)", p)
		}
	}
}

// TestMillTriggerMirelurkQueenActivationLimitOncePerTurn pins that the new
// modes honour ActivationLimit$ (Forge's "This ability triggers only once
// each turn"): a second mill in the same turn does not fire Mirelurk Queen's
// MilledAll line again. MilledAll must therefore be in actionTriggerModes,
// which is the scope the trigger-level parameters ride.
func TestMillTriggerMirelurkQueenActivationLimitOncePerTurn(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	queen := searchCorpusCard(t, reg, "Mirelurk Queen")
	e, _ := millTriggerEngine(t, queen)

	qID := seatOnBattlefield(t, e, queen, 0)
	if o := e.G.Obj(qID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Mirelurk Queen id %d zone = %v, want battlefield (vacuous setup)", qID, o)
	}
	// Two nonland cards on top, one for each mill.
	orderLibraryTop(t, e, 0, "Grizzly Bears", 2)

	handBefore := len(e.G.Zone(state.ZHand, 0))
	resolveMill(t, e, 0, 1)
	drainMillTrigger(t, e, 40)
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("hand after first mill = %d, want %d (the trigger did not draw)", got, handBefore+1)
	}
	if c := e.G.Obj(qID).Counter("P1P1"); c != 1 {
		t.Fatalf("queen +1/+1 counters after first mill = %d, want 1", c)
	}

	// A SECOND mill action in the same turn: ActivationLimit$ 1 denies it.
	resolveMill(t, e, 0, 1)
	drainMillTrigger(t, e, 40)
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("hand after second mill = %d, want %d (ActivationLimit$ 1 not enforced)", got, handBefore+1)
	}
	if c := e.G.Obj(qID).Counter("P1P1"); c != 1 {
		t.Fatalf("queen +1/+1 counters after second mill = %d, want 1 (ActivationLimit$ 1 not enforced)", c)
	}
}
