package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// seatNamesGame builds a two-seat engine whose deck-identity Names are the
// hyphenated repo-deck slugs a table serves and whose table display names
// are the vs-bot pair, then drives past the London mulligan round so a
// priority decision is pending. Used to pin that the client-facing prompt
// and option-label text (seatFacingName) reads the display name, never the
// deck slug, while event text keeps the deck identity (F3,
// TestPlayerNamesDoNotReachTheChain owns the chain-side invariant).
func seatNamesGame(t *testing.T, withPlayerNames bool) *Engine {
	t.Helper()
	cfg := Config{
		Seed:  7,
		Names: []string{"foundations-keen-engineering", "foundations-wretched-ranks"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)},
	}
	if withPlayerNames {
		cfg.PlayerNames = []string{"You", "Bot"}
	}
	e := New(seatZeroStart(cfg))
	e.Advance()
	for e.Pending() != nil && e.Pending().Kind == decision.KMulligan {
		submitChoices(t, e, 0) // "keep", the first offered option
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected a priority decision pending, got %+v", e.Pending())
	}
	return e
}

// TestPriorityPromptUsesSeatFacingName pins the fb-20260917T004304Z report:
// the seat panel's text box read "turn 2, main1 — foundations-keen-engineering
// has priority" to a player whose display name is "You". The prompt is
// decision content, not chain content, so it must compose from the table's
// PlayerNames; the transcript convention is "You has priority"
// (view/describe.go), and the box matching the transcript is the point.
func TestPriorityPromptUsesSeatFacingName(t *testing.T) {
	t.Parallel()
	e := seatNamesGame(t, true)
	d := e.Pending()
	want := "turn 1, " + e.G.Step.String() + " — You has priority"
	if d.Prompt != want {
		t.Fatalf("priority prompt = %q, want %q", d.Prompt, want)
	}
	if strings.Contains(d.Prompt, "foundations") {
		t.Fatalf("priority prompt leaks the deck slug: %q", d.Prompt)
	}
}

// TestPriorityPromptFallsBackToDeckName pins the no-display-name fallback:
// without PlayerNames (mtgsim, chain-head goldens, engine tests) the prompt
// is byte-identical to the pre-fix composition from the deck-identity Name.
func TestPriorityPromptFallsBackToDeckName(t *testing.T) {
	t.Parallel()
	e := seatNamesGame(t, false)
	d := e.Pending()
	want := "turn 1, " + e.G.Step.String() + " — foundations-keen-engineering has priority"
	if d.Prompt != want {
		t.Fatalf("priority prompt = %q, want %q", d.Prompt, want)
	}
}

// TestTargetOptionLabelUsesSeatFacingName pins the sibling leak: a target
// option's "(controller)" suffix read the controller's deck slug in a
// vs-bot game.
func TestTargetOptionLabelUsesSeatFacingName(t *testing.T) {
	t.Parallel()
	e := seatNamesGame(t, true)
	hand := e.G.Zone(state.ZHand, 0)
	if len(hand) == 0 {
		t.Fatal("no hand object to label")
	}
	label := e.targetOptionLabel(targetCandidate{obj: hand[0], player: 0})
	if !strings.HasSuffix(label, "(You)") {
		t.Fatalf("target option label = %q, want it to end in \"(You)\"", label)
	}
	if strings.Contains(label, "foundations") {
		t.Fatalf("target option label leaks the deck slug: %q", label)
	}
}
