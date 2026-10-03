package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// chooseNumberHost is a scripted-ask double for the mid-resolution
// ChooseNumber ask (task cli-20260923T060000Z-choose-number): Ask records
// every posed decision and reports suspended, so the test can drive the
// engine's two passes (ask, then answer through Ctx.ChosenNumberPick /
// ChosenNumberAnswered) the way rules' resumeResolution does.
type chooseNumberHost struct {
	fakeHost
	asks      []*decision.Decision
	suspended bool
}

func (h *chooseNumberHost) Ask(d *decision.Decision) bool {
	cp := *d
	cp.Options = append([]decision.Option(nil), d.Options...)
	h.asks = append(h.asks, &cp)
	h.suspended = true
	return true
}

func (h *chooseNumberHost) Suspended() bool { return h.suspended }

// chooseNumberEvents collects the Choose "number" events a resolution
// emitted.
func chooseNumberEvents(h *fakeHost) []events.Event {
	var out []events.Event
	for _, e := range h.log {
		if e.Kind == events.Choose && e.Counter == "number" {
			out = append(out, e)
		}
	}
	return out
}

// chooseNumberSrc builds a 2-seat game with one land source for seat 0.
func chooseNumberSrc(t *testing.T, h *fakeHost) state.ObjID {
	t.Helper()
	return h.g.AddObject(mkCard(t, "Name:Source\nTypes:Land\nOracle:x\n"), 0).ID
}

// TestChooseNumberNoHostDegradesToZero pins R-9: on a host that cannot ask
// (the plain fakeHost), the ask falls through to the deterministic fallback
// 0 with no extra Note, preserving the old stand-in's event byte-for-byte.
func TestChooseNumberNoHostDegradesToZero(t *testing.T) {
	h := newHost(t, 2)
	src := chooseNumberSrc(t, h)
	Resolve(h, &Ctx{Source: src, Controller: 0}, sa(t, "DB$ ChooseNumber | Defined$ You"))
	if h.g.Obj(src).ChosenNumber != 0 {
		t.Fatalf("no-host fallback = %d, want the deterministic 0", h.g.Obj(src).ChosenNumber)
	}
	evs := chooseNumberEvents(h)
	if len(evs) != 1 || evs[0].Amount != 0 {
		t.Fatalf("no-host Choose events = %+v, want exactly one Amount 0", evs)
	}
	for _, e := range h.log {
		if e.Kind == events.Note {
			t.Fatalf("unexpected Note on the no-host degradation: %+v", e)
		}
	}
}

// TestChooseNumberEntryBodyStaysTheNoOp pins the preserved half: the as-enters
// ENTRY-choice body (Ctx.ETBNumberRecorded -- rules' replCtx flags the
// K:ETBReplacement ChooseNumber repl's body) never poses an ask, even when
// the recorded entry answer is 0 (the value a bare o.ChosenNumber guard could
// not distinguish from unset). The flag is consumed so a nested ChooseNumber
// poses its own ask.
func TestChooseNumberEntryBodyStaysTheNoOp(t *testing.T) {
	// Recorded entry answer 0: the ambiguous case the flag exists for.
	h := &chooseNumberHost{}
	h.g = state.NewGame(names(2))
	src := chooseNumberSrc(t, &h.fakeHost)
	h.Emit(events.Event{Kind: events.Choose, Obj: src, Counter: "number", Amount: 0})
	ctx := &Ctx{Source: src, Controller: 0, ETBNumberRecorded: true}
	before := len(h.log)
	Resolve(h, ctx, sa(t, "DB$ ChooseNumber"))
	if len(h.asks) != 0 {
		t.Fatalf("the entry body posed %d asks, want none: %+v", len(h.asks), h.asks)
	}
	if len(h.log) != before {
		t.Fatalf("the entry body emitted %d event(s), want none", len(h.log)-before)
	}
	if ctx.ETBNumberRecorded {
		t.Fatalf("the entry-body flag was not consumed")
	}
	// A fresh resolution after the entry body still asks (the flag is gone).
	Resolve(h, ctx, sa(t, "SP$ ChooseNumber | Defined$ You"))
	if len(h.asks) != 1 {
		t.Fatalf("a resolution-time ask after the entry body posed %d asks, want one", len(h.asks))
	}
}
