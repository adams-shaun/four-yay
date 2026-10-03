package rules

// resolve_record.go holds the per-kind answer records the resolution kernel
// writes when it serves a converted ask from its tape (resolveBoard.Record):
// the part of a decision's legacy handler that belongs to the ANSWER, not to
// the resume -- the events a Submit's handler emits before it re-enters the
// suspended resolution. Each record is shared with that handler, so the two
// paths emit the same events from one home; the asking code then simply
// continues with the answer.

import (
	"github.com/adams-shaun/gorge/decision"
)

// tapeAnswerRecord writes d's answer record for the acting intent in (the
// decision is the acting view, a CR 722 redirect applied).
func tapeAnswerRecord(e *Engine, d *decision.Decision, in decision.Intent) {
	switch d.Kind {
	case decision.KModes:
		obj := d.Source
		if n := len(e.G.Stack); n > 0 {
			obj = e.G.Stack[n-1] // the resolving object stays on the stack
		}
		recordModesAnswer(e, d, in.Player, d.Chosen(in), obj)
	case decision.KArrange:
		arrangeAnswerRecord(e, d, in, d.ResumeSA)
	}
	// Per-resume-kind records: the events a resume arm
	// (resumeAnswerBinding) emits before re-entering, for the arms whose
	// answer the asking effect cannot apply itself. Each is a free function
	// shared with its arm.
	switch d.ResumeKind {
	}
	// (An if-chain, not case arms: the stringCaseLiterals ratchet.)
	if d.ResumeKind == "extort" {
		extortAnswerRecord(e, d.Chosen(in))
	}
}
