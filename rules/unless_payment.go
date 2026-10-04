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

// unlessPayment is the in-progress unless-cost payment (pay.UnlessPayment).
type unlessPayment = pay.UnlessPayment

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
		e.UnlessPayment = &unlessPayment{Payer: payer, Ctx: pay.CloneUnlessCtx(*ctx), StackObj: stackObj}
		e.finishUnlessPayment(false)
		return
	}
	e.UnlessPayment = &unlessPayment{Payer: payer, Cost: cost, Ctx: pay.CloneUnlessCtx(*ctx), StackObj: stackObj}
	e.advanceUnlessPayment()
}

func (e *Engine) advanceUnlessPayment() {
	u := e.UnlessPayment
	if u == nil {
		return
	}
	if int(u.Payer) < 0 || int(u.Payer) >= len(e.G.Players) ||
		!e.unlessCountersAffordable(u) ||
		!pay.UnlessRevealChosenDesignated(e.G, u.Cost, u.Ctx) {
		e.finishUnlessPayment(false)
		return
	}
	// A mana component the pool cannot yet cover opens the one-source-at-a-
	// time CR 601.2g window (the ward/attack-prop discipline) rather than
	// declining: the payer taps until the pool covers the charge, then the
	// ordinary path below pays it. The window is only opened while a source
	// remains that the budget counted.
	d := paymentDescriptor{ID: u.StackObj, Class: paymentOther, Cost: &u.Cost}
	// Open the window only when the POOL ALONE cannot pay (lifeGrant false:
	// a {B} pip K'rrik's grant could settle with 2 life must not close it)
	// AND a window source remains. The ordinary grant-bearing check below is
	// retained: when no source can help (or the payer answers Done with the
	// pool already covering the charge) the granted life still pays it.
	if !pay.CostPayableClassLife(asPayer(e), u.Payer, d, pipRider{}, u.Cost, false) &&
		u.Cost.HasManaPayment() && len(e.windowManaUnits(u.Payer)) > 0 {
		e.askUnlessMana()
		return
	}
	if !pay.CostPayableClass(asPayer(e), u.Payer, d, pipRider{}, u.Cost) {
		e.finishUnlessPayment(false)
		return
	}
	for u.Part < u.PaymentPartCount() {
		part, zone, kind := pay.UnlessPartAt(u.Cost, u.Part)
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
			u.Part++
			continue
		}
		if int32(len(eligible)) == part.N {
			e.recordUnlessPaymentPick(u, kind, eligible)
			u.Part++
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
		d := &decision.Decision{Player: u.Payer, Kind: decision.KChoose,
			Min: int(part.N), Max: int(part.N), Source: u.Ctx.Source,
			ResumeKind: "unless_cost",
			Prompt:     prompt}
		for _, id := range eligible {
			label := "a card"
			if o := e.G.Obj(id); o != nil && o.Face() != nil {
				label = o.Face().Name
			}
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: kind,
				Label: label, Obj: id, Player: u.Payer})
		}
		windowAsk(e, d, chooseUnlessCost)
		return
	}
	// Resolve every drawer before charging any component. A Draw<N/Spec> may
	// name a role that this suspended resolution did not retain; that is an
	// unpayable cost, not a reason to spend mana/life and then silently omit
	// the draw. Keeping the resolved players also makes the selected-cost path
	// use the same binding rules as payUnlessCost's no-choice path.
	drawers := make([][]state.PlayerID, len(u.Cost.Draw))
	for i, part := range u.Cost.Draw {
		players, ok := pay.UnlessDrawPlayers(&u.Ctx, u.Payer, part.Spec)
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
	if !pay.PayManaConv(asPayer(e), u.Payer, u.Cost, asPayer(e).Conv(u.Payer, u.StackObj, false)) { // guarded above; retain totality if state changes.
		e.finishUnlessPayment(false)
		return
	}
	// Energy parts (PayEnergy<N>/<X>): the same shared charging site the cast
	// flow uses, one PlayerCounterChange per part. The offer gate proved the
	// total affordable; a state change since the choices is still guarded by
	// the same read.
	if !pay.UnlessEnergyAffordable(asPayer(e), u.Payer, u.Cost, &u.Ctx) {
		e.finishUnlessPayment(false)
		return
	}
	x := int32(0)
	if u.Ctx.XAnnounced {
		x = u.Ctx.X
	}
	pay.ChargeEnergyCost(asPayer(e), u.Payer, u.Cost, x)
	// The settled reveal picks are announced exactly like the cast flow's
	// pay.EmitChoiceCosts announces them: one public Note carrying the revealed
	// ids (the cards STAY in hand), emitted only on the paid path. The
	// RevealChosen parts have no ids -- they make the source's secret
	// designation public with one Note too, the same call pay.EmitChoiceCosts
	// makes.
	if len(u.Reveals) > 0 {
		names := make([]string, 0, len(u.Reveals))
		for _, id := range u.Reveals {
			names = append(names, e.targetName(id))
		}
		e.emit(events.Event{Kind: events.Note, Player: u.Payer, Obj: u.Ctx.Source,
			IDs:  append([]state.ObjID(nil), u.Reveals...),
			Text: "revealed " + strings.Join(names, ", ") + " as a cost"})
	}
	// The settled Behold picks are announced exactly like the cast flow's
	// pay.EmitChoiceCosts announces them: one public Note carrying the beheld ids
	// (nothing is moved for a plain Behold; the ids let a `Remembered`/
	// observer read the choice).
	if len(u.Beholds) > 0 {
		names := make([]string, 0, len(u.Beholds))
		for _, id := range u.Beholds {
			names = append(names, e.targetName(id))
		}
		e.emit(events.Event{Kind: events.Note, Player: u.Payer, Obj: u.Ctx.Source,
			IDs:  append([]state.ObjID(nil), u.Beholds...),
			Text: "beheld " + strings.Join(names, ", ") + " as a cost"})
	}
	for _, part := range u.Cost.RevealChosen {
		if text, ok := pay.RevealChosenText(e.G, e.G.Obj(u.Ctx.Source), part.Spec); ok {
			e.emit(events.Event{Kind: events.Note, Player: u.Payer, Obj: u.Ctx.Source, Text: text})
		}
	}
	for _, id := range u.Sacs {
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
	if len(u.Discards) > 0 {
		e.BeginDiscardBatch()
	}
	for _, id := range u.Discards {
		e.emit(events.Discard(id, u.Payer))
	}
	if len(u.Discards) > 0 {
		e.EndDiscardBatch()
	}
	// Return parts: each chosen permanent moves to its OWNER's hand (Forge
	// CostReturn.moveToHand), the same event shape the cast flow's settle
	// emits for a Return cost part.
	for _, id := range u.Returns {
		if o := e.G.Obj(id); o != nil {
			e.emit(events.ReturnCost(id, o.Zone))
		}
	}
	// Exile parts: each chosen card moves from its own zone to exile, the
	// same event shape the cast flow's settle emits for an Exile cost part
	// (Forge CostExile.moveToExile).
	for _, id := range u.Exiles {
		if o := e.G.Obj(id); o != nil {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZExile, Text: "exiled as a cost"})
		}
	}
	src := u.Ctx.Source
	if o := e.G.Obj(u.StackObj); o != nil && o.Ability != nil {
		src = o.Source
	}
	for _, part := range u.Cost.SubCounter {
		e.emit(events.Event{Kind: events.CounterChange, Obj: src, Counter: part.Spec, Amount: -part.N})
	}
	for i, part := range u.Cost.Draw {
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
	u := e.UnlessPayment
	if u == nil {
		return
	}
	units := e.windowManaUnits(u.Payer)
	pool := e.G.Players[u.Payer].Pool
	snow := e.G.Players[u.Payer].Snow
	typed := e.G.Players[u.Payer].ManaUnits()
	life := e.G.Players[u.Payer].Life
	d := &decision.Decision{Player: u.Payer, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Activate mana abilities to pay the unless cost", ResumeKind: "unless_mana"}
	// Safe alternatives first: tapping this one leaves the charge reachable
	// from the remaining sources, so the first-activate bot arm cannot strand.
	for _, safe := range []bool{true, false} {
		for si, src := range units {
			rest := make([]windowManaUnit, 0, len(units)-1)
			rest = append(rest, units[:si]...)
			rest = append(rest, units[si+1:]...)
			for ai, a := range src.Alts {
				if safe != (pay.UnlessManaReachable(asPayer(e), u.Payer, u.Cost, pay.ManaAdd(pool, a.Mana()), snow, typed, life,
					asPayer(e).Conv(u.Payer, u.StackObj, false), rest)) {
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
	u := e.UnlessPayment
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
	for _, src := range e.windowManaUnits(u.Payer) {
		if src.ID != chosen[0].Obj {
			continue
		}
		ai := chosen[0].Ability
		if ai < 0 || ai >= len(src.Alts) {
			break
		}
		e.resolveManaAbility(u.Payer, src.ID, src.Alts[ai].Ma, false)
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
	used := make([]state.ObjID, 0, len(u.Sacs)+len(u.Discards)+len(u.Reveals)+len(u.Beholds)+len(u.Returns)+len(u.Exiles))
	used = append(used, u.Sacs...)
	used = append(used, u.Discards...)
	used = append(used, u.Reveals...)
	used = append(used, u.Beholds...)
	used = append(used, u.Returns...)
	used = append(used, u.Exiles...)
	return pay.UnlessCandidatesFor(asPayer(e), u.Payer, u.Ctx, zone, kind, part, used)
}

func (e *Engine) recordUnlessPaymentPick(u *unlessPayment, kind string, ids []state.ObjID) {
	switch recordUnlessPaymentPickCodes.Code(string(kind)) {
	case recordUnlessPaymentPickSacrifice:
		u.Sacs = append(u.Sacs, ids...)
	case recordUnlessPaymentPickRevealcost:
		u.Reveals = append(u.Reveals, ids...)
	case recordUnlessPaymentPickBeholdcost:
		u.Beholds = append(u.Beholds, ids...)
	case recordUnlessPaymentPickReturncost:
		u.Returns = append(u.Returns, ids...)
	case recordUnlessPaymentPickExilecost:
		u.Exiles = append(u.Exiles, ids...)
	default:
		u.Discards = append(u.Discards, ids...)
	}
}

func (e *Engine) unlessCountersAffordable(u *unlessPayment) bool {
	return pay.UnlessCountersAffordableFor(asPayer(e), u.Cost, u.Ctx, u.StackObj)
}

func (e *Engine) answerUnlessPayment(chosen []decision.Option) {
	u := e.UnlessPayment
	if u == nil || u.Part >= u.PaymentPartCount() {
		return
	}
	part, zone, kind := pay.UnlessPartAt(u.Cost, u.Part)
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
	u.Part++
	e.choosing = chooseNone
	e.advanceUnlessPayment()
}

func (e *Engine) finishUnlessPayment(paid bool) {
	u := e.UnlessPayment
	if u == nil {
		return
	}
	e.UnlessPayment = nil
	e.choosing = chooseNone
	if u.Tape {
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
