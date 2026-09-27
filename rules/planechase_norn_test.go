package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// This file pins the fix-round findings on the planechase machinery (task
// agent-20260920T064028Z-27dc1b07, review t2):
//
//   - [MAJOR] Norn's Seedcore's `DB$ Planeswalk | Defined$ Remembered |
//     DontPlaneswalkAway$ True` must actually land on the remembered plane
//     and actually suppress the walked-away-from ability -- not rotate and
//     fire it as the deleted AGENTS row documented.
//   - [MINOR] the chaos-ensues scan must reject a marker naming another
//     seat's plane (or a face-up non-top plane) before queueing a trigger.
//
// Helpers (seatZeroStart, moveSeededCard, replayCheck) are the shared harness
// in this package; the planes are real or synthetic cards parsed the way
// rules/planechase_test.go's probePlaneCard does.

// twoDeckConfig builds a 2-seat Config with the given planar decks for BOTH
// seats, so a test can exercise the cross-seat marker boundary (the MINOR
// finding needs a plane that is real but not the marker seat's current one).
func twoDeckConfig(t *testing.T, seed uint64, seat0, seat1 []*cards.Card) Config {
	t.Helper()
	build := func(s uint64) Config {
		return Config{Seed: s, Names: []string{"a", "b"},
			Decks:       [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)},
			PlanarDecks: [][]*cards.Card{seat0, seat1},
		}
	}
	return seatZeroStart(build(seed))
}

// namedProbePlane parses a synthetic plane whose named trigger mode runs a
// Draw, so a test can observe that a plane's ability FIRED (a Draw event)
// rather than only that something was queued. Like probePlaneCard it is a
// real registered primitive (api:Draw), never a test-only API, and it is
// never committed as a Forge script -- it stands in for a corpus plane whose
// own body is not observable in a two-seat board.
func namedProbePlane(t *testing.T, name, trigger string) *cards.Card {
	t.Helper()
	src := "Name:" + name + "\nManaCost:no cost\nTypes:Plane Probe\n" +
		trigger +
		"SVar:Body:DB$ Draw | NumCards$ 1\n" +
		"Oracle:x\n"
	c, err := cards.ParseBytes(name+".txt", []byte(src))
	if err != nil {
		t.Fatalf("parse probe plane %q: %v", name, err)
	}
	c.Link()
	return c
}

// planeWalkedFromProbe is a plane whose "When you planeswalk away from
// CARDNAME" ability draws, so the absence of a Draw proves the suppression.
func planeWalkedFromProbe(t *testing.T, name string) *cards.Card {
	t.Helper()
	return namedProbePlane(t, name,
		"T:Mode$ PlaneswalkedFrom | ValidCard$ Plane.Self | Execute$ Body | TriggerDescription$ When you planeswalk away from CARDNAME, draw a card.\n")
}

// planeWalkedToProbe is a plane whose "When you planeswalk to CARDNAME"
// ability draws, so the Draw proves the arrival trigger fired.
func planeWalkedToProbe(t *testing.T, name string) *cards.Card {
	t.Helper()
	return namedProbePlane(t, name,
		"T:Mode$ PlaneswalkedTo | ValidCard$ Plane.Self | Execute$ Body | TriggerDescription$ When you planeswalk to CARDNAME, draw a card.\n")
}

// planeChaosProbe is a plane whose chaos ability draws.
func planeChaosProbe(t *testing.T, name string) *cards.Card {
	t.Helper()
	return namedProbePlane(t, name,
		"T:Mode$ ChaosEnsues | TriggerZones$ Command | Execute$ Body | TriggerDescription$ Whenever chaos ensues, draw a card.\n")
}

// plainProbePlane is a plane with no triggers at all, so it contributes no
// Draw of its own and cannot mask the ability under test.
func plainProbePlane(t *testing.T, name string) *cards.Card {
	t.Helper()
	c, err := cards.ParseBytes(name+".txt", []byte(
		"Name:"+name+"\nManaCost:no cost\nTypes:Plane Probe\nOracle:x\n"))
	if err != nil {
		t.Fatalf("parse plain plane %q: %v", name, err)
	}
	c.Link()
	return c
}

// drawsAfter returns how many cards seat p drew in the log slice.
func drawsAfter(evs []events.Event, p state.PlayerID) int {
	n := 0
	for _, ev := range evs {
		if ev.Kind == events.Draw && ev.Player == p {
			n++
		}
	}
	return n
}

// drainPlaneTriggers puts every queued trigger on the stack and drains it, so
// a planewalk/chaos ability's body (a Draw) runs before the test asserts.
func drainPlaneTriggers(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; i < limit && len(e.pendingTriggers) > 0; i++ {
		e.putTriggersOnStack()
	}
	for i := 0; i < limit && len(e.G.Stack) > 0; i++ {
		passUntilStackEmpty(t, e, 1)
	}
}

// TestDefinedPlaneswalkLandsOnTheRememberedPlane is the MAJOR pin: Norn's
// Seedcore's `Defined$ Remembered` planeswalk must move the walk to the named
// plane, NOT rotate to the deck's next plane. With a 3-plane deck whose
// current plane is planes[0] and whose named destination is planes[2], a
// rotation would land on planes[1]; the defined walk must land on planes[2].
func TestDefinedPlaneswalkLandsOnTheRememberedPlane(t *testing.T) {
	p0 := planeWalkedToProbe(t, "Probe Zero")
	p1 := planeWalkedToProbe(t, "Probe One")
	p2 := planeWalkedToProbe(t, "Probe Two")
	cfg := twoDeckConfig(t, 83001, []*cards.Card{p0, p1, p2}, nil)
	e := New(cfg)
	e.Advance()

	// PRECONDITION: planes[0] is the current plane and p1/p2 are the other
	// two by object id, in deck order.
	ids := e.G.Zone(state.ZPlanarDeck, 0)
	if len(ids) != 3 {
		t.Fatalf("planar deck has %d cards, want 3: %v", len(ids), ids)
	}
	cur := currentPlaneOfTest(t, e, 0)
	if cur != ids[0] {
		t.Fatalf("current plane %d, want the deck top %d", cur, ids[0])
	}
	dest := ids[2]

	before := len(e.L.Events)
	e.PlaneswalkTo(0, []state.ObjID{dest}, false)
	drainPlaneTriggers(t, e, 20)

	// The walk must be recorded with the destination named and the departing
	// plane in Obj.
	var walked bool
	for _, ev := range e.L.Events[before:] {
		if ev.Kind != events.PlanarWalk || ev.Player != 0 {
			continue
		}
		walked = true
		if ev.Obj != cur {
			t.Fatalf("PlanarWalk Obj = %d, want the departing plane %d", ev.Obj, cur)
		}
		if len(ev.IDs) != 1 || ev.IDs[0] != dest {
			t.Fatalf("PlanarWalk IDs = %v, want the destination %d", ev.IDs, dest)
		}
		if ev.Amount != 0 {
			t.Fatalf("PlanarWalk Amount = %d, want 0 (no DontPlaneswalkAway)", ev.Amount)
		}
	}
	if !walked {
		t.Fatal("no PlanarWalk event was emitted")
	}

	// THE FIX: the current plane is the named destination, not the rotation's
	// next plane.
	got := currentPlaneOfTest(t, e, 0)
	if got != dest {
		t.Fatalf("after the defined walk the current plane is %d, want the remembered plane %d (a rotation would have landed on %d)", got, dest, ids[1])
	}
	// The zone is still a permutation of the same three planes -- the fold
	// never loses or duplicates one.
	after := e.G.Zone(state.ZPlanarDeck, 0)
	if len(after) != 3 {
		t.Fatalf("after the walk the planar deck has %d cards, want 3", len(after))
	}
	for _, id := range ids {
		if !zoneContainsID(after, id) {
			t.Fatalf("plane %d left the deck: %v", id, after)
		}
	}
	// The arrival trigger fired (a Draw after the walk), which is the
	// observable proof the defined destination is recognized as "planeswalked
	// to" rather than merely reordered.
	if drawsAfter(e.L.Events[before:], 0) == 0 {
		t.Fatal("the destination plane's Mode$ PlaneswalkedTo ability never fired (no Draw)")
	}
	replayCheck(t, e, cfg)
}

// TestDontPlaneswalkAwaySuppressesTheFromTrigger is the other half of the
// MAJOR pin: Norn's "except don't planeswalk away from any plane" must
// suppress the walked-away-from plane's Mode$ PlaneswalkedFrom ability. The
// control arm (same walk without the flag) proves the ability DOES fire
// otherwise, so the test distinguishes suppression from a trigger that never
// worked.
func TestDontPlaneswalkAwaySuppressesTheFromTrigger(t *testing.T) {
	away := planeWalkedFromProbe(t, "Probe Away")
	// The destination is PLAIN: only the departed plane's PlaneswalkedFrom
	// ability can produce a Draw, so the count isolates the ability under
	// test (an arrival trigger would fire in both arms and mask it).
	dest := plainProbePlane(t, "Probe Dest")

	// Control arm: an ordinary walk fires the away trigger.
	cfgA := twoDeckConfig(t, 83011, []*cards.Card{away, dest}, nil)
	eA := New(cfgA)
	eA.Advance()
	idsA := eA.G.Zone(state.ZPlanarDeck, 0)
	if len(idsA) != 2 {
		t.Fatalf("control planar deck has %d cards, want 2", len(idsA))
	}
	beforeA := len(eA.L.Events)
	eA.PlaneswalkTo(0, []state.ObjID{idsA[1]}, false)
	drainPlaneTriggers(t, eA, 20)
	if got := drawsAfter(eA.L.Events[beforeA:], 0); got != 1 {
		t.Fatalf("control walk drew %d cards, want exactly 1 from the away trigger", got)
	}
	replayCheck(t, eA, cfgA)

	// Test arm: the same walk with DontPlaneswalkAway$ True fires nothing.
	cfgB := twoDeckConfig(t, 83011, []*cards.Card{away, dest}, nil)
	eB := New(cfgB)
	eB.Advance()
	idsB := eB.G.Zone(state.ZPlanarDeck, 0)
	beforeB := len(eB.L.Events)
	eB.PlaneswalkTo(0, []state.ObjID{idsB[1]}, true)
	drainPlaneTriggers(t, eB, 20)
	if got := drawsAfter(eB.L.Events[beforeB:], 0); got != 0 {
		t.Fatalf("DontPlaneswalkAway$ walk drew %d cards, want 0 (the away trigger must be suppressed)", got)
	}
	// The walk still happened: the destination is current.
	if got := currentPlaneOfTest(t, eB, 0); got != idsB[1] {
		t.Fatalf("after the suppressing walk the current plane is %d, want the destination %d", got, idsB[1])
	}
	// The suppression is recorded on the event boundary, not merely absent.
	var flagged bool
	for _, ev := range eB.L.Events[beforeB:] {
		if ev.Kind == events.PlanarWalk && ev.Player == 0 && ev.Amount == events.PlanarWalkDontPlaneswalkAway {
			flagged = true
		}
	}
	if !flagged {
		t.Fatal("the DontPlaneswalkAway$ walk did not carry the PlanarWalk flag")
	}
	replayCheck(t, eB, cfgB)
}

// TestNornsSeedcoreVerbLandsOnRememberedAndSuppressesAwayWalk drives the REAL
// corpus carrier's own SVar: `DB$ Planeswalk | Defined$ Remembered |
// DontPlaneswalkAway$ True` resolved against a planar deck and a Ctx whose
// Remembered names a plane. That is exactly the shape the deleted AGENTS row
// documented as inert, so this is the end-to-end pin on the finding.
func TestNornsSeedcoreVerbLandsOnRememberedAndSuppressesAwayWalk(t *testing.T) {
	seedcore := tokenReplCorpusCard(t, "Norn's Seedcore")
	away := planeWalkedFromProbe(t, "Probe Away")
	// Plain destination: the only possible Draw is the departed plane's
	// PlaneswalkedFrom, which must be suppressed here.
	other := plainProbePlane(t, "Probe Other")

	cfg := twoDeckConfig(t, 83021, []*cards.Card{seedcore, away, other}, nil)
	e := New(cfg)
	e.Advance()

	ids := e.G.Zone(state.ZPlanarDeck, 0)
	if len(ids) != 3 {
		t.Fatalf("planar deck has %d cards, want 3", len(ids))
	}
	// The seedcore is the current plane; the remembered destination is the
	// last plane (a rotation from the seedcore would reveal `away`, the next
	// one), so landing on `other` proves Defined$ was read.
	remembered := ids[2]
	if ids[2] != e.G.Obj(remembered).ID {
		t.Fatalf("bad fixture: remembered %d", remembered)
	}
	sa := nornsSeedcorePlaneswalkSA(t)

	before := len(e.L.Events)
	effects.Resolve(e, &effects.Ctx{
		Controller: 0,
		Source:     ids[0],
		Remembered: []state.Target{{Obj: remembered}},
	}, sa)
	drainPlaneTriggers(t, e, 20)

	if got := currentPlaneOfTest(t, e, 0); got != remembered {
		t.Fatalf("Norn's Seedcore planeswalk landed on %d, want the remembered plane %d", got, remembered)
	}
	if got := drawsAfter(e.L.Events[before:], 0); got != 0 {
		t.Fatalf("Norn's Seedcore planeswalk drew %d cards, want 0 (DontPlaneswalkAway$ must suppress the away trigger)", got)
	}
	var flagged bool
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.PlanarWalk && ev.Player == 0 {
			flagged = flagged || ev.Amount == events.PlanarWalkDontPlaneswalkAway
			if len(ev.IDs) != 1 || ev.IDs[0] != remembered {
				t.Fatalf("Norn's Seedcore PlanarWalk IDs = %v, want [%d]", ev.IDs, remembered)
			}
		}
	}
	if !flagged {
		t.Fatal("Norn's Seedcore's DontPlaneswalkAway$ True did not flag the walk")
	}
	replayCheck(t, e, cfg)
}

// nornsSeedcorePlaneswalkSA returns the real DBPlaneswalk SVar body from the
// corpus-seeded Norn's Seedcore card, so the test drives the card's own
// parameter text rather than a re-typed copy of it.
func nornsSeedcorePlaneswalkSA(t *testing.T) *cards.SA {
	t.Helper()
	seedcore := tokenReplCorpusCard(t, "Norn's Seedcore")
	sa := cards.ResolveSVar(seedcore.Faces[0].SVars, "DBPlaneswalk")
	if sa == nil || sa.API != "Planeswalk" {
		t.Fatalf("Norn's Seedcore DBPlaneswalk is not a compiled Planeswalk SA: %+v", sa)
	}
	if sa.Params["Defined"] != "Remembered" || sa.Params["DontPlaneswalkAway"] != "True" {
		t.Fatalf("Norn's Seedcore DBPlaneswalk params drifted: %+v", sa.Params)
	}
	return sa
}

// TestDefinedChaosEnsuesEruptsTheRememberedPlane is the chaos half of the
// MAJOR pin: The Fertile Lands of Saulvinia's `DB$ ChaosEnsues | Defined$
// Remembered` must erupt the remembered plane, not the controller's current
// one. Two arms:
//
//   - `Defined$ Remembered` naming a NON-current own plane emits a marker
//     for exactly that plane (proving the verb reads Defined$ rather than
//     falling back to current), while the chaos SCAN correctly refuses to
//     fire a trigger for a plane that is not the seat's face-up current one
//     (the MINOR boundary) -- so this arm also pins the boundary together
//     with the verb.
//   - a plain `DB$ ChaosEnsues` (no Defined$) with the same Remembered set
//     falls back to the current plane, so the two paths are distinguishable.
func TestDefinedChaosEnsuesEruptsTheRememberedPlane(t *testing.T) {
	cur := planeChaosProbe(t, "Probe Current")
	low := plainProbePlane(t, "Probe Lower")
	cfg := twoDeckConfig(t, 83031, []*cards.Card{cur, low}, nil)
	e := New(cfg)
	e.Advance()
	ids := e.G.Zone(state.ZPlanarDeck, 0)
	if len(ids) != 2 {
		t.Fatalf("planar deck has %d cards, want 2", len(ids))
	}
	current := currentPlaneOfTest(t, e, 0)
	if current == 0 {
		t.Fatal("precondition: no current plane")
	}
	// The remembered plane is the OTHER one (not current), so naming it can
	// only come from Defined$ Remembered.
	var other state.ObjID
	for _, id := range ids {
		if id != current {
			other = id
		}
	}
	if other == 0 {
		t.Fatal("precondition: no second plane to remember")
	}

	definedSA := mustParseVerb(t, "Name:Chaos Defined\nTypes:Sorcery\nA:SP$ ChaosEnsues | Defined$ Remembered\nOracle:x\n")
	plainSA := mustParseVerb(t, "Name:Chaos Plain\nTypes:Sorcery\nA:SP$ ChaosEnsues\nOracle:x\n")

	// Arm 1: Defined$ Remembered names the non-current plane.
	before := len(e.L.Events)
	effects.Resolve(e, &effects.Ctx{
		Controller: 0,
		Source:     current,
		Remembered: []state.Target{{Obj: other}},
	}, definedSA)
	var sawOther, sawCurrent bool
	for _, ev := range e.L.Events[before:] {
		if ev.Kind != events.ChaosEnsues {
			continue
		}
		if ev.Obj == other {
			sawOther = true
		}
		if ev.Obj == current {
			sawCurrent = true
		}
	}
	if !sawOther {
		t.Fatalf("DB$ ChaosEnsues | Defined$ Remembered emitted no marker for the remembered plane %d", other)
	}
	if sawCurrent {
		t.Fatalf("the defined chaos verb also erupted the current plane %d", current)
	}
	// The marker named a non-current plane, so the scan must have queued
	// nothing for it (the MINOR boundary) -- it is not the seat's current
	// plane and it is face down.
	drainPlaneTriggers(t, e, 10)
	if got := drawsAfter(e.L.Events[before:], 0); got != 0 {
		t.Fatalf("a marker for the non-current remembered plane fired %d chaos abilities, want 0", got)
	}

	// Arm 2: no Defined$ -- the same Remembered set is ignored and the marker
	// names the CURRENT plane.
	before2 := len(e.L.Events)
	effects.Resolve(e, &effects.Ctx{
		Controller: 0,
		Source:     current,
		Remembered: []state.Target{{Obj: other}},
	}, plainSA)
	var plainOther, plainCurrent bool
	for _, ev := range e.L.Events[before2:] {
		if ev.Kind != events.ChaosEnsues {
			continue
		}
		if ev.Obj == other {
			plainOther = true
		}
		if ev.Obj == current {
			plainCurrent = true
		}
	}
	if !plainCurrent {
		t.Fatalf("a plain DB$ ChaosEnsues did not name the current plane %d", current)
	}
	if plainOther {
		t.Fatalf("a plain DB$ ChaosEnsues named the remembered non-current plane %d", other)
	}
	replayCheck(t, e, cfg)
}

// mustParseVerb compiles a one-ability synthetic sorcery and returns its
// first ability, so a verb test drives exactly the printed parameter text.
func mustParseVerb(t *testing.T, src string) *cards.SA {
	t.Helper()
	sa, err := cards.ParseBytes("verb.txt", []byte(src))
	if err != nil {
		t.Fatalf("parse verb: %v", err)
	}
	sa.Link()
	if len(sa.Faces) == 0 || len(sa.Faces[0].Abilities) == 0 {
		t.Fatalf("verb has no ability: %q", src)
	}
	return sa.Faces[0].Abilities[0]
}

// TestChaosScanRejectsAnotherSeatsPlaneMarker is the MINOR pin: a
// hand-built ChaosEnsues marker naming a plane that is not the marker
// Player's own current plane must queue nothing. Two negative cases: seat 0
// names seat 1's face-up plane, and seat 0 names its own face-down lower
// plane. A positive control on seat 0's real current plane proves the scan
// still fires for a valid marker.
func TestChaosScanRejectsAnotherSeatsPlaneMarker(t *testing.T) {
	mine := planeChaosProbe(t, "Probe Mine")
	theirs := planeChaosProbe(t, "Probe Theirs")
	cfg := twoDeckConfig(t, 83041, []*cards.Card{mine}, []*cards.Card{theirs})
	e := New(cfg)
	e.Advance()

	myPlane := currentPlaneOfTest(t, e, 0)
	theirIDs := e.G.Zone(state.ZPlanarDeck, 1)
	if myPlane == 0 || len(theirIDs) == 0 {
		t.Fatalf("precondition: my plane %d, their deck %v", myPlane, theirIDs)
	}
	theirPlane := currentPlaneOfTest(t, e, 1)
	if theirPlane == 0 {
		t.Fatal("precondition: seat 1 has no face-up current plane")
	}

	// Negative 1: seat 0's marker names seat 1's plane.
	e.pendingTriggers = nil
	before := len(e.L.Events)
	e.emit(events.Event{Kind: events.ChaosEnsues, Player: 0, Obj: theirPlane})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("a marker naming another seat's plane queued %d triggers: %+v", len(e.pendingTriggers), e.pendingTriggers)
	}
	drainPlaneTriggers(t, e, 10)
	if got := drawsAfter(e.L.Events[before:], 0); got != 0 {
		t.Fatalf("a foreign-plane marker made seat 0 draw %d cards, want 0", got)
	}

	// Negative 2: seat 0's marker names its own face-down lower plane.
	extra := planeChaosProbe(t, "Probe Extra")
	cfg2 := twoDeckConfig(t, 83042, []*cards.Card{mine, extra}, []*cards.Card{theirs})
	e2 := New(cfg2)
	e2.Advance()
	if len(e2.G.Zone(state.ZPlanarDeck, 0)) < 2 {
		t.Fatal("fixture needs a second plane to name a non-top one")
	}
	lowID := e2.G.Zone(state.ZPlanarDeck, 0)[1]
	e2.pendingTriggers = nil
	before2 := len(e2.L.Events)
	e2.emit(events.Event{Kind: events.ChaosEnsues, Player: 0, Obj: lowID})
	if len(e2.pendingTriggers) != 0 {
		t.Fatalf("a marker naming a face-down lower plane queued %d triggers", len(e2.pendingTriggers))
	}
	drainPlaneTriggers(t, e2, 10)
	if got := drawsAfter(e2.L.Events[before2:], 0); got != 0 {
		t.Fatalf("a non-top-plane marker made seat 0 draw %d cards, want 0", got)
	}

	// Positive control: the real current plane still fires.
	e.pendingTriggers = nil
	before3 := len(e.L.Events)
	e.emit(events.Event{Kind: events.ChaosEnsues, Player: 0, Obj: myPlane})
	drainPlaneTriggers(t, e, 20)
	if got := drawsAfter(e.L.Events[before3:], 0); got != 1 {
		t.Fatalf("the valid current-plane marker drew %d cards, want exactly 1", got)
	}
}

// TestNornsSeedcorePlaneswalkedToEruptsChaos is the real-corpus arrival pin:
// Norn's Seedcore's own `T:Mode$ PlaneswalkedTo | ValidCard$ Plane.Self |
// Execute$ TrigChaos` (DB$ ChaosEnsues) must queue on arrival. The trigger is
// asserted at the QUEUE rather than drained end to end, because the seedcore's
// chaos body is the unimplemented `DigUntil | DigZone$ PlanarDeck` remainder
// (loud-unimplemented by design); draining it would enter that remainder, not
// the arrival path under test.
func TestNornsSeedcorePlaneswalkedToEruptsChaos(t *testing.T) {
	seedcore := tokenReplCorpusCard(t, "Norn's Seedcore")
	other := plainProbePlane(t, "Probe Other")
	cfg := twoDeckConfig(t, 83051, []*cards.Card{seedcore, other}, nil)
	e := New(cfg)
	e.Advance()
	ids := e.G.Zone(state.ZPlanarDeck, 0)
	if len(ids) != 2 {
		t.Fatalf("planar deck has %d cards, want 2", len(ids))
	}
	var seedcoreID state.ObjID
	for _, id := range ids {
		if e.G.Obj(id).Face().Name == "Norn's Seedcore" {
			seedcoreID = id
		}
	}
	if seedcoreID == 0 {
		t.Fatal("the seedcore is not in the deck")
	}
	// Walk to the seedcore from wherever we are.
	e.PlaneswalkTo(0, []state.ObjID{seedcoreID}, false)

	if got := currentPlaneOfTest(t, e, 0); got != seedcoreID {
		t.Fatalf("current plane %d, want the seedcore %d", got, seedcoreID)
	}
	// The seedcore's PlaneswalkedTo trigger must be queued with the seedcore
	// as its source and the DB$ ChaosEnsues body.
	wantSA := cards.ResolveSVar(e.G.Obj(seedcoreID).Face().SVars, "TrigChaos")
	if wantSA == nil || wantSA.API != "ChaosEnsues" {
		t.Fatalf("seedcore TrigChaos drifted: %+v", wantSA)
	}
	var queued bool
	for _, pt := range e.pendingTriggers {
		if pt.Source == seedcoreID && pt.SA != nil && pt.SA.API == "ChaosEnsues" {
			queued = true
		}
	}
	if !queued {
		t.Fatalf("Norn's Seedcore's PlaneswalkedTo did not queue its DB$ ChaosEnsues body: %+v", e.pendingTriggers)
	}
	// Put it on the stack and resolve it: the marker names the seedcore.
	drainPlaneTriggers(t, e, 20)
	var marker bool
	for _, ev := range e.L.Events {
		if ev.Kind == events.ChaosEnsues && ev.Obj == seedcoreID && ev.Player == 0 {
			marker = true
		}
	}
	if !marker {
		t.Fatalf("Norn's Seedcore's PlaneswalkedTo did not erupt chaos on the seedcore %d", seedcoreID)
	}
	replayCheck(t, e, cfg)
}
