package botpolicy

import (
	"sort"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// This file holds the trigger-order and trigger-optional rules the policy
// answers without any randomness. Before this task (dp1) those two kinds
// were a Fisher-Yates over the bot's rng (trigger_order) and a coin between
// yes and no (trigger_optional): two decisions that injected variance into
// every game and widened the very confidence intervals that make most deck
// pairs read as coin flips. The semantic content the policy needs -- which
// trigger, from what source, carrying what label -- is carried on the
// Decision's own options (each trigger option's Obj is the source permanent
// and its Label is the trigger's own text) and the source's board facts on
// the Board's Cards census, so neither rule needs a new Board field or a
// new plumbing path: it reads the same card-worth arithmetic the cast rule
// reads, from the same facts both adapter halves fill.

// chooseTriggerOrder is the KTriggerOrder ranking: a deterministic
// permutation of the offered trigger indices, ordered by the source card's
// worth (cardWorth — the same creature-power/mana-value arithmetic the cast
// rule reads), most valuable source first, ties on option index. It replaces
// the Fisher-Yates shuffle: the bot no longer randomises the order its
// own simultaneous triggers hit the stack, so the same game state always
// yields the same order (and the same replay), and the ordering reads the
// board facts available rather than gambling.
//
// DIRECTION, stated rather than hidden: Choices[0] is the trigger put on the
// stack first and so resolves last (CR 603.3b). Placing the most valuable
// source first puts it on the stack first and lets it resolve last, so the
// trigger the board says is worth the most sits on the stack longest; with
// the effects unreadable, this is a defensible consistency rule rather than
// a claimed interaction win. A perfect interaction-aware ranking is out of
// reach without reading the triggers' effects; the alternative to this rule
// was leaving a shuffle in place, which the task explicitly excludes.
func (b Board) chooseTriggerOrder(d *decision.Decision) []int {
	n := len(d.Options)
	if n == 0 {
		return nil
	}
	perm := make([]int, n)
	for i, o := range d.Options {
		perm[i] = o.Index
	}
	// perm is a permutation of [0, n) (every option's Index is its
	// position), so sorting it is a permutation of the offered indices --
	// exactly the "n distinct in-range indices" shape Decision.Validate
	// requires for KTriggerOrder. Stable so equal-worth triggers keep the
	// offer order rather than a map order.
	sort.SliceStable(perm, func(i, j int) bool {
		a := b.cardWorth(d.Options[perm[i]].Obj)
		c := b.cardWorth(d.Options[perm[j]].Obj)
		if a != c {
			return a > c // most valuable source first
		}
		return perm[i] < perm[j] // deterministic tie: low index first
	})
	return perm
}

// chooseWorst spends the lowest cast-worth sacrifice/delve candidates.
// Hand retention uses chooseDiscard instead: expensive cards can be liabilities.
func (b Board) chooseWorst(d *decision.Decision) []int {
	return b.chooseLowest(d, b.cardWorth)
}

func (b Board) chooseLowest(d *decision.Decision, worth func(state.ObjID) int32) []int {
	n := len(d.Options)
	if n == 0 {
		return nil
	}
	idx := make([]int, n)
	for i, o := range d.Options {
		idx[i] = o.Index
	}
	sort.Slice(idx, func(i, j int) bool {
		a := worth(d.Options[idx[i]].Obj)
		c := worth(d.Options[idx[j]].Obj)
		if a != c {
			return a < c // least valuable first
		}
		return idx[i] < idx[j] // deterministic tie: low index first
	})
	m := d.Max
	if m < 0 {
		m = 0
	}
	if m > n {
		m = n
	}
	worst := idx[:m]
	sort.Slice(worst, func(i, j int) bool { return worst[i] < worst[j] })
	return worst
}

// declineOptional handles only a resolving api:Effect OptionalDecider$
// election, marked by the engine on its decision. Printed optional triggers
// and Miracle keep their previous production and legacy policies. The
// option's index, not its position, is the answer that Decision.Validate reads.
// If no decline is offered, Clamp handles the fallback as for any other ask.
func declineOptional(d *decision.Decision) []int {
	for _, o := range d.Options {
		if o.Kind == "no" {
			return []int{o.Index}
		}
	}
	return nil
}

// chooseDiscard keeps cheap plays over expensive ones, without the cast rule's
// blanket creature premium. Basic positively identifies basic lands; zero-CMC
// noncreatures are conservatively land-like (also protects free artifacts).
// Castable is zone eligibility, NOT affordability, and includes hand lands on
// both adapters. No land count or future available-mana census exists here:
// this deliberately protects mana development even when the hand is flooded.
func (b Board) chooseDiscard(d *decision.Decision) []int {
	return b.chooseLowest(d, func(id state.ObjID) int32 {
		c := b.Cards.Get(id)
		if c.Basic || (!c.Creature && c.CMC == 0) {
			return 1
		}
		return -c.CMC
	})
}
