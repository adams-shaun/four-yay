package rules

import (
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
)

// cloneRemap is the identity-preserving half of Clone: the mutable objects a
// suspended resolution reaches through MORE THAN ONE reference -- a
// resolution's coin-flip and ExchangeLife rider memories (shared by every
// resume frame of that resolution and by its parked ExchangeLife
// transaction), the transaction itself (on pendingLifeExchange and on a
// parked life-replacement choice), and resume frames a parked choice
// remembers by pointer (replChoice.resumeAtPose, compared against
// Engine.resume by identity) -- are each copied exactly once per clone, and
// every reference in the clone is re-pointed at that one copy.
//
// So the clone owns its memories (a search clone's hypothetical flip or
// exchange settle can never write into the live game's), and within the
// clone every frame still shares one memory, exactly as within the original.
//
// It lives on cloneWith's stack and allocates nothing until there is a
// pointer to remap: the tables are fixed arrays with an overflow slice, and
// the common clone (no suspended resolution) never touches them.
type cloneRemap struct {
	flips   ptrRemap[effects.FlipMemory]
	exchs   ptrRemap[effects.ExchangeMemory]
	txs     ptrRemap[lifeExchangeTransaction]
}

// ptrRemap maps an original pointer to its clone's copy (linear search: a
// suspended resolution has a handful of these at most).
type ptrRemap[T any] struct {
	n        int
	from, to [4]*T
	more     []*T // overflow, as from,to pairs
}

func (m *ptrRemap[T]) find(p *T) *T {
	for i := 0; i < m.n; i++ {
		if m.from[i] == p {
			return m.to[i]
		}
	}
	for i := 0; i+1 < len(m.more); i += 2 {
		if m.more[i] == p {
			return m.more[i+1]
		}
	}
	return nil
}

func (m *ptrRemap[T]) add(from, to *T) {
	if m.n < len(m.from) {
		m.from[m.n], m.to[m.n] = from, to
		m.n++
		return
	}
	m.more = append(m.more, from, to)
}

// flipMemory returns the clone's copy of p. A nil remap is an intra-engine
// copy (a continuation frame of the same resolution), which must keep
// sharing the pointer.
func (m *cloneRemap) flipMemory(p *effects.FlipMemory) *effects.FlipMemory {
	if p == nil || m == nil {
		return p
	}
	if q := m.flips.find(p); q != nil {
		return q
	}
	q := new(effects.FlipMemory)
	*q = *p
	q.Results = append([]effects.FlipResult(nil), p.Results...)
	m.flips.add(p, q)
	return q
}

// exchangeMemory is flipMemory for the ExchangeLife rider memory.
func (m *cloneRemap) exchangeMemory(p *effects.ExchangeMemory) *effects.ExchangeMemory {
	if p == nil || m == nil {
		return p
	}
	if q := m.exchs.find(p); q != nil {
		return q
	}
	q := new(effects.ExchangeMemory)
	*q = *p
	m.exchs.add(p, q)
	return q
}

// lifeExchange returns the clone's copy of a parked ExchangeLife
// transaction: its staged sides are re-allocated and its rider memory is the
// clone's one copy (the same one the clone's resume frames carry).
func (m *cloneRemap) lifeExchange(tx *lifeExchangeTransaction) *lifeExchangeTransaction {
	if tx == nil {
		return nil
	}
	if q := m.txs.find(tx); q != nil {
		return q
	}
	q := new(lifeExchangeTransaction)
	*q = *tx
	q.staged = append([]events.Event(nil), tx.staged...)
	q.rememberMemory = m.exchangeMemory(tx.rememberMemory)
	m.txs.add(tx, q)
	return q
}

// unlessCtx is cloneUnlessCtx with the resolution memories the
// parked Ctx carries re-pointed at the clone's copies.
func (m *cloneRemap) unlessCtx(in effects.Ctx) effects.Ctx {
	out := cloneUnlessCtx(in)
	out.FlipMemory = m.flipMemory(in.FlipMemory)
	out.ExchangeMemory = m.exchangeMemory(in.ExchangeMemory)
	return out
}
