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

func TestPaymentPlanPriorityAskPublishesAdditively(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9108, "Name:Published Plan\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	// The fixture's initial priority was asked before its eventless board
	// setup. Re-ask as the live engine does after a state change.
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %#v, want priority", d)
	}
	if len(d.PaymentActions) != 1 || d.PaymentActions[0].Cast.Object != spell {
		t.Fatalf("payment actions = %#v, want one action for spell %d", d.PaymentActions, spell)
	}
	if d.PaymentActions[0].BaseOptionIndex != nil {
		t.Fatalf("unfunded plan base option = %v, want nil", *d.PaymentActions[0].BaseOptionIndex)
	}
	legacy := append([]decision.Option(nil), d.Options...)
	if got := e.legalActions(0); !reflect.DeepEqual(legacy, got) {
		t.Fatalf("priority options changed by payment publication:\n got %#v\nwant %#v", d.Options, got)
	}
	// A caller cannot mutate the pending offer by retaining a copied decision.
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

func TestPaymentPlanDeclinesEffectCreatedProduceManaReplacement(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9112, "Name:Plan Spell\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	source := onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	e.AddContinuous(state.ContinuousEffect{Source: source, Controller: 0,
		ReplacementEvent: "ProduceMana", ReplacementBody: "DB$ ReplaceMana | ReplaceAmount$ 2"})
	if got := e.PlanCastPayment(0, paymentCast(spell)); got.Plan != nil || got.Reason != "unsupported" {
		t.Fatalf("plan under effect-created mana replacement = %#v, want unsupported", got)
	}
}
