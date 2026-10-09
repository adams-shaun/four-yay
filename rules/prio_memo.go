package rules

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The posed-priority option memo.
//
// A seat is asked for priority many times a turn (upkeep, draw, each combat
// step, the end step, both mains) and, between a large share of those
// windows, nothing its offer list reads has changed: the only events logged
// since its previous window are the priority hand-offs themselves, an empty
// mana-pool clear and a plain step change. askPriority's walk then rebuilds
// the identical list. This memo keeps, per seat, the last list the real walk
// built and serves it again while the "options-relevant state" below is
// provably the one the walk read.
//
// Why a hit is exact. The walk (legalActionsWalkWithWindow with forAsk and
// no hypothetical pool) is a pure read of
//
//  1. event-folded state e.G, which moves only through events.Apply;
//  2. the continuous registry (continuousVersion) and the object arena
//     (len(e.G.Objs));
//  3. the engine's non-event runtime fields, all at rest at a priority ask
//     outside a cast, resolution or replacement (prioMemoUsable) and with no
//     inert hold-out set (inertHeldOut, which filters the list);
//  4. the step, which it reads in a fixed set of places: the sorcery-timing
//     class (re-evaluated on every lookup and part of the key) and the
//     script-token gates listed on faceStepSensitive.
//
// A stored entry (one per seat and sorcery-timing class) is reusable only
// when every event logged since it was stored is one of
//
//   - Priority, DecisionAsk, DecisionMade: bookkeeping (bookkeepingKind),
//     whose fold writes only g.Priority and g.Passes, neither of which the
//     walk reads;
//   - ManaClear: its fold writes only pools, their tallies and restriction
//     batches. An entry is stored only while every pool is empty and no seat
//     holds a restricted batch (prioPoolsIdle), so a clear changes nothing;
//   - StepChange entering Upkeep, Draw, Main1, DeclareAttackers,
//     DeclareBlockers, CombatDamage, EndCombat, Main2 or End: the fold writes
//     g.Step, the combat-mana demotion (nothing while pools are empty),
//     LastUpkeepTurn (echo, a trigger gate) and EndStepsThisTurn (read only
//     by the FinishedEndOfTurnsThisTurn head, a StepSensitive token).
//     BeginCombat is skipped too while no permanent holds a mode pick: its
//     fold bumps CombatsThisTurn (read by the FirstCombat tokens,
//     StepSensitive) and rewrites only picks. Untap and Cleanup never are;
//   - EndCombatReset, while no permanent was attacking or blocked when the
//     entry was stored (the fold clears exactly those flags, so it is a
//     no-op);
//
// and the key still matches: the same event log and restore epoch (a kernel
// restore rewinds state under one log), registry version, arena length,
// stack depth, active seat, turn, and the seat's sorcery-timing class (the
// entry slot). When the step has changed the entry additionally requires
// that nothing that can reach the walk is step-sensitive (prioStepSensitive:
// any object outside the libraries but their end cards, or a registered
// UntilEndOfCombat effect). That answer is computed on first need: object
// membership of those zones moves only through non-ignorable events, so it
// equals the answer at the store.
//
// Verify mode (GORGE_PRIO_MEMO_VERIFY=1, or rules.derivedMemoVerifyFlag, which
// the rules test binary sets) runs the real walk on every hit and panics if
// the memo's list differs in any field.
//
// The memo is OFF unless GORGE_PRIO_MEMO=1 (the rules test binary turns it
// on, with verify). Measured on the dzgorge 34-pair bench it hits 12% of the
// 4.7M posed priority windows, but the hits land on cheap walks: the net CPU
// change is within noise (the walk time it saves, ~1.3 s of 89 s, is spent on
// the store and lookup), so it ships disabled until more event kinds can be
// proven option-neutral.
var (
	prioMemoOff    = os.Getenv("GORGE_PRIO_MEMO") != "1"
	prioMemoVerify = derivedMemoVerifyFlag != "" || os.Getenv("GORGE_PRIO_MEMO_VERIFY") != ""

	prioMemoHits, prioMemoMisses, prioMemoSkips atomic.Uint64
)

// PrioMemoStats reports the process-wide memo counters: windows served from
// the memo, windows walked and stored, and windows that bypassed it.
func PrioMemoStats() (hits, misses, skips uint64) {
	return prioMemoHits.Load(), prioMemoMisses.Load(), prioMemoSkips.Load()
}

type prioMemoEnt struct {
	valid     bool
	log       *events.Log
	tape      uint32
	pos       int
	ver       int
	objs      int
	stack     int
	active    state.PlayerID
	turn      int32
	sens      bool // step-sensitive (valid once sensKnown)
	sensKnown bool
	anyModes  bool // some object holds a mode pick (BeginCombat's fold rewrites them)
	anyCombat bool // some object is attacking or blocked (EndCombatReset's fold clears them)
	step      state.Step
	opts      []decision.Option
}

// prioMemoUsable reports whether the engine is at a plain priority ask: no
// cast, resolution, replacement or tape in flight, no diagnostics collector,
// no hold-out filter, and no seat holding restricted mana.
func (e *Engine) prioMemoUsable(window *windowCollector) bool {
	if prioMemoOff || window != nil || e.L == nil || e.G == nil || len(e.G.Players) > 8 ||
		e.hostAsking != 0 || e.cast != nil || e.Suspended() || e.offStackMana != nil ||
		e.resolvingObj != 0 || e.applyingReplacement || e.activeDepth != 0 ||
		len(e.inertHeldOut) != 0 || !e.derivedMemoUsable() || e.tape.Watching() {
		return false
	}
	for i := range e.G.Players {
		if len(e.G.Players[i].RestrictedMana) != 0 {
			return false
		}
	}
	return true
}

// prioMemoStepPlain reports whether a StepChange into step s is skippable.
func prioMemoStepPlain(s state.Step) bool {
	switch s {
	case state.StepUpkeep, state.StepDraw, state.StepMain1, state.StepDeclareAttackers,
		state.StepDeclareBlockers, state.StepCombatDamage, state.StepEndCombat,
		state.StepMain2, state.StepEnd:
		return true
	}
	return false
}

// prioMemoLookup returns p's memoised list when it is provably the walk's.
func (e *Engine) prioMemoSlot(p state.PlayerID) int {
	i := int(p) * 2
	if e.sorcerySpeed(p) {
		i++
	}
	return i
}

func (e *Engine) prioMemoLookup(p state.PlayerID) *prioMemoEnt {
	i := e.prioMemoSlot(p)
	if i >= len(e.prioMemo) {
		return nil
	}
	m := &e.prioMemo[i]
	if !m.valid || m.log != e.L || m.tape != e.tapeEpoch || m.ver != e.continuousVersion ||
		m.objs != len(e.G.Objs) || m.stack != len(e.G.Stack) || m.active != e.G.Active ||
		m.turn != e.G.Turn || m.pos > len(e.L.Events) {
		return nil
	}
	if m.step != e.G.Step {
		if !m.sensKnown {
			m.sens, m.sensKnown = e.prioStepSensitive(), true
		}
		if m.sens {
			return nil
		}
	}
	for i := m.pos; i < len(e.L.Events); i++ {
		ev := &e.L.Events[i]
		switch ev.Kind {
		case events.Priority, events.DecisionAsk, events.DecisionMade, events.ManaClear:
		case events.StepChange:
			if !prioMemoStepPlain(ev.Step) && !(ev.Step == state.StepBeginCombat && !m.anyModes) {
				return nil
			}
		case events.EndCombatReset:
			if m.anyCombat {
				return nil
			}
		default:
			return nil
		}
	}
	if !e.prioPoolsIdle() {
		return nil
	}
	return m
}

// prioPoolsIdle reports whether no seat holds mana: with every pool empty,
// the only ignorable events that touch pools (ManaClear, the combat-mana
// demotion in a StepChange fold) have nothing to change, so the entry needs
// no pool comparison beyond this emptiness check, which also keeps a
// direct write to a pool (a test floating mana without an event) from being
// served stale. Tallies (snow, typed, persistent, combat) never exceed
// the pool they describe.
func (e *Engine) prioPoolsIdle() bool {
	for i := range e.G.Players {
		pl := &e.G.Players[i]
		if pl.Pool != (state.Mana{}) || pl.PersistentMana != (state.Mana{}) || pl.CombatMana != (state.Mana{}) {
			return false
		}
	}
	return true
}

// prioMemoStore records the real walk's list for p. res is the walk's
// result, copied into the entry's own buffer.
func (e *Engine) prioMemoStore(p state.PlayerID, res []decision.Option) {
	i := e.prioMemoSlot(p)
	for len(e.prioMemo) <= i {
		e.prioMemo = append(e.prioMemo, prioMemoEnt{})
	}
	m := &e.prioMemo[i]
	if !e.prioPoolsIdle() {
		m.valid = false
		return
	}
	modes, combat := e.prioBoardFlags()
	*m = prioMemoEnt{
		valid: true, log: e.L, tape: e.tapeEpoch, pos: len(e.L.Events), ver: e.continuousVersion,
		objs: len(e.G.Objs), stack: len(e.G.Stack), active: e.G.Active, turn: e.G.Turn,
		anyModes: modes, anyCombat: combat, step: e.G.Step,
		opts: append(m.opts[:0], res...),
	}
}

// prioBoardFlags reports whether any battlefield permanent holds a mode pick
// (BeginCombat's fold rewrites them) or is attacking or blocked
// (EndCombatReset's fold clears them).
func (e *Engine) prioBoardFlags() (modes, combat bool) {
	for p := range e.G.Players {
		for _, id := range e.G.Zone(state.ZBattlefield, state.PlayerID(p)) {
			o := e.G.Obj(id)
			if o == nil {
				continue
			}
			if len(o.ModeChoices) != 0 {
				modes = true
			}
			if o.IsAttacking || o.AttackingBattle != 0 || len(o.BlockedBy) != 0 {
				combat = true
			}
		}
	}
	return
}

// prioStepSensitive reports whether any object whose text can reach the
// offer walk is step-sensitive (faceStepSensitive), or an
// UntilEndOfCombat effect (whose liveness reads the step) is registered.
// Object membership of the scanned zones moves only through non-ignorable
// events, so the answer at a lookup is the one at the entry's store.
func (e *Engine) prioStepSensitive() bool {
	for i := range e.continuous {
		if d := e.continuous[i].Duration; len(d) == len("UntilEndOfCombat") && strings.EqualFold(d, "UntilEndOfCombat") {
			return true
		}
	}
	for p := range e.G.Players {
		pid := state.PlayerID(p)
		for _, z := range [...]state.Zone{state.ZHand, state.ZBattlefield, state.ZGraveyard, state.ZExile, state.ZCommand} {
			for _, id := range e.G.Zone(z, pid) {
				if o := e.G.Obj(id); o != nil && prioObjSensitive(o) {
					return true
				}
			}
		}
		if lib := e.G.Zone(state.ZLibrary, pid); len(lib) != 0 {
			for _, id := range [2]state.ObjID{lib[0], lib[len(lib)-1]} {
				if o := e.G.Obj(id); o != nil && prioObjSensitive(o) {
					return true
				}
			}
		}
	}
	for _, id := range e.G.Stack {
		if o := e.G.Obj(id); o != nil && prioObjSensitive(o) {
			return true
		}
	}
	return false
}

func prioObjSensitive(o *state.Object) bool {
	if f := o.Face(); f != nil && faceStepSensitive(f) {
		return true
	}
	if c := o.Card; c != nil && len(c.Faces) > 1 {
		for _, f := range c.Faces {
			if f != nil && faceStepSensitive(f) {
				return true
			}
		}
	}
	return false
}

// prioMemoCheck is verify mode: the real walk must equal the memo's list.
func (e *Engine) prioMemoCheck(p state.PlayerID, m *prioMemoEnt) {
	fresh := e.legalActionsWithWindow(p, nil)
	if len(fresh) == len(m.opts) {
		same := true
		for i := range fresh {
			if !reflect.DeepEqual(fresh[i], m.opts[i]) {
				same = false
				break
			}
		}
		if same {
			return
		}
	}
	panic(fmt.Sprintf("rules: priority memo stale for seat %d at turn %d step %v (events %d, stored at %d):\nmemo:  %v\nfresh: %v",
		p, e.G.Turn, e.G.Step, len(e.L.Events), m.pos, optSummary(m.opts), optSummary(fresh)))
}

func optSummary(os []decision.Option) []string {
	out := make([]string, len(os))
	for i, o := range os {
		out[i] = fmt.Sprintf("%d:%s:%q:%d", o.Index, o.Kind, o.Label, o.Obj)
	}
	return out
}

// priorityOptions is askPriority's offer list: the memo's when it is
// provably the walk's, otherwise the real walk (stored for the next window).
func (e *Engine) priorityOptions(p state.PlayerID, window *windowCollector) (out []decision.Option) {
	if quietStatsEnabled() {
		t0 := quietNow()
		defer func() { e.quietObserve(p, out, uint64(quietNow()-t0)) }()
	}
	// The quiet serve (rules/quiet_serve.go, design §3.2): a proved window's
	// options without the walk. Sits before the memo and does not depend on
	// it; the proof's false answer runs the walk below exactly as before.
	// The counter runs unconditionally: it adds only on served windows, so
	// the default build pays nothing on the windows it walks.
	if !quietOff && window == nil && e.quietUsable() && e.seatQuiet(p) {
		quietServed.Add(1)
		return e.quietOptions(p)
	}
	if !e.prioMemoUsable(window) {
		prioMemoSkips.Add(1)
		return e.legalActionsWithWindow(p, window)
	}
	if m := e.prioMemoLookup(p); m != nil {
		prioMemoHits.Add(1)
		if prioMemoVerify {
			e.prioMemoCheck(p, m)
		}
		res := e.arenaOptions(len(m.opts))
		copy(res, m.opts)
		// The entry now stands for the log up to here: later windows scan
		// only what follows.
		m.pos = len(e.L.Events)
		return res
	}
	prioMemoMisses.Add(1)
	res := e.legalActionsWithWindow(p, nil)
	e.prioMemoStore(p, res)
	return res
}
