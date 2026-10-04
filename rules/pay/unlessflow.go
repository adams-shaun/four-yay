package pay

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// The unless-payment flow (lasagna spec §9.2, E7 flow slice): the walk that
// collects an accepted UnlessCost$'s payer-chosen components, opens the CR
// 601.2g mana window, and settles the payment. Its state is the session's
// UnlessPayment; every ask goes through Engine.Ask, which answers it in place
// under the resolution kernel (the tape) or poses it and returns. What
// happens once the payment is settled -- resuming the tape-driven resolution
// or the activated mana ability that owns it -- is the engine's flow, so the
// walk reports the settlement (UnlessStep) instead of continuing itself.

// UnlessStep is the outcome of one step of the unless-payment walk.
type UnlessStep struct {
	// Done reports the payment settled: the engine finishes it (Paid) after
	// running the Draws. A step that is not Done posed an ask (or found no
	// payment in progress) and has nothing more to do.
	Done bool
	// Paid reports the cost was paid in full.
	Paid bool
	// Draws are the resolved Draw<N/Spec> drawers, per Draw part, of a paid
	// payment: the engine draws Cost.Draw[i].N cards for each, in order,
	// before it finishes (a draw evaluates through the engine as a Host).
	Draws [][]state.PlayerID
}

func unlessDeclined() UnlessStep { return UnlessStep{Done: true} }

// AdvanceUnless runs the in-progress unless payment as far as it can go: it
// records every component whose candidates are exact, asks for the next
// payer-chosen one (or the next mana source while the pool cannot cover the
// charge), and otherwise charges the whole cost.
func AdvanceUnless(e Engine) UnlessStep {
	s := e.Session()
	g := e.Game()
	u := s.UnlessPayment
	if u == nil {
		return UnlessStep{}
	}
	if int(u.Payer) < 0 || int(u.Payer) >= len(g.Players) ||
		!UnlessCountersAffordable(e, u) ||
		!UnlessRevealChosenDesignated(g, u.Cost, u.Ctx) {
		return unlessDeclined()
	}
	// A mana component the pool cannot yet cover opens the one-source-at-a-
	// time CR 601.2g window (the ward/attack-prop discipline) rather than
	// declining: the payer taps until the pool covers the charge, then the
	// ordinary path below pays it. The window is only opened while a source
	// remains that the budget counted.
	d := Descriptor{ID: u.StackObj, Class: PurposeOther, Cost: &u.Cost}
	// Open the window only when the POOL ALONE cannot pay (lifeGrant false:
	// a {B} pip K'rrik's grant could settle with 2 life must not close it)
	// AND a window source remains. The ordinary grant-bearing check below is
	// retained: when no source can help (or the payer answers Done with the
	// pool already covering the charge) the granted life still pays it.
	if !CostPayableClassLife(e, u.Payer, d, PipRider{}, u.Cost, false) &&
		u.Cost.HasManaPayment() && len(e.Eval().WindowUnits(u.Payer)) > 0 {
		askUnlessMana(e)
		return UnlessStep{}
	}
	if !CostPayableClass(e, u.Payer, d, PipRider{}, u.Cost) {
		return unlessDeclined()
	}
	for u.Part < u.PaymentPartCount() {
		part, zone, kind := UnlessPartAt(u.Cost, u.Part)
		eligible := UnlessPaymentCandidates(e, u, zone, kind, part)
		if int32(len(eligible)) < part.N {
			return unlessDeclined()
		}
		if kind == "exilecost" && costvocab.IsWholeZoneExileSpec(part.Spec) {
			// ExileFromGrave<1/All> names the WHOLE zone: every matching
			// candidate is the payment, never a pick (the same
			// isWholeZoneExileSpec reading the cast gate and the
			// triggered-cost window take). The count check above still
			// demands part.N cards, so an empty zone declines.
			RecordUnlessPaymentPick(e, u, kind, eligible)
			u.Part++
			continue
		}
		if int32(len(eligible)) == part.N {
			RecordUnlessPaymentPick(e, u, kind, eligible)
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
			if o := g.Obj(id); o != nil && o.Face() != nil {
				label = o.Face().Name
			}
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: kind,
				Label: label, Obj: id, Player: u.Payer})
		}
		e.Ask(AskUnlessCost, d)
		return UnlessStep{}
	}
	// Resolve every drawer before charging any component. A Draw<N/Spec> may
	// name a role that this suspended resolution did not retain; that is an
	// unpayable cost, not a reason to spend mana/life and then silently omit
	// the draw. Keeping the resolved players also makes the selected-cost path
	// use the same binding rules as payUnlessCost's no-choice path.
	drawers := make([][]state.PlayerID, len(u.Cost.Draw))
	for i, part := range u.Cost.Draw {
		players, ok := UnlessDrawPlayers(&u.Ctx, u.Payer, part.Spec)
		if !ok {
			return unlessDeclined()
		}
		for _, p := range players {
			if int(p) < 0 || int(p) >= len(g.Players) {
				return unlessDeclined()
			}
		}
		drawers[i] = players
	}
	if !PayManaConv(e, u.Payer, u.Cost, e.Conv(u.Payer, u.StackObj, false)) { // guarded above; retain totality if state changes.
		return unlessDeclined()
	}
	// Energy parts (PayEnergy<N>/<X>): the same shared charging site the cast
	// flow uses, one PlayerCounterChange per part. The offer gate proved the
	// total affordable; a state change since the choices is still guarded by
	// the same read.
	if !UnlessEnergyAffordable(e, u.Payer, u.Cost, &u.Ctx) {
		return unlessDeclined()
	}
	x := int32(0)
	if u.Ctx.XAnnounced {
		x = u.Ctx.X
	}
	ChargeEnergyCost(e, u.Payer, u.Cost, x)
	// The settled reveal picks are announced exactly like the cast flow's
	// EmitChoiceCosts announces them: one public Note carrying the revealed
	// ids (the cards STAY in hand), emitted only on the paid path. The
	// RevealChosen parts have no ids -- they make the source's secret
	// designation public with one Note too, the same call EmitChoiceCosts
	// makes.
	if len(u.Reveals) > 0 {
		names := make([]string, 0, len(u.Reveals))
		for _, id := range u.Reveals {
			names = append(names, TargetName(g, id))
		}
		e.Emit(events.Event{Kind: events.Note, Player: u.Payer, Obj: u.Ctx.Source,
			IDs:  append([]state.ObjID(nil), u.Reveals...),
			Text: "revealed " + strings.Join(names, ", ") + " as a cost"})
	}
	// The settled Behold picks are announced exactly like the cast flow's
	// EmitChoiceCosts announces them: one public Note carrying the beheld ids
	// (nothing is moved for a plain Behold; the ids let a `Remembered`/
	// observer read the choice).
	if len(u.Beholds) > 0 {
		names := make([]string, 0, len(u.Beholds))
		for _, id := range u.Beholds {
			names = append(names, TargetName(g, id))
		}
		e.Emit(events.Event{Kind: events.Note, Player: u.Payer, Obj: u.Ctx.Source,
			IDs:  append([]state.ObjID(nil), u.Beholds...),
			Text: "beheld " + strings.Join(names, ", ") + " as a cost"})
	}
	for _, part := range u.Cost.RevealChosen {
		if text, ok := RevealChosenText(g, g.Obj(u.Ctx.Source), part.Spec); ok {
			e.Emit(events.Event{Kind: events.Note, Player: u.Payer, Obj: u.Ctx.Source, Text: text})
		}
	}
	for _, id := range u.Sacs {
		e.Emit(events.Sacrifice(id))
	}
	// Bracket the settled Discard component's emissions as ONE discard action
	// (CR 701.8), exactly as the cast flow's PayDiscardCost and effDiscard's
	// api:Discard do: a call to discard two cards is one discard action, so a
	// Mode$ DiscardedAll observer queues ONE trigger whose TriggerCount$Amount
	// is the number of cards, not one batch-of-one per event. Nothing here can
	// suspend (all choices were collected above), so the close runs immediately
	// after the last emitted discard, before the Return/Draw parts and the
	// engine's finish resume resolution. Guarded so a payment with no discard
	// leaves no bracket open.
	if len(u.Discards) > 0 {
		e.Batch(BatchDiscard, true)
	}
	for _, id := range u.Discards {
		e.Emit(events.Discard(id, u.Payer))
	}
	if len(u.Discards) > 0 {
		e.Batch(BatchDiscard, false)
	}
	// Return parts: each chosen permanent moves to its OWNER's hand (Forge
	// CostReturn.moveToHand), the same event shape the cast flow's settle
	// emits for a Return cost part.
	for _, id := range u.Returns {
		if o := g.Obj(id); o != nil {
			e.Emit(events.ReturnCost(id, o.Zone))
		}
	}
	// Exile parts: each chosen card moves from its own zone to exile, the
	// same event shape the cast flow's settle emits for an Exile cost part
	// (Forge CostExile.moveToExile).
	for _, id := range u.Exiles {
		if o := g.Obj(id); o != nil {
			e.Emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZExile, Text: "exiled as a cost"})
		}
	}
	src := u.Ctx.Source
	if o := g.Obj(u.StackObj); o != nil && o.Ability != nil {
		src = o.Source
	}
	for _, part := range u.Cost.SubCounter {
		e.Emit(events.Event{Kind: events.CounterChange, Obj: src, Counter: part.Spec, Amount: -part.N})
	}
	return UnlessStep{Done: true, Paid: true, Draws: drawers}
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
func askUnlessMana(e Engine) {
	u := e.Session().UnlessPayment
	if u == nil {
		return
	}
	g := e.Game()
	units := e.Eval().WindowUnits(u.Payer)
	pool := g.Players[u.Payer].Pool
	snow := g.Players[u.Payer].Snow
	typed := g.Players[u.Payer].ManaUnits()
	life := g.Players[u.Payer].Life
	d := &decision.Decision{Player: u.Payer, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Activate mana abilities to pay the unless cost", ResumeKind: "unless_mana"}
	// Safe alternatives first: tapping this one leaves the charge reachable
	// from the remaining sources, so the first-activate bot arm cannot strand.
	for _, safe := range []bool{true, false} {
		for si, src := range units {
			rest := make([]WindowUnit, 0, len(units)-1)
			rest = append(rest, units[:si]...)
			rest = append(rest, units[si+1:]...)
			for ai, a := range src.Alts {
				if safe != (UnlessManaReachable(e, u.Payer, u.Cost, ManaAdd(pool, a.Mana()), snow, typed, life,
					e.Conv(u.Payer, u.StackObj, false), rest)) {
					continue
				}
				name := "a mana source"
				if o := g.Obj(src.ID); o != nil && o.Face() != nil {
					name = o.Face().Name
				}
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "activate",
					Obj: src.ID, Ability: ai, Label: "Tap " + name + AltLabel(a)})
			}
		}
	}
	d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "done", Label: "Done"})
	e.Ask(AskUnlessMana, d)
}

// AnswerUnlessPayment applies the payer's pick for the component the walk
// asked about. It reports false, with the payment untouched, when no
// component is awaiting a pick (the engine then does nothing). Otherwise the
// returned step is the payment's next state: a rejected pick declines it.
// The engine clears its ask marker before the call.
func AnswerUnlessPayment(e Engine, chosen []decision.Option) (UnlessStep, bool) {
	u := e.Session().UnlessPayment
	if u == nil || u.Part >= u.PaymentPartCount() {
		return UnlessStep{}, false
	}
	part, zone, kind := UnlessPartAt(u.Cost, u.Part)
	ids := make([]state.ObjID, 0, len(chosen))
	for _, o := range chosen {
		if o.Obj != 0 {
			ids = append(ids, o.Obj)
		}
	}
	// Decision.Validate guaranteed the count and offered identity. Recheck the
	// zone/filter against the current game before any event is emitted so a
	// malformed resumed state declines rather than paying an illegal cost.
	eligible := UnlessPaymentCandidates(e, u, zone, kind, part)
	allowed := make(map[state.ObjID]bool, len(eligible))
	for _, id := range eligible {
		allowed[id] = true
	}
	if int32(len(ids)) != part.N {
		return unlessDeclined(), true
	}
	for _, id := range ids {
		if !allowed[id] {
			return unlessDeclined(), true
		}
	}
	RecordUnlessPaymentPick(e, u, kind, ids)
	u.Part++
	return AdvanceUnless(e), true
}

// BeginUnless installs a payer-selected payment of cost and runs it. It owns
// all Sac, Discard and Reveal components, including the exact-candidate
// no-ask cases, so neither this path nor a future sibling can fall back to a
// first-in-zone-order pick.
func BeginUnless(e Engine, payer state.PlayerID, cost costvocab.Cost, ctx *effects.Ctx, stackObj state.ObjID) UnlessStep {
	// Fold the dynamic life tokens once, at continuation start: the offer
	// gate folded the same amounts against the same payer read, and the
	// charge below prices exactly this folded cost.
	s := e.Session()
	cost, ok := UnlessFoldDynamic(e, payer, cost, ctx)
	if !ok {
		s.UnlessPayment = &UnlessPayment{Payer: payer, Ctx: CloneUnlessCtx(*ctx), StackObj: stackObj}
		return unlessDeclined()
	}
	s.UnlessPayment = &UnlessPayment{Payer: payer, Cost: cost, Ctx: CloneUnlessCtx(*ctx), StackObj: stackObj}
	return AdvanceUnless(e)
}
