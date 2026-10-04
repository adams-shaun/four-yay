package rules

// Restored from effects/attach_test.go (W3 legacy removal): the Attach
// Choices$ asks, answered through the resolution kernel on a real engine.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const (
	kr0SwordSrc = "Name:Sword\nManaCost:3\nTypes:Artifact Equipment\nOracle:x\n"
	kr0BearSrc  = "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
)

// kr0AttachBoard puts an Equipment (the source) and a Bear on seat 0's
// battlefield.
func kr0AttachBoard(t *testing.T) (e *Engine, eq, bear state.ObjID) {
	t.Helper()
	e = kr0Engine(t, 2)
	eq = kr0Src(t, e, 0, kr0SwordSrc, state.ZBattlefield)
	bear = kr0Src(t, e, 0, kr0BearSrc, state.ZBattlefield)
	return e, eq, bear
}

func kr0Attaches(evs []events.Event) []events.Event {
	var out []events.Event
	for _, ev := range evs {
		if ev.Kind == events.Attach {
			out = append(out, ev)
		}
	}
	return out
}

// TestAttachChoicesObjectPoolAsksAndAnsweredReentryAttachesKernel: Choices$
// without Object$ names the OBJECT to attach (Goldwardens' Gambit). Two
// Equipment candidates pose a KChoose; nothing attaches before the answer;
// the answered Equipment attaches to the Defined$ destination and
// RememberAttached$ remembers it.
func TestAttachChoicesObjectPoolAsksAndAnsweredReentryAttachesKernel(t *testing.T) {
	t.Parallel()
	e, eq, bear := kr0AttachBoard(t)
	eq2 := kr0Src(t, e, 0, "Name:Sword2\nManaCost:3\nTypes:Artifact Equipment\nOracle:x\n", state.ZBattlefield)
	s := kr0SA(t, "DB$ Attach | Choices$ Equipment.YouCtrl+!IsRemembered | Defined$ Remembered | RememberAttached$ True")
	start := len(e.L.Events)
	var last *effects.Ctx
	d := kr0Run(t, e, s, func() *effects.Ctx {
		return &effects.Ctx{Source: eq, Controller: 0, Remembered: []state.Target{{Obj: bear}}}
	}, &last)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "attach_choice" || len(d.Options) != 2 {
		t.Fatalf("object pool posed %+v, want a 2-option attach_choice", d)
	}
	if got := kr0Attaches(kr0Since(e, start)); len(got) != 0 {
		t.Fatalf("unanswered ask attached: %+v", got)
	}
	kr0Answer(t, e, kr0Opt(t, d, eq2))
	got := kr0Attaches(kr0Since(e, start))
	if len(got) != 1 || got[0].Obj != eq2 || len(got[0].IDs) == 0 || got[0].IDs[0] != bear {
		t.Fatalf("attachs = %+v, want eq2->bear", got)
	}
	if e.G.Obj(eq2).AttachedTo != bear || e.G.Obj(eq).AttachedTo != 0 {
		t.Fatalf("AttachedTo eq2=%d eq=%d, want eq2 on the bear only", e.G.Obj(eq2).AttachedTo, e.G.Obj(eq).AttachedTo)
	}
	found := false
	for _, r := range last.Remembered {
		found = found || r.Obj == eq2
	}
	if !found {
		t.Fatalf("ctx Remembered = %+v, want the chosen equipment", last.Remembered)
	}
}

// TestAttachChoicesSingleCandidateTakesItAndEmptyAnswerDeclinesKernel: a
// mandatory pool with one candidate takes it without an ask; an Optional$
// pool answered with nothing chosen is the decline -- no Attach, no Note.
func TestAttachChoicesSingleCandidateTakesItAndEmptyAnswerDeclinesKernel(t *testing.T) {
	t.Parallel()
	e, eq, bear := kr0AttachBoard(t)
	mk := func() *effects.Ctx {
		return &effects.Ctx{Source: eq, Controller: 0, Remembered: []state.Target{{Obj: bear}}}
	}
	if d := kr0Run(t, e, kr0SA(t, "DB$ Attach | Choices$ Equipment.YouCtrl | Defined$ Remembered | RememberAttached$ True"), mk, nil); d != nil {
		t.Fatalf("single-candidate mandatory pool asked %+v", d)
	}
	if e.G.Obj(eq).AttachedTo != bear {
		t.Fatal("single-candidate mandatory pool did not attach")
	}

	e2, eq2, bear2 := kr0AttachBoard(t)
	start := len(e2.L.Events)
	d := kr0Run(t, e2, kr0SA(t, "DB$ Attach | Optional$ True | Choices$ Equipment.YouCtrl | Defined$ Remembered"), func() *effects.Ctx {
		return &effects.Ctx{Source: eq2, Controller: 0, Remembered: []state.Target{{Obj: bear2}}}
	}, nil)
	if d == nil || d.Min != 0 {
		t.Fatalf("optional pool posed %+v, want a Min-0 ask", d)
	}
	kr0Answer(t, e2)
	evs := kr0Since(e2, start)
	if got := kr0Attaches(evs); len(got) != 0 {
		t.Fatalf("decline emitted Attach: %+v", got)
	}
	if n := kr0Count(evs, events.Note); n != 0 {
		t.Fatalf("decline must be silent, got %d note(s): %+v", n, evs)
	}
	if e2.G.Obj(eq2).AttachedTo != 0 {
		t.Fatal("declined optional attach still attached")
	}
}

// TestAttachDestinationAskNeverOffersTheSourceAndRefusesAChosenSourceKernel:
// aura_graft's `Object$ Self | Choices$ Permanent` admits the attaching
// object itself; the destination ask offers only the legal destinations
// (never the source), and answering a legal one attaches there.
func TestAttachDestinationAskNeverOffersTheSourceAndRefusesAChosenSourceKernel(t *testing.T) {
	t.Parallel()
	e, eq, bear := kr0AttachBoard(t)
	bear2 := kr0Src(t, e, 0, kr0BearSrc, state.ZBattlefield)
	d := kr0Run(t, e, kr0SA(t, "DB$ Attach | Object$ Self | Choices$ Permanent"), func() *effects.Ctx {
		return &effects.Ctx{Source: eq, Controller: 0}
	}, nil)
	if d == nil {
		t.Fatal("expected a destination ask, none posed")
	}
	for _, o := range d.Options {
		if o.Obj == eq {
			t.Fatalf("the attaching object was offered as its own destination: %+v", d.Options)
		}
		if o.Obj != bear && o.Obj != bear2 {
			t.Fatalf("unexpected option %+v in %+v", o, d.Options)
		}
	}
	if len(d.Options) != 2 {
		t.Fatalf("options = %+v, want the two bears", d.Options)
	}
	kr0Answer(t, e, kr0Opt(t, d, bear2))
	if got := e.G.Obj(eq).AttachedTo; got != bear2 {
		t.Fatalf("legal destination answer: AttachedTo = %d, want %d", got, bear2)
	}
}
