package rules

// aph-last-resort-plans (spec §3.2, §4, §5, §6 as amended 2026-09-26): a plan
// may use a last-resort source only when no plan from normal sources exists;
// every such step discloses its consequence in the witness; the plan ranks by
// the Arena-calibrated key-1 weights; the planner never offers a plan whose
// summed life + damage would kill its caster; execution runs every step
// through the ordinary mana path and revalidates consequence and lethality
// before each step.

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/rules/pay"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const (
	lrPlains = "Name:Plains\nTypes:Basic Land Plains\nOracle:x\n"
	lrIsland = "Name:Island\nTypes:Basic Land Island\nOracle:x\n"
	lrForest = "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n"
)

func lrSpell(cost string) string {
	return "Name:Last Resort Spell\nManaCost:" + cost + "\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n"
}

// lrShock is a targeted instant: after Submit the cast pauses on its target
// ask, which is where a test changes the board between offer and execution.
func lrShock(cost string) string {
	return "Name:Last Resort Shock\nManaCost:" + cost + "\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"
}

// lrTreasure places the corpus Treasure token script ({T}, Sacrifice this
// token: Add one mana of any color) on seat 0's battlefield.
func lrTreasure(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	c, ok := testutil.CorpusRegistry(t).Token("c_a_treasure_sac")
	if !ok {
		t.Fatal("corpus token c_a_treasure_sac missing")
	}
	return onBoardCard(t, e, 0, c)
}

func lrCorpus(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	id := onBoardCard(t, e, 0, corpusCard(t, name))
	e.G.Obj(id).SummonSick = false
	return id
}

// lrStep is the expected witness step: the source, its production and its
// disclosed consequence (nil on a normal step). The ability identity is
// checked separately against the source's own printed/intrinsic ability.
type lrStep struct {
	source      state.ObjID
	produces    decision.ManaAmount
	consequence *decision.PaymentConsequence
}

var (
	lrW = decision.ManaAmount{1, 0, 0, 0, 0, 0}
	lrU = decision.ManaAmount{0, 1, 0, 0, 0, 0}
	lrR = decision.ManaAmount{0, 0, 0, 1, 0, 0}
	lrG = decision.ManaAmount{0, 0, 0, 0, 1, 0}
)

func lrC(n uint32) decision.ManaAmount { return decision.ManaAmount{0, 0, 0, 0, 0, n} }

func lrAssertPlan(t *testing.T, e *Engine, got PaymentPlanOutcome, want []lrStep) {
	t.Helper()
	if got.Plan == nil {
		t.Fatalf("no plan (reason %q detail %q), want %d steps", got.Reason, got.Detail, len(want))
	}
	if got.Reason != "" {
		t.Fatalf("plan reason = %q, want a clean plan", got.Reason)
	}
	acts := got.Plan.Activations
	if len(acts) != len(want) {
		t.Fatalf("plan = %s, want %d steps", lrPlanString(acts), len(want))
	}
	for i, w := range want {
		a := acts[i]
		if a.Source != w.source || a.Produces != w.produces || !reflect.DeepEqual(a.Consequence, w.consequence) {
			t.Fatalf("step %d = %s, want source %d produces %v consequence %s (plan %s)", i, lrStepString(a), w.source, w.produces, lrConsString(w.consequence), lrPlanString(acts))
		}
		if a.SourceZoneSeq != pay.PaymentSourceZoneSeq(asPayer(e), a.Source) {
			t.Fatalf("step %d zone seq = %d, want %d", i, a.SourceZoneSeq, pay.PaymentSourceZoneSeq(asPayer(e), a.Source))
		}
	}
}

func lrConsString(c *decision.PaymentConsequence) string {
	if c == nil {
		return "<none>"
	}
	return fmt.Sprintf("%+v", *c)
}

func lrStepString(a decision.PaymentActivation) string {
	return fmt.Sprintf("{src %d %s/%d/%d%s produces %v consequence %s}", a.Source, a.Ability.Kind, a.Ability.Face, a.Ability.Index, a.Ability.Intrinsic, a.Produces, lrConsString(a.Consequence))
}

func lrPlanString(acts []decision.PaymentActivation) string {
	var parts []string
	for _, a := range acts {
		parts = append(parts, lrStepString(a))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// lrSubmit publishes the priority decision's actions, submits spell's plan
// and returns it with the event index Submit started at.
func lrSubmit(t *testing.T, e *Engine, spell state.ObjID) (decision.PaymentPlan, int) {
	t.Helper()
	d := paymentPlanReask(t, e)
	a := paymentPlanActionFor(t, d, spell)
	mark := len(e.L.Events)
	submitPaymentPlan(t, e, d, a)
	return a.Plans[0], mark
}

// lrOnlyPriorityAsked fails when anything but a priority decision was asked
// from event index from onward.
func lrOnlyPriorityAsked(t *testing.T, e *Engine, from int) {
	t.Helper()
	for _, ev := range e.L.Events[from:] {
		if ev.Kind == events.DecisionAsk && ev.Text != string(decision.KPriority) {
			t.Fatalf("a %q decision was asked between Submit and the spell reaching the stack", ev.Text)
		}
	}
}

func lrLifeEventsSince(e *Engine, from int, p state.PlayerID) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events[from:] {
		if ev.Kind == events.LifeChange && ev.Player == p {
			out = append(out, ev)
		}
	}
	return out
}

func lrSetLife(e *Engine, p state.PlayerID, life int32) {
	e.emit(events.Event{Kind: events.LifeChange, Player: p, Amount: life - e.G.Players[p].Life})
}

// 1. {1}{W}, Plains + Treasure: the only plan is [Plains W, Treasure] with the
// Treasure's sacrifice disclosed; Submit sacrifices it through the ordinary
// forced-sacrifice settle (no ask), keeps its typed Treasure provenance, taps
// the Plains, puts the spell on the stack and leaves the pool empty.
func TestPaymentPlanLastResortTreasureSacrifice(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 12001, lrSpell("1 W"))
	plains := onBoard(t, e, 0, lrPlains)
	treasure := lrTreasure(t, e)
	got := e.PlanCastPayment(0, paymentCast(spell))
	lrAssertPlan(t, e, got, []lrStep{
		{plains, lrW, nil},
		{treasure, lrG, &decision.PaymentConsequence{Sacrifice: true}},
	})
	if a := got.Plan.Activations[1].Ability; a.Kind != decision.PaymentAbilityPrinted || a.Index != 0 {
		t.Fatalf("Treasure step ability = %+v, want printed ability 0", a)
	}
	if err := e.ValidateCastPayment(0, paymentCast(spell), *got.Plan); err != nil {
		t.Fatalf("ValidateCastPayment(offered last-resort plan): %v", err)
	}
	plan, mark := lrSubmit(t, e, spell)
	if !reflect.DeepEqual(plan.Activations, got.Plan.Activations) {
		t.Fatalf("offered plan %s differs from the pure query's %s", lrPlanString(plan.Activations), lrPlanString(got.Plan.Activations))
	}
	lrOnlyPriorityAsked(t, e, mark)
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending after Submit = %s, want priority", paymentPlanPendingSummary(d))
	}
	if o := e.G.Obj(treasure); o != nil && o.Zone == state.ZBattlefield {
		t.Fatal("the Treasure is still on the battlefield")
	}
	if !e.G.Obj(plains).Tapped {
		t.Fatal("the Plains was not tapped")
	}
	if e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("spell zone = %s, want stack", e.G.Obj(spell).Zone)
	}
	if pool := e.G.Players[0].Pool; pool.Total() != 0 {
		t.Fatalf("pool after the planned cast = %v, want empty", pool)
	}
	// The Treasure's production carried its typed producer tag into the pool,
	// and the payment spent that typed unit (a Treasure-tagged debit), so the
	// provenance survived to the spend.
	added, spent := false, false
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind != events.ManaAdd || !strings.Contains(ev.Counter, "Treasure") {
			continue
		}
		if ev.Amount > 0 {
			added = true
		} else if ev.Amount < 0 {
			spent = true
		}
	}
	if !added || !spent {
		t.Fatalf("Treasure-tagged mana added=%v spent=%v, want both: the typed producer provenance was lost", added, spent)
	}
	if units := e.G.Players[0].ManaUnits(); units != ([7]state.Mana{}) {
		t.Fatalf("typed pool after the cast = %v, want empty", units)
	}
}

// 2. The same board plus an Island: a normal plan exists, so the Treasure is
// never considered.
func TestPaymentPlanLastResortNormalPlanLeavesTreasure(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 12002, lrSpell("1 W"))
	plains := onBoard(t, e, 0, lrPlains)
	treasure := lrTreasure(t, e)
	island := onBoard(t, e, 0, lrIsland)
	lrAssertPlan(t, e, e.PlanCastPayment(0, paymentCast(spell)), []lrStep{{plains, lrW, nil}, {island, lrU, nil}})
	lrSubmit(t, e, spell)
	if o := e.G.Obj(treasure); o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("Treasure zone=%s tapped=%v, want untouched", o.Zone, o.Tapped)
	}
}

// 3. Arena's Treasure-versus-Mana-Vault calibration: {2} takes two Treasures
// (20) over the Vault (25); {3} takes the Vault (25) over three Treasures (30).
func TestPaymentPlanLastResortTreasuresVersusManaVault(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 12003, lrSpell("2"))
	t1, t2 := lrTreasure(t, e), lrTreasure(t, e)
	vault := lrCorpus(t, e, "Mana Vault")
	sac := &decision.PaymentConsequence{Sacrifice: true}
	lrAssertPlan(t, e, e.PlanCastPayment(0, paymentCast(spell)), []lrStep{{t1, lrG, sac}, {t2, lrG, sac}})
	_ = vault

	e3, _, spell3 := newFixtureDeck(t, 12004, lrSpell("3"))
	lrTreasure(t, e3)
	lrTreasure(t, e3)
	lrTreasure(t, e3)
	vault3 := lrCorpus(t, e3, "Mana Vault")
	lrAssertPlan(t, e3, e3.PlanCastPayment(0, paymentCast(spell3)), []lrStep{{vault3, lrC(3), &decision.PaymentConsequence{NoUntap: true}}})
	plan, mark := lrSubmit(t, e3, spell3)
	lrOnlyPriorityAsked(t, e3, mark)
	if len(plan.Activations) != 1 || !e3.G.Obj(vault3).Tapped || e3.G.Obj(spell3).Zone != state.ZStack {
		t.Fatalf("Vault tapped=%v spell zone=%s", e3.G.Obj(vault3).Tapped, e3.G.Obj(spell3).Zone)
	}
}

// 4. Island + Mana Confluence for {1}{U}: both, the Confluence step disclosing
// life:1, paid through a LifeChange event. At 1 life the Confluence has no
// alternative and the cast is insufficient.
func TestPaymentPlanLastResortManaConfluenceLife(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 12005, lrSpell("1 U"))
	island := onBoard(t, e, 0, lrIsland)
	confluence := lrCorpus(t, e, "Mana Confluence")
	lrAssertPlan(t, e, e.PlanCastPayment(0, paymentCast(spell)), []lrStep{
		{island, lrU, nil},
		{confluence, lrG, &decision.PaymentConsequence{Life: 1}},
	})
	_, mark := lrSubmit(t, e, spell)
	lrOnlyPriorityAsked(t, e, mark)
	if life := e.G.Players[0].Life; life != 19 {
		t.Fatalf("life after the planned cast = %d, want 19", life)
	}
	if lc := lrLifeEventsSince(e, mark, 0); len(lc) != 1 || lc[0].Amount != -1 {
		t.Fatalf("life events = %+v, want one LifeChange of -1", lc)
	}
	if e.G.Obj(spell).Zone != state.ZStack || !e.G.Obj(confluence).Tapped {
		t.Fatal("the spell is not on the stack or the Confluence is untapped")
	}

	e1, _, spell1 := newFixtureDeck(t, 12006, lrSpell("1 U"))
	onBoard(t, e1, 0, lrIsland)
	c1 := lrCorpus(t, e1, "Mana Confluence")
	lrSetLife(e1, 0, 1)
	// CR 119.4: at 1 life the Confluence's PayLife<1> is still payable, so the
	// shared gate offers it; the lethal guard is what removes it.
	if abilities := e1.availableManaAbilitiesForWindow(0, c1, false); len(abilities) != 1 {
		t.Fatalf("gate offers %d Confluence abilities at 1 life, want 1 (CR 119.4 payable)", len(abilities))
	}
	if alts := pay.PlanLastResortChoices(e1.paymentPlanQueryChoices(0), 1); lrUnitHasAlternative(e1, alts, c1) {
		t.Fatal("at 1 life the Confluence still has a phase-2 alternative")
	}
	if got := e1.PlanCastPayment(0, paymentCast(spell1)); got.Plan != nil || got.Reason != "insufficient" {
		t.Fatalf("at 1 life the plan = %s (reason %q), want insufficient", lrPlanString(lrActs(got)), got.Reason)
	}
}

// A PayLife<2> source at 1 life is refused by the shared gate itself (CR
// 119.4: life >= N), before any planner rule.
func TestPaymentPlanLastResortPayLifeGateCR1194(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 12007, lrSpell("U"))
	src := onBoard(t, e, 0, "Name:Dear Land\nTypes:Land\nA:AB$ Mana | Cost$ T PayLife<2> | Produced$ U | SpellDescription$ x\nOracle:x\n")
	lrSetLife(e, 0, 1)
	if abilities := e.availableManaAbilitiesForWindow(0, src, false); len(abilities) != 0 {
		t.Fatalf("gate offers a PayLife<2> ability at 1 life: %d", len(abilities))
	}
	if got := e.PlanCastPayment(0, paymentCast(spell)); got.Plan != nil {
		t.Fatalf("plan at 1 life = %s, want none", lrPlanString(got.Plan.Activations))
	}
	lrSetLife(e, 0, 3)
	lrAssertPlan(t, e, e.PlanCastPayment(0, paymentCast(spell)), []lrStep{{src, lrU, &decision.PaymentConsequence{Life: 2}}})
}

func lrActs(got PaymentPlanOutcome) []decision.PaymentActivation {
	if got.Plan == nil {
		return nil
	}
	return got.Plan.Activations
}

func lrUnitHasAlternative(e *Engine, choices [][]pay.Alt, id state.ObjID) bool {
	for _, alts := range choices {
		for _, a := range alts {
			if a.Activation.Source == id {
				return true
			}
		}
	}
	return false
}

// 5. {2} with only Ancient Tomb: damage:2 at 20 life; at 2 life the plan would
// kill its caster and is never offered (the fuzz self-kill).
func TestPaymentPlanLastResortAncientTombLethalGuard(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 12008, lrSpell("2"))
	tomb := lrCorpus(t, e, "Ancient Tomb")
	lrAssertPlan(t, e, e.PlanCastPayment(0, paymentCast(spell)), []lrStep{{tomb, lrC(2), &decision.PaymentConsequence{Damage: 2}}})
	_, mark := lrSubmit(t, e, spell)
	lrOnlyPriorityAsked(t, e, mark)
	if life := e.G.Players[0].Life; life != 18 {
		t.Fatalf("life after the Tomb plan = %d, want 18", life)
	}
	if e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("spell zone = %s, want stack", e.G.Obj(spell).Zone)
	}

	for _, tc := range []struct {
		life int32
		plan bool
	}{{2, false}, {1, false}, {3, true}} {
		e2, _, spell2 := newFixtureDeck(t, 12009, lrSpell("2"))
		lrCorpus(t, e2, "Ancient Tomb")
		lrSetLife(e2, 0, tc.life)
		got := e2.PlanCastPayment(0, paymentCast(spell2))
		if (got.Plan != nil) != tc.plan {
			t.Fatalf("life %d: plan = %s (reason %q), want plan=%v", tc.life, lrPlanString(lrActs(got)), got.Reason, tc.plan)
		}
		if !tc.plan && got.Reason != "insufficient" {
			t.Fatalf("life %d: reason = %q, want insufficient", tc.life, got.Reason)
		}
		d := paymentPlanReask(t, e2)
		for _, a := range d.PaymentActions {
			if a.Cast.Object == spell2 && !tc.plan {
				t.Fatalf("life %d: offered %s", tc.life, lrPlanString(a.Plans[0].Activations))
			}
		}
	}
}

// 6. City of Brass alone for {R}: damage:1 from its own Taps trigger, which
// after Submit is on the stack above the spell, as for a manual activation in
// the 601.2g window.
func TestPaymentPlanLastResortCityOfBrassTrigger(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 12010, lrSpell("R"))
	city := lrCorpus(t, e, "City of Brass")
	lrAssertPlan(t, e, e.PlanCastPayment(0, paymentCast(spell)), []lrStep{{city, lrR, &decision.PaymentConsequence{Damage: 1}}})
	_, mark := lrSubmit(t, e, spell)
	lrOnlyPriorityAsked(t, e, mark)
	st := e.G.Stack
	if len(st) < 2 || st[len(st)-2] != spell {
		t.Fatalf("stack = %v, want the spell under the City trigger", st)
	}
	if top := e.G.Obj(st[len(st)-1]); top == nil || top.Source != city {
		t.Fatalf("stack top = %+v, want City of Brass's Taps trigger", top)
	}
	if life := e.G.Players[0].Life; life != 20 {
		t.Fatalf("life = %d before the trigger resolves, want 20", life)
	}

	// The manual route: the same board, the cast answered by hand, puts the
	// same trigger at the same place.
	m, _, mspell := newFixtureDeck(t, 12010, lrSpell("R"))
	mcity := lrCorpus(t, m, "City of Brass")
	m.pending = nil
	m.beginCast(0, decision.Option{Kind: "cast", Obj: mspell})
	m.Advance()
	lrManualPay(t, m, mcity, "R")
	mst := m.G.Stack
	if len(mst) != len(st) || mst[len(mst)-2] != mspell || m.G.Obj(mst[len(mst)-1]).Source != mcity {
		t.Fatalf("manual stack = %v, planned %v: the trigger's place differs", mst, st)
	}
}

// lrManualPay answers the cast's CR 601.2g window by activating source and,
// on a colour ask, picking colour.
func lrManualPay(t *testing.T, e *Engine, source state.ObjID, colour string) {
	t.Helper()
	for i := 0; i < 8; i++ {
		d := e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			return
		}
		pick := -1
		for _, o := range d.Options {
			if o.Obj == source && (o.ManaSymbol == "" || o.ManaSymbol == colour) || o.Label == colour || strings.HasSuffix(o.Label, "{"+colour+"}") || o.ManaSymbol == colour {
				pick = o.Index
				break
			}
		}
		if pick < 0 {
			t.Fatalf("manual window has no option for %d/%s: %+v", source, colour, d.Options)
		}
		submitChoices(t, e, pick)
	}
	t.Fatal("manual payment did not finish")
}

// 7. Undiscovered Paradise alone for {G}: return_to_hand.
func TestPaymentPlanLastResortUndiscoveredParadise(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 12011, lrSpell("G"))
	paradise := lrCorpus(t, e, "Undiscovered Paradise")
	lrAssertPlan(t, e, e.PlanCastPayment(0, paymentCast(spell)), []lrStep{{paradise, lrG, &decision.PaymentConsequence{ReturnToHand: true}}})
	_, mark := lrSubmit(t, e, spell)
	lrOnlyPriorityAsked(t, e, mark)
	if e.G.Obj(spell).Zone != state.ZStack || !e.G.Obj(paradise).Tapped {
		t.Fatal("the spell is not on the stack or the Paradise is untapped")
	}
}

// 8. A normal plan always wins, even one that taps a creature: Forest +
// Llanowar Elves (not summoning sick) + Treasure for {1}{G}.
func TestPaymentPlanLastResortNormalCreatureBeatsTreasure(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 12012, lrSpell("1 G"))
	forest := onBoard(t, e, 0, lrForest)
	elves := lrCorpus(t, e, "Llanowar Elves")
	treasure := lrTreasure(t, e)
	lrAssertPlan(t, e, e.PlanCastPayment(0, paymentCast(spell)), []lrStep{{forest, lrG, nil}, {elves, lrG, nil}})
	lrSubmit(t, e, spell)
	if o := e.G.Obj(treasure); o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatal("the Treasure was used although a normal plan exists")
	}
}

// Eldrazi Spawn's Sac<1/CARDNAME> (no {T}) is a creature sacrifice (20); an
// artifact sacrifice (10) is preferred to it.
func TestPaymentPlanLastResortCreatureSacrificeWeighsMore(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 12013, lrSpell("1"))
	spawn := onBoard(t, e, 0, "Name:Spawn Test\nTypes:Creature Eldrazi Spawn\nPT:0/1\nA:AB$ Mana | Cost$ Sac<1/CARDNAME> | Produced$ C | SpellDescription$ x\nOracle:x\n")
	lrAssertPlan(t, e, e.PlanCastPayment(0, paymentCast(spell)), []lrStep{{spawn, lrC(1), &decision.PaymentConsequence{Sacrifice: true}}})
	_, mark := lrSubmit(t, e, spell)
	lrOnlyPriorityAsked(t, e, mark)
	if o := e.G.Obj(spawn); o != nil && o.Zone == state.ZBattlefield {
		t.Fatal("the Spawn is still on the battlefield")
	}

	e2, _, spell2 := newFixtureDeck(t, 12014, lrSpell("1"))
	onBoard(t, e2, 0, "Name:Spawn Test\nTypes:Creature Eldrazi Spawn\nPT:0/1\nA:AB$ Mana | Cost$ Sac<1/CARDNAME> | Produced$ C | SpellDescription$ x\nOracle:x\n")
	treasure := lrTreasure(t, e2)
	lrAssertPlan(t, e2, e2.PlanCastPayment(0, paymentCast(spell2)), []lrStep{{treasure, lrG, &decision.PaymentConsequence{Sacrifice: true}}})
}

// 9a. The Treasure removed between offer and execution: automation falls
// back source_changed before any tap.
func TestPaymentPlanLastResortSourceRemovedFallsBack(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 12015, lrShock("1 W"))
	plains := onBoard(t, e, 0, lrPlains)
	treasure := lrTreasure(t, e)
	plan, _ := lrSubmit(t, e, spell)
	if len(plan.Activations) != 2 || plan.Activations[1].Consequence == nil {
		t.Fatalf("offered plan = %s, want Plains + Treasure", lrPlanString(plan.Activations))
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: treasure, From: state.ZBattlefield, To: state.ZGraveyard})
	submitChoices(t, e, 0) // the target
	nd := e.Pending()
	if nd == nil || nd.PaymentFallback == nil || nd.PaymentFallback.Reason != paymentFallbackSourceChanged || nd.PaymentFallback.PlanID != plan.ID {
		t.Fatalf("pending = %s, want a source_changed fallback for %s", paymentPlanPendingSummary(nd), plan.ID)
	}
	if e.G.Obj(plains).Tapped {
		t.Fatal("the Plains was tapped before the fallback")
	}
}

// 9b. The caster's life lowered between offer and execution so the remaining
// life payment is lethal: cost_changed before any tap.
func TestPaymentPlanLastResortLethalAfterOfferFallsBack(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 12016, lrShock("1 U"))
	island := onBoard(t, e, 0, lrIsland)
	confluence := lrCorpus(t, e, "Mana Confluence")
	plan, _ := lrSubmit(t, e, spell)
	if len(plan.Activations) != 2 {
		t.Fatalf("offered plan = %s, want Island + Confluence", lrPlanString(plan.Activations))
	}
	lrSetLife(e, 0, 1)
	start := len(e.L.Events)
	submitChoices(t, e, 0) // the target
	nd := e.Pending()
	if nd == nil || nd.PaymentFallback == nil || nd.PaymentFallback.Reason != paymentFallbackCostChanged {
		t.Fatalf("pending = %s, want a cost_changed fallback", paymentPlanPendingSummary(nd))
	}
	if e.G.Obj(island).Tapped || e.G.Obj(confluence).Tapped {
		t.Fatal("a planned source was tapped before the lethal fallback")
	}
	if lc := lrLifeEventsSince(e, start, 0); len(lc) != 0 {
		t.Fatalf("life moved during the fallback: %+v", lc)
	}
}

// 9c. The lethal revalidation runs before every LATER step too, against the
// remaining steps only: with the Tomb step counted as completed, the
// remaining Confluence life:1 is lethal at 1 life (cost_changed) and not at 2.
// (A board-level trigger for this mid-plan -- a damage doubler making the
// Tomb deal 4 -- is not usable here: this engine does not apply a DamageDone
// replacement to a mana ability's damage rider.)
func TestPaymentPlanLastResortLethalBeforeLaterStep(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 12017, lrShock("2 U"))
	tomb := lrCorpus(t, e, "Ancient Tomb")
	lrCorpus(t, e, "Mana Confluence")
	lrSetLife(e, 0, 5)
	plan, _ := lrSubmit(t, e, spell)
	if len(plan.Activations) != 2 || plan.Activations[0].Source != tomb {
		t.Fatalf("offered plan = %s, want Tomb then Confluence", lrPlanString(plan.Activations))
	}
	pc := e.cast
	if pc == nil || pc.payment == nil {
		t.Fatal("precondition: the planned cast is not pending on its target")
	}
	if reason := e.paymentPlanCheck(pc); reason != "" {
		t.Fatalf("check before the first step = %q, want ready", reason)
	}
	lrSetLife(e, 0, 3) // remaining 2 + 1 = 3: lethal before the first step
	if reason := e.paymentPlanCheck(pc); reason != paymentFallbackCostChanged {
		t.Fatalf("check at life 3 before the first step = %q, want cost_changed", reason)
	}
	pc.paymentNext = 1 // the Tomb step counted as completed
	defer func() { pc.paymentNext = 0 }()
	lrSetLife(e, 0, 1)
	if reason := e.paymentPlanCheck(pc); reason != paymentFallbackCostChanged {
		t.Fatalf("check before the Confluence at life 1 = %q, want cost_changed", reason)
	}
	lrSetLife(e, 0, 2)
	if reason := e.paymentPlanCheck(pc); reason == paymentFallbackCostChanged {
		t.Fatal("check before the Confluence at life 2 reports cost_changed, but 1 life is not lethal")
	}
}

// A consequence that changes between offer and execution is production_changed:
// a Treasure-shaped rock whose sacrifice cost is gone is a different step.
func TestPaymentPlanLastResortConsequenceChangeIsProductionChanged(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 12018, lrShock("R"))
	city := lrCorpus(t, e, "City of Brass")
	plan, _ := lrSubmit(t, e, spell)
	if len(plan.Activations) != 1 {
		t.Fatalf("offered plan = %s, want City of Brass", lrPlanString(plan.Activations))
	}
	pa := plan.Activations[0]
	units := e.paymentPlanManaUnits(0)
	if _, reason := pay.PaymentPlanStepReady(asPayer(e), 0, units, pa); reason != "" {
		t.Fatalf("offered step not ready: %s", reason)
	}
	pa.Consequence = &decision.PaymentConsequence{Damage: 2}
	if _, reason := pay.PaymentPlanStepReady(asPayer(e), 0, units, pa); reason != paymentFallbackProductionChanged {
		t.Fatalf("a changed consequence = %q, want production_changed", reason)
	}
	pa.Consequence = nil
	if _, reason := pay.PaymentPlanStepReady(asPayer(e), 0, units, pa); reason != paymentFallbackProductionChanged {
		t.Fatalf("a dropped consequence = %q, want production_changed", reason)
	}
	_ = city
}

// The phase rule holds for validation too: a witness whose summed life +
// damage is lethal is refused, and so is a last-resort step while a normal
// plan exists.
func TestPaymentPlanLastResortValidateRejects(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 12019, lrSpell("2"))
	tomb := lrCorpus(t, e, "Ancient Tomb")
	got := e.PlanCastPayment(0, paymentCast(spell))
	if got.Plan == nil {
		t.Fatal("no Tomb plan")
	}
	lrSetLife(e, 0, 2)
	if err := e.ValidateCastPayment(0, paymentCast(spell), *got.Plan); err == nil {
		t.Fatal("ValidateCastPayment accepted a lethal witness")
	}
	lrSetLife(e, 0, 20)
	onBoard(t, e, 0, lrPlains)
	onBoard(t, e, 0, lrPlains)
	e.staticEpoch, e.activeEpoch = -1, -1
	if err := e.ValidateCastPayment(0, paymentCast(spell), *got.Plan); err == nil {
		t.Fatal("ValidateCastPayment accepted a last-resort witness while a normal plan exists")
	}
	_ = tomb
}

// The search against the brute-force oracle on random boards mixing normal
// and last-resort sources at random life totals: whenever phase 1 has no
// plan, the phase-2 search returns exactly the oracle's best non-lethal plan
// over the phase-2 table, or insufficient when the oracle has none.
func TestPaymentPlanLastResortSearchMatchesOracle(t *testing.T) {
	t.Parallel()
	treasure, ok := testutil.CorpusRegistry(t).Token("c_a_treasure_sac")
	if !ok {
		t.Fatal("corpus token c_a_treasure_sac missing")
	}
	corpus := map[string]*cards.Card{}
	for _, n := range []string{"Ancient Tomb", "City of Brass", "Mana Confluence", "Mana Vault", "Adarkar Wastes", "Horizon Canopy", "Lotus Petal"} {
		corpus[n] = corpusCard(t, n)
	}
	lastResort := []string{"treasure", "spawn", "Ancient Tomb", "City of Brass", "Mana Confluence", "Mana Vault", "Adarkar Wastes", "Horizon Canopy", "Lotus Petal"}
	normal := []string{srchIsland, srchMountain, srchPlains, srchWastes}
	rng := rand.New(rand.NewPCG(20260927, 7))
	phase2, none, used := 0, 0, 0
	for board := 0; board < 80; board++ {
		e, _, spell := newFixtureDeck(t, 12100+uint64(board), srchSpell("U"))
		for n := rng.IntN(3); n > 0; n-- {
			onBoard(t, e, 0, normal[rng.IntN(len(normal))])
		}
		for n := 1 + rng.IntN(5); n > 0; n-- {
			var id state.ObjID
			switch k := lastResort[rng.IntN(len(lastResort))]; k {
			case "treasure":
				id = onBoardCard(t, e, 0, treasure)
			case "spawn":
				id = onBoard(t, e, 0, "Name:Spawn Test\nTypes:Creature Eldrazi Spawn\nPT:0/1\nA:AB$ Mana | Cost$ Sac<1/CARDNAME> | Produced$ C | SpellDescription$ x\nOracle:x\n")
			default:
				id = onBoardCard(t, e, 0, corpus[k])
			}
			e.G.Obj(id).SummonSick = false
		}
		lrSetLife(e, 0, int32(1+rng.IntN(8)))
		life := e.G.Players[0].Life
		all := e.paymentPlanQueryChoices(0)
		normalTable := pay.PhaseChoices(all, pay.TierNormal)
		table := pay.PlanLastResortChoices(all, life)
		demand := pay.PaymentPlanHandDemand(asPayer(e), 0, spell)
		for k := 0; k < 4; k++ {
			var c Cost
			c.Generic = int32(rng.IntN(4))
			for n := rng.IntN(3); n > 0; n-- {
				c.Colored[state.ManaIndex("WUBRGC"[rng.IntN(6)])]++
			}
			if w, _ := paymentPlanSearchOracleOver(e, 0, c, normalTable, demand); w != nil {
				continue // phase 1 answers; covered by the ordinary oracle tests
			}
			want, _ := paymentPlanSearchOracleOver(e, 0, c, table, demand)
			got := e.planPaymentCost(0, paymentCast(spell), c)
			what := fmt.Sprintf("board %d life %d cost %+v", board, life, c)
			if want == nil {
				if got.Plan != nil || got.Reason != "insufficient" {
					t.Fatalf("%s: search = %s (reason %q), oracle none", what, lrPlanString(lrActs(got)), got.Reason)
				}
				none++
				continue
			}
			if got.Reason != "" || got.Plan == nil || !reflect.DeepEqual(*got.Plan, *want) {
				t.Fatalf("%s: search (%q, %d nodes) differs from the oracle\nsearch: %s\noracle: %s", what, got.Reason, got.Nodes, lrPlanString(lrActs(got)), lrPlanString(want.Activations))
			}
			var pain uint32
			for _, a := range got.Plan.Activations {
				if a.Consequence != nil {
					used++
					pain += a.Consequence.Life + a.Consequence.Damage
				}
			}
			if pain > 0 && int32(pain) >= life {
				t.Fatalf("%s: plan pain %d at life %d is lethal", what, pain, life)
			}
			phase2++
		}
	}
	if phase2 == 0 || used == 0 || none == 0 {
		t.Fatalf("phase-2 coverage: %d planned (%d last-resort steps), %d insufficient", phase2, used, none)
	}
	t.Logf("phase-2 boards: %d planned (%d last-resort steps) and %d insufficient agree with the oracle", phase2, used, none)
}

// Combo halves (brief item 2): a Talisman's painful Combo half and a horizon
// land's PayLife Combo expand one alternative per listed colour, each
// disclosing its consequence, while the Talisman's painless {C} ability stays
// normal and pays generic without any consequence.
func TestPaymentPlanLastResortComboHalves(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 12021, lrSpell("B"))
	talisman := lrCorpus(t, e, "Talisman of Dominance")
	lrAssertPlan(t, e, e.PlanCastPayment(0, paymentCast(spell)), []lrStep{
		{talisman, decision.ManaAmount{0, 0, 1, 0, 0, 0}, &decision.PaymentConsequence{Damage: 1}}})
	generic := choiceHand(t, e, lrSpell("1"))
	lrAssertPlan(t, e, e.PlanCastPayment(0, paymentCast(generic)), []lrStep{{talisman, lrC(1), nil}})
	if got := e.PlanCastPayment(0, paymentCast(generic)); got.Plan.Activations[0].Ability.Index != 0 {
		t.Fatalf("{1} plan uses Talisman ability %d, want its painless {C} ability 0", got.Plan.Activations[0].Ability.Index)
	}

	e2, _, spell2 := newFixtureDeck(t, 12022, lrSpell("W"))
	canopy := lrCorpus(t, e2, "Horizon Canopy")
	lrAssertPlan(t, e2, e2.PlanCastPayment(0, paymentCast(spell2)), []lrStep{{canopy, lrW, &decision.PaymentConsequence{Life: 1}}})
	_, mark := lrSubmit(t, e2, spell2)
	lrOnlyPriorityAsked(t, e2, mark)
	if life := e2.G.Players[0].Life; life != 19 || e2.G.Obj(spell2).Zone != state.ZStack {
		t.Fatalf("life = %d, spell zone = %s; want 19 and the stack", life, e2.G.Obj(spell2).Zone)
	}
}
