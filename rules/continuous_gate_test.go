package rules

// The continuous recheck gate (rules/layers.go's continuousGateHolds): a
// Mode$ Continuous static carrying IsPresent$/IsPresent2$ or
// CheckSVar$/SVarCompare$ grants only while its gate holds, re-evaluated once
// per emitted event (the staticContinuous memo's epoch key) so board movement
// turns the grant on and off. Every fixture below embeds the real Forge
// script line(s) of the corpus card it probes (never a committed .txt -- the
// licensing rule), except the synthetic Human the Overseer's gate counts.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const angelicOverseerSrc = "Name:Angelic Overseer\nManaCost:3 W W\nTypes:Creature Angel\nPT:5/3\nK:Flying\n" +
	"S:Mode$ Continuous | Affected$ Card.Self | AddKeyword$ Hexproof & Indestructible | IsPresent$ Human.YouCtrl | Description$ As long as you control a Human, CARDNAME has hexproof and indestructible.\n" +
	"Oracle:As long as you control a Human, Angelic Overseer has hexproof and indestructible.\n"

const auriokSteelshaperSrc = "Name:Auriok Steelshaper\nManaCost:1 W\nTypes:Creature Human Soldier\nPT:1/1\n" +
	"S:Mode$ ReduceCost | ValidCard$ Card | ValidSpell$ Activated.Equip | Activator$ You | Amount$ 1 | Description$ Equip costs you pay cost {1} less.\n" +
	"S:Mode$ Continuous | Affected$ Creature.Soldier+YouCtrl,Creature.Knight+YouCtrl | AddPower$ 1 | AddToughness$ 1 | IsPresent$ Card.Self+equipped | Description$ As long as CARDNAME is equipped, each creature you control that's a Soldier or a Knight gets +1/+1.\n" +
	"Oracle:Equip costs you pay cost {1} less.\n"

const kiyomaroSrc = "Name:Kiyomaro, First to Stand\nManaCost:3 W W\nTypes:Legendary Creature Spirit\nPT:*/*\n" +
	"S:Mode$ Continuous | Affected$ Card.Self | AddKeyword$ Vigilance | CheckSVar$ X | SVarCompare$ GE4 | Description$ As long as you have four or more cards in hand, NICKNAME has vigilance.\n" +
	"SVar:X:Count$ValidHand Card.YouOwn\n" +
	"Oracle:As long as you have four or more cards in hand, Kiyomaro has vigilance.\n"

// TestContinuousIsPresentGateTurnsTheGrantOnAndOff drives Angelic Overseer's
// real IsPresent$ gate -- Human.YouCtrl -- across a full off/on/off cycle:
// the hexproof/indestructible grant is absent with no Human on the
// battlefield, present once one enters, and absent again the moment it
// leaves, all on the same engine with no re-registration.
func TestContinuousIsPresentGateTurnsTheGrantOnAndOff(t *testing.T) {
	t.Parallel()
	e, _, overseer := newFixtureDeck(t, 61, angelicOverseerSrc, "Name:Human\nManaCost:W\nTypes:Creature Human Soldier\nPT:1/1\nOracle:x\n")
	e.emit(events.Event{Kind: events.MoveZone, Obj: overseer, From: state.ZHand, To: state.ZBattlefield})
	if e.HasKeyword(overseer, "Hexproof") || e.HasKeyword(overseer, "Indestructible") {
		t.Fatalf("grant applied with no Human on the battlefield (hexproof %v indestructible %v)",
			e.HasKeyword(overseer, "Hexproof"), e.HasKeyword(overseer, "Indestructible"))
	}
	human := putCreature(t, e, 0, "Name:Human\nManaCost:W\nTypes:Creature Human Soldier\nPT:1/1\nOracle:x\n")
	if !e.HasKeyword(overseer, "Hexproof") || !e.HasKeyword(overseer, "Indestructible") {
		t.Fatalf("grant absent with a Human on the battlefield (hexproof %v indestructible %v)",
			e.HasKeyword(overseer, "Hexproof"), e.HasKeyword(overseer, "Indestructible"))
	}
	// The Human leaves: the gate re-evaluates on the next emitted event and
	// the grant turns back off -- a static recheck, not a registration.
	e.emit(events.Event{Kind: events.MoveZone, Obj: human, From: state.ZBattlefield, To: state.ZGraveyard})
	if e.HasKeyword(overseer, "Hexproof") || e.HasKeyword(overseer, "Indestructible") {
		t.Fatalf("grant still applied after the Human left (hexproof %v indestructible %v)",
			e.HasKeyword(overseer, "Hexproof"), e.HasKeyword(overseer, "Indestructible"))
	}
	// An OPPONENT's Human never satisfies YouCtrl. (onBoard is eventless, so
	// the Tap emit below is the epoch mover that forces the statics re-scan.)
	oh := onBoard(t, e, 1, "Name:Other Human\nManaCost:W\nTypes:Creature Human Soldier\nPT:1/1\nOracle:x\n")
	e.emit(events.Event{Kind: events.Tap, Obj: oh})
	if e.HasKeyword(overseer, "Hexproof") || e.HasKeyword(overseer, "Indestructible") {
		t.Fatal("grant applied for an opponent's Human -- the YouCtrl qualifier was ignored")
	}
}

// TestContinuousIsPresentGateEquippedPredicate drives Auriok Steelshaper's
// real IsPresent$ Card.Self+equipped through the genuine equip flow: the
// +1/+1 to Soldiers and Knights is absent while nothing is attached, present
// once the Equipment attaches, and absent again after the Equipment leaves.
func TestContinuousIsPresentGateEquippedPredicate(t *testing.T) {
	t.Parallel()
	sword := "Name:Gate Sword\nManaCost:3\nTypes:Artifact Equipment\nK:Equip:2\nOracle:x\n"
	e, cfg, sw := newFixtureDeck(t, 61, sword, auriokSteelshaperSrc)
	auriok := putCreature(t, e, 0, auriokSteelshaperSrc)
	if got := e.Power(auriok); got != 1 {
		t.Fatalf("Auriok power = %d, want 1 (unequipped: the IsPresent$ gate withholds the pump)", got)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: sw, From: state.ZHand, To: state.ZBattlefield})
	addMana(t, e, 0, "CC")
	e.Advance()
	opt := abilityOption(t, e, sw, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || len(d.Options) != 1 || d.Options[0].Obj != auriok {
		t.Fatalf("equip target %+v", d)
	}
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(sw).AttachedTo != auriok {
		t.Fatalf("sword attached to %d, want %d", e.G.Obj(sw).AttachedTo, auriok)
	}
	if got := e.Power(auriok); got != 2 {
		t.Fatalf("equipped Auriok power = %d, want 2 (1 base + 1 pump; Auriok is itself a Soldier)", got)
	}
	// The Equipment leaves: the gate fails again and the pump is gone.
	e.emit(events.Event{Kind: events.MoveZone, Obj: sw, From: state.ZBattlefield, To: state.ZGraveyard})
	if got := e.Power(auriok); got != 1 {
		t.Fatalf("Auriok power after the Equipment left = %d, want 1 (gate re-checked)", got)
	}
	replayCheck(t, e, cfg)
}

// TestContinuousCheckSVarGateTurnsTheGrantOnAndOff drives Kiyomaro, First to
// Stand's real CheckSVar$/SVarCompare$ gate -- SVar X = Count$ValidHand
// Card.YouOwn, SVarCompare$ GE4: vigilance is present with four or more
// cards in hand and absent below the threshold, re-checked as the hand
// shrinks.
func TestContinuousCheckSVarGateTurnsTheGrantOnAndOff(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	kiyoCard, ok := reg.Lookup("Kiyomaro, First to Stand")
	if !ok {
		t.Fatal("corpus has no Kiyomaro, First to Stand")
	}
	e := layerEngine(t)
	kiyo := onBoardCard(t, e, 0, kiyoCard)
	hand := e.G.Zone(state.ZHand, 0)
	if len(hand) < 4 {
		t.Fatalf("fixture opening hand = %d cards, want at least 4 for the GE4 gate", len(hand))
	}
	if !e.HasKeyword(kiyo, "Vigilance") {
		t.Fatalf("vigilance absent with %d cards in hand (the GE4 gate should hold)", len(hand))
	}
	// Shrink the hand below four: the gate re-evaluates and the grant turns
	// off -- the same engine, no re-registration.
	for _, id := range append([]state.ObjID(nil), hand[:len(hand)-3]...) {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZGraveyard})
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != 3 {
		t.Fatalf("hand after the moves = %d cards, want 3", got)
	}
	if e.HasKeyword(kiyo, "Vigilance") {
		t.Fatal("vigilance still applied with 3 cards in hand -- the CheckSVar$ gate did not re-check")
	}
}

// deliriumPumpSrc is the synthetic shape every Delirium Continuous carrier
// prints (Grim Flayer's line): a self-pump gated on Condition$ Delirium.
const deliriumPumpSrc = "Name:Delirium Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\n" +
	"S:Mode$ Continuous | Affected$ Card.Self | AddPower$ 2 | AddToughness$ 2 | Condition$ Delirium | Description$ Delirium -- CARDNAME gets +2/+2 as long as there are four or more card types in your graveyard.\n" +
	"Oracle:x\n"

// addToGraveyardType adds one fresh card of the named printed type line
// straight to p's graveyard (eventless placement stales the memos the way
// onBoard does) and returns its id.
func addToGraveyardType(t testing.TB, e *Engine, p state.PlayerID, typeLine string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, "Name:GY "+typeLine+"\nTypes:"+typeLine+"\nOracle:x\n"), p)
	o.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, p, append(e.G.Zone(state.ZGraveyard, p), o.ID))
	e.staticEpoch = -1
	e.activeEpoch = -1
	return o.ID
}

// TestContinuousConditionDeliriumTurnsTheGrantOnAndOff drives a synthetic
// Condition$ Delirium Continuous static across a full off/on/off cycle: the
// +2/+2 is absent with zero and with three distinct graveyard types, present
// at four, and absent again the moment one type leaves the graveyard -- the
// continuous recheck (the same on/off/off shape as the Angelic Overseer test).
func TestContinuousConditionDeliriumTurnsTheGrantOnAndOff(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	bear := onBoard(t, e, 0, deliriumPumpSrc)
	if got := e.Power(bear); got != 2 {
		t.Fatalf("power with an empty graveyard = %d, want 2 (the Delirium grant must not apply)", got)
	}
	a := addToGraveyardType(t, e, 0, "Artifact")
	_ = addToGraveyardType(t, e, 0, "Instant")
	_ = addToGraveyardType(t, e, 0, "Sorcery")
	if got := e.Power(bear); got != 2 {
		t.Fatalf("power with three graveyard types = %d, want 2 (still below the Delirium threshold)", got)
	}
	_ = addToGraveyardType(t, e, 0, "Enchantment")
	if got, tou := e.Power(bear), e.Toughness(bear); got != 4 || tou != 4 {
		t.Fatalf("P/T with four graveyard types = %d/%d, want 4/4 (Delirium holds)", got, tou)
	}
	// One type leaves: the gate re-evaluates on the next emitted event and the
	// grant turns back off -- a continuous recheck, not a registration.
	e.emit(events.Event{Kind: events.MoveZone, Obj: a, From: state.ZGraveyard, To: state.ZExile})
	if got := e.Power(bear); got != 2 {
		t.Fatalf("power after a type left the graveyard = %d, want 2 (gate re-checked)", got)
	}
}

// TestContinuousConditionDeliriumRealCorpusCards asserts the SAME real corpus
// cards BOTH ways -- the over-apply bug is invisible to a one-sided test.
// Deathcap Cultivator has no deathtouch with an empty graveyard and has it at
// four distinct core types; Grim Flayer is 2/2 vs 4/4 across the same swing.
func TestContinuousConditionDeliriumRealCorpusCards(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	deathcap := searchCorpusCard(t, reg, "Deathcap Cultivator")
	grim := searchCorpusCard(t, reg, "Grim Flayer")
	e := layerEngine(t)
	dc := onBoardCard(t, e, 0, deathcap)
	gf := onBoardCard(t, e, 0, grim)
	if e.HasKeyword(dc, "Deathtouch") {
		t.Fatal("Deathcap Cultivator has deathtouch with an empty graveyard -- the Delirium gate was not read")
	}
	if got := e.Power(gf); got != 2 {
		t.Fatalf("Grim Flayer power with an empty graveyard = %d, want 2", got)
	}
	types := []string{"Artifact", "Instant", "Sorcery", "Enchantment"}
	for i, tl := range types {
		addToGraveyardType(t, e, 0, tl)
		if i < 3 {
			if e.HasKeyword(dc, "Deathtouch") || e.Power(gf) != 2 {
				t.Fatalf("delirium applied at %d graveyard types (want 4+): deathtouch %v power %d",
					i+1, e.HasKeyword(dc, "Deathtouch"), e.Power(gf))
			}
		}
	}
	if !e.HasKeyword(dc, "Deathtouch") {
		t.Fatal("Deathcap Cultivator has no deathtouch with four graveyard types -- Delirium did not hold")
	}
	if got, tou := e.Power(gf), e.Toughness(gf); got != 4 || tou != 4 {
		t.Fatalf("Grim Flayer P/T at four types = %d/%d, want 4/4", got, tou)
	}
}

// duskFeasterSrc is Dusk Feaster's real delirioum ReduceCost line; the {2}
// discount must apply at four distinct core types and never below.
const duskFeasterSrc = "Name:Dusk Feaster\nManaCost:5 B B\nTypes:Creature Vampire\nPT:4/5\n" +
	"S:Mode$ ReduceCost | ValidCard$ Card.Self | Type$ Spell | Amount$ 2 | EffectZone$ All | Condition$ Delirium | Description$ Delirium -- This spell costs {2} less to cast if there are four or more card types among cards in your graveyard.\n" +
	"K:Flying\n" +
	"Oracle:x\n"

// TestContinuousConditionDeliriumReduceCostRealCarrier pins the cost direction
// of the same defect: Dusk Feaster's ReduceCost with Condition$ Delirium
// discounts {2} at four distinct types and not one mana below that. The
// graveyard is filled by real seeded moves, so the whole scenario replays.
func TestContinuousConditionDeliriumReduceCostRealCarrier(t *testing.T) {
	t.Parallel()
	types := []string{"Artifact", "Instant", "Sorcery", "Enchantment"}
	extras := make([]string, len(types))
	for i, tl := range types {
		extras[i] = "Name:GY " + tl + "\nManaCost:1\nTypes:" + tl + "\nOracle:x\n"
	}
	e, cfg, feaster := newFixtureDeck(t, 77, duskFeasterSrc, extras...)
	if got := reduceOf(t, e, 0, feaster); got != 0 {
		t.Fatalf("Dusk Feaster reduction with an empty graveyard = %d, want 0", got)
	}
	for i, ex := range extras {
		addToGraveyard(t, e, 0, ex)
		if i < 3 {
			if got := reduceOf(t, e, 0, feaster); got != 0 {
				t.Fatalf("reduction at %d graveyard types = %d, want 0", i+1, got)
			}
		}
	}
	if got := reduceOf(t, e, 0, feaster); got != 2 {
		t.Fatalf("Dusk Feaster reduction at four types = %d, want 2", got)
	}
	replayCheck(t, e, cfg)
}

// TestContinuousConditionTable holds each implemented Condition$ value in its
// true state and denies it in its false state, all through the one
// continuousGateHolds switch.
func TestContinuousConditionTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cond       string
		setupTrue  func(e *Engine)
		setupFalse func(e *Engine)
	}{
		{"PlayerTurn",
			func(e *Engine) { e.G.Active = 0 },
			func(e *Engine) { e.G.Active = 1 }},
		{"NotPlayerTurn",
			func(e *Engine) { e.G.Active = 1 },
			func(e *Engine) { e.G.Active = 0 }},
		{"Metalcraft",
			func(e *Engine) {
				for i := 0; i < 3; i++ {
					onBoard(t, e, 0, "Name:Mox\nManaCost:0\nTypes:Artifact\nOracle:x\n")
				}
			},
			func(e *Engine) {}},
		{"Threshold",
			func(e *Engine) {
				for i := 0; i < 7; i++ {
					addToGraveyardType(t, e, 0, "Sorcery")
				}
			},
			func(e *Engine) {}},
		{"Hellbent",
			func(e *Engine) { e.G.SetZone(state.ZHand, 0, nil); e.staticEpoch = -1 },
			func(e *Engine) {}},
		{"Blessing",
			func(e *Engine) { e.emit(events.Event{Kind: events.BlessingChange, Player: 0}) },
			func(e *Engine) {}},
		{"EnduringStory",
			func(e *Engine) { e.emit(events.Event{Kind: events.EnduringStoryChange, Player: 0}) },
			func(e *Engine) {}},
	}
	for _, tc := range cases {
		t.Run(tc.cond+"/true", func(t *testing.T) {
			e := layerEngine(t)
			bear := onBoard(t, e, 0, "Name:Cond Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\n"+
				"S:Mode$ Continuous | Affected$ Card.Self | AddPower$ 2 | Condition$ "+tc.cond+" | Description$ x\n"+
				"Oracle:x\n")
			tc.setupTrue(e)
			if got := e.Power(bear); got != 4 {
				t.Fatalf("%s: grant absent in the true state (power %d, want 4)", tc.cond, got)
			}
		})
		t.Run(tc.cond+"/false", func(t *testing.T) {
			e := layerEngine(t)
			bear := onBoard(t, e, 0, "Name:Cond Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\n"+
				"S:Mode$ Continuous | Affected$ Card.Self | AddPower$ 2 | Condition$ "+tc.cond+" | Description$ x\n"+
				"Oracle:x\n")
			tc.setupFalse(e)
			if got := e.Power(bear); got != 2 {
				t.Fatalf("%s: grant applied in the false state (power %d, want 2)", tc.cond, got)
			}
		})
	}
}

// TestContinuousConditionUnknownNeverApplies pins the fail-closed direction: a
// Condition$ value this gate does not implement (FatefulHour) never grants.
// Blessing USED to be this test's unknown example and now reads the real
// CR 702.131 latch (ascend1) -- see TestContinuousConditionTable's Blessing
// entry and rules/ascend_test.go.
func TestContinuousConditionUnknownNeverApplies(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	bear := onBoard(t, e, 0, "Name:Comatose Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\n"+
		"S:Mode$ Continuous | Affected$ Card.Self | AddPower$ 2 | Condition$ FatefulHour | Description$ x\n"+
		"Oracle:x\n")
	if got := e.Power(bear); got != 2 {
		t.Fatalf("unimplemented Condition$ FatefulHour granted (power %d, want 2 -- fail closed)", got)
	}
}
