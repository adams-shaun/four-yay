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
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
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
		if strings.TrimSpace(ce.ReplacementParams["ValidCard"]) == "" && strings.TrimSpace(ce.ReplacementParams["ValidActivator"]) == "" {
			return true, "global_mana_effect:" + e.paymentPlanObjName(ce.Source)
		}
	}
	if conv := e.paymentConv(p, id, false); conv != nil && paymentPlanConvRestricts(conv) {
		return true, "global_mana_effect:" + e.paymentPlanManaConvertName(p)
	}
	return false, ""
}

// paymentPlanConvRestricts classifies a payer's effective conversion set
// (paymentConv, the payment path's own parse of every ManaConvert static
// reaching the payer) by whether it can make a planned payment INVALID.
//
//   - wild / wildC ("spend mana as though it were mana of any color/type":
//     Mycosynth Lattice, Chromatic Orrery, the AnyType->AnyColor family) and
//     to ("White->Red") only ever ADD pips a unit of mana may pay. A witness
//     the ordinary solver proved payable without them stays payable with
//     them, so they are not a global plan-blocker.
//   - onlyC ("you may spend other mana only as though it were colorless
//     mana": Celestial Dawn's nonWhite<-C) REMOVES pips a unit may pay, so a
//     plan priced without it can be unpayable: global.
//
// Every manaConv field is classified here; a field added to manaConv later
// must be classified too. A ManaConversion$ token the parser cannot read is
// inert at payment as well (manaColourFrom/applyManaConversionTo), so it can
// invalidate nothing the solver priced.
func paymentPlanConvRestricts(c *manaConv) bool {
	return slices.Contains(c.onlyC[:], true)
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
		if vp, ok := sv.Params["ValidPlayer"]; ok && !effects.MatchesPlayerSpec(e.G, vp, p, sv.Controller) {
			continue
		}
		if strings.Contains(sv.Params["ManaConversion"], "<-") {
			return e.paymentPlanObjName(sv.Source)
		}
		if first == 0 {
			first = sv.Source
		}
	}
	if first != 0 {
		return e.paymentPlanObjName(first)
	}
	return "ManaConvert"
}

// paymentPlanObjName is an object's current face name, for diagnostics.
func (e *Engine) paymentPlanObjName(id state.ObjID) string {
	if o := e.G.Obj(id); o != nil && o.Face() != nil {
		return o.Face().Name
	}
	return "object " + strconv.Itoa(int(id))
}

// paymentPlanSourceInterference classifies what can act on activating ma on
// source id: paymentTierNormal when nothing does, paymentTierLastResort with
// the source's own fully determined consequence, or paymentTierDeferred with
// "source:interference:<card name>" naming the object whose trigger or
// replacement would apply. A choice-shaped production is checked for every
// colour it can be planned as, so a trigger restricted to one colour defers
// the whole source rather than one alternative.
func (e *Engine) paymentPlanSourceInterference(id state.ObjID, ma *cards.SA) (paymentAbilityTier, paymentConsequence, string) {
	src := e.G.Obj(id)
	if src == nil || src.Face() == nil || ma == nil {
		return paymentTierDeferred, paymentConsequence{}, "source:interference"
	}
	deferredBy := func(obj state.ObjID) (paymentAbilityTier, paymentConsequence, string) {
		return paymentTierDeferred, paymentConsequence{}, "source:interference:" + e.paymentPlanObjName(obj)
	}
	var c paymentConsequence
	// The source's own "doesn't untap during your untap step". An Untap
	// replacement naming another object (Claustrophobia's enchanted creature)
	// is not about this source's tap at all.
	for _, r := range src.Face().Repls {
		if r.Event != "Untap" || !strings.HasPrefix(strings.TrimSpace(r.Params["ValidCard"]), "Card.Self") {
			continue
		}
		if !paymentPlanNoUntapShape(r) {
			return deferredBy(id)
		}
		c.noUntap = true
	}
	controller := src.Controller
	cost := e.parseCost(ma.Params["Cost"])
	for _, produced := range e.paymentPlanProductions(id, ma) {
		if cost.Tap {
			dmg, by, ok := e.paymentPlanTapObservers(id, controller, produced)
			if !ok {
				return deferredBy(by)
			}
			// One activation fires the trigger once whichever colour it
			// takes: the consequence is the largest, not the sum.
			c.damage = max(c.damage, dmg)
		}
		if by, ok := e.paymentPlanProductionReplaced(id, controller, produced, availableAmount(ma), cost.Tap); ok {
			return deferredBy(by)
		}
	}
	if c != (paymentConsequence{}) {
		return paymentTierLastResort, c, "source:last_resort"
	}
	return paymentTierNormal, paymentConsequence{}, ""
}

// paymentPlanProductions lists the concrete Produced$ values a plan can
// execute for ma: the fixed declaration itself, or each colour a choice
// shape resolves to (the withProduced rewrite execution activates).
func (e *Engine) paymentPlanProductions(id state.ObjID, ma *cards.SA) []string {
	raw := strings.TrimSpace(ma.Params["Produced"])
	if _, any := cards.ProducedCounts(raw); any {
		return e.paymentPlanChoiceColours(id, ma)
	}
	return []string{raw}
}

// paymentPlanNoUntapShape is spec §3.2's no_untap row: exactly the source's
// own "doesn't untap during your untap step" -- ValidCard$ Card.Self, Layer$
// CantHappen, no ReplaceWith$ body, an optional ValidStepTurnToController$
// You and presentation/zone keys only. Anything else is not a fully
// determined consequence.
func paymentPlanNoUntapShape(r cards.Repl) bool {
	if r.With != nil || strings.TrimSpace(r.Params["ValidCard"]) != "Card.Self" ||
		!strings.EqualFold(strings.TrimSpace(r.Params["Layer"]), "CantHappen") {
		return false
	}
	if v, ok := r.Params["ValidStepTurnToController"]; ok && strings.TrimSpace(v) != "You" {
		return false
	}
	if v, ok := r.Params["ActiveZones"]; ok && strings.TrimSpace(v) != "Battlefield" {
		return false
	}
	for _, k := range slices.Sorted(maps.Keys(r.Params)) {
		switch k {
		case "Event", "ValidCard", "Layer", "ValidStepTurnToController", "ActiveZones", "Description":
		default:
			return false
		}
	}
	return true
}

// paymentPlanSelfDamageTrigger is spec §3.2's damage:N trigger row: the
// source's own `Mode$ Taps | ValidCard$ Card.Self` whose effect is exactly
// `DealDamage | Defined$ You | NumDmg$ <literal>` and nothing else (City of
// Brass). f owns the trigger's Execute$ table.
func paymentPlanSelfDamageTrigger(f *cards.Face, t cards.Trigger) (uint32, bool) {
	if t.Mode != "Taps" || strings.TrimSpace(t.Params["ValidCard"]) != "Card.Self" {
		return 0, false
	}
	if v, ok := t.Params["TriggerZones"]; ok && strings.TrimSpace(v) != "Battlefield" {
		return 0, false
	}
	for _, k := range slices.Sorted(maps.Keys(t.Params)) {
		switch k {
		case "Mode", "ValidCard", "Execute", "TriggerZones", "TriggerDescription":
		default:
			return 0, false
		}
	}
	body := t.Effect
	if body == nil && f != nil {
		body = cards.ResolveSVar(f.SVars, strings.TrimSpace(t.Params["Execute"]))
	}
	return paymentPlanDamageBody(body)
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
		if o == nil || o.PhasedOut || o.Face() == nil || e.printedAbilitiesGone(o) || !e.paymentPlanAlive(o) {
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
					if d, self := paymentPlanSelfDamageTrigger(fc.face, t); self {
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
			if f := grantedTriggerFace(e.G.Obj(grantor), t.Params["Execute"]); f != nil {
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
		fn := trigMatchers[t.Mode]
		if fn == nil || !triggerModeEvents(t.Mode).allows(ev.Kind) {
			continue
		}
		e.effectMatchSource, e.effectMatchController = dt.Source, dt.Controller
		e.effectMatchRemembered, e.effectMatchOverride = dt.Remembered, true
		if fn(e, t, dt.Source, ev, nil) {
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
			if o == nil || !e.paymentPlanAlive(o) {
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
	if e.paymentPlanCarriersValid && e.paymentPlanCarriersObjs == len(e.G.Objs) && e.paymentPlanCarriersEvents == len(e.L.Events) {
		return e.paymentPlanCarriers
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
	e.paymentPlanCarriers, e.paymentPlanCarriersValid = out, true
	e.paymentPlanCarriersObjs, e.paymentPlanCarriersEvents = len(e.G.Objs), len(e.L.Events)
	return out
}

// paymentPlanAlive mirrors the engine walks' AliveFrom scope: an object in a
// lost player's zones is not walked for triggers or replacements.
func (e *Engine) paymentPlanAlive(o *state.Object) bool {
	holder := o.Owner
	if o.Zone == state.ZBattlefield {
		holder = o.Controller
	}
	return int(holder) < len(e.G.Players) && !e.G.Players[holder].Lost
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

// faceGrantsSunburstForPlan reports whether f carries a keyword GRANT naming
// Sunburst: a Keywords$/KW$/AddKeyword$ value in any ability, trigger body,
// replacement body, SVar or static. The face's own printed keywords are not
// grants.
func faceGrantsSunburstForPlan(f *cards.Face) bool {
	// Cheap gate first: nearly every face never mentions the word.
	if f == nil || !f.Mentions("Sunburst") {
		return false
	}
	grantKey := func(k string) bool {
		return k == "Keywords" || k == "KW" || k == "AddKeyword" || k == "AddKeywords"
	}
	grantValue := func(v string) bool {
		for kw := range strings.SplitSeq(v, "&") {
			if strings.HasPrefix(strings.TrimSpace(kw), "Sunburst") {
				return true
			}
		}
		return false
	}
	grants := func(params map[string]string) bool {
		for _, k := range [...]string{"Keywords", "KW", "AddKeyword", "AddKeywords"} {
			if grantValue(params[k]) {
				return true
			}
		}
		return false
	}
	var walk func(sa *cards.SA, depth int) bool
	walk = func(sa *cards.SA, depth int) bool {
		return sa != nil && depth <= 32 && (grants(sa.Params) || walk(sa.Sub, depth+1))
	}
	for _, a := range f.Abilities {
		if walk(a, 0) {
			return true
		}
	}
	for _, t := range f.Triggers {
		if walk(t.Effect, 0) {
			return true
		}
	}
	for _, r := range f.Repls {
		if walk(r.With, 0) {
			return true
		}
	}
	for _, st := range f.Statics {
		if grants(st.Params) {
			return true
		}
	}
	// SVar bodies are raw `Key$ Value | ...` text: read the grant keys off
	// their segments without parsing whole abilities. The answer is a plain
	// any-of, so the map's iteration order cannot matter.
	for _, raw := range f.SVars {
		for seg := range strings.SplitSeq(raw, "|") {
			if k, v, ok := strings.Cut(strings.TrimSpace(seg), "$"); ok && grantKey(strings.TrimSpace(k)) && grantValue(v) {
				return true
			}
		}
	}
	return false
}

// paymentPlanSpellTargets reports whether casting f's ordinary spell
// announces a target (CR 601.2c): an Aura spell (CR 303.4a), a mutating
// creature spell (CR 702.140a), or a spell ability whose chain -- its
// SubAbility$ links and a Charm's Choices$ modes -- declares a target. It
// fails closed (true) on a nil face.
func paymentPlanSpellTargets(f *cards.Face) bool {
	if f == nil {
		return true
	}
	if slices.Contains(f.Types, "Aura") || f.HasKeyword("Enchant") {
		return true
	}
	if _, ok := f.KeywordParam("Enchant"); ok {
		return true
	}
	if _, ok := f.KeywordParam("Mutate"); ok {
		return true
	}
	targets := func(sa *cards.SA) bool {
		return strings.TrimSpace(sa.Params["ValidTgts"]) != "" || strings.TrimSpace(sa.Params["TgtPrompt"]) != "" ||
			strings.TrimSpace(sa.Params["TargetType"]) != "" || strings.TrimSpace(sa.Params["TargetMin"]) != "" ||
			strings.TrimSpace(sa.Params["TargetMax"]) != "" || strings.TrimSpace(sa.Params["TgtZone"]) != ""
	}
	var walk func(sa *cards.SA, depth int) bool
	walk = func(sa *cards.SA, depth int) bool {
		if sa == nil || depth > 32 {
			return false
		}
		if targets(sa) || walk(sa.Sub, depth+1) {
			return true
		}
		if name := strings.TrimSpace(sa.Params["SubAbility"]); name != "" && sa.Sub == nil {
			if walk(cards.ResolveSVar(f.SVars, name), depth+1) {
				return true
			}
		}
		for mode := range strings.SplitSeq(sa.Params["Choices"], ",") {
			if mode = strings.TrimSpace(mode); mode != "" && walk(cards.ResolveSVar(f.SVars, mode), depth+1) {
				return true
			}
		}
		return false
	}
	return walk(f.SpellAbility(), 0)
}
