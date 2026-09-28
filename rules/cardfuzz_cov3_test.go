package rules

// Regressions for the engine gaps the cardfuzz coverage audit (fuzz-cov3)
// found behind cards that sat in the "supported" pool while a check they
// carry could never pass, each pinned on real corpus cards:
//
//   - unmodelled count heads (Count$Domain, Count$Void, the
//     LeftGraveyard/LeftBattlefield/SacrificedThisTurn/LifeLostLastTurn
//     log folds, Charging Cinderhorn's PlayerCountPlayers$AttackersDeclared,
//     Spinerock Knoll's MaxOppDamageThisTurn, Avenge's
//     attackedYouTheirLastTurn) that failed their gates closed or read a
//     meaningless zero;
//   - unrecognised filter words: Crown of Doom's `Player.!CardOwner`,
//     Magewright's Stone's `hasAbility Activated.hasTapCost`, and the
//     PresentCompare$ EQX SVar right-hand side (Zealots en-Dal);
//   - ActivationZone$ Exile never offered (Greater Gargadon) and Doubling
//     Cube's Produced$ Special DoubleManaInPool.

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// evalHead evaluates one count body from seat you's perspective and fails
// the test when the head is not modelled.
func evalHead(t *testing.T, e *Engine, you state.PlayerID, source state.ObjID, body string) int32 {
	t.Helper()
	ctx := &effects.Ctx{Controller: you, Source: source}
	if o := e.G.Obj(source); o != nil && o.Face() != nil {
		ctx.SVars = o.Face().SVars
	}
	n, ok := effects.EvalCountOK(e, ctx, body)
	if !ok {
		t.Fatalf("%s is not evaluated", body)
	}
	return n
}

// TestCountDomain: Tribal Flames' "for each basic land type among lands you
// control" -- a dual land counts both of its types, an opponent's land none.
func TestCountDomain(t *testing.T) {
	e := layerEngine(t)
	if n := evalHead(t, e, 0, 0, "Count$Domain"); n != 0 {
		t.Fatalf("empty board domain = %d", n)
	}
	onBoardCard(t, e, 0, corpusCard(t, "Tundra"))
	onBoardCard(t, e, 0, corpusCard(t, "Plains"))
	onBoardCard(t, e, 1, corpusCard(t, "Forest"))
	if n := evalHead(t, e, 0, 0, "Count$Domain"); n != 2 {
		t.Fatalf("Tundra + Plains domain = %d, want 2 (Plains, Island)", n)
	}
	if n := evalHead(t, e, 1, 0, "Count$Domain"); n != 1 {
		t.Fatalf("seat 1 domain = %d, want 1 (Forest)", n)
	}
}

// TestCountVoid: Edge of Eternities' Void -- a NONLAND permanent left the
// battlefield this turn (a land leaving does not count).
func TestCountVoid(t *testing.T) {
	e := layerEngine(t)
	land := onBoardCard(t, e, 0, corpusCard(t, "Plains"))
	bear := onBoardCard(t, e, 1, corpusCard(t, "Grizzly Bears"))
	if n := evalHead(t, e, 0, 0, "Count$Void.1.0"); n != 0 {
		t.Fatalf("nothing left: Void = %d", n)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: land, From: state.ZBattlefield, To: state.ZGraveyard})
	if n := evalHead(t, e, 0, 0, "Count$Void.1.0"); n != 0 {
		t.Fatalf("a land left: Void = %d, want 0", n)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZGraveyard})
	if n := evalHead(t, e, 0, 0, "Count$Void.1.0"); n != 1 {
		t.Fatalf("an opponent's creature died: Void = %d, want 1", n)
	}
}

// TestCountLeftZoneThisTurn: Bonecache Overseer's "cards left your
// graveyard this turn" and Kutzil's Flanker's "creatures you controlled
// left the battlefield this turn".
func TestCountLeftZoneThisTurn(t *testing.T) {
	e := layerEngine(t)
	bear := onBoardCard(t, e, 0, corpusCard(t, "Grizzly Bears"))
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZGraveyard})
	if n := evalHead(t, e, 0, 0, "Count$LeftBattlefieldThisTurn Creature.YouCtrl"); n != 1 {
		t.Fatalf("LeftBattlefieldThisTurn = %d, want 1", n)
	}
	if n := evalHead(t, e, 0, 0, "Count$LeftGraveyardThisTurn Card.YouOwn"); n != 0 {
		t.Fatalf("LeftGraveyardThisTurn before the exile = %d, want 0", n)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZGraveyard, To: state.ZExile})
	if n := evalHead(t, e, 0, 0, "Count$LeftGraveyardThisTurn Card.YouOwn"); n != 1 {
		t.Fatalf("LeftGraveyardThisTurn = %d, want 1", n)
	}
	if n := evalHead(t, e, 1, 0, "Count$LeftGraveyardThisTurn Card.YouOwn"); n != 0 {
		t.Fatalf("seat 1 LeftGraveyardThisTurn = %d, want 0 (not its card)", n)
	}
}

// TestChargingCinderhornNoAttackGate: "if no creatures attacked this turn"
// reads PlayerCountPlayers$AttackersDeclared EQ0 -- unmodelled, the end-step
// trigger never fired at all.
func TestChargingCinderhornNoAttackGate(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	onBoardCard(t, e, 0, corpusCard(t, "Charging Cinderhorn"))
	if n := stepTriggers(e, state.StepEnd, 1); n != 1 {
		t.Fatalf("no attack: end-step triggers = %d, want Charging Cinderhorn's one", n)
	}
	e2 := layerEngine(t)
	onBoardCard(t, e2, 0, corpusCard(t, "Charging Cinderhorn"))
	attacker := onBoardCard(t, e2, 1, corpusCard(t, "Grizzly Bears"))
	e2.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{attacker}})
	if n := stepTriggers(e2, state.StepEnd, 1); n != 0 {
		t.Fatalf("a creature attacked: end-step triggers = %d, want none", n)
	}
}

// TestLastTurnHeads: Avenge's "a player attacked you during their last
// turn", First Response's "you lost life last turn" and Spinerock Knoll's
// "an opponent was dealt 7 or more damage this turn" -- log folds over the
// previous turn's window and this turn's damage.
func TestLastTurnHeads(t *testing.T) {
	e := layerEngine(t)
	bear := onBoardCard(t, e, 1, corpusCard(t, "Grizzly Bears"))
	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: e.G.Turn + 1})
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{bear}})
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -3})
	if n := evalHead(t, e, 0, 0, "PlayerCountPlayers$HasPropertyattackedYouTheirLastTurn"); n != 0 {
		t.Fatalf("during the attacking turn itself: attackedYouTheirLastTurn = %d, want 0", n)
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
	if n := evalHead(t, e, 0, 0, "PlayerCountPlayers$HasPropertyattackedYouTheirLastTurn"); n != 1 {
		t.Fatalf("attackedYouTheirLastTurn = %d, want 1", n)
	}
	if n := evalHead(t, e, 1, 0, "PlayerCountPlayers$HasPropertyattackedYouTheirLastTurn"); n != 0 {
		t.Fatalf("seat 1 was never attacked: attackedYouTheirLastTurn = %d, want 0", n)
	}
	if n := evalHead(t, e, 0, 0, "PlayerCountPropertyYou$LifeLostLastTurn"); n != 3 {
		t.Fatalf("LifeLostLastTurn = %d, want 3", n)
	}
	if n := evalHead(t, e, 0, 0, "PlayerCountPropertyYou$LifeLostThisTurn"); n != 0 {
		t.Fatalf("LifeLostThisTurn = %d, want 0 (the loss was last turn)", n)
	}
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 4})
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 3})
	if n := evalHead(t, e, 0, 0, "Count$MaxOppDamageThisTurn"); n != 7 {
		t.Fatalf("MaxOppDamageThisTurn = %d, want 7", n)
	}
}

// TestZealotsEnDalPresentCompareSVar: "if all nonland permanents you
// control are white" is IsPresent$ Permanent.nonLand+White+YouCtrl |
// PresentCompare$ EQX with X the nonland-permanent count; the SVar
// right-hand side failed the comparison closed, so it never fired.
func TestZealotsEnDalPresentCompareSVar(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	onBoardCard(t, e, 0, corpusCard(t, "Zealots en-Dal"))
	onBoardCard(t, e, 0, corpusCard(t, "Plains"))
	if n := stepTriggers(e, state.StepUpkeep, 0); n != 1 {
		t.Fatalf("all white: upkeep triggers = %d, want Zealots en-Dal's one", n)
	}
	onBoardCard(t, e, 0, corpusCard(t, "Grizzly Bears"))
	e.pendingTriggers = nil
	if n := stepTriggers(e, state.StepUpkeep, 0); n != 0 {
		t.Fatalf("a green creature too: upkeep triggers = %d, want none", n)
	}
}

// abilityWithAPI returns the first printed ability of the object with api.
func abilityWithAPI(t *testing.T, e *Engine, id state.ObjID, api string) int {
	t.Helper()
	for i, ab := range e.G.Obj(id).Face().Abilities {
		if ab.API == api {
			return i
		}
	}
	t.Fatalf("no %s ability on %s", api, e.G.Obj(id).Face().Name)
	return -1
}

// TestCrownOfDoomTargetsANonOwner: "Target player other than Crown of
// Doom's owner gains control of it" (ValidTgts$ Player.!CardOwner) matched
// nobody, so the ability could never be activated.
func TestCrownOfDoomTargetsANonOwner(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	crown := onBoardCard(t, e, 0, corpusCard(t, "Crown of Doom"))
	ab := e.G.Obj(crown).Face().Abilities[abilityWithAPI(t, e, crown, "GainControl")]
	got := e.LegalTargets(0, crown, ab)
	if !slices.Contains(got, state.Target{Player: 1, IsPlayer: true}) {
		t.Fatalf("Crown of Doom cannot target the non-owner: %+v", got)
	}
	if slices.Contains(got, state.Target{Player: 0, IsPlayer: true}) {
		t.Fatalf("Crown of Doom can target its own owner: %+v", got)
	}
}

// TestMagewrightsStoneTargetsATapAbilityCreature: ValidTgts$
// Creature.hasAbility Activated.hasTapCost -- a creature with a {T} ability
// is a legal target, a vanilla creature is not.
func TestMagewrightsStoneTargetsATapAbilityCreature(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	stone := onBoardCard(t, e, 0, corpusCard(t, "Magewright's Stone"))
	sorcerer := onBoardCard(t, e, 0, corpusCard(t, "Prodigal Sorcerer"))
	bear := onBoardCard(t, e, 0, corpusCard(t, "Grizzly Bears"))
	ab := e.G.Obj(stone).Face().Abilities[abilityWithAPI(t, e, stone, "Untap")]
	got := e.LegalTargets(0, stone, ab)
	if !slices.Contains(got, state.Target{Obj: sorcerer}) {
		t.Fatalf("Prodigal Sorcerer ({T}: 1 damage) is not a legal target: %+v", got)
	}
	if slices.Contains(got, state.Target{Obj: bear}) {
		t.Fatalf("Grizzly Bears (no activated ability) is a legal target: %+v", got)
	}
}

// resolveStack passes priority until the stack is empty, answering any
// non-priority ask with its first option.
func resolveStack(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 40 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision pending with a non-empty stack")
		}
		idx := d.Options[0].Index
		if d.Kind == decision.KPriority {
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
		}
		submitChoices(t, e, idx)
	}
	if len(e.G.Stack) > 0 {
		t.Fatalf("stack did not empty: %v", e.G.Stack)
	}
}

// TestGreaterGargadonActivatesFromExile: "Sacrifice an artifact, creature,
// or land: Remove a time counter from Greater Gargadon. Activate only if
// Greater Gargadon is suspended" (ActivationZone$ Exile, IsPresent$
// Card.Self+suspended | PresentZone$ Exile) was never offered: the offer
// walk skipped exile and the present gate counted the battlefield.
func TestGreaterGargadonActivatesFromExile(t *testing.T) {
	t.Parallel()
	e, cfg, gargadon := newFixtureDeck(t, 311, corpusCardText(t, "g/greater_gargadon.txt"))
	from := e.G.Obj(gargadon).Zone
	e.emit(events.Event{Kind: events.MoveZone, Obj: gargadon, From: from, To: state.ZExile})
	e.emit(events.Event{Kind: events.CounterChange, Obj: gargadon, Counter: "TIME", Amount: 3})
	land := e.G.Zone(state.ZLibrary, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: land, From: state.ZLibrary, To: state.ZBattlefield})
	driveToStep(t, e, 1, 0, state.StepMain1)
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == gargadon {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no Greater Gargadon activation from exile: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	// The sacrifice cost's choice (a Mountain is the only candidate).
	for i := 0; i < 5 && len(e.G.Stack) == 0; i++ {
		d = e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			break
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	resolveStack(t, e)
	if n := e.G.Obj(gargadon).Counter("TIME"); n != 2 {
		t.Fatalf("time counters = %d, want 2", n)
	}
	if o := e.G.Obj(land); o.Zone == state.ZBattlefield {
		t.Fatal("the sacrifice cost was not paid")
	}
	replayCheck(t, e, cfg)
}

// TestDoublingCubeDoublesThePool: "{3}, {T}: Double the amount of each type
// of unspent mana you have" (Produced$ Special DoubleManaInPool) emitted a
// loud "unhandled Produced$" Note and no mana.
func TestDoublingCubeDoublesThePool(t *testing.T) {
	t.Parallel()
	e, cfg, cube := newFixtureDeck(t, 312, corpusCardText(t, "d/doubling_cube.txt"))
	from := e.G.Obj(cube).Zone
	e.emit(events.Event{Kind: events.MoveZone, Obj: cube, From: from, To: state.ZBattlefield})
	addMana(t, e, 0, "RRRRRG")
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == cube {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no Doubling Cube activation: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	for i := 0; i < 10; i++ {
		d = e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			break
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	if !e.G.Obj(cube).Tapped {
		t.Fatal("Doubling Cube was not activated")
	}
	if total := e.G.Players[0].Pool.Total(); total != 6 {
		t.Fatalf("pool total after {3} and the doubling = %d, want 6 (3 left, doubled)", total)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Obj == cube {
			t.Fatalf("Doubling Cube emitted a Note: %q", ev.Text)
		}
	}
	replayCheck(t, e, cfg)
}
