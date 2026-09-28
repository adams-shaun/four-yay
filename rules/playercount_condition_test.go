package rules

// The PlayerCount<group>$Condition<OP><RHS> <property> family and its
// companions (PlayerCountHasLost$Amount, the relative-player StartingLife
// property). Every fixture drives the REAL compiled corpus card. The brief's
// three carriers are the observable surface:
//
//   - Smuggler's Share: `PlayerCountOpponents$ConditionGE2 CardsDrawn` (the
//     draw leg) and `...ConditionGE2 ThisTurnEntered_Battlefield_Land.YouCtrl`
//     (the Treasure leg).
//   - Anya, Merciless Angel: `PlayerCountOpponents$ConditionLTZ LifeTotal`
//     with `Z = PlayerCountDefinedPlayer.PlayerUID_RelativePlayerUID$StartingLife/HalfDown`,
//     behind both her +3/+3-per-opponent static and her Indestructible static.
//   - Hot Pursuit: `CheckSVar$ PlayerCountHasLost$Amount | SVarCompare$ GE2`,
//     the intervening-if on its begin-combat gain-control trigger.
//
// The brief claimed Hot Pursuit and Anya failed OPEN (wrong-wide). Both
// gate paths fail CLOSED on an unresolvable count head
// (rules/trigger_condition.go's `!evaluated || !holds` and
// rules/statics.go's `!evaluated` branch), so the pre-fix behaviour was
// wrong-DEAD, not wrong-wide; these tests pin the post-fix CORRECT firing
// (fires exactly at the threshold), which is the defect the brief named.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// pcCondEngine builds a seats-seat engine (seat 0 the starter) with Mountain
// decks and the corpus token scripts, so a resolved effect can mint real
// Treasure tokens.
func pcCondEngine(t *testing.T, seats int) *Engine {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	names := make([]string, seats)
	decks := make([][]*cards.Card, seats)
	for i := range names {
		names[i] = string(rune('a' + i))
		decks[i] = mountainDeck(t, 40)
	}
	return New(seatZeroStart(Config{Seed: 1, Names: names, Decks: decks, Tokens: reg.Tokens}))
}

// pcMoveLand enters a land onto p's battlefield through a REAL MoveZone event,
// so state.Game.Entered records the entry the ThisTurnEntered_ head reads and
// the object's controller stays p (its owner). Returns the object id.
func pcMoveLand(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	land := corpusCard(t, name)
	o := e.G.AddObject(land, p)
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	if got := e.G.Obj(o.ID).Controller; got != p {
		t.Fatalf("PlayerCount fixture: %s entered under controller %d, want %d", name, got, p)
	}
	return o.ID
}

// pcTreasureTokens counts Treasure-token permanents on p's battlefield (the
// "Treasure Token" face the c_a_treasure_sac corpus script mints).
func pcTreasureTokens(e *Engine, p state.PlayerID) []state.ObjID {
	var out []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if o != nil && o.Face() != nil && o.Face().Name == "Treasure Token" {
			out = append(out, id)
		}
	}
	return out
}

// TestSmugglersShareCountsOpponentsByDrawAndLandEntry drives the real card's
// end-step trigger through both legs at once: two opponents drew two cards
// and had two lands enter under their control, so X = Y = 2 and the
// resolution draws two cards and mints two Treasures. The one-opponent
// control on the same card proves the per-member scoping.
func TestSmugglersShareCountsOpponentsByDrawAndLandEntry(t *testing.T) {
	t.Parallel()
	e := pcCondEngine(t, 3)
	share := onBoardCard(t, e, 0, corpusCard(t, "Smuggler's Share"))
	face := e.G.Obj(share).Face()
	if face.Name != "Smuggler's Share" {
		t.Fatalf("PlayerCount fixture changed: on-board card is %q", face.Name)
	}
	// The compiled SVar bodies are the contract under test; assert the
	// fixture still carries the two Condition heads this class implements.
	if got := face.SVars["X"]; got != "PlayerCountOpponents$ConditionGE2 CardsDrawn" {
		t.Fatalf("Smuggler's Share SVar X changed: %q", got)
	}
	if got := face.SVars["Y"]; got != "PlayerCountOpponents$ConditionGE2 ThisTurnEntered_Battlefield_Land.YouCtrl" {
		t.Fatalf("Smuggler's Share SVar Y changed: %q", got)
	}

	// Both opponents: two draws and two lands each, so X = Y = 2. A third
	// (below-threshold) opponent would need a fourth seat; the one-opponent
	// control below covers the scoping instead.
	for _, p := range []state.PlayerID{1, 2} {
		for i := 0; i < 2; i++ {
			drawn := e.G.Zone(state.ZLibrary, p)[0]
			e.emit(events.Event{Kind: events.Draw, Player: p, Obj: drawn, From: state.ZLibrary, To: state.ZHand, Secret: true})
		}
		for i := 0; i < 2; i++ {
			pcMoveLand(t, e, p, "Mountain")
		}
	}
	// A land under the CONTROLLER's own control must not count: the spec's
	// YouCtrl binds to the counted opponent, never the resolving controller.
	pcMoveLand(t, e, 0, "Mountain")

	handBefore := len(e.G.Zone(state.ZHand, 0))
	trig := cards.ResolveSVar(face.SVars, "TrigDraw")
	if trig == nil || trig.API != "Draw" || trig.Params["NumCards"] != "X" {
		t.Fatalf("Smuggler's Share TrigDraw fixture changed: %+v", trig)
	}
	e.resolveAbility(share, 0, nil, trig, face.SVars)

	if got, want := len(e.G.Zone(state.ZHand, 0)), handBefore+2; got != want {
		t.Fatalf("Smuggler's Share drew to %d cards, want %d (X should be 2: both opponents drew >= 2)", got, want)
	}
	if tres := pcTreasureTokens(e, 0); len(tres) != 2 {
		t.Fatalf("Smuggler's Share created %d Treasure(s), want 2 (Y should be 2: both opponents had >= 2 lands enter)", len(tres))
	}
}

// TestSmugglersShareOneOpponentMeetsCountsOnlyThatOne is the real
// one-opponent scoping control (the earlier round's report claimed the
// full-threshold test carried one; it did not — both opponents met both
// thresholds there): seat 1 meets both thresholds, seat 2 meets neither,
// so exactly ONE card is drawn and ONE Treasure minted. A group-wide read
// of either threshold ("some opponent drew 2") would count both legs 1
// the same way, so the discriminating check is that the BELOW-threshold
// opponent contributes nothing while the above-threshold one is counted
// per member — the pair of tests together pins both directions.
func TestSmugglersShareOneOpponentMeetsCountsOnlyThatOne(t *testing.T) {
	t.Parallel()
	e := pcCondEngine(t, 3)
	share := onBoardCard(t, e, 0, corpusCard(t, "Smuggler's Share"))
	face := e.G.Obj(share).Face()

	// Seat 1 meets both thresholds; seat 2 is below both (one draw, one
	// land); a controller-owned land must not count either.
	for i := 0; i < 2; i++ {
		drawn := e.G.Zone(state.ZLibrary, 1)[0]
		e.emit(events.Event{Kind: events.Draw, Player: 1, Obj: drawn, From: state.ZLibrary, To: state.ZHand, Secret: true})
		pcMoveLand(t, e, 1, "Mountain")
	}
	drawn := e.G.Zone(state.ZLibrary, 2)[0]
	e.emit(events.Event{Kind: events.Draw, Player: 2, Obj: drawn, From: state.ZLibrary, To: state.ZHand, Secret: true})
	pcMoveLand(t, e, 2, "Mountain")
	pcMoveLand(t, e, 0, "Mountain")

	handBefore := len(e.G.Zone(state.ZHand, 0))
	trig := cards.ResolveSVar(face.SVars, "TrigDraw")
	e.resolveAbility(share, 0, nil, trig, face.SVars)

	if got, want := len(e.G.Zone(state.ZHand, 0)), handBefore+1; got != want {
		t.Fatalf("Smuggler's Share drew to %d cards, want %d (X = 1: only seat 1 drew >= 2)", got, want)
	}
	if tres := pcTreasureTokens(e, 0); len(tres) != 1 {
		t.Fatalf("Smuggler's Share created %d Treasure(s), want 1 (Y = 1: only seat 1 had >= 2 lands enter)", len(tres))
	}
}

// TestSmugglersShareNoOpponentMeetsEitherThreshold pins the other end: with
// every opponent below both thresholds neither leg fires (X = Y = 0), so no
// card is drawn and no Treasure is created.
func TestSmugglersShareNoOpponentMeetsEitherThreshold(t *testing.T) {
	t.Parallel()
	e := pcCondEngine(t, 3)
	share := onBoardCard(t, e, 0, corpusCard(t, "Smuggler's Share"))
	face := e.G.Obj(share).Face()

	for i := 0; i < 1; i++ {
		drawn := e.G.Zone(state.ZLibrary, 1)[0]
		e.emit(events.Event{Kind: events.Draw, Player: 1, Obj: drawn, From: state.ZLibrary, To: state.ZHand, Secret: true})
	}
	pcMoveLand(t, e, 1, "Mountain") // one land only, under threshold 2

	handBefore := len(e.G.Zone(state.ZHand, 0))
	trig := cards.ResolveSVar(face.SVars, "TrigDraw")
	e.resolveAbility(share, 0, nil, trig, face.SVars)

	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore {
		t.Fatalf("Smuggler's Share drew to %d cards, want %d (X should be 0)", got, handBefore)
	}
	if tres := pcTreasureTokens(e, 0); len(tres) != 0 {
		t.Fatalf("Smuggler's Share created %d Treasure(s), want 0 (Y should be 0)", len(tres))
	}
}

// TestRampantFrogantuaHasLostTimesSuffix pins the /Op suffix on the head's
// SECOND corpus carrier, driving the real card: its
// `SVar:X:PlayerCountHasLost$Amount/Times.10` (+10/+10 per lost player)
// failed closed before the suffix split, so the Frogantua stayed its
// printed 3/3 however many seats had lost.
func TestRampantFrogantuaHasLostTimesSuffix(t *testing.T) {
	t.Parallel()
	e := pcCondEngine(t, 2)
	frog := onBoardCard(t, e, 0, corpusCard(t, "Rampant Frogantua"))
	face := e.G.Obj(frog).Face()
	if got := face.SVars["X"]; got != "PlayerCountHasLost$Amount/Times.10" {
		t.Fatalf("Rampant Frogantua SVar X changed: %q", got)
	}
	if got := e.Derived(frog); got.Power != 3 || got.Toughness != 3 {
		t.Fatalf("Frogantua with no losses = %d/%d, want 3/3", got.Power, got.Toughness)
	}
	e.emit(events.Event{Kind: events.PlayerLost, Player: 1})
	if got := e.Derived(frog); got.Power != 13 || got.Toughness != 13 {
		t.Fatalf("Frogantua with one loss = %d/%d, want 13/13 (1 lost x Times.10)", got.Power, got.Toughness)
	}
}

// TestAnyaMercilessAngelHalvesStartingLifeThreshold pins Anya's relative
// half-starting-life read: with every opponent at 20 life (20 >= 10) she is a
// 4/4 with no Indestructible; once an opponent drops to 9 (< 10) Y = 1, so
// she is a 7/7 and indestructible.
func TestAnyaMercilessAngelHalvesStartingLifeThreshold(t *testing.T) {
	t.Parallel()
	e := pcCondEngine(t, 2)
	anya := onBoardCard(t, e, 0, corpusCard(t, "Anya, Merciless Angel"))
	face := e.G.Obj(anya).Face()
	if got := face.SVars["Y"]; got != "PlayerCountOpponents$ConditionLTZ LifeTotal" {
		t.Fatalf("Anya SVar Y changed: %q", got)
	}
	if got := face.SVars["Z"]; got != "PlayerCountDefinedPlayer.PlayerUID_RelativePlayerUID$StartingLife/HalfDown" {
		t.Fatalf("Anya SVar Z changed: %q", got)
	}

	// Full life: no opponent below half, Y = 0.
	if got := e.Derived(anya); got.Power != 4 || got.Toughness != 4 {
		t.Fatalf("Anya at 20-life opponents = %d/%d, want 4/4 (Y = 0)", got.Power, got.Toughness)
	}
	if e.HasKeyword(anya, "Indestructible") {
		t.Fatalf("Anya gained Indestructible with no opponent below half their starting life (Y = 0)")
	}

	// Opponent to 9 (< half of 20 = 10): Y = 1.
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -11})
	if got := e.G.Players[1].Life; got != 9 {
		t.Fatalf("PlayerCount fixture: opponent life = %d, want 9", got)
	}
	if got := e.Derived(anya); got.Power != 7 || got.Toughness != 7 {
		t.Fatalf("Anya at a 9-life opponent = %d/%d, want 7/7 (Y = 1)", got.Power, got.Toughness)
	}
	if !e.HasKeyword(anya, "Indestructible") {
		t.Fatalf("Anya lost Indestructible while an opponent was below half their starting life (Y = 1)")
	}

	// Back above the threshold: both the bonus and the keyword evaporate.
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: 1})
	if got := e.Derived(anya); got.Power != 4 || got.Toughness != 4 {
		t.Fatalf("Anya back at 10 life opponent = %d/%d, want 4/4 (10 >= half of 20)", got.Power, got.Toughness)
	}
	if e.HasKeyword(anya, "Indestructible") {
		t.Fatalf("Anya kept Indestructible at a 10-life opponent (10 is not below half of 20)")
	}
}

// TestHotPursuitGateFiresOnlyAtTwoLosses pins the CR 603.4 intervening-if:
// `PlayerCountHasLost$Amount` now resolves, so the begin-combat gain-control
// trigger fires with two players lost and stays silent with one.
func TestHotPursuitGateFiresOnlyAtTwoLosses(t *testing.T) {
	t.Parallel()
	e := pcCondEngine(t, 4)
	pursuit := onBoardCard(t, e, 0, corpusCard(t, "Hot Pursuit"))
	face := e.G.Obj(pursuit).Face()
	var gate *cards.Trigger
	for i := range face.Triggers {
		tr := &face.Triggers[i]
		if tr.Mode == "Phase" && tr.Params["CheckSVar"] == "PlayerCountHasLost$Amount" {
			gate = tr
		}
	}
	if gate == nil {
		t.Fatalf("Hot Pursuit fixture changed: no Phase trigger with the PlayerCountHasLost gate")
	}
	if gate.Params["SVarCompare"] != "GE2" || gate.Params["ValidPlayer"] != "You" {
		t.Fatalf("Hot Pursuit gate params changed: %+v", gate.Params)
	}

	// One player lost: the gate is below GE2, no trigger.
	e.emit(events.Event{Kind: events.PlayerLost, Player: 3})
	if e.G.Players[3].Lost != true {
		t.Fatalf("PlayerCount fixture: PlayerLost did not mark seat 3")
	}
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})
	queued := 0
	for _, pt := range e.pendingTriggers {
		if pt.Source == pursuit {
			queued++
		}
	}
	if queued != 0 {
		t.Fatalf("Hot Pursuit queued %d trigger(s) with ONE player lost, want 0", queued)
	}

	// Two players lost: the gate holds, the trigger fires.
	e.emit(events.Event{Kind: events.PlayerLost, Player: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})
	queued = 0
	for _, pt := range e.pendingTriggers {
		if pt.Source == pursuit {
			queued++
		}
	}
	if queued != 1 {
		t.Fatalf("Hot Pursuit queued %d trigger(s) with TWO players lost, want 1", queued)
	}
}
