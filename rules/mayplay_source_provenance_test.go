package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestMayPlaySourceCastProvenance(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	glimpse, ok := reg.Lookup("Glimpse the Cosmos")
	if !ok {
		t.Fatal("Glimpse the Cosmos missing from corpus")
	}
	if len(glimpse.Faces) == 0 {
		t.Fatal("Glimpse the Cosmos has no face")
	}
	face := glimpse.Faces[0]
	if len(face.Repls) == 0 || face.Repls[0].Params["ValidLKI"] != "Card.CastSa Spell.MayPlaySource" {
		t.Fatalf("Glimpse the Cosmos replacement does not carry the expected ValidLKI: %+v", face)
	}

	e, _, _ := newFixtureDeck(t, 20260923, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	spell := e.G.AddObject(glimpse, 0)
	spell.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 0, []state.ObjID{spell.ID})
	e.emit(events.Event{Kind: events.PutOnStack, Obj: spell.ID, From: state.ZGraveyard, To: state.ZStack, Player: 0})
	e.emit(events.Event{Kind: events.CastInfo, Obj: spell.ID, Counter: events.FlagsString(state.FlagMayPlay)})
	if spell.Zone != state.ZStack || spell.CastFlags&state.FlagMayPlay == 0 {
		t.Fatalf("precondition: spell zone=%v cast flags=%#x; want stack and may-play permission", spell.Zone, spell.CastFlags)
	}
	if spell.CastFlags&state.FlagMayhem != 0 {
		t.Fatal("precondition: may-play permission and mayhem provenance must differ")
	}

	validLKI := face.Repls[0].Params["ValidLKI"]
	admitted, matches := e.castSaAdmits(validLKI, spell.ID)
	if !matches || admitted == "" || strings.Contains(admitted, "MayPlaySource") {
		t.Fatalf("paid may-play cast was not resolved by CastSa: admitted=%q matches=%v", admitted, matches)
	}
	if _, matches = e.castSaAdmits(validLKI, 0); matches {
		t.Fatal("missing cast object unexpectedly satisfied MayPlaySource")
	}
	// The layer Affected$ path uses the same CastSa splitter before the normal
	// filter; this asserts that the parsed card-level predicate survives it.
	layerSpec, matches := e.castProvenanceAdmits("Card.CastSa Spell.MayPlaySource", spell.ID, 0)
	if !matches || layerSpec == "" || strings.Contains(layerSpec, "MayPlaySource") {
		t.Fatalf("layer provenance rejected paid may-play cast: %q, %v", layerSpec, matches)
	}
	ce := ContinuousEffect{Source: spell.ID, Controller: 0,
		Affects: "Card.Self+CastSa Spell.MayPlaySource"}
	if !e.matchesWithTypes(&ce, spell.ID, nil, state.ZStack) {
		t.Fatal("Affected$ layer match rejected the paid may-play cast")
	}

	counted := e.spellsCastThisTurnMatching(0, "Card.CastSa Spell.MayPlaySource", 0)
	if len(counted) != 1 || counted[0] != spell.ID {
		t.Fatalf("Count$ThisTurnCast_ matches = %v, want this paid may-play cast %d", counted, spell.ID)
	}

	move := events.Event{Kind: events.MoveZone, Obj: spell.ID, From: state.ZStack, To: state.ZGraveyard}
	repl := cards.Repl{Event: "Moved", Params: map[string]string{
		"Origin": "Stack", "Destination": "Graveyard", "ValidLKI": validLKI,
	}}
	if !e.replacementMatches(repl, spell.ID, move) {
		t.Fatal("Glimpse the Cosmos ValidLKI replacement did not admit its paid may-play cast")
	}
	e.emit(events.Event{Kind: events.CastInfo, Obj: spell.ID})
	if spell.CastFlags&state.FlagMayPlay != 0 {
		t.Fatal("negative-case precondition: CastInfo did not clear MayPlaySource")
	}
	if e.replacementMatches(repl, spell.ID, move) {
		t.Fatal("Glimpse the Cosmos replacement admitted a cast without MayPlaySource provenance")
	}
}
