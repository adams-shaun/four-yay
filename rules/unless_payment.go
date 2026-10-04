package rules

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
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

// beginUnlessPayment begins a payer-selected payment (pay.BeginUnless) and
// finishes it when it settles without an ask.
func (e *Engine) beginUnlessPayment(payer state.PlayerID, cost Cost, ctx *effects.Ctx, stackObj state.ObjID) {
	e.settleUnlessStep(pay.BeginUnless(asPayer(e), payer, cost, ctx, stackObj))
}

// advanceUnlessPayment runs the in-progress unless payment (pay.AdvanceUnless)
// and finishes it when it settles.
func (e *Engine) advanceUnlessPayment() {
	e.settleUnlessStep(pay.AdvanceUnless(asPayer(e)))
}

// settleUnlessStep finishes a settled unless payment: a paid one first draws
// its Draw<N/Spec> parts (a draw evaluates through the engine as a Host),
// then the payment's owner resumes (finishUnlessPayment). A step that posed
// an ask does nothing: the answer's handler continues it.
func (e *Engine) settleUnlessStep(st pay.UnlessStep) {
	if !st.Done {
		return
	}
	if st.Paid {
		u := e.UnlessPayment
		for i, part := range u.Cost.Draw {
			for _, p := range st.Draws[i] {
				for n := int32(0); n < part.N; n++ {
					effects.DrawFor(e, p)
				}
			}
		}
	}
	e.finishUnlessPayment(st.Paid)
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

func (e *Engine) answerUnlessPayment(chosen []decision.Option) {
	if u := e.UnlessPayment; u == nil || u.Part >= u.PaymentPartCount() {
		return
	}
	e.choosing = chooseNone
	st, _ := pay.AnswerUnlessPayment(asPayer(e), chosen)
	e.settleUnlessStep(st)
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
