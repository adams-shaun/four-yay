package rules

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Announce then pay (docs/superpowers/specs/2026-09-27-announce-then-pay.md).
//
// A seat that receives PaymentActions may submit Intent.Announce naming an
// action that carries a plan: the engine begins that ordinary cast with no
// payment witness and marks it announced. The CR 601.2g mana window of an
// announced cast (announcedManaWindowAsk) offers one "mana" option per
// (source, ability, colour), Auto-fill, Undo last tap, Cancel cast and -- for
// a pay-life grant -- Pay. Every other cast poses the legacy window
// untouched, which is what keeps bots, TestHeads and botbench byte-identical.

// windowTap records one mana activation made from an announced window (a
// "mana" answer or an Auto-fill step), for Undo last tap and Cancel cast
// (spec §5). mark and trig are the event-log and trigger-queue lengths before
// the activation; normal is the planner's tier verdict for the activated
// ability at activation time. The record is closed (its span judged) the next
// time the window is posed or the next activation begins, before any other
// event is logged.
type windowTap struct {
	source     state.ObjID
	mark       int
	trig       int
	normal     bool
	closed     bool
	reversible bool
	adds       []windowTapAdd
}

// windowTapAdd is one ManaAdd a reversible activation made: the exact counter
// and the positive amount a ManaUndo removes again.
type windowTapAdd struct {
	counter string
	amount  int32
}

func cloneWindowTaps(in []windowTap) []windowTap {
	if in == nil {
		return nil
	}
	out := make([]windowTap, len(in))
	for i, t := range in {
		t.adds = append([]windowTapAdd(nil), t.adds...)
		out[i] = t
	}
	return out
}

// ValidateCastAnnounce is Submit's (and the host's) independent proof that an
// announced cast is still payable from the pool plus untapped sources: the
// planner must still return a plan (spec §3.1, operator decision 2), and a
// fixed-count sacrifice the cast will ask for must still be payable. A pure
// read.
func (e *Engine) ValidateCastAnnounce(p state.PlayerID, cast decision.PlannedCast) error {
	e.beginDerivedMemo()
	defer e.endDerivedMemo()
	defer e.paymentPlanQueryScope()()
	if o := e.G.Obj(cast.Object); o != nil && o.Face() != nil && o.Zone == state.ZHand {
		composed := e.offerCostFor(p, cast.Object, withSpellAbilityExtras(o.Face(), e.rawBaseCost(p, cast.Object)), spellScope(""))
		if len(composed.Sac) != 0 && !e.nonManaCastable(p, cast.Object, composed, false, "") {
			return fmt.Errorf("announced cast's sacrifice cost is no longer payable")
		}
	}
	got := e.PlanCastPayment(p, cast)
	if got.Plan == nil {
		reason := got.Reason
		if reason == "" {
			reason = "unpayable"
		}
		return fmt.Errorf("announced cast is not payable: %s", reason)
	}
	return nil
}

// decisionMadeAnnounceText is decisionMadeText with the announce suffix. The
// legacy and planned encodings are untouched; only an announce carries it.
func decisionMadeAnnounceText(kind decision.Kind, choices []int, announce *decision.AnnounceSelection) string {
	return decisionMadeText(kind, choices) + ";announce:" + announce.ActionID
}

// paymentOwed is what the floating pool does not yet cover of cost, for the
// window readout (spec §4.1): each coloured and {C} pip is covered only by
// its own pool slot, generic by whatever remains. It is exact for the V1
// cost shapes an announce admits.
func paymentOwed(c Cost, pool state.Mana) decision.PaymentCost {
	var owed decision.PaymentCost
	left := pool
	for i := range c.Colored {
		need := c.Colored[i]
		if need <= 0 {
			continue
		}
		use := min(need, max(left[i], 0))
		left[i] -= use
		owed.Mana[i] = uint32(need - use)
	}
	gen := c.Generic
	for i := range left {
		if gen <= 0 {
			break
		}
		take := min(gen, max(left[i], 0))
		gen -= take
	}
	if gen > 0 {
		owed.Generic = uint32(gen)
	}
	return owed
}

// announcedAbilityColours is the colour list one ability flattens into in the
// announced window (spec §4.2), or nil for a single option: an explicit Combo
// (the manual wheel's own flattener), or -- for a planner-tier ability whose
// choice the planner already makes concrete -- Any / Chosen / ColorIdentity.
func (e *Engine) announcedAbilityColours(p state.PlayerID, id state.ObjID, ma *cards.SA, chosen string) []string {
	if cols, ok := manaAbilityComboColours(ma, chosen); ok {
		return cols
	}
	if ma == nil || ma.API != "Mana" {
		return nil
	}
	raw := strings.TrimSpace(ma.Params["Produced"])
	if raw != "Any" && raw != "ColorIdentity" && !paymentPlanChoiceShape(raw) {
		return nil
	}
	if tier, _, _ := e.paymentPlanAbilityTier(p, id, ma); tier != paymentTierNormal && tier != paymentTierLastResort {
		return nil
	}
	return e.paymentPlanChoiceColours(id, ma)
}

// announcedAutoFillPlan is the planner's plan for what the announced cast
// still owes, from this exact state (spec §4.4), or nil. Deterministic and
// pure, so the answer re-derives the plan the ask offered.
func (e *Engine) announcedAutoFillPlan(pc *pendingCast, mana Cost) *decision.PaymentPlan {
	p := pc.player
	if !paymentPlanPoolOK(e.G.Players[p]) {
		return nil
	}
	if global, _ := e.paymentPlanGlobalManaEffect(p, pc.card); global {
		return nil
	}
	cast := decision.PlannedCast{Object: pc.card, Face: 0, Origin: "hand"}
	got := e.planPaymentCost(p, cast, mana)
	if got.Plan == nil || len(got.Plan.Activations) == 0 {
		return nil
	}
	plan := decision.ClonePaymentPlan(*got.Plan)
	for _, a := range plan.Activations {
		if e.convokeCommitted(pc, a.Source) {
			return nil
		}
	}
	if id, err := decision.PaymentPlanID(0, p, cast, plan); err == nil {
		plan.ID = id
	}
	return &plan
}

// announcedManaWindowAsk poses the announced CR 601.2g window (spec §4). It
// is called by manaWindowAsk once the pool alone cannot pay mana; it always
// poses (Cancel cast is always available), so it always returns true.
func (e *Engine) announcedManaWindowAsk(pc *pendingCast, mana Cost) bool {
	e.closeWindowTap(pc)
	p := pc.player
	card := e.G.Obj(pc.card)
	name := ""
	if card != nil && card.Face() != nil {
		name = card.Face().Name
	}
	pool := e.G.Players[p].Pool
	d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Pay for " + name, Source: pc.card}
	d.ManaPayment = &decision.ManaPaymentWindow{Card: pc.card, Cost: paymentCost(mana),
		Owed: paymentOwed(mana, pool), Pool: paymentManaAmount(pool)}
	if pc.paymentFallback != nil {
		f := *pc.paymentFallback
		d.PaymentFallback = &f
	}
	for _, id := range e.manaSourceIDs(p) {
		if e.convokeCommitted(pc, id) || !e.untappedManaSource(p, id) {
			continue
		}
		chosen := e.chosenProducedColour(id)
		for i, ma := range e.availableManaAbilitiesForWindow(p, id, false) {
			marker := e.manaActivationCostMarker([]*cards.SA{ma})
			if cols := e.announcedAbilityColours(p, id, ma, chosen); len(cols) > 0 {
				for _, col := range cols {
					d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "mana", Obj: id,
						Ability: i, ManaSymbol: col, Cost: marker,
						Label: manaAbilityCostPrefix(ma) + "Add " + manaAmountPips(ma, col)})
				}
				continue
			}
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "mana", Obj: id,
				Ability: i, Cost: marker, Label: manaAbilityLabel(ma, chosen)})
		}
	}
	if plan := e.announcedAutoFillPlan(pc, mana); plan != nil {
		names := make([]string, 0, len(plan.Activations))
		for _, a := range plan.Activations {
			d.ManaPayment.AutoFill = append(d.ManaPayment.AutoFill, a.Source)
			if o := e.G.Obj(a.Source); o != nil && o.Face() != nil {
				names = append(names, o.Face().Name)
			}
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: decision.OptAutoFill,
			Label: "Auto-fill: tap " + strings.Join(names, ", ")})
	}
	if t, ok := e.undoableWindowTap(pc); ok {
		label := "Undo last tap"
		if o := e.G.Obj(t.source); o != nil && o.Face() != nil {
			label = "Undo tapping " + o.Face().Name
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: decision.OptUndoTap,
			Obj: t.source, Label: label})
	}
	// A pay-life grant (K'rrik) can settle what the pool cannot: the legacy
	// window's "done" is kept for exactly that case (manaWindowAsk's gate
	// suspended the grant to open this window).
	if e.costPayableClassLife(p, paymentForCast(pc, mana),
		pipRider{anyColor: pc.mayPlayIgnore, anyType: pc.mayPlayIgnoreType}, mana, true) {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "done", Label: "Pay"})
	}
	d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: decision.OptCancelCast, Label: "Cancel cast"})
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// announcedActivate answers a "mana" option of the announced window: resolve
// exactly the named ability (a withProduced copy for a flattened colour),
// through the calls the stage-1 mana wheel and the plan executor make.
func (e *Engine) announcedActivate(pc *pendingCast, opt decision.Option) {
	p, src := pc.player, opt.Obj
	abilities := e.availableManaAbilitiesForWindow(p, src, false)
	if opt.Ability < 0 || opt.Ability >= len(abilities) {
		return
	}
	ab := abilities[opt.Ability]
	normal := false
	if tier, _, _ := e.paymentPlanAbilityTier(p, src, ab); tier == paymentTierNormal &&
		paymentPlanTapOnlyCost(e.parseCost(ab.Params["Cost"])) {
		normal = true
	}
	gained := e.gainedManaRefFor(p, src, ab)
	e.beginWindowTap(pc, src, normal)
	if col := opt.ManaSymbol; len(col) == 1 && strings.Contains("WUBRGC", col) {
		e.resolveManaAbilityRefOriginal(p, src, withProduced(ab, ab, col), ab, gained, true, false, true)
		return
	}
	e.resolveManaAbilityRef(p, src, ab, gained, true, false, true)
}

// beginWindowTap closes the previous record and opens one for the activation
// about to run.
func (e *Engine) beginWindowTap(pc *pendingCast, src state.ObjID, normal bool) {
	e.closeWindowTap(pc)
	pc.windowTaps = append(pc.windowTaps, windowTap{source: src, mark: len(e.L.Events),
		trig: len(e.pendingTriggers), normal: normal})
}

// closeWindowTap judges the open record's span (spec §5 conditions 1-3): a
// normal-tier ability whose activation logged exactly one Tap of its source
// and positive plain/snow ManaAdds to the payer, and queued no trigger.
func (e *Engine) closeWindowTap(pc *pendingCast) {
	n := len(pc.windowTaps)
	if n == 0 || pc.windowTaps[n-1].closed {
		return
	}
	t := &pc.windowTaps[n-1]
	t.closed = true
	if !t.normal || len(e.pendingTriggers) != t.trig || t.mark > len(e.L.Events) {
		return
	}
	taps := 0
	var adds []windowTapAdd
	for _, ev := range e.L.Events[t.mark:] {
		switch ev.Kind {
		case events.Tap:
			if ev.Obj != t.source {
				return
			}
			taps++
		case events.ManaAdd:
			if ev.Player != pc.player || ev.Amount <= 0 || ev.Text != "" || !plainOrSnowManaCounter(ev.Counter) {
				return
			}
			adds = append(adds, windowTapAdd{counter: ev.Counter, amount: ev.Amount})
		default:
			return
		}
	}
	if taps != 1 || len(adds) == 0 {
		return
	}
	t.adds, t.reversible = adds, true
}

// plainOrSnowManaCounter admits a bare WUBRGC counter (or the empty default)
// and a snow "S<colour>" counter; a typed-producer counter is not reversed.
func plainOrSnowManaCounter(c string) bool {
	switch len(c) {
	case 0:
		return true
	case 1:
		return strings.Contains("WUBRGC", c)
	case 2:
		return c[0] == 'S' && strings.ContainsRune("WUBRGC", rune(c[1]))
	}
	return false
}

// undoableWindowTap reports the last record when it can be reversed now
// (spec §5 conditions 4-5): the source is still on the battlefield, tapped,
// the payer's and free of stun counters, and the pool still holds every unit
// it added.
func (e *Engine) undoableWindowTap(pc *pendingCast) (windowTap, bool) {
	n := len(pc.windowTaps)
	if n == 0 {
		return windowTap{}, false
	}
	t := pc.windowTaps[n-1]
	if !t.closed || !t.reversible {
		return windowTap{}, false
	}
	o := e.G.Obj(t.source)
	if o == nil || o.Zone != state.ZBattlefield || !o.Tapped || o.Controller != pc.player || o.Counter("STUN") > 0 {
		return windowTap{}, false
	}
	pl := e.G.Players[pc.player]
	var need, snow state.Mana
	for _, a := range t.adds {
		idx := manaCounterSlot(a.counter)
		need[idx] += a.amount
		if len(a.counter) == 2 {
			snow[idx] += a.amount
		}
	}
	for i := range need {
		if need[i] > pl.Pool[i] || snow[i] > pl.Snow[i] {
			return windowTap{}, false
		}
	}
	return t, true
}

// manaCounterSlot is the pool slot of a plain or snow ManaAdd counter.
func manaCounterSlot(c string) int {
	switch len(c) {
	case 1:
		return state.ManaIndex(c[0])
	case 2:
		return state.ManaIndex(c[1])
	}
	return state.MC
}

// undoWindowTap reverses the last record (spec §5): one ManaUndo per recorded
// ManaAdd, the first also untapping the source, with any trigger the
// reversal would queue dropped (CR 733.1). The caller has checked
// undoableWindowTap.
func (e *Engine) undoWindowTap(pc *pendingCast) {
	n := len(pc.windowTaps)
	t := pc.windowTaps[n-1]
	pc.windowTaps = pc.windowTaps[:n-1]
	trig := len(e.pendingTriggers)
	for i, a := range t.adds {
		ev := events.Event{Kind: events.ManaUndo, Player: pc.player, Counter: a.counter, Amount: a.amount}
		if i == 0 {
			ev.Obj = t.source
		}
		e.emit(ev)
	}
	if len(e.pendingTriggers) > trig {
		e.pendingTriggers = e.pendingTriggers[:trig]
	}
}

// cancelAnnouncedCast answers Cancel cast (spec §6): reverse the reversible
// in-window activations from the most recent back, stopping at the first
// that is not, then reverse the cast itself (CR 733.1) without holding it
// out of the next offer.
func (e *Engine) cancelAnnouncedCast(pc *pendingCast) {
	e.closeWindowTap(pc)
	for {
		if _, ok := e.undoableWindowTap(pc); !ok {
			break
		}
		e.undoWindowTap(pc)
	}
	pc.payment = nil
	e.abortCast(pc, "cast cancelled by its caster (CR 733.1)", false)
}
