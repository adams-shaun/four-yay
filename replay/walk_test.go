package replay

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules"
)

func TestWalkVisitsEveryBoundaryAndMatchesReplayTo(t *testing.T) {
	cfg, original := playGame(t, 43)
	n := len(original.L.Intents) / 2
	if n < 1 {
		t.Fatalf("test game has only %d intents; need a non-empty prefix", len(original.L.Intents))
	}
	var boundaries []int
	walked, err := Walk(original.L, cfg, n, func(e *rules.Engine, i int) error {
		boundaries = append(boundaries, i)
		if got := len(e.L.Intents); got != i {
			t.Errorf("boundary %d: engine has %d submitted intents", i, got)
		}
		if i < n {
			if d := e.Pending(); d == nil {
				t.Errorf("boundary %d: no pending decision", i)
			} else if want := original.L.Intents[i]; d.Seq != want.Seq || d.Player != want.Player {
				t.Errorf("boundary %d pending decision = seq %d/player %d, want seq %d/player %d", i, d.Seq, d.Player, want.Seq, want.Player)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(boundaries) != n+1 {
		t.Fatalf("visited %d boundaries, want %d", len(boundaries), n+1)
	}
	for i, got := range boundaries {
		if got != i {
			t.Fatalf("boundary visit %d has index %d", i, got)
		}
	}
	want, err := ReplayTo(original.L, cfg, n)
	if err != nil {
		t.Fatalf("ReplayTo: %v", err)
	}
	if len(walked.L.Intents) != n || len(walked.L.Events) != len(want.L.Events) {
		t.Fatalf("Walk result has %d intents/%d events, ReplayTo has %d/%d", len(walked.L.Intents), len(walked.L.Events), len(want.L.Intents), len(want.L.Events))
	}
	for i := range want.L.Events {
		if string(walked.L.Events[i].Append(nil)) != string(want.L.Events[i].Append(nil)) {
			t.Fatalf("event %d differs from ReplayTo", i)
		}
	}
	// The visitor must not change replay's comparison behavior.
	if _, err := Walk(&events.Log{}, cfg, 0, nil); err == nil {
		t.Fatal("Walk accepted an empty log that cannot match a real genesis")
	}
}
