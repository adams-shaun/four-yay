package pay

import (
	"fmt"
	"math/bits"
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects/params"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// AnnouncedAbilityColours is the colour list one ability flattens into in the
// announced window (spec §4.2), or nil for a single option: an explicit Combo
// (the manual wheel's own flattener), or -- for a planner-tier ability whose
// choice the planner already makes concrete -- Any / Chosen / ColorIdentity.
func AnnouncedAbilityColours(e Engine, p state.PlayerID, id state.ObjID, ma *cards.SA, chosen string) []string {
	if cols, ok := ManaAbilityComboColours(ma, chosen); ok {
		return cols
	}
	if ma == nil || ma.API != "Mana" {
		return nil
	}
	raw := params.ManaOf(ma).Produced
	if raw != "Any" && raw != "ColorIdentity" && !PaymentPlanChoiceShape(raw) {
		return nil
	}
	if tier, _, _ := PaymentPlanAbilityTier(e, p, id, ma); tier != TierNormal && tier != TierLastResort {
		return nil
	}
	return PaymentPlanChoiceColours(e, id, ma)
}

// PaymentPlanStepReady resolves one remaining witness step to the exact
// alternative it names (paymentPlanStepAlternative), or says why it no longer
// can. The source itself changed -- gone, another incarnation, tapped, phased
// out, another controller, or no V1 mana ability left under the step's
// identity -- is source_changed; the same untapped source still offering the
// step's ability identity, but not the witnessed production, is
// production_changed. It never names a substitute.
func PaymentPlanStepReady(e Engine, p state.PlayerID, units []WindowUnit, pa decision.PaymentActivation) (Alt, string) {
	o := e.Game().Obj(pa.Source)
	if o == nil || o.Zone != state.ZBattlefield || o.Tapped || o.PhasedOut || o.Controller != p ||
		pa.SourceZoneSeq != PaymentSourceZoneSeq(e, pa.Source) {
		return Alt{}, FallbackSourceChanged
	}
	if step, ok := PaymentPlanStepAlternative(e, units, pa); ok {
		return step, ""
	}
	for _, u := range units {
		if u.ID != pa.Source {
			continue
		}
		for _, alt := range PaymentPlanQueryAlternatives(e, u) {
			if alt.Activation.Ability == pa.Ability {
				return Alt{}, FallbackProductionChanged
			}
		}
	}
	return Alt{}, FallbackSourceChanged
}

// PaymentPlanAbilityTier is the single source-shape authority for automatic
// payment. It is intentionally closed-world: new Forge parameters require an
// explicit review before the planner can rely on them. It folds the ability's
// own shape (paymentPlanAbilityShapeTier) with the triggers and replacements
// that can act on this source's tap or mana (paymentPlanSourceInterference):
// a deferring interference defers the source, and the source's own
// fully determined consequence (City of Brass's damage, Mana Vault's
// doesn't-untap) makes an otherwise normal source last resort.
func PaymentPlanAbilityTier(e Engine, p state.PlayerID, id state.ObjID, ma *cards.SA) (Tier, Consequence, string) {
	tier, c, detail := paymentPlanAbilityShapeTier(e, p, id, ma)
	if tier == TierDeferred {
		return tier, c, detail
	}
	// A last-resort step always discloses a consequence (spec §4: a present
	// consequence sets at least one field); a shape that classified last
	// resort without one is not a shape the witness can describe.
	if tier == TierLastResort && c == (Consequence{}) {
		return TierDeferred, Consequence{}, "source:last_resort"
	}
	switch it, ic, idetail := e.Eval().SourceInterference(id, ma); it {
	case TierDeferred:
		return it, ic, idetail
	case TierLastResort:
		c.Sacrifice = c.Sacrifice || ic.Sacrifice
		c.Life += ic.Life
		c.Damage += ic.Damage
		c.NoUntap = c.NoUntap || ic.NoUntap
		c.ReturnToHand = c.ReturnToHand || ic.ReturnToHand
		return TierLastResort, c, "source:last_resort"
	}
	return tier, c, detail
}

// paymentPlanAbilityShapeTier classifies the ability's own cost, production,
// parameters and SubAbility$ chain.
//
// Without a SubAbility$ the verdict reads only the ability's own Params and
// its compiled cost (paymentPlanShapeTierOf), so a configured ability's
// verdict is computed once with its configured facts (manaSAFacts.shape*)
// instead of walking its parameter map on every census; verify mode
// (manaSAFactsVerify) recomputes the facts on every hit.
func paymentPlanAbilityShapeTier(e Engine, p state.PlayerID, id state.ObjID, ma *cards.SA) (Tier, Consequence, string) {
	if ma != nil {
		if tier, c, detail, known := e.Eval().ManaShape(ma); known {
			return tier, c, detail
		}
	}
	var cost costvocab.Cost
	if ma != nil {
		cost = ParseCostOf(e, ma.ParamStr(cards.PKCost))
	}
	tier, c, detail, rider := PaymentPlanShapeTierOf(ma, cost)
	if !rider {
		return tier, c, detail
	}
	deferred := func(detail string) (Tier, Consequence, string) {
		return TierDeferred, Consequence{}, detail
	}
	if PlanRiderHasTarget(e.Game(), id, ma) {
		return deferred("source:target")
	}
	if !PaymentPlanTapOnlyCost(cost) {
		return deferred("source:last_resort")
	}
	if d, ok := PaymentPlanDamageRider(e, id, ma); ok {
		return TierLastResort, Consequence{Damage: d}, "source:last_resort"
	}
	if PaymentPlanParadiseRider(e, id, ma) {
		return TierLastResort, Consequence{ReturnToHand: true}, "source:last_resort"
	}
	return deferred("source:rider")
}

// PaymentPlanUnitAlternatives expands one physical source into the exact
// one-tap outcomes V1 can execute.  A fixed production is one alternative.
// A finite choice -- Produced$ Any (choose one of WUBRG, any literal
// amount), an amount-1 Combo (one alternative per listed/resolved colour),
// a recorded Chosen, or the commander colour identity -- is one alternative
// per producible colour: the planner records the selected colour in
// Produces AND carries a withProduced copy of the ability as exec, so
// execution runs the ordinary mana path with no colour prompt.  An
// allocation (amount > 1), Combo Any, and every open production remain
// manual because they need a shape or source-state read the witness cannot
// represent.
func PaymentPlanUnitAlternatives(e Engine, u WindowUnit) []Alt {
	_, alts := appendUnitAlternatives(e, nil, u)
	return alts
}

// appendUnitAlternatives is paymentPlanUnitAlternatives appending into dst
// (a query scope's alternative arena): it returns the grown dst and u's
// alternatives as a capped span of it, or nil when u has none.
func appendUnitAlternatives(e Engine, dst []Alt, u WindowUnit) (grown, alts []Alt) {
	start := len(dst)
	out := dst
	// The source's payer, zone-entry sequence and creature bit are read once
	// for all its alternatives (each a pure read of the source).
	payer := state.PlayerID(0)
	if source := e.Game().Obj(u.ID); source != nil {
		payer = source.Controller
	}
	sourceRead := false
	var zoneSeq uint64
	var creature bool
	for _, alt := range u.Alts {
		tier, consequence, _ := PaymentPlanAbilityTier(e, payer, u.ID, alt.Ma)
		switch tier {
		case TierNormal:
			if !e.Eval().ManaStatic(alt.Ma).TapOnly {
				continue
			}
		case TierLastResort:
			// The classifier vetted the whole cost and chain: {T} plus the
			// disclosed self-sacrifice/life/self-return parts, or a tap-only
			// cost with a disclosed rider, trigger or replacement.
		default:
			continue
		}
		ab, ok := PaymentAbility(e.Game(), u.ID, alt.Ma)
		if !ok {
			continue
		}
		if !sourceRead {
			sourceRead = true
			zoneSeq, creature = PaymentSourceZoneSeq(e, u.ID), slices.Contains(e.Chars().DerivedTypes(u.ID), "Creature")
		}
		if PlanAltOK(alt) {
			m := alt.Mana()
			if out == nil {
				out = make([]Alt, 0, len(u.Alts))
			}
			out = append(out, Alt{Activation: decision.PaymentActivation{
				Source: u.ID, SourceZoneSeq: zoneSeq, Ability: ab, Produces: ManaAmount(m)},
				Mana: m, Creature: creature, Ma: alt.Ma, Tier: tier, Consequence: consequence})
			continue
		}
		if !alt.Any || alt.Amt <= 0 {
			continue
		}
		for _, col := range PaymentPlanChoiceColours(e, u.ID, alt.Ma) {
			i := strings.IndexByte("WUBRG", col[0])
			if i < 0 {
				continue
			}
			var m state.Mana
			m[i] = alt.Amt
			if out == nil {
				out = make([]Alt, 0, len(u.Alts))
			}
			out = append(out, Alt{Activation: decision.PaymentActivation{
				Source: u.ID, SourceZoneSeq: zoneSeq, Ability: ab, Produces: ManaAmount(m)},
				Mana: m, Creature: creature, Ma: alt.Ma, ExecProduced: col, Tier: tier, Consequence: consequence})
		}
	}
	// Preserve flexible sources: rank each selected source by every eligible
	// outcome it could have supplied, rather than by only the outcome the
	// search happened to choose. Flexibility is the number of DISTINCT mana
	// types those outcomes produce (spec 5 key 5), so a Produced$ Any source
	// counts 5, a typed dual 2, and two abilities that both add {U} count 1.
	if len(out) == start {
		return out, nil
	}
	grown = out
	out = out[start:len(out):len(out)]
	var types, all uint8 // bit i = mana index i (W/U/B/R/G/C)
	for _, a := range out {
		for i, n := range a.Mana {
			if n > 0 {
				all |= 1 << i
				if a.Tier == TierNormal {
					types |= 1 << i
				}
			}
		}
	}
	flex, flexAll := bits.OnesCount8(types), bits.OnesCount8(all)
	colours := types &^ (1 << state.MC)
	for i := range out {
		out[i].Flex = flex
		out[i].FlexAll = flexAll
		out[i].Colours = colours
	}
	return grown, out
}

// PaymentPlanStepAlternative resolves one witness step to the exact
// alternative the planner offers for its source: the one whose ability
// identity AND production both equal the step's. Identity alone is not
// enough -- every intrinsic ability shares {intrinsic, basic_land}, so a
// Volcanic Island's {U} and {R} abilities differ only in Produces. The
// disclosed consequence must also be exactly the one the source would incur
// now (spec §6: a changed consequence is production_changed). The pair
// is: a printed identity names one ability by index (a Produced$ Any
// ability's alternatives then differ by Produces), and a source's intrinsic
// abilities are de-duplicated by production (cards.ApplyIntrinsics and the
// granted basic-land-type walk). Validation (ValidateCastPayment,
// validatePendingPaymentPlan) and execution (executePlannedManaActivation)
// all resolve a step here, so the alternative validation priced is the one
// execution activates; none of them may pair one alternative's identity with
// another alternative's production.
func PaymentPlanStepAlternative(e Engine, units []WindowUnit, pa decision.PaymentActivation) (Alt, bool) {
	for _, u := range units {
		if u.ID != pa.Source {
			continue
		}
		for _, candidate := range PaymentPlanQueryAlternatives(e, u) {
			if candidate.Ma != nil && candidate.Activation.Ability == pa.Ability && candidate.Activation.Produces == pa.Produces &&
				ConsequenceEqual(candidate.Consequence, pa.Consequence) {
				return candidate, true
			}
		}
	}
	return Alt{}, false
}

// PaymentSourceZoneSeq is the existing log sequence of this object's current
// zone entry. Genesis objects have no entry event and use the contract's zero
// sentinel. It reads the engine's incremental zone-entry index
// (payment_zone_entry.go), which answers exactly as the backward scan
// (paymentSourceZoneSeqScan) does.
func PaymentSourceZoneSeq(e Engine, id state.ObjID) uint64 {
	got := e.ZoneEntrySeq(id)
	if e.Verify() {
		if want := PaymentSourceZoneSeqScan(e, id); got != want {
			panic(fmt.Sprintf("payment zone-entry index: object %d seq %d, log scan %d", id, got, want))
		}
	}
	return got
}

// PaymentPlanQueryAlternatives is paymentPlanUnitAlternatives, computed once
// per query scope and source. The returned alternatives are shared: callers
// only read them.
func PaymentPlanQueryAlternatives(e Engine, u WindowUnit) []Alt {
	q := e.Session().PlanQuery
	if !q.Valid(e.Log()) {
		return PaymentPlanUnitAlternatives(e, u)
	}
	if alts, ok := q.Alts[u.ID]; ok {
		if e.Verify() && !slices.EqualFunc(alts, PaymentPlanUnitAlternatives(e, u), PlanSameAlternative) {
			panic(fmt.Sprintf("payment plan query: cached alternatives for source %d are stale", u.ID))
		}
		return alts
	}
	// The scope's alternatives share one arena, reset when the scope is
	// recycled (paymentPlanQueryBegin): no alternative outlives its scope.
	var alts []Alt
	q.AltArena, alts = appendUnitAlternatives(e, q.AltArena, u)
	if q.Alts == nil {
		q.Alts = map[state.ObjID][]Alt{}
	}
	q.Alts[u.ID] = alts
	return alts
}
