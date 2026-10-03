package effects

import "github.com/adams-shaun/gorge/decision"

// AskTape is the resolution kernel's converted ask boundary (lasagna spec
// §7, W3; rules/resolve): a converted asking primitive calls it before its
// legacy Ask. ok means the answer is in hand -- served from the kernel's
// intent tape, or by an all-policy engine's synchronous answerer -- and the
// primitive simply continues with it, holding every local it already
// computed: there is nothing to resume. !ok (no tape run, a host without the
// seam, or a shape Ask resolves silently) falls through to the legacy Ask,
// which suspends as before. With the kernel off (the default) it is always
// !ok.
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
	if d == nil || OnlyEmptyAnswer(d) || len(d.Options) == 0 {
		return decision.Intent{}, false
	}
	if s := askSeamOf(h); s != nil {
		return s.TapeAnswer(d)
	}
	return decision.Intent{}, false
}
