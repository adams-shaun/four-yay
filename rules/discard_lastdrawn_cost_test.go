// Discard<1/LastDrawn> (Jandor's Ring) — "Discard the last card you drew
// this turn". LastDrawn is a history-keyed slot, like Random/Hand are
// selection-method slots, not a card filter: before the fix it fell through
// to matchesSpecFrom, which no hand card satisfies, so discardCandidates
// returned nothing, discardCostPayable withheld the offer forever and the
// ability was never offered. The fix reads the last events.Draw naming the
// player since the most recent TurnChange (lastDrawnThisTurn) and returns
// exactly that one card, and only while it is still in hand. These tests pin
// the offer, the exact-card payment, and both unpayable shapes (the last
// drawn card left the hand; nothing was drawn this turn).
//
// Jandor's Ring is in no repo deck, so this cannot move TestHeads or the
// botbench split.

package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// jandorsRingAbilityIdx is the printed ability index of Jandor's Ring's
// "{2}, {T}, Discard the last card you drew this turn: Draw a card".
const jandorsRingAbilityIdx = 0

// seedJandorsRing builds a two-seat game whose seat 0 carries the real
// corpus Jandor's Ring (compiled through the corpus registry, so the corpus
// file's AI: line never reaches the inline parser), and returns the engine,
// its replay config and the ring's object id moved to the battlefield. The
// caller lands at seat 0's Main1 with no decision pending.
func seedJandorsRing(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg, id, caster := corpusCardConfig(t, seed, "Jandor's Ring")
	if caster != 0 {
		t.Fatalf("precondition: corpusCardConfig active seat = %d, want 0", caster)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	e.pending = nil
	e.priorityRound()
	return e, cfg, id
}

// TestJandorsRingDiscardsTheLastCardDrawnThisTurn is the end-to-end pin:
// after a Draw of a known card, {2} funding offers the ability; activating it
// discards exactly that card, taps the ring and spends the {2}; then the
// ability's own Draw resolves.
func TestJandorsRingDiscardsTheLastCardDrawnThisTurn(t *testing.T) {
	t.Parallel()
	e, cfg, id := seedJandorsRing(t, 711)

	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("precondition: ring is in %s, want battlefield", e.G.Obj(id).Zone)
	}

	// Draw a known card: the library top, emitted with the engine's own Draw
	// shape (same as drawCardFor).
	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) == 0 {
		t.Fatal("precondition: seat 0's library is empty")
	}
	drawn := lib[0]
	handBefore := len(e.G.Zone(state.ZHand, 0))
	e.emit(events.Event{Kind: events.Draw, Player: 0, Obj: drawn, From: state.ZLibrary, To: state.ZHand, Secret: true})
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("precondition: hand after the emitted draw = %d, want %d", got, handBefore+1)
	}
	if e.G.Obj(drawn).Zone != state.ZHand {
		t.Fatalf("precondition: drawn card %d is in %s, want hand", drawn, e.G.Obj(drawn).Zone)
	}
	if last := pay.LastDrawnThisTurn(asPayer(e), 0); last != drawn {
		t.Fatalf("precondition: lastDrawnThisTurn(0) = %d, want the just-drawn %d", last, drawn)
	}

	// Fund {2} through logged ManaAdd events (replay-safe) and re-ask
	// priority so the offer sees the pool.
	addMana(t, e, 0, "CC")

	// The offer must carry an "ability" option for the ring (the exact defect:
	// pre-fix there was none).
	opt, ok := findAbilityOption(e, id, jandorsRingAbilityIdx)
	if !ok {
		t.Fatalf("Jandor's Ring ability is not offered: %+v", e.Pending())
	}
	if got := pay.DiscardCandidates(asPayer(e), 0, id, CostPart{N: 1, Spec: "LastDrawn"}, false, nil); len(got) != 1 || got[0] != drawn {
		t.Fatalf("discardCandidates(LastDrawn) = %v, want only the drawn card %d", got, drawn)
	}

	if err := e.Submit(decision.Intent{Seq: e.Pending().Seq, Player: 0, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit the ring's ability: %v", err)
	}

	// The discard cost asks for exactly the one candidate.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 {
		t.Fatalf("discard-cost decision = %+v, want choose exactly one", d)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != drawn {
		t.Fatalf("discard-cost options = %+v, want only the drawn card %d", d.Options, drawn)
	}
	submitChoices(t, e, d.Options[0].Index)

	// The cost: the drawn card is in the graveyard, the hand lost exactly it,
	// the ring is tapped and the {2} is spent.
	if e.G.Obj(drawn).Zone != state.ZGraveyard {
		t.Fatalf("the discarded card ended in %s, want graveyard", e.G.Obj(drawn).Zone)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore {
		t.Fatalf("hand after the discard cost = %d, want %d (the drawn card left)", got, handBefore)
	}
	if !e.G.Obj(id).Tapped {
		t.Fatal("the ring is not tapped after paying {T}")
	}
	if got := e.G.Players[0].Pool[state.MC]; got != 0 {
		t.Fatalf("generic mana after paying {2} = %d, want 0", got)
	}
	if !hasEvent(e, events.AbilityPush, id) {
		t.Fatal("the ring's ability was not pushed")
	}

	// The ability's own Draw resolves: the hand regains a card. The ability is
	// already on the stack and priority is pending, so drain rather than
	// Advance (Advance returns immediately while a decision is pending).
	handAfterCost := len(e.G.Zone(state.ZHand, 0))
	passUntilStackEmpty(t, e, 30)
	if got := len(e.G.Zone(state.ZHand, 0)); got != handAfterCost+1 {
		t.Fatalf("hand after the ability's Draw = %d, want %d", got, handAfterCost+1)
	}
	replayCheck(t, e, cfg)
}

// TestLastDrawnDiscardUnpayableWhenTheDrawnCardLeftHand pins the one-card
// semantics: the last card drawn this turn is a specific card, so once it
// leaves the hand the cost is unpayable (no skipping back to an earlier
// draw).
func TestLastDrawnDiscardUnpayableWhenTheDrawnCardLeftHand(t *testing.T) {
	t.Parallel()
	e, _, id := seedJandorsRing(t, 712)

	lib := e.G.Zone(state.ZLibrary, 0)
	drawn := lib[0]
	e.emit(events.Event{Kind: events.Draw, Player: 0, Obj: drawn, From: state.ZLibrary, To: state.ZHand, Secret: true})
	if last := pay.LastDrawnThisTurn(asPayer(e), 0); last != drawn {
		t.Fatalf("precondition: lastDrawnThisTurn(0) = %d, want %d", last, drawn)
	}
	// Positive control: while the drawn card is still in hand the feature
	// returns it. Without the fix this is empty, so the negative below can
	// only pass once the feature is live.
	if got := pay.DiscardCandidates(asPayer(e), 0, id, CostPart{N: 1, Spec: "LastDrawn"}, false, nil); len(got) != 1 || got[0] != drawn {
		t.Fatalf("control: discardCandidates(LastDrawn) = %v, want only the drawn card %d", got, drawn)
	}
	// The drawn card leaves the hand.
	e.emit(events.Event{Kind: events.MoveZone, Obj: drawn, From: state.ZHand, To: state.ZGraveyard})
	e.pending = nil
	if e.G.Obj(drawn).Zone != state.ZGraveyard {
		t.Fatalf("precondition: drawn card is in %s, want graveyard", e.G.Obj(drawn).Zone)
	}

	if got := pay.DiscardCandidates(asPayer(e), 0, id, CostPart{N: 1, Spec: "LastDrawn"}, false, nil); len(got) != 0 {
		t.Fatalf("discardCandidates(LastDrawn) = %v, want none once the drawn card left the hand", got)
	}
	addMana(t, e, 0, "CC")
	if _, ok := findAbilityOption(e, id, jandorsRingAbilityIdx); ok {
		t.Fatalf("the ring was offered with an unpayable LastDrawn cost: %+v", e.Pending())
	}
}

// TestLastDrawnDiscardUnpayableWithNoDrawThisTurn pins the empty-history
// shape: a player who drew nothing this turn has no LastDrawn candidate, so
// the ability is withheld.
func TestLastDrawnDiscardUnpayableWithNoDrawThisTurn(t *testing.T) {
	t.Parallel()
	e, _, id := seedJandorsRing(t, 713)

	// The ring is on the battlefield at turn 1 Main1; seat 0 (the starting
	// player, CR 103.7a) has taken no draw this turn.
	if got := e.CardsDrawnThisTurn(0); got != 0 {
		t.Fatalf("precondition: seat 0 drew %d this turn, want 0 (turn 1 draw step is skipped)", got)
	}
	if last := pay.LastDrawnThisTurn(asPayer(e), 0); last != 0 {
		t.Fatalf("precondition: lastDrawnThisTurn(0) = %d, want 0", last)
	}

	if got := pay.DiscardCandidates(asPayer(e), 0, id, CostPart{N: 1, Spec: "LastDrawn"}, false, nil); len(got) != 0 {
		t.Fatalf("discardCandidates(LastDrawn) = %v, want none with no draw this turn", got)
	}
	addMana(t, e, 0, "CC")
	if _, ok := findAbilityOption(e, id, jandorsRingAbilityIdx); ok {
		t.Fatalf("the ring was offered with no draw this turn: %+v", e.Pending())
	}

	// Positive control on the SAME board with only the history changed: emit
	// a Draw and the feature returns that card. Without the fix this is
	// empty, so the negative assertions above cannot pass on an unregistered
	// feature.
	lib := e.G.Zone(state.ZLibrary, 0)
	drawn := lib[0]
	e.emit(events.Event{Kind: events.Draw, Player: 0, Obj: drawn, From: state.ZLibrary, To: state.ZHand, Secret: true})
	if got := pay.DiscardCandidates(asPayer(e), 0, id, CostPart{N: 1, Spec: "LastDrawn"}, false, nil); len(got) != 1 || got[0] != drawn {
		t.Fatalf("control: discardCandidates(LastDrawn) = %v, want only the drawn card %d", got, drawn)
	}
}
