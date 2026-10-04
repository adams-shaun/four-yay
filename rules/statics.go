// Static play restrictions and cost modifiers: the six S: modes besides
// stat:Continuous (layers.go's own concern). These change what is legal and
// what things cost rather than what a permanent's characteristics are, so
// they hook into legalActions and ParseCost rather than the layer system.
package rules

import (
	"fmt"
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
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
	// effectStamp identifies the delivering Effect of an Effect-delivered
	// cost-modifier view (its ContinuousEffect.Timestamp, unique per
	// registration; 0 for every other view). The cost chain binds Remembered
	// for such a view and finds a pending cast's captured set by it
	// (costStaticSpecCtx).
	effectStamp uint32
}

// costRememberedEntry is one Effect-delivered cost static's Remembered set
// captured on a pending cast at beginCast (pendingCast.costRemembered).
type costRememberedEntry struct {
	source state.ObjID
	stamp  uint32
	ids    []state.ObjID
}

// costRememberedCapture returns the Remembered sets of the Effect-delivered
// cost-modifier statics that hold card id right now, in e.active() order.
func (e *Engine) costRememberedCapture(id state.ObjID) []costRememberedEntry {
	var out []costRememberedEntry
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if !costRememberedCaptureKeys1.Has(ce.CostStaticMode) {
			continue
		}
		if ce.CostStaticGranted || !slices.Contains(ce.Remembered, id) {
			continue
		}
		out = append(out, costRememberedEntry{source: ce.Source, stamp: ce.Timestamp,
			ids: append([]state.ObjID(nil), ce.Remembered...)})
	}
	return out
}

// costStaticSpecCtx is the spec context a cost static's ValidCard$ matches
// the priced object id under. An Effect-delivered view additionally binds
// the delivering Effect's captured Remembered set, so Card.IsRemembered names
// the card the Effect remembered (Soul Partition, Elite Spellbinder, Invasion
// of Gobakhan -- whose source's own memory the Effect's DBCleanup cleared),
// exactly as manaConvSpecCtx does for an Effect-delivered ManaConvert. For
// the pending cast of id the set recorded at beginCast (costRemembered)
// stands in for the live one.
func (e *Engine) costStaticSpecCtx(sv staticView, id state.ObjID) effects.SpecContext {
	sc := e.staticSpecCtx(sv)
	if sv.effectStamp == 0 {
		return sc
	}
	rem := sv.Remembered
	if pc := e.cast; pc != nil && pc.card == id {
		for _, c := range pc.costRemembered {
			if c.stamp == sv.effectStamp && c.source == sv.Source {
				rem = c.ids
				break
			}
		}
	}
	if len(rem) > 0 {
		sc.Remembered = rememberedTargets(rem)
	}
	return sc
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
	// board is the offer walk's board-wide facts (legal_walk_skip.go),
	// published by the walk before its mana sweep; zero (not ready) for
	// every other source.
	board walkBoardFacts
}

// addAbilityContinuous returns, in order, the Continuous statics of get()
// whose AddAbility$ is non-blank: the only ones the mana walk's per-object
// AddAbility$ scan (appendAvailableManaAbilitiesGate) does not skip at its
// first test, filtered once per walk instead of once per object.
func (s *actionStaticSource) addAbilityContinuous() []staticView {
	if !s.addAbilityReady {
		s.addAbility = addAbilityCarriers(s.get().continuous)
		s.addAbilityReady = true
	}
	return s.addAbility
}

// addAbilityCarriers returns, in order, the Continuous statics whose
// AddAbility$ is non-blank. It is the one filter both mana-grant membership
// sources share: the pass-scoped snapshot (addAbilityContinuous) and the
// fresh direct walk in appendAvailableManaAbilitiesGate, so the offer walk
// and the handler/guard walk cannot disagree on which grants exist.
func addAbilityCarriers(continuous []staticView) []staticView {
	var out []staticView
	for _, sv := range continuous {
		if strings.TrimSpace(sv.ParamStr(cards.PKAddAbility)) != "" {
			out = append(out, sv)
		}
	}
	return out
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
	return e.scanActionStaticsMode(false)
}

// collectAddAbilityCarriers is addAbilityCarriers(collectActionStatics()
// .continuous) without building the other lists: outside a walk the scan
// keeps only the Continuous statics carrying AddAbility$, in the same seat,
// zone and static order, so a board with no grantor allocates nothing (the
// mana path asks it once per object).
func (e *Engine) collectAddAbilityCarriers() []staticView {
	if v, ok := e.boardStaticsWalk(); ok {
		return addAbilityCarriers(v.action.continuous)
	}
	return e.scanActionStaticsMode(true).continuous
}

// scanActionStaticsMode is scanActionStatics' walk; carriersOnly keeps only
// the Continuous statics addAbilityCarriers would keep.
func (e *Engine) scanActionStaticsMode(carriersOnly bool) actionStaticViews {
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
				// CR 702.25b/d: a phased-out permanent is treated as though it
				// does not exist, so its statics do not function. Phase-out is
				// only ever set on a battlefield permanent (events.Apply's
				// PhaseOut fold, the Move fold clears it), so the same gate
				// scanActiveStatics runs applies here to every arm at once.
				if z == state.ZBattlefield && o.PhasedOut {
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
				if e.printedAbilitiesGone(o) {
					continue
				}
				for si, sn := 0, o.PileStaticCount(); si < sn; si++ {
					pst, ok := o.PileStaticAt(si)
					if !ok {
						continue
					}
					st := pst.Static
					var dst *[]staticView
					switch st.ModeKind() {
					case cards.StaticCantBeCast:
						if carriersOnly || z != state.ZBattlefield {
							continue
						}
						dst = &out.cantCast
					case cards.StaticCantBeActivated:
						if carriersOnly || z != state.ZBattlefield {
							continue
						}
						dst = &out.cantActivate
					case cards.StaticContinuous:
						// The EffectZone$ gate (the default is the battlefield,
						// so every battlefield Continuous static keeps today's
						// admission exactly): a static naming another zone is
						// collected from THAT zone here and denied from the
						// battlefield, the same gate staticEffects runs.
						if !effectZoneOK(st.ParamStr(cards.PKEffectZone), o.Zone) {
							continue
						}
						if carriersOnly && strings.TrimSpace(st.ParamStr(cards.PKAddAbility)) == "" {
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
			if o.PhasedOut {
				// CR 702.25b/d: a phased-out permanent is treated as though it
				// does not exist, so its statics do not function (and it is not
				// an affected permanent either).
				continue
			}
			// CR 708.8: a face-down permanent's printed statics do not exist
			// while it is face down (the shared gate in layers.go); CR
			// 613.1f: nor do those of one that lost all abilities. Read at
			// the first static of the mode: most permanents carry none, and
			// the gate is a pure read.
			gone, goneRead := false, false
			for si, sn := 0, o.PileStaticCount(); si < sn && !gone; si++ {
				pst, ok := o.PileStaticAt(si)
				if !ok {
					continue
				}
				st := pst.Static
				if st.Mode == mode {
					if !goneRead {
						if gone, goneRead = e.printedAbilitiesGone(o), true; gone {
							continue
						}
					}
					// The battlefield-only walk honours each static's own
					// EffectZone$: a static whose EffectZone$ excludes the
					// battlefield (Anger's graveyard-scoped haste grant) must
					// not apply while its source is on the battlefield, on
					// every mode's consumer. The default and the explicit
					// Battlefield/All values keep today's admission exactly;
					// a static naming a hidden zone is collected from there by
					// staticEffects/collectActionStatics/collectCostStatics
					// instead.
					if !effectZoneOK(st.ParamStr(cards.PKEffectZone), o.Zone) {
						continue
					}
					out = append(out, staticView{Source: id, Controller: o.Controller, Params: st.Params, PS: st.ParamSetOf(), SVars: pst.Face.SVars})
				}
			}
		}
	}
	return out
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
		Source: source,
		Layers: e.boardLayers(),
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
		// P/T skips the Derived walk the bind costs (specderived.go). One
		// cached lookup answers every textual bind test below.
		facts := specBindFacts(spec)
		if o := e.G.Obj(id); o != nil && (specDerivedVerify || facts.reads) {
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
		// The one shared bind every seam that can reach greatestPower goes
		// through: the whole comparison set reads layer-derived power, not
		// only the candidate's DerivedPower above.
		if facts.greatest {
			sc.DerivedPTs = append(sc.DerivedPTs, effects.GreatestPowerDerivedPTs(e.G, spec, e)...)
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
		if e.goadProbe == 0 && facts.goaded {
			sc.Layers.StaticGoads = e.staticallyGoaded()
		}
		// The colour predicates' layer-5 bind (layer5colors.go): a spec that
		// names a colour word reads the derived colours, so a creature a
		// continuous effect recoloured matches by what it is now. Nil on a
		// board with no live colour effect.
		if facts.colors {
			sc.Layers.DerivedColors = e.derivedColorTable()
		}
		if specDerivedVerify && !facts.reads {
			return e.verifySpecDerivedSkip(spec, id, sc)
		}
	}
	return effects.MatchesSpecCtxPtr(e.G, spec, id, &sc)
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

func init() {
	effects.RegisterNonAPI("stat:CantBeCast", "stat:CantBeActivated", "stat:CantBeCopied", "stat:RaiseCost", "stat:CastWithFlash",
		"stat:ReduceCost", "stat:AlternativeCost", "stat:OptionalCost", "stat:CantBlock", "stat:CantBlockBy",
		"stat:CantGainLife", "stat:Continuous", "stat:ManaConvert", "stat:NumLoyaltyAct",
		// cantdraw1 / cantdraw-drawlimit-cap: CR 121.6 CantDraw statics
		// (rules/replacement.go drawForbidden, consulted by applyReplacements
		// before any Draw replacement). ValidPlayer$ scopes total prohibitions
		// and DrawLimit$ per-turn count caps.
		"stat:CantDraw",
		// surveilnum1: the stat:SurveilNum static (Host.SurveilLookExtra,
		// consulted by effects' effSurveil through the shared activeStatics
		// collector). Only the literal-or-SVar Num$ value and the Optional$
		// election are read; a Num$ this build cannot price fails closed.
		"stat:SurveilNum",
		// combatrestriction1: the three combat/sacrifice restriction statics.
		// CantAttack is enforced per (attacker, defender) pair
		// (rules/combat/restrictions.go AttackBlocked, consulted by askAttackers /
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
		// pair). rules/combat/defender.go AttackAllowedThroughDefender is the
		// read, consulted through combat.CanAttackPair from the offer list, the
		// validator and the encore gate; the Effect-granted form registers as
		// a CanAttackDefender restriction through effEffect (the Assault
		// Formation shape). Only the whitelisted parameter shapes are
		// enforced (effects.CanAttackDefenderParamsReadable for the face
		// route with the shared gate grammar;
		// effects.CanAttackDefenderGrantParamsReadable for the grant route,
		// which cannot evaluate a gate and so keeps the narrower list).
		"stat:CanAttackDefender",
		// minmaxblocker1: the CR 509.1a block-count restriction static
		// (rules/combat/restrictions.go MinMaxBlockerBounds, enforced whole-declaration by
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
		"stat:CombatDamageToughness", "stat:CountersRemain",
		// IgnoreLegendRule: the CR 704.5j legend-rule exemption
		// (rules/sba.go legendGroups reads activeStatics("IgnoreLegendRule")
		// and skips exempt permanents before grouping; the static's
		// ValidCard$/Condition$ are matched through the shared static walk).
		// Proof test: rules/ignorelegendrule_test.go.
		"stat:IgnoreLegendRule",
		// TapPowerValue: the Station/Crew/Saddle value static, read by the
		// ONE value helper Engine.tapPowerValue (rules/statics.go) through
		// tapPowerValueStatics/activeStatics("TapPowerValue"). Proof tests:
		// rules/tappowervalue_test.go (TestTapPowerValueStationMechanism),
		// rules/tappowervalue_crew_test.go,
		// rules/setaudit_eoe_test.go
		// (TestSetAudit_eoe_TapestryWarden_StationUsesToughness).
		"stat:TapPowerValue",
		// CantExile: the CR 701.13 exile restriction, read by
		// Engine.exileBlocked's face-static branch
		// (rules/layers.go activeStatics("CantExile")); the Effect-
		// registered continuous branch is effects/misc.go's effEffect
		// case. Proof tests: rules/master_multiplied_restriction_test.go,
		// rules/master_multiplied_exile_test.go.
		"stat:CantExile",
		// WitherDamage: CR 702.79's static that makes all damage wither
		// (Everlasting Torment), read by Engine.witherDamageStaticActive
		// (rules/wither.go activeStatics("WitherDamage")) and consumed by
		// convertWitherDamage. Proof test:
		// rules/witherdamage_static_test.go.
		"stat:WitherDamage",
		// Activations: the per-turn activation-count ceiling, read by
		// Engine.additionalActivationLimit (rules/legal.go
		// activeStatics("Activations")) and consumed by
		// activationLimitBlocked. Proof test:
		// rules/additional_activations_test.go.
		"stat:Activations")
	// kw:MustBlock -- CR 509.1a, the ATTACKER's requirement "CARDNAME must be
	// blocked if able.", read by combat.HasMustBeBlockedKeyword (combat.DerivedHiddenFlags /
	// combat.ParseHiddenKeyword, rules/combat) and enforced by rules/combat.go
	// askBlockers/validateBlockers. The printed sentence spelling is
	// canonicalised to this head by cards/parse.go (cards/hiddenkeyword.go), so
	// the coverage walk interns a real registered keyword head instead of a
	// phantom `kw:CARDNAME must be blocked if able.` primitive. It is a
	// keyword head, a separate namespace from the blocker-oriented Mode$
	// MustBlock static; the corpus spells the attacker requirement only as the
	// sentence, never as a bare `K:MustBlock` or `KW$ MustBlock` (0 occurrences
	// at the pin), so the head cannot be confused with a native Forge keyword.
	// The obvious alternative spelling MustBeBlocked is already taken by an
	// unrelated Forge AI-hint SVar name (rules/layers.go GrantedSVar, 41
	// corpus files), so it is deliberately NOT used here.
	effects.RegisterNonAPI("kw:MustBlock", "kw:CantAttackOrBlock")
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

var costRememberedCaptureKeys1 = state.NewNameSet("RaiseCost", "ReduceCost", "SetCost")
