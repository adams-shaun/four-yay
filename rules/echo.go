package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// Echo (kw:Echo, CR 702.35a: "At the beginning of your upkeep, if this
// permanent came under your control since the beginning of your most recent
// upkeep, you may pay {cost}. If you don't, sacrifice it.") is expanded by
// cards into an ordinary beginning-of-upkeep Phase trigger — the Cumulative
// upkeep shape — so it is ordered and placed with every other upkeep trigger
// and can be responded to before it resolves. This file owns three pieces:
//
//   - the intervening-if (CR 603.4): triggerMatches's Phase branch checks
//     Echo$ True against echoGateHolds BEFORE the trigger stacks, so a
//     permanent whose acquisition predates its controller's most recent
//     upkeep never goes on the stack at all (the gate is a trigger
//     condition, not a resolution-time choice — a stacked-but-suppressed
//     echo would be an observable divergence);
//   - the provenance the gate reads: Object.AcqTurn/AcqStep stamped by
//     events.Apply on every battlefield entry and every battlefield control
//     change, against Player.LastUpkeepTurn recorded when the Draw step
//     begins (see the StepChange case in events/apply.go for why the record
//     is the Draw step and not the Upkeep one);
//   - the resolution-time election: pay the echo cost through the shared
//     pool-only payment window (paymentManaAsk/costPayable/payManaConv,
//     reused verbatim from rules/cumulative.go), or sacrifice. The pay
//     option is offered FIRST when payable (a bot's clamp fallback takes
//     option 0 — pay is the sane default) and the sacrifice option is
//     always present, so nobody is ever wedged.
//
// chooseEcho sits above the shared package enum's current highest
// (chooseRiot = 20); the numbers only need to differ, per the chooseFor
// contract — transient engine RAM, Clone copies the struct, no event or wire
// field ever carries one.
const chooseEcho chooseFor = chooseRiot + 1

type echoFlow struct {
	stackObj   state.ObjID
	source     state.ObjID
	player     state.PlayerID
	amount     Cost
	costLabel  string
	windowDone bool

	// pips carries the flexible-pip payment announcement (CR 601.2b/107.4e-f)
	// for this window, exactly as triggeredEffectCost.pips does: pipAnnounceAsk
	// asks each of the cost's announcement pips, announced() folds the elected
	// faces into the cost the pay gate prices and the pay arm charges, and the
	// answer arm advances through the shared accumulator. A value struct, so a
	// clone carries it.
	pips pipAnnounce

	// action carries a non-mana echo cost (Discard<1/Card>, Sac<2/Land>)
	// parsed by the shared parseCumulativeAction vocabulary; executing it
	// reuses cumulativeObjects/cumulativeActionPayable against this shim.
	action *cumulativeAction
}

// echoGateHolds is the CR 702.35a intervening-if: the permanent owes echo at
// its controller's current upkeep iff it came under that controller's control
// since the beginning of the most recent upkeep. Zero LastUpkeepTurn (no
// upkeep has been recorded for this seat) is vacuously true — a permanent
// under a controller who has never had an upkeep is owed at their first one.
// The (turn, step) tuple comparison handles the CR-critical turn-granularity
// counterexample: an acquisition on turn N AFTER that turn's upkeep began
// (any step from Upkeep on) is still owed at turn N+1, because the record
// only reaches turn N when the Draw step begins and the AcqStep half counts
// a same-turn acquisition from the Upkeep step onward.
func (e *Engine) echoGateHolds(source state.ObjID) bool {
	o := e.G.Obj(source)
	if o == nil || o.Zone != state.ZBattlefield {
		return false
	}
	p := e.controllerOf(source)
	if int(p) >= len(e.G.Players) {
		return false
	}
	last := e.G.Players[p].LastUpkeepTurn
	if last == 0 {
		return true
	}
	return o.AcqTurn > last || (o.AcqTurn == last && o.AcqStep >= state.StepUpkeep)
}

// echoAnnounceableCost reports whether a cost Priceable() rejects is still a
// real echo election because its ONLY unpriceable components are announcement
// pips the election announces (CR 601.2b). Clearing the four pip lists leaves
// the remainder, and that remainder must be Priceable -- so a cost that ALSO
// carries an X, a Tap or a Sac component stays unresolvable and is skipped
// loudly, exactly as before. A cost with no announcement pip is never
// announceable here.
func echoAnnounceableCost(c Cost) bool {
	if c.AnnPipCount() == 0 {
		return false
	}
	c.Hybrid = nil
	c.Phyrexian = nil
	c.Twobrid = nil
	c.HybridPhyrexian = nil
	return c.Priceable()
}

// startEcho starts resolution of the already-stacked keyword trigger. The
// election's outcome is not applied before this point: responses and another
// simultaneous upkeep trigger observe the pre-resolution board, like every
// other resolving ability.
func (e *Engine) startEcho(stackObj, source state.ObjID, sa *cards.SA) {
	o := e.G.Obj(source)
	stack := e.G.Obj(stackObj)
	if o == nil || o.Zone != state.ZBattlefield || stack == nil {
		e.finishResumption(stackObj)
		return
	}
	// The triggered ability's controller was captured when it was placed on
	// the stack; a response changing the permanent's control must not
	// transfer the already-triggered election (the Cumulative upkeep
	// contract, CR 113.8).
	ef := &echoFlow{stackObj: stackObj, source: source, player: stack.Controller}
	label := sa.ParamStr(cards.PKCost)
	if action, ok := parseCumulativeAction(label); ok {
		// Non-mana echo cost (Discard<1/Card>, Sac<2/Land> — 3 corpus
		// files): reuse the cumulative-upkeep action vocabulary and its
		// payable/object helpers; the election below poses echo-labelled
		// asks and the answer executes the action once.
		ef.action = action
	} else {
		ef.amount = e.parseCost(label)
		if !ef.amount.Priceable() && !echoAnnounceableCost(ef.amount) {
			// An unresolvable echo cost (Volcano Hellion's K:Echo:X, whose
			// SVar prices X at the controller's life total): the election is
			// the card's contract, so it is skipped LOUDLY, never silently —
			// one Note names the unresolved cost and the permanent stays
			// (a silent sacrifice every upkeep would be flatly wrong).
			e.emit(events.Event{Kind: events.Note, Player: ef.player, Obj: source,
				Text: "echo cost unresolvable: " + label})
			e.finishResumption(stackObj)
			e.emit(events.Event{Kind: events.Priority, Player: e.G.Active})
			return
		}
	}
	ef.costLabel = cumulativeCostLabel(label, ef.action, ef.amount)
	e.echo = ef
	e.echoElectionAsk()
}

// echoElectionAsk poses the pay-or-sacrifice election. A plain mana cost
// announces its flexible pips first (CR 601.2b), then opens the shared
// mana-ability payment window when the pool cannot pay yet; the pay option is
// offered only when the ANNOUNCED cost is payable (the cumulativePaymentAsk
// shape — nobody is ever wedged), the sacrifice option always.
func (e *Engine) echoElectionAsk() {
	ef := e.echo
	if ef == nil {
		return
	}
	o := e.G.Obj(ef.source)
	if o == nil || o.Zone != state.ZBattlefield {
		e.finishEcho()
		return
	}
	if ef.action == nil && e.pipAnnounceAsk(ef.player, ef.source, ef.amount, &ef.pips,
		ef.costLabel, chooseEcho) {
		return
	}
	announced := ef.pips.fold(ef.amount)
	if ef.action == nil && e.paymentManaAsk(ef.player, ef.source, announced, ef.windowDone,
		"Activate mana abilities to pay echo", chooseEcho) {
		return
	}
	payable := false
	if ef.action == nil {
		payable = announced.Priceable() && pay.CostPayableOther(asPayer(e), ef.player, ef.source, announced)
	} else {
		shim := &cumulativeUpkeep{player: ef.player, source: ef.source,
			action: ef.action, actionRemaining: 1}
		payable = e.cumulativeActionPayable(shim)
	}
	var opts []decision.Option
	if payable {
		opts = append(opts, decision.Option{Index: 0, Kind: "echo_pay", Obj: ef.source,
			Label: capitaliseFirst(ef.costLabel)})
	}
	opts = append(opts, decision.Option{Index: len(opts), Kind: "echo_sac", Obj: ef.source,
		Label: "Sacrifice " + o.Face().Name})
	windowAsk(e, &decision.Decision{Player: ef.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: o.Face().Name + " — echo: " + ef.costLabel + " or sacrifice",
		Source: ef.source, Options: opts}, chooseEcho)
}

// echoAnswer handles the election (and the mana window feeding it). The
// decline arm and any failed payment fall through to the sacrifice — CR
// 702.35a's "if you don't, sacrifice it".
func (e *Engine) echoAnswer(chosen []decision.Option) {
	ef := e.echo
	if ef == nil || len(chosen) == 0 {
		return
	}
	e.choosing = chooseNone
	if ef.pips.accept(chosen[0].Kind, chosen[0].Amount) {
		e.echoElectionAsk()
		return
	}
	switch echoAnswerCodes.Code(string(chosen[0].Kind)) {
	case echoAnswerActivate:
		e.activatePaymentMana(ef.player, chosen[0].Obj)
		return
	case echoAnswerDone:
		ef.windowDone = true
		e.echoElectionAsk()
		return
	case echoAnswerEchoPay:
		if ef.action != nil {
			e.echoActionAsk()
			return
		}
		announced := ef.pips.fold(ef.amount)
		if announced.Priceable() &&
			pay.PayManaConv(asPayer(e), ef.player, announced, asEval(e).Conv(ef.player, ef.source, false)) {
			e.finishEcho()
			return
		}
	case echoAnswerEchoActionPick:
		e.echoActionExecute(chosen)
		return
	}
	if o := e.G.Obj(ef.source); o != nil && o.Zone == state.ZBattlefield {
		e.emit(events.Event{Kind: events.MoveZone, Obj: ef.source, From: state.ZBattlefield,
			To: state.ZGraveyard, Text: "sacrificed for echo"})
	}
	e.finishEcho()
}

// echoActionAsk poses the object-choice ask a non-mana echo cost's pay arm
// owes (Discard<1/Card>: which card; Sac<2/Land>: which two permanents),
// reusing the cumulative action's object finder over an echo-labelled
// decision so the prompts never say "cumulative upkeep".
func (e *Engine) echoActionAsk() {
	ef := e.echo
	if ef == nil || ef.action == nil {
		return
	}
	shim := &cumulativeUpkeep{player: ef.player, source: ef.source,
		action: ef.action, actionRemaining: 1}
	total := int(ef.action.n)
	zone := state.ZBattlefield
	prompt := "Choose permanents to sacrifice for echo"
	kind := "echo_action_pick"
	if ef.action.kind == "Discard" {
		zone = state.ZHand
		prompt = "Choose cards to discard for echo"
	}
	// A Sac echo payment is demanded by the echo trigger (CR 702.35), so its
	// candidate walk runs the same CantSacrifice cost gate the cumulative
	// upkeep Sac arm does (cantsac1 r2); the Discard arm reads a hand, where
	// no sacrifice is made.
	var ids []state.ObjID
	if ef.action.kind == "Sac" {
		ids = e.cumulativeSacObjects(shim)
	} else {
		ids = e.cumulativeObjects(shim, zone, ef.action.spec)
	}
	d := &decision.Decision{Player: ef.player, Kind: decision.KChoose, Min: total, Max: total,
		Prompt: prompt, Source: ef.source}
	for _, id := range ids {
		label := "card"
		if o := e.G.Obj(id); o != nil && o.Face() != nil {
			label = o.Face().Name
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: kind, Obj: id, Label: label})
	}
	windowAsk(e, d, chooseEcho)
}

// echoActionExecute applies the answered object choice — one move per chosen
// object, with the echo text on the move — then finishes.
func (e *Engine) echoActionExecute(chosen []decision.Option) {
	ef := e.echo
	if ef == nil {
		return
	}
	for _, option := range chosen {
		o := e.G.Obj(option.Obj)
		if o == nil {
			continue
		}
		if ef.action.kind == "Discard" && o.Zone == state.ZHand && o.Owner == ef.player {
			e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZHand,
				To: state.ZGraveyard, Text: "discarded for echo"})
		} else if ef.action.kind == "Sac" && o.Zone == state.ZBattlefield {
			e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZBattlefield,
				To: state.ZGraveyard, Text: "sacrificed for echo"})
		}
	}
	e.finishEcho()
}

func (e *Engine) finishEcho() {
	ef := e.echo
	e.echo = nil
	e.choosing = chooseNone
	if ef == nil {
		return
	}
	e.finishResumption(ef.stackObj)
	e.emit(events.Event{Kind: events.Priority, Player: e.G.Active})
}

func init() {
	effects.RegisterNonAPI("kw:Echo")
}

type echoAnswerCode uint16

const (
	echoAnswerActivate echoAnswerCode = iota + 1
	echoAnswerDone
	echoAnswerEchoPay
	echoAnswerEchoActionPick
)

var echoAnswerCodes = state.NewStrCodes(
	state.StrEntry[echoAnswerCode]{Key: "activate", Val: echoAnswerActivate},
	state.StrEntry[echoAnswerCode]{Key: "done", Val: echoAnswerDone},
	state.StrEntry[echoAnswerCode]{Key: "echo_pay", Val: echoAnswerEchoPay},
	state.StrEntry[echoAnswerCode]{Key: "echo_action_pick", Val: echoAnswerEchoActionPick},
)
