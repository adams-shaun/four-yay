package rules

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// validateCastContributions is the Submit-time gate for the cast flow's
// Convoke/Harmonize announcement decision (convokeAsk). The decision's
// static Validate sees only the offered option list -- two white creatures
// each carry a convoke_W option for a {W} spell, in distinct groups -- so
// an over-selection passes it; this gate rejects any answer containing a
// contribution that reduces nothing (convokeAbsorbs), before the intent is
// recorded and the pending decision is consumed, exactly like
// validateAttackers. The client then resubmits a legal subset. Any other
// KChoose decision, and an out-of-range choice (Validate's own error), is
// passed through untouched.
func (e *Engine) validateCastContributions(d *decision.Decision, in decision.Intent) error {
	pc := e.cast
	if pc == nil || len(in.Choices) == 0 {
		return nil
	}
	if len(d.Options) > 0 && castAnswerCodes.Code(d.Options[0].Kind) == castAnswerTeamwork {

		for _, choice := range in.Choices {
			o := d.Options[choice]
			switch castAnswerCodes.Code(o.Kind) {
			case castAnswerTeamwork:
				creature := e.G.Obj(o.Obj)
				if creature == nil || creature.Tapped || creature.Controller != pc.player || !e.matchesSpecFrom("Creature.YouCtrl", o.Obj, pc.player, pc.card) {
					return fmt.Errorf("teamwork creature is no longer an untapped creature you control")
				}
			}
		}
		return nil
	}
	var pays []convokePayment
	for _, c := range in.Choices {
		if c < 0 || c >= len(d.Options) {
			return nil
		}
		o := d.Options[c]
		switch {
		case o.Kind == "harmonize":
			pays = append(pays, convokePayment{ID: o.Obj, Power: int32(o.Amount)})
		case strings.HasPrefix(o.Kind, "activation_creature"):
			pays = append(pays, convokePayment{ID: o.Obj})
		case o.Kind == "improvise_generic":
			pays = append(pays, convokePayment{ID: o.Obj})
		case o.Kind == "waterbend_generic":
			pays = append(pays, convokePayment{ID: o.Obj, Waterbend: true})
		case strings.HasPrefix(o.Kind, "convoke_"):
			color := byte(0)
			if o.Kind != "convoke_generic" {
				color = o.Kind[len("convoke_")]
			}
			pays = append(pays, convokePayment{ID: o.Obj, Color: color, CountsMana: true})
		default:
			return nil // a different cast-flow ask, not the convoke announcement
		}
	}
	all := append(append([]convokePayment(nil), pc.Convoke...), pays...)
	if !e.convokeAbsorbs(pc, e.manaToPay(pc), all, pc.cost.X > 0) {
		return fmt.Errorf("announcement reduces nothing: the outstanding cost cannot absorb every chosen contribution")
	}
	// Waterbend taps pay only the waterbend amount (CR 701.67a). A fixed
	// Waterbend<N> part has a cap known here; a Waterbend<X> amount is the
	// announced X, which does not exist yet at this pre-X announcement gate,
	// so xAsk -- the one site that knows X -- enforces that cap through
	// waterbendCap, and its last-resort fallback never resurrects an X the
	// cap rejected.
	if !pc.mods.WaterbendX && pc.mods.RaiseX == 0 && waterbendTaps(all) > waterbendCap(pc.mods, 0) {
		return fmt.Errorf("more permanents tapped than the waterbend cost allows")
	}
	return nil
}

// convokeAsk announces every creature used for Convoke or Harmonize. It is
// deliberately before manaWindowAsk: tapping is part of paying, so a chosen
// creature cannot first be used as a mana source.
func (e *Engine) convokeAsk() bool {
	pc := e.cast
	if pc == nil || pc.ConvokeDone {
		return false
	}
	pc.ConvokeDone = true
	// A cast announces Convoke/Harmonize/Improvise; an activated ability
	// announces only its own waterbend contributions (Giant Koi's
	// `Cost$ Waterbend<3>`), so the cast-only keyword readers are skipped
	// for an ability.
	isAbility := pc.isAbility()
	isConvoke := !isAbility && e.hasCastConvoke(pc.card)
	isHarmonize := !isAbility && pc.mode == "harmonize"
	isImprovise := !isAbility && e.hasCastImprovise(pc.card)
	// A Waterbend<N>/<X> cost (a RaiseCost additional cost, or the ability's
	// or optional part's own Waterbend token folded into mods by
	// foldRaiseExtra): each untapped artifact or creature tapped while paying
	// it pays for {1} of the waterbend amount (CR 701.67a).
	isWaterbend := pc.mods.Waterbend > 0 || pc.mods.WaterbendX
	creatureMana := isAbility && pay.TapCreaturesForMana(e.pcAbility(pc))
	if !isConvoke && !isHarmonize && !isImprovise && !isWaterbend && !creatureMana {
		return false
	}
	// Before X is announced, its generic requirement is not folded into
	// mana. It nevertheless makes every creature a possible generic payment;
	// the subsequent xAsk prices the selected contributions against the real
	// X total.
	return pay.ConvokeAsk(asPayer(e), &pc.CastPayment, &pc.PaidCost, pay.ConvokeOffer{
		Player: pc.player, Card: pc.card,
		Convoke: isConvoke, Harmonize: isHarmonize, Improvise: isImprovise, Waterbend: isWaterbend,
		CreatureMana: creatureMana, SourceTaps: pc.cost.Tap,
		Mana: e.manaToPay(pc), HasX: pc.cost.X > 0,
		WaterbendN: pc.mods.Waterbend, WaterbendOpen: pc.mods.RaiseX != 0 || pc.mods.WaterbendX,
	})
}

func (e *Engine) manaToPayX(pc *pendingCast, x int32) Cost {
	return e.manaToPayXUsing(pc, x, e.manaToPayXMods(pc, x))
}

// manaToPayXMods is the modifier composition manaToPayX applies to a
// candidate X: pc.mods, except that a cost whose announced count feeds a
// ReduceCost static reading the paid X (Dargo's {2}-less-per-sacrifice) is
// re-priced with the X-bound targets, because the offer-time pc.mods snapshot
// was bound to X=0. xAsk reads it so the convoke-absorption check and the
// potential-target retry compose the SAME modifiers the payable check used.
func (e *Engine) manaToPayXMods(pc *pendingCast, x int32) costMods {
	if costAnnouncesPaidX(pc.cost) {
		if scope, ok := e.pendingCastScope(pc); ok {
			return e.costModifiersForTargetsX(pc.player, pc.card, scope, pc.targets, x)
		}
	}
	return pc.mods
}

// manaToPayXUsing is manaToPayX with an explicit modifier composition.
// xAsk's target-potential retry prices a candidate X under the reduction a
// potential target would give (mods is then the potential composition); every
// other caller passes manaToPayXMods. The commander tax is folded in the same
// place as manaToPayX, so an alternative composition charges the identical
// total.
func (e *Engine) manaToPayXUsing(pc *pendingCast, x int32, mods costMods) Cost {
	m := mods.Apply(pc.resolvedManaX(x))
	m.Generic += pc.taxGeneric
	return m
}

// announceCost is gone: its per-pip composition (mods applied to a cost that
// still carried the unannounced pips, so a Color$ reduction saw no W pip it
// could legally take and a floor priced an unresolved pip at its generic
// face) answered a different feasibility question than the offer gate and
// the charge, and could reject the only legal announcement (a reduction that
// is legally assigned to a LATER pip). announceFeasible below folds the
// already-announced pips into the cost as their final resolved faces and
// hands the remainder to the one shared primitive, costMods.feasibleAny.
// announceFeasible reports whether offering alternative alt for the pip the
// flow is announcing still leaves the whole cost payable: the pips already
// committed (payColor/payLife/payGeneric on pc) and the candidate alt are
// folded into the cost as their FINAL resolved faces, and the still-
// unannounced pips are enumerated by the shared primitive with the CR 601.2f
// modifiers composed onto each fully-resolved assignment, the CR 903.8
// commander tax added after and (for a spell) the Delve credit taken off the
// generic — exactly the composition manaToPay/payCast will charge once every
// pip is settled. It is the CR 601.2b legality question: an announced payment
// is offered only if SOME legal assignment of the remaining pips makes the
// total cost payable, so a player is never offered a payment that can only
// strand the cast in an unpayable remainder (and an abort at payCast).
func (e *Engine) announceFeasible(pc *pendingCast, alt pipAlt, pool, snow state.Mana, life int32) bool {
	c := pc.cost.WithX(pc.x)
	for i := range c.Colored {
		c.Colored[i] += pc.PayColor[i]
	}
	c.Generic = addClampedGeneric(c.Generic, int64(pc.PayGeneric))
	c.Life = addClampedGeneric(c.Life, int64(pc.PayLife))
	switch {
	case alt.Color != 0:
		c.Colored[state.ManaIndex(alt.Color)]++
	case alt.Generic > 0:
		c.Generic = addClampedGeneric(c.Generic, int64(alt.Generic))
	case alt.Life > 0:
		c.Life = addClampedGeneric(c.Life, int64(alt.Life))
	}
	delve := int32(0)
	if !pc.isAbility() {
		delve = int32(len(pc.delve))
	}
	// The pips 0..payIdx have been announced (their faces are folded in
	// above), so their slots leave the cost; the pips after payIdx stay live
	// for the shared primitive to enumerate.
	c = c.DropAnnouncePrefix(pc.PayIdx + 1)
	payment := paymentForCast(pc, c)
	rider := pipRider{AnyColor: pc.mayPlayIgnore, AnyType: pc.mayPlayIgnoreType}
	if e.manaFeasibleDescriptor(pc.player, payment, c, pc.mods, pc.taxGeneric, delve, rider) {
		return true
	}
	// CR 601.2b chooses a Phyrexian or hybrid face BEFORE the 601.2g mana
	// ability window.  Pricing a face only against floating mana therefore
	// withholds a coloured face whenever its source is still untapped: a
	// Gitaxian Probe with an Island offered only "Pay 2 life".  Probe the same
	// concrete source alternatives that manaWindowAsk can subsequently offer,
	// after composing exactly the modifiers, commander tax, and Delve credit
	// that payCast will charge.  This is an offer-side read only; the chosen
	// face still reaches the ordinary mana window and is paid there.
	charged := pc.mods.Apply(c)
	charged.Generic = addClampedGeneric(charged.Generic, int64(pc.taxGeneric))
	charged.Generic -= delve
	if charged.Generic < 0 {
		charged.Generic = 0
	}
	if !charged.HasManaPayment() {
		return false
	}
	av := pay.AvailableFor(asPayer(e), pc.player, payment)
	return pay.ManaReachable(asPayer(e), pc.player, charged, av.Pool, e.G.Players[pc.player].Snow,
		av.Typed, e.G.Players[pc.player].Life, rider,
		asEval(e).Conv(pc.player, payment.ID, payment.Class == paymentActivated), e.castWindowUnits(pc))
}

// manaAsk offers the player's payment choice for the next unsettled hybrid or
// Phyrexian pip of the cost (CR 601.2b), one decision per pip. EVERY face is
// gated on the ONE shared feasibility primitive (announceFeasible →
// costMods.feasibleAny): with the pips already announced and the candidate
// folded in as its final resolved face, some legal assignment of the
// remaining pips must make the composed total (modifiers on final faces,
// commander tax, Delve credit) payable. Any composed modifier can make a
// locally affordable face strand the final payment — a generic Thalia raise
// (Dismember's black faces), a Color$ reduction that is only legally
// assignable to a later pip ({W/U}{W/U} under Color$ W), a SetCost floor on
// an unresolved twobrid — and only the whole-cost search sees that, so a
// player is never offered a payment a complete assignment cannot pay. The
// valid options keep their announcePip order, so the deterministic bot
// fallback (index 0) always picks a legal payment and a no-answer host never
// wedges. The offer gate (offerCastable) proved at least one full assignment
// feasible over the same primitive, so the decision is never empty for a
// state the gate measured; an empty menu is still possible after the offer
// (the {X} choice or a repricing changed the composition) and the defensive
// arm below preserves the flow's behaviour for it. It returns true once it
// has asked (and therefore suspended); payCast applies the accumulated
// payColor / payLife / payGeneric when every pip is settled.
func (e *Engine) manaAsk() bool {
	pc := e.cast
	if pc == nil || pc.PayIdx >= pc.cost.AnnPipCount() {
		return false
	}
	// announceFeasible receives the full pool and life total because the
	// commitments already made (and the candidate face) are folded into the
	// cost it evaluates; nothing has been paid yet. Do not pre-filter a colour
	// face merely because the current pool lacks that colour: a Color$
	// reduction can make the announced face free (for example {W/U} under
	// Color$ W).
	pool, snow := e.G.Players[pc.player].Pool, e.G.Players[pc.player].Snow
	fullLife := e.G.Players[pc.player].Life
	return pay.ManaAsk(asPayer(e), &pc.CastPayment, pc.player, pc.card, pc.cost, func(alt pipAlt) bool {
		return e.announceFeasible(pc, alt, pool, snow, fullLife)
	})
}

// manaConvertAsk poses the Optional$ ManaConvert election once for this
// proposal. A mandatory conversion remains automatic; an optional conversion
// is offered even when the ordinary pool already pays, because declining is a
// meaningful player choice and the grant may matter to a later repricing.
func (e *Engine) manaConvertAsk() bool {
	pc := e.cast
	if pc == nil || castModeCodes.Code(pc.mode) == castModeLand || pc.ManaConvertDone {
		return false
	}
	_, optional := e.manaConversionParts(pc.player, pc.card, pc.isAbility())
	return pay.ManaConvertAsk(asPayer(e), &pc.CastPayment, pc.player, pc.card, !optional.Empty())
}

// castAnswer records a chooseCast answer into the flow, keyed off which
// stage asked it (every option in one decision shares a Kind).
// A payment-shaped answer (a cost-part pick, a pip face, a Convoke
// contribution, the window's "done", the ManaConvert election) is recorded by
// pay.RecordCastPaymentAnswer into the cast's CastPayment and PaidCost; the
// rest are the cast flow's own. A mana-window
// decision (CR 601.2g) is the exception: it offers both "activate" and
// "done" options, so the CHOSEN option's kind, not the stage's, identifies
// the answer.
func (e *Engine) castAnswer(d *decision.Decision, chosen []decision.Option) {
	pc := e.cast
	if pc == nil || len(d.Options) == 0 {
		return
	}
	// The decision's CHOSEN option identifies the answer, never the first
	// offered option. Most chooseCast decisions are single-kind (an {X} value,
	// a Delve exile, a sacrifice) so Options[0].Kind would coincidentally be
	// right, but a hybrid/Phyrexian/twobrid pip decision offers MIXED kinds
	// (pay_W, pay_generic, pay_life) and the mana window offers activate/done,
	// so dispatching on Options[0].Kind would mis-route a non-first choice
	// (picking a twobrid generic face from a decision whose first option is
	// pay_W fell into the pay_W branch and minted a colourless pip).
	kind := d.Options[0].Kind
	if len(chosen) > 0 {
		kind = chosen[0].Kind
	}
	if kind == "sacrifice" && pc.emerge && pc.SacPart == 0 && len(chosen) == 1 {
		pc.emergeSac = chosen[0].Obj
	}
	if pay.RecordCastPaymentAnswer(&pc.CastPayment, pay.CastPayAnswer{Paid: &pc.PaidCost, Cost: &pc.cost, X: &pc.x, XDone: pc.xDone}, kind, chosen) {
		return
	}
	switch castAnswerCodes.Code(kind) {
	case castAnswerNamedAnnounce:
		if len(chosen) > 0 {
			pc.namedN = int32(chosen[0].Amount)
		}
	case castAnswerX:
		if len(chosen) > 0 {
			// The value rides on Option.Amount, not Option.Index: xAsk is the
			// first stage and appends 0..max into an empty option list, so
			// Index happens to equal the value today, but a later task that
			// prepends an option (a "cancel", Task 10's ability variants)
			// would silently corrupt an Index-derived value.
			pc.x = int32(chosen[0].Amount)
		}
	case castAnswerReplicate:
		// CR 702.55a: the answered payment count folds that many replicate
		// payments into cost, so every later stage (Convoke, X, the payment
		// window, payCast) charges the composed total. The count rides on
		// Option.Amount, not Index, for the same reason xAsk's value does.
		if len(chosen) > 0 && pc.replicateSet {
			n := int32(chosen[0].Amount)
			rc := ParseCost(pc.replicateParam)
			for i := int32(0); i < n; i++ {
				pc.cost = pc.cost.Plus(rc)
			}
			pc.replicateTimes = n
		}
	case castAnswerMultikick:
		// CR 702.43: the answered payment count folds that many multikicker
		// payments into cost -- the replicate arm's exact shape.
		if len(chosen) > 0 && pc.multikickSet {
			n := int32(chosen[0].Amount)
			mk := ParseCost(pc.multikickParam)
			for i := int32(0); i < n; i++ {
				pc.cost = pc.cost.Plus(mk)
			}
			pc.multikickTimes = n
		}
	case castAnswerSquad:
		// CR 702.66: the answered payment count folds that many squad
		// payments into cost -- the replicate/multikicker arm's exact shape.
		if len(chosen) > 0 && pc.squadSet {
			n := int32(chosen[0].Amount)
			sc := ParseCost(pc.squadParam)
			for i := int32(0); i < n; i++ {
				pc.cost = pc.cost.Plus(sc)
			}
			pc.squadTimes = n
		}
	case castAnswerMutatePlace:
		// CR 702.140b: the answered over/under placement. Option.Amount is 1
		// for "on top", 0 for "under", so the answer is read positionally.
		if len(chosen) > 0 {
			pc.mutateTop = chosen[0].Amount == 1
		}
	case castAnswerCasualty:
		if len(chosen) == 1 {
			pc.Sacs = append(pc.Sacs, chosen[0].Obj)
			pc.casualtyPaid = true
			// Casualty:X: the chosen creature's power names the amount; it is
			// read live at payment (payCast), the rules time the sacrifice
			// settles.
			pc.casualtySac = chosen[0].Obj
		}
	case castAnswerBargain:
		// CR 702.166: the answered sacrifice settles through pc.Sacs with
		// every other cost part; the cast mode already names the election.
		if len(chosen) == 1 {
			pc.Sacs = append(pc.Sacs, chosen[0].Obj)
		}
	case castAnswerGiftDecline:
		// CR 702.168: a declined gift is the plain cast -- no promise, and
		// pushCast emits only the Amount-0 record. The byte-identical shape
		// for every non-Gift carrier, which never reaches this ask at all.
		pc.giftPromise = false
	case castAnswerGiftPromise:
		// The promised opponent rides Option.Player, the field the
		// protector/player elections share. An answer naming no live seat
		// (only reachable from a hand-built decision) degrades to a decline
		// rather than silently promising seat 0.
		pc.giftPromise = false
		if len(chosen) > 0 && chosen[0].Player != pc.player &&
			int(chosen[0].Player) < len(e.G.Players) && !e.G.Players[chosen[0].Player].Lost {
			pc.giftPromise = true
			pc.giftTo = chosen[0].Player
		}
	case castAnswerTeamwork:
		// An empty answer declines: no taps and no paid provenance.
		for _, o := range chosen {
			pc.Taps = append(pc.Taps, o.Obj)
		}
		pc.teamworkPaid = len(chosen) > 0
	case castAnswerConspire:
		// CR 702.78a: the two chosen creatures are the tap the conspired cast
		// pays. They settle through pc.taps (payCast taps them) and
		// conspirePaid records that the provenance flag is owed. A Min==Max==2
		// KChoose, so a well-formed answer is exactly two options.
		for _, o := range chosen {
			pc.Taps = append(pc.Taps, o.Obj)
		}
		if len(chosen) >= 2 {
			pc.conspirePaid = true
		}
	case castAnswerExile:
		for _, o := range chosen {
			pc.delve = append(pc.delve, o.Obj)
		}
	case castAnswerAltaddcost:
		// The either-or additional cost (AlternateAdditionalCost): the chosen
		// part's cost folds into pc.cost (Plus), so the ordinary stages settle
		// it and payCast charges it. The chosen part rides on Option.Amount
		// (the index into pc.altAddParts), not the option Index, for the same
		// reason xAsk's value does.
		if len(chosen) > 0 && chosen[0].Amount >= 0 && int(chosen[0].Amount) < len(pc.altAddParts) {
			pc.cost = pc.cost.Plus(ParseCost(pc.altAddParts[chosen[0].Amount]))
		}
	case castAnswerEvidence:
		// The CollectEvidence payment's chosen graveyard cards (alltargeted1).
		// The SETTLE validation (total mana value at least the resolved
		// amount, every card still the payer's) runs in evidenceAsk on the
		// continueCast re-entry, which re-poses the ask when the answer falls
		// short -- so a malformed answer never pays a short evidence.
		for _, o := range chosen {
			pc.evidence = append(pc.evidence, o.Obj)
		}
	case castAnswerMoveToGraveCost:
		for _, o := range chosen {
			pc.moveGraves = append(pc.moveGraves, o.Obj)
		}
		pc.moveGravePart++
	case castAnswerReturncost:
		for _, o := range chosen {
			// K:Ninjutsu (CR 702.49b): the permanent the activated ability puts
			// onto the battlefield attacks the SAME defender the returned
			// creature was attacking. Capture that defender here, while the
			// chosen attacker is still a battlefield object (payCast moves it to
			// hand at settlement, which clears its Attacking field), and carry it
			// on the AbilityPush event so resolution can bind it.
			if e.activationIsNinjutsu(pc) {
				if o := e.G.Obj(o.Obj); o != nil {
					pc.ninjutsuDefender = o.Attacking
					pc.ninjutsuDefenderObject = o.AttackingBattle
					pc.ninjutsuHasDefender = true
				}
			}
			// K:Sneak (CR 702.190b): the permanent the sneak cast puts onto
			// the battlefield attacks the SAME defender the returned creature
			// was attacking. Capture it while the chosen attacker is still a
			// battlefield object; pushCast then folds it onto the spell's
			// Remembered so the entry hook can bind it.
			if pc.mode == "sneak" {
				if o := e.G.Obj(o.Obj); o != nil {
					pc.sneakDefender = o.Attacking
					pc.sneakDefenderObject = o.AttackingBattle
					pc.sneakHasDefender = true
				}
			}
			pc.returns = append(pc.returns, o.Obj)
		}
		pc.returnPart++
	case castAnswerPuttolibcost:
		for _, o := range chosen {
			pc.putToLibs = append(pc.putToLibs, o.Obj)
		}
		pc.putToLibPart++
	case castAnswerForageExile:
		pc.cost.Exile = append(pc.cost.Exile, CostPart{N: 3, Spec: "Card", Zone: state.ZGraveyard})
	case castAnswerForageFood:
		if len(chosen) > 0 {
			pc.Sacs = append(pc.Sacs, chosen[0].Obj)
		}
	case castAnswerActivate:
		// CR 601.2g: a source's mana abilities are distinct activations that
		// share its tap cost. activateManaPayment resolves a singleton
		// immediately or asks the caster to choose one before re-entering
		// this payment window -- the payment-window form, so an
		// InstantSpeed$ True mana ability (Lion's Eye Diamond, "Activate
		// only as an instant") is withheld: paying a cost is no priority
		// moment.
		if len(chosen) > 0 {
			e.activateManaPayment(pc.player, chosen[0].Obj, true)
		}
	case castAnswerMana:
		// The announced window's per-ability option (announce_pay.go).
		if pc.Announced && len(chosen) > 0 {
			e.announcedActivate(pc, chosen[0])
		}
	case castAnswerAutoFill:
		// Auto-fill: the planner's activations for what is still owed, run by
		// the ordinary plan executor on re-entry (spec §4.4). The plan is
		// re-derived at the unchanged state the ask priced.
		if pc.Announced {
			if plan := e.announcedAutoFillPlan(pc, e.castPaymentMana(pc)); plan != nil {
				pc.Payment = &plannedCastPayment{ActionID: decision.OptAutoFill, Plan: *plan}
				pc.PaymentNext = 0
				pc.PaymentFallback = nil
			}
		}
	case castAnswerUndoTap:
		if pc.Announced {
			if _, ok := e.undoableWindowTap(pc); ok {
				e.undoWindowTap(pc)
			}
		}
	case castAnswerCancelCast:
		if pc.Announced {
			e.cancelAnnouncedCast(pc)
		}
	}
}

type castAnswerCode uint16

const (
	castAnswerNamedAnnounce castAnswerCode = iota + 1
	castAnswerX
	castAnswerReplicate
	castAnswerMultikick
	castAnswerSquad
	castAnswerMutatePlace
	castAnswerCasualty
	castAnswerBargain
	castAnswerGiftDecline
	castAnswerGiftPromise
	castAnswerConspire
	castAnswerTeamwork
	castAnswerTeamworkMode
	castAnswerExile
	castAnswerAltaddcost
	castAnswerEvidence
	castAnswerMoveToGraveCost
	castAnswerReturncost
	castAnswerPuttolibcost
	castAnswerForageExile
	castAnswerForageFood
	castAnswerActivate
	castAnswerMana
	castAnswerAutoFill
	castAnswerUndoTap
	castAnswerCancelCast
)

var castAnswerCodes = state.NewStrCodes(
	state.StrEntry[castAnswerCode]{Key: "named_announce", Val: castAnswerNamedAnnounce},
	state.StrEntry[castAnswerCode]{Key: "x", Val: castAnswerX},
	state.StrEntry[castAnswerCode]{Key: "replicate", Val: castAnswerReplicate},
	state.StrEntry[castAnswerCode]{Key: "multikick", Val: castAnswerMultikick},
	state.StrEntry[castAnswerCode]{Key: "squad", Val: castAnswerSquad},
	state.StrEntry[castAnswerCode]{Key: "mutate_place", Val: castAnswerMutatePlace},
	state.StrEntry[castAnswerCode]{Key: "casualty", Val: castAnswerCasualty},
	state.StrEntry[castAnswerCode]{Key: "bargain", Val: castAnswerBargain},
	state.StrEntry[castAnswerCode]{Key: "gift_decline", Val: castAnswerGiftDecline},
	state.StrEntry[castAnswerCode]{Key: "gift_promise", Val: castAnswerGiftPromise},
	state.StrEntry[castAnswerCode]{Key: "conspire", Val: castAnswerConspire},
	state.StrEntry[castAnswerCode]{Key: "teamwork", Val: castAnswerTeamwork},
	state.StrEntry[castAnswerCode]{Key: "teamworked", Val: castAnswerTeamworkMode},
	state.StrEntry[castAnswerCode]{Key: "exile", Val: castAnswerExile},
	state.StrEntry[castAnswerCode]{Key: "altaddcost", Val: castAnswerAltaddcost},
	state.StrEntry[castAnswerCode]{Key: "evidence", Val: castAnswerEvidence},
	state.StrEntry[castAnswerCode]{Key: "movetogravecost", Val: castAnswerMoveToGraveCost},
	state.StrEntry[castAnswerCode]{Key: "returncost", Val: castAnswerReturncost},
	state.StrEntry[castAnswerCode]{Key: "puttolibcost", Val: castAnswerPuttolibcost},
	state.StrEntry[castAnswerCode]{Key: "forage_exile", Val: castAnswerForageExile},
	state.StrEntry[castAnswerCode]{Key: "forage_food", Val: castAnswerForageFood},
	state.StrEntry[castAnswerCode]{Key: "activate", Val: castAnswerActivate},
	state.StrEntry[castAnswerCode]{Key: "mana", Val: castAnswerMana},
	state.StrEntry[castAnswerCode]{Key: decision.OptAutoFill, Val: castAnswerAutoFill},
	state.StrEntry[castAnswerCode]{Key: decision.OptUndoTap, Val: castAnswerUndoTap},
	state.StrEntry[castAnswerCode]{Key: decision.OptCancelCast, Val: castAnswerCancelCast},
)
