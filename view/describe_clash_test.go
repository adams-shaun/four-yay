package view

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
)

// TestDescribeClashNamesSeatAndOutcome pins the transcript line for the
// events.Clash marker (CR 701.31, task clash1): one record per clashing
// player, Player the clashing seat, Amount 1 when that player won and 0 when
// they lost or tied. Without the describe case the Kind renders "unknown
// event" and TestDescribeCoversEveryKind (describe_test.go) fails the module
// gate -- the exact failure this fix round answers -- so asserting the
// non-empty line here proves the case is wired, and asserting the two
// orientations proves it reads the event's Amount rather than emitting one
// generic phrase.
func TestDescribeClashNamesSeatAndOutcome(t *testing.T) {
	g, _, _ := describeFixture(t)
	// Precondition: the fixture resolves seat names, so the line can carry
	// one; a nil or unnamed game would make the assertion vacuous.
	if got := Describe(g, events.Event{Kind: events.GameStart, Amount: 2}); got == "" {
		t.Fatalf("describe fixture does not resolve a game: %q", got)
	}

	won := Describe(g, events.Event{Kind: events.Clash, Player: 0, Amount: 1})
	if won != "Ann wins the clash" {
		t.Errorf("won clash = %q, want %q", won, "Ann wins the clash")
	}
	lost := Describe(g, events.Event{Kind: events.Clash, Player: 1, Amount: 0})
	if lost != "Bob loses the clash" {
		t.Errorf("lost clash = %q, want %q", lost, "Bob loses the clash")
	}
	if won == lost {
		t.Fatalf("win and lose orientations render identically: %q", won)
	}
}
