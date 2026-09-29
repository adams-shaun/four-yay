package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// recruitEngine puts carrier on seat 0's battlefield, seeds seat 0's library
// with lib, queues the carrier's compiled TrigRecruit trigger and resolves
// it. The returned engine sits at the pending discard ask when the post-draw
// hand holds more than one card, and at the completed action otherwise.
func recruitEngine(t *testing.T, carrier string, handExtra, lib []*cards.Card) *Engine {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e := New(Config{Seed: 1, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)},
		Tokens: reg.Tokens})
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	var ids []state.ObjID
	addHand := func(c *cards.Card) state.ObjID {
		o := e.G.AddObject(c, 0)
		o.Zone = state.ZHand
		ids = append(ids, o.ID)
		return o.ID
	}
	carrierID := addHand(hobCard(t, reg, carrier))
	for _, c := range handExtra {
		addHand(c)
	}
	e.G.SetZone(state.ZHand, 0, ids)
	var libIDs []state.ObjID
	for _, c := range lib {
		o := e.G.AddObject(c, 0)
		o.Zone = state.ZLibrary
		libIDs = append(libIDs, o.ID)
	}
	e.G.SetZone(state.ZLibrary, 0, libIDs)
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1

	// Move the carrier to the battlefield and queue its compiled Recruit
	// trigger directly, isolating the action from the trigger's own event.
	e.emit(events.Event{Kind: events.MoveZone, Obj: carrierID, From: state.ZHand, To: state.ZBattlefield})
	if o := e.G.Obj(carrierID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Recruit carrier %q did not enter the battlefield", carrier)
	}
	if len(lib) == 0 {
		t.Fatal("precondition: recruitEngine needs a nonempty library for the draw")
	}
	trigIdx := -1
	for i, tr := range e.G.Obj(carrierID).Face().Triggers {
		if tr.Params["Execute"] == "TrigRecruit" {
			trigIdx = i
			break
		}
	}
	if trigIdx < 0 {
		t.Fatalf("precondition: %q has no compiled TrigRecruit trigger", carrier)
	}
	sa := e.G.Obj(carrierID).Face().Triggers[trigIdx].Effect
	if sa == nil || sa.API != "Recruit" {
		t.Fatalf("precondition: TrigRecruit SA API = %v, want Recruit", sa)
	}
	e.pushTrigger(pendingTrigger{Source: carrierID, Controller: 0, Idx: trigIdx, SA: sa})
	if len(e.G.Stack) == 0 {
		t.Fatal("precondition: Recruit trigger was not put on the stack")
	}
	e.resolveTop()
	return e
}

// recruitObjIDByName returns the id of the (unique) card named name in seat
// p's zone, or a fatal.
func recruitObjIDByName(t *testing.T, e *Engine, z state.Zone, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	id := state.ObjID(0)
	n := 0
	for _, cand := range e.G.Zone(z, p) {
		o := e.G.Obj(cand)
		if o != nil && o.Face() != nil && o.Face().Name == name {
			id = cand
			n++
		}
	}
	if n != 1 {
		t.Fatalf("precondition: %d objects named %q in zone %v, want exactly 1", n, name, z)
	}
	return id
}

// recruitPick submits the discard option naming wanted and returns the
// decision that was pending.
func recruitPick(t *testing.T, e *Engine, wanted state.ObjID) *decision.Decision {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("precondition: no pending decision at the Recruit discard ask")
	}
	for _, o := range d.Options {
		if o.Obj == wanted {
			submitChoices(t, e, o.Index)
			return d
		}
	}
	t.Fatalf("precondition: card %d is not offered among %+v", wanted, d.Options)
	return nil
}

// recruitSoldierCount counts Human Soldier tokens on seat 0's battlefield.
func recruitSoldierCount(e *Engine) int {
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o != nil && o.Face() != nil && o.Face().Name == "Human Soldier Token" {
			n++
		}
	}
	return n
}

// TestRecruitDiscardsNonlandAndMakesSoldier: api:Recruit's full action (CR
// 701.9): draw, then discard a chosen card, then create a 1/1 white Human
// Soldier when a nonland was discarded.
func TestRecruitDiscardsNonlandAndMakesSoldier(t *testing.T) {
	t.Parallel()
	filler := card(t, "Name:Recruit Filler\nTypes:Instant\nOracle:x\n")
	other := card(t, "Name:Recruit Other Filler\nTypes:Instant\nOracle:x\n")
	drawn := card(t, "Name:Recruit Drawn Card\nTypes:Sorcery\nOracle:x\n")
	e := recruitEngine(t, "Great Gilded Boat", []*cards.Card{filler, other}, []*cards.Card{drawn})

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("discard ask = %+v, want a KChoose after the draw", d)
	}
	if d.Player != 0 {
		t.Fatalf("discard ask seat = %d, want the Recruit controller's seat 0", d.Player)
	}
	// Precondition: the offer is a REAL choice (more than one card in hand),
	// which is exactly when the strict-supersets rule poses an ask.
	if len(d.Options) < 2 {
		t.Fatalf("discard options = %d, want a real choice over the post-draw hand", len(d.Options))
	}
	picked := recruitObjIDByName(t, e, state.ZHand, 0, "Recruit Filler")
	if recruitSoldierCount(e) != 0 {
		t.Fatal("precondition: a Human Soldier existed before the discard resolved")
	}
	recruitPick(t, e, picked)

	if o := e.G.Obj(picked); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("discarded card = %+v, want it in the graveyard", e.G.Obj(picked))
	}
	n := recruitSoldierCount(e)
	if n != 1 {
		t.Fatalf("Human Soldier tokens after discarding a nonland = %d, want 1 (CR 701.9)", n)
	}
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil || o.Face().Name != "Human Soldier Token" {
			continue
		}
		if !hobHasType(o.Face(), "Human") || !hobHasType(o.Face(), "Soldier") ||
			!hobHasType(o.Face(), "Creature") || o.Face().PT != "1/1" {
			t.Fatalf("soldier token = types %v PT %q, want a 1/1 Human Soldier creature",
				o.Face().Types, o.Face().PT)
		}
	}
}

// TestRecruitNoSoldierWhenLandDiscarded: the token half is conditional. A
// land discard completes the draw and the discard but makes no token -- and
// the handler must still have run (no unknown-script Note, the discarded
// land really in the graveyard).
func TestRecruitNoSoldierWhenLandDiscarded(t *testing.T) {
	t.Parallel()
	landA := card(t, "Name:Recruit Land A\nTypes:Land\nOracle:x\n")
	landB := card(t, "Name:Recruit Land B\nTypes:Land\nOracle:x\n")
	drawn := card(t, "Name:Recruit Drawn Land\nTypes:Land\nOracle:x\n")
	e := recruitEngine(t, "Great Gilded Boat", []*cards.Card{landA, landB}, []*cards.Card{drawn})

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("discard ask = %+v, want a KChoose after the draw", d)
	}
	if len(d.Options) < 2 {
		t.Fatalf("discard options = %d, want a real choice over the post-draw hand", len(d.Options))
	}
	picked := recruitObjIDByName(t, e, state.ZHand, 0, "Recruit Land A")
	before := len(e.L.Events)
	recruitPick(t, e, picked)

	if o := e.G.Obj(picked); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("discarded land = %+v, want it in the graveyard (handler must have run)", e.G.Obj(picked))
	}
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.TokenCreate && ev.Text == "w_1_1_human_soldier" {
			t.Fatal("a Human Soldier token was created after a LAND discard (CR 701.9 condition)")
		}
		if ev.Kind == events.Note && strings.Contains(ev.Text, "Recruit:") {
			t.Fatalf("Recruit emitted a Note: %q", ev.Text)
		}
	}
	if team := recruitSoldierCount(e); team != 0 {
		t.Fatalf("Human Soldier tokens after discarding a land = %d, want 0", team)
	}
}

// TestSetAudit_hob_RecruitCarriersAreSupported names the other affected
// cards: every one of The Hobbit's ten api:Recruit carriers must compile
// without api:Recruit in its unsupported set.
func TestSetAudit_hob_RecruitCarriersAreSupported(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	if !supported["api:Recruit"] {
		t.Fatal("precondition: api:Recruit is not registered in effects.Supported()")
	}
	carriers := []string{
		"Celebrate the Mountain-king",
		"Esgaroth Garrison",
		"Lake-town Lookout",
		"The Mountain-king's Return",
		"The Queen of Dale",
		"Great Gilded Boat",
		"Long Lake Nuisance",
		"Sound the Trumpets",
		"Bard's Company",
		"Patient Instructor",
	}
	for _, name := range carriers {
		c := hobCard(t, reg, name)
		for _, miss := range reg.Unsupported(c, supported) {
			if strings.Contains(miss, "api:Recruit") {
				t.Errorf("%s is Unsupported [%s]", name, miss)
			}
		}
	}
}
