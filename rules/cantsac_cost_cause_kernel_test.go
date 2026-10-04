package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// kr4WardCause is cantsacWardCause with the fixture's held decision dropped
// first, so the ward trigger's resolution runs as a kernel probe that poses
// its asks.
func kr4WardCause(t *testing.T, e *Engine, warded state.ObjID) {
	t.Helper()
	e.pending = nil
	cantsacWardCause(t, e, warded)
}

// kr4UpkeepCumulative is resolveUpkeepCumulative with the held decision
// dropped first.
func kr4UpkeepCumulative(t *testing.T, e *Engine) {
	t.Helper()
	e.pending = nil
	resolveUpkeepCumulative(t, e)
}

// kr4OptionKind returns the index of the first option of kind k, or -1.
func kr4OptionKind(d *decision.Decision, k string) int {
	for _, o := range d.Options {
		if o.Kind == k {
			return o.Index
		}
	}
	return -1
}

// TestCantSacCostCauseTriggeredBlocksWardSacrificeKernel: a `ValidCause$
// Triggered | ForCost$ True` carrier blocks every creature from paying Vein
// Ripper's Ward—Sacrifice a creature; the control board offers and pays it.
func TestCantSacCostCauseTriggeredBlocksWardSacrificeKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	ripper := mustCorpusCard(t, reg, "Vein Ripper")
	warden := card(t, cantsacTrigCreatureFixture)
	payment := card(t, cantsacPaymentCreature)

	ctl, ctlCfg := cantsacWardGame(t, 9501, []*cards.Card{ripper}, []*cards.Card{payment})
	ctlWarded := battlefieldObj(t, ctl, 0, ripper)
	ctlPayment := battlefieldObj(t, ctl, 1, payment)
	kr4WardCause(t, ctl, ctlWarded)
	if len(ctl.Pending().Options) < 2 {
		t.Fatalf("control: ward pay ask has no Pay option: %+v", ctl.Pending().Options)
	}
	submitChoices(t, ctl, 0)
	sac := ctl.Pending()
	if sac == nil || sac.ResumeKind != "ward_sac" {
		t.Fatalf("control: ward did not ask for a sacrifice: %+v", sac)
	}
	if len(tokenOptionsFor(sac, ctlPayment)) != 1 {
		t.Fatalf("control: the payment creature is not offered: %+v", sac.Options)
	}
	submitChoices(t, ctl, tokenOptionsFor(sac, ctlPayment)[0].Index)
	if z := ctl.G.Obj(ctlPayment).Zone; z != state.ZGraveyard {
		t.Fatalf("control: the ward payment creature zone = %s, want graveyard", z)
	}
	kr4Settle(ctl)
	cantsacDrainStack(t, ctl)
	replayCheck(t, ctl, ctlCfg)

	e, cfg := cantsacWardGame(t, 9502, []*cards.Card{ripper}, []*cards.Card{payment, warden})
	warded := battlefieldObj(t, e, 0, ripper)
	payID := battlefieldObj(t, e, 1, payment)
	if !e.sacrificeBlockedForCost(payID, costCauseTriggered) {
		t.Fatal("the ValidCause$ Triggered carrier does not block a trigger-demanded cost sacrifice")
	}
	if e.sacrificeBlockedForCost(payID, costCauseSpell) {
		t.Fatal("the ValidCause$ Triggered carrier blanket-blocks a spell-cast cost sacrifice")
	}
	kr4WardCause(t, e, warded)
	submitChoices(t, e, 0)
	if sac := e.Pending(); sac != nil && sac.ResumeKind == "ward_sac" {
		t.Fatalf("restricted: the ward still asked for a blocked sacrifice: %+v", sac.Options)
	}
	if z := e.G.Obj(payID).Zone; z != state.ZBattlefield {
		t.Fatalf("restricted: the payment creature moved to %s, want battlefield", z)
	}
	replayCheck(t, e, cfg)
}

// TestCantSacCostCauseAngelLeavesWardAloneKernel: Angel of Jubilation's
// `ValidCause$ Spell,Activated` scopes past a ward payment, so the ward still
// asks, offers the payment creature and the payment completes.
func TestCantSacCostCauseAngelLeavesWardAloneKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	ripper := mustCorpusCard(t, reg, "Vein Ripper")
	angel := mustCorpusCard(t, reg, "Angel of Jubilation")
	payment := card(t, cantsacPaymentCreature)

	e, cfg := cantsacWardGame(t, 9503, []*cards.Card{ripper, angel}, []*cards.Card{payment})
	warded := battlefieldObj(t, e, 0, ripper)
	payID := battlefieldObj(t, e, 1, payment)
	if !e.sacrificeBlockedForCost(payID, costCauseSpell) {
		t.Fatal("precondition: Angel's ForCost$ True static is not live on the cost path")
	}
	if e.sacrificeBlockedForCost(payID, costCauseTriggered) {
		t.Fatal("Angel blocked a ward payment, which the ward trigger demands")
	}
	kr4WardCause(t, e, warded)
	submitChoices(t, e, 0)
	sac := e.Pending()
	if sac == nil || sac.ResumeKind != "ward_sac" {
		t.Fatalf("Angel: the ward sacrifice payment was withheld: %+v", sac)
	}
	if len(tokenOptionsFor(sac, payID)) != 1 {
		t.Fatalf("Angel: the ward payment creature is not offered: %+v", sac.Options)
	}
	submitChoices(t, e, tokenOptionsFor(sac, payID)[0].Index)
	if z := e.G.Obj(payID).Zone; z != state.ZGraveyard {
		t.Fatalf("Angel: the ward payment creature zone = %s, want graveyard", z)
	}
	kr4Settle(e)
	cantsacDrainStack(t, e)
	replayCheck(t, e, cfg)
}

// TestCantSacCostCauseTriggeredBlocksCumulativeUpkeepPaymentKernel: on Polar
// Kraken's `Cumulative upkeep:Sac<1/Land>`, the control board pays a land and
// keeps the Kraken; a land-scoped ValidCause$ Triggered carrier makes the
// payment unpayable so only the sacrifice arm is offered and takes the Kraken.
func TestCantSacCostCauseTriggeredBlocksCumulativeUpkeepPaymentKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	kraken := mustCorpusCard(t, reg, "Polar Kraken")
	warden := card(t, cantsacTrigLandFixture)
	mtn := card(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")

	ctl, ctlCfg := cantsacUpkeepGame(t, 9504, []*cards.Card{kraken, mtn, mtn})
	krakenCtl := battlefieldObj(t, ctl, 0, kraken)
	m1 := battlefieldObj(t, ctl, 0, mtn)
	kr4UpkeepCumulative(t, ctl)
	d := ctl.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("control: no cumulative-upkeep ask: %+v", d)
	}
	payIdx := kr4OptionKind(d, "cumulative_pay")
	if payIdx < 0 {
		t.Fatalf("control: the land payment is not offered: %+v", d.Options)
	}
	submitChoices(t, ctl, payIdx)
	sac := ctl.Pending()
	if sac == nil || sac.Kind != decision.KChoose {
		t.Fatalf("control: no land choice ask: %+v", sac)
	}
	submitChoices(t, ctl, kr4Option(t, sac, m1))
	if z := ctl.G.Obj(m1).Zone; z != state.ZGraveyard {
		t.Fatalf("control: the paid land zone = %s, want graveyard", z)
	}
	if z := ctl.G.Obj(krakenCtl).Zone; z != state.ZBattlefield {
		t.Fatalf("control: Polar Kraken left the battlefield: %s", z)
	}
	replayCheck(t, ctl, ctlCfg)

	e, cfg := cantsacUpkeepGame(t, 9505, []*cards.Card{kraken, mtn, mtn, warden})
	krakenID := battlefieldObj(t, e, 0, kraken)
	m1 = battlefieldObj(t, e, 0, mtn)
	if !e.sacrificeBlockedForCost(m1, costCauseTriggered) {
		t.Fatal("the carrier does not block a trigger-demanded land sacrifice")
	}
	if e.sacrificeBlockedForCost(m1, costCauseSpell) {
		t.Fatal("the carrier blanket-blocks a spell-cast land sacrifice")
	}
	if e.sacrificeBlockedForCost(krakenID, costCauseTriggered) {
		t.Fatal("the carrier's ValidCard$ Land scope caught Polar Kraken itself")
	}
	kr4UpkeepCumulative(t, e)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 1 || d.Options[0].Kind != "cumulative_sac" {
		t.Fatalf("restricted: the upkeep ask is not the sacrifice-only arm: %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	if z := e.G.Obj(krakenID).Zone; z != state.ZGraveyard {
		t.Fatalf("restricted: unpayable upkeep left Polar Kraken in %s, want graveyard", z)
	}
	replayCheck(t, e, cfg)
}

// TestCantSacCostCauseAngelLeavesUpkeepAloneKernel: on Phyrexian Soulgorger's
// `Cumulative upkeep:Sac<1/Creature>`, Angel of Jubilation leaves the
// trigger-demanded payment alone: the bear and the Soulgorger are offered and
// the bear pays.
func TestCantSacCostCauseAngelLeavesUpkeepAloneKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	soulgorger := mustCorpusCard(t, reg, "Phyrexian Soulgorger")
	angel := mustCorpusCard(t, reg, "Angel of Jubilation")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")

	e, cfg := cantsacUpkeepGame(t, 9506, []*cards.Card{soulgorger, angel, bear})
	bearID := battlefieldObj(t, e, 0, bear)
	soulID := battlefieldObj(t, e, 0, soulgorger)
	if !e.sacrificeBlockedForCost(bearID, costCauseSpell) {
		t.Fatal("precondition: Angel's ForCost$ True static is not live on the cost path")
	}
	if e.sacrificeBlockedForCost(bearID, costCauseTriggered) {
		t.Fatal("Angel blocked an upkeep payment, which the upkeep trigger demands")
	}
	kr4UpkeepCumulative(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("Angel: no cumulative-upkeep ask: %+v", d)
	}
	payIdx := kr4OptionKind(d, "cumulative_pay")
	if payIdx < 0 {
		t.Fatalf("Angel: the upkeep payment is not offered: %+v", d.Options)
	}
	submitChoices(t, e, payIdx)
	sac := e.Pending()
	if sac == nil || sac.Kind != decision.KChoose {
		t.Fatalf("Angel: no creature choice ask: %+v", sac)
	}
	if len(tokenOptionsFor(sac, bearID)) != 1 || len(tokenOptionsFor(sac, soulID)) != 1 {
		t.Fatalf("Angel: bear %d / Soulgorger %d not both offered: %+v", bearID, soulID, sac.Options)
	}
	submitChoices(t, e, tokenOptionsFor(sac, bearID)[0].Index)
	if z := e.G.Obj(bearID).Zone; z != state.ZGraveyard {
		t.Fatalf("Angel: the paid creature zone = %s, want graveyard", z)
	}
	if z := e.G.Obj(soulID).Zone; z != state.ZBattlefield {
		t.Fatalf("Angel: Phyrexian Soulgorger left the battlefield: %s", z)
	}
	replayCheck(t, e, cfg)
}

// TestCantSacCostCauseAngelLeavesUnlessAloneKernel: on Tresserhorn's Lord,
// Returned's `UnlessCost$ Sac<3/Creature>`, an unless payment is a resolution
// election Angel cannot name: the pay election and the creatures are offered
// and the three creatures are paid.
func TestCantSacCostCauseAngelLeavesUnlessAloneKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 744)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Angel of Jubilation"))
	lord := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Tresserhorn's Lord, Returned"))
	creatures := []state.ObjID{
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears")),
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Goblin Piledriver")),
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears")),
	}
	if !e.sacrificeBlockedForCost(creatures[0], costCauseSpell) {
		t.Fatal("precondition: Angel's ForCost$ True static is not live on the cost path")
	}
	if e.sacrificeBlockedForCost(creatures[0], costCauseResolution) {
		t.Fatal("Angel blocked an unless payment, a resolution election")
	}
	e.emit(events.Event{Kind: events.TriggerPush, Obj: lord, Player: 0, Amount: 0})
	ability := e.G.Stack[len(e.G.Stack)-1]
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: ability, Player: 1, Amount: 1})
	e.pending = nil
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.ResumeKind != "unless_pay" || len(d.Options) < 2 {
		t.Fatalf("Angel: the unless pay election was withheld: %+v", d)
	}
	answerUnlessPay(t, e, true)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "unless_cost" || d.Min != 3 {
		t.Fatalf("Angel: the unless cost choice was withheld: %+v", d)
	}
	choices := make([]int, 0, len(creatures))
	for _, want := range creatures {
		opts := tokenOptionsFor(d, want)
		if len(opts) != 1 {
			t.Fatalf("Angel: cost creature %d not offered: %+v", want, d.Options)
		}
		choices = append(choices, opts[0].Index)
	}
	submitChoices(t, e, choices...)
	for _, id := range creatures {
		if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
			t.Fatalf("Angel: the paid cost creature %d zone = %s, want graveyard", id, z)
		}
	}
}
