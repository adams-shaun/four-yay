package effects

import (
	"github.com/adams-shaun/gorge/decision"
)

// AskTape is the resolution kernel's ask boundary (lasagna spec §7, W3;
// rules/resolve). ok means the answer is in hand -- served from the kernel's
// intent tape, or by an all-policy engine's synchronous answerer -- and the
// primitive simply continues with it. !ok means no answer is served (no tape
// run, an unless-pay election outside a run, a host without the seam): the
// decision is handed to Host.Ask as an observation, so the host records that
// the ask was answered with its default, and the site applies its
// deterministic stand-in.
//
// A tape run that is out of answers does not return: the kernel poses d and
// unwinds the run to its checkpoint, and re-executes it from there once the
// answer is submitted.
func AskTape(h Host, d *decision.Decision) ([]decision.Option, bool) {
	in, ok := AskTapeIntent(h, d)
	if !ok {
		return nil, false
	}
	return d.Chosen(in), true
}

// AskTapeIntent is AskTape for a site that reads more of the answer than its
// chosen options (an arrange's Rest, the pile-B order): the intent the
// asking code acts on, re-seated on the seat d is asked OF.
func AskTapeIntent(h Host, d *decision.Decision) (decision.Intent, bool) {
	if in, ok, posable := tapeAnswer(h, d); ok || !posable {
		return in, ok
	}
	// Unserved: the host observes the ask (rules emits its deterministic
	// "ask answered with its default" Note) and the site's stand-in applies.
	h.Ask(d)
	return decision.Intent{}, false
}

// tapeAnswer is the tape lookup alone, without AskTapeIntent's unserved
// observation: for a site that hands an unserved decision to Host.Ask
// itself (effMana's off-stack colour choice, which the host poses). posable
// is false for a decision the boundary never poses (askUnposable).
func tapeAnswer(h Host, d *decision.Decision) (in decision.Intent, ok, posable bool) {
	if askUnposable(d) {
		return decision.Intent{}, false, false
	}
	if s := askSeamOf(h); s != nil {
		in, ok = s.TapeAnswer(d)
		return in, ok, true
	}
	return decision.Intent{}, false, true
}
