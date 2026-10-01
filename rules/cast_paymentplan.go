package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// plannedCastPayment is deliberately private continuation state rather than
// an alternate payment engine.  Its plan is deep-copied at Submit and Clone.
type plannedCastPayment struct {
	actionID string
	plan     decision.PaymentPlan
}

// The spec §6 PaymentFallback vocabulary. It is closed: a plan that stops
// names exactly one of these on the manual window the cast returns to.
const (
	paymentFallbackCostChanged       = "cost_changed"
	paymentFallbackSourceChanged     = "source_changed"
	paymentFallbackProductionChanged = "production_changed"
	paymentFallbackChoiceRequired    = "choice_required"
)

// paymentPlanFallback stops automation for the rest of the cast: the witness
// is dropped so no later re-entry can resume it, the reason is recorded for
// the ordinary manual window, and completed activations (and the mana they
// floated) stay exactly as they are -- no rollback, no substitute source.
func (e *Engine) paymentPlanFallback(pc *pendingCast, reason string) {
	if pc == nil || pc.payment == nil {
		return
	}
	pc.paymentFallback = &decision.PaymentFallback{PlanID: pc.payment.plan.ID, Reason: reason}
	e.paymentStats.recordFallback(reason)
	pc.payment = nil
	pc.paymentNext = 0
}

// castPaymentMana is the mana a pending cast's CR 601.2h payment charges: the
// composed total less announced creature contributions (paymentMana), less a
// spell's Delve credit. The mana window prices it, payCast pays it, and a
// payment plan still describes the payment only while its witnessed Cost
// equals it.
func (e *Engine) castPaymentMana(pc *pendingCast) Cost {
	mana := e.paymentMana(pc)
	if !pc.isAbility() {
		mana.Generic -= int32(len(pc.delve))
		if mana.Generic < 0 {
			mana.Generic = 0
		}
	}
	return mana
}

// paymentPlanCheck revalidates the selected witness against the pending cast
// before each planned activation and once more after the last (spec §6): the
// resolved mana cost, the remaining steps' life + damage against the caster's
// life, the global gates, every remaining step's source and exact alternative
// (consequence included), and that the floating pool plus the remaining
// production still settles exactly as witnessed. ValidateCastPayment checked the same
// witness at Submit while the card was in hand; this reads the cast after its
// announcements (targets, sacrifices, discards, delve), so a delve exile or a
// convoked creature that changed the mana to pay is a cost change. It is a
// pure read and returns the fallback reason, or "" while the rest of the plan
// is exactly executable.
//
// The global gates are the offer's. The pool gate reads the floating pool the
// plan starts from, so it applies before the first activation only: after
// that the plan's own production (a snow land's, a rock's typed mana)
// legitimately floats. The interference gate applies before every later
// activation. Submit ran it for the first, and a change between Submit and
// this window surfaces in that first activation's actual production or its
// interruption, both of which the executor checks.
func (e *Engine) paymentPlanCheck(pc *pendingCast) string {
	reason, _, _, _ := e.paymentPlanCheckUnits(pc)
	return reason
}

// paymentPlanCheckUnits is paymentPlanCheck also returning the source census
// it validated against (nil on an early return, which never reads one) and,
// when the check passed and a step remains, the resolved next step (ok), so
// the executor's immediately following step resolution reuses both rather
// than taking the identical census and resolving the identical step again at
// the unchanged state.
func (e *Engine) paymentPlanCheckUnits(pc *pendingCast) (string, []windowManaUnit, plannedManaActivation, bool) {
	// A pure read: one zone-entry index serves every remaining step.
	defer e.paymentPlanQueryEnd(e.paymentPlanQueryBegin())
	plan := pc.payment.plan
	cost := e.castPaymentMana(pc)
	if plan.Version != decision.PaymentPlanV1 || plan.Cost != paymentCost(cost) {
		return paymentFallbackCostChanged, nil, plannedManaActivation{}, false
	}
	next := pc.paymentNext
	// The lethal guard, re-read before every step (spec §6): the remaining
	// steps' disclosed life + damage must still leave the caster alive. A
	// life total lowered since the offer (or by an earlier step's replaced
	// damage) stops the plan before the next activation. Each remaining
	// step's consequence itself is re-derived with its alternative below
	// (paymentPlanStepReady): a changed one is production_changed.
	if pain := paymentPlanRemainingPain(plan, next); pain > 0 && pain >= int64(e.G.Players[pc.player].Life) {
		return paymentFallbackCostChanged, nil, plannedManaActivation{}, false
	}
	if next == 0 && !paymentPlanPoolOK(&e.G.Players[pc.player]) {
		return paymentFallbackProductionChanged, nil, plannedManaActivation{}, false
	}
	if next > 0 && next < len(plan.Activations) && e.paymentPlanManaInterference() {
		return paymentFallbackProductionChanged, nil, plannedManaActivation{}, false
	}
	// Only the remaining steps' sources are resolved below (and the
	// executor resolves the next of them), so the census is taken for those
	// sources alone (paymentPlanManaUnitsOnly: exactly the full census's
	// units for them).
	var srcBuf [16]state.ObjID
	only := srcBuf[:0]
	for _, pa := range plan.Activations[min(next, len(plan.Activations)):] {
		only = append(only, pa.Source)
	}
	units := e.paymentPlanManaUnitsOnly(pc.player, only)
	pool := e.G.Players[pc.player].Pool
	var first plannedManaActivation
	for i, pa := range plan.Activations {
		// A source named twice is never a V1 witness (the planner taps each
		// source at most once); the plans are a handful of steps, so a scan
		// of the earlier steps replaces a set.
		for _, prev := range plan.Activations[:i] {
			if prev.Source == pa.Source {
				return paymentFallbackSourceChanged, units, plannedManaActivation{}, false
			}
		}
		if i < next {
			continue // completed: its checked production already floats.
		}
		step, reason := e.paymentPlanStepReady(pc.player, units, pa)
		if reason != "" {
			return reason, units, plannedManaActivation{}, false
		}
		if i == next {
			first = step
		}
		pool = manaAdd(pool, step.mana)
	}
	payment, ok := cost.resolveManaWith(pool, state.Mana{}, [7]state.Mana{}, e.G.Players[pc.player].Life, false, pipRider{}, nil)
	if !ok || paymentManaAmount(payment.pool) != plan.PoolAfter {
		return paymentFallbackProductionChanged, units, plannedManaActivation{}, false
	}
	return "", units, first, next < len(plan.Activations)
}

// paymentPlanStepReady resolves one remaining witness step to the exact
// alternative it names (paymentPlanStepAlternative), or says why it no longer
// can. The source itself changed -- gone, another incarnation, tapped, phased
// out, another controller, or no V1 mana ability left under the step's
// identity -- is source_changed; the same untapped source still offering the
// step's ability identity, but not the witnessed production, is
// production_changed. It never names a substitute.
func (e *Engine) paymentPlanStepReady(p state.PlayerID, units []windowManaUnit, pa decision.PaymentActivation) (plannedManaActivation, string) {
	o := e.G.Obj(pa.Source)
	if o == nil || o.Zone != state.ZBattlefield || o.Tapped || o.PhasedOut || o.Controller != p ||
		pa.SourceZoneSeq != e.paymentSourceZoneSeq(pa.Source) {
		return plannedManaActivation{}, paymentFallbackSourceChanged
	}
	if step, ok := e.paymentPlanStepAlternative(units, pa); ok {
		return step, ""
	}
	for _, u := range units {
		if u.id != pa.Source {
			continue
		}
		for _, alt := range e.paymentPlanQueryAlternatives(u) {
			if alt.activation.Ability == pa.Ability {
				return plannedManaActivation{}, paymentFallbackProductionChanged
			}
		}
	}
	return plannedManaActivation{}, paymentFallbackSourceChanged
}

// paymentPlanProducedExactly reports whether the mana p's pool gained from
// event index from onward is exactly want, with nothing taken out: one
// planned activation's actual production against its witness step. Every
// ManaAdd counter form (plain, snow, typed) is read into its pool slot.
func (e *Engine) paymentPlanProducedExactly(p state.PlayerID, from int, want decision.ManaAmount) bool {
	var added state.Mana
	for _, ev := range e.L.Events[from:] {
		if ev.Kind != events.ManaAdd || ev.Player != p {
			continue
		}
		if ev.Amount < 0 {
			return false
		}
		added[state.ManaSlot(ev.Counter)] += ev.Amount
	}
	return paymentManaAmount(added) == want
}

// executePlannedManaActivation runs the witness's next step through the
// ordinary mana ability path, then checks what actually happened (spec §6:
// "after each activation, check actual production and outstanding
// continuation; do not continue blindly"). It reports true when the cast has
// moved on without the caller: the activation suspended on a real decision,
// or the continuation it resumed posed the next ask, settled the cast or
// reversed it. It reports false only when nothing was activated because the
// step no longer holds; the fallback is recorded and the caller continues
// into the ordinary manual window. It never substitutes a source.
func (e *Engine) executePlannedManaActivation(pc *pendingCast) bool {
	return e.executePlannedManaActivationUnits(pc, nil, plannedManaActivation{}, false)
}

// executePlannedManaActivationUnits is executePlannedManaActivation over a
// source census the caller took at this exact state (paymentPlanCheckUnits,
// with nothing run in between); nil takes a fresh one. ready reports that
// the caller's check also resolved the step to run (checked), at the same
// state and with the same census, so it is not resolved again.
func (e *Engine) executePlannedManaActivationUnits(pc *pendingCast, units []windowManaUnit, checked plannedManaActivation, ready bool) bool {
	pa := pc.payment.plan.Activations[pc.paymentNext]
	// Activate the exact alternative the step names -- the one whose ability
	// identity AND production equal the witness -- never the first ability
	// sharing the identity: a dual land's intrinsic {U} and {R} abilities are
	// both {intrinsic, basic_land}, and a step asking it for {R} must not
	// activate its {U} ability.
	if units == nil {
		units = e.paymentPlanManaUnits(pc.player)
		ready = false
	}
	step := checked
	if !ready {
		var reason string
		step, reason = e.paymentPlanStepReady(pc.player, units, pa)
		if reason != "" {
			e.paymentPlanFallback(pc, reason)
			return false
		}
	}
	if walkCacheVerify {
		// The checked census (restricted to the plan's sources) and step
		// must be exactly what a fresh full census resolves here.
		fresh, reason := e.paymentPlanStepReady(pc.player, e.paymentPlanManaUnits(pc.player), pa)
		if reason != "" || !paymentPlanSameStep(fresh, step) {
			panic("payment plan executor: the checked step is stale")
		}
	}
	ma := step.ma
	if pc.announced {
		// An Auto-fill step of an announced window is an in-window
		// activation too: Undo last tap and Cancel cast may reverse it.
		e.beginWindowTap(pc, pa.Source, step.tier == paymentTierNormal)
	}
	pc.paymentNext++ // a synchronous continuation may re-enter payCast.
	// The planner already resolved the exact ability to run: the original for
	// fixed production, or a withProduced copy carrying the selected colour
	// for Any/Combo/Chosen/ColorIdentity. Resolve step.exec while retaining
	// step.ma as the compiled original for activation limits and replay, so no
	// colour prompt is posed at execution.
	exec := alternativeExec(step)
	mark := len(e.L.Events)
	// This is a spell's CR 601.2g payment window: the call the manual
	// "activate" answer makes (activateManaPayment), never the distinct
	// cumulative/triggered-cost window, which is not open during a cast.
	e.resolveManaAbilityRefOriginal(pc.player, pa.Source, exec, ma,
		e.gainedManaRefFor(pc.player, pa.Source, ma), true, false, true)
	if e.pending != nil {
		// The activation posed a real decision (a replacement's colour
		// choice, say). Keep what completed, cancel the remaining steps, and
		// let that decision's answer resume the cast manually. Only a pending
		// decision is an interruption: e.choosing can still name the
		// cast-time ask answered before this window (a sacrifice, discard or
		// delve pick) and says nothing about this activation.
		e.paymentPlanFallback(pc, paymentFallbackChoiceRequired)
		return true
	}
	if e.cast != pc {
		return true // the activation itself settled or reversed the cast.
	}
	if !e.paymentPlanProducedExactly(pc.player, mark, pa.Produces) {
		// The source produced something other than its step: stop before
		// any further planned source; the manual window names the reason.
		e.paymentPlanFallback(pc, paymentFallbackProductionChanged)
	}
	// Ordinary manually selected mana is resumed by the answer handler.  A
	// payment-plan activation is selected internally, so resume the cast here
	// to re-validate and run the next step, settle the funded cost, or pose
	// the manual window after a fallback.
	e.continueCast()
	return true
}

// paymentPlanSameStep compares two resolutions of one witness step at one
// state: every field, the ability up to the identity of an ability built
// per call (a CR 305.6 intrinsic).
func paymentPlanSameStep(a, b plannedManaActivation) bool {
	if !sameManaAbility(a.ma, b.ma) {
		return false
	}
	a.ma, b.ma = nil, nil
	return a == b
}

// manaWindowAsk implements CR 601.2g: if the total cost includes a mana
// payment, the player gets a chance to activate mana abilities before paying
// (601.2h). The engine poses a mid-cast KChoose window -- one "activate"
// option per untapped mana-ability source the player controls, then a "done"
// option -- only when the pool alone cannot pay the resolved total cost and
// at least one such source is untapped; a caster who already has the mana, or
// has no untapped source, has nothing a window could enable, so payCast
// proceeds straight to payment. "The pool alone" is literal here: a {B} pip
// the payer's PayLifeInsteadOf:B grant could settle with 2 life is not
// counted as paid, so the window opens and the mana-or-life choice is the
// payer's (the grant is suspended only for this gate; the payment still
// spends the life when the payer answers "done"). Answering routes
// through castAnswer
// (chooseCast): "activate" taps the source and resolves its mana abilities
// (a tap consumes it, so it is not re-offered) and continueCast re-enters
// payCast to re-price the window; "done" sets windowDone so payCast pays.
//
// A selected payment plan runs here first, one step per re-entry, each step
// revalidated before it runs (paymentPlanCheck). It returns true only when a
// decision is pending or the cast has settled or reversed; a plan that stops
// falls through to this ordinary window, which then carries its
// PaymentFallback.
func (e *Engine) manaWindowAsk() bool {
	pc := e.cast
	if pc == nil || pc.windowDone {
		return false
	}
	if pc.payment != nil {
		if reason, units, step, ready := e.paymentPlanCheckUnits(pc); reason != "" {
			e.paymentPlanFallback(pc, reason)
		} else if pc.paymentNext < len(pc.payment.plan.Activations) && e.executePlannedManaActivationUnits(pc, units, step, ready) {
			return true
		}
	}
	mana := e.castPaymentMana(pc)
	if !mana.hasManaPayment() {
		return false
	}
	// A pool that already pays the total cost needs no window (nothing to
	// gain by activating more mana abilities here). The descriptor carries
	// the announced-X marker so a CostContainsX batch sees the X payment.
	// The check deliberately suspends the payer's PayLifeInsteadOf:B grant
	// (costPayableClassLife's lifeGrant false): a {B} pip K'rrik could settle
	// with 2 life is NOT "already paid" by the pool, so the window must open
	// and let the payer choose an untapped source over the life. Answering
	// "done" still spends the life through the ordinary payment, which keeps
	// the grant.
	if e.costPayableClassLife(pc.player, paymentForCast(pc, mana),
		pipRider{anyColor: pc.mayPlayIgnore, anyType: pc.mayPlayIgnoreType}, mana, false) {
		return false
	}
	if pc.announced {
		// Announce then pay: the "select mana" window, posed even with no
		// untapped source left so the caster can undo or cancel. A plan
		// (Auto-fill) that ran every step yet left the pool short falls back
		// into it with its reason, as a planned cast does.
		if pc.payment != nil {
			reason := e.paymentPlanCheck(pc)
			if reason == "" {
				reason = paymentFallbackProductionChanged
			}
			e.paymentPlanFallback(pc, reason)
		}
		return e.announcedManaWindowAsk(pc, mana)
	}
	var sources []state.ObjID
	for _, id := range e.manaSourceIDs(pc.player) {
		if !e.convokeCommitted(pc, id) && e.untappedManaSource(pc.player, id) {
			sources = append(sources, id)
		}
	}
	if len(sources) == 0 {
		return false
	}
	if pc.payment != nil {
		// Every step ran and checked, yet the pool cannot pay: the manual
		// window never appears during a planned cast without saying why.
		reason := e.paymentPlanCheck(pc)
		if reason == "" {
			reason = paymentFallbackProductionChanged
		}
		e.paymentPlanFallback(pc, reason)
	}
	name := e.G.Obj(pc.card).Face().Name
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Activate mana abilities to pay for " + name, Source: pc.card}
	if pc.paymentFallback != nil {
		f := *pc.paymentFallback
		d.PaymentFallback = &f
	}
	for _, id := range sources {
		opt := decision.Option{Index: len(d.Options), Kind: "activate",
			Obj: id, Label: "Activate " + e.G.Obj(id).Face().Name + " for mana"}
		// The same beyond-tap cost marker legal.go's priority-window offer
		// carries, so the one "activate" option shape stays consistent across
		// both ask sites (fb-led1); this ask sits on a choose decision, which
		// every auto path refuses, so the marker changes no classification.
		if marker := e.manaActivationCostMarker(e.availableManaAbilities(pc.player, id)); marker != "" {
			opt.Cost = marker
		}
		d.Options = append(d.Options, opt)
	}
	d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "done", Label: "Done"})
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// convokeCommitted reports whether id is already committed to this cast's
// payment: a Convoke/Harmonize/Improvise election (pc.convoke) or a Conspire
// tap election (pc.taps -- the election records the creatures before payCast's
// emitChoiceCosts taps them, so an elected creature must be excluded from the
// mana window and from a later convoke announcement, or it could be activated
// for mana and then tapped a second time).
func (e *Engine) convokeCommitted(pc *pendingCast, id state.ObjID) bool {
	for _, pay := range pc.convoke {
		if pay.id == id {
			return true
		}
	}
	for _, tid := range pc.taps {
		if tid == id {
			return true
		}
	}
	return false
}

// untappedManaSource reports whether id is an untapped permanent under
// the player p's control with at least one unrestricted mana ability. It
// is the PAYMENT-WINDOW gate only -- every call site (the CR 601.2g
// cast window, a ward payment, a cumulative-upkeep payment) runs while
// the payer holds no priority -- so an InstantSpeed$ True mana ability
// (Lion's Eye Diamond, "Activate only as an instant") does not make its
// source eligible: the ability is activatable at priority, never inside
// a payment window (CR 605.4 defers to the ability's own timing
// restriction).
func (e *Engine) untappedManaSource(p state.PlayerID, id state.ObjID) bool {
	for _, ma := range e.availableManaAbilities(p, id) {
		if e.instantSpeedOnly(ma) {
			continue
		}
		return true
	}
	return false
}

// hasUntappedManaSource reports whether p controls ANY untapped permanent
// with a usable mana ability -- the condition under which the 601.2g window
// could supply the mana a pool alone cannot.
func (e *Engine) hasUntappedManaSource(p state.PlayerID) bool {
	for _, id := range e.manaSourceIDs(p) {
		if e.untappedManaSource(p, id) {
			return true
		}
	}
	return false
}

// installPaidCostLists publishes the cards this cast/activation's cost exiled
// (pc.exiles) and revealed (pc.reveals) onto the engine keyed by the stack
// object it just minted, in stable cost order. Resolution loads them into
// effects.Ctx.Exiled/Revealed so the `Exiled$<Property>` /
// `Revealed$<Property>` count refs and `Defined$ Exiled`/`Revealed` read the
// exact cards the cost paid (Forge's SpellAbility.getPaidList rows). It is the
// sacrificedLKI discipline: engine-only scratch (a log-only reconstruction
// rebuilds it because payCast re-executes), cloned with the engine, removed
// with the stack object by the shared MoveZone cleanup. Empty lists are not
// recorded -- an absent entry and an empty one read the same legitimate zero.
func (e *Engine) installPaidCostLists(pc *pendingCast) {
	if pc.stackObj == 0 {
		return
	}
	if len(pc.exiles) > 0 {
		if e.castExiled == nil {
			e.castExiled = make(map[state.ObjID][]state.ObjID)
		}
		e.castExiled[pc.stackObj] = append([]state.ObjID(nil), pc.exiles...)
	}
	if len(pc.reveals) > 0 {
		if e.castRevealed == nil {
			e.castRevealed = make(map[state.ObjID][]state.ObjID)
		}
		e.castRevealed[pc.stackObj] = append([]state.ObjID(nil), pc.reveals...)
	}
}

func (e *Engine) emitChoiceCosts(pc *pendingCast) {
	names := func(ids []state.ObjID) string {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			out = append(out, e.targetName(id))
		}
		return strings.Join(out, ", ")
	}
	// A whole-hand Reveal part paid with an empty hand (Land Grant with no
	// other cards in hand): nothing else would appear in the log, so emit the
	// empty reveal as its own public note.
	if pc.revealedEmptyHand {
		e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card,
			Text: "revealed no cards (an empty hand) as a cost"})
	}
	if len(pc.reveals) > 0 {
		// Split the paid list by arm: an announced hand reveal (a plain Reveal
		// card, the REVEAL arm of an either-or cost, or any legacy entry with no
		// arm recorded) is a public reveal; a permanent elected by the CHOOSE
		// arm is a public CHOICE, never a reveal of a hand card. Both ride the
		// same paid list the `Revealed$<Property>` refs read.
		var revealed, chosen []state.ObjID
		for i, id := range pc.reveals {
			if i < len(pc.revealHandArm) && !pc.revealHandArm[i] {
				chosen = append(chosen, id)
			} else {
				revealed = append(revealed, id)
			}
		}
		if len(revealed) > 0 {
			e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card,
				IDs: append([]state.ObjID(nil), revealed...), Text: "revealed " + names(revealed) + " as a cost"})
		}
		if len(chosen) > 0 {
			e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card,
				IDs: append([]state.ObjID(nil), chosen...), Text: "chose " + names(chosen) + " as a cost"})
		}
	}
	// RevealChosen<Player>/<Type> parts: the payer's secret designation is
	// made public as the cost is paid. One public Note per part, naming the
	// designation (the chosen player's chain-safe tossName, or the chosen
	// creature type). Nothing is asked -- the choice was made earlier by the
	// Secretly$ True ChoosePlayer/ChooseType.
	for _, part := range pc.cost.RevealChosen {
		if text, ok := revealChosenText(e.G, e.G.Obj(pc.card), part.Spec); ok {
			e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card, Text: text})
		}
	}
	if len(pc.beholds) > 0 {
		e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card,
			IDs: append([]state.ObjID(nil), pc.beholds...), Text: "beheld " + names(pc.beholds) + " as a cost"})
		// BeholdExile<N/Spec> parts (CostPart.ThenExile): the beheld objects
		// are exiled as the rest of the same payment. beholdCostAsk records
		// each part's N objects in part order, so the paid list is sliced by
		// part. The MoveZone carries the paying source in IDs, the event-
		// derived ExiledWith provenance the Champion cycle's "return the
		// exiled card to its owner's hand" (Defined$ ExiledWith) reads.
		at := 0
		for _, part := range pc.cost.Behold {
			n := int(part.N)
			if at+n > len(pc.beholds) {
				break
			}
			if part.ThenExile {
				for _, id := range pc.beholds[at : at+n] {
					if o := e.G.Obj(id); o != nil && o.Zone != state.ZExile {
						e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZExile,
							IDs: []state.ObjID{pc.card}, Text: "exiled as a cost"})
					}
				}
			}
			at += n
		}
	}
	for _, id := range pc.taps {
		e.emit(events.Event{Kind: events.Tap, Obj: id, Text: "tapped as a cost"})
	}
	// CR 702.122: the creatures that paid a Crew ability's tap cost crewed the
	// Vehicle. The crew keyword rides the minted Animate SA as `Keyword$ Crew`
	// (cards/kw_crew.go), so the tag -- not any card name -- is what marks this
	// activation: one Crew event per tapped crewer, pairing it with the source
	// Vehicle (pc.card) for the Creature.CrewedThisTurn filter. The ordinary
	// tapXType costs of other abilities (Mossbridge Troll's regeneration, the
	// {T} cost) carry no Crew tag and record nothing.
	if saHasKeyword(e.pcAbility(pc), "Crew") {
		for _, id := range pc.taps {
			e.emit(events.Event{Kind: events.Crew, Obj: id, Player: pc.player,
				IDs: []state.ObjID{pc.card}})
		}
	}
	for i, id := range pc.blights {
		if i < len(pc.cost.Blight) {
			n := pc.cost.Blight[i].N
			// An announced Blight<X> part's count is the announced X, not the
			// (unused) part.N.
			if pc.cost.Blight[i].Announced {
				n = pc.x
			}
			e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "M1M1",
				Amount: n})
		}
	}
}
