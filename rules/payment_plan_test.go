package rules

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func paymentCast(id state.ObjID) decision.PlannedCast {
	return decision.PlannedCast{Object: id, Face: 0, Origin: "hand"}
}

func TestPaymentPlanBacktracksExclusiveSources(t *testing.T) {
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
	e, _, spell := newFixtureDeck(t, 9110, "Name:Colorless Plan Spell\nManaCost:C\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	source := onBoard(t, e, 0, "Name:Millikin Shape\nTypes:Artifact Creature Construct\nA:AB$ Mana | Cost$ T Mill<1> | Produced$ C\nOracle:x\n")
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
	e, _, spell := newFixtureDeck(t, 9112, "Name:Plan Spell\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	source := onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	e.AddContinuous(state.ContinuousEffect{Source: source, Controller: 0,
		ReplacementEvent: "ProduceMana", ReplacementBody: "DB$ ReplaceMana | ReplaceAmount$ 2"})
	if got := e.PlanCastPayment(0, paymentCast(spell)); got.Plan != nil || got.Reason != "unsupported" {
		t.Fatalf("plan under effect-created mana replacement = %#v, want unsupported", got)
	}
}
