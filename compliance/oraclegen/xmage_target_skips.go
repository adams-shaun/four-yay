package oraclegen

import (
	"fmt"

	"github.com/adams-shaun/gorge/rules"
)

// XTargetSkip is one target object of a cast step that XMage's own bounds do
// not close: a completely omitted optional object, or a multi-pick object
// (TargetMin$ m, TargetMax$ M) filled with fewer than M picks. XMage poses
// every target object of a spell in order and only a "[target_skip]" answer
// closes an object short of its maximum, so the driver must queue one at the
// right place between the filled targets. The fixture flattens that position
// away, so the generator keeps it here.
//
// Slot is the index of the closed slot in the card's SlotSpecs. At is the
// number of filled targets that precede the skip: the driver queues the skip
// before the cast step's Targets[At]. Consecutive closed objects repeat At
// (each contributes no target), a leading one has At 0 and a trailing one has
// At == len(Targets).
type XTargetSkip struct {
	At   int `json:"at"`
	Slot int `json:"slot"`
}

// targetSkips derives an Item's XTargetSkips from the cast's slots and
// gorge's own target decisions. It returns nil -- the driver then keeps its
// legacy trailing skip -- unless every slot is accounted for: each filled
// slot posed one target decision whose picks are the cast's next targets, in
// order, and each omitted fixture slot posed none. A filled slot with fewer
// picks than its declared maximum needs a closing skip after its last pick;
// an omitted Min-0 object needs one at its own position. A slot whose maximum
// cannot be read off the text (a repeated or dynamic-bound object) or a cast
// whose decisions differ (a declined slot, a merged 0..N object, a reflexive
// ask) is not guessed at.
func targetSkips(slots []Slot, omitted []int, sc Scenario, ds []rules.OracleDecision, castSteps map[int]bool) [][]XTargetSkip {
	if len(omitted) == len(slots) {
		return nil
	}
	if len(castSteps) != 1 {
		return nil
	}
	step := -1
	for i := range castSteps {
		step = i
	}
	if step < 0 || step >= len(sc.Steps) || sc.Steps[step].Op != "cast" {
		return nil
	}
	targets := sc.Steps[step].Targets
	omittedSet := map[int]bool{}
	for _, o := range omitted {
		omittedSet[o] = true
	}
	var posed []rules.OracleDecision
	for _, d := range ds {
		if d.Via == "target" && d.Step == step {
			posed = append(posed, d)
		}
	}
	skips := make([][]XTargetSkip, len(sc.Steps))
	at, di := 0, 0
	for i, slot := range slots {
		if omittedSet[i] {
			// An omitted fixture slot posed no decision: XMage still asks a
			// Min-0 object even with no legal candidate, and only a skip
			// closes it. A required object could never be omitted by a legal
			// cast.
			if slot.Min > 0 || slot.Max < 1 {
				return nil
			}
			skips[step] = append(skips[step], XTargetSkip{At: at, Slot: i})
			continue
		}
		if di >= len(posed) {
			return nil
		}
		d := posed[di]
		di++
		n := len(d.PickRefs)
		if n == 0 {
			// A declined slot reaches the fixture as an omitted one.
			return nil
		}
		if at+n > len(targets) || !sameTargetRefs(d.PickRefs, targets[at:at+n]) {
			return nil
		}
		at += n
		if slot.Max < 1 {
			return nil
		}
		if n < slot.Max && len(d.OptionRefs) > n {
			// XMage keeps asking a partly filled object until its maximum --
			// but only while candidates remain: an object whose legal set is
			// exhausted completes by itself (TargetImpl.isChoiceCompleted's
			// moreSelectCount == 0). gorge's ask offered the same legal set,
			// so a skip is queued only when gorge offered more candidates
			// than the slot took; a skip on a self-completing object would be
			// left in the queue for the next ask.
			skips[step] = append(skips[step], XTargetSkip{At: at, Slot: i})
		}
	}
	if at != len(targets) || di != len(posed) {
		return nil
	}
	if len(skips[step]) == 0 {
		return nil
	}
	if CheckTargetSkips(slots, sc.Steps, skips) != nil {
		return nil
	}
	return skips
}

// sameTargetRefs reports whether a decision's pick refs are exactly the given
// run of the cast's targets.
func sameTargetRefs(refs, targets []string) bool {
	if len(refs) != len(targets) {
		return false
	}
	for i := range refs {
		if refs[i] != targets[i] {
			return false
		}
	}
	return true
}

// CheckTargetSkips validates an XTargetSkips plan against the card's slots
// and the scenario's steps. A non-empty entry must belong to a cast step; its
// skips must name distinct, ascending slots that XMage can close early (a
// literal bound with Max > Min -- a required 1..1 object is never skipped);
// each At must be non-decreasing and within the step's target count; and the
// plan must leave a possible pick count for every slot.
func CheckTargetSkips(slots []Slot, steps []Step, skips [][]XTargetSkip) error {
	if len(skips) != len(steps) {
		return fmt.Errorf("xmage_target_skips has %d entries for %d steps", len(skips), len(steps))
	}
	for i, list := range skips {
		if len(list) == 0 {
			continue
		}
		if steps[i].Op != "cast" {
			return fmt.Errorf("step %d (%s) carries target skips but is not a cast", i, steps[i].Op)
		}
		prev := -1
		for j, sk := range list {
			if sk.Slot < 0 || sk.Slot >= len(slots) {
				return fmt.Errorf("step %d skip %d names slot %d of %d", i, j, sk.Slot, len(slots))
			}
			if sk.Slot <= prev {
				return fmt.Errorf("step %d skip %d slot %d does not follow slot %d", i, j, sk.Slot, prev)
			}
			slot := slots[sk.Slot]
			if slot.Max < 1 || slot.Min >= slot.Max {
				return fmt.Errorf("step %d skip %d names slot %d (%s), which XMage cannot close early", i, j, sk.Slot, slot.Filter)
			}
			lo, hi := slot.Min, slot.Max-1
			for k := 0; k < sk.Slot; k++ {
				if skipSlot(list, k) {
					lo += slots[k].Min
					hi += slots[k].Max - 1
					continue
				}
				lo += slots[k].Max
				hi += slots[k].Max
			}
			if sk.At < lo || sk.At > hi {
				return fmt.Errorf("step %d skip %d at %d, outside the %d..%d targets its own and earlier slots admit", i, j, sk.At, lo, hi)
			}
			prev = sk.Slot
		}
		base, lo, hi := 0, 0, 0
		for j, slot := range slots {
			if skipSlot(list, j) {
				lo += slot.Min
				hi += slot.Max - 1
				continue
			}
			base += slot.Max
		}
		lo, hi = base+lo, base+hi
		if n := len(steps[i].Targets); n < lo || n > hi {
			return fmt.Errorf("step %d has %d targets, outside the %d..%d its slots and skips admit", i, n, lo, hi)
		}
	}
	return nil
}

// skipSlot reports whether the plan closes slot k.
func skipSlot(list []XTargetSkip, k int) bool {
	for _, sk := range list {
		if sk.Slot == k {
			return true
		}
	}
	return false
}
