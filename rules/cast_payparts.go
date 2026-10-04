package rules

import (
	"fmt"
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// exileFromTopCards returns the aggregate top-of-library prefix paid by a set
// of ExileFromTop parts. Parts do not each get to reuse the same prefix.
func exileFromTopCards(lib []state.ObjID, parts []CostPart) ([]state.ObjID, bool) {
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

// nonManaCastable is castable's payment-independent tail. Cost-modifier
// offer checks use it after their flexible-pip walk has established a payable
// resolved mana face: applying Color$ before that walk would otherwise see a
// hybrid pip as neither of its colours and withhold a cast that the eventual
// announced face can legally make free. Keeping all non-mana checks in this
// one helper means that specialized offer logic cannot bypass Sac/Discard/
// counter/tap legality.
func (e *Engine) nonManaCastable(p state.PlayerID, id state.ObjID, cost Cost, ability bool, tapKind string) bool {
	return e.nonManaCastableP(p, id, &cost, ability, tapKind)
}

// nonManaCastableP is nonManaCastable reading *cost in place (never written),
// so the offer gate's hot path does not copy the ~800-byte Cost.
func (e *Engine) nonManaCastableP(p state.PlayerID, id state.ObjID, cost *Cost, ability bool, tapKind string) bool {
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
		matchSpec := sacrificeMatchSpec(part.Spec)
		for _, oid := range e.G.Zone(state.ZBattlefield, p) {
			if reserved[oid] || e.sacrificeBlockedForCost(oid, costCauseForAbility(ability)) { // an earlier Sac part already claimed this one; a CantSacrifice-blocked one can never pay
				continue
			}
			if (part.Referent != 0 && oid == part.Referent) ||
				(part.Referent == 0 && e.matchesSpecFrom(matchSpec, oid, p, id)) {
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
	if !e.discardCostPayable(p, id, cost.Discard, !ability) {
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
	castObj := e.G.Obj(id)
	// Top-of-library exile parts share one ordered prefix. Check their
	// aggregate size, not each part against the same library prefix.
	if _, ok := exileFromTopCards(e.G.Zone(state.ZLibrary, p), cost.ExileFromTop); !ok {
		return false
	}
	for _, part := range cost.Exile {
		zone := part.Zone
		if zone == 0 {
			zone = state.ZHand
		}
		selfInZone := !ability && castObj != nil && castObj.Zone == zone
		wholeZone := isWholeZoneExileSpec(part.Spec)
		var avail []state.ObjID
		for _, oid := range pay.ExileCostCandidates(e.G, zone, p, part) {
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
			if zone == state.ZBattlefield && e.exileBlockedForCost(oid, costCauseForAbility(ability)) {
				continue
			}
			if wholeZone || (part.Referent != 0 && oid == part.Referent) ||
				(part.Referent == 0 && e.matchesSpecFrom(part.Spec, oid, p, id)) {
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
		avail := e.moveToGraveCandidates(p, id, part.Spec, reserved)
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
		if isWholeHandRevealSpec(part.Spec) {
			continue
		}
		// Reveal<N/SameColor> (Illuminated Folio) is RELATIONAL: SameColor
		// matches no card as a filter, so the candidates are every eligible
		// hand card and the part is payable iff N DISTINCT ones share one
		// colour (SetPropCapacity's largest shared class; a colourless
		// card's empty token set shares nothing). The payment ask carries
		// the same decision.SetPropShared rule, so offer and intake read
		// one definition -- sameColorRevealSets in rules/setprops.go.
		if isSameColorRevealSpec(part.Spec) {
			_, sets := e.sameColorRevealSets(p, id, !ability)
			if decision.SetPropCapacity(decision.SetPropShared, sets) < int(part.N) {
				return false
			}
			continue
		}
		if len(e.costCandidates(p, id, state.ZHand, part.Spec, !ability, false)) < int(part.N) {
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
		hand, battlefield := e.revealOrChooseCandidates(p, id, part)
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
		if !hasRevealChosenDesignation(e.G.Obj(id), part.Spec) {
			return false
		}
	}
	for _, part := range cost.Behold {
		n := len(e.costCandidates(p, id, state.ZHand, part.Spec, true, false)) +
			len(e.costCandidates(p, id, state.ZBattlefield, part.Spec, false, false))
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
				for _, oid := range e.tapCostCandidates(p, id, part) {
					if reserved[oid] || (cost.Tap && oid == id) {
						continue
					}
					avail++
					floorSum += e.tapPowerValue(oid, tapKind)
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
		for _, oid := range e.tapCostCandidates(p, id, part) {
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
		if part.MinPower > 0 && e.tapTopPowerSum(avail, int(part.N), tapKind) < part.MinPower {
			return false
		}
		for i := 0; i < int(part.N); i++ {
			reserved[avail[i]] = true
		}
	}
	for range cost.Blight {
		if len(e.costCandidates(p, id, state.ZBattlefield, "Creature.YouCtrl", false, false)) == 0 {
			return false
		}
	}
	if cost.Forage && len(e.G.Zone(state.ZGraveyard, p)) < 3 &&
		len(e.costCandidates(p, id, state.ZBattlefield, "Food.YouCtrl", false, false)) == 0 {
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
	if !pay.EnergyPayable(e.G, p, cost) {
		return false
	}
	// Return cost parts (Return<N/Spec>): the source itself (Spec CARDNAME,
	// Forge's payCostFromSource) must be in play; otherwise the payer controls
	// at least N distinct matching permanents, reserved against the Sac and
	// Exile reservations above so two parts cannot claim one permanent.
	for _, part := range cost.Return {
		spec := sacrificeMatchSpec(part.Spec)
		if strings.EqualFold(spec, "CARDNAME") {
			if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
				return false
			}
			continue
		}
		var avail []state.ObjID
		for _, oid := range e.G.Zone(state.ZBattlefield, p) {
			if reserved[oid] {
				continue
			}
			if e.matchesSpecFrom(spec, oid, p, id) {
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
		spec := sacrificeMatchSpec(part.Spec)
		if part.N == 1 && part.Zone == state.ZBattlefield && strings.EqualFold(spec, "CARDNAME") {
			// The singleton self-reference fast path only covers N=1; a larger
			// N needs the general candidate walk below (it would otherwise be
			// offered on the source alone and abort at payment time). The
			// controller check matches putToLibAsk's candidates branch: a
			// control-changed source is not a cost the payer can pay.
			if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Controller != p {
				return false
			}
			reserved[id] = true
			continue
		}
		var avail []state.ObjID
		for _, oid := range e.G.Zone(part.Zone, p) {
			if reserved[oid] {
				continue
			}
			if e.matchesSpecFrom(spec, oid, p, id) {
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
		if _, ok := castFlowDrawPlayer(part.Spec, p); !ok {
			return false
		}
		if part.Dyn != "" {
			if _, ok := e.drawCostCount(id, p, part); !ok {
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
		if len(e.gainLifeCostPlayers(p, part)) == 0 {
			return false
		}
	}
	if o := e.G.Obj(id); o != nil {
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
			if !subCounterTargetsSource(part.Target) {
				cands := e.subCounterRemovalCandidates(p, id, part, part.N, reserved)
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
			if subCounterAvailable(o, part.Spec) < part.N {
				return false
			}
		}
		if activationTapCostUnavailable(o, cost) {
			return false
		}
	} else if len(cost.SubCounter) > 0 || cost.Tap {
		return false
	}
	return true
}

// castFlowDrawPlayer resolves a Draw cost part's spec inside the
// cast/activation flow: the payer draws (spec "", You, Player, Self), and
// Player.Activator too, because within an activation the activator IS the
// payer. A trigger-only role has no binding here; nonManaCastable blocks
// such a part so it is never offered.
func castFlowDrawPlayer(spec string, payer state.PlayerID) (state.PlayerID, bool) {
	switch castFlowDrawPlayerCodes.Code(string(spec)) {
	case castFlowDrawPlayerPayer:
		return payer, true
	}
	return 0, false
}

// drawCostCard emits the ordinary Draw event one card of a cost payment
// draws: the library's top card moves to the payer's hand, and a draw from
// an empty library is the loss the SBA checks (the same shape DrawFor's
// no-replacement draw and resumeOrdinaryDraw emit).
func (e *Engine) drawCostCard(p state.PlayerID) {
	lib := e.G.Zone(state.ZLibrary, p)
	if len(lib) == 0 {
		e.playerLoses(p, loseReasonMilled, "drew from an empty library")
		return
	}
	e.emit(events.Event{Kind: events.Draw, Player: p, Obj: lib[0],
		From: state.ZLibrary, To: state.ZHand, Secret: true})
}

// payMillCost settles every Mill<N> cost component: the payer mills the SUM
// of the parts' requirements from the top of their own library, one real
// MoveZone event per card in deterministic top-first order. The moves share a
// mill batch so MilledAll triggers once for this cost payment. No choice is
// involved, so nothing is asked. A mill instruction moves all remaining cards
// when its count exceeds the library size, so the snapshot clamps to the
// available prefix.
func (e *Engine) payMillCost(p state.PlayerID, parts []CostPart) {
	total, ok := millCostTotal(parts)
	if !ok || total <= 0 {
		return
	}
	lib := e.G.Zone(state.ZLibrary, p)
	if int64(len(lib)) < total {
		total = int64(len(lib))
	}
	// Snapshot the ids before emitting: each MoveZone mutates the library
	// the slice was read from.
	ids := append([]state.ObjID(nil), lib[:total]...)
	e.BeginMillBatch()
	for _, id := range ids {
		e.emit(events.Mill(id, p))
	}
	e.EndMillBatch()
}

func (e *Engine) payMillCostParts(pc *pendingCast) {
	e.payMillCost(pc.player, pc.cost.Mill)
}

// payDiscardCost settles every hand->graveyard discard component of a cost
// payment as ONE discard action: the payer discards the settled cards
// together, so Mode$ DiscardedAll fires once for the payment with its
// TriggerCount$Amount (and Remembered/Captured set) equal to the number of
// matching cards (CR 701.8), exactly as payMillCost batches a multi-card Mill
// cost. Every cost discard goes through this helper -- a cast, an activated
// ability, a mana-ability activation and a triggered mandatory cost all reach
// it -- so the four sites cannot diverge and a new one cannot forget the
// bracket. The bracket is opened and closed entirely inside this call and the
// emission loop cannot suspend, so successive cost actions never coalesce and
// an enclosing api:Discard batch (effects/cardflow.go's effDiscard) simply
// nests by depth. cycling names the cycling ability when the discard is paid
// for one (CR 702.29), tagging each card's event for Mode$ Cycled; an empty
// keyword emits the plain cost form. An absent object is skipped, exactly as
// the triggered-cost site's own guard did.
func (e *Engine) payDiscardCost(ids []state.ObjID, cycling string) {
	if len(ids) == 0 {
		return
	}
	e.BeginDiscardBatch()
	for _, id := range ids {
		if e.G.Obj(id) == nil {
			continue
		}
		if cycling != "" {
			e.emit(events.DiscardCostCycling(id, cycling))
		} else {
			e.emit(events.DiscardCost(id))
		}
	}
	e.EndDiscardBatch()
}

// payDrawCostParts settles every Draw cost component of a cast or activation
// payment: one ordinary draw per card of the part's count, for the drawer the
// part's spec names. The count is the literal N, or -- for the dynamic
// Draw<X/Spec> form -- the source's SVar bound by part.Dyn, resolved here at
// payment time (Champion of Wits' "draw cards equal to its power"). The
// offer gate (nonManaCastable) already proved each part's drawer and dynamic
// count resolvable, so a part that is somehow unresolvable at payment -- a
// stale stored cost -- pays nothing rather than guessing a count; the whole
// cost is never offered, so this is a belt-and-braces no-op, not a live path.
func (e *Engine) payDrawCostParts(pc *pendingCast) {
	for _, part := range pc.cost.Draw {
		drawer, ok := castFlowDrawPlayer(part.Spec, pc.player)
		if !ok {
			continue
		}
		n, ok := e.drawCostCount(pc.card, pc.player, part)
		if !ok {
			continue
		}
		for k := int32(0); k < n; k++ {
			e.drawCostCard(drawer)
		}
	}
}

// settlePutToLibCost settles every PutToLib cost component of a cast or
// activation payment (PutCardToLibFrom<Zone><N/Pos/Spec>): the chosen cards
// move to their OWNER's library. MoveZone appends to the destination zone, so
// a plain move lands at the bottom (Forge's Pos -1); a top placement (Pos 0)
// follows the move with one LibraryOrder per owner putting the moved cards
// back on top in the order they were chosen -- exactly the shape effects'
// libraryOrderPlacement emits (the same private flag), re-derived here rather
// than imported because effects must never be reached for a cost settle.
func (e *Engine) settlePutToLibCost(pc *pendingCast) {
	idx := 0
	for _, part := range pc.cost.PutToLib {
		n := int(part.N)
		end := idx + n
		if end > len(pc.putToLibs) {
			end = len(pc.putToLibs)
		}
		picks := pc.putToLibs[idx:end]
		idx = end
		for _, id := range picks {
			if o := e.G.Obj(id); o != nil {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZLibrary,
					Text: "put on the library as a cost"})
			}
		}
		if part.LibraryPos == 0 && len(picks) > 0 {
			pay.PutLibPicksOnTop(asPayer(e), picks)
		}
	}
}

// gainLifeCostPlayers lists the seats a GainLife cost part's Spec names
// relative to the payer, in APNAP order starting at the payer (the same walk
// every multi-player effect uses). Spec is Forge's raw player word
// (Player.Opponent / Player.Other); both mean "a player other than the
// payer", and the engine evaluates them through the ONE shared player-spec
// evaluator (effects.MatchesPlayerSpec) so this can never disagree with the
// ValidPlayer$ rider arm that reads the same call. The payer is never its
// own gain-life target ("an opponent"/"each other player"), so a part that
// somehow matched the payer is skipped -- harmless for the corpus spellings,
// and it keeps the payer's own life out of a cost payment.
func (e *Engine) gainLifeCostPlayers(payer state.PlayerID, part CostPart) []state.PlayerID {
	var out []state.PlayerID
	for _, p := range e.G.AliveFrom(payer) {
		if p == payer {
			continue
		}
		if effects.MatchesPlayerSpec(e.G, part.Spec, p, payer) {
			out = append(out, p)
		}
	}
	return out
}

// payGainLifeCost settles the payer's GainLife<N/Player...> cost parts
// (Forge CostGainLife; Invigorate/Reverent Silence/Skyshroud Cutter): each
// part has every player its Spec names relative to the payer gain N life as
// one POSITIVE LifeChange per player. That is exactly the oracle for the
// /* "each other player" spelling and exact in the two-seat game for the
// bare Player.Opponent spelling; in a larger pod the bare spelling pays
// EVERY opponent rather than one chosen opponent (a documented deviation --
// the alternative-cost route has no mid-cast choose-an-opponent ask). The
// emit routes through applyReplacements/applyLifeReplacements, so a CR 616
// GainLife replacement and the CantGainLife static apply for free.
func (e *Engine) payGainLifeCost(payer state.PlayerID, parts []CostPart) {
	for _, part := range parts {
		if part.N <= 0 {
			continue
		}
		for _, p := range e.gainLifeCostPlayers(payer, part) {
			e.emit(events.Event{Kind: events.LifeChange, Player: p, Amount: part.N})
		}
	}
}

// payDamageCost makes the payer take n damage from the source -- the
// DamageYou<N> cost payment (Forge CostDamage). The event shape is the one
// payUnlessDamageCost emits: the Damage event names the payer, the engine's
// damage-source context names the source, and the same-source lifelink
// gains the controller the damage (CR 702.16d).
func (e *Engine) payDamageCost(payer state.PlayerID, n int32, source state.ObjID, sourceLKI damageKeywordLKI, sourceControllerLKI state.PlayerID) {
	if n <= 0 {
		return
	}
	liveSource := e.G.Obj(source)
	keywords := e.damageKeywordsOf(source)
	controller := payer
	if liveSource != nil && liveSource.Zone == state.ZBattlefield {
		controller = liveSource.Controller
	} else {
		keywords = sourceLKI
		controller = sourceControllerLKI
	}
	prev := e.SetDamageSource(source)
	dam := events.Event{Kind: events.Damage, Player: payer, Amount: n}
	if keywords.infect {
		// CR 702.90b: even a cost payment is damage dealt by its source, so
		// an infect source's DamageYou cost pays in counter/poison form.
		dam.Counter = "infect"
	}
	ev := e.emit(dam)
	e.SetDamageSource(prev)
	if ev.Kind != events.Damage || !keywords.lifelink {
		return
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: controller, Amount: n})
}

func (e *Engine) costCandidates(p state.PlayerID, source state.ObjID, zone state.Zone, spec string, excludeSource, untapped bool) []state.ObjID {
	var out []state.ObjID
	for _, id := range e.G.Zone(zone, p) {
		o := e.G.Obj(id)
		if o == nil || (zone == state.ZBattlefield && !existsOnBattlefield(o)) || (excludeSource && id == source) || (untapped && o.Tapped) {
			continue
		}
		if e.matchesSpecFrom(spec, id, p, source) {
			out = append(out, id)
		}
	}
	return out
}

// tapCostCandidates is costCandidates for one TapPermanent (tapXType) part:
// a part bound to a granted ability's grantor (bindGrantedCostReferents --
// Fishing Pole's "Tap Fishing Pole") names exactly that permanent, offered
// while the payer controls it untapped on the battlefield; every other part
// is the ordinary untapped filter scan. Offer, planning and payment all read
// this one helper, so they cannot disagree about who can pay.
func (e *Engine) tapCostCandidates(p state.PlayerID, source state.ObjID, part CostPart) []state.ObjID {
	if part.Referent == 0 {
		return e.costCandidates(p, source, state.ZBattlefield, part.Spec, false, true)
	}
	o := e.G.Obj(part.Referent)
	if o == nil || o.Zone != state.ZBattlefield || o.Controller != p || !existsOnBattlefield(o) || o.Tapped {
		return nil
	}
	return []state.ObjID{part.Referent}
}

// revealOrChooseCandidates returns the objects that can pay one
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
func (e *Engine) revealOrChooseCandidates(p state.PlayerID, source state.ObjID, part CostPart) (hand, battlefield []state.ObjID) {
	hand = e.costCandidates(p, source, state.ZHand, part.Spec, true, false)
	battlefield = e.costCandidates(p, source, state.ZBattlefield, part.Spec, false, false)
	return hand, battlefield
}

// sacrificeMatchSpec normalizes Forge's NICKNAME spelling to CARDNAME before
// the source-aware filter is applied. The filter owns CARDNAME's object-ID
// semantics; costs use this helper at both offer and payment time so the two
// stages cannot disagree about whether a self-reference is payable.
func sacrificeMatchSpec(spec string) string {
	if strings.EqualFold(spec, "NICKNAME") {
		return "CARDNAME"
	}
	return spec
}

// sacrificeCostCandidates returns, in battlefield scan order, the permanents
// that can pay one Sac cost part for a cast (ability=false) or an activation
// (ability=true) of source by p. Every Sac stage derives its candidate list
// from this one helper -- the X announcement's upper bound (xAsk), the
// sacrifice settle (sacAsk) and the offer gate's announced-X affordability
// sweep (offerCastableUsing) -- so the count an offer is priced on, the count
// the payer may announce, and the count the payment can settle cannot
// disagree about whether a self-reference or a CantSacrifice block is
// payable.
func (e *Engine) sacrificeCostCandidates(p state.PlayerID, source state.ObjID, part CostPart, ability bool) []state.ObjID {
	matchSpec := sacrificeMatchSpec(part.Spec)
	cause := costCauseForAbility(ability)
	var out []state.ObjID
	if part.Referent != 0 {
		// A granted ability's bound OriginalHost (bindGrantedCostReferents):
		// exactly the grantor, and only while the payer controls it on the
		// battlefield (CR 701.21a: only a permanent you control can be
		// sacrificed).
		r := part.Referent
		if slices.Contains(e.G.Zone(state.ZBattlefield, p), r) && existsOnBattlefield(e.G.Obj(r)) &&
			!e.sacrificeBlockedForCost(r, cause) {
			out = append(out, r)
		}
		return out
	}
	if matchSpec == "CARDNAME" {
		// A bare self-reference matches exactly the source (CR 201.5; the
		// filter's CARDNAME base rejects every object whose ID is not
		// sc.Source, and a zero source matches nothing), so the scan below
		// can admit at most the source itself, at its battlefield position.
		// Test it alone instead of matching the whole battlefield: a mass of
		// Sac<1/CARDNAME> mana tokens (Eldrazi Spawn) otherwise makes every
		// payability check O(board) and the priority walk O(board^2).
		if source != 0 && slices.Contains(e.G.Zone(state.ZBattlefield, p), source) &&
			existsOnBattlefield(e.G.Obj(source)) && !e.sacrificeBlockedForCost(source, cause) &&
			e.matchesSpecFrom(matchSpec, source, p, source) {
			out = append(out, source)
		}
		if sacrificeCardnameVerify {
			if want := e.sacrificeCostScan(p, source, matchSpec, cause); !slices.Equal(out, want) {
				panic(fmt.Sprintf("rules: CARDNAME sacrifice fast path %v, full scan %v (source %d)", out, want, source))
			}
		}
		return out
	}
	return e.sacrificeCostScan(p, source, matchSpec, cause)
}

// sacrificeCardnameVerify makes the CARDNAME fast path above also run the
// full battlefield scan and panic on any difference. Set by the rules test
// binary (derivedmemo_verify_test.go), or at link time with
// derivedMemoVerifyFlag.
var sacrificeCardnameVerify = derivedMemoVerifyFlag != ""

// sacrificeCostScan is sacrificeCostCandidates' full battlefield scan.
func (e *Engine) sacrificeCostScan(p state.PlayerID, source state.ObjID, matchSpec string, cause costCause) []state.ObjID {
	var out []state.ObjID
	for _, oid := range e.G.Zone(state.ZBattlefield, p) {
		if !existsOnBattlefield(e.G.Obj(oid)) || e.sacrificeBlockedForCost(oid, cause) {
			continue
		}
		if e.matchesSpecFrom(matchSpec, oid, p, source) {
			out = append(out, oid)
		}
	}
	return out
}

// sacrificeCostAssignable tests whether all Sac parts can be paid with
// distinct permanents for an announced X. A per-part candidate count is
// insufficient: two parts can each have X candidates but share every one.
// Match each required sacrifice to an object, rerouting earlier matches when
// a later, narrower part needs one of their objects. This is an existence
// check, not a payment choice; sacAsk still lets the player choose the
// actual sacrifices in cost-part order.
func (e *Engine) sacrificeCostAssignable(p state.PlayerID, source state.ObjID, parts []CostPart, ability bool, x int32) bool {
	candidates := make([][]state.ObjID, len(parts))
	for i, part := range parts {
		candidates[i] = e.sacrificeCostCandidates(p, source, part, ability)
		need := part.N
		if part.Announced {
			need = x
		}
		if need > int32(len(candidates[i])) {
			return false
		}
	}
	assigned := make(map[state.ObjID]int)
	var claim func(int, map[state.ObjID]bool) bool
	claim = func(i int, seen map[state.ObjID]bool) bool {
		for _, oid := range candidates[i] {
			if seen[oid] {
				continue
			}
			seen[oid] = true
			prev, used := assigned[oid]
			if !used || claim(prev, seen) {
				assigned[oid] = i
				return true
			}
		}
		return false
	}
	for i, part := range parts {
		need := part.N
		if part.Announced {
			need = x
		}
		for n := int32(0); n < need; n++ {
			if !claim(i, make(map[state.ObjID]bool)) {
				return false
			}
		}
	}
	return true
}

// lastDrawnThisTurn returns the card player p most recently DREW this turn
// (the last events.Draw naming p since the most recent TurnChange), or 0 if p
// drew nothing this turn. Derived from the event log exactly like
// CardsDrawnThisTurn, so a replay derives it identically; it is NOT a filter.
// Discard<1/LastDrawn> (Jandor's Ring) is the corpus's only carrier.
func (e *Engine) lastDrawnThisTurn(p state.PlayerID) state.ObjID {
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.Draw && ev.Player == p {
			return ev.Obj
		}
	}
	return 0
}

// discardCandidates returns the still-available cards that can pay one
// Discard cost part. Random names a selection method rather than a card
// characteristic, and a Hand spec is Forge's "discard your hand" shape
// (the corpus spells its ignored count as both 0 and 1). LastDrawn is the
// other history-keyed slot: "the last card you drew this turn", one specific
// card, unpayable when p drew nothing or that card has left the hand.
// A spell being announced is excluded because it will be on the stack when
// costs are paid; an activated ability's source may remain in hand and can
// therefore pay CARDNAME/NICKNAME costs such as channel and bloodrush.
func (e *Engine) discardCandidates(p state.PlayerID, source state.ObjID, part CostPart, casting bool, reserved map[state.ObjID]bool) []state.ObjID {
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
		last := e.lastDrawnThisTurn(p)
		if last != 0 && !reserved[last] && !(casting && last == source) {
			if o := e.G.Obj(last); o != nil && o.Zone == state.ZHand {
				return []state.ObjID{last}
			}
		}
		return nil
	}
	var out []state.ObjID
	for _, id := range e.G.Zone(state.ZHand, p) {
		if reserved[id] || (casting && id == source) {
			continue
		}
		if all || e.matchesSpecFrom(matchSpec, id, p, source) {
			out = append(out, id)
		}
	}
	return out
}

// discardCostPayable is the offer-side totality gate for Discard costs. It
// mirrors discardAsk's deterministic reservation walk without consuming RNG.
func (e *Engine) discardCostPayable(p state.PlayerID, source state.ObjID, parts []CostPart, casting bool) bool {
	reserved := map[state.ObjID]bool{}
	for _, part := range parts {
		if part.Announced {
			// An announced Discard<X/Spec> (the RaiseCost bridge's Aether
			// Tide shape): X = 0 is a legal announcement, and xAsk caps the
			// X by the matching cards, so the part never withholds an offer.
			continue
		}
		candidates := e.discardCandidates(p, source, part, casting, reserved)
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

// spellsCastThisTurn counts PutOnStack events for player p since the last
// TurnChange in the log (or since the start of the log, on turn 1).
func (e *Engine) spellsCastThisTurn(p state.PlayerID) int {
	n := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.PutOnStack && ev.Player == p {
			n++
		}
	}
	return n
}

// withSpellAbilityExtras folds a spell's own SpellAbility Cost$ ADDITIONAL
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
func withSpellAbilityExtras(f *cards.Face, cost Cost) Cost {
	sa := f.SpellAbility()
	if sa == nil {
		return cost
	}
	sc := sa.ParamStr(cards.PKCost)
	if sc == "" {
		return cost
	}
	return foldAdditionalCost(cost, ParseCost(sc))
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
func foldAdditionalCost(cost, extra Cost) Cost {
	cost.Life = addClampedGeneric(cost.Life, int64(extra.Life))
	if len(extra.Sac) > 0 {
		cost.Sac = append(append([]CostPart(nil), cost.Sac...), extra.Sac...)
	}
	if len(extra.Discard) > 0 {
		cost.Discard = append(append([]CostPart(nil), cost.Discard...), extra.Discard...)
	}
	if len(extra.SubCounter) > 0 {
		cost.SubCounter = append(append([]CostPart(nil), cost.SubCounter...), extra.SubCounter...)
	}
	if len(extra.Exile) > 0 {
		cost.Exile = append(append([]CostPart(nil), cost.Exile...), extra.Exile...)
	}
	if len(extra.ExileFromTop) > 0 {
		cost.ExileFromTop = append(append([]CostPart(nil), cost.ExileFromTop...), extra.ExileFromTop...)
	}
	if len(extra.MoveToGrave) > 0 {
		cost.MoveToGrave = append(append([]CostPart(nil), cost.MoveToGrave...), extra.MoveToGrave...)
	}
	if len(extra.Reveal) > 0 {
		cost.Reveal = append(append([]CostPart(nil), cost.Reveal...), extra.Reveal...)
	}
	if len(extra.RevealOrChoose) > 0 {
		cost.RevealOrChoose = append(append([]CostPart(nil), cost.RevealOrChoose...), extra.RevealOrChoose...)
	}
	if len(extra.RevealChosen) > 0 {
		cost.RevealChosen = append(append([]CostPart(nil), cost.RevealChosen...), extra.RevealChosen...)
	}
	if len(extra.Behold) > 0 {
		cost.Behold = append(append([]CostPart(nil), cost.Behold...), extra.Behold...)
	}
	if len(extra.TapPermanent) > 0 {
		cost.TapPermanent = append(append([]CostPart(nil), cost.TapPermanent...), extra.TapPermanent...)
	}
	if len(extra.Blight) > 0 {
		cost.Blight = append(append([]CostPart(nil), cost.Blight...), extra.Blight...)
	}
	if len(extra.Energy) > 0 {
		cost.Energy = append(append([]CostPart(nil), cost.Energy...), extra.Energy...)
	}
	if len(extra.Return) > 0 {
		cost.Return = append(append([]CostPart(nil), cost.Return...), extra.Return...)
	}
	if len(extra.Draw) > 0 {
		cost.Draw = append(append([]CostPart(nil), cost.Draw...), extra.Draw...)
	}
	if len(extra.LifeX) > 0 {
		cost.LifeX = append(append([]CostPart(nil), cost.LifeX...), extra.LifeX...)
	}
	if len(extra.DamageYou) > 0 {
		cost.DamageYou = append(append([]CostPart(nil), cost.DamageYou...), extra.DamageYou...)
	}
	if len(extra.GainLife) > 0 {
		cost.GainLife = append(append([]CostPart(nil), cost.GainLife...), extra.GainLife...)
	}
	if len(extra.Mill) > 0 {
		cost.Mill = append(append([]CostPart(nil), cost.Mill...), extra.Mill...)
	}
	if len(extra.Evidence) > 0 {
		cost.Evidence = append(append([]CostPart(nil), cost.Evidence...), extra.Evidence...)
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
