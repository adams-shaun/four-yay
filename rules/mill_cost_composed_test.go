package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file is the regression suite for the Mill<N> cost component
// (task agent-20260918T230554Z-2b1a0e21). Ordinary activated abilities and
// spells must settle their parsed Mill cost parts as real zone moves.

const composedMillManaScript = "Name:Composed Mill Rock\nManaCost:0\nTypes:Artifact\n" +
	"A:AB$ Mana | Cost$ T Mill<1> Mill<1> | Produced$ C | SpellDescription$ Add {C}.\n" +
	"Oracle:x\n"

// millFodderCard is the filler card smallLibraryEngine stocks the library with.
func millFodderCard(t testing.TB) *cards.Card {
	t.Helper()
	return card(t, "Name:Mill Fodder\nTypes:Sorcery\nOracle:x\n")
}

// smallLibraryEngine is handEngine with the seat-0 library replaced by n
// anonymous cards, so a Mill requirement can be made exactly short or exactly
// met.
func smallLibraryEngine(t *testing.T, n int, hand ...*cards.Card) *Engine {
	t.Helper()
	e := handEngine(t, hand...)
	var ids []state.ObjID
	for i := 0; i < n; i++ {
		o := e.G.AddObject(millFodderCard(t), 0)
		ids = append(ids, o.ID)
	}
	e.G.SetZone(state.ZLibrary, 0, ids)
	return e
}

// TestComposedMillManaAbilityMillsAvailableLibrary pins CR 701.13a's
// partial-library rule: a Mill<1> Mill<1> cost with one card mills that
// card, then completes the remainder of the activation cost.
func TestComposedMillManaAbilityMillsAvailableLibrary(t *testing.T) {
	t.Parallel()
	e := smallLibraryEngine(t, 1, card(t, composedMillManaScript))
	src := onBoard(t, e, 0, composedMillManaScript)
	o := e.G.Obj(src)
	if o == nil || len(o.Face().Abilities) == 0 {
		t.Fatal("precondition: composed Mill rock has no ability")
	}
	ma := o.Face().Abilities[0]
	parsed := ParseCost(ma.Params["Cost"])
	if len(parsed.Mill) != 2 || parsed.Mill[0].N != 1 || parsed.Mill[1].N != 1 {
		t.Fatalf("precondition: script did not parse as two Mill<1> parts: %+v", parsed.Mill)
	}
	lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	if len(lib) != 1 {
		t.Fatalf("precondition: library has %d cards, want 1", len(lib))
	}
	if !e.manaAbilityPayable(0, src, ma) {
		t.Fatal("a Mill<1> Mill<1> mana ability must be payable with one library card")
	}

	e.resolveManaAbility(0, src, ma, false)
	if e.G.Obj(lib[0]).Zone != state.ZGraveyard {
		t.Fatalf("partial mill did not move the library card: zone=%s", e.G.Obj(lib[0]).Zone)
	}
	if e.G.Players[0].Pool[state.MC] != 1 || !e.G.Obj(src).Tapped {
		t.Fatalf("partial Mill cost did not complete: pool=%+v tapped=%v", e.G.Players[0].Pool, e.G.Obj(src).Tapped)
	}
}

// TestSpellMillCostMillsOnPayment covers the other ordinary payment branch the
// r1 review named (a spell, not an ability): casting a spell whose additional
// Cost$ carries Mill<1> really mills the top card as its cost.
func TestSpellMillCostMillsOnPayment(t *testing.T) {
	t.Parallel()
	e := smallLibraryEngine(t, 2, card(t, "Name:Mill Spell\nManaCost:0\nTypes:Sorcery\nA:SP$ Draw | Cost$ Mill<1> | NumCards$ 1 | SpellDescription$ Draw a card.\nOracle:x\n"))
	id := e.G.Zone(state.ZHand, 0)[0]
	lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	if len(lib) != 2 {
		t.Fatalf("precondition: library has %d cards, want 2", len(lib))
	}
	castMode(t, e, id, "")
	finishCast(t, e, id)
	if e.G.Obj(lib[0]).Zone != state.ZGraveyard {
		t.Fatalf("spell Mill<1> cost did not mill the top library card: zone=%s", e.G.Obj(lib[0]).Zone)
	}
}

// TestOrdinaryAbilityMillCostMillsShortLibrary pins CR 701.13a on a real
// corpus carrier: Rot Farm Skeleton's "{2}{B}{G}, Mill four cards" ability
// remains payable with three library cards and mills every available card.
func TestOrdinaryAbilityMillCostMillsShortLibrary(t *testing.T) {
	t.Parallel()
	skeleton, ok := testutil.CorpusRegistry(t).Lookup("Rot Farm Skeleton")
	if !ok {
		t.Fatal("corpus missing Rot Farm Skeleton")
	}
	e := smallLibraryEngine(t, 3, skeleton)
	id := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZGraveyard})
	if e.G.Obj(id).Zone != state.ZGraveyard {
		t.Fatalf("precondition: Rot Farm Skeleton is in %s, want graveyard", e.G.Obj(id).Zone)
	}
	// Fund the {2}{B}{G} so payability turns only on the mill requirement.
	e.G.Players[0].Pool[state.MC] = 2
	e.G.Players[0].Pool[state.MB] = 1
	e.G.Players[0].Pool[state.MG] = 1
	lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	e.priorityRound()
	opt, offered := findAbilityOption(e, id, 0)
	if !offered {
		t.Fatalf("Rot Farm Skeleton must be offered with 3 library cards: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	finishCast(t, e, id)
	for _, cardID := range lib {
		if e.G.Obj(cardID).Zone != state.ZGraveyard {
			t.Fatalf("short-library card %v not milled to the graveyard: zone=%s", cardID, e.G.Obj(cardID).Zone)
		}
	}
	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("Rot Farm Skeleton did not resolve back to the battlefield: zone=%s", e.G.Obj(id).Zone)
	}
}

// TestOrdinaryAbilityMillCostMillsBeforeResolving pins finding 2's payment
// side: activating Rot Farm Skeleton's ability really mills its four library
// cards as part of paying the cost.
func TestOrdinaryAbilityMillCostMillsBeforeResolving(t *testing.T) {
	t.Parallel()
	skeleton, ok := testutil.CorpusRegistry(t).Lookup("Rot Farm Skeleton")
	if !ok {
		t.Fatal("corpus missing Rot Farm Skeleton")
	}
	e := smallLibraryEngine(t, 4, skeleton)
	id := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZGraveyard})
	lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	if len(lib) != 4 {
		t.Fatalf("precondition: library has %d cards, want 4", len(lib))
	}
	e.G.Players[0].Pool[state.MC] = 2
	e.G.Players[0].Pool[state.MB] = 1
	e.G.Players[0].Pool[state.MG] = 1
	e.priorityRound()
	opt, offered := findAbilityOption(e, id, 0)
	if !offered {
		t.Fatalf("Rot Farm Skeleton not offered with 4 library cards: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	finishCast(t, e, id)
	for _, cardID := range lib {
		if e.G.Obj(cardID).Zone != state.ZGraveyard {
			t.Fatalf("library card %v not milled to the graveyard: zone=%s", cardID, e.G.Obj(cardID).Zone)
		}
	}
	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("Rot Farm Skeleton did not resolve back to the battlefield: zone=%s", e.G.Obj(id).Zone)
	}
}
