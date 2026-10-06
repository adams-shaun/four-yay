package rules

import (
	"fmt"

	"github.com/adams-shaun/gorge/state"
)

// renameGateVerifyFlag / renameGateVerify: when set, every refresh the
// no-SetName gate below skips is recomputed in full and the engine panics on
// any difference. Enabled for the rules test binary by
// setname_gate_verify_test.go; for a built binary, pass
// go build -ldflags "-X github.com/adams-shaun/gorge/rules.renameGateVerifyFlag=1".
var renameGateVerifyFlag string

var renameGateVerify = renameGateVerifyFlag != ""

// setNamePossible is a cheap SUPERSET of anySetNameActive that does not build
// active(). active() is the live registry entries (a subset of e.continuous,
// filtered by continuousLive) plus the static memo e.staticContinuous, then
// pruneLostSourceAbilities (which only drops entries). So if neither source
// list carries a layer-3 SetName effect, no active effect sets a name, the
// rename table is necessarily empty, and refreshRenames can skip the full
// active() rebuild it would otherwise force after every board-changing event.
//
// Granted or copied SetName statics are covered: they reach active() only
// through e.continuous or the static scan, which this reads directly, rather
// than through a per-card printed-text probe.
func (e *Engine) setNamePossible() bool {
	for i := range e.continuous {
		if ce := &e.continuous[i]; ce.Layer == LText && ce.SetName != "" {
			return true
		}
	}
	// The same memoised refresh active() makes first; an out-of-band call is
	// already a sanctioned pattern (staticControlWants) and only bumps the
	// static build sequence active() keys on.
	e.refreshStaticContinuous()
	for i := range e.staticContinuous {
		if ce := &e.staticContinuous[i]; ce.Layer == LText && ce.SetName != "" {
			return true
		}
	}
	return false
}

// renameGateSkip is refreshRenames' no-SetName fast path: with an empty table
// and no SetName effect anywhere it could come from, the table stays empty.
// It stamps the scalar keys and clears renameDSeq so the derived-transparent
// path never trusts a derivedSeq stamped without an active() build.
func (e *Engine) renameGateSkip() bool {
	if len(e.renames) != 0 || e.setNamePossible() {
		return false
	}
	e.renameEpoch, e.renameVersion, e.renameObjs = len(e.L.Events), e.continuousVersion, len(e.G.Objs)
	e.renameDSeq = 0
	if renameGateVerify {
		e.verifyRenameGateSkip()
	}
	return true
}

// verifyRenameGateSkip recomputes a skipped refresh in full (bypassing the
// gate) and panics if the full walk finds any rename.
func (e *Engine) verifyRenameGateSkip() {
	if e.anySetNameActive() {
		panic(fmt.Sprintf("rules: rename gate skipped at log %d but an active SetName effect exists", len(e.L.Events)))
	}
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone != state.ZBattlefield || o.Face() == nil {
			continue
		}
		if name := e.derivedName(o.ID); name != "" && name != o.Face().Name {
			panic(fmt.Sprintf("rules: rename gate skipped at log %d but object %d is named %q (printed %q)", len(e.L.Events), o.ID, name, o.Face().Name))
		}
	}
}
