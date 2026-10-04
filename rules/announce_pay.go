package rules

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/rules/pay"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
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

// windowTap is one announced-window activation record (pay.WindowTap).
type windowTap = pay.WindowTap

// ValidateCastAnnounce is Submit's (and the host's) independent proof that an
// announced cast is still payable from the pool plus untapped sources: the
// planner must still return a plan (spec §3.1, operator decision 2), and a
// fixed-count sacrifice the cast will ask for must still be payable. A pure
// read.
func (e *Engine) ValidateCastAnnounce(p state.PlayerID, cast decision.PlannedCast) error {
	e.beginDerivedMemo()
	defer e.endDerivedMemo()
	defer pay.PaymentPlanQueryEnd(asPayer(e), pay.PaymentPlanQueryBegin(asPayer(e)))
	if o := e.G.Obj(cast.Object); o != nil && o.Face() != nil && o.Zone == state.ZHand {
		composed := e.offerCostFor(p, cast.Object, pay.WithSpellAbilityExtras(o.Face(), pay.RawBaseCost(asPayer(e), p, cast.Object)), spellScope(""))
		if len(composed.Sac) != 0 && !pay.NonManaCastable(asPayer(e), p, cast.Object, composed, false, "") {
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

// announcedAutoFillPlan is the planner's plan for what the announced cast
// still owes, from this exact state (spec §4.4), or nil. Deterministic and
// pure, so the answer re-derives the plan the ask offered.
func (e *Engine) announcedAutoFillPlan(pc *pendingCast, mana Cost) *decision.PaymentPlan {
	p := pc.player
	if !pay.PlanPoolOK(&e.G.Players[p]) {
		return nil
	}
	if global, _ := e.paymentPlanGlobalManaEffect(p, pc.card); global {
		return nil
	}
	cast := decision.PlannedCast{Object: pc.card, Face: 0, Origin: "hand"}
	got := pay.PlanPaymentCost(asPayer(e), p, cast, mana)
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
	d.ManaPayment = &decision.ManaPaymentWindow{Card: pc.card, Cost: pay.WireCost(mana),
		Owed: pay.PaymentOwed(mana, pool), Pool: pay.ManaAmount(pool)}
	if pc.PaymentFallback != nil {
		f := *pc.PaymentFallback
		d.PaymentFallback = &f
	}
	for _, id := range pay.ManaSourceIDs(e.G, p) {
		if e.convokeCommitted(pc, id) || !e.untappedManaSource(p, id) {
			continue
		}
		chosen := pay.ChosenProducedColour(e.G, id)
		for i, ma := range e.availableManaAbilitiesForWindow(p, id, false) {
			marker := e.manaActivationCostMarker([]*cards.SA{ma})
			if cols := pay.AnnouncedAbilityColours(asPayer(e), p, id, ma, chosen); len(cols) > 0 {
				for _, col := range cols {
					d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "mana", Obj: id,
						Ability: i, ManaSymbol: col, Cost: marker,
						Label: pay.ManaAbilityCostPrefix(ma) + "Add " + pay.ManaAmountPips(ma, col)})
				}
				continue
			}
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "mana", Obj: id,
				Ability: i, Cost: marker, Label: pay.ManaAbilityLabel(ma, chosen)})
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
		if o := e.G.Obj(t.Source); o != nil && o.Face() != nil {
			label = "Undo tapping " + o.Face().Name
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: decision.OptUndoTap,
			Obj: t.Source, Label: label})
	}
	// A pay-life grant (K'rrik) can settle what the pool cannot: the legacy
	// window's "done" is kept for exactly that case (manaWindowAsk's gate
	// suspended the grant to open this window).
	if pay.CostPayableClassLife(asPayer(e), p, paymentForCast(pc, mana),
		pipRider{AnyColor: pc.mayPlayIgnore, AnyType: pc.mayPlayIgnoreType}, mana, true) {
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
	if tier, _, _ := pay.PaymentPlanAbilityTier(asPayer(e), p, src, ab); tier == pay.TierNormal &&
		pay.PaymentPlanTapOnlyCost(e.parseCost(ab.ParamStr(cards.PKCost))) {
		normal = true
	}
	gained := pay.GainedManaRefFor(asPayer(e), p, src, ab)
	e.beginWindowTap(pc, src, normal)
	if col := opt.ManaSymbol; len(col) == 1 && strings.Contains("WUBRGC", col) {
		e.resolveManaAbilityRefOriginal(p, src, pay.WithProduced(ab, ab, col), ab, gained, true, false, true)
		return
	}
	e.resolveManaAbilityRef(p, src, ab, gained, true, false, true)
}

// beginWindowTap, closeWindowTap, undoableWindowTap and undoWindowTap are
// the announced window's activation records (pay.BeginWindowTap and its
// siblings); the engine supplies the pending-trigger queue, and an undo drops
// the triggers its reversal queued (CR 733.1).
func (e *Engine) beginWindowTap(pc *pendingCast, src state.ObjID, normal bool) {
	pay.BeginWindowTap(asPayer(e), &pc.CastPayment, pc.player, src, normal, len(e.pendingTriggers))
}

func (e *Engine) closeWindowTap(pc *pendingCast) {
	pay.CloseWindowTap(asPayer(e), &pc.CastPayment, pc.player, len(e.pendingTriggers))
}

func (e *Engine) undoableWindowTap(pc *pendingCast) (windowTap, bool) {
	return pay.UndoableWindowTap(asPayer(e), &pc.CastPayment, pc.player)
}

func (e *Engine) undoWindowTap(pc *pendingCast) {
	trig := len(e.pendingTriggers)
	pay.UndoWindowTap(asPayer(e), &pc.CastPayment, pc.player)
	if len(e.pendingTriggers) > trig {
		e.noteTrigShrink()
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
	pc.Payment = nil
	e.abortCast(pc, "cast cancelled by its caster (CR 733.1)", false)
}
