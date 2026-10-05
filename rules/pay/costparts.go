package pay

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// ExileFromTopCards returns the aggregate top-of-library prefix paid by a set
// of ExileFromTop parts. Parts do not each get to reuse the same prefix.
func ExileFromTopCards(lib []state.ObjID, parts []costvocab.CostPart) ([]state.ObjID, bool) {
	var n int64
	for _, part := range parts {
		if part.N <= 0 {
			return nil, false
		}
		n += int64(part.N)
	}
	if n > int64(len(lib)) {
		return nil, false
	}
	return append([]state.ObjID(nil), lib[:int(n)]...), true
}

// CastFlowDrawPlayer resolves a Draw cost part's spec inside the
// cast/activation flow: the payer draws (spec "", You, Player, Self), and
// Player.Activator too, because within an activation the activator IS the
// payer. A trigger-only role has no binding here; nonManaCastable blocks
// such a part so it is never offered.
func CastFlowDrawPlayer(spec string, payer state.PlayerID) (state.PlayerID, bool) {
	switch castFlowDrawPlayerCodes.Code(string(spec)) {
	case castFlowDrawPlayerPayer:
		return payer, true
	}
	return 0, false
}

func CostCandidates(e Engine, p state.PlayerID, source state.ObjID, zone state.Zone, spec string, excludeSource, untapped bool) []state.ObjID {
	var out []state.ObjID
	for _, id := range e.Game().Zone(zone, p) {
		o := e.Game().Obj(id)
		if o == nil || (zone == state.ZBattlefield && !ExistsOnBattlefield(o)) || (excludeSource && id == source) || (untapped && o.Tapped) {
			continue
		}
		if e.MatchesSpecFrom(spec, id, p, source) {
			out = append(out, id)
		}
	}
	return out
}

// TapCostCandidates is costCandidates for one TapPermanent (tapXType) part:
// a part bound to a granted ability's grantor (bindGrantedCostReferents --
// Fishing Pole's "Tap Fishing Pole") names exactly that permanent, offered
// while the payer controls it untapped on the battlefield; every other part
// is the ordinary untapped filter scan. Offer, planning and payment all read
// this one helper, so they cannot disagree about who can pay.
func TapCostCandidates(e Engine, p state.PlayerID, source state.ObjID, part costvocab.CostPart) []state.ObjID {
	if part.Referent == 0 {
		return CostCandidates(e, p, source, state.ZBattlefield, part.Spec, false, true)
	}
	o := e.Game().Obj(part.Referent)
	if o == nil || o.Zone != state.ZBattlefield || o.Controller != p || !ExistsOnBattlefield(o) || o.Tapped {
		return nil
	}
	return []state.ObjID{part.Referent}
}

// RevealOrChooseCandidates returns the objects that can pay one
// RevealOrChoose<N/Spec> part, hand cards first (the REVEAL arm) then
// battlefield permanents (the CHOOSE arm). The two arms' candidate lists are
// DISTINCT -- a card in hand is never a legal choose-arm candidate and a
// permanent is never a legal reveal-arm candidate -- so the ask can offer
// them as separate option kinds and the payment records which arm was used.
// The choose arm scans the payer's own battlefield (costCandidates' zone walk
// is controller-scoped), which is the "you control" the card text requires;
// a permanent controlled by an opponent is not offered. One helper backs both
// the offer gate (nonManaCastable) and the ask so the count that offered the
// cast and the objects the payer may elect cannot disagree.
func RevealOrChooseCandidates(e Engine, p state.PlayerID, source state.ObjID, part costvocab.CostPart) (hand, battlefield []state.ObjID) {
	const closeEncounter = "Creature.YouCtrl+inZoneBattlefield;Creature.YouOwn+inZoneExile+warped"
	if part.Spec == closeEncounter {
		// ChooseCard is a choice-only cost. Build one deterministic list from
		// the two printed alternatives; exile is indexed by owner, while the
		// battlefield is indexed by controller.
		for _, id := range e.Game().Zone(state.ZBattlefield, p) {
			o := e.Game().Obj(id)
			if o != nil && ExistsOnBattlefield(o) && o.EffectiveIsCreature() {
				battlefield = append(battlefield, id)
			}
		}
		for _, id := range e.Game().Zone(state.ZExile, p) {
			o := e.Game().Obj(id)
			if o != nil && o.Face() != nil && o.Face().IsCreature() && o.CastFlags&state.FlagWarped != 0 {
				battlefield = append(battlefield, id)
			}
		}
		return nil, battlefield
	}
	hand = CostCandidates(e, p, source, state.ZHand, part.Spec, true, false)
	battlefield = CostCandidates(e, p, source, state.ZBattlefield, part.Spec, false, false)
	return hand, battlefield
}

// LastDrawnThisTurn returns the card player p most recently DREW this turn
// (the last events.Draw naming p since the most recent TurnChange), or 0 if p
// drew nothing this turn. Derived from the event log exactly like
// CardsDrawnThisTurn, so a replay derives it identically; it is NOT a filter.
// Discard<1/LastDrawn> (Jandor's Ring) is the corpus's only carrier.
func LastDrawnThisTurn(e Engine, p state.PlayerID) state.ObjID {
	for i := len(e.Log().Events) - 1; i >= 0; i-- {
		ev := e.Log().Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.Draw && ev.Player == p {
			return ev.Obj
		}
	}
	return 0
}

// DiscardCandidates returns the still-available cards that can pay one
// Discard cost part. Random names a selection method rather than a card
// characteristic, and a Hand spec is Forge's "discard your hand" shape
// (the corpus spells its ignored count as both 0 and 1). LastDrawn is the
// other history-keyed slot: "the last card you drew this turn", one specific
// card, unpayable when p drew nothing or that card has left the hand.
// A spell being announced is excluded because it will be on the stack when
// costs are paid; an activated ability's source may remain in hand and can
// therefore pay CARDNAME/NICKNAME costs such as channel and bloodrush.
func DiscardCandidates(e Engine, p state.PlayerID, source state.ObjID, part costvocab.CostPart, casting bool, reserved map[state.ObjID]bool) []state.ObjID {
	all := strings.EqualFold(part.Spec, "Random") || strings.EqualFold(part.Spec, "Hand")
	matchSpec := part.Spec
	if strings.EqualFold(matchSpec, "NICKNAME") {
		// Forge uses NICKNAME as the same self-reference as CARDNAME in the
		// four discard-cost lines that carry it.
		matchSpec = "CARDNAME"
	}
	if strings.EqualFold(matchSpec, "LastDrawn") {
		// The single candidate is the last card p drew this turn, and only if
		// it is still in p's hand. If p drew nothing, or that card has left
		// the hand, the cost is unpayable -- do not skip back to an earlier
		// draw.
		last := LastDrawnThisTurn(e, p)
		if last != 0 && !reserved[last] && !(casting && last == source) {
			if o := e.Game().Obj(last); o != nil && o.Zone == state.ZHand {
				return []state.ObjID{last}
			}
		}
		return nil
	}
	var out []state.ObjID
	for _, id := range e.Game().Zone(state.ZHand, p) {
		if reserved[id] || (casting && id == source) {
			continue
		}
		if all || e.MatchesSpecFrom(matchSpec, id, p, source) {
			out = append(out, id)
		}
	}
	return out
}

// DiscardCostPayable is the offer-side totality gate for Discard costs. It
// mirrors discardAsk's deterministic reservation walk without consuming RNG.
func DiscardCostPayable(e Engine, p state.PlayerID, source state.ObjID, parts []costvocab.CostPart, casting bool) bool {
	reserved := map[state.ObjID]bool{}
	for _, part := range parts {
		if part.Announced {
			// An announced Discard<X/Spec> (the RaiseCost bridge's Aether
			// Tide shape): X = 0 is a legal announcement, and xAsk caps the
			// X by the matching cards, so the part never withholds an offer.
			continue
		}
		candidates := DiscardCandidates(e, p, source, part, casting, reserved)
		if strings.EqualFold(part.Spec, "Hand") {
			for _, id := range candidates {
				reserved[id] = true
			}
			continue
		}
		if part.N <= 0 || int32(len(candidates)) < part.N {
			return false
		}
		for i := int32(0); i < part.N; i++ {
			reserved[candidates[i]] = true
		}
	}
	return true
}

// SpellsCastThisTurn counts PutOnStack events for player p since the last
// TurnChange in the log (or since the start of the log, on turn 1).
func SpellsCastThisTurn(e Engine, p state.PlayerID) int {
	n := 0
	for i := len(e.Log().Events) - 1; i >= 0; i-- {
		ev := e.Log().Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.PutOnStack && ev.Player == p {
			n++
		}
	}
	return n
}

// WithSpellAbilityExtras folds a spell's own SpellAbility Cost$ ADDITIONAL
// (non-mana) parts into cost. It exists so the OFFER and the CHARGE cannot
// disagree about what a plain cast costs.
//
// They did disagree, and it wedged a live game. legal.go offered a plain cast
// after gating castable on adjustedCost alone -- the printed mana -- while
// beginCast folded the SpellAbility's Cost$ Sac part in afterwards. Village
// Rites ({B}, "As an additional cost, sacrifice a creature") was therefore
// offered to a player with no creature: sacAsk found zero candidates and
// aborted the cast, the abort consumed nothing, priority returned to a board
// identical to the one that produced the offer, and the same option was
// offered again -- an unbounded livelock (measured: a 5-event cycle repeating
// until the match was killed). The abort in sacAsk is correct and stays; what
// was wrong is that the option existed at all, which is the standing rule that
// an option that cannot be paid must never be offered.
//
// The alternative-cost path was given this same gate in an earlier round (see
// the ruling comment in legal.go's alternativeCosts loop). The base cast path
// has the identical hole and was missed, so both now go through this one
// definition rather than each repeating the fold.
//
// The mana part of a Cost$ is deliberately NOT folded: it RESTATES the printed
// mana cost rather than adding to it, so re-adding it would double charge.
// Every OTHER component -- Life/Sac/Discard/SubCounter/Tap, and the whole
// non-mana family including Exile, MoveToGrave, Reveal, Energy, Draw, LifeX,
// DamageYou and Mill -- is additional and is concatenated below.
func WithSpellAbilityExtras(f *cards.Face, cost costvocab.Cost) costvocab.Cost {
	sa := f.SpellAbility()
	if sa == nil {
		return cost
	}
	sc := sa.ParamStr(cards.PKCost)
	if sc == "" {
		return cost
	}
	return foldAdditionalCost(cost, costvocab.ParseCost(sc))
}

// foldAdditionalCost concatenates the non-mana parts of extra onto cost. It is
// THE one definition shared by two carriers that must agree: withSpellAbilityExtras
// folds a spell's own SpellAbility Cost$, and beginCast folds a RaiseCost
// static's non-mana Cost$ (Soul Immolation's `Cost$ Blight<X>`) that the cost
// composition carried in costMods.extra. The mana part of a Cost$ is
// deliberately NOT folded: it RESTATES the printed mana cost rather than
// adding to it, so re-adding it would double charge. Every OTHER component --
// Life/Sac/Discard/SubCounter/Tap, and the whole non-mana family including
// Exile, MoveToGrave, Reveal, Energy, Draw, LifeX, DamageYou, Mill and Blight --
// is additional and is concatenated below.
func foldAdditionalCost(cost, extra costvocab.Cost) costvocab.Cost {
	cost.Life = costvocab.AddClampedGeneric(cost.Life, int64(extra.Life))
	if len(extra.Sac) > 0 {
		cost.Sac = append(append([]costvocab.CostPart(nil), cost.Sac...), extra.Sac...)
	}
	if len(extra.Discard) > 0 {
		cost.Discard = append(append([]costvocab.CostPart(nil), cost.Discard...), extra.Discard...)
	}
	if len(extra.SubCounter) > 0 {
		cost.SubCounter = append(append([]costvocab.CostPart(nil), cost.SubCounter...), extra.SubCounter...)
	}
	if len(extra.Exile) > 0 {
		cost.Exile = append(append([]costvocab.CostPart(nil), cost.Exile...), extra.Exile...)
	}
	if len(extra.ExileFromTop) > 0 {
		cost.ExileFromTop = append(append([]costvocab.CostPart(nil), cost.ExileFromTop...), extra.ExileFromTop...)
	}
	if len(extra.MoveToGrave) > 0 {
		cost.MoveToGrave = append(append([]costvocab.CostPart(nil), cost.MoveToGrave...), extra.MoveToGrave...)
	}
	if len(extra.Reveal) > 0 {
		cost.Reveal = append(append([]costvocab.CostPart(nil), cost.Reveal...), extra.Reveal...)
	}
	if len(extra.RevealOrChoose) > 0 {
		cost.RevealOrChoose = append(append([]costvocab.CostPart(nil), cost.RevealOrChoose...), extra.RevealOrChoose...)
	}
	if len(extra.RevealChosen) > 0 {
		cost.RevealChosen = append(append([]costvocab.CostPart(nil), cost.RevealChosen...), extra.RevealChosen...)
	}
	if len(extra.Behold) > 0 {
		cost.Behold = append(append([]costvocab.CostPart(nil), cost.Behold...), extra.Behold...)
	}
	if len(extra.TapPermanent) > 0 {
		cost.TapPermanent = append(append([]costvocab.CostPart(nil), cost.TapPermanent...), extra.TapPermanent...)
	}
	if len(extra.Blight) > 0 {
		cost.Blight = append(append([]costvocab.CostPart(nil), cost.Blight...), extra.Blight...)
	}
	if len(extra.Energy) > 0 {
		cost.Energy = append(append([]costvocab.CostPart(nil), cost.Energy...), extra.Energy...)
	}
	if len(extra.Return) > 0 {
		cost.Return = append(append([]costvocab.CostPart(nil), cost.Return...), extra.Return...)
	}
	if len(extra.Draw) > 0 {
		cost.Draw = append(append([]costvocab.CostPart(nil), cost.Draw...), extra.Draw...)
	}
	if len(extra.LifeX) > 0 {
		cost.LifeX = append(append([]costvocab.CostPart(nil), cost.LifeX...), extra.LifeX...)
	}
	if len(extra.DamageYou) > 0 {
		cost.DamageYou = append(append([]costvocab.CostPart(nil), cost.DamageYou...), extra.DamageYou...)
	}
	if len(extra.GainLife) > 0 {
		cost.GainLife = append(append([]costvocab.CostPart(nil), cost.GainLife...), extra.GainLife...)
	}
	if len(extra.Mill) > 0 {
		cost.Mill = append(append([]costvocab.CostPart(nil), cost.Mill...), extra.Mill...)
	}
	if len(extra.Evidence) > 0 {
		cost.Evidence = append(append([]costvocab.CostPart(nil), cost.Evidence...), extra.Evidence...)
	}
	cost.Forage = cost.Forage || extra.Forage
	cost.Tap = cost.Tap || extra.Tap
	return cost
}

type castFlowDrawPlayerCode uint16

const (
	castFlowDrawPlayerPayer castFlowDrawPlayerCode = iota + 1
)

var castFlowDrawPlayerCodes = state.NewStrCodes(
	state.StrEntry[castFlowDrawPlayerCode]{Key: "", Val: castFlowDrawPlayerPayer},
	state.StrEntry[castFlowDrawPlayerCode]{Key: "You", Val: castFlowDrawPlayerPayer},
	state.StrEntry[castFlowDrawPlayerCode]{Key: "Player", Val: castFlowDrawPlayerPayer},
	state.StrEntry[castFlowDrawPlayerCode]{Key: "Self", Val: castFlowDrawPlayerPayer},
	state.StrEntry[castFlowDrawPlayerCode]{Key: "Player.Activator", Val: castFlowDrawPlayerPayer},
)

// SubCounterAvailable reports how many counters of the part's kind the object
// could give up: the kind's own count, or the object's TOTAL counter count
// for the "Any" kind (Forge's Any removes that many counters regardless of
// kind). Used by the offer gate, the X bound and the candidate walk.
func SubCounterAvailable(o *state.Object, kind string) int32 {
	if strings.EqualFold(kind, "Any") {
		total := int32(0)
		for _, c := range o.Counters {
			if c.N > 0 {
				total += c.N
			}
		}
		return total
	}
	return o.Counter(kind)
}

// SubCounterRemovalCandidates lists the permanents the payer could remove
// part's counters from. A source-anchored part (subCounterTargetsSource)
// offers just the source when it still carries enough counters; a filtered
// part offers every battlefield object the PAYER controls that matches the
// target spec (MatchesSpecFrom, the sacAsk machinery's read) and still
// carries enough counters, excluding ids already reserved by an earlier part
// of the same cost (sacs and earlier counter removals).
func SubCounterRemovalCandidates(e Engine, p state.PlayerID, source state.ObjID, part costvocab.CostPart, amt int32, reserved map[state.ObjID]bool) []state.ObjID {
	if costvocab.SubCounterTargetsSource(part.Target) {
		if o := e.Game().Obj(source); o != nil && o.Zone == state.ZBattlefield &&
			SubCounterAvailable(o, part.Spec) >= amt && (reserved == nil || !reserved[source]) {
			return []state.ObjID{source}
		}
		return nil
	}
	var out []state.ObjID
	for _, oid := range e.Game().Zone(state.ZBattlefield, p) {
		if reserved != nil && reserved[oid] {
			continue
		}
		o := e.Game().Obj(oid)
		if o == nil || SubCounterAvailable(o, part.Spec) < amt {
			continue
		}
		if e.MatchesSpecFrom(part.Target, oid, p, source) {
			out = append(out, oid)
		}
	}
	return out
}

// ExistsOnBattlefield reports whether o is a permanent the engine treats as
// existing (CR 702.25b): it is on the battlefield and not phased out. A
// phased-out permanent is treated as though it does not exist -- it cannot be
// targeted (rules/stack.go candidatesFor), activated, tapped or sacrificed as
// a cost, its static and triggered abilities are off, and it does not stay in
// combat (events.Apply's PhaseOut fold removes it, CR 702.25c). PhasedOut is
// only ever true on a battlefield permanent (the PhaseOut fold is
// battlefield-gated, the Move fold clears it), so gating a walk that already
// restricts itself to the battlefield on it is exact. Battlefield action
// and cost walks use this helper; mana-ability discovery and trigger scanning
// separately reject phased-out objects. Other readers must gate where relevant.
func ExistsOnBattlefield(o *state.Object) bool {
	return o != nil && o.Zone == state.ZBattlefield && !o.PhasedOut
}

// RememberedTargets lifts a ContinuousEffect's Remembered object ids into
// the []state.Target shape the filter grammar's SpecContext carries.
func RememberedTargets(ids []state.ObjID) []state.Target {
	if len(ids) == 0 {
		return nil
	}
	out := make([]state.Target, 0, len(ids))
	for _, id := range ids {
		out = append(out, state.Target{Obj: id})
	}
	return out
}

// OfferRetryFutile reports that offerCastableUsing's potential-target retry
// would re-ask exactly the mana question its first pass just failed: no
// cost static survived a target-independent gate (so the retry, whatever
// targets it binds, composes no static either), the composition is the zero
// costMods the retry's own empty composition is (no waterbend credit was
// folded on top), the scope is not an ability's (whose own ReduceCost$
// reads targets, ownManaReduction), and no ValidCard$ provenance capture is
// pending (the retry would leave it as the first pass did). The retry's
// accept is then manaFeasiblePriced over identical arguments, which failed.
func OfferRetryFutile(scope CostScope, mods *CostMods, mayApply, provenance bool) bool {
	if mayApply || provenance || (scope.Kind == "Ability" && scope.Ab != nil) {
		return false
	}
	return CostModsZero(mods)
}

func ManaTapsPayable(e Engine, p state.PlayerID, source state.ObjID, cost costvocab.Cost, reserved map[state.ObjID]bool) bool {
	claimed := make(map[state.ObjID]bool, len(reserved)+1)
	for id := range reserved {
		claimed[id] = true
	}
	if cost.Tap {
		claimed[source] = true
	}
	for _, part := range cost.TapPermanent {
		if part.Dyn != "" && part.Dyn != "X" {
			return false
		}
		cands := ManaTapCandidates(e, p, source, part.Spec, claimed)
		if part.Dyn == "X" {
			// The dynamic election itself announces X (0..len(cands)), and the
			// payment election in continueManaDiscard claims each elected
			// candidate, so the offer gate reserves nothing here: an X part is
			// affordable at X=0 whatever the candidate count.
			continue
		}
		if int32(len(cands)) < part.N {
			return false
		}
		for i := int32(0); i < part.N; i++ {
			claimed[cands[i]] = true
		}
	}
	return true
}

// ManaTapCandidates returns, in battlefield order, the untapped permanents
// that can pay one literal tapXType<N/Spec> part and are not already claimed.
// It is the one candidate walk shared by the offer gate (manaTapsPayable)
// and the payment election (continueManaDiscard), so the count the offer is
// priced on and the objects the payer may tap cannot diverge.
func ManaTapCandidates(e Engine, p state.PlayerID, source state.ObjID, spec string, claimed map[state.ObjID]bool) []state.ObjID {
	var out []state.ObjID
	for _, id := range CostCandidates(e, p, source, state.ZBattlefield, spec, false, true) {
		if !claimed[id] {
			out = append(out, id)
		}
	}
	return out
}

// ManaTapsPicked is manaTapsPayable's deterministic first-eligible pick set:
// the R-9 no-ask stand-in for a caller that cannot pose the tap election
// (interactive == false). It walks the same manaTapCandidates order the
// election offers, claiming each part's first N, so a non-interactive
// activation taps exactly what the silent build did and a replay rebuilds
// the identical Tap events.
func ManaTapsPicked(e Engine, p state.PlayerID, source state.ObjID, cost costvocab.Cost, sacs []state.ObjID) []state.ObjID {
	claimed := make(map[state.ObjID]bool, len(sacs)+1)
	for _, id := range sacs {
		claimed[id] = true
	}
	if cost.Tap {
		claimed[source] = true
	}
	var taps []state.ObjID
	for _, part := range cost.TapPermanent {
		if part.Dyn != "" {
			// A caller without a choice surface must not silently announce X=0
			// for an ability whose output depends on the dynamic tap count; the
			// whole non-interactive activation already fails closed in
			// resolveManaAbilityRefOriginal, so this is belt-and-braces.
			return nil
		}
		cands := ManaTapCandidates(e, p, source, part.Spec, claimed)
		n := int(part.N)
		if n > len(cands) {
			n = len(cands)
		}
		for i := 0; i < n; i++ {
			claimed[cands[i]] = true
			taps = append(taps, cands[i])
		}
	}
	return taps
}

func ManaDiscards(e Engine, p state.PlayerID, source state.ObjID, cost costvocab.Cost) ([]state.ObjID, bool) {
	var discards []state.ObjID
	reserved := map[state.ObjID]bool{}
	for _, part := range cost.Discard {
		candidates := DiscardCandidates(e, p, source, part, false, reserved)
		if strings.EqualFold(part.Spec, "Hand") {
			for _, id := range candidates {
				reserved[id] = true
				discards = append(discards, id)
			}
			continue
		}
		n := int(part.N)
		if n <= 0 || n > len(candidates) {
			return nil, false
		}
		for i := 0; i < n; i++ {
			id := candidates[0]
			reserved[id] = true
			discards = append(discards, id)
			candidates = candidates[1:]
		}
	}
	return discards, true
}

// ManaProducedSince returns the fixed-order WUBRGC set of mana types carried
// by the positive-amount ManaAdd events at or after log index mark, in the
// same order effects/trigger_referents.go documents for TriggerMana. A
// counter's trailing rune is its type: state.TypedManaTags and the snow "S"
// prefix a producer tag, never the type. Amount <= 0 is a spend or a zeroed
// unit, not production. Returns "" when the window produced nothing.
func ManaProducedSince(e Engine, mark int) string {
	if mark < 0 || mark > len(e.Log().Events) {
		return ""
	}
	var set uint8
	for _, ev := range e.Log().Events[mark:] {
		if ev.Kind != events.ManaAdd || ev.Amount <= 0 || ev.Counter == "" {
			continue
		}
		r := ev.Counter[len(ev.Counter)-1]
		if i := strings.IndexByte("WUBRGC", r); i >= 0 {
			set |= 1 << uint(i)
		}
	}
	if set == 0 {
		return ""
	}
	var b strings.Builder
	for i := range "WUBRGC" {
		if set&(1<<uint(i)) != 0 {
			b.WriteByte("WUBRGC"[i])
		}
	}
	return b.String()
}

// ManaSubCounterXBound returns the largest X a cost's announced
// SubCounter<X/Kind> parts can settle, and whether the cost carries any. It
// mirrors the cast path's xAsk SubCounter bound exactly (the largest
// candidate's counter count for a filtered part, the source's count for a
// source-anchored part, the aggregate for the "Any" kind), and takes the
// MIN across parts so a composed cost announces only what every part can
// settle. The offer gate and the X ask both read it, so the count an
// activation is priced on and the count the payer may announce cannot
// diverge.
func ManaSubCounterXBound(e Engine, p state.PlayerID, o *state.Object, source state.ObjID, cost costvocab.Cost) (int32, bool) {
	bound := int32(0)
	any := false
	for _, part := range cost.SubCounter {
		if !part.Announced {
			continue
		}
		have := int32(0)
		if costvocab.SubCounterTargetsSource(part.Target) {
			if o != nil {
				have = SubCounterAvailable(o, part.Spec)
			}
		} else {
			for _, oid := range SubCounterRemovalCandidates(e, p, source, part, 1, nil) {
				co := e.Game().Obj(oid)
				if co == nil {
					continue
				}
				n := SubCounterAvailable(co, part.Spec)
				if strings.EqualFold(part.Spec, "Any") {
					have += n
				} else if n > have {
					have = n
				}
			}
		}
		if !any || have < bound {
			bound = have
		}
		any = true
	}
	return bound, any
}

// PayManaSourceParts settles a mana ability's source-anchored non-mana cost
// parts, after the {T} tap and before any sacrifice (so a counter or exert
// lands on the still-present source): SubCounter removals, the PayEnergy<N>
// spend (chargeEnergyCost, the cast path's one energy-charging site), the
// AddCounter<N/KIND> placement (Wall of Roots' -0/-1 counter; the same
// CounterChange the activation settle emits) and the Exert<1/CARDNAME>
// exert (Oasis Ritualist; the same events.Exert the attack election emits).
// manaAbilityPayablePool gated each one, so every part here is payable.
func PayManaSourceParts(e Engine, p state.PlayerID, source state.ObjID, cost costvocab.Cost) {
	for _, part := range cost.SubCounter {
		if part.Announced || !costvocab.SubCounterTargetsSource(part.Target) {
			// An announced part (its amount is the announced X) or a part
			// anchored to another permanent is settled by
			// settleManaSubCounter off the continuation's picks, never here.
			continue
		}
		e.Emit(events.Event{Kind: events.CounterChange, Obj: source, Counter: part.Spec, Amount: -part.N})
	}
	ChargeEnergyCost(e, p, cost, 0)
	for _, part := range cost.AddCounter {
		if part.N != 0 {
			e.Emit(events.Event{Kind: events.CounterChange, Obj: source, Counter: part.Spec, Amount: part.N})
		}
	}
	for range cost.Exert {
		if o := e.Game().Obj(source); o != nil && o.Zone == state.ZBattlefield {
			e.Emit(events.Event{Kind: events.Exert, Obj: source, Player: p})
		}
	}
}
