// CantAttackUnless (the attack-prop static, CR 508.1g attack costs).
//
// `S:Mode$ CantAttackUnless | ValidCard$ ... | Target$ ... | Cost$ N` is the
// "creatures can't attack you unless their controller pays {N} for each
// creature they control that's attacking you" family (Ghostly Prison,
// Windborn Muse, Propaganda, Baird, ...). Before this file the mode was a
// bare Note: nothing read it, so the cards did not even stop attacks.
//
// The reading, one model for every carrier: a static whose ValidCard$ admits
// the attacking creature and whose Target$ admits the defender (absent = every
// defender, the same scoping attackBlocked gives CantAttack's Target$) charges
// the attacking creature's controller a composite price PER ATTACKING
// CREATURE the static matches: mana (a literal, a Count$/SVar expression),
// life (PayLife<N>), tapXType<N/Spec> obligations, Sac<N/Spec>/Return<N/Spec>
// permanent obligations, and Phyrexian pips ({W/P}, payable with the colour
// or two life, CR 107.4f). Several statics on one defender sum. The charge is
// paid during the declare-attackers step (CR 508.1: costs paid as attackers
// are declared): the floating pool first, then a payment window
// (rules/attack_cost.go's startAttackPay, the CR 601.2g payment-window
// discipline rules/ward.go already runs for ward) that taps mana sources and,
// for a Phyrexian pip both branches of which are affordable, asks the real
// colour-versus-life election.
//
// A static this build cannot price fails closed: the matching pair is marked
// unpriceable and not offered, rather than letting the creature attack or
// block for free. Unsupported grammar is reported as a remaining deviation,
// never silently degraded to phantom generic mana.
package rules

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// chooseAttackPay is the declare-attackers attack-cost payment window
// (attackPayAnswer). chooseEnlist is 27; this is the next free value.
const chooseAttackPay chooseFor = chooseEnlist + 1

// chooseUnleash occupies chooseAttackPay+1; keep this transient window above
// the fixed untap selector (40).
const chooseBlockPay chooseFor = 41

// cantAttackUnlessParamsReadable is the parameter whitelist a face
// CantAttackUnless static must pass before this build enforces it. It
// delegates to effects.CantAttackUnlessRestrictionParamsReadable -- the ONE
// whitelist the two delivery routes (effEffect's restriction case and
// registerAnimateStaticAbilities' staticAbilities$ grant) and this pricing
// read share, so no path can disagree about what is readable. The gate
// parameters (IsPresent$/IsPresent2$/CheckSVar$/SVarCompare$/Condition$) are
// evaluated by the shared continuousGateHolds grammar; every other parameter
// fails the whitelist and the static is skipped permissively.
//
// RememberingAttacker$ True is readable: attackUnlessCharge binds the
// attacking creature into the pricing context as Remembered, which is what
// the Remembered$CardCounters.ALL SVar body (Nils, Discipline Enforcer)
// resolves against. It is meaningful only for the attack direction, so
// blockPairCharge deliberately prices with a zero attacker.
func cantAttackUnlessParamsReadable(params map[string]string) bool {
	return effects.CantAttackUnlessRestrictionParamsReadable(params)
}

// attackUnlessCharge prices one static's Cost$ into a composite charge: a
// literal integer as generic mana; an inline Count$ expression or an SVar
// name on the static's own face through the shared effects.EvalCountOK
// grammar bound to the static's source (so Sphere of Safety's Count$Valid
// Enchantment.YouCtrl and Cowed by Wisdom's Count$ValidHand Card.YouOwn both
// read "you" as the static's controller, and a Card.Self shape such as Myr
// Prototype's Count$CardCounters.P1P1 reads the static's own source); and the
// shared cost-token grammar (chargeFromCost) for Sac/Return/tapXType/PayLife
// and Phyrexian components. A cost this resolver cannot price (a hybrid, an
// announced Sac<X>, an unresolvable SVar, ...) returns ok=false and the
// matching pair is marked unpriceable, so an unsupported shape never lets a
// creature attack or block for free.
// The charge is re-derived per ATTACKER so a RememberingAttacker$ static can
// read the creature the charge is for. attacker is the attacking creature
// whose pair is being priced, or 0 for the block direction (a CantBlockUnless
// static has no RememberingAttacker$ carrier in the corpus).
func (e *Engine) attackUnlessCharge(sv staticView, attacker state.ObjID) (blockCharge, bool) {
	raw := strings.TrimSpace(sv.Params["Cost"])
	if raw == "" {
		return blockCharge{}, false
	}
	if n, err := strconv.Atoi(raw); err == nil {
		if n < 0 {
			return blockCharge{}, false
		}
		return blockCharge{mana: int32(n)}, true
	}
	ctx := &effects.Ctx{Source: sv.Source, Controller: sv.Controller, SVars: sv.SVars,
		ChosenNumber: sv.ChosenNumber, ChosenNumberBound: sv.chosenNumberBound}
	// RememberingAttacker$ True makes the attacking creature the resolution's
	// Remembered referent (Forge's CostRememberingAttacker convention), so an
	// SVar body such as Nils's `Remembered$CardCounters.ALL` prices the charge
	// off THAT creature's counters rather than the static's own source.
	if attacker != 0 && strings.EqualFold(strings.TrimSpace(sv.Params["RememberingAttacker"]), "True") {
		ctx.Remembered = []state.Target{{Obj: attacker}}
	}
	if strings.HasPrefix(raw, "Count$") {
		n, ok := effects.EvalCountOK(e, ctx, raw)
		if !ok || n < 0 {
			return blockCharge{}, false
		}
		return blockCharge{mana: n}, true
	}
	if body, ok := sv.SVars[raw]; ok {
		n, ok2 := effects.EvalCountOK(e, ctx, body)
		if !ok2 || n < 0 {
			return blockCharge{}, false
		}
		return blockCharge{mana: n}, true
	}
	return e.chargeFromCost(e.parseCost(raw), sv.Source)
}

// attackPairCharge prices the (attacker, defender) pair: the total charge the
// attacker's controller must pay for that creature attacking defender, the
// sum of every live face CantAttackUnless static that admits the pair and
// prices PLUS every delivered one: an Effect's StaticAbilities$ CantAttackUnless
// registration (Sivitri, Dragon Master's +1, Forbidding Spirit, Summon:
// Yojimbo, War Tax) and an Animate's staticAbilities$ grant (Whipgrass
// Entangler) register into the continuous-effect registry with the granting
// ability's SVar table, and the registry walk below consults them beside the
// printed statics with the same read. It is a pure read (no event, no state
// write) and deterministic (both walks are the fixed scans every static
// consumer shares), so the offer list, the requirement solver, the validator
// and the payer all re-derive the same charge.
func (e *Engine) attackPairCharge(id state.ObjID, defender state.PlayerID, attacked state.ObjID) blockCharge {
	total := blockCharge{}
	for _, sv := range e.activeStatics("CantAttackUnless") {
		if !cantAttackUnlessParamsReadable(sv.Params) {
			continue
		}
		if !e.continuousGateHolds(sv) {
			continue
		}
		spec := sv.Params["ValidCard"]
		if spec == "" {
			// Every corpus carrier states ValidCard$; a static without one
			// has no readable shape here. Skip, never blanket.
			continue
		}
		if !e.matchesSpec(spec, id, e.specCtxSVars(sv.Source, sv.Controller, sv.SVars)) {
			continue
		}
		if !restrictionPlayerTargetMatches(e.G, sv.Params["Target"], defender, sv.Controller, sv.Source, nil, attacked) {
			continue
		}
		ch, ok := e.attackUnlessCharge(sv, id)
		if !ok {
			// The static matches the pair but its Cost$ is a shape this build
			// cannot price. Fail CLOSED: the pair is marked unpriceable and the
			// offer/affordability gates reject it, rather than skipping the
			// static and letting the creature attack for free.
			total.unpriceable = true
			continue
		}
		total = total.plus(ch)
	}
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Restriction != "CantAttackUnless" {
			continue
		}
		sv := staticView{Source: ce.Source, Controller: ce.Controller,
			Params: ce.RestrictParams, SVars: ce.RestrictSVars,
			ChosenNumber: ce.ChosenNumber, chosenNumberBound: true}
		if !cantAttackUnlessParamsReadable(sv.Params) || !e.continuousGateHolds(sv) {
			continue
		}
		spec := sv.Params["ValidCard"]
		if spec == "" {
			continue
		}
		sc := e.specCtx(ce.Source, ce.Controller)
		for _, r := range ce.Remembered {
			sc.Remembered = append(sc.Remembered, state.Target{Obj: r})
		}
		if !e.matchesSpec(spec, id, sc) {
			continue
		}
		if !restrictionPlayerTargetMatches(e.G, sv.Params["Target"], defender, ce.Controller, ce.Source, nil, attacked) {
			continue
		}
		if ch, ok := e.attackUnlessCharge(sv, id); ok {
			total = total.plus(ch)
		} else {
			// Matching delivered static, unpriceable Cost$: fail closed (see
			// the printed walk above).
			total.unpriceable = true
		}
	}
	return total
}

// blockTapReq is one tapXType component of a combat charge: tap n untapped
// permanents matching spec, with the spec resolved relative to the static's
// source (costCandidates' matchesSpecFrom binding).
type blockTapReq struct {
	n      int32
	spec   string
	source state.ObjID
}

// chargeObjReq is one Sac<N/Spec> or Return<N/Spec> component of a combat
// charge: sacrifice / return to its owner's hand n permanents matching spec,
// with the spec resolved relative to the static's source.
type chargeObjReq struct {
	n      int32
	spec   string
	source state.ObjID
}

// blockCharge is the composite per-(blocker, attacker) combat charge: the
// mana (the flat/Count$/SVar prices, the only component the original
// mana-only model had), the life a PayLife<N> or Phyrexian component
// charges (CR 118.3 / 107.4f), the tapXType<N/Spec> tap obligations, the
// Sac<N/Spec>/Return<N/Spec> permanent obligations, and the Phyrexian pips.
// The components SUM over every matching static, printed or delivered.
// Attack and block share the type: an attack offer's charge and a block
// pair's charge are priced by the same grammar.
type blockCharge struct {
	mana int32
	life int32
	taps []blockTapReq
	// sacs are Sac<N/Spec> obligations: sacrifice n matching permanents.
	sacs []chargeObjReq
	// returns are Return<N/Spec> obligations: return n matching permanents to
	// their owner's hand.
	returns []chargeObjReq
	// phyrexian holds one colour letter per Phyrexian pip ({W/P} is 'W'),
	// each payable with one mana of that colour OR two life (CR 107.4f).
	phyrexian []byte
	// unpriceable is set when a static that MATCHES the pair charges a Cost$
	// this build cannot price (a hybrid, an announced Sac<X>, a dynamic tap
	// head, ...). Such a pair is never offered and never settles: the brief's
	// fail-closed direction. It is deliberately NOT a zero() component -- a
	// caller that only reads the priced parts keeps working -- every offer and
	// affordability gate tests it explicitly.
	unpriceable bool
}

func (c blockCharge) zero() bool {
	return !c.unpriceable && c.mana == 0 && c.life == 0 && len(c.taps) == 0 &&
		len(c.sacs) == 0 && len(c.returns) == 0 && len(c.phyrexian) == 0
}

// payable reports whether the charge may be offered and settled at all: a
// priceable charge or the zero charge (a free pair). An unpriceable charge is
// never payable, which is how a matching static this build cannot price fails
// closed instead of letting the creature attack or block for free.
func (c blockCharge) payable() bool { return !c.unpriceable }

func (c blockCharge) plus(o blockCharge) blockCharge {
	out := c
	out.mana += o.mana
	out.life += o.life
	out.taps = append(out.taps, o.taps...)
	out.sacs = append(out.sacs, o.sacs...)
	out.returns = append(out.returns, o.returns...)
	out.phyrexian = append(out.phyrexian, o.phyrexian...)
	out.unpriceable = out.unpriceable || o.unpriceable
	return out
}

// chargeFromCost turns a parsed Cost$ body into a combat charge. It accepts
// ONLY the components a declare-attackers/declare-blockers static can price:
// generic mana, the tapXType obligation, the Sac/Return permanent
// obligations, PayLife, and Phyrexian pips (pay the colour or two life). Any
// other component -- a hybrid, a {T}, an {X}, a Snow/Energy/Discard/Exile/
// Reveal/Behold/Blight/Draw/Counter part, a plain coloured pip, or an
// unrecognised token -- leaves the whole cost unpriced (ok=false), so the
// matching combat pair fails closed; the build never degrades an unmodelled
// cost to phantom generic mana. Phyrexian
// pips ARE modelled (as the life-or-colour component); the mana component is
// generic only, and a plain coloured pip fails closed rather than being
// promised as generic.
func (e *Engine) chargeFromCost(c Cost, source state.ObjID) (blockCharge, bool) {
	if len(c.Hybrid) > 0 || len(c.Twobrid) > 0 || len(c.HybridPhyrexian) > 0 ||
		c.Tap || c.Snow > 0 || c.X > 0 || c.Forage || c.LifeHalfUp ||
		len(c.Unknown) > 0 || len(c.Discard) > 0 || len(c.SubCounter) > 0 ||
		len(c.AddCounter) > 0 || len(c.Exile) > 0 || len(c.ExileFromTop) > 0 || len(c.Reveal) > 0 ||
		len(c.RevealOrChoose) > 0 ||
		len(c.RevealChosen) > 0 || len(c.Behold) > 0 || len(c.Blight) > 0 ||
		len(c.Draw) > 0 || len(c.Energy) > 0 || len(c.LifeX) > 0 ||
		len(c.PutToLib) > 0 || len(c.DamageYou) > 0 || len(c.GainLife) > 0 ||
		len(c.MoveToGrave) > 0 || len(c.Mill) > 0 || len(c.Exert) > 0 {
		return blockCharge{}, false
	}
	if c.Colored.Total() > 0 {
		// A plain coloured pip: this build's combat-cost payment window
		// prices only generic mana and Phyrexian life-or-colour, so a
		// coloured pip fails closed rather than being promised as generic.
		// No corpus combat static carries one.
		return blockCharge{}, false
	}
	var ch blockCharge
	ch.mana = c.Generic
	ch.life = c.Life
	for _, part := range c.TapPermanent {
		if part.Dyn != "" || part.N <= 0 {
			return blockCharge{}, false
		}
		ch.taps = append(ch.taps, blockTapReq{n: part.N, spec: part.Spec, source: source})
	}
	for _, part := range c.Sac {
		if part.Announced || part.N <= 0 {
			return blockCharge{}, false
		}
		ch.sacs = append(ch.sacs, chargeObjReq{n: part.N, spec: part.Spec, source: source})
	}
	for _, part := range c.Return {
		if part.N <= 0 {
			return blockCharge{}, false
		}
		ch.returns = append(ch.returns, chargeObjReq{n: part.N, spec: part.Spec, source: source})
	}
	ch.phyrexian = append(ch.phyrexian, c.Phyrexian...)
	return ch, true
}

// blockUnlessCharge prices one CantBlockUnless static's Cost$ into a
// composite blockCharge, the block-side sibling of attackUnlessCharge: a
// literal integer directly; an inline Count$ expression or an SVar name on
// the static's own face (with the Effect's frozen ChosenNumber binding when
// the view carries one, so War Cadence's Cost$ XChosen reads
// Count$ChosenNumber); then the shared cost-token grammar
// (chargeFromCost) for PayLife, tapXType, Sac, Return and Phyrexian
// components. Anything else -- a dynamic tapXType head (X/Any), a hybrid, an
// unresolvable SVar -- returns ok=false, marking the matching pair
// unpriceable rather than allowing a free block.
func (e *Engine) blockUnlessCharge(sv staticView) (blockCharge, bool) {
	raw := strings.TrimSpace(sv.Params["Cost"])
	if raw == "" {
		return blockCharge{}, false
	}
	ctx := &effects.Ctx{Source: sv.Source, Controller: sv.Controller, SVars: sv.SVars,
		ChosenNumber: sv.ChosenNumber, ChosenNumberBound: sv.chosenNumberBound}
	if n, err := strconv.Atoi(raw); err == nil {
		if n < 0 {
			return blockCharge{}, false
		}
		return blockCharge{mana: int32(n)}, true
	}
	if strings.HasPrefix(raw, "Count$") {
		n, ok := effects.EvalCountOK(e, ctx, raw)
		if !ok || n < 0 {
			return blockCharge{}, false
		}
		return blockCharge{mana: n}, true
	}
	if body, ok := sv.SVars[raw]; ok {
		n, ok2 := effects.EvalCountOK(e, ctx, body)
		if !ok2 || n < 0 {
			return blockCharge{}, false
		}
		return blockCharge{mana: n}, true
	}
	return e.chargeFromCost(e.parseCost(raw), sv.Source)
}

// blockStaticMatches evaluates one CantBlockUnless static's two combat
// specs against their respective combat objects: ValidCard$ against the
// BLOCKER, Attacker$ against the ATTACKER; an absent spec is unconditional
// on that half (Awesome Presence scopes only by Attacker$ and still prices
// every blocker against its enchanted attacker; War Cadence scopes neither
// and prices every blocker in the game).
func (e *Engine) blockStaticMatches(sv staticView, sc effects.SpecContext, blocker, attacker state.ObjID) bool {
	if spec := sv.Params["ValidCard"]; spec != "" && !e.matchesSpec(spec, blocker, sc) {
		return false
	}
	if spec := sv.Params["Attacker"]; spec != "" && !e.matchesSpec(spec, attacker, sc) {
		return false
	}
	return true
}

// blockPairCharge is the per (blocker, attacker) composite charge from
// block-prop statics, the sum of every live face static that admits the
// pair and prices PLUS every delivered one: an Effect's
// StaticAbilities$ CantBlockUnless registration (War Cadence) and an
// Animate's staticAbilities$ grant (Whipgrass Entangler) register into the
// continuous-effect registry with the granting ability's SVar table, and
// the registry walk below consults them beside the printed statics with the
// same absent-spec-is-unconditional read. It is a pure read (no event, no
// state write) and deterministic (both walks are the fixed scans every
// static consumer shares), so the offer list, the validator, the bot guard
// and the payer all re-derive the same charge.
func (e *Engine) blockPairCharge(blocker, attacker state.ObjID) blockCharge {
	total := blockCharge{}
	for _, sv := range e.activeStatics("CantBlockUnless") {
		if !cantAttackUnlessParamsReadable(sv.Params) || !e.continuousGateHolds(sv) {
			continue
		}
		if !e.blockStaticMatches(sv, e.specCtxSVars(sv.Source, sv.Controller, sv.SVars), blocker, attacker) {
			continue
		}
		if ch, ok := e.blockUnlessCharge(sv); ok {
			total = total.plus(ch)
		} else {
			// Matching static, unpriceable Cost$: fail closed (see
			// attackPairCharge).
			total.unpriceable = true
		}
	}
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Restriction != "CantBlockUnless" {
			continue
		}
		sv := staticView{Source: ce.Source, Controller: ce.Controller,
			Params: ce.RestrictParams, SVars: ce.RestrictSVars,
			ChosenNumber: ce.ChosenNumber, chosenNumberBound: true}
		if !cantAttackUnlessParamsReadable(sv.Params) || !e.continuousGateHolds(sv) {
			continue
		}
		sc := e.specCtx(ce.Source, ce.Controller)
		for _, r := range ce.Remembered {
			sc.Remembered = append(sc.Remembered, state.Target{Obj: r})
		}
		if !e.blockStaticMatches(sv, sc, blocker, attacker) {
			continue
		}
		if ch, ok := e.blockUnlessCharge(sv); ok {
			total = total.plus(ch)
		} else {
			total.unpriceable = true
		}
	}
	return total
}

// blockChargeOf sums the composite charges of a whole chosen declaration --
// the one charge derivation validateBlockers and handleBlockers share.
func (e *Engine) blockChargeOf(chosen []decision.Option) blockCharge {
	total := blockCharge{}
	for _, opt := range chosen {
		total = total.plus(e.blockPairCharge(opt.Obj, opt.Attacker))
	}
	return total
}

// blockTapCandidates lists the payer's untapped permanents that satisfy one
// tap obligation's spec, minus the given exclusions -- the declaration's own
// committed blockers, which are "declared as a blocking creature this
// combat" the moment the declaration lands even though the DeclareBlockers
// event has not been applied yet (Hollow Warrior's Creature.!blocking).
func (e *Engine) blockTapCandidates(p state.PlayerID, t blockTapReq, excluded map[state.ObjID]bool) []state.ObjID {
	cands := e.costCandidates(p, t.source, state.ZBattlefield, t.spec, false, true)
	out := cands[:0]
	for _, id := range cands {
		if excluded[id] {
			continue
		}
		out = append(out, id)
	}
	return out
}

// chargeObjCandidates lists the payer's battlefield permanents that satisfy
// one Sac/Return obligation's spec, minus the given exclusions. The spec is
// resolved relative to the static's source, exactly as blockTapCandidates
// binds a tap obligation (costCandidates' matchesSpecFrom). A Sac candidate
// is additionally filtered by the same CantSacrifice check the unless
// payment's own candidate walk applies, so a permanent that cannot be
// sacrificed never appears in a plan.
func (e *Engine) chargeObjCandidates(p state.PlayerID, r chargeObjReq, kind string, excluded map[state.ObjID]bool) []state.ObjID {
	cands := e.costCandidates(p, r.source, state.ZBattlefield, r.spec, false, false)
	out := cands[:0]
	for _, id := range cands {
		if excluded[id] {
			continue
		}
		if kind == "sacrifice" && e.sacrificeBlockedForCost(id, costCauseResolution) {
			continue
		}
		out = append(out, id)
	}
	return out
}

// chargeObjPlan reserves the exact permanents a charge's tap/sac/return
// obligations take, solving the JOINT assignment rather than a per-requirement
// greedy one: a tapXType<1/Creature> and a Sac<1/Creature.Artifact> on the
// same board must not let the tap reservation claim a permanent the sacrifice
// uniquely needs. Each obligation takes candidates in zone order and the
// obligations are tried in a fixed order (taps, then sacrifices, then
// returns), so the first full assignment the search finds is deterministic
// (R-9: the build never asks which permanents to tap/sacrifice/return). A
// permanent pays at most one obligation. Returns nil, false when no assignment
// fills every obligation. The search is bounded; beyond the bound it fails
// closed, the conservative direction (a charge the search cannot settle is
// never offered).
func (e *Engine) chargeObjPlan(p state.PlayerID, c blockCharge, excluded map[state.ObjID]bool) (taps, sacs, returns []state.ObjID, ok bool) {
	type obligation struct {
		kind   string
		req    chargeObjReq
		tapReq blockTapReq
	}
	var obs []obligation
	for _, t := range c.taps {
		obs = append(obs, obligation{kind: "tap", tapReq: t})
	}
	for _, r := range c.sacs {
		obs = append(obs, obligation{kind: "sacrifice", req: r})
	}
	for _, r := range c.returns {
		obs = append(obs, obligation{kind: "returncost", req: r})
	}
	taken := make(map[state.ObjID]bool, len(excluded))
	for id := range excluded {
		taken[id] = true
	}
	picks := make([][]state.ObjID, len(obs))
	nodes := 0
	var solve func(i int) bool
	solve = func(i int) bool {
		if i == len(obs) {
			return true
		}
		nodes++
		if nodes > 1<<16 {
			return false
		}
		ob := obs[i]
		var cands []state.ObjID
		var n int32
		if ob.kind == "tap" {
			cands = e.blockTapCandidates(p, ob.tapReq, taken)
			n = ob.tapReq.n
		} else {
			cands = e.chargeObjCandidates(p, ob.req, ob.kind, taken)
			n = ob.req.n
		}
		var choose func(start int, got []state.ObjID) bool
		choose = func(start int, got []state.ObjID) bool {
			if int32(len(got)) == n {
				picks[i] = append([]state.ObjID(nil), got...)
				for _, id := range got {
					taken[id] = true
				}
				if solve(i + 1) {
					return true
				}
				for _, id := range got {
					delete(taken, id)
				}
				picks[i] = nil
				return false
			}
			for j := start; j < len(cands); j++ {
				id := cands[j]
				if taken[id] {
					continue
				}
				if choose(j+1, append(got, id)) {
					return true
				}
			}
			return false
		}
		return choose(0, nil)
	}
	if !solve(0) {
		return nil, nil, nil, false
	}
	for i, ob := range obs {
		switch ob.kind {
		case "tap":
			taps = append(taps, picks[i]...)
		case "sacrifice":
			sacs = append(sacs, picks[i]...)
		default:
			returns = append(returns, picks[i]...)
		}
	}
	return taps, sacs, returns, true
}

// combatPhyLife is the life a Phyrexian pip charges when paid with life
// (CR 107.4f: two life per pip). It aliases decision.PhyrexianLife, the one
// home the wire-side charge pre-filter also prices a pip through, so the
// bot's bound and this affordability read cannot disagree.
const combatPhyLife = decision.PhyrexianLife

// combatPayPlan is the frozen payment plan for one combat charge, shared by
// the attack and block payment windows. The obligation plans are computed
// once, against the board as it stood when the window opened, and paid
// verbatim at completion -- the mana window's own taps can remove a
// candidate (tapping a creature land), so re-deriving would drift.
// phyToLife is the number of Phyrexian pips paid with life rather than
// colour; phyDecided records whether that election was made; phyElection is
// TRUE when BOTH branches are affordable, so the CR 107.4f colour-versus-life
// choice is owed and the window must pose it (a charge with no Phyrexian
// pips, or one where only a single branch is affordable, settles
// deterministically).
type combatPayPlan struct {
	player      state.PlayerID
	charge      blockCharge
	taps        []state.ObjID
	sacs        []state.ObjID
	returns     []state.ObjID
	phyToLife   int32
	phyDecided  bool
	phyElection bool
}

// openCombatPayPlan freezes the plan for a charge against the payer's board
// and applies the DETERMINISTIC Phyrexian routing when no election is owed:
// a charge whose only affordable pip branch is life has its pips routed to
// life up front, so the inline/coverage read and the payment window both see
// the same mana requirement. A charge with BOTH branches affordable sets
// phyElection and leaves phyToLife 0, so the window poses the real choice.
// excluded holds the declaration's own committed creatures for the OBLIGATION
// half; manaExcluded holds the mana-half withholdings beyond the plan's own
// tap reservations (the attack direction's declared attackers; nil on the
// block side, where a committed blocker is still a mana source). Returns nil,
// false when an obligation cannot be met (the caller declines).
func (e *Engine) openCombatPayPlan(p state.PlayerID, c blockCharge, excluded, manaExcluded map[state.ObjID]bool) (*combatPayPlan, bool) {
	if !c.payable() {
		return nil, false
	}
	taps, sacs, returns, ok := e.chargeObjPlan(p, c, excluded)
	if !ok {
		return nil, false
	}
	plan := &combatPayPlan{player: p, charge: c, taps: taps, sacs: sacs, returns: returns}
	if len(c.phyrexian) > 0 {
		both, canColour, canLife := e.combatPhyBothBranches(p, c, plan.tapExclude(), manaExcluded)
		plan.phyElection = both
		if !both && !canColour && canLife {
			plan.phyToLife = int32(len(c.phyrexian))
		}
	}
	return plan, true
}

// manaCost is the part of the charge the mana window must satisfy: the
// generic component plus every Phyrexian pip not routed to life (CR 107.4f).
func (pl *combatPayPlan) manaCost() Cost {
	out := Cost{Generic: pl.charge.mana}
	for i, col := range pl.charge.phyrexian {
		if int32(i) < pl.phyToLife {
			continue
		}
		// The pip is paid with one mana of its colour on the colour branch.
		// Modelled as a coloured pip (not a Phyrexian pip) so the mana window
		// requires REAL colour mana: a Phyrexian Cost entry would let
		// resolveManaWith satisfy it with life, which is the branch this
		// decision already made not to take.
		out.Colored[state.ManaIndex(col)]++
	}
	return out
}

// lifeExtra is the life the life-routed Phyrexian pips charge (two per pip).
func (pl *combatPayPlan) lifeExtra() int32 { return pl.phyToLife * combatPhyLife }

// tapExclude is the set of permanents the plan reserves for its tap
// obligations. The mana window withholds them from its tap list: a permanent
// already paying a tapXType cost cannot also be tapped for mana (the
// double-tap defect), and the affordability read excludes the same set so the
// offer gate and the window agree.
func (pl *combatPayPlan) tapExclude() map[state.ObjID]bool {
	if len(pl.taps) == 0 {
		return nil
	}
	m := make(map[state.ObjID]bool, len(pl.taps))
	for _, id := range pl.taps {
		m[id] = true
	}
	return m
}

// manaSatisfied reports whether the payer's floating pool already satisfies
// the plan's mana requirement (used to decide whether the mana window must
// ask for another source, and whether it may complete).
func (e *Engine) manaSatisfied(pl *combatPayPlan) bool {
	pc := pl.player
	player := e.G.Players[pc]
	cost := pl.manaCost()
	if cost.Generic == 0 && len(cost.Phyrexian) == 0 && cost.Colored.Total() == 0 {
		return true
	}
	_, ok := cost.resolveManaWith(player.Pool, player.Snow, player.ManaUnits(),
		player.Life-pl.lifeExtra(), false, pipRider{}, e.paymentConv(pc, 0, false))
	return ok
}

// combatPhyBothBranches reports whether BOTH the colour branch and the life
// branch of a charge's Phyrexian pips are individually affordable, so the
// payer may be offered the real CR 107.4f choice. A charge with a single
// affordable branch skips the ask and settles deterministically.
//
// Both branches price the charge's GENERIC component JOINTLY with the pips:
// a `Cost$ 2 WP` with only two white mana in reach is affordable in NEITHER
// branch, because the two white must also cover the two generic, and the
// round-1 read checked the pips and the generic independently against the
// same sources (the review's atomicity defect).
//
// manaExcluded holds only permanents withheld from the MANA HALF -- the
// plan's own tap reservations, plus (attack direction only) the declared
// attackers, which CR 508.1f genuinely taps. A committed BLOCKER is not
// withheld here: declaring a block never taps it (CR 509.1), so it is a
// legitimate mana source for its own pair's tax. The obligation half is a
// separate exclusion owned by chargeObjPlan.
func (e *Engine) combatPhyBothBranches(p state.PlayerID, c blockCharge, manaExcluded ...map[state.ObjID]bool) (both bool, canColour, canLife bool) {
	if len(c.phyrexian) == 0 {
		return false, false, false
	}
	player := e.G.Players[p]
	conv := e.paymentConv(p, 0, false)
	exclude := make(map[state.ObjID]bool)
	for _, set := range manaExcluded {
		for id := range set {
			exclude[id] = true
		}
	}
	units := e.attackWindowUnits(p, exclude)
	// Colour branch: the generic plus every pip in its own colour, no pip paid
	// with life. A coloured Cost entry, not a Phyrexian one, so this probes
	// REAL colour mana; unlessManaReachable counts the payer's untapped
	// sources (the pool alone would miss a Plains the payer has not tapped).
	colourCost := Cost{Generic: c.mana}
	for _, col := range c.phyrexian {
		colourCost.Colored[state.ManaIndex(col)]++
	}
	canColour = e.unlessManaReachable(p, colourCost, player.Pool, player.Snow, player.ManaUnits(),
		player.Life-c.life, conv, units)
	// Life branch: the generic reachable with no life spent on pips, and the
	// payer's life after the fixed life charge covers two per pip.
	canLife = player.Life-c.life >= int32(len(c.phyrexian))*combatPhyLife &&
		e.unlessManaReachable(p, Cost{Generic: c.mana}, player.Pool, player.Snow, player.ManaUnits(),
			player.Life-c.life, conv, units)
	return canColour && canLife, canColour, canLife
}

// combatChargeAffordable reports whether the payer can pay every component of
// the charge: the JOINT mana requirement (generic plus every Phyrexian pip,
// each payable with one mana of its colour OR two life, CR 107.4f) reachable
// from the payer's floating pool plus the legal window mana sources, the
// fixed life, and enough distinct permanents for every tap/sac/return
// obligation. excluded holds any permanent the declaration already commits.
//
// The two exclusions have DIFFERENT roles and are passed separately. excluded
// governs the OBLIGATION half only (chargeObjPlan's tap/sac/return candidate
// pool): a permanent the declaration commits cannot also pay an obligation.
// manaExcluded governs the MANA half only, and holds exactly the permanents
// that the declaration genuinely withholds from being tapped for mana -- the
// plan's own tap reservations plus, in the attack direction, the declared
// attackers (CR 508.1f taps them at declaration). A committed BLOCKER is not
// in manaExcluded: declaring a block never taps it (CR 509.1), so its mana is
// real and the payment window offers it. Overloading one map for both roles
// was the block-tax defect: validation withheld every committed blocker from
// the mana half, so a blocker that was its own pair's only mana source was
// rejected while the window (taps-only) and the published MaxSum both said
// payable, and the bot's own answer crashed the table.
//
// This is the ONE affordability read: the offer gate, the whole-declaration
// validator and the payment all re-derive from it, so they cannot disagree
// about what is payable. A charge with an unpriceable component is never
// affordable (fail closed), and a charge whose obligations cannot all be met
// is unaffordable, never free.
//
// The mana is priced as ONE Cost in a single reachability call, so the
// generic and the pips compete for the same units: checking them separately
// against the same pool admitted a `Cost$ 2 WP` the payer could not actually
// pay (the review's atomicity defect). The plan's own tap obligations are
// excluded from the mana sources, so a permanent reserved to pay a tapXType
// cost cannot also be counted as a mana source (the double-tap defect).
func (e *Engine) combatChargeAffordable(p state.PlayerID, c blockCharge, excluded, manaExcluded map[state.ObjID]bool) bool {
	if !c.payable() {
		return false
	}
	player := e.G.Players[p]
	life := player.Life
	if life < c.life {
		return false
	}
	// Solve the obligation assignment first: its tap reservations must be
	// withheld from the mana sources, so the two components are one joint
	// problem, not two independent checks.
	taps, _, _, ok := e.chargeObjPlan(p, c, excluded)
	if !ok {
		return false
	}
	mc := Cost{Generic: c.mana, Phyrexian: append([]byte(nil), c.phyrexian...)}
	if mc.Generic == 0 && len(mc.Phyrexian) == 0 {
		return true
	}
	exclude := make(map[state.ObjID]bool, len(manaExcluded)+len(taps))
	for id := range manaExcluded {
		exclude[id] = true
	}
	for _, id := range taps {
		exclude[id] = true
	}
	reachable := e.unlessManaReachable(p, mc, player.Pool, player.Snow, player.ManaUnits(),
		life-c.life, e.paymentConv(p, 0, false), e.attackWindowUnits(p, exclude))
	if !reachable || len(c.phyrexian) == 0 {
		return reachable
	}
	// The current choice contract routes all Phyrexian pips together. Do not
	// offer a mixed-only multi-pip charge that the payment continuation cannot
	// settle; a future per-pip chooser can widen this safely.
	_, canColour, canLife := e.combatPhyBothBranches(p, c, exclude)
	return canColour || canLife
}

// blockChargeAffordable is the block-direction alias of the shared
// combatChargeAffordable read. Block payment settles from the defending
// player, whose budget is the same attackBudget attackBudget computes, so the
// two directions share one affordability rule. excluded holds the committed
// blockers for the OBLIGATION half; manaExcluded holds only permanent sets
// the declaration genuinely withholds from mana, which on the block side is
// nothing beyond the plan's own tap reservations (so every block caller
// passes nil).
func (e *Engine) blockChargeAffordable(p state.PlayerID, c blockCharge, excluded, manaExcluded map[state.ObjID]bool) bool {
	return e.combatChargeAffordable(p, c, excluded, manaExcluded)
}

// payCombatExtras settles a completed charge's non-mana components in a fixed
// order: the life (one LifeChange, the same event shape payMana's Life part
// emits, so life-loss replacements see it), then the sacrifice and return
// zone changes, then the taps (one Tap event each, the same event the cast
// tap costs emit). They fire only after the charge's mana is covered, so a
// defensive window strand can never leave a half-paid composite behind.
func (e *Engine) payCombatExtras(p state.PlayerID, c blockCharge, taps, sacs, returns []state.ObjID, lifeExtra int32) {
	if total := c.life + lifeExtra; total > 0 {
		e.emit(events.Event{Kind: events.LifeChange, Player: p, Amount: -total})
	}
	for _, id := range sacs {
		e.emit(events.Sacrifice(id))
	}
	for _, id := range returns {
		if o := e.G.Obj(id); o != nil {
			e.emit(events.ReturnCost(id, o.Zone))
		}
	}
	for _, id := range taps {
		e.emit(events.Event{Kind: events.Tap, Obj: id, Text: "tapped as a block cost"})
	}
}

// combatPlanSettlesInline reports whether the plan can settle without
// opening the payment window: no Phyrexian election is owed and the pool
// already covers the (deterministically routed) mana requirement. An
// obligation/life component is settled inline either way, so it does not
// affect this read.
func (e *Engine) combatPlanSettlesInline(plan *combatPayPlan) bool {
	if plan.phyElection {
		return false
	}
	return e.manaSatisfied(plan)
}

// payCombatChargeInline settles a charge the pool already covers, plus its
// non-mana components, without opening a payment window.
func (e *Engine) payCombatChargeInline(plan *combatPayPlan) {
	cost := plan.manaCost()
	if cost.Generic > 0 || len(cost.Phyrexian) > 0 {
		e.payManaConv(plan.player, cost, e.paymentConv(plan.player, 0, false))
	}
	e.payCombatExtras(plan.player, plan.charge, plan.taps, plan.sacs, plan.returns, plan.lifeExtra())
}

// chosenBlockers is the set of permanents a declaration commits to blocking;
// they are excluded from the tap-cost candidate pool.
func chosenBlockers(chosen []decision.Option) map[state.ObjID]bool {
	m := make(map[state.ObjID]bool, len(chosen))
	for _, o := range chosen {
		m[o.Obj] = true
	}
	return m
}

// blockManaBudget is the defending player's available payment budget. Unlike
// attackers, that player has had priority since attackers were declared, so
// floating mana is part of the normal declaration path.
func (e *Engine) blockManaBudget(p state.PlayerID) int32 { return e.attackBudget(p) }

type blockPayWindow struct {
	chosen []decision.Option
	plan   *combatPayPlan
	// sources is the tap list the CURRENT ask posed, in option order: the
	// answer resolves its exact entry by the chosen option's Index (see
	// paySourceForAnswer), the same alternative-identity rule the attack
	// window follows.
	sources []attackManaSource
}

// startBlockPay opens the block payment window for a declaration whose
// composite charge the OPENER's plan could not settle inline. It returns
// false when the charge cannot be paid at all -- a mana window that ran out
// of sources while the pool still did not cover the charge -- and the caller
// then declines the declaration rather than committing it unpaid.
func (e *Engine) startBlockPay(chosen []decision.Option, plan *combatPayPlan) bool {
	e.blockPay = &blockPayWindow{chosen: chosen, plan: plan}
	if !e.askNextBlockPay() {
		e.blockPay = nil
		if !e.manaSatisfied(plan) {
			// The window found nothing left to ask while the pool still does
			// not cover the charge: refuse the declaration. Committing here is
			// the review's unpaid-block defect. The caller owns the loud Note
			// and the empty DeclareBlockers marker (declineBlockDeclaration).
			return false
		}
		e.completeBlockPay(chosen, plan)
	}
	return true
}

// declineBlockDeclaration records a refused blocking declaration: one loud
// Note, the empty DeclareBlockers marker for this defender (the same terminal
// event the ordinary empty branch emits) and the cursor advance. It is the
// block-direction sibling of the attack side's abort branch in
// attackPayAnswer, and the ONE place a refused declaration is settled, so no
// path can fall through to commit a charge nobody paid.
func (e *Engine) declineBlockDeclaration(player state.PlayerID) {
	e.blockPay = nil
	e.emit(events.Event{Kind: events.Note, Player: player, Text: "could not pay the block cost"})
	e.emit(events.Event{Kind: events.DeclareBlockers, Player: player})
	e.blockerRound.cursor++
}

// completeBlockPay settles the plan's mana and non-mana components and emits
// the parked DeclareBlockers declaration.
func (e *Engine) completeBlockPay(chosen []decision.Option, plan *combatPayPlan) {
	cost := plan.manaCost()
	if cost.Generic > 0 || len(cost.Phyrexian) > 0 {
		e.payManaConv(plan.player, cost, e.paymentConv(plan.player, 0, false))
	}
	e.payCombatExtras(plan.player, plan.charge, plan.taps, plan.sacs, plan.returns, plan.lifeExtra())
	chosenPairs := make([][2]state.ObjID, 0, len(chosen))
	for _, opt := range chosen {
		chosenPairs = append(chosenPairs, [2]state.ObjID{opt.Attacker, opt.Obj})
	}
	e.emit(events.Event{Kind: events.DeclareBlockers, Player: plan.player, Pairs: chosenPairs})
	e.blockerRound.cursor++
}

// askNextBlockPay poses the next payment step: the CR 107.4f Phyrexian
// colour-versus-life election first when both branches are affordable and the
// election is still open, then one mana source, then nothing (the caller
// completes).
func (e *Engine) askNextBlockPay() bool {
	st := e.blockPay
	if st == nil {
		return false
	}
	plan := st.plan
	if plan.phyElection && !plan.phyDecided {
		d := &decision.Decision{Player: plan.player, Kind: decision.KChoose, Min: 1, Max: 1,
			Prompt: "Pay the Phyrexian cost with mana or life"}
		d.Options = append(d.Options,
			decision.Option{Index: 0, Kind: "block_phy_colour", Label: "Pay the Phyrexian symbols with mana"},
			decision.Option{Index: 1, Kind: "block_phy_life", Label: fmt.Sprintf("Pay %d life", int32(len(plan.charge.phyrexian))*combatPhyLife)})
		e.choosing = chooseBlockPay
		e.ask(d)
		return true
	}
	if e.manaSatisfied(plan) {
		return false
	}
	sources := e.attackManaSources(plan.player)
	if x := plan.tapExclude(); x != nil {
		kept := sources[:0]
		for _, s := range sources {
			if !x[s.id] {
				kept = append(kept, s)
			}
		}
		sources = kept
	}
	if len(sources) == 0 {
		return false
	}
	st.sources = sources
	d := &decision.Decision{Player: plan.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: fmt.Sprintf("Pay %d to block -- tap a mana source", plan.charge.mana)}
	for _, s := range sources {
		name := "a permanent"
		if o := e.G.Obj(s.id); o != nil && o.Face() != nil {
			name = o.Face().Name
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "block_mana", Obj: s.id,
			Label: fmt.Sprintf("Tap %s for %s", name, s.prod)})
	}
	e.choosing = chooseBlockPay
	e.ask(d)
	return true
}

func (e *Engine) blockPayAnswer(d *decision.Decision, in decision.Intent) {
	st := e.blockPay
	e.choosing = chooseNone
	if st == nil {
		return
	}
	chosen := d.Chosen(in)
	if len(chosen) == 1 {
		switch chosen[0].Kind {
		case "block_phy_colour":
			st.plan.phyDecided = true
			st.plan.phyToLife = 0
		case "block_phy_life":
			st.plan.phyDecided = true
			st.plan.phyToLife = int32(len(st.plan.charge.phyrexian))
		default:
			if s, ok := paySourceForAnswer(st.sources, chosen[0]); ok {
				e.resolveManaAbilityRefOriginal(st.plan.player, s.id, s.ma, s.original, s.gained, false, false, false)
			}
		}
	}
	if !e.askNextBlockPay() {
		plan := st.plan
		if !e.manaSatisfied(plan) {
			// The window ran out while the pool still did not cover the
			// charge: decline rather than committing it unpaid.
			e.declineBlockDeclaration(plan.player)
			return
		}
		e.blockPay = nil
		e.completeBlockPay(st.chosen, plan)
	}
}

// attackManaSource is one mana source the declare-attackers payment window
// may tap, with the units its single free mana ability produces.
type attackManaSource struct {
	id state.ObjID
	// ma is the EXACT ability the window resolves, with a choice-shaped
	// Produced$ already pinned to one concrete colour so the activation poses
	// no colour sub-ask and adds exactly units.
	ma *cards.SA
	// original is the source pile's compiled ability. ma can be an immutable
	// Produced$ rewrite, but activation-limit markers must retain original's
	// pile identity.
	original *cards.SA
	// gained is the ability's gained-ability identity, captured from original
	// before a Produced$ rewrite.
	gained gainedManaRef
	units  int32
	// counts and amt are the production's slot vector and per-activation
	// scaling, kept beside prod so attackWindowUnits can rebuild the same
	// mana vector unlessManaReachable prices against.
	counts [6]int32
	amt    int32
	// prod is the exact production one activation yields, rendered as braced
	// symbols ("{R}", "{C}{C}"): the tap option's label, so the
	// alternatives of one multi-ability permanent are distinguishable on the
	// wire and the payer taps the ability it meant to tap.
	prod string
}

// attackWindowUnits groups the attack window's mana sources by permanent into
// the windowManaUnit shape unlessManaReachable consumes, minus the sources in
// exclude. The affordability read and the tap list therefore share ONE
// membership (attackManaSources): the offer gate can never promise a source
// the window cannot tap, and a permanent reserved to pay a tapXType
// obligation is withheld from both. A unit with several priceable abilities
// keeps one alt per ability, exactly as the window offers one option per
// ability.
func (e *Engine) attackWindowUnits(p state.PlayerID, exclude map[state.ObjID]bool) []windowManaUnit {
	idx := make(map[state.ObjID]int)
	var out []windowManaUnit
	for _, s := range e.attackManaSources(p) {
		if exclude[s.id] {
			continue
		}
		alt := windowManaAlt{ma: s.ma, counts: s.counts, amt: s.amt}
		if i, ok := idx[s.id]; ok {
			out[i].alts = append(out[i].alts, alt)
			continue
		}
		idx[s.id] = len(out)
		out = append(out, windowManaUnit{id: s.id, alts: []windowManaAlt{alt}})
	}
	return out
}

// manaUnitsLabel renders a production as braced mana symbols, e.g. "{R}{R}":
// counts[i] slots of symbol i, each scaled by amt. It is the tap option's
// production half of the label.
func manaUnitsLabel(counts [6]int32, amt int32) string {
	var b strings.Builder
	for i := range counts {
		for n := int32(0); n < counts[i]*amt; n++ {
			b.WriteString("{")
			b.WriteString(string(cards.ManaSymbol(i)))
			b.WriteString("}")
		}
	}
	return b.String()
}

// paySourceForAnswer resolves the EXACT source a tap answer selected. The
// option's Index is its position in the list the window posed and the window
// stores that list (attackPayWindow.sources / blockPayWindow.sources), so a
// multi-ability permanent's second alternative resolves ITS ability, not the
// first alternative that shares its Obj -- the round-1 defect: duplicate-Obj
// options resolved the first ability while the budget counted the maximum.
// The Obj cross-check keeps a malformed answer from activating another
// object's ability; on a mismatch or an out-of-range Index the legacy
// first-match-by-Obj scan applies, so an old-style answer still pays.
func paySourceForAnswer(sources []attackManaSource, opt decision.Option) (attackManaSource, bool) {
	if opt.Index >= 0 && opt.Index < len(sources) && sources[opt.Index].id == opt.Obj {
		return sources[opt.Index], true
	}
	for _, s := range sources {
		if s.id == opt.Obj {
			return s, true
		}
	}
	return attackManaSource{}, false
}

// attackManaSources walks the payer's battlefield in zone order and returns
// every untapped permanent whose window-usable mana abilities contain a
// free-cost ability whose production this build can price: a plain Produced$
// symbol list ("G", "R G", "RR") adds exactly those symbols; a
// choice-shaped production (the blank/"Any"/"Combo Any" shapes and the
// "Combo <colours>"/"Chosen" families) yields ONE unit of mana per
// activation, of a colour chosen when it is tapped (CR 106.1b) -- and any
// colour pays a generic attack tax, so it counts one unit. A fail-closed
// shape the parser claims no colour for (ColorIdentity, a Special word) is
// excluded. A known literal Amount$ scales the units. The SAME membership is
// the affordability bound's input (attackBudget), so a pair is offered only
// when the window can actually reach the charge -- the wedge guard: every
// tap adds its counted units, so the window can never strand.
//
// One source per PRICEABLE ALTERNATIVE: a permanent with several free mana
// abilities (a Volcanic Island's intrinsic {U} and {R}) offers one tap option
// per ability. Each option resolves the exact ability (no nested colour
// sub-ask), so the membership is no longer narrowed to a single-ability
// permanent. An ability the shared walk cannot price deterministically
// (Indeterminate Amount$, a RestrictValid$-governed batch) is still excluded:
// its produced batch cannot pay an attack cost, so counting its units would
// overstate the payer's reach. A permanent whose only remaining abilities are
// so excluded contributes nothing.
func (e *Engine) attackManaSources(p state.PlayerID) []attackManaSource {
	// A pure read: one Derived memo scope spans both membership walks, so the
	// per-permanent window-ability walks share one Continuous-static board
	// scan (rules/walkcache.go) instead of rescanning per permanent.
	e.beginDerivedMemo()
	defer e.endDerivedMemo()
	var out []attackManaSource
	// windowManaUnits is the ONE membership the offer gate (attackBudget) and
	// this tap list share, so the attack window can never be offered a charge
	// its sources cannot reach (see the doc comment on windowManaUnits). The
	// window taps one concrete ability with no sub-ask, and windowManaUnits
	// already exposes one alt per priceable ability, so every alt becomes an
	// option -- a multi-colour dual's two intrinsics are both payable now.
	for _, u := range e.windowManaUnits(p) {
		for _, a := range u.alts {
			units := int32(0)
			for _, n := range a.counts {
				units += n * a.amt
			}
			if units <= 0 {
				continue
			}
			out = append(out, attackManaSource{id: u.id, ma: a.ma, original: a.ma, gained: e.gainedManaRefFor(p, u.id, a.ma), units: units, counts: a.counts, amt: a.amt, prod: manaUnitsLabel(a.counts, a.amt)})
		}
	}
	// The choice-shaped productions the shared membership deliberately
	// excludes (windowManaUnits skips every "Any"/"Combo"/"Chosen" shape: the
	// unless-cost window cannot tap one without a mid-resolution colour
	// sub-ask). A generic attack tax is payable by ANY colour, so the attack
	// window admits them through its own walk and pins the choice (ticket
	// cli-20260922T225137Z-c670f42d) -- every source counted here is also
	// tappable, so the wedge guard still holds.
	out = append(out, e.attackChoiceManaSources(p)...)
	return out
}

// attackChoiceManaSources is the attack window's choice-shaped membership: it
// walks the payer's battlefield in zone order and returns, for every untapped
// permanent, one option per free-cost window-usable mana ability whose
// production is choice-shaped (the blank-excluded "Any" / "Combo Any" /
// "Combo <colours>" / "Chosen" families). A permanent with several free
// abilities (a plain symbol ability plus a choice-shaped one, or two
// colour-fixing abilities) therefore yields one option per choice-shaped
// ability. A plain Produced$ symbol list is the shared walk's domain and never
// reaches here; a fail-closed shape the parser claims no colour for
// (ColorIdentity, a Special word) has an empty slot set and is excluded. A
// known literal Amount$ scales the units.
func (e *Engine) attackChoiceManaSources(p state.PlayerID) []attackManaSource {
	var out []attackManaSource
	for _, id := range e.battlefieldManaSourceIDs(p) {
		o := e.G.Obj(id)
		if o == nil || o.Tapped || o.Face() == nil {
			continue
		}
		for _, ma := range e.availableManaAbilitiesForWindow(p, id, false) {
			if strings.TrimSpace(ma.Params["RestrictValid"]) != "" {
				continue
			}
			if !manaFreeCost(e.parseCost(ma.Params["Cost"])) {
				continue
			}
			amt := availableAmount(ma)
			if amt <= 0 {
				continue
			}
			produced := strings.TrimSpace(ma.Params["Produced"])
			if produced == "" {
				// The shared walk's domain: the executor's deterministic one-
				// colourless default, already counted there. Admitting it here
				// too would double-count the source.
				continue
			}
			// Chosen is an as-enters read. Substitute it before parsing so a
			// Combo R Chosen land recorded as G has only R/G slots, never the
			// raw parser's source-agnostic WUBRG superset. Without a valid record
			// it cannot produce a colour and stays out of this payment window.
			if producedNeedsChosen(produced) {
				chosen := e.chosenProducedColour(id)
				if chosen == "" {
					continue
				}
				produced = substituteChosenProduced(produced, chosen)
			}
			counts, choice := cards.ProducedCounts(produced)
			units := int32(0)
			if choice {
				// A choice-shaped production produces exactly ONE unit of mana
				// per activation, of a colour chosen when the source is tapped
				// ("Any" / "Combo Any" / "Combo B R" / "Chosen", CR 106.1b). One
				// unit of any colour pays a generic attack tax, so the source
				// counts one payable unit no matter how many colour slots its
				// alternatives name.
				total := int32(0)
				for _, n := range counts {
					total += n
				}
				if total > 0 {
					units = amt
				}
			}
			if units <= 0 {
				continue
			}
			// Pin the choice to ONE concrete colour before recording the
			// ability. resolveManaAbility resolves the source's resolution
			// inline, and a choice-shaped Produced$ would pose a
			// mid-resolution colour ask there; attackPayAnswer then re-reads
			// the pool and re-poses the next tap ask, and e.ask (with e.resume
			// nil for a window, not a suspended resolution) would silently
			// displace that colour ask -- tapping the source for nothing. The
			// tax is generic, so the colour cannot matter: take the
			// deterministic first producible colour (R-9), the same rewrite
			// the activation path performs, so the tap adds exactly one unit
			// and poses no sub-ask. The gained identity is captured before the
			// rewrite, which changes the SA pointer.
			gained := e.gainedManaRefFor(p, id, ma)
			rewritten := withProduced(ma, ma, oneColourProduced(counts))
			pc, _ := cards.ProducedCounts(oneColourProduced(counts))
			out = append(out, attackManaSource{id: id, ma: rewritten, original: ma, gained: gained, units: units, counts: pc, amt: amt, prod: manaUnitsLabel(pc, amt)})
		}
	}
	return out
}

// oneColourProduced turns a choice-shaped production's slot vector into the
// single plain Produced$ symbol the attack payment window resolves without a
// sub-ask. It takes the first producible slot in WUBRG order (deterministic,
// R-9) and colourless only when no colour is on offer, so a blank ability's
// one colourless unit round-trips to "C". The caller has already established
// at least one non-zero slot.
// producedNeedsChosen reports whether a Produced$ grammar token needs the
// source's recorded as-enters colour before it can be priced.
func producedNeedsChosen(produced string) bool {
	for tok := range strings.FieldsSeq(produced) {
		switch strings.Trim(tok, "{}") {
		case "Chosen", "ChosenColor", "ComboChosen":
			return true
		}
	}
	return false
}

func oneColourProduced(counts [6]int32) string {
	for i := range counts {
		if counts[i] > 0 {
			return string(cards.ManaSymbol(i))
		}
	}
	return "C"
}

// attackBudget is the declare-attackers affordability bound: the floating
// pool plus the units attackManaSources can produce. At the declare-attackers
// step the pool is empty (CR 500.4 empties it at every step boundary and the
// declaring player has held no priority since), so the bound is the tappable
// production; the pool term keeps the helper honest if a future path ever
// reaches a declaration with mana floating.
//
// A permanent contributes ONE tap, so its alternatives are NOT additive: a
// dual land that can tap for {U} or {R} contributes one unit, not two. Each
// permanent's contribution is therefore the MAXIMUM units over its
// alternatives (plain abilities from windowManaUnits and choice-shaped ones
// from attackChoiceManaSources, both keyed by the same ObjID), and only then
// are the per-permanent bounds summed. Summing the alternatives instead would
// let the offer gate admit a charge the window cannot reach (a single
// two-ability land would look like two sources), which is the stranding
// defect the wedge guard exists to prevent.
func (e *Engine) attackBudget(p state.PlayerID) int32 {
	total := e.G.Players[p].Pool.Total()
	// A map range is order-independent here (integer addition is
	// commutative) and reaches no event, option or view: only total matters.
	for _, n := range e.attackSourceUnits(p) {
		total += n
	}
	return total
}

// attackBudgetUnits is attackBudget and attackSourceUnits from one
// attackSourceUnits walk (attackBudget sums exactly that map), taken only
// when the declaration carries a mana tax (taxed): without one, every
// option's budget Value is its zero mana price, so neither is read.
func (e *Engine) attackBudgetUnits(p state.PlayerID, taxed bool) (int32, map[state.ObjID]int32) {
	if !taxed {
		return 0, nil
	}
	units := e.attackSourceUnits(p)
	total := e.G.Players[p].Pool.Total()
	// Order-independent integer sum (attackBudget's own argument).
	for _, n := range units {
		total += n
	}
	return total, units
}

// attackSourceUnits returns, per mana-source Obj, the maximum units that
// source contributes to attackBudget -- the max over its priceable
// alternatives, exactly the per-source term attackBudget sums. A creature
// that is DECLARED as attacking cannot also be tapped for mana, so a
// declaration that includes a source must give up this contribution: the
// published budget cost of one attack option is its mana price PLUS this
// value (attackOffers' per-pair charge plus the source it forgoes). The same
// read is the engine's whole-declaration check and the wire's, so the two
// cannot disagree about what the declaration can pay.
func (e *Engine) attackSourceUnits(p state.PlayerID) map[state.ObjID]int32 {
	best := make(map[state.ObjID]int32)
	for _, s := range e.attackManaSources(p) {
		if s.units > best[s.id] {
			best[s.id] = s.units
		}
	}
	return best
}

// attackTaxed reports whether any currently offered pair carries a positive
// mana price. The published per-option budget cost folds in the option's own
// mana production only when a mana tax is in play (askAttackers). Without a
// tax every Value stays at its historical 0, so every prop-free declaration's
// option list and wire payload is byte-identical -- but the fold cannot be
// applied per-option, because a mana-source creature that attacks while
// OTHER mana funds another creature's tax must STILL give up its own
// contribution; the fold is a property of the whole declaration.
func (e *Engine) attackTaxed(offers []attackOffer) bool {
	for _, of := range offers {
		if of.charge.mana > 0 {
			return true
		}
	}
	return false
}

// attackOptionBudgetValue is the ONE home for an attack option's published
// budget cost: its per-pair mana charge plus -- only when the decision carries
// a mana tax -- the units its own Obj contributes to attackBudget (a declared
// attacker cannot also tap for mana). askAttackers publishes this as
// Option.Value and the whole-declaration check (validateAttackers) sums the
// same currency against attackBudget, so a declaration is admitted exactly
// when the sources it does NOT commit can pay its tax.
func (e *Engine) attackOptionBudgetValue(charge blockCharge, id state.ObjID, selfUnits map[state.ObjID]int32, taxed bool) int32 {
	if !taxed {
		return charge.mana
	}
	return charge.mana + selfUnits[id]
}

// attackOffer is one (attacker, defender) pair askAttackers offers, with the
// mana price attacking that defender charges per creature (0 = free).
type attackOffer struct {
	id  state.ObjID
	def state.PlayerID
	// charge is the pair's full composite charge: mana, life, tap/sac/return
	// obligations and Phyrexian pips. manaPrice is its mana component, the
	// quantity the KAttackers budget (Decision.MaxSum over Option.Value)
	// prices in.
	charge blockCharge
	// battle is the non-player permanent being attacked -- a CR 310.7 battle
	// or a planeswalker (CR 508.1) -- or 0 for a player attack. def is the
	// permanent's seat for a permanent attack (a battle's protector, or the
	// planeswalker's controller; it is what blocks and what every
	// player-scoped restriction reads), so the two fields together are the
	// whole defender. Despite the historical field name, the value covers both
	// permanent kinds; the recipient's face decides the damage conversion
	// (defense counters vs loyalty) in events.Apply's Damage fold.
	battle state.ObjID
}

// attackOfferKey identifies one offered (attacker, defender, battle) pair for
// membership reads (validateAttackers rebuilds the offered set). attackOffer
// itself is not comparable (it carries the charge's slices), so the pair's
// three identity fields are the key.
type attackOfferKey struct {
	id     state.ObjID
	def    state.PlayerID
	battle state.ObjID
}

// attackOffers builds the offer list askAttackers, attackDutyDischargeable
// and validateAttackers share -- the one source of truth for which pairs exist
// this combat. The enumeration and the ORDER are exactly askAttackers':
// defender-major -- for each defender in AliveFrom(0) minus the active
// player, for each canAttack-filtered battlefield creature in zone order,
// through the goad/CantAttack filters. On top, the affordability
// bound: a chargeable pair is admitted when its INDIVIDUAL price fits the
// payer's attackBudget, so the list never offers an option the payer cannot
// afford at all. The declaration's TOTAL is enforced at submit
// (validateAttackers sums the chosen prices and rejects a total over the
// budget) rather than by a running serialization here: a list-order greedy
// bound denied legal, payable declarations -- two attackers at a {1} and a
// {2} prop with a budget of 3 could not both be declared when they were
// enumerated in the wrong order, and validateAttackers then rejected the
// declaration outright. Admitting each individually-affordable pair and
// pricing the whole declaration on submission lets the client assemble any
// declaration the payer can actually pay; the solver's requirement read
// (attackDutyDischargeable) then asks only that a required creature has an
// affordable pair that DISCHARGES one of its duties, which is the CR 508.1d
// "if able" reading.
//
// CR 508.1d's "satisfy as many requirements as possible": once the legal,
// affordable pairs are known, every pair of a creature that satisfies FEWER
// player-attack duties (named or goad) than the creature's best available
// defender is dropped.
// That is what keeps a named MustAttack$ duty and a goad from cancelling each
// other out into "the creature attacks nobody": the earlier single-defender
// filter removed the non-named pairs while goadMayAttack removed the named
// one, so the offer list emptied and the requirement solver saw an unrequired
// creature (the t2 review's defect). A creature with no requirement set keeps
// every legal pair, so an ordinary declaration is byte-identical. When two
// named duties name different defenders neither dominates (both satisfy one),
// so both defenders stay offered and the controller picks; when they name the
// same defender that defender is uniquely maximal.
func (e *Engine) attackOffers() []attackOffer {
	// One derivation per log head: mustAttackRequired's per-creature duty
	// checks re-derive the list at the head askAttackers derived it.
	if e.L != nil && e.atkOffersEp > 0 && e.atkOffersEp == len(e.L.Events) && e.atkOffersKeyHolds() {
		return e.attackOffersReused()
	}
	return e.attackOffersStore()
}

// attackOffersPosed is attackOffers for validateAttackers' answer check:
// askAttackers derived the list at the log head it posed d (d.Seq), and only
// d's own DecisionAsk and the answer's DecisionMade -- priority bookkeeping
// that writes nothing the list reads (layercache.go) -- can have been logged
// since, so that list is reused rather than re-derived from the same board.
func (e *Engine) attackOffersPosed(d *decision.Decision) []attackOffer {
	if e.L != nil && e.atkOffersEp > 0 && e.atkOffersEp == int(d.Seq) && e.atkOffersKeyHolds() &&
		e.layerInertSince(e.atkOffersEp) {
		return e.attackOffersReused()
	}
	return e.attackOffersStore()
}

// atkOffersKeyHolds compares the stored list's non-log key.
func (e *Engine) atkOffersKeyHolds() bool {
	return e.atkOffersVer == e.continuousVersion && e.atkOffersObjs == len(e.G.Objs) && e.atkOffersActive == e.G.Active
}

// attackOffersReused returns the stored list; layerInertVerify recomputes it
// and panics on a difference. Callers never mutate the list.
func (e *Engine) attackOffersReused() []attackOffer {
	if layerInertVerify {
		if fresh := e.attackOffersCompute(); !reflect.DeepEqual(fresh, e.atkOffers) {
			panic(fmt.Sprintf("rules: reused attack offer list at log %d (derived at %d) disagrees with a rebuild (%d vs %d offers)",
				len(e.L.Events), e.atkOffersEp, len(e.atkOffers), len(fresh)))
		}
	}
	return e.atkOffers
}

// attackOffersStore derives the list and stores it with its key. A fresh
// slice every time, so a list a caller still holds is never overwritten.
func (e *Engine) attackOffersStore() []attackOffer {
	out := e.attackOffersCompute()
	if e.L != nil {
		e.atkOffers, e.atkOffersEp = out, len(e.L.Events)
		e.atkOffersVer, e.atkOffersObjs, e.atkOffersActive = e.continuousVersion, len(e.G.Objs), e.G.Active
	}
	return out
}

// attackOffersCompute is attackOffers' derivation.
func (e *Engine) attackOffersCompute() []attackOffer {
	// A pure read (askAttackers and validateAttackers both re-derive it), so
	// one Derived memo scope serves every per-creature requirement, goad and
	// restriction static read from the walk cache (rules/walkcache.go): each
	// is otherwise a whole-battlefield activeStatics scan, O(creatures^2) per
	// offer build on a token board (cardfuzz seed 6181111140895991800).
	e.beginDerivedMemo()
	defer e.endDerivedMemo()
	p := e.G.Active
	var out []attackOffer
	var defenders []state.PlayerID
	for _, q := range e.G.AliveFrom(0) {
		if q != p {
			defenders = append(defenders, q)
		}
	}
	// CR 310.7: a battle a player protects is a legal defender for that
	// player's opponents, offered in addition to (not instead of) the player
	// themselves. battleDefenders names each such battle ONCE per (protector,
	// battle) pair, in a deterministic order: the defender enumeration above
	// (ascending seat), then each protected player's battle in that player's
	// battlefield zone order. Only the protector's opponents may attack it, so
	// the active player is excluded here exactly as it is above.
	battles := e.battleDefenders(p)
	// One requirement set per creature, computed once from the board (never
	// per pair), keyed by ObjID and read by lookup only -- no map iteration
	// reaches the offer list order.
	reqs := make(map[state.ObjID]attackRequirementSet)
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if e.canAttack(id) {
			reqs[id] = e.attackRequirements(id)
		}
	}
	// Each (creature able to attack, defending player) pair is at most one
	// offer; planeswalker and battle pairs, rarer, grow past it.
	if n := len(reqs) * len(defenders); n > 0 {
		out = make([]attackOffer, 0, n)
	}
	for _, d := range defenders {
		var walkerTargets []state.ObjID
		for _, wid := range e.G.Zone(state.ZBattlefield, d) {
			o := e.G.Obj(wid)
			if o != nil && !o.FaceDown && o.Face() != nil && o.Face().IsPlaneswalker() {
				walkerTargets = append(walkerTargets, wid)
			}
		}
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			if !e.canAttackPair(id, d) || !e.goadMayAttack(id, d) || e.attackBlocked(id, d, 0) {
				continue
			}
			charge := e.attackPairCharge(id, d, 0)
			if !charge.zero() && !e.combatChargeAffordable(p, charge, map[state.ObjID]bool{id: true}, map[state.ObjID]bool{id: true}) {
				continue
			}
			out = append(out, attackOffer{id: id, def: d, charge: charge})
			// CR 508.1: a planeswalker on d's battlefield is a legal defender
			// for d's opponents, offered in addition to the player themselves
			// (defender-major: the player's own pair block, then its
			// planeswalkers). The walk happens in the planeswalker's
			// controller's zone order, and d IS that controller here.
			for _, wid := range walkerTargets {
				if e.attackBlocked(id, d, wid) {
					continue
				}
				walkerCharge := e.attackPairCharge(id, d, wid)
				if !walkerCharge.zero() && !e.combatChargeAffordable(p, walkerCharge, map[state.ObjID]bool{id: true}, map[state.ObjID]bool{id: true}) {
					continue
				}
				out = append(out, attackOffer{id: id, def: d, battle: wid, charge: walkerCharge})
			}
		}
		// This protector's battles, immediately after the protector's own
		// pair block (defender-major: one defender slot at a time).
		for _, b := range battles {
			if b.protector != d {
				continue
			}
			for _, id := range e.G.Zone(state.ZBattlefield, p) {
				if !e.canAttackPair(id, d) {
					continue
				}
				if !e.goadMayAttack(id, d) {
					continue
				}
				if e.attackBlocked(id, d, b.id) {
					continue
				}
				charge := e.attackPairCharge(id, d, b.id)
				if !charge.zero() && !e.combatChargeAffordable(p, charge, map[state.ObjID]bool{id: true}, map[state.ObjID]bool{id: true}) {
					continue
				}
				out = append(out, attackOffer{id: id, def: d, charge: charge, battle: b.id})
			}
		}
	}
	// Best player-attack duty satisfaction per creature over surviving pairs.
	best := make(map[state.ObjID]int)
	for _, of := range out {
		if n := reqs[of.id].satisfiedByOffer(of); n > best[of.id] {
			best[of.id] = n
		}
	}
	keep := out[:0]
	for _, of := range out {
		rs := reqs[of.id]
		if rs.any() && rs.satisfiedByOffer(of) < best[of.id] {
			continue
		}
		keep = append(keep, of)
	}
	return keep
}

// attackTapPool publishes the decision-wide tap-candidate pool for the
// declaration's tapXType obligation(s) and the per-attacker share of it that a
// declaration would consume. It is the engine half of the wire rule
// decision.ChargeTapPoolFit: askAttackers puts the pool on
// Decision.ChargeTapPool and each offer's consumption on Option.TapPoolCost,
// and validateAttackers reads the same published rule, so a client that cannot
// see the board can still avoid a declaration that leaves the obligation
// unpayable -- the Hollow Warrior case, where the obligation is
// tapXType<1/Creature.!attacking> and committing every other creature removes
// the pool the obligation would draw from.
//
// It returns ok=true only when every tap obligation across every offered pair
// shares one (spec, source) shape: then a single candidate pool is exact and
// pool - consumed >= owed is the whole rule. Any other shape returns ok=false
// and publishes nothing, so the caller falls back to the conservative
// drop-the-pick behaviour rather than publish a pool that does not describe
// the obligations. The candidate list comes from the same blockTapCandidates
// walk the payment plan uses (no exclusions here: the pool is measured before
// the declaration), so the published count and the engine's plan cannot
// disagree about who may pay. Deterministic: a battlefield-order walk and
// lookup-only maps.
func (e *Engine) attackTapPool(p state.PlayerID, offers []attackOffer) (pool int, costs map[state.ObjID]int, ok bool) {
	var shape *blockTapReq
	for i := range offers {
		for j := range offers[i].charge.taps {
			t := offers[i].charge.taps[j]
			if shape == nil {
				r := t
				shape = &r
				continue
			}
			if t.spec != shape.spec || t.source != shape.source {
				return 0, nil, false
			}
		}
	}
	if shape == nil {
		return 0, nil, false
	}
	cands := e.blockTapCandidates(p, *shape, nil)
	member := make(map[state.ObjID]bool, len(cands))
	for _, id := range cands {
		member[id] = true
	}
	costs = make(map[state.ObjID]int, len(offers))
	for i := range offers {
		if member[offers[i].id] {
			costs[offers[i].id] = 1
		}
	}
	return len(cands), costs, true
}

// battleTarget is one attackable battle and the player who protects it.
type battleTarget struct {
	id        state.ObjID
	protector state.PlayerID
}

// battleDefenders lists every battle the active player p may attack under
// CR 310.7: the battle is on the battlefield, is a battle (its printed face,
// and not face down -- a face-down permanent is a vanilla 2/2 creature, CR
// 708.5), has a recorded protector whose seat is still in the game, and that
// protector is not p itself. It is the ONE eligibility home: attackOffers
// enumerates from it, and canAttackBattle below reads it through this list,
// so the offer list and the legality guard can never disagree about which
// battles are attackable. Order is deterministic: protector ascending, then
// the protector's battlefield zone order (the battle's own controller may
// differ from its protector, but a battle is only ever offered under the
// protector's slot, and every controller's battlefield is walked in the
// ascending seat order attacks already use).
func (e *Engine) battleDefenders(p state.PlayerID) []battleTarget {
	var out []battleTarget
	for _, ctrl := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, ctrl) {
			o := e.G.Obj(id)
			if o == nil || o.FaceDown || o.Face() == nil || !o.Face().IsBattle() {
				continue
			}
			if !o.ProtectorValid || o.Protector == p ||
				int(o.Protector) >= len(e.G.Players) || e.G.Players[o.Protector].Lost {
				continue
			}
			out = append(out, battleTarget{id: id, protector: o.Protector})
		}
	}
	return out
}

// canAttackBattle reports whether p may declare an attack at a non-player
// permanent id -- a CR 310.7 battle or a planeswalker (CR 508.1) -- reading
// the same eligibility attacks enumerates: a battle comes from battleDefenders
// (a battle the active player protects, a protectorless battle, or a battle
// that has left the battlefield is not attackable), and a planeswalker is one
// on p's opponent's battlefield, face up (a face-down permanent is a vanilla
// 2/2 creature, CR 708.5).
func (e *Engine) canAttackBattle(id state.ObjID, p state.PlayerID) bool {
	for _, b := range e.battleDefenders(p) {
		if b.id == id {
			return true
		}
	}
	o := e.G.Obj(id)
	if o != nil && !o.FaceDown && o.Zone == state.ZBattlefield && o.Face() != nil && o.Face().IsPlaneswalker() {
		return o.Controller != p
	}
	return false
}

// attackCharge prices a whole declaration: the sum over its chosen pairs of
// the full composite charge (mana + life + tap/sac/return obligations +
// Phyrexian pips).
func (e *Engine) attackCharge(chosen []decision.Option) blockCharge {
	total := blockCharge{}
	for _, opt := range chosen {
		total = total.plus(e.attackPairCharge(opt.Obj, opt.Player, opt.Battle))
	}
	return total
}

// attackPayWindow is the declare-attackers attack-cost payment window's
// resumable state (the enlistAsk/exertAsk precedent, plain value... the
// chosen options slice is shared with the enlist state, never mutated): the
// answered KAttackers declaration, the paying player, and the frozen payment
// plan. Clone copies the pointer.
type attackPayWindow struct {
	chosen []decision.Option
	plan   *combatPayPlan
	// sources is the tap list the CURRENT ask posed, in option order: the
	// answer resolves its exact entry by the chosen option's Index (see
	// paySourceForAnswer), so a permanent with several free abilities pays
	// with the alternative the payer selected.
	sources []attackManaSource
}

// startAttackPay opens the payment window for a declaration whose plan could
// not settle inline. The coverage guard is the shared affordability read the
// offer list already applied (attackOffers admits each individually-affordable
// pair) re-checked here for the defensive paths. Returns false (the caller
// emits the one loud Note and completes the empty declaration) when the
// window cannot complete the charge -- unreachable through a submitted intent
// whose total Decision.MaxSum already bounded the mana, kept for a hand-built
// declaration.
func (e *Engine) startAttackPay(chosen []decision.Option, plan *combatPayPlan) bool {
	e.attackPay = &attackPayWindow{chosen: chosen, plan: plan}
	if !e.askNextAttackPay() {
		e.attackPay = nil
		if !e.manaSatisfied(plan) {
			// The window found nothing left to ask while the pool still does
			// not cover the charge: refuse the declaration. Committing here is
			// the review's unpaid-attack defect (the probe's `Cost$ 2 WP`
			// against two white and one life committed with nothing spent).
			e.emit(events.Event{Kind: events.Note, Player: plan.player,
				Text: fmt.Sprintf("could not pay the {%d} attack cost", plan.charge.mana)})
			return false
		}
		e.completeAttackPay(chosen, plan)
	}
	return true
}

// chosenAttackers is the set of creatures a declaration commits to attacking;
// they are excluded from the tap/sac/return candidate pools (they are
// "declared as attacking" the moment the declaration lands, CR 508.1).
func chosenAttackers(chosen []decision.Option) map[state.ObjID]bool {
	m := make(map[state.ObjID]bool, len(chosen))
	for _, o := range chosen {
		m[o.Obj] = true
	}
	return m
}

// completeAttackPay settles the plan's mana and non-mana components, clears
// the window, and resumes the declaration where handleAttackers parked it
// (the enlist election, or finishAttackers when no attacker enlists).
func (e *Engine) completeAttackPay(chosen []decision.Option, plan *combatPayPlan) {
	cost := plan.manaCost()
	if cost.Generic > 0 || len(cost.Phyrexian) > 0 {
		e.payManaConv(plan.player, cost, e.paymentConv(plan.player, 0, false))
	}
	e.payCombatExtras(plan.player, plan.charge, plan.taps, plan.sacs, plan.returns, plan.lifeExtra())
	if !e.startEnlistAsks(chosen, plan.player) {
		// No attacker enlists: startEnlistAsks reset the election and left
		// the declaration to finish here, the same fallthrough handleAttackers
		// takes when it seeds the election inline.
		e.finishAttackers(chosen, plan.player)
	}
}

// askNextAttackPay poses the next payment step: the CR 107.4f Phyrexian
// colour-versus-life election first when both branches are affordable and the
// election is still open, then one mana source, then nothing (the caller
// completes). The pool is re-read every round: each tap grew it, and the
// window completes the moment it covers the charge -- exact coverage, never
// over-tapping.
func (e *Engine) askNextAttackPay() bool {
	st := e.attackPay
	if st == nil {
		return false
	}
	plan := st.plan
	if plan.phyElection && !plan.phyDecided {
		d := &decision.Decision{Player: plan.player, Kind: decision.KChoose, Min: 1, Max: 1,
			Prompt: "Pay the Phyrexian cost with mana or life"}
		d.Options = append(d.Options,
			decision.Option{Index: 0, Kind: "attack_phy_colour", Label: "Pay the Phyrexian symbols with mana"},
			decision.Option{Index: 1, Kind: "attack_phy_life", Label: fmt.Sprintf("Pay %d life", int32(len(plan.charge.phyrexian))*combatPhyLife)})
		e.choosing = chooseAttackPay
		e.ask(d)
		return true
	}
	if e.manaSatisfied(plan) {
		return false
	}
	sources := e.attackManaSources(plan.player)
	if x := plan.tapExclude(); x != nil {
		kept := sources[:0]
		for _, s := range sources {
			if !x[s.id] {
				kept = append(kept, s)
			}
		}
		sources = kept
	}
	if len(sources) == 0 {
		// Unreachable while the coverage invariant holds (the remaining
		// sources' units are always >= the remaining charge). Loud and
		// non-wedging if a future shape ever breaks it.
		e.emit(events.Event{Kind: events.Note, Player: plan.player,
			Text: fmt.Sprintf("could not pay the {%d} attack cost", plan.charge.mana)})
		return false
	}
	st.sources = sources
	d := &decision.Decision{Player: plan.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: fmt.Sprintf("Pay {%d} to attack -- tap a mana source", plan.charge.mana)}
	for _, s := range sources {
		name := "a permanent"
		if o := e.G.Obj(s.id); o != nil && o.Face() != nil {
			name = o.Face().Name
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options),
			Kind: "attack_mana", Obj: s.id,
			Label: fmt.Sprintf("Tap %s for %s", name, s.prod)})
	}
	e.choosing = chooseAttackPay
	e.ask(d)
	return true
}

// attackPayAnswer applies one answered payment step. It resolves the source's
// single free mana ability (the exact ma the membership walk recorded, so the
// activation poses no sub-ask) or records the Phyrexian election, then
// completes the moment the pool covers the charge -- payManaConv spends it,
// the window clears, and the declaration resumes where handleAttackers parked
// it (the enlist election, or finishAttackers when no attacker enlists).
func (e *Engine) attackPayAnswer(d *decision.Decision, in decision.Intent) {
	st := e.attackPay
	e.choosing = chooseNone
	if st == nil {
		return
	}
	chosen := d.Chosen(in)
	if len(chosen) == 1 {
		switch chosen[0].Kind {
		case "attack_phy_colour":
			st.plan.phyDecided = true
			st.plan.phyToLife = 0
		case "attack_phy_life":
			st.plan.phyDecided = true
			st.plan.phyToLife = int32(len(st.plan.charge.phyrexian))
		default:
			if chosen[0].Obj != 0 {
				if s, ok := paySourceForAnswer(st.sources, chosen[0]); ok {
					e.resolveManaAbilityRefOriginal(st.plan.player, s.id, s.ma, s.original, s.gained, false, false, false)
				}
			}
		}
	}
	if !e.askNextAttackPay() {
		if !e.manaSatisfied(st.plan) {
			// Defensive (the coverage invariant makes this unreachable): the
			// window found no source left to tap, so the charge cannot be
			// paid. Emit ONE loud Note and ABORT -- the empty no-attack
			// declaration -- rather than finishing the declaration unpaid
			// (which would commit an attack whose CR 508.1 cost was never
			// paid).
			plan := st.plan
			e.attackPay = nil
			e.emit(events.Event{Kind: events.Note, Player: plan.player,
				Text: fmt.Sprintf("could not pay the {%d} attack cost", plan.charge.mana)})
			e.emit(events.Event{Kind: events.DeclareAttackers, Player: e.G.NextAlive(e.G.Active)})
			return
		}
		plan := st.plan
		e.attackPay = nil
		e.completeAttackPay(st.chosen, plan)
	}
}
