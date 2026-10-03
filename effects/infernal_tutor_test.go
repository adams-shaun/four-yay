package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// infernalTutorCorpus returns the REAL compiled corpus card Infernal Tutor
// (.cards/cardsfolder/i/infernal_tutor.txt), failing the test when the corpus
// pin moved. Its main SA is
//
//	SP$ Reveal | RememberRevealed$ True | Defined$ You | SubAbility$ DBChangeZone
//	SVar:DBChangeZone:DB$ ChangeZone | Origin$ Library | Destination$ Hand
//	    | ChangeType$ Remembered.sameName | ChangeNum$ 1 ...
//	SVar:DBChangeZone2:DB$ ChangeZone | ... ChangeType$ Card ...  (hellbent)
//
// so the reveal's Remembered capture is exactly what the chained search
// reads. The test drives the real script, never a synthetic fixture.
func infernalTutorCorpus(t *testing.T) (*cards.Card, *cards.SA) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Infernal Tutor")
	if !ok {
		t.Skip("corpus missing Infernal Tutor")
	}
	if len(c.Faces) == 0 {
		t.Fatal("corpus card Infernal Tutor has no face")
	}
	face := c.Faces[0]
	var main *cards.SA
	for _, a := range face.Abilities {
		if a.API == "Reveal" {
			main = a
			break
		}
	}
	if main == nil {
		t.Fatal("corpus pin moved: Infernal Tutor carries no SP$ Reveal")
	}
	if main.Sub == nil {
		t.Fatal("corpus card's Reveal SA is not linked to its SubAbility chain")
	}
	if main.Sub.Params["ChangeType"] != "Remembered.sameName" {
		t.Fatalf("corpus pin moved: chained search ChangeType$ = %q, want Remembered.sameName", main.Sub.Params["ChangeType"])
	}
	return c, main
}

// infernalTutorBoard builds a 2-seat game whose seat 0 holds a two-card hand
// (handOrder[0] first) and a library holding libOrder (in order), and returns
// the host, the ctx (with the card's real SVar table bound) and the ids keyed
// by name. Cards are inline fixtures; the behaviour under test is the REAL
// Infernal Tutor script, which matches them by name.
func infernalTutorBoard(t *testing.T, handOrder, libOrder []string) (*fakeHost, *Ctx, map[string]state.ObjID) {
	t.Helper()
	h := newHost(t, 2)
	ids := map[string]state.ObjID{}
	add := func(name string, zone state.Zone, owner state.PlayerID) state.ObjID {
		o := h.g.AddObject(mkCard(t, "Name:"+name+"\nTypes:Sorcery\nOracle:x\n"), owner)
		o.Zone = zone
		ids[name] = o.ID
		return o.ID
	}
	var hand []state.ObjID
	for _, n := range handOrder {
		if id, ok := ids[n]; ok {
			hand = append(hand, id)
			continue
		}
		hand = append(hand, add(n, state.ZHand, 0))
	}
	h.g.SetZone(state.ZHand, 0, hand)
	var lib []state.ObjID
	for _, n := range libOrder {
		// A library card may share a name with a hand card; both objects are
		// distinct, so key them by a library-qualified name for the caller.
		id := h.g.AddObject(mkCard(t, "Name:"+n+"\nTypes:Sorcery\nOracle:x\n"), 0).ID
		h.g.Obj(id).Zone = state.ZLibrary
		lib = append(lib, id)
		ids["lib:"+n] = id
	}
	h.g.SetZone(state.ZLibrary, 0, lib)
	src := h.g.AddObject(mkCard(t, "Name:Infernal Tutor\nManaCost:1 B\nTypes:Sorcery\nOracle:x\n"), 0)
	ctx := &Ctx{Source: src.ID, Controller: 0}
	return h, ctx, ids
}

// revealPickNotes returns the single public reveal Note carrying ids.
func revealPickNotes(t *testing.T, log []events.Event) *events.Event {
	t.Helper()
	var note *events.Event
	for i := range log {
		if log[i].Kind == events.Note && !log[i].Secret && len(log[i].IDs) > 0 {
			if note != nil {
				t.Fatalf("more than one reveal Note: %+v", log)
			}
			note = &log[i]
		}
	}
	return note
}

// TestInfernalTutorHellbentEmptyHandSearchesAnyCard pins the hellbent leg: an
// empty hand has no eligible card, so the reveal poses NO pick and captures
// nothing, and the card's own SVar gate routes to DBChangeZone2 (the any-card
// search). The failure mode this guards is a regression where the empty-hand
// reveal wedges on a pick ask over zero options.
func TestInfernalTutorHellbentEmptyHandSearchesAnyCard(t *testing.T) {
	card, main := infernalTutorCorpus(t)
	h, ctx, ids := infernalTutorBoard(t, nil, []string{"Lightning Bolt"})
	h.g.SetZone(state.ZHand, 0, nil)
	SetSVars(ctx, card.Faces[0].SVars)
	sh := &suspendHost{fakeHost: *h}

	Resolve(sh, ctx, main)

	// An empty hand has no card to reveal: no reveal_pick ask of any kind.
	if sh.asked != nil && sh.asked.ResumeKind == "reveal_pick" {
		t.Fatalf("an empty hand posed a reveal pick: %+v", sh.asked)
	}
	if len(ctx.Remembered) != 0 {
		t.Fatalf("Remembered = %+v, want nothing revealed from an empty hand", ctx.Remembered)
	}
	// The hellbent branch (ConditionSVarCompare$ LT1) runs the any-card
	// search; its hidden-library pick offers whatever the library holds (no
	// same-name restriction).
	if sh.asked == nil || sh.asked.ResumeKind != "search" {
		t.Fatalf("hellbent search did not ask: %+v", sh.asked)
	}
	if len(sh.asked.Options) != 1 || sh.asked.Options[0].Obj != ids["lib:Lightning Bolt"] {
		t.Fatalf("hellbent search offered %+v, want the any-card library pick", sh.asked.Options)
	}
}
