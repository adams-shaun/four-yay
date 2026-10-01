package rules

import (
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Pool-independent block reuse between a posed priority decision's offer
// walk and its potential walk.
//
// The potential walk (potential_walk_cache.go) is the priority decision's
// own offer walk run again with one difference: its pricing pool is the
// PotentialMana bound instead of the floating pool. Every read of the
// pricing pool goes through legalWalk.pricing (the mana-pricing gates:
// offerCastable, offerCastableAsFace, affordable, the turn-face-up and
// specialize pricing), which counts it. Everything else the walk reads is the
// state, identical for both walks at one stamp (priorityWalkTail's argument).
//
// So a BLOCK of the walk -- a run of code over a fixed slice of the state
// that appends options and reads nothing from outside the walk but the
// state -- that read no pricing pool in the priority walk follows the same
// path in the potential walk: up to its first pricing read both walks
// execute identically, and it has none. Its options are therefore the
// recorded ones, with only their running Index renumbered.
//
// Two blocks of the battlefield section are recorded:
//
//   - the mana-activation section as a whole (it prices nothing: each
//     object's mana abilities are gated by the real pool, never the pricing
//     one);
//   - each object's printed and keyword-granted ability block, in walk order.
//     A block that read the pricing pool (an ability with a cost to price)
//     is walked again; a block that did not is served.
//
// Recording runs only in the priority walk (askPriority's forAsk walk, real
// pool, full) on an engine whose potential readers have asked for the full
// walk (potentialFullDemand), and is promoted to usable only when
// notePriorityWalk records that walk as the decision's tail. A potential
// walk uses it only when the tail is still exact for its seat
// (priorityWalkTailFor) and the walk is full. Blocks are matched by walk
// order and checked by zone, zone owner and object; any mismatch stops the
// reuse for the rest of the walk. Verify mode (walkCacheVerify) recomputes
// every reusing walk without reuse and panics unless the two are identical.
type walkBlockRec struct {
	owner *Engine
	p     state.PlayerID
	// done marks a completed recording not yet promoted by
	// notePriorityWalk.
	done bool
	// manaOK, manaStart, manaEnd: the mana section's span of raw.
	manaOK             bool
	manaStart, manaEnd int32
	blocks             []walkBlock
	// raw is the recorded walk's options before the post-walk filters.
	raw []decision.Option
}

// walkBlock is one object's ability block: its span of raw, and whether it
// read the pricing pool.
type walkBlock struct {
	id         state.ObjID
	zone       state.Zone
	zp         state.PlayerID
	start, end int32
	priced     bool
}

// walkBlockRecorder returns e's recording storage, reset for a recording of
// p's walk (a by-value Engine copy gets its own).
func (e *Engine) walkBlockRecorder(p state.PlayerID) *walkBlockRec {
	r := &e.walkRec
	if r.owner != e {
		*r = walkBlockRec{owner: e}
	}
	clear(r.raw)
	r.p, r.done, r.manaOK = p, false, false
	r.blocks, r.raw = r.blocks[:0], r.raw[:0]
	return r
}

// finishWalkRecord completes a recording with the walk's raw options.
func (r *walkBlockRec) finish(out []decision.Option) {
	r.raw = append(r.raw[:0], out...)
	r.done = true
}

// reuseManaSection serves the mana section from the recording; false walks
// it.
func (w *legalWalk) reuseManaSection() bool {
	r := w.reuse
	if r == nil || !r.manaOK {
		return false
	}
	w.appendRecorded(r, r.manaStart, r.manaEnd)
	return true
}

// recordManaSection records the mana section's span [start, len(out)).
func (w *legalWalk) recordManaSection(start int) {
	if r := w.rec; r != nil {
		r.manaOK, r.manaStart, r.manaEnd = true, int32(start), int32(len(w.out))
	}
}

// reuseAbilityBlock serves object id's ability block from the recording
// when that block read no pricing pool; false walks it (and a gated block
// is walked).
func (w *legalWalk) reuseAbilityBlock(z state.Zone, zp state.PlayerID, id state.ObjID) bool {
	r := w.reuse
	if r == nil {
		return false
	}
	k := w.reuseCursor
	if k >= len(r.blocks) || r.blocks[k].id != id || r.blocks[k].zone != z || r.blocks[k].zp != zp {
		if walkCacheVerify {
			panic(fmt.Sprintf("rules: walk block %d (obj %d zone %d) does not match the recorded walk", k, id, z))
		}
		w.reuse = nil
		return false
	}
	w.reuseCursor++
	b := r.blocks[k]
	if b.priced {
		return false
	}
	w.appendRecorded(r, b.start, b.end)
	return true
}

// recordAbilityBlock records object id's ability block, which began at
// output length start with gates pricing reads.
func (w *legalWalk) recordAbilityBlock(z state.Zone, zp state.PlayerID, id state.ObjID, start, gates int) {
	if r := w.rec; r != nil {
		r.blocks = append(r.blocks, walkBlock{id: id, zone: z, zp: zp,
			start: int32(start), end: int32(len(w.out)), priced: w.gates != gates})
	}
}

// appendRecorded appends the recorded options [start, end) with their
// running Index renumbered.
func (w *legalWalk) appendRecorded(r *walkBlockRec, start, end int32) {
	w.e.walkBlocksServed++
	for _, o := range r.raw[start:end] {
		o.Index = len(w.out)
		w.out = append(w.out, o)
	}
}
