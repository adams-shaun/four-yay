package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// chooseColorHost is a scripted-ask double for the mid-resolution ChooseColor
// ask (task cli-20260923T060000Z-choose-color): Ask records every posed
// decision and reports suspended, so the test can drive the engine's two
// passes (ask, then answer through Ctx.ChosenColor) the way rules'
// resumeResolution does.
type chooseColorHost struct {
	fakeHost
	asks      []*decision.Decision
	suspended bool
}

func (h *chooseColorHost) Ask(d *decision.Decision) bool {
	cp := *d
	cp.Options = append([]decision.Option(nil), d.Options...)
	h.asks = append(h.asks, &cp)
	h.suspended = true
	return true
}

func (h *chooseColorHost) Suspended() bool { return h.suspended }

// chooseColorEvents collects the Choose events a resolution emitted.
func chooseColorEvents(h *fakeHost) []events.Event {
	return chooseColorLog(h.log)
}

// chooseColorLog collects the Choose "color" events in an event slice.
func chooseColorLog(log []events.Event) []events.Event {
	var out []events.Event
	for _, e := range log {
		if e.Kind == events.Choose && e.Counter == "color" {
			out = append(out, e)
		}
	}
	return out
}

// chooseColorSrc builds a 2-seat game with one land source for seat 0.
func chooseColorSrc(t *testing.T, h *fakeHost) state.ObjID {
	t.Helper()
	return h.g.AddObject(mkCard(t, "Name:Source\nTypes:Land\nOracle:x\n"), 0).ID
}

// The wantOptions list every unrestricted ChooseColor ask must offer: the
// fixed WUBRG order with full colour names as Labels, the same list shape
// the cast-time "as this enters" colour ask offers.
var wantWUBRGOptions = []decision.Option{
	{Index: 0, Kind: "color", Label: "White"},
	{Index: 1, Kind: "color", Label: "Blue"},
	{Index: 2, Kind: "color", Label: "Black"},
	{Index: 3, Kind: "color", Label: "Red"},
	{Index: 4, Kind: "color", Label: "Green"},
}

// TestChooseColorFallbackRespectsTheExclusion pins the deterministic
// no-host degradation reading the SA's own colour restriction: on a host
// that cannot ask (the plain fakeHost), Exclude$ White removes White from
// the option list, so the fallback records the next colour "U" -- NOT the
// first-WUBRG "W" the pre-fix stand-in always recorded.
func TestChooseColorFallbackRespectsTheExclusion(t *testing.T) {
	h := newHost(t, 2)
	src := chooseColorSrc(t, h)
	Resolve(h, &Ctx{Source: src, Controller: 0}, sa(t, "DB$ ChooseColor | Defined$ You | Exclude$ White"))
	if h.g.Obj(src).ChosenColor != "U" {
		t.Fatalf("excluded-White fallback = %q, want U", h.g.Obj(src).ChosenColor)
	}
	evs := chooseColorEvents(h)
	if len(evs) != 1 || evs[0].Text != "U" {
		t.Fatalf("excluded-White Choose events = %+v, want exactly one \"U\"", evs)
	}
}

// TestChooseColorRandomStaysTheSilentDeterministicFallback pins Random$:
// the card text makes the choice a die roll, never a player's pick, so no
// ask is posed and the deterministic first-WUBRG "W" stands in exactly as
// the pre-fix behaviour did (no Note, no ask, one Choose).
func TestChooseColorRandomStaysTheSilentDeterministicFallback(t *testing.T) {
	h := newHost(t, 2)
	src := chooseColorSrc(t, h)
	Resolve(h, &Ctx{Source: src, Controller: 0}, sa(t, "DB$ ChooseColor | Random$ True"))
	if h.g.Obj(src).ChosenColor != "W" {
		t.Fatalf("Random$ fallback = %q, want the deterministic W", h.g.Obj(src).ChosenColor)
	}
	if evs := chooseColorEvents(h); len(evs) != 1 || evs[0].Text != "W" {
		t.Fatalf("Random$ Choose events = %+v, want exactly one \"W\"", evs)
	}
	for _, e := range h.log {
		if e.Kind == events.Note {
			t.Fatalf("unexpected Note on a Random$ carrier: %+v", e)
		}
	}
}

// TestChooseColorUnaskableShapesNoteAndFallBack pins the loud convention the
// unsupported ChooseType category carries: a mid-resolution carrier whose
// option list this build cannot build (TwoColors$) keeps the deterministic
// first-WUBRG "W" AND emits the one Note naming the parameter, so the
// degradation is never silent.
func TestChooseColorUnaskableShapesNoteAndFallBack(t *testing.T) {
	h := newHost(t, 2)
	src := chooseColorSrc(t, h)
	Resolve(h, &Ctx{Source: src, Controller: 0}, sa(t, "DB$ ChooseColor | Defined$ You | TwoColors$ True"))
	if h.g.Obj(src).ChosenColor != "W" {
		t.Fatalf("TwoColors$ fallback = %q, want the deterministic W", h.g.Obj(src).ChosenColor)
	}
	notes := 0
	for _, e := range h.log {
		if e.Kind == events.Note {
			notes++
			if !strings.Contains(e.Text, "TwoColors$") {
				t.Fatalf("Note = %q, want it to name the unaskable parameter", e.Text)
			}
		}
	}
	if notes != 1 {
		t.Fatalf("TwoColors$ emitted %d Notes, want exactly one", notes)
	}
	if evs := chooseColorEvents(h); len(evs) != 1 || evs[0].Text != "W" {
		t.Fatalf("TwoColors$ Choose events = %+v, want exactly one \"W\"", evs)
	}
}

// TestChooseColorEntryBodyStaysTheNoOp pins the preserved half of the same
// finding: the as-enters ENTRY-choice body (Ctx.ETBColorRecorded -- rules'
// replCtx flags the K:ETBReplacement ChooseColor repl's body) never poses an
// ask. With the entry answer already recorded it emits NOTHING (the
// machinery recorded it); with no recorded entry answer it emits exactly the
// deterministic fallback Choose event. Either way the pre-fix entry
// behaviour is byte-identical.
func TestChooseColorEntryBodyStaysTheNoOp(t *testing.T) {
	// Recorded entry answer: complete no-op.
	h := &chooseColorHost{}
	h.g = state.NewGame(names(2))
	src := chooseColorSrc(t, &h.fakeHost)
	h.Emit(events.Event{Kind: events.Choose, Obj: src, Counter: "color", Text: "B"})
	if got := h.g.Obj(src).ChosenColor; got != "B" {
		t.Fatalf("precondition: the entry answer did not record: %q", got)
	}
	before := len(h.log)
	Resolve(h, &Ctx{Source: src, Controller: 0, ETB: ETBRecords{ColorRecorded: true}}, sa(t, "DB$ ChooseColor"))
	if len(h.asks) != 0 {
		t.Fatalf("the entry body posed %d asks, want none: %+v", len(h.asks), h.asks)
	}
	if len(h.log) != before {
		t.Fatalf("the entry body emitted %d event(s) with the answer already recorded, want none", len(h.log)-before)
	}
	if got := h.g.Obj(src).ChosenColor; got != "B" {
		t.Fatalf("recorded entry answer moved to %q, want B", got)
	}
	// No recorded entry answer (a malformed entry answer the fold could not
	// name): the deterministic fallback, still with no ask. Precondition: the
	// source carries no choice.
	h2 := &chooseColorHost{}
	h2.g = state.NewGame(names(2))
	src2 := chooseColorSrc(t, &h2.fakeHost)
	if got := h2.g.Obj(src2).ChosenColor; got != "" {
		t.Fatalf("precondition: fresh source already carries %q", got)
	}
	Resolve(h2, &Ctx{Source: src2, Controller: 0, ETB: ETBRecords{ColorRecorded: true}}, sa(t, "DB$ ChooseColor"))
	if len(h2.asks) != 0 {
		t.Fatalf("the unrecorded entry body posed %d asks, want none", len(h2.asks))
	}
	evs := chooseColorEvents(&h2.fakeHost)
	if len(evs) != 1 || evs[0].Text != "W" {
		t.Fatalf("unrecorded entry body Choose events = %+v, want exactly one \"W\"", evs)
	}
}
