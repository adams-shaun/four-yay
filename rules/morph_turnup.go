package rules

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// CR 708.6 / CR 116.2b: any time its controller has priority, a face-down
// permanent that was cast with Morph (CR 702.37a), Megamorph (CR 702.168a)
// or Disguise (CR 702.169a) may be turned face up. Turning it face up is a
// SPECIAL ACTION -- it does not use the stack and cannot be responded to --
// whose only cost is the keyword's own parameter (the {3} cast is the
// face-down one; the colon parameter is the turn-up one). Megamorph's rider
// adds a +1/+1 counter as the permanent turns face up.
//
// A manifested card (Choices$ Manifest) and a cloaked card have their own
// turn-up rules and carry none of the three family flags, so morphFaceUpCost
// reports false for them and this action is never offered: their turn-up is
// a separate subsystem. The cast side (rules/cast.go's modeFlags and
// moveResolvedOffStack) is the provenance source -- the family flag survives
// onto the battlefield permanent, and the printed face (which is retained on
// the object even while face down, CR 708.8 only hides it from the RULES)
// carries the cost.
//
// The turn-up cost is the FULL keyword parameter: mana, life, and the
// non-mana components the corpus prints (Reveal, Sac, Discard, Return, and an
// announced {X}). The offer and the payment share one payability predicate
// (morphTurnUpPayable) so an option is never offered whose payment would
// abort, and the non-mana parts are driven through a self-contained flow
// (turnUpPay) that reuses the cast/activation candidate primitives rather
// than pendingCast's push/pay machinery: a special action mints no stack
// object.
func init() {
	// kw:Morph / kw:Megamorph / kw:Disguise are implemented rules-side (the
	// cast side and this turn-up special action), so they register as
	// non-API keywords.
	effects.RegisterNonAPI("kw:Morph", "kw:Megamorph", "kw:Disguise")
}

type morphFaceUp struct {
	cost Cost
	// family is the printed keyword head ("Morph", "Megamorph", "Disguise"),
	// used only for the offer label and diagnostics.
	family string
	// megamorph is true when the turn-up must also put a +1/+1 counter.
	megamorph bool
}

// scope is the special-action cost scope the turn-up is priced under, so the
// CR 601.2f modifiers a ValidSpell$ Static.MorphUp / Static.isTurnFaceUp
// static names reach it: Morph and Megamorph turn up under "morphup" (Forge's
// abilityTurnFaceUp stamps MorphUp$ on both), Disguise under "disguiseup".
func (mf morphFaceUp) scope() costScope {
	if mf.family == "Disguise" {
		return specialActionScope("disguiseup")
	}
	return specialActionScope("morphup")
}

// morphTurnUpMods composes the cost modifiers that apply to id's turn-up
// action right now. A modifier that adds a non-mana cost part (a RaiseCost
// Cost$ extra) has no ask in this self-contained flow, so ok=false withholds
// the action rather than silently waiving that part.
func (e *Engine) morphTurnUpMods(p state.PlayerID, id state.ObjID, mf morphFaceUp) (costMods, bool) {
	mods := e.costModifiers(p, id, mf.scope())
	if mods.HasExtra {
		return costMods{}, false
	}
	return mods, true
}

// morphFaceUpCost resolves the turn-face-up action a face-down battlefield
// permanent offers, or ok=false when it offers none: not face down, not a
// creature the morph family put down (no family flag), or a face that
// carries no keyword parameter to price the action.
func morphFaceUpCost(o *state.Object) (morphFaceUp, bool) {
	if o == nil || o.Zone != state.ZBattlefield || !o.FaceDown {
		return morphFaceUp{}, false
	}
	fam := o.CastFlags & (state.FlagMorphed | state.FlagMegamorphed | state.FlagDisguised)
	if fam == 0 {
		return morphFaceUp{}, false
	}
	f := o.Face()
	if f == nil {
		return morphFaceUp{}, false
	}
	var head string
	switch {
	case fam&state.FlagMorphed != 0:
		head = "Morph"
	case fam&state.FlagMegamorphed != 0:
		head = "Megamorph"
	case fam&state.FlagDisguised != 0:
		head = "Disguise"
	}
	raw, ok := f.KeywordParam(head)
	if !ok || strings.TrimSpace(raw) == "" {
		return morphFaceUp{}, false
	}
	c := ParseCost(raw)
	// A cost token the parser could not model (an unknown symbol) is charged
	// one phantom generic by ParseCost AND reported through Cost.Unknown; the
	// turn-up must fail closed rather than silently waive it.
	if len(c.Unknown) > 0 || !morphTurnUpCostSupported(c) {
		return morphFaceUp{}, false
	}
	// A variable-count part (Sac<X/Spec>, Discard<X/...>, ...) announces its
	// count through an X the turn-up flow does not pose (it is not the cost's
	// mana {X}); offering the action would pay ZERO objects of that part -- a
	// silent waiver. No morph-family carrier prints one at the pin; fail
	// closed rather than half-pay if one ever does.
	if morphTurnUpCountAnnounced(c) {
		return morphFaceUp{}, false
	}
	return morphFaceUp{
		cost:      c,
		family:    head,
		megamorph: fam&state.FlagMegamorphed != 0,
	}, true
}

// morphTurnUpCostSupported is a positive allowlist for the components this
// special-action flow actually settles. ParseCost models substantially more
// than this flow (for casts and activations); a parsed component is not proof
// it is paid. Keep the turn-up boundary closed whenever a new Cost field is
// introduced until its candidate, ask and event-backed settlement are added.
func morphTurnUpCostSupported(c Cost) bool {
	return !c.Tap && len(c.SubCounter) == 0 && len(c.AddCounter) == 0 &&
		len(c.Exile) == 0 && len(c.RevealOrChoose) == 0 && len(c.RevealChosen) == 0 &&
		len(c.Behold) == 0 && len(c.TapPermanent) == 0 && len(c.Blight) == 0 &&
		len(c.Exert) == 0 && !c.Forage && len(c.Draw) == 0 && len(c.Energy) == 0 &&
		len(c.LifeX) == 0 && !c.LifeHalfUp && len(c.DamageYou) == 0 &&
		len(c.GainLife) == 0 &&
		len(c.PutToLib) == 0 && len(c.MoveToGrave) == 0 && len(c.Mill) == 0 &&
		len(c.Evidence) == 0 && len(c.RollDice) == 0
}

// morphTurnUpCountAnnounced reports whether any non-mana count part of cost
// is the variable (Announced) form -- Sac<X/Spec>, Discard<X/...>,
// Reveal<X/...>, Return<X/...>, or the announced ExileFromGrave<X/...>. The
// turn-up flow poses only the cost's mana {X}; an announced count has no ask,
// so such a part would settle zero objects. morphFaceUpCost fails closed on
// any of them.
func morphTurnUpCountAnnounced(c Cost) bool {
	for _, parts := range [][]CostPart{c.Sac, c.Discard, c.Reveal, c.Return, c.Exile} {
		for _, part := range parts {
			if part.Announced {
				return true
			}
		}
	}
	return false
}

// morphTurnUpPayable is the ONE payability predicate the offer and the
// action share: a face-down morph-family permanent's turn-up cost is payable
// when every non-mana component has eligible candidates (nonManaCastable,
// the same totality gate the cast/activation offer uses) AND some announced X
// (including X=0) leaves the mana/life remainder (costPayable) payable. A
// cost with no {X} is priced once at X=0. A nonzero XMin floor is honored, and
// no payable X at all withholds the action -- never an offer whose payment
// would abort.
//
// mods are the turn-up's CR 601.2f cost modifiers (morphTurnUpMods), composed
// onto the cost AFTER the X announcement -- the same increase-then-reduce
// order the cast flow's charge takes -- so the offer, the X ask and the
// settlement price one total.
func (e *Engine) morphTurnUpPayable(p state.PlayerID, id state.ObjID, cost Cost, mods costMods) bool {
	if !e.nonManaCastable(p, id, cost, false, "") {
		return false
	}
	min := cost.XMin
	if min < 0 {
		min = 0
	}
	if cost.X == 0 {
		return pay.CostPayable(asPayer(e), p, id, false, mods.Apply(cost))
	}
	// Each extra X adds one generic, so the pool total is the finite ceiling
	// past which no further X can be paid (the same safe bound xAsk uses).
	// A reduction can only lower the price of a larger X, so the modifiers'
	// reduction total widens the ceiling rather than cutting it short.
	bound := e.G.Players[p].Pool.Total() + cost.Generic + int32(cost.X) + 1 + mods.ReduceTotal()
	for x := min; x <= bound; x++ {
		if pay.CostPayable(asPayer(e), p, id, false, mods.Apply(cost.WithX(x))) {
			return true
		}
	}
	return false
}

// morphTurnUpPayablePriced is morphTurnUpPayable for the offer walk's two
// pricing modes (legalActionsPriced): hyp nil is exactly morphTurnUpPayable
// (the floating pool, the offer the priority decision carries); hyp non-nil
// prices the mana/life remainder against the potential-action walk's
// hypothetical bound (PotentialMana) while the non-mana components are still
// checked against the REAL state, the same split castablePriced makes. An X
// cost is priced at its smallest legal announcement: each extra X only adds
// generic, so no larger X is payable when the smallest is not, and the
// hypothetical bound may be unbounded (no finite loop ceiling exists there).
func (e *Engine) morphTurnUpPayablePriced(p state.PlayerID, id state.ObjID, cost Cost, mods costMods, hyp *state.Mana) bool {
	if hyp == nil {
		return e.morphTurnUpPayable(p, id, cost, mods)
	}
	if !e.nonManaCastable(p, id, cost, false, "") {
		return false
	}
	if cost.X != 0 {
		cost = cost.WithX(max(cost.XMin, 0))
	}
	return pay.CostPayablePool(asPayer(e), p, id, false, mods.Apply(cost), *hyp, e.G.Players[p].ManaUnits())
}

// turnUpPay carries the CR 708.6 turn-face-up special action's payment
// state. It is a SELF-CONTAINED flow: turning face up neither casts a spell
// nor activates an ability, so it mints no stack object and never enters
// pendingCast's push/pay machinery. It still reuses the shared candidate
// primitives (sacrificeCostCandidates, discardCandidates, costCandidates,
// costPayable) so the objects it offers and the objects it pays come from
// the exact helpers the cast/activation gate uses.
type turnUpPay struct {
	player state.PlayerID
	card   state.ObjID
	cost   Cost
	// mods are the CR 601.2f cost modifiers composed onto cost (after the X
	// announcement) at every payability read and at settlement, captured
	// once when the action is taken -- the snapshot the offer priced.
	mods costMods
	// x is the announced value of the cost's {X}, folded into the payment at
	// settle time; xDone marks the announcement already answered.
	x     int32
	xDone bool
	// The chosen non-mana components, in the order they settle: sacrifice
	// (a MoveZone of the chosen permanents), discard (DiscardCost events),
	// reveal (a public Note) and return (ReturnCost moves to the owner's
	// hand).
	sacs    []state.ObjID
	discs   []state.ObjID
	reveal  []state.ObjID
	returns []state.ObjID
	settled bool
	sacNext int
}

// turnFaceUp is handlePriority's "turn_face_up" action. It creates the
// payment flow, which poses any needed non-mana/X choices, then settles the
// whole cost exactly once before emitting the CR 708.6 TurnFaceUp event
// (whose Apply clears the face-down marker and the folded CR 708.5 set) and,
// for a Megamorph cast, the +1/+1 counter. No stack object is minted: a
// special action resolves immediately (CR 116.2b). The offer gated on
// morphTurnUpPayable against this same floating pool, so the settlement
// cannot disagree with what was offered; a stale option (the permanent left
// play, was already turned face up, or the cost became unpayable) degrades to
// a no-op, and the inert backstop holds it out of the re-offer.
func (e *Engine) turnFaceUp(p state.PlayerID, opt decision.Option) {
	o := e.G.Obj(opt.Obj)
	mf, ok := morphFaceUpCost(o)
	if !ok || o.Controller != p {
		return
	}
	// CR 614.1a: a live CantHappen turn-up replacement makes the action
	// illegal even if a stale option named it.
	if e.turnFaceUpCantHappen(opt.Obj) {
		return
	}
	// The offer proved the cost payable; re-prove it here so a board that
	// changed under the option degrades to a no-op rather than paying a
	// partial cost (no partial payment followed by a no-op).
	mods, ok := e.morphTurnUpMods(p, opt.Obj, mf)
	if !ok || !e.morphTurnUpPayable(p, opt.Obj, mf.cost, mods) {
		return
	}
	e.emit(events.Event{Kind: events.Priority, Player: e.G.Priority, Amount: 0})
	e.turnUp = &turnUpPay{player: p, card: opt.Obj, cost: mf.cost, mods: mods}
	e.advanceTurnUp()
}

// advanceTurnUp drives the turn-up payment's asks in cost order -- the
// announced X first (CR 601.2b), then the non-mana components -- and settles
// the whole cost once every part is chosen. It is re-entered by
// handleChoose's chooseTurnUp arm after each answer; a decision it poses sets
// e.choosing and returns without settling.
func (e *Engine) advanceTurnUp() {
	tp := e.turnUp
	if tp == nil {
		return
	}
	if !tp.xDone && tp.cost.X > 0 {
		if e.turnUpXAsk(tp) {
			return
		}
	}
	if e.turnUpSacAsk(tp) {
		return
	}
	if e.turnUpDiscardAsk(tp) {
		return
	}
	if e.turnUpRevealAsk(tp) {
		return
	}
	if e.turnUpReturnAsk(tp) {
		return
	}
	e.settleTurnUp(tp)
}

// turnUpXAsk poses the announced-X choice for a turn-up cost carrying {X}.
// Every value from the cost's floor (XMin, default 0) up to the payable
// ceiling is offered, so X=0 is legal when the base cost is payable and an
// unpayable announcement can never be chosen. Option.Amount carries the
// value, the same field xAsk uses.
func (e *Engine) turnUpXAsk(tp *turnUpPay) bool {
	tp.xDone = true
	min := tp.cost.XMin
	if min < 0 {
		min = 0
	}
	var legal []int32
	bound := e.G.Players[tp.player].Pool.Total() + tp.cost.Generic + int32(tp.cost.X) + 1 + tp.mods.ReduceTotal()
	for x := min; x <= bound; x++ {
		if pay.CostPayable(asPayer(e), tp.player, tp.card, false, tp.mods.Apply(tp.cost.WithX(x))) {
			legal = append(legal, x)
		}
	}
	if len(legal) == 0 {
		// The offer gate would not have offered this; degrade rather than
		// pose a decision with no options (e.ask panic-guards that shape).
		e.turnUp = nil
		return true
	}
	d := &decision.Decision{Player: tp.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Choose a value for X", Source: tp.card}
	for _, x := range legal {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "x",
			Label: fmt.Sprintf("X = %d", x), Amount: int(x)})
	}
	e.choosing = chooseTurnUp
	e.ask(d)
	return true
}

// turnUpSacAsk poses one exact-N KChoose per Sac cost part, offering the same
// candidates sacrificeCostCandidates returns for the cast/activation gate (so
// "another creature"/Creature.Other excludes the face-down source itself) and
// paying from that list. A singleton part with exactly one candidate is
// recorded without an ask, mirroring sacAsk's no-choice shortcut.
func (e *Engine) turnUpSacAsk(tp *turnUpPay) bool {
	off := 0
	for _, part := range tp.cost.Sac {
		need := int(part.N)
		if part.Announced {
			// No morph-family carrier prints a Sac<X/...>; fail closed rather
			// than guess a count.
			need = 0
		}
		if off+need <= len(tp.sacs) {
			off += need
			continue
		}
		remaining := off + need - len(tp.sacs)
		paid := map[state.ObjID]bool{}
		for _, s := range tp.sacs {
			paid[s] = true
		}
		var cands []state.ObjID
		for _, oid := range pay.SacrificeCostCandidates(asPayer(e), tp.player, tp.card, part, false) {
			if !paid[oid] {
				cands = append(cands, oid)
			}
		}
		if remaining > len(cands) {
			// Unreachable from a well-formed offer; abort the whole payment
			// rather than half-pay.
			e.abortTurnUp(tp)
			return true
		}
		if remaining == 1 && len(cands) == 1 {
			tp.sacs = append(tp.sacs, cands[0])
			// Recorded without an ask: keep driving the remaining parts.
			return e.turnUpSacAsk(tp)
		}
		d := &decision.Decision{Player: tp.player, Kind: decision.KChoose, Min: remaining, Max: remaining,
			Prompt: "Sacrifice a permanent to turn " + e.targetName(tp.card) + " face up", Source: tp.card}
		for _, id := range cands {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "sacrifice",
				Obj: id, Label: e.targetName(id)})
		}
		e.choosing = chooseTurnUp
		e.ask(d)
		return true
	}
	return false
}

// turnUpDiscardAsk settles Discard cost parts from the payer's hand. A
// singleton part with one candidate is recorded without an ask; otherwise an
// exact-N KChoose is posed over the same candidates discardCandidates
// returns.
func (e *Engine) turnUpDiscardAsk(tp *turnUpPay) bool {
	off := 0
	for _, part := range tp.cost.Discard {
		need := int(part.N)
		if part.Announced {
			need = 0
		}
		if off+need <= len(tp.discs) {
			off += need
			continue
		}
		remaining := off + need - len(tp.discs)
		reserved := map[state.ObjID]bool{}
		for _, id := range tp.discs {
			reserved[id] = true
		}
		// The casting flag matches the offer gate exactly: morphTurnUpPayable
		// calls nonManaCastable(p, id, cost, false), whose discard check passes
		// casting=true. The face-down source is a battlefield permanent, never
		// a hand card, so the self-exclusion is inert here -- but sharing the
		// gate's value keeps the two reads one rule.
		cands := pay.DiscardCandidates(asPayer(e), tp.player, tp.card, part, true, reserved)
		if remaining > len(cands) {
			e.abortTurnUp(tp)
			return true
		}
		if remaining == 1 && len(cands) == 1 {
			tp.discs = append(tp.discs, cands[0])
			return e.turnUpDiscardAsk(tp)
		}
		d := &decision.Decision{Player: tp.player, Kind: decision.KChoose, Min: remaining, Max: remaining,
			Prompt: "Discard a card to turn " + e.targetName(tp.card) + " face up", Source: tp.card}
		for _, id := range cands {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "discard",
				Obj: id, Label: e.targetName(id)})
		}
		e.choosing = chooseTurnUp
		e.ask(d)
		return true
	}
	return false
}

// turnUpRevealAsk settles Reveal cost parts from the payer's hand. A part
// with exactly N candidates is recorded without an ask (the reveal is
// forced), mirroring revealCostAsk.
func (e *Engine) turnUpRevealAsk(tp *turnUpPay) bool {
	off := 0
	for _, part := range tp.cost.Reveal {
		need := int(part.N)
		if off+need <= len(tp.reveal) {
			off += need
			continue
		}
		remaining := off + need - len(tp.reveal)
		reserved := map[state.ObjID]bool{}
		for _, id := range tp.reveal {
			reserved[id] = true
		}
		cands := pay.CostCandidates(asPayer(e), tp.player, tp.card, state.ZHand, part.Spec, true, false)
		var avail []state.ObjID
		for _, id := range cands {
			if !reserved[id] {
				avail = append(avail, id)
			}
		}
		if remaining > len(avail) {
			e.abortTurnUp(tp)
			return true
		}
		if remaining == len(avail) {
			tp.reveal = append(tp.reveal, avail...)
			return e.turnUpRevealAsk(tp)
		}
		d := &decision.Decision{Player: tp.player, Kind: decision.KChoose, Min: remaining, Max: remaining,
			Prompt: "Reveal a card to turn " + e.targetName(tp.card) + " face up", Source: tp.card}
		for _, id := range avail {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "revealcost",
				Obj: id, Label: e.targetName(id)})
		}
		e.choosing = chooseTurnUp
		e.ask(d)
		return true
	}
	return false
}

// turnUpReturnAsk settles Return cost parts. A CARDNAME part names the source
// itself (the face-down permanent), so it is recorded without an ask; any
// other spec walks the payer's battlefield with costCandidates.
func (e *Engine) turnUpReturnAsk(tp *turnUpPay) bool {
	off := 0
	for _, part := range tp.cost.Return {
		need := int(part.N)
		if off+need <= len(tp.returns) {
			off += need
			continue
		}
		remaining := off + need - len(tp.returns)
		spec := pay.SacrificeMatchSpec(part.Spec)
		var cands []state.ObjID
		if strings.EqualFold(spec, "CARDNAME") {
			if o := e.G.Obj(tp.card); o != nil && o.Zone == state.ZBattlefield {
				cands = append(cands, tp.card)
			}
		} else {
			reserved := map[state.ObjID]bool{}
			for _, id := range tp.returns {
				reserved[id] = true
			}
			for _, id := range pay.CostCandidates(asPayer(e), tp.player, tp.card, state.ZBattlefield, spec, false, false) {
				if !reserved[id] {
					cands = append(cands, id)
				}
			}
		}
		if remaining > len(cands) {
			e.abortTurnUp(tp)
			return true
		}
		if remaining == 1 && len(cands) == 1 {
			tp.returns = append(tp.returns, cands[0])
			return e.turnUpReturnAsk(tp)
		}
		d := &decision.Decision{Player: tp.player, Kind: decision.KChoose, Min: remaining, Max: remaining,
			Prompt: "Return a permanent to its owner's hand to turn " + e.targetName(tp.card) + " face up", Source: tp.card}
		for _, id := range cands {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "returncost",
				Obj: id, Label: e.targetName(id)})
		}
		e.choosing = chooseTurnUp
		e.ask(d)
		return true
	}
	return false
}

// turnUpAnswer records one KChoose answer for the active turn-up payment and
// re-drives the flow. It dispatches on the option kind, the same shared
// identifier the cast flow's castAnswer uses, so an answer the engine routes
// differently can never be constructed by a bot arm.
func (e *Engine) turnUpAnswer(d *decision.Decision, chosen []decision.Option) {
	tp := e.turnUp
	if tp == nil || len(chosen) == 0 {
		return
	}
	switch turnUpAnswerCodes.Code(string(chosen[0].Kind)) {
	case turnUpAnswerX:
		tp.x = int32(chosen[0].Amount)
	case turnUpAnswerSacrifice:
		for _, o := range chosen {
			tp.sacs = append(tp.sacs, o.Obj)
		}
	case turnUpAnswerDiscard:
		for _, o := range chosen {
			tp.discs = append(tp.discs, o.Obj)
		}
	case turnUpAnswerRevealcost:
		for _, o := range chosen {
			tp.reveal = append(tp.reveal, o.Obj)
		}
	case turnUpAnswerReturncost:
		for _, o := range chosen {
			tp.returns = append(tp.returns, o.Obj)
		}
	default:
		return
	}
	e.advanceTurnUp()
}

// settleTurnUp pays the whole turn-up cost exactly once and emits the
// TurnFaceUp event. Every chosen object is re-verified against its cost part
// (turnUpChoicesValid) before anything moves, so an object invalidated
// between the ask that named it and this settle -- moved, destroyed,
// re-controlled, or claimed by another part -- aborts the whole payment with
// the board untouched. Only then do the non-mana parts settle, mana/life pay
// last, and on success does the TurnFaceUp event flip the permanent.
func (e *Engine) settleTurnUp(tp *turnUpPay) {
	if e.choosing == chooseTurnUp {
		e.choosing = chooseNone
	}
	o := e.G.Obj(tp.card)
	if o == nil || o.Zone != state.ZBattlefield || !o.FaceDown {
		// The source is gone (or already face up): there is nothing left to
		// turn up. Drop the flow so no later resume re-enters it.
		e.dropTurnUp(tp)
		return
	}
	// A cost carrying {X} must have been announced; a missing announcement
	// (only reachable from a hand-built decision) fails closed.
	if tp.cost.X > 0 && !tp.xDone {
		e.dropTurnUp(tp)
		return
	}
	paidCost := tp.cost
	if paidCost.X > 0 {
		paidCost = paidCost.WithX(tp.x)
	}
	paidCost = tp.mods.Apply(paidCost)
	if !tp.settled {
		// Revalidate EVERY saved cost object and the mana/life remainder before
		// anything moves. Once a replacement answer suspends payment, the
		// already-paid choices are intentionally not revalidated against the
		// post-payment board.
		if !e.turnUpChoicesValid(tp) {
			e.abortTurnUp(tp)
			return
		}
		if !pay.CostPayable(asPayer(e), tp.player, tp.card, false, paidCost) {
			e.abortTurnUp(tp)
			return
		}
		// Pay mana/life and the non-sacrifice components once. Any cost event
		// below may park on a decision (a CR 903.9 commander-zone choice, a
		// CR 616.1 replacement order choice, a madness choice); the
		// continuation then resumes from resumeTurnUpAfterCost without
		// charging any of these components again.
		if !pay.PayMana(asPayer(e), tp.player, paidCost) {
			e.abortTurnUp(tp)
			return
		}
		if len(tp.discs) > 0 {
			e.payDiscardCost(tp.discs, "")
		}
		for _, id := range tp.returns {
			if o := e.G.Obj(id); o != nil {
				e.emit(events.ReturnCost(id, o.Zone))
			}
		}
		if len(tp.reveal) > 0 {
			names := make([]string, 0, len(tp.reveal))
			for _, id := range tp.reveal {
				names = append(names, e.targetName(id))
			}
			e.emit(events.Event{Kind: events.Note, Player: tp.player, Obj: tp.card,
				IDs:  append([]state.ObjID(nil), tp.reveal...),
				Text: "revealed " + strings.Join(names, ", ") + " as a cost"})
		}
		tp.settled = true
		if !e.turnUpCostIdle() {
			// A discard or return parked its move on a decision: wait for it
			// before the sacrifices, so the cost events settle in cost order.
			return
		}
	}
	for tp.sacNext < len(tp.sacs) {
		id := tp.sacs[tp.sacNext]
		tp.sacNext++ // counted before the emit: the event may park on a decision
		e.emit(events.Sacrifice(id))
		if !e.turnUpCostIdle() {
			return
		}
	}
	e.finishTurnUp(tp)
}

// turnUpCostIdle reports whether no decision a turn-up cost event can park
// on is outstanding or queued: nothing pending, no parked CR 903.9
// commander-zone move, no queued CR 616.1 replacement order choice or madness
// choice, no ask deferred behind one of those, and no replacement body
// suspended mid-resolution. It is the ONE predicate the settle loop, the
// finish and the Submit-time resume share, so a new parking decision class
// is covered by adding it here rather than by a per-handler resume call.
func (e *Engine) turnUpCostIdle() bool {
	return e.pending == nil && len(e.cmdZone) == 0 && len(e.replChoices) == 0 &&
		len(e.madnessChoices) == 0 && len(e.deferredAsks) == 0 && !e.Suspended()
}

// resumeTurnUpAfterCost is the single continuation point of a turn-up whose
// cost events parked on a decision. Submit calls it after every answered
// decision, once its own drains (deferred asks, queued replacement choices)
// have run, so the payment resumes whatever kind of decision the cost event
// parked on -- a commander-zone choice, a replacement order choice, a
// madness choice, or an ask posed from inside a replacement body -- and only
// once all of them have landed. Before the cost is settled the flow is
// driven by its own KChoose asks (turnUpAnswer), never from here.
func (e *Engine) resumeTurnUpAfterCost() {
	if tp := e.turnUp; tp != nil && tp.settled && e.turnUpCostIdle() {
		e.settleTurnUp(tp)
	}
}

// dropTurnUp silently clears a turn-up flow that has nothing left to do.
func (e *Engine) dropTurnUp(tp *turnUpPay) {
	if e.turnUp == tp {
		e.turnUp = nil
	}
}

// finishTurnUp closes a paid special action only after every decision
// caused by its cost events has resolved.
func (e *Engine) finishTurnUp(tp *turnUpPay) {
	if e.turnUp != tp || !e.turnUpCostIdle() {
		return
	}
	e.turnUp = nil
	// Read the megamorph rider BEFORE the TurnFaceUp event: its Apply leaves
	// CastFlags untouched, but the read belongs to the cast provenance the
	// action is priced from, so it is taken once here.
	megamorph := false
	if mf, ok := morphFaceUpCost(e.G.Obj(tp.card)); ok {
		megamorph = mf.megamorph
	}
	// The TurnFaceUp event is also the CR 107.3m trigger binding point for
	// an X paid as this permanent turns face up (Bane of the Living's
	// Count$xPaid trigger). Amount is already part of the event encoding; the
	// event kind disambiguates this payload from other Amount meanings.
	turnUpX := int32(0)
	if tp.cost.X > 0 {
		turnUpX = tp.x
	}
	e.emit(events.Event{Kind: events.TurnFaceUp, Obj: tp.card, Amount: turnUpX})
	if megamorph {
		e.emit(events.Event{Kind: events.CounterChange, Obj: tp.card, Counter: "P1P1", Amount: 1})
	}
	// The special action is settled: asks after it (triggers, the next
	// priority) are ordinary (turnup_tape.go).
	e.tape.ResolutionDone()
}

// turnUpChoicesValid re-derives every saved cost-object choice against the
// SAME candidate helpers the offer and the asks used, immediately before
// settlement. The saved IDs are held across several asks (the announced X
// first, then each non-mana part), and a replacement or trigger in between
// can move, destroy, bounce or re-control one of them -- or a part's earlier
// payment can claim an object a later part also named. Settling a saved ID
// blind would charge a cost the board no longer offers and still flip the
// source; worse, a battlefield Return part would move an object from
// whatever zone it drifted into. So each part's saved slice is re-checked
// against that part's live candidates (same zone, same spec, same payer
// control, source exclusion and reservation as the ask), every saved ID must
// be distinct, and the saved count must equal the printed count. Any miss is
// false and the caller aborts with nothing moved.
func (e *Engine) turnUpChoicesValid(tp *turnUpPay) bool {
	used := map[state.ObjID]bool{}
	claim := func(ids []state.ObjID) bool {
		for _, id := range ids {
			if used[id] {
				return false
			}
			used[id] = true
		}
		return true
	}

	// Sacrifice: a part's saved permanents must all still be battlefield
	// permanents of the payer's that sacrificeCostCandidates returns for that
	// exact part (which carries the source exclusion and CantSacrifice block).
	sacOff := 0
	for _, part := range tp.cost.Sac {
		need := int(part.N)
		if part.Announced {
			need = 0
		}
		if sacOff+need > len(tp.sacs) {
			return false
		}
		saved := tp.sacs[sacOff : sacOff+need]
		sacOff += need
		if !claim(saved) {
			return false
		}
		cands := pay.SacrificeCostCandidates(asPayer(e), tp.player, tp.card, part, false)
		for _, id := range saved {
			if !turnUpContainsObj(cands, id) {
				return false
			}
		}
	}
	if sacOff != len(tp.sacs) {
		return false
	}

	// Discard: each saved card must still be a hand card of the payer's that
	// discardCandidates returns for that part, excluding ids an earlier part
	// (and the earlier saved ids of THIS walk) already claimed.
	discOff := 0
	reserved := map[state.ObjID]bool{}
	for _, part := range tp.cost.Discard {
		need := int(part.N)
		if part.Announced {
			need = 0
		}
		if discOff+need > len(tp.discs) {
			return false
		}
		saved := tp.discs[discOff : discOff+need]
		discOff += need
		if !claim(saved) {
			return false
		}
		cands := pay.DiscardCandidates(asPayer(e), tp.player, tp.card, part, true, reserved)
		for _, id := range saved {
			if !turnUpContainsObj(cands, id) {
				return false
			}
			reserved[id] = true
		}
	}
	if discOff != len(tp.discs) {
		return false
	}

	// Reveal: each saved card must still be a hand card of the payer's that
	// costCandidates returns for that part's hand scan.
	revOff := 0
	for _, part := range tp.cost.Reveal {
		need := int(part.N)
		if revOff+need > len(tp.reveal) {
			return false
		}
		saved := tp.reveal[revOff : revOff+need]
		revOff += need
		if !claim(saved) {
			return false
		}
		cands := pay.CostCandidates(asPayer(e), tp.player, tp.card, state.ZHand, part.Spec, true, false)
		for _, id := range saved {
			if !turnUpContainsObj(cands, id) {
				return false
			}
		}
	}
	if revOff != len(tp.reveal) {
		return false
	}

	// Return: a CARDNAME part names the source (recorded without an ask); any
	// other part walks the payer's battlefield. Either way the saved permanent
	// must still be where the part reads it -- the source itself, or a live
	// battlefield permanent costCandidates still returns.
	retOff := 0
	for _, part := range tp.cost.Return {
		need := int(part.N)
		if retOff+need > len(tp.returns) {
			return false
		}
		saved := tp.returns[retOff : retOff+need]
		retOff += need
		if !claim(saved) {
			return false
		}
		spec := pay.SacrificeMatchSpec(part.Spec)
		var cands []state.ObjID
		if strings.EqualFold(spec, "CARDNAME") {
			if o := e.G.Obj(tp.card); o != nil && o.Zone == state.ZBattlefield {
				cands = append(cands, tp.card)
			}
		} else {
			cands = pay.CostCandidates(asPayer(e), tp.player, tp.card, state.ZBattlefield, spec, false, false)
		}
		for _, id := range saved {
			if !turnUpContainsObj(cands, id) {
				return false
			}
		}
	}
	if retOff != len(tp.returns) {
		return false
	}
	return true
}

// turnUpContainsObj reports whether id is in ids; a small linear scan over the
// candidate lists (battlefield/hand sized) keeps the revalidation free of a
// per-call map allocation.
func turnUpContainsObj(ids []state.ObjID, id state.ObjID) bool {
	for _, oid := range ids {
		if oid == id {
			return true
		}
	}
	return false
}

// abortTurnUp drops an unpayable mid-flow turn-up without moving anything.
// The inert backstop then holds the option out of the rest of the priority
// window (rules/priority_guard.go), exactly as an unpayable cast abort does.
func (e *Engine) abortTurnUp(tp *turnUpPay) {
	e.turnUp = nil
	if e.choosing == chooseTurnUp {
		e.choosing = chooseNone
	}
	e.emit(events.Event{Kind: events.Note, Player: tp.player, Obj: tp.card,
		Text: "turn-face-up cost no longer payable; the special action did nothing"})
}

type turnUpAnswerCode uint16

const (
	turnUpAnswerX turnUpAnswerCode = iota + 1
	turnUpAnswerSacrifice
	turnUpAnswerDiscard
	turnUpAnswerRevealcost
	turnUpAnswerReturncost
)

var turnUpAnswerCodes = state.NewStrCodes(
	state.StrEntry[turnUpAnswerCode]{Key: "x", Val: turnUpAnswerX},
	state.StrEntry[turnUpAnswerCode]{Key: "sacrifice", Val: turnUpAnswerSacrifice},
	state.StrEntry[turnUpAnswerCode]{Key: "discard", Val: turnUpAnswerDiscard},
	state.StrEntry[turnUpAnswerCode]{Key: "revealcost", Val: turnUpAnswerRevealcost},
	state.StrEntry[turnUpAnswerCode]{Key: "returncost", Val: turnUpAnswerReturncost},
)
