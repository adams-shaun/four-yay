// resolution_suspend.go holds Suspended, the engine's "a resolution-time
// payment window is open" test, and the effects.Host Suspend* reports, which
// are no-ops: a resolution on the W3 kernel never suspends at an ask (every
// ask inside it is answered in place from the tape), so there is no
// continuation to record.
package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
)

// Suspended reports whether a resolution-time payment window (an unless
// payment, cumulative upkeep, a triggered-effect Cost$ or echo) is open, or
// an off-stack mana resolution is waiting on its colour choice.
func (e *Engine) Suspended() bool {
	if f := e.offStackMana; f != nil {
		return f.suspended(e)
	}
	return e.unlessPayment != nil || e.cumulative != nil || e.triggerCost != nil || e.echo != nil
}

func (e *Engine) SuspendUnless(sa *cards.SA, paid bool)                                 {}
func (e *Engine) SuspendContinuation(sa *cards.SA)                                      {}
func (e *Engine) SuspendRepeatBody(sa *cards.SA, next, count int32)                     {}
func (e *Engine) SuspendRepeat(s effects.RepeatSuspension)                              {}
func (e *Engine) SuspendCharmRest(sa *cards.SA, rest []string)                          {}
func (e *Engine) SuspendVillainousRest(sa *cards.SA, rest effects.VillainousRest)       {}
func (e *Engine) SuspendGenericChoiceRest(sa *cards.SA, rest effects.GenericChoiceRest) {}
func (e *Engine) SuspendFlipRest(sa *cards.SA, rest effects.FlipRest)                   {}
