package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// payCast implements CR 601.2h (pay all costs) and, for a spell, CR 601.2i
// (the "when you cast" trigger). It runs after the target choice (601.2c);
// the object is already on the stack (pushCast). A payment that fails here
// (a pool that changed under a hand-built intent -- castable already gated
// the option the caster chose, so this is not reachable from an ordinary,
// well-formed client) aborts and REVERSES the push (CR 733.1): the object
// returns to the zone it came from, nothing remains paid and no cast trigger
// fires. The land play is also handled here (it never goes on the stack).
func (e *Engine) payCast() {
	pc := e.cast
	if pc == nil {
		return
	}
	if pc.mode == "land" {
		// A land's as-enters choice is now asked by the MoveZone replacement
		// boundary, not during this proposal. Keep LandPlayed behind that
		// boundary so it is logged only after the answered entry completes.
		e.cast, e.choosing = nil, chooseNone
		e.etbLandPlay, e.etbLandObj, e.etbLandPlayer = true, pc.card, pc.player
		card := pc.card
		e.emit(events.Event{Kind: events.MoveZone, Obj: card, From: pc.from, To: state.ZBattlefield})
		// The entry may never have happened: a ReplacementResult$ Replaced
		// entry replacement discards the move entirely and the land stays in
		// the zone it was played from. The special action was still taken, so
		// settle the land play here unless the entry is merely parked on an
		// as-enters answer (which settles on its own completion).
		e.settleLandPlayIfDone(card)
		return
	}
	// Publish the counter adder for the whole payment. A counter a COST places
	// (a blight M1M1, a planeswalker's [+N] loyalty counter, Suspend's TIME
	// counters) is put by the paying player, and an activated ability's cost is
	// paid before its wrapper exists on the stack -- so actionCause cannot
	// attribute it and the AddCounter class's ValidSource$ would fail closed.
	// A spell is already on the stack here, but the payer is its adder too, so
	// the one publish covers every cost site in both branches.
	prevAdder := e.SetCounterAdder(pc.player)
	defer e.SetCounterAdder(prevAdder)
	// The flow is now past the 601.2c target choice (either it was asked and
	// answered, or the SA has no target), so a mana-window resume through
	// continueCast must not re-ask for one.
	pc.passedTarget = true
	// alltargeted1: for a SPELL the stack object already exists (pushCast),
	// so the chain's pre-asked sub-ability target answers install here; the
	// ability arm installs after its AbilityPush mints the object.
	e.installSubPreAsk(pc)
	// CR 601.2e: the game checks that the proposed spell can legally be cast,
	// once every announcement choice (the {X} value) is known. An illegal
	// proposal is reversed (CR 733.1) -- see recheckIllegal.
	if e.recheckIllegal(pc) {
		return
	}
	// CR 601.2g: if the total cost includes a mana payment, the player gets a
	// chance to activate mana abilities before paying. manaWindowAsk poses
	// that window (returning true to suspend) only when the pool alone cannot
	// pay and an untapped mana source exists.
	if e.manaWindowAsk() {
		return
	}
	// Mana abilities can draw/reorder the library. Resolve the aggregate
	// ExileFromTop prefix only now, before any cost is paid, so the IDs moved
	// are still the actual top cards and insufficient cards abort without
	// spending the spell/activation's mana.
	if len(pc.cost.ExileFromTop) > 0 {
		top, ok := exileFromTopCards(e.G.Zone(state.ZLibrary, pc.player), pc.cost.ExileFromTop)
		if !ok {
			e.abortCast(pc, "exile cost no longer payable; cast/activation aborted", true)
			return
		}
		pc.exiles = append(pc.exiles, top...)
	}
	// Snapshot the source before any non-mana cost can move it. DamageYou
	// shares this LKI with resolution-time damage costs when its source has
	// already left the battlefield; a live source still uses current layers.
	sourceKeywordLKI := e.damageKeywordsOf(pc.card)
	sourceControllerLKI := state.PlayerID(0)
	if sourceObj := e.G.Obj(pc.card); sourceObj != nil {
		sourceControllerLKI = sourceObj.Controller
	}
	if pc.isAbility() {
		// Task 10: an activated ability. The shared stages above (X, Delve --
		// never present on an ability --, Sac) have already run and been
		// recorded; what differs from a spell here is the cost's remaining
		// non-mana parts. Pay mana, then each Tap (a Tap event), each
		// SubCounter part (a CounterChange of -N), and every chosen sacrifice.
		// The ability object is minted after payment below; answered targets
		// are recorded onto it after AbilityPush, including a window resume.
		// An activated ability may carry a Waterbend<N>/<X> cost (Giant
		// Koi), whose announced taps (pc.convoke) pay {1} each of the generic
		// (CR 701.67a). paymentMana folds them like a cast's Convoke
		// contributions; with no contributions it is manaToPay unchanged.
		mana := e.paymentMana(pc)
		// The descriptor carries the announced-X marker (the ability's own
		// {X} cost was folded), so a CostContainsX batch sees this activation
		// as an X payment exactly as the offer did.
		ok, _, spentMana, _, _ := e.payManaDescriptorForSpent(pc.player, paymentForCast(pc, mana), mana,
			e.paymentConv(pc.player, pc.card, true), pipRider{})
		if !ok {
			e.abortCast(pc, "activation aborted: cost no longer payable", true)
			return
		}
		// RememberCostMana$ (Jeweled Amulet: "Note the type of mana spent to
		// pay this activation cost"): the colours the payment actually spent
		// (the same per-colour delta the negative ManaAdd events above
		// record, in WUBRG order) fold onto the source object through the
		// "noted-mana" Choose marker, where the card's mana ability (Produced$
		// Special LastNotedType) reads them back. A cost with no mana part
		// notes nothing — Forge's CostRememberSpentMana records only mana
		// costs too.
		remembered := false
		if ab := e.pcAbility(pc); ab != nil {
			remembered = strings.EqualFold(strings.TrimSpace(ab.Params["RememberCostMana"]), "True")
		}
		if remembered {
			noted := ""
			for i, letter := range manaLetters {
				if spentMana[i] > 0 {
					noted += letter
				}
			}
			e.emit(events.Event{Kind: events.Choose, Obj: pc.card, Counter: "noted-mana", Text: noted})
		}
		if pc.payLife != 0 {
			e.emit(events.Event{Kind: events.LifeChange, Player: pc.player, Amount: -pc.payLife})
		}
		// An activated ability's announced waterbend taps (pc.convoke) become
		// tapped as part of paying the cost, the same Tap event a cast's
		// Convoke/Harmonize/Improvise contributions emit (CR 701.67a).
		waterbent := false
		for _, pay := range pc.convoke {
			e.emit(events.Event{Kind: events.Tap, Obj: pay.id})
			waterbent = waterbent || pay.waterbend
		}
		if waterbent {
			e.emit(events.Event{Kind: events.ElementalBend, Obj: pc.card, Player: pc.player, Text: "water"})
		}
		for _, id := range pc.delve {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZGraveyard, To: state.ZExile, Text: "delved"})
		}
		e.payDiscardCost(pc.discards, e.cyclingKeyword(pc))
		// Exile cost parts (ExileFromHand/ExileFromGrave): each chosen card
		// leaves its zone (hand, or the graveyard for a self-reference) for
		// exile. Read the zone live: the settled card is still where exAsk
		// found it, but a From read from the object keeps a graveyard
		// self-exile honest about where it moved from.
		for _, id := range pc.exiles {
			if o := e.G.Obj(id); o != nil {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZExile, Text: "exiled as a cost"})
			}
		}
		// CollectEvidence parts (alltargeted1): the evidence chosen at the
		// evidenceAsk stage leaves the payer's graveyard for exile, the same
		// action the Ward evidence payment performs.
		for _, id := range pc.evidence {
			if o := e.G.Obj(id); o != nil {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZGraveyard, To: state.ZExile, Text: "collected as evidence"})
			}
		}
		// ExiledMoveToGrave cost parts: each chosen card leaves exile for
		// its OWNER's graveyard (events.Move's zoneOwner already routes a
		// non-battlefield move to the owner), and the ExiledWith provenance
		// is cleared by the same move.
		for _, id := range pc.moveGraves {
			if o := e.G.Obj(id); o != nil {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZGraveyard, Text: "moved to its owner's graveyard as a cost"})
			}
		}
		// Energy cost parts (PayEnergy<N>/<X>): the announced amount leaves
		// the payer's energy pool as one PlayerCounterChange (a player
		// counter, not an object's -- CR 118.2d). The X form spends exactly
		// the announced value (xAsk bounded it by this same total). The
		// shared chargeEnergyCost helper is the ONE energy-charging site.
		e.chargeEnergyCost(pc.player, pc.cost, pc.x)
		// Announced PayLife<X> parts (Toxic Deluge's "pay X life"): each pays
		// the announced X as one LifeChange beside the fixed life payMana
		// charged above (payLife). xAsk bounded the announcement by the payer's
		// life, so the payment cannot drive the total below zero here.
		for range pc.cost.LifeX {
			if pc.x > 0 {
				e.emit(events.Event{Kind: events.LifeChange, Player: pc.player, Amount: -pc.x})
			}
		}
		// DamageYou<N> cost parts: the payer takes N damage from the source
		// (Forge CostDamage; the same event shape payUnlessDamageCost emits).
		for _, part := range pc.cost.DamageYou {
			e.payDamageCost(pc.player, part.N, pc.card, sourceKeywordLKI, sourceControllerLKI)
		}
		// GainLife<N/Player...> cost parts (see payGainLifeCost): every player
		// the part names relative to the payer gains N life.
		e.payGainLifeCost(pc.player, pc.cost.GainLife)
		// Mill cost parts (Mill<N>): the payer mills the summed requirement
		// from the top of their library as part of the payment.
		e.payMillCostParts(pc)
		// Draw cost parts (Draw<N/Spec>): the payer draws N, as one ordinary
		// Draw event per card (an empty library's loss is the SBA's). The
		// dredge replacement is NOT posed here -- the cast-flow payment stage
		// cannot re-enter mid-payment -- and no corpus card reaches a Draw
		// cost payment with a dredger in the graveyard.
		e.payDrawCostParts(pc)
		// Return cost parts: each chosen object moves to its OWNER's hand
		// (Forge CostReturn.doPayment's moveToHand) beside the other payments.
		for _, id := range pc.returns {
			if o := e.G.Obj(id); o != nil {
				e.emit(events.ReturnCost(id, o.Zone))
			}
		}
		e.emitChoiceCosts(pc)
		if pc.cost.Untap {
			e.emit(events.Event{Kind: events.Untap, Obj: pc.card, Player: pc.player, Text: "untapped as a cost"})
		}
		if pc.cost.Tap {
			// The {T} cost's payer taps the permanent (Forge CostTap). This
			// MUST come before settlePutToLibCost: a self-placement cost that
			// also carries {T} (Timestream Navigator's
			// "{2}{U}{U}, {T}, Put Timestream Navigator on the bottom of its
			// owner's library") moves the source off the battlefield, and
			// tapping a library card is not a state that exists (CR 110.5) --
			// a library tap would also survive a direct library→battlefield
			// re-entry, whose Move entry arm does not clear Tapped. Emitted
			// in this order the tap lands on the still-battlefield permanent
			// and the move's leave-battlefield arm resets it.
			e.emitTap(pc.card, pc.player, false)
		}
		// Settled after the {T} tap for the same reason (see above).
		e.settlePutToLibCost(pc)
		e.settleSubCounterParts(pc)
		// CR 606.3: a [+N] loyalty cost adds N loyalty counters to the walker
		// as part of the activation's payment, settled beside the SubCounter
		// removals and before the AbilityPush (the ability object the effect
		// resolves through). AddCounter is a free cost component -- no mana,
		// no gate -- so this is the only thing the activation does with it.
		// A 0-count part (the [0] abilities) emits nothing: a CounterChange of
		// 0 would be a no-op folded into state but a spurious log entry.
		for _, part := range pc.cost.AddCounter {
			if part.N != 0 {
				e.emit(events.Event{Kind: events.CounterChange, Obj: pc.card, Counter: part.Spec, Amount: part.N})
			}
		}
		// Exert<1/CARDNAME> (CR 701.39a): one Exert event on the source, the
		// same fold the declare-attackers election emits, so the untap skip
		// and "whenever you exert" triggers read one path.
		for range pc.cost.Exert {
			if o := e.G.Obj(pc.card); o != nil && o.Zone == state.ZBattlefield {
				e.emit(events.Event{Kind: events.Exert, Obj: pc.card, Player: pc.player})
			}
		}
		// Capture the sacrifice LKI (Task sac1) BEFORE the MoveZone events
		// drain the permanents: each chosen object is still on the battlefield
		// here, so SacrificedInfoOf reads its live face and +1/+1 counters (the
		// layer-7d portion of its P/T, which Move will reset). The resulting
		// stack object carries these to resolution, where the ability's
		// Sacrificed$<Property> SVar heads answer "the sacrificed creature's
		// power/toughness/mana value" (CR 608.2g) against them.
		var sacrificedLKI []state.SacrificedInfo
		for _, id := range pc.sacs {
			sacrificedLKI = append(sacrificedLKI, state.SacrificedInfoOf(e.G, id))
		}
		for _, id := range pc.sacs {
			e.emit(events.Sacrifice(id))
		}
		// RollDice cost parts are free, engine-driven payment actions. Publish
		// each die through the same canonical Note as DB$ RollDice so trigger
		// matching and replay observe the exact seeded result. The final result
		// is the ability's CR 107.3i X and is stamped onto its stack object below.
		for _, part := range pc.cost.RollDice {
			sides, err := strconv.ParseInt(part.Spec, 10, 32)
			if err != nil || sides <= 0 {
				continue // ParseCost admits only positive, bounded sides.
			}
			for i := int32(0); i < part.N; i++ {
				result := int32(e.Rand(int(sides)) + 1)
				e.emit(effects.DieRollNote(pc.card, pc.player, int32(sides), result, result))
				if part.Dyn == "X" {
					pc.x = result
				}
			}
		}
		// AbilityPush mints the ability object onto the stack AFTER the cost
		// settles, so an aborted activation leaves no stack object behind
		// (CR 733.1). handleTarget records the chosen targets onto it. A
		// GRANTED activation (task grantcost1) mints through the SAME three
		// events beginGrantedActivation/beginKeywordGrantedActivation mint --
		// the delayed-shape DelayedPush for a self-grant (Counter carries the
		// SVar name; the ^uint32(0) registration id matches nothing) and
		// GrantAbilityPush for a cross-object grant (IDs[0] carries the
		// grantor; the minted ability's Source is the recipient) -- so
		// resolution reads the SVar-anchored body exactly as it always has,
		// while every cost part above (sacrifice, discard, counter, energy,
		// draw, ...) is now paid by the shared flow too. A KEYWORD-GRANTED
		// activation (the AddKeyword$ Cycling route, CR 613.1f) mints through
		// KeywordAbilityPush, whose Counter carries the derived keyword line
		// the body is synthesized from.
		if pc.gainedFrom != 0 {
			e.emit(events.Event{Kind: events.GainedAbilityPush, Player: pc.player, Obj: pc.card,
				Amount: int32(pc.gainedIdx), IDs: []state.ObjID{pc.gainedFrom}})
		} else if pc.grantSVar != "" {
			if pc.grantSource == pc.card {
				e.emit(events.Event{Kind: events.DelayedPush, Player: pc.player, Obj: pc.card,
					Amount: -1, Counter: pc.grantSVar, Text: "granted ability"})
			} else {
				e.emit(events.Event{Kind: events.GrantAbilityPush, Player: pc.player, Obj: pc.card,
					Counter: pc.grantSVar, IDs: []state.ObjID{pc.grantSource}})
			}
		} else {
			e.emit(pc.activationPushEvent(e))
		}
		if len(e.G.Stack) > 0 {
			pc.stackObj = e.G.Stack[len(e.G.Stack)-1]
		}
		// alltargeted1: the chain's pre-asked sub-ability target answers are
		// now bound to the minted ability object, so the resolution consumes
		// them instead of re-posing the asks mid-resolution.
		e.installSubPreAsk(pc)
		// CR 107.3i: record the chosen {X} on the ability stack object, the
		// same way the spell arm records it on the spell below. The shared
		// xAsk stage asked and paid it (pc.cost.WithX(pc.x)), but AbilityPush's
		// Amount is the ability index, not the X value, so without this the
		// paid X never reaches resolution and every Cost$-X parameter
		// (CounterNum$ X, NumDmg$ X, NumCards$ X, SVar:X:Count$xPaid) reads 0.
		// Obj is pc.stackObj (the minted ability object), never pc.card (the
		// source permanent), so the permanent's own X (e.g. a Walking
		// Ballista's ETB value) is not clobbered. Emitted AFTER the push so
		// the object exists for events.Apply to write it on. Zero means no
		// X was paid: no event, matching the spell arm's guard.
		if pc.x != 0 {
			e.emit(events.Event{Kind: events.CastInfo, Obj: pc.stackObj, Amount: pc.x})
		}
		if e.sacrificedLKI == nil {
			e.sacrificedLKI = make(map[state.ObjID][]state.SacrificedInfo)
		}
		e.sacrificedLKI[pc.stackObj] = sacrificedLKI
		e.installPaidCostLists(pc)
		for _, id := range pc.sacs {
			if id != pc.card {
				continue
			}
			if e.sourceLifelinkLKI == nil {
				e.sourceLifelinkLKI = make(map[state.ObjID]bool)
			}
			if e.sourceControllerLKI == nil {
				e.sourceControllerLKI = make(map[state.ObjID]state.PlayerID)
			}
			e.sourceLifelinkLKI[pc.stackObj] = sourceKeywordLKI.lifelink
			e.sourceControllerLKI[pc.stackObj] = sourceControllerLKI
			// The own-source fields above carry only lifelink and controller.
			// CR 113.7a's other damage-relevant characteristics -- infect
			// (CR 702.90b) and deathtouch (CR 702.2b) -- live in the named
			// map, which Engine.emit's departure walk cannot seed here
			// either, because AbilityPush is minted only after the cost is
			// paid. Seed it with the same pre-cost snapshot, keyed on this
			// ability and its own source, so a bearer sacrificed to pay for
			// its own ability still deals damage in the granted form.
			e.captureNamedDamageSourceLKI(pc.stackObj, pc.card, sourceKeywordLKI, sourceControllerLKI)
			break
		}
		// A target answer can suspend payment in the 601.2g mana window.
		// Record it only once the ability object actually exists, on either
		// the immediate pay path or a resumed payCast; finishTargetedCast
		// cannot do so if the window has not minted the stack object yet.
		if pc.stackObj != 0 && pc.rootOpts != nil {
			e.recordChosenTargets(pc.stackObj, pc.rootOpts, false)
		}
		if pc.rootOpts == nil {
			// No target-recording continuation: dispatch at the completed
			// AbilityPush boundary while the spent-source capture is still live.
			e.fireManaSpentTriggers(pc.activationPushEvent(e), nil)
		}
		e.cast, e.choosing = nil, chooseNone
		return
	}
	mana := e.castPaymentMana(pc)
	paid, spentMana, spentSnow, spentTyped := e.payManaCastSpent(pc, mana)
	if !paid {
		// E2 (round 2) / F05-2. This is the reachable no-progress arm: a Delve
		// exile ask (Min:0, Max the shortfall) was answered with fewer cards
		// than the shortfall needs, so the cast aborts with no state change
		// and priority re-offers it. Declining is a legal, conforming answer --
		// CR 601.2h rewinds the cast -- but the engine must not re-offer the
		// SAME unpayable cast forever. CR 733.2 lets a reversed illegal action
		// be redone legally, so the FIRST no-progress abort leaves THIS card's
		// option offered; only the SECOND identical abort in the same window
		// holds it out of the remaining priority window (the suppression clears
		// on the first state-changing event, so the option returns as soon as
		// the window ends or the mana/board changes).
		e.abortCast(pc, "cast aborted: cost no longer payable", true)
		return
	}
	if f := e.G.Obj(pc.card).Face(); faceWantsConverge(f) || e.triggeredConvergeReaderOut() || e.sunburstGrantOut() {
		pc.convergeOn = true
		pc.converge = convergeColours(spentMana)
	}
	if f := e.G.Obj(pc.card).Face(); faceWantsCastSpend(f) || e.triggeredCastSpendReaderOut() {
		pc.manaSpentOn = true
		pc.manaSpent = manaSpentTotal(spentMana)
		pc.manaSpentSnow = manaSpentTotal(spentSnow)
		pc.manaSpentTreasure = manaSpentTotal(spentTyped[state.TypedTreasure]) + manaSpentTotal(spentTyped[state.TypedArtifactTreasure])
		pc.manaSpentCave = manaSpentTotal(spentTyped[state.TypedCave]) + manaSpentTotal(spentTyped[state.TypedArtifactCave])
		pc.manaSpentDesert = manaSpentTotal(spentTyped[state.TypedDesert]) + manaSpentTotal(spentTyped[state.TypedArtifactDesert])
		pc.manaSpentArtifact = manaSpentTotal(spentTyped[state.TypedArtifact]) + manaSpentTotal(spentTyped[state.TypedArtifactTreasure]) + manaSpentTotal(spentTyped[state.TypedArtifactCave]) + manaSpentTotal(spentTyped[state.TypedArtifactDesert])
	}
	// AddsCounters$ (task opalp): capture the rider grants from the SAME
	// payment capture emitRestrictedManaSpend built. This must run before
	// fireManaSpentTriggers consumes and clears e.manaSpentSources (nothing
	// can suspend between the payment and here -- the capture site only
	// emits). The capture is already filtered to rider-bearing batches and
	// keeps one record per spent unit, so a cast that spent ordinary (or only
	// restricted) mana emits no rider event and stays byte-identical.
	pc.addsCounterGrants = append([]state.ManaAddsCounterGrant(nil), e.manaSpentAddsCounters...)
	if pc.payLife != 0 {
		e.emit(events.Event{Kind: events.LifeChange, Player: pc.player, Amount: -pc.payLife})
	}
	waterbent := false
	for _, pay := range pc.convoke {
		e.emit(events.Event{Kind: events.Tap, Obj: pay.id})
		waterbent = waterbent || pay.waterbend
	}
	if waterbent {
		e.emit(events.Event{Kind: events.ElementalBend, Obj: pc.card, Player: pc.player, Text: "water"})
	}
	for _, id := range pc.delve {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZGraveyard, To: state.ZExile, Text: "delved"})
	}
	e.payDiscardCost(pc.discards, "")
	// Exile cost parts (see the ability branch above for the why).
	for _, id := range pc.exiles {
		if o := e.G.Obj(id); o != nil {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZExile, Text: "exiled as a cost"})
		}
	}
	// CollectEvidence parts (alltargeted1; see the ability branch above).
	for _, id := range pc.evidence {
		if o := e.G.Obj(id); o != nil {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZGraveyard, To: state.ZExile, Text: "collected as evidence"})
		}
	}
	// ExiledMoveToGrave cost parts (see the ability branch above for the why).
	for _, id := range pc.moveGraves {
		if o := e.G.Obj(id); o != nil {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZGraveyard, Text: "moved to its owner's graveyard as a cost"})
		}
	}
	// Mill cost parts (see the ability branch above for the why).
	e.payMillCostParts(pc)
	// Energy cost parts (see the ability branch above for the why).
	e.chargeEnergyCost(pc.player, pc.cost, pc.x)
	// Announced PayLife<X>, DamageYou<N> and Draw<N/Spec> cost parts (see the
	// ability branch above for the why).
	for range pc.cost.LifeX {
		if pc.x > 0 {
			e.emit(events.Event{Kind: events.LifeChange, Player: pc.player, Amount: -pc.x})
		}
	}
	for _, part := range pc.cost.DamageYou {
		e.payDamageCost(pc.player, part.N, pc.card, sourceKeywordLKI, sourceControllerLKI)
	}
	e.payGainLifeCost(pc.player, pc.cost.GainLife)
	e.payDrawCostParts(pc)
	// Return cost parts (see the ability branch above for the why).
	for _, id := range pc.returns {
		if o := e.G.Obj(id); o != nil {
			e.emit(events.ReturnCost(id, o.Zone))
		}
	}
	e.settlePutToLibCost(pc)
	e.settleSubCounterParts(pc)
	e.emitChoiceCosts(pc)
	// Capture the sacrifice LKI before the MoveZones (see the ability branch's
	// comment): the sacrificed permanents are still on the battlefield here.
	var sacrificedLKI []state.SacrificedInfo
	for _, id := range pc.sacs {
		sacrificedLKI = append(sacrificedLKI, state.SacrificedInfoOf(e.G, id))
	}
	// Casualty:X (Ob Nixilis, the Adversary): the amount is the sacrificed
	// creature's power, read live here -- the sacrifice settles with the
	// cost parts below, and the copy trigger queued later in this same
	// payment carries the resolved value (CR 702.249a).
	if pc.casualtyVariable && pc.casualtySac != 0 {
		if o := e.G.Obj(pc.casualtySac); o != nil && o.Zone == state.ZBattlefield {
			pc.casualtyX = e.Power(pc.casualtySac)
		}
	}
	for _, id := range pc.sacs {
		e.emit(events.Sacrifice(id))
	}
	if pc.mode == "suspend" {
		info, _ := suspendCost(e.G.Obj(pc.card).Face())
		time := info.time
		if info.timeX {
			time = pc.x
		}
		// CastInfo is the replayable provenance marker: only this action sets
		// FlagSuspend, so an arbitrary exiled Suspend card is never treated as
		// having been suspended. Its Amount retains X while the card is exiled:
		// counter-removal triggers on X-time Suspend cards read xPaid there.
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: pc.x, Counter: events.FlagsString(state.FlagSuspend)})
		e.emit(events.Event{Kind: events.MoveZone, Obj: pc.card, From: pc.from, To: state.ZExile, Text: "suspended"})
		if time > 0 {
			e.emit(events.Event{Kind: events.CounterChange, Obj: pc.card, Counter: "TIME", Amount: time})
		}
		e.cast, e.choosing = nil, chooseNone
		return
	}
	if pc.mode == "plot" {
		// CR 701.34a/b: the plot ACTION is not a cast. It pays the K:Plot
		// colon parameter, exiles the card face up, and gives it the plotted
		// designation -- NO counters (that is Suspend's mechanic): the free
		// cast's only timing restriction is CR 701.34b's "on a later turn",
		// so the designation is recorded as an events.AlterAttribute grant,
		// folded into Object.PlottedTurn with the CURRENT turn (the Enlist
		// turn-stamp shape). An arbitrary exiled Plot carrier is never
		// offered the cast: it carries no PlottedTurn, and only this action
		// (and the corpus's DB$ AlterAttribute | Attributes$ Plotted family,
		// once the effect side models it) grants the designation.
		e.emit(events.Event{Kind: events.MoveZone, Obj: pc.card, From: pc.from, To: state.ZExile, Text: "plotted"})
		e.emit(events.Event{Kind: events.AlterAttribute, Obj: pc.card, Text: "Plotted", Amount: 1})
		e.cast, e.choosing = nil, chooseNone
		return
	}
	if pc.mode == "foretell" {
		// CR 702.126a: the Foretell ACTION is not a cast. CastInfo is the
		// replayable provenance marker -- only this action sets FlagForetold
		// on a hand->exile move, so an arbitrary exiled card is never treated
		// as foretold -- and the MoveZone carries the face-down exile
		// encoding (events.Apply's decode sets FaceDown and clears ExiledWith:
		// no exiling source permanent exists for Foretell, so Amount 0). Do
		// NOT route through effects' applyExileFaceDown: it is unexported and
		// binds an ability source that does not exist here -- the raw event
		// encoding is emitted directly, the suspend branch's own pattern. The
		// view redacts the face-down exile to everyone but the exiler (the
		// owner, for Foretell), and any move NOT to exile clears FaceDown, so
		// the later foretell-cost cast reveals automatically.
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Counter: events.FlagsString(state.FlagForetold)})
		e.emit(events.Event{Kind: events.MoveZone, Obj: pc.card, From: pc.from, To: state.ZExile,
			Counter: "exiled_with_face_down", Amount: 0})
		e.cast, e.choosing = nil, chooseNone
		return
	}
	if e.sacrificedLKI == nil {
		e.sacrificedLKI = make(map[state.ObjID][]state.SacrificedInfo)
	}
	e.sacrificedLKI[pc.stackObj] = sacrificedLKI
	e.installPaidCostLists(pc)
	// A Fuse cast publishes its per-stage target split for resolution
	// (review MAJOR 1): resolveFused reads it instead of re-deriving the
	// split from the flat target list. Engine-only scratch like
	// sacrificedLKI — rebuilt by replay because payCast re-executes, and
	// removed with the stack object by the shared MoveZone cleanup.
	if pc.mode == "fuse" && len(pc.stageTargets) > 0 {
		if e.fuseTargets == nil {
			e.fuseTargets = make(map[state.ObjID][][]state.Target)
		}
		e.fuseTargets[pc.stackObj] = pc.stageTargets
	}
	if len(pc.charmTargets) > 0 {
		if e.charmTargets == nil {
			e.charmTargets = make(map[state.ObjID][][]state.Target)
		}
		e.charmTargets[pc.stackObj] = pc.charmTargets
	}
	// AddsNoCounter$ mana (Cavern of Souls): if the payment just consumed a
	// batch carrying the can't-be-countered provenance FOR THIS CAST, fold
	// state.FlagNoCounter into the same pay-time CastInfo so a replay marks
	// the spell exactly like every other cast flag. The capture is read once
	// here and cleared — nothing can suspend between emitRestrictedManaSpend's
	// set and this read (it emits, never asks).
	noCounter := e.noCounterSpend == pc.stackObj
	e.noCounterSpend = 0
	// CR 601.2b: record how the spell was cast (the X value and mode flags).
	// Deferred to payment rather than the up-front push so an aborted
	// proposal leaves no cast-time trace on the card. A cast trigger that
	// reads the mode (e.g. "cast a kicked spell") sees it, because the flag
	// is applied before the trigger fires next.
	flags := modeFlags(pc.mode)
	// CR 702.168: the Gift promise rides the pay-time CastInfo too -- the
	// CastFlags word is assigned wholesale here, so the bit events.GiftPromise
	// folded at pushCast (which the CR 601.2c target ask read) must be
	// re-stated or this later event would clear it. A declined promise emits
	// no flag.
	if pc.giftPromise {
		flags = events.FlagsString(events.FlagsFrom(flags) | state.FlagPromisedGift)
	}
	if pc.mode == "mutated" && pc.mutateTop {
		flags = events.FlagsString(events.FlagsFrom(flags) | state.FlagMutatedTop)
	}
	if noCounter {
		flags = events.FlagsString(events.FlagsFrom(flags) | state.FlagNoCounter)
	}
	// DB$ Play's ReplaceGraveyard$ Exile rider (task replplay1): the played
	// spell's provenance — "if that spell would be put into your graveyard
	// this turn, exile it instead" — rides the same pay-time CastInfo every
	// other mode flag uses. A free Play cast today satisfies neither the X
	// gate nor a non-empty modeFlags above, so setting the bit is what makes
	// the `flags != ""` emission arm below fire at all — exactly the event
	// the resolution reader needs; a Play whose SA carries no rider keeps
	// the byte-identical no-event shape.
	if pc.replaceGraveyard {
		flags = events.FlagsString(events.FlagsFrom(flags) | state.FlagReplaceGraveyard)
	}
	// kw:MayFlashSac (CR 702.8): a card cast off-sorcery through the keyword's
	// own flash permission carries the flag the keyword's ETB hook reads to
	// register the cleanup-step sacrifice. A sorcery-timed cast of the same
	// card (offSorcery false) or a cast of any other card emits nothing, so
	// unrelated casts stay byte-identical. The modeFlags switch has no case
	// for this keyword because the cast is ORDINARY -- there is no cast mode
	// to read and no extra cost; the permission alone sets no flag.
	if !pc.isAbility() && pc.offSorcery {
		if o := e.G.Obj(pc.card); mayFlashSacFace(o.Face()) {
			flags = events.FlagsString(events.FlagsFrom(flags) | state.FlagMayFlashSac)
		}
	}
	// kw:Rebound (CR 702.95a): a spell cast from its controller's HAND whose
	// face carries the keyword is exiled as it resolves and offers the free
	// recast at the next upkeep. The flag is stamped only for a hand-origin
	// cast, so the re-bound cast from exile (CR 702.95e: "doesn't rebound
	// again") carries none and resolves ordinarily. The bit is a
	// CastProvenanceFlag, so a stack copy -- put on the stack, never cast
	// (CR 707.10) -- is stripped of it at the mint and resolves without the
	// promise. The modeFlags switch has no case for this keyword because
	// the cast is ORDINARY -- the keyword grants no alternative cost and no
	// mode; only the origin zone sets the flag.
	if !pc.isAbility() && pc.from == state.ZHand {
		if o := e.G.Obj(pc.card); o != nil && o.Face() != nil {
			if _, ok := o.Face().KeywordParam("Rebound"); ok {
				flags = events.FlagsString(events.FlagsFrom(flags) | state.FlagRebound)
			}
		}
	}
	// Replicate (CR 702.55a): the payment count rides the same pay-time
	// CastInfo. modeFlags deliberately maps "replicated" to "" -- a DECLINED
	// replicate (count 0) must stay the byte-identical plain cast, no flag
	// and no event -- so the flag is ORed here only when a payment was made.
	// The count and a paid {X} never share one Amount: measured at the corpus
	// pin, no K:Replicate carrier's mana value carries {X}, so the
	// single-event shape below is the live path; the defensive two-event
	// split keeps the two provenances distinct should one ever pair.
	// A MayPlayText$-typed permission (rules/mayplay.go): stamp the
	// permission token on the pay-time CastInfo so mayPlayTypedLimitReached
	// can attribute the play to the static that granted it. The token rides
	// the Counter as an extra comma-separated field, which FlagsFrom ignores
	// (the CastFlags word is unaffected). It is appended ONLY to the main
	// CastInfo emission below, so a face whose trailing provenance events
	// (converge, mana spend) also carry `flags` cannot count the permission
	// twice; an untyped cast (empty mayPlayPerm) is byte-identical.
	permSuffix := ""
	if pc.mayPlayPerm != "" {
		permSuffix = ",perm=" + pc.mayPlayPerm
	}
	repCount := int32(0)
	if pc.mode == "replicated" {
		repCount = pc.replicateTimes
	}
	if repCount > 0 {
		flags = events.FlagsString(events.FlagsFrom(flags) | state.FlagReplicated)
	}
	if repCount > 0 && pc.x != 0 {
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: pc.x,
			Counter: events.FlagsString(events.FlagsFrom(flags)&^state.FlagReplicated) + permSuffix})
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: repCount, Counter: flags})
	} else if pc.x != 0 || flags != "" {
		amt := pc.x
		if repCount > 0 {
			amt = repCount
		}
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: amt, Counter: flags + permSuffix})
	}
	// Squad (CR 702.66): the payment count rides its own TRAILING pay-time
	// CastInfo -- the flag routes the Amount into Object.SquadPaid (events.Apply's
	// CastInfo case), so this event never clobbers the X or replicate count an
	// earlier event in this block carried (no corpus carrier pairs {X} with
	// Squad, measured over the 15 K:Squad files), and its Counter leaves
	// CastFlags carrying every earlier flag too. modeFlags deliberately maps
	// "squadded" to "" -- a DECLINED squad (count 0) must stay the
	// byte-identical plain cast, no flag and no event -- so the emission is
	// gated on a payment having actually been made. Only a "squadded" cast
	// can carry a nonzero count, and no other mode reads pc.squadTimes, so
	// the gate is exact.
	if pc.mode == "squadded" && pc.squadTimes > 0 {
		sqFlags := events.FlagsString(events.FlagsFrom(flags) | state.FlagSquadPaid)
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: pc.squadTimes, Counter: sqFlags})
	}
	// Converge (CR 107.4f-family, task converge1): the distinct-colour spend
	// count rides its own TRAILING pay-time CastInfo -- the flag routes the
	// Amount into Object.ConvergeColours (events.Apply's CastInfo case), so
	// this event never clobbers the X or replicate count an earlier event in
	// this block set, and its Counter (flags + FlagConverged) leaves
	// CastFlags carrying every earlier flag too. Emitted whenever the face
	// carries a Count$Converge SVar -- or when a battlefield permanent's
	// trigger names TriggeredCard$Converge and so reads THIS cast's colours --
	// count 0 included (a colourless-only converge cast is a real zero, not an
	// absent one); the two-arm gate is heads-safety, so no game that casts no
	// converge card with no reader out changes an event.
	if pc.convergeOn {
		flags = events.FlagsString(events.FlagsFrom(flags) | state.FlagConverged)
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: pc.converge, Counter: flags})
	}
	// Multikicker (CR 702.43, task multikicker1): the times-kicked count
	// rides its own TRAILING pay-time CastInfo -- the flag routes the Amount
	// into Object.TimesKicked (events.Apply's CastInfo case), so this event
	// never clobbers the X a main CastInfo carried (Comet Storm pairs {X}
	// with Multikicker; the two-event split falls out of the trailing shape
	// itself), and its Counter leaves CastFlags carrying every earlier flag
	// too. A multikicked cast IS a kicked cast, so the flag rides with the
	// bare FlagKicked (the bare predicate and the Condition$ Kicked gate
	// keep matching). The emission gate keeps unrelated casts
	// byte-identical: ALWAYS on a multikicked-mode cast with count > 0 (a
	// declined kick -- count 0, the modeFlags("replicated") contract --
	// emits nothing), and on a plain-Kicker cast mode ONLY when the face
	// carries a Count$TimesKicked SVar (faceWantsTimesKicked): the 11 legacy
	// plain-Kicker carriers' scripts still read the count, and no other
	// kicked cast gains an event.
	mkCount := int32(0)
	switch pc.mode {
	case "multikicked":
		mkCount = pc.multikickTimes
	case "kicked", "kicked1", "kicked2":
		mkCount = 1
	case "kickedboth":
		mkCount = 2
	}
	if mkCount > 0 && (pc.mode == "multikicked" || faceWantsTimesKicked(e.G.Obj(pc.card).Face())) {
		mkFlags := events.FlagsString(events.FlagsFrom(flags) | state.FlagKicked | state.FlagMultikicked)
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: mkCount, Counter: mkFlags})
	}
	// Conspire (CR 702.78a): the tap provenance rides its own trailing
	// pay-time CastInfo -- FlagConspired routes the bool into
	// Object.Conspired (events.Apply folds it outside the Amount switch, so
	// no later event's routing is disturbed), and Count$Conspired reads it
	// off the source. modeFlags deliberately maps "conspired" to "" -- a
	// declined/plain cast (conspirePaid false) must stay the byte-identical
	// plain cast, no flag and no event -- so the emission is gated on the
	// tap having actually been paid.
	if pc.conspirePaid {
		cFlags := events.FlagsString(events.FlagsFrom(flags) | state.FlagConspired)
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: 1, Counter: cFlags})
	}
	// Convoke (CR 702.66, task connive1): the creatures the caster tapped to
	// help pay for the cast ride their own TRAILING pay-time CastInfo's IDs
	// -- the flag (NOT ORed into the accumulating flags, the Conspired
	// pattern) routes the IDs into Object.Convoked (events.Apply folds it
	// outside the Amount switch), and Defined$ Convoked reads it. Emitted
	// only for a face whose SVar table or abilities reference the selector
	// (faceWantsConvoked), so every unrelated convoke cast stays
	// byte-identical; no accumulation means no later CastInfo carries it,
	// so its arm's position in the newest-first switch is order-independent.
	if len(pc.convoke) > 0 && faceWantsConvoked(e.G.Obj(pc.card).Face()) {
		ids := make([]state.ObjID, 0, len(pc.convoke))
		seen := make(map[state.ObjID]bool, len(pc.convoke))
		for _, pay := range pc.convoke {
			if !seen[pay.id] {
				seen[pay.id] = true
				ids = append(ids, pay.id)
			}
		}
		cvFlags := events.FlagsString(events.FlagsFrom(flags) | state.FlagConvoked)
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: int32(len(ids)), Counter: cvFlags, IDs: ids})
	}
	// AddsCounters$ (task opalp): the producing abilities' rider grants this
	// cast earned ride their own TRAILING pay-time CastInfo's Text payload --
	// the flag (NOT ORed into the accumulating flags, the Conspired/Convoked
	// pattern) routes the payload into Object.ManaAddsCounterGrants
	// (events.Apply folds it outside the Amount switch), and rules'
	// entry-counter plan uses each snapshot verbatim when the spell enters as
	// a permanent. Emitted only when the payment consumed rider-bearing mana,
	// so every unrelated cast stays byte-identical; no accumulation means no
	// later CastInfo carries the flag, so its arm's position in the
	// newest-first switch is order-independent.
	if len(pc.addsCounterGrants) > 0 {
		acFlags := events.FlagsString(events.FlagsFrom(flags) | state.FlagAddsCounters)
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: int32(len(pc.addsCounterGrants)), Counter: acFlags, Text: events.ManaAddsCounterGrantsText(pc.addsCounterGrants)})
	}
	// Cast-spend (task castprov1): the TOTAL mana actually spent to cast the
	// spell rides its own TRAILING pay-time CastInfo -- the flag routes the
	// Amount into Object.ManaSpent (events.Apply's CastInfo case), so this
	// event never clobbers the X an earlier event in this block carried, and
	// its Counter leaves CastFlags carrying every earlier flag too. A
	// convoke-only cast (tapped creatures, no mana) is a real zero, not an
	// absent one -- the same "count 0 included" contract the converge
	// emission keeps. The emission gate keeps unrelated casts byte-identical:
	// only a face whose SVar table reads the count (faceWantsCastSpend)
	// stamps the event.
	if pc.manaSpentOn {
		flags = events.FlagsString(events.FlagsFrom(flags) | state.FlagManaSpent)
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: pc.manaSpent, Counter: flags})
		// The SNOW-unit part of that same spend (task castfilter1) rides its
		// own trailing CastInfo: the flag routes the Amount into
		// Object.ManaSnowSpent, so it never clobbers the unfiltered total the
		// event just set (the converge/multikick two-event split, applied one
		// step further). Emitted unconditionally alongside the total -- a cast
		// that spent no snow mana is a real zero, not an absent one -- so the
		// filtered Count$CastTotalManaSpent Snow read is exact for the six
		// Snow carriers without a second gate.
		snowFlags := events.FlagsString(events.FlagsFrom(flags) | state.FlagManaSnowSpent)
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: pc.manaSpentSnow, Counter: snowFlags})
		// The TYPED parts of that same spend (task castfilter2) ride their own
		// trailing CastInfos, one per tag, each accumulating the earlier
		// flags -- the same split, applied twice further. Emitted
		// unconditionally alongside the total -- a cast that spent no mana of
		// a tag is a real zero, not an absent one -- so the filtered
		// Count$CastTotalManaSpent Treasure/Cave/Desert read is exact for
		// their carriers without a second gate. The emission order is total,
		// then Snow, then Treasure, then Cave, then Desert, then Artifact;
		// since every later event carries all earlier flags,
		// events.Apply's CastInfo switch
		// checks the NEWEST flag first (Artifact, Desert, Cave, Treasure,
		// Snow, then the total) or every later event would route into the
		// first tag's field.
		typedAmounts := [4]int32{pc.manaSpentTreasure, pc.manaSpentCave, pc.manaSpentDesert, pc.manaSpentArtifact}
		typedFlags := [4]uint64{state.FlagManaTreasureSpent, state.FlagManaCaveSpent, state.FlagManaDesertSpent, state.FlagManaArtifactSpent}
		acc := events.FlagsFrom(flags)
		for t := range typedFlags {
			acc |= typedFlags[t]
			e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: typedAmounts[t],
				Counter: events.FlagsString(acc)})
		}
	}
	// ManaExpend (trig:ManaExpend): fold this cast's pool spend into the
	// per-turn engine tally UNCONDITIONALLY -- including casts made before a
	// carrier entered the battlefield, which emit no FlagManaExpendCast event
	// under the gate below. The tally is what manaExpendMatches reads for the
	// crossing test (a cast that moves it from below Amount$ N to at-or-above
	// it fires once; a cast that starts at-or-above fires nothing); a
	// gated-only tally would undercount the pre-entry base and misfire BOTH
	// ways (spurious fire after a carrier entered mid-turn, missed crossing
	// when the real total crossed with the carrier out).
	//
	// The wake-up CastInfo emission stays gated (heads safety: only a cast
	// made while the caster's battlefield already holds a ManaExpend carrier
	// can fire one, so no game without a carrier changes an event). Count pool
	// mana actually spent plus each Convoke contribution: CR 702.50 says each
	// creature tapped for Convoke pays for one mana, and CR 601.2g-h includes
	// those contributions in paying the spell's total cost. Delve, Harmonize,
	// Improvise, free casts and ability activations do not spend mana. The
	// tally update runs BEFORE the emit, so the matcher reads the post-payment
	// total.
	if spend := manaSpentTotal(spentMana) + convokeManaSpent(pc.convoke); spend > 0 {
		e.manaExpendAdd(pc.player, spend)
		if e.manaExpendReaderOut(pc.player) {
			meFlags := events.FlagsString(events.FlagsFrom(flags) | state.FlagManaExpendCast)
			e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Player: pc.player, Amount: spend, Counter: meFlags})
		}
	}
	// Compleated's life-paid amount is deliberately the FINAL CastInfo: all
	// earlier payment captures may carry accumulated flags, so this event
	// must not be followed by one that routes its Amount elsewhere.
	if pc.payLife > 0 && !pc.isAbility() {
		if o := e.G.Obj(pc.card); o != nil && o.Face() != nil {
			for _, keyword := range o.Face().Keywords {
				if strings.EqualFold(strings.TrimSpace(keyword), "Compleated") {
					cf := events.FlagsString(events.FlagsFrom(flags) | state.FlagCompleated)
					e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: pc.payLife, Counter: cf})
					break
				}
			}
		}
	}
	// CR 601.2i: the "when you cast" trigger, held back from the up-front
	// push, fires now -- only after the spell is paid for. Capture the deferred
	// PutOnStack event (and its LKI) BEFORE the call: fireDeferredCastTrigger
	// nils them. The same event feeds the mana-spent riders below, which queue
	// AFTER the deferred cast triggers (deterministic append).
	var castEv events.Event
	var castLKI *state.Object
	if e.deferredPush != nil {
		castEv = *e.deferredPush
		castLKI = e.deferredPushLKI
	}
	e.fireDeferredCastTrigger()
	// Casualty is a cast trigger only when its additional sacrifice was paid.
	// Queue a respondable ability, rather than copying at payment; the event
	// payload rebuilds its body during replay (including permanent copies).
	if pc.casualtyPaid && castEv.Kind == events.PutOnStack {
		pt := pendingTrigger{
			Source: pc.card, Controller: pc.player, Casualty: true,
			Ctx: effects.Ctx{Source: pc.card, Controller: pc.player,
				Remembered: []state.Target{{Obj: pc.card}}},
		}
		// Casualty:X's script riders (Ob Nixilis, the Adversary's
		// NonLegendary$ True | SetLoyalty$ Casualty:...): the copy's
		// characteristics ride the trigger event's payload, so the replay
		// rebuilds the identical copy body. The loyalty value is the
		// casualty amount resolved at payment: the sacrificed creature's
		// power for X, the printed threshold for a numeric carrier that
		// named the rider (no corpus carrier does).
		if spec, ok := e.casualtySpec(pc.card); ok && (spec.nonLegendary || spec.setLoyalty) {
			var cc events.StackCopyCounter
			cc.NonLegendary = spec.nonLegendary
			if spec.setLoyalty {
				cc.Loyalty, cc.HasLoyalty = spec.threshold, true
				if pc.casualtyVariable {
					cc.Loyalty = pc.casualtyX
				}
			}
			pt.CasPayload = events.StackCopyCounterString(cc)
		}
		e.pendingTriggers = append(e.pendingTriggers, pt)
	}
	if castEv.Kind == events.PutOnStack {
		e.fireManaSpentTriggers(castEv, castLKI)
	}
	// Cascade (CR 702.85, task cascade1): one cast trigger per Cascade
	// instance, queued AFTER the ordinary cast triggers (deterministic
	// append; the drain's APNAP ordering places them). The queue emits
	// nothing and asks nothing, so no game without a cascade carrier
	// changes an event.
	e.queueCascadeTriggers(pc.stackObj, pc.player)
	// The Effect grants' cast-driven lifetime (ForgetOnCast$, task
	// param:api:Effect.ForgetOnCast) ends the grants at this completed-cast
	// moment, LAST in the pay stage: the qualifying cast often has its
	// behaviour FROM the grant (Dark Apostle's granted cascade fires this
	// very cast's cascade trigger, and the cascade resolution itself
	// re-derives the spell's granted keywords), so every reader above must
	// see pre-sweep state.
	e.effectCastSweep(castEv)
	e.cast, e.choosing = nil, chooseNone
}

// abortCast reverses a cast or activation proposal that cannot complete, per
// CR 733.1. If the object was already pushed (CR 601.2a / 602.2a), the
// reversal undoes the push: a spell returns to the zone it came from, and an
// ability's stack object leaves the stack (it ceases to exist, moved to exile
// as the existing ability-fizzle resting place does). Nothing is paid and
// the held cast trigger is dropped. e.cast and e.choosing are cleared.
func (e *Engine) abortCast(pc *pendingCast, text string, suppress bool) {
	// The reversal below and the push that preceded it are state-changing
	// events to emit's suppression-clearing rule, but their NET effect is no
	// progress -- the object returns to the zone it came from -- so the
	// held-out no-progress state must survive. For a pushed spell that state
	// is pc.preSuppress / pc.preAborts (captured before the push, which
	// cleared it); for an ability (never pushed) it is the current maps. Both
	// the held-out set and the per-card no-progress count are restored, so a
	// no-progress decline of THIS object can be counted again across the
	// push (F05-2).
	var saved map[state.ObjID]bool
	if pc.pushed && pc.preSuppress != nil {
		saved = make(map[state.ObjID]bool, len(pc.preSuppress))
		for id := range pc.preSuppress {
			saved[id] = true
		}
	} else if e.suppressedCast != nil {
		saved = make(map[state.ObjID]bool, len(e.suppressedCast))
		for id := range e.suppressedCast {
			saved[id] = true
		}
	}
	var savedAborts map[state.ObjID]int32
	if pc.pushed && pc.preAborts != nil {
		savedAborts = make(map[state.ObjID]int32, len(pc.preAborts))
		for id, n := range pc.preAborts {
			savedAborts[id] = n
		}
	} else if e.castAborts != nil {
		savedAborts = make(map[state.ObjID]int32, len(e.castAborts))
		for id, n := range e.castAborts {
			savedAborts[id] = n
		}
	}
	if suppress {
		if savedAborts == nil {
			savedAborts = map[state.ObjID]int32{}
		}
		savedAborts[pc.card]++
		// F05-2 (CR 733.2): the FIRST no-progress abort of a card leaves its
		// option offered, so a merely-reversed illegal action may be redone
		// legally; the SECOND identical abort holds the option out. Undo the
		// restore-only path for a count below two by not adding to `saved`.
		if savedAborts[pc.card] >= 2 {
			if saved == nil {
				saved = map[state.ObjID]bool{}
			}
			saved[pc.card] = true
		}
	}
	if pc.pushed && pc.stackObj != 0 {
		if pc.isAbility() {
			e.emit(events.Event{Kind: events.MoveZone, Obj: pc.stackObj, From: state.ZStack, To: state.ZExile, Text: "reversed"})
		} else {
			e.emit(events.Event{Kind: events.MoveZone, Obj: pc.stackObj, From: state.ZStack, To: pc.from, Text: "reversed"})
		}
	}
	if pc.faceBefore != nil {
		if o := e.G.Obj(pc.card); o != nil && o.FaceIdx != *pc.faceBefore {
			e.emit(events.Event{Kind: events.FlipFace, Obj: pc.card, Amount: int32(*pc.faceBefore)})
		}
	}
	// CR 733.1 applies identically to a mode announced during the proposal.
	// ModeChosen is a marker event, so the cache is restored beside the reverse
	// marker just as handleModes maintains it beside the forward marker.
	if pc.modeChosen {
		if o := e.G.Obj(pc.card); o != nil {
			if sa := o.Face().SpellAbility(); sa != nil {
				e.emit(events.Event{Kind: events.ModeChosen, Obj: pc.card, Player: pc.player,
					Text: strings.Join(modeLabels(sa, o.Face().SVars, pc.preModes), ",")})
			}
			o.ChosenModes = state.CloneChosenModes(pc.preModes)
		}
	}
	e.dropProposalTriggers(pc)
	e.deferredPush = nil
	e.deferredPushLKI = nil
	e.cast, e.choosing = nil, chooseNone
	e.emit(events.Event{Kind: events.Note, Player: pc.player, Text: text})
	e.suppressedCast = saved
	e.castAborts = savedAborts
}

// fireDeferredCastTrigger re-walks the up-front PutOnStack event that
// pushCast held back (deferredPush) so the CR 601.2i "when you cast" triggers
// fire, which is only after the spell is paid for. It is called from payCast
// for a spell; a no-op when nothing was deferred (an ability, a land, or an
// aborted proposal).
func (e *Engine) fireDeferredCastTrigger() {
	if e.deferredPush == nil {
		return
	}
	ev := e.deferredPush
	e.deferredPush = nil
	lki := e.deferredPushLKI
	e.deferredPushLKI = nil
	e.sweepEffectDelayedCast(*ev)
	push := *ev
	e.checkTriggers(&push, lki, 0, 0, false)
}

// fireManaSpentTriggers queues the TriggersWhenSpent$ rider of every mana
// source whose provenance batch paid for the just-completed spell cast or
// activated ability. Sources are captured by emitRestrictedManaSpend; ev is
// the completed PutOnStack/AbilityPush event. It runs after payment and push,
// preserving deterministic trigger append order.
//
// A rider's SVar is a T:-shaped trigger body (Mode$ SpellCast | ValidCard$ ...
// | Execute$ ...) that the ordinary trigger scan never walks -- it lives in
// the face's SVar table, not its printed T: lines. So each is parsed by
// cards.ParseTriggerLine and queued by hand, shaped exactly like the exert
// rider (rules/trigger_match.go checkExertTriggers): Source = the mana
// permanent, Idx -1, Granted=true with Execute = the body's Execute$ name and
// SA = the resolved Execute body, so the live queue and a replayed log carry
// the identical granted-trigger push (events.Apply resolves Execute from the
// source's SVar table). A source that has left the battlefield, has no face,
// or names no longer-resolvable body fails closed -- the rider belongs to the
// permanent. SpellCast is spell-only; SpellAbilityCast dispatches on both
// spell casts and activated abilities.
// manaSpentAddsCounterSources (task opalp) is gone: the rider grant is
// captured per batch by emitRestrictedManaSpend (e.manaSpentAddsCounters) so
// the producing ability's snapshot and the spent unit count survive, instead
// of a deduplicated source list that re-read the source face at entry.

func (e *Engine) fireManaSpentTriggers(ev events.Event, lki *state.Object) {
	sources := e.manaSpentSources
	e.manaSpentSources = nil
	if len(sources) == 0 || (ev.Kind != events.PutOnStack && ev.Kind != events.AbilityPush && ev.Kind != events.KeywordAbilityPush) {
		return
	}
	for _, src := range sources {
		o := e.G.Obj(src)
		if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil {
			continue
		}
		f := o.Face()
		for _, ma := range f.ManaAbilities() {
			rider := strings.TrimSpace(ma.Params["TriggersWhenSpent"])
			if rider == "" {
				continue
			}
			body := svarBodyForObject(o, rider)
			if body == "" {
				continue
			}
			t, ok := cards.ParseTriggerLine(body)
			if !ok || (t.Mode != "SpellCast" && t.Mode != "SpellAbilityCast") {
				continue
			}
			matches := false
			switch t.Mode {
			case "SpellCast":
				matches = e.spellCastEval(t, src, ev)
			case "SpellAbilityCast":
				matches = e.spellAbilityCastMatches(t, src, ev, lki)
			}
			if !e.zoneGate(t, src, ev) || !e.phaseGate(t) || !matches {
				continue
			}
			exec := strings.TrimSpace(t.Params["Execute"])
			sa := grantedTriggerExecute(o, exec)
			if sa == nil {
				continue
			}
			key := triggerKey{Source: src, Idx: -1}
			if e.triggerFireCount == nil {
				e.triggerFireCount = map[triggerKey]int32{}
			}
			if e.triggerFireCount[key] >= maxTriggerFires {
				continue // cascade bound: see maxTriggerFires.
			}
			e.triggerFireCount[key]++
			e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
				Source:     src,
				Controller: o.Controller,
				Idx:        -1,
				SA:         sa,
				Granted:    true,
				Execute:    exec,
				Ctx: effects.Ctx{
					Source:         src,
					Controller:     o.Controller,
					TriggerContext: e.triggerReferents(t, src, ev, lki),
				},
			})
		}
	}
}

// svarBodyForObject resolves a raw SVar body by name against the object's own
// face first, then every other face of its card (the resolveSVarAcrossFaces
// walk events.Apply's granted-trigger push uses, so the queue and the replay
// agree on which body a name links). Empty when no face declares it.
func svarBodyForObject(o *state.Object, name string) string {
	if o == nil || name == "" {
		return ""
	}
	if f := o.Face(); f != nil {
		if body, ok := f.SVars[name]; ok {
			return body
		}
	}
	if o.Card != nil {
		for _, cf := range o.Card.Faces {
			if body, ok := cf.SVars[name]; ok {
				return body
			}
		}
	}
	return ""
}

// recordCmdCast increments the CmdCasts[k] bookkeeping parallel to
// Commanders[k] for a commander cast from the command zone. It is called by
// commitCast only when a command-zone cast's PutOnStack was just appended, so
// a cast from any other zone (hand, graveyard-flashback, ...) is never
// counted here.
//
// The count it maintains is DERIVED state, and the brief's "derived from
// events on replay" is the right of its two options for exactly this reason:
// CmdCasts[k] is a deterministic pure function of the already-logged
// PutOnStack events (From == ZCommand, per commander id). The generic,
// format-agnostic events.Apply handler is the wrong home for it -- this is a
// Commander-format rule, not a universally-applicable state transition -- so
// it is maintained as a projection at the exact point its authoritative event
// is appended, which introduces no new degree of freedom: a faithful replay,
// which re-runs this same beginCast -> commitCast path against the recorded
// Intents, appends the identical PutOnStack events and so lands on the
// identical count. Clone deep-copies the slice (state goes through
// Game.Clone) so the O(1) tax read (commanderTaxFor) survives a clone; the
// number itself comes from the event stream alone. No event of its own is
// needed, and events/ is outside this task's boundary.
func (e *Engine) recordCmdCast(p state.PlayerID, id state.ObjID) {
	if e.format != FormatCommander {
		return
	}
	for k, cid := range e.G.Players[p].Commanders {
		if cid == id {
			e.G.Players[p].CmdCasts[k]++
			return
		}
	}
}

// castSuppressed reports whether id's cast option is currently held out of
// p's priority offers (see suppressedCast, engine.go). The id names the one
// seat holding it, so p is not consulted beyond matching that id's zone in
// the walk that called it.
func (e *Engine) castSuppressed(p state.PlayerID, id state.ObjID) bool {
	return e.suppressedCast != nil && e.suppressedCast[id]
}
