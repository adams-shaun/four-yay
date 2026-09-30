package rules

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

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
// documented direction).
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

// altCostLabel names the nth (0-indexed) alternative-cost option for a
// spell, distinct from the base "Cast <name>" label and from each other when
// a card somehow offers more than one alternative.
func altCostLabel(name string, i int) string {
	if i == 0 {
		return "Cast " + name + " (alternative cost)"
	}
	return fmt.Sprintf("Cast %s (alternative cost %d)", name, i+1)
}
