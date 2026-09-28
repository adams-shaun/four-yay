package rules

// The 2026-09-22 repo-deck audit found two Count$ heads the evaluator had
// never modelled — both degraded to the dispatch's (0, false) fallthrough,
// so the cards carrying them were dead in the decks that ran them:
//
//   - Cabal Ritual (the-epic-storm x4): SVar:X:Count$Threshold.5.3 behind
//     `SP$ Mana | Produced$ B | Amount$ X` — added NO mana at all.
//   - Aspect of Hydra (mono-green-stompy x4): SVar:X:Count$Devotion.Green
//     behind `SP$ Pump | NumAtt$ +X | NumDef$ +X` — pumped +0/+0.
//
// This file pins both fixes end to end on the REAL compiled corpus cards,
// through the public engine surface, with the graveyard/battlefield
// preconditions the assertions depend on asserted first.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// ritualShape asserts the audited corpus shape the count lives in, so a
// corpus pin change cannot silently hollow the pin out.
func ritualShape(t *testing.T, ritual *cards.Card) {
	t.Helper()
	f := ritual.Faces[0]
	var manaSA *cards.SA
	for _, a := range f.Abilities {
		if a.Kind == "SP" && a.API == "Mana" {
			manaSA = a
		}
	}
	if manaSA == nil {
		t.Fatalf("corpus Cabal Ritual carries no SP$ Mana ability: %+v", f.Abilities)
	}
	if manaSA.Params["Produced"] != "B" || manaSA.Params["Amount"] != "X" {
		t.Fatalf("corpus Cabal Ritual mana params = %v, want Produced$ B Amount$ X", manaSA.Params)
	}
	if got := f.SVars["X"]; got != "Count$Threshold.5.3" {
		t.Fatalf("corpus Cabal Ritual SVar:X = %q, want Count$Threshold.5.3", got)
	}
}

// castRitualIdx finds the pending cast option for a hand spell, failing the
// test when the spell is not offered — the precondition every later
// assertion rides on.
func castRitualIdx(t *testing.T, e *Engine, id state.ObjID) int {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no pending decision")
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			return o.Index
		}
	}
	t.Fatalf("ritual %d not offered as a cast option: %+v", id, d.Options)
	return -1
}

// millTo brings seat 0's graveyard to exactly n cards by moving cards from
// the library through logged MoveZone events (a genesis card may already sit
// in it — the count is absolute).
func millTo(t *testing.T, e *Engine, n int) {
	t.Helper()
	pre := len(e.G.Zone(state.ZGraveyard, 0))
	if pre > n {
		t.Fatalf("graveyard already holds %d cards, cannot bring it to %d", pre, n)
	}
	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) < n-pre {
		t.Fatalf("library holds %d cards, cannot mill %d", len(lib), n-pre)
	}
	for _, id := range lib[:n-pre] {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZGraveyard})
	}
	e.pending = nil
	e.Advance()
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != n {
		t.Fatalf("graveyard holds %d cards after milling from %d, want %d", got, pre, n)
	}
}

// TestCabalRitualAddsThreeWithoutThreshold drives the threshold-OFF half: an
// empty graveyard, {1}{B} paid from a floating {B}{B}, and the resolution
// adding exactly three black mana.
func TestCabalRitualAddsThreeWithoutThreshold(t *testing.T) {
	t.Parallel()
	ritual := corpusCard(t, "Cabal Ritual")
	ritualShape(t, ritual)
	e := handEngine(t, ritual, ritual)
	hand := e.G.Zone(state.ZHand, 0)
	if len(hand) != 2 || e.G.Obj(hand[0]).Face().Name != "Cabal Ritual" {
		t.Fatalf("fixture broken: hand = %v", hand)
	}

	addMana(t, e, 0, "BB")
	if e.G.Players[0].Pool[state.MB] != 2 {
		t.Fatalf("float not seeded: pool %+v", e.G.Players[0].Pool)
	}
	submitChoices(t, e, castRitualIdx(t, e, hand[0]))
	passUntilStackEmpty(t, e, 20)

	if len(e.G.Stack) != 0 {
		t.Fatal("stack did not empty")
	}
	if got := e.G.Players[0].Pool[state.MB]; got != 3 {
		t.Fatalf("pool black after the ritual = %d, want 3 (the {1}{B} cost spent the float; X = 3 without Threshold)", got)
	}
}

// TestCabalRitualThresholdAddsFive drives the threshold-ON half: seven
// graveyard cards (CR 702.24's boundary), a second cast, five black mana.
func TestCabalRitualThresholdAddsFive(t *testing.T) {
	t.Parallel()
	ritual := corpusCard(t, "Cabal Ritual")
	e := handEngine(t, ritual, ritual)
	hand := e.G.Zone(state.ZHand, 0)

	millTo(t, e, 7)
	addMana(t, e, 0, "BB")
	submitChoices(t, e, castRitualIdx(t, e, hand[1]))
	passUntilStackEmpty(t, e, 20)

	if got := len(e.G.Zone(state.ZGraveyard, 0)); got < 7 {
		t.Fatalf("graveyard holds %d cards after the ritual resolved, want at least 7 (the boundary held through resolution)", got)
	}
	if got := e.G.Players[0].Pool[state.MB]; got != 5 {
		t.Fatalf("pool black after the threshold ritual = %d, want 5 (CR 702.24: seven cards in the graveyard)", got)
	}
}

// TestCabalRitualSixGraveyardCardsStaysAtThree pins the boundary from the
// engine side: six cards in the graveyard is NOT Threshold — the second
// ritual still adds three.
func TestCabalRitualSixGraveyardCardsStaysAtThree(t *testing.T) {
	t.Parallel()
	ritual := corpusCard(t, "Cabal Ritual")
	e := handEngine(t, ritual, ritual)
	hand := e.G.Zone(state.ZHand, 0)

	millTo(t, e, 6)
	addMana(t, e, 0, "BB")
	submitChoices(t, e, castRitualIdx(t, e, hand[1]))
	passUntilStackEmpty(t, e, 20)

	if got := len(e.G.Zone(state.ZGraveyard, 0)); got < 6 {
		t.Fatalf("graveyard holds %d cards after the ritual resolved, want at least 6", got)
	}
	if got := e.G.Players[0].Pool[state.MB]; got != 3 {
		t.Fatalf("pool black with six graveyard cards = %d, want 3 (CR 702.24 needs seven)", got)
	}
}

// hydraShape asserts the audited corpus shape: the pump reads its X off the
// devotion SVar.
func hydraShape(t *testing.T, hydra *cards.Card) {
	t.Helper()
	f := hydra.Faces[0]
	var pump *cards.SA
	for _, a := range f.Abilities {
		if a.Kind == "SP" && a.API == "Pump" {
			pump = a
		}
	}
	if pump == nil {
		t.Fatalf("corpus Aspect of Hydra carries no SP$ Pump ability: %+v", f.Abilities)
	}
	if pump.Params["NumAtt"] != "+X" || pump.Params["NumDef"] != "+X" {
		t.Fatalf("corpus Aspect of Hydra pump params = %v, want NumAtt$ +X NumDef$ +X", pump.Params)
	}
	if got := f.SVars["X"]; got != "Count$Devotion.Green" {
		t.Fatalf("corpus Aspect of Hydra SVar:X = %q, want Count$Devotion.Green", got)
	}
}

// TestAspectOfHydraPumpsByDevotion pins the devotion pump end to end: three
// green pips among seat 0's permanents (Llanowar Elves {G}, Elvish Mystic
// {G}, the target's own {1}{G}), so the +X/+X is +3/+3 — not the +0/+0 the
// unmodelled head produced.
func TestAspectOfHydraPumpsByDevotion(t *testing.T) {
	t.Parallel()
	hydra := corpusCard(t, "Aspect of Hydra")
	hydraShape(t, hydra)
	e := handEngine(t, hydra)
	bears := onBoardCard(t, e, 0, card(t, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	onBoardCard(t, e, 0, corpusCard(t, "Llanowar Elves"))
	onBoardCard(t, e, 0, corpusCard(t, "Elvish Mystic"))

	if e.Power(bears) != 2 || e.Toughness(bears) != 2 {
		t.Fatalf("precondition broken: bears = %d/%d, want 2/2", e.Power(bears), e.Toughness(bears))
	}

	addMana(t, e, 0, "G")
	d := e.Pending()
	if d == nil {
		t.Fatal("no pending decision")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == e.G.Zone(state.ZHand, 0)[0] {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Aspect of Hydra not offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the target ask, got %+v", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == bears {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("bears not offered as a target: %+v", d.Options)
	}
	submitChoices(t, e, tgt)
	passUntilStackEmpty(t, e, 20)

	if len(e.G.Stack) != 0 {
		t.Fatal("stack did not empty")
	}
	if got := e.Power(bears); got != 5 {
		t.Fatalf("bears power after Aspect of Hydra = %d, want 5 (2 + devotion 3: Elves, Mystic, its own {G})", got)
	}
	if got := e.Toughness(bears); got != 5 {
		t.Fatalf("bears toughness after Aspect of Hydra = %d, want 5", got)
	}
}

// TestAspectOfHydraZeroDevotionPumpsNothing pins the zero leg: with no
// mana-cost pips anywhere among the controller's permanents (a costless
// creature and costless lands), X counts 0 and the +X/+X is +0/+0 — the
// target is untouched.
func TestAspectOfHydraZeroDevotionPumpsNothing(t *testing.T) {
	t.Parallel()
	hydra := corpusCard(t, "Aspect of Hydra")
	e := handEngine(t, hydra)
	wall := onBoardCard(t, e, 0, card(t, "Name:Vanilla Wall\nManaCost:no cost\nTypes:Creature Wall\nPT:0/4\nOracle:x\n"))
	if e.Power(wall) != 0 || e.Toughness(wall) != 4 {
		t.Fatalf("precondition broken: Wall = %d/%d, want 0/4", e.Power(wall), e.Toughness(wall))
	}

	addMana(t, e, 0, "G")
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == e.G.Zone(state.ZHand, 0)[0] {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Aspect of Hydra not offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the target ask, got %+v", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == wall {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("the Wall not offered as a target: %+v", d.Options)
	}
	submitChoices(t, e, tgt)
	passUntilStackEmpty(t, e, 20)

	if got := e.Toughness(wall); got != 4 {
		t.Fatalf("Wall toughness after a zero-devotion Aspect = %d, want 4 (X counted 0)", got)
	}
	if got := e.Power(wall); got != 0 {
		t.Fatalf("Wall power after a zero-devotion Aspect = %d, want 0", got)
	}
}
