package azmcts

import (
	"context"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
)

// TestSearchArmedDeadlinePlaysTheBot pins the wall-clock bail-out (Search's
// ctx, the hosted seats' escape hatch against search pile-ups): a context
// that is already done at the call stops the tree before its first
// simulation, Stats.DeadlineHits counts it, and the bot's own answer is
// played -- never the partial tree's choice.
//
// The cheapest deterministic way to test a bail-out: pass an
// already-cancelled context and assert the search ran ZERO simulations,
// rather than racing a real wall clock. The precondition is asserted first:
// the same search with a live context runs all of its simulations, so the
// zero below is the bail-out and not an unsearchable position.
func TestSearchArmedDeadlinePlaysTheBot(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KPriority, 0, 2000)
	opts := DefaultOptions()
	opts.Sims, opts.Seed = 12, 7

	live := searchAt(t, e, d, bot, nil, opts)
	if live.Stats.Simulations != 12 || live.Stats.Completed != 12 || live.Stats.Searched != 1 {
		t.Fatalf("the live search did not run its 12 simulations: %+v", live.Stats)
	}

	obs := searchprobe.NewCollector(d.Player)
	src, err := newTestClairvoyant(e, obs)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	armed, err := Search(ctx, Root{Engine: e, Decision: d, Bot: bot, Observer: obs}, src, nil, opts)
	if err != nil {
		t.Fatalf("Search with a done context: %v", err)
	}
	if armed.Stats.DeadlineHits != 1 {
		t.Fatalf("DeadlineHits = %d, want 1 (the bail-out never fired): %+v", armed.Stats.DeadlineHits, armed.Stats)
	}
	if armed.Stats.Simulations != 0 || armed.Stats.Completed != 0 || armed.Stats.Expanded != 0 {
		t.Fatalf("the armed search still ran the tree: %+v", armed.Stats)
	}
	if armed.Choice != 0 {
		t.Fatalf("the armed search chose candidate %d; a bailed-out search never takes the partial tree's choice", armed.Choice)
	}
	if !reflect.DeepEqual(armed.Intent, bot) {
		t.Fatalf("the armed search played %+v, want the bot's own answer %+v", armed.Intent, bot)
	}
	if len(armed.Visits) != 0 || len(armed.Q) != 0 || armed.RootValue != 0 {
		t.Fatalf("the armed search reported tree results: visits %v q %v root %v", armed.Visits, armed.Q, armed.RootValue)
	}
}
