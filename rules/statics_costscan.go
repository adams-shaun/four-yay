package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// collectCostStatics collects cost-modifier statics from every zone a static
// can be live in, gated by each static's own EffectZone$ (Forge's
// default is the battlefield, so a static with no EffectZone$ behaves exactly
// as activeStatics did — a battlefield-only collector). This is what lets a
// card's own reduction apply while it is still in hand: Ghalta, Primal
// Hunger's `EffectZone$ All | ValidCard$ Card.Self` static is live from the
// hand, the library, the command zone and the stack alike. The walk is
// deterministic: AliveFrom(0) seats, a fixed zone order, slice order inside
// each zone, and each face's own Statics order.
func (e *Engine) collectCostStatics() costStaticViews {
	if v, ok := e.boardStaticsWalk(); ok {
		return v.cost
	}
	return e.scanCostStatics()
}

func (e *Engine) scanCostStatics() costStaticViews {
	var out costStaticViews
	add := func(o *state.Object, id state.ObjID) {
		f := o.Face()
		if f == nil {
			return
		}
		// CR 702.25b/d: a phased-out permanent is treated as though it does
		// not exist, so its cost statics do not function -- the same
		// object-level gate scanActiveStatics, scanActionStatics and the
		// fused scanBoardStatics run. Without it this standalone scan
		// diverges from the fused scan (verifyBoardStatics recomputes both)
		// and from the memoised path, which serves the fused arm.
		if o.Zone == state.ZBattlefield && o.PhasedOut {
			return
		}
		// CR 708.8: a face-down permanent has no printed cost statics
		// (scanActionStatics' and scanActiveStatics' gate).
		if e.printedAbilitiesGone(o) {
			return
		}
		for si, sn := 0, o.PileStaticCount(); si < sn; si++ {
			pst, ok := o.PileStaticAt(si)
			if !ok {
				continue
			}
			st := pst.Static
			var dst *[]staticView
			switch st.ModeKind() {
			case cards.StaticRaiseCost:
				dst = &out.raise
			case cards.StaticReduceCost:
				dst = &out.reduce
			case cards.StaticSetCost:
				dst = &out.set
			case cards.StaticOptionalCost:
				dst = &out.optional
			default:
				continue
			}
			if !effectZoneOK(st.ParamStr(cards.PKEffectZone), o.Zone) {
				continue
			}
			*dst = append(*dst, staticView{Source: id, Controller: o.Controller, Params: st.Params, PS: st.ParamSetOf(), SVars: pst.Face.SVars})
		}
	}
	for pi, p := range e.G.AliveFrom(0) {
		for _, z := range []state.Zone{state.ZBattlefield, state.ZStack, state.ZGraveyard,
			state.ZHand, state.ZLibrary, state.ZExile, state.ZCommand} {
			// The stack is a SHARED zone (state.Game.Zone returns g.Stack for
			// every player), so walking it under every alive seat would
			// collect each stack card's statics once per seat -- a spell's own
			// reduction (Dargo's EffectZone$ All statics while it sits on the
			// stack) would apply twice. Walk the shared stack exactly once,
			// under the first alive seat, keeping the original zone order.
			if z == state.ZStack && pi > 0 {
				continue
			}
			for _, id := range e.staticSourceIDs(p, z) {
				if o := e.G.Obj(id); o != nil && (o.Face() == nil || !offBattlefieldStaticsInert(z, o)) {
					add(o, id)
				}
			}
		}
	}
	// The Effect-delivered cost-modifier statics (task
	// param:api:Effect.ForgetOnCast): Mode$ ReduceCost/RaiseCost/SetCost
	// bodies effEffect registered with the line's own parameter map. The
	// walk reads e.active() so the entries inherit exactly the lifetimes the
	// layer walk honours -- Permanent entries survive their (already gone)
	// spell source, UntilEOT entries are already dropped by cleanup,
	// non-permanent entries end with the source -- and the printed walk
	// above never sees these (they are not face statics). The printed walk's
	// own PileStaticCount discipline stays untouched.
	e.appendEffectCostStatics(&out)
	markCostValidTarget(&out)
	return out
}

// markCostValidTarget sets out.validTarget from the collected members (and
// each member's selfOnly bit, markCostSelfOnly).
func markCostValidTarget(out *costStaticViews) {
	markCostSelfOnly(out)
	for _, group := range [...]struct {
		mode  string
		views []staticView
	}{{"RaiseCost", out.raise}, {"ReduceCost", out.reduce}, {"SetCost", out.set}} {
		for _, sv := range group.views {
			if _, ok := sv.Param(cards.PKValidTarget); ok {
				out.validTarget = true
				return
			}
			// Target-conditional ValidSpell$ and target-relative ReduceCost$
			// amounts read chosen targets, so the offer gate must retry with
			// potential targets for either shape.
			if validSpellHasTargeting(sv.ParamStr(cards.PKValidSpell)) ||
				(group.mode == "ReduceCost" && sv.ParamStr(cards.PKRelative) == "True") {
				out.validTarget = true
				return
			}
			// Any computed Amount$ may read the chosen targets: Battlefield
			// Thaumaturge's `TargetedObjectsDistinct$Valid Creature`, Not of
			// This World's `Count$Compare` over `TargetedByTarget$`,
			// Lullmage's Domination's `TargetedController$`, and whatever
			// spelling the next card uses. Target-dependence is NOT inferred
			// from spelling any more -- each spelling-matched rule here missed
			// the next carrier. Only a plain integer literal is provably
			// target-independent. A non-literal amount that turns out not to
			// read targets composes the same modifiers on the retry, so the
			// widening costs a target census on the already-failed path only.
			if amountMayReadTargets(sv) {
				out.validTarget = true
				return
			}
		}
	}
}

// amountMayReadTargets reports whether a cost-modifier static's Amount$ is
// anything other than a plain integer literal (see markCostValidTarget).
func amountMayReadTargets(sv staticView) bool {
	raw := strings.TrimSpace(sv.ParamStr(cards.PKAmount))
	if raw == "" {
		return false
	}
	_, ok := parseInt10(raw)
	return !ok
}

// appendEffectCostStatics appends the registry-delivered cost-modifier
// statics (see scanCostStatics) after the printed ones: the Effect-delivered
// ones, and every GRANTED one (state.ContinuousEffect.CostStaticGranted --
// Animate/AnimateAll staticAbilities$, Mode$ Continuous AddStaticAbility$,
// CopyPermanent AddStaticAbilities$). Both read e.active(), so a grant's
// lifetime is exactly the lifetime the layer walk honours for its siblings:
// cleanup drops an UntilEOT grant, the move sweep ends a zone-scoped one, a
// static-derived grant exists only while its granting static is live (the
// staticContinuous scan re-runs per event). Order is active()'s CR 613
// layer/timestamp order, then each spec-scoped grant's hosts in the
// deterministic zone walk grantedCostStaticHosts takes.
func (e *Engine) appendEffectCostStatics(out *costStaticViews) {
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		// A GRANTED AddKeyword$ Affinity entry (CR 702.41a) prices here: a
		// printed K:Affinity is expanded at keyword-expansion time into a face
		// ReduceCost static (cards/kw_affinity.go) that the printed walk above
		// collects, but a keyword GRANTED by a layer-6 Continuous static -- a
		// printed S: line's AddKeyword$ (Witherbloom, the Balancer's
		// "Instant and sorcery spells you cast have affinity for creatures",
		// Mycosynth Golem) or the same body delivered by an api:Effect
		// (effects' registerStaticEffectGrant) -- rides in
		// ContinuousEffect.AddKeywords and has no face to carry that static,
		// so the cost scan saw nothing. Synthesize the ReduceCost static the
		// printed expander would have minted and bind it to the grant's
		// recipients through appendGrantedCostStatic, the same machinery a
		// granted cost static uses: Affected$ and AffectedZone$ come off the
		// ContinuousEffect itself (Witherbloom's AffectedZone$ Stack keeps the
		// reduction off a spell still in hand), Amount$ is the inline
		// Count$Valid <spec><sep>YouCtrl (kwAffinity's separator rule: a
		// dot-less spec joins ".", a dotted one "+"), and no Color$ --
		// affinity reduces generic only. The count reads the battlefield, so a
		// grantor that is itself a creature you control prices its own granted
		// affinity.
		//
		// Fail closed: an AddKeywords entry that is not Affinity mints nothing
		// (its grant still applies through the ordinary layer-6 walk), and an
		// Affinity entry with an empty spec mints nothing. Condition gates
		// (Condition$/CheckSVar$/IsPresent$/...) are NOT re-read here because
		// registration already evaluated them before the effect went live --
		// the layer walk's continuousGateHolds for a printed S: line,
		// effects' effectStaticGrantReadable for an Effect-delivered body --
		// so what reaches this walk is a live grant and a dead one is not in
		// e.active() at all. A future grammar that prices other granted
		// cost-reduction keywords (Delve, Improvise, Convoke -- none granted
		// via AddKeyword$ in the corpus) registers alongside this arm.
		if arms := affinityGrantCostStatics(ce); len(arms) > 0 {
			for _, arm := range arms {
				e.appendGrantedCostStatic(&out.reduce, arm, state.ZStack)
			}
			continue
		}
		var dst *[]staticView
		switch cards.StaticModeOf(ce.CostStaticMode) {
		case cards.StaticRaiseCost:
			dst = &out.raise
		case cards.StaticReduceCost:
			dst = &out.reduce
		case cards.StaticSetCost:
			dst = &out.set
		default:
			continue
		}
		if ce.CostStaticGranted {
			e.appendGrantedCostStatic(dst, ce, 0)
			continue
		}
		*dst = append(*dst, staticView{Source: ce.Source, Controller: ce.Controller,
			Params: ce.CostStaticParams, ChosenNumber: ce.ChosenNumber, chosenNumberBound: true,
			Remembered: ce.Remembered, effectStamp: ce.Timestamp})
	}
}

// appendGrantedCostStatic binds one granted cost-modifier static to each of
// its hosts exactly as scanCostStatics binds a printed static to the object
// printing it: the host is the view's Source (ValidCard$/ValidTarget$
// Card.Self name the host, not the grantor), the host's CURRENT controller is
// its Controller (Activator$ You/Opponent read the object that has the
// ability), EffectZone$ gates on the host's zone (default the battlefield),
// and the granting face's SVar table resolves Amount$/CheckSVar$ names.
//
// atStack is the cast-context zone override (derivedWith's shape): a spell
// being PRICED is a stack spell for CR purposes even while it is still in
// hand (CR 601.2i -- the convoke announcement uses the same override), so
// when the caller sets it the AffectedZone$ gate and the host match read the
// cast zone instead of each candidate's live one. Callers pricing objects
// outside a cast pass 0 and keep the historical live-zone read.
func (e *Engine) appendGrantedCostStatic(dst *[]staticView, ce *ContinuousEffect, atStack state.Zone) {
	add := func(id state.ObjID) {
		o := e.G.Obj(id)
		if o == nil || (o.Zone == state.ZBattlefield && o.PhasedOut) ||
			!effectZoneOK(ce.CostStaticParam(cards.PKEffectZone), o.Zone) {
			return
		}
		*dst = append(*dst, staticView{Source: id, Controller: o.Controller,
			Params: ce.CostStaticParams, SVars: ce.CostStaticSVars})
	}
	affects := strings.TrimSpace(ce.Affects)
	if affects == "" || affects == "Card.Self" {
		add(ce.Source)
		return
	}
	for pi, p := range e.G.AliveFrom(0) {
		for _, z := range staticSourceZones {
			if z == state.ZStack && pi > 0 {
				continue
			}
			zone := z
			if atStack != 0 {
				zone = atStack
			}
			if !grantedHostZone(ce.AffectedZone, zone) {
				continue
			}
			for _, id := range e.G.Zone(z, p) {
				if e.grantedCostHostMatches(ce, id, atStack) {
					add(id)
				}
			}
		}
	}
}

// grantedCostHostMatches binds one spec-scoped grant to one candidate host.
// atStack==0 is the historical log-only read (matchesSpecFrom). An
// AffectedZone$ Stack cast-pricing context (atStack set) binds through the
// layer walk's own matcher instead -- castProvenanceAdmitsWindow's pre-push
// OFFER window (the object BEING CAST is still in hand at offer time; the
// window's own doc names Witherbloom's and Mycosynth Golem's affinity grants
// as its carriers) plus the filter's AsStack read -- exactly the binding
// derivedWith/derivedCompute gives the layer walk for the same situation, so
// a granted keyword and the cost static minted from it cannot disagree about
// who is affected. The Card.Self early-out and the phased-out gate inside
// matchesWithCharsPT are pure rejections, identical to the plain read.
func (e *Engine) grantedCostHostMatches(ce *ContinuousEffect, id state.ObjID, atStack state.Zone) bool {
	if atStack == 0 {
		return e.matchesSpecFrom(strings.TrimSpace(ce.Affects), id, ce.Controller, ce.Source)
	}
	return e.matchesWithCharsPT(ce, id, nil, nil, atStack, 0, 0, 0, 0, false)
}

// grantedHostZone reports whether a spec-scoped grant reaches objects in z:
// the battlefield by default (Forge's Affected$ default), otherwise the
// grant's own AffectedZone$ list.
func grantedHostZone(affectedZone string, z state.Zone) bool {
	if strings.TrimSpace(affectedZone) == "" {
		return z == state.ZBattlefield
	}
	return affectedZoneOK(affectedZone, z)
}

// affinityGrantCostStatics turns one active continuous effect's granted
// AddKeyword$ Affinity entries into the ReduceCost cost statics the cost scan
// prices, one synthesized static per Affinity entry (see
// appendEffectCostStatics). Returns nil when no entry in this grant produces
// one.
func affinityGrantCostStatics(ce *ContinuousEffect) []*ContinuousEffect {
	if ce.CostStaticMode != "" || len(ce.AddKeywords) == 0 {
		return nil
	}
	// Fail closed: a grant whose Affected$ is absent (Forge's Card.Self
	// default) names the grantor itself -- a spell-cost static pointed at a
	// permanent is not a readable grant, so mint nothing rather than bind the
	// source through the short-circuit.
	if a := strings.TrimSpace(ce.Affects); a == "" || a == "Card.Self" {
		return nil
	}
	var arms []*ContinuousEffect
	for _, k := range ce.AddKeywords {
		head, param, _ := strings.Cut(k, ":")
		if !strings.EqualFold(head, "Affinity") {
			continue
		}
		// A second colon is a human description, exactly the trailing-field
		// strip the printed expander does; only the first field is the spec.
		spec, _, _ := strings.Cut(param, ":")
		if strings.TrimSpace(spec) == "" {
			continue
		}
		sep := "."
		if strings.ContainsRune(spec, '.') {
			sep = "+"
		}
		arm := *ce
		arm.CostStaticGranted = true
		arm.CostStaticParams = map[string]string{
			"Mode":       "ReduceCost",
			"ValidCard":  "Card.Self",
			"Type":       "Spell",
			"EffectZone": "All",
			"Amount":     "Count$Valid " + spec + sep + "YouCtrl",
		}
		arm.CostStaticSVars = ce.SVars
		arms = append(arms, &arm)
	}
	return arms
}
