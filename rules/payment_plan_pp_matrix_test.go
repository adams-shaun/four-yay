package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const (
	ppMountain = "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n"
	ppIsland   = "Name:Island\nTypes:Basic Land Island\nOracle:x\n"
	ppSwamp    = "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n"
	ppShock    = "Name:Planned Shock\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"
	ppShock2   = "Name:Planned Blast\nManaCost:R R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 2\nOracle:x\n"
)

func ppAsk(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %#v, want seat-0 priority", d)
	}
	// Payment actions are published lazily (aph-lazy-offers): build them
	// for this ask, as an opted-in consumer would.
	e.EnsurePaymentActions()
	return d
}
func ppAction(t *testing.T, d *decision.Decision, spell state.ObjID) decision.PaymentAction {
	t.Helper()
	for _, a := range d.PaymentActions {
		if a.Cast.Object == spell && len(a.Plans) > 0 {
			return a
		}
	}
	t.Fatalf("no payment action for %d", spell)
	return decision.PaymentAction{}
}
func ppHasAction(d *decision.Decision, spell state.ObjID) bool {
	for _, a := range d.PaymentActions {
		if a.Cast.Object == spell {
			return true
		}
	}
	return false
}
func ppSubmitPlan(t *testing.T, e *Engine, d *decision.Decision, a decision.PaymentAction) {
	t.Helper()
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Payment: &decision.PaymentSelection{ActionID: a.ID, Plan: a.Plans[0]}}); err != nil {
		t.Fatalf("Submit planned cast: %v", err)
	}
}
func ppProduced(e *Engine, from int) []string {
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
func ppTaps(e *Engine, from int, id state.ObjID) int {
	n := 0
	for _, ev := range e.L.Events[from:] {
		if ev.Kind == events.Tap && ev.Obj == id {
			n++
		}
	}
	return n
}

func TestPaymentPlanPrefersMountainOverBadlandsAndLeavesItUntapped(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9310, "Name:Grixis Plan\nManaCost:1 U B\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	onBoard(t, e, 0, ppIsland)
	onBoard(t, e, 0, ppSwamp)
	badlands := onBoard(t, e, 0, "Name:Badlands Test\nTypes:Land Swamp Mountain\nOracle:x\n")
	mountain := onBoard(t, e, 0, ppMountain)
	d := ppAsk(t, e)
	a := ppAction(t, d, spell)
	again := e.PaymentActionsForPriority(0, d.Seq)
	if !reflect.DeepEqual(d.PaymentActions, again) {
		t.Fatalf("repeated offer differs:\n%#v\n%#v", d.PaymentActions, again)
	}
	used := map[state.ObjID]bool{}
	for _, act := range a.Plans[0].Activations {
		used[act.Source] = true
	}
	if !used[mountain] || used[badlands] {
		t.Fatalf("witness %#v, want the Mountain and not the Badlands", a.Plans[0].Activations)
	}
	ppSubmitPlan(t, e, d, a)
	if e.G.Obj(badlands).Tapped || !e.G.Obj(mountain).Tapped {
		t.Fatalf("after execution badlands tapped=%v mountain tapped=%v", e.G.Obj(badlands).Tapped, e.G.Obj(mountain).Tapped)
	}
}

func TestPaymentPlanPP01PaysGenericWithMountainOnExactBoard(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9300, "Name:Grixis Plan\nManaCost:1 U B\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	island := onBoard(t, e, 0, ppIsland)
	swamp := onBoard(t, e, 0, ppSwamp)
	mountain := onBoard(t, e, 0, ppMountain)
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("precondition: mana pool = %v, want empty", e.G.Players[0].Pool)
	}
	d := ppAsk(t, e)
	a := ppAction(t, d, spell)
	used := make(map[state.ObjID]bool)
	for _, activation := range a.Plans[0].Activations {
		used[activation.Source] = true
	}
	if len(used) != 3 || !used[island] || !used[swamp] || !used[mountain] {
		t.Fatalf("PP-01 witness sources = %#v, want Island, Swamp and Mountain", a.Plans[0].Activations)
	}
	ppSubmitPlan(t, e, d, a)
	if e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("planned Grixis spell zone = %s, want stack", e.G.Obj(spell).Zone)
	}
}

func TestPaymentPlanFixedMultiOutputLeavesSurplusFloating(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9311, "Name:Generic Plan\nManaCost:1\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	rock := onBoard(t, e, 0, "Name:Twin Rock\nTypes:Artifact\nA:AB$ Mana | Cost$ T | Produced$ C | Amount$ 2\nOracle:x\n")
	d := ppAsk(t, e)
	a := ppAction(t, d, spell)
	p := a.Plans[0]
	if len(p.Activations) != 1 || p.Activations[0].Source != rock || p.Activations[0].Produces[5] != 2 || p.PoolAfter[5] != 1 {
		t.Fatalf("witness = %#v, want the rock for CC leaving one C", p)
	}
	ppSubmitPlan(t, e, d, a)
	if got := e.G.Players[0].Pool; got[state.ManaIndex('C')] != 1 || got.Total() != 1 {
		t.Fatalf("pool after planned payment = %v, want exactly one floating C", got)
	}
}

func TestPaymentPlanSourceEligibilityShapes(t *testing.T) {
	const dork = "Name:Elf Dork\nTypes:Creature Elf\nPT:1/1\nA:AB$ Mana | Cost$ T | Produced$ G\nOracle:x\n"
	const hasty = "Name:Hasty Dork\nTypes:Creature Elf\nPT:1/1\nK:Haste\nA:AB$ Mana | Cost$ T | Produced$ G\nOracle:x\n"
	const greenSpell = "Name:Green Plan\nManaCost:G\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n"
	t.Run("ready dork funds", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9320, greenSpell)
		id := onBoardReady(t, e, 0, dork)
		if a := ppAction(t, ppAsk(t, e), spell); a.Plans[0].Activations[0].Source != id {
			t.Fatalf("witness %#v", a.Plans[0])
		}
	})
	t.Run("sick dork with haste admitted", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9322, greenSpell)
		onBoard(t, e, 0, hasty)
		ppAction(t, ppAsk(t, e), spell)
	})
	t.Run("tapped excluded", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9323, greenSpell)
		id := onBoardReady(t, e, 0, dork)
		e.emit(events.Event{Kind: events.Tap, Obj: id})
		if ppHasAction(ppAsk(t, e), spell) {
			t.Fatal("tapped dork funded a plan")
		}
	})
	t.Run("phased out excluded", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9324, greenSpell)
		id := onBoardReady(t, e, 0, dork)
		e.emit(events.Event{Kind: events.PhaseOut, Obj: id, Amount: 1})
		if !e.G.Obj(id).PhasedOut {
			t.Fatal("fixture did not phase the dork out")
		}
		if ppHasAction(ppAsk(t, e), spell) {
			t.Fatal("phased-out dork funded a plan")
		}
	})
	t.Run("opponent source excluded", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9325, greenSpell)
		onBoardReady(t, e, 1, dork)
		if ppHasAction(ppAsk(t, e), spell) {
			t.Fatal("opponent's dork funded seat 0's plan")
		}
	})
	t.Run("activation prohibited excluded", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9326, greenSpell)
		onBoardReady(t, e, 0, dork)
		onBoard(t, e, 1, "Name:Cursed Totem Test\nTypes:Artifact\nS:Mode$ CantBeActivated | AffectedZone$ Battlefield | ValidCard$ Creature | ValidSA$ Activated | Description$ x\nOracle:x\n")
		if ppHasAction(ppAsk(t, e), spell) {
			t.Fatal("activation-prohibited dork funded a plan")
		}
	})
}

func TestPaymentPlanLegalityGatesSuppressPlans(t *testing.T) {
	t.Run("sorcery outside main phase", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9330, "Name:Slow Plan\nManaCost:R\nTypes:Sorcery\nA:SP$ Draw | Num$ 1\nOracle:x\n")
		for i := 0; i < 4; i++ {
			onBoard(t, e, 0, ppMountain)
		}
		if e.G.Step.IsMain() {
			t.Fatal("fixture unexpectedly in a main phase")
		}
		if ppHasAction(ppAsk(t, e), spell) {
			t.Fatal("sorcery offered a plan outside sorcery timing")
		}
	})
	t.Run("no legal target", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9331, "Name:Kill Plan\nManaCost:R\nTypes:Instant\nA:SP$ Destroy | ValidTgts$ Creature\nOracle:x\n")
		for i := 0; i < 4; i++ {
			onBoard(t, e, 0, ppMountain)
		}
		if ppHasAction(ppAsk(t, e), spell) {
			t.Fatal("targetless spell offered a plan")
		}
	})
	t.Run("cant be cast", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9332, "Name:Hushed Plan\nManaCost:R\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
		for i := 0; i < 4; i++ {
			onBoard(t, e, 0, ppMountain)
		}
		onBoard(t, e, 1, "Name:Hush Test\nTypes:Artifact\nS:Mode$ CantBeCast | ValidCard$ Instant | Description$ x\nOracle:x\n")
		if ppHasAction(ppAsk(t, e), spell) {
			t.Fatal("CantBeCast spell offered a plan")
		}
	})
	t.Run("taxed cost", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9333, "Name:Taxed Plan\nManaCost:R\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
		for i := 0; i < 3; i++ {
			onBoard(t, e, 0, ppMountain)
		}
		onBoard(t, e, 1, "Name:Thalia Test\nTypes:Artifact\nS:Mode$ RaiseCost | ValidCard$ Card.nonCreature | Type$ Spell | Amount$ 1 | Description$ x\nOracle:x\n")
		a := ppAction(t, ppAsk(t, e), spell)
		if c := a.Plans[0].Cost; c.Generic != 1 || c.Mana[state.ManaIndex('R')] != 1 || len(a.Plans[0].Activations) != 2 {
			t.Fatalf("taxed witness = %#v, want {1}{R} from two sources", a.Plans[0])
		}
	})
}

func TestPaymentPlanProducerAndPoolExclusions(t *testing.T) {
	const redSpell = "Name:Red Plan\nManaCost:R\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n"
	t.Run("unsupported producer does not block a basic plan", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9350, redSpell)
		onBoard(t, e, 0, "Name:Pain Rock\nTypes:Artifact\nA:AB$ Mana | Cost$ T PayLife<1> | Produced$ R\nOracle:x\n")
		mtn := onBoard(t, e, 0, ppMountain)
		if a := ppAction(t, ppAsk(t, e), spell); a.Plans[0].Activations[0].Source != mtn {
			t.Fatalf("witness %#v, want the Mountain", a.Plans[0])
		}
	})
	t.Run("costly producer funds only as a disclosed last resort", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9351, redSpell)
		onBoard(t, e, 0, "Name:Pain Rock\nTypes:Artifact\nA:AB$ Mana | Cost$ T PayLife<1> | Produced$ R\nOracle:x\n")
		a := ppAction(t, ppAsk(t, e), spell)
		if acts := a.Plans[0].Activations; len(acts) != 1 || acts[0].Consequence == nil || *acts[0].Consequence != (decision.PaymentConsequence{Life: 1}) {
			t.Fatalf("life-cost producer witness = %#v, want one step disclosing life:1", a.Plans[0])
		}
	})
	t.Run("restricted producer never funds", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9352, redSpell)
		onBoard(t, e, 0, "Name:Picky Rock\nTypes:Artifact\nA:AB$ Mana | Cost$ T | Produced$ R | RestrictValid$ Spell.Creature\nOracle:x\n")
		if ppHasAction(ppAsk(t, e), spell) {
			t.Fatal("restricted producer funded a plan")
		}
	})
	// PP-09 / spec §3.2: a source another object's tap/mana trigger can match
	// is deferred (not scheduled), so it may never appear in a witness. The
	// engine's V1 gate is stricter than per-source deferral -- it declines the
	// whole offer while any TapsForMana trigger is live -- but the deferred
	// source is never scheduled either way, which is the property asserted
	// here. The over-conservative global shape is named in the report.
	t.Run("source matched by a taps-for-mana trigger is deferred", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9353, redSpell)
		mtn := onBoard(t, e, 0, ppMountain)
		onBoard(t, e, 1, "Name:Flare Test\nTypes:Enchantment\nT:Mode$ TapsForMana | ValidCard$ Land | Execute$ TrigMana | TriggerZones$ Battlefield | Static$ True | TriggerDescription$ x\nSVar:TrigMana:DB$ ManaReflected | ColorOrType$ Type | ReflectProperty$ Produced | Defined$ TriggeredActivator\nOracle:x\n")
		d := ppAsk(t, e)
		for _, a := range d.PaymentActions {
			for _, p := range a.Plans {
				for _, act := range p.Activations {
					if act.Source == mtn {
						t.Fatalf("matched source scheduled despite the deferral: %#v", p.Activations)
					}
				}
			}
		}
		if ppHasAction(d, spell) {
			t.Fatalf("a plan funded the spell from a source a TapsForMana trigger can match: %#v", d.PaymentActions)
		}
	})
	t.Run("restricted floating pool declines", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9354, redSpell)
		onBoard(t, e, 0, ppMountain)
		e.G.Players[0].RestrictedMana = append(e.G.Players[0].RestrictedMana, state.ManaRestriction{Color: "R", Amount: 1, Valid: "Spell.Creature"})
		if ppHasAction(ppAsk(t, e), spell) {
			t.Fatal("restricted pool was planned around as ordinary mana")
		}
	})
}

func TestPaymentPlanSearchLimitDeterministic(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9360, "Name:Huge Plan\nManaCost:12\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	for i := 0; i < 30; i++ {
		onBoard(t, e, 0, ppMountain)
	}
	for i := 0; i < 6; i++ {
		onBoard(t, e, 0, "Name:Volcanic Test\nTypes:Land Island Mountain\nOracle:x\n")
	}
	first := e.PlanCastPayment(0, paymentCast(spell))
	second := e.PlanCastPayment(0, paymentCast(spell))
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("search-limit outcome not deterministic:\n%#v\n%#v", first, second)
	}
	if first.Nodes > decision.MaxPaymentPlanSearchNodes {
		t.Fatalf("nodes = %d beyond the V1 bound", first.Nodes)
	}
	if first.Plan != nil {
		if err := e.ValidateCastPayment(0, paymentCast(spell), *first.Plan); err != nil {
			t.Fatalf("limited search returned an unvalidatable plan: %v", err)
		}
	}
	t.Logf("12-generic spell over 36 sources: reason=%q nodes=%d plan=%v", first.Reason, first.Nodes, first.Plan != nil)
}

func TestPaymentPlanEngineRejectsForgedWitnessBeforeMutation(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9370, "Name:Red Plan\nManaCost:R\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	onBoard(t, e, 0, ppMountain)
	d := ppAsk(t, e)
	a := ppAction(t, d, spell)
	forged := decision.ClonePaymentPlan(a.Plans[0])
	forged.Activations[0].Produces[state.ManaIndex('R')] = 2
	forged.PoolAfter[state.ManaIndex('R')] = 1
	id, err := decision.PaymentPlanID(d.Seq, d.Player, a.Cast, forged)
	if err != nil {
		t.Fatal(err)
	}
	forged.ID = id
	d.PaymentActions[0].Plans[0] = forged // simulate an inconsistent publisher
	events0, head0, intents0, rng0 := len(e.L.Events), e.L.Head(), len(e.L.Intents), e.G.Clone()
	err = e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Payment: &decision.PaymentSelection{ActionID: a.ID, Plan: forged}})
	if err == nil {
		t.Fatal("engine accepted a forged production witness")
	}
	if len(e.L.Events) != events0 || e.L.Head() != head0 || len(e.L.Intents) != intents0 || e.Pending() != d ||
		!reflect.DeepEqual(rng0, e.G) {
		t.Fatal("rejected payment mutated the engine")
	}
}

func TestPaymentPlanTargetsPrecedePlannedActivation(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9308, ppShock)
	mtn := onBoard(t, e, 0, ppMountain)
	d := ppAsk(t, e)
	a := ppAction(t, d, spell)
	start := len(e.L.Events)
	ppSubmitPlan(t, e, d, a)
	td := e.Pending()
	if td == nil || td.Kind != decision.KTarget {
		t.Fatalf("pending = %#v, want the CR 601.2c target ask", td)
	}
	if e.G.Obj(mtn).Tapped || len(ppProduced(e, start)) != 0 {
		t.Fatal("planned source activated before targets were chosen")
	}
	submitChoices(t, e, 0)
	nd := e.Pending()
	if nd == nil || nd.Kind != decision.KPriority || nd.PaymentFallback != nil {
		t.Fatalf("after the target answer pending = %#v, want priority with no extra tap/colour ask", nd)
	}
	if !e.G.Obj(mtn).Tapped || e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("mountain tapped=%v spell zone=%s", e.G.Obj(mtn).Tapped, e.G.Obj(spell).Zone)
	}
}

func TestPaymentPlanPlannedMatchesManualExecution(t *testing.T) {
	build := func() (*Engine, state.ObjID, []state.ObjID) {
		e, _, spell := newFixtureDeck(t, 9309, "Name:Drake Plan\nManaCost:1 U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
		ids := []state.ObjID{onBoard(t, e, 0, ppIsland), onBoard(t, e, 0, ppSwamp)}
		return e, spell, ids
	}
	planned, spell, lands := build()
	d := ppAsk(t, planned)
	a := ppAction(t, d, spell)
	ppSubmitPlan(t, planned, d, a)

	manual, mspell, mlands := build()
	md := ppAsk(t, manual)
	for i, id := range mlands {
		idx := -1
		for _, o := range manual.Pending().Options {
			if o.Kind == "activate" && o.Obj == id {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("manual activation %d missing: %#v", i, manual.Pending().Options)
		}
		submitChoices(t, manual, idx)
	}
	idx := -1
	for _, o := range manual.Pending().Options {
		if o.Kind == "cast" && o.Obj == mspell {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("manual cast missing after floating: %#v", manual.Pending().Options)
	}
	submitChoices(t, manual, idx)
	_ = md
	for i := range lands {
		if planned.G.Obj(lands[i]).Tapped != manual.G.Obj(mlands[i]).Tapped {
			t.Fatalf("land %d tapped planned=%v manual=%v", i, planned.G.Obj(lands[i]).Tapped, manual.G.Obj(mlands[i]).Tapped)
		}
	}
	po, mo := planned.G.Obj(spell), manual.G.Obj(mspell)
	if po.Zone != mo.Zone || po.ManaSpent != mo.ManaSpent || planned.G.Players[0].Pool != manual.G.Players[0].Pool ||
		len(planned.G.Stack) != len(manual.G.Stack) {
		t.Fatalf("planned zone=%s spent=%d pool=%v stack=%d; manual zone=%s spent=%d pool=%v stack=%d",
			po.Zone, po.ManaSpent, planned.G.Players[0].Pool, len(planned.G.Stack),
			mo.Zone, mo.ManaSpent, manual.G.Players[0].Pool, len(manual.G.Stack))
	}
}

func TestPaymentPlanExecutionHonoursWitnessPoolSpend(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9396, "Name:Pool Plan\nManaCost:1 U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	e.G.Players[0].Pool[state.ManaIndex('U')] = 1
	e.G.Players[0].Pool[state.ManaIndex('R')] = 1
	e.G.Players[0].Pool[state.ManaIndex('G')] = 1
	d := ppAsk(t, e)
	a := ppAction(t, d, spell)
	p := a.Plans[0]
	if len(p.Activations) != 0 {
		t.Fatalf("witness %#v, want pool-only", p)
	}
	ppSubmitPlan(t, e, d, a)
	if got := paymentManaAmount(e.G.Players[0].Pool); got != p.PoolAfter {
		t.Fatalf("pool after execution %v, witness pool_after %v (spend %v)", got, p.PoolAfter, p.PoolSpend)
	}
}

func TestPaymentPlanCloneAtTargetAskFinishesIdentically(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9380, ppShock2)
	onBoard(t, e, 0, ppMountain)
	onBoard(t, e, 0, ppMountain)
	d := ppAsk(t, e)
	a := ppAction(t, d, spell)
	ppSubmitPlan(t, e, d, a)
	if td := e.Pending(); td == nil || td.Kind != decision.KTarget {
		t.Fatalf("pending = %#v, want target ask", td)
	}
	c := e.Clone()
	submitChoices(t, c, 0)
	submitChoices(t, e, 0)
	if c.L.Head() != e.L.Head() || len(c.L.Events) != len(e.L.Events) {
		t.Fatalf("clone at the planned target ask diverged: %s/%d vs %s/%d", c.L.Head(), len(c.L.Events), e.L.Head(), len(e.L.Events))
	}
	if !reflect.DeepEqual(c.G, e.G) {
		t.Fatal("clone game state diverged after the planned payment")
	}
}

func TestPaymentPlanNewSeqRejectsPreviousOffer(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9371, "Name:Red Plan\nManaCost:R\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	onBoard(t, e, 0, ppMountain)
	onBoard(t, e, 0, ppMountain)
	d1 := ppAsk(t, e)
	old := ppAction(t, d1, spell)
	// Answer with a legacy manual activation: the game moves to a new Seq.
	idx := -1
	for _, o := range d1.Options {
		if o.Kind == "activate" {
			idx = o.Index
			break
		}
	}
	submitChoices(t, e, idx)
	d2 := e.Pending()
	if d2 == nil || d2.Seq == d1.Seq {
		t.Fatalf("no new decision: %#v", d2)
	}
	before := len(e.L.Events)
	err := e.Submit(decision.Intent{Seq: d2.Seq, Player: 0, Payment: &decision.PaymentSelection{ActionID: old.ID, Plan: old.Plans[0]}})
	if err == nil || len(e.L.Events) != before {
		t.Fatalf("new Seq accepted the previous offer (err=%v)", err)
	}
}
