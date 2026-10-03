package rules

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules/trigmatch"
	"github.com/adams-shaun/gorge/state"
)

// costStaticApplies runs the gate chain one cost-modifier static must pass
// before its Amount$ is evaluated and applied. Every unimplementable
// qualifier denies (never silently over-applies): ValidTarget$ needs the
// chosen targets no offer-time composition has, so a target-conditional
// modifier is skipped; ValidSpell$ shapes this build cannot evaluate fail
// closed; a SetCost without RaiseTo$ True is not the shape this build
// implements.
func (e *Engine) costStaticApplies(sv staticView, mode string, p state.PlayerID, id state.ObjID, scope costScope, targets []state.Target, xBound bool) bool {
	ok, _ := e.costStaticGate(sv, mode, p, id, scope, targets, xBound)
	return ok
}

// costStaticGate is costStaticApplies reporting, beside the verdict, whether
// the static was denied by a TARGET-INDEPENDENT gate (indepFail): one that
// reads neither targets nor the pass's potential/announced-X mode (its
// classes, actor, ValidCard$, zone, turn, presence and condition gates, and
// the SetCost shape). Such a denial holds for every target assignment, so a
// composition retried with potential targets is denied it too. A denial by
// ValidSpell$, CheckSVar$, ValidTarget$ or Relative$ reports false.
func (e *Engine) costStaticGate(sv staticView, mode string, p state.PlayerID, id state.ObjID, scope costScope, targets []state.Target, xBound bool) (ok, indepFail bool) {
	// ValidCard$ Card.Self -- the corpus's self-cost-reduction shape ("this
	// spell costs {1} less ...") -- matches only the static's own source
	// (the Self predicate is o.ID == Source on both the compiled and the
	// text matcher path), and every gate before it is a pure,
	// target-independent read, so for any other priced object the verdict
	// is the ValidCard$ denial whatever those gates say. It is the one
	// static every other card in the zone is otherwise priced against.
	if id != sv.Source {
		if spec, has := sv.Param(cards.PKValidCard); has && spec == "Card.Self" {
			if walkSkipVerify {
				if full, _ := e.costStaticGateFull(sv, mode, p, id, scope, targets, xBound); full {
					panic(fmt.Sprintf("rules: Card.Self cost static of %d applied to obj %d", sv.Source, id))
				}
			}
			return false, true
		}
	}
	return e.costStaticGateFull(sv, mode, p, id, scope, targets, xBound)
}

// costStaticGateFull is costStaticGate's full gate chain.
func (e *Engine) costStaticGateFull(sv staticView, mode string, p state.PlayerID, id state.ObjID, scope costScope, targets []state.Target, xBound bool) (ok, indepFail bool) {
	if !e.classBandGateHolds(sv.ParamStr(cards.PKClassBand), sv.Source) {
		return false, true
	}
	if ty, ok := sv.Param(cards.PKType); ok && ty != "" && ty != scope.kind {
		return false, true
	}
	if !e.costActorMatches(sv, p) {
		return false, true
	}
	if strings.EqualFold(strings.TrimSpace(sv.ParamStr(cards.PKOnlyFirstSpell)), "True") &&
		e.onlyFirstSpellUsed(sv, p, id) {
		// OnlyFirstSpell$ (Conduit of Ruin: "The first creature spell you cast
		// each turn costs {2} less"): the reduction is spent once the
		// turn's first covered spell has been announced. See
		// onlyFirstSpellUsed for the tracking.
		return false, true
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
		if strings.Contains(spec, castSaMayPlaySource) {
			// Card.CastSa Spell.MayPlaySource (the "spell cast this way"
			// raises: Elite Spellbinder, Soul Partition, Lightstall
			// Inquisitor, Mavinda): the card-level form of the ValidSpell$
			// Spell.MayPlaySource read, answered by the same one source of
			// truth -- the cast being priced rides a may-play permission this
			// static's host granted (castRidesMayPlayOf) -- at the pre-push
			// offer and after the push alike, where the post-payment CastFlags
			// the other CastSa readers use do not exist yet.
			var alive bool
			if spec, alive = admitProvenanceAlternatives(spec, castSaMayPlaySource,
				e.castRidesMayPlayOf(p, id, sv.Source, scope)); !alive {
				return false, true
			}
		}
		spec, ok2 := e.castProvenanceAdmitsPending(spec, id, sv.Controller)
		if !ok2 {
			return false, true
		}
		if scope.kind == "Spell" && strings.Contains(spec, "Permanent") {
			// The priced object is a SPELL -- in hand/graveyard/exile at the
			// offer, on the stack at the charge -- never a battlefield
			// permanent, so Forge's `Permanent` base (a permanent card by
			// type, CR 110.4a's "permanent spell") reads the printed type,
			// exactly as the "cast a permanent spell" trigger matcher reads
			// it (trigmatch.SpellCastPermanentSpec). Beluna Grandsquall's
			// `Permanent.AdventureCard` was otherwise dead.
			spec = trigmatch.SpellCastPermanentSpec(spec)
		}
		if !e.matchesSpec(spec, id, e.costStaticSpecCtx(sv, id)) {
			return false, true
		}
	}
	if first, ok := sv.Param(cards.PKFirstForetell); ok && strings.EqualFold(strings.TrimSpace(first), "True") &&
		scope.kind == "Foretell" && e.firstForetellUsed(p) {
		return false, true
	}
	if vs, ok := sv.Param(cards.PKValidSpell); ok && !e.validSpellMatches(sv, scope, p, id, vs, targets) {
		return false, false
	}
	if az, ok := sv.Param(cards.PKAffectedZone); ok {
		// AffectedZone$ names where the priced object must be: an ability's
		// source zone, or the zone a spell is cast FROM (Forge gates the
		// cast-from zone -- Invasion of Gobakhan's "a spell cast this way
		// costs {2} more" reaches only the exiled card's cast from exile).
		zone, known := e.costAffectedZone(id, scope)
		if !known || !affectedZoneOK(az, zone) {
			return false, true
		}
	}
	if !e.costTurnGateHolds(sv) {
		return false, true
	}
	if spec, ok := sv.Param(cards.PKIsPresent); ok && !e.presentGate(sv, spec) {
		// The shared IsPresent$ gate: PresentZone$ picks the zone counted
		// (Igneous Elemental's graveyard, Forceful Cultivator's hand) and
		// PresentCompare$ the threshold (default GE1; Hour of Revelation's
		// GE10, Saiba Syphoner's EQ0 "no ... cards in your hand").
		return false, true
	}
	if !e.costConditionHolds(sv, p) {
		return false, true
	}
	if !e.checkSVarHoldsFor(sv, costSubject{p: p, id: id, ab: scope.ab}, targets) {
		return false, false
	}
	if spec, ok := sv.Param(cards.PKValidTarget); ok {
		if sv.Params["UnlessValidTarget"] == "True" {
			// UnlessValidTarget$ True inverts the test (Mavinda's "if that
			// spell doesn't target a creature you control, it costs {8}
			// more"): see costTargetsUnless for the offer-phase direction.
			if !e.costTargetsUnless(sv, spec, id, scope, targets) {
				return false, false
			}
		} else if !e.costTargetsMatch(sv, spec, targets) {
			return false, false
		}
	}
	if mode == "SetCost" && sv.Params["RaiseTo"] != "True" {
		// Forge's SetCost carries RaiseTo$ True (Trinisphere) for the
		// raise-to-N shape; any other SetCost shape is unimplemented and
		// must not silently floor the cost.
		return false, true
	}
	// Secondary$ True is NOT a gate: Forge reads CardTraitBase.isSecondary
	// only while building card text (Card.java's description walks), and
	// StaticAbilityCostChange applies a secondary cost static like any other.
	// The marker only says "this line's text is folded into another trait's
	// description" -- Assassin's Ink's enchantment half, Nahiri's equip
	// discount under its first-strike Continuous, a Class level's granted
	// reduction. No corpus cost static marked Secondary$ duplicates an
	// identically gated sibling, so applying it never double-counts.
	if sv.ParamStr(cards.PKRelative) == "True" && !(mode == "ReduceCost" && xBound) {
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
		// offer gate (potentialCostModsUsing's single candidate) and
		// at the CR 601.2c reprice (the chosen targets) alike. The probe is
		// the SAME evaluation modAmountX runs, so the gate and the amount
		// cannot disagree; an amount that resolves to zero prices as no
		// reduction, exactly what the skip produced. Anything unresolvable
		// keeps the fail-closed skip.
		//
		// THIRD EXCEPTION: a Relative$ RAISE whose amount the bound context
		// evaluates (Damping Sphere, Hum of the Radix, Fireball, Hinata,
		// Officious Interrogation) applies too. A raise can only raise the
		// price, so it is safe at every composition point: the offer prices
		// it at the announcement it can see (nil targets -- Fireball's single
		// target costs nothing extra), the potential pass at its least, and
		// the CR 601.2c reprice charges the chosen targets. A SetCost
		// Relative$ has no corpus carrier and keeps the skip.
		if mode == "SetCost" || !e.relativeAmountResolves(sv, costSubject{p: p, id: id, ab: scope.ab}, targets) {
			return false, false
		}
	}
	return true, false
}

// relativeAmountResolves reports whether a Relative$ cost-modifier static's
// Amount$ evaluates under a context bound with the cast's targets — the
// verdict (not the value) is what the target-bound costStaticApplies Relative$
// exception consumes, so the gate and the amount evaluation share one read. A plain
// integer literal stands as itself. See the call site's SECOND EXCEPTION for
// the measured blast radius (notofthisworld1).
func (e *Engine) relativeAmountResolves(sv staticView, sub costSubject, targets []state.Target) bool {
	raw := strings.TrimSpace(sv.ParamStr(cards.PKAmount))
	if raw == "" {
		return false
	}
	if _, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return true
	}
	ctx, svars, ok := e.costAmountCtx(sv, sub, 0, targets)
	if !ok {
		return false
	}
	body, found := svars[raw]
	if !found {
		body = raw
	}
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

// costTargetsUnless is the UnlessValidTarget$ True reading of a
// ValidTarget$ cost requirement: the static applies when the spell or
// ability targets NOTHING matching spec. An announcement with no targeting
// at all (an untargeted spell) can never target a match, so it applies from
// the offer on. A targeting announcement whose targets are not yet known
// (the nil-target offer phase) does not apply: whether it will is the
// target choice's to settle, and the offer is existential -- the CR 601.2c
// reprice charges it for the targets actually chosen, and a target that
// makes the cast unaffordable is withheld from the target menu
// (affordableTargetCandidates).
func (e *Engine) costTargetsUnless(sv staticView, spec string, id state.ObjID, scope costScope, targets []state.Target) bool {
	if len(targets) == 0 {
		sa := e.costTargetingSA(id, scope)
		return sa == nil || strings.TrimSpace(sa.ParamStr(cards.PKValidTgts)) == ""
	}
	return !e.costTargetsMatch(sv, spec, targets)
}

// costAffectedZone is the zone an AffectedZone$ cost gate reads for the
// priced object: an ability's source zone (a special action's -- Static
// scope, Doc Aurlock's plotting from hand -- likewise the card's own zone),
// or the zone a spell is cast from -- its current zone before CR 601.2a's push, its latest PutOnStack
// origin once it is on the stack. A stack object that was never cast (a
// copy) has no cast-from zone.
func (e *Engine) costAffectedZone(id state.ObjID, scope costScope) (state.Zone, bool) {
	o := e.G.Obj(id)
	if o == nil {
		return 0, false
	}
	if scope.kind == "Ability" || scope.kind == "Static" || o.Zone != state.ZStack {
		return o.Zone, true
	}
	if o.IsCopy {
		return 0, false
	}
	from, _, ok := e.latestCastOrigin(id)
	return from, ok
}

// costTurnGateHolds reads a cost-modifier static's turn/step window:
// PlayerTurn$ (True or You: during the static's controller's turn;
// Opponent: during another seat's -- Mental Modulation's "during your
// turn") and Phases$ (the shared phase-name parser: Closing Statement's
// "during your end step" is Phases$ End of Turn + PlayerTurn$ You). An
// unrecognised value denies, the gate chain's fail-closed direction.
func (e *Engine) costTurnGateHolds(sv staticView) bool {
	if turn := strings.TrimSpace(sv.ParamStr(cards.PKPlayerTurn)); turn != "" {
		switch turn {
		case "True", "You":
			if e.G.Active != sv.Controller {
				return false
			}
		case "Opponent":
			if e.G.Active == sv.Controller {
				return false
			}
		default:
			return false
		}
	}
	if phases := strings.TrimSpace(sv.Params["Phases"]); phases != "" {
		pp := e.parsedPhaseSpec(phases)
		if !pp.valid || !pp.set.Has(e.G.Step) {
			return false
		}
	}
	return true
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
	cond, ok := sv.Param(cards.PKCondition)
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
		if o := e.G.Obj(id); o != nil && slices.Contains(e.derivedTypesOf(id), "Artifact") {
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
	return e.checkSVarHoldsFor(sv, costSubject{}, nil)
}

// checkSVarHoldsFor is checkSVarHolds with the cost-modifier subject bound:
// the priced object (AffectedObj), the activation being priced
// (AffectedAbility -- the in-flight activation Professor Hojo's and
// Tezzeret's Count$ThisTurnActivated_ gates count) and the announced
// targets. A zero subject is the plain static read.
func (e *Engine) checkSVarHoldsFor(sv staticView, sub costSubject, targets []state.Target) bool {
	raw, ok := sv.Param(cards.PKCheckSVar)
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
	ctx := effects.NewCtxPtr(sv.Source, sv.Controller, effects.CtxInit{SVars: svars, Targets: targets})
	ctx.AffectedObj, ctx.AffectedAbility = sub.id, sub.ab
	holds, evaluated := effects.CheckSVarHolds(e, ctx, raw, sv.ParamStr(cards.PKSVarCompare))
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
// tag, its API, or the loyalty-ability classifier); Kind Static matches the
// special action being priced (staticConstraintMatches: foretelling,
// plotting, unlocking, turning face up). A constraint this build cannot
// evaluate denies — a discount that wrongly applies is a wrong cost, the
// same fail-closed direction the ValidSA$ grammar takes.
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
			if staticConstraintMatches(scope, constraint) {
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
// the same target list costTargetsMatch reads. The cast-option constraints
// read the cast mode the offer walk named and beginCast charges -- the one
// source of truth for "how is this spell being cast": Blitz (Henzie, Toolbox
// Torre), Dash (Warbringer), Buyback (Memory Crystal), isCastFaceDown (Dream
// Chisel) -- and MayPlaySource the may-play permission the "mayplay" cast
// rides (castRidesMayPlayOf). Bargain denies: this build implements no
// Bargain keyword, so no cast is ever bargained. Anything else denies.
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
	case "Dash":
		// Forge's isDash: the dash alternative cast, the "dashed" mode the
		// hand walk offers and beginCast charges (Warbringer).
		return scope.mode == "dashed"
	case "Buyback":
		// Forge's isBuyback: the cast that pays the Buyback additional cost,
		// the "buyback" mode (Memory Crystal). Like Forge, the reduction
		// applies to that cast's total cost.
		return scope.mode == "buyback"
	case "isCastFaceDown":
		// Forge's isCastFaceDown: the morph family's face-down cast (Dream
		// Chisel, Obscuring Aether).
		return modeIsCastFaceDown(scope.mode)
	case "MayPlaySource":
		// Forge's MayPlaySource: the cast rides a may-play permission whose
		// host is this static's own host (Urianger Augurelt's Play Arcanum
		// effect grants the permission AND carries the reduction).
		return e.castRidesMayPlayOf(p, id, sv.Source, scope)
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

// staticConstraintMatches checks one ValidSpell$ Static.* constraint: the
// special action being priced (Forge's static-ability SpellAbility
// properties). Foretelling is the foretell action (its own "Foretell" scope
// kind, which predates specialActionScope); Plotting is the plot action,
// which rides the cast flow under the "plot" mode (Doc Aurlock); Unlock is
// the Room unlock (Inquisitive Glimmer); MorphUp is Forge's isMorphUp -- the
// Morph and Megamorph turn-face-up (Exiled Doomsayer) -- and isTurnFaceUp its
// union with the Disguise turn-up (Harrowing Swarm's granted reduction).
// Forge's isTurnFaceUp also covers the manifest and cloak turn-ups; this
// build models no turn-up action for either, so there is nothing more for it
// to match. Anything else denies.
func staticConstraintMatches(scope costScope, constraint string) bool {
	switch strings.TrimSpace(constraint) {
	case "Foretelling":
		return scope.kind == "Foretell"
	case "Plotting":
		return scope.mode == "plot"
	case "Unlock":
		return scope.kind == "Static" && scope.mode == "unlock"
	case "MorphUp":
		return scope.kind == "Static" && scope.mode == "morphup"
	case "isTurnFaceUp":
		return scope.kind == "Static" && (scope.mode == "morphup" || scope.mode == "disguiseup")
	}
	return false
}

// abilityConstraintMatches checks one ValidSpell$ Activated.* constraint
// against an activated ability. Keyword-derived constraints (Equip, Ninjutsu,
// Cycling, ...) match the Keyword$ tag every keyword expansion carries
// (cards/keywords.go); the param-backed flags (Exhaust, PowerUp, Boast,
// Monstrosity) match the SA's own `<Name>$ True` (saFlagProperty);
// ManaAbility/!ManaAbility read the
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
	if saFlagProperty(ab, constraint) {
		return true
	}
	// Keyword-derived: the expansion's Keyword$ tag (comma list).
	for kw := range strings.SplitSeq(ab.ParamStr(cards.PKKeyword), ",") {
		if strings.EqualFold(strings.TrimSpace(kw), constraint) {
			return true
		}
	}
	return false
}

// saParamFlagProperties are the Forge SpellAbility properties that are a
// parameter ON the ability, not a keyword the ability was expanded from:
// SpellAbility.isBoast/isExhaust/isPowerUp/isMonstrosity are each
// hasParam("<Name>") (SpellAbilityProperty's "Boast"/"Exhaust"/"PowerUp"/
// "Monstrosity" branches). A script writes them as `<Name>$ True` on the A:
// line itself (Prowcatcher Specialist's Exhaust$ True, Serpent Specialist's
// PowerUp$ True), so no keyword expansion stamps a Keyword$ tag for them.
var saParamFlagProperties = [...]string{"Boast", "Exhaust", "PowerUp", "Monstrosity"}

// saFlagProperty reports whether property names one of the param-backed SA
// flags and ab carries it. Forge tests presence (hasParam); every corpus
// carrier spells the value True, and an explicit False is read as absent so
// a script can never switch the flag on by naming it off.
func saFlagProperty(ab *cards.SA, property string) bool {
	if ab == nil {
		return false
	}
	var v string
	var ok bool
	// Literal keys (one per saParamFlagProperties entry) so the param
	// census attributes each read.
	switch property {
	case "Boast":
		v, ok = ab.Param(cards.PKBoast)
	case "Exhaust":
		v, ok = ab.Param(cards.PKExhaust)
	case "PowerUp":
		v, ok = ab.Param(cards.PKPowerUp)
	case "Monstrosity":
		v, ok = ab.Param(cards.PKMonstrosity)
	}
	return ok && !strings.EqualFold(strings.TrimSpace(v), "False")
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
	v, ok := parseInt10(strings.TrimSpace(s))
	if !ok || v < 0 || v > int64(math.MaxInt32) {
		return def
	}
	return int32(v)
}

// parseInt10 is strconv.ParseInt(s, 10, 64) reporting success as a bool. A
// string that is not an optionally signed run of ASCII digits -- the only
// shape ParseInt accepts in base 10 -- is refused before ParseInt is asked,
// because ParseInt's refusal allocates a *NumError, and the cost-modifier
// readers parse an absent (empty) or non-literal parameter on every pass.
func parseInt10(s string) (int64, bool) {
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	if i == len(s) {
		return 0, false
	}
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	v, err := strconv.ParseInt(s, 10, 64)
	return v, err == nil
}
