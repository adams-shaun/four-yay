package rules

// unless_tape.go is the one home of an answered UnlessCost$ election's
// settlement on the resolution kernel's tape path (W3 batch d, lasagna spec
// §7.7: the unless-pay window). The CR 601.2g mana window runs in line as a
// multi-intent answer -- one served intent per source activated, then Done --
// and so does the choice-bearing component continuation.

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// unlessSettle is what an answered unless election needs next.
type unlessSettle uint8

const (
	// unlessSettled: ctx.UnlessPay holds the outcome.
	unlessSettled unlessSettle = iota
	// unlessOpenWindow: the mana-only cost needs the CR 601.2g activation
	// window before it is charged.
	unlessOpenWindow
	// unlessPayComponents: the choice-bearing payment continuation
	// (beginUnlessPayment) assembles the cost.
	unlessPayComponents
)

// settleUnlessElection applies an answered generic unless_pay election (not
// Ward's, which owns its payment forms) for the resolving object obj. The payer chose the explicitly marked pay option
// or declined. Payment happens HERE, in rules, because payMana owns the cost
// grammar and emits the ManaAdd events -- so a replay re-derives the
// identical payment. An answer to pay from a payer that cannot cover the
// cost is a decline: the effect's body runs (or not) per its orientation.
// On unlessSettled ctx.UnlessPay is set; otherwise payer and cost name the
// payment the window or the continuation must assemble.
func settleUnlessElection(e *Engine, ctx *effects.Ctx, sa *cards.SA, obj state.ObjID, chosen []decision.Option) (unlessSettle, state.PlayerID, Cost) {
	payOption, chosePay := unlessPayChoice(chosen)
	// Sacrifice's damage-payment offer (Vexing Devil's UnlessCost$
	// DamageYou<4>, UnlessPayer$ Opponent, UnlessSwitched$ True): "paying" is
	// TAKING THE DAMAGE, which the mana path below cannot express --
	// ParseCost silently substitutes an unknown spelling for a flat {1} and
	// would charge one floating mana for four damage. The accepting
	// opponent's Damage event is emitted from the offering permanent, and
	// effSacrifice, reading the answered UnlessPay, then sacrifices (the
	// switched orientation: paying CAUSES the sacrifice). A plain-mana
	// UnlessCost$ (the echo / cumulative-upkeep family) falls through to the
	// shared mana path below, exactly like a Counter's: paid spares the
	// permanent, decline sacrifices it.
	if sa.API == "Sacrifice" {
		if n, dmg := effects.ParseDamageUnlessCost(sa.ParamStr(cards.PKUnlessCost)); dmg {
			ctx.UnlessPay = "decline"
			if chosePay {
				e.payUnlessDamageCost(ctx, payOption.Player, n)
				ctx.UnlessPay = "pay"
			}
			return unlessSettled, payOption.Player, Cost{}
		}
	}
	// UnlessCostResolved first: an UnlessCost$ naming an SVar whose count
	// body resolves folds its numeric result into a generic amount (Feather,
	// Radiant Arbiter's SVar:CopyCost:Count$ChosenSize/Times.2 -- "{2} for
	// each of those creatures"), the same string unlessProceed's ask label
	// showed, so the offer and the charge can never disagree. An SVar the
	// ctx's table lacks or whose body does not resolve passes through raw and
	// lands in the same hard decline as before.
	raw := effects.UnlessCostResolved(e, ctx, sa)
	paid, ok := ParseUnlessCost(raw)
	ctx.UnlessPay = "decline"
	if !ok {
		// I-5: an unless-cost the payment API cannot price is a hard
		// DECLINE. ParseCost("X") is {Generic:0, X:1}; payMana never charges
		// the unfolded X, so an empty pool "pays" it for free and the
		// counterspell stays inert. The named dynamic shapes close against
		// the resolution context (the announced X, the resolvable SVar
		// bodies, the DefinedCost_ card-anchored mana values, the energy
		// parts, the Return<N/Spec> choice parts and LifeTotalHalfUp);
		// ParseUnlessCost is still the strict parser, so every remaining
		// dynamic or unmodelled token declines here rather than ParseCost's
		// flat {1} substitution buying it for one generic. The ask is still
		// posed to the payer (the answer is recorded by ModeChosen) but
		// cannot succeed, which keeps the decision on the wire for hosts to
		// observe while never letting an empty pool satisfy it.
		return unlessSettled, payOption.Player, Cost{}
	}
	if !chosePay || !pay.UnlessCostPayable(asPayer(e), payOption.Player, raw, ctx, obj) {
		return unlessSettled, payOption.Player, Cost{}
	}
	if len(paid.Sac) > 0 || len(paid.Discard) > 0 || len(paid.Reveal) > 0 || len(paid.Behold) > 0 ||
		len(paid.RevealChosen) > 0 || len(paid.Return) > 0 || len(paid.Exile) > 0 {
		// Sacrifice, discard, reveal, behold, return and exile are
		// choice-bearing costs: the payer selects every component before
		// anything is charged.
		ctx.UnlessPay = ""
		return unlessPayComponents, payOption.Player, paid
	}
	// CR 601.2g: a mana-only unless cost gives the payer the same chance to
	// activate mana abilities before the charge as a cast or a Ward does, so
	// a converted colour (stat:ManaConvert) can be produced by tapping. The
	// window only opens when the pool (under the payment's conversion)
	// cannot already pay and an untapped source exists; otherwise the
	// charge below is unchanged.
	if e.unlessManaWindowNeeded(payOption.Player, paid, obj) {
		ctx.UnlessPay = ""
		return unlessOpenWindow, payOption.Player, paid
	}
	if e.payUnlessCost(payOption.Player, paid, ctx, obj) {
		ctx.UnlessPay = "pay"
	} else if paid.HasManaPayment() && len(e.windowManaUnits(payOption.Player)) > 0 {
		// A failed pool-only attempt is not a decline: the continuation lets
		// the payer assemble enough floating mana. The offer gate proved the
		// budget reachable before Pay was offered, so sources remain while
		// the charge is unmet.
		ctx.UnlessPay = ""
		return unlessPayComponents, payOption.Player, paid
	}
	return unlessSettled, payOption.Player, paid
}

// unlessAnswerSettle is the "unless_pay" answer record (tapeAnswerRecord):
// the tape-served answer to the UnlessCost$ election effects' poseUnlessAsk
// posed settles here, into the asking walk's live Ctx (the chain
// effects.Resolve published, whose gate then reads Ctx.UnlessPay and
// Ctx.UnlessNext). The CR 601.2g mana window runs in line
// (tapeUnlessWindow), and so does the choice-bearing component
// continuation (tapeUnlessComponents). Ward's election settles through
// wardAnswerSettle.
func unlessAnswerSettle(e *Engine, d *decision.Decision, chosen []decision.Option) {
	ctx, sa := e.resolutionCtx, d.ResumeSA
	if ctx == nil || sa == nil {
		panic("rules: a tape-served unless election outside a resolution chain")
	}
	obj := ctx.Source
	if n := len(e.G.Stack); n > 0 {
		obj = e.G.Stack[n-1] // Engine.Ask's resume object: the resolving one
	}
	if sa.API == "Ward" {
		wardAnswerSettle(e, ctx, sa, obj, chosen)
		return
	}
	next, payer, cost := settleUnlessElection(e, ctx, sa, obj, chosen)
	switch next {
	case unlessOpenWindow:
		tapeUnlessWindow(e, ctx, sa, obj, payer, cost, d.ResumeTarget)
	case unlessPayComponents:
		tapeUnlessComponents(e, ctx, payer, cost, obj)
	}
	ctx.UnlessNext = d.ResumeTarget
}

// tapeUnlessWindow is the unless-cost mana window as a multi-intent tape
// answer: the window decision askWardMana poses is asked again after every
// activation, each answer one intent, until Done charges the cost -- the
// same decisions, answers and events as the legacy window's park, resume
// (answerWardMana) and re-ask (continueWardMana). A mana ability that asks
// reaches the engine's ask, which ends the tape run there (the kernel's
// legacy replay).
func tapeUnlessWindow(e *Engine, ctx *effects.Ctx, sa *cards.SA, obj state.ObjID, payer state.PlayerID, cost Cost, target int) {
	tapeManaWindow(e, ctx, &wardManaPayment{payer: payer, cost: cost, obj: obj, sa: sa, target: target,
		resumeKind: unlessManaKind, prompt: unlessManaPrompt, unless: true})
}

// tapeManaWindow runs wm's mana window in line (the unless window above, and
// Ward's CR 702.21a window, askWardMana inside a tape run): Done charges the
// cost -- the unless cost's own payment, or Ward's plain mana charge -- the
// legacy answerWardMana's exact arms.
func tapeManaWindow(e *Engine, ctx *effects.Ctx, wm *wardManaPayment) {
	for {
		d := wardManaDecision(e, wm)
		in, ok := e.TapeAnswer(d)
		if !ok {
			tapeUnservable(e, "mana window")
		}
		chosen := d.Chosen(in)
		if len(chosen) == 1 && chosen[0].Kind == "done" {
			paid := false
			if wm.unless {
				paid = e.payUnlessCost(wm.payer, wm.cost, ctx, wm.obj)
			} else {
				paid = pay.PayMana(asPayer(e), wm.payer, wm.cost)
			}
			ctx.UnlessPay = "decline"
			if paid {
				ctx.UnlessPay = "pay"
			}
			return
		}
		if len(chosen) != 1 || chosen[0].Kind != "activate" || !e.untappedManaSource(wm.payer, chosen[0].Obj) {
			ctx.UnlessPay = "decline"
			return
		}
		e.activateManaPayment(wm.payer, chosen[0].Obj, false)
	}
}

// tapeUnservable hands a tape run back to legacy at a served answer's step
// the kernel cannot serve yet, reporting it to the legacy-ask census as an
// abort of the named step.
func tapeUnservable(e *Engine, step string) {
	if f := tapeLegacyObserver.Load(); f != nil {
		(*f)("abort  unservable " + step + "  [" + tapeShape(e) + "]")
	}
	e.tape.Unservable()
}

// wardAnswerSettle is the tape-served Ward election's settlement, the legacy
// "unless_pay" arm's Ward branch: a payment that needs no further ask (a
// floating-mana charge, poison counters, a random discard) settles in line;
// one that asks (an object pick, the CR 702.21a mana window) reaches the
// engine's ask choke point as a legacy ask after a tape ask, which aborts the
// run and hands the resolution back to legacy.
func wardAnswerSettle(e *Engine, ctx *effects.Ctx, sa *cards.SA, obj state.ObjID, chosen []decision.Option) {
	ctx.UnlessPay = "decline"
	if _, chosePay := unlessPayChoice(chosen); !chosePay {
		return
	}
	// asked: the CR 702.21a mana window ran in line (askWardMana) and set
	// ctx.UnlessPay itself.
	if paid, _ := e.beginWardPayment(&resumePoint{kind: "unless_pay", obj: obj, sa: sa}, ctx); paid {
		ctx.UnlessPay = "pay"
	}
}

// tapeUnlessComponents is the choice-bearing unless payment continuation
// (beginUnlessPayment) driven in line: the holder is the legacy one, marked
// tape, so its component picks and mana window ask through windowAsk and are
// served from the tape, and finishUnlessPayment settles into the live Ctx
// (tapeUnlessSettled) instead of resuming a parked frame.
func tapeUnlessComponents(e *Engine, ctx *effects.Ctx, payer state.PlayerID, cost Cost, obj state.ObjID) {
	cost, ok := pay.UnlessFoldDynamic(asPayer(e), payer, cost, ctx)
	e.unlessPayment = &unlessPayment{payer: payer, ctx: pay.CloneUnlessCtx(*ctx), stackObj: obj, tape: true}
	if !ok {
		e.finishUnlessPayment(false)
		return
	}
	e.unlessPayment.cost = cost
	e.advanceUnlessPayment()
}

// tapeUnlessSettled is finishUnlessPayment for a tape-driven payment: the
// fields the effects-side unless gate reads (Ctx.UnlessPay, the settled
// Discard picks as Ctx.UnlessDiscarded) written straight into the asking
// walk's Ctx.
func tapeUnlessSettled(e *Engine, u *unlessPayment, paid bool) {
	ctx := e.resolutionCtx
	if ctx == nil {
		panic("rules: a tape-driven unless payment outside a resolution chain")
	}
	ctx.UnlessPay = "decline"
	if paid {
		ctx.UnlessPay = "pay"
		if len(u.discards) > 0 {
			ctx.UnlessDiscarded = make([]state.Target, 0, len(u.discards))
			for _, id := range u.discards {
				ctx.UnlessDiscarded = append(ctx.UnlessDiscarded, state.Target{Obj: id})
			}
		}
	}
}
