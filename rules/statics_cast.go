package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// SpellCopyAllowed reports whether no live CantBeCopied static names id.
// The source-zone walk follows staticSourceZones, so Stack-scoped spell
// statics and broader EffectZone statics share the normal static filter path.
func (e *Engine) SpellCopyAllowed(id state.ObjID) bool {
	candidate := e.G.Obj(id)
	if candidate == nil || candidate.Zone != state.ZStack {
		return false
	}
	for pi, p := range e.G.AliveFrom(0) {
		for _, z := range staticSourceZones {
			if z == state.ZStack && pi > 0 {
				continue
			}
			for _, source := range e.staticSourceIDs(p, z) {
				o := e.G.Obj(source)
				if o == nil || o.Face() == nil || offBattlefieldStaticsInert(z, o) || e.printedAbilitiesGone(o) {
					continue
				}
				for si, n := 0, o.PileStaticCount(); si < n; si++ {
					pst, ok := o.PileStaticAt(si)
					if !ok || pst.Static.Mode != "CantBeCopied" || !staticEffectZoneOK(pst.Static, o.Zone) {
						continue
					}
					spec := strings.TrimSpace(pst.Static.ParamStr(cards.PKValidCard))
					if spec == "" {
						continue
					}
					sv := staticView{Source: source, Controller: o.Controller, Params: pst.Static.Params, PS: pst.Static.ParamSetOf(), SVars: pst.Face.SVars}
					if e.matchesSpec(spec, id, e.staticSpecCtx(sv)) {
						return false
					}
				}
			}
		}
	}
	return true
}

// countersRemainApplies reports whether the departing permanent itself has
// an active CountersRemain static. Static lines are read through the canonical
// activeStatics walk so EffectZone, merged-face and face-down rules stay
// consistent with the other static consumers.
func (e *Engine) countersRemainApplies(id state.ObjID) bool {
	for _, sv := range e.activeStatics("CountersRemain") {
		spec := sv.ParamStr(cards.PKValidCard)
		if spec != "" && e.matchesSpec(spec, id, e.staticSpecCtx(sv)) {
			return true
		}
	}
	return false
}

// SurveilLookExtra reports the additional cards a surveil performed by player
// p looks at, from the battlefield statics with Mode$ SurveilNum whose
// ValidPlayer$ admits p (Enhanced Surveillance's "You may look at an
// additional two cards each time you surveil"). mandatory is added to the
// count unconditionally. optional holds ONE entry per OPTIONAL static -- that
// static's own Num$ -- in deterministic activeStatics order: each Optional$
// True static is an independent may effect (surveilnum-r2), so effSurveil
// poses one multi-select election over the entries and the controller can
// accept any subset, never an all-or-nothing sum of two "may"s. The walk is
// the canonical activeStatics collector, so a face-down, merged-pile or
// EffectZone-scoped static is read exactly as every other static mode is, and
// the order is deterministic. Num$ must be a literal or an SVar name the
// static's own face defines; anything else fails closed to no contribution,
// the same direction HandSizeValueOK takes.
func (e *Engine) SurveilLookExtra(p state.PlayerID) (mandatory int32, optional []int32) {
	for _, sv := range e.activeStatics("SurveilNum") {
		spec := strings.TrimSpace(sv.ParamStr(cards.PKValidPlayer))
		if spec == "" {
			spec = "You"
		}
		if !effects.MatchesPlayerSpec(e.G, spec, p, sv.Controller) {
			continue
		}
		n, ok := e.surveilNumValue(sv)
		if !ok || n <= 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(sv.ParamStr(cards.PKOptional)), "True") {
			optional = append(optional, n)
		} else {
			mandatory += n
		}
	}
	return mandatory, optional
}

// surveilNumValue prices one SurveilNum static's Num$: a plain decimal
// literal, else an SVar name resolved against the static's own face table.
// A missing/empty Num$, an unresolvable SVar and a negative value all report
// false.
func (e *Engine) surveilNumValue(sv staticView) (int32, bool) {
	raw := strings.TrimSpace(sv.ParamStr(cards.PKNum))
	if raw == "" {
		return 0, false
	}
	if v, err := strconv.Atoi(raw); err == nil {
		if v < 0 {
			return 0, false
		}
		return int32(v), true
	}
	body, ok := sv.SVars[raw]
	if !ok {
		return 0, false
	}
	v, ok := effects.EvalCountOK(e, effects.NewCtxPtr(sv.Source, sv.Controller, effects.CtxInit{SVars: sv.SVars}), body)
	if !ok || v < 0 {
		return 0, false
	}
	return v, true
}

// castRestricted reports whether p is forbidden from casting id (CantBeCast).
func (e *Engine) castRestricted(p state.PlayerID, id state.ObjID) bool {
	return e.castRestrictedUsing(e.activeStatics("CantBeCast"), p, id)
}

func (e *Engine) castRestrictedUsing(statics []staticView, p state.PlayerID, id state.ObjID) bool {
	for _, sv := range e.castRestrictionSources(statics, id) {
		if !e.actorMatches(sv, "Caster", p) {
			continue
		}
		// The shared continuous gate (rules/layers.go) adds the IsPresent$/
		// IsPresent2$/PresentCompare$/PresentZone$/CheckSVar$/SVarCompare$
		// family for the CantBeCast consumer (Blizzard's "as long as the
		// defending player doesn't control a snow land"). It is wired HERE
		// and at recheckIllegal only: the other restrictionGateHolds callers
		// -- CantBeActivated, AssignCombatDamageAsUnblocked,
		// CombatDamageToughness -- keep their pre-existing gate set, so no
		// out-of-scope consumer's semantics move with this task. It subsumes
		// the checkSVarHolds the caller used to run separately, and the
		// duplicate ClassBand$/Condition$ reads inside restrictionGateHolds
		// below are pure state reads with identical semantics.
		if !e.continuousGateHolds(sv) || !e.restrictionGateHolds(sv, id) {
			continue
		}
		spec := sv.ParamStr(cards.PKValidCard)
		// The origin-zone cast-provenance split (task wascastfrom): a
		// CantBeCast restriction's ValidCard$ carrying a wasCastFromExile /
		// wasCastFromTheirHand-shaped token gates the cast IN PROGRESS -
		// which has no PutOnStack yet - so the origin is the restricted
		// object's CURRENT zone (every cast evaluation site's pending origin;
		// see castOriginAdmitsAtZone). Without the split the token is unknown
		// to the effects-side filter and the restriction silently never
		// applies (the permissive-wrong direction for a prohibition).
		if specCarriesCastOrigin(spec) {
			if o := e.G.Obj(id); o != nil {
				s, ok := e.castOriginAdmitsAtZone(spec, id, o.Zone)
				if !ok {
					continue
				}
				spec = s
			}
		}
		if e.matchesSpec(spec, id, e.staticSpecCtx(sv)) {
			return true
		}
	}
	return false
}

// castRestrictionSources merges the battlefield restriction statics with the
// target card's OWN CantBeCast statics -- a self-restriction (Rakdos, Lord of
// Riots' "You can't cast this spell unless an opponent lost life this turn")
// is carried on the restricted card itself, so it must be live wherever that
// card sits, gated by each static's EffectZone$ (Forge's default is the
// battlefield, so the same gate collectCostStatics uses decides whether a
// hand/library/stack source is live). Without the self-merge a hand-zone
// lockout is invisible: activeStatics walks the battlefield only, and a
// battlefield-only restriction can never reach the card it restricts.
func (e *Engine) castRestrictionSources(statics []staticView, id state.ObjID) []staticView {
	out := statics
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return out
	}
	for _, st := range o.Face().Statics {
		if st.Mode != "CantBeCast" || !staticEffectZoneOK(st, o.Zone) {
			continue
		}
		out = append(out, staticView{Source: id, Controller: o.Controller, Params: st.Params, PS: st.ParamSetOf()})
	}
	return out
}

// restrictionGateHolds evaluates the non-matching gates one CantBeCast /
// CantBeActivated restriction must pass before its ValidCard$/ValidSA$ match
// is even consulted: AffectedZone$ (the zone the restricted card must sit
// in -- Linvala's and Karn's Battlefield, Ashes of the Abhorrent's Graveyard)
// and Condition$ (PlayerTurn / NotPlayerTurn, resolved against the SOURCE's
// controller: Grand Abolisher's "During your turn" is the abolisher
// controller's turn, never the restricted caster's, which is why the shared
// costConditionHolds -- keyed to the payer -- must not be reused here). A
// condition this build cannot evaluate fails closed: a lockout that silently
// always applies over-restricts, but one that silently never applies lets an
// illegal action through, and the static family's whole point is the
// prohibition.
func (e *Engine) restrictionGateHolds(sv staticView, target state.ObjID) bool {
	if !e.classBandGateHolds(sv.ParamStr(cards.PKClassBand), sv.Source) {
		return false
	}
	if az, ok := sv.Param(cards.PKAffectedZone); ok {
		o := e.G.Obj(target)
		if o == nil || !affectedZoneOK(az, o.Zone) {
			return false
		}
	}
	switch sv.condition() {
	case condBlank:
		return true
	case condPlayerTurn:
		return e.G.Active == sv.Controller
	case condNotPlayerTurn:
		return e.G.Active != sv.Controller
	}
	return false
}

// abilityRestricted reports whether id's specific ability ab is forbidden
// from being activated (CantBeActivated), scoped by the restriction's
// ValidSA$ to the ability being considered (Task 10: p, the would-be
// activator, scopes Activator$; ab, the exact activated ability, scopes
// ValidSA$). A nonexistent object has no ability to restrict, so it degrades
// to false rather than dereferencing a nil Object.
func (e *Engine) abilityRestricted(p state.PlayerID, id state.ObjID, ab *cards.SA) bool {
	return e.abilityRestrictedUsing(e.activeStatics("CantBeActivated"), p, id, ab)
}

func (e *Engine) abilityRestrictedUsing(statics []staticView, p state.PlayerID, id state.ObjID, ab *cards.SA) bool {
	o := e.G.Obj(id)
	if o == nil {
		return false
	}
	for _, sv := range statics {
		if !e.actorMatches(sv, "Activator", p) {
			continue
		}
		if !e.restrictionGateHolds(sv, id) || !e.checkSVarHolds(sv) {
			continue
		}
		if !e.matchesSpec(sv.ParamStr(cards.PKValidCard), id, e.staticSpecCtx(sv)) {
			continue
		}
		if activatedMatchesValidSA(ab, sv.ParamStr(cards.PKValidSA)) {
			return true
		}
	}
	return false
}

// activatedMatchesValidSA reports whether a CantBeActivated restriction whose
// ValidSA$ reads validSA applies to the specific activated ability ab. The
// grammar is Forge's comma-separated OR list of "<kind>.<constraint>" values.
//
// An absent ValidSA$ applies to every activated ability (including mana). A
// value whose kind is not "Activated" (a Spell / Instant / Sorcery shape)
// describes a cast, never an activation, so it does not match an ability.
// Within the Activated kind: no constraint matches everything; "!ManaAbility"
// matches everything but a mana ability (ab.API == "Mana"); "ManaAbility"
// matches only a mana ability, and "ManaAbility<Produce:C>" the subset that
// produces colour C (test-only grammar, fix round 1 -- see the case below);
// and a constraint this build cannot evaluate --
// Loyalty, Equip, Crew+Vehicle, hasTapCost, ... -- DENIES, per the "a
// restriction that cannot be evaluated must deny, not silently allow" rule:
// erring toward applying a CantBeActivated is the safe direction, because the
// consequence of wrongly allowing an activation a static forbids is an illegal
// game action, while wrongly blocking one merely withholds an option the
// activator could have taken.
func activatedMatchesValidSA(ab *cards.SA, validSA string) bool {
	v := strings.TrimSpace(validSA)
	if v == "" {
		return true // no ValidSA$: applies to every activated ability
	}
	for alt := range strings.SplitSeq(v, ",") {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		kind, constraint := alt, ""
		if i := strings.IndexByte(alt, '.'); i >= 0 {
			kind, constraint = alt[:i], alt[i+1:]
		}
		if kind != "Activated" {
			continue
		}
		if constraint == "" {
			return true
		}
		switch {
		case constraint == "!ManaAbility":
			if !cards.IsManaAbilityAPI(ab.API) {
				return true
			}
			// else: mana abilities are expressly spared; try the next alt
		case constraint == "ManaAbility" || strings.HasPrefix(constraint, "ManaAbility<"):
			if !cards.IsManaAbilityAPI(ab.API) {
				break // not a mana ability; try the next alt
			}
			// Bare ManaAbility matches every mana ability. A
			// ManaAbility<Produce:C> scopes to a mana ability that produces
			// colour C (fix round 1, reviewer Important 2): a permanent with
			// several mana abilities can then have one singled out by a
			// CantBeActivated while the others stay activatable -- the shape
			// that exposed the gate/activation disagreement Test
			// TestActivateSkipsRestrictedManaAbility exercises. The corpus has
			// no such value (grep shows CantBeActivated ValidSA$ is empty,
			// "Activated", or "Activated.!ManaAbility"), so this extension is
			// test-only grammar, but it makes the per-ability agreement
			// reachable instead of hypothetical.
			if constraint != "ManaAbility" && strings.HasSuffix(constraint, ">") {
				inner := constraint[len("ManaAbility<") : len(constraint)-1]
				color := inner
				if j := strings.IndexByte(inner, ':'); j >= 0 {
					color = inner[j+1:]
				}
				produced := effects.ManaOf(ab).Produced
				if produced == "" {
					produced = "C"
				}
				target := strings.TrimSpace(color)
				if target != "" && (produced == target || (len(target) == 1 && strings.Contains(produced, target))) {
					return true
				}
				break // this mana ability produces a different colour; next alt
			}
			return true // bare ManaAbility
		default:
			// Unevaluable constraint under the Activated kind: deny (see doc).
			return true
		}
	}
	return false
}

// adjustedCost applies the full RaiseCost/ReduceCost/SetCost composition to
// id's printed mana cost. RaiseCost Cost$ may add coloured pips (and fixed
// life), while ReduceCost Color$ may remove matching coloured pips before its
// remaining generic reduction and MinMana$ floor. costMods.apply owns that
// CR 601.2f order and clamps only the generic component at zero; it never
// lets a generic reduction consume a coloured pip.
//
// A missing object or a Face()-less one (an ability object or a token
// mid-resolution) degrades to the zero Cost rather than panicking; nothing
// in this build calls adjustedCost with such an id today; the guard exists
// because a caller might one day compute a would-be cost speculatively.
func (e *Engine) adjustedCost(p state.PlayerID, id state.ObjID) Cost {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return Cost{}
	}
	return e.costModifiers(p, id, spellScope("")).Apply(e.faceCost(o.Face()))
}

// castWithFlash reports whether an active CastWithFlash static gives p
// permission to cast id at instant speed. It is intentionally shared by every
// zone that can cast a spell; a Vedalken Orrery must not stop working when a
// later alternative permits casting from another zone.
//
// The offer walk passes id's POTENTIAL legal targets: a target-conditional
// grant such as Flash Photography's
// `ValidSA$ Spell.IsTargeting Valid Permanent.YouCtrl` is permission only when
// some legal target can satisfy the restriction. CR 601.2e's recheckIllegal
// re-runs castWithFlashTargets with the ANNOUNCED targets, so a cast that took
// the permission on a non-qualifying target is reversed rather than completed.
func (e *Engine) castWithFlash(p state.PlayerID, id state.ObjID) bool {
	// Fast path: with no CastWithFlash static anywhere (the overwhelming
	// majority of offers), do not pay for a potential-target census.
	if len(e.withSelfStatics(e.activeStatics("CastWithFlash"), id, "CastWithFlash")) == 0 {
		return false
	}
	return e.castWithFlashTargets(p, id, e.costPotentialTargets(p, id, spellScope("")))
}

// castWithFlashTargets is castWithFlash against an explicit target list. The
// whole CastWithFlash collection is read through ONE home -- the battlefield
// walk every static consumer uses plus the card's OWN face statics, which
// activeStatics never sees while the card is still in hand (the same two
// sources alternativeCosts reads for a self-carried AlternativeCost) -- so the
// offer and the recheck cannot disagree about which statics grant the
// permission.
func (e *Engine) castWithFlashTargets(p state.PlayerID, id state.ObjID, targets []state.Target) bool {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return false
	}
	for _, sv := range e.withSelfStatics(e.activeStatics("CastWithFlash"), id, "CastWithFlash") {
		if !e.actorMatches(sv, "Caster", p) || !e.staticTimingGate(sv) {
			continue
		}
		// EffectZone$ (Skittering Cicada's EffectZone$ Battlefield): the
		// static functions only while its source sits in the named zone --
		// a battlefield static's permission ends with the source's presence,
		// exactly like every other zone-scoped static.
		if az, ok := sv.Param(cards.PKEffectZone); ok {
			src := e.G.Obj(sv.Source)
			if src == nil || !affectedZoneOK(az, src.Zone) {
				continue
			}
		}
		if !e.spellMatchesValidSA(o.Face(), sv.ParamStr(cards.PKValidSA), id, sv.Source, p, targets) {
			continue
		}
		if e.matchesSpec(sv.ParamStr(cards.PKValidCard), id, e.staticSpecCtx(sv)) {
			return true
		}
	}
	return false
}

// castWithFlashAsFace is castWithFlash priced AS face f of id: the
// potential-target census and the self statics read f, not the face the
// object currently displays. The split_alt offer uses it so a target-
// conditional grant is judged against the half actually being cast (CR
// 709.4: each half is cast as its own spell, with its own targets and
// face-local statics), and the fuse offer's per-half timing reads it for
// each half -- so neither half borrows the other's permission. f must be one
// of the object's own faces; offerAsFace prices the live face unchanged
// otherwise, and is a no-op when f IS the live face.
func (e *Engine) castWithFlashAsFace(p state.PlayerID, id state.ObjID, f *cards.Face) bool {
	return e.offerAsFace(id, f, func() bool { return e.castWithFlash(p, id) })
}

// withSelfStatics appends the named statics the card carries on its OWN face
// to a collected list: activeStatics never sees a self-carried static while
// the card is still in hand (the same second source alternativeCosts reads
// for a self-carried AlternativeCost). A self static's source and controller
// are the card itself, so its Caster$/You context resolves relative to the
// caster's own card. base is copied before any append because activeStatics
// returns a cached slice during a legal-actions walk and must never be
// mutated.
func (e *Engine) withSelfStatics(base []staticView, id state.ObjID, mode string) []staticView {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return base
	}
	var count int
	for _, st := range o.Face().Statics {
		if st.Mode == mode {
			count++
		}
	}
	if count == 0 {
		return base
	}
	out := append([]staticView(nil), base...)
	for _, st := range o.Face().Statics {
		if st.Mode == mode {
			out = append(out, staticView{Source: id, Controller: o.Controller, Params: st.Params, PS: st.ParamSetOf()})
		}
	}
	return out
}

// hasTargetConditionalFlash reports whether id carries or is granted a
// CastWithFlash static whose ValidSA$ names an `IsTargeting` alternative. It
// is the narrow predicate recheckIllegal uses to decide whether an off-sorcery
// cast's timing rested on a target-conditional grant (a card with no such
// static keeps its existing CR 601.2e behaviour).
func (e *Engine) hasTargetConditionalFlash(p state.PlayerID, id state.ObjID) bool {
	for _, sv := range e.withSelfStatics(e.activeStatics("CastWithFlash"), id, "CastWithFlash") {
		if !e.actorMatches(sv, "Caster", p) {
			continue
		}
		if validSpellHasTargeting(sv.ParamStr(cards.PKValidSA)) {
			return true
		}
	}
	return false
}

// flashGrantCoversTargets is the CR 601.2e enforcement half of a target-
// conditional CastWithFlash grant, read for ONE face of a split card: it
// answers false only when face f's off-sorcery timing could have rested on
// an IsTargeting-conditional grant AND the announced targets do not satisfy
// that grant. A face with its own unconditional timing (instant, Flash,
// MayFlashSac's rider) never rested on the grant; a grant without an
// IsTargeting alternative imposes no target requirement. The reads are
// face-scoped (offerAsFace), so a fused cast's alternate half is judged
// against ITS face and ITS stage's targets -- the same read the offer ran
// through castWithFlashAsFace, keeping offer and enforcement on one
// interpretation.
func (e *Engine) flashGrantCoversTargets(p state.PlayerID, id state.ObjID, f *cards.Face, targets []state.Target) bool {
	if f == nil || f.IsInstant() || e.hasKeywordH(id, kwhFlash) || mayFlashSacFace(f) {
		return true
	}
	return e.offerAsFace(id, f, func() bool {
		return !e.hasTargetConditionalFlash(p, id) || e.castWithFlashTargets(p, id, targets)
	})
}

// validSpellHasTargeting reports whether a ValidSA$/ValidSpell$ OR-list
// carries an `IsTargeting` alternative under the Spell base. Both parameters
// spell that shape identically (Flash Photography's
// `ValidSA$ Spell.IsTargeting Valid Permanent.YouCtrl`, Head of the Class's
// `ValidSpell$ Spell.IsTargeting Valid Creature`).
func validSpellHasTargeting(raw string) bool {
	for alt := range strings.SplitSeq(raw, ",") {
		kind, constraint, _ := strings.Cut(strings.TrimSpace(alt), ".")
		if kind == "Spell" && strings.HasPrefix(strings.TrimSpace(constraint), "IsTargeting") {
			return true
		}
	}
	return false
}

// presentGate evaluates one IsPresent spec, counted over PresentZone$
// (default battlefield, countStaticPresent), against PresentCompare$
// (default GE1). The ONE static IsPresent$ gate: staticTimingGate, the
// Continuous gate and costStaticApplies all read it, so an EQ0/GEn threshold
// or a hand/graveyard zone cannot mean different things on different paths.
func (e *Engine) presentGate(sv staticView, spec string) bool {
	n := e.countStaticPresent(sv, spec)
	cmp := sv.ParamStr(cards.PKPresentCompare)
	if cmp == "" {
		cmp = "GE1"
	}
	return comparePresent(n, e.presentCompareFor(cmp, sv.Source, sv.Controller))
}

// staticTimingGate evaluates the static conditions that can decide whether a
// CastWithFlash permission exists before a spell is announced. An unknown
// gate fails closed: granting instant timing without proving the script's
// condition would permit an illegal cast.
func (e *Engine) staticTimingGate(sv staticView) bool {
	if !e.classBandGateHolds(sv.ParamStr(cards.PKClassBand), sv.Source) {
		return false
	}
	if spec, ok := sv.Param(cards.PKIsPresent); ok && !e.presentGate(sv, spec) {
		return false
	}
	if spec, ok := sv.Param(cards.PKIsPresent2); ok && !e.presentGate(sv, spec) {
		return false
	}
	if !e.checkSVarHolds(sv) {
		return false
	}
	switch sv.condition() {
	case condBlank, condPlayerTurn:
		if sv.ParamStr(cards.PKCondition) == "PlayerTurn" && e.G.Active != sv.Controller {
			return false
		}
	case condFerocious:
		found := false
		for _, id := range e.G.Zone(state.ZBattlefield, sv.Controller) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.EffectiveIsCreature() && !o.BestowedAttached() && !o.ReconfiguredAttached() && e.Derived(id).Power >= 4 {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	default:
		return false
	}
	if phase := strings.TrimSpace(sv.ParamStr(cards.PKPhases)); phase != "" && !(strings.Contains(phase, "End of Turn") && e.G.Step == state.StepEnd) {
		return false
	}
	if turn := strings.TrimSpace(sv.ParamStr(cards.PKPlayerTurn)); turn != "" {
		if turn != "Opponent" || e.G.Active == sv.Controller {
			return false
		}
	}
	return true
}

func (e *Engine) countStaticPresent(sv staticView, spec string) int {
	zone, ok := presentZoneFromParam(sv.ParamStr(cards.PKPresentZone))
	if !ok {
		return 0
	}
	sc := e.staticSpecCtx(sv)
	if zone == state.ZBattlefield {
		return e.countPresentCtx(spec, sv.Source, sv.Controller, sc)
	}
	n := 0
	e.forEachObject(func(id state.ObjID) {
		o := e.G.Obj(id)
		if o != nil && o.Zone == zone && e.matchesSpec(spec, id, sc) {
			n++
		}
	})
	return n
}

// presentZoneFromParam maps a Forge PresentZone$ value onto the state.Zone the
// IsPresent$ count family scans. An ABSENT (or Battlefield) value is the
// battlefield -- the default every card without PresentZone$ reads, so the
// empty string is a valid mapping, not an unknown one. An unrecognised value
// reports false and the caller must fail closed (count 0), which is the
// direction countStaticPresent has always taken and the delayed-trigger
// presence gate (rules/trigger_delayed.go) now shares, so a new PresentZone$
// spelling cannot mean two different things at the two count sites.
func presentZoneFromParam(zone string) (state.Zone, bool) {
	switch presentZoneFromParamCodes.Code(string(strings.TrimSpace(zone))) {
	case presentZoneFromParamBattlefield:
		return state.ZBattlefield, true
	case presentZoneFromParamGraveyard:
		return state.ZGraveyard, true
	case presentZoneFromParamExile:
		// IsPresent$ over exile (Ketramose, the New Dawn's
		// `IsPresent$ Card | PresentZone$ Exile | PresentCompare$ LT7`
		// CantAttack,CantBlock static). forEachObject walks every zone of
		// every seat, exile included, so the same scan covers it.
		return state.ZExile, true
	case presentZoneFromParamHand:
		// IsPresent$ over a hand (Kefnet the Mindful's
		// `IsPresent$ Card.YouOwn | PresentZone$ Hand | PresentCompare$ LE6`
		// CantAttack,CantBlock static). forEachObject walks hands too.
		return state.ZHand, true
	case presentZoneFromParamStack:
		// IsPresent$ over the stack (Molten Disaster's kicked-gated AddKeyword$
		// Split second static: IsPresent$ Card.Self+kicked | PresentZone$ Stack
		// on its own stack object). forEachObject walks the stack zone, so the
		// same scan covers it.
		return state.ZStack, true
	default:
		return 0, false
	}
}

// spellMatchesValidSA checks the spell-side subset of Forge's ValidSA grammar.
// Activated-only constraints are not knowable before announcing a spell and
// therefore do not accidentally grant flash timing. id is the card the cast
// offers and staticSource the static's source: the "Spell.Self" form (115
// corpus lines, all on self-granting AlternativeCost statics, Daze the
// most-played) means the affected card itself is the spell -- true exactly
// when the cast card IS the static's source (the card's own S: line, where
// alternativeCosts builds the view with source == id), false for a grant from
// another permanent.
//
// The target-conditional form (`Spell.IsTargeting <spec>`, Flash Photography
// and Timely Ward) is evaluated through effects' ONE `Spell.IsTargeting`
// grammar against the targets argument: the offer passes the potential legal
// targets and CR 601.2e's recheck passes the announced ones. A nil/empty
// target list matches nothing, so an IsTargeting alternative never grants
// unconditional timing. you is the caster the target spec's You clause binds
// (never the granting static's controller). Constraint values beyond Self and
// IsTargeting (XCostLE3, Teamwork, ...) remain unimplemented shapes and fail
// closed.
func (e *Engine) spellMatchesValidSA(f *cards.Face, raw string, id, staticSource state.ObjID, you state.PlayerID, targets []state.Target) bool {
	if strings.TrimSpace(raw) == "" {
		return true
	}
	for alt := range strings.SplitSeq(raw, ",") {
		kind, constraint, _ := strings.Cut(strings.TrimSpace(alt), ".")
		switch spellMatchesValidSACodes.Code(string(kind)) {
		case spellMatchesValidSASpell:
			if constraint == "" {
				return true
			}
			if constraint == "Self" && id == staticSource {
				return true
			}
			if strings.HasPrefix(constraint, "IsTargeting") {
				sc := e.specCtx(staticSource, you)
				sc.AsStack = true
				sc.ProposedTargets = targets
				if e.matchesSpec("Spell."+constraint, id, sc) {
					return true
				}
			}
		case spellMatchesValidSAInstant:
			if constraint == "" && f.IsInstant() {
				return true
			}
		case spellMatchesValidSASorcery:
			if constraint == "" && f.IsSorcery() {
				return true
			}
		}
	}
	return false
}

// spellTimingOK is the one timing predicate for every zone which offers a
// spell cast. CastWithFlash is a permission, not a hand-only property: it
// also applies to Flashback, Harmonize, and command-zone casts.
//
// ActivationPhases$ and its rider qualifiers are consulted here too, so the
// cast window binds to EVERY way the card is cast (hand, may-play, command
// zone, flashback/harmonize/escape, adventure, foretell) rather than to the
// hand walk alone. This is a pure read; the helper never emits.
func (e *Engine) spellTimingOK(p state.PlayerID, id state.ObjID, f *cards.Face, sorcery bool) bool {
	if f == nil {
		return sorcery
	}
	if !e.activationPhasesOK(p, f.SpellAbility()) {
		return false
	}
	return sorcery || (f.IsInstant() || e.hasKeywordH(id, kwhFlash) || mayFlashSacFace(f) || e.castWithFlash(p, id))
}

type presentZoneFromParamCode uint16

const (
	presentZoneFromParamBattlefield presentZoneFromParamCode = iota + 1
	presentZoneFromParamGraveyard
	presentZoneFromParamExile
	presentZoneFromParamHand
	presentZoneFromParamStack
)

var presentZoneFromParamCodes = state.NewStrCodes(
	state.StrEntry[presentZoneFromParamCode]{Key: "", Val: presentZoneFromParamBattlefield},
	state.StrEntry[presentZoneFromParamCode]{Key: "Battlefield", Val: presentZoneFromParamBattlefield},
	state.StrEntry[presentZoneFromParamCode]{Key: "Graveyard", Val: presentZoneFromParamGraveyard},
	state.StrEntry[presentZoneFromParamCode]{Key: "Exile", Val: presentZoneFromParamExile},
	state.StrEntry[presentZoneFromParamCode]{Key: "Hand", Val: presentZoneFromParamHand},
	state.StrEntry[presentZoneFromParamCode]{Key: "Stack", Val: presentZoneFromParamStack},
)

type spellMatchesValidSACode uint16

const (
	spellMatchesValidSASpell spellMatchesValidSACode = iota + 1
	spellMatchesValidSAInstant
	spellMatchesValidSASorcery
)

var spellMatchesValidSACodes = state.NewStrCodes(
	state.StrEntry[spellMatchesValidSACode]{Key: "Spell", Val: spellMatchesValidSASpell},
	state.StrEntry[spellMatchesValidSACode]{Key: "Instant", Val: spellMatchesValidSAInstant},
	state.StrEntry[spellMatchesValidSACode]{Key: "Sorcery", Val: spellMatchesValidSASorcery},
)
