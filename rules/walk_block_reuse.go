package rules

import (
	"fmt"
	"slices"

	"github.com/adams-shaun/gorge/cards"

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
// (priorityWalkTailFor) and the walk is full. Both walks visit the same
// objects in the same order (the same state); only the ability blocks that
// read the pricing pool or offered something are recorded, in that order,
// and matched by zone, zone owner and object -- an object with no record
// read no pricing pool and offered nothing. Verify mode (walkCacheVerify)
// recomputes every reusing walk without reuse and panics unless the two are
// identical.
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
	// members / memberSpans: the seat's own battlefield objects' mana
	// membership with the payability gate deferred (the list PotentialMana
	// walks), by zone position, recorded by the mana section
	// (ownManaMembers) for the decision's PotentialMana (potentialMembers).
	members     []*cards.SA
	memberSpans []potentialManaSpan
	// board is the recorded walk's board facts (legal_walk_skip.go), a pure
	// read of the board both later readers take instead of re-deriving.
	board walkBoardFacts
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
	clear(r.members)
	r.p, r.done, r.manaOK, r.board = p, false, false, walkBoardFacts{}
	r.blocks, r.raw, r.members = r.blocks[:0], r.raw[:0], r.members[:0]
	r.memberSpans = resizeCleared(r.memberSpans, len(e.G.Zone(state.ZBattlefield, p)))
	return r
}

// recordMembers records the deferred-payability mana membership of the
// seat's battlefield object at zone position zi (nil: provably none).
func (r *walkBlockRec) recordMembers(zi int, all []*cards.SA) {
	if zi >= len(r.memberSpans) {
		return
	}
	start := len(r.members)
	r.members = append(r.members, all...)
	r.memberSpans[zi] = potentialManaSpan{start: int32(start), end: int32(len(r.members)), walked: true}
}

// ownManaMembers is the mana section's membership walk for the seat's own
// battlefield object id (zone position zi) in a recording walk: the
// membership with the payability gate deferred -- PotentialMana's list,
// recorded for it -- then that gate applied, which is exactly the walk's
// own list (appendAvailableManaAbilitiesGate applies payability as one more
// conjunct of each ability's pure gates). Built into dst.
func (w *legalWalk) ownManaMembers(dst []*cards.SA, zi int, o *state.Object, id state.ObjID) []*cards.SA {
	e, p := w.e, w.p
	all := e.appendAvailableManaAbilitiesGate(dst, &w.actionStatics, p, id, true)
	w.rec.recordMembers(zi, all)
	out := all[:0]
	for _, ma := range all {
		cc := e.compiledCostOf(ma.Params["Cost"])
		if mf := e.manaFactsOf(ma); mf != nil {
			cc = mf.cost
		}
		if e.manaCostPayable(p, o, id, cc, nil) {
			out = append(out, ma)
		}
	}
	if walkCacheVerify {
		if want := e.appendAvailableManaAbilities(nil, &w.actionStatics, p, id); !slices.EqualFunc(want, out, sameManaAbility) {
			panic(fmt.Sprintf("rules: recorded mana membership of %d filtered to %d abilities, the walk's has %d", id, len(out), len(want)))
		}
	}
	return out
}

// potentialMembers serves PotentialMana's membership walk for p's
// battlefield object at zone position zi from the decision's recorded
// priority walk r (ok false: none recorded, walk it).
func (r *walkBlockRec) potentialMembers(p state.PlayerID, zi int) ([]*cards.SA, bool) {
	if r == nil || r.p != p || zi >= len(r.memberSpans) || !r.memberSpans[zi].walked {
		return nil, false
	}
	sp := r.memberSpans[zi]
	return r.members[sp.start:sp.end], true
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
//
// Only a block that read the pricing pool or offered something is
// recorded (recordAbilityBlock): an unrecorded object's block, which read
// no pricing pool and offered nothing, is served as empty. The recorded
// blocks are a subsequence of the walk's, in walk order, matched by zone,
// zone owner and object.
func (w *legalWalk) reuseAbilityBlock(z state.Zone, zp state.PlayerID, id state.ObjID) bool {
	r := w.reuse
	if r == nil {
		return false
	}
	k := w.reuseCursor
	if k >= len(r.blocks) || r.blocks[k].id != id || r.blocks[k].zone != z || r.blocks[k].zp != zp {
		return true // unrecorded: provably empty
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
// output length start with gates pricing reads, when it read the pricing
// pool or offered something (an empty pool-independent block is implied by
// its absence).
func (w *legalWalk) recordAbilityBlock(z state.Zone, zp state.PlayerID, id state.ObjID, start, gates int) {
	if r := w.rec; r != nil {
		if priced := w.gates != gates; priced || len(w.out) > start {
			r.blocks = append(r.blocks, walkBlock{id: id, zone: z, zp: zp,
				start: int32(start), end: int32(len(w.out)), priced: priced})
		}
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

// recordedBoardFacts installs the record's board facts on a walk's
// action-static source (where boardFacts and the mana walk read them),
// checked against a fresh read in verify mode.
func (e *Engine) recordedBoardFacts(r *walkBlockRec, s *actionStaticSource, p state.PlayerID) {
	if r == nil || !r.board.ready {
		return
	}
	if walkCacheVerify {
		fresh := (&legalWalk{e: e, p: p, actionStatics: actionStaticSource{e: e}}).boardFacts()
		if fresh != r.board {
			panic(fmt.Sprintf("rules: recorded walk board facts %+v, a fresh read %+v", r.board, fresh))
		}
	}
	s.board = r.board
}
