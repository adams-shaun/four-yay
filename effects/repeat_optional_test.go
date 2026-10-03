package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
)

func TestRepeatOptionalRecordsCursorWhenBodySuspends(t *testing.T) {
	Register("TestRepeatOptionalSuspendingBody", func(Host, *Ctx, *cards.SA) {})
	t.Cleanup(func() { unregister("TestRepeatOptionalSuspendingBody") })

	h, c := fixtureHost(t)
	h.suspendAfterAsk = true
	c.SVars = map[string]string{"Body": "DB$ TestRepeatOptionalSuspendingBody"}
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Repeat", Params: map[string]string{
		"RepeatSubAbility": "Body", "RepeatOptional": "True",
	}})
	if !h.repeatBodyCalled {
		t.Fatal("RepeatOptional body suspension did not register a continuation")
	}
	if h.repeatBodyNext != 1 {
		t.Fatalf("RepeatOptional cursor = %d, want 1", h.repeatBodyNext)
	}
}

func askNext(d *decision.Decision) int32 {
	if d == nil {
		return -1
	}
	return d.ResumeRepeatNext
}
