// resolution_suspend.go holds Suspended, the engine's "a resolution-time
// payment window is open" test. A resolution on the W3 kernel never suspends
// at an ask (every ask inside it is answered in place from the tape).
package rules

// Suspended reports whether a resolution-time payment window (an unless
// payment, cumulative upkeep, a triggered-effect Cost$ or echo) is open, or
// an off-stack mana resolution is waiting on its colour choice.
func (e *Engine) Suspended() bool {
	if f := e.offStackMana; f != nil {
		return f.suspended(e)
	}
	return e.UnlessPayment != nil || e.cumulative != nil || e.triggerCost != nil || e.echo != nil
}
