package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestHandleRoutesChooseToTheAsker(t *testing.T) {
	t.Parallel()
	names, decks := testutil.SampleDecks(t, 2)
	e := New(Config{Seed: 1, Names: names, Decks: decks})
	e.Advance()
	// Ask a bare choose with no asker registered: the engine must not panic
	// and must not strand the match — it records a Note and re-grants priority.
	d := &decision.Decision{Player: 0, Kind: decision.KChoose, Min: 1, Max: 1, Options: []decision.Option{{Index: 0, Kind: "number", Label: "0"}}}
	e.pending = nil
	e.ask(d)
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	if e.Pending() == nil || e.Pending().Kind != decision.KPriority {
		t.Fatalf("after a stray choose the engine should be back at priority, got %+v", e.Pending())
	}
}
