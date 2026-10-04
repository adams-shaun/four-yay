package pay

import (
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// TapPowerSum sums the current power of a tap-cost candidate list -- the
// set-level read a withTotalPowerGE<N> group predicate (Crew, Mossbridge
// Troll) constrains. "Any number" may tap the whole list, so the list's
// total is the most power a payment can tap.
func TapPowerSum(e Engine, ids []state.ObjID, saKind string) int32 {
	sum := int32(0)
	for _, id := range ids {
		sum += e.Chars().TapPower(id, saKind)
	}
	return sum
}

// TapTopPowerSum is tapPowerSum for a literal tapXType<N/...> part with a
// group predicate: exactly N are tapped, so the most a payment can tap is
// the power of the N largest candidates. A slice sort is fine here -- the
// result is a sum, so the order equal powers sort in cannot reach an event.
func TapTopPowerSum(e Engine, ids []state.ObjID, n int, saKind string) int32 {
	if n <= 0 {
		return 0
	}
	if n >= len(ids) {
		return TapPowerSum(e, ids, saKind)
	}
	pw := make([]int32, 0, len(ids))
	for _, id := range ids {
		pw = append(pw, e.Chars().TapPower(id, saKind))
	}
	sort.Slice(pw, func(a, b int) bool { return pw[a] > pw[b] })
	sum := int32(0)
	for _, v := range pw[:n] {
		sum += v
	}
	return sum
}

// MoveToGraveCandidates returns the cards matching spec (you-relative to p,
// source-relative to source) still available in ANY alive player's exile
// zone, minus everything in reserved. Exiled cards live in their OWNER's
// exile zone (events/apply.go's zoneOwner), so the scan iterates the
// players rather than p's own zone: Shelob, Dread Weaver's exiles land in
// the OPPONENT's exile zone, and a controller-only scan would find nothing.
// Zone order is deterministic (players in seat order, each zone in list
// order), so the candidate order -- and therefore every pick built from it
// -- replays.
func MoveToGraveCandidates(e Engine, p state.PlayerID, source state.ObjID, spec string, reserved map[state.ObjID]bool) []state.ObjID {
	var out []state.ObjID
	for i := range e.Game().Players {
		if e.Game().Players[i].Lost {
			continue
		}
		for _, id := range e.Game().Zone(state.ZExile, state.PlayerID(i)) {
			if reserved[id] {
				continue
			}
			if e.MatchesSpecFrom(spec, id, p, source) {
				out = append(out, id)
			}
		}
	}
	return out
}

// NonManaCastable is castable's payment-independent tail. Cost-modifier
// offer checks use it after their flexible-pip walk has established a payable
// resolved mana face: applying Color$ before that walk would otherwise see a
// hybrid pip as neither of its colours and withhold a cast that the eventual
// announced face can legally make free. Keeping all non-mana checks in this
// one helper means that specialized offer logic cannot bypass Sac/Discard/
// counter/tap legality.
func NonManaCastable(e Engine, p state.PlayerID, id state.ObjID, cost costvocab.Cost, ability bool, tapKind string) bool {
	return NonManaCastableP(e, p, id, &cost, ability, tapKind)
}

// NonManaCastableP is nonManaCastable reading *cost in place (never written),
// so the offer gate's hot path does not copy the ~800-byte Cost.
func NonManaCastableP(e Engine, p state.PlayerID, id state.ObjID, cost *costvocab.Cost, ability bool, tapKind string) bool {
	// A RaiseCost Cost$ part no payment stage can settle (Cost.Withheld):
	// the additional cost cannot be paid, so neither can the whole cost.
	if len(cost.Withheld) > 0 {
		return false
	}
	// untapYType<N/Spec> parts have a settle only on the mana-activation path
	// (manaUntapStage). The cast / non-mana activated-ability path has none,
	// so a cost carrying one is refused rather than offered with the untap
	// silently unpaid (the head now parses to a real part, no longer
	// Cost.Unknown).
	if len(cost.UntapPermanent) > 0 {
		return false
	}
	reserved := map[state.ObjID]bool{}
	for _, part := range cost.Sac {
		var avail []state.ObjID
		matchSpec := SacrificeMatchSpec(part.Spec)
		for _, oid := range e.Game().Zone(state.ZBattlefield, p) {
			if reserved[oid] || e.CostBlocked(BlockSacrifice, oid, CostCauseForAbility(ability)) { // an earlier Sac part already claimed this one; a CantSacrifice-blocked one can never pay
				continue
			}
			if (part.Referent != 0 && oid == part.Referent) ||
				(part.Referent == 0 && e.MatchesSpecFrom(matchSpec, oid, p, id)) {
				avail = append(avail, oid)
			}
		}
		if int32(len(avail)) < part.N {
			return false
		}
		for i := int32(0); i < part.N; i++ {
			reserved[avail[i]] = true
		}
	}
	if !DiscardCostPayable(e, p, id, cost.Discard, !ability) {
		return false
	}
	// Exile cost parts (ExileFromHand/ExileFromGrave): each needs N matching
	// cards still available in the part's zone, reserved against the earlier
	// parts the same way the Sac parts above reserve against each other. For a
	// CAST (ability == false) the card being cast can never pay its own exile
	// cost: at offer time it still sits in the part's zone, so without the
	// self-skip below a Kotis with exactly Kotis+2 other cards would be
	// offered and then abort at payment (only 2 "other" cards remain once the
	// card is on the stack). An ability activation (ability == true) is
	// untouched -- encore's Cost$ ExileFromGrave<1/CARDNAME> really does exile
	// its own source. This also closes the same latent over-offer for an
	// escape cast from the graveyard.
	castObj := e.Game().Obj(id)
	// Top-of-library exile parts share one ordered prefix. Check their
	// aggregate size, not each part against the same library prefix.
	if _, ok := ExileFromTopCards(e.Game().Zone(state.ZLibrary, p), cost.ExileFromTop); !ok {
		return false
	}
	for _, part := range cost.Exile {
		zone := part.Zone
		if zone == 0 {
			zone = state.ZHand
		}
		selfInZone := !ability && castObj != nil && castObj.Zone == zone
		wholeZone := costvocab.IsWholeZoneExileSpec(part.Spec)
		var avail []state.ObjID
		for _, oid := range ExileCostCandidates(e.Game(), zone, p, part) {
			if reserved[oid] || (selfInZone && oid == id) {
				continue
			}
			// A battlefield Exile cost part (Exile<N/Spec>, Karn's Sylex,
			// Mechtitan Core) is a COST exile: a CantExile static whose
			// ForCost$ True restricts cost payments withholds the candidate
			// here, while a ForCost$ False line (The Master, Multiplied)
			// leaves it offered. exileBlockedForCost carries the pending
			// cast/activation identity so a cost-path ValidCause$ can be
			// evaluated, the same plumbing sacrificeCostCandidates uses.
			if zone == state.ZBattlefield && e.CostBlocked(BlockExile, oid, CostCauseForAbility(ability)) {
				continue
			}
			if wholeZone || (part.Referent != 0 && oid == part.Referent) ||
				(part.Referent == 0 && e.MatchesSpecFrom(part.Spec, oid, p, id)) {
				avail = append(avail, oid)
			}
		}
		if int32(len(avail)) < part.N {
			return false
		}
		if wholeZone {
			// ExileFromHand<1/All> names the WHOLE zone, not a filter (the
			// same isWholeZoneExileSpec reading the triggered window's arm
			// takes): every still-available card pays, so reserve every
			// candidate, not just part.N. No cast/activation corpus carrier
			// exists today; the wiring keeps the two paths from diverging.
			for _, oid := range avail {
				reserved[oid] = true
			}
			continue
		}
		for i := int32(0); i < part.N; i++ {
			reserved[avail[i]] = true
		}
	}
	// ExiledMoveToGrave cost parts: each needs N matching cards still in
	// ANY player's exile zone (exiled cards live in their OWNER's exile
	// zone -- events/apply.go's zoneOwner -- so a controller-only scan
	// finds nothing on the Shelob shape, where the exiled card is in the
	// OPPONENT's exile zone), reserved against the earlier parts the same
	// way the Sac/Exile parts above reserve against each other.
	for _, part := range cost.MoveToGrave {
		avail := MoveToGraveCandidates(e, p, id, part.Spec, reserved)
		if int32(len(avail)) < part.N {
			return false
		}
		for i := int32(0); i < part.N; i++ {
			reserved[avail[i]] = true
		}
	}
	// Reveal cost parts: a whole-hand Reveal (Reveal<N/Hand>) is payable with
	// ANY hand -- including an empty one (CR 701.20a) -- so it never gates the
	// offer. Every other part needs N matching hand cards. The self-exclusion
	// follows the payment stage's rule: for a CAST (ability == false) the card
	// being cast is on the stack while its cost is paid, so it cannot pay its
	// own Reveal; for an ABILITY activation (ability == true) the source stays
	// in the hand and CAN pay a self-reveal (Reveal<1/CARDNAME>, the forecast
	// cycle) -- the unconditional exclusion here used to make every such
	// ability unpayable and therefore unoffered.
	for _, part := range cost.Reveal {
		if costvocab.IsWholeHandRevealSpec(part.Spec) {
			continue
		}
		// Reveal<N/SameColor> (Illuminated Folio) is RELATIONAL: SameColor
		// matches no card as a filter, so the candidates are every eligible
		// hand card and the part is payable iff N DISTINCT ones share one
		// colour (SetPropCapacity's largest shared class; a colourless
		// card's empty token set shares nothing). The payment ask carries
		// the same decision.SetPropShared rule, so offer and intake read
		// one definition -- sameColorRevealSets in rules/setprops.go.
		if costvocab.IsSameColorRevealSpec(part.Spec) {
			_, sets := e.Chars().SameColorRevealSets(p, id, !ability)
			if decision.SetPropCapacity(decision.SetPropShared, sets) < int(part.N) {
				return false
			}
			continue
		}
		if len(CostCandidates(e, p, id, state.ZHand, part.Spec, !ability, false)) < int(part.N) {
			return false
		}
	}
	// RevealOrChoose<N/Spec> is the either-or cost: the cast is payable when
	// EITHER arm can pay -- N matching hand cards to reveal, OR N matching
	// permanents p controls to choose. The gate is existential, not the
	// reveal arm's alone: Monstrous Emergence must be castable with only a
	// controlled creature and no creature card in hand. Which arm is elected
	// is asked at payment (revealCostOrChooseAsk); here only existence is
	// tested, and the payability of the CHOOSE arm reads the battlefield, the
	// reveal arm the hand, exactly as revealCostOrChooseAsk offers them.
	for _, part := range cost.RevealOrChoose {
		hand, battlefield := RevealOrChooseCandidates(e, p, id, part)
		if len(hand) < int(part.N) && len(battlefield) < int(part.N) {
			return false
		}
	}
	// RevealChosen<Player>/<Type> parts (Stalking Leonin, Guardian Archon,
	// Emissary of Grudges, A Killer Among Us): there is no hand choice and no
	// mana to pay, so the ONLY gate is that the ability's source still carries
	// the secretly-chosen designation. A source whose choice was cleared (or
	// never recorded) cannot activate, which is the fail-closed direction --
	// the ability is not offered rather than paying for a reveal of nothing.
	for _, part := range cost.RevealChosen {
		if !HasRevealChosenDesignation(e.Game().Obj(id), part.Spec) {
			return false
		}
	}
	for _, part := range cost.Behold {
		n := len(CostCandidates(e, p, id, state.ZHand, part.Spec, true, false)) +
			len(CostCandidates(e, p, id, state.ZBattlefield, part.Spec, false, false))
		if n < int(part.N) {
			return false
		}
	}
	// Tap cost parts reserve their candidates against the Sac/Exile/Return
	// reservations above (one permanent cannot pay both) AND against each
	// other: a composed cost carrying the same tap part several times -- a
	// replicated cast re-pays its tapXType cost once per payment -- must not
	// count one untapped permanent for every part. Without the reservation
	// an affordability walk over the composed cost offered a bound far above
	// what the board could actually pay, and answering it aborted the cast
	// at the payment stage (CR 733's clean reversal, but a needless one).
	for _, part := range cost.TapPermanent {
		if part.Dyn != "" {
			// The dynamic tapXType heads (tapXType<X/Spec>, tapXType<Any/Spec>):
			// the tap election resolves the count at payment. An X-form part is
			// payable with zero candidates (X = 0 is a legal announcement); an
			// Any-form part must tap at least one matching permanent the earlier
			// parts have not already claimed, so a spec no unreserved candidate
			// satisfies leaves the cost unpayable rather than offering a
			// zero-tap payment of an effect that does not scale with the taps.
			if part.Dyn == "Any" {
				avail := 0
				var floorSum int32
				for _, oid := range TapCostCandidates(e, p, id, part) {
					if reserved[oid] || (cost.Tap && oid == id) {
						continue
					}
					avail++
					floorSum += e.Chars().TapPower(oid, tapKind)
				}
				if avail == 0 {
					return false
				}
				// The withTotalPowerGE<N> group predicate (Crew's "total power N
				// or greater", Mossbridge Troll's "total power 10 or greater"):
				// the paid set's TOTAL power must reach the floor, and "any
				// number" may tap every candidate, so the most the board can
				// pay is the sum of all their powers. A shortfall leaves the
				// cost unpayable rather than offering an election no legal
				// answer satisfies (Decision.Validate would reject every
				// answer and the game would wedge).
				if part.MinPower > 0 && floorSum < part.MinPower {
					return false
				}
			}
			continue
		}
		var avail []state.ObjID
		for _, oid := range TapCostCandidates(e, p, id, part) {
			if reserved[oid] || (cost.Tap && oid == id) {
				continue
			}
			avail = append(avail, oid)
		}
		if len(avail) < int(part.N) {
			return false
		}
		// The literal form with a group predicate (none in the corpus today,
		// modelled for symmetry with the Any form): exactly N are tapped, so
		// the most power a legal answer can tap is the N largest candidates.
		// A shortfall withholds the cost.
		if part.MinPower > 0 && TapTopPowerSum(e, avail, int(part.N), tapKind) < part.MinPower {
			return false
		}
		for i := 0; i < int(part.N); i++ {
			reserved[avail[i]] = true
		}
	}
	for range cost.Blight {
		if len(CostCandidates(e, p, id, state.ZBattlefield, "Creature.YouCtrl", false, false)) == 0 {
			return false
		}
	}
	if cost.Forage && len(e.Game().Zone(state.ZGraveyard, p)) < 3 &&
		len(CostCandidates(e, p, id, state.ZBattlefield, "Food.YouCtrl", false, false)) == 0 {
		return false
	}
	// Energy cost parts (PayEnergy<N>): the payer's energy counter total
	// covers the SUM of the fixed parts -- Forge CostPayEnergy.canPay reads
	// the same total, and a composed cost carrying the part several times (a
	// replicated cast re-pays its PayEnergy cost once per payment) draws the
	// pool down once per part, so the parts cannot each spend the whole
	// counter total independently. The dynamic X form is bounded by that
	// total at the X ask, so the offer gate needs no assumption about the
	// not-yet-chosen value. The read is the shared energyPayable helper, so
	// the cast path and the triggered-cost window cannot disagree about it.
	if !EnergyPayable(e.Game(), p, cost) {
		return false
	}
	// Return cost parts (Return<N/Spec>): the source itself (Spec CARDNAME,
	// Forge's payCostFromSource) must be in play; otherwise the payer controls
	// at least N distinct matching permanents, reserved against the Sac and
	// Exile reservations above so two parts cannot claim one permanent.
	for _, part := range cost.Return {
		spec := SacrificeMatchSpec(part.Spec)
		if strings.EqualFold(spec, "CARDNAME") {
			if o := e.Game().Obj(id); o == nil || o.Zone != state.ZBattlefield {
				return false
			}
			continue
		}
		var avail []state.ObjID
		for _, oid := range e.Game().Zone(state.ZBattlefield, p) {
			if reserved[oid] {
				continue
			}
			if e.MatchesSpecFrom(spec, oid, p, id) {
				avail = append(avail, oid)
			}
		}
		if int32(len(avail)) < part.N {
			return false
		}
		for i := int32(0); i < part.N; i++ {
			reserved[avail[i]] = true
		}
	}
	// PutToLib cost parts (PutCardToLibFrom<Zone><N/Pos/Spec>): the payer needs
	// N matching cards in the part's zone. A Battlefield CARDNAME part is the
	// source itself (Forge's payCostFromSource), which must still be on the
	// battlefield. Cards are reserved against the earlier Sac/Exile/Return
	// parts so two components cannot claim the same permanent. The library
	// POSITION never affects payability.
	for _, part := range cost.PutToLib {
		spec := SacrificeMatchSpec(part.Spec)
		if part.N == 1 && part.Zone == state.ZBattlefield && strings.EqualFold(spec, "CARDNAME") {
			// The singleton self-reference fast path only covers N=1; a larger
			// N needs the general candidate walk below (it would otherwise be
			// offered on the source alone and abort at payment time). The
			// controller check matches putToLibAsk's candidates branch: a
			// control-changed source is not a cost the payer can pay.
			if o := e.Game().Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Controller != p {
				return false
			}
			reserved[id] = true
			continue
		}
		var avail []state.ObjID
		for _, oid := range e.Game().Zone(part.Zone, p) {
			if reserved[oid] {
				continue
			}
			if e.MatchesSpecFrom(spec, oid, p, id) {
				avail = append(avail, oid)
			}
		}
		if int32(len(avail)) < part.N {
			return false
		}
		for i := int32(0); i < part.N; i++ {
			reserved[avail[i]] = true
		}
	}
	for _, part := range cost.Draw {
		// Draw cost parts (Draw<N/Spec>): the cast flow draws the PAYER; a
		// spec naming a trigger-only role (Player.TriggeredPlayer and friends)
		// has no binding here and is unpayable -- never offered -- rather than
		// silently drawing nobody. A dynamic part (Draw<X/Spec>) additionally
		// needs its SVar-resolved count to evaluate at payment; an
		// unresolvable body withholds the whole cost (fail closed, the
		// fixLifeXCost direction), never an unpayable offer with a zero draw.
		if _, ok := CastFlowDrawPlayer(part.Spec, p); !ok {
			return false
		}
		if part.Dyn != "" {
			if _, ok := DrawCostCount(e, id, p, part); !ok {
				return false
			}
		}
	}
	// GainLife<N/Player...> cost parts: the payment needs at least one alive
	// player the part's spec names relative to the payer (a player has to be
	// there to gain the life). A part whose spec matches nobody -- the only
	// payer alive, or an unevaluable spec -- leaves the cost unpayable and
	// therefore unoffered, the fail-closed direction. The same helper the
	// payment uses resolves the candidates, so gate and settlement agree.
	for _, part := range cost.GainLife {
		if len(GainLifeCostPlayers(e, p, part)) == 0 {
			return false
		}
	}
	if o := e.Game().Obj(id); o != nil {
		for _, part := range cost.SubCounter {
			// An announced SubCounter<X/Kind> part's count is the cast's X,
			// bounded by the source's counter count at the X ask; the offer
			// gate makes no assumption about the not-yet-chosen value.
			if part.Announced {
				continue
			}
			// A part whose removal-target field names something other than the
			// source needs a battlefield candidate the payer controls with
			// enough counters (Ghave's "remove a +1/+1 counter from a creature
			// you control"), reserved against the earlier parts the same way.
			// A source-anchored part keeps the pre-existing source read.
			if !costvocab.SubCounterTargetsSource(part.Target) {
				cands := SubCounterRemovalCandidates(e, p, id, part, part.N, reserved)
				if len(cands) == 0 {
					return false
				}
				// Deterministically mirror the ask stage's reservation: the
				// first candidate (candidates are in zone order) pays this
				// part when the later parts of the same cost count the same
				// pool. A board change before the ask aborts there.
				reserved[cands[0]] = true
				continue
			}
			if SubCounterAvailable(o, part.Spec) < part.N {
				return false
			}
		}
		if ActivationTapCostUnavailable(o, cost) {
			return false
		}
	} else if len(cost.SubCounter) > 0 || cost.Tap {
		return false
	}
	return true
}
