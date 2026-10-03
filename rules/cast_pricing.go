package rules

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// announcePip resolves the i-th announcement pip of a cost's hybrid →
// monocolour-hybrid → Phyrexian → hybrid-Phyrexian list into its alternative
// payments, in the order manaAsk offers them (each colour, then a generic
// face, then life). It is the single source both manaAsk (the ask's
// valid-option set) and castAnswer (recording the choice) consult, so the
// option offered and the recorded choice always agree. Snow pips are not
// announcement pips: a {S} pip has no alternative payment to announce.
func announcePip(c Cost, i int) []pipAlt {
	if i < len(c.Hybrid) {
		p := c.Hybrid[i]
		return []pipAlt{{color: p.A}, {color: p.B}}
	}
	i -= len(c.Hybrid)
	if i < len(c.Twobrid) {
		t := c.Twobrid[i]
		alts := []pipAlt{{color: t.Col}}
		if t.Generic > 0 {
			alts = append(alts, pipAlt{generic: t.Generic})
		}
		return alts
	}
	i -= len(c.Twobrid)
	if i < len(c.Phyrexian) {
		letter := c.Phyrexian[i]
		return []pipAlt{{color: letter}, {life: 2}}
	}
	i -= len(c.Phyrexian)
	hp := c.HybridPhyrexian[i]
	return []pipAlt{{color: hp.A}, {color: hp.B}, {life: 2}}
}

// annPipCount is how many announcement pips a cost carries: the two-colour
// isAbility reports whether this proposal activates an ABILITY (a printed
// Face().Abilities index or a granted SVar anchor) rather than casting a
// spell. Every "is this an ability" test in the flow reads this, never the
// raw index, so a granted proposal -- whose ability field is -1 -- takes the
// ability arms (no spell legality recheck, no cast trigger, the ability
// payment/mint branch, no modes ask) instead of the spell ones.
func (pc *pendingCast) isAbility() bool {
	return pc.ability >= 0 || pc.grantSVar != "" || pc.gainedFrom != 0 || pc.grantKeyword != ""
}

// pcAbility resolves the proposal's ability body. A printed activation reads
// its Face().Abilities index; a granted activation (task grantcost1) resolves
// the SVar anchor off the GRANTOR's face -- the recipient's face has no such
// SVar, which is the whole reason the anchor exists. The resolve is
// deterministic (ParseSVar over a fixed table), so re-resolving at each
// read-site cannot drift; a grantor that left the battlefield (or an SVar the
// grantor's face no longer names -- a stale proposal) resolves to nil and the
// caller degrades the way a stale option always has.
func (e *Engine) pcAbility(pc *pendingCast) *cards.SA {
	if pc.grantKeyword != "" {
		// A keyword-granted body (CR 613.1f, the AddKeyword$ Cycling/TypeCycling
		// route): synthesized from the derived keyword line, a pure function of
		// the string -- no state read, so every read site and a replay re-derive
		// the identical SA. A line no synthesizer can model resolves nil and the
		// caller degrades the way a stale option always has.
		return cards.GrantedKeywordAbility(pc.grantKeyword)
	}
	if pc.gainedFrom != 0 {
		// A has-all-abilities-of body: the SA is the named foreign face's
		// own compiled ability at gainedIdx. A card that left the scoped zone
		// (or a stale index) resolves to nil and the caller degrades the way
		// a stale option always has.
		fo := e.G.Obj(pc.gainedFrom)
		if fo == nil || fo.Face() == nil {
			return nil
		}
		abilities := fo.Face().Abilities
		if pc.gainedIdx < 0 || pc.gainedIdx >= len(abilities) {
			return nil
		}
		return abilities[pc.gainedIdx]
	}
	if pc.grantSVar == "" {
		if pc.ability < 0 {
			return nil
		}
		o := e.G.Obj(pc.card)
		if o == nil || o.Face() == nil {
			return nil
		}
		// CR 702.140d: the index is a FLAT pile index (top face first, then
		// each under-card), the one enumeration the offer loop, events.Apply
		// and the activation-limit census share -- never a bare
		// Face().Abilities index, which would name a different ability on a
		// mutated pile. A plain permanent's index is unchanged.
		pa, ok := o.PileAbilityAt(pc.ability)
		if !ok {
			return nil
		}
		return pa.SA
	}
	return e.grantedSAFrom(pc.grantSource, pc.card, pc.grantSVar)
}

// cyclingKeyword returns the Forge keyword head ("Cycling" or "TypeCycling")
// when the activation pc's own ability is a cycling ability (CR 702.29), and
// "" for every other activation. It is the provenance events.DiscardCostCycling
// records on the cost discard: the discard is a cycle because THIS ability
// paid for it, whether the cycling is printed (K:Cycling) or granted (a layer's
// AddKeyword$ Cycling / K:TypeCycling, e.g. Rhet-Tomb Mystic, Tectonic
// Reformation, Homing Sliver). Resolved through pcAbility, so it names the
// ability a granted activation actually resolved rather than the card's face.
// TypeCycling is a variant of cycling (CR 702.29d's "[type]cycling"), so it
// tags a Mode$ Cycled trigger too.
func (e *Engine) cyclingKeyword(pc *pendingCast) string {
	ab := e.pcAbility(pc)
	if ab == nil {
		return ""
	}
	switch kw := strings.TrimSpace(ab.ParamStr(cards.PKKeyword)); kw {
	case "Cycling", "TypeCycling":
		return kw
	default:
		return ""
	}
}

// resolvedMana returns the cost the announced payment actually commits: X
// folded, every hybrid and Phyrexian pip removed (each was announced by
// manaAsk into payColor/payLife), and the announced coloured spend folded
// into Colored so payMana charges it from the pool. payLife is applied
// separately by payCast. For a cost with no hybrid or Phyrexian pip this is
// just the X-folded cost, so ordinary casting is unchanged.
func (pc *pendingCast) resolvedMana() Cost {
	m := pc.cost.WithX(pc.x)
	m.Hybrid = nil
	m.Phyrexian = nil
	m.Twobrid = nil
	m.HybridPhyrexian = nil
	for i := range pc.payColor {
		m.Colored[i] += pc.payColor[i]
	}
	m.Generic += pc.payGeneric
	return m
}

// resolvedMana is the X-folded, pip-resolved cost (see above). manaToPay is
// the full CR 601.2f composition on top of it.
func (pc *pendingCast) resolvedManaX(x int32) Cost {
	m := pc.cost.WithX(x)
	m.Hybrid = nil
	m.Phyrexian = nil
	m.Twobrid = nil
	m.HybridPhyrexian = nil
	for i := range pc.payColor {
		m.Colored[i] += pc.payColor[i]
	}
	m.Generic += pc.payGeneric
	return m
}

// repriceForTargets refreshes the modifier snapshot after CR 601.2c chooses
// targets and before CR 601.2h pays. ValidTarget$ is necessarily unavailable
// at the initial offer, but it is a cost requirement rather than a
// resolution-time condition, so this is the one point every spell and
// activation can apply it. It deliberately preserves taxGeneric: commander
// tax is independent of the chosen target and is captured at proposal start.
func (e *Engine) repriceForTargets(pc *pendingCast) {
	o := e.G.Obj(pc.card)
	if o == nil || o.Face() == nil {
		return
	}
	// Strive (CR 702.52a): the per-extra-target additional cost is priced
	// here, the same post-601.2c pre-601.2h seam the modifier refresh below
	// owns, because the payment count (targets minus one) exists only once
	// the target answer is in. Spell-only: an activated ability's face
	// carries no Strive and pc.targets means something else on that path.
	if !pc.isAbility() && pc.striveSet {
		e.foldStriveCost(pc, o.Face())
	}
	scope := spellScope(pc.mode)
	if ab := e.pcAbility(pc); ab != nil {
		scope = abilityScope(ab)
		// The ability's own target-dependent ReduceCost$ (Raft Security
		// Officer's AllTargeted$Valid Creature.powerLE3): beginActivation
		// folded pc.ownReduce with nil targets (full price at offer time,
		// fail closed); now the CR 601.2c answer exists, so re-evaluate and
		// net-adjust the generic by the delta. The Ctx binds BOTH the root's
		// own targets and the whole-chain union (alltargeted1): the sub-ask
		// answers are already in hand -- Forge pre-asks the chain before
		// payment -- so an AllTargeted$ body reads the union, not 0. The net
		// form is idempotent -- a second pass computes delta 0 -- which
		// matters because repriceForTargets can run again on a mana-window
		// resume, and once more per sub-ask answer as the union grows.
		if n := e.ownReduceCost(pc.player, pc.card, ab, pc.targets, pc.allTargets(), pc.abilityMerged); n != pc.ownReduce {
			pc.cost.Generic = addClampedGeneric(pc.cost.Generic, int64(pc.ownReduce-n))
			pc.ownReduce = n
		}
	}
	pc.mods = e.costModifiersForTargets(pc.player, pc.card, scope, pc.targets)
}

// foldStriveCost prices the Strive keyword's per-extra-target additional
// cost (CR 702.52, "this spell costs <cost> more for each target beyond the
// first") into pc.cost now that CR 601.2c's answer is known. The count is
// len(pc.targets)-1; striveUnits records how many payments are already
// folded, so a re-entry (a sub-ask answer re-runs repriceForTargets, the
// ownReduce delta shape) is an exact delta and never double-charges. An
// unpriceable parameter (ParseCost's degraded Unknown tokens) is a loud
// Note and no charge (the escalate convention), never a fabricated generic.
// Targets only grow across the flow's re-entries, so a shrinking want is
// unreachable; it is clamped to no-op rather than refunded.
func (e *Engine) foldStriveCost(pc *pendingCast, f *cards.Face) {
	// A stale proposal whose face no longer carries the keyword charges
	// nothing rather than reading the last captured parameter blindly.
	if _, ok := f.KeywordParam("Strive"); !ok {
		return
	}
	want := int32(len(pc.targets)) - 1
	if want < 0 {
		want = 0
	}
	if want == pc.striveUnits {
		return
	}
	sc := ParseCost(pc.striveParam)
	if len(sc.Unknown) > 0 {
		if want > pc.striveUnits {
			e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card,
				Text: "strive cost unpriceable; casting without the strive charge"})
		}
		pc.striveUnits = want
		return
	}
	for i := pc.striveUnits; i < want; i++ {
		pc.cost = pc.cost.Plus(sc)
	}
	pc.striveUnits = want
}

// targetDependentCostMayPay is targetAsk's pre-payment exception: before a
// target is selected, the ordinary modifier snapshot intentionally excludes
// ValidTarget$ statics. Do not abort the proposal merely because that base
// snapshot is unaffordable when some legal target can make a reduction apply;
// repriceForTargets will replace the potential snapshot with the actual one
// as soon as the target answer arrives.
func (e *Engine) targetDependentCostMayPay(pc *pendingCast) bool {
	scope, ok := e.pendingCastScope(pc)
	if !ok {
		return false
	}
	delve := int32(0)
	if !pc.isAbility() {
		delve = int32(len(pc.delve))
	}
	_, ok = e.potentialCostModsUsing(e.collectCostStatics(), pc.player, pc.card, scope, e.costPotentialTargets(pc.player, pc.card, scope), 0, func(mods costMods) bool {
		return e.manaFeasibleDescriptor(pc.player, paymentForCast(pc, pc.resolvedMana()), pc.resolvedMana(), mods, pc.taxGeneric, delve, pipRider{anyColor: pc.mayPlayIgnore, anyType: pc.mayPlayIgnoreType})
	})
	return ok
}

// pendingCastScope returns the exact spell or ability scope whose modifiers
// price pc. Keeping this derivation shared by the potential-target gate and
// the final target menu makes a new ValidSpell$/Type$ rule reach both sides
// of the cast transaction rather than admitting a target the payment phase
// will price under different modifiers.
func (e *Engine) pendingCastScope(pc *pendingCast) (costScope, bool) {
	o := e.G.Obj(pc.card)
	if o == nil || o.Face() == nil {
		return costScope{}, false
	}
	if !pc.isAbility() {
		return spellScope(pc.mode), true
	}
	ab := e.pcAbility(pc)
	if ab == nil {
		return costScope{}, false
	}
	return abilityScope(ab), true
}

// affordableTargetCandidates filters legal CR 115 targets to the choices
// whose final target-dependent cost can complete this transaction. A target
// is tested as the sole selection: that is exact for the normal one-target
// shape and conservatively safe for multi-target declarations (where the
// decision API cannot express that one option requires another option).
//
// The probe also reprices the ability's own target-dependent ReduceCost$
// per candidate (belt_of_giant_strength's Targeted$CardPower): the offer
// gate folded the BEST legal target's reduction into pc.cost (pc.ownReduce),
// so without this fold a weaker candidate reads payable at the best-target
// offer price while repriceForTargets will actually charge its own, higher
// price at CR 601.2h -- an abort after an apparently-legal target choice.
// The delta is exactly the net shift repriceForTargets applies (same
// helper, idempotent fold); it is never negative because the offer's max
// runs over the same candidate set costPotentialTargets derives from
// legalTargetCandidates, and the clamp keeps that invariant load-bearing.
//
// The probe is a pure read (it emits nothing and writes no state; the
// convoke fold and window census are probed, never charged), so it runs in
// one Derived memo scope (derivedmemo.go): the per-candidate cost checks
// re-derive the same objects -- nonManaCastable's discard census matches
// every hand card once PER CANDIDATE -- and a mass of candidates otherwise
// makes the ask O(candidates x hand) layer walks.
func (e *Engine) affordableTargetCandidates(pc *pendingCast, candidates []targetCandidate) []targetCandidate {
	e.beginDerivedMemo()
	defer e.endDerivedMemo()
	scope, ok := e.pendingCastScope(pc)
	if !ok {
		return nil
	}
	pl := e.G.Players[pc.player]
	// Only a root-target-dependent own reduction needs a proven window
	// reachability check. Other activations retain their existing mana-window
	// offer semantics (including sources this static probe cannot price).
	targetDiscount := pc.isAbility() && pc.ownReduce > e.ownReduceCost(pc.player, pc.card, e.pcAbility(pc), nil, nil, pc.abilityMerged)
	var windowUnits []windowManaUnit
	if targetDiscount {
		windowUnits = e.castWindowUnits(pc)
	}
	out := make([]targetCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		target := state.Target{Obj: candidate.obj}
		if candidate.kind == "player" {
			target = state.Target{Player: candidate.player, IsPlayer: true}
		}
		mods := e.costModifiersForTargets(pc.player, pc.card, scope, []state.Target{target})
		cost := mods.apply(pc.resolvedMana())
		if pc.ownReduce > 0 {
			if n := e.ownReduceCost(pc.player, pc.card, e.pcAbility(pc), []state.Target{target}, nil, pc.abilityMerged); n < pc.ownReduce {
				cost.Generic = addClampedGeneric(cost.Generic, int64(pc.ownReduce-n))
			}
		}
		// An announce-bound Exile part (the Shoal cycle's cmcEQX) is priced
		// by the ANNOUNCED X, not by the candidate: nonManaCastable below
		// evaluates a non-literal cmc comparison fail-closed (it has no X
		// binding in scope), so an alt-cost cast whose only payment is such
		// an exile would drop every candidate and reverse the proposal at
		// the target ask. The offer gate (altCostXCandidates' existential
		// over X) and the X ask settled the part's payability independent of
		// any target, so the probe re-runs the exact exAsk binding for it
		// rather than dropping it silently. Parts are collected and filtered
		// AFTER the loop — mutating cost.Exile mid-range would splice wrong
		// indices once a second announce-bound part exists.
		exileSpent := make([]bool, len(cost.Exile))
		for i, part := range cost.Exile {
			if pc.announceX == "" || !strings.Contains(part.Spec, "cmcEQ"+pc.announceX) {
				continue
			}
			zone := part.Zone
			if zone == 0 {
				zone = state.ZHand
			}
			sc := e.withNames(effects.NewSpecContext(pc.player, pc.card))
			sc.Resolve = func(n string) (int32, bool) {
				if n == pc.announceX {
					return pc.x, true
				}
				return 0, false
			}
			n := 0
			for _, oid := range e.G.Zone(zone, pc.player) {
				if e.matchesSpec(part.Spec, oid, sc) {
					n++
				}
			}
			if n >= int(part.N) {
				exileSpent[i] = true
			}
		}
		for _, spent := range exileSpent {
			if !spent {
				continue
			}
			kept := make([]CostPart, 0, len(cost.Exile))
			for i, part := range cost.Exile {
				if !exileSpent[i] {
					kept = append(kept, part)
				}
			}
			cost.Exile = kept
			break
		}
		cost.Generic = addClampedGeneric(cost.Generic, int64(pc.taxGeneric))
		if !pc.isAbility() {
			cost.Generic -= int32(len(pc.delve))
			if cost.Generic < 0 {
				cost.Generic = 0
			}
		}
		// Mana abilities cannot make a non-mana payment or a life shortage
		// disappear, so preserve a candidate for the mana window only after
		// those independent requirements pass.
		if !e.nonManaCastable(pc.player, pc.card, cost, pc.isAbility(), tapCostSAKind(e.pcAbility(pc))) {
			continue
		}
		// A life cost with a POSITIVE component needs that much life (CR
		// 119.4: paying N>0 life requires life >= N). A cost with NO life
		// component pays 0 life, which is always legal whatever the life
		// total -- a payer dropped below 0 mid-cast (Ancient Tomb's rider)
		// may still select targets and finish paying (CR 704.3: state-based
		// actions wait for a player to receive priority). The same gate
		// lives in resolveManaWith, which payCast charges through.
		if cost.Life > 0 && cost.Life > pl.Life {
			continue
		}
		// resolvedMana carries no live pip, so manaFeasible (the shared
		// primitive) here degenerates to the composed payable check — the same
		// composition payCast will charge for this candidate's repricing. The
		// announced Convoke/Harmonize/Improvise contributions fold in exactly
		// the way paymentMana folds them into the charged total (applyConvoke
		// on the composed mods+tax+delve cost), so a cast whose pool alone
		// cannot pay but whose announced artifacts/creatures can keeps its
		// targets on the menu instead of being reversed at this ask. The
		// announcement itself was already gate-checked for absorbability
		// (convokeAbsorbs), so the fold is the payment's own arithmetic,
		// probed, never charged.
		convoked := e.applyConvoke(pc, cost)
		pay := paymentForCast(pc, convoked)
		if e.manaFeasibleDescriptor(pc.player, pay, convoked, costMods{}, 0, 0, pipRider{anyColor: pc.mayPlayIgnore, anyType: pc.mayPlayIgnoreType}) {
			out = append(out, candidate)
			continue
		}
		if !cost.HasManaPayment() {
			continue
		}
		if targetDiscount {
			// A best-target discount can make this option affordable before
			// choosing targets while a weaker target is not. An arbitrary
			// untapped source is not proof that the 601.2g window can cover
			// the difference. Probe the window's concrete free productions,
			// one alternative per source, instead of offering a target whose
			// activation will abort at payment (CR 601.2h).
			av := e.manaAvailableFor(pc.player, pay)
			if e.castWindowReachable(pc.player, convoked, av.pool, pl.Snow, av.typed, pl.Life,
				e.paymentConv(pc.player, pay.id, pay.class == paymentActivated), windowUnits) {
				out = append(out, candidate)
			}
		} else if e.hasUntappedManaSource(pc.player) {
			out = append(out, candidate)
		}
	}
	return out
}

// manaToPay is the CR 601.2f total-cost composition for pc: resolvedMana
// ({X} folded and flexible-pip announcement recorded), then cost increases
// and reductions, then the CR 903.8 commander tax. Delve credit is the
// caller's concern (targetAsk/payCast subtract pc.delve from Generic).
func (e *Engine) manaToPay(pc *pendingCast) Cost {
	m := pc.mods.apply(pc.resolvedMana())
	if costAnnouncesPaidX(pc.cost) {
		// The announced sacrifice count re-prices the ReduceCost statics that
		// read the paid X (Dargo's {2}-less-per-sacrifice): the offer-time
		// pc.mods snapshot was bound to X=0.
		if scope, ok := e.pendingCastScope(pc); ok {
			m = e.costModifiersForTargetsX(pc.player, pc.card, scope, pc.targets, pc.x).apply(pc.resolvedMana())
		}
	}
	m.Generic += pc.taxGeneric
	return m
}

// costAnnouncesSacX reports whether the cost carries a Sac<X/Spec> part whose
// count the cast announces (the Dargo shape).
func costAnnouncesSacX(c Cost) bool {
	for _, part := range c.Sac {
		if part.Announced {
			return true
		}
	}
	return false
}

// costAnnouncesPaidX reports whether the cost carries ANY announced-count
// part whose count the cast announces as X: the Sac<X/Spec> shape
// (costAnnouncesSacX), the announced SubCounter<X/Kind> removal and the
// announced PayLife<X> or ExileFromGrave<X/Spec> payment. The offer-time
// costModifiers snapshot was bound to X=0, so a static reading the paid X
// must be re-priced once the
// announcement is known -- the same reason Dargo's Sac<X> needed it.
func costAnnouncesPaidX(c Cost) bool {
	if costAnnouncesSacX(c) {
		return true
	}
	if len(c.LifeX) > 0 {
		return true
	}
	for _, part := range c.SubCounter {
		if part.Announced {
			return true
		}
	}
	for _, part := range c.Exile {
		if part.Announced {
			return true
		}
	}
	// An X-form tapXType part binds the same X (the election announced it or
	// the tap settle paid the announced value), so a ReduceCost static
	// reading Count$xPaid is re-priced on it the same way.
	for _, part := range c.TapPermanent {
		if part.Dyn == "X" {
			return true
		}
	}
	// A named-announcement count (the March cycle's Exiled, Explosive
	// Singularity's Tapped) feeds its Relative$ ReduceCost the same way.
	return costHasNamedCount(c)
}

// manaToPayX is manaToPay with {X} folded to an explicit value.
// paymentMana applies announced Convoke/Harmonize contributions to the
// already-formed total. A stale answer can never make a requirement negative.
// faceWantsConvoked reports whether the cast's face could have a reader of
// the Convoked provenance: an SVar body or ability parameter naming the
// `Defined$ Convoked` selector (Lethal Scheme's DBConnive, Venerated
// Loxodon's and Zephyr Singer's TrigPutCounterAll) or a filter that names the
// `Convoked` referent of the sharesCardTypeWith/sharesCreatureTypeWith family
// (Everything Comes to Dust's ChangeType$ `...sharesCreatureTypeWith
// Convoked...`). The string scan is the faceWantsConverge shape; the
// shares-referent half goes through effects.SpecUsesConvokedReferent, the
// SAME classifier the matcher uses, so the provenance gate can never drift
// from who reads Convoked.
func faceWantsConvoked(f *cards.Face) bool {
	if f == nil {
		return false
	}
	for _, v := range f.SVars {
		if strings.Contains(v, "Defined$ Convoked") || effects.SpecUsesConvokedReferent(v) || effects.SpecUsesConvokedAmount(v) {
			return true
		}
	}
	for _, a := range f.Abilities {
		if strings.EqualFold(strings.TrimSpace(a.ParamStr(cards.PKDefined)), "Convoked") {
			return true
		}
		if abilityParamsUseConvoked(a.Params) {
			return true
		}
	}
	return false
}

// abilityParamsUseConvoked is the ability half of faceWantsConvoked: a
// whole-map VALUE scan -- it reads no specific Params key, only every value,
// the same shape the SVar loop above reads f.SVars with -- so it takes the
// map as a plain map[string]string parameter (the paramcensus scanner's
// helper-passed-map form, with no key indexed and therefore no key read the
// census would attribute). Keeping it a value scan is the point: the
// share-family referent can ride any spec-bearing parameter, and a key
// whitelist here would silently miss the next one -- exactly the silent gap
// (sharesCreatureTypeWith Convoked classifying wordUnknown) this ticket
// fixed.
func abilityParamsUseConvoked(params map[string]string) bool {
	for _, v := range params {
		if effects.SpecUsesConvokedReferent(v) || effects.SpecUsesConvokedAmount(v) {
			return true
		}
	}
	return false
}

// faceWantsConverge is the heads-safety gate for the pay-time converge
// CastInfo: it reports whether the face carries a Count$Converge SVar body
// or the printed Sunburst keyword. Without it a count>0-only gate would stamp
// a CastInfo onto EVERY multicolour cast and move the chain heads; with it,
// no game that casts no converge card changes an event (measured: no repo-deck
// card carries Count$Converge or Sunburst, so TestHeads stays put). Sunburst
// is the second consumer of this seam (CR 702.47): its whole meaning is the
// converge count put as counters on entry (rules/replacement.go's
// sunburstEntryMatch), and the count must be captured at pay time because the
// Animate-granted shape can deliver the keyword only after payment.
func faceWantsConverge(f *cards.Face) bool {
	if f == nil {
		return false
	}
	if f.HasKeyword("Sunburst") {
		return true
	}
	for _, v := range f.SVars {
		if body, ok := strings.CutPrefix(v, "Count$"); ok && strings.EqualFold(strings.TrimSpace(body), "Converge") {
			return true
		}
	}
	return false
}

// faceWantsCastSpend is the heads-safety gate for the pay-time cast-spend
// CastInfo (the converge gate's shape): it reports whether the face's SVar
// table reads the TOTAL mana actually spent to cast the spell -- a body
// naming the Count$CastTotalManaSpent head (Freestrider Commando's
// SVar:X:Count$CastTotalManaSpent feeding its etbCounter CheckSVar$ gate).
// A trigger on ANOTHER permanent that reads the cast spell's spend through
// the ref-property spelling (TriggeredCard$CastTotalManaSpent -- Aberrant
// Manawurm, Manaform Hellkite, Muse Seeker) is invisible here because the
// CAST face is an ordinary instant/sorcery; that path is gated by
// triggeredCastSpendReaderOut below instead.
func faceWantsCastSpend(f *cards.Face) bool {
	if f == nil {
		return false
	}
	for _, body := range f.SVars {
		if strings.Contains(body, "Count$CastTotalManaSpent") {
			return true
		}
	}
	return false
}

// triggeredCastSpendReaderOut is the capture gate's second arm (the
// triggeredConvergeReaderOut shape): it reports whether any alive player's
// battlefield holds a permanent whose faces read the TRIGGER-relative spend
// spelling TriggeredCard$CastTotalManaSpent -- a trigger that reads ANOTHER
// spell's total spend, which faceWantsCastSpend cannot see because the cast
// face itself is an ordinary instant/sorcery. Only then does the pay-time
// CastInfo need stamping on a plain cast; a TriggerZones$ Battlefield
// SpellCast trigger can only exist for casts made while the reader is out, so
// this scan-at-pay-time gate stamps exactly when the value can be needed and
// no game without a reader out changes an event (heads stay put: no reader is
// in any repo deck). Pure read -- the boolean OR over the deterministic
// seat/zone walk cannot reach an event; replay re-runs payCast and derives the
// same scan.
func (e *Engine) triggeredCastSpendReaderOut() bool {
	g := e.G
	for _, p := range g.AliveFrom(0) {
		for _, id := range g.Zone(state.ZBattlefield, p) {
			if o := g.Obj(id); o != nil && e.objectReadsTriggeredCastSpend(o) {
				return true
			}
		}
	}
	return false
}

// objectReadsTriggeredCastSpend reports whether any face the ordinary trigger
// scan walks for o reads TriggeredCard$CastTotalManaSpent: the cast face, an
// unlocked Room's alternate face (roomTriggerFaces) and every merged
// under-card face (triggerFacesWithMerged). The shared face enumeration is
// what makes this the gate's structural twin of the trigger scan rather than
// a second, driftable list -- the next such face shape is covered without a
// new arm. Face.Mentions scans every string the face owns (SVars, keywords,
// ability params), so an inline parameter spelling is covered too, not just
// the SVar-table form the corpus uses today.
func (e *Engine) objectReadsTriggeredCastSpend(o *state.Object) bool {
	f := o.Face()
	if f == nil {
		return false
	}
	faces, n := roomTriggerFaces(o, f)
	walk := faces[:n]
	if len(o.MergedCards) > 0 {
		walk = triggerFacesWithMerged(o, walk)
	}
	for _, fc := range walk {
		// The per-face Mentions scan, memoised (face_scan_memo.go).
		if e.faceScanHas(fc.face, faceScanCastSpendReader) {
			return true
		}
	}
	return false
}

// faceWantsTimesKicked is the heads-safety gate for the pay-time multikick
// CastInfo on a PLAIN-Kicker cast mode (the converge gate's shape): it
// reports whether anything on the face reads the times-kicked count
// (Count$TimesKicked, op suffix included) -- an SVar value body, an ability
// parameter (DestAltSVar$), a trigger/replacement body, a static parameter
// or a keyword string. The 11 legacy plain-Kicker carriers read it from an
// SVar and get a real count stamped; The Five Doctors reads it inline in its
// ChangeZone's DestAltSVar$. A kicker card that never reads the count casts
// byte-identically to before. A multikicked-mode cast needs no gate -- its
// count>0 emission is the primitive itself.
//
// The walk is deliberately structural (cards.Face.Mentions scans every string
// the face owns) rather than an SVar-table-only scan: the earlier SVar-only
// form missed the inline parameter shape and would miss the next one (an
// ability's ChangeNum$, NumDmg$, or any future count-reading param),
// silently stamping no provenance for a script that reads it.
func faceWantsTimesKicked(f *cards.Face) bool {
	return f.Mentions("Count$TimesKicked")
}

// manaExpendReaderOut is the ManaExpend emission gate (the
// triggeredConvergeReaderOut pattern): true when the CASTING player's own
// battlefield holds a permanent whose live faces carry a Mode$ ManaExpend
// trigger. A ManaExpend trigger can only fire for its controller's own cast
// expenditure (every corpus line carries Player$ You), so scoping the scan to
// the caster's zone stamps exactly when the value can be needed and no game
// without a carrier out changes an event (heads stay put: no ManaExpend
// carrier is in any repo deck). Pure read -- the deterministic zone walk
// cannot reach an event; replay re-runs payCast and derives the same scan.
//
// The scan enumerates the SAME faces the ordinary trigger scan walks
// (roomTriggerFaces plus triggerFacesWithMerged), not just the printed top
// face: an unlocked Room's alternate face and a mutated pile's under-cards
// can each carry a ManaExpend trigger (CR 309.6, CR 702.140d), and the
// trigger scan would fire one if the wake-up event existed. Routing the gate
// through the shared helpers keeps the gate from silently under-stamping a
// shape the matcher supports -- the next such face shape is covered without
// a second list.
func (e *Engine) manaExpendReaderOut(player state.PlayerID) bool {
	g := e.G
	for _, id := range g.Zone(state.ZBattlefield, player) {
		o := g.Obj(id)
		if o == nil {
			continue
		}
		if objectHasManaExpendTrigger(o) {
			return true
		}
	}
	return false
}

// objectHasManaExpendTrigger reports whether any face the trigger scan walks
// for o carries a Mode$ ManaExpend trigger: the cast face, an unlocked Room's
// alternate face (roomTriggerFaces) and every merged under-card face
// (triggerFacesWithMerged). The shared face enumeration is what makes this
// the gate's structural twin of the scan rather than a second, driftable
// list.
func objectHasManaExpendTrigger(o *state.Object) bool {
	f := o.Face()
	if f == nil {
		return false
	}
	faces, n := roomTriggerFaces(o, f)
	walk := faces[:n]
	if len(o.MergedCards) > 0 {
		walk = triggerFacesWithMerged(o, walk)
	}
	for _, fc := range walk {
		if fc.face == nil {
			continue
		}
		for _, t := range fc.face.Triggers {
			if t.Mode == "ManaExpend" {
				return true
			}
		}
	}
	return false
}

// manaExpendAdd folds a paid cast's mana expenditure into the per-turn
// ManaExpend tally, resetting it first when the turn has moved on. Called
// UNCONDITIONALLY from payCast (before the gated wake-up emission), so the
// tally counts casts made while no carrier was out -- the pre-entry base the
// crossing test needs. Deterministic: e.G.Turn advances only through
// TurnChange, and replay's payCast re-execution folds the same calls in the
// same order.
func (e *Engine) manaExpendAdd(player state.PlayerID, spend int32) {
	if spend <= 0 {
		return
	}
	if e.manaExpendedTurn != e.G.Turn {
		e.manaExpendedTurn = e.G.Turn
		for i := range e.manaExpended {
			e.manaExpended[i] = 0
		}
	}
	if int(player) < len(e.manaExpended) {
		e.manaExpended[player] += spend
	}
}

// manaExpendTotal is the player's cumulative mana spent casting spells this
// turn -- the current-turn tally, or zero when the tally belongs to an
// earlier turn (no cast has stamped the new turn yet). trigmatch.ManaExpendMatches
// reads it for the crossing test.
func (e *Engine) manaExpendTotal(player state.PlayerID) int32 {
	if e.manaExpendedTurn != e.G.Turn || int(player) >= len(e.manaExpended) {
		return 0
	}
	return e.manaExpended[player]
}

// triggeredConvergeReaderOut is the capture gate's second arm: it reports
// whether any alive player's battlefield holds a permanent whose face SVars
// name TriggeredCard$Converge -- a trigger that reads ANOTHER spell's cast
// colours (Magmablood Archaic's "for each color of mana spent to cast that
// spell"), which the Count$Converge face gate cannot see because the cast
// face itself is an ordinary non-converge instant/sorcery. Only then does the
// pay-time CastInfo need stamping on a plain cast; a TriggerZones$ Battlefield
// SpellCast trigger can only exist for casts made while the reader is out, so
// this scan-at-pay-time gate stamps exactly when the value can be needed and
// no game without a reader out changes an event (heads stay put: neither
// Archaic is in any repo deck). Pure read -- the boolean OR over the
// deterministic seat/zone walk cannot reach an event; replay re-runs payCast
// and derives the same scan.
func (e *Engine) triggeredConvergeReaderOut() bool {
	g := e.G
	for _, p := range g.AliveFrom(0) {
		for _, id := range g.Zone(state.ZBattlefield, p) {
			o := g.Obj(id)
			if o == nil {
				continue
			}
			// faceConvergeSVarReader per face, memoised (face_scan_memo.go).
			if e.faceScanHas(o.Face(), faceScanConvergeReader) {
				return true
			}
		}
	}
	return false
}

// sunburstGrantOut is the converge capture gate's third arm (the
// triggeredConvergeReaderOut shape): it reports whether any alive player's
// battlefield holds a permanent whose face BODY grants sunburst to a spell --
// Solar Array's and Lux Artillery's `DB$ Animate | Keywords$ Sunburst`
// (task kw:Sunburst). The Animate grant lands on the spell AFTER payment (a
// SpellCast trigger resolving while the spell is on the stack), so the
// printed-keyword arm of faceWantsConverge cannot see it at pay time; this
// board scan arms the capture so the entering permanent reads its colours.
// Pure read over the deterministic seat/zone walk, so replay re-runs payCast
// and derives the same scan.
func (e *Engine) sunburstGrantOut() bool {
	g := e.G
	for _, p := range g.AliveFrom(0) {
		for _, id := range g.Zone(state.ZBattlefield, p) {
			o := g.Obj(id)
			if o == nil || o.Face() == nil {
				continue
			}
			// Mentions("Sunburst"), memoised (face_scan_memo.go).
			if e.faceScanHas(o.Face(), faceScanMentionsSunburst) {
				return true
			}
		}
	}
	return false
}

// convergeColours is CR 107.4f-family's converge count: the number of
// DISTINCT colours among W,U,B,R,G actually spent to cast the spell.
// Colourless/generic ({C}, generic pips) is not a colour and does not count;
// snow mana spent as a colour lives in the colour buckets here, so the plain
// per-colour delta is already right; mana conversion and the may-play
// ignore-colour rider changed what was actually paid, which is exactly what
// converge asks about.
func convergeColours(spent state.Mana) int32 {
	n := int32(0)
	for i := state.MW; i <= state.MG; i++ {
		if spent[i] > 0 {
			n++
		}
	}
	return n
}

// manaSpentTotal is the total mana a cast's payment actually spent: the
// spent delta's pips summed over every slot (coloured and colourless).
// Contribute the FULL delta -- a generic pip spent from a coloured unit is
// one mana spent -- so the sum is CR 601.2h's "mana spent to cast it".
func manaSpentTotal(spent state.Mana) int32 {
	var n int32
	for i := range spent {
		n += spent[i]
	}
	return n
}

func (e *Engine) paymentMana(pc *pendingCast) Cost {
	return e.applyConvoke(pc, e.manaToPay(pc))
}

// paymentManaX applies the same announced creature contributions after X is
// folded into the total. xAsk uses it so an X value funded by Convoke or
// Harmonize is actually offered, not rejected before the payment is known.
func (e *Engine) paymentManaX(pc *pendingCast, x int32) Cost {
	return e.paymentManaXUsing(pc, x, e.manaToPayXMods(pc, x))
}

// paymentManaXUsing is paymentManaX with an explicit modifier composition
// (xAsk's target-potential retry), so the announced Convoke/Harmonize
// contributions are folded onto the alternatively-priced total exactly as
// they are onto the ordinary one.
func (e *Engine) paymentManaXUsing(pc *pendingCast, x int32, mods costMods) Cost {
	return e.applyConvoke(pc, e.manaToPayXUsing(pc, x, mods))
}

func convokeManaSpent(pays []convokePayment) int32 {
	var n int32
	for _, pay := range pays {
		if pay.countsMana {
			n++
		}
	}
	return n
}

func (e *Engine) applyConvoke(pc *pendingCast, m Cost) Cost {
	for _, pay := range pc.convoke {
		if pay.color != 0 {
			i := state.ManaIndex(pay.color)
			if m.Colored[i] > 0 {
				m.Colored[i]--
			}
			continue
		}
		if pay.power > 0 {
			m.Generic -= pay.power
		} else {
			m.Generic--
		}
		if m.Generic < 0 {
			m.Generic = 0
		}
	}
	return m
}

// convokeCountCredit reduces composed cost m by the largest payment the
// caster's permanents can actually make through Convoke (CR 702.51),
// Harmonize (CR 702.46) or Improvise (CR 702.66) right now. It exists so a
// repeatable-cost count walk posed BEFORE the contributions are announced
// (replicateAsk and its multikicker/squad siblings) can see the mana a
// later Convoke announcement will free: without it the walk priced the cost
// against the pool alone and offered fewer payments than the board could
// pay. The assignment built here is the one convokeAsk then offers: a
// maximum matching covers as many coloured pips as distinct
// colour-eligible creatures allow, and every remaining eligible permanent
// takes one generic (a Harmonize permanent its power, per CR 702.46a). Both
// convokeAsk and this helper read the SAME composed total, so the payment
// the announcement is validated against (convokeAbsorbs's m) is the one
// priced here. A cast carrying none of the three keywords -- every
// pre-Convoke-replicate game -- returns m unchanged, so no game lacking one
// moves.
func (e *Engine) convokeCountCredit(pc *pendingCast, m Cost) Cost {
	isConvoke := e.hasCastConvoke(pc.card)
	isHarmonize := pc.mode == "harmonize"
	isImprovise := e.hasCastImprovise(pc.card)
	if !isConvoke && !isHarmonize && !isImprovise {
		return m
	}
	// candidate is one untapped permanent the announcement could tap, with
	// the payments it may make. A Convoke creature taps for a colour pip it
	// has or one generic; a Harmonize creature taps for its power in generic;
	// an Improvise artifact taps for one generic.
	type candidate struct {
		colors  string
		power   int32
		convoke bool
		generic bool // may pay one generic (Convoke creature or Improvise artifact)
	}
	var cands []candidate
	for _, id := range e.G.Zone(state.ZBattlefield, pc.player) {
		o := e.G.Obj(id)
		if o == nil || o.Tapped || o.Face() == nil || o.BestowedAttached() ||
			o.ReconfiguredAttached() || e.convokeCommitted(pc, id) {
			continue
		}
		c := candidate{}
		if o.EffectiveIsCreature() {
			if isConvoke {
				c.colors = e.objColors(o)
			}
			if isHarmonize {
				if p := e.Derived(id).Power; p > 0 {
					c.power = p
				}
			}
		}
		if isConvoke && o.EffectiveIsCreature() {
			c.convoke = true
			c.generic = true
		}
		if isImprovise && o.EffectiveIsArtifact() {
			c.generic = true
		}
		if c.convoke || c.power > 0 || c.generic {
			cands = append(cands, c)
		}
	}
	if len(cands) == 0 {
		return m
	}
	// Maximum bipartite matching of coloured pips to colour-eligible Convoke
	// creatures. The graph is tiny (at most five pip slots and the cast's
	// untapped creatures), so the classic augmenting-path walk is exact; any
	// maximal assignment here is a real announcement, and covering a pip frees
	// exactly one generic for the pool, so maximizing coloured coverage first
	// never costs generic units that could have been covered anyway.
	assign := make([]int, len(cands)) // cands index -> coloured slot, -1 unused
	for i := range assign {
		assign[i] = -1
	}
	var matchedCand func(slot int, seen []bool) bool
	matchedCand = func(slot int, seen []bool) bool {
		for i := range cands {
			if seen[i] || !cands[i].convoke || !strings.Contains(cands[i].colors, manaLetters[slot]) {
				continue
			}
			seen[i] = true
			if assign[i] == -1 || matchedCand(assign[i], seen) {
				assign[i] = slot
				return true
			}
		}
		return false
	}
	coveredPips := [6]int32{}
	// A Convoke creature taps for one mana of a colour it is (CR 702.51a),
	// so only the five coloured slots are matchable; {C} is never a
	// creature colour and the pool pays it.
	for slot := 0; slot < 5; slot++ {
		for n := int32(0); n < m.Colored[slot]; n++ {
			seen := make([]bool, len(cands))
			if matchedCand(slot, seen) {
				coveredPips[slot]++
			}
		}
	}
	for slot := range m.Colored {
		m.Colored[slot] -= coveredPips[slot]
	}
	// Every permanent not already paying a coloured pip takes one generic: a
	// Harmonize creature its power, a Convoke creature or Improvise artifact
	// one. Stop once the generic requirement is gone (each contribution must
	// reduce something, or the announcement is rejected).
	for i, c := range cands {
		if assign[i] >= 0 || m.Generic <= 0 {
			continue
		}
		reduce := int32(1)
		if c.power > 0 {
			reduce = c.power
		}
		if reduce > m.Generic {
			reduce = m.Generic
		}
		m.Generic -= reduce
	}
	return m
}

// convokeAbsorbs reports whether every announced Convoke/Harmonize payment
// actually reduces the outstanding mana requirement m when applied in
// announcement order. A colour contribution whose pip is already covered,
// or a generic/power contribution against a generic total already at {0},
// pays nothing -- CR 601.2b/702.51a let a creature be tapped only for a
// reduction the total cost still needs -- and payCast taps every announced
// creature, so an announcement containing such a no-op is an illegal
// over-payment that must be rejected, not silently tapped.
//
// With an unfixed {X} the generic requirement is not yet known when the
// announcement is answered (CR 601.2b announces Convoke before X), so a
// generic/power contribution against {0} generic is tentatively allowed
// there (xOpen) and xAsk prices the announcement against every candidate X
// with xOpen false, m already X-folded.
func (e *Engine) convokeAbsorbs(pc *pendingCast, m Cost, pays []convokePayment, xOpen bool) bool {
	for _, pay := range pays {
		if pay.color != 0 {
			i := state.ManaIndex(pay.color)
			if m.Colored[i] <= 0 {
				return false
			}
			m.Colored[i]--
			continue
		}
		if m.Generic <= 0 {
			if xOpen {
				continue
			}
			return false
		}
		reduce := pay.power
		if reduce <= 0 {
			reduce = 1
		}
		m.Generic -= reduce
		if m.Generic < 0 {
			m.Generic = 0
		}
	}
	return true
}

// validateSearch is the Submit-time gate for a hidden-library search
// decision (ResumeKind "search") whose SA carries ShareLandType$ True
// (Myriad Landscape's "up to two basic land cards that share a land type").
// The decision's static Validate sees only the offered option list -- no
// option carries the shared-type constraint -- so an answer naming two lands
// of disjoint types would pass it; this gate rejects such an answer before
// the intent is recorded and the pending decision is consumed, exactly like
// validateAttackers/validateCastContributions. Single-card answers are
// trivially legal (one card always shares with itself). Any other search --
// no ResumeSA, no ShareLandType$ -- is passed through untouched. The effect
// side (applyLibrarySearch's trim) keeps the same constraint for a host
// that bypassed the wire, through the one shared classifier
// effects.SharedLandTypes.
func (e *Engine) validateSearch(d *decision.Decision, in decision.Intent) error {
	if d.ResumeKind != "search" || d.ResumeSA == nil ||
		!strings.EqualFold(strings.TrimSpace(d.ResumeSA.Params["ShareLandType"]), "True") {
		return nil
	}
	ids := make([]state.ObjID, 0, len(in.Choices))
	for _, c := range in.Choices {
		if c < 0 || c >= len(d.Options) {
			continue // Validate's own out-of-range error already fired
		}
		if o := d.Options[c]; o.Obj != 0 {
			ids = append(ids, o.Obj)
		}
	}
	if !effects.SharedLandTypes(e.G, ids) {
		return fmt.Errorf("chosen cards do not share a land type")
	}
	return nil
}
