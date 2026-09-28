package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Burning-Rune Demon's ETB: search your library for exactly two cards with
// different names, reveal them, then an OPPONENT chooses one of the two to go
// into your hand (the other to your graveyard). The choose is a hidden-library
// ChangeZone holding `Chooser$ ChosenPlayer` and NO `DefinedPlayer$`, so the
// pick's decider is the player chosen by the preceding DBChoosePlayer -- not
// the library's controller. Before Chooser$ ChosenPlayer was read,
// searchChooser fell through to c.Controller, so the caster picked which of
// their own two revealed cards went to hand, defeating the card.
//
// 7 of the 8 corpus `Chooser$ ChosenPlayer` carriers are correct only by
// coincidence (they also carry `DefinedPlayer$ ChosenPlayer`); Burning-Rune
// Demon has no DefinedPlayer$, so it is the shape that exposes the bug.
func TestBurningRuneDemonChosenOpponentPicksTheCard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	demon := mustCorpusCard(t, reg, "Burning-Rune Demon")
	bears := mustCorpusCard(t, reg, "Grizzly Bears")
	giant := mustCorpusCard(t, reg, "Hill Giant")

	e, cfg := tokenReplGameSeats(t, 74, []*cards.Card{demon, bears, giant}, nil)

	// The Demon enters the battlefield from the library, firing its ETB.
	demonID := moveSeededCard(t, e, 0, demon, state.ZBattlefield)
	if o := e.G.Obj(demonID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Burning-Rune Demon precondition: %+v", o)
	}
	e.pending = nil
	e.Advance()

	// The optional ETB trigger (OptionalDecider$ You) sits behind the
	// priority round that resolves it: answer yes.
	trig := passPriorityUntil(t, e, decision.KTriggerOptional)
	submitChoices(t, e, optionIndexOfKind(t, trig, "yes"))

	// First leg: search the controller's library for two differently named
	// cards. Answer with two options whose DifferentNames$ Group differs.
	leg1 := e.Pending()
	if leg1 == nil || leg1.Kind != decision.KChoose || leg1.Player != 0 {
		t.Fatalf("first-leg search ask: %+v", leg1)
	}
	if leg1.Max != 2 {
		t.Fatalf("first-leg search Max = %d, want 2 (ChangeNum$ 2)", leg1.Max)
	}
	a, b, ok := twoDistinctGroupOptions(leg1)
	if !ok {
		t.Fatalf("first-leg search offers no two differently named cards: %+v", leg1.Options)
	}
	// Precondition: both chosen cards really sit in seat 0's library, the
	// zone the search reads.
	for _, id := range []state.ObjID{a.Obj, b.Obj} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZLibrary || o.Controller != 0 {
			t.Fatalf("first-leg chosen card %d precondition: %+v", id, o)
		}
	}
	submitChoices(t, e, a.Index, b.Index)

	// Second: ChoosePlayer. The engine offers the living opponent(s); pick
	// seat 1 and prove that really is the opponent, not the caster.
	choose := e.Pending()
	if choose == nil || choose.Kind != decision.KChoose {
		t.Fatalf("ChoosePlayer ask: %+v", choose)
	}
	oppIdx := -1
	for _, o := range choose.Options {
		if o.Kind == "player" && o.Player == 1 {
			oppIdx = o.Index
		}
		if o.Kind == "player" && o.Player == 0 {
			t.Fatalf("ChoosePlayer ask offers the caster seat 0: %+v", choose.Options)
		}
	}
	if oppIdx < 0 {
		t.Fatalf("ChoosePlayer ask does not offer opponent seat 1: %+v", choose.Options)
	}
	submitChoices(t, e, oppIdx)

	// Third leg: the hidden-library pick. It MUST be posed to the chosen
	// opponent (seat 1), over exactly the two revealed/remembered cards.
	pick := e.Pending()
	if pick == nil || pick.Kind != decision.KChoose {
		t.Fatalf("third-leg pick: %+v", pick)
	}
	if pick.Player != 1 {
		t.Fatalf("third-leg pick posed to player %d, want the chosen opponent 1: %+v", pick.Player, pick)
	}
	if len(pick.Options) != 2 {
		t.Fatalf("third-leg pick offers %d options, want the 2 remembered cards: %+v", len(pick.Options), pick.Options)
	}
	// Precondition: the offered cards are exactly the two chosen in leg 1.
	offered := map[state.ObjID]bool{}
	for _, o := range pick.Options {
		offered[o.Obj] = true
	}
	if !offered[a.Obj] || !offered[b.Obj] {
		t.Fatalf("third-leg pick does not offer the two remembered cards %d/%d: %+v", a.Obj, b.Obj, pick.Options)
	}
	// The opponent chooses card a; it goes to the CASTER's hand (Destination$
	// Hand), the other to the caster's graveyard.
	chosenID, otherID := a.Obj, b.Obj
	idx := indexOfObjOption(pick, chosenID)
	if idx < 0 {
		t.Fatalf("third-leg pick does not offer the card the opponent chose: %+v", pick.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(chosenID); o == nil || o.Zone != state.ZHand || o.Controller != 0 {
		t.Fatalf("opponent-chosen card: %+v, want seat 0's hand", o)
	}
	if o := e.G.Obj(otherID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("un-chosen card: %+v, want seat 0's graveyard", o)
	}
	replayCheck(t, e, cfg)
}

// passPriorityUntil submits pass options (or a pending non-priority decision
// already of the wanted kind) until the wanted kind is pending, failing after
// a bounded number of hand-offs.
func passPriorityUntil(t *testing.T, e *Engine, kind decision.Kind) *decision.Decision {
	t.Helper()
	for i := 0; i < 20; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while waiting for kind %v", kind)
		}
		if d.Kind == kind {
			return d
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("non-priority decision %v while waiting for kind %v", d.Kind, kind)
		}
		submitChoicePass(t, e)
	}
	t.Fatalf("kind %v never became pending", kind)
	return nil
}

// optionIndexOfKind returns the Index of the first option whose Kind matches.
func optionIndexOfKind(t *testing.T, d *decision.Decision, kind string) int {
	t.Helper()
	if d == nil {
		t.Fatal("no decision pending")
	}
	for _, o := range d.Options {
		if o.Kind == kind {
			return o.Index
		}
	}
	t.Fatalf("no option of kind %q in %+v", kind, d.Options)
	return -1
}

// twoDistinctGroupOptions returns two options whose non-empty Group differs --
// the DifferentNames$ answer shape -- or false when the decision does not
// offer one.
func twoDistinctGroupOptions(d *decision.Decision) (decision.Option, decision.Option, bool) {
	for i := range d.Options {
		if d.Options[i].Group == "" {
			continue
		}
		for j := i + 1; j < len(d.Options); j++ {
			if d.Options[j].Group != "" && d.Options[j].Group != d.Options[i].Group {
				return d.Options[i], d.Options[j], true
			}
		}
	}
	return decision.Option{}, decision.Option{}, false
}
