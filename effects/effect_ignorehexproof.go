package effects

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// effectUnsupportedStatic handles a statically unknown Effect body. Detection
// Tower's targeting exception is one supported mode delivered this way.
func effectUnsupportedStatic(h Host, c *Ctx, mode string, params staticLineParams, name, rawDur, dur, what string) bool {
	if mode == ModeIgnoreHexproof && len(params["ValidEntity"]) > 0 &&
		staticKeysReadable(params, "Mode", "Activator", "ValidEntity", "Description") {
		effectContinuous(h, state.ContinuousEffect{
			Source: c.Source, Controller: c.Controller, Name: name,
			UntilEOT: effectUntilEOT(h, c.Source, rawDur), Duration: dur,
			Restriction: mode, RestrictParams: params,
		})
		return true
	}
	if len(mode) > 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "continuous effect " + mode + " unimplemented (" + what + ")"})
	}
	return false
}
