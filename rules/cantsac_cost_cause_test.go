package rules

// cantsac1 r2: the COST-path cause semantics for the three cost sites r1
// left on costCauseNone, defined per the costCause table in rules/layers.go:
//
//   - a ward cost is demanded by the ward trigger (CR 702.22), so its cause
//     is costCauseTriggered;
//   - a cumulative-upkeep payment is demanded by the upkeep trigger
//     (CR 702.25a), so its Sac arm's candidate walk is costCauseTriggered --
//     and that walk consults the CantSacrifice gate at all now (the
//     cumulative-upkeep/echo Sac action path never did);
//   - an unless payment is a resolution-election payment, never a cast or
//     activation cost, so its cause is costCauseResolution, which no
//     readable ValidCause$ base admits (fail closed, permissive).
//
// Consequence for the corpus's two ForCost$ True carriers (Angel of
// Jubilation, Yasharn -- both `ValidCause$ Spell,Activated`): they scope to
// the cast/activation cost sites only and correctly do NOT block a ward,
// unless or upkeep payment. The trigger-demand enforcement itself is pinned
// by hand-built `ValidCause$ Triggered` carriers -- the corpus has none, so
// they are fixture-only, the same way staticBearFixture is.
//
// Every corpus leaf drives a REAL corpus card through the ordinary
// offer/payment paths; the ward and upkeep leaves end replay-verified (leaf
// E mirrors the existing TestUnlessCostTresserhornPaysSacLifeAndDraw drive,
// whose board is placed eventlessly, so it carries the same no-replayCheck
// shape that test has). Each leaf asserts its own precondition: the objects
// are in the zones the rule reads, the control board offers the payment the
// restricted board must change, and the unit-level discrimination asserts
// run against the carrier's REAL static params so a reverted registration
// fails loudly.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// cantsacTrigCreatureFixture blocks every creature from a cost sacrifice a
// triggered ability demands. Fixture-only: the corpus's ForCost$ True
// carriers are both `ValidCause$ Spell,Activated`.
const cantsacTrigCreatureFixture = "Name:Ward Warden\nManaCost:1 W\nTypes:Creature Human Cleric\nPT:1/1\n" +
	"S:Mode$ CantSacrifice | ValidCard$ Creature | ValidCause$ Triggered | ForCost$ True | Description$ Creatures can't be sacrificed to pay a cost a triggered ability demands.\nOracle:x\n"

// cantsacTrigLandFixture blocks every land from a cost sacrifice a triggered
// ability demands -- the Polar Kraken leaf's carrier, scoped to lands so the
// upkeep trigger's forced settle (which sacrifices the Kraken itself) is not
// contested by the static.
const cantsacTrigLandFixture = "Name:Upkeep Warden\nManaCost:1 W\nTypes:Creature Human Cleric\nPT:1/1\n" +
	"S:Mode$ CantSacrifice | ValidCard$ Land | ValidCause$ Triggered | ForCost$ True | Description$ Lands can't be sacrificed to pay a cost a triggered ability demands.\nOracle:x\n"

// cantsacPaymentCreature is the ward leaf's sacrifice-candidate fixture.
const cantsacPaymentCreature = "Name:Payment\nTypes:Creature\nPT:1/1\nOracle:x\n"

// cantsacWardGame parks a two-seat game at turn-2 Main 1 with both boards
// placed through emitted moves (so the game replays) and seat 1's targeting
// prop still in its library. The ward leaves drive the targeting spell the
// way rules/combat_keywords_test.go's ward tests do: the cause is PutOnStack
// + TargetsChosen seed events, so no cast/mana plumbing is involved and the
// ward trigger is the only machinery under test.
func cantsacWardGame(t *testing.T, seed uint64, board0, board1 []*cards.Card) (*Engine, Config) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append(append([]*cards.Card{}, board0...), mountainDeck(t, 40-len(board0))...),
			append(append([]*cards.Card{}, board1...), mountainDeck(t, 40-len(board1))...),
		}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	for seat, bs := range [][]*cards.Card{board0, board1} {
		want := make(map[*cards.Card]bool, len(bs))
		for _, c := range bs {
			want[c] = true
		}
		moved := 0
		for _, id := range append(append([]state.ObjID(nil), e.G.Zone(state.ZHand, state.PlayerID(seat))...),
			e.G.Zone(state.ZLibrary, state.PlayerID(seat))...) {
			o := e.G.Obj(id)
			if o == nil || !want[o.Card] {
				continue
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZBattlefield})
			moved++
		}
		if moved != len(bs) {
			t.Fatalf("seat %d: only %d of %d board cards dealt", seat, moved, len(bs))
		}
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	return e, cfg
}

// cantsacWardCause seeds a targeting spell from seat 1's library at the
// warded creature and resolves the queue, leaving the engine at the ward pay
// election (asserted, so a board that never asks fails loudly).
func cantsacWardCause(t *testing.T, e *Engine, warded state.ObjID) {
	t.Helper()
	cause := e.G.Zone(state.ZLibrary, 1)[0]
	e.emit(events.Event{Kind: events.PutOnStack, Obj: cause, Player: 1, From: state.ZLibrary, To: state.ZStack})
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: cause, IDs: []state.ObjID{warded}})
	e.putTriggersOnStack()
	e.resolveTop()
	if d := e.Pending(); d == nil || d.ResumeKind != "unless_pay" {
		t.Fatalf("the ward pay election did not surface: %+v", d)
	}
}

// cantsacDrainStack drives the engine after a ward payment: the ward's paid
// sacrifice kills the payment creature, whose death fires Vein Ripper's own
// drain trigger (its target ask is answered with its only option), and the
// priority passes resolve what is left of the stack.
func cantsacDrainStack(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 60; i++ {
		if len(e.G.Stack) == 0 {
			return
		}
		d := e.Pending()
		if d == nil {
			return
		}
		switch d.Kind {
		case decision.KPriority:
			passPriorityOnce(t, e)
		case decision.KTarget:
			if len(d.Options) == 0 {
				t.Fatalf("target ask with no options: %+v", d)
			}
			submitChoices(t, e, d.Options[0].Index)
		default:
			t.Fatalf("unexpected %v decision while draining the stack: %+v", d.Kind, d)
		}
	}
	t.Fatal("the stack never drained")
}

// TestCantSacCostCauseTriggeredBlocksWardSacrifice is the ward positive: a
// `ValidCause$ Triggered | ForCost$ True` carrier blocks every creature from
// paying Vein Ripper's `Ward—Sacrifice a creature`, so the ward is unpayable
// and the targeting spell is countered. The control board (no carrier)
// offers the ward's sacrifice payment, so the restriction's absence is the
// only difference.
func TestCantSacCostCauseTriggeredBlocksWardSacrifice(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	ripper := mustCorpusCard(t, reg, "Vein Ripper")
	warden := card(t, cantsacTrigCreatureFixture)
	payment := card(t, cantsacPaymentCreature)

	// Control: without the carrier the ward asks and the payment creature is
	// offered.
	ctl, ctlCfg := cantsacWardGame(t, 9501, []*cards.Card{ripper}, []*cards.Card{payment})
	ctlWarded := battlefieldObj(t, ctl, 0, ripper)
	ctlPayment := battlefieldObj(t, ctl, 1, payment)
	cantsacWardCause(t, ctl, ctlWarded)
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
	submitChoices(t, ctl, 0)
	if z := ctl.G.Obj(ctlPayment).Zone; z != state.ZGraveyard {
		t.Fatalf("control: the ward payment creature zone = %s, want graveyard", z)
	}
	cantsacDrainStack(t, ctl)
	replayCheck(t, ctl, ctlCfg)

	// Restricted: the ValidCause$ Triggered carrier blocks the ward's cost
	// sacrifice, the ward is unpayable, and the targeting spell is countered.
	e, cfg := cantsacWardGame(t, 9502, []*cards.Card{ripper}, []*cards.Card{payment, warden})
	warded := battlefieldObj(t, e, 0, ripper)
	payID := battlefieldObj(t, e, 1, payment)
	if o := e.G.Obj(payID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the payment creature is not on the battlefield: %+v", o)
	}
	// The carrier's real static params through the real body: a triggered
	// demand is blocked, a spell-cast cost is not -- the cause filter, not a
	// blanket block.
	if !e.sacrificeBlockedForCost(payID, costCauseTriggered) {
		t.Fatal("the ValidCause$ Triggered carrier does not block a trigger-demanded cost sacrifice")
	}
	if e.sacrificeBlockedForCost(payID, costCauseSpell) {
		t.Fatal("the ValidCause$ Triggered carrier blanket-blocks a spell-cast cost sacrifice")
	}
	cantsacWardCause(t, e, warded)
	submitChoices(t, e, 0) // elect Pay: beginWardPayment must find no candidate
	if sac := e.Pending(); sac != nil && sac.ResumeKind == "ward_sac" {
		t.Fatalf("restricted: the ward still asked for a blocked sacrifice: %+v", sac.Options)
	}
	if z := e.G.Obj(payID).Zone; z != state.ZBattlefield {
		t.Fatalf("restricted: the payment creature moved to %s, want battlefield", z)
	}
	replayCheck(t, e, cfg)
}

// TestCantSacCostCauseAngelLeavesWardAlone is the corpus negative for the
// same site: Angel of Jubilation (`ForCost$ True | ValidCause$
// Spell,Activated`) scopes PAST a ward payment -- the ward trigger demands
// it, which is neither a spell cast nor an ability activation -- so the ward
// still asks and the payment creature is still offered. The unit asserts
// prove the static is live and the cause filter (not a dead registration)
// is what leaves the ward alone.
func TestCantSacCostCauseAngelLeavesWardAlone(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	ripper := mustCorpusCard(t, reg, "Vein Ripper")
	angel := mustCorpusCard(t, reg, "Angel of Jubilation")
	payment := card(t, cantsacPaymentCreature)

	e, cfg := cantsacWardGame(t, 9503, []*cards.Card{ripper, angel}, []*cards.Card{payment})
	warded := battlefieldObj(t, e, 0, ripper)
	payID := battlefieldObj(t, e, 1, payment)
	if o := e.G.Obj(payID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the payment creature is not on the battlefield: %+v", o)
	}
	if !e.sacrificeBlockedForCost(payID, costCauseSpell) {
		t.Fatal("precondition: Angel's ForCost$ True static is not live on the cost path")
	}
	if e.sacrificeBlockedForCost(payID, costCauseTriggered) {
		t.Fatal("Angel's ValidCause$ Spell,Activated blocked a ward payment, which the ward trigger demands")
	}
	cantsacWardCause(t, e, warded)
	submitChoices(t, e, 0)
	sac := e.Pending()
	if sac == nil || sac.ResumeKind != "ward_sac" {
		t.Fatalf("Angel: the ward sacrifice payment was withheld: %+v", sac)
	}
	if len(tokenOptionsFor(sac, payID)) != 1 {
		t.Fatalf("Angel: the ward payment creature is not offered: %+v", sac.Options)
	}
	submitChoices(t, e, 0)
	if z := e.G.Obj(payID).Zone; z != state.ZGraveyard {
		t.Fatalf("Angel: the ward payment creature zone = %s, want graveyard", z)
	}
	cantsacDrainStack(t, e)
	replayCheck(t, e, cfg)
}

// cantsacUpkeepGame parks seat 0 at turn-2 Main 1 with the board cards on
// the battlefield through emitted moves (so the game replays).
func cantsacUpkeepGame(t *testing.T, seed uint64, board []*cards.Card) (*Engine, Config) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{append(append([]*cards.Card{}, board...), mountainDeck(t, 40-len(board))...), mountainDeck(t, 40)},
		Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	want := make(map[*cards.Card]bool, len(board))
	for _, c := range board {
		want[c] = true
	}
	moved := 0
	for _, id := range append(append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...), e.G.Zone(state.ZLibrary, 0)...) {
		o := e.G.Obj(id)
		if o == nil || !want[o.Card] {
			continue
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZBattlefield})
		moved++
	}
	if moved != len(board) {
		t.Fatalf("seat 0: only %d of %d board cards dealt", moved, len(board))
	}
	return e, cfg
}

// TestCantSacCostCauseTriggeredBlocksCumulativeUpkeepPayment is the upkeep
// positive, on Polar Kraken's real `K:Cumulative upkeep:Sac<1/Land>`: a
// `ValidCause$ Triggered | ForCost$ True` carrier (scoped to lands, so the
// forced settle's sacrifice of the Kraken itself is uncontested) makes the
// land payment unpayable, and the upkeep trigger's own sacrifice arm takes
// the Kraken. The control board pays a real land and keeps the Kraken.
func TestCantSacCostCauseTriggeredBlocksCumulativeUpkeepPayment(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	kraken := mustCorpusCard(t, reg, "Polar Kraken")
	warden := card(t, cantsacTrigLandFixture)
	mtn := card(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")

	// Control: two mountains on the battlefield, the land payment payable.
	ctl, ctlCfg := cantsacUpkeepGame(t, 9504, []*cards.Card{kraken, mtn, mtn})
	krakenCtl := battlefieldObj(t, ctl, 0, kraken)
	m1 := battlefieldObj(t, ctl, 0, mtn)
	resolveUpkeepCumulative(t, ctl)
	d := ctl.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("control: no cumulative-upkeep ask: %+v", d)
	}
	payIdx := -1
	for _, o := range d.Options {
		if o.Kind == "cumulative_pay" {
			payIdx = o.Index
		}
	}
	if payIdx < 0 {
		t.Fatalf("control: the land payment is not offered: %+v", d.Options)
	}
	submitChoices(t, ctl, payIdx)
	sac := ctl.Pending()
	if sac == nil || sac.Kind != decision.KChoose || sac.ResumeKind != "" {
		t.Fatalf("control: no land choice ask: %+v", sac)
	}
	landIdx := -1
	for _, o := range sac.Options {
		if o.Obj == m1 {
			landIdx = o.Index
		}
	}
	if landIdx < 0 {
		t.Fatalf("control: the battlefield mountain is not offered: %+v", sac.Options)
	}
	submitChoices(t, ctl, landIdx)
	if z := ctl.G.Obj(m1).Zone; z != state.ZGraveyard {
		t.Fatalf("control: the paid land zone = %s, want graveyard", z)
	}
	if z := ctl.G.Obj(krakenCtl).Zone; z != state.ZBattlefield {
		t.Fatalf("control: Polar Kraken left the battlefield: %s", z)
	}
	replayCheck(t, ctl, ctlCfg)

	// Restricted: the ValidCause$ Triggered carrier blocks both mountains, so
	// the payment is unpayable and only the sacrifice arm remains.
	e, cfg := cantsacUpkeepGame(t, 9505, []*cards.Card{kraken, mtn, mtn, warden})
	krakenID := battlefieldObj(t, e, 0, kraken)
	m1 = battlefieldObj(t, e, 0, mtn)
	if !e.sacrificeBlockedForCost(m1, costCauseTriggered) {
		t.Fatal("the ValidCause$ Triggered carrier does not block a trigger-demanded land sacrifice")
	}
	if e.sacrificeBlockedForCost(m1, costCauseSpell) {
		t.Fatal("the ValidCause$ Triggered carrier blanket-blocks a spell-cast land sacrifice")
	}
	if e.sacrificeBlockedForCost(krakenID, costCauseTriggered) {
		t.Fatal("the carrier's ValidCard$ Land scope caught Polar Kraken itself")
	}
	resolveUpkeepCumulative(t, e)
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

// TestCantSacCostCauseAngelLeavesUpkeepAlone is the corpus negative for the
// upkeep site, on Phyrexian Soulgorger's real
// `K:Cumulative upkeep:Sac<1/Creature>`: Angel of Jubilation's
// `ValidCause$ Spell,Activated` scopes past a payment the upkeep trigger
// demands, so the pay election and the creature candidates are still offered
// and a real creature pays.
func TestCantSacCostCauseAngelLeavesUpkeepAlone(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	soulgorger := mustCorpusCard(t, reg, "Phyrexian Soulgorger")
	angel := mustCorpusCard(t, reg, "Angel of Jubilation")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")

	e, cfg := cantsacUpkeepGame(t, 9506, []*cards.Card{soulgorger, angel, bear})
	bearID := battlefieldObj(t, e, 0, bear)
	soulID := battlefieldObj(t, e, 0, soulgorger)
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the bear is not on the battlefield: %+v", o)
	}
	// Angel's real static is live and discriminates: a spell-cast creature
	// cost is blocked, a trigger-demanded one is not.
	if !e.sacrificeBlockedForCost(bearID, costCauseSpell) {
		t.Fatal("precondition: Angel's ForCost$ True static is not live on the cost path")
	}
	if e.sacrificeBlockedForCost(bearID, costCauseTriggered) {
		t.Fatal("Angel's ValidCause$ Spell,Activated blocked an upkeep payment, which the upkeep trigger demands")
	}
	resolveUpkeepCumulative(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("Angel: no cumulative-upkeep ask: %+v", d)
	}
	payIdx := -1
	for _, o := range d.Options {
		if o.Kind == "cumulative_pay" {
			payIdx = o.Index
		}
	}
	if payIdx < 0 {
		t.Fatalf("Angel: the upkeep payment is not offered: %+v", d.Options)
	}
	submitChoices(t, e, payIdx)
	sac := e.Pending()
	if sac == nil || sac.Kind != decision.KChoose {
		t.Fatalf("Angel: no creature choice ask: %+v", sac)
	}
	if len(tokenOptionsFor(sac, bearID)) != 1 {
		t.Fatalf("Angel: the bear is not offered for the upkeep payment: %+v", sac.Options)
	}
	if len(tokenOptionsFor(sac, soulID)) != 1 {
		t.Fatalf("Angel: the Soulgorger itself is not offered: %+v", sac.Options)
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

// TestCantSacCostCauseAngelLeavesUnlessAlone is the corpus negative for the
// unless site, on Tresserhorn's Lord, Returned's real
// `UnlessCost$ Sac<3/Creature>`: an unless payment is a resolution-election
// payment (costCauseResolution), which Angel's `ValidCause$ Spell,Activated`
// cannot name, so the pay election and the creature candidates are still
// offered and the payment completes. The board shape is the existing
// TestUnlessCostTresserhornPaysSacLifeAndDraw drive's, which places its
// board eventlessly and therefore carries no replayCheck, like that test.
func TestCantSacCostCauseAngelLeavesUnlessAlone(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 744)
	angel := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Angel of Jubilation"))
	lord := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Tresserhorn's Lord, Returned"))
	creatures := []state.ObjID{
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears")),
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Goblin Piledriver")),
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears")),
	}
	if o := e.G.Obj(creatures[0]); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the cost creature is not on the battlefield: %+v", o)
	}
	if o := e.G.Obj(angel); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Angel of Jubilation is not on the battlefield: %+v", o)
	}
	if !e.sacrificeBlockedForCost(creatures[0], costCauseSpell) {
		t.Fatal("precondition: Angel's ForCost$ True static is not live on the cost path")
	}
	if e.sacrificeBlockedForCost(creatures[0], costCauseResolution) {
		t.Fatal("Angel's ValidCause$ Spell,Activated blocked an unless payment, a resolution election")
	}
	e.emit(events.Event{Kind: events.TriggerPush, Obj: lord, Player: 0, Amount: 0})
	ability := e.G.Stack[len(e.G.Stack)-1]
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: ability, Player: 1, Amount: 1})
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.ResumeKind != "unless_pay" {
		t.Fatalf("Angel: the unless pay election was withheld: %+v", d)
	}
	if len(d.Options) < 2 {
		t.Fatalf("Angel: the unless pay election has no Pay option: %+v", d.Options)
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

// TestCantSacCostCauseAdmitsTable is the cause-filter regression: the
// readable ValidCause$ bases on the cost path admit exactly the cause each
// names, and nothing else -- including the two corpus carriers' shape.
func TestCantSacCostCauseAdmitsTable(t *testing.T) {
	t.Parallel()
	spellActivated := "Spell,Activated"
	if !causeCostAdmits(spellActivated, costCauseSpell) {
		t.Error("Spell,Activated must admit a spell-cast cost")
	}
	if !causeCostAdmits(spellActivated, costCauseActivated) {
		t.Error("Spell,Activated must admit an activation cost")
	}
	for _, c := range []costCause{costCauseNone, costCauseTriggered, costCauseResolution} {
		if causeCostAdmits(spellActivated, c) {
			t.Errorf("Spell,Activated must not admit cause %d", c)
		}
	}
	if !causeCostAdmits("Triggered", costCauseTriggered) {
		t.Error("Triggered must admit a trigger-demanded payment")
	}
	for _, c := range []costCause{costCauseNone, costCauseSpell, costCauseActivated, costCauseResolution} {
		if causeCostAdmits("Triggered", c) {
			t.Errorf("Triggered must not admit cause %d", c)
		}
	}
	for _, spec := range []string{"Spell.Instant", "Spell.OppCtrl", "Triggered.YouCtrl", "SpellAbility", "Ability"} {
		if causeCostAdmits(spec, costCauseSpell) || causeCostAdmits(spec, costCauseActivated) ||
			causeCostAdmits(spec, costCauseTriggered) {
			t.Errorf("qualified/unknown base %q must fail closed", spec)
		}
	}
	if causeCostAdmits("", costCauseSpell) {
		t.Error("an empty spec must fail closed")
	}
}
