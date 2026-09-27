package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// gainControlVariantBoard builds a four-permanent board for the
// GainControlVariant tests:
//
//   - bear:    a creature owned and controlled by seat 0 (already
//     owner-controlled).
//   - stolen:  a creature owned by seat 1 but controlled by seat 0 (the
//     creature the effect must hand back).
//   - theirs:  a creature owned and controlled by seat 1.
//   - relic:   an ARTIFACT owned by seat 1 but controlled by seat 0, so
//     `AllValid$ Creature` must leave it alone even though its controller
//     differs from its owner.
//
// Objects are returned in a fixed order; their IDs are creation order.
func gainControlVariantBoard(t *testing.T) (*state.Game, map[string]state.ObjID) {
	t.Helper()
	g := state.NewGame([]string{"you", "them"})
	mk := func(owner state.PlayerID, src string) state.ObjID {
		o := g.AddObject(mkCard(t, src), owner)
		h := &fakeHost{g: g}
		h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
		return o.ID
	}
	ids := map[string]state.ObjID{
		"bear":   mk(0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"),
		"stolen": mk(1, "Name:Stolen\nManaCost:2 R\nTypes:Creature Giant\nPT:5/5\nOracle:x\n"),
		"theirs": mk(1, "Name:Theirs\nManaCost:1 U\nTypes:Creature Bird\nPT:1/1\nOracle:x\n"),
		"relic":  mk(1, "Name:Relic\nManaCost:1\nTypes:Artifact\nOracle:x\n"),
	}
	// stolen and relic are under seat 0's control though seat 1 owns them.
	g.Obj(ids["stolen"]).Controller = 0
	g.Obj(ids["relic"]).Controller = 0
	return g, ids
}

// TestGainControlVariantAliciaMastersCorpusSA exercises the real corpus SA of
// Alicia Masters, Skilled Sculptor's "Sense the Good" ability: at the
// beginning of your end step, each player gains control of all creatures
// they own.
func TestGainControlVariantAliciaMastersCorpusSA(t *testing.T) {
	_, sa := corpusSA(t, "Alicia Masters, Skilled Sculptor", "TrigGainControl")
	if sa.API != "GainControlVariant" {
		t.Fatalf("unexpected SA: %+v", sa)
	}
	if sa.Params["AllValid"] != "Creature" || sa.Params["ChangeController"] != "CardOwner" {
		t.Fatalf("unexpected SA params: %+v", sa.Params)
	}

	g, ids := gainControlVariantBoard(t)
	// Precondition: the two objects the effect must move/leave really carry a
	// controller different from their owner, and the excluded non-creature
	// really is one.
	if g.Obj(ids["stolen"]).Owner != 1 || g.Obj(ids["stolen"]).Controller != 0 {
		t.Fatalf("precondition: stolen owner/controller = %d/%d, want 1/0",
			g.Obj(ids["stolen"]).Owner, g.Obj(ids["stolen"]).Controller)
	}
	if r := g.Obj(ids["relic"]); r.Owner != 1 || r.Controller != 0 || MatchesObjectCtx(g, "Creature", r, SpecContext{}) {
		t.Fatalf("precondition: relic owner/controller/creature = %d/%d/%v, want 1/0/false",
			r.Owner, r.Controller, MatchesObjectCtx(g, "Creature", r, SpecContext{}))
	}
	if b := g.Obj(ids["bear"]); b.Owner != 0 || b.Controller != 0 {
		t.Fatalf("precondition: bear owner/controller = %d/%d, want 0/0", b.Owner, b.Controller)
	}
	if th := g.Obj(ids["theirs"]); th.Owner != 1 || th.Controller != 1 {
		t.Fatalf("precondition: theirs owner/controller = %d/%d, want 1/1", th.Owner, th.Controller)
	}

	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0, Source: ids["bear"]}, sa)

	if got := g.Obj(ids["stolen"]).Controller; got != 1 {
		t.Fatalf("stolen controller = %d, want 1 (its owner)", got)
	}
	if got := g.Obj(ids["bear"]).Controller; got != 0 {
		t.Fatalf("bear controller = %d, want 0", got)
	}
	if got := g.Obj(ids["theirs"]).Controller; got != 1 {
		t.Fatalf("theirs controller = %d, want 1", got)
	}
	if got := g.Obj(ids["relic"]).Controller; got != 0 {
		t.Fatalf("relic controller = %d, want 0 (AllValid$ Creature must exclude it)", got)
	}

	// The change is event-backed: exactly one ControlChange, for the stolen
	// creature, naming its owner; and a grant is registered for EVERY matching
	// creature -- including the two their owners already control (CR 613.7:
	// the newest control effect on each permanent is this one).
	var changes []events.Event
	for _, e := range h.log {
		if e.Kind == events.ControlChange {
			changes = append(changes, e)
		}
	}
	if len(changes) != 1 || changes[0].Obj != ids["stolen"] || changes[0].Player != 1 {
		t.Fatalf("ControlChange events = %+v, want exactly one for stolen -> 1", changes)
	}
	// Arena order is bear, stolen, theirs (relic is excluded by the filter);
	// each gets a grant, and only the stolen one moves visibly.
	wantGrants := []ControlGrant{
		{Obj: ids["bear"], Previous: 0, Controller: 0},
		{Obj: ids["stolen"], Previous: 0, Controller: 1},
		{Obj: ids["theirs"], Previous: 1, Controller: 1},
	}
	if len(h.controls) != len(wantGrants) {
		t.Fatalf("control grants = %+v, want %d grants (one per matching creature)", h.controls, len(wantGrants))
	}
	for i, want := range wantGrants {
		got := h.controls[i]
		if got.Obj != want.Obj || got.Previous != want.Previous || got.Controller != want.Controller {
			t.Fatalf("grant %d = %+v, want obj %d previous %d controller %d", i, got, want.Obj, want.Previous, want.Controller)
		}
	}
}

// TestGainControlVariantRejectsUnsupportedChangeController proves the
// fail-closed contract: a ChangeController$ value the engine does not model
// must emit a loud Note and change NOTHING -- never fall through to the
// owner-directed behaviour and flip control to the wrong player. The four
// corpus player-selection values (Random, ChooseFromPlayerToTheirRight,
// NextPlayerInChosenDirection, ChooseNextPlayerInChosenDirection) are
// implemented and covered by gain_control_variant_players_test.go; this test
// holds the UNKNOWN-value boundary those must not widen.
//
// Driven through Resolve rather than the handler directly, so reverting the
// REGISTRATION (not just the hunk) also fails this test: an unregistered API
// emits the generic "unimplemented API GainControlVariant" Note instead of
// the fail-closed one asserted below.
func TestGainControlVariantRejectsUnsupportedChangeController(t *testing.T) {
	for _, value := range []string{"Bogus", "RandomX", "CardOwner2", "NextPlayer"} {
		g, ids := gainControlVariantBoard(t)
		h := &fakeHost{g: g}
		variant := sa(t, "SP$ GainControlVariant | AllValid$ Creature | ChangeController$ "+value)
		Resolve(h, &Ctx{Controller: 0, Source: ids["bear"]}, variant)

		for _, e := range h.log {
			if e.Kind == events.ControlChange {
				t.Fatalf("ChangeController$ %s still moved control: %+v", value, e)
			}
		}
		if got := g.Obj(ids["stolen"]).Controller; got != 0 {
			t.Fatalf("ChangeController$ %s changed controller to %d, want 0", value, got)
		}
		if len(h.log) != 1 || h.log[0].Kind != events.Note || len(h.controls) != 0 {
			t.Fatalf("ChangeController$ %s: expected one Note and no grant, got log=%+v grants=%+v", value, h.log, h.controls)
		}
		if want := "GainControlVariant ChangeController$ " + value + " unimplemented"; h.log[0].Text != want {
			t.Fatalf("ChangeController$ %s note = %q, want %q (registration reverted?)", value, h.log[0].Text, want)
		}
	}
}
