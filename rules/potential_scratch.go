package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// pmScratch is PotentialMana's per-call working storage -- the per-source
// membership lists, walked/admitted flags and counted production -- kept on
// the engine (hypSparePool, owner-guarded) and reset per call instead of
// allocated per call. Every slot a call reads is written by that call first:
// walked and own are cleared, members and admitted are nil until set, and
// the membership lists and flags are carved from flat arrays whose handed-out
// regions never overlap within a call.
type pmScratch struct {
	busy     bool
	members  [][]*cards.SA
	walked   []bool
	admitted [][]bool
	own      []state.Mana
	sas      []*cards.SA // backing for every membership list
	sasAt    int
	bools    []bool // backing for every admitted list
	boolsAt  int
	ses      []*cards.SA // one source's newly admitted members
}

// potentialManaScratch returns e's scratch sized for n sources, or a fresh
// one when a call is already using it (a nested PotentialMana).
func (e *Engine) potentialManaScratch(n int) *pmScratch {
	sc := &e.hypPool().pm
	if sc.busy {
		sc = &pmScratch{}
	}
	sc.busy = true
	sc.members = resizeCleared(sc.members, n)
	sc.walked = resizeCleared(sc.walked, n)
	sc.admitted = resizeCleared(sc.admitted, n)
	sc.own = resizeCleared(sc.own, n)
	sc.sasAt, sc.boolsAt = 0, 0
	return sc
}

// resizeCleared returns s with length n and every element zero, reusing its
// array when it is large enough.
func resizeCleared[T any](s []T, n int) []T {
	if cap(s) < n {
		return make([]T, n)
	}
	s = s[:n]
	clear(s)
	return s
}

// spill is the empty list the next membership walk appends into: the unused
// tail of the flat array, so a walk that fits allocates nothing.
func (sc *pmScratch) spill() []*cards.SA { return sc.sas[sc.sasAt:sc.sasAt] }

// take records a membership list a walk built from spill(). A list that
// outgrew the flat array was reallocated by append and is kept as is (it is
// still this call's own); the flat array is grown for the next call.
func (sc *pmScratch) take(l []*cards.SA) []*cards.SA {
	if len(l) == 0 {
		return nil
	}
	if sc.sasAt+len(l) <= len(sc.sas) && &l[0] == &sc.sas[sc.sasAt] {
		sc.sasAt += len(l)
		return l[:len(l):len(l)]
	}
	if need := sc.sasAt + len(l); need > len(sc.sas) {
		grown := make([]*cards.SA, 2*need)
		copy(grown, sc.sas[:sc.sasAt])
		sc.sas = grown
	}
	return l
}

// flags returns n false flags carved from the flat flag array.
func (sc *pmScratch) flags(n int) []bool {
	if sc.boolsAt+n > len(sc.bools) {
		grown := make([]bool, max(2*(sc.boolsAt+n), 64))
		sc.bools, sc.boolsAt = grown, 0
	}
	f := sc.bools[sc.boolsAt : sc.boolsAt+n : sc.boolsAt+n]
	clear(f)
	sc.boolsAt += n
	return f
}

// done releases the scratch, dropping the ability pointers it holds.
func (sc *pmScratch) done() {
	clear(sc.sas[:sc.sasAt])
	clear(sc.members)
	clear(sc.ses[:cap(sc.ses)])
	sc.busy = false
}
