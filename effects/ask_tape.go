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
	if d == nil || OnlyEmptyAnswer(d) || len(d.Options) == 0 {
		return nil, false
	}
	if s := askSeamOf(h); s != nil {
		return s.TapeAnswer(d)
	}
	return nil, false
}
