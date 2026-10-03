package rules

import (
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// The ability-object Ctx seeds resolveTop's ability branch and
// resumeResolution share (spike S3, legacy defect 3). A suspended resolution
// re-enters through resumeResolution with a freshly built Ctx, so every seed
// the first pass derived from the stack object must be derived the same way
// on the resume, or the resumed chain reads a different Ctx than the one it
// suspended under. Each helper below is the one home of one such seed; the
// Ctx-field parity ratchet (resume_ability_ctx_test.go) holds the two builders
// to the same field set.

// abilityResolutionSVars is the SVar table an ability stack object's chain
// resolves against. The ability object has no Face, so the table comes from
// the permanent it belongs to: the face that OWNS the resolving trigger
// (findTriggerForAbilityFace -- a mutated pile's under-card trigger, CR
// 702.140d), else the face owning an activated ability of a pile
// (pileFaceForSA -- Porcuparrot's `NumDmg$ X` resolves X on ITS face), else
// the source's current face. A granted (AddTrigger$) or Effect-delivered
// delayed trigger's body is the GRANTOR's SVar-named line, so the table
// pushTrigger recorded with that line (Engine.triggerLineSVars) wins: the
// recipient's face does not define the body's SVars, and the grant may have
// ended before the ability resolves. A source that has left the battlefield
// degrades to a nil table.
func (e *Engine) abilityResolutionSVars(id state.ObjID, o *state.Object) map[string]string {
	var svars map[string]string
	if src := e.G.Obj(o.Source); src != nil {
		if _, mf, ok := e.findTriggerForAbilityFace(o.Source, o.Ability); ok && mf != nil {
			svars = mf.SVars
		} else if mf, ok := e.pileFaceForSA(o.Source, o.Ability); ok && mf != nil {
			svars = mf.SVars
		} else if sf := src.Face(); sf != nil {
			svars = sf.SVars
		}
	}
	if owned, ok := e.triggerLineSVars[id]; ok {
		svars = owned
	}
	return svars
}

// bindNinjutsuDefender re-binds CR 702.49b's defender: a K:Ninjutsu
// permanent enters attacking the same player (planeswalker or battle) the
// returned creature was attacking. The activator captured that defender when
// the Return cost was paid (rules/cast.go's returncost arm) and it rides the
// AbilityPush event's IDs, which events.Apply folded into o.Remembered as a
// player target (and the planeswalker/battle as a real id after it), so
// effects/zone.go's Attacking$ True rider reads Ctx.DefendingPlayer /
// DefendingBattle. Only a ninjutsu activation carries the tag, so no other
// resolution's Remembered player is reinterpreted as a defending player.
func bindNinjutsuDefender(ctx *effects.Ctx, o *state.Object) {
	ab := o.Ability
	if ab == nil || !saHasKeyword(ab, "Ninjutsu") {
		return
	}
	for _, rem := range o.Remembered {
		if rem.IsPlayer {
			ctx.DefendingPlayer = rem
			continue
		}
		if rem.Obj != 0 {
			ctx.DefendingBattle = rem.Obj
		}
	}
}

// abilityXAnnounced is an ability stack object's CR 107.3i announced-X flag:
// the stack object's own announcement, or a paid nonzero X recorded by
// commitCast's CastInfo.
func abilityXAnnounced(o *state.Object) bool {
	return stackXAnnounced(o) || o.X != 0
}
