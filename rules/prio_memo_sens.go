package rules

import (
	"strings"
	"sync"

	"github.com/adams-shaun/gorge/cards"
)

// faceStepSensitive reports whether the face carries any text a priority
// offer walk can evaluate against the exact current step, as opposed to the
// coarser "main phase" class (sorcery timing) that the engine reads through
// sorcerySpeed.
//
// The offer walk reads state.Game.Step in exactly these places, each driven
// by a script token: ActivationPhases$ / Phases$ / ConditionPhases$ (all
// contain "Phases"), ActivationAfterBlockers$, ActivationFirstCombat$ /
// ConditionFirstCombat$ (which read the per-turn combat count a BeginCombat
// step change bumps), the count heads InOwnMainPhase / IfCastInOwnMainPhase
// and FinishedEndOfTurnsThisTurn, and the Sneak and Paradigm keywords (sneak.go,
// paradigm.go). The prio memo (prio_memo.go) lets an offer list survive a plain
// step change only while no object that can reach the walk is
// step-sensitive. The scan is deliberately a superset: it reads every
// ability, static, replacement and SVar parameter (key or value) and each
// trigger's executed chain as raw text. A false positive costs a recompute,
// never an answer.
//
// The answer is cached per face pointer with a signature of the face's list
// lengths, so a face that grew after the first read (a keyword link
// appending a static) is re-scanned.
func faceStepSensitive(f *cards.Face) bool {
	sig := len(f.Abilities)*131 + len(f.Statics)*31 + len(f.Repls)*17 + len(f.SVars)*7 + len(f.Keywords)*3 + len(f.Triggers)
	if v, ok := faceStepSensMemo.Load(f); ok {
		if e := v.(faceStepSensEnt); e.sig == sig {
			return e.sens
		}
	}
	r := scanFaceStepSensitive(f)
	faceStepSensMemo.Store(f, faceStepSensEnt{sig: sig, sens: r})
	return r
}

type faceStepSensEnt struct {
	sig  int
	sens bool
}

var faceStepSensMemo sync.Map // *cards.Face -> faceStepSensEnt

func stepSensText(s string) bool {
	return strings.Contains(s, "Phases") || strings.Contains(s, "AfterBlockers") ||
		strings.Contains(s, "InOwnMainPhase") || strings.Contains(s, "FirstCombat") ||
		strings.Contains(s, "FinishedEndOfTurns")
}

func stepSensParams(m map[string]string) bool {
	for k, v := range m {
		if stepSensText(k) || stepSensText(v) {
			return true
		}
	}
	return false
}

func stepSensSA(sa *cards.SA) bool {
	for depth := 0; sa != nil && depth < 16; sa, depth = sa.Sub, depth+1 {
		if stepSensParams(sa.Params) || stepSensText(sa.Line) {
			return true
		}
	}
	return false
}

func scanFaceStepSensitive(f *cards.Face) bool {
	for _, k := range f.Keywords {
		if strings.HasPrefix(k, "Sneak") || strings.HasPrefix(k, "Paradigm") || stepSensText(k) {
			return true
		}
	}
	for _, a := range f.Abilities {
		if stepSensSA(a) {
			return true
		}
	}
	for i := range f.Statics {
		if stepSensParams(f.Statics[i].Params) {
			return true
		}
	}
	for i := range f.Repls {
		if stepSensParams(f.Repls[i].Params) || stepSensSA(f.Repls[i].With) {
			return true
		}
	}
	for i := range f.Triggers {
		if stepSensSA(f.Triggers[i].Effect) {
			return true
		}
	}
	for k, v := range f.SVars {
		if stepSensText(k) || stepSensText(v) {
			return true
		}
	}
	return false
}
