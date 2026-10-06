package rules

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// battlefieldWalk is the battlefield/hand/graveyard/exile ability section,
// offered only when the walk is not castsOnly.
func (w *legalWalk) battlefieldWalk() {
	e, p := w.e, w.p
	sorcery := w.sorcery
	actionStatics := &w.actionStatics
	out := &w.out
	castsOnly := w.castsOnly
	if !castsOnly {
		// Mana abilities may explicitly function from the battlefield, hand or
		// graveyard (Spirit Guides and Jack-o'-Lantern). availableManaAbilities
		// applies each ability's ActivationZone, Activator$ and full cost gate.
		// The battlefield is walked for EVERY seat, not just p's, because an
		// ability another player's Activator$ permits (Mana Cache's "Any player
		// may activate this ability") reaches them through p's offer; every other
		// object still fails the controller/selector gate in the choke point.
		// The walk only inspects each object's mana-ability list, so one scratch
		// buffer serves every object (taken from the Engine for the loop, so a
		// re-entrant walk allocates its own).
		// The walk's board-wide facts (legal_walk_skip.go): read once here,
		// outside every face probe, and shared with the mana walk through
		// actionStatics so it stops re-deriving them per object.
		board := w.boardFacts()
		if w.rec != nil {
			w.rec.board = board
		}
		// The object classes below are exact only once the log's touches
		// are caught up (walk_objclass.go).
		e.walkClassesCatchUp()
		// The mana section reads no pricing pool: a potential walk at the
		// recorded priority walk's state serves its options
		// (walk_block_reuse.go).
		manaStart := len(*out)
		if !w.reuseManaSection() {
			masBuf := e.manaAbBuf
			e.manaAbBuf = nil
			for _, z := range []state.Zone{state.ZBattlefield, state.ZHand, state.ZGraveyard} {
				zonePlayers := []state.PlayerID{p}
				if z == state.ZBattlefield {
					zonePlayers = make([]state.PlayerID, len(e.G.Players))
					for seat := range zonePlayers {
						zonePlayers[seat] = state.PlayerID(seat)
					}
				}
				// A mana-cold object offers nothing on this board
				// (walk_objclass.go); verify mode visits it and checks its skip.
				manaCls := board.ready && !board.addAbility && !board.hasGrants
				lTypeBlock := w.manaLTypeBlockMay(board)
				for _, zonePlayer := range zonePlayers {
					// The seat's own battlefield membership is recorded for
					// PotentialMana (walk_block_reuse.go: recordMembers).
					own := w.rec != nil && z == state.ZBattlefield && zonePlayer == p
					useCls := manaCls && !own && !(z == state.ZBattlefield && lTypeBlock)
					for zi, id := range e.G.Zone(z, zonePlayer) {
						if useCls && !e.walkClassOf(id).manaHot {
							if !walkSkipVerify {
								continue
							}
							w.verifyManaCold(board, e.G.Obj(id), id, z)
						}
						o := e.G.Obj(id)
						if z == state.ZBattlefield && !pay.ExistsOnBattlefield(o) {
							// CR 702.25b: a phased-out permanent is treated as though it
							// does not exist, so its mana abilities are not offered. The
							// choke point appendAvailableManaAbilities is gated too, which
							// covers the payment windows this offer walk does not reach.
							continue
						}
						f := o.Face()
						if f == nil {
							continue
						}
						if w.manaWalkEmpty(board, o, id, f) {
							// Provably nothing to offer (legal_walk_skip.go).
							if walkSkipVerify {
								if got := e.appendAvailableManaAbilities(masBuf[:0], actionStatics, p, id); len(got) != 0 {
									panic(fmt.Sprintf("rules: mana walk skip dropped %d abilities of obj %d", len(got), id))
								}
							}
							if own {
								w.rec.recordMembers(zi, nil)
							}
							continue
						}
						var mas []*cards.SA
						if own {
							mas = w.ownManaMembers(masBuf[:0], zi, o, id)
						} else {
							mas = e.appendAvailableManaAbilities(masBuf[:0], actionStatics, p, id)
						}
						masBuf = mas
						if len(mas) == 0 {
							continue
						}
						opt := decision.Option{Index: len(*out), Kind: "activate", Label: w.manaLabel(f), Obj: id}
						// fb-led1: a mana ability that costs more than a bare tap is the
						// play the window exists for — carry its cost so the client's
						// empty-priority-window floor stops instead of passing it away.
						if marker := e.manaActivationCostMarker(mas); marker != "" {
							opt.Cost = marker
						}
						*out = append(*out, opt)
					}
				}
			}
			clear(masBuf)
			e.manaAbBuf = masBuf[:0]
		}
		w.recordManaSection(manaStart)

		// Activated abilities (Task 10): every non-mana AB$ ability on a
		// permanent p controls, and every one on a card in p's graveyard whose
		// ActivationZone$ names the graveyard, offered as an "ability" option the
		// way a cast is (rules/activate.go resolves one). Each is gated on the
		// same rules the cast options are: its own zone matches ActivationZone$,
		// SorcerySpeed$ True needs a full sorcery window, no CantBeActivated
		// restriction scopes down to it, a {T} cost needs an untapped source that
		// is neither tapped nor (for a creature, CR 302.6) summoning-sick without
		// Haste, and -- the totality gate -- the whole cost must be castable
		// (mana payable, every Sac/Discard/SubCounter part satisfiable) before the option
		// is ever offered. Equip (Task 14's attachments) is carried by this same
		// loop -- its K:Equip expansion (cards/keywords.go) is an AB$ Attach with
		// SorcerySpeed$ True, so the gates above cover it with no carve-out, and
		// rules/activate.go resolves it exactly like any other ability. This was
		// not always true: Task 14 round 1 shipped a second, Equip-only loop and
		// deleted it again on the main merge (one offer path, one activation
		// path), so do not resurrect one.
		for _, z := range []state.Zone{state.ZBattlefield, state.ZStack, state.ZGraveyard, state.ZHand, state.ZExile} {
			zonePlayers := []state.PlayerID{p}
			if z != state.ZStack {
				// Every seat's zone, so an ability another player's Activator$
				// permits can reach that player's offer walk. The stack is the
				// exception: Zone(ZStack, p) returns the WHOLE stack regardless of
				// p, so it is walked once with the Activator$/controller
				// selector doing the filtering -- iterating seats would append the
				// same option once per seat.
				zonePlayers = make([]state.PlayerID, len(e.G.Players))
				for seat := range zonePlayers {
					zonePlayers[seat] = state.PlayerID(seat)
				}
			}
			// An ability-cold object's block is empty on a board with no
			// granted-head AddKeywords (walk_objclass.go); the stack, walked
			// once, is visited whole. Verify mode visits every object and
			// checks each cold one.
			useCls := board.kwOK && !board.kwMaybe && z != state.ZStack
			for _, zonePlayer := range zonePlayers {
				for _, id := range e.G.Zone(z, zonePlayer) {
					if useCls && !e.walkClassOf(id).abHot(z) {
						if !walkSkipVerify {
							continue
						}
						w.verifyAbilityCold(board, e.G.Obj(id), id, z, zonePlayer)
					}
					o := e.G.Obj(id)
					if z == state.ZBattlefield && !pay.ExistsOnBattlefield(o) {
						// CR 702.25b: a phased-out permanent is treated as though it
						// does not exist, so none of its printed activated abilities is
						// offered or activatable.
						continue
					}
					f := o.Face()
					if f == nil {
						continue
					}
					if z == state.ZExile && (o.FaceDown || !faceHasActivationZone(f, "Exile")) {
						// The exile walk exists only for an ability whose
						// ActivationZone$ names exile; skip every other exiled card
						// before the per-ability gates (the zone can be large).
						continue
					}
					if e.printedAbilitiesGone(o) {
						// CR 613.1f: a permanent that lost all abilities has no
						// printed activated abilities either.
						// CR 708.8: a face-down permanent's printed activated abilities
						// and mana abilities do not exist while it is face down, and
						// turn-face-up (CR 708.6) is not implemented -- nothing on a
						// face-down permanent is offered at all.
						continue
					}
					// One object's ability block: served from the recorded
					// priority walk when it read no pricing pool there
					// (walk_block_reuse.go), else walked and recorded.
					if w.reuseAbilityBlock(z, zonePlayer, id) {
						continue
					}
					blockStart, blockGates := len(*out), w.gates
					// CR 702.140d: a mutated permanent has the top card's abilities
					// PLUS all abilities of the cards beneath it. Walk the FLAT pile
					// list (top face first, then each under-card): the flat index is
					// the identity AbilityPush records and the activation-limit
					// census counts, and a top-face ability keeps exactly its old
					// index. abFace is the face that carries the ability -- an
					// under-card's label and SVar table must be its own, never the
					// pile top's.
					pileSkip, faceFacts := w.pileAbilitiesEmpty(o, id, f, z)
					pileMark := len(*out)
					for i, pn := 0, o.PileAbilityCount(); i < pn && (!pileSkip || walkSkipVerify); i++ {
						pa, okAb := o.PileAbilityAt(i)
						if !okAb {
							continue
						}
						ab := pa.SA
						abFace := o.PileFaceFor(pa.Merged)
						if abFace == nil {
							continue
						}
						if ab.Kind != "AB" {
							continue
						}
						// CR 605.1b: a mana ability is never a loyalty ability, so a
						// loyalty-marked AB$ Mana (Koth's [+1], Ugin, Eye of the
						// Storms' [0]: Add {C}{C}{C}) is NOT exempted here: it is a
						// loyalty ability, offered through this loop under the CR 606.3
						// gates below -- sorcery timing, once per permanent per turn --
						// exactly like every other [+N]/[-N]. The mana-ability path
						// (availableManaAbilitiesUsing) excludes it symmetrically; the
						// exclusion there is what closed the ulalek-eldrazi seed-1019
						// livelock (an un-tapping, gate-free, zero-cost repeatable
						// +3 colourless activation re-offered every priority window).
						//
						// The zone gate and the loyalty read come from the configured
						// ability's facts (mana_safacts.go: abilityZoneOK per zone and
						// isLoyaltyAbility, verified in the rules test binary) when the
						// ability has them, else from the ordinary readers. Both gates
						// only skip, so testing the zone first -- the one a hand,
						// graveyard or stack card's battlefield ability fails -- selects
						// exactly the abilities the mana-then-zone order did.
						var loyal bool
						mf := e.manaFactsOf(ab)
						if mf != nil {
							if manaSAFactsVerify && (mf.zoneOKFact(ab, z) != abilityZoneOK(ab, z) || mf.loyalty != e.isLoyaltyAbility(ab)) {
								panic(fmt.Sprintf("rules: configured ability facts for %q disagree with the zone/loyalty readers", ab.Line))
							}
							if !mf.zoneOKFact(ab, z) {
								continue
							}
							loyal = mf.loyalty
						} else {
							if !abilityZoneOK(ab, z) {
								continue
							}
							loyal = e.isLoyaltyAbility(ab)
						}
						if cards.IsManaAbilityAPI(ab.API) && !loyal {
							continue
						}
						if ab.ParamStr(cards.PKSorcerySpeed) == "True" && !sorcery {
							continue
						}
						// Activator$ constrains who may activate the ability, not who
						// controls its source. Resolve You/Opponent relative to the
						// source's current controller and bind source-dependent selectors
						// to the ability's permanent; unknown selectors fail closed.
						if !e.activatorAllows(p, id, ab) {
							continue
						}
						// PlayerTurn$ True (Wishclaw Talisman's "Activate only during
						// your turn"): the ability is offered only while its
						// controller is the active player. CR 602.1b would otherwise
						// offer it on any player's priority. ActivationPhases$ and the
						// other window riders (OpponentTurn$, ActivationFirstCombat$,
						// ActivationAfterBlockers$) ride the same shared
						// offer-time gate, so one helper covers the cast and ability
						// halves alike.
						if !e.activationPhasesOK(p, ab) {
							continue
						}
						// ActivationGameTypes$ (activationGameTypesOK, above): a comma
						// list of the formats the ability exists in. In a Constructed
						// game every token list fails closed and the ability is
						// withheld -- one gate here covers both the real-pool offer and
						// the hypothetical walk (offerCastable's hyp variants share
						// this loop body).
						if raw, ok := ab.Param(cards.PKActivationGameTypes); ok && !activationGameTypesOK(e.format, raw) {
							continue
						}
						// CR 606.3: a planeswalker's loyalty ability may be activated
						// only at the time a sorcery could be played -- during the
						// controller's own main phase with an empty stack -- and a
						// player may not activate a loyalty ability of a PERMANENT if
						// any loyalty ability OF THAT PERMANENT has already been
						// activated this turn. The gate is per permanent, not per
						// ability index: after [+2] the [0] draw-three is just as
						// withheld as a second [+2]. sorcerySpeed already folds the
						// own-turn and main-phase halves; the once-per-turn half is
						// the loyaltyActivationsThisTurn scan below (the event-log
						// scan the ActivationLimit$ gate uses, keyed to the object
						// and bounded by its current battlefield stint, CR 400.7),
						// because no corpus loyalty ability carries ActivationLimit$
						// and the gate must exist anyway (before this gate the
						// [+2]/[0] abilities were offered, payable and repeatable
						// without bound -- the live Jace draw-three exploit).
						if loyal {
							// A loyalty-timing grant (loyalty_flash.go: Jace's
							// Machinations, The Wandering Emperor) lifts only the
							// sorcery-timing half, never the per-turn limit below.
							if !sorcery && !e.loyaltyAtInstantSpeed(p, id) {
								continue
							}
							if e.loyaltyActivationsThisTurn(id) >= e.loyaltyAbilityLimit(id) {
								continue
							}
						}
						if w.abilityRestricted(p, id, ab) {
							continue
						}
						// Activation$ (Sea Gate Wreckage's "Activate only if you have
						// no cards in hand"): the keyword activation condition at offer
						// time, the same funnel the CheckSVar$ gate below applies --
						// a gate you can read must not leave a paid no-op reachable.
						if !e.activationConditionOK(p, ab) {
							continue
						}
						// F05-2 (CR 733.2): a card whose activation aborted with no
						// progress twice in this window is held out here too, exactly
						// like the cast options above -- the suppression is per CARD
						// (abortCast keys it on pc.card, which for an activation is the
						// source), and the abort sites cover "cast/activation" alike.
						// Without this check an activation the payer cannot complete
						// (a mis-answered phyrexian pip, a pool that moved) re-offered
						// forever inside one priority window: measured, the commander
						// bench spun 20000 intents on Solphim's {1}{R/P}{R/P} ability
						// (seed 1295, 2026-09-15) because the ability-offer path never
						// read the map the abort wrote.
						if e.castSuppressed(p, id) {
							continue
						}
						if e.activationLimitBlocked(p, id, ab, i, "", pa.Merged) {
							continue
						}
						// kw:Boast (CR 702.142): a Boast ability (Forge's `Boast$ True`
						// parameter on the AB, not a K: keyword line) may be activated
						// only if the source creature attacked this turn, and only once
						// each turn. The once-per-turn half folds into the same
						// activation-event scan the ActivationLimit$ gate uses.
						if strings.EqualFold(strings.TrimSpace(ab.ParamStr(cards.PKBoast)), "True") && !e.boastGateOK(id, i, "") {
							continue
						}
						// The configured facts carry the compiled Cost$ (the same
						// frozen parse parseCost copies out of the cost table).
						var cost Cost
						if mf != nil && mf.cost != freeCost {
							cost = mf.cost.Cost
						} else {
							cost = e.parseCost(ab.ParamStr(cards.PKCost))
						}
						// The ability's own ReduceCost$ (Otawara's Channel): the CR
						// 601.2f composition the offer gate and beginActivation's
						// charge share, so an offered cost and the paid one agree.
						// ownReduceCostOffer resolves a target-dependent body against
						// the best legal root target, because the chosen target does
						// not exist at offer time (belt_of_giant_strength).
						if n := e.ownReduceCostOffer(p, id, ab, pa.Merged); n > 0 && cost.Generic >= n {
							cost.Generic -= n
						} else if n > 0 {
							cost.Generic = 0
						}
						e.powerUpReduceCost(id, ab, &cost)
						if pay.ActivationTapCostUnavailable(o, &cost) || pay.TapCostSick(asPayer(e), id, &cost, activatesAsIfHaste(e, id)) {
							continue
						}
						// CR 702.6 / CR 601.2f: a minted attach-cost SA (K:Equip/K:Fortify,
						// cards/kw_equip.go) whose rider carries AlternateCost$ -- the
						// fourth colon field -- is an alternative cost the activator may
						// pay INSTEAD of the printed one. Offer it as its own "ability"
						// option, exactly the way the cast walk offers an AlternativeCost
						// static's cost as its own "cast" option (AltCostIndex = 1 marks
						// "the alternate cost", 0 the printed one --
						// decision.Option.AltCostIndex). The rider is evaluated
						// INDEPENDENTLY of the printed cost: an equip whose printed cost
						// is unpayable but whose alternate is payable must still be
						// offered (that is the whole point of "pay {B} instead" for
						// Transmogrant's Crown). abilityAlternateCost scopes itself to
						// the minted attach-cost SAs (isAttachCostSA) -- an AB$ line's
						// own AlternateCost$ parameter stays unread here -- and fails
						// closed on an unpriceable rider, so no unpayable option is ever
						// offered, and the ability is withheld only when NEITHER cost is
						// payable.
						altCost, hasAlt := e.abilityAlternateCost(ab)
						printedOK := w.offerCastable(p, id, cost, abilityScope(ab), true)
						altOK := hasAlt && w.offerCastable(p, id, altCost, abilityScope(ab), true)
						if !printedOK && !altOK {
							continue
						}
						if !e.abilityTargetsAvailable(p, id, ab) {
							continue
						}
						// CR 603.2's intervening-if at activation: an ability whose
						// CheckSVar$ fails is not offered (Bloodsoaked Champion's Raid —
						// "Activate only if you attacked this turn"). Offer time, not
						// resolve time: the resolution runs the effect's own
						// Condition* gate (conditionMet) where the SA carries one; an
						// offered-but-gated activation that resolves into nothing would
						// be a paid no-op the offer loop could have withheld.
						if !e.sVarGateOK(p, id, ab, pa.Merged) {
							continue
						}
						// IsPresent$/PresentCompare$ (Mistveil Plains' "Activate only if
						// you control two or more white permanents"): the same offer-time
						// gate funnel as the CheckSVar$ read above.
						if !e.abilityPresentHolds(p, id, ab) {
							continue
						}
						// Adapt$ (CR 702.35a): "Activate only if this creature has no
						// +1/+1 counters on it" -- the offer-time twin of the effect's own
						// if-condition effects/counters.go enforces at resolution.
						if !e.adaptGateOK(id, ab) {
							continue
						}
						// Monstrosity$ (CR 701.31b): "Activate only if this creature
						// isn't monstrous" -- the once-only monstrosity gate, the same
						// offer-time funnel the Adapt$ gate above sits in.
						if !e.monstrosityGateOK(id, ab) {
							continue
						}
						// kw:Reconfigure (CR 702.150): the expansion's unattach half
						// carries Unattach$ True and is offered only while the source is
						// attached -- "unattach from a creature" has no legal action for
						// an unattached permanent, and a payable no-op the deterministic
						// bot can answer identically forever is the livelock shape the
						// offer gates exist to withhold.
						if o.AttachedTo == 0 && ab.API == "Attach" && effects.AttachOf(ab).Unattach {
							continue
						}
						if printedOK {
							*out = append(*out, decision.Option{Index: len(*out), Kind: "ability",
								Label: abFace.Name + ": " + ab.ParamStr(cards.PKSpellDescription), Obj: id, Ability: i,
								Cost:  e.abilityOfferCost(p, id, ab),
								Grant: e.abilityGrant(id, ab), Attach: ab.API == "Attach",
								GrantStatics:  staticModesFromSVars(ab, abFace.SVars),
								SelfSkipTurns: abilitySelfSkipTurns(ab), ForeignSource: o.Controller != p})
						}
						if altOK {
							*out = append(*out, decision.Option{Index: len(*out), Kind: "ability",
								Label: abFace.Name + ": " + ab.ParamStr(cards.PKSpellDescription) + " (alternate cost)",
								Obj:   id, Ability: i, AltCostIndex: 1, Grant: e.abilityGrant(id, ab), Attach: ab.API == "Attach",
								GrantStatics:  staticModesFromSVars(ab, abFace.SVars),
								SelfSkipTurns: abilitySelfSkipTurns(ab), ForeignSource: o.Controller != p})
						}
					}
					// Keyword-granted abilities (CR 613.1f): a layer-6 AddKeyword$
					// grant of an EXPANDED keyword (Cycling, TypeCycling, Saddle,
					// Crew) gives a card an activated ability NO printed face
					// carries, so the pile walk above never offers it. Synthesize
					// the same body the printed expansion builds
					// (cards.GrantedKeywordAbility) and offer it through the same
					// gates, anchored on the derived keyword line beginActivation
					// resolves -- exactly the SVar-anchor shape with the line
					// standing in for the name. A line the printed face (or a pile
					// under-card) already expands is skipped -- the printed offer
					// exists -- and a shape the synthesizer cannot model is
					// skipped whole (fail closed).
					//
					// ZONE: the body's own ActivationZone$ decides (abilityZoneOK
					// below). The cycling bodies carry ActivationZone$ Hand; the
					// Saddle/Crew bodies carry no rider and are battlefield
					// abilities, so this block reads every zone, not just the hand.
					//
					// The pile walk's rider gates that a synthesized body can
					// carry are replicated here: SorcerySpeed$ (Saddle's body is
					// CR 702.171a sorcery-only), the tap-cost availability gates
					// (crew/saddle both pay a tapXType cost) and the offer-time
					// CheckSVar$/IsPresent$ funnels.
					if pileSkip && len(*out) != pileMark {
						panic(fmt.Sprintf("rules: pile-ability skip dropped %d options of obj %d in zone %d", len(*out)-pileMark, id, z))
					}
					for _, line := range w.grantedKeywordLines(board, o, id, f, faceFacts) {
						ab := cards.GrantedKeywordAbility(line)
						if ab == nil || !abilityZoneOK(ab, z) {
							continue
						}
						if ab.ParamStr(cards.PKSorcerySpeed) == "True" && !sorcery {
							continue
						}
						if !e.activatorAllows(p, id, ab) {
							continue
						}
						if !e.activationPhasesOK(p, ab) {
							continue
						}
						if w.abilityRestricted(p, id, ab) || e.castSuppressed(p, id) {
							continue
						}
						if !e.activationConditionOK(p, ab) {
							continue
						}
						if e.activationLimitBlocked(p, id, ab, -1, line, 0) {
							continue
						}
						cost := e.parseCost(ab.ParamStr(cards.PKCost))
						if n := e.ownReduceCostOffer(p, id, ab, 0); n > 0 && cost.Generic >= n {
							cost.Generic -= n
						} else if n > 0 {
							cost.Generic = 0
						}
						e.powerUpReduceCost(id, ab, &cost)
						if pay.ActivationTapCostUnavailable(o, &cost) || pay.TapCostSick(asPayer(e), id, &cost, activatesAsIfHaste(e, id)) {
							continue
						}
						if !w.offerCastable(p, id, cost, abilityScope(ab), true) {
							continue
						}
						if !e.abilityTargetsAvailable(p, id, ab) {
							continue
						}
						if !e.sVarGateOK(p, id, ab, 0) {
							continue
						}
						if !e.abilityPresentHolds(p, id, ab) {
							continue
						}
						*out = append(*out, decision.Option{Index: len(*out), Kind: "ability",
							Label: f.Name + ": " + ab.ParamStr(cards.PKSpellDescription), Obj: id,
							Ability: -1, Keyword: line})
					}
					w.recordAbilityBlock(z, zonePlayer, id, blockStart, blockGates)
				}
			}
		}

		// Granted activated abilities (CR 613.1f, rules/legal_granted.go's
		// grantedAbilities): the abilities an AddAbilities continuous grant -- a
		// Saga chapter's Animate, "CARDNAME gains '{T}: Add {C}'." -- gives a
		// permanent. Non-mana ones are offered here with the SVar anchor
		// beginActivation resolves (the same anchor the max-speed "granted"
		// option carries); mana ones flow through availableManaAbilities below so
		// the "Tap for mana" priority action and the payment window share one
		// member set. Gates mirror the printed loop above, loyalty gate included:
		// a GAINED ability (GainsAbilitiesOf$, Nicol Bolas Dragon-God's
		// `GainsValidAbilities$ Activated.Loyalty`) and an SVar-anchored GRANT
		// (Rowan's Talent's AddAbility$ [+1]) can each be a loyalty ability, so
		// the CR 606.3 gates below apply to them exactly as to a printed one. The two
		// activation limits are checked here too, with the SVar-name identity
		// (see the gate's own comment below): Touch of Vitae carries
		// GameActivationLimit$ 1 on an Animate-delivered AddAbility$ body.
		// Without a grant anywhere on the board grantedAbilities is nil for
		// every object (activeSummary.hasGrants), so the sweep is skipped.
		for zonePlayer := range e.G.Players {
			if !board.hasGrants && !walkSkipVerify {
				break
			}
			for _, id := range e.G.Zone(state.ZBattlefield, state.PlayerID(zonePlayer)) {
				o := e.G.Obj(id)
				if o == nil || o.Face() == nil || e.faceDownPrintedHides(o) {
					// A face-down permanent is not offered granted abilities: the
					// offer label reads the printed face name, which CR 708.8 says
					// does not exist while face down.
					continue
				}
				if !pay.ExistsOnBattlefield(o) {
					// CR 702.25b: a phased-out permanent is treated as though it does
					// not exist, so none of its granted or gained activated abilities
					// is offered or activatable.
					continue
				}
				for _, ga := range e.grantedAbilities(p, id) {
					ab := ga.SA
					// cards.IsManaAbilityAPI, not a bare "Mana" check: a granted
					// ManaReflected flows through availableManaAbilities too (its
					// IsPresent$ gate lives in manaReflectedPresentHolds, which knows
					// the hasAbility Activated.otherAbility special form).
					if cards.IsManaAbilityAPI(ab.API) {
						continue
					}
					if ab.ParamStr(cards.PKSorcerySpeed) == "True" && !sorcery {
						continue
					}
					// Activator$ applies to a granted/gained ability exactly as to a
					// printed one (the source is the recipient permanent).
					if !e.activatorAllows(p, id, ab) {
						continue
					}
					// CR 606.3 for a GAINED or GRANTED loyalty ability: the same
					// sorcery-timing and once-per-permanent gates the printed loop
					// applies -- a gained (GainsAbilitiesOf$) or SVar-granted
					// (AddAbility$: Rowan's Talent's "[+1]: Up to one target creature
					// gets +2/+0 ...") loyalty ability is a loyalty ability of THIS
					// permanent (the recipient), and loyaltyActivationsThisTurn counts
					// its GainedAbilityPush / GrantAbilityPush activations beside the
					// printed AbilityPush ones. The granted half was once exempt on the
					// premise that an AddAbility$ body is never a loyalty ability;
					// Rowan's Talent's body is one, and the exemption let a bot
					// activate it without bound (cardfuzz batch1 line 18: 20000
					// intents of "+1" on one Jaya Ballard in one main phase).
					if e.isLoyaltyAbility(ab) {
						if !sorcery && !e.loyaltyAtInstantSpeed(p, id) {
							continue
						}
						if e.loyaltyActivationsThisTurn(id) >= e.loyaltyAbilityLimit(id) {
							continue
						}
					}
					// F05-2 (CR 733.2): the granted twin of the printed loop's
					// no-progress hold-out. abortCast keys the suppression on the
					// activation's source (pc.card), which for a granted or gained
					// ability is this recipient -- without the check a granted
					// activation whose transaction aborts (no legal target, an
					// unpayable cost) was re-offered inside one priority window
					// forever (cardfuzz batch5 line 1: Trazyn's gained Equip).
					if w.abilityRestricted(p, id, ab) || e.castSuppressed(p, id) {
						continue
					}
					cost := e.parseCost(ab.ParamStr(cards.PKCost))
					bindGrantedCostReferents(&cost, ga.Source)
					// The granted twin of the printed loop's own ReduceCost$ fold.
					if n := e.ownReduceCostOffer(p, id, ab, 0); n > 0 && cost.Generic >= n {
						cost.Generic -= n
					} else if n > 0 {
						cost.Generic = 0
					}
					if pay.ActivationTapCostUnavailable(o, &cost) || pay.TapCostSick(asPayer(e), id, &cost, activatesAsIfHaste(e, id)) {
						continue
					}
					if !w.offerCastable(p, id, cost, abilityScope(ab), true) {
						continue
					}
					if !e.abilityTargetsAvailable(p, id, ab) {
						continue
					}
					// IsPresent$/PresentCompare$: the same offer-time gate the printed
					// loop applies, so a granted ability and its printed twin share one
					// eligibility set.
					if !e.abilityPresentHolds(p, id, ab) {
						continue
					}
					// Adapt$ (CR 702.35a): the granted twin of the printed loop's gate.
					if !e.adaptGateOK(id, ab) {
						continue
					}
					// A has-all-abilities-of gained ability (GainsAbilitiesOf$) is
					// offered with its foreign-card anchor; every other granted
					// ability keeps the SVar-name anchor (boastGateOK's and the
					// activation-limit gate's identity).
					if ga.Gained {
						if strings.EqualFold(strings.TrimSpace(ab.ParamStr(cards.PKBoast)), "True") && !e.boastGateOK(id, -1, "") {
							continue
						}
						if e.activationLimitBlocked(p, id, ab, -1, "", 0) {
							continue
						}
						*out = append(*out, decision.Option{Index: len(*out), Kind: "ability",
							Label: o.Face().Name + ": " + ab.ParamStr(cards.PKSpellDescription), Obj: id,
							GainedSource: ga.GainedFrom, GainedIdx: ga.GainedIdx, Attach: ab.API == "Attach"})
						continue
					}
					// kw:Boast (CR 702.142): the granted twin of the printed loop's
					// Boast gate. The identity is the SVar name the grant anchored on,
					// because beginGrantedActivation mints a DelayedPush rather than an
					// AbilityPush (boastGateOK reads both).
					if strings.EqualFold(strings.TrimSpace(ab.ParamStr(cards.PKBoast)), "True") && !e.boastGateOK(id, -1, ga.SVar) {
						continue
					}
					// The two activation limits, for a GRANTED ability: the same shared
					// gate the printed loop above calls, with the SVar-name identity
					// because the mint is a DelayedPush/GrantAbilityPush. The printed
					// loop's old claim -- "no corpus granted ability carries a limit" --
					// is FALSE: Touch of Vitae carries GameActivationLimit$ 1 on the
					// AddAbility$ body it animates onto a target. (That specific grant
					// does not resolve yet for an unrelated reason -- its SVar lives on
					// the Instant's face, while Animate resolves granted names off the
					// ANIMATED object's table; see the report's Issues.) The gate is kept
					// so a granted ability with a limit is never re-offered once used,
					// and self-animate grants -- where the SVar table IS the recipient's
					// -- are pinned by TestGameActivationLimitGrantedAbilityWithheldAfterOneUse.
					if e.activationLimitBlocked(p, id, ab, -1, ga.SVar, 0) {
						continue
					}
					// Offer only what the activation can resolve. The collector
					// above reads the body off the emitting effect's captured SVar
					// table (ce.SVars), while beginGrantedActivation -- and the
					// GrantAbilityPush/DelayedPush mint a replay re-runs -- resolve
					// the NAME against the grantor object's faces. When the two
					// disagree (the grantor's face does not carry the table the
					// effect captured) the option was a silent no-op: chosen, it
					// emitted nothing and the identical board re-offered it forever
					// (cardfuzz batch7 line 2: a gained-Animate grant on Manascape
					// Refractor, 100x "Regenerate CARDNAME" in one main phase).
					if e.grantedSAFrom(ga.Source, id, ga.SVar) == nil {
						continue
					}
					*out = append(*out, decision.Option{Index: len(*out), Kind: "ability",
						Label: o.Face().Name + ": " + ab.ParamStr(cards.PKSpellDescription), Obj: id, SVar: ga.SVar,
						GrantSource: ga.Source, Attach: ab.API == "Attach"})
				}
			}
		}

		// kw:Station (CR 702.150, rules/station.go): each Spacecraft the player
		// controls may be stationed as a sorcery by tapping another creature.
		// The offer is gated on the sorcery window (Station only as a sorcery)
		// and on a legal tap candidate existing, so an unpayable station is
		// never offered; the tap candidate itself is the KChoose askStation
		// poses after the option is chosen.
		if sorcery {
			for _, id := range e.G.Zone(state.ZBattlefield, p) {
				o := e.G.Obj(id)
				if o == nil || o.Face() == nil || !e.hasKeywordH(id, kwhStation) {
					continue
				}
				if !pay.ExistsOnBattlefield(o) {
					// CR 702.25b: a phased-out permanent is treated as though it
					// does not exist, so it cannot be stationed.
					continue
				}
				if len(e.stationCandidates(p, id)) == 0 {
					continue
				}
				w.add("station", "Station "+o.Face().Name, id)
			}
			// Room unlock (CR 309.5, rules/rooms.go): a room whose second door is
			// still locked may be unlocked as a sorcery by paying that half's own
			// mana cost. The gate is the same castable total the cast options
			// use, so an unpayable unlock is never offered (and the payment on
			// the answer cannot disagree with the offer). The unlock is a
			// special action, priced under specialActionScope("unlock") so a
			// ValidSpell$ Static.Unlock modifier (Inquisitive Glimmer) reaches
			// it and a Type$ Spell/Ability one does not; the payment composes
			// the same modifiers (handlePriority's "unlock" case).
			for _, id := range e.G.Zone(state.ZBattlefield, p) {
				o := e.G.Obj(id)
				if o == nil || o.Face() == nil || e.faceDownPrintedHides(o) {
					continue
				}
				if !pay.ExistsOnBattlefield(o) {
					// CR 702.25b: a phased-out room is treated as though it does
					// not exist, so it cannot be unlocked.
					continue
				}
				for _, fi := range []int{1 - int(o.FaceIdx), int(o.FaceIdx)} {
					cost, ok := e.unlockRoomFaceCost(o, fi)
					if !ok {
						continue
					}
					if _, ok := e.unlockMods(p, id); ok && w.offerCastable(p, id, cost, specialActionScope("unlock"), true) {
						w.out = append(w.out, decision.Option{Index: len(w.out), Kind: "unlock", Label: "Unlock " + o.Card.Faces[fi].Name, Obj: id, Mode: strconv.Itoa(fi)})
					}
				}
			}
		}

		// kw:Start your engines (CR 702.179e, rules/speed.go): a max-speed
		// static grants its AddAbility$ while its controller has speed 4, and
		// the granted ability is offered through the same cost/target gates
		// every other activation uses. NOT sorcery-gated: the grant is an
		// ordinary activated ability (Amonkhet Raceway's {T}: pump) whose timing
		// is its own cost's -- it needs a priority window, not a main phase, so
		// the offer sits outside the sorcery block with the other activation
		// offers.
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			o := e.G.Obj(id)
			if o == nil || o.Face() == nil {
				continue
			}
			if !pay.ExistsOnBattlefield(o) {
				// CR 702.25b: a phased-out permanent is treated as though it
				// does not exist, so its max-speed granted abilities are not
				// offered.
				continue
			}
			for _, ab := range e.maxSpeedAbilities(p, id) {
				// castSuppressed: the F05-2 no-progress hold-out every other
				// activation offer reads (an aborted activation is keyed on its
				// source).
				if w.abilityRestricted(p, id, ab) || e.castSuppressed(p, id) {
					continue
				}
				cost := e.parseCost(ab.ParamStr(cards.PKCost))
				if pay.ActivationTapCostUnavailable(o, &cost) || pay.TapCostSick(asPayer(e), id, &cost, activatesAsIfHaste(e, id)) {
					continue
				}
				if !w.offerCastable(p, id, cost, abilityScope(ab), true) {
					continue
				}
				if !e.abilityTargetsAvailable(p, id, ab) {
					continue
				}
				// IsPresent$/PresentCompare$: the same offer-time gate the printed
				// loop applies, so a max-speed grant and its printed twin share one
				// eligibility set.
				if !e.abilityPresentHolds(p, id, ab) {
					continue
				}
				// Adapt$ (CR 702.35a): the max-speed grant's twin of the same gate.
				if !e.adaptGateOK(id, ab) {
					continue
				}
				// kw:Boast (CR 702.142): the max-speed grant is a third offer site
				// for an SVar-anchored ability, so it shares the Boast gate. The
				// identity is the SVar name beginGrantedActivation mints its
				// DelayedPush with (abSVarName), never a face index.
				sv := abSVarName(o.Face(), ab)
				if strings.EqualFold(strings.TrimSpace(ab.ParamStr(cards.PKBoast)), "True") && !e.boastGateOK(id, -1, sv) {
					continue
				}
				*out = append(*out, decision.Option{Index: len(*out), Kind: "granted",
					Label: o.Face().Name + ": " + ab.ParamStr(cards.PKSpellDescription),
					Obj:   id, SVar: sv})
			}
		}
		// Morph-family turn face up (CR 708.6 / CR 116.2b, rules/morph_turnup.go):
		// a face-down permanent its controller cast with Morph, Megamorph or
		// Disguise may be turned face up as a SPECIAL ACTION any time they have
		// priority -- it is not sorcery-gated, does not use the stack, and its
		// only cost is the keyword's own printed parameter. The offer is gated on
		// the same floating pool the action pays, so the charge cannot disagree
		// with what was offered; a manifest or cloak carrier (no family flag) is
		// never offered here -- its turn-up is a separate subsystem.
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			// morphFaceUpCost answers false for any object that is not face
			// down; test that flag before building its cost.
			if o := e.G.Obj(id); o == nil || !o.FaceDown {
				continue
			}
			mf, ok := morphFaceUpCost(e.G.Obj(id))
			if !ok {
				continue
			}
			// morphTurnUpPayablePriced: hyp nil is the floating-pool gate the
			// action itself re-reads; the potential walk prices the same cost
			// against its hypothetical bound, so a float-gated turn-up is a
			// potential play like every other mana-costed offer.
			mods, ok := e.morphTurnUpMods(p, id, mf)
			if !ok || !e.morphTurnUpPayablePriced(p, id, mf.cost, mods, w.pricing()) {
				continue
			}
			if e.turnFaceUpCantHappen(id) {
				// CR 614.1a: a live CantHappen turn-up replacement (Karlov
				// Watchdog's "permanents your opponents control can't be turned
				// face up during your turn") makes the special action illegal, so
				// the option is never offered -- and never paid for. The one match
				// predicate lives in rules/replacement.go (turnFaceUpCantHappen);
				// the submitted-option guard in rules/priority_guard.go re-reads
				// the same helper.
				continue
			}
			w.add("turn_face_up", "Turn face up ("+costPhrase(mf.cost)+")", id)
		}

		// Specialize is a no-stack special action and may be taken only as a
		// sorcery. Each legal target face is a separate option, avoiding a second
		// decision while preserving player choice.
		if sorcery {
			for _, id := range e.G.Zone(state.ZBattlefield, p) {
				o := e.G.Obj(id)
				if o == nil || o.Card == nil {
					continue
				}
				for i := 1; i < len(o.Card.Faces); i++ {
					// hyp nil is specializeLegal exactly; the potential walk
					// prices the printed cost against its hypothetical bound.
					cost, ok := e.specializeLegalPriced(p, id, i, w.pricing())
					if !ok {
						continue
					}
					w.add("specialize", "Specialize as "+o.Card.Faces[i].Name+" ("+costPhrase(cost)+")", id)
					(*out)[len(*out)-1].Mode = strconv.Itoa(i)
				}
			}
		}
	}
}
