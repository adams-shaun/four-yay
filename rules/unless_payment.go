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

// unlessPayment is the continuation between accepting an UnlessCost$ and
// completing its non-mana Sac/Discard/Reveal components. Unlike ordinary
// activation costs, an unless cost is paid during a suspended resolution, so
// its choices must retain that resolution rather than silently taking the
// first card.
type unlessPayment struct {
	payer    state.PlayerID
	cost     Cost
	ctx      effects.Ctx
	stackObj state.ObjID
	// tape marks a payment the resolution kernel drives in line
	// (tapeUnlessComponents): its asks are served from the tape and its
	// settlement lands in the asking walk's live Ctx. A payment that is not
	// tape-driven belongs to an activated mana ability, whose continuation
	// remains in manaUnlessActivation.
	tape     bool
	part     int
	sacs     []state.ObjID
	discards []state.ObjID
	// reveals holds the Reveal<N/Spec> picks (the hideaway-family ETB
	// lands, Xyru Specter's Challenge): unlike a discard the revealed cards
	// STAY in hand, so the dedup must be explicit — one card must not pay
	// two parts — and the settled picks are announced with one public Note
	// (the same event the cast flow's pay.EmitChoiceCosts emits).
	reveals []state.ObjID
	// beholds holds the Behold<N/Spec> picks (CR 702.176, Elven Passage's
	// "you may behold an Elf"): an object the payer controls on the
	// battlefield or a card revealed from their hand. The chosen object is
	// not moved (a plain Behold, unlike BeholdExile), so like a reveal it
	// needs the explicit dedup and one public Note.
	beholds []state.ObjID
	// returns holds the Return<N/Spec> picks (the cumulative-upkeep family's
	// "return a non-Lair land"): a permanent matching the spec returned to
	// its OWNER's hand (Forge CostReturn.moveToHand), one choice-bearing part
	// exactly like a sacrifice.
	returns []state.ObjID
	// exiles holds the Exile<N/Spec> picks (the Grip of Amnesia family's
	// "exile all cards from their graveyard"): cards exiled from the payer's
	// own hand or graveyard as the cost. A whole-zone part
	// (isWholeZoneExileSpec) takes EVERY candidate without asking; an
	// ordinary part is a choice exactly like a sacrifice.
	exiles []state.ObjID
}

// UnlessCostPayable is the rules-side offer gate for the generic unless
// election (the ctx-less form used by tests and any mana-only caller).
func (e *Engine) UnlessCostPayable(p state.PlayerID, raw string) bool {
	return pay.UnlessCostPayable(asPayer(e), p, raw, nil, 0)
}

// UnlessCostPayableFromCtx is the full-context offer gate the effects ask
// uses (poseUnlessAsk): it hands the gate the very resolution context, so
// source- and role-dependent components (RevealChosen's secret designation,
// a SubCounter drain, and a Draw<.../Player.targetedBy> or TriggeredPlayer
// drawer) are evaluated against the same bindings the pay path will use. It
// is the same gate the resolution arm runs, so the option list and the pay
// decision cannot disagree.
func (e *Engine) UnlessCostPayableFromCtx(p state.PlayerID, raw string, ctx *effects.Ctx) bool {
	var stackObj state.ObjID
	if ctx != nil {
		stackObj = ctx.Source
	}
	return pay.UnlessCostPayable(asPayer(e), p, raw, ctx, stackObj)
}

// beginUnlessPayment begins a payer-selected payment. It owns all Sac,
// Discard and Reveal components, including the exact-candidate no-ask cases,
// so neither this path nor a future sibling can fall back to a
// first-in-zone-order pick.
func (e *Engine) beginUnlessPayment(payer state.PlayerID, cost Cost, ctx *effects.Ctx, stackObj state.ObjID) {
	// Fold the dynamic life tokens once, at continuation start: the offer
	// gate folded the same amounts against the same payer read, and the
	// charge below prices exactly this folded cost.
	cost, ok := pay.UnlessFoldDynamic(asPayer(e), payer, cost, ctx)
	if !ok {
		e.unlessPayment = &unlessPayment{payer: payer, ctx: pay.CloneUnlessCtx(*ctx), stackObj: stackObj}
		e.finishUnlessPayment(false)
		return
	}
	e.unlessPayment = &unlessPayment{payer: payer, cost: cost, ctx: pay.CloneUnlessCtx(*ctx), stackObj: stackObj}
	e.advanceUnlessPayment()
}

// paymentPartCount is the flat count of the choice-bearing components
// (Sac, Discard, Reveal, Behold, Return, Exile) the continuation walks before
// it settles the synchronous ones.
func (u *unlessPayment) paymentPartCount() int {
	return len(u.cost.Sac) + len(u.cost.Discard) + len(u.cost.Reveal) + len(u.cost.Behold) + len(u.cost.Return) + len(u.cost.Exile)
}

func (e *Engine) advanceUnlessPayment() {
	u := e.unlessPayment
	if u == nil {
		return
	}
	if int(u.payer) < 0 || int(u.payer) >= len(e.G.Players) ||
		!e.unlessCountersAffordable(u) ||
		!u.revealChosenDesignated(e) {
		e.finishUnlessPayment(false)
		return
	}
	// A mana component the pool cannot yet cover opens the one-source-at-a-
	// time CR 601.2g window (the ward/attack-prop discipline) rather than
	// declining: the payer taps until the pool covers the charge, then the
	// ordinary path below pays it. The window is only opened while a source
	// remains that the budget counted.
	d := paymentDescriptor{ID: u.stackObj, Class: paymentOther, Cost: &u.cost}
	// Open the window only when the POOL ALONE cannot pay (lifeGrant false:
	// a {B} pip K'rrik's grant could settle with 2 life must not close it)
	// AND a window source remains. The ordinary grant-bearing check below is
	// retained: when no source can help (or the payer answers Done with the
	// pool already covering the charge) the granted life still pays it.
	if !pay.CostPayableClassLife(asPayer(e), u.payer, d, pipRider{}, u.cost, false) &&
		u.cost.HasManaPayment() && len(e.windowManaUnits(u.payer)) > 0 {
		e.askUnlessMana()
		return
	}
	if !pay.CostPayableClass(asPayer(e), u.payer, d, pipRider{}, u.cost) {
		e.finishUnlessPayment(false)
		return
	}
	for u.part < u.paymentPartCount() {
		part, zone, kind := pay.UnlessPartAt(u.cost, u.part)
		eligible := e.unlessPaymentCandidates(u, zone, kind, part)
		if int32(len(eligible)) < part.N {
			e.finishUnlessPayment(false)
			return
		}
		if kind == "exilecost" && isWholeZoneExileSpec(part.Spec) {
			// ExileFromGrave<1/All> names the WHOLE zone: every matching
			// candidate is the payment, never a pick (the same
			// isWholeZoneExileSpec reading the cast gate and the
			// triggered-cost window take). The count check above still
			// demands part.N cards, so an empty zone declines.
			e.recordUnlessPaymentPick(u, kind, eligible)
			u.part++
			continue
		}
		if int32(len(eligible)) == part.N {
			e.recordUnlessPaymentPick(u, kind, eligible)
			u.part++
			continue
		}
		prompt := fmt.Sprintf("Choose %d card(s) to %s to pay the cost", part.N, kind)
		if kind == "revealcost" {
			prompt = fmt.Sprintf("Choose %d card(s) to reveal to pay the cost", part.N)
		}
		if kind == "returncost" {
			prompt = fmt.Sprintf("Return %d permanent(s) to their owner's hand to pay the cost", part.N)
		}
		if kind == "beholdcost" {
			prompt = fmt.Sprintf("Choose %d permanent(s) you control or card(s) in your hand to behold to pay the cost", part.N)
		}
		if kind == "exilecost" {
			zoneName := "hand"
			if zone == state.ZGraveyard {
				zoneName = "graveyard"
			}
			prompt = fmt.Sprintf("Exile %d card(s) from your %s to pay the cost", part.N, zoneName)
		}
		d := &decision.Decision{Player: u.payer, Kind: decision.KChoose,
			Min: int(part.N), Max: int(part.N), Source: u.ctx.Source,
			ResumeKind: "unless_cost",
			Prompt:     prompt}
		for _, id := range eligible {
			label := "a card"
			if o := e.G.Obj(id); o != nil && o.Face() != nil {
				label = o.Face().Name
			}
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: kind,
				Label: label, Obj: id, Player: u.payer})
		}
		windowAsk(e, d, chooseUnlessCost)
		return
	}
	// Resolve every drawer before charging any component. A Draw<N/Spec> may
	// name a role that this suspended resolution did not retain; that is an
	// unpayable cost, not a reason to spend mana/life and then silently omit
	// the draw. Keeping the resolved players also makes the selected-cost path
	// use the same binding rules as payUnlessCost's no-choice path.
	drawers := make([][]state.PlayerID, len(u.cost.Draw))
	for i, part := range u.cost.Draw {
		players, ok := pay.UnlessDrawPlayers(&u.ctx, u.payer, part.Spec)
		if !ok {
			e.finishUnlessPayment(false)
			return
		}
		for _, p := range players {
			if int(p) < 0 || int(p) >= len(e.G.Players) {
				e.finishUnlessPayment(false)
				return
			}
		}
		drawers[i] = players
	}
	if !pay.PayManaConv(asPayer(e), u.payer, u.cost, asPayer(e).Conv(u.payer, u.stackObj, false)) { // guarded above; retain totality if state changes.
		e.finishUnlessPayment(false)
		return
	}
	// Energy parts (PayEnergy<N>/<X>): the same shared charging site the cast
	// flow uses, one PlayerCounterChange per part. The offer gate proved the
	// total affordable; a state change since the choices is still guarded by
	// the same read.
	if !pay.UnlessEnergyAffordable(asPayer(e), u.payer, u.cost, &u.ctx) {
		e.finishUnlessPayment(false)
		return
	}
	x := int32(0)
	if u.ctx.XAnnounced {
		x = u.ctx.X
	}
	pay.ChargeEnergyCost(asPayer(e), u.payer, u.cost, x)
	// The settled reveal picks are announced exactly like the cast flow's
	// pay.EmitChoiceCosts announces them: one public Note carrying the revealed
	// ids (the cards STAY in hand), emitted only on the paid path. The
	// RevealChosen parts have no ids -- they make the source's secret
	// designation public with one Note too, the same call pay.EmitChoiceCosts
	// makes.
	if len(u.reveals) > 0 {
		names := make([]string, 0, len(u.reveals))
		for _, id := range u.reveals {
			names = append(names, e.targetName(id))
		}
		e.emit(events.Event{Kind: events.Note, Player: u.payer, Obj: u.ctx.Source,
			IDs:  append([]state.ObjID(nil), u.reveals...),
			Text: "revealed " + strings.Join(names, ", ") + " as a cost"})
	}
	// The settled Behold picks are announced exactly like the cast flow's
	// pay.EmitChoiceCosts announces them: one public Note carrying the beheld ids
	// (nothing is moved for a plain Behold; the ids let a `Remembered`/
	// observer read the choice).
	if len(u.beholds) > 0 {
		names := make([]string, 0, len(u.beholds))
		for _, id := range u.beholds {
			names = append(names, e.targetName(id))
		}
		e.emit(events.Event{Kind: events.Note, Player: u.payer, Obj: u.ctx.Source,
			IDs:  append([]state.ObjID(nil), u.beholds...),
			Text: "beheld " + strings.Join(names, ", ") + " as a cost"})
	}
	for _, part := range u.cost.RevealChosen {
		if text, ok := pay.RevealChosenText(e.G, e.G.Obj(u.ctx.Source), part.Spec); ok {
			e.emit(events.Event{Kind: events.Note, Player: u.payer, Obj: u.ctx.Source, Text: text})
		}
	}
	for _, id := range u.sacs {
		e.emit(events.Sacrifice(id))
	}
	// Bracket the settled Discard component's emissions as ONE discard action
	// (CR 701.8), exactly as the cast flow's payDiscardCost and effDiscard's
	// api:Discard do: a call to discard two cards is one discard action, so a
	// Mode$ DiscardedAll observer queues ONE trigger whose TriggerCount$Amount
	// is the number of cards, not one batch-of-one per event. Nothing here can
	// suspend (all choices were collected above), so the close runs immediately
	// after the last emitted discard, before the Return/Draw parts and
	// finishUnlessPayment resume resolution. Guarded so a payment with no
	// discard leaves no bracket open.
	if len(u.discards) > 0 {
		e.BeginDiscardBatch()
	}
	for _, id := range u.discards {
		e.emit(events.Discard(id, u.payer))
	}
	if len(u.discards) > 0 {
		e.EndDiscardBatch()
	}
	// Return parts: each chosen permanent moves to its OWNER's hand (Forge
	// CostReturn.moveToHand), the same event shape the cast flow's settle
	// emits for a Return cost part.
	for _, id := range u.returns {
		if o := e.G.Obj(id); o != nil {
			e.emit(events.ReturnCost(id, o.Zone))
		}
	}
	// Exile parts: each chosen card moves from its own zone to exile, the
	// same event shape the cast flow's settle emits for an Exile cost part
	// (Forge CostExile.moveToExile).
	for _, id := range u.exiles {
		if o := e.G.Obj(id); o != nil {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZExile, Text: "exiled as a cost"})
		}
	}
	src := u.ctx.Source
	if o := e.G.Obj(u.stackObj); o != nil && o.Ability != nil {
		src = o.Source
	}
	for _, part := range u.cost.SubCounter {
		e.emit(events.Event{Kind: events.CounterChange, Obj: src, Counter: part.Spec, Amount: -part.N})
	}
	for i, part := range u.cost.Draw {
		for _, p := range drawers[i] {
			for n := int32(0); n < part.N; n++ {
				effects.DrawFor(e, p)
			}
		}
	}
	e.finishUnlessPayment(true)
}

// askUnlessMana opens one tap ask over the payer's remaining window-eligible
// sources. Every production alternative of a source is its own option (a dual
// land offers "Tap Volcanic Island for {U}" and "... for {R}"), so the tap
// resolves the chosen ability with no nested colour ask. Options that keep
// the charge reachable come first, and the bot's first-activate pick is then
// always a non-stranding tap; options that would strand remain offered (a
// player may make a losing tap) but after every safe one. "Done" is always
// legal: it settles the payment (the ordinary path declines if the pool still
// cannot cover the charge) and is the R-9 no-host-compatible completion.
func (e *Engine) askUnlessMana() {
	u := e.unlessPayment
	if u == nil {
		return
	}
	units := e.windowManaUnits(u.payer)
	pool := e.G.Players[u.payer].Pool
	snow := e.G.Players[u.payer].Snow
	typed := e.G.Players[u.payer].ManaUnits()
	life := e.G.Players[u.payer].Life
	d := &decision.Decision{Player: u.payer, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Activate mana abilities to pay the unless cost", ResumeKind: "unless_mana"}
	// Safe alternatives first: tapping this one leaves the charge reachable
	// from the remaining sources, so the first-activate bot arm cannot strand.
	for _, safe := range []bool{true, false} {
		for si, src := range units {
			rest := make([]windowManaUnit, 0, len(units)-1)
			rest = append(rest, units[:si]...)
			rest = append(rest, units[si+1:]...)
			for ai, a := range src.Alts {
				if safe != (pay.UnlessManaReachable(asPayer(e), u.payer, u.cost, pay.ManaAdd(pool, a.Mana()), snow, typed, life,
					asPayer(e).Conv(u.payer, u.stackObj, false), rest)) {
					continue
				}
				name := "a mana source"
				if o := e.G.Obj(src.ID); o != nil && o.Face() != nil {
					name = o.Face().Name
				}
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "activate",
					Obj: src.ID, Ability: ai, Label: "Tap " + name + pay.AltLabel(a)})
			}
		}
	}
	d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "done", Label: "Done"})
	windowAsk(e, d, chooseUnlessMana)
}

// answerUnlessMana applies one window answer. "Done" re-enters the ordinary
// payment path (which declines if the pool is still short); an activation
// resolves the recorded alternative's exact *cards.SA -- the membership walk
// captured it, so no chooseMana sub-ask is posed -- and then either re-enters
// advanceUnlessPayment (a sub-askless activation) or waits for
// handleChoose's continuation.
func (e *Engine) answerUnlessMana(chosen []decision.Option) {
	u := e.unlessPayment
	e.choosing = chooseNone
	if u == nil || len(chosen) != 1 {
		return
	}
	if chosen[0].Kind == "done" {
		e.advanceUnlessPayment()
		return
	}
	if chosen[0].Kind != "activate" {
		e.finishUnlessPayment(false)
		return
	}
	for _, src := range e.windowManaUnits(u.payer) {
		if src.ID != chosen[0].Obj {
			continue
		}
		ai := chosen[0].Ability
		if ai < 0 || ai >= len(src.Alts) {
			break
		}
		e.resolveManaAbility(u.payer, src.ID, src.Alts[ai].Ma, false)
		if e.Pending() == nil {
			e.advanceUnlessPayment()
		}
		return
	}
	e.finishUnlessPayment(false)
}

func (e *Engine) unlessPaymentCandidates(u *unlessPayment, zone state.Zone, kind string, part CostPart) []state.ObjID {
	// The dedup is over the union of every component's picks, not just this
	// one's list: one card must not pay two parts, and a card already
	// sacrificed has left its zone anyway, so the wider union only ever
	// removes an already-impossible candidate. Revealed and beheld cards are
	// NOT removed from the hand, which is exactly why they need the explicit
	// exclusion.
	used := make([]state.ObjID, 0, len(u.sacs)+len(u.discards)+len(u.reveals)+len(u.beholds)+len(u.returns)+len(u.exiles))
	used = append(used, u.sacs...)
	used = append(used, u.discards...)
	used = append(used, u.reveals...)
	used = append(used, u.beholds...)
	used = append(used, u.returns...)
	used = append(used, u.exiles...)
	return pay.UnlessCandidatesFor(asPayer(e), u.payer, u.ctx, zone, kind, part, used)
}

func (e *Engine) recordUnlessPaymentPick(u *unlessPayment, kind string, ids []state.ObjID) {
	switch recordUnlessPaymentPickCodes.Code(string(kind)) {
	case recordUnlessPaymentPickSacrifice:
		u.sacs = append(u.sacs, ids...)
	case recordUnlessPaymentPickRevealcost:
		u.reveals = append(u.reveals, ids...)
	case recordUnlessPaymentPickBeholdcost:
		u.beholds = append(u.beholds, ids...)
	case recordUnlessPaymentPickReturncost:
		u.returns = append(u.returns, ids...)
	case recordUnlessPaymentPickExilecost:
		u.exiles = append(u.exiles, ids...)
	default:
		u.discards = append(u.discards, ids...)
	}
}

// revealChosenDesignated reports whether every RevealChosen<Spec> part of the
// pending payment still has its secret designation on the source. A part with
// no designation is unpayable, so the whole payment declines.
func (u *unlessPayment) revealChosenDesignated(e *Engine) bool {
	return pay.UnlessRevealChosenDesignated(e.G, u.cost, u.ctx)
}

func (e *Engine) unlessCountersAffordable(u *unlessPayment) bool {
	return pay.UnlessCountersAffordableFor(asPayer(e), u.cost, u.ctx, u.stackObj)
}

func (e *Engine) answerUnlessPayment(chosen []decision.Option) {
	u := e.unlessPayment
	if u == nil || u.part >= u.paymentPartCount() {
		return
	}
	part, zone, kind := pay.UnlessPartAt(u.cost, u.part)
	ids := make([]state.ObjID, 0, len(chosen))
	for _, o := range chosen {
		if o.Obj != 0 {
			ids = append(ids, o.Obj)
		}
	}
	// Decision.Validate guaranteed the count and offered identity. Recheck the
	// zone/filter against the current game before any event is emitted so a
	// malformed resumed state declines rather than paying an illegal cost.
	eligible := e.unlessPaymentCandidates(u, zone, kind, part)
	allowed := make(map[state.ObjID]bool, len(eligible))
	for _, id := range eligible {
		allowed[id] = true
	}
	if int32(len(ids)) != part.N {
		e.finishUnlessPayment(false)
		return
	}
	for _, id := range ids {
		if !allowed[id] {
			e.finishUnlessPayment(false)
			return
		}
	}
	e.recordUnlessPaymentPick(u, kind, ids)
	u.part++
	e.choosing = chooseNone
	e.advanceUnlessPayment()
}

func (e *Engine) finishUnlessPayment(paid bool) {
	u := e.unlessPayment
	if u == nil {
		return
	}
	e.unlessPayment = nil
	e.choosing = chooseNone
	if u.tape {
		tapeUnlessSettled(e, u, paid)
		return
	}
	e.finishManaUnlessPayment(paid)
}

type recordUnlessPaymentPickCode uint16

const (
	recordUnlessPaymentPickSacrifice recordUnlessPaymentPickCode = iota + 1
	recordUnlessPaymentPickRevealcost
	recordUnlessPaymentPickBeholdcost
	recordUnlessPaymentPickReturncost
	recordUnlessPaymentPickExilecost
)

var recordUnlessPaymentPickCodes = state.NewStrCodes(
	state.StrEntry[recordUnlessPaymentPickCode]{Key: "sacrifice", Val: recordUnlessPaymentPickSacrifice},
	state.StrEntry[recordUnlessPaymentPickCode]{Key: "revealcost", Val: recordUnlessPaymentPickRevealcost},
	state.StrEntry[recordUnlessPaymentPickCode]{Key: "beholdcost", Val: recordUnlessPaymentPickBeholdcost},
	state.StrEntry[recordUnlessPaymentPickCode]{Key: "returncost", Val: recordUnlessPaymentPickReturncost},
	state.StrEntry[recordUnlessPaymentPickCode]{Key: "exilecost", Val: recordUnlessPaymentPickExilecost},
)
