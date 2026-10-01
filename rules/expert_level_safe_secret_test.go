package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The multi-chooser SECRET ChooseNumber election (api:ChooseNumber's
// MatchedAbility$/UnmatchedAbility$ shape): Expert-Level Safe's `{1},{T}`
// ability asks the controller AND the target opponent each to secretly pick
// 1, 2 or 3, reveals both, then runs DBSacrifice (matched) or DBFillSafe
// (unmatched). Before the fix only the controller was asked, the two picks
// were never compared and neither branch ran.
//
// The tests drive the REAL corpus card end to end: cast it (funding {2}),
// activate the ability through the ordinary priority walk, answer the target
// and BOTH choosers through the decision seam, then assert zones and counts.

// exiledWith counts the cards associated with src in any exile zone (the
// Card.ExiledWithSource set the matched branch returns).
func exiledWith(e *Engine, src state.ObjID) int {
	n := 0
	for _, p := range []state.PlayerID{0, 1} {
		for _, id := range e.G.Zone(state.ZExile, p) {
			if o := e.G.Obj(id); o != nil && o.ExiledWith == src {
				n++
			}
		}
	}
	return n
}

// faceDownExiledWith counts the cards associated with src in exile that are
// face down (the "exile the top card ... face down" branch's product).
func faceDownExiledWith(e *Engine, src state.ObjID) int {
	n := 0
	for _, p := range []state.PlayerID{0, 1} {
		for _, id := range e.G.Zone(state.ZExile, p) {
			if o := e.G.Obj(id); o != nil && o.ExiledWith == src && o.FaceDown {
				n++
			}
		}
	}
	return n
}

// setupExpertLevelSafe casts the real corpus Expert-Level Safe and returns at
// seat 0's priority with the artifact untapped on the battlefield and exactly
// two cards exiled with it by its enter-the-battlefield ability. It asserts
// its own preconditions: the card started in hand, entered the battlefield,
// and the ETB really exiled two cards.
func setupExpertLevelSafe(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	e := handEngine(t, corpusCard(t, "Expert-Level Safe"))
	safe := e.G.Zone(state.ZHand, 0)[0]
	if o := e.G.Obj(safe); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: Expert-Level Safe not in seat 0's hand: %+v", o)
	}
	e.G.Players[0].Pool[state.MC] = 2
	e.priorityRound()
	castObj(t, e, safe)
	if o := e.G.Obj(safe); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Expert-Level Safe did not enter the battlefield: %+v", o)
	}
	if n := exiledWith(e, safe); n != 2 {
		t.Fatalf("precondition: the ETB exiled %d cards with the Safe, want 2 (exile %v, lib %d)",
			n, e.G.Zone(state.ZExile, 0), len(e.G.Zone(state.ZLibrary, 0)))
	}
	return e, safe
}

// activateSafe funds {1}, offers the Safe's `{1},{T}` ability at priority and
// submits it, then answers the opponent target. It returns the pending
// election ask.
func activateSafe(t *testing.T, e *Engine, safe state.ObjID) *decision.Decision {
	t.Helper()
	e.G.Players[0].Pool[state.MC] = 1
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("not at priority for the activation: %+v", d)
	}
	opt, ok := findAbilityOption(e, safe, 0)
	if !ok {
		t.Fatalf("precondition: the Safe's ability is not offered: %+v", d.Options)
	}
	submitChoices(t, e, opt.Index)
	td := e.Pending()
	if td == nil || td.Kind != decision.KTarget {
		t.Fatalf("expected the opponent target ask, got %+v", td)
	}
	idx := indexOfPlayerOption(td, 1)
	if idx < 0 {
		t.Fatalf("precondition: no seat-1 target offered: %+v", td.Options)
	}
	submitChoices(t, e, idx)
	d0 := passUntilAsk(t, e)
	if d0 == nil || d0.Kind != decision.KChoose || d0.ResumeKind != "choosenumbermulti" {
		t.Fatalf("expected the first election ask, got %+v", d0)
	}
	return d0
}

// submitNumber answers an election ask with the option whose Amount is n.
func submitNumber(t *testing.T, e *Engine, d *decision.Decision, n int) {
	t.Helper()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "number" && o.Amount == n {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no number option %d in %+v", n, d.Options)
	}
	submitChoices(t, e, idx)
}

// answerElection answers both seats' election asks with p0's and p1's picks
// and returns the two decisions so the caller can assert the seats differ.
func answerSafeElection(t *testing.T, e *Engine, p0, p1 int) (*decision.Decision, *decision.Decision) {
	t.Helper()
	first := e.Pending()
	if first == nil || first.Kind != decision.KChoose || first.ResumeKind != "choosenumbermulti" {
		t.Fatalf("expected the first election ask, got %+v", first)
	}
	submitNumber(t, e, first, p0)
	second := e.Pending()
	if second == nil || second.Kind != decision.KChoose || second.ResumeKind != "choosenumbermulti" {
		t.Fatalf("expected the second election ask, got %+v", second)
	}
	submitNumber(t, e, second, p1)
	return first, second
}

// TestExpertLevelSafeSecretElectionAsksBothSeats proves the reported defect's
// first half is fixed: the ability poses ONE secret number ask to the
// controller AND one to the target opponent, in that deterministic order.
func TestExpertLevelSafeSecretElectionAsksBothSeats(t *testing.T) {
	t.Parallel()
	e, safe := setupExpertLevelSafe(t)
	first := activateSafe(t, e, safe)
	if first.Player != 0 || first.Min != 1 || first.Max != 1 {
		t.Fatalf("first election ask = %+v, want seat 0 Min/Max 1", first)
	}
	if len(first.Options) != 3 {
		t.Fatalf("first election options = %+v, want the bounded 1..3 list", first.Options)
	}
	for i, o := range first.Options {
		if o.Kind != "number" || o.Amount != i+1 {
			t.Fatalf("option %d = %+v, want number %d", i, o, i+1)
		}
	}
	submitNumber(t, e, first, 2)
	second := e.Pending()
	if second == nil || second.Kind != decision.KChoose || second.ResumeKind != "choosenumbermulti" {
		t.Fatalf("expected the second election ask to the opponent, got %+v", second)
	}
	if second.Player == first.Player {
		t.Fatalf("both election asks went to seat %d, want two distinct seats", second.Player)
	}
	if second.Player != 1 {
		t.Fatalf("second election ask seat = %d, want the target opponent (1)", second.Player)
	}
}

// TestExpertLevelSafeSecretElectionMatchedSacrificesAndReturns is the matched
// path: both seats choose 2, so the Safe is sacrificed and every card exiled
// with it is returned to its owner's hand.
func TestExpertLevelSafeSecretElectionMatchedSacrificesAndReturns(t *testing.T) {
	t.Parallel()
	e, safe := setupExpertLevelSafe(t)
	handBefore := len(e.G.Zone(state.ZHand, 0))
	activateSafe(t, e, safe)
	first, second := answerSafeElection(t, e, 2, 2)
	if first.Player == second.Player {
		t.Fatalf("precondition: the two compared picks were asked of one seat %d", first.Player)
	}
	passUntilStackEmpty(t, e, 60)
	if o := e.G.Obj(safe); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("matched branch: Safe zone = %+v, want graveyard", o)
	}
	if n := exiledWith(e, safe); n != 0 {
		t.Fatalf("matched branch: %d cards still exiled with the Safe, want 0", n)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+2 {
		t.Fatalf("matched branch: seat 0 hand = %d, want %d (the two exiled cards returned)", got, handBefore+2)
	}
}

// TestExpertLevelSafeSecretElectionMismatchExilesOne is the unmatched path:
// the seats choose different numbers, so the Safe survives and one more card
// is exiled with it face down.
func TestExpertLevelSafeSecretElectionMismatchExilesOne(t *testing.T) {
	t.Parallel()
	e, safe := setupExpertLevelSafe(t)
	handBefore := len(e.G.Zone(state.ZHand, 0))
	libBefore := len(e.G.Zone(state.ZLibrary, 0))
	activateSafe(t, e, safe)
	first, second := answerSafeElection(t, e, 2, 3)
	if first.Player == second.Player {
		t.Fatalf("precondition: the two compared picks were asked of one seat %d", first.Player)
	}
	passUntilStackEmpty(t, e, 60)
	if o := e.G.Obj(safe); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("unmatched branch: Safe zone = %+v, want battlefield", o)
	}
	if n := exiledWith(e, safe); n != 3 {
		t.Fatalf("unmatched branch: %d cards exiled with the Safe, want 3", n)
	}
	if n := faceDownExiledWith(e, safe); n != 3 {
		t.Fatalf("unmatched branch: %d face-down cards exiled with the Safe, want 3", n)
	}
	if got := len(e.G.Zone(state.ZLibrary, 0)); got != libBefore-1 {
		t.Fatalf("unmatched branch: seat 0 library = %d, want %d (one more card exiled)", got, libBefore-1)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore {
		t.Fatalf("unmatched branch: seat 0 hand = %d, want %d (nothing returned)", got, handBefore)
	}
}
