package rules

// Saddle (CR 702.171, task agent-20260929T014717Z-0f0a84be).
//
// The printed `K:Saddle:<N>` line expands (cards/kw_saddle.go) into one
// AB$ AlterAttribute ability whose cost is the same tap-any-number group
// predicate Crew uses and whose effect marks the Mount with the CR 702.171b
// "saddled" designation. The designation is a turn stamp on state.Object
// folded by events.Apply's AlterAttribute case (from g.Turn, never carried on
// the event, so replay re-derives it), read back by the effects.IsSaddled
// predicate, cleared when the permanent leaves the battlefield, and
// deliberately NOT cleared by a controller change.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// saddleExtraCards returns the corpus Runeclaw Bear twice, so each Saddle
// fixture can seed two saddle-payment creatures in seat 0's deck.
func saddleExtraCards(t *testing.T, reg *cards.Registry) []*cards.Card {
	t.Helper()
	return []*cards.Card{lookup(t, reg, "Runeclaw Bear"), lookup(t, reg, "Runeclaw Bear")}
}

// saddleBears moves the two seeded Runeclaw Bears onto seat 0's battlefield
// and clears their summoning sickness so they can be tapped as the saddle
// cost without the sickness flag confounding an assertion. It returns the two
// ids.
func saddleBears(t *testing.T, e *Engine) (state.ObjID, state.ObjID) {
	t.Helper()
	b1 := moveByName(t, e, 0, "Runeclaw Bear", state.ZBattlefield)
	b2 := moveByName(t, e, 0, "Runeclaw Bear", state.ZBattlefield)
	for _, id := range []state.ObjID{b1, b2} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: bear %d is not on the battlefield", id)
		}
		e.G.Obj(id).SummonSick = false
	}
	return b1, b2
}

// saddleElection submits the object's ability index 0 and returns the KChoose
// tap election it poses, asserting the total-power floor the Saddle keyword
// carries.
func saddleElection(t *testing.T, e *Engine, id state.ObjID, minSum int) *decision.Decision {
	t.Helper()
	e.pending = nil
	e.priorityRound()
	opt := abilityOption(t, e, id, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("pending decision %+v, want the saddle tap election (KChoose)", d)
	}
	if d.MinSum != minSum {
		t.Fatalf("saddle tap election MinSum = %d, want %d", d.MinSum, minSum)
	}
	return d
}

// TestSaddleVenomsacLagacAttackTrigger drives the REAL corpus Saddle carrier
// end to end. Venomsac Lagac is a 2/1 Deathtouch Mount with `K:Saddle:2` and
// "Whenever this creature attacks while saddled, it gets +0/+3 until end of
// turn." Before this task the printed keyword minted no ability at all
// (kw:Saddle unsupported), so it could never be saddled and the attack
// trigger could never fire. This exercises the expander, the designation
// fold, the IsSaddled predicate and the attack trigger in one path.
func TestSaddleVenomsacLagacAttackTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	lagac := lookup(t, reg, "Venomsac Lagac")
	extras := append([]*cards.Card{lagac}, saddleExtraCards(t, reg)...)
	e := corpusEngine(t, reg, extras, nil)
	lagacID := moveByName(t, e, 0, "Venomsac Lagac", state.ZBattlefield)
	e.G.Obj(lagacID).SummonSick = false
	b1, b2 := saddleBears(t, e)

	// Preconditions: the Lagac is on the battlefield with its printed 2/1,
	// unsaddled, and the printed keyword really did mint an ability.
	if o := e.G.Obj(lagacID); o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Lagac is in zone %v, want battlefield", o.Zone)
	}
	if e.Power(lagacID) != 2 || e.Toughness(lagacID) != 1 {
		t.Fatalf("precondition: Lagac is %d/%d, want 2/1", e.Power(lagacID), e.Toughness(lagacID))
	}
	if e.G.Obj(lagacID).SaddledTurn != 0 {
		t.Fatalf("precondition: Lagac starts saddled (SaddledTurn %d)", e.G.Obj(lagacID).SaddledTurn)
	}
	if len(lagac.Faces[0].Abilities) == 0 {
		t.Fatal("precondition: printed K:Saddle:2 did not mint an ability")
	}
	if effects.MatchesObjectCtx(e.G, "Creature.IsSaddled", e.G.Obj(lagacID), effects.SpecContext{}) {
		t.Fatal("precondition: the IsSaddled predicate is already true")
	}

	// Activate Saddle 2 in Main1 (CR 702.171a: only as a sorcery), paying
	// with the two bears' total power 2.
	d := saddleElection(t, e, lagacID, 2)
	var choices []int
	for _, o := range d.Options {
		if o.Obj == b1 || o.Obj == b2 {
			choices = append(choices, o.Index)
		}
	}
	if len(choices) != 2 {
		t.Fatalf("saddle election offered %d bears, want 2: %+v", len(choices), d.Options)
	}
	submitChoices(t, e, choices...)
	passUntilStackEmpty(t, e, 20)

	for _, id := range []state.ObjID{b1, b2} {
		if !e.G.Obj(id).Tapped {
			t.Fatalf("precondition: bear %d was not tapped as the saddle cost", id)
		}
	}
	if got := e.G.Obj(lagacID).SaddledTurn; got != e.G.Turn {
		t.Fatalf("after Saddle resolves SaddledTurn = %d, want the current turn %d", got, e.G.Turn)
	}
	if !effects.MatchesObjectCtx(e.G, "Creature.IsSaddled", e.G.Obj(lagacID), effects.SpecContext{}) {
		t.Fatal("IsSaddled does not match a creature the Saddle ability just saddled")
	}

	// Advance to the declare-attackers step and attack with the Lagac.
	driveToStep(t, e, e.G.Turn, 0, state.StepDeclareAttackers)
	d = e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("attacker declaration = %+v, want KAttackers", d)
	}
	attack := -1
	for _, o := range d.Options {
		if o.Obj == lagacID {
			attack = o.Index
			break
		}
	}
	if attack < 0 {
		t.Fatal("precondition: Lagac is not offered as an attacker")
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{attack}}); err != nil {
		t.Fatalf("declare Lagac attacker: %v", err)
	}
	if len(e.G.Stack) == 0 {
		t.Fatal("precondition: the saddled attack trigger did not go on the stack")
	}
	passUntilStackEmpty(t, e, 30)
	if got := e.Toughness(lagacID); got != 4 {
		t.Fatalf("saddled attack trigger toughness = %d, want 4 (1 + 3)", got)
	}
}

// TestSaddleSorcerySpeedGate pins CR 702.171a's "activate only as a sorcery":
// with a spell on the stack the Saddle ability is not offered.
func TestSaddleSorcerySpeedGate(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	lagac := lookup(t, reg, "Venomsac Lagac")
	bolt := lookup(t, reg, "Lightning Bolt")
	extras := append([]*cards.Card{lagac, bolt}, saddleExtraCards(t, reg)...)
	e := corpusEngine(t, reg, extras, nil)
	lagacID := moveByName(t, e, 0, "Venomsac Lagac", state.ZBattlefield)
	e.G.Obj(lagacID).SummonSick = false
	saddleBears(t, e)

	// Offered on the empty-stack sorcery-speed window.
	e.pending = nil
	e.priorityRound()
	if _, offered := findAbilityOption(e, lagacID, 0); !offered {
		t.Fatal("precondition: Saddle is not offered on the empty-stack main phase")
	}

	// Put Lightning Bolt on the stack and confirm Saddle is withheld.
	boltID := moveByName(t, e, 0, "Lightning Bolt", state.ZHand)
	addMana(t, e, 0, "R")
	idx := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == boltID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Lightning Bolt: %+v", e.Pending().Options)
	}
	submitChoices(t, e, idx)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget && len(d.Options) > 0 {
		submitChoices(t, e, d.Options[0].Index)
	}
	if len(e.G.Stack) == 0 {
		t.Fatal("precondition: Lightning Bolt did not reach the stack")
	}
	if _, offered := findAbilityOption(e, lagacID, 0); offered {
		t.Fatal("Saddle was offered with a spell on the stack; CR 702.171a says only as a sorcery")
	}
}

// TestSaddleDesignationSurvivesControlChangeAndEndsOnLeave pins CR 702.171b's
// scope: the saddled designation is NOT cleared by a controller change but
// ends when the permanent leaves the battlefield.
func TestSaddleDesignationSurvivesControlChangeAndEndsOnLeave(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	mountSrc := "Name:Saddle Test Mount\nManaCost:2\nTypes:Creature Mount\nPT:2/2\n" +
		"K:Saddle:2\nOracle:Saddle 2\n"
	extras := append([]*cards.Card{card(t, mountSrc)}, saddleExtraCards(t, reg)...)
	e := corpusEngine(t, reg, extras, nil)
	mountID := moveByName(t, e, 0, "Saddle Test Mount", state.ZBattlefield)
	e.G.Obj(mountID).SummonSick = false
	b1, b2 := saddleBears(t, e)

	if len(e.G.Obj(mountID).Face().Abilities) == 0 {
		t.Fatal("precondition: hand-authored K:Saddle:2 did not mint an ability")
	}
	if e.G.Obj(mountID).SaddledTurn != 0 {
		t.Fatal("precondition: the test Mount starts saddled")
	}

	d := saddleElection(t, e, mountID, 2)
	var choices []int
	for _, o := range d.Options {
		if o.Obj == b1 || o.Obj == b2 {
			choices = append(choices, o.Index)
		}
	}
	submitChoices(t, e, choices...)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(mountID).SaddledTurn == 0 {
		t.Fatal("precondition: the Mount was not saddled")
	}

	// A controller change must NOT clear the designation.
	e.emit(events.Event{Kind: events.ControlChange, Obj: mountID, Player: 1})
	if e.G.Obj(mountID).Controller != 1 {
		t.Fatalf("precondition: control did not actually change (controller %d)", e.G.Obj(mountID).Controller)
	}
	if e.G.Obj(mountID).SaddledTurn == 0 {
		t.Fatal("the saddled designation was cleared by a controller change; CR 702.171b keeps it")
	}

	// Leaving the battlefield ends it.
	e.emit(events.Event{Kind: events.MoveZone, Obj: mountID, From: state.ZBattlefield, To: state.ZGraveyard})
	if e.G.Obj(mountID).SaddledTurn != 0 {
		t.Fatal("the saddled designation survived leaving the battlefield; CR 702.171b ends it")
	}
}

// TestSaddlePrintedCarrierReachesTapPowerValueKind proves the printed
// K:Saddle:<N> expansion's `Keyword$ Saddle` tag reaches rules/statics.go's
// tapCostSAKind through a REAL corpus printed carrier (not the hand-written
// SA TestTapPowerValueSaddleReadsItsOwnKind uses). Cloudspire Captain reads
// its own power as 2 greater when it saddles a Mount (ValidSA$ Saddle-scoped,
// ValidCard$ Card.Self): plain power 2, Saddle value 4.
func TestSaddlePrintedCarrierReachesTapPowerValueKind(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	lagac := lookup(t, reg, "Venomsac Lagac")
	captain := lookup(t, reg, "Cloudspire Captain")
	extras := append([]*cards.Card{lagac, captain}, saddleExtraCards(t, reg)...)
	e := corpusEngine(t, reg, extras, nil)
	lagacID := moveByName(t, e, 0, "Venomsac Lagac", state.ZBattlefield)
	captainID := moveByName(t, e, 0, "Cloudspire Captain", state.ZBattlefield)
	for _, id := range []state.ObjID{lagacID, captainID} {
		e.G.Obj(id).SummonSick = false
	}

	// Precondition: the Captain really is 2/3 and its own Saddle-scoped static
	// reads it as 4 (power 2 + 2), while its plain power is 2.
	if p, tough := e.Power(captainID), e.Toughness(captainID); p != 2 || tough != 3 {
		t.Fatalf("precondition: Cloudspire Captain is %d/%d, want 2/3", p, tough)
	}
	if got := e.tapPowerValue(captainID, "Saddle"); got != 4 {
		t.Fatalf("precondition: Cloudspire Captain's Saddle value = %d, want 4 (its +2 static is not scoped to Saddle)", got)
	}

	// The printed K:Saddle:2 ability's election must carry the Captain at 4,
	// proving the minted `Keyword$ Saddle` tag reached tapCostSAKind.
	d := saddleElection(t, e, lagacID, 2)
	captainIdx, captainValue := -1, -1
	for _, o := range d.Options {
		if o.Obj == captainID {
			captainIdx, captainValue = o.Index, o.Value
		}
	}
	if captainIdx < 0 {
		t.Fatalf("Cloudspire Captain was not offered as a saddle candidate: %+v", d.Options)
	}
	if captainValue != 4 {
		t.Fatalf("Captain's saddle Option.Value = %d, want 4 (its printed-power 2 read as 2 greater)", captainValue)
	}
	// A lone Captain pays the Saddle 2 floor (4 >= 2).
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{captainIdx}}); err != nil {
		t.Fatalf("submitting the Captain's saddle election: %v", err)
	}
	passUntilStackEmpty(t, e, 20)
	if !e.G.Obj(captainID).Tapped {
		t.Fatal("Cloudspire Captain was not tapped as the saddle cost")
	}
	if e.G.Obj(lagacID).SaddledTurn != e.G.Turn {
		t.Fatal("the printed Saddle ability did not saddle the Lagac")
	}
}
