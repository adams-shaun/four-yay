package rules

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func paymentCast(id state.ObjID) decision.PlannedCast {
	return decision.PlannedCast{Object: id, Face: 0, Origin: "hand"}
}

func TestPaymentPlanBacktracksExclusiveSources(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9101, "Name:Plan Spell\nManaCost:U R\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	dual := onBoard(t, e, 0, "Name:Volcanic Test\nTypes:Land Island Mountain\nOracle:x\n")
	got := e.PlanCastPayment(0, paymentCast(spell))
	if got.Plan == nil || len(got.Plan.Activations) != 2 {
		t.Fatalf("plan = %#v, want complete two-source witness", got)
	}
	if got.Plan.Activations[0].Source == got.Plan.Activations[1].Source || got.Plan.Activations[0].Source != dual && got.Plan.Activations[1].Source != dual {
		t.Fatalf("activations = %#v, want the dual used once", got.Plan.Activations)
	}
	if err := e.ValidateCastPayment(0, paymentCast(spell), *got.Plan); err != nil {
		t.Fatalf("validate witness: %v", err)
	}
}

func TestPaymentPlanDoesNotDoubleCountDualAndKeepsColorlessDistinct(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9102, "Name:Double Pip\nManaCost:U R\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	onBoard(t, e, 0, "Name:Only Dual\nTypes:Land Island Mountain\nOracle:x\n")
	if got := e.PlanCastPayment(0, paymentCast(spell)); got.Plan != nil || got.Reason != "insufficient" {
		t.Fatalf("one dual plan = %#v, want insufficient", got)
	}
	e2, _, cspell := newFixtureDeck(t, 9103, "Name:Colorless Spell\nManaCost:C\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	e2.G.Players[0].Pool[state.ManaIndex('U')] = 1
	if got := e2.PlanCastPayment(0, paymentCast(cspell)); got.Plan != nil {
		t.Fatalf("blue paid true colorless: %#v", got.Plan)
	}
	e3, _, gspell := newFixtureDeck(t, 9104, "Name:Generic Spell\nManaCost:1\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	e3.G.Players[0].Pool[state.ManaIndex('U')] = 1
	got := e3.PlanCastPayment(0, paymentCast(gspell))
	if got.Plan == nil || len(got.Plan.Activations) != 0 || got.Plan.PoolSpend[state.ManaIndex('U')] != 1 {
		t.Fatalf("generic pool-only plan = %#v", got)
	}
}

// V1 payment witnesses only describe tapping a source.  A mana ability with
// an extra cost is still legal for manual payment, but must never appear in a
// suggested plan because the witness cannot carry or execute that cost.
func TestPaymentPlanExcludesManaAbilityWithMillCost(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9110, "Name:Colorless Plan Spell\nManaCost:C\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	source := onBoard(t, e, 0, "Name:Millikin Shape\nTypes:Artifact Creature Construct\nA:AB$ Mana | Cost$ T Mill<1> | Produced$ C\nOracle:x\n")
	e.G.Obj(source).SummonSick = false
	ma := e.G.Obj(source).Face().Abilities[0]
	if got := e.parseCost(ma.Params["Cost"]); !got.Tap || len(got.Mill) != 1 {
		t.Fatalf("mana cost = %+v, want tap plus Mill<1>", got)
	}
	if got := e.PlanCastPayment(0, paymentCast(spell)); got.Plan != nil || got.Reason != "insufficient" {
		t.Fatalf("plan with Mill<1> source = %#v, want insufficient", got)
	}
	// The strict plan gate must not remove the legitimate manual action.
	found := false
	for _, opt := range e.legalActions(0) {
		if opt.Kind == "activate" && opt.Obj == source {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Mill<1> mana activation disappeared from manual actions")
	}
}

func TestPaymentPlanQueryIsPure(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9105, "Name:Planned Instant\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	beforeGame := e.G.Clone()
	beforeHead := e.L.Head()
	beforeEvents := len(e.L.Events)
	beforePending := e.Pending()
	first := e.PlanCastPayment(0, paymentCast(spell))
	second := e.PlanCastPayment(0, paymentCast(spell))
	if first.Plan == nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("plans differ: %#v %#v", first, second)
	}
	if !reflect.DeepEqual(beforeGame, e.G) || beforeHead != e.L.Head() || beforeEvents != len(e.L.Events) || beforePending != e.Pending() {
		t.Fatal("pure payment plan query changed engine state")
	}
}

func TestPaymentPlanPriorityAskLeavesExtensionLazy(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9108, "Name:Published Plan\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	// Re-ask as the live engine does after an eventless fixture change.
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %#v, want priority", d)
	}
	if d.PaymentActionsBuilt || len(d.PaymentActions) != 0 {
		t.Fatalf("priority ask eagerly built payment extension: built=%v actions=%#v", d.PaymentActionsBuilt, d.PaymentActions)
	}
	legacy := append([]decision.Option(nil), d.Options...)
	actions := e.EnsurePaymentActions()
	if len(actions) != 1 || actions[0].Cast.Object != spell || actions[0].BaseOptionIndex != nil {
		t.Fatalf("payment actions = %#v, want one planned-only action for spell %d", actions, spell)
	}
	if got := e.legalActions(0); !reflect.DeepEqual(legacy, got) {
		t.Fatalf("legacy options changed by lazy publication:\n got %#v\nwant %#v", got, legacy)
	}
	cp := d.Clone()
	cp.PaymentActions[0].Plans[0].Activations[0].Produces[state.ManaIndex('U')] = 9
	if d.PaymentActions[0].Plans[0].Activations[0].Produces[state.ManaIndex('U')] != 1 {
		t.Fatal("payment action clone aliases pending witness")
	}
}

func TestPaymentPlanActionsPreserveLegacyOptions(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9106, "Name:Plan Offer\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	withoutPool := e.PaymentActionsForPriority(0, 77)
	if len(withoutPool) != 1 || withoutPool[0].Cast.Object != spell || withoutPool[0].BaseOptionIndex != nil {
		t.Fatalf("unfunded actions = %#v, want planned-only cast", withoutPool)
	}
	e.G.Players[0].Pool[state.ManaIndex('U')] = 1
	withPool := e.PaymentActionsForPriority(0, 78)
	if len(withPool) != 1 || len(withPool[0].Plans[0].Activations) != 0 || withPool[0].BaseOptionIndex == nil {
		t.Fatalf("funded actions = %#v, want grouped pool-only plan", withPool)
	}
	legacy := e.legalActions(0)
	if legacy[*withPool[0].BaseOptionIndex].Obj != spell || legacy[*withPool[0].BaseOptionIndex].Kind != "cast" {
		t.Fatalf("base option = %#v, want ordinary cast", legacy)
	}
}

// AlternativeCost offers have an empty Mode, so filtering only on Mode makes
// both the ordinary and alternate routes construct the identical V1
// PlannedCast. That duplicates the canonical payment-action ID and breaks
// keyed clients. V1 supports only the ordinary printed-cost route.
func TestPaymentPlanPriorityExcludesAlternativeCostCast(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9111, "Name:Two Costs\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nS:Mode$ AlternativeCost | ValidCard$ Card.Self | Cost$ U\nOracle:x\n")
	toMain1(t, e)
	e.G.Players[0].Pool[state.MU] = 1
	var normal, alternate int
	for _, opt := range e.legalActions(0) {
		if opt.Kind != "cast" || opt.Obj != spell {
			continue
		}
		if opt.AltCostIndex == 0 {
			normal++
		} else {
			alternate++
		}
	}
	if normal != 1 || alternate != 1 {
		t.Fatalf("legacy casts normal=%d alternate=%d, want one of each", normal, alternate)
	}
	actions := e.PaymentActionsForPriority(0, 79)
	if len(actions) != 1 || actions[0].Cast.Object != spell {
		t.Fatalf("payment actions = %#v, want exactly the ordinary cast", actions)
	}
	if actions[0].BaseOptionIndex == nil {
		t.Fatalf("ordinary funded cast has no base option: %#v", actions[0])
	}
}

func TestPaymentPlanSubmitExecutesWitness(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9107, "Name:Planned Cast\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	island := onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %#v, want priority", d)
	}
	d.PaymentActions = e.PaymentActionsForPriority(0, d.Seq)
	if len(d.PaymentActions) != 1 {
		t.Fatalf("actions = %#v", d.PaymentActions)
	}
	a := d.PaymentActions[0]
	in := decision.Intent{Seq: d.Seq, Player: 0, Payment: &decision.PaymentSelection{ActionID: a.ID, Plan: a.Plans[0]}}
	if err := e.Submit(in); err != nil {
		t.Fatalf("Submit planned cast: %v", err)
	}
	if !e.G.Obj(island).Tapped {
		t.Fatal("selected Island was not tapped through mana activation")
	}
	if e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("spell zone = %s, want stack", e.G.Obj(spell).Zone)
	}
	if got := e.L.Intents[len(e.L.Intents)-1].Payment; got == nil || got.Plan.ID != a.Plans[0].ID {
		t.Fatalf("recorded payment = %#v", got)
	}
	var made string
	for _, ev := range e.L.Events {
		if ev.Kind == events.DecisionMade {
			made = ev.Text
		}
	}
	if !strings.HasSuffix(made, ";payment:"+a.ID+":"+a.Plans[0].ID) {
		t.Fatalf("DecisionMade = %q, want canonical payment suffix", made)
	}
}

// A payment-plan witness must fund the whole mana cost, rather than merely
// its coloured pips.  In particular a {2}{U} cast with three Islands must
// tap all three sources; leaving either generic mana unpaid would let a
// second such spell be cast from the same board.
func TestPaymentPlanPaysGenericAndColoredCostInFull(t *testing.T) {
	t.Parallel()
	e, _, first := newFixtureDeck(t, 9109, "Name:First Wind Drake\nManaCost:2 U\nTypes:Creature Bird Drake\nPT:2/2\nOracle:x\n")
	toMain1(t, e)
	lands := []state.ObjID{
		onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n"),
		onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n"),
		onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n"),
	}
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %#v, want priority", d)
	}
	d.PaymentActions = e.PaymentActionsForPriority(0, d.Seq)
	var a *decision.PaymentAction
	for i := range d.PaymentActions {
		if d.PaymentActions[i].Cast.Object == first {
			a = &d.PaymentActions[i]
			break
		}
	}
	if a == nil {
		t.Fatalf("payment actions = %#v, want first drake", d.PaymentActions)
	}
	if got := len(a.Plans[0].Activations); got != len(lands) {
		t.Fatalf("plan activations = %#v, want all %d lands", a.Plans[0].Activations, len(lands))
	}
	underpay := decision.ClonePaymentPlan(a.Plans[0])
	underpay.Activations = underpay.Activations[:1]
	if err := e.ValidateCastPayment(0, paymentCast(first), underpay); err == nil {
		t.Fatal("ValidateCastPayment accepted a witness that leaves generic mana unpaid")
	}
	in := decision.Intent{Seq: d.Seq, Player: 0, Payment: &decision.PaymentSelection{ActionID: a.ID, Plan: a.Plans[0]}}
	if err := e.Submit(in); err != nil {
		t.Fatalf("Submit planned cast: %v", err)
	}
	for _, id := range lands {
		if !e.G.Obj(id).Tapped {
			t.Fatalf("land %d was not tapped paying {2}{U}", id)
		}
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("mana pool after {2}{U} payment = %d, want 0", got)
	}
}

func TestPaymentPlanExecutesFiniteProducedAnyChoice(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9111, "Name:Blue Plan Spell\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	source := onBoard(t, e, 0, "Name:Any Land\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ Any\nOracle:x\n")
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	if d == nil {
		t.Fatal("missing priority")
	}
	d.PaymentActions = e.PaymentActionsForPriority(0, d.Seq)
	if len(d.PaymentActions) != 1 || len(d.PaymentActions[0].Plans) != 1 {
		t.Fatalf("actions = %#v, want one Any-mana plan", d.PaymentActions)
	}
	plan := d.PaymentActions[0].Plans[0]
	if len(plan.Activations) != 1 || plan.Activations[0].Produces[state.ManaIndex('U')] != 1 {
		t.Fatalf("plan = %#v, want concrete U production", plan)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Payment: &decision.PaymentSelection{ActionID: d.PaymentActions[0].ID, Plan: plan}}); err != nil {
		t.Fatalf("Submit Any plan: %v", err)
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("Any plan did not resume priority: %#v", d)
	}
	if !e.G.Obj(source).Tapped || e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("source/spell after plan = tapped:%v zone:%s", e.G.Obj(source).Tapped, e.G.Obj(spell).Zone)
	}
}

// paymentPlanReask re-poses seat 0's priority after an (eventless) fixture
// change and opts into the lazy payment extension.
func paymentPlanReask(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %#v, want seat-0 priority", d)
	}
	e.EnsurePaymentActions()
	return d
}

func paymentPlanActionFor(t *testing.T, d *decision.Decision, spell state.ObjID) decision.PaymentAction {
	t.Helper()
	for _, a := range d.PaymentActions {
		if a.Cast.Object == spell && len(a.Plans) > 0 {
			return a
		}
	}
	t.Fatalf("no payment action for %d in %#v", spell, d.PaymentActions)
	return decision.PaymentAction{}
}

func submitPaymentPlan(t *testing.T, e *Engine, d *decision.Decision, a decision.PaymentAction) {
	t.Helper()
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player,
		Payment: &decision.PaymentSelection{ActionID: a.ID, Plan: a.Plans[0]}}); err != nil {
		t.Fatalf("Submit planned cast: %v", err)
	}
}

// producedManaSince lists the colour of every positive ManaAdd from event
// index from onward, one entry per unit.
func producedManaSince(e *Engine, from int) []string {
	var out []string
	for _, ev := range e.L.Events[from:] {
		if ev.Kind == events.ManaAdd && ev.Amount > 0 {
			for i := int32(0); i < ev.Amount; i++ {
				out = append(out, ev.Counter)
			}
		}
	}
	return out
}

// Every intrinsic ability shares the PaymentAbility identity {intrinsic,
// basic_land}, so a witness step names its ability by that identity AND its
// Produces together. A Volcanic-Island-shaped dual (intrinsic abilities in
// W,U,B,R,G order: U then R) asked for R must activate its R ability, not the
// first intrinsic ability whose identity matches.
func TestPaymentPlanDualLandExecutesWitnessedColour(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9301, "Name:Red Plan Spell\nManaCost:R\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	dual := onBoard(t, e, 0, "Name:Volcanic Test\nTypes:Land Island Mountain\nOracle:x\n")
	d := paymentPlanReask(t, e)
	a := paymentPlanActionFor(t, d, spell)
	plan := a.Plans[0]
	if len(plan.Activations) != 1 || plan.Activations[0].Source != dual || plan.Activations[0].Produces[state.ManaIndex('R')] != 1 {
		t.Fatalf("witness = %#v, want the dual producing R", plan)
	}
	start := len(e.L.Events)
	submitPaymentPlan(t, e, d, a)
	if got := producedManaSince(e, start); !reflect.DeepEqual(got, []string{"R"}) {
		t.Errorf("planned dual produced %v, want exactly [R] (the witnessed production)", got)
	}
	if z := e.G.Obj(spell).Zone; z != state.ZStack {
		t.Errorf("spell zone after planned payment = %s, want stack", z)
	}
	if nd := e.Pending(); nd == nil || nd.Kind != decision.KPriority {
		t.Errorf("after planned payment pending = %#v, want the ordinary priority", nd)
	}
}

// The same exact-alternative rule on a real two-colour cast: {U}{R} with an
// Island and a dual is TestPaymentPlanBacktracksExclusiveSources' board, here
// executed rather than only validated. The dual must supply the R its witness
// step names.
func TestPaymentPlanBacktrackedDualWitnessExecutes(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9302, "Name:Plan Spell\nManaCost:U R\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	island := onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	dual := onBoard(t, e, 0, "Name:Volcanic Test\nTypes:Land Island Mountain\nOracle:x\n")
	d := paymentPlanReask(t, e)
	a := paymentPlanActionFor(t, d, spell)
	start := len(e.L.Events)
	submitPaymentPlan(t, e, d, a)
	if !e.G.Obj(island).Tapped || !e.G.Obj(dual).Tapped {
		t.Errorf("tapped island=%v dual=%v, want both", e.G.Obj(island).Tapped, e.G.Obj(dual).Tapped)
	}
	got := producedManaSince(e, start)
	if len(got) != 2 || !(got[0] == "U" && got[1] == "R" || got[0] == "R" && got[1] == "U") {
		t.Errorf("planned {U}{R} produced %v, want one U and one R", got)
	}
	if z := e.G.Obj(spell).Zone; z != state.ZStack {
		t.Errorf("spell zone after planned payment = %s, want stack", z)
	}
}

// A basic land type granted in layer 4 (an Urborg-style "each land is a
// Swamp") gives a Mountain an intrinsic {B} ability beside its own {R}
// (CR 305.6), again under the one identity {intrinsic, basic_land}, and the
// granted ability is rebuilt on every walk rather than stored on the face. A
// step asking the Mountain for {B} must produce {B}. The ask-time offer does
// not reach this cast yet (PotentialMana reads printed faces only), so the
// planner's own witness is admitted onto the pending decision exactly as
// PaymentActionsForPriority builds an action.
func TestPaymentPlanGrantedLandTypeExecutesWitnessedColour(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9303, "Name:Black Plan Spell\nManaCost:B B\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	// A live game derives this vocabulary from its NameUniverse; without it
	// no granted basic land type adds its intrinsic ability.
	e.landTypeWords = []string{"Forest", "Island", "Mountain", "Plains", "Swamp"}
	onBoard(t, e, 0, "Name:Swamp Grant\nTypes:Land\nS:Mode$ Continuous | Affected$ Land | AddType$ Swamp\nOracle:x\n")
	mountain := onBoard(t, e, 0, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	d := paymentPlanReask(t, e)
	cast := paymentCast(spell)
	got := e.PlanCastPayment(0, cast)
	if got.Plan == nil {
		t.Fatalf("plan = %#v, want the granted Swamp type to fund {B}{B}", got)
	}
	plan := *got.Plan
	askedB := false
	for _, act := range plan.Activations {
		if act.Source == mountain && act.Produces == (decision.ManaAmount{0, 0, 1, 0, 0, 0}) {
			askedB = true
		}
	}
	if !askedB {
		t.Fatalf("witness = %#v, want the Mountain producing B", plan.Activations)
	}
	var err error
	if plan.ID, err = decision.PaymentPlanID(d.Seq, 0, cast, plan); err != nil {
		t.Fatal(err)
	}
	a := decision.PaymentAction{Cast: cast, Label: "Cast Black Plan Spell", Plans: []decision.PaymentPlan{plan}}
	if a.ID, err = decision.PaymentActionID(decision.PaymentPlanV1, d.Seq, 0, cast); err != nil {
		t.Fatal(err)
	}
	d.PaymentActions = []decision.PaymentAction{a}
	d.PaymentActionsBuilt = true
	start := len(e.L.Events)
	submitPaymentPlan(t, e, d, a)
	if got := producedManaSince(e, start); !reflect.DeepEqual(got, []string{"B", "B"}) {
		t.Errorf("planned {B}{B} produced %v, want [B B]", got)
	}
	if z := e.G.Obj(spell).Zone; z != state.ZStack {
		t.Errorf("spell zone after planned payment = %s, want stack", z)
	}
}

func TestPaymentPlanDeclinesEffectCreatedProduceManaReplacement(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9112, "Name:Plan Spell\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	source := onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	e.AddContinuous(state.ContinuousEffect{Source: source, Controller: 0,
		ReplacementEvent: "ProduceMana", ReplacementBody: "DB$ ReplaceMana | ReplaceAmount$ 2"})
	if got := e.PlanCastPayment(0, paymentCast(spell)); got.Plan != nil || got.Reason != "unsupported" {
		t.Fatalf("plan under effect-created mana replacement = %#v, want unsupported", got)
	}
}

const paymentPlanMountain = "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n"

// paymentPlanFallbackReasons is the closed spec §6 fallback vocabulary.
var paymentPlanFallbackReasons = map[string]bool{
	paymentFallbackCostChanged: true, paymentFallbackSourceChanged: true,
	paymentFallbackProductionChanged: true, paymentFallbackChoiceRequired: true,
}

// paymentPlanSpentSince sums the mana payments took from p's pool (the
// negative ManaAdd events) from event index from onward, by pool slot.
func paymentPlanSpentSince(e *Engine, p state.PlayerID, from int) state.Mana {
	var out state.Mana
	for _, ev := range e.L.Events[from:] {
		if ev.Kind == events.ManaAdd && ev.Player == p && ev.Amount < 0 {
			out[state.ManaSlot(ev.Counter)] -= ev.Amount
		}
	}
	return out
}

// paymentPlanSources lists a witness's planned sources in step order.
func paymentPlanSources(plan decision.PaymentPlan) []state.ObjID {
	out := make([]state.ObjID, 0, len(plan.Activations))
	for _, act := range plan.Activations {
		out = append(out, act.Source)
	}
	return out
}

// paymentPlanChooseObj answers the pending KChoose with the option naming obj.
func paymentPlanChooseObj(t *testing.T, e *Engine, obj state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("pending = %s, want a cost choice", paymentPlanPendingSummary(d))
	}
	for _, o := range d.Options {
		if o.Obj == obj {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("cost choice %q offers no option for %d: %#v", d.Prompt, obj, d.Options)
}

// paymentPlanSettle is the spec §6 contract for a planned cast whose cost
// choices have been answered: either the plan kept running and the cast
// settled, or automation stopped on the ordinary manual window with a
// vocabulary PaymentFallback naming the selected plan -- which the caller's
// manual answers must then be able to finish. It returns the fallback seen.
func paymentPlanSettle(t *testing.T, e *Engine, plan decision.PaymentPlan) *decision.PaymentFallback {
	t.Helper()
	var fb *decision.PaymentFallback
	for i := 0; i < 16; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("planned cast left no decision pending")
		}
		if d.PaymentFallback != nil && fb == nil {
			f := *d.PaymentFallback
			fb = &f
			if !paymentPlanFallbackReasons[f.Reason] || f.PlanID != plan.ID {
				t.Fatalf("fallback = %#v, want a vocabulary reason for plan %s", f, plan.ID)
			}
		}
		if d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "activate" {
			return fb
		}
		if d.PaymentFallback == nil {
			t.Fatalf("manual mana window %q posed during a planned cast without a PaymentFallback", d.Prompt)
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	t.Fatal("manual payment window did not close")
	return nil
}

// assertPlannedCastPaid fails unless spell sits on the stack with its whole
// cast settled: the cast continuation closed, the ordinary priority posed,
// and the mana spent since from covering generic plus every coloured pip.
func assertPlannedCastPaid(t *testing.T, e *Engine, spell state.ObjID, from int, generic int32, colored state.Mana) {
	t.Helper()
	if z := e.G.Obj(spell).Zone; z != state.ZStack {
		t.Fatalf("spell zone = %s, want stack", z)
	}
	if e.cast != nil {
		t.Fatalf("spell is on the stack but its cast never settled (payment continuation still open, pending %s)", paymentPlanPendingSummary(e.Pending()))
	}
	spent := paymentPlanSpentSince(e, 0, from)
	for i, n := range colored {
		if spent[i] < n {
			t.Fatalf("spent %v does not cover the coloured cost %v", spent, colored)
		}
	}
	if spent.Total() < generic+colored.Total() {
		t.Fatalf("spent %v (total %d) does not cover {%d} plus %v", spent, spent.Total(), generic, colored)
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after the settled cast pending = %s, want the ordinary priority", paymentPlanPendingSummary(d))
	}
}

// PP-08 (spec §3.1 as amended): a plain cast whose spell ability also carries
// a non-mana `Cost$` part -- Harrow's land sacrifice, Thrill of Possibility's
// discard, Gurmag Angler's delve -- receives NO V1 payment plan, because the
// witness cannot bind the additional cost. The ordinary cast option is
// unchanged and still offered once the mana is floated. The executor path for
// a plan that does name such a cast is exercised directly (not through the
// planner offer) by TestPaymentPlanExecutesAfterCastTimeChoice and
// TestPaymentPlanShapeGateAdditionalCostExecutorPays in
// payment_plan_shape_gate_test.go. This test is the planner half of the audit
// story aph-cast-shape-gate owns; autopay-exec-harden owns the executor half.
func TestPaymentPlanAdditionalCostCastGetsNoPlan(t *testing.T) {
	t.Parallel()
	red := state.Mana{}
	red[state.ManaIndex('R')] = 1
	_ = red

	// assertWithheld proves the planner offers neither an action nor a plan
	// for spell, and that the ordinary cast option survives after floating.
	assertWithheld := func(t *testing.T, e *Engine, spell state.ObjID, pool map[int]int32) {
		t.Helper()
		if got := e.PlanCastPayment(0, paymentCast(spell)); got.Plan != nil || got.Reason != "unsupported" {
			t.Fatalf("additional-cost cast %d outcome = %+v, want unsupported with no plan", spell, got)
		}
		for _, a := range e.PaymentActionsForPriority(0, 77) {
			if a.Cast.Object == spell {
				t.Fatalf("additional-cost cast %d received a payment action %+v", spell, a)
			}
		}
		for slot, n := range pool {
			e.G.Players[0].Pool[slot] = n
		}
		e.pending = nil
		e.askPriority(0)
		for _, opt := range e.Pending().Options {
			if opt.Kind == "cast" && opt.Obj == spell {
				return
			}
		}
		t.Fatalf("ordinary cast option for %d was removed; options=%+v", spell, e.Pending().Options)
	}

	sacrificeSetup := func(t *testing.T, seed uint64) (*Engine, state.ObjID, []state.ObjID) {
		e, _, spell := newFixtureDeck(t, seed, "Name:Planned Harrow Shape\nManaCost:1 R\nTypes:Instant\nA:SP$ Draw | Cost$ 1 R Sac<1/Land> | NumCards$ 1\nOracle:x\n")
		lands := []state.ObjID{onBoard(t, e, 0, paymentPlanMountain), onBoard(t, e, 0, paymentPlanMountain), onBoard(t, e, 0, paymentPlanMountain)}
		for _, id := range lands {
			e.G.Obj(id).SummonSick = false
		}
		return e, spell, lands
	}

	t.Run("sacrifice", func(t *testing.T) {
		e, spell, _ := sacrificeSetup(t, 9401)
		if e.G.Obj(spell).Zone != state.ZHand {
			t.Fatal("fixture spell is not in hand")
		}
		// PP-08 now admits exactly the fixed-count mandatory sacrifice shape:
		// the planner offers a mana-only witness and the sacrifice is answered
		// through the ordinary in-flow ask (TestPaymentPlanSeeksSacrificeOffer).
		if detail := e.paymentPlanCastShapeDetail(0, spell); detail != "" {
			t.Fatalf("sacrifice cast detail = %q, want the shape admitted", detail)
		}
		if got := e.PlanCastPayment(0, paymentCast(spell)); got.Plan == nil {
			t.Fatalf("sacrifice cast outcome = %+v, want a mana-only plan", got)
		}
	})

	t.Run("discard", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9403, "Name:Planned Thrill Shape\nManaCost:1 R\nTypes:Instant\nA:SP$ Draw | Cost$ 1 R Discard<1/Card> | NumCards$ 2\nOracle:x\n")
		onBoard(t, e, 0, paymentPlanMountain)
		onBoard(t, e, 0, paymentPlanMountain)
		if e.paymentPlanCastShapeDetail(0, spell) != "shape:additional_cost" {
			t.Fatalf("discard cast detail = %q, want shape:additional_cost", e.paymentPlanCastShapeDetail(0, spell))
		}
		assertWithheld(t, e, spell, map[int]int32{state.ManaIndex('R'): 2})
	})

	delveSetup := func(t *testing.T, seed uint64) (*Engine, state.ObjID, []state.ObjID) {
		e, _, spell := newFixtureDeck(t, seed, "Name:Planned Delve Shape\nManaCost:2 R\nTypes:Instant\nK:Delve\nA:SP$ Draw | NumCards$ 1\nOracle:x\n")
		for i := 0; i < 3; i++ {
			onBoard(t, e, 0, paymentPlanMountain)
		}
		var gy []state.ObjID
		for _, id := range e.G.Zone(state.ZLibrary, 0)[:2] {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZGraveyard})
			gy = append(gy, id)
		}
		return e, spell, gy
	}

	t.Run("delve", func(t *testing.T) {
		e, spell, _ := delveSetup(t, 9404)
		if e.paymentPlanCastShapeDetail(0, spell) != "shape:contribution" {
			t.Fatalf("delve cast detail = %q, want shape:contribution", e.paymentPlanCastShapeDetail(0, spell))
		}
		assertWithheld(t, e, spell, map[int]int32{state.ManaIndex('R'): 3})
	})

	// The census's own cards, from the corpus by name: Harrow (sacrifice a
	// land), Thrill of Possibility (discard a card), Gurmag Angler (delve).
	reg := testutil.CorpusRegistry(t)
	corpusCard := func(t *testing.T, name string) *cards.Card {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus card %s missing", name)
		}
		return c
	}
	const (
		forest = "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n"
		swamp  = "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n"
	)

	t.Run("corpus Harrow", func(t *testing.T) {
		e, _, _ := newFixtureDeck(t, 9406, paymentPlanShock)
		onBoard(t, e, 0, forest)
		onBoard(t, e, 0, forest)
		onBoard(t, e, 0, forest)
		onBoard(t, e, 0, paymentPlanMountain)
		spell := putInHand(t, e, 0, corpusCard(t, "Harrow"))
		// Harrow's `Cost$ Sac<1/Land>` is the fixed-count mandatory sacrifice
		// shape PP-08 now admits, so its plan is offered; the land sacrifice
		// is answered through the ordinary in-flow ask.
		if detail := e.paymentPlanCastShapeDetail(0, spell); detail != "" {
			t.Fatalf("Harrow shape detail = %q, want the fixed-count sacrifice shape admitted", detail)
		}
		if got := e.PlanCastPayment(0, paymentCast(spell)); got.Plan == nil {
			t.Fatalf("Harrow outcome = %+v, want a mana-only plan", got)
		}
	})

	t.Run("corpus Thrill of Possibility", func(t *testing.T) {
		e, _, _ := newFixtureDeck(t, 9407, paymentPlanShock)
		onBoard(t, e, 0, paymentPlanMountain)
		onBoard(t, e, 0, paymentPlanMountain)
		spell := putInHand(t, e, 0, corpusCard(t, "Thrill of Possibility"))
		assertWithheld(t, e, spell, map[int]int32{state.ManaIndex('R'): 2})
	})

	t.Run("corpus Gurmag Angler", func(t *testing.T) {
		e, _, _ := newFixtureDeck(t, 9408, paymentPlanShock)
		toMain1(t, e)
		for i := 0; i < 7; i++ {
			onBoard(t, e, 0, swamp)
		}
		for _, id := range e.G.Zone(state.ZLibrary, 0)[:2] {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZGraveyard})
		}
		spell := putInHand(t, e, 0, corpusCard(t, "Gurmag Angler"))
		assertWithheld(t, e, spell, map[int]int32{state.ManaIndex('B'): 7})
	})
}

const (
	paymentPlanShock = "Name:Planned Shock\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"
	paymentPlanBlast = "Name:Planned Blast\nManaCost:R R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 2\nOracle:x\n"
)

// paymentPlanTapsSince counts the Tap events naming id from event index from.
func paymentPlanTapsSince(e *Engine, from int, id state.ObjID) int {
	n := 0
	for _, ev := range e.L.Events[from:] {
		if ev.Kind == events.Tap && ev.Obj == id {
			n++
		}
	}
	return n
}

// paymentPlanPendingSummary names a pending decision briefly for a failure.
func paymentPlanPendingSummary(d *decision.Decision) string {
	if d == nil {
		return "<nothing pending>"
	}
	fb := "<no fallback>"
	if d.PaymentFallback != nil {
		fb = d.PaymentFallback.PlanID + ":" + d.PaymentFallback.Reason
	}
	return string(d.Kind) + " " + strconv.Quote(d.Prompt) + " fallback " + fb
}

// paymentPlanTappedContamination is Contamination's replacement gated on a
// land already being tapped: before any planned tap it applies to nothing, so
// the per-source interference check (paymentPlanSourceInterference, which
// defers a land an unconditional Contamination matches before it is ever
// tapped) cannot foresee it, and it changes the production only DURING the
// first planned activation -- the post-activation check's own territory.
const paymentPlanTappedContamination = "Name:Tapped Contamination\nTypes:Enchantment\nR:Event$ ProduceMana | ActiveZones$ Battlefield | ValidCard$ Land | IsPresent$ Land.tapped | ReplaceWith$ ProduceB | Description$ Fixture: once a land is tapped, a land tapped for mana produces B instead.\nSVar:ProduceB:DB$ ReplaceMana | ReplaceMana$ B\nOracle:x\n"

// Spec §6: after each planned activation the executor compares the mana it
// actually added with the witness step. A ProduceMana replacement arriving
// after the offer that applies only once a land is tapped makes the first
// planned Mountain produce B where its step says R. Automation stops at once
// -- the second planned Mountain stays untapped, nothing substitutes for it
// -- and the manual window names the selected plan with production_changed.
// (The corpus's unconditional Contamination is now caught before the first
// tap: TestPaymentPlanInterferenceArrivingAfterOfferStopsBeforeTapping.)
func TestPaymentPlanProductionChangeStopsAutomation(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9303, paymentPlanBlast)
	m1 := onBoard(t, e, 0, paymentPlanMountain)
	m2 := onBoard(t, e, 0, paymentPlanMountain)
	d := paymentPlanReask(t, e)
	a := paymentPlanActionFor(t, d, spell)
	if len(a.Plans[0].Activations) != 2 {
		t.Fatalf("witness = %#v, want both Mountains", a.Plans[0])
	}
	submitPaymentPlan(t, e, d, a)
	if td := e.Pending(); td == nil || td.Kind != decision.KTarget {
		t.Fatalf("pending = %s, want the target ask", paymentPlanPendingSummary(td))
	}
	// The controlled post-offer change: a land mana replacement that only
	// applies once the first planned land is tapped.
	onBoard(t, e, 1, paymentPlanTappedContamination)
	start := len(e.L.Events)
	submitChoices(t, e, 0)
	produced := producedManaSince(e, start)
	tapped := 0
	for _, id := range []state.ObjID{m1, m2} {
		if e.G.Obj(id).Tapped {
			tapped++
		}
	}
	if tapped != 1 {
		t.Errorf("after a mismatched production the executor tapped %d planned sources (produced %v), want it to stop after the first", tapped, produced)
	}
	nd := e.Pending()
	if nd == nil || nd.Kind != decision.KChoose || nd.PaymentFallback == nil ||
		nd.PaymentFallback.Reason != paymentFallbackProductionChanged || nd.PaymentFallback.PlanID != a.Plans[0].ID {
		t.Errorf("pending = %s, want the manual window with production_changed for plan %s", paymentPlanPendingSummary(nd), a.Plans[0].ID)
	}
}

// A planned source tapped after the offer is a changed source: automation
// stops before activating anything, never substitutes the other Mountain,
// and the manual window names the selected plan with source_changed.
func TestPaymentPlanTappedSourceFallsBackWithoutSubstitution(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9304, paymentPlanShock)
	m1 := onBoard(t, e, 0, paymentPlanMountain)
	m2 := onBoard(t, e, 0, paymentPlanMountain)
	d := paymentPlanReask(t, e)
	a := paymentPlanActionFor(t, d, spell)
	planned := a.Plans[0].Activations[0].Source
	other := m1
	if planned == m1 {
		other = m2
	}
	submitPaymentPlan(t, e, d, a)
	e.emit(events.Event{Kind: events.Tap, Obj: planned}) // the post-offer change
	submitChoices(t, e, 0)
	nd := e.Pending()
	if nd == nil || nd.Kind != decision.KChoose || nd.PaymentFallback == nil {
		t.Fatalf("pending = %s, want the manual mana window with a fallback", paymentPlanPendingSummary(nd))
	}
	if e.G.Obj(other).Tapped {
		t.Fatal("executor substituted an unplanned source")
	}
	if nd.PaymentFallback.PlanID != a.Plans[0].ID {
		t.Fatalf("fallback plan = %q, want %q", nd.PaymentFallback.PlanID, a.Plans[0].ID)
	}
	if nd.PaymentFallback.Reason != paymentFallbackSourceChanged {
		t.Errorf("fallback reason for a tapped planned source = %q, want source_changed", nd.PaymentFallback.Reason)
	}
}

// A planned source that left and re-entered the battlefield after the offer
// is a new object (CR 400.7): its incarnation no longer matches the witness,
// so the stale step never taps it and the manual window reports
// source_changed.
func TestPaymentPlanBlinkedSourceFallsBack(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9305, paymentPlanShock)
	onBoard(t, e, 0, paymentPlanMountain)
	onBoard(t, e, 0, paymentPlanMountain)
	d := paymentPlanReask(t, e)
	a := paymentPlanActionFor(t, d, spell)
	planned := a.Plans[0].Activations[0].Source
	submitPaymentPlan(t, e, d, a)
	e.emit(events.Event{Kind: events.MoveZone, Obj: planned, From: state.ZBattlefield, To: state.ZExile})
	e.emit(events.Event{Kind: events.MoveZone, Obj: planned, From: state.ZExile, To: state.ZBattlefield})
	if e.G.Obj(planned).Zone != state.ZBattlefield || e.G.Obj(planned).Tapped {
		t.Fatalf("blinked source zone=%s tapped=%v", e.G.Obj(planned).Zone, e.G.Obj(planned).Tapped)
	}
	start := len(e.L.Events)
	submitChoices(t, e, 0)
	if n := paymentPlanTapsSince(e, start, planned); n != 0 {
		t.Fatalf("blinked source was tapped %d times by the stale witness", n)
	}
	nd := e.Pending()
	if nd == nil || nd.PaymentFallback == nil || nd.PaymentFallback.Reason != paymentFallbackSourceChanged {
		t.Fatalf("pending = %s, want a source_changed fallback", paymentPlanPendingSummary(nd))
	}
}

// A cost raise arriving after the offer (a taxing permanent) is checked
// before the first activation: nothing is tapped and the manual window
// reports cost_changed.
func TestPaymentPlanCostRaisedAfterOfferFallsBackBeforeTapping(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9306, paymentPlanShock)
	m1 := onBoard(t, e, 0, paymentPlanMountain)
	m2 := onBoard(t, e, 0, paymentPlanMountain)
	d := paymentPlanReask(t, e)
	a := paymentPlanActionFor(t, d, spell)
	submitPaymentPlan(t, e, d, a)
	onBoard(t, e, 1, "Name:Tax Relic\nTypes:Artifact\nS:Mode$ RaiseCost | ValidCard$ Card | Type$ Spell | Amount$ 1 | Description$ Spells cost {1} more.\nOracle:x\n")
	start := len(e.L.Events)
	submitChoices(t, e, 0)
	if n := paymentPlanTapsSince(e, start, m1) + paymentPlanTapsSince(e, start, m2); n != 0 {
		t.Fatalf("executor tapped %d sources before noticing the raised cost", n)
	}
	nd := e.Pending()
	if nd == nil || nd.PaymentFallback == nil || nd.PaymentFallback.Reason != paymentFallbackCostChanged {
		t.Fatalf("pending = %s, want a cost_changed fallback on the manual window", paymentPlanPendingSummary(nd))
	}
}

// A planned activation that unexpectedly poses a decision (a Pulse of
// Llanowar replacement arriving after the offer, gated on a land already
// being tapped so the per-source pre-tap check cannot foresee it, turns the
// planned basic's mana into a colour choice) cancels automation: the
// completed activation and its mana stay, the second planned source is never
// tapped by itself, and once the choice is answered the manual window reports
// choice_required.
func TestPaymentPlanActivationInterruptionCancelsRemainingSteps(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9307, paymentPlanBlast)
	m1 := onBoard(t, e, 0, paymentPlanMountain)
	m2 := onBoard(t, e, 0, paymentPlanMountain)
	d := paymentPlanReask(t, e)
	a := paymentPlanActionFor(t, d, spell)
	submitPaymentPlan(t, e, d, a)
	onBoard(t, e, 0, "Name:Tapped Pulse\nTypes:Enchantment\nR:Event$ ProduceMana | ActiveZones$ Battlefield | ValidCard$ Land.Basic+YouCtrl | IsPresent$ Land.tapped+YouCtrl | ReplaceWith$ ProduceAny | Description$ Fixture: once a land you control is tapped, a basic land you control tapped for mana produces a colour of your choice.\nSVar:ProduceAny:DB$ ReplaceMana | ReplaceType$ Any\nOracle:x\n")
	submitChoices(t, e, 0) // the target
	rd := e.Pending()
	if rd == nil || rd.Kind != decision.KReplacement {
		t.Fatalf("pending = %s, want the Pulse colour replacement ask", paymentPlanPendingSummary(rd))
	}
	red := -1
	for _, o := range rd.Options {
		if o.ManaSymbol == "R" || o.Label == "Add R" {
			red = o.Index
		}
	}
	if red < 0 {
		red = 3
	}
	submitChoices(t, e, red)
	tapped := 0
	for _, id := range []state.ObjID{m1, m2} {
		if e.G.Obj(id).Tapped {
			tapped++
		}
	}
	if tapped != 1 {
		t.Fatalf("after the interruption %d planned sources are tapped, want exactly the completed one", tapped)
	}
	nd := e.Pending()
	if nd == nil || nd.Kind != decision.KChoose || nd.PaymentFallback == nil || nd.PaymentFallback.Reason != paymentFallbackChoiceRequired {
		t.Fatalf("pending = %s, want the manual window carrying choice_required", paymentPlanPendingSummary(nd))
	}
	if e.G.Players[0].Pool[state.ManaIndex('R')] != 1 {
		t.Fatalf("completed activation's mana = %v, want R floating", e.G.Players[0].Pool)
	}
}

// The A/B mirror audit's shape of the same defect, with the witness injected
// directly so it stays valid whatever the planner offers: a Sac<1/Creature>
// additional cost is answered through the ordinary cast answer path before
// the planned Swamp runs. The cast must settle completely -- Swamp tapped,
// creature sacrificed, spell on the stack, pool empty, no cast proposal or
// choose flow left open -- before the caster receives priority.
func TestPaymentPlanExecutesAfterCastTimeChoice(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9302, "Name:Rite Test\nManaCost:B\nTypes:Instant\nA:SP$ Draw | Cost$ B Sac<1/Creature> | NumCards$ 2\nOracle:x\n")
	swamp := onBoard(t, e, 0, "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n")
	victim := onBoard(t, e, 0, "Name:Victim Test\nManaCost:1\nTypes:Creature Test\nPT:1/1\nOracle:x\n")
	e.pending = nil
	e.askPriority(0)
	b := decision.ManaAmount{0, 0, 1, 0, 0, 0}
	plan := decision.PaymentPlan{Version: decision.PaymentPlanV1, Cost: decision.PaymentCost{Mana: b},
		Activations: []decision.PaymentActivation{{Source: swamp, SourceZoneSeq: e.paymentSourceZoneSeq(swamp),
			Ability:  decision.PaymentAbility{Kind: decision.PaymentAbilityIntrinsic, Intrinsic: "basic_land"},
			Produces: b}}}
	e.pending = nil
	e.beginCastWithPayment(0, decision.Option{Kind: "cast", Obj: spell}, &decision.PaymentSelection{ActionID: "fixture", Plan: plan})
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("pending = %s, want the sacrifice choice", paymentPlanPendingSummary(d))
	}
	pick := -1
	for _, o := range d.Options {
		if o.Obj == victim {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("sacrifice options %#v lack the fixture creature", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{pick}}); err != nil {
		t.Fatalf("answer sacrifice: %v", err)
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %s, want the caster's priority after the cast", paymentPlanPendingSummary(d))
	}
	if e.cast != nil || e.choosing != chooseNone {
		t.Errorf("priority posed mid-cast: cast pending=%v choosing=%d", e.cast != nil, e.choosing)
	}
	if !e.G.Obj(swamp).Tapped {
		t.Error("the witness's Swamp was not tapped")
	}
	if z := e.G.Obj(victim).Zone; z != state.ZGraveyard {
		t.Errorf("sacrificed creature zone = %s, want graveyard (the additional cost was not paid)", z)
	}
	if z := e.G.Obj(spell).Zone; z != state.ZStack {
		t.Errorf("spell zone = %s, want stack", z)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Errorf("pool after payment = %d, want 0 (the produced B must pay the cost)", got)
	}
}
