package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestFlipCoinMemoryFlagsAreRead pins the closure of the reviewed loud-note
// gate: Forge's per-flip memory parameters (RememberResult$, RememberNumber$,
// RememberLoser$) are now persisted, so a flip carrying them must NOT emit the
// old "unread" Note. This is the inverse of the pre-fix test that asserted one
// loud Note per flag.
func TestFlipCoinMemoryFlagsAreRead(t *testing.T) {
	cases := []string{
		"DB$ FlipCoin | RememberResult$ True",
		"DB$ FlipCoin | RememberNumber$ Wins",
		"DB$ FlipCoin | RememberLoser$ True",
		"DB$ FlipCoin | RememberResult$ True | RememberNumber$ Wins | RememberLoser$ True",
	}
	for _, line := range cases {
		h, c := fixtureHost(t)
		Resolve(h, c, sa(t, line))
		for _, ev := range h.log {
			if ev.Kind == events.Note && strings.Contains(ev.Text, "unread") {
				t.Fatalf("%q still emits a loud unread note: %q", line, ev.Text)
			}
		}
	}
}

// TestFlipCoinFlippedHeadsTailsResolve drives one explicit flip and asserts
// Defined$ FlippedHeads / FlippedTails name the flipper on the side the flip
// landed -- the memory RememberResult$ True records. The precondition (exactly
// one flip happened) is asserted so a vacuous empty-set pass cannot hide.
func TestFlipCoinFlippedHeadsTailsResolve(t *testing.T) {
	h, c := fixtureHost(t)
	Resolve(h, c, sa(t, "DB$ FlipCoin | RememberResult$ True | RememberNumber$ Wins"))
	if c.FlipMemory == nil || len(c.FlipMemory.Results) != 1 {
		t.Fatalf("precondition: want exactly 1 recorded flip, got %+v", c.FlipMemory)
	}
	heads := c.FlipMemory.Results[0].Heads

	gotHeads, ok := definedSpec(h, c, "FlippedHeads")
	if !ok {
		t.Fatal("FlippedHeads did not resolve")
	}
	gotTails, ok := definedSpec(h, c, "FlippedTails")
	if !ok {
		t.Fatal("FlippedTails did not resolve")
	}
	want := []state.Target{{Player: c.Controller, IsPlayer: true}}
	if heads {
		if !targetsEqual(gotHeads, want) {
			t.Fatalf("heads flip: FlippedHeads = %v, want %v", gotHeads, want)
		}
		if len(gotTails) != 0 {
			t.Fatalf("heads flip: FlippedTails = %v, want empty", gotTails)
		}
	} else {
		if !targetsEqual(gotTails, want) {
			t.Fatalf("tails flip: FlippedTails = %v, want %v", gotTails, want)
		}
		if len(gotHeads) != 0 {
			t.Fatalf("tails flip: FlippedHeads = %v, want empty", gotHeads)
		}
	}
}

// TestFlipCoinRunTimePublication pins the per-side publication the chained
// branches read: a winning flip publishes Wins 1 / Losses 0, a losing flip the
// reverse, and Count$RememberedNumber counts the RememberNumber$ side.
func TestFlipCoinRunTimePublication(t *testing.T) {
	h, c := fixtureHost(t)
	Resolve(h, c, sa(t, "DB$ FlipCoin | RememberResult$ True | RememberNumber$ Wins"))
	win := c.FlipMemory.Results[0].Heads
	wins, okW := runtimePublished(c, "Wins")
	losses, okL := runtimePublished(c, "Losses")
	if !okW || !okL {
		t.Fatalf("Wins/Losses not published: okW=%v okL=%v", okW, okL)
	}
	if win && (wins != 1 || losses != 0) {
		t.Fatalf("winning flip published Wins=%d Losses=%d, want 1/0", wins, losses)
	}
	if !win && (wins != 0 || losses != 1) {
		t.Fatalf("losing flip published Wins=%d Losses=%d, want 0/1", wins, losses)
	}
	n, ok := evalCountExprOK(h, c, "RememberedNumber", 0)
	if !ok {
		t.Fatal("Count$RememberedNumber did not resolve")
	}
	if win && n != 1 {
		t.Fatalf("winning flip: Count$RememberedNumber = %d, want 1", n)
	}
	if !win && n != 0 {
		t.Fatalf("losing flip: Count$RememberedNumber = %d, want 0", n)
	}
}

func targetsEqual(a, b []state.Target) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
