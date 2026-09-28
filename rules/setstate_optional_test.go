package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// setStateDagger drives the real Dowsing Dagger damage trigger to its
// Optional$ Transform election. The source is a two-faced battlefield card;
// the effect must ask before changing it.
func setStateDagger(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg, id := gateFixture(t, seed, "Dowsing Dagger")
	if o := e.G.Obj(id); o == nil || o.Card == nil || len(o.Card.Faces) != 2 || o.FaceIdx != 0 {
		t.Fatalf("precondition: expected front face of a two-faced Dowsing Dagger, got %+v", o)
	}
	id = gateMoveFromLibrary(t, e, "Dowsing Dagger", state.ZBattlefield)
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Dagger zone = %v, want battlefield", o.Zone)
	}
	if f := e.G.Obj(id).Face(); f == nil || len(f.Triggers) < 2 || f.Triggers[1].Effect == nil || f.Triggers[1].Effect.API != "SetState" || f.Triggers[1].Effect.Params["Optional"] != "True" {
		t.Fatalf("precondition: Dagger's second trigger must be Optional$ SetState, got %+v", f)
	}
	e.emit(events.Event{Kind: events.TriggerPush, Obj: id, Player: 0, Amount: 1})
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "setstate_optional" || d.Min != 1 || d.Max != 1 {
		t.Fatalf("pending = %+v, want SetState optional election", d)
	}
	if d.Player != 0 || len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("election = %+v, want yes/no for seat 0", d)
	}
	if e.G.Obj(id).FaceIdx != 0 || hasEvent(e, events.FlipFace, id) {
		t.Fatal("Dagger transformed before the election")
	}
	return e, cfg, id
}

func TestSetStateOptionalDaggerDeclineAndAccept(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		seed   uint64
		answer int
		face   uint8
		flips  int
	}{
		{"decline", 917, 1, 0, 0},
		{"accept", 918, 0, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg, id := setStateDagger(t, tc.seed)
			submitChoices(t, e, tc.answer)
			// Dagger's independent enter-the-battlefield token trigger may
			// still be pending and ask for an opponent target. It does not
			// change the SetState answer or the resulting face.
			if d := e.Pending(); d != nil && d.ResumeKind == "setstate_optional" {
				t.Fatalf("SetState election still pending after answer: %+v", d)
			}
			if got := e.G.Obj(id).FaceIdx; got != tc.face {
				t.Fatalf("face = %d, want %d", got, tc.face)
			}
			flips := 0
			for _, ev := range e.L.Events {
				if ev.Kind == events.FlipFace && ev.Obj == id {
					flips++
				}
			}
			if flips != tc.flips {
				t.Fatalf("FlipFace events = %d, want %d", flips, tc.flips)
			}
			replayCheck(t, e, cfg)
		})
	}
}

func TestSetStateOptionalBotClampAnswerValid(t *testing.T) {
	t.Parallel()
	e, cfg, id := setStateDagger(t, 919)
	d := e.Pending()
	// The bot's deterministic first-option clamp has one home in botpolicy;
	// check its actual output against the real decision's validator.
	in := botpolicy.Clamp(d, decision.Intent{Seq: d.Seq, Player: d.Player})
	if len(in.Choices) != 1 || in.Choices[0] != 0 {
		t.Fatalf("bot clamp chose %v, want yes at option 0", in.Choices)
	}
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer rejected: %v", err)
	}
	submitChoices(t, e, in.Choices...)
	if e.G.Obj(id).FaceIdx != 1 {
		t.Fatalf("bot's yes left face at %d, want 1", e.G.Obj(id).FaceIdx)
	}
	replayCheck(t, e, cfg)
}
