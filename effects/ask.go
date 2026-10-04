package effects

import (
	"github.com/adams-shaun/gorge/decision"
)

// OnlyEmptyAnswer reports whether d's ONLY legal answer is the empty one:
// Min 0 is what makes the empty answer legal, and Max 0 -- or a decision
// with no options at all -- is what makes it the only one. A Min 0 / Max 0
// decision with no options (the Squadron Hawk fail-to-find search was the
// live instance) cannot be answered differently by any seat, so posing it
// wedges a client that has no control to send: the seat must answer, and no
// answer exists that the client could render. Such a decision is resolved
// silently by the asking primitive's own deterministic path instead, never
// posted. Exported so rules' single posting boundary (Engine.ask) can reject
// the shape for every construction site at once, including any future one
// that skips the helper.
func OnlyEmptyAnswer(d *decision.Decision) bool {
	return d.Min == 0 && (d.Max == 0 || len(d.Options) == 0)
}

// askUnposable reports whether d is a decision the ask boundary never poses:
// a nil one, one whose only legal answer is the empty one (OnlyEmptyAnswer),
// or one with no options at all (with Min > 0 no answer is legal, and posting
// it would strand the seat). The asking primitive resolves such a decision
// through its deterministic path without the no-answer Note.
func askUnposable(d *decision.Decision) bool {
	return d == nil || OnlyEmptyAnswer(d) || len(d.Options) == 0
}

// askSeam is the optional host seam of rules' mid-resolution ask machinery.
//
// AskCount counts the mid-resolution asks the host has taken, posed or
// deferred. rules' Engine may DEFER a second ask posed while an earlier ask
// of the same resolution pass is still pending (it rides the resume chain
// and is posed once the earlier one resolves), so Suspended() alone cannot
// tell a caller that its own ask was taken.
//
// TapeAnswer is the resolution kernel's converted ask boundary (AskTape,
// ask_tape.go).
type askSeam interface {
	AskCount() uint64
	TapeAnswer(d *decision.Decision) (decision.Intent, bool)
}

// askSeamOf is h's ask seam, nil for a host without one (a test double, whose
// asks are always visible through Suspended() and which has no tape).
func askSeamOf(h Host) askSeam {
	s, _ := h.(askSeam)
	return s
}

// askCount is h's ask count, or 0 for a host without the seam.
func askCount(h Host) uint64 {
	if s := askSeamOf(h); s != nil {
		return s.AskCount()
	}
	return 0
}

// eventMarker is the optional host seam exposing the event log's length and
// whether any state-changing event (anything but a Note) was logged after a
// mark. effRepeat's RepeatOptional$ loop reads it to recognise an iteration
// that changed nothing, whose repeat can only reproduce the same no-op.
type eventMarker interface {
	EventMark() int
	StateChangedSince(mark int) bool
}

// eventMark is h's current event-log mark; ok is false for a host without
// the seam (the test doubles), which keeps every caller's old behaviour.
func eventMark(h Host) (mark int, ok bool) {
	if m, is := h.(eventMarker); is {
		return m.EventMark(), true
	}
	return 0, false
}

// stateChangedSince reports whether h logged a state-changing event after
// mark. A host without the seam reports true (assume progress).
func stateChangedSince(h Host, mark int) bool {
	if m, is := h.(eventMarker); is {
		return m.StateChangedSince(mark)
	}
	return true
}
