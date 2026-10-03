package rules

import (
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// Cascade (CR 702.85, task cascade1) — the rules-side half. The keyword is
// READ, never expanded: cards/keywords.go has no Cascade case, and both the
// printed K:Cascade line and every layer-6 AddKeyword$ Cascade grant (the
// printed S: statics the layer walk already emits AND the DB$ Effect-delivered
// statics effects' effEffect registers) reach this code through the ONE
// derived-keyword read below, the way hasCastConvoke reads Convoke.
//
// The trigger is a REAL triggered ability (CR 702.85a: "Cascade is
// triggered"): one pendingTrigger per instance is queued in the cast flow's
// pay stage, and the trigger drain mints a respondable KeywordTriggerPush
// stack object ABOVE the cascade spell. Everything observable — the
// exile-until scan, the may-cast election, the bottoming — happens when that
// ability RESOLVES (effects/cascade.go's effCascade), so the trigger can be
// responded to and countered like any other, and a free cast it begins sits
// on the stack above it exactly as CR's resolution order puts it.
//
// Deliberately NOT fired: a suspended card (mode "suspend") and a foretold
// action are not casts — payCast returns before the queue is reached — and a
// copy is never cast, so neither can cascade. A free cast (the "play" mode)
// IS a cast and cascades like any other, which is how Maelstrom Wanderer
// cascades into another cascade card (CR 702.85b's chain).
func (e *Engine) queueCascadeTriggers(stackObj state.ObjID, p state.PlayerID) {
	if stackObj == 0 {
		return
	}
	// The grant walk counts PRIOR casts (the stackGrantCast scratch,
	// rules/engine.go): this is the one read where the in-flight cast's own
	// PutOnStack — already in the log — must not satisfy the gate's EQ0.
	// Neither scratch is part of the two cache keys active() serves the
	// grants from (activeEpoch by log head + continuousVersion for the
	// battlefield permanents' list, staticEpoch by log head alone for the
	// static-derived list — and a build INSIDE the payment flow has already
	// stamped the current log head with the scratch unset), so both are
	// invalidated around the walk; the post-walk state (scratch cleared)
	// invalidates again so the memo never serves a scratch-built list.
	e.stackGrantCast = stackObj
	e.invalidateScratchLayerLists()
	n := e.cascadeInstances(stackObj)
	e.stackGrantCast = 0
	e.invalidateScratchLayerLists()
	if n == 0 {
		return
	}
	if int(p) >= len(e.G.Players) || e.G.Players[p].Lost {
		return
	}
	for i := 0; i < n; i++ {
		e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
			Source:     stackObj,
			Controller: p,
			Idx:        -1,
			Cascade:    true,
			Ctx:        effects.NewCtx(stackObj, p, effects.CtxInit{}),
		})
	}
}
