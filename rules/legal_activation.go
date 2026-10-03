package rules

import (
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// coreCardTypes is CardType.CoreType (forge-card's CardType.java) -- the
// distinct-type census Delirium's gate counts. Kindred is included: modern
// type lines spell it as a word and the engine's printed Types carry it.
var coreCardTypes = []string{"Artifact", "Battle", "Creature", "Enchantment",
	"Instant", "Kindred", "Land", "Planeswalker", "Sorcery"}

// activationConditionOK evaluates an ability's Activation$ activation
// condition -- the "Hellbent —", "Threshold —", "Metalcraft —",
// "Delirium —" cost-prompt family (Sea Gate Wreckage's draw, Mox Opal's
// mana). Forge's SpellAbilityCondition.areMet keyword half, on the
// ACTIVATOR (the controller asking to activate), evaluated at OFFER time
// like the CheckSVar$ gate below: an ability whose condition fails is not
// offered, so a paid no-op activation is never reachable:
//
//   - Hellbent: the activator's hand is empty (Player.hasHellbent);
//   - Threshold: the activator's graveyard holds 7+ cards;
//   - Metalcraft: the activator controls 3+ artifacts;
//   - Delirium: the activator's graveyard holds 4+ distinct core card types
//     (AbilityUtils.countCardTypesFromList's non-permanent form);
//   - Blessing: the activator holds CR 702.131's city's blessing (the
//     one-way state.Player.Blessing latch rules/ascend.go's Ascend scan and
//     events.Apply's BlessingChange fold maintain). This is the gate half of
//     the city's-blessing family; Count$Blessing.<yes>.<no> (effects/count.go)
//     and the Condition$ Blessing gate read the same bit.
//
// Solved (the Case permanents' solved flag) names state this build does not
// track, so that gate FAILS CLOSED -- the conservative direction for an
// "only if" condition whose meeting cannot be verified. Blessing is the
// city's-blessing latch in state.Player and is read by the same offer-time
// gate as the other conditions. No repo-deck card carries Solved or Blessing
// (measured at the current corpus pin: 3 raw lines each, none in the decks).
func (e *Engine) activationConditionOK(p state.PlayerID, ab *cards.SA) bool {
	raw, ok := ab.Param(cards.PKActivation)
	if !ok || strings.TrimSpace(raw) == "" {
		return true
	}
	switch strings.TrimSpace(raw) {
	case "Hellbent":
		return len(e.G.Zone(state.ZHand, p)) == 0
	case "Threshold":
		return len(e.G.Zone(state.ZGraveyard, p)) >= 7
	case "Metalcraft":
		n := 0
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			if o := e.G.Obj(id); o != nil && slices.Contains(e.derivedTypesOf(id), "Artifact") {
				n++
			}
		}
		return n >= 3
	case "Blessing":
		// CR 702.131: the city's blessing, read off the same one-way latch
		// the Condition$ Blessing gate and the Count$Blessing branch head
		// read. An out-of-range activator denies -- the fail-closed
		// direction a blessing gate that cannot name its seat must take.
		return int(p) < len(e.G.Players) && !e.G.Players[p].Lost && e.G.Players[p].Blessing
	case "Delirium":
		seen := map[string]bool{}
		for _, id := range e.G.Zone(state.ZGraveyard, p) {
			if o := e.G.Obj(id); o != nil {
				for _, ty := range o.Face().Types {
					seen[ty] = seen[ty] || slices.Contains(coreCardTypes, ty)
				}
			}
		}
		n := 0
		for _, ty := range coreCardTypes {
			if seen[ty] {
				n++
			}
		}
		return n >= 4
	}
	return false
}

// activationGameTypesOK evaluates an activated ability's ActivationGameTypes$
// format list at offer time (War Room's "Activate only in a game of
// Commander, Brawl, Tiny Leaders, or Oathbreaker", the corpus's only
// carrier). The value is a comma list of the game formats the ability exists
// in; gorge models exactly two formats -- FormatCommander and
// FormatConstructed -- and only the "Commander" token maps to one. Brawl,
// TinyLeaders and Oathbreaker are not modelled and match nothing, so the
// list never admits a Constructed game: a format-gated ability is withheld
// before it could ever be announced (CR 602.1/601.2c: an illegal activation
// is not offered). Deterministic pure read -- no map range, tokens trimmed.
func activationGameTypesOK(f Format, raw string) bool {
	for tok := range strings.SplitSeq(raw, ",") {
		switch strings.TrimSpace(tok) {
		case "Commander":
			if f == FormatCommander {
				return true
			}
		}
	}
	return false
}

// abilityZoneOK reports whether ability ab may be activated while the
// source cardinal is in zone z (CR 602.1b): the printed ActivationZone$
// when present, the battlefield by default. Battlefield, Hand, Graveyard,
// Exile and Stack are enumerated by the legal-action walks (Exile since
// fuzz-cov3: Greater Gargadon's suspended sacrifice outlet; Stack since the
// Activator$ offer gate, for Lightning Storm's "Any player may activate this
// ability but only if CARDNAME is on the stack"); Command is not and
// therefore never offers an option.
// abilityZoneMask is abilityZoneOK(ab, z) for every z < 32, as bit z, from
// a single ActivationZone$ read (buildManaSAFacts' zone mask: one map lookup
// instead of one per zone).
func abilityZoneMask(ab *cards.SA) uint32 {
	az, ok := ab.Param(cards.PKActivationZone)
	if !ok {
		return 1 << state.ZBattlefield
	}
	switch strings.TrimSpace(az) {
	case "Battlefield":
		return 1 << state.ZBattlefield
	case "Graveyard":
		return 1 << state.ZGraveyard
	case "Hand":
		return 1 << state.ZHand
	case "Exile":
		return 1 << state.ZExile
	case "Stack":
		return 1 << state.ZStack
	}
	return 0
}

func abilityZoneOK(ab *cards.SA, z state.Zone) bool {
	az, ok := ab.Param(cards.PKActivationZone)
	if !ok {
		return z == state.ZBattlefield
	}
	switch strings.TrimSpace(az) {
	case "Battlefield":
		return z == state.ZBattlefield
	case "Graveyard":
		return z == state.ZGraveyard
	case "Hand":
		return z == state.ZHand
	case "Exile":
		return z == state.ZExile
	case "Stack":
		return z == state.ZStack
	}
	return false
}

// activatorAllows reports whether player p may activate ability ab, per the
// ability's Activator$ parameter (Forge's PlayerProperty on an AB$/SP$ line,
// e.g. Oft-Nabbed Goat's "Only your opponents may activate this ability" ->
// Player.Opponent, Mana Cache's "Any player may activate this ability" ->
// Player). An absent Activator$ means the source's controller and only them:
// the CR 602.2a default. A present spec is resolved source-relative -- You is
// the source's current controller, and source-dependent selectors
// (Player.EnchantedController, Player.IsRemembered) bind their attachment and
// choice state to the source permanent -- through effects.MatchesPlayerSpecFrom,
// so every rule has one home and an unknown or unread selector fails closed
// rather than admitting extra activators. Every offer path (the printed AB walk,
// the granted/gained-ability walk and the mana-ability membership walk) routes
// through this helper, so the paths cannot drift.
func (e *Engine) activatorAllows(p state.PlayerID, id state.ObjID, ab *cards.SA) bool {
	spec := strings.TrimSpace(ab.ParamStr(cards.PKActivator))
	if spec == "" {
		return e.controllerOf(id) == p
	}
	return effects.MatchesPlayerSpecFrom(e.G, spec, p, e.controllerOf(id), id)
}

// abilityPresentHolds evaluates an activated ability's IsPresent$ /
// PresentCompare$ activation gate at OFFER time -- Mistveil Plains' "Activate
// only if you control two or more white permanents" (IsPresent$
// Permanent.White+YouCtrl, PresentCompare$ GE2). The same deterministic
// battlefield count the statics' presentGate (rules/statics.go) and the
// reflected-mana offer's manaReflectedPresentHolds (rules/mana_activation.go)
// evaluate; PresentCompare$ defaults to "at least one" when it is absent, the
// shared reading everywhere else the gate appears. The gate is offer-time
// only, exactly like SorcerySpeed$/PlayerTurn$/CheckSVar$: no state can move
// between the offer and the answer inside one priority window, and the
// resolution does not re-gate.
//
// Because the gate applies to every non-mana activation (the offer loop
// skips cards.IsManaAbilityAPI abilities -- a plain AB$ Mana ability's own
// IsPresent$ gate is the mana path's business), its reads are excluded from
// the census's generic rules-side SA union for Mana/ManaReflected: see
// genericSAExcludes in paramcensus_test.go.
func (e *Engine) abilityPresentHolds(p state.PlayerID, id state.ObjID, ab *cards.SA) bool {
	if !e.classBandGateHolds(ab.ParamStr(cards.PKClassBand), id) {
		return false
	}
	spec := strings.TrimSpace(ab.ParamStr(cards.PKIsPresent))
	if spec == "" {
		return true
	}
	n := 0
	if pz := strings.TrimSpace(ab.ParamStr(cards.PKPresentZone)); pz != "" {
		// PresentZone$ (Greater Gargadon's "Activate only if this is
		// suspended": IsPresent$ Card.Self+suspended | PresentZone$ Exile)
		// counts the named zone in every living seat, the trigger clause's
		// presentZoneCount; an unknown zone word fails closed.
		zone, known := effects.ParseZoneWord(pz)
		if !known {
			return false
		}
		n = e.presentZoneCount(zone, spec, id, p)
	} else {
		n = e.countPresent(spec, id, p)
	}
	if cmp := strings.TrimSpace(ab.ParamStr(cards.PKPresentCompare)); cmp != "" {
		return comparePresent(n, e.presentCompareFor(cmp, id, p))
	}
	return n > 0
}

// adaptGateOK evaluates AB$ PutCounter's Adapt$ activation gate (CR 702.35a:
// "Activate only if this creature has no +1/+1 counters on it"). Adapt$ is
// an ability PARAMETER, not a K: keyword line, so the ability offer loop has
// to read it directly -- the same shape Boast$ takes (rules/boast_test.go's
// header). Measured corpus: 24 `A:AB$ PutCounter ... Adapt$` carriers
// (Pteramander, Incubation Druid, ... every value a literal 1-4) plus one
// chained `DB$ PutCounter | Adapt$ 3` body (Jetfire), which is governed by
// the effect's own if-condition at resolution (effects/counters.go), not by
// this activation gate. Offer-time only, exactly like the IsPresent$ /
// CheckSVar$/Boast$ gates it sits beside: no state can move between the
// offer and the answer inside one priority window.
func (e *Engine) adaptGateOK(id state.ObjID, ab *cards.SA) bool {
	if !effects.IsPutCounter(ab) || !effects.PutCounterOf(ab).AdaptSet {
		return true
	}
	o := e.G.Obj(id)
	return o != nil && o.Counter("P1P1") == 0
}

// monstrosityGateOK evaluates AB$ PutCounter's Monstrosity$ once-only gate
// (CR 701.31b: "Activate only if this creature isn't already monstrous",
// Giggling Skitterspike's `{5}: Monstrosity 5`). Monstrosity$ is an ability
// PARAMETER like Adapt$, so the offer loop reads it directly -- the same
// shape the Adapt$ gate takes. Offer-time only, exactly like the Adapt$ /
// IsPresent$ / CheckSVar$ gates it sits beside: no state can move between
// the offer and the answer inside one priority window. effects/counters.go
// keeps a resolve-time already-monstrous skip as defense-in-depth (no
// corpus shape reaches the resolution through any other door -- no granted
// or copied route for these abilities).
func (e *Engine) monstrosityGateOK(id state.ObjID, ab *cards.SA) bool {
	if !effects.IsPutCounter(ab) || !effects.PutCounterOf(ab).MonstrositySet {
		return true
	}
	o := e.G.Obj(id)
	return o != nil && !o.Monstrous
}

// sVarGateOK evaluates the ability's CheckSVar$/SVarCompare$ intervening-if
// at OFFER time: Bloodsoaked Champion's Raid ("Activate only if you attacked
// this turn", CheckSVar$ RaidTest = Count$AttackersDeclared) and Ojer
// Axonil's transformed Temple of Power ("Activate only if red sources you
// controlled dealt 4 or more noncombat damage this turn" — a count head this
// build does not model, so its gate reads 0 and the transform stays
// unoffered, the documented degrade-to-zero direction). The same evaluator
// conditionMet (ConditionCheckSVar$, effects/conditions.go) and the statics'
// checkSVarHolds delegate to: effects.CheckSVarHolds. Because the gate
// applies to every non-mana activation, its reads are the census's generic
// rules-side SA set, not any one api's.
func (e *Engine) sVarGateOK(p state.PlayerID, id state.ObjID, ab *cards.SA, merged int) bool {
	check, ok := ab.Param(cards.PKCheckSVar)
	if !ok {
		return true
	}
	o := e.G.Obj(id)
	if o == nil || o.PileFaceFor(merged) == nil {
		return false
	}
	svars := e.pileSVars(id, merged)
	ctx := effects.NewCtxPtr(id, p, effects.CtxInit{SVars: svars})
	holds, evaluated := effects.CheckSVarHolds(e, ctx, check, ab.ParamStr(cards.PKSVarCompare))
	if !evaluated {
		// The gate's count body is not one the evaluator models: fail OPEN —
		// the ability is still offered. A gate you cannot read must not
		// silently remove a card's activation (Ojer Axonil's transformed
		// Temple of Power counts noncombat damage by source this build does
		// not track; suppressing the transform on that account would brick
		// the card's mechanic on an unreadable gate).
		return true
	}
	return holds
}

// ownReduceCost evaluates an ability's own ReduceCost$ parameter (Otawara,
// Soaring City's Channel: "This ability costs {1} less to activate for each
// legendary creature you control" — ReduceCost$ X over
// SVar:X:Count$Valid Creature.Legendary+YouCtrl): the generic reduction the
// value resolves to. A literal is the value; a name (X) resolves through the
// source face's SVar table via effects.EvalCountOK — the same resolver
// fixLifeXCost uses — and an unresolvable body degrades to zero (a
// reduction this build cannot compute is never silently over-applied; the
// full-cost ability stays legal, just never discounted). The CR 601.2f
// composition: folded into the offer gate's cost AND beginActivation's
// stored cost, so the two can never disagree. Because the read applies to
// every non-mana activation, it joins the census's generic rules-side SA
// set, not one api's.
//
// targets are the chosen ROOT targets, carried on the Ctx so a target-
// dependent body (Raft Security Officer's AllTargeted$Valid
// Creature.powerLE3) can resolve; allTargets is the whole-chain union
// (alltargeted1) bound as Ctx.AllTargets, so an AllTargeted$ body reads
// Forge's union over the root/sub-ability chain (Wayta, Trainer Prodigy's
// fight) rather than the root's list alone. The offer/projection sites pass
// nil for both — targets do not exist yet at offer time, so a target-
// dependent reduction reads 0 there (full price, fail closed) — and
// repriceForTargets re-runs the evaluation with the answered targets (and
// the pre-asked sub answers) at CR 601.2c, before CR 601.2h pays.
func (e *Engine) ownReduceCost(p state.PlayerID, id state.ObjID, ab *cards.SA, targets, allTargets []state.Target, merged int) int32 {
	v := strings.TrimSpace(ab.ParamStr(cards.PKReduceCost))
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil {
		if n < 0 {
			return 0
		}
		return int32(n)
	}
	o := e.G.Obj(id)
	if o == nil || o.PileFaceFor(merged) == nil {
		return 0
	}
	svars := e.pileSVars(id, merged)
	body := v
	if b, ok := svars[v]; ok {
		body = b
	}
	ctx := effects.NewCtxPtr(id, p, effects.CtxInit{SVars: svars, Targets: targets})
	ctx.AllTargets = allTargets
	if n, ok := effects.EvalCountOK(e, ctx, body); ok && n > 0 {
		return n
	}
	return 0
}

// ownReduceManaShape parses an ability's ReduceCost$ as a MANA COST (Kami of
// Jealous Thirst's `ReduceCost$ 4 B`): the coloured pips and generic it
// removes per unit. ok is false for the numeric/SVar-name form
// ownReduceCost reads, and for any cost that is not plain mana.
func ownReduceManaShape(v string) (col state.Mana, gen int32, ok bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return col, 0, false
	}
	if _, err := strconv.Atoi(v); err == nil {
		return col, 0, false
	}
	col, gen, life, plain := raiseFromCost(v)
	if !plain || life != 0 || col.Total()+gen == 0 {
		return col, 0, false
	}
	return col, gen, true
}

// ownManaReduction composes an ability's own mana-cost ReduceCost$ into one
// reduction: the parsed pips times ReduceAmount$ (Forge's
// CostAdjustment.adjust repeats the ReduceCost$ cost that many times; absent
// means once). ReduceAmount$ is read through the same evaluator
// ownReduceCost uses (Kami's Count$Compare Y GE3.1.0 over
// Count$YouDrewThisTurn -- 1 once you have drawn three cards this turn, else
// 0), and an unreadable body reduces nothing, the fail-closed direction.
// The coloured part is a Color$-style reduction (hasColor): a {B} pip with no
// matching pip in the cost spills to generic exactly as a colour reduction
// does.
func (e *Engine) ownManaReduction(p state.PlayerID, id state.ObjID, ab *cards.SA, targets []state.Target) (costMod, bool) {
	col, gen, ok := ownReduceManaShape(ab.ParamStr(cards.PKReduceCost))
	if !ok {
		return costMod{}, false
	}
	n := int32(1)
	if raw := strings.TrimSpace(ab.Params["ReduceAmount"]); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			n = int32(v)
		} else {
			svars := e.pileSVars(id, 0)
			body := raw
			if b, found := svars[raw]; found {
				body = b
			}
			v, evaluated := effects.EvalCountOK(e, effects.NewCtxPtr(id, p, effects.CtxInit{SVars: svars, Targets: targets}), body)
			if !evaluated {
				return costMod{}, false
			}
			n = v
		}
	}
	if n <= 0 {
		return costMod{}, false
	}
	red := costMod{generic: addClampedGeneric(0, int64(gen)*int64(n))}
	for i := range col {
		red.colored[i] = addClampedGeneric(0, int64(col[i])*int64(n))
	}
	red.hasColor = col.Total() > 0
	return red, true
}

// powerUpReducedCost applies CR 702.193b to an activated Power-up ability.
// The reduction is computed from the permanent's own face mana cost and is
// active only during the turn it entered. This helper is shared by the offer
// and activation paths so the displayed/validated cost equals the charge.
func (e *Engine) powerUpReducedCost(id state.ObjID, ab *cards.SA, cost Cost) Cost {
	e.powerUpReduceCost(id, ab, &cost)
	return cost
}

// powerUpReduceCost is powerUpReducedCost applied to *cost in place, so the
// offer walk does not copy the ~800-byte Cost twice per ability.
func (e *Engine) powerUpReduceCost(id state.ObjID, ab *cards.SA, cost *Cost) {
	if ab == nil || !strings.EqualFold(strings.TrimSpace(ab.ParamStr(cards.PKPowerUp)), "True") {
		return
	}
	o := e.G.Obj(id)
	if o == nil || !o.EnteredThisTurn || o.Face() == nil {
		return
	}
	reduction := e.parseCost(o.Face().ManaCost)
	// Generic mana reduces only generic mana. Each colored/colorless symbol
	// reduces its matching symbol first; any excess of that symbol reduces
	// generic mana (CR 118.7).
	if reduction.Generic > 0 {
		n := reduction.Generic
		if n > cost.Generic {
			n = cost.Generic
		}
		cost.Generic -= n
	}
	for i := range reduction.Colored {
		n := reduction.Colored[i]
		if n <= 0 {
			continue
		}
		matched := n
		if matched > cost.Colored[i] {
			matched = cost.Colored[i]
		}
		cost.Colored[i] -= matched
		left := n - matched
		if left > cost.Generic {
			left = cost.Generic
		}
		cost.Generic -= left
	}
}

// ownReduceCostOffer is ownReduceCost's offer-time reading for a body that
// reads a ROOT target ref (Targeted$CardPower, CR 702.6's equip target). The
// ability's own targets do not exist until CR 601.2c, so a plain nil-target
// read is 0 and the offer gate would withhold the ability at FULL price even
// when a legal target reduces it into reach: Belt of Giant Strength's
// `Equip {10}` against a 4-power creature costs {6}, and a {6} pool must
// offer it. This evaluates the body once per legal root target candidate and
// returns the LARGEST reduction, so the option is offered iff SOME legal
// CR 601.2c announcement is payable; beginActivation folds the same amount
// (so the pre-target payability check in continueCast passes) and
// repriceForTargets then charges the exact amount for the target actually
// chosen (CR 601.2f, idempotent net-adjust). A body that reads no root target
// ref keeps the plain nil-target read, unchanged; the AllTargeted$ union is
// deliberately not consulted here (alltargeted1's sub-ability shape). A
// property the <Ref>$<Property> family does not model (dragonfire_blade's
// Targeted$CardNumColors) still evaluates to 0 here, so that card's offer
// price is unchanged -- only the Ctx binding is in this helper's scope.
func (e *Engine) ownReduceCostOffer(p state.PlayerID, id state.ObjID, ab *cards.SA, merged int) int32 {
	best := e.ownReduceCost(p, id, ab, nil, nil, merged)
	if ab == nil {
		return best
	}
	v := strings.TrimSpace(ab.ParamStr(cards.PKReduceCost))
	if v == "" {
		return best
	}
	if _, err := strconv.Atoi(v); err == nil {
		return best
	}
	svars := e.pileSVars(id, merged)
	body := v
	if b, ok := svars[v]; ok {
		body = b
	}
	if !bodyReadsRootTarget(body, svars, 0) {
		return best
	}
	for _, cand := range e.costPotentialTargets(p, id, abilityScope(ab)) {
		if n := e.ownReduceCost(p, id, ab, []state.Target{cand}, nil, merged); n > best {
			best = n
		}
	}
	return best
}

// manaActivateLabel is the "Activate <name> for mana" option label, built
// once per card name per Engine: the offer walk mints it for every untapped
// mana source on every priority walk, and the string is immutable, so each
// walk's options can share it.
func (e *Engine) manaActivateLabel(name string) string {
	if l, ok := e.manaLabels[name]; ok {
		return l
	}
	l := "Activate " + name + " for mana"
	if e.manaLabels == nil {
		e.manaLabels = make(map[string]string)
	}
	e.manaLabels[name] = l
	return l
}
