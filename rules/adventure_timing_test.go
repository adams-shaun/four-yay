package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func adventureBonecrusherEngine(t *testing.T, reg *cards.Registry) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	front := searchCorpusCard(t, reg, "Bonecrusher Giant")
	island := searchCorpusCard(t, reg, "Island")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := []*cards.Card{front}
	for len(deck) < 40 {
		deck = append(deck, island)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = bear
	}
	cfg := seatZeroStart(Config{Seed: 7401, Names: []string{"adventure", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	oppBear := e.G.Zone(state.ZLibrary, 1)[0]
	id := searchMoveByName(t, e, "Bonecrusher Giant", state.ZHand)
	return e, cfg, id, oppBear
}

// TestAdventureInstantSpellFaceOfferedAtInstantTiming pins CR 714.3a: the
// Adventure face gets its own timing check before the creature front can
// suppress the card's hand offers.
func TestAdventureInstantSpellFaceOfferedAtInstantTiming(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, id, oppBear := adventureBonecrusherEngine(t, reg)
	front := e.G.Obj(id).Face()
	if front == nil || !front.IsCreature() || front.IsInstant() || e.HasKeyword(id, "Flash") {
		t.Fatalf("front timing precondition failed: %+v, flash=%v", front, e.HasKeyword(id, "Flash"))
	}
	altFace := adventureSpellFace(e.G.Obj(id))
	if altFace == nil || !altFace.IsInstant() {
		t.Fatalf("Adventure timing precondition failed: %+v", altFace)
	}
	if front.ManaCost == altFace.ManaCost {
		t.Fatalf("front and Adventure costs unexpectedly equal: %q", front.ManaCost)
	}
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepBeginCombat)
	e.emit(events.Event{Kind: events.MoveZone, Obj: oppBear, From: state.ZLibrary, To: state.ZBattlefield})
	e.pending = nil
	e.priorityRound()
	for _, r := range "RRR" {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: string(r), Amount: 1})
	}
	e.priorityRound()

	if e.G.Step != state.StepBeginCombat {
		t.Fatalf("step = %s, want Begin Combat", e.G.Step)
	}
	if bear := e.G.Obj(oppBear); bear == nil || bear.Zone != state.ZBattlefield {
		t.Fatalf("opposing bear precondition failed: %+v", bear)
	}
	if got := e.G.Obj(id); got == nil || got.Zone != state.ZHand {
		t.Fatalf("Bonecrusher precondition failed: %+v", got)
	}
	if plain := adventureOption(t, e, id, ""); plain != nil {
		t.Fatalf("creature front offered at instant timing: %+v", plain)
	}
	alt := adventureOption(t, e, id, "adventure_alt")
	if alt == nil || alt.Label != "Cast Stomp" {
		t.Fatalf("instant Adventure face not offered: %+v", castOptions(t, e))
	}
	replayCheck(t, e, cfg)
}
