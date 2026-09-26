package rules

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
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
	if len(c.Unknown) > 0 {
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
func (e *Engine) morphTurnUpPayable(p state.PlayerID, id state.ObjID, cost Cost) bool {
	if !e.nonManaCastable(p, id, cost, false) {
		return false
	}
	min := cost.XMin
	if min < 0 {
		min = 0
	}
	if cost.X == 0 {
		return e.costPayable(p, id, false, cost)
	}
	// Each extra X adds one generic, so the pool total is the finite ceiling
	// past which no further X can be paid (the same safe bound xAsk uses).
	bound := e.G.Players[p].Pool.Total() + cost.Generic + int32(cost.X) + 1
	for x := min; x <= bound; x++ {
		if e.costPayable(p, id, false, cost.WithX(x)) {
			return true
		}
	}
	return false
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
	if !e.morphTurnUpPayable(p, opt.Obj, mf.cost) {
		return
	}
	e.emit(events.Event{Kind: events.Priority, Player: e.G.Priority, Amount: 0})
	e.turnUp = &turnUpPay{player: p, card: opt.Obj, cost: mf.cost}
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
	bound := e.G.Players[tp.player].Pool.Total() + tp.cost.Generic + int32(tp.cost.X) + 1
	for x := min; x <= bound; x++ {
		if e.costPayable(tp.player, tp.card, false, tp.cost.WithX(x)) {
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
		for _, oid := range e.sacrificeCostCandidates(tp.player, tp.card, part, false) {
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
		cands := e.discardCandidates(tp.player, tp.card, part, true, reserved)
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
		cands := e.costCandidates(tp.player, tp.card, state.ZHand, part.Spec, true, false)
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
		spec := sacrificeMatchSpec(part.Spec)
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
			for _, id := range e.costCandidates(tp.player, tp.card, state.ZBattlefield, spec, false, false) {
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
	switch chosen[0].Kind {
	case "x":
		tp.x = int32(chosen[0].Amount)
	case "sacrifice":
		for _, o := range chosen {
			tp.sacs = append(tp.sacs, o.Obj)
		}
	case "discard":
		for _, o := range chosen {
			tp.discs = append(tp.discs, o.Obj)
		}
	case "revealcost":
		for _, o := range chosen {
			tp.reveal = append(tp.reveal, o.Obj)
		}
	case "returncost":
		for _, o := range chosen {
			tp.returns = append(tp.returns, o.Obj)
		}
	default:
		return
	}
	e.advanceTurnUp()
}

// settleTurnUp pays the whole turn-up cost exactly once and emits the
// TurnFaceUp event. Every chosen object is verified still available before
// anything moves (an abort leaves the board untouched); then the non-mana
// parts settle, mana/life pays last, and only on success does the
// TurnFaceUp event flip the permanent.
func (e *Engine) settleTurnUp(tp *turnUpPay) {
	e.turnUp = nil
	if e.choosing == chooseTurnUp {
		e.choosing = chooseNone
	}
	o := e.G.Obj(tp.card)
	if o == nil || o.Zone != state.ZBattlefield || !o.FaceDown {
		return
	}
	// A cost carrying {X} must have been announced; a missing announcement
	// (only reachable from a hand-built decision) fails closed.
	if tp.cost.X > 0 && !tp.xDone {
		return
	}
	paidCost := tp.cost
	if paidCost.X > 0 {
		paidCost = paidCost.WithX(tp.x)
	}
	// The mana/life remainder must still be payable against the live board
	// before ANY object moves, so a shortfall cannot half-pay.
	if !e.costPayable(tp.player, tp.card, false, paidCost) {
		return
	}
	// Pay the mana and life part (payMana charges the fixed Life component).
	if !e.payMana(tp.player, paidCost) {
		return
	}
	// Non-mana settlement, one event per paid object. These run after the
	// mana so a mana shortfall never moves an object; every candidate was
	// verified by morphTurnUpPayable at offer time and the asks only ever
	// offered live candidates.
	if len(tp.discs) > 0 {
		e.payDiscardCost(tp.discs, "")
	}
	for _, id := range tp.returns {
		if o := e.G.Obj(id); o != nil {
			e.emit(events.ReturnCost(id, o.Zone))
		}
	}
	for _, id := range tp.sacs {
		e.emit(events.Sacrifice(id))
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
	// Read the megamorph rider BEFORE the TurnFaceUp event: its Apply leaves
	// CastFlags untouched, but the read belongs to the cast provenance the
	// action is priced from, so it is taken once here.
	megamorph := false
	if mf, ok := morphFaceUpCost(e.G.Obj(tp.card)); ok {
		megamorph = mf.megamorph
	}
	e.emit(events.Event{Kind: events.TurnFaceUp, Obj: tp.card})
	if megamorph {
		e.emit(events.Event{Kind: events.CounterChange, Obj: tp.card, Counter: "P1P1", Amount: 1})
	}
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
