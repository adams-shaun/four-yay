// Static play restrictions and cost modifiers: the six S: modes besides
// stat:Continuous (layers.go's own concern). These change what is legal and
// what things cost rather than what a permanent's characteristics are, so
// they hook into legalActions and ParseCost rather than the layer system.
package rules

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// staticView is one S: line together with where it came from, so the filter
// predicates that are relative to a source (Self, Other) and the ones
// relative to a controller (YouCtrl, OppCtrl) resolve correctly.
type staticView struct {
	Source     state.ObjID
	Controller state.PlayerID
	Params     map[string]string
	// PS is the printed static's compiled parameter set (nil for a view
	// built from a map that has none): the view's ParamStr/Param/HasParam
	// read through it when it is bound to Params.
	PS *cards.ParamSet
	// SVars is the SVar table of the face that carries this static. For a
	// plain permanent it is the top face's table (unchanged); for a card
	// merged beneath a mutated pile's top (CR 702.140d) it is the
	// under-card's own table, so a static whose Amount$/CheckSVar$ names an
	// SVar resolves against the face that wrote it rather than the pile top's.
	// nil means "read the source object's top face" -- every construction that
	// predates this field keeps today's behaviour exactly.
	SVars map[string]string
	// ChosenNumber is the Effect's SetChosenNumber$ binding an Effect-delivered
	// registry entry carries (state.ContinuousEffect.ChosenNumber, frozen at
	// creation): an Amount$ body reading Count$ChosenNumber (Kaza, Roil
	// Chaser's wizard count, Maelstrom Muse's power) resolves against it
	// through modAmountX's Ctx instead of the always-zero read an unbound
	// context gives. Printed statics never carry the head and keep the zero.
	ChosenNumber int32
	// chosenNumberBound marks a view whose ChosenNumber IS a real
	// SetChosenNumber$ binding (the delivered-static route: every
	// CantBlockUnless registry view carries one, frozen at registration).
	// It is the Count$ChosenNumber head's verdict in the block charge
	// resolver's Ctx, the same flag rules' seedEffectReplCtx sets. Printed
	// statics keep it false.
	chosenNumberBound bool
	// Remembered is the captured object set carried by an Effect-delivered
	// static. It binds Card.IsRemembered in the same shared spec context as
	// restriction registrations; printed statics leave it nil.
	Remembered []state.ObjID
}

// costStaticViews is one ordered snapshot of cost-modifier membership. The
// three slices preserve each mode's independent application order while the
// collector walks the game's zones only once. It contains no evaluated
// applicability, amount, target, X, condition, or final cost.
type costStaticViews struct {
	raise    []staticView
	reduce   []staticView
	set      []staticView
	optional []staticView
	// validTarget: some raise/reduce/set member's predicate reads the chosen
	// targets -- either a ValidTarget$ or a target-conditional ValidSpell$
	// (`Spell.IsTargeting <spec>`, Head of the Class) -- the one condition under
	// which a composition must be retried with the potential targets (see
	// offerCastableUsing's potential-target retry).
	validTarget bool
}

// costStaticSource lazily owns one call-scoped membership snapshot. It is
// deliberately not stored on Engine: a legal-actions pass may reuse it, but
// a later pass or payment-side recomputation must observe the current board,
// including test fixtures that mutate setup without emitting events. (Inside
// a legal-actions walk the collection itself is served from the walk-scoped
// fused scan, rules/walkcache.go, which a later pass never reads.)
type costStaticSource struct {
	e     *Engine
	views costStaticViews
	ready bool
}

func (s *costStaticSource) get() costStaticViews {
	if !s.ready {
		s.views = s.e.collectCostStatics()
		s.ready = true
	}
	return s.views
}

// actionStaticViews contains ordered membership only, never evaluated
// restrictions, grants or affordability. A legalActions pass owns its source
// locally; later offers and payment/activation callers collect afresh.
type actionStaticViews struct {
	cantCast     []staticView
	cantActivate []staticView
	continuous   []staticView
}

type actionStaticSource struct {
	e     *Engine
	views actionStaticViews
	ready bool
	// addAbility is views.continuous' subsequence carrying a non-blank
	// AddAbility$, built on first use (addAbilityContinuous).
	addAbility      []staticView
	addAbilityReady bool
}

// addAbilityContinuous returns, in order, the Continuous statics of get()
// whose AddAbility$ is non-blank: the only ones the mana walk's per-object
// AddAbility$ scan (appendAvailableManaAbilitiesGate) does not skip at its
// first test, filtered once per walk instead of once per object.
func (s *actionStaticSource) addAbilityContinuous() []staticView {
	if !s.addAbilityReady {
		for _, sv := range s.get().continuous {
			if strings.TrimSpace(sv.Params["AddAbility"]) != "" {
				s.addAbility = append(s.addAbility, sv)
			}
		}
		s.addAbilityReady = true
	}
	return s.addAbility
}

func (s *actionStaticSource) get() actionStaticViews {
	if !s.ready {
		s.views = s.e.collectActionStatics()
		s.ready = true
	}
	return s.views
}

// collectActionStatics mirrors activeStatics' active-face-only battlefield
// walk. In particular it must not inherit staticEffects' alternate Room face
// expansion or collectCostStatics' other zones. Each mode keeps its original
// seat, zone and parsed-static order while sharing a single membership walk.
func (e *Engine) collectActionStatics() actionStaticViews {
	if v, ok := e.boardStaticsWalk(); ok {
		return v.action
	}
	return e.scanActionStatics()
}

func (e *Engine) scanActionStatics() actionStaticViews {
	var out actionStaticViews
	for pi, p := range e.G.AliveFrom(0) {
		// Continuous statics are zone-scoped by their EffectZone$, so the
		// membership walk mirrors collectCostStatics': every zone a source
		// can sit in, one fixed order, the shared stack walked once under the
		// first alive seat. The CantBeCast/CantBeActivated modes keep their
		// battlefield-only membership (their readers gate EffectZone$
		// downstream themselves and no corpus shape takes them off the
		// battlefield).
		for _, z := range staticSourceZones {
			if z == state.ZStack && pi > 0 {
				continue
			}
			for _, id := range e.staticSourceIDs(p, z) {
				o := e.G.Obj(id)
				if o == nil || o.Face() == nil || offBattlefieldStaticsInert(z, o) {
					continue
				}
				// CR 708.8: a face-down permanent's printed statics do not
				// exist while it is face down -- the same gate
				// scanActiveStatics runs. Without it a legal-actions pass
				// saw a manifested Citanul Hierophants still granting
				// "{T}: Add {G}" (the offer's cached snapshot) while the
				// activation's fresh activeStatics walk did not, so the
				// offered "Activate ... for mana" was a silent no-op
				// re-offered forever (cardfuzz batch9 line 1).
				if e.faceDownPrintedHides(o) {
					continue
				}
				for si, sn := 0, o.PileStaticCount(); si < sn; si++ {
					pst, ok := o.PileStaticAt(si)
					if !ok {
						continue
					}
					st := pst.Static
					var dst *[]staticView
					switch st.Mode {
					case "CantBeCast":
						if z != state.ZBattlefield {
							continue
						}
						dst = &out.cantCast
					case "CantBeActivated":
						if z != state.ZBattlefield {
							continue
						}
						dst = &out.cantActivate
					case "Continuous":
						// The EffectZone$ gate (the default is the battlefield,
						// so every battlefield Continuous static keeps today's
						// admission exactly): a static naming another zone is
						// collected from THAT zone here and denied from the
						// battlefield, the same gate staticEffects runs.
						if !effectZoneOK(st.Params["EffectZone"], o.Zone) {
							continue
						}
						dst = &out.continuous
					default:
						continue
					}
					*dst = append(*dst, staticView{Source: id, Controller: o.Controller, Params: st.Params, PS: st.ParamSetOf(), SVars: pst.Face.SVars})
				}
			}
		}
	}
	return out
}

// activeStatics collects every S:Mode$ <mode> line from a permanent on the
// battlefield. The order is deterministic: AliveFrom(0) walks seats in fixed
// APNAP order, each seat's battlefield zone is a slice built by ordinary
// append (never a map), and each object's Statics is the slice order its
// card script parsed in — nothing here ever ranges a map, so cost adjustment
// and the resulting option list are stable run to run, which is what
// TestActiveStaticsIsDeterministicallyOrdered checks for.
func (e *Engine) activeStatics(mode string) []staticView {
	// Board-only, so a legal-actions walk serves it from the walk cache
	// (rules/walkcache.go); outside a walk it is scanned every call.
	return e.activeStaticsCached(mode)
}

func (e *Engine) scanActiveStatics(mode string, out []staticView) []staticView {
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			o := e.G.Obj(id)
			if o == nil {
				continue
			}
			f := o.Face()
			if f == nil {
				continue
			}
			if e.faceDownPrintedHides(o) {
				// CR 708.8: a face-down permanent's printed statics do not
				// exist while it is face down (the shared gate in layers.go).
				continue
			}
			if o.PhasedOut {
				// CR 702.25b/d: a phased-out permanent is treated as though it
				// does not exist, so its statics do not function (and it is not
				// an affected permanent either).
				continue
			}
			for si, sn := 0, o.PileStaticCount(); si < sn; si++ {
				pst, ok := o.PileStaticAt(si)
				if !ok {
					continue
				}
				st := pst.Static
				if st.Mode == mode {
					// The battlefield-only walk honours each static's own
					// EffectZone$: a static whose EffectZone$ excludes the
					// battlefield (Anger's graveyard-scoped haste grant) must
					// not apply while its source is on the battlefield, on
					// every mode's consumer. The default and the explicit
					// Battlefield/All values keep today's admission exactly;
					// a static naming a hidden zone is collected from there by
					// staticEffects/collectActionStatics/collectCostStatics
					// instead.
					if !effectZoneOK(st.Params["EffectZone"], o.Zone) {
						continue
					}
					out = append(out, staticView{Source: id, Controller: o.Controller, Params: st.Params, PS: st.ParamSetOf(), SVars: pst.Face.SVars})
				}
			}
		}
	}
	return out
}

// countersRemainApplies reports whether the departing permanent itself has
// an active CountersRemain static. Static lines are read through the canonical
// activeStatics walk so EffectZone, merged-face and face-down rules stay
// consistent with the other static consumers.
func (e *Engine) countersRemainApplies(id state.ObjID) bool {
	for _, sv := range e.activeStatics("CountersRemain") {
		spec := sv.Params["ValidCard"]
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
		spec := strings.TrimSpace(sv.Params["ValidPlayer"])
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
		if strings.EqualFold(strings.TrimSpace(sv.Params["Optional"]), "True") {
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
	raw := strings.TrimSpace(sv.Params["Num"])
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
	v, ok := effects.EvalCountOK(e, &effects.Ctx{Source: sv.Source, Controller: sv.Controller, SVars: sv.SVars}, body)
	if !ok || v < 0 {
		return 0, false
	}
	return v, true
}

// actorMatches implements the Caster$/Activator$ parameter, which scopes a
// restriction to whose action it is. A restriction with no such parameter
// applies regardless of actor.
func (e *Engine) actorMatches(sv staticView, key string, actor state.PlayerID) bool {
	spec, ok := sv.Params[key]
	if !ok {
		return true
	}
	return effects.MatchesPlayerSpecCtx(e.G, spec, actor, sv.Controller, e.playerSpecCtx(sv.Source))
}

// specCtx builds the SpecContext a per-source "ValidCard$"/spec match is
// resolved against, with a resolver that answers "Chosen" (the source object's
// ChosenNumber) and any SVar name on the source's face via EvalCount. This is
// what a numeric-RHS restriction actually needs: Sanctum Prelate's
// "cmcEQChosen" (read the number chosen as it entered) and Chalice of the
// Void's "cmcEQY" (Y an SVar over the source's charge counters) both resolve
// through here. Without the resolver those terms would silently never match
// (numericPred's "recognised shape, unresolvable RHS never matches") and the
// restriction would be dead. The resolver closes over source/you -- both
// plain scalars -- so it is deterministic and Clone-safe.
func (e *Engine) specCtx(source state.ObjID, you state.PlayerID) effects.SpecContext {
	return e.specCtxSVars(source, you, nil)
}

// playerSpecCtx is the player-side sibling of specCtx: it carries the same
// layer-3 rename and layer-4 derived-type tables into the player filter, so a
// Player.controlsCreature / Player.controlsPermanent qualifier evaluates its
// object spec against the derived characteristics every ordinary filter site
// already reads, rather than the printed face alone. A FIELD READ, never a
// call into the layer walk: the tables are the snapshots active() refreshes
// after each emitted event, exactly the values specCtx binds.
func (e *Engine) playerSpecCtx(source state.ObjID) effects.PlayerSpecCtx {
	return effects.PlayerSpecCtx{
		Source:         source,
		EffectiveNames: e.renames,
		DerivedTypes:   e.layer4Types,
	}
}

// matchesSpec evaluates a live-object filter with its current derived
// characteristics. The keyword slice is a value snapshot borrowed from the
// allocation-free Derived path; it is never used for LKI or hypothetical
// token objects, which go through MatchesObjectCtx and retain printed/counter
// semantics. Keeping this seam in rules prevents effects from depending on
// the layer owner while making every live-object rules query layer-aware.
func (e *Engine) matchesSpec(spec string, id state.ObjID, sc effects.SpecContext) bool {
	// During the layer scan Derived is already being built; consulting it
	// again would recurse through active(). The scan's own matchesWithChars
	// supplies its keywords-so-far snapshot directly.
	if e.activeDepth == 0 {
		// specReadsDerived: a spec that cannot read the bound keyword list or
		// P/T skips the Derived walk the bind costs (specderived.go).
		if o := e.G.Obj(id); o != nil && (specDerivedVerify || specReadsDerived(spec)) {
			d := e.Derived(id)
			sc.ExtraKeywords = d.Keywords
			// The numeric power/basePower predicates read the same derived
			// values the rest of the engine does: the candidate's current P/T
			// and its base P/T through layer 7b (CR 613.4). Binding both here
			// -- the one seam every rules-side filter match goes through --
			// keeps `powerGTbasePower` from comparing two printed faces:
			// a +1/+1 counter or a 7c pump moves DerivedPower while
			// BasePower stays put.
			sc.DerivedPower, sc.DerivedToughness, sc.HasDerivedPT = d.Power, d.Toughness, true
			sc.BasePower, sc.BaseToughness, sc.HasBasePT = d.BasePower, d.BaseToughness, true
		}
		// The IsGoaded predicate's static route (staticgoad1): a spec that
		// consults IsGoaded binds the live static-goad table, so EVERY
		// rules-side read (trigger ValidCard$/ValidSource$, a CantBlock
		// static's ValidCard$, Count$Valid through the rules seam) sees a
		// printed or granted Goad$ static, not the event-backed list alone.
		// The goadProbe guard keeps a goad line's own Affected$ match from
		// re-deriving the set (an IsGoaded-conditioned Affected$ would loop);
		// mid-layer-scan specs keep the event-backed read, the same stand-down
		// the ExtraKeywords consult above takes. INLINED, not routed through
		// bindStaticGoads: the helper call moves this hot matcher past its
		// inlining budget and heap-allocates the context on every candidate
		// (TestLegalActionsReusesActionStaticMembership's pin).
		if e.goadProbe == 0 && strings.Contains(spec, "IsGoaded") {
			sc.StaticGoads = e.staticallyGoaded()
		}
		if specDerivedVerify && !specReadsDerived(spec) {
			return e.verifySpecDerivedSkip(spec, id, sc)
		}
	}
	return effects.MatchesSpecCtx(e.G, spec, id, sc)
}

// verifySpecDerivedSkip is matchesSpec's verify-mode tail for a spec
// specReadsDerived clears: sc carries the Derived bind, and the match must
// agree with the unbound one production takes.
func (e *Engine) verifySpecDerivedSkip(spec string, id state.ObjID, sc effects.SpecContext) bool {
	bound := effects.MatchesSpecCtx(e.G, spec, id, sc)
	sc.ExtraKeywords = nil
	sc.DerivedPower, sc.DerivedToughness, sc.HasDerivedPT = 0, 0, false
	sc.BasePower, sc.BaseToughness, sc.HasBasePT = 0, 0, false
	if unbound := effects.MatchesSpecCtx(e.G, spec, id, sc); unbound != bound {
		panic(fmt.Sprintf("rules: specReadsDerived cleared %q but the derived bind changed obj %d's match (%v bound, %v unbound)", spec, id, bound, unbound))
	}
	return bound
}

// specCtxSVars is specCtx with an explicit SVar table: a static carried by a
// card merged beneath a mutated pile's top resolves its Chosen*/SVar* terms
// against that under-card's own table. A nil svars falls back to the source
// object's top face, so every pre-existing caller is unchanged.
func (e *Engine) specCtxSVars(source state.ObjID, you state.PlayerID, svars map[string]string) effects.SpecContext {
	var predicates *effects.PredicatePrograms
	if e.compiledText != nil {
		predicates = e.compiledText.predicates
	}
	sc := effects.SpecContext{
		You:               you,
		Source:            source,
		PredicatePrograms: predicates,
		// setname.go: the layer-3 rename set, so a name filter rules
		// evaluates agrees with the layer walk instead of the printed face.
		// setname.go's layer-3 rename table. A FIELD READ, never a call: a
		// call here breaks this constructor's inlining and heap-allocates the
		// Resolve closure on every hot-path construction.
		EffectiveNames: e.renames,
		// layer4types.go's layer-4 derived type table. The same field-read
		// discipline as EffectiveNames above: it makes the ordinary filter
		// grammar (target offer, cost site, Count$Valid, CantTarget) see a
		// type a continuous effect granted.
		DerivedTypes: e.layer4Types,
		Resolve: func(name string) (int32, bool) {
			o := e.G.Obj(source)
			if o == nil {
				return 0, false
			}
			if name == "Chosen" {
				return o.ChosenNumber, true
			}
			table := svars
			if table == nil {
				f := o.Face()
				if f == nil {
					return 0, false
				}
				table = f.SVars
			}
			if body, ok := table[name]; ok {
				return effects.EvalCount(e, &effects.Ctx{Source: source, Controller: you, SVars: table}, body), true
			}
			return 0, false
		},
	}
	// A recurring Effect's matcher reads the registration's captured objects,
	// not the creating card's (possibly unrelated) event-backed memory. The
	// override exists only on the read-only observer for that registration.
	if e.effectMatchOverride && source == e.effectMatchSource {
		sc.Remembered = e.effectMatchRemembered
	}
	return sc
}

// staticSpecCtx is the SpecContext a staticView's spec match resolves against:
// its own SVar table when the view carries one (an under-card static), else the
// source object's top face.
func (e *Engine) staticSpecCtx(sv staticView) effects.SpecContext {
	return e.specCtxSVars(sv.Source, sv.Controller, sv.SVars)
}

// assignmentStaticSpecCtx binds captured Effect memory only for the narrow
// assignment-static path. Keeping staticSpecCtx's ordinary hot-path shape
// preserves allocation-free matching for unrelated statics.
func (e *Engine) assignmentStaticSpecCtx(sv staticView) effects.SpecContext {
	sc := e.staticSpecCtx(sv)
	for _, id := range sv.Remembered {
		sc.Remembered = append(sc.Remembered, state.Target{Obj: id})
	}
	return sc
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
		spec := sv.Params["ValidCard"]
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
		if st.Mode != "CantBeCast" || !effectZoneOK(st.Params["EffectZone"], o.Zone) {
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
	if !e.classBandGateHolds(sv.Params, sv.Source) {
		return false
	}
	if az, ok := sv.Param(cards.PKAffectedZone); ok {
		o := e.G.Obj(target)
		if o == nil || !affectedZoneOK(az, o.Zone) {
			return false
		}
	}
	switch strings.TrimSpace(sv.ParamStr(cards.PKCondition)) {
	case "":
		return true
	case "PlayerTurn":
		return e.G.Active == sv.Controller
	case "NotPlayerTurn":
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
		if !e.matchesSpec(sv.Params["ValidCard"], id, e.staticSpecCtx(sv)) {
			continue
		}
		if activatedMatchesValidSA(ab, sv.Params["ValidSA"]) {
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
			if !isManaAbilityAPI(ab.API) {
				return true
			}
			// else: mana abilities are expressly spared; try the next alt
		case constraint == "ManaAbility" || strings.HasPrefix(constraint, "ManaAbility<"):
			if !isManaAbilityAPI(ab.API) {
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
				produced := strings.TrimSpace(ab.Params["Produced"])
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
	return e.costModifiers(p, id, spellScope("")).apply(e.faceCost(o.Face()))
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
		if az, ok := sv.Params["EffectZone"]; ok {
			src := e.G.Obj(sv.Source)
			if src == nil || !affectedZoneOK(az, src.Zone) {
				continue
			}
		}
		if !e.spellMatchesValidSA(o.Face(), sv.Params["ValidSA"], id, sv.Source, p, targets) {
			continue
		}
		if e.matchesSpec(sv.Params["ValidCard"], id, e.staticSpecCtx(sv)) {
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
		if validSpellHasTargeting(sv.Params["ValidSA"]) {
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

// presentGate evaluates one IsPresent spec against PresentCompare (default
// GE1); staticTimingGate fails closed when either present gate does not hold.
func (e *Engine) presentGate(sv staticView, spec string) bool {
	n := e.countStaticPresent(sv, spec)
	cmp := sv.Params["PresentCompare"]
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
	if !e.classBandGateHolds(sv.Params, sv.Source) {
		return false
	}
	if spec, ok := sv.Params["IsPresent"]; ok && !e.presentGate(sv, spec) {
		return false
	}
	if spec, ok := sv.Params["IsPresent2"]; ok && !e.presentGate(sv, spec) {
		return false
	}
	if name, ok := sv.Params["CheckSVar"]; ok {
		o := e.G.Obj(sv.Source)
		if o == nil || o.Face() == nil {
			return false
		}
		svars := sv.SVars
		if svars == nil {
			svars = o.Face().SVars
		}
		body, ok := svars[name]
		if !ok {
			return false
		}
		cmp := sv.Params["SVarCompare"]
		if cmp == "" || !comparePresent(int(effects.EvalCount(e, &effects.Ctx{Source: sv.Source, Controller: sv.Controller, SVars: svars}, body)), cmp) {
			return false
		}
	}
	switch strings.TrimSpace(sv.Params["Condition"]) {
	case "", "PlayerTurn":
		if sv.Params["Condition"] == "PlayerTurn" && e.G.Active != sv.Controller {
			return false
		}
	case "Ferocious":
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
	if phase := strings.TrimSpace(sv.Params["Phases"]); phase != "" && !(strings.Contains(phase, "End of Turn") && e.G.Step == state.StepEnd) {
		return false
	}
	if turn := strings.TrimSpace(sv.Params["PlayerTurn"]); turn != "" {
		switch turn {
		case "Opponent":
			if e.G.Active == sv.Controller {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (e *Engine) countStaticPresent(sv staticView, spec string) int {
	zone, ok := presentZoneFromParam(sv.Params["PresentZone"])
	if !ok {
		return 0
	}
	if zone == state.ZBattlefield {
		return e.countPresent(spec, sv.Source, sv.Controller)
	}
	n := 0
	e.forEachObject(func(id state.ObjID) {
		o := e.G.Obj(id)
		if o != nil && o.Zone == zone && e.matchesSpec(spec, id, e.staticSpecCtx(sv)) {
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
	switch strings.TrimSpace(zone) {
	case "", "Battlefield":
		return state.ZBattlefield, true
	case "Graveyard":
		return state.ZGraveyard, true
	case "Exile":
		// IsPresent$ over exile (Ketramose, the New Dawn's
		// `IsPresent$ Card | PresentZone$ Exile | PresentCompare$ LT7`
		// CantAttack,CantBlock static). forEachObject walks every zone of
		// every seat, exile included, so the same scan covers it.
		return state.ZExile, true
	case "Hand":
		// IsPresent$ over a hand (Kefnet the Mindful's
		// `IsPresent$ Card.YouOwn | PresentZone$ Hand | PresentCompare$ LE6`
		// CantAttack,CantBlock static). forEachObject walks hands too.
		return state.ZHand, true
	case "Stack":
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
		switch kind {
		case "Spell":
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
		case "Instant":
			if constraint == "" && f.IsInstant() {
				return true
			}
		case "Sorcery":
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

// altCostView is one alternative-cost entry: the parsed cost plus the
// granting static's own riders the cast flow needs (the Announce$ X value —
// an alternative cost whose X the caster announces at CR 601.2b before the
// exile filter's cmcEQX resolves, the Shoal cycle) and the static's source,
// whose face SVar table resolves the announced value's SVar.
type altCostView struct {
	cost     Cost
	announce string
	src      state.ObjID
}

// alternativeCosts lists extra ways to cast id, each becoming its own
// "cast" option in legalActions so the client can present the choice
// without knowing any rules. Two sources: another permanent's static
// granting the alternative (activeStatics, battlefield-only), and a static
// the card carries on itself, which activeStatics alone would never see
// while the card is still in hand.
//
// p is the casting player: the ValidPlayer$ rider on an AlternativeCost
// static scopes WHO may take the alternative (Deadly Rollick/Deflecting
// Swat's "ValidPlayer$ You"), evaluated against the static's own controller
// so a grant from another permanent's static resolves You/Opponent relative
// to the granter, exactly like every other static filter predicate.
func (e *Engine) alternativeCosts(p state.PlayerID, id state.ObjID) []altCostView {
	var out []altCostView
	for _, sv := range e.activeStatics("AlternativeCost") {
		if !e.matchesSpec(sv.Params["ValidCard"], id, e.staticSpecCtx(sv)) {
			continue
		}
		if !e.alternativeCostScopeOK(sv.Params, id, sv.Source, p, sv.Controller) {
			continue
		}
		cost, ok := e.altCostParse(id, sv.Params["Cost"])
		if !ok {
			continue
		}
		out = append(out, altCostView{cost: cost,
			announce: strings.TrimSpace(sv.Params["Announce"]), src: sv.Source})
	}
	// The Effect-delivered AlternativeCost statics (task
	// param:api:Effect.ForgetOnCast, Marshland Bloodcaster): registry entries
	// effEffect registered, read through the SAME reader logic as the printed
	// route above over the same e.active() source collectCostStatics' sibling
	// walk feeds the Raise/Reduce/Set modes, so the two delivery routes
	// cannot disagree about what applies or when it expires.
	ces := e.active()
	for i := range ces {
		ce := &ces[i]
		if ce.CostStaticMode != "AlternativeCost" {
			continue
		}
		sv := staticView{Source: ce.Source, Controller: ce.Controller,
			Params: ce.CostStaticParams, ChosenNumber: ce.ChosenNumber, chosenNumberBound: true}
		// ValidCard$ is presence-gated here exactly as costStaticApplies gates
		// it: an absent spec restricts nothing (the printed face-static walk
		// below never consults one at all -- Marshland's AlternativeCost body
		// names none). The bare matchesObjectText read of an empty spec
		// matches NOTHING, so an unconditional check would silently deny
		// every ValidCard$-less grant.
		if spec, ok := sv.Params["ValidCard"]; ok && spec != "" &&
			!e.matchesSpec(spec, id, e.staticSpecCtx(sv)) {
			continue
		}
		if !e.alternativeCostScopeOK(sv.Params, id, sv.Source, p, sv.Controller) {
			continue
		}
		cost, ok := e.altCostParse(id, sv.Params["Cost"])
		if !ok {
			continue
		}
		out = append(out, altCostView{cost: cost,
			announce: strings.TrimSpace(sv.Params["Announce"]), src: sv.Source})
	}
	if o := e.G.Obj(id); o != nil {
		if f := o.Face(); f != nil {
			for _, st := range f.Statics {
				if st.Mode != "AlternativeCost" {
					continue
				}
				if !e.alternativeCostScopeOK(st.Params, id, id, p, o.Controller) {
					continue
				}
				cost, ok := e.altCostParse(id, st.Params["Cost"])
				if !ok {
					continue
				}
				out = append(out, altCostView{cost: cost,
					announce: strings.TrimSpace(st.Params["Announce"]), src: id})
			}
		}
	}
	// A MayPlay static's MayPlayAltManaCost$ (Darksteel Monolith) is the same
	// "pay THIS instead of the mana cost" shape delivered by the may-play
	// family; the family root carries its own gates and limit.
	for _, c := range e.mayPlayAltCosts(p, id) {
		out = append(out, altCostView{cost: c})
	}
	return out
}

// altCostParse prices one alternative-cost token (an AlternativeCost
// static's Cost$, either registration route -- a printed S: static or an
// Effect-delivered registry entry -- or a MayPlay static's
// MayPlayAltManaCost$) for the cast of id. The ONE dynamic token the corpus
// carries, ConvertedManaCost (Marshland Bloodcaster's "pay life equal to
// that spell's mana value" Cost$ and the 12 may-play statics'
// MayPlayAltManaCost$), substitutes the cast card's own mana value, the
// same read the Play route's pricePlayCost makes. ok=false is the fail-
// closed withholding: a token that cannot be resolved (no card face) or
// parses into an unmodelled part is never offered -- an unpriceable cost
// must not exist as an option, because ParseCost's malformed-token fallback
// would otherwise price it one generic mana (the may-play route's
// documented direction; the printed AlternativeCost statics all parse
// today, measured over the corpus's 150 Mode$ AlternativeCost lines).
func (e *Engine) altCostParse(id state.ObjID, raw string) (Cost, bool) {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return Cost{}, false
	}
	c := e.parseCost(convertedManaCostToken.ReplaceAllString(raw,
		strconv.FormatInt(int64(o.Face().ManaValue()), 10)))
	if len(c.Unknown) > 0 {
		return Cost{}, false
	}
	return c, true
}

// altCostXCandidates returns the ASCENDING distinct mana values at which at
// least one exilable card in the caster's hand still matches the view's
// Exile parts — the candidate set the Announce$ X ask offers (Blazing/Disrupting
// Shoal's "exile a red/blue card with mana value X"): the spec's cmcEQX
// predicate is bound to each candidate value through SpecContext.Resolve,
// the same closure mechanism a Chosen* predicate resolves through, so the
// filter never sees an unannounced X. An empty hand or no matching card at
// any value yields an empty set (the offer gate and the xAsk arm both
// withhold on that).
func (e *Engine) altCostXCandidates(p state.PlayerID, id state.ObjID, alt altCostView) []int32 {
	vals := []int32{}
	seen := map[int32]bool{}
	for _, part := range alt.cost.Exile {
		zone := part.Zone
		if zone == 0 {
			zone = state.ZHand
		}
		for _, oid := range e.G.Zone(zone, p) {
			o := e.G.Obj(oid)
			if o == nil || o.Face() == nil {
				continue
			}
			v := o.Face().ManaValue()
			if seen[v] {
				continue
			}
			sc := e.withNames(effects.SpecContext{You: p, Source: alt.src, Resolve: func(name string) (int32, bool) {
				if name == alt.announce {
					return v, true
				}
				return 0, false
			}})
			if e.matchesSpec(part.Spec, oid, sc) {
				seen[v] = true
				vals = append(vals, v)
			}
		}
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	return vals
}

// alternativeCostScopeOK reads an AlternativeCost static's scope riders:
// ValidSA$ (which cast the alternative prices — Daze's, the Force cycle's and
// the Flare cycle's "Spell.Self", the commander free-cast's bare "Spell") and
// EffectZone$ (the zone the static's source must sit in — the self-carried
// free-cast statics name "All" so the grant reaches the hand), ValidPlayer$
// (the casting player, relative to the static's controller — Deadly
// Rollick/Deflecting Swat) and IsPresent$ (an existence precondition over
// the battlefield, the shared presentGate with PresentCompare$ defaulting to
// GE1 — the commander-protection cycle). An absent rider is vacuously true;
// a ValidSA$ value whose Spell constraint this build cannot evaluate denies,
// the same fail-closed direction ValidSpell$ takes — a wrongly-granted free
// cast is an illegal game action, a wrongly-withheld one merely an option
// lost.
func (e *Engine) alternativeCostScopeOK(params map[string]string, id, srcID state.ObjID, caster, controller state.PlayerID) bool {
	if !e.classBandGateHolds(params, srcID) {
		return false
	}
	if vp := strings.TrimSpace(params["ValidPlayer"]); vp != "" && !effects.MatchesPlayerSpec(e.G, vp, caster, controller) {
		return false
	}
	if ip := strings.TrimSpace(params["IsPresent"]); ip != "" {
		view := staticView{Source: srcID, Controller: controller, Params: params}
		if !e.presentGate(view, ip) {
			return false
		}
	}
	if vs := strings.TrimSpace(params["ValidSA"]); vs != "" {
		ok := false
		for alt := range strings.SplitSeq(vs, ",") {
			alt = strings.TrimSpace(alt)
			kind, constraint := alt, ""
			if i := strings.IndexByte(alt, '.'); i >= 0 {
				kind, constraint = alt[:i], alt[i+1:]
			}
			if kind != "Spell" {
				// An Activated/Static kind scopes an ability or an unmodelled
				// casting option; this list prices a spell cast only.
				continue
			}
			switch strings.TrimSpace(constraint) {
			case "":
				ok = true // bare Spell: any cast
			case "Self":
				if id == srcID {
					ok = true // the card's own cast (Daze, the Flares)
				}
			}
			// An unevaluable Spell constraint (Spell.Samurai, ...): deny — a
			// free cast wrongly granted is an illegal action.
		}
		if !ok {
			return false
		}
	}
	if ez := strings.TrimSpace(params["EffectZone"]); ez != "" {
		if src := e.G.Obj(srcID); src != nil && !effectZoneOK(ez, src.Zone) {
			return false
		}
	}
	// CheckSVar$ / CheckSecondSVar$ (Mogg Salvage's two-condition
	// alternative cost): Forge's StaticAbility.checkConditions — the value
	// of each named SVar must satisfy its compare, the DEFAULT being GE1 for
	// both (X = Count$Valid Island.OppCtrl, Y = Count$Valid
	// Mountain.YouCtrl: opponent controls an Island AND you control a
	// Mountain). The gate evaluates against the static's SOURCE face's SVar
	// table, the same precedence sVarGateOK applies to an ability's own
	// CheckSVar$. An unresolvable body fails OPEN — the documented
	// conditionMet convention — so a gate this build cannot evaluate never
	// withholds the alternative by itself.
	ctx := &effects.Ctx{Source: srcID, Controller: controller}
	if o := e.G.Obj(srcID); o != nil && o.Face() != nil {
		ctx.SVars = o.Face().SVars
	}
	if ck := strings.TrimSpace(params["CheckSVar"]); ck != "" {
		if holds, evaluated := effects.CheckSVarHolds(e, ctx, ck, params["SVarCompare"]); evaluated && !holds {
			return false
		}
	}
	if ck := strings.TrimSpace(params["CheckSecondSVar"]); ck != "" {
		if holds, evaluated := effects.CheckSVarHolds(e, ctx, ck, params["SecondSVarCompare"]); evaluated && !holds {
			return false
		}
	}
	return true
}

// onlyFirstSpellUsed reports whether a ReduceCost static carrying
// OnlyFirstSpell$ (Conduit of Ruin: "The first creature spell you cast each
// turn costs {2} less") has already spent its this-turn application: a covered
// spell was cast by the payer earlier in the turn. The answer is a log walk
// over PutOnStack events back to the last TurnChange (cast.go's
// spellsCastThisTurn derivation), so a replay agrees by construction, and it
// counts the ValidCard$-covered casts -- the oracle's "first creature spell"
// is first among the covered kind, not first among all spells.
//
// Every cost-static evaluation site runs BEFORE the cast's own PutOnStack
// exists (the offer walk, beginCast's modifier snapshot, and the
// target-announcement recompute all precede pushCast -- CR 601.2f's
// modifiers are computed into pc.mods and manaToPay only reads them), so the
// in-flight cast is never in the log while it is being priced. The ev.Obj ==
// id exclusion is defensive against a future evaluation site past the push:
// the cast being priced must not count as its own "previous cast".
//
// A cast whose object no longer carries a face (or whose face the spec cannot
// re-evaluate) is not counted -- the missing-match direction for a USED
// tracking, which widens the discount by at most one cast on a board this
// build cannot reconstruct, never withholds it.
func (e *Engine) firstForetellUsed(p state.PlayerID) bool {
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind != events.MoveZone || ev.To != state.ZExile ||
			ev.Counter != "exiled_with_face_down" || i == 0 {
			continue
		}
		o := e.G.Obj(ev.Obj)
		if o == nil || o.Owner != p {
			continue
		}
		prev := e.L.Events[i-1]
		if prev.Kind == events.CastInfo &&
			events.FlagsFrom(prev.Counter)&state.FlagForetold != 0 {
			return true
		}
	}
	return false
}

func (e *Engine) onlyFirstSpellUsed(sv staticView, p state.PlayerID, id state.ObjID) bool {
	spec := strings.TrimSpace(sv.Params["ValidCard"])
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind != events.PutOnStack || ev.Player != p || ev.Obj == id {
			continue
		}
		if spec == "" {
			return true
		}
		if o := e.G.Obj(ev.Obj); o != nil && o.Face() != nil &&
			e.matchesSpec(spec, ev.Obj, e.staticSpecCtx(sv)) {
			return true
		}
	}
	return false
}

// hiddenKeywordFlags is the parsed meaning of one derived keyword line that
// carries an English combat-restriction/requirement sentence. Forge delivers
// these three the same way -- as keyword TEXT, sometimes under a
// HiddenKeywords$ Animate parameter, sometimes under a Pump/PumpAll KW$ -- so
// ONE reader every rules consumer calls is what keeps the spellings from
// drifting apart (a new equivalent spelling is then one switch arm).
type hiddenKeywordFlags struct {
	cantAttack bool
	cantBlock  bool
	mustBlock  bool
}

// parseHiddenKeyword reads one derived keyword line (already the head, via
// cardsKeywordHead) into its combat meaning, normalising Forge's optional
// "HIDDEN " marker away first because the corpus spells the SAME restriction
// both ways. Only the three measured phrase shapes are recognised; any other
// sentence contributes nothing (the deliberate fail-closed direction for a
// per-keyword switch). The comparison is case-insensitive and the phrase is
// matched whole -- a longer sentence that merely contains one of these (a
// conditional rider) is a different grant and must not borrow the
// unconditional meaning.
func parseHiddenKeyword(k string) hiddenKeywordFlags {
	head := strings.TrimSpace(strings.TrimPrefix(cardsKeywordHead(k), "HIDDEN "))
	switch {
	case strings.EqualFold(head, "CARDNAME can't attack or block."):
		// The compound spelling imparts BOTH restrictions (Opportunistic
		// Dragon, Extraction Specialist); it must satisfy the cant-block
		// reader as well as the cant-attack one, so it is matched before
		// either simple spelling.
		return hiddenKeywordFlags{cantAttack: true, cantBlock: true}
	case strings.EqualFold(head, "CARDNAME can't attack."):
		return hiddenKeywordFlags{cantAttack: true}
	case strings.EqualFold(head, "CARDNAME can't block."):
		return hiddenKeywordFlags{cantBlock: true}
	case strings.EqualFold(head, "CARDNAME must be blocked if able."):
		return hiddenKeywordFlags{mustBlock: true}
	}
	return hiddenKeywordFlags{}
}

// derivedHiddenFlags folds parseHiddenKeyword over the object's CURRENT
// derived keyword list (printed plus layer-granted), so both an Animate
// HiddenKeywords$ grant and a Pump/PumpAll KW$ grant reach the combat oracle
// alike.
func (e *Engine) derivedHiddenFlags(id state.ObjID) hiddenKeywordFlags {
	var f hiddenKeywordFlags
	for _, k := range e.Derived(id).Keywords {
		g := parseHiddenKeyword(k)
		f.cantAttack = f.cantAttack || g.cantAttack
		f.cantBlock = f.cantBlock || g.cantBlock
		f.mustBlock = f.mustBlock || g.mustBlock
	}
	return f
}

// hasCantBlockKeyword reports whether the object's CURRENT derived keyword
// list carries Forge's textual can't-block grant ("CARDNAME can't block." or
// the compound "CARDNAME can't attack or block."), with or without the
// HIDDEN marker Forge prepends. Unlike HasKeyword, which compares heads
// exactly, parseHiddenKeyword normalises the optional "HIDDEN " prefix away
// first, because the corpus spells the SAME restriction both ways: many files
// carry `KW$ HIDDEN CARDNAME can't block.` (Pump/PumpAll templates, Concussive
// Bolt) and Incite Hysteria, Unearthly Blizzard and Siegebreaker Giant carry
// the bare `KW$ CARDNAME can't block.`. Both are one derived layer-6 grant and
// must reach the block oracle alike; a hardcoded pair of literals would miss
// the next spelling. The grant is a rules-side casting/blocking option, so it
// is read from the derived list (printed plus layer-granted), never the
// printed face.
func (e *Engine) hasCantBlockKeyword(id state.ObjID) bool {
	return e.derivedHiddenFlags(id).cantBlock
}

// hasCantAttackKeyword is hasCantBlockKeyword's attacker-side counterpart:
// the derived keyword grant "CARDNAME can't attack." or the compound
// "CARDNAME can't attack or block." (Opportunistic Dragon, Extraction
// Specialist). Read by attackBlocked, which feeds both the attacker offer
// list and validateAttackers, so a rules-ignorant client can never declare
// the attack and the validator recomputes it with the same oracle.
func (e *Engine) hasCantAttackKeyword(id state.ObjID) bool {
	return e.derivedHiddenFlags(id).cantAttack
}

// hasMustBeBlockedKeyword reports whether the object's derived keyword list
// carries the attacker-oriented requirement "CARDNAME must be blocked if
// able." (Elemental Uprising, Vengeant Earth, Disturbed Slumber, and the
// Pump/PumpAll KW$ carriers). This is the ATTACKER's CR 509.1c requirement to
// receive at least one legal blocker -- not the blocker-oriented Mode$
// MustBlock (mustBlockCandidates), which requires a particular BLOCKER to
// block. The requirement's feasibility is decided by askBlockers over the
// offered pairs, never here.
func (e *Engine) hasMustBeBlockedKeyword(id state.ObjID) bool {
	return e.derivedHiddenFlags(id).mustBlock
}

// blockRestricted reports whether blocker is forbidden from blocking
// attacker (CantBlock, CantBlockBy, or a granted can't-block keyword).
// Called from rules/combat.go's canBlock, which askBlockers and handleBlockers
// both use for real declare-blockers option generation and validation.
func (e *Engine) blockRestricted(blocker, attacker state.ObjID) bool {
	// Forge's Pump/PumpAll KW$ (HIDDEN) CARDNAME can't block. is a derived
	// layer-6 grant, not a static. Read it here so the same restriction
	// governs offered blocks and validation, including Concussive Bolt.
	if e.hasCantBlockKeyword(blocker) {
		return true
	}
	// The Effect-registered CantBlockBy grants walk FIRST, beside the
	// CantTarget precedent (restrictionBlocksTarget): the registered
	// restriction's ValidAttacker$ is matched against the ATTACKER with the
	// registration's Remembered set bound (Rikku Resourceful Guardian's
	// "that creature can't be blocked by creatures your opponents control",
	// RememberObjects$ TriggeredObjectLKICopy -- the gaining creature), and
	// ValidBlocker$ against the would-be blocker with the effect's own
	// controller as the spec's "you". The registration path (effEffect's
	// CantBlockByRestrictionParamsReadable whitelist) already excluded gated
	// bodies, so
	// no per-static condition gate runs here.
	for _, ce := range e.active() {
		if ce.Restriction != "CantBlockBy" {
			continue
		}
		atkSpec := ce.RestrictParams["ValidAttacker"]
		if atkSpec == "" {
			atkSpec = ce.RestrictParams["ValidCard"]
		}
		sc := e.specCtx(ce.Source, ce.Controller)
		for _, r := range ce.Remembered {
			sc.Remembered = append(sc.Remembered, state.Target{Obj: r})
		}
		if !e.matchesSpec(atkSpec, attacker, sc) {
			continue
		}
		blkSpec, ok := ce.RestrictParams["ValidBlocker"]
		if !ok {
			return true
		}
		if e.matchesSpec(blkSpec, blocker, sc) {
			return true
		}
	}
	for _, sv := range e.activeStatics("CantBlock") {
		// The shared continuous gate evaluates Condition$, IsPresent$ and
		// CheckSVar$ families with the same fail-closed semantics used by
		// continuous effects.
		if !e.continuousGateHolds(sv) {
			continue
		}
		if e.matchesSpec(sv.Params["ValidCard"], blocker, e.staticSpecCtx(sv)) {
			return true
		}
	}
	for _, sv := range e.activeStatics("CantBlockBy") {
		// Apply the same shared per-static gate as the CantBlock loop above.
		if !e.continuousGateHolds(sv) {
			continue
		}
		// ValidAttacker$ is Forge's own spelling for the attacker side of a
		// CantBlockBy static (Steel Leaf Champion's "Creature.Self", the
		// Unblockable pump templates' "Card.IsRemembered", the blocker-side
		// "CARDNAME can block only creatures with flying" shape) — 594 corpus
		// files carry it and NONE of them spell the attacker with ValidCard$;
		// that spelling is the hand-authored test fixture's. The historical
		// ValidCard$ read stays as the fallback so both grammars work, and an
		// SA carrying neither fails closed exactly as before (the empty spec
		// matches nothing).
		attackerSpec := sv.Params["ValidAttacker"]
		if attackerSpec == "" {
			attackerSpec = sv.Params["ValidCard"]
		}
		if !e.matchesSpec(attackerSpec, attacker, e.staticSpecCtx(sv)) {
			continue
		}
		spec, ok := sv.Params["ValidBlocker"]
		if !ok {
			return true
		}
		if e.matchesSpec(spec, blocker, e.staticSpecCtx(sv)) {
			return true
		}
	}
	// The Effect-registered CantBlockBy restrictions (task cbb1): an
	// `AB$ Effect | StaticAbilities$ Unblockable` whose SVar body is
	// `Mode$ CantBlockBy | ValidAttacker$ Card.IsRemembered` -- Suspicious
	// Bookcase's "{3},{T}: Target creature can't be blocked this turn", the
	// dominant unblockable template (measured 246 corpus files carry the
	// Effect-delivered shape) -- registers through effEffect's restriction
	// case and is consulted here beside the face statics, so the remembered
	// creature really is unblockable for the effect's lifetime. The
	// registration gate (effects.CantBlockByRestrictionParamsReadable) has
	// already refused every body whose scoping this loop cannot evaluate
	// (ValidBlockerRelative$, IsPresent$/PresentCompare$ gates), so the
	// loop reads ValidAttacker$/ValidBlocker$ unconditionally and the only
	// fail-closed direction is the ordinary matcher's empty-set read.
	for _, ce := range e.active() {
		if ce.Restriction != "CantBlockBy" {
			continue
		}
		attackerSpec := ce.RestrictParams["ValidAttacker"]
		if attackerSpec == "" {
			attackerSpec = ce.RestrictParams["ValidCard"]
		}
		sc := e.specCtx(ce.Source, ce.Controller)
		for _, r := range ce.Remembered {
			sc.Remembered = append(sc.Remembered, state.Target{Obj: r})
		}
		if attackerSpec == "" {
			// A spec-less restriction names exactly its remembered set (the
			// restrictionApplies convention, evaluated per-pair here because
			// this read consults one (blocker, attacker) candidate at a
			// time): an attacker outside the set is not restricted.
			remembered := false
			for _, r := range ce.Remembered {
				if r == attacker {
					remembered = true
				}
			}
			if !remembered {
				continue
			}
		} else if !e.matchesSpec(attackerSpec, attacker, sc) {
			continue
		}
		if spec, ok := ce.RestrictParams["ValidBlocker"]; ok {
			if !e.matchesSpec(spec, blocker, sc) {
				continue
			}
		}
		return true
	}
	return false
}

// minMaxBlockerParamsReadable is the parameter whitelist a MinMaxBlocker
// static must pass before its blocker-count bound is enforced. A static
// carrying a semantic parameter this build cannot evaluate is SKIPPED (the
// restriction simply does not apply), the permissive direction for a
// restriction and the same convention CantRestrictionParamsReadable uses for
// CantAttack/CantSacrifice. The gate parameters are whitelisted because
// continuousGateHolds evaluates them (fail-closed).
func minMaxBlockerParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCard", "Min", "Max", "Description", "Secondary",
			"Condition", "IsPresent", "IsPresent2", "PresentCompare", "PresentZone",
			"CheckSVar", "SVarCompare", "AffectedZone":
		default:
			return false
		}
	}
	return true
}

// minMaxBlockerBounds reports the blocker-count bounds an attacking creature
// is subject to from every applicable S:Mode$ MinMaxBlocker static (CR 509.1a's
// block-restriction family: "can't be blocked by more than one creature" and
// "can't be blocked except by N or more creatures"). min is the STRICTEST
// Min$ among the matching statics (the largest), max the strictest Max$ (the
// smallest); minOK/maxOK say whether a bound was present at all. all is set by
// Min$ All (Tromokratis: "can't be blocked unless all creatures defending
// player controls block it"), which the caller resolves against the defending
// player's own board.
//
// ValidCard$ is resolved against the ATTACKER (the creature the restriction
// applies to) with the static's host as the spec source, so a non-self scope
// (Vorrac Battlehorns' Creature.EquippedBy, the YouCtrl team statics) reaches
// the right creature. A static whose parameters or gates this build cannot
// read is skipped -- a restriction that cannot be proven must not silently
// apply. The SVar-delivered form (SVar:MinMaxBlocked:Mode$ MinMaxBlocker,
// reached through DB$ Effect | StaticAbilities$) is NOT seen here: activeStatics
// reads printed statics only, the same Effect-delivered gap AGENTS.md records
// for other modes.
func (e *Engine) minMaxBlockerBounds(attacker state.ObjID) (min, max int, minOK, maxOK, all bool) {
	max = math.MaxInt32
	for _, sv := range e.activeStatics("MinMaxBlocker") {
		if !minMaxBlockerParamsReadable(sv.Params) {
			continue
		}
		if !e.continuousGateHolds(sv) {
			continue
		}
		if !e.matchesSpec(sv.Params["ValidCard"], attacker, e.staticSpecCtx(sv)) {
			continue
		}
		if raw, ok := sv.Params["Min"]; ok {
			if strings.EqualFold(strings.TrimSpace(raw), "All") {
				all = true
				continue
			}
			if n, ok := literalBlockCount(raw); ok && (!minOK || n > min) {
				min, minOK = n, true
			}
		}
		if raw, ok := sv.Params["Max"]; ok {
			if n, ok := literalBlockCount(raw); ok && (!maxOK || n < max) {
				max, maxOK = n, true
			}
		}
	}
	return min, max, minOK, maxOK, all
}

// literalBlockCount parses a Min$/Max$ bound: a non-negative integer, the
// only shape the corpus prints. A non-literal value (an SVar name, an
// expression) reports ok=false, so the bound is not enforced rather than
// mis-enforced -- the same permissive direction the whitelist takes.
func literalBlockCount(raw string) (int, bool) {
	v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || v < 0 || v > int64(math.MaxInt32) {
		return 0, false
	}
	return int(v), true
}

// costScope names WHAT is being priced when cost modifiers are collected:
// a spell cast (kind "Spell", with the cast variant mode naming the
// flashback/surge/kicked/miracle shape a ValidSpell$ may gate on) or an
// activated ability (kind "Ability", with ab the exact SA being activated —
// the thing a ValidSpell$ Activated.<keyword> static scopes to). The zero
// kind is "Spell"; mode "" is the plain cast.
type costScope struct {
	kind string
	mode string
	ab   *cards.SA
}

func spellScope(mode string) costScope    { return costScope{kind: "Spell", mode: mode} }
func foretellScope() costScope            { return costScope{kind: "Foretell", mode: "foretell"} }
func abilityScope(ab *cards.SA) costScope { return costScope{kind: "Ability", ab: ab} }

// costMod is ONE evaluated ReduceCost static's contribution to a total cost.
// generic is the literal/SVar-evaluated Amount$; colored carries a Color$
// reduction (per colour slot, set only when the static names one);
// ignoreGeneric records the IgnoreGeneric$ flag (a colour reduction whose
// pip the cost lacks spills to generic only when this is false); floor is
// the MinMana$ lower bound ("can't reduce the mana in that cost to less than
// N mana") evaluated AFTER this reduction alone.
type costMod struct {
	generic       int32
	colored       state.Mana
	hasColor      bool
	ignoreGeneric bool
	floor         int32
}

// costMods is the evaluated composition for one cast/activation, in the CR
// 601.2f order: every increase first, then every reduction in static order
// (each with its own MinMana floor), then a SetCost floor last.
type costMods struct {
	raises []int32
	extra  Cost
	// raiseCol / raiseLife carry the aggregated RaiseCost Cost$ raises: the
	// coloured pips, generic and life a "Black spells you cast cost {B} more"
	// static adds to the total (an additional cost — never reduced by the
	// reductions that follow, per CR 601.2f's increase-then-reduce order).
	raiseCol  state.Mana
	raiseGen  int32
	raiseLife int32
	reduces   []costMod
	setFloor  int32
	// hasExtra records that extra was ever composed into (the only writes
	// that can make it non-zero). While it is false extra is the zero Cost,
	// which apply may then skip (see apply).
	hasExtra bool
}

// empty reports whether the composition would change nothing, so a caller can
// keep its old zero-value shorthand.
func (m costMods) empty() bool {
	return len(m.raises) == 0 && len(m.extra.Blight) == 0 && m.raiseGen == 0 && m.raiseLife == 0 &&
		m.raiseCol.Total() == 0 && len(m.reduces) == 0 && m.setFloor == 0
}

// apply composes c with the modifiers, CR 601.2f: increases before
// reductions, each reduction applied whole (Color$ reductions take the
// matching pips first, spilling the shortfall to generic unless
// IgnoreGeneric$), each reduction's MinMana$ floor restored immediately, and
// the SetCost floor raised to last (Trinisphere: total mana below 3 becomes
// 3). Generic never dips below zero at any point.
func (m costMods) apply(c Cost) Cost {
	// Plus with the zero Cost is the identity on every field except the
	// three it clamps or maxes at zero (Life, Snow, XMin), so the two
	// 744-byte copies are skipped only when extra is provably zero and none
	// of those three is negative.
	if m.hasExtra || c.Life < 0 || c.Snow < 0 || c.XMin < 0 {
		c = c.Plus(m.extra)
	}
	for _, r := range m.raises {
		c.Generic = addClampedGeneric(c.Generic, int64(r))
	}
	for i := range m.raiseCol {
		c.Colored[i] = addClampedGeneric(c.Colored[i], int64(m.raiseCol[i]))
	}
	c.Generic = addClampedGeneric(c.Generic, int64(m.raiseGen))
	c.Life = addClampedGeneric(c.Life, int64(m.raiseLife))
	for _, red := range m.reduces {
		leftover := red.generic
		if red.hasColor {
			for i := range red.colored {
				take := red.colored[i]
				if c.Colored[i] < take {
					take = c.Colored[i]
				}
				c.Colored[i] -= take
				leftover += red.colored[i] - take
			}
		}
		if leftover > 0 && !(red.hasColor && red.ignoreGeneric) {
			c.Generic -= leftover
			if c.Generic < 0 {
				c.Generic = 0
			}
		}
		if red.floor > 0 {
			if total := totalMana(c); total < red.floor {
				c.Generic = addClampedGeneric(c.Generic, int64(red.floor-total))
			}
		}
	}
	if m.setFloor > 0 {
		if total := totalMana(c); total < m.setFloor {
			c.Generic = addClampedGeneric(c.Generic, int64(m.setFloor-total))
		}
	}
	return c
}

// totalMana counts the mana a cost demands, pip by pip — every symbol CR
// 107.4 knows counts one, and numeric tokens count their value. This is the
// total the MinMana$ and SetCost floors are measured against.
func totalMana(c Cost) int32 {
	return c.CMC()
}

// hasFloor reports whether the composition carries any MinMana$ or SetCost
// floor. A floor raises the cost's total mana (Cost.CMC) to a minimum, and
// CR 202.4b prices a monocolour-hybrid pip at its generic face — the highest
// of its faces — so an unresolved pip overprices every cheaper face the
// announcement may resolve it to. That is the one part of the composition
// whose result depends on the not-yet-announced pip faces; raises and
// reductions are face-independent, so only a composition with a floor needs
// the per-face enumeration feasibleAny performs.
func (m costMods) hasFloor() bool {
	if m.setFloor > 0 {
		return true
	}
	for _, red := range m.reduces {
		if red.floor > 0 || red.hasColor {
			return true
		}
	}
	return false
}

// composeFeasible is feasibleAny's leaf composition of the cost it prices:
// the XMin announcement floor, the modifiers, the commander tax and the delve
// credit, in that order.
func (m costMods) composeFeasible(c Cost, taxGeneric, delve int32) Cost {
	// A cost carrying an XMin<N> lower bound is priced at its smallest
	// LEGAL announcement: "X can't be 0" means the offer must be able
	// to pay {X}=XMin, never {X}=0 (Thieving Skydiver's kicked Kicker).
	// The fold is on a LOCAL copy, so the payment descriptor's
	// announced-X marker (set from the raw cost's own Cost.X by
	// paymentFor) still reports CostContainsX. WithX clears XMin, so
	// an already-announced cost (XMin==0) is untouched here.
	if c.XMin > 0 {
		c = c.WithX(c.XMin)
	}
	cc := m.apply(c)
	cc.Generic = addClampedGeneric(cc.Generic, int64(taxGeneric))
	if cc.Generic > delve {
		cc.Generic -= delve
	} else {
		cc.Generic = 0
	}
	return cc
}

// poolUnitsFloor is a lower bound on the pool units every successful
// resolveManaWith payment of c spends: its Generic plus one unit per strict
// W/U/R/G/C pip. Each such pip is paid only by a colour alternative (the
// anyColor/anyType riders and a conversion widen WHICH colour, never
// whether a unit is taken), and takeUnit removes exactly one unit of the
// remainder; the search then needs the remainder to cover a generic
// requirement that only ever grows from c.Generic. A {B} pip is left out
// (PayLifeInsteadOf:B may pay it with life), as is every hybrid, Phyrexian
// and snow pip, so the bound holds whatever the payer's grants are. A pool
// holding fewer units than this can pay nothing, which is exactly the
// answer the search would give.
func (c *Cost) poolUnitsFloor() int64 {
	n := int64(c.Generic)
	for _, letter := range pipLetters {
		if letter == 'B' {
			continue
		}
		if k := c.Colored[state.ManaIndex(letter)]; k > 0 {
			n += int64(k)
		}
	}
	return n
}

// feasibleAny is THE one shared mana-feasibility primitive of the cast flow.
// It answers the CR 601.2b/601.2f question for a cost whose flexible pips may
// still be unresolved — at the offer gate (offerCastable), at each CR 601.2b
// announcement menu (announceFeasible, with the pips already announced folded
// in as their final resolved faces) and at the target-repricing gates (a cost
// with no live pip, where it degenerates to the composed payable check): is
// there SOME legal face assignment of the remaining announcement pips
// (two-colour hybrid, monocolour hybrid, Phyrexian, hybrid-Phyrexian) that,
// with m composed onto the RESOLVED faces at the leaf of the walk and
// taxGeneric added after (an additional cost is never reduced), is payable
// from pool/snow/life? Because the offer gate, every announcement menu and
// the charge (manaToPay/payMana) compose the same modifiers through this one
// primitive, an offered cast, an offered announcement face and the charged
// total can never disagree.
// delve is the payer's Delve graveyard credit pool (0 without Delve): the
// credit is taken off each assignment's generic after the composition and
// tax — the same place the payment (payCast) takes it off manaToPay.
//
// A cost with no announcement pip, or a composition with no face-sensitive
// modifier, composes identically for every face and degrades to the single
// composed payable check. Without the enumeration an offer priced under a
// floor with an unresolved twobrid pip could be offered although NEITHER of
// its announced faces completes the raised cost — Trinisphere over an
// unresolved {2/W} composes {2/W}+{1} (payable from {W}{W} by its white face
// plus one generic), while the white face announces to {W}+{2} and the
// generic face to {3}, neither payable from {W}{W}. Color$ needs the same
// ordering: a W reduction must see an announced W half of {W/U}. The walk
// resolves one pip per level in announcePip order and stops at the first
// payable assignment, so a payable cost is found without visiting the whole
// tree.
func (m costMods) feasibleAny(c Cost, pool, snow state.Mana, typed [7]state.Mana, life, taxGeneric, delve int32, bLifeOK bool, rider pipRider, conv *manaConv) bool {
	composed := func(c Cost) bool {
		cc := m.composeFeasible(c, taxGeneric, delve)
		// The pool-unit floor is a necessary condition of resolveManaWith's
		// own search (poolUnitsFloor), decided without building its pips.
		if int64(pool.Total()) < cc.poolUnitsFloor() {
			return false
		}
		_, ok := cc.resolveManaWith(pool, snow, typed, life, bLifeOK, rider, conv)
		return ok
	}
	if !m.hasFloor() || c.annPipCount() == 0 {
		return composed(c)
	}
	var walk func(c Cost) bool
	walk = func(c Cost) bool {
		if c.annPipCount() == 0 {
			return composed(c)
		}
		for _, alt := range c.announcePip(0) {
			r := c
			switch {
			case alt.color != 0:
				r.Colored[state.ManaIndex(alt.color)]++
			case alt.generic > 0:
				r.Generic = addClampedGeneric(r.Generic, int64(alt.generic))
			case alt.life > 0:
				r.Life = addClampedGeneric(r.Life, int64(alt.life))
			}
			if walk(r.dropAnnouncePrefix(1)) {
				return true
			}
		}
		return false
	}
	return walk(c)
}

// manaFeasible is the engine-facing form of the shared primitive: it reads
// the payer's restriction-aware pool (manaAvailableFor: RestrictValid$ mana
// is invisible to a payment its restriction does not admit), snow tally and
// life, the payer's stat:ManaConvert conversion set (paymentConv), and hands
// them to costMods.feasibleAny. Every mana-feasibility gate of the cast flow
// goes through it — the offer gate (offerCastable), each CR 601.2b
// announcement menu (announceFeasible, over the partially announced cost) and
// the target-repricing gates — so no site re-derives its own "payable so far"
// answer. With no RestrictValid$ batch and no ManaConvert static on the
// battlefield this is exactly the plain-pool payable check it was before the
// mana-shaping primitives landed (paymentConv returns nil, manaAvailableFor
// returns Pool verbatim), so every pre-existing game resolves byte-identically.
func (e *Engine) manaFeasible(p state.PlayerID, id state.ObjID, ability bool, c Cost, mods costMods, taxGeneric, delve int32) bool {
	return e.manaFeasibleGrant(p, id, ability, c, mods, taxGeneric, delve,
		pipRider{anyColor: e.payerGrantsIgnoreColor(p, id), anyType: e.payerGrantsIgnoreType(p, id)})
}

// manaFeasibleGrant is manaFeasible with the may-play ignore-colour rider
// passed explicitly (a pendingCast's pc.mayPlayIgnore, or the offer-side
// payerGrantsIgnoreColor derivation), and the payer's PayLifeInsteadOf:B
// grant derived here. Both widen the leaf payable check the same way the
// payment (resolveManaWith) widens it, so an offered cast, an offered
// announcement face and the charged total can never disagree on a
// K'rrik-shaped or MayPlayIgnoreColor$-shaped cost either. The descriptor is
// the caller's: post-announcement costs (pc.resolvedMana and friends) have
// WithX folded their X into Generic, so they must arrive through
// paymentForCast — a paymentFor-built descriptor from a folded cost would
// hide every CostContainsX batch from the target-repricing gates exactly
// where the offer and the payment both admit it.
func (e *Engine) manaFeasibleDescriptor(p state.PlayerID, d paymentDescriptor, c Cost, mods costMods, taxGeneric, delve int32, rider pipRider) bool {
	pl := e.G.Players[p]
	av := e.manaAvailableFor(p, d)
	return mods.feasibleAny(c, av.pool, pl.Snow, av.typed, pl.Life, taxGeneric, delve,
		e.payerGrantsPayLifeInsteadOfB(p), rider, e.paymentConv(p, d.id, d.class == paymentActivated))
}

// manaFeasibleGrant prices a RAW (unannounced) cost: the offer-side entry
// whose cost still carries any unfolded X, so paymentFor's own derivation
// sets the announced-X marker.
func (e *Engine) manaFeasibleGrant(p state.PlayerID, id state.ObjID, ability bool, c Cost, mods costMods, taxGeneric, delve int32, rider pipRider) bool {
	return e.manaFeasibleDescriptor(p, paymentFor(id, ability, c), c, mods, taxGeneric, delve, rider)
}

// manaFeasiblePool is manaFeasible priced against an EXPLICIT pool instead of
// the seat's restriction-adjusted floating one. The ordinary gate
// (manaFeasible above) is exactly this with the real manaAvailableFor pool;
// the potential-action walk (rules/legal.go legalActionsPriced) passes the
// hypothetical bound the seat would hold after floating every untapped
// source. The payer grants and conversion shaping are the same reads in both
// modes, so a potential action and the offer the walk mirrors can never
// disagree about what the pool may satisfy.
func (e *Engine) manaFeasiblePool(p state.PlayerID, id state.ObjID, ability bool, c Cost, mods costMods, taxGeneric, delve int32, pool state.Mana, typed [7]state.Mana) bool {
	// Without an announcement walk feasibleAny is exactly its composed leaf,
	// whose first answer is the pool-unit floor (poolUnitsFloor): decide that
	// here, before the payer's grant and conversion reads the search needs.
	if !mods.hasFloor() || c.annPipCount() == 0 {
		if cc := mods.composeFeasible(c, taxGeneric, delve); int64(pool.Total()) < cc.poolUnitsFloor() {
			return false
		}
	}
	pl := e.G.Players[p]
	return mods.feasibleAny(c, pool, pl.Snow, typed, pl.Life, taxGeneric, delve,
		e.payerGrantsPayLifeInsteadOfB(p),
		pipRider{anyColor: e.payerGrantsIgnoreColor(p, id), anyType: e.payerGrantsIgnoreType(p, id)},
		e.paymentConv(p, id, ability))
}

// manaFeasiblePriced is manaFeasible's priced-mode entry: hyp nil keeps the
// ordinary real-pool gate, hyp non-nil prices the feasibility against the
// potential walk's hypothetical bound (rules/legal.go legalActionsPriced).
func (e *Engine) manaFeasiblePriced(p state.PlayerID, id state.ObjID, ability bool, c Cost, mods costMods, taxGeneric, delve int32, hyp *state.Mana) bool {
	av := e.manaAvailableFor(p, paymentFor(id, ability, c))
	pool, typed := av.pool, av.typed
	if hyp != nil {
		pool = *hyp
		// A hypothetical bound is a pure mana bound (see costPayablePool),
		// so its typed partition is the raw tally.
		typed = e.G.Players[p].ManaUnits()
	}
	return e.manaFeasiblePool(p, id, ability, c, mods, taxGeneric, delve, pool, typed)
}

// effectZoneOK reports whether a static whose EffectZone$ reads v applies
// while its source sits in zone z. Forge's default is the battlefield, and
// the corpus names zones with Forge's comma-separated All/Battlefield/Stack/
// Graveyard/Hand/Library/Exile/Command words; anything unrecognised denies
// (the same fail-closed direction every unparseable static qualifier takes),
// because wrongly applying a hand-or-library static is exactly the class of
// over-reach the zone gate exists to prevent.
func effectZoneOK(v string, z state.Zone) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return z == state.ZBattlefield
	}
	for name := range strings.SplitSeq(v, ",") {
		switch strings.TrimSpace(name) {
		case "All":
			return true
		case "Battlefield":
			if z == state.ZBattlefield {
				return true
			}
		case "Stack":
			if z == state.ZStack {
				return true
			}
		case "Graveyard":
			if z == state.ZGraveyard {
				return true
			}
		case "Hand":
			if z == state.ZHand {
				return true
			}
		case "Library":
			if z == state.ZLibrary {
				return true
			}
		case "Exile":
			if z == state.ZExile {
				return true
			}
		case "Command":
			if z == state.ZCommand {
				return true
			}
		}
	}
	return false
}

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
		// CR 708.8: a face-down permanent has no printed cost statics
		// (scanActionStatics' and scanActiveStatics' gate).
		if e.faceDownPrintedHides(o) {
			return
		}
		for si, sn := 0, o.PileStaticCount(); si < sn; si++ {
			pst, ok := o.PileStaticAt(si)
			if !ok {
				continue
			}
			st := pst.Static
			var dst *[]staticView
			switch st.Mode {
			case "RaiseCost":
				dst = &out.raise
			case "ReduceCost":
				dst = &out.reduce
			case "SetCost":
				dst = &out.set
			case "OptionalCost":
				dst = &out.optional
			default:
				continue
			}
			if !effectZoneOK(st.Params["EffectZone"], o.Zone) {
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

// markCostValidTarget sets out.validTarget from the collected members.
func markCostValidTarget(out *costStaticViews) {
	for _, views := range [...][]staticView{out.raise, out.reduce, out.set} {
		for _, sv := range views {
			if _, ok := sv.Params["ValidTarget"]; ok {
				out.validTarget = true
				return
			}
			// A target-conditional ValidSpell$ (Head of the Class's
			// `Spell.IsTargeting Valid Creature` reduction) reads the chosen
			// targets exactly as ValidTarget$ does, so the offer gate's
			// potential-target retry must run for it too.
			if validSpellHasTargeting(sv.Params["ValidSpell"]) {
				out.validTarget = true
				return
			}
		}
	}
}

// appendEffectCostStatics appends the Effect-delivered cost-modifier statics
// (see scanCostStatics) after the printed ones.
func (e *Engine) appendEffectCostStatics(out *costStaticViews) {
	for _, ce := range e.active() {
		var dst *[]staticView
		switch ce.CostStaticMode {
		case "RaiseCost":
			dst = &out.raise
		case "ReduceCost":
			dst = &out.reduce
		case "SetCost":
			dst = &out.set
		default:
			continue
		}
		*dst = append(*dst, staticView{Source: ce.Source, Controller: ce.Controller,
			Params: ce.CostStaticParams, ChosenNumber: ce.ChosenNumber, chosenNumberBound: true})
	}
}

// modAmountX evaluates one cost-modifier static's Amount$: a plain literal
// stands as itself; anything else is an SVar name on the source's face or an
// inline Count$ expression, resolved through effects.EvalCount against the
// source and its SVar table. An unresolvable value degrades to ZERO, never
// to 1: the old fallback made Rakdos, Lord of Riots and Herald of War reduce
// by exactly 1 whenever their SVar amount was genuinely 0, and a wrong
// reduction is a wrong cost — 0 ("no reduction") is the honest read of an
// amount the engine cannot evaluate.
//
// x is the cast's announced {X}, bound into the evaluation context so an
// Amount$ chain that reads Count$xPaid (Dargo's SVar:X:Count$xPaid over
// SVar:Y:SVar$X/Times.2) sees the announced value during the in-cast
// recomputation manaToPay/manaToPayX run; x=0 is the offer-time read (an
// unbound {X} prices as 0). targets is the cast's chosen targets (or the
// offer gate's cost-potential candidate list) — the same list
// costStaticApplies already gates ValidTarget$ against — bound onto
// effects.Ctx.Targets so a target-conditional Amount$ head such as Not of
// This World's TargetedByTarget$Valid Count$Compare chain reads what the
// spell is being cast at. The raise and set sites pass targets too, so one
// composition path cannot half-apply a static with an unbound read.
func (e *Engine) modAmountX(sv staticView, x int32, targets []state.Target) int32 {
	raw := strings.TrimSpace(sv.Params["Amount"])
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if n < 0 {
			return 0
		}
		if n > int64(math.MaxInt32) {
			return math.MaxInt32
		}
		return int32(n)
	}
	o := e.G.Obj(sv.Source)
	if o == nil || o.Face() == nil {
		return 0
	}
	svars := sv.SVars
	if svars == nil {
		svars = o.Face().SVars
	}
	// An Effect-delivered cost static carries its SetChosenNumber$ binding
	// (chosenNumberBound): the Count$ChosenNumber head reads it rather than
	// the source object's own logged choice.
	ctx := &effects.Ctx{Source: sv.Source, Controller: sv.Controller, SVars: svars, X: x,
		ChosenNumber: sv.ChosenNumber, ChosenNumberBound: sv.chosenNumberBound,
		Targets: targets}
	// An SVar NAME resolves through its body on the source's face; anything
	// else is an inline Count$-class expression evaluated as written.
	if body, ok := svars[raw]; ok {
		return effects.EvalCount(e, ctx, body)
	}
	return effects.EvalCount(e, ctx, raw)
}

// modAmountPotential evaluates one ReduceCost static's Amount$ for the
// OFFER gate, where the caller holds the list of every legal candidate target
// rather than one announced selection. It prices ONE possible announcement --
// the best single candidate -- never the union of all candidates: a
// target-count Amount$ (Battlefield Thaumaturge's `Amount$ ReduceCost` over
// `SVar:ReduceCost:TargetedObjectsDistinct$Valid Creature.inZoneBattlefield`,
// "{1} less for each creature it targets") would otherwise sum across every
// candidate and admit an offer that no single target choice can pay. The
// per-candidate discipline is the SAME one affordableTargetCandidates applies
// at the CR 601.2c target menu (each candidate tested as the sole selection),
// so the offer and the menu cannot disagree about which targets are payable;
// the multi-target case stays deliberately conservative, exactly like the
// menu. With one candidate (the common single-target shape, and Not of This
// World's single stack-spell candidate) this is the plain modAmountX, so the
// fix cannot change a one-candidate price.
func (e *Engine) modAmountPotential(sv staticView, x int32, targets []state.Target) int32 {
	if len(targets) <= 1 {
		return e.modAmountX(sv, x, targets)
	}
	best := e.modAmountX(sv, x, []state.Target{targets[0]})
	for _, t := range targets[1:] {
		if n := e.modAmountX(sv, x, []state.Target{t}); n > best {
			best = n
		}
	}
	return best
}

// modAmountReduce is the reduce loop's Amount$ read: the potential-target
// offer gate must price one candidate announcement (modAmountPotential),
// while the chosen-target and announced-X recomputations price the actual
// selection (modAmountX). Keeping the choice here means every ReduceCost
// branch -- generic, Color$, and the X-bound recomputation -- makes the same
// one and cannot half-apply.
func (e *Engine) modAmountReduce(sv staticView, x int32, targets []state.Target, potential bool) int32 {
	if potential {
		return e.modAmountPotential(sv, x, targets)
	}
	return e.modAmountX(sv, x, targets)
}

// raiseExtraFromCost parses a RaiseCost Cost$ that raiseFromCost does NOT
// model as plain mana/life into the non-mana ADDITIONAL cost it names, which
// the cast flow folds into the pending cost exactly like a SpellAbility's own
// Cost$ (withSpellAbilityExtras). Only the Blight<X>/Blight<N> shape is
// bridged here: Soul Immolation's `Cost$ Blight<X>` and Blighted Nightmare's
// same-token ability cost are the corpus's two carriers, and the whole point
// is that the announced X must survive into offer gating, the X ask, the
// blight payment and the X-dependent effect with ONE representation
// (costMods.extra, folded by costMods.apply and by the pending cast), never a
// re-parse that could disagree.
//
// The Cost$ must parse into EXACTLY the blight part: a mixed token the parser
// also leaves an Unknown for, or a mana/other non-mana part beside it, reports
// false so the static degrades to the ordinary Amount$ fallback (fail closed,
// the raiseFromCost convention for an unmodelled shape). Other non-mana
// RaiseCost shapes (Waterbend, ExileFromHand, Sac<...>) remain unsupported and
// are not bridged here.
func raiseExtraFromCost(s string) (Cost, bool) {
	if strings.TrimSpace(s) == "" {
		return Cost{}, false
	}
	c := ParseCost(s)
	if len(c.Blight) == 0 || len(c.Unknown) > 0 {
		return Cost{}, false
	}
	// Reject anything beyond the blight part: bridging must not silently drop
	// a mana or other non-mana component of a mixed Cost$.
	if c.Colored.Total() != 0 || c.Generic != 0 || c.Life != 0 || c.X != 0 || c.XMin != 0 ||
		c.Tap || len(c.Sac) > 0 || len(c.Discard) > 0 || len(c.SubCounter) > 0 ||
		len(c.AddCounter) > 0 || len(c.Exile) > 0 || len(c.Reveal) > 0 ||
		len(c.RevealOrChoose) > 0 ||
		len(c.RevealChosen) > 0 || len(c.Behold) > 0 || len(c.TapPermanent) > 0 ||
		len(c.Energy) > 0 || len(c.Return) > 0 || len(c.Draw) > 0 || len(c.LifeX) > 0 ||
		len(c.DamageYou) > 0 || len(c.MoveToGrave) > 0 || len(c.Mill) > 0 || len(c.Evidence) > 0 ||
		len(c.Exert) > 0 || c.Forage {
		return Cost{}, false
	}
	return Cost{Blight: c.Blight}, true
}

// raiseFromCost parses a RaiseCost Cost$ into its mana and life raise. Only
// the plain shapes apply: single colour letters, numeric tokens, and the
// fixed PayLife<N> token. Anything else — hybrid pips (none in the corpus's
// cost raises), X/T, or a <...> component this build does not model as an
// additional raise (Waterbend, ExileFromHand, BeholdExile, Sac<...>,
// AddCounter, tapXType) — reports false, so the static degrades to the
// Amount$ reading (absent → the zero raise) rather than silently pricing an
// unmodelled cost as one generic mana.
func raiseFromCost(s string) (col state.Mana, gen, life int32, ok bool) {
	for toks := (costTokenIter{s: s}); ; {
		sym, more := toks.next()
		if !more {
			break
		}
		switch {
		case len(sym) == 1 && strings.ContainsRune("WUBRGC", rune(sym[0])):
			col[state.ManaIndex(sym[0])]++
		case isDigitRun(sym):
			n, err := strconv.ParseInt(sym, 10, 64)
			if err != nil || n < 0 || n > int64(math.MaxInt32) {
				return col, 0, 0, false
			}
			gen = addClampedGeneric(gen, n)
		default:
			if m := lifeCost.FindStringSubmatch(sym); m != nil {
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					return col, 0, 0, false
				}
				life = addClampedGeneric(life, n)
				continue
			}
			return col, 0, 0, false
		}
	}
	return col, gen, life, true
}

// costActorMatches is the cost-modifier actor gate: a RaiseCost/ReduceCost
// static with an Activator$ or Caster$ parameter scopes to whose cost it
// modifies. With neither it applies regardless of actor.
func (e *Engine) costActorMatches(sv staticView, actor state.PlayerID) bool {
	if sv.HasParam(cards.PKActivator) {
		return e.actorMatches(sv, "Activator", actor)
	}
	if sv.HasParam(cards.PKCaster) {
		return e.actorMatches(sv, "Caster", actor)
	}
	return true
}

// costModifiers evaluates and orders the RaiseCost/ReduceCost/SetCost statics
// that apply to a cast/activation of id by p, per CR 601.2f and Forge's
// CostAdjustment. A static applies only when every gate it carries holds:
// Type$ (the other kind is skipped, neither means both), Activator$/Caster$
// (whose action), ValidCard$ (what is being paid for), ValidSpell$ (which
// spell or ability — Auriok Steelshaper's Activated.Equip), ValidTarget$
// (the announced target, repriced before payment), AffectedZone$ for an
// ability modifier (which zone its source sits in) and IsPresent$
// (an intervening-if, e.g. Trinisphere's untapped self). Amount$ is
// evaluated through the SVar/Count$ machinery, and the modifiers are
// returned in Forge's application order (increases, reductions in static
// order, SetCost floor).
func (e *Engine) costModifiers(p state.PlayerID, id state.ObjID, scope costScope) costMods {
	return e.costModifiersWithTargets(p, id, scope, nil, false)
}

// costModifiersForTargets is costModifiers with the chosen cast-time targets
// supplied. A nil target slice is the pre-announcement offer phase, where a
// ValidTarget$ static cannot yet apply; target choice re-enters this helper
// before payment with the actual targets.
func (e *Engine) costModifiersForTargets(p state.PlayerID, id state.ObjID, scope costScope, targets []state.Target) costMods {
	return e.withCostCompositionEvent(id, func() costMods {
		return e.costModifiersWithTargets(p, id, scope, targets, false)
	})
}

// withCostCompositionEvent excludes only the current cast's latest push while
// its payment modifiers are recomputed. Object identity alone is insufficient:
// a spell may have been cast, returned to hand, and cast again this turn.
func (e *Engine) withCostCompositionEvent(id state.ObjID, compose func() costMods) costMods {
	previous := e.costCompositionEvent
	if e.cast != nil && e.cast.card == id && !e.cast.isAbility() {
		for i := len(e.L.Events) - 1; i >= 0; i-- {
			ev := e.L.Events[i]
			if ev.Kind == events.PutOnStack && ev.Obj == id {
				e.costCompositionEvent = i + 1
				break
			}
		}
	}
	if e.costCompositionEvent == previous {
		return compose()
	}
	// costCompositionEvent hides the pending cast from Count$ThisTurnCast, a
	// layer-7 input (CheckSVar$ statics) the cross-walk Derived memo cannot
	// see: retire its entries on entry and exit so none built under the
	// exclusion is served outside it, nor a live one inside it. active()'s
	// two log-head-keyed lists evaluate the same CheckSVar$ gates at build,
	// so they are invalidated on both edges too (the cascade.go
	// stackGrantCast pattern). Without it a list built at the same log head
	// OUTSIDE the exclusion was served inside it: Leapfrog ("flying as long
	// as you've cast an instant or sorcery this turn") kept the flying Gust
	// of Wind's own push gave it, so Gust's "costs {2} less if you control a
	// creature with flying" was charged {1}{U} where the planner -- and CR
	// 601.2i, the spell is not yet cast -- price {3}{U} (round-8 cardfuzz
	// mirror seed 12687133153333408407, a_witness pool_after).
	e.retireCrossWalkMemo()
	e.activeEpoch, e.staticEpoch = -1, -1
	mods := compose()
	e.costCompositionEvent = previous
	e.retireCrossWalkMemo()
	e.activeEpoch, e.staticEpoch = -1, -1
	return mods
}

// costModifiersForPotentialTargets is the offer-side counterpart for a
// target-conditional REDUCTION. It admits a spell whose base cost is
// unaffordable only when at least one legal target can make the reduction
// apply. Target-conditional raises and SetCost floors are intentionally not
// assumed: they can only make an otherwise legal offer more expensive, so
// charging them speculatively would incorrectly withhold a nonmatching
// target choice. The selected target is always repriced by
// costModifiersForTargets before payment.
func (e *Engine) costModifiersForPotentialTargets(p state.PlayerID, id state.ObjID, scope costScope, targets []state.Target) costMods {
	return e.costModifiersWithTargets(p, id, scope, targets, true)
}

// costModifiersForTargetsX is costModifiersForTargets with the cast's
// announced {X} bound: the payment-side recomputation (manaToPay/manaToPayX)
// uses it when the cost announces a variable sacrifice count (Sac<X/Spec>),
// because the offer-time snapshot priced every Amount$ with X=0 and a
// reduction reading Count$xPaid would otherwise never apply (Dargo's
// "{2} less for each permanent sacrificed this way").
func (e *Engine) costModifiersForTargetsX(p state.PlayerID, id state.ObjID, scope costScope, targets []state.Target, x int32) costMods {
	return e.withCostCompositionEvent(id, func() costMods {
		return e.costModifiersWithTargetsX(p, id, scope, targets, false, x)
	})
}

func (e *Engine) costModifiersWithTargetsX(p state.PlayerID, id state.ObjID, scope costScope, targets []state.Target, potential bool, x int32) costMods {
	return e.costModifiersWithTargetsXUsing(e.collectCostStatics(), p, id, scope, targets, potential, x)
}

func (e *Engine) costModifiersWithTargetsXUsing(statics costStaticViews, p state.PlayerID, id state.ObjID, scope costScope, targets []state.Target, potential bool, x int32) costMods {
	// The same per-pass provenance capture costModifiersWithTargetsUsing owns.
	e.costProvenanceSeen = false
	var mods costMods
	xBound := x != 0
	for _, group := range []struct {
		mode  string
		views []staticView
	}{
		{"RaiseCost", statics.raise},
		{"ReduceCost", statics.reduce},
	} {
		mode := group.mode
		for _, sv := range group.views {
			if potential && mode == "RaiseCost" {
				if _, targetConditional := sv.Params["ValidTarget"]; targetConditional {
					continue
				}
			}
			if !e.costStaticApplies(sv, mode, p, id, scope, targets, xBound) {
				continue
			}
			if mode == "RaiseCost" {
				// A RaiseCost Cost$ names the whole additional cost (Forge
				// CostAdjustment's RaiseCost branch): a plain mana/life cost
				// is raised as-is, pips and life included. A Cost$ paired
				// with an Amount$ ("you may pay {1}{G} any number of times")
				// is an OPTIONAL additional-cost shape this build does not
				// model -- the exotic Amount$ skips the static below, so only
				// the plain raise applies. Cost$ shapes that are not plain
				// mana/life (Waterbend, ExileFromHand, Sac<...>) parse
				// nowhere and are skipped by raiseFromCost.
				rc, rg, rl, costOK := raiseFromCost(sv.Params["Cost"])
				if costOK {
					if _, hasAmt := sv.Params["Amount"]; !hasAmt {
						for i := range rc {
							mods.raiseCol[i] = addClampedGeneric(mods.raiseCol[i], int64(rc[i]))
						}
						mods.raiseGen = addClampedGeneric(mods.raiseGen, int64(rg))
						mods.raiseLife = addClampedGeneric(mods.raiseLife, int64(rl))
						continue
					}
				}
				// A non-mana RaiseCost Cost$ the plain parser rejected (the
				// Blight<X> bridge) is carried as an ADDITIONAL cost so its X
				// announcement, offer gate and payment all price the same way.
				if _, hasAmt := sv.Params["Amount"]; !hasAmt {
					if extra, ok := raiseExtraFromCost(sv.Params["Cost"]); ok {
						mods.extra = mods.extra.Plus(extra)
						mods.hasExtra = true
						continue
					}
				}
				mods.raises = append(mods.raises, e.modAmountX(sv, x, targets))
				continue
			}
			red := costMod{
				ignoreGeneric: sv.Params["IgnoreGeneric"] == "True",
				floor:         parseAmount(sv.Params["MinMana"], 0),
			}
			if col, ok := sv.Params["Color"]; ok && strings.TrimSpace(col) != "" {
				// Each listed token is reduced by the Amount$: colour letters
				// take their pip from the cost's coloured part, and a numeric
				// token names that many generic pips.  Numeric is deliberately
				// not limited to "1": Discontinuity's real `Color$ 2 U U`
				// removes two generic and two blue pips.  Treating `2` as a
				// colour letter would route it through ManaIndex and remove one
				// colourless pip instead.  Amount$ applies to every token, so
				// `Color$ 2 U | Amount$ X` means 2*X generic plus X blue.
				red.hasColor = true
				amount := e.modAmountReduce(sv, x, targets, potential)
				for tok := range strings.FieldsSeq(col) {
					if isDigitRun(tok) {
						n, err := strconv.ParseInt(tok, 10, 64)
						if err != nil || n < 0 || n > int64(math.MaxInt32) {
							continue // malformed Color$ token fails closed
						}
						red.generic = addClampedGeneric(red.generic, n*int64(amount))
						continue
					}
					if len(tok) == 1 && strings.ContainsRune("WUBRGC", rune(tok[0])) {
						red.colored[state.ManaIndex(tok[0])] = addClampedGeneric(
							red.colored[state.ManaIndex(tok[0])], int64(amount))
					}
				}
			} else {
				red.generic = e.modAmountReduce(sv, x, targets, potential)
			}
			mods.reduces = append(mods.reduces, red)
		}
	}
	for _, sv := range statics.set {
		if potential {
			if _, targetConditional := sv.Params["ValidTarget"]; targetConditional {
				continue
			}
		}
		if !e.costStaticApplies(sv, "SetCost", p, id, scope, targets, xBound) {
			continue
		}
		if n := e.modAmountX(sv, x, targets); n > mods.setFloor {
			mods.setFloor = n
		}
	}
	return mods
}

func (e *Engine) costModifiersWithTargets(p state.PlayerID, id state.ObjID, scope costScope, targets []state.Target, potential bool) costMods {
	return e.costModifiersWithTargetsUsing(e.collectCostStatics(), p, id, scope, targets, potential)
}

func (e *Engine) costModifiersWithTargetsUsing(statics costStaticViews, p state.PlayerID, id state.ObjID, scope costScope, targets []state.Target, potential bool) costMods {
	// Each pass owns the provenance capture: cleared here, set by
	// costStaticApplies when a ValidCard$ carries a cast-provenance token.
	e.costProvenanceSeen = false
	var mods costMods
	for _, group := range []struct {
		mode  string
		views []staticView
	}{
		{"RaiseCost", statics.raise},
		{"ReduceCost", statics.reduce},
	} {
		mode := group.mode
		for _, sv := range group.views {
			if potential && mode == "RaiseCost" {
				if _, targetConditional := sv.Params["ValidTarget"]; targetConditional {
					continue
				}
			}
			if !e.costStaticApplies(sv, mode, p, id, scope, targets, false) {
				continue
			}
			if mode == "RaiseCost" {
				// A RaiseCost Cost$ names the whole additional cost (Forge
				// CostAdjustment's RaiseCost branch): a plain mana/life cost
				// is raised as-is, pips and life included. A Cost$ paired
				// with an Amount$ ("you may pay {1}{G} any number of times")
				// is an OPTIONAL additional-cost shape this build does not
				// model — the exotic Amount$ skips the static below, so only
				// the plain raise applies. Cost$ shapes that are not plain
				// mana/life (Waterbend, ExileFromHand, Sac<...>) parse
				// nowhere and are skipped by raiseFromCost.
				rc, rg, rl, costOK := raiseFromCost(sv.Params["Cost"])
				if costOK {
					if _, hasAmt := sv.Params["Amount"]; !hasAmt {
						for i := range rc {
							mods.raiseCol[i] = addClampedGeneric(mods.raiseCol[i], int64(rc[i]))
						}
						mods.raiseGen = addClampedGeneric(mods.raiseGen, int64(rg))
						mods.raiseLife = addClampedGeneric(mods.raiseLife, int64(rl))
						continue
					}
				}
				// A non-mana RaiseCost Cost$ the plain parser rejected (the
				// Blight<X> bridge) is carried as an ADDITIONAL cost so its X
				// announcement, offer gate and payment all price the same way.
				if _, hasAmt := sv.Params["Amount"]; !hasAmt {
					if extra, ok := raiseExtraFromCost(sv.Params["Cost"]); ok {
						mods.extra = mods.extra.Plus(extra)
						mods.hasExtra = true
						continue
					}
				}
				mods.raises = append(mods.raises, e.modAmountX(sv, 0, targets))
				continue
			}
			red := costMod{
				ignoreGeneric: sv.Params["IgnoreGeneric"] == "True",
				floor:         parseAmount(sv.Params["MinMana"], 0),
			}
			if col, ok := sv.Params["Color"]; ok && strings.TrimSpace(col) != "" {
				// Each listed token is reduced by the Amount$: colour letters
				// take their pip from the cost's coloured part, and a numeric
				// token names that many generic pips.  Numeric is deliberately
				// not limited to "1": Discontinuity's real `Color$ 2 U U`
				// removes two generic and two blue pips.  Treating `2` as a
				// colour letter would route it through ManaIndex and remove one
				// colourless pip instead.  Amount$ applies to every token, so
				// `Color$ 2 U | Amount$ X` means 2*X generic plus X blue.
				red.hasColor = true
				amount := e.modAmountReduce(sv, 0, targets, potential)
				for tok := range strings.FieldsSeq(col) {
					if isDigitRun(tok) {
						n, err := strconv.ParseInt(tok, 10, 64)
						if err != nil || n < 0 || n > int64(math.MaxInt32) {
							continue // malformed Color$ token fails closed
						}
						red.generic = addClampedGeneric(red.generic, n*int64(amount))
						continue
					}
					if len(tok) == 1 && strings.ContainsRune("WUBRGC", rune(tok[0])) {
						red.colored[state.ManaIndex(tok[0])] = addClampedGeneric(
							red.colored[state.ManaIndex(tok[0])], int64(amount))
					}
				}
			} else {
				red.generic = e.modAmountReduce(sv, 0, targets, potential)
			}
			mods.reduces = append(mods.reduces, red)
		}
	}
	for _, sv := range statics.set {
		if potential {
			if _, targetConditional := sv.Params["ValidTarget"]; targetConditional {
				continue
			}
		}
		if !e.costStaticApplies(sv, "SetCost", p, id, scope, targets, false) {
			continue
		}
		if n := e.modAmountX(sv, 0, targets); n > mods.setFloor {
			mods.setFloor = n
		}
	}
	return mods
}

// optionalCostViews returns self-spell OptionalCost statics in collector order.
// These are deliberately narrower than the general cost-modifier grammar: the
// supported corpus shape is an EffectZone$ All self static on the spell face.
func (e *Engine) optionalCostViews(statics costStaticViews, p state.PlayerID, id state.ObjID) []Cost {
	var out []Cost
	for _, sv := range statics.optional {
		if strings.TrimSpace(sv.Params["ValidSA"]) != "Spell" ||
			strings.TrimSpace(sv.Params["EffectZone"]) != "All" ||
			!strings.Contains(sv.Params["ValidCard"], "Card.Self") ||
			sv.Source != id || !e.costStaticApplies(sv, "OptionalCost", p, id, spellScope(""), nil, false) {
			continue
		}
		c := ParseCost(sv.Params["Cost"])
		if len(c.Unknown) == 0 {
			out = append(out, c)
		}
	}
	return out
}

// costStaticApplies runs the gate chain one cost-modifier static must pass
// before its Amount$ is evaluated and applied. Every unimplementable
// qualifier denies (never silently over-applies): ValidTarget$ needs the
// chosen targets no offer-time composition has, so a target-conditional
// modifier is skipped; ValidSpell$ shapes this build cannot evaluate fail
// closed; a SetCost without RaiseTo$ True is not the shape this build
// implements.
func (e *Engine) costStaticApplies(sv staticView, mode string, p state.PlayerID, id state.ObjID, scope costScope, targets []state.Target, xBound bool) bool {
	if !e.classBandGateHolds(sv.Params, sv.Source) {
		return false
	}
	if ty, ok := sv.Param(cards.PKType); ok && ty != "" && ty != scope.kind {
		return false
	}
	if !e.costActorMatches(sv, p) {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(sv.ParamStr(cards.PKOnlyFirstSpell)), "True") &&
		e.onlyFirstSpellUsed(sv, p, id) {
		// OnlyFirstSpell$ (Conduit of Ruin: "The first creature spell you cast
		// each turn costs {2} less"): the reduction is spent once the
		// turn's first covered spell has been announced. See
		// onlyFirstSpellUsed for the tracking.
		return false
	}
	if spec, ok := sv.Param(cards.PKValidCard); ok {
		// The provenance-keyed ValidCard$ (castprov3: Bilbo's
		// "!wasCastFromYourHand" ReduceCost) is unresolvable while the priced
		// object has no cast in the log yet — the offer walk and the
		// option-selection snapshot both evaluate pre-push, where the negated
		// spelling would wrongly hold for the hand cast it must not cover.
		// Deny the modifier (full price, this gate chain's documented
		// fail-closed direction); continueCast re-prices the pending cast
		// right after CR 601.2a's push, once the PutOnStack is in the log.
		// The capture (noCounterSpend's shape) tells the pending-cast flow a
		// re-price is owed; the read stays inside the attributed cost-static
		// pass, so the param census sees no new Params site.
		if strings.Contains(spec, "wasCastFromYourHand") || strings.Contains(spec, "wasCastByYou") {
			e.costProvenanceSeen = true
		}
		spec, ok2 := e.castProvenanceAdmitsPending(spec, id, sv.Controller)
		if !ok2 || !e.matchesSpec(spec, id, e.staticSpecCtx(sv)) {
			return false
		}
	}
	if first, ok := sv.Param(cards.PKFirstForetell); ok && strings.EqualFold(strings.TrimSpace(first), "True") &&
		scope.kind == "Foretell" && e.firstForetellUsed(p) {
		return false
	}
	if vs, ok := sv.Param(cards.PKValidSpell); ok && !e.validSpellMatches(sv, scope, p, id, vs, targets) {
		return false
	}
	if az, ok := sv.Param(cards.PKAffectedZone); ok && scope.kind == "Ability" {
		o := e.G.Obj(id)
		if o == nil {
			return false
		}
		if !affectedZoneOK(az, o.Zone) {
			return false
		}
	}
	if spec, ok := sv.Param(cards.PKIsPresent); ok && !e.isPresent(spec, sv) {
		return false
	}
	if !e.costConditionHolds(sv, p) {
		return false
	}
	if !e.checkSVarHolds(sv) {
		return false
	}
	if spec, ok := sv.Params["ValidTarget"]; ok && !e.costTargetsMatch(sv, spec, targets) {
		return false
	}
	if mode == "SetCost" && sv.Params["RaiseTo"] != "True" {
		// Forge's SetCost carries RaiseTo$ True (Trinisphere) for the
		// raise-to-N shape; any other SetCost shape is unimplemented and
		// must not silently floor the cost.
		return false
	}
	if sv.Params["Secondary"] == "True" {
		// Forge's Secondary$ marks a static that duplicates another one's
		// effect under a different wording; applying both would double the
		// modifier (the paired pair of "spells that target ... cost {2} more"
		// lines). Skipping the secondary applies the primary only.
		return false
	}
	if sv.Params["Relative"] == "True" && !(mode == "ReduceCost" && xBound) {
		// Relative$ Amount$ scales with something the composition point does
		// not yet know (IncreaseCost per target beyond the first, or a game
		// state the offer-time read cannot price) — a per-target shape no
		// offer-time composition knows. Skipping, like ValidTarget$.
		// EXCEPTION: the announced-X recomputation (costModifiersForTargetsX,
		// manaToPay/manaToPayX) is exactly the caller whose composition point
		// DOES know the variable a "costs {2} less for each permanent
		// sacrificed this way" amount scales with — Dargo's Relative$ True
		// static reads Amount$ Y over SVar:Y:SVar$X/Times.2 with X the
		// announced sacrifice count, and at that point the amount is
		// evaluated with X bound. A Relative$ ReduceCost whose Amount$ is
		// unresolvable still degrades to zero (the honest no-reduction),
		// never to an invented discount.
		//
		// SECOND EXCEPTION (notofthisworld1): with the cast's targets bound
		// onto effects.Ctx.Targets (modAmountX now threads them), a Relative$
		// REDUCE-cost whose Amount$ the bound context can actually EVALUATE
		// is no longer a shape the composition point cannot price — Not of
		// This World's Amount$ CostReduction over
		// SVar:CostReduction:Count$Compare CheckTgt GE1.7.0 with
		// SVar:CheckTgt:TargetedByTarget$Valid Card.powerGE7+YouCtrl reads
		// its real {7}-or-0 from the targeted spell's own targets, at the
		// offer gate (costModifiersForPotentialTargets' candidate list) and
		// at the CR 601.2c reprice (the chosen targets) alike. The probe is
		// the SAME evaluation modAmountX runs, so the gate and the amount
		// cannot disagree; an amount that resolves to zero prices as no
		// reduction, exactly what the skip produced. Anything unresolvable
		// (and every RaiseCost/SetCost Relative$) keeps the fail-closed skip.
		if !(mode == "ReduceCost" && e.relativeAmountResolves(sv, targets)) {
			return false
		}
	}
	return true
}

// relativeAmountResolves reports whether a Relative$ cost-modifier static's
// Amount$ evaluates under a context bound with the cast's targets — the
// verdict (not the value) is what the costStaticApplies Relative$ exception
// consumes, so the gate and the amount evaluation share one read. A plain
// integer literal stands as itself. See the call site's SECOND EXCEPTION for
// the measured blast radius (notofthisworld1).
func (e *Engine) relativeAmountResolves(sv staticView, targets []state.Target) bool {
	raw := strings.TrimSpace(sv.Params["Amount"])
	if raw == "" {
		return false
	}
	if _, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return true
	}
	o := e.G.Obj(sv.Source)
	if o == nil || o.Face() == nil {
		return false
	}
	svars := sv.SVars
	if svars == nil {
		svars = o.Face().SVars
	}
	body, ok := svars[raw]
	if !ok {
		body = raw
	}
	ctx := &effects.Ctx{Source: sv.Source, Controller: sv.Controller, SVars: svars, Targets: targets}
	_, ok = effects.EvalCountOK(e, ctx, body)
	return ok
}

// costTargetsMatch reports whether at least one chosen target satisfies a
// ValidTarget$ cost-modifier requirement. Forge's "spells that target a
// creature" grammar is an any-target condition: selecting one matching
// target is sufficient, including in a multi-target spell. Object targets use
// the static source context so Card.Self/NICKNAME bind to the permanent that
// supplied the modifier; player targets use the same controller-relative
// player-spec matcher as the other static gates. A nil target slice is the
// pre-announcement offer phase and deliberately cannot satisfy the condition.
func (e *Engine) costTargetsMatch(sv staticView, spec string, targets []state.Target) bool {
	if len(targets) == 0 {
		return false
	}
	ctx := e.staticSpecCtx(sv)
	for _, target := range targets {
		if target.IsPlayer {
			if effects.MatchesPlayerSpec(e.G, spec, target.Player, sv.Controller) {
				return true
			}
			continue
		}
		if e.matchesSpec(spec, target.Obj, ctx) {
			return true
		}
	}
	return false
}

// costConditionHolds evaluates Condition$ on a cost-modifier static. The
// implementable conditions: PlayerTurn / NotPlayerTurn (the caster is or is
// not the active player -- discontinuity's "During your turn"), Metalcraft
// (three artifacts on the battlefield, the shared metalcraftHolds read) and
// Delirium (four or more distinct core card types in the caster's graveyard,
// the shared graveyardCardTypeCount census -- drag_to_the_roots and its
// cycle). An unimplementable condition (Night) DENIES: a conditional
// discount that silently always applies is a wrong cost, the same fail-closed
// direction the ValidSpell$ shapes take.
func (e *Engine) costConditionHolds(sv staticView, p state.PlayerID) bool {
	cond, ok := sv.Params["Condition"]
	if !ok {
		return true
	}
	switch strings.TrimSpace(cond) {
	case "PlayerTurn":
		return e.G.Active == p
	case "NotPlayerTurn":
		return e.G.Active != p
	case "Metalcraft":
		return e.metalcraftHolds(p)
	case "Delirium":
		// The same shared census the Continuous gate and the ability-offer
		// gate (rules/legal.go's activationConditionOK) read.
		return e.graveyardCardTypeCount(p) >= 4
	}
	return false
}

// metalcraftHolds is the shared Metalcraft read: three or more artifacts the
// player controls, counted off the derived types so a layer-4 type grant is
// seen (the same read rules/legal.go's activationConditionOK makes). Used by
// the cost-modifier gate, the Continuous gate, the trigger condition gate
// (triggerConditionHoldsAs' Metalcraft$ / bare-Condition$ Metalcraft clauses)
// and any future condition reader -- ONE census, so none can drift apart.
func (e *Engine) metalcraftHolds(p state.PlayerID) bool {
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && slices.Contains(e.Derived(id).Types, "Artifact") {
			n++
		}
	}
	return n >= 3
}

// checkSVarHolds evaluates the CheckSVar$/SVarCompare$ intervening-if: the
// named SVar (or inline Count$ expression) is evaluated with the same
// machinery modAmount uses, and compared under SVarCompare$ ("<op><number>",
// e.g. GE4 — the threshold may also be an SVar name, resolved the same way).
// No SVarCompare$ means "nonzero" (Forge's default truthiness read).
//
// The grammar lives in effects.CheckSVarHolds, the ONE SVar-compare evaluator
// this build ships: conditionMet's ConditionCheckSVar$ branch
// (effects/conditions.go) and rules/legal.go's ability-offer gate delegate to
// the same function. This wrapper only maps the staticView onto a Ctx — the
// source face's SVar table, the static's controller as You.
func (e *Engine) checkSVarHolds(sv staticView) bool {
	raw, ok := sv.Params["CheckSVar"]
	if !ok {
		return true
	}
	if strings.TrimSpace(raw) == "" {
		// Present-but-empty: the empty expression, EvalCount reads it 0 and
		// the no-compare nonzero read fails it -- what the pre-delegation
		// code did too (and no corpus static carries).
		return false
	}
	o := e.G.Obj(sv.Source)
	if o == nil || o.Face() == nil {
		return false
	}
	svars := sv.SVars
	if svars == nil {
		svars = o.Face().SVars
	}
	ctx := &effects.Ctx{Source: sv.Source, Controller: sv.Controller, SVars: svars}
	holds, evaluated := effects.CheckSVarHolds(e, ctx, raw, sv.Params["SVarCompare"])
	if !evaluated {
		// The statics' shipped convention: an unreadable gate body (an
		// unmodelled count head, an unparseable compare) degrades to zero and
		// the static's gate fails — a continuous "as long as X" must not
		// silently always-apply on a gate this build cannot read. The SA-level
		// callers (conditionMet, the ability-offer gate) fail OPEN instead;
		// each call site documents its own direction.
		return false
	}
	return holds
}

// isPresent evaluates the IsPresent$ intervening-if: an object matching the
// spec exists on any battlefield (Trinisphere's `Card.Self+untapped` matches
// the trinisphere itself while untapped). Resolved against the static's
// source so Self-class predicates bind.
func (e *Engine) isPresent(spec string, sv staticView) bool {
	ctx := e.staticSpecCtx(sv)
	for _, p := range e.G.AliveFrom(0) {
		for _, oid := range e.G.Zone(state.ZBattlefield, p) {
			if e.matchesSpec(spec, oid, ctx) {
				return true
			}
		}
	}
	return false
}

// affectedZoneOK reports whether zone z is named in an AffectedZone$ list
// (Forge's comma-separated zone words; an unrecognised word denies, the same
// direction effectZoneOK takes).
func affectedZoneOK(v string, z state.Zone) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return true
	}
	for name := range strings.SplitSeq(v, ",") {
		switch strings.TrimSpace(name) {
		case "All":
			return true
		case "Battlefield":
			if z == state.ZBattlefield {
				return true
			}
		case "Graveyard":
			if z == state.ZGraveyard {
				return true
			}
		case "Hand":
			if z == state.ZHand {
				return true
			}
		case "Exile":
			if z == state.ZExile {
				return true
			}
		case "Stack":
			if z == state.ZStack {
				return true
			}
		case "Library":
			if z == state.ZLibrary {
				return true
			}
		case "Command":
			if z == state.ZCommand {
				return true
			}
		}
	}
	return false
}

// validSpellMatches implements the ValidSpell$ parameter: a comma-separated
// OR list of "Kind.Constraint" shapes scoping a cost modifier to WHICH spell
// or ability is being paid for. Kind Spell matches a cast (constraint
// checked against the cast variant and the face); Kind Activated matches an
// activated ability (constraint checked against the ability's own keyword
// tag, its API, or the loyalty-ability classifier); Kind Static (morph-up,
// foretell, unlock — casting options this build does not model) never
// matches, so a static naming one is inert rather than over-applied. A
// constraint this build cannot evaluate denies — a discount that wrongly
// applies is a wrong cost, the same fail-closed direction the ValidSA$
// grammar takes.
//
// sv is the owning static (its source and controller bind the constraint's
// spec context), p the caster and targets the cast's target list: the
// nil-target offer phase denies a target-conditional Spell.IsTargeting
// constraint, the potential-target phase admits it on any matching
// candidate, and the chosen-target re-price enforces the announced target —
// the same three-phase discipline costTargetsMatch already applies.
func (e *Engine) validSpellMatches(sv staticView, scope costScope, p state.PlayerID, id state.ObjID, spec string, targets []state.Target) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return true
	}
	for alt := range strings.SplitSeq(spec, ",") {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		kind, constraint := alt, ""
		if i := strings.IndexByte(alt, '.'); i >= 0 {
			kind, constraint = alt[:i], alt[i+1:]
		}
		switch kind {
		case "Spell":
			if scope.kind != "Spell" {
				continue
			}
			if e.spellConstraintMatches(sv, scope, p, id, constraint, targets) {
				return true
			}
		case "Activated":
			if scope.kind != "Ability" || scope.ab == nil {
				continue
			}
			if e.abilityConstraintMatches(scope, p, id, constraint) {
				return true
			}
		case "Static":
			if scope.kind == "Foretell" && constraint == "Foretelling" {
				return true
			}
		}
	}
	return false
}

// spellConstraintMatches checks one ValidSpell$ Spell.* constraint against a
// cast. The constraints the engine can evaluate: bare (any spell), the cast
// variant modes the cast flow names (Flashback, Kicked, Blitz, ...), the card
// types Instant/Sorcery, and the target-conditional `IsTargeting <spec>` form
// (Head of the Class's "the first spell you cast each turn that targets a
// creature"), which rides effects' ONE `Spell.IsTargeting` grammar against
// the same target list costTargetsMatch reads. Everything else — Bargain,
// Buyback, the Dash alternative cast, isCastFaceDown, MayPlaySource —
// is a casting option this function does not read, and denies.
// (Blitz matches the blitzed cast mode: Henzie, Toolbox Torre's
// "Blitz costs you pay cost {1} less" ReduceCost keys on ValidSpell$
// Spell.Blitz, and its scope mode is exactly the mode legal.go offers and
// beginCast charges. Dash is the remaining denied alternative cast.)
func (e *Engine) spellConstraintMatches(sv staticView, scope costScope, p state.PlayerID, id state.ObjID, constraint string, targets []state.Target) bool {
	c := strings.TrimSpace(constraint)
	if strings.HasPrefix(c, "IsTargeting") {
		sc := e.staticSpecCtx(sv)
		sc.You = p
		sc.AsStack = true
		sc.ProposedTargets = targets
		return e.matchesSpec("Spell."+c, id, sc)
	}
	switch c {
	case "":
		return true
	case "Flashback":
		return scope.mode == "flashback"
	case "Kicked":
		// The bare form is the single-cost Kicker's mode; the and/or
		// two-part Kicker's per-part modes (kicked1/kicked2/kickedboth) are
		// kicked casts too -- a cost static gated on "was this kicked" must
		// not depend on WHICH part was paid. A multikicked cast (CR 702.43's
		// kicker variant) is a kicked cast the same way. Shared with
		// targetBoundCtx's pre-payment Count$Kicked binding via modeIsKicked
		// so the two spellings cannot drift.
		return modeIsKicked(scope.mode)
	case "Surged":
		return scope.mode == "surged"
	case "Miracle":
		return scope.mode == "miracle"
	case "Blitz":
		return scope.mode == "blitzed" || strings.HasPrefix(scope.mode, "blitzed_grant_")
	case "Instant":
		if o := e.G.Obj(id); o != nil && o.Face() != nil {
			return o.Face().IsInstant()
		}
		return false
	case "Sorcery":
		if o := e.G.Obj(id); o != nil && o.Face() != nil {
			return o.Face().IsSorcery()
		}
		return false
	}
	return false
}

// abilityConstraintMatches checks one ValidSpell$ Activated.* constraint
// against an activated ability. Keyword-derived constraints (Equip, Ninjutsu,
// Cycling, Boast, Exhaust, ...) match the Keyword$ tag every keyword
// expansion carries (cards/keywords.go); ManaAbility/!ManaAbility read the
// SA API; Loyalty reuses the loyalty-ability classifier; YouCtrl reads the
// ability source's controller. An unevaluable constraint denies.
func (e *Engine) abilityConstraintMatches(scope costScope, p state.PlayerID, id state.ObjID, constraint string) bool {
	ab := scope.ab
	constraint = strings.TrimSpace(constraint)
	switch constraint {
	case "":
		return true
	case "ManaAbility":
		return ab.API == "Mana"
	case "!ManaAbility":
		return ab.API != "Mana"
	case "Loyalty":
		return e.isLoyaltyAbility(ab)
	case "YouCtrl":
		o := e.G.Obj(id)
		return o != nil && o.Controller == p
	case "YouDontCtrl":
		o := e.G.Obj(id)
		return o != nil && o.Controller != p
	}
	// Keyword-derived: the expansion's Keyword$ tag (comma list).
	for kw := range strings.SplitSeq(ab.Params["Keyword"], ",") {
		if strings.EqualFold(strings.TrimSpace(kw), constraint) {
			return true
		}
	}
	return false
}

// parseAmount reads an Amount$ parameter, falling back to def for anything
// that is not a plain non-negative int32 (missing, empty, negative, out of
// int32 range, or a malformed value from a card script this build cannot
// otherwise validate).
//
// Ruling T19b-c: this used to cast straight to int32 with no range check at
// all, unlike mana.go's ParseCost (which explicitly checks 0 <= n <=
// math.MaxInt32 before ever converting). An out-of-range Amount$ silently
// wrapped into a negative int32 -- inverting a RaiseCost into a discount and
// a ReduceCost into a tax -- and a plain negative Amount$ was accepted as-is,
// turning a ReduceCost into a raise (adjustedCost's sign is fixed by the
// mode, so a negative n flips the intended direction rather than reducing
// the magnitude). Both are reachable from card data with no need for the
// value to be anywhere near a real int32 overflow at the mana-cost level:
// the bug is entirely in this parse, not in anything cost-shaped.
func parseAmount(s string, def int32) int32 {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || v < 0 || v > int64(math.MaxInt32) {
		return def
	}
	return int32(v)
}

func init() {
	effects.RegisterNonAPI("stat:CantBeCast", "stat:CantBeActivated", "stat:RaiseCost", "stat:CastWithFlash",
		"stat:ReduceCost", "stat:AlternativeCost", "stat:CantBlock", "stat:CantBlockBy",
		"stat:CantGainLife", "stat:Continuous", "stat:ManaConvert", "stat:NumLoyaltyAct",
		// surveilnum1: the stat:SurveilNum static (Host.SurveilLookExtra,
		// consulted by effects' effSurveil through the shared activeStatics
		// collector). Only the literal-or-SVar Num$ value and the Optional$
		// election are read; a Num$ this build cannot price fails closed.
		"stat:SurveilNum",
		// combatrestriction1: the three combat/sacrifice restriction statics.
		// CantAttack is enforced per (attacker, defender) pair
		// (rules/layers.go attackBlocked, consulted by askAttackers /
		// validateAttackers / mustAttackRequired's pair gate), CantSacrifice at
		// every sacrifice candidate choke point (rules.Engine.SacrificeBlocked,
		// the effects.Host method), and MustAttack by the board-wide
		// activeStatics walk in mustAttackRequired. Only the whitelisted
		// parameter shapes are enforced (cantRestrictionParamsReadable for the
		// two Cant* statics; the Mode$/ValidCreature$/Description$ whitelist the
		// requirement solver already carried for MustAttack) — the conditional
		// shapes stay unregistered behaviour-wise and are ledgered in AGENTS.md.
		"stat:CantAttack", "stat:CantSacrifice", "stat:MustAttack",
		// cantputcounter1: the counter-placement restriction static
		// (rules/layers.go PutCounterBlocked, consulted at the counter choke
		// point in rules/replacement.go before any AddCounter replacement).
		// Only the whitelisted parameter shapes are enforced
		// (effects.CantPutCounterParamsReadable, shared with effEffect's
		// registration gate); the conditional shapes stay unregistered
		// behaviour-wise and are ledgered in AGENTS.md.
		"stat:CantPutCounter",
		// exert1: CR 702.100's attack-time election.
		"stat:OptionalAttackCost",
		// attackprop1: the CR 508.1g attack-prop static (rules/attack_cost.go
		// attackPairCharge, priced per (attacker, defender) pair and paid
		// during the declaration through the attackPay window). The
		// whitelisted shapes are enforced (cantAttackUnlessParamsReadable,
		// delegating to effects.CantAttackUnlessRestrictionParamsReadable),
		// including the composite non-mana components Sac<...>/Return<...>/
		// tapXType<...>/PayLife<...>/{W/P} (chargeFromCost); an unmodelled
		// component still skips the static fail-closed, and the
		// per-attacker-variable price (Nils' RememberingAttacker$) is priced
		// through the SVar grammar. The Effect/Animate-delivered forms are
		// charged through the e.active() walk attackPairCharge carries.
		"stat:CantAttackUnless",
		// blockprop1: the CR 509.1b block-prop static. Face statics are
		// charged per (blocker, attacker) with the same composite grammar,
		// including Sac<...>/Return<...>/PayLife<...>/tapXType<...>/{W/P};
		// the Effect/Animate-delivered forms are charged through the
		// e.active() walk blockPairCharge carries.
		"stat:CantBlockUnless",
		// canattackdefender1: the CR 702.3b permission static (the inverse of
		// a restriction: it LIFTS the Defender wall per (attacker, defender)
		// pair). rules/attack_defender.go attackAllowedThroughDefender is the
		// read, consulted through canAttackPair from the offer list, the
		// validator and the encore gate; the Effect-granted form registers as
		// a CanAttackDefender restriction through effEffect (the Assault
		// Formation shape). Only the whitelisted parameter shapes are
		// enforced (effects.CanAttackDefenderParamsReadable for the face
		// route with the shared gate grammar;
		// effects.CanAttackDefenderGrantParamsReadable for the grant route,
		// which cannot evaluate a gate and so keeps the narrower list).
		"stat:CanAttackDefender",
		// minmaxblocker1: the CR 509.1a block-count restriction static
		// (rules/statics.go minMaxBlockerBounds, enforced whole-declaration by
		// rules/combat.go validateBlockers and consulted by askBlockers' option
		// filter). Only the literal Min$/Max$ bounds are read; the printed
		// StaticAbilities$ directives are the Effect-delivered form and stay
		// out of scope.
		"stat:MinMaxBlocker",
		// unspentmana1: the CR 500.4 exception static (rules/statics.go
		// unspentManaKeep, consulted at the one ManaClear emit site in
		// rules/turn.go's finishStepBoundary; the keep letters ride the
		// ManaClear event Text and the ManaClear fold honours them). Both
		// delivery routes are read -- the printed S: face statics and
		// effEffect's UnspentMana registration arm -- and only the whitelisted
		// parameter shapes are enforced (effects.UnspentManaParamsReadable,
		// shared with effEffect's registration gate).
		"stat:UnspentMana",
		// The static's Cost$ Exert<1/CARDNAME> and Trigger$ rider are consumed
		// by the declare-attackers offer (rules/combat.go's askNextExert) and
		// the Exert-event trigger walker (rules/trigger_match.go
		// checkExertTriggers); its IsPresent$ gate reuses the shared
		// presentGate/countPresent grammar, whose filter now knows the
		// notExertedThisTurn predicate (effects/filter.go).
		// asunblk1: the combat-damage assignment election (rules/combat.go
		// asUnblockedNeeding / damageStep's chosenElection case, CR 509's
		// optional "assign as though it weren't blocked"). Only the printed
		// S:Mode$ statics are read; the SVar:Static: family that rides the
		// Effect path is a separate ledgered gap, and Ruxa's NoAbilities
		// predicate stays an unknown that fails closed.
		"stat:AssignCombatDamageAsUnblocked",
		// toughtdmg1: the CR 510.1 combat-damage assignment statics
		// (rules/statics.go combatDamageToughnessMatches, consumed by the ONE
		// amount helper combatDamageAmount that every assignment site reads).
		// Only the printed S:Mode$ statics are read through activeStatics; the
		// Effect-delivered SVar form (an AB$ Effect | StaticAbilities$
		// CombatDamageToughness body) is the same Effect-registration gap
		// AssignCombatDamageAsUnblocked carries and stays ledgered.
		"stat:CombatDamageToughness", "stat:CountersRemain")
}

// asUnblockedStaticMatches reports whether any battlefield
// AssignCombatDamageAsUnblocked static applies to candidate creature id, and
// whether the matching static is MANDATORY (no Optional$, the auto-accept
// reading) rather than the election-bearing Optional$ True every printed
// corpus carrier spells. The match follows castRestrictedUsing's pattern:
// ValidCard$ is resolved against the CANDIDATE (the attacking creature) with
// the static's host as the spec SOURCE (Indomitable Might's Aura resolves
// Creature.EnchantedBy against its own bearer), and the shared
// restriction/condition gates run first so an unmodelled condition fails
// closed rather than applying blanket. IsPresent$ gates on top through the
// shared countPresent walk (Siege Behemoth's "Card.Self+attacking" — both
// predicates are known), the same clause shape the trigger-side
// presentCondition reader evaluates.
func (e *Engine) asUnblockedStaticMatches(id state.ObjID) (matched, mandatory bool) {
	for _, sv := range e.assignmentStatics("AssignCombatDamageAsUnblocked") {
		if !e.restrictionGateHolds(sv, id) || !e.checkSVarHolds(sv) {
			continue
		}
		if !e.classBandGateHolds(sv.Params, sv.Source) {
			continue
		}
		if spec := strings.TrimSpace(sv.Params["IsPresent"]); spec != "" {
			if e.countPresent(spec, sv.Source, sv.Controller) <= 0 {
				continue
			}
		}
		if !e.matchesSpec(sv.Params["ValidCard"], id, e.staticSpecCtx(sv)) {
			continue
		}
		matched = true
		if strings.TrimSpace(sv.Params["Optional"]) != "True" {
			return true, true
		}
	}
	return matched, false
}

// assignmentStatics collects every S:Mode$ <mode> line from ANY zone a
// static's EffectZone$ admits, for the combat damage-ASSIGNMENT static
// family (CombatDamageToughness, AssignCombatDamageAsUnblocked). These two
// modes are not battlefield-bound the way a lord's Continuous static is:
// Weight Advantage is a Conspiracy that functions from the COMMAND ZONE
// (EffectZone$ Command), so a battlefield-only walk can never see it and its
// controller's creatures would keep assigning power. The walk mirrors
// collectActionStatics/collectCostStatics exactly -- staticSourceZones in
// one fixed order, each static gated by effectZoneOK, the shared stack
// walked once under the first alive seat -- so the class of
// assignment-source statements is covered by construction and the next
// sibling mode added to this family cannot silently miss a non-battlefield
// source the way CombatDamageToughness did.
func (e *Engine) assignmentStatics(mode string) []staticView {
	var out []staticView
	for pi, p := range e.G.AliveFrom(0) {
		for _, z := range staticSourceZones {
			// The stack is a SHARED zone (state.Game.Zone returns g.Stack
			// for every player), so walking it under every alive seat would
			// collect each stack card's statics once per seat. Walk it once,
			// under the first alive seat, keeping staticSourceZones' order.
			if z == state.ZStack && pi > 0 {
				continue
			}
			for _, id := range e.G.Zone(z, p) {
				o := e.G.Obj(id)
				if o == nil || o.Face() == nil {
					continue
				}
				if e.faceDownPrintedHides(o) {
					// CR 708.8: a face-down permanent's printed statics do not
					// exist while it is face down.
					continue
				}
				for si, sn := 0, o.PileStaticCount(); si < sn; si++ {
					pst, ok := o.PileStaticAt(si)
					if !ok {
						continue
					}
					st := pst.Static
					if st.Mode != mode {
						continue
					}
					// The mode's own EffectZone$ gate. The default (Battlefield)
					// keeps a plain printed static exactly where activeStatics put
					// it; a static naming Command (Weight Advantage) is admitted
					// only while its source really sits there, which is the same
					// fail-closed direction effectZoneOK takes everywhere.
					if !effectZoneOK(st.Params["EffectZone"], o.Zone) {
						continue
					}
					out = append(out, staticView{Source: id, Controller: o.Controller, Params: st.Params, PS: st.ParamSetOf(), SVars: pst.Face.SVars})
				}
			}
		}
	}
	// Effect-created assignment statics are admitted only for the exact mode
	// the registration branch records; they do not participate in the printed
	// EffectZone source-zone walk above. active() supplies lifetime and movement
	// filtering, and its order is stable, so append in registry order.
	for _, ce := range e.active() {
		if ce.AssignmentStaticMode != mode {
			continue
		}
		out = append(out, staticView{
			Source: ce.Source, Controller: ce.Controller,
			Params: ce.AssignmentStaticParams, SVars: ce.AssignmentStaticSVars,
			Remembered: ce.Remembered,
		})
	}
	return out
}

// combatDamageToughnessMatches reports whether any
// CombatDamageToughness static applies to candidate creature id (CR 510.1:
// "assigns combat damage equal to its toughness rather than its power"). The
// match follows asUnblockedStaticMatches' pattern exactly -- the shared
// restriction/condition gates, the ClassLevel band, IsPresent$ through the
// shared countPresent walk, and ValidCard$ resolved against the CANDIDATE
// with the static's host as the spec SOURCE, so an Aura's
// Creature.EnchantedBy and a lord's Creature.YouCtrl both resolve. The source
// walk is assignmentStatics (every EffectZone$ the mode admits), so a
// command-zone Conspiracy (Weight Advantage) applies too.
func (e *Engine) combatDamageToughnessMatches(id state.ObjID) bool {
	for _, sv := range e.assignmentStatics("CombatDamageToughness") {
		if !e.restrictionGateHolds(sv, id) || !e.checkSVarHolds(sv) {
			continue
		}
		if !e.classBandGateHolds(sv.Params, sv.Source) {
			continue
		}
		if spec := strings.TrimSpace(sv.Params["IsPresent"]); spec != "" {
			if e.countPresent(spec, sv.Source, sv.Controller) <= 0 {
				continue
			}
		}
		if !e.matchesSpec(sv.Params["ValidCard"], id, e.assignmentStaticSpecCtx(sv)) {
			continue
		}
		return true
	}
	return false
}

// combatDamageAmount is the amount creature id assigns as combat damage in a
// damage step: its TOUGHNESS when a CombatDamageToughness static applies (CR
// 510.1), else its power. This is the ONE read of the assignment source, so
// every consumer -- the attacker's own assignment, each blocker's hit-back,
// the division option count and the as-unblocked election gate -- cannot
// disagree. It reads through the layer walk (Toughness/Power are
// derivedScalar), so a static P/T bonus the same turn changes the amount
// exactly as it changes the characteristic.
//
// A creature whose effective amount is at most zero assigns no combat damage
// (an assignment of zero is not a decision and deals nothing), matching the
// power gate it replaces; callers that need the amount also read its sign.
func (e *Engine) combatDamageAmount(id state.ObjID) int32 {
	if e.combatDamageToughnessMatches(id) {
		return e.Toughness(id)
	}
	return e.Power(id)
}

// altCostLabel names the nth (0-indexed) alternative-cost option for a
// spell, distinct from the base "Cast <name>" label and from each other when
// a card somehow offers more than one alternative.
func altCostLabel(name string, i int) string {
	if i == 0 {
		return "Cast " + name + " (alternative cost)"
	}
	return fmt.Sprintf("Cast %s (alternative cost %d)", name, i+1)
}

// panharmoniconEchoes reports how many ADDITIONAL trigger placements the
// battlefield's stat:Panharmonicon statics demand for the trigger the EVENT
// ev just caused on source object src (CR 702.109: "that ability triggers an
// additional time"). One echo per matching static, in activeStatics'
// deterministic scan order (which never matters here -- the echoes are summed
// -- but keeps the call off any map range).
//
// A static must match the triggering CAUSE, not just the triggering
// permanent, before it doubles anything. The gate reads the corpus's own
// parameter set, each optional, every one present needing to match:
//
//   - ValidMode$: the comma-separated Forge event-mode family the causing
//     event belongs to (panharmoniconModes); a static naming modes the event
//     is not in contributes nothing.
//   - ValidCause$: the event's causing OBJECT -- the moved/cast card for a
//     zone change or cast, the single attacker of a one-attacker declare
//     (a multi-attacker event has no singular cause and fails closed, the
//     same convention triggerReferents' Attacks case applies) -- matched as
//     a filter spec. Origin$/Destination$ scope a ChangesZone cause to the
//     move's own zones exactly as the trigger grammar does.
//   - ValidSource$: the in-flight damage source (e.damaging -- combat
//     damage's dealing creature; absent for any other event), a filter spec.
//   - ValidTarget$: the event's target -- the damage recipient (object or
//     player, whichever the event carries) or the BecomesTarget-ed object
//     (the trigger's own source), a filter spec or player spec.
//   - ValidActivator$: the cast's player, a player spec.
//   - ValidPlayer$: the event's player (the life gainer, the drawing
//     player), a player spec.
//   - CombatDamage$ True: the in-flight damage must be the combat damage
//     step's own assignment (the combatDamaging flag); False is the default
//     and matches either.
//
// ValidZone$ (Echoes of Eternity's "Battlefield,Stack") is honoured as a
// filter on the triggering object's own zone; a static naming zones the
// object is not in contributes nothing. A static with no ValidCard$
// contributes nothing rather than matching everything -- a Panharmonicon
// static that does not say WHAT it doubles is a script defect this build
// will not paper over by doubling every trigger on the board. ValidTurned$
// (Panoptic Projektor's "if turning a face-down permanent face up ...", the
// corpus's one carrier) scopes the permanent that was turned up, matched
// against the turn-up event's own object. Params this build cannot evaluate
// (IsPresent/PresentCompare, Condition) fail closed: the static does not
// double, never over-applies.
func (e *Engine) panharmoniconEchoes(observer *Engine, src state.ObjID, ev events.Event) int {
	g := observer.G
	o := g.Obj(src)
	if o == nil {
		return 0
	}
	modes := panharmoniconModes(ev)
	n := 0
	for _, sv := range e.activeStatics("Panharmonicon") {
		if vm := sv.Params["ValidMode"]; vm != "" {
			ok := false
			for want := range strings.SplitSeq(vm, ",") {
				want = strings.TrimSpace(want)
				for _, have := range modes {
					if want == have {
						ok = true
						break
					}
				}
				if ok {
					break
				}
			}
			if !ok {
				continue
			}
		}
		if vz := sv.Params["ValidZone"]; vz != "" {
			zones, all, valid := effects.ParseZones(vz)
			ok := false
			if valid || all {
				for _, z := range zones {
					if z == o.Zone {
						ok = true
						break
					}
				}
			}
			if !ok {
				continue
			}
		}
		// ValidCause$: the causing object per event family. A zone change or
		// cast's cause is the moved/cast card; an attack's is the single
		// declared attacker (a multi-attacker event has no singular cause);
		// a damage event's cause is not modelled -- fail closed.
		if spec := sv.Params["ValidCause"]; spec != "" {
			cause := state.ObjID(0)
			switch ev.Kind {
			case events.MoveZone, events.Draw, events.PutOnStack:
				cause = ev.Obj
			case events.DeclareAttackers:
				if len(ev.IDs) == 1 {
					cause = ev.IDs[0]
				}
			}
			if cause == 0 || !observer.matchesSpecFrom(spec, cause, sv.Controller, sv.Source) {
				continue
			}
		}
		if sv.Params["Origin"] != "" && effects.ParseZone(sv.Params["Origin"]) != ev.From {
			continue
		}
		if sv.Params["Destination"] != "" && effects.ParseZone(sv.Params["Destination"]) != ev.To {
			continue
		}
		if spec := sv.Params["ValidSource"]; spec != "" {
			if ev.Kind != events.Damage || e.damaging == 0 ||
				!observer.matchesSpecFrom(spec, e.damaging, sv.Controller, sv.Source) {
				continue
			}
		}
		if spec := sv.Params["ValidTarget"]; spec != "" {
			switch {
			case ev.Kind == events.Damage && ev.Obj != 0:
				if !observer.matchesSpecFrom(spec, ev.Obj, sv.Controller, sv.Source) {
					continue
				}
			case ev.Kind == events.Damage:
				if !effects.MatchesPlayerSpec(g, spec, ev.Player, sv.Controller) {
					continue
				}
			case ev.Kind == events.TargetsChosen:
				// The BecomesTarget-ed object is the trigger's own source.
				if !observer.matchesSpecFrom(spec, src, sv.Controller, sv.Source) {
					continue
				}
			default:
				continue
			}
		}
		if spec := sv.Params["ValidActivator"]; spec != "" {
			if ev.Kind != events.PutOnStack || !effects.MatchesPlayerSpec(g, spec, ev.Player, sv.Controller) {
				continue
			}
		}
		if spec := sv.Params["ValidPlayer"]; spec != "" {
			if (ev.Kind != events.LifeChange && ev.Kind != events.Draw) ||
				!effects.MatchesPlayerSpec(g, spec, ev.Player, sv.Controller) {
				continue
			}
		}
		if sv.Params["CombatDamage"] == "True" && !(ev.Kind == events.Damage && e.combatDamaging) {
			continue
		}
		if spec := sv.Params["ValidTurned"]; spec != "" {
			// ValidTurned$ scopes the permanent that was turned face up
			// (Panoptic Projektor: "if turning a face-down permanent face up
			// causes ..."). It is a PAIR with ValidMode$ TurnFaceUp, and the
			// turned object is the turn-up event's own Obj -- distinct from
			// ValidCard$, which scopes the trigger's source. An event with no
			// turned permanent carries nothing the clause can match, so a
			// non-turn-up event fails closed rather than doubling.
			if ev.Kind != events.TurnFaceUp || ev.Obj == 0 ||
				!observer.matchesSpecFrom(spec, ev.Obj, sv.Controller, sv.Source) {
				continue
			}
		}
		spec := sv.Params["ValidCard"]
		if spec == "" {
			continue
		}
		if observer.matchesSpecFrom(spec, src, sv.Controller, sv.Source) {
			n++
		}
	}
	return n
}

// panharmoniconModes maps the folded causing event to the Forge trigger-mode
// family it can serve as a triggering event for -- the same event-to-mode
// correspondence trigger_match.go's per-mode matchers test one mode at a
// time, stated once here for a ValidMode$ list to check against. A MoveZone
// with the card played from hand also serves LandPlayed (landPlayedMatches
// matches the same event), the way the corpus spells multi-family statics.
func panharmoniconModes(ev events.Event) []string {
	switch ev.Kind {
	case events.MoveZone:
		out := []string{"ChangesZone", "ChangesZoneAll"}
		if ev.From == state.ZHand && ev.To == state.ZBattlefield {
			out = append(out, "LandPlayed")
		}
		return out
	case events.Draw:
		return []string{"Drawn"}
	case events.PutOnStack:
		return []string{"SpellCast", "SpellCastOrCopy"}
	case events.StackCopy:
		return []string{"SpellCopy", "SpellCastOrCopy"}
	case events.DeclareAttackers:
		return []string{"Attacks", "AttackersDeclared", "AttackersDeclaredOneTarget"}
	case events.Damage:
		return []string{"DamageDone", "DamageDealtOnce", "DamageDoneOnce", "DamageAll"}
	case events.TargetsChosen:
		return []string{"BecomesTarget", "BecomesTargetOnce"}
	case events.LifeChange:
		if ev.Amount > 0 {
			return []string{"LifeGained"}
		}
		return []string{"LifeLost"}
	case events.TurnFaceUp:
		// The turn-up marker (CR 708.6/702.36e). Only ValidMode$
		// TurnFaceUp statics -- Panoptic Projektor's "if turning a face-down
		// permanent face up causes a triggered ability ... to trigger" --
		// serve this family.
		return []string{"TurnFaceUp"}
	default:
		return nil
	}
}

// unspentManaKeep renders the pool slots whose unspent mana the CR 500.4
// boundary ManaClear must NOT empty for player p — the stat:UnspentMana
// continuous static ("You don't lose unspent <colour> mana as steps and
// phases end", Leyline Tyrant / Omnath, Locus of Mana / Upwelling). The
// answer is one letter per protected slot in fixed WUBRGC slot order
// ("R", "WU", ...); "" means every slot empties, so a game without a live
// carrier emits the historical Text-less ManaClear byte-identically.
//
// Both delivery routes are read, the same pair SacrificeBlocked walks:
// the printed S: face statics (activeStatics) and the Effect-delivered
// registration (effEffect's UnspentMana arm — The Last Agni Kai's
// `DB$ Effect | StaticAbilities$ Unspent`), the latter through the ordinary
// active() lifetime machinery so the grant ends with its source or its
// duration. A face static's "as long as" gates run through the shared
// continuousGateHolds grammar; ValidPlayer$ goes through the shared player
// spec matcher (You scopes to the static's controller, absent — Upwelling —
// protects every seat). ManaType$ is a comma-separated colour-word list
// parsed by effects.ColorLetters; absent (or an unparseable "Colorless"
// word set, the C slot) protects every slot only when the parameter is
// wholly absent. An unresolvable ManaType$ value fails closed and protects
// nothing — the shipped-statics convention.
func (e *Engine) unspentManaKeep(p state.PlayerID) string {
	keep := make([]bool, len(e.G.Players[p].Pool))
	apply := func(params map[string]string, you state.PlayerID) {
		if spec := params["ValidPlayer"]; spec != "" &&
			!effects.MatchesPlayerSpec(e.G, spec, p, you) {
			return
		}
		mt := strings.TrimSpace(params["ManaType"])
		if mt == "" {
			for i := range keep {
				keep[i] = true
			}
			return
		}
		letters, ok := effects.ColorLetters(mt)
		if !ok {
			return
		}
		if len(letters) == 0 {
			// "Colorless" — the C slot alone.
			keep[len(keep)-1] = true
			return
		}
		for _, l := range letters {
			keep[state.ManaIndex(l[0])] = true
		}
	}
	for _, sv := range e.activeStatics("UnspentMana") {
		if !e.continuousGateHolds(sv) {
			continue
		}
		apply(sv.Params, sv.Controller)
	}
	for _, ce := range e.active() {
		if ce.Restriction != "UnspentMana" {
			continue
		}
		apply(ce.RestrictParams, ce.Controller)
	}
	var out []byte
	for i, k := range keep {
		if k {
			out = append(out, manaSlotSymbols[i])
		}
	}
	return string(out)
}

// manaSlotSymbols indexes the pool slot order (state.MW..state.MC) to its
// WUBRGC letter, the encoding the ManaClear keep Text rides.
const manaSlotSymbols = "WUBRGC"

// Param is Params[k] with presence, through the view's compiled set.
func (sv staticView) Param(k cards.ParamKey) (string, bool) {
	return cards.ParamSetParam(sv.PS, sv.Params, k)
}

// ParamStr is Params[k] ("" when absent).
func (sv staticView) ParamStr(k cards.ParamKey) string {
	v, _ := cards.ParamSetParam(sv.PS, sv.Params, k)
	return v
}

// HasParam reports whether key k is present.
func (sv staticView) HasParam(k cards.ParamKey) bool {
	_, ok := cards.ParamSetParam(sv.PS, sv.Params, k)
	return ok
}
