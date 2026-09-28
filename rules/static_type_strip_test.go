package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestDarksteelMutationStripsCardAndCreatureTypes pins both S:-static strip
// flags against the real carrier card: Darksteel Mutation attached to Grizzly
// Bears leaves the Bear an Insect artifact creature -- the printed Creature
// card type and Bear subtype stripped (both flags), the static's AddType$
// landing on the stripped base, the 0/1 base P/T and the Indestructible
// keyword from the same static line.
func TestDarksteelMutationStripsCardAndCreatureTypes(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Darksteel Mutation"), lookup(t, reg, "Grizzly Bears")}, []*cards.Card{})
	bear := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	if bear == 0 {
		t.Fatal("Grizzly Bears not placed")
	}
	mutation := moveByName(t, e, 0, "Darksteel Mutation", state.ZBattlefield)
	if mutation == 0 {
		t.Fatal("Darksteel Mutation not placed")
	}
	e.emit(events.Event{Kind: events.Attach, Obj: mutation, IDs: []state.ObjID{bear}})

	d := e.Derived(bear)
	if !slices.Equal(d.Types, []string{"Artifact", "Creature", "Insect"}) {
		t.Fatalf("Darksteel Mutation types = %v, want [Artifact Creature Insect] (printed Creature/Bear stripped)", d.Types)
	}
	if d.Power != 0 || d.Toughness != 1 {
		t.Fatalf("Darksteel Mutation P/T = %d/%d, want 0/1", d.Power, d.Toughness)
	}
	if !slices.Contains(d.Keywords, "Indestructible") {
		t.Fatalf("Darksteel Mutation keywords = %v, want Indestructible present", d.Keywords)
	}
	replayCheck(t, e, cfg)
}

// TestContinuousRemoveCardTypesKeepsOnlySupertypes is the strip's boundary,
// the mirror of TestRemoveCreatureTypesStripsOnlySubtypes: a
// RemoveCardTypes-only layer effect takes every card type AND every subtype
// and keeps only the supertypes, with the effect's own AddTypes landing after
// the strip.
func TestContinuousRemoveCardTypesKeepsOnlySupertypes(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Lyra Dawnbringer")}, []*cards.Card{})
	id := moveByName(t, e, 0, "Lyra Dawnbringer", state.ZBattlefield)
	e.AddContinuous(state.ContinuousEffect{Source: id, Affects: "Card.Self", Layer: state.LType,
		RemoveCardTypes: true, AddTypes: []string{"Insect"}})
	d := e.Derived(id)
	if !slices.Equal(d.Types, []string{"Legendary", "Insect"}) {
		t.Fatalf("types after RemoveCardTypes strip+add = %v, want [Legendary Insect] (Creature card type and Angel subtype stripped, Legendary survives)", d.Types)
	}
}

// TestAbilityRemovalTimestampStillDominates pins the layer-6 tie-break's
// boundary: a full tie (same timestamp) applies the removal BEFORE the grant
// (the Darksteel Mutation same-line shape), but a genuinely LATER removal
// still wipes an earlier grant, and an EARLIER removal does not touch a
// later one -- CR 613.1f timestamp order otherwise.
func TestAbilityRemovalTimestampStillDominates(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	build := func(grantTS, removalTS uint32) (*Engine, state.ObjID) {
		e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Grizzly Bears")}, []*cards.Card{})
		id := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
		e.AddContinuous(state.ContinuousEffect{Source: id, Affects: "Card.Self", Layer: state.LAbilities,
			Timestamp: grantTS, AddKeywords: []string{"Indestructible"}})
		e.AddContinuous(state.ContinuousEffect{Source: id, Affects: "Card.Self", Layer: state.LAbilities,
			Timestamp: removalTS, RemoveAbilities: true})
		return e, id
	}
	eLate, idLate := build(1, 2)
	if got := eLate.Derived(idLate).Keywords; len(got) != 0 {
		t.Fatalf("later removal: keywords = %v, want empty (a later removal still wipes an earlier grant)", got)
	}
	e, id := build(2, 1)
	if kw := e.Derived(id).Keywords; !slices.Equal(kw, []string{"Indestructible"}) {
		t.Fatalf("earlier removal: keywords = %v, want [Indestructible] (an earlier removal never touches a later grant)", kw)
	}
}
