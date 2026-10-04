package rules

import (
	"maps"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Reflexive triggered abilities (CR 603.12): "Create two tokens. When you do,
// tap target creature an opponent controls." The "when you do" half is a
// triggered ability that triggers during the resolution that spawned it and
// goes on the stack -- with its own targets, respondable, firing ward and
// "becomes the target" triggers -- the next time a player would receive
// priority.
//
// The spawning primitive is effects' ImmediateTrigger. It used to resolve its
// Execute$ body inline, inside the spawning resolution, which made the target
// an untargeted resolution-time choice and gave nobody a window between the
// two halves. It now queues one pendingTrigger per instance here.
//
// No new event kind and no registration: the entry is the delayed-shape
// pendingTrigger with DelayedID -1, the shape a Saga chapter, a dungeon room
// and a Room's unlock trigger already queue (rules/saga.go). pushTrigger mints
// it through a DelayedPush whose Counter is the Execute$ SVar name, which
// events.Apply resolves from the source object's own SVar tables, and poses
// the ordinary CR 603.3c mode/target placement asks for it. The instance's
// remembered set rides the event's IDs.

// QueueReflexiveTrigger implements effects.Host.
//
// It reports false -- the caller then resolves the body inline, exactly as
// before -- unless a log-only replay can mint the SAME body: events.Apply
// resolves the Execute$ name from the source object's faces, so the resolving
// chain's SVar table must be the source's own current-face table (a gained or
// granted body compiled on a foreign face is not; the chain's table is a
// defensive copy, effects.SetSVars, so contents are compared, not identity)
// and the name must resolve there to the body being queued.
func (e *Engine) QueueReflexiveTrigger(c *effects.Ctx, execute string, body *cards.SA, remembered []state.Target) bool {
	if c == nil || body == nil || execute == "" {
		return false
	}
	src := e.G.Obj(c.Source)
	if src == nil || src.Face() == nil || !maps.Equal(c.SVars, src.Face().SVars) {
		return false
	}
	sa := events.ResolveSVarAcrossFaces(src, execute)
	if sa == nil || sa.Line != body.Line {
		return false
	}
	tc := c.TriggerContext
	tc.Reflexive = true
	tc.SpawnerCaptured = append([]state.Target(nil), c.Captured...)
	// The spawning resolution's delayed-registration capture is not this
	// ability's: DelayTriggerRemembered names the instance's own set.
	tc.DelayedRemembered = nil
	tc.OptionalSpec = ""
	// CR 107.3: "you may pay {X}. When you do, ... X ..." -- the reflexive
	// ability's X is the value the spawning ability was paid or announced
	// with (triggerPaidX reads it back for the minted ability).
	if c.X != 0 {
		tc.TriggerPaidX = c.X
	}
	pt := pendingTrigger{
		Source:     c.Source,
		Controller: c.Controller,
		Delayed:    true,
		DelayedID:  ^uint32(0),
		Execute:    execute,
		SA:         sa,
		Ctx:        c.ForTrigger(tc),
	}
	pt.Ctx.Remembered = append([]state.Target(nil), remembered...)
	e.pendingTriggers = append(e.pendingTriggers, pt)
	return true
}

// bindReflexiveContext carries what the spawning resolution knew onto the
// minted reflexive ability, in the engine maps resolveTop already reads per
// stack object (the same replay-derived maps a face trigger's push fills):
// the spawning trigger's LKI snapshot ("equal to its power" on a dies
// trigger's reflexive half), the damage-source LKI flags, and the lists the
// spawning ability PAID (Sacrificed$/Exiled$/Revealed$ heads).
func (e *Engine) bindReflexiveContext(id state.ObjID, pt *pendingTrigger) {
	if !pt.Ctx.TriggerContext.Reflexive {
		return
	}
	if pt.Ctx.LKI != nil {
		if e.triggerLKI == nil {
			e.triggerLKI = make(map[state.ObjID]triggerObjectLKI)
		}
		lki := pt.Ctx.LKI.CloneDeep()
		e.triggerLKI[id] = triggerObjectLKI{object: &lki,
			power: pt.Ctx.Snap.Power, toughness: pt.Ctx.Snap.Toughness,
			ptValid: pt.Ctx.Snap.PTValid}
	}
	if pt.Ctx.Snap.SourceLifelinkValid {
		if e.sourceLifelinkLKI == nil {
			e.sourceLifelinkLKI = make(map[state.ObjID]bool)
		}
		e.sourceLifelinkLKI[id] = pt.Ctx.Snap.SourceLifelink
	}
	if pt.Ctx.Snap.SourceControllerValid {
		if e.sourceControllerLKI == nil {
			e.sourceControllerLKI = make(map[state.ObjID]state.PlayerID)
		}
		e.sourceControllerLKI[id] = pt.Ctx.Snap.SourceController
	}
	if len(pt.Ctx.Sacrificed) > 0 {
		if e.sacrificedLKI == nil {
			e.sacrificedLKI = make(map[state.ObjID][]state.SacrificedInfo)
		}
		e.sacrificedLKI[id] = pt.Ctx.Sacrificed
	}
	if len(pt.Ctx.Exiled) > 0 {
		if e.castExiled == nil {
			e.castExiled = make(map[state.ObjID][]state.ObjID)
		}
		e.castExiled[id] = pt.Ctx.Exiled
	}
	if len(pt.Ctx.Revealed) > 0 {
		if e.castRevealed == nil {
			e.castRevealed = make(map[state.ObjID][]state.ObjID)
		}
		e.castRevealed[id] = pt.Ctx.Revealed
	}
}

// reflexiveCaptured rebinds a reflexive ability's resolution Ctx.Captured to
// the spawning ability's capture: resolveTop seeds Captured from the stack
// object's Remembered, which for a reflexive ability is the instance's own
// remembered set (real memory the body reads through Remembered /
// TriggerRemembered, never to be excluded as a capture).
func reflexiveCaptured(ctx *effects.Ctx) {
	if ctx.TriggerContext.Reflexive {
		ctx.Captured = append([]state.Target(nil), ctx.TriggerContext.SpawnerCaptured...)
	}
}
