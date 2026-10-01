package botpolicy

import "github.com/adams-shaun/gorge/decision"

// chooseReplacementOrder answers a CR 616.1 KReplacement order decision: it
// ranks the real "replacement" options by their source permanent's standing
// worth (cardWorth, the same feature dot the trigger-order arm reads),
// descending, and returns the winning option's Index. The offered index is
// the deterministic tie-break (the scan keeps the first option on an equal
// worth), and the option list is already in the engine's deterministic scan
// order, never a map iteration.
//
// It returns -1 when no option is a real replacement, so every other
// KReplacement shape -- a colour-valued "mana" pick, or an optional
// "apply"/"decline" -- keeps Decide's ordinary fallback rather than being
// answered by a ranking that does not describe it. A "skip_replacement"
// opt-out is therefore bypassed whenever at least one real replacement is
// offered: declining the whole competition is the answer only when nothing
// else can apply.
func (b Board) chooseReplacementOrder(d *decision.Decision) int {
	best := -1
	var bestWorth int32
	for oi := range d.Options {
		o := &d.Options[oi]
		if o.Kind != "replacement" {
			continue
		}
		w := b.cardWorth(o.Obj)
		if best < 0 || w > bestWorth {
			best, bestWorth = o.Index, w
		}
	}
	return best
}
