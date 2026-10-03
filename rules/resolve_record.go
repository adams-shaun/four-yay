package rules

// resolve_record.go holds the per-kind answer records the resolution kernel
// writes when it serves a converted ask from its tape (resolveBoard.Record):
// the part of a decision's legacy handler that belongs to the ANSWER, not to
// the resume -- the events a Submit's handler emits before it re-enters the
// suspended resolution. Each record is shared with that handler, so the two
// paths emit the same events from one home; the asking code then simply
// continues with the answer.

import (
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
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
		if k := d.ResumeKind; k == "villainous" || k == "generic_players" {
			// A per-victim / per-chooser pick: handleModes' own arms
			// record only the ModeChosen marker (no SetChosenMode, no
			// ChoiceRestriction$ record).
			e.emit(events.Event{Kind: events.ModeChosen, Obj: obj, Player: in.Player,
				Text: strings.Join(chosenModeLabels(d.Chosen(in)), ",")})
		} else {
			recordModesAnswer(e, d, in.Player, d.Chosen(in), obj)
		}
	case decision.KArrange:
		arrangeAnswerRecord(e, d, in, d.ResumeSA)
	}
	// Per-resume-kind records: the events a resume arm
	// (resumeAnswerBinding) emits before re-entering, for the arms whose
	// answer the asking effect cannot apply itself. Each is a free function
	// shared with its arm. (An if-chain, not case arms: the
	// stringCaseLiterals ratchet.)
	if d.ResumeKind == resumeKindDredge {
		dredgeAnswerApply(e, d.Acting(), d.Chosen(in))
	}
	if d.ResumeKind == "extort" {
		extortAnswerRecord(e, d.Chosen(in))
	}
}
