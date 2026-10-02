package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// castSubAsk returns the pending decision after asserting it is a chain
// link's CAST-TIME target ask (CR 601.2c): a KTarget posed by subTargetAsk
// (ResumeKind "cast_sub") while the spell or ability is still being
// announced -- never the mid-resolution "tgts" KChoose.
func castSubAsk(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "cast_sub" {
		t.Fatalf("pending = %+v, want the chain link's cast-time KTarget ask (ResumeKind cast_sub, CR 601.2c)", d)
	}
	return d
}

// answerCastSubObj answers the pending cast-time chain ask with the object.
func answerCastSubObj(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	d := castSubAsk(t, e)
	for _, o := range d.Options {
		if o.Kind != "player" && o.Obj == id {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("object %d is not offered by the cast-time chain ask: %+v", id, d.Options)
}

// answerCastSubPlayer answers the pending cast-time chain ask with the seat.
func answerCastSubPlayer(t *testing.T, e *Engine, p state.PlayerID) {
	t.Helper()
	d := castSubAsk(t, e)
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == p {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("seat %d is not offered by the cast-time chain ask: %+v", p, d.Options)
}
