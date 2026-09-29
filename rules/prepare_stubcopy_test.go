package rules

// CR 722.3c prepared-copy test for a CopyFaceFrom stub back (ticket
// agent-20260928T215303Z-ab37989c).
//
// Cheerful Osteomancer // Raise Dead is one of the 21 AlternateMode:Prepare
// cards whose inset spell is written only as `CopyFaceFrom:Raise Dead`. Before
// the parser resolved that directive the CR 722.3c exile copy minted by
// grantPreparedCopy carried an empty face: no name, no cost, no ability, so it
// either had no cast option or resolved as a do-nothing "Cast  (prepared)".
// This is the stub-back sibling of
// TestSetAudit_sos_EliteInterceptor_PreparedSpellCopyUnprepares (inline back).

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestPrepareStubBackPreparedCopyCastable proves the resolved stub back is a
// real spell end to end: the permanent enters prepared (its ETB replacement),
// the exile copy carries the referenced spell's name/cost/types, the copy is
// offered a cast option, and casting it unprepares the front permanent. The
// effect itself is asserted too (Raise Dead returns the targeted creature card
// from the graveyard to hand), so an empty stub face could not pass.
func TestPrepareStubBackPreparedCopyCastable(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 917, []string{"Cheerful Osteomancer // Raise Dead"}, []string{sosBearSrc}, nil)

	intl := findAndMoveToHand(t, e, 0, "Cheerful Osteomancer")
	// ManaCost 3 B: four black mana covers the generic three.
	addMana(t, e, 0, "BBBB")
	submitChoices(t, e, castOptionFor(t, e, intl).Index)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(intl); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Osteomancer must enter the battlefield, got %+v", o)
	}
	if o := e.G.Obj(intl); o == nil || !o.Prepared {
		t.Fatal("precondition: Osteomancer must enter prepared (its ETB replacement)")
	}
	// Raise Dead needs a legal target: a creature card in the caster's
	// graveyard. Seed it BEFORE reading the copy's cast options -- a targeted
	// spell the engine cannot legally cast is simply not offered.
	bear := addToGraveyard(t, e, 0, sosBearSrc)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: copy has no legal creature-card target in the graveyard, got %+v", o)
	}
	e.priorityRound()

	var copyID state.ObjID
	for _, id := range e.G.Zone(state.ZExile, 0) {
		if o := e.G.Obj(id); o != nil && o.IsCopy && o.Face() != nil && o.Face().Name == "Raise Dead" {
			copyID = id
		}
	}
	if copyID == 0 {
		t.Fatal("prepared copy: no Raise Dead spell copy in exile after Osteomancer enters (CR 722.3c)")
	}
	// The copy must carry the referenced spell's characteristics, not a stub.
	cp := e.G.Obj(copyID)
	if cp.Face().ManaCost != "B" || !cp.Face().IsSorcery() || cp.Face().SpellAbility() == nil {
		t.Fatalf("prepared copy face = %q %q %v ability=%v, want a resolved Raise Dead",
			cp.Face().Name, cp.Face().ManaCost, cp.Face().Types, cp.Face().SpellAbility())
	}
	idx := -1
	for _, opt := range castOptions(t, e) {
		if opt.Obj == copyID {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("prepared copy: exiled Raise Dead copy %d has no cast option", copyID)
	}
	handBefore := len(e.G.Zone(state.ZHand, 0))
	submitChoices(t, e, idx)
	// Casting puts the copy on the stack. The creature must be unprepared as
	// part of the cast (CR 601.2i / 722.3c), before any resolution.
	if o := e.G.Obj(copyID); o == nil || o.Zone != state.ZStack {
		t.Fatalf("prepared copy: after cast choice, copy zone = %v, want stack before resolution", zoneOf(o))
	}
	unpreparedAtCast := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.AlterAttribute && ev.Obj == intl && ev.Text == "Prepared" && ev.Amount < 0 {
			unpreparedAtCast = true
		}
	}
	if !unpreparedAtCast {
		t.Fatal("prepared copy: no removal of Osteomancer's Prepared designation immediately after casting, before resolution")
	}
	sosDrain(t, e, bear, 30)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZHand {
		t.Fatalf("prepared copy: Raise Dead target zone = %v, want hand (the resolved spell must return it)", zoneOf(o))
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Errorf("prepared copy: hand has %d cards, want %d (Raise Dead returns the creature)", got, handBefore+1)
	}
	if o := e.G.Obj(intl); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("prepared copy: Osteomancer left the battlefield unexpectedly")
	}
	replayCheck(t, e, cfg)
}
