package rules

// Mana/tap interference scoping for payment plans (spec §3.2 as amended
// 2026-09-26, ticket aph-interference-scope). Like payment_plan.go this file
// contains no call to emit: every read below is a pure query of the current
// state, run against a HYPOTHETICAL tap and production of one candidate
// source through the engine's own trigger and replacement matchers.
//
// A trigger or replacement on source S affects only S's tier:
//
//   - S's own `T:Mode$ Taps | ValidCard$ Card.Self` whose effect is exactly
//     `DealDamage | Defined$ You | NumDmg$ <literal>` (City of Brass) makes S
//     last resort with consequence damage:N;
//   - S's own `R:Event$ Untap | ValidCard$ Card.Self | Layer$ CantHappen`
//     doesn't-untap replacement (Mana Vault, Grim Monolith) makes S last
//     resort with consequence no_untap;
//   - any other Taps/TapsForMana trigger or ProduceMana replacement -- S's own
//     or another object's, printed, granted or effect-created -- that the real
//     matcher says would apply to S's activation defers S, naming the object.
//
// Untap and LoseMana replacements on other objects alter neither a tap nor a
// production and are ignored. Only a global effect whose scope the planner
// cannot prove declines every plan of the affected player
// (paymentPlanGlobalManaEffect).

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/rules/pay"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/trigmatch"
	"github.com/adams-shaun/gorge/state"
)

// paymentPlanGlobalManaEffect reports whether a mana effect whose scope the
// planner cannot prove reaches p paying for id, and names it as
// "global_mana_effect:<card name>". Exactly two shapes qualify:
//
//   - an effect-created ProduceMana replacement carrying no ValidCard$ or
//     ValidActivator$ filter (its scope is every production of every
//     player; a filtered one is scoped per source by the real matcher);
//   - a RESTRICTING ManaConvert static that reaches the payer, found the way
//     the payment path finds it (paymentConv) and classified by
//     paymentPlanConvRestricts: the planner prices with the ordinary solver,
//     which does not apply the conversion. A purely widening conversion
//     (Mycosynth Lattice) cannot make a priced plan unpayable and is not
//     global.
//
// id may be 0 when no spell is known (the zero-argument wrapper outside a
// cast); a ManaConvert static then reaches p only through its player scope.
func (e *Engine) paymentPlanGlobalManaEffect(p state.PlayerID, id state.ObjID) (bool, string) {
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.ReplacementEvent != "ProduceMana" {
			continue
		}
		if strings.TrimSpace(ce.ReplacementParam(cards.PKValidCard)) == "" && strings.TrimSpace(ce.ReplacementParam(cards.PKValidActivator)) == "" {
			return true, "global_mana_effect:" + pay.PaymentPlanObjName(asPayer(e), ce.Source)
		}
	}
	if conv := asPayer(e).Conv(p, id, false); conv != nil && pay.PaymentPlanConvRestricts(conv) {
		return true, "global_mana_effect:" + e.paymentPlanManaConvertName(p)
	}
	return false, ""
}

// paymentPlanManaConvertName names the ManaConvert static behind a global
// decline, for the diagnostic only (the decision is paymentConv's, classified
// by paymentPlanConvRestricts): the first static reaching p whose
// ManaConversion$ carries a restricting "<-" token, printed sources in board
// order before active effects; else the first static reaching p.
func (e *Engine) paymentPlanManaConvertName(p state.PlayerID) string {
	var views []staticView
	for _, src := range e.manaConvPrintedSources() {
		views = append(views, src.sv)
	}
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.CostStaticMode == "ManaConvert" {
			views = append(views, staticView{Source: ce.Source, Controller: ce.Controller, Params: ce.CostStaticParams})
		}
	}
	first := state.ObjID(0)
	for _, sv := range views {
		if vp, ok := sv.Param(cards.PKValidPlayer); ok && !effects.MatchesPlayerSpec(e.G, vp, p, sv.Controller) {
			continue
		}
		if strings.Contains(sv.ParamStr(cards.PKManaConversion), "<-") {
			return pay.PaymentPlanObjName(asPayer(e), sv.Source)
		}
		if first == 0 {
			first = sv.Source
		}
	}
	if first != 0 {
		return pay.PaymentPlanObjName(asPayer(e), first)
	}
	return "ManaConvert"
}

// paymentPlanSourceInterference classifies what can act on activating ma on
// source id: pay.TierNormal when nothing does, pay.TierLastResort with
// the source's own fully determined consequence, or pay.TierDeferred with
// "source:interference:<card name>" naming the object whose trigger or
// replacement would apply. A choice-shaped production is checked for every
// colour it can be planned as, so a trigger restricted to one colour defers
// the whole source rather than one alternative.
func (e *Engine) paymentPlanSourceInterference(id state.ObjID, ma *cards.SA) (pay.Tier, pay.Consequence, string) {
	src := e.G.Obj(id)
	if src == nil || src.Face() == nil || ma == nil {
		return pay.TierDeferred, pay.Consequence{}, "source:interference"
	}
	deferredBy := func(obj state.ObjID) (pay.Tier, pay.Consequence, string) {
		return pay.TierDeferred, pay.Consequence{}, "source:interference:" + pay.PaymentPlanObjName(asPayer(e), obj)
	}
	var c pay.Consequence
	// The source's own "doesn't untap during your untap step". An Untap
	// replacement naming another object (Claustrophobia's enchanted creature)
	// is not about this source's tap at all.
	for _, r := range src.Face().Repls {
		if r.Event != "Untap" || !strings.HasPrefix(strings.TrimSpace(r.ParamStr(cards.PKValidCard)), "Card.Self") {
			continue
		}
		if !pay.PaymentPlanNoUntapShape(r) {
			return deferredBy(id)
		}
		c.NoUntap = true
	}
	controller := src.Controller
	cost := e.parseCost(ma.ParamStr(cards.PKCost))
	for _, produced := range e.paymentPlanProductions(id, ma) {
		if cost.Tap {
			dmg, by, ok := e.paymentPlanTapObservers(id, controller, produced)
			if !ok {
				return deferredBy(by)
			}
			// One activation fires the trigger once whichever colour it
			// takes: the consequence is the largest, not the sum.
			c.Damage = max(c.Damage, dmg)
		}
		if by, ok := e.paymentPlanProductionReplaced(id, controller, produced, availableAmount(ma), cost.Tap); ok {
			return deferredBy(by)
		}
	}
	if c != (pay.Consequence{}) {
		return pay.TierLastResort, c, "source:last_resort"
	}
	return pay.TierNormal, pay.Consequence{}, ""
}

// paymentPlanProductions lists the concrete Produced$ values a plan can
// execute for ma: the fixed declaration itself, or each colour a choice
// shape resolves to (the withProduced rewrite execution activates).
func (e *Engine) paymentPlanProductions(id state.ObjID, ma *cards.SA) []string {
	mp := effects.ManaOf(ma)
	raw := mp.Produced
	if mp.CountsAny {
		return e.paymentPlanChoiceColours(id, ma)
	}
	return []string{raw}
}

// paymentPlanTapObservers runs every Taps/TapsForMana trigger that could see
// a mana tap of id (activated by activator, declaring produced) through the
// real matcher. It returns the source's own City-of-Brass damage, or ok=false
// with the object whose trigger would otherwise act on the tap or the mana.
//
// The printed walk visits every carrier object (paymentPlanInterferenceCarriers)
// of an alive player that the engine's own trigger walk would consider --
// printed faces, unlocked Room faces and mutated under-cards, as
// checkFaceTriggers walks them -- and leaves the zone question to the real
// zoneGate. Granted-trigger statics that observe a Tap are walked over the
// battlefield recipients their Affected$ admits.
func (e *Engine) paymentPlanTapObservers(id state.ObjID, activator state.PlayerID, produced string) (damage uint32, by state.ObjID, ok bool) {
	// The synchronous context emitManaTap/emitTap establish around the Tap
	// event, restored before return: this is matcher scratch, not game state.
	savedObj, savedPlayer, savedEntering := e.tapObj, e.tapPlayer, e.tapEntering
	savedMana, savedProduced := e.tappingForMana, e.tappingManaProduced
	e.tapObj, e.tapPlayer, e.tapEntering = id, activator, false
	e.tappingForMana, e.tappingManaProduced = id, produced
	defer func() {
		e.tapObj, e.tapPlayer, e.tapEntering = savedObj, savedPlayer, savedEntering
		e.tappingForMana, e.tappingManaProduced = savedMana, savedProduced
	}()
	ev := events.Event{Kind: events.Tap, Obj: id}
	tapMode := func(mode string) bool { return mode == "Taps" || mode == "TapsForMana" }
	for _, oid := range e.paymentPlanInterferenceCarriers() {
		o := e.G.Obj(oid)
		if o == nil || o.PhasedOut || o.Face() == nil || e.printedAbilitiesGone(o) || !pay.PaymentPlanAlive(asPayer(e), o) {
			continue
		}
		faces, n := roomTriggerFaces(o, o.Face())
		walk := faces[:n]
		if len(o.MergedCards) > 0 {
			walk = triggerFacesWithMerged(o, walk)
		}
		for _, fc := range walk {
			for _, t := range fc.face.Triggers {
				if !tapMode(t.Mode) || !e.triggerMatches(t, oid, ev, nil) {
					continue
				}
				if oid == id && fc.active && fc.merged == 0 {
					if d, self := pay.PaymentPlanSelfDamageTrigger(fc.face, t); self {
						damage += d
						continue
					}
				}
				return 0, oid, false
			}
		}
	}
	// Triggers granted by a static (AddTrigger$ / GainsTriggerAbsOf$): any
	// battlefield recipient whose granted Taps/TapsForMana trigger would see
	// this tap defers the source, named by the granting object.
	for _, ce := range grantedTriggerStaticsFor(e.active(), events.Tap, nil) {
		var granted []cards.Trigger
		var svars []map[string]string
		if ce.AddTrigger != nil && tapMode(ce.AddTrigger.Mode) {
			t := *ce.AddTrigger
			grantor := ce.Source
			if ce.TriggerGrantor != 0 {
				grantor = ce.TriggerGrantor
			}
			var sv map[string]string
			if f := grantedTriggerFace(e.G.Obj(grantor), t.ParamStr(cards.PKExecute)); f != nil {
				sv = f.SVars
			}
			granted, svars = append(granted, t), append(svars, sv)
		}
		for _, gf := range ce.GainedTriggerFaces {
			if gf.Face == nil {
				continue
			}
			for _, t := range gf.Face.Triggers {
				if tapMode(t.Mode) {
					granted, svars = append(granted, t), append(svars, gf.Face.SVars)
				}
			}
		}
		if len(granted) == 0 {
			continue
		}
		for _, p := range e.G.AliveFrom(0) {
			for _, rid := range e.G.Zone(state.ZBattlefield, p) {
				if !e.matchesSpecFrom(ce.Affects, rid, ce.Controller, ce.Source) {
					continue
				}
				for i, t := range granted {
					if e.triggerMatchesWithSVars(t, rid, ev, nil, svars[i]) {
						return 0, ce.Source, false
					}
				}
			}
		}
	}
	if by, seen := e.paymentPlanDelayedTapObserver(id, ev); seen {
		return 0, by, false
	}
	return damage, 0, true
}

// paymentPlanDelayedTapObserver reports the source of the first live
// effect-created Taps/TapsForMana registration (state.Game.Delayed with
// EffectRepeat: Bubbling Muck's "until end of turn, whenever a player taps a
// Swamp for mana, that player adds an additional {B}") that would see ev.
// It reads the registration exactly as checkEventDelayedTriggers' EffectRepeat
// arm does -- the stored body, the generic trigMatchers dispatch, matched as
// the registration's owner under the effect-match scope -- and restores that
// matcher scratch before returning. A non-repeating registration with a tap
// mode is never fired by that scan, so it observes nothing. Without this walk
// the planner priced a Swamp at {B} while the tap added {B}{B} (round-5
// cardfuzz mirror seed 14886721532440673633: wrong_production and a
// pool_after the witness did not promise).
func (e *Engine) paymentPlanDelayedTapObserver(id state.ObjID, ev events.Event) (state.ObjID, bool) {
	savedSrc, savedCtl := e.effectMatchSource, e.effectMatchController
	savedRem, savedOverride := e.effectMatchRemembered, e.effectMatchOverride
	defer func() {
		e.effectMatchSource, e.effectMatchController = savedSrc, savedCtl
		e.effectMatchRemembered, e.effectMatchOverride = savedRem, savedOverride
	}()
	for i := range e.G.Delayed {
		dt := &e.G.Delayed[i]
		if !dt.EffectRepeat || (dt.EventMode != "Taps" && dt.EventMode != "TapsForMana") || dt.Trigger == "" {
			continue
		}
		if (dt.MaxTurn > 0 && e.G.Turn > dt.MaxTurn) || !e.delayedRegistrationLive(dt) {
			continue
		}
		raw := dt.Trigger
		if !strings.HasPrefix(raw, "Mode$") {
			raw = events.SVarAcrossFaces(e.G.Obj(dt.Source), raw)
		}
		t, ok := cards.ParseTriggerLine(raw)
		if !ok || t.Mode != dt.EventMode {
			continue
		}
		fn := trigmatch.LookupMode(t.ModeKind())
		if fn == nil || !triggerLineEvents(&t).allows(ev.Kind) {
			continue
		}
		e.effectMatchSource, e.effectMatchController = dt.Source, dt.Controller
		e.effectMatchRemembered, e.effectMatchOverride = dt.Remembered, true
		if fn(boardOf(e), t, dt.Source, ev, nil) {
			return dt.Source, true
		}
	}
	return 0, false
}

// paymentPlanProductionReplaced reports the first object whose ProduceMana
// replacement -- printed on a carrier's face (paymentPlanInterferenceCarriers,
// in any zone: activeZonesGateOK, applied by the matcher, is the zone
// authority) or created by an effect -- the real matcher applies to id
// producing `produced` (amount units per symbol) for activator. Each produced
// symbol is proposed as its own ManaAdd batch, the shape effMana emits.
func (e *Engine) paymentPlanProductionReplaced(id state.ObjID, activator state.PlayerID, produced string, amount int32, fromTap bool) (state.ObjID, bool) {
	if amount <= 0 {
		amount = 1
	}
	active := e.active()
	carriers := e.paymentPlanInterferenceCarriers()
	// With no effect-created ProduceMana replacement and no carrier object
	// both walks below find nothing for any symbol: answer without parsing
	// the production.
	if len(carriers) == 0 {
		produceMana := false
		for i := range active {
			if active[i].ReplacementEvent == "ProduceMana" {
				produceMana = true
				break
			}
		}
		if !produceMana {
			return 0, false
		}
	}
	counts, _ := cards.ProducedCounts(produced)
	savedTap, savedProducer := e.manaFromTap, e.manaProducer
	e.manaFromTap, e.manaProducer = fromTap, id
	defer func() { e.manaFromTap, e.manaProducer = savedTap, savedProducer }()
	for i, n := range counts {
		if n <= 0 {
			continue
		}
		ev := events.Event{Kind: events.ManaAdd, Player: activator, Counter: string(cards.ManaSymbol(i)), Amount: n * amount}
		for ceI, ceL := 0, active; ceI < len(ceL); ceI++ {
			ce := &ceL[ceI]
			if ce.ReplacementEvent != "ProduceMana" {
				continue
			}
			r := cards.Repl{Event: ce.ReplacementEvent, Params: ce.ReplacementParams, With: replacementBodySA(ce.ReplacementBody)}
			if e.replacementMatchesEffectCreatedBy(r, ce.Source, ev, ce.Remembered, ce.RememberedPlayers, ce.Controller) {
				return ce.Source, true
			}
		}
		for _, oid := range carriers {
			o := e.G.Obj(oid)
			if o == nil || !pay.PaymentPlanAlive(asPayer(e), o) {
				continue
			}
			f := e.replacementFace(oid, ev)
			if f == nil {
				continue
			}
			for _, r := range f.Repls {
				if r.Event == "ProduceMana" && e.replacementMatches(r, oid, ev) {
					return oid, true
				}
			}
		}
	}
	return 0, false
}

// paymentPlanInterferenceCarriers lists, in object-ID order, every object
// any of whose card faces or mutated under-cards prints a Taps/TapsForMana
// trigger or a ProduceMana replacement. It is zone-agnostic -- the matchers
// own the zone gates -- so it changes only when an object is created or a
// logged event runs, and is memoised on exactly that key.
func (e *Engine) paymentPlanInterferenceCarriers() []state.ObjID {
	if e.PaymentPlanCarriersValid && e.PaymentPlanCarriersObjs == len(e.G.Objs) && e.PaymentPlanCarriersEvents == len(e.L.Events) {
		return e.PaymentPlanCarriers
	}
	carries := func(f *cards.Face) bool {
		if f == nil {
			return false
		}
		for _, t := range f.Triggers {
			if t.Mode == "Taps" || t.Mode == "TapsForMana" {
				return true
			}
		}
		for _, r := range f.Repls {
			if r.Event == "ProduceMana" {
				return true
			}
		}
		return false
	}
	var out []state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		found := o.Card != nil && slices.ContainsFunc(o.Card.Faces, carries)
		for j := 0; !found && j < len(o.MergedCards); j++ {
			found = carries(o.MergedFaceAt(j))
		}
		if found {
			out = append(out, o.ID)
		}
	}
	// A fresh slice every rebuild: a Clone never shares this memo, and a
	// caller may still be walking the previous one.
	e.PaymentPlanCarriers, e.PaymentPlanCarriersValid = out, true
	e.PaymentPlanCarriersObjs, e.PaymentPlanCarriersEvents = len(e.G.Objs), len(e.L.Events)
	return out
}

// paymentPlanSunburstGrantOut is the planner-local sunburst arm of the cast
// shape gate: whether any alive player's battlefield face GRANTS sunburst to
// a spell (Solar Array's and Lux Artillery's `Animate | Keywords$ Sunburst`,
// or a Pump/Continuous keyword grant). The shared capture gate
// sunburstGrantOut deliberately over-reads any mention -- it only arms a
// pay-time capture -- but a face with its own K:Sunburst (Engineered
// Explosives) changes nothing about another spell's payment.
func (e *Engine) paymentPlanSunburstGrantOut() bool {
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			// faceGrantsSunburstForPlan, memoised (face_scan_memo.go).
			if o := e.G.Obj(id); o != nil && e.faceScanHas(o.Face(), faceScanSunburstGrantPlan) {
				return true
			}
		}
	}
	return false
}
