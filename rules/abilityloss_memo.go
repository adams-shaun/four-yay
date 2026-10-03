package rules

import (
	"fmt"

	"github.com/adams-shaun/gorge/state"
)

// Per-build memo of abilityLoss.
//
// Once a "loses all abilities" effect is live (abilityLossPossible's proof is
// gone and the list holds a remover), abilityLoss recomputes the object's
// layer-4 type list and walks every remover's Affected$ match -- and the
// offer walk asks it about the same object several times (grantedAbilities,
// the printed-face gates, the mana walk), on every walk.
//
// The answer for o is a pure function of active()'s published list and the
// board that list was built from: the removers are entries of the list, the
// type list is typeCharacteristicsActive over the same list, and the match
// reads the board. So it is cached per object, keyed on activeBuildSeq read
// AFTER the active() call every lookup makes first:
//
//   - the board moves only through events.Apply. active()'s exact hit
//     requires the log head it was built at, and its layer-inert re-stamp
//     admits only DecisionAsk/DecisionMade/Priority runs (layerInertSince:
//     Apply writes nothing for the markers, and Priority writes only
//     g.Priority/g.Passes, which no Affected$ match or layer-4 walk reads)
//     with continuousVersion and len(e.G.Objs) unchanged. Every other event,
//     every registry write (continuousVersion) and every object append is a
//     rebuild, and every rebuild moves activeBuildSeq;
//   - the explicit invalidations move it too: activeEpoch = -1
//     (invalidateScratchLayerLists, refreshStaticContinuous, cascade) forces
//     a rebuild, and retireCrossWalkMemo -- the face probe's flip, the cast
//     probe, the cost-composition exclusion: the engine's no-event inputs to
//     an active() reader -- moves the count directly;
//   - the remaining no-event inputs are the derivation guards the Derived
//     memo lists (derivedMemoUsable: a nested derivation, a rename/type table
//     build, an Effect observer's match binding, a goad probe, a colour-table
//     build). The memo is neither read nor written while any is live, and
//     abilityLoss itself stands down mid-build (activeDepth != 0) before the
//     memo is consulted.
//
// So an entry stamped at the current count was computed from the same list
// and an equal board. Entries carry a generation (gen) instead of the count
// itself: boardLive moves gen whenever the count it was opened at is not the
// current one, so a stale entry never matches and nothing is ever cleared.
// gen never repeats over the storage's lifetime: a fresh Engine starts with
// a zero memo (gen 0 is never live), and storage handed on through a Spare
// or a reused look-back observer keeps its gen (recycled).
//
// In the rules test binary abilityLossMemoVerify recomputes every hit and
// panics on a difference.
type abilityLossMemo struct {
	seq uint64 // activeBuildSeq the current gen was opened at
	gen uint64
	// live: the list built at seq holds a "loses all abilities" effect.
	live bool
	ents []abilityLossEnt
}

type abilityLossEnt struct {
	gen  uint64
	ts   uint32
	lost bool
}

// abilityLossMemoVerify: see derivedMemoVerify. Set by the rules test binary.
var abilityLossMemoVerify = derivedMemoVerifyFlag != ""

// boardLive answers abilityLoss's board-wide early outs and returns
// active()'s list: false mid-build (activeDepth != 0, the stand-down
// matchesSpec's Derived bind takes), while abilityLossPossible's proof holds,
// or when the list holds no remover. The last is a property of the list, so
// it is read once per build -- when the build count moves, which also opens
// a new entry generation. Every printed-face gate asks this for every object
// it visits, so the common board (no remover live) answers here with a few
// compares.
func (m *abilityLossMemo) boardLive(e *Engine) ([]ContinuousEffect, bool) {
	if e.activeDepth != 0 || (!e.lossProof.seen && !e.abilityLossPossible()) {
		return nil, false
	}
	act := e.active()
	if m.gen == 0 || m.seq != e.activeBuildSeq {
		m.gen++
		m.seq = e.activeBuildSeq
		m.live = e.activeSummaryOf(act).hasRemoveAbilities
	} else if abilityLossMemoVerify {
		if fresh := e.activeSummaryOf(act).hasRemoveAbilities; fresh != m.live {
			panic(fmt.Sprintf("rules: ability-loss memo's remover flag stale at build %d: cached %v, fresh %v", m.seq, m.live, fresh))
		}
	}
	return act, m.live
}

// recycled returns m's storage for another engine (a Spare, a reused
// look-back observer): every entry is kept but none can match, because the
// impossible seq makes the first boardLive open a generation above every
// stored one.
func (m *abilityLossMemo) recycled() abilityLossMemo {
	return abilityLossMemo{gen: m.gen, seq: ^uint64(0), ents: m.ents}
}

// store records id's answer under gen. hint is the capacity target when the
// table must grow (the arena's own headroom, so it is allocated about once
// per game).
func (m *abilityLossMemo) store(id state.ObjID, gen uint64, ts uint32, lost bool, hint int) {
	if int(id) >= len(m.ents) {
		n := int(id) + 1
		if cap(m.ents) < n {
			grown := make([]abilityLossEnt, n, max(n, hint, 2*cap(m.ents)))
			copy(grown, m.ents)
			m.ents = grown
		} else {
			// [len, cap) was never written: zero entries, gen 0, never live.
			m.ents = m.ents[:n]
		}
	}
	m.ents[id] = abilityLossEnt{gen: gen, ts: ts, lost: lost}
}
