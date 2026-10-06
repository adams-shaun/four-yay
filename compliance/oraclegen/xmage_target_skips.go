package oraclegen

import (
	"fmt"

	"github.com/adams-shaun/gorge/rules"
)

// XTargetSkip is one completely omitted optional XMage target object of a
// cast step. XMage poses every target object of a spell in order -- an empty
// 0..1 object too -- and only a "[target_skip]" answer closes it, so the
// driver must queue one at the right place between the filled targets. The
// fixture flattens that position away, so the generator keeps it here.
//
// Slot is the index of the omitted slot in the card's SlotSpecs; in the
// supported shape (every slot an independent literal 0..1 object, no stack
// slot) it is also the index of the XMage target object. At is the number of
// filled targets that precede the object: the driver queues the skip before
// the cast step's Targets[At]. Consecutive omitted objects repeat At, a
// leading one has At 0 and a trailing one has At == len(Targets).
type XTargetSkip struct {
	At   int `json:"at"`
	Slot int `json:"slot"`
}

// targetSkips derives an Item's XTargetSkips from the fixture's omitted
// slots. It returns nil -- the driver then keeps its legacy trailing skip --
// unless the cast is the supported shape AND gorge's own decisions confirm
// it: every filled slot posed one 0..1 target decision with one pick, in slot
// order, so the omitted slot's position in the final Targets is the slot
// index less the omitted slots before it. A cast whose decisions differ (a
// declined slot, a merged 0..N object, a reflexive ask) is not guessed at.
func targetSkips(slots []Slot, omitted []int, sc Scenario, ds []rules.OracleDecision, castSteps map[int]bool) [][]XTargetSkip {
	if len(omitted) == 0 || len(castSteps) != 1 {
		return nil
	}
	step := -1
	for i := range castSteps {
		step = i
	}
	if step < 0 || step >= len(sc.Steps) || sc.Steps[step].Op != "cast" {
		return nil
	}
	filled := len(slots) - len(omitted)
	if filled < 1 || len(sc.Steps[step].Targets) != filled {
		return nil
	}
	posed := 0
	for _, d := range ds {
		if d.Via != "target" || d.Step != step {
			continue
		}
		if posed >= filled || d.Min != 0 || d.Max != 1 || len(d.PickRefs) != 1 || d.PickRefs[0] != sc.Steps[step].Targets[posed] {
			return nil
		}
		posed++
	}
	if posed != filled {
		return nil
	}
	skips := make([][]XTargetSkip, len(sc.Steps))
	for j, slot := range omitted {
		skips[step] = append(skips[step], XTargetSkip{At: slot - j, Slot: slot})
	}
	if CheckTargetSkips(slots, sc.Steps, skips) != nil {
		return nil
	}
	return skips
}

// CheckTargetSkips validates an XTargetSkips plan against the card's slots
// and the scenario's steps. A non-empty entry must belong to a cast step; its
// skips must name distinct, ascending slots that are independent literal 0..1
// optional objects (a required or "up to N" slot is never skipped); each At
// must equal the number of filled targets before the slot; and filled plus
// omitted must account for every slot, so no object is left to a blind skip.
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
		for j, s := range slots {
			if !s.Optional || !s.ZeroOrOne {
				return fmt.Errorf("step %d slot %d (%s) is not an independent optional 0..1 object", i, j, s.Filter)
			}
		}
		prev := -1
		for j, sk := range list {
			if sk.Slot < 0 || sk.Slot >= len(slots) {
				return fmt.Errorf("step %d skip %d names slot %d of %d", i, j, sk.Slot, len(slots))
			}
			if sk.Slot <= prev {
				return fmt.Errorf("step %d skip %d slot %d does not follow slot %d", i, j, sk.Slot, prev)
			}
			prev = sk.Slot
			if want := sk.Slot - j; sk.At != want {
				return fmt.Errorf("step %d skip %d at %d, but %d filled targets precede slot %d", i, j, sk.At, want, sk.Slot)
			}
		}
		if n := len(steps[i].Targets); n+len(list) != len(slots) {
			return fmt.Errorf("step %d has %d targets and %d skips for %d slots", i, n, len(list), len(slots))
		}
	}
	return nil
}
