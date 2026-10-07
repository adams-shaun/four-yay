package effects

import (
	"reflect"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestRepeatTypesFromFindsDistinctTypesOnImprintedLibraryCards pins the
// RepeatTypesFrom source population: repeated card types are visited once,
// and only cards matching the source's IsImprinted association contribute.
func TestRepeatTypesFromFindsDistinctTypesOnImprintedLibraryCards(t *testing.T) {
	h, src, lib := riderBoard(t,
		"Name:Bear\nTypes:Artifact Creature Bear\nPT:2/2\nOracle:x\n",
		riderLand)
	h.Emit(events.Event{Kind: events.Imprint, Obj: src, IDs: append([]state.ObjID(nil), lib...), Text: "seek-found"})
	got, ok := repeatEachTypesFrom(h, &Ctx{Source: src, Controller: 0}, "ValidLibrary Card.IsImprinted")
	if !ok {
		t.Fatal("RepeatTypesFrom selector unexpectedly unsupported")
	}
	want := []string{"Artifact", "Creature", "Land"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RepeatTypesFrom types = %v, want %v", got, want)
	}
}

// TestRepeatTypesFromHonorsZonePrefix pins the leading Valid<Zone> token:
// the scan reads the zone the token names, not always the library. A creature
// in the GRAVEYARD (not the library) supplies its type under
// `ValidGraveyard`, and a bare `Valid` reads the battlefield.
func TestRepeatTypesFromHonorsZonePrefix(t *testing.T) {
	h, src, _ := riderBoard(t)
	gyBear := h.g.AddObject(mkCard(t, riderBear), 0).ID
	h.g.SetZone(state.ZGraveyard, 0, []state.ObjID{gyBear})
	bfSorcery := h.g.AddObject(mkCard(t, "Name:Ritual\nTypes:Sorcery\nOracle:x\n"), 0).ID
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{src, bfSorcery})

	gyTypes, ok := repeatEachTypesFrom(h, &Ctx{Source: src, Controller: 0}, "ValidGraveyard Card")
	if !ok || !reflect.DeepEqual(gyTypes, []string{"Creature"}) {
		t.Fatalf("ValidGraveyard types = %v (ok=%v), want [Creature]", gyTypes, ok)
	}
	bfTypes, ok := repeatEachTypesFrom(h, &Ctx{Source: src, Controller: 0}, "Valid Card")
	if !ok || !reflect.DeepEqual(bfTypes, []string{"Artifact", "Sorcery"}) {
		t.Fatalf("Valid types = %v (ok=%v), want [Artifact Sorcery]", bfTypes, ok)
	}
}

// TestRepeatTypesFromUnknownZoneFailsLoud pins that a selector whose leading
// token is not a known zone (Hurkyl, Master Wizard's ThisTurnCast_ cast
// history) is refused, so the caller emits its loud unimplemented Note rather
// than silently producing no types.
func TestRepeatTypesFromUnknownZoneFailsLoud(t *testing.T) {
	if _, _, ok := splitTypesFromSelector("ThisTurnCast_Card.nonCreature+YouCtrl Remembered"); ok {
		t.Fatal("cast-history selector reported as a supported zone")
	}
	if _, _, ok := splitTypesFromSelector("Card.IsImprinted"); ok {
		t.Fatal("selector with no Valid<Zone> token reported as supported")
	}
}

// TestRepeatEachBindsEachDistinctTypeToTheBody drives the whole dispatch: a
// RepeatEach with RepeatTypesFrom iterates once per distinct type, binds the
// type on the resolving source (the event fold's ChosenType), and runs the
// body once per type. Portent of Calamity's ChooseCard reads Card.ChosenType
// from exactly this binding.
func TestRepeatEachBindsEachDistinctTypeToTheBody(t *testing.T) {
	h, src, lib := riderBoard(t,
		"Name:Bear\nTypes:Artifact Creature Bear\nPT:2/2\nOracle:x\n",
		riderLand)
	h.Emit(events.Event{Kind: events.Imprint, Obj: src, IDs: append([]state.ObjID(nil), lib...), Text: "seek-found"})
	sa := &cards.SA{API: "RepeatEach", Params: map[string]string{
		"RepeatTypesFrom":  "ValidLibrary Card.IsImprinted",
		"RepeatSubAbility": "Sub",
	}}
	c := &Ctx{Source: src, Controller: 0, SVars: map[string]string{
		"Sub": "DB$ GainLife | Defined$ You | LifeAmount$ 1",
	}}
	effRepeatEach(h, c, sa)

	var bound []string
	bodies := 0
	for _, e := range h.log {
		switch {
		case e.Kind == events.Choose && e.Obj == src && e.Counter == "type":
			bound = append(bound, e.Text)
		case e.Kind == events.LifeChange && e.Amount > 0:
			bodies++
		}
	}
	// Precondition: the two imprinted cards genuinely contribute three
	// distinct types, so "three iterations" cannot pass on an empty scan.
	if want := []string{"Artifact", "Creature", "Land"}; !reflect.DeepEqual(bound, want) {
		t.Fatalf("types bound = %v, want %v", bound, want)
	}
	if bodies != 3 {
		t.Fatalf("body ran %d times, want one per distinct type (3); log=%+v", bodies, h.log)
	}
}

// repeatTypesFromCarriers pins every corpus RepeatTypesFrom$ carrier. A new
// script using the selector is a NEW carrier whose zone token and body shape
// must be handled (or refused loudly), so it fails here rather than silently
// regressing.
var repeatTypesFromCarriers = []string{
	"Atraxa, Grand Unifier",
	"Grime Gorger",
	"Hurkyl, Master Wizard",
	"Only I Know What Awaits",
	"Portent of Calamity",
}

// TestRepeatTypesFromCensus walks every corpus RepeatEach SA and pins the set
// of carriers that use RepeatTypesFrom$. Portent of Calamity and Atraxa read
// the library's imprinted cards; Grime Gorger the graveyard; Only I Know What
// Awaits the battlefield; Hurkyl a cast history (refused loudly).
func TestRepeatTypesFromCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	got := map[string]bool{}
	visit := func(name string, sa *cards.SA) {
		if !isRepeatEachSA(sa) {
			return
		}
		if RepeatEachOf(sa).TypesFrom != "" {
			got[name] = true
		}
	}
	for _, card := range reg.AllCards() {
		for _, f := range card.Faces {
			for _, sa := range f.Abilities {
				visit(f.Name, sa)
			}
			names := make([]string, 0, len(f.SVars))
			for n := range f.SVars {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				visit(f.Name, cards.ResolveSVar(f.SVars, n))
			}
		}
	}
	want := map[string]bool{}
	for _, n := range repeatTypesFromCarriers {
		want[n] = true
	}
	var added, removed []string
	for n := range got {
		if !want[n] {
			added = append(added, n)
		}
	}
	for n := range want {
		if !got[n] {
			removed = append(removed, n)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	if len(added) > 0 {
		t.Errorf("new RepeatTypesFrom$ carriers: %v", added)
	}
	if len(removed) > 0 {
		t.Errorf("pinned RepeatTypesFrom$ carriers gone (stale entries): %v", removed)
	}
	t.Logf("%d RepeatTypesFrom carriers", len(got))
}
