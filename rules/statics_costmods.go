package rules

import (
	"math"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

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
func (e *Engine) modAmountX(sv staticView, sub costSubject, x int32, targets []state.Target) int32 {
	raw := strings.TrimSpace(sv.ParamStr(cards.PKAmount))
	if n, ok := parseInt10(raw); ok {
		if n < 0 {
			return 0
		}
		if n > int64(math.MaxInt32) {
			return math.MaxInt32
		}
		return int32(n)
	}
	ctx, svars, ok := e.costAmountCtx(sv, sub, x, targets)
	if !ok {
		return 0
	}
	// An SVar NAME resolves through its body on the source's face; anything
	// else is an inline Count$-class expression evaluated as written.
	body := raw
	if b, ok := svars[raw]; ok {
		body = b
	}
	// A negative evaluation (Fireball's TargetedObjects$Amount/Minus.1 with
	// no target announced yet) is no modification: a raise must never turn
	// into a discount, nor a reduction into a tax.
	if n := effects.EvalCount(e, ctx, body); n > 0 {
		return n
	}
	return 0
}

// costSubject names what a cost-modifier static is pricing: the payer, the
// priced object (the spell, or the activated ability's source) and, for an
// activation, the ability itself.
type costSubject struct {
	p  state.PlayerID
	id state.ObjID
	ab *cards.SA
	// electedOptional is the composition's CR 601.2b election: true only
	// when the scope prices the optional-cost cast variant
	// (spellScope("optionalcost")), so the amount context's
	// NumberInputs.OptionalCostElected seed makes a Count$OptionalGenericCostPaid
	// amount read its PAID branch at offer time (the card is still in hand;
	// the pay-time object flag is not folded until CR 601.2a's push). The
	// plain cast and every other scope keep the unpaid read.
	electedOptional bool
}

// newCostSubject is the ONE constructor of the costSubject a cost-static
// composition prices with: it folds the scope's optional-cost election in,
// so the gate verdict and the amount, which share the one amount context,
// can never disagree about which branch of a Count$OptionalGenericCostPaid
// amount is live. "optionalcost" is a spell-cast mode only (the offer walks'
// spellScope), so the castModeCodes row alone names it.
func newCostSubject(p state.PlayerID, id state.ObjID, scope costScope) costSubject {
	return costSubject{p: p, id: id, ab: scope.Ab,
		electedOptional: castModeCodes.Code(scope.Mode) == castModeOptionalcost}
}

// costAmountCtx is the ONE evaluation context a cost-modifier static's
// Amount$ reads (modAmountX, and relativeAmountResolves' verdict), so the
// gate and the amount can never disagree. The source's SVar table (the
// static's own, else the face's) with a pending cast's named announcement
// bound; the static's controller as You -- except for a Relative$ static,
// whose amount Forge computes relative to the spell being cast
// (StaticAbilityCostChange evaluates it against the paid SpellAbility), so
// You is the PAYER: Hum of the Radix's "each artifact its controller
// controls" and Damping Sphere's "each other spell that player has cast this
// turn" read the caster's board. The priced object rides AffectedObj
// (Cemetery Prowler's AffectedX) and the activation AffectedAbility.
func (e *Engine) costAmountCtx(sv staticView, sub costSubject, x int32, targets []state.Target) (*effects.Ctx, map[string]string, bool) {
	o := e.G.Obj(sv.Source)
	if o == nil || o.Face() == nil {
		return nil, nil, false
	}
	svars := sv.SVars
	if svars == nil {
		svars = o.Face().SVars
	}
	// A pending cast's named announcement (the March cycle's Exiled,
	// Explosive Singularity's Tapped) binds the SVar its name spells.
	svars = e.namedAnnounceSVars(sv.Source, svars)
	you := sv.Controller
	if sv.ParamStr(cards.PKRelative) == "True" && sub.id != 0 {
		you = sub.p
	}
	// An Effect-delivered cost static carries its SetChosenNumber$ binding
	// (chosenNumberBound): the Count$ChosenNumber head reads it rather than
	// the source object's own logged choice. The pending cast's CR 601.2b
	// optional-cost election rides the same inputs (sub.electedOptional):
	// the Count$OptionalGenericCostPaid amount head reads its paid branch at
	// offer time instead of the unpaid object flag the hand card still has.
	ctx := effects.NewCtxPtr(sv.Source, you, effects.CtxInit{SVars: svars, X: x,
		Num: effects.NumberInputs{Chosen: sv.ChosenNumber, ChosenBound: sv.chosenNumberBound,
			OptionalCostElected: sub.electedOptional}, Targets: targets})
	ctx.AffectedObj, ctx.AffectedAbility = sub.id, sub.ab
	return ctx, svars, true
}

// raiseFromCost parses a RaiseCost Cost$ into its mana and life raise. Only
// the plain shapes apply: single colour letters, numeric tokens, and the
// fixed PayLife<N> token. Anything else — hybrid pips (none in the corpus's
// cost raises), X/T, or a <...> component (Sac<...>, BeholdExile,
// Waterbend, AddCounter, tapXType, a named count) — reports false and is
// priced by the additional-cost bridge instead (composeRaiseCost).
func raiseFromCost(s string) (col state.Mana, gen, life int32, ok bool) {
	for toks := newCostTokenIter(s); ; {
		sym, more := toks.Next()
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
			if digits, ok := matchPayLife(sym); ok {
				n, err := strconv.ParseInt(digits, 10, 64)
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
	e.invalidateScratchLayerLists()
	mods := compose()
	e.costCompositionEvent = previous
	e.retireCrossWalkMemo()
	e.invalidateScratchLayerLists()
	return mods
}

// potentialCostModsUsing prices a COMPLETE composition for one legal target
// assignment at a time, accepting only when an announcement satisfies the
// caller's payment gate. A target-count amount must see at most TargetMax$
// targets, while independent conditional statics must not combine reductions
// from different, mutually exclusive single-target choices. Target-dependent
// raises/floors remain excluded; chosen targets are repriced before payment.
func (e *Engine) potentialCostModsUsing(statics costStaticViews, p state.PlayerID, id state.ObjID, scope costScope, targets []state.Target, x int32, accept func(costMods) bool) (costMods, bool) {
	max := len(e.costAmountTargets(p, id, scope, targets))
	for i, target := range targets {
		if max == 0 {
			break
		}
		assignment := []state.Target{target}
		for j, other := range targets {
			if len(assignment) == max {
				break
			}
			if i != j {
				assignment = append(assignment, other)
			}
		}
		var mods costMods
		if x != 0 {
			mods = e.costModifiersWithTargetsXUsing(statics, p, id, scope, assignment, true, x)
		} else {
			mods = e.costModifiersWithTargetsUsing(statics, p, id, scope, assignment, true)
		}
		if accept(mods) {
			return mods, true
		}
	}
	return costMods{}, false
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
	return e.costModifiersCompose(statics, p, id, scope, targets, potential, x, nil)
}

// costModifiersCompose is costModifiersWithTargetsXUsing; a non-nil mayApply
// is set when some raise/reduce/set member was NOT denied by a
// target-independent gate (costStaticGate) -- false means no member can
// apply under any target assignment.
func (e *Engine) costModifiersCompose(statics costStaticViews, p state.PlayerID, id state.ObjID, scope costScope, targets []state.Target, potential bool, x int32, mayApply *bool) costMods {
	// Each pass owns the provenance capture: cleared here, set by
	// costStaticApplies when a ValidCard$ carries a cast-provenance token.
	e.CostProvenanceSeen = false
	// The potential pass hands the whole candidate census to the gate chain
	// (so ValidTarget$/ValidSpell$ can match ANY candidate) but a
	// target-relative Amount$ reads only a complete legal assignment
	// (costAmountTargets).
	amountTargets := targets
	if potential {
		amountTargets = e.costAmountTargets(p, id, scope, targets)
	}
	sub := newCostSubject(p, id, scope)
	var mods costMods
	xBound := x != 0
	// An activated ability's OWN mana-cost ReduceCost$ (Kami of Jealous
	// Thirst's "costs {4}{B} less", Flying Drone's {1}{U}) is a reduction
	// with coloured pips, composed here beside the statics' so every
	// pricing site applies it; the numeric/SVar form stays ownReduceCost's.
	if scope.Kind == "Ability" && scope.Ab != nil {
		if red, ok := e.ownManaReduction(p, id, scope.Ab, targets); ok {
			mods.Reduces = append(mods.Reduces, red)
		}
	}
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
				if sv.HasParam(cards.PKValidTarget) {
					continue
				}
			}
			if ok, indepFail := e.costStaticGate(sv, mode, p, id, scope, targets, xBound); !ok {
				if !indepFail && mayApply != nil {
					*mayApply = true
				}
				continue
			}
			if mayApply != nil {
				*mayApply = true
			}
			if mode == "RaiseCost" {
				// A Relative$ raise scales with the announcement (Fireball's
				// "{1} more for each target beyond the first", Hinata's "for
				// each target they have"). The potential pass is the offer's
				// "does SOME legal announcement exist" relaxation, so it
				// prices the raise at its least -- no announced target --
				// exactly as it leaves every other target-conditional raise
				// out; the chosen targets are repriced before payment.
				raiseTargets := amountTargets
				if potential && sv.ParamStr(cards.PKRelative) == "True" {
					raiseTargets = nil
				}
				// A RaiseCost Cost$ names the whole additional cost (Forge's
				// CostAdjustment RaiseCost branch): a plain mana/life cost
				// is raised as-is, and every other Cost$ is carried as an
				// ADDITIONAL cost (composeRaiseCost, rules/raise_cost_extra.go)
				// so the offer gate, the cast-flow stages and the settle all
				// price the same parts. A Cost$ paired with an Amount$ is that
				// cost paid Amount$ times (Officious Interrogation's "{W}{U}
				// more for each target beyond the first").
				amount := e.modAmountX(sv, sub, x, raiseTargets)
				if e.composeRaiseCost(&mods, sv, id, scope, x, targets, amount) {
					continue
				}
				mods.Raises = append(mods.Raises, amount)
				continue
			}
			red := costMod{
				IgnoreGeneric: sv.ParamStr(cards.PKIgnoreGeneric) == "True",
				Floor:         parseAmount(sv.ParamStr(cards.PKMinMana), 0),
			}
			if col, ok := sv.Param(cards.PKColor); ok && strings.TrimSpace(col) != "" {
				// Each listed token is reduced by the Amount$: colour letters
				// take their pip from the cost's coloured part, and a numeric
				// token names that many generic pips.  Numeric is deliberately
				// not limited to "1": Discontinuity's real `Color$ 2 U U`
				// removes two generic and two blue pips.  Treating `2` as a
				// colour letter would route it through ManaIndex and remove one
				// colourless pip instead.  Amount$ applies to every token, so
				// `Color$ 2 U | Amount$ X` means 2*X generic plus X blue.
				red.HasColor = true
				amount := e.modAmountX(sv, sub, x, amountTargets)
				for tok := range strings.FieldsSeq(col) {
					if isDigitRun(tok) {
						n, err := strconv.ParseInt(tok, 10, 64)
						if err != nil || n < 0 || n > int64(math.MaxInt32) {
							continue // malformed Color$ token fails closed
						}
						red.Generic = addClampedGeneric(red.Generic, n*int64(amount))
						continue
					}
					if len(tok) == 1 && strings.ContainsRune("WUBRGC", rune(tok[0])) {
						red.Colored[state.ManaIndex(tok[0])] = addClampedGeneric(
							red.Colored[state.ManaIndex(tok[0])], int64(amount))
					}
				}
			} else {
				red.Generic = e.modAmountX(sv, sub, x, amountTargets)
			}
			mods.Reduces = append(mods.Reduces, red)
		}
	}
	for _, sv := range statics.set {
		if potential {
			if sv.HasParam(cards.PKValidTarget) {
				continue
			}
		}
		if ok, indepFail := e.costStaticGate(sv, "SetCost", p, id, scope, targets, xBound); !ok {
			if !indepFail && mayApply != nil {
				*mayApply = true
			}
			continue
		}
		if mayApply != nil {
			*mayApply = true
		}
		if n := e.modAmountX(sv, sub, x, amountTargets); n > mods.SetFloor {
			mods.SetFloor = n
		}
	}
	return mods
}

func (e *Engine) costModifiersWithTargets(p state.PlayerID, id state.ObjID, scope costScope, targets []state.Target, potential bool) costMods {
	return e.costModifiersWithTargetsUsing(e.collectCostStatics(), p, id, scope, targets, potential)
}

func (e *Engine) costModifiersWithTargetsUsing(statics costStaticViews, p state.PlayerID, id state.ObjID, scope costScope, targets []state.Target, potential bool) costMods {
	// The unannounced composition IS the announced-X one at X = 0 (the offer
	// gate's and the option snapshot's reading): one body, so a gate or an
	// amount rule added to either can never half-apply.
	return e.costModifiersWithTargetsXUsing(statics, p, id, scope, targets, potential, 0)
}

// optionalCostViews returns self-spell OptionalCost statics in collector order.
// These are deliberately narrower than the general cost-modifier grammar: the
// supported corpus shape is an EffectZone$ All self static on the spell face.
func (e *Engine) optionalCostViews(statics costStaticViews, p state.PlayerID, id state.ObjID) []Cost {
	var out []Cost
	for _, sv := range statics.optional {
		if strings.TrimSpace(sv.ParamStr(cards.PKValidSA)) != "Spell" ||
			strings.TrimSpace(sv.ParamStr(cards.PKEffectZone)) != "All" ||
			!strings.Contains(sv.ParamStr(cards.PKValidCard), "Card.Self") ||
			sv.Source != id || !e.costStaticApplies(sv, "OptionalCost", p, id, spellScope(""), nil, false) {
			continue
		}
		c := ParseCost(sv.ParamStr(cards.PKCost))
		if len(c.Unknown) == 0 {
			out = append(out, c)
		}
	}
	return out
}
