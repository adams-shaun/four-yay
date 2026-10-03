package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// Source-characteristic last-known information for a target filter.
//
// CR 113.7a: an ability on the stack exists independently of its source, and
// CR 608.2h: when it needs information about a source that has left the
// battlefield, it uses the source's last known information. A target filter
// whose bound is the source's own characteristic -- Alesha, Who Laughs at
// Fate's "creature card with mana value less than or equal to Alesha's
// power" (ValidTgts$ ...cmcLEX with SVar:X:Count$CardPower), Legacy's
// Allure's "power less than or equal to the number of treasure counters on
// it" (Count$CardCounters.TREASURE, the source sacrificed as the cost) -- is
// re-judged at resolution (CR 608.2b, legalTargets) through
// targetSpecContext's Resolve. Before this, that Resolve read the LIVE source:
// a graveyard card with its printed power and no counters, so a target the
// offer had certified fizzled when the source was destroyed in response.
//
// The snapshot is the source's layer-derived power and toughness and its
// counters at the last moment it was on the battlefield, captured at the same
// departure boundary as the own-source lifelink/controller LKI
// (captureSourceLifelinkLKI): for every waiting ability object of that source
// on the stack, and for every pending trigger of it (moved onto the stack
// object at TriggerPush). An activation that sacrifices its own source as a
// cost is seeded before the cost is paid (payCast). A zone-change trigger of
// the source itself (it died) already carries its LKI in triggerLKI, which
// sourceCharLKIFor reads when no departure snapshot exists.
//
// Engine-only and replay-derived like the other LKI maps: a replay re-runs
// the same intents, so the same departures repopulate it identically.

// sourceCharSnapshot is one source's characteristics as it last existed on
// the battlefield. counters is an owned copy that is never written after
// capture, so a clone may share it.
type sourceCharSnapshot struct {
	power, toughness int32
	counters         []state.Counter
}

// sourceCharSnapshotOf reads src's characteristics now; src must still be on
// the battlefield for the power/toughness to be layer-derived.
func (e *Engine) sourceCharSnapshotOf(src state.ObjID) sourceCharSnapshot {
	o := e.G.Obj(src)
	if o == nil {
		return sourceCharSnapshot{}
	}
	return sourceCharSnapshot{power: e.Power(src), toughness: e.Toughness(src),
		counters: append([]state.Counter(nil), o.Counters...)}
}

// captureSourceCharLKI records src's snapshot, immediately before it leaves
// the battlefield, for every ability object of src on the stack and every
// pending trigger of src that has none yet. The FIRST departure wins: an
// ability whose source already left, came back as a new object (CR 400.7)
// and left again still refers to the object that created it. The snapshot is
// taken only when some waiting ability needs it.
func (e *Engine) captureSourceCharLKI(src state.ObjID) {
	var snap sourceCharSnapshot
	have := false
	take := func() sourceCharSnapshot {
		if !have {
			snap, have = e.sourceCharSnapshotOf(src), true
		}
		return snap
	}
	for _, id := range e.G.Stack {
		o := e.G.Obj(id)
		if o == nil || o.Ability == nil || o.Source != src {
			continue
		}
		if _, ok := e.sourceCharLKI[id]; ok {
			continue
		}
		if e.sourceCharLKI == nil {
			e.sourceCharLKI = make(map[state.ObjID]sourceCharSnapshot)
		}
		e.sourceCharLKI[id] = take()
	}
	for i := range e.pendingTriggers {
		pt := &e.pendingTriggers[i]
		if pt.Source == src && !pt.sourceCharLKIValid {
			pt.sourceCharLKI, pt.sourceCharLKIValid = take(), true
		}
	}
}

// setSourceCharLKI records snap for the stack object id.
func (e *Engine) setSourceCharLKI(id state.ObjID, snap sourceCharSnapshot) {
	if e.sourceCharLKI == nil {
		e.sourceCharLKI = make(map[state.ObjID]sourceCharSnapshot)
	}
	e.sourceCharLKI[id] = snap
}

// sourceCharLKIFor returns the last-known snapshot a target filter of the
// stack object stack must read for its source, and whether one applies: a
// departure snapshot captured while the ability waited, else the trigger's
// own zone-change LKI when that LKI IS the source (a dies trigger reading
// "its power") and the live object is no longer that permanent. A source
// still on the battlefield as the same object reads live, as before.
func (e *Engine) sourceCharLKIFor(stack, source state.ObjID, lki *state.Object, lkiPower, lkiToughness int32, lkiPTValid bool) (sourceCharSnapshot, bool) {
	if snap, ok := e.sourceCharLKI[stack]; ok {
		return snap, true
	}
	if lki == nil || !lkiPTValid || lki.ID != source || lki.Zone != state.ZBattlefield {
		return sourceCharSnapshot{}, false
	}
	if o := e.G.Obj(source); o != nil && o.Zone == state.ZBattlefield && o.Incarnation == lki.Incarnation {
		return sourceCharSnapshot{}, false
	}
	return sourceCharSnapshot{power: lkiPower, toughness: lkiToughness, counters: lki.Counters}, true
}

// count answers a source-characteristic Count$ body from the snapshot:
// Count$CardPower, Count$CardToughness and Count$CardCounters.<KIND> (ALL
// included), each with an optional /Op suffix applied by the count grammar's
// own arithmetic. ok is false for every other body, which the caller then
// evaluates as before.
func (s sourceCharSnapshot) count(body string) (int32, bool) {
	head, ok := strings.CutPrefix(strings.TrimSpace(body), "Count$")
	if !ok {
		return 0, false
	}
	head, op, hasOp := strings.Cut(head, "/")
	var n int32
	switch {
	case head == "CardPower":
		n = s.power
	case head == "CardToughness":
		n = s.toughness
	case strings.HasPrefix(head, "CardCounters."):
		scratch := state.Object{Counters: s.counters}
		n = scratch.Counter(strings.TrimPrefix(head, "CardCounters."))
	default:
		return 0, false
	}
	if hasOp {
		n = effects.ApplyCountOp(n, op)
	}
	return n, true
}
