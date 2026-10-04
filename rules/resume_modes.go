package rules

import (
	"github.com/adams-shaun/gorge/state"
)

// Ctx.Modes is a ONE-SHOT answer: the first mode reader a resolution walk
// reaches (effCharm, the per-player GenericChoice loop, effVillainousChoice,
// a KWChoice$ Pump) consumes and clears it. A resumed resolution builds a
// fresh Ctx, so it may leave Modes non-nil only when the answer it binds IS
// a mode selection of the SA it re-enters, or the announced modes of the
// ability's own root. Anything else is stale, and the first reader the walk
// reaches takes it as its own answer.

// resumeChosenModes is the Ctx.Modes seed a resumed ability frame starts
// from. CR 603.3c: a modal triggered (or activated) ability's modes were
// announced when it was put on the stack, and resolveTop's first pass seeds
// them so the ROOT Charm runs exactly those modes. The seed is owed only to
// a frame that re-enters that root (an accepted optional trigger, a paid
// trigger cost, a root target pre-ask). Every other frame re-enters some SA
// INSIDE the chain the root already ran: the root consumed the announcement
// on the first pass (its remaining modes ride a charm_rest frame with their
// own Ctx.Modes), so the seed there is stale, and the first mode reader the
// frame reaches -- a nested Charm, a per-player GenericChoice, a KWChoice$
// Pump -- would take the root's mode names as its own answer (re-running
// the root's modes, or reading a GenericChoice chooser at index -1).
func resumeChosenModes(rp *resumePoint, o *state.Object) []string {
	if o == nil || o.Ability == nil || rp.sa == nil {
		return nil
	}
	// Pointer identity, or the same SVar body text: an SA reached through
	// ResolveSVar is parsed fresh, so a re-entered root need not be the
	// stack object's own pointer.
	if rp.sa == o.Ability || (rp.sa.Line != "" && rp.sa.Line == o.Ability.Line) {
		return o.ChosenModes
	}
	return nil
}
