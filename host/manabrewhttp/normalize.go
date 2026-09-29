package manabrewhttp

import (
	"reflect"

	mb "github.com/adams-shaun/gorge/protocol/manabrew"
)

// normalizeClientMessage works around a decode/translate mismatch this
// ticket's own wire testing found between protocol/manabrew (MB-1) and
// internal/manabrew (MB-4/5/6), and does not belong to either of those
// packages' files (out of this ticket's scope to edit): every entry in
// protocol/manabrew's promptOutputCases table constructs a POINTER
// (`func() PromptOutputValue { return &PassOutput{} }`), so
// PromptOutputData.UnmarshalJSON always leaves Value holding a *T after a
// real JSON decode. internal/manabrew's TranslateResponse and its per-kind
// parsers, though, type-switch on the VALUE (`out.(mb.PassOutput)`,
// `out.(mb.MulliganDecision)`, …) — a shape that only ever matches a
// hand-built Go value, never anything that actually went through Decode.
// Every response kind but "act" is affected (chooseAction's PassOutput,
// RestoreSnapshotOutput; mulligan; mulliganPutBack; chooseBoardTargets;
// chooseAttackers; chooseBlockers), which is presumably why it was never
// caught: internal/manabrew's own tests construct values directly and call
// the translator in-process, never round-tripping a message through
// Decode/Encode the way an actual ManaBrew client's bytes do. This
// transport is the first thing that exercises the full wire path, so it is
// the first thing to hit it.
//
// This function is the one place gorge turns wire bytes into what the
// translator consumes (send.go), so it is the correct seam to paper over
// the mismatch without reaching into either package's files: it dereferences
// a decoded response's output back to the value type the switches expect,
// which is observably harmless (every concrete type's methods have value
// receivers, so a dereferenced value still satisfies PromptOutputValue) and
// restores every response kind to working over real JSON. The real fix
// belongs in whichever package the reviewer decides should not produce this
// mismatch in the first place (protocol/manabrew's promptOutputCases
// returning values, or internal/manabrew switching on pointers) — see this
// ticket's final report.
func normalizeClientMessage(msg mb.ClientMessage) mb.ClientMessage {
	cr, ok := msg.Value.(mb.ClientResponse)
	if !ok {
		return msg
	}
	cr.Action.Output.Value = dereferencePromptOutput(cr.Action.Output.Value)
	return mb.ClientMessage{Value: cr}
}

// dereferencePromptOutput returns v itself unless it is a non-nil pointer to
// a PromptOutputValue-satisfying type, in which case it returns the pointed-
// to value.
func dereferencePromptOutput(v mb.PromptOutputValue) mb.PromptOutputValue {
	if v == nil {
		return v
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return v
	}
	elem := rv.Elem().Interface()
	dv, ok := elem.(mb.PromptOutputValue)
	if !ok {
		return v
	}
	return dv
}
