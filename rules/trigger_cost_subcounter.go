package rules

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// The triggered-effect window's counter-removal cost (Level B D8): a
// triggered ability whose effect sits behind "you may remove a counter from
// this creature" -- Guiding Hydra's and Ingenious Prodigy's `Cost$
// SubCounter<1/P1P1>`, Slumbering Walker's and Leatherhead's `ImmediateTrigger
// | Cost$ RemoveAnyCounter<1/Any/CARDNAME>` (the same SubCounter component).
// CR 117.3 / 603.3: the controller may pay a cost it can pay, so the window
// offers "pay" whenever the SOURCE holds the counters. Only the source-self
// shape is modelled: a part naming another object (a removal-target filter)
// or an announced X count keeps the decline-only ask, and so does a cost
// that carries any component beyond a priceable mana/life half (the
// half-paid-commitment rule the rest of the window keeps).

// triggerSubCounterShape reports whether every SubCounter part of c is a
// fixed, positive count removed from the source itself.
func triggerSubCounterShape(c Cost) bool {
	if len(c.SubCounter) == 0 {
		return false
	}
	for _, part := range c.SubCounter {
		if part.Announced || part.N <= 0 || !subCounterTargetsSource(part.Target) {
			return false
		}
	}
	return true
}

// subCounterAnyKind reports whether a SubCounter part's kind is Forge's
// wildcard "Any" (remove counters of any kind).
func subCounterAnyKind(spec string) bool { return strings.EqualFold(spec, anyCounterKind) }

// anyCounterKind is the wildcard SubCounter kind.
const anyCounterKind = "Any"

// triggerSubCounterFixedUse is how many counters of kind the cost's fixed-kind
// parts remove (the "Any" parts excluded), so a wildcard pick never spends a
// counter a fixed part needs.
func triggerSubCounterFixedUse(c Cost, kind string) int32 {
	var n int32
	for _, part := range c.SubCounter {
		if !subCounterAnyKind(part.Spec) && part.Spec == kind {
			n += part.N
		}
	}
	return n
}

// triggerSubCounterAnyUnits is the total count the cost's "Any" parts remove:
// one kind pick per unit.
func triggerSubCounterAnyUnits(c Cost) int32 {
	var n int32
	for _, part := range c.SubCounter {
		if subCounterAnyKind(part.Spec) {
			n += part.N
		}
	}
	return n
}

// triggerSubCounterPayable reports whether the source can give up every
// counter the cost's SubCounter parts name: each fixed kind's parts summed
// against that kind's count, and the whole removal against the source's total
// (pay.SubCounterAvailable's "Any" read, the cast flow's one home).
func triggerSubCounterPayable(g *state.Game, source state.ObjID, c Cost) bool {
	if !triggerSubCounterShape(c) {
		return false
	}
	o := g.Obj(source)
	if !pay.ExistsOnBattlefield(o) {
		return false
	}
	var total int32
	for _, part := range c.SubCounter {
		total += part.N
		if !subCounterAnyKind(part.Spec) && pay.SubCounterAvailable(o, part.Spec) < triggerSubCounterFixedUse(c, part.Spec) {
			return false
		}
	}
	return pay.SubCounterAvailable(o, anyCounterKind) >= total
}

// triggerSubCounterKinds lists, in the source's counter order, the kinds an
// "Any" unit can still be removed from: a kind with counters left after the
// fixed-kind parts and the picks already recorded.
func triggerSubCounterKinds(o *state.Object, c Cost, picks []string) []string {
	var out []string
	for _, ctr := range o.Counters {
		left := ctr.N - triggerSubCounterFixedUse(c, ctr.Kind)
		for _, k := range picks {
			if k == ctr.Kind {
				left--
			}
		}
		if left > 0 {
			out = append(out, ctr.Kind)
		}
	}
	return out
}

// subCounterPayable is the window gate's SubCounter arm: the source holds the
// counters and the rest of the cost is a priceable mana/life half the pool
// covers right now (the same pricing the pay arm charges).
func (tc *triggeredEffectCost) subCounterPayable(e *Engine, rest Cost) bool {
	if !triggerSubCounterPayable(e.G, tc.source, rest) {
		return false
	}
	other := rest
	other.SubCounter = nil
	return other.Priceable() && e.triggeredCostManaHalfPayable(tc, other)
}

// subCounterKindAsk records the kind of every "Any" unit, posing the
// counter-kind choice when more than one kind is left and settling the
// single-kind case without an ask. It reports whether an ask was posed; the
// answer (trigger_cost_counter) records the pick and re-enters the pay arm.
func (tc *triggeredEffectCost) subCounterKindAsk(e *Engine, announced Cost) bool {
	o := e.G.Obj(tc.source)
	if o == nil {
		return false
	}
	for int32(len(tc.counterPicks)) < triggerSubCounterAnyUnits(announced) {
		kinds := triggerSubCounterKinds(o, announced, tc.counterPicks)
		switch len(kinds) {
		case 0:
			return false
		case 1:
			tc.counterPicks = append(tc.counterPicks, kinds[0])
			continue
		}
		name := "triggered ability"
		if o.Face() != nil {
			name = o.Face().Name
		}
		d := &decision.Decision{Player: tc.player, Kind: decision.KChoose, Min: 1, Max: 1,
			Prompt: name + " — choose a counter to remove", Source: tc.source}
		for _, k := range kinds {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "trigger_cost_counter",
				Obj: tc.source, Counter: k, Label: fmt.Sprintf("%s (%s counter)", name, k)})
		}
		windowAsk(e, d, chooseTriggeredCost)
		return true
	}
	return false
}

// subCounterSettle charges the cost: the mana/life half from the pool, any
// energy, then one CounterChange per fixed part and per picked "Any" kind
// (two units of one kind settle as ONE CounterChange, the cast flow's
// grouping). It re-proves payability first, so a paid answer is never a
// partial payment.
func (tc *triggeredEffectCost) subCounterSettle(e *Engine, announced Cost) bool {
	if !triggerSubCounterPayable(e.G, tc.source, announced) ||
		int32(len(tc.counterPicks)) != triggerSubCounterAnyUnits(announced) {
		return false
	}
	lower := announced.WithoutEnergy()
	lower.SubCounter = nil
	if !lower.Priceable() || !pay.PayManaConv(asPayer(e), tc.player, lower, asEval(e).Conv(tc.player, tc.source, false)) {
		return false
	}
	pay.ChargeEnergyCost(asPayer(e), tc.player, tc.amount, tc.xPaid)
	for _, part := range announced.SubCounter {
		if !subCounterAnyKind(part.Spec) {
			e.emit(events.Event{Kind: events.CounterChange, Obj: tc.source, Counter: part.Spec, Amount: -part.N})
		}
	}
	var order []string
	for _, k := range tc.counterPicks {
		seen := false
		for _, s := range order {
			seen = seen || s == k
		}
		if !seen {
			order = append(order, k)
		}
	}
	for _, k := range order {
		var n int32
		for _, p := range tc.counterPicks {
			if p == k {
				n++
			}
		}
		e.emit(events.Event{Kind: events.CounterChange, Obj: tc.source, Counter: k, Amount: -n})
	}
	return true
}
