package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// manaMemberCarry caches, per own-battlefield object, the deferred-payability
// mana-ability membership across a seat's priority walks (legal-walk design
// §S4). Payability is never cached: the walk re-applies it every time.
//
// Validity: the board stamp at the last write, plus the object's own touch
// generation. A board-stamp move retires every entry through gen, so an entry
// stored before the move can never satisfy a lookup after it.
type manaMemberBoardStamp struct {
	lineage           *events.Log
	derivedSeq        uint64
	staticTouchGen    uint64
	crossWalkRetires  uint64
	continuousVersion int
	tapeEpoch         uint64
	objs              int
	turn              int32
}

type manaMemberEntry struct {
	gen      uint64
	objTouch uint64
	all      []*cards.SA
	n        int32
	set      bool
}

type manaMemberCarry struct {
	owner    *Engine
	entries  []manaMemberEntry
	touch    []uint64
	stamp    manaMemberBoardStamp
	stampSet bool
	gen      uint64
	hits     uint64
	misses   uint64
}

// sync moves the carry to cur, retiring every older entry by bumping gen.
func (c *manaMemberCarry) sync(cur manaMemberBoardStamp) {
	if !c.stampSet || c.stamp != cur {
		c.gen++
		c.stamp, c.stampSet = cur, true
	}
}

func (c *manaMemberCarry) entryFor(i int) *manaMemberEntry {
	if i < 0 {
		return nil
	}
	if i >= len(c.entries) {
		grown := make([]manaMemberEntry, i+1, i+1+i/2+8)
		copy(grown, c.entries)
		c.entries = grown
	}
	return &c.entries[i]
}

// lookup returns id's cached membership when its entry is live at (cur, touch).
func (c *manaMemberCarry) lookup(id state.ObjID, cur manaMemberBoardStamp, touch uint64) ([]*cards.SA, bool) {
	if id == 0 {
		return nil, false
	}
	c.sync(cur)
	i := int(id) - 1
	if i >= len(c.entries) {
		c.misses++
		return nil, false
	}
	en := &c.entries[i]
	if !en.set || en.gen != c.gen || en.objTouch != touch {
		c.misses++
		return nil, false
	}
	c.hits++
	return en.all[:en.n], true
}

// store records id's deferred membership under (cur, touch).
func (c *manaMemberCarry) store(id state.ObjID, all []*cards.SA, cur manaMemberBoardStamp, touch uint64) {
	if id == 0 {
		return
	}
	c.sync(cur)
	en := c.entryFor(int(id) - 1)
	if en == nil {
		return
	}
	en.all = append(en.all[:0], all...)
	en.n = int32(len(en.all))
	en.gen, en.objTouch, en.set = c.gen, touch, true
}

// touchObj bumps the per-object generation at index i (id i+1).
func (c *manaMemberCarry) touchObj(i int) {
	if i < 0 {
		return
	}
	if i >= len(c.touch) {
		grown := make([]uint64, i+1, i+1+i/2+8)
		copy(grown, c.touch)
		c.touch = grown
	}
	c.touch[i]++
}

// manaBoardStamp is the derived memo's cross-walk key plus the mana-specific
// counters. tapeEpoch covers a kernel restore that rewinds state under the
// same log (rules/board_read_key.go).
func (e *Engine) manaBoardStamp() manaMemberBoardStamp {
	return manaMemberBoardStamp{
		lineage: e.L, derivedSeq: e.derivedSeq, staticTouchGen: e.staticTouchGen,
		crossWalkRetires: e.crossWalkRetires, continuousVersion: e.continuousVersion,
		tapeEpoch: uint64(e.tapeEpoch), objs: len(e.G.Objs), turn: e.G.Turn,
	}
}

func (e *Engine) manaTouchOf(id state.ObjID) uint64 {
	if id == 0 {
		return 0
	}
	if i := int(id) - 1; i >= 0 && i < len(e.manaCarry.touch) {
		return e.manaCarry.touch[i]
	}
	return 0
}

// ownManaCarry gives a by-value Engine copy its own carry, so it never writes
// the original's arrays (the walkObjCls pattern).
func (e *Engine) ownManaCarry() {
	if e.manaCarry.owner != e {
		e.manaCarry = manaMemberCarry{owner: e}
	}
}

// manaTouchBumpIdx owns the carry then bumps object id i+1's generation. The
// owner guard is mandatory: a by-value Engine copy shares the carry's backing
// arrays while manaCarry.owner still points at the original, so a bare
// touch[i]++ would corrupt the original's carry (the ownWalkClasses reason).
func (e *Engine) manaTouchBumpIdx(i int) {
	e.ownManaCarry()
	e.manaCarry.touchObj(i)
}

// manaTouchBumpAll owns the carry then bumps every object's generation -- the
// per-object twin of walkClassDropAll's staticTouchGen bump.
func (e *Engine) manaTouchBumpAll() {
	e.ownManaCarry()
	for i := range e.manaCarry.touch {
		e.manaCarry.touch[i]++
	}
}

func (e *Engine) manaMemberLookup(id state.ObjID) ([]*cards.SA, bool) {
	e.ownManaCarry()
	return e.manaCarry.lookup(id, e.manaBoardStamp(), e.manaTouchOf(id))
}

func (e *Engine) manaMemberStore(id state.ObjID, all []*cards.SA) {
	e.ownManaCarry()
	e.manaCarry.store(id, all, e.manaBoardStamp(), e.manaTouchOf(id))
}

// ManaCarryStats is a test-visible diagnostic.
func (e *Engine) ManaCarryStats() (hits, misses uint64) {
	return e.manaCarry.hits, e.manaCarry.misses
}
