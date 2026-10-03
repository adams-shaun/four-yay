package rules

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Gate re-check for the staticEffects memo.
//
// A staticEffects build that encountered a gate-carrying Continuous static
// (staticMemoGated: IsPresent$/IsPresent2$/Condition$/CheckSVar$/ClassBand$)
// used to be refused every re-stamp but a layer-inert one, because a gate is
// a read of arbitrary board state -- a life total, the active player, a
// battlefield count -- that the static-quiet event kinds (staticQuietKinds)
// do write. On a board carrying even one such static (a PlayerTurn Condition$
// or a Count$YourLifeTotal CheckSVar$ is common) that forced the full scan on
// almost every step, tap, mana and move event.
//
// A build that made no OTHER state read (staticMemoStateRead false) is a
// function of exactly two things: the static-quiet input every quiet build
// reads (see staticQuietKinds), and the boolean outcome of each gate it
// evaluated -- a gate is consulted only as `if !holds { continue }`, and the
// emission that follows a passing gate reads only the quiet input. So the
// build records every gate it evaluates, in scan order, with its outcome
// (staticGates). Across an event run the quiet admission accepts
// (staticSafeSince with gatesRechecked), the quiet input is unchanged, so the
// memo is exactly what a rescan would produce iff every recorded gate
// re-evaluates to its recorded outcome; refreshStaticContinuous then
// re-stamps the memo instead of rescanning. Any changed outcome falls through
// to the ordinary full rescan.
//
// The recorded list can only over-approximate the gates a rescan would
// evaluate: the admitted runs never add a static-hot object (a cold move and
// a static-cold token add no static, so no gate), and an admitted cold move
// of a gate-carrying object that emitted nothing (so it is no memo Source)
// leaves a stale record behind -- a static-hot object cannot come back onto
// the battlefield through an admitted move. A stale record no longer reaches
// the output; re-checking it like any other can only refuse a re-stamp a
// rescan would allow, never admit a wrong one.
//
// Order matters only for what a gate's own nested reads see: the re-check
// runs with staticEpoch already stamped at the head (exactly as the full
// rescan stamps it before its walk) and stops at the first changed outcome,
// so every gate it evaluates sees the same memo a rescan's evaluation of
// that gate would (the outputs before it are identical).
//
// Dependency filter. Re-evaluating every gate on every admitted run is
// still a per-event cost, so a gate whose inputs are exactly known is
// classified once, at record time, and re-evaluated only when the run held
// an event kind that can write one of them (staticGateDep). Only shapes
// whose whole read set is a handful of fields with a known writer are
// classified; everything else is staticGateDepAny and always re-evaluated.
// The classification reads the record's own params and SVar table, which
// are quiet input: for a record whose source still sits where it was
// scanned they are the build's own, and a stale record's outcome is
// irrelevant. Skipping is induction over stamps: a record's outcome is valid
// at the memo's last stamp, and a run with no writer of its inputs leaves it
// valid at the next.
//
// layerInertVerify rescans on every such re-stamp and panics on a
// difference, as for every other re-stamp, and additionally re-evaluates
// every live gate the filter skipped and panics if its outcome moved.

// staticGateDep classes (staticGateRec.dep).
const (
	// staticGateDepAny: re-evaluated on every admitted run.
	staticGateDepAny uint8 = iota
	// staticGateDepActive: a bare Condition$ PlayerTurn/NotPlayerTurn reads
	// only g.Active (written by TurnChange alone) against the record's
	// fixed controller.
	staticGateDepActive
	// staticGateDepLife: a bare CheckSVar$ naming a Count$YourLifeTotal
	// SVar, compared against a literal or a Count$YourStartingLife[/Plus.N]
	// SVar (Elenda, Saint of Dusk; Angel of Vitality), reads the
	// controller's life (written by LifeChange and Damage only) and the
	// source's runtime SVar of that name (StoreSVar); the starting life is
	// the engine's fixed Config value.
	staticGateDepLife
)

// staticGateRec is one gate staticEffectsWalk evaluated: the staticView it
// built (minus the fields a printed static leaves zero), the outcome and its
// dependency class.
type staticGateRec struct {
	params     map[string]string
	ps         *cards.ParamSet
	svars      map[string]string
	source     state.ObjID
	controller state.PlayerID
	zone       state.Zone
	holds      bool
	dep        uint8
}

// view rebuilds the staticView the walk evaluated the gate under.
func (r *staticGateRec) view() staticView {
	return staticView{Source: r.source, Controller: r.controller, Params: r.params, PS: r.ps, SVars: r.svars}
}

// staticGateHolds evaluates a Continuous static's gate in the walk and
// records it for the re-check.
func (e *Engine) staticGateHolds(sv staticView) bool {
	holds := e.continuousGateHolds(sv)
	var zone state.Zone
	if o := e.G.Obj(sv.Source); o != nil {
		zone = o.Zone
	}
	e.staticGates = append(e.staticGates, staticGateRec{
		params: sv.Params, ps: sv.PS, svars: sv.SVars,
		source: sv.Source, controller: sv.Controller, zone: zone, holds: holds,
		dep: e.staticGateDep(sv),
	})
	return holds
}

// staticGateDep classifies a gate's read set (see the constants).
func (e *Engine) staticGateDep(sv staticView) uint8 {
	if sv.HasParam(cards.PKIsPresent) || sv.HasParam(cards.PKIsPresent2) || sv.HasParam(cards.PKClassBand) {
		return staticGateDepAny
	}
	cond, hasCond := sv.Param(cards.PKCondition)
	chk, hasChk := sv.Param(cards.PKCheckSVar)
	switch {
	case hasCond && !hasChk:
		switch strings.TrimSpace(cond) {
		case "PlayerTurn", "NotPlayerTurn":
			return staticGateDepActive
		}
	case hasChk && !hasCond:
		// checkSVarHolds' own table choice: the view's, else the source's
		// active face's.
		table := sv.SVars
		if table == nil {
			o := e.G.Obj(sv.Source)
			if o == nil || o.Face() == nil {
				return staticGateDepAny
			}
			table = o.Face().SVars
		}
		chk = strings.TrimSpace(chk)
		if chk == "" || table[chk] != "Count$YourLifeTotal" {
			return staticGateDepAny
		}
		if name := lifeGateThresholdName(sv.ParamStr(cards.PKSVarCompare)); name != "" {
			if body, found := table[name]; !found || !startingLifeBody(body) {
				return staticGateDepAny
			}
		}
		return staticGateDepLife
	}
	return staticGateDepAny
}

// lifeGateThresholdName returns the SVar name an SVarCompare$ value prices
// its threshold by, or "" when the threshold reads no state: an absent or
// unreadable compare (effects.CheckSVarHolds reads nothing for either) or a
// literal. A named threshold is fixed only when its body is
// (startingLifeBody).
func lifeGateThresholdName(cmp string) string {
	cmp = strings.TrimSpace(cmp)
	if len(cmp) < 3 {
		return ""
	}
	rhs := cmp[2:]
	if _, err := strconv.ParseInt(rhs, 10, 64); err == nil {
		return ""
	}
	return rhs
}

// startingLifeBody reports whether an SVar body is Count$YourStartingLife,
// optionally offset by /Plus.<digits>: the engine's fixed Config value.
func startingLifeBody(body string) bool {
	rest, ok := strings.CutPrefix(body, "Count$YourStartingLife")
	if !ok {
		return false
	}
	if rest == "" {
		return true
	}
	digits, ok := strings.CutPrefix(rest, "/Plus.")
	if !ok || digits == "" {
		return false
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return false
		}
	}
	return true
}

// staticGateActiveWriters / staticGateLifeWriters are the kinds whose Apply
// can write each class's inputs.
var (
	staticGateActiveWriters = newKindSet(events.TurnChange)
	staticGateLifeWriters   = newKindSet(events.LifeChange, events.Damage, events.StoreSVar)
)

// staticGateMayMove reports whether a run that held the kinds in seen can
// have moved a gate of class dep.
func staticGateMayMove(dep uint8, seen *kindSet) bool {
	var w *kindSet
	switch dep {
	case staticGateDepActive:
		w = &staticGateActiveWriters
	case staticGateDepLife:
		w = &staticGateLifeWriters
	default:
		return true
	}
	return w[0]&seen[0]|w[1]&seen[1]|w[2]&seen[2]|w[3]&seen[3] != 0
}

// staticGatesUnchanged re-evaluates, in scan order, every recorded gate the
// run (seen) can have moved, and reports whether each still has its
// recorded outcome. The caller has stamped staticEpoch at the head; a nested
// read that dropped the memo meanwhile (staticEpoch moved) reports false, so
// the caller rescans.
func (e *Engine) staticGatesUnchanged(seen *kindSet) bool {
	if !e.staticGatesKnown {
		return false
	}
	epoch := e.staticEpoch
	for i := 0; i < len(e.staticGates); i++ {
		r := &e.staticGates[i]
		if !staticGateMayMove(r.dep, seen) {
			if layerInertVerify {
				e.verifySkippedGate(r)
			}
			continue
		}
		if e.continuousGateHolds(r.view()) != r.holds {
			return false
		}
		if e.staticEpoch != epoch || !e.staticGatesKnown {
			return false
		}
	}
	return true
}

// verifySkippedGate is layerInertVerify's check on the dependency filter: a
// skipped gate of a live source must still evaluate to its outcome.
func (e *Engine) verifySkippedGate(r *staticGateRec) {
	o := e.G.Obj(r.source)
	if o == nil || o.Zone != r.zone {
		return // stale: no longer scanned where it was recorded (see above)
	}
	if got := e.continuousGateHolds(r.view()); got != r.holds {
		panic(fmt.Sprintf("rules: static gate dependency filter skipped a moved gate (source %d, class %d) at log %d", r.source, r.dep, len(e.L.Events)))
	}
}
