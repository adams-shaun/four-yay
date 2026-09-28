package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The ward pay-or-decline ask (effects.effWard, the constructor the generic
// poseUnlessAsk path deliberately skips) must mark its options with the
// explicit pay/decline semantic, exactly as the unless-pay asks do: an
// unmarked option list makes rules' unless_pay arm read every answer as a
// decline, silently countering every ward target whose cost WAS offered.
// The labels stay the user-facing ones; the marker is the meaning.
func TestWardAskOptionsCarryExplicitPayDeclineMarkers(t *testing.T) {
	t.Parallel()
	e, bear := hexingEngine(t)
	if e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatal("precondition: the warded bear must be on the battlefield")
	}
	e.G.Players[1].Life = 12
	opponentBoltAt(t, e, bear)
	dw := e.Pending()
	if dw == nil || dw.Kind != decision.KModes || dw.Player != 1 || len(dw.Options) != 2 {
		t.Fatalf("precondition: expected the ward pay-or-decline ask with two options, got %+v", dw)
	}
	pay := dw.Options[0]
	decline := dw.Options[1]
	if !strings.HasPrefix(pay.Label, "Pay ") || decline.Label != "Don't pay" {
		t.Fatalf("precondition: unexpected ward labels %q / %q", pay.Label, decline.Label)
	}
	if pay.Mode != decision.ModeUnlessPay || decline.Mode != decision.ModeUnlessDecline {
		t.Fatalf("ward ask options must carry the explicit semantic, got pay=%+v decline=%+v", pay, decline)
	}

	// Answering the explicitly-marked Pay option charges the cost — the
	// consumer reads the marker, not the position.
	submitChoices(t, e, pay.Index)
	passUntilStackEmpty(t, e, 20)
	if life := e.G.Players[1].Life; life != 10 {
		t.Fatalf("seat 1 life=%d, want 10 (the marked Pay option paid the ward's 2 life)", life)
	}
	if e.G.Obj(bear).Zone != state.ZGraveyard {
		t.Fatalf("bear zone=%s, want graveyard (the bolt resolved after the ward was paid)", e.G.Obj(bear).Zone)
	}
}
