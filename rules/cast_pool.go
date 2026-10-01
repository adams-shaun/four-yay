package rules

import "github.com/adams-shaun/gorge/state"

// pendingCast recycling.
//
// A pendingCast is ~4 KB and one is allocated for every cast, every
// activated ability's cost flow and every land play, so a search that plays
// thousands of games allocates and collects gigabytes of them. The engine
// keeps the one it last issued (castIssued) and, once that cast is over,
// reuses its storage for the next one (castFree).
//
// "Over" is decided only at Submit's entry, where no engine frame is live:
// the flows that keep using a pendingCast after clearing e.cast (handleTarget's
// charm arm reads pc.stackObj after payCast settled it; a nested Play from a
// resolution sets e.cast while an outer frame still holds its own) all run
// inside one Submit, and between Submits the ONLY heap reference to a cast in
// flight is e.cast itself (Clone copies the struct, it never shares it).
// So the last issued cast is free exactly when it is no longer e.cast at the
// next Submit. A recycled struct is zeroed before reuse, so nothing a cast
// left behind (its slices, its cost) is visible to the next one; with
// castPoolPoison on (cast_pool_test.go) it is instead filled with garbage at
// recycle time, so a stale reader in the rules test binary reads nonsense
// rather than a plausible zero.

// castPoolPoison is a test hook: when set, a recycled pendingCast is filled
// with garbage instead of zeroed.
var castPoolPoison bool

// newCast returns a zeroed pendingCast with the fields every cast flow sets,
// drawn from the recycled one when there is one.
func (e *Engine) newCast(p state.PlayerID, card state.ObjID, from state.Zone, mode string, ability int) *pendingCast {
	pc := e.castFree
	if pc == nil {
		pc = new(pendingCast)
	} else {
		e.castFree = nil
		if castPoolPoison {
			*pc = pendingCast{}
		}
	}
	pc.player, pc.card, pc.from, pc.mode, pc.ability = p, card, from, mode, ability
	e.castIssued = pc
	return pc
}

// recycleCast frees the last issued pendingCast once it is no longer the
// cast in flight. Called only where no engine frame can hold a cast: Submit's
// entry and Release.
func (e *Engine) recycleCast() {
	pc := e.castIssued
	if pc == nil || pc == e.cast {
		return
	}
	e.castIssued = nil
	if castPoolPoison {
		poisonCast(pc)
	} else {
		*pc = pendingCast{}
	}
	e.castFree = pc
}

// poisonCast fills a recycled pendingCast with values no live cast holds.
func poisonCast(pc *pendingCast) {
	*pc = pendingCast{}
	pc.player = 0x7e
	pc.card = 0x7fff_fff0
	pc.from = 0x7e
	pc.mode = "\x00poisoned-cast"
	pc.ability = -0x7ff0
	pc.stackObj = 0x7fff_fff1
	pc.x = -0x7ff1
	pc.xDone = true
	pc.pushed = true
}
