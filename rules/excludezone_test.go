package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// ExcludeZone$ on a Continuous static (task agent-20260919T192133Z): the
// source-zone exclusion a static's ExcludeZone$ names is read at the ONE
// gate every characteristic emission passes through (staticEffects, which
// walks every staticSourceZones zone) and at the layer-7a CDA read
// (cdaPTStatic, via the same staticZoneAdmits helper). Grist, the Hunger
// Tide is the corpus's ONLY carrier (measured, 1 file):
//
//	S:Mode$ Continuous | Affected$ Card.Self | ExcludeZone$ Battlefield |
//	SetPower$ 1 | SetToughness$ 1 | AddType$ Creature & Insect |
//	CharacteristicDefining$ True
//
// -- "As long as Grist isn't on the battlefield, it's a 1/1 Insect creature
// in addition to its other types." Before the gate the static was admitted
// by the EffectZone$ battlefield default ONLY on the battlefield -- the
// exact inverse of the card: there the planeswalker wrongly layered a 1/1
// Insect creature body on top of its loyalty body, and in hand/graveyard it
// kept nothing. All tests run on the real compiled corpus card; no Forge
// script text is committed.

// TestGristOnBattlefieldKeepsItsPlaneswalkerBody: on the battlefield the
// ExcludeZone$ kills the static whole -- the derived type list is exactly
// the printed one (Legendary Planeswalker Grist, no Creature/Insect) and
// the planeswalker carries no creature P/T, only its printed starting
// loyalty (CR 306.5b).
func TestGristOnBattlefieldKeepsItsPlaneswalkerBody(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Grist, the Hunger Tide")}, nil)
	id := moveByName(t, e, 0, "Grist, the Hunger Tide", state.ZBattlefield)
	o := e.G.Obj(id)
	// Precondition: a real planeswalker entry with its printed loyalty, and
	// the card under test really is the ExcludeZone$ carrier.
	if o.Counter("LOYALTY") != 3 {
		t.Fatalf("Grist's loyalty = %d, want 3 (printed starting loyalty)", o.Counter("LOYALTY"))
	}
	carrier := false
	for _, st := range o.Face().Statics {
		if st.Params["ExcludeZone"] == "Battlefield" && st.Params["CharacteristicDefining"] != "" {
			carrier = true
		}
	}
	if !carrier {
		t.Fatal("Grist's face carries no ExcludeZone$ CDA static -- the fixture is not the card under test")
	}
	types := e.typeCharacteristics(id, 0)
	if slices.Contains(types, "Creature") || slices.Contains(types, "Insect") {
		t.Fatalf("Grist on the battlefield derived types = %v, must not gain Creature/Insect", types)
	}
	if e.Power(id) != 0 || e.Toughness(id) != 0 {
		t.Fatalf("Grist on the battlefield = %d/%d, want the planeswalker's no-P/T body", e.Power(id), e.Toughness(id))
	}
}

// TestGristOffBattlefieldIsAOneOneInsectCreature: in hand and graveyard the
// static is live -- the CDA type claim makes it an Insect creature in
// addition to its other types (CR 604.3's every-zone reading, minus the
// excluded battlefield), and cdaSetPT's layer-7a claim sets the 1/1 base.
func TestGristOffBattlefieldIsAOneOneInsectCreature(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Grist, the Hunger Tide")}, nil)
	id := cdaMoveTo(t, e, 0, "Grist, the Hunger Tide", state.ZHand)
	for _, tc := range []struct {
		where string
		from  state.Zone
		to    state.Zone
	}{
		{"hand", 0, 0},
		{"graveyard", state.ZHand, state.ZGraveyard},
	} {
		if tc.from != 0 {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: tc.from, To: tc.to})
			e.pending = nil
		}
		types := e.typeCharacteristics(id, 0)
		if !slices.Contains(types, "Creature") || !slices.Contains(types, "Insect") {
			t.Fatalf("Grist in the %s derived types = %v, want Creature and Insect", tc.where, types)
		}
		if e.Power(id) != 1 || e.Toughness(id) != 1 {
			t.Fatalf("Grist in the %s = %d/%d, want 1/1", tc.where, e.Power(id), e.Toughness(id))
		}
	}
}

// TestGristStaticIsLiveOnTheStackToo: while the Grist card is ON THE STACK
// the exclusion does not reach it ("isn't on the battlefield"), so the
// spell is an Insect creature spell there.
func TestGristStaticIsLiveOnTheStackToo(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Grist, the Hunger Tide")}, nil)
	id := cdaMoveTo(t, e, 0, "Grist, the Hunger Tide", state.ZHand)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZStack})
	e.pending = nil
	types := e.typeCharacteristics(id, 0)
	if !slices.Contains(types, "Creature") || !slices.Contains(types, "Insect") {
		t.Fatalf("Grist on the stack derived types = %v, want Creature and Insect", types)
	}
}
