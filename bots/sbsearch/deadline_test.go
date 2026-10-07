package sbsearch

import (
	"context"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	gsearch "github.com/adams-shaun/gorge/internal/spellbench/sbsearch"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/view"
)

// TestSBSearchDecisionDeadlineBailsToTactical is the hosted bail-out's
// end-to-end gate: the adapter's DecideEnv is the only hosted path that
// reaches the search, so a context the host armed (bots.Options.
// DecisionDeadlineMS, armed by host.decisionCtx) that is already done stops
// the search between worlds and sb-tactical's pick is played -- the same
// fallback as a search with no valid world -- while the same call with a
// live context runs the search and answers from its tree.
//
// The bail is tested with an already-cancelled context (deterministic; the
// sbsearch-level test pins the between-worlds stop directly). CorpusRegistry
// skips when no corpus is present, so a corpus-less run reports the skip,
// not a green.
func TestSBSearchDecisionDeadlineBailsToTactical(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	deck := testutil.RepoDeck(t, reg, "dimir-tempo")
	cfg := rules.Config{Seed: 20260928, Names: []string{"p0", "p1"},
		Decks: [][]*cards.Card{deck, deck}, Tokens: reg.Tokens, NameUniverse: reg.AllCards()}

	var diags []gsearch.Diag
	prev := gsearch.Watch
	gsearch.Watch = func(dg gsearch.Diag) { diags = append(diags, dg) }
	defer func() { gsearch.Watch = prev }()

	e := rules.New(cfg)
	e.Advance()
	lookup := builtins.NewRegistryLookup(reg)
	scout := builtins.NewTactical(builtins.AutoPay, 7, lookup, builtins.DefaultTacticalWeights())
	// The adapter under test is built like the hosted seat, with the host's
	// per-decision budget declared (bots.Options.DecisionDeadlineMS).
	probe := mustNew(t, bots.Options{Seed: 7, Deps: bots.Deps{Cards: reg}, DecisionDeadlineMS: 2000}, Hosted())
	if got := probe.DecisionBudgetMS(); got != 2000 {
		t.Fatalf("DecisionBudgetMS = %d, want the declared 2000", got)
	}
	var root *rules.Engine
	var d *decision.Decision
	var feed *searchseat.Feed
	var v view.View
	for step := 0; step < 4000; step++ {
		if e.Pending() == nil {
			t.Fatal("the game ended before a searchable priority decision was found")
		}
		pd := e.Pending()
		v = view.Project(e.G, e, pd.Player, pd)
		if pd.Kind == decision.KPriority {
			if cands, best, ok := scout.TacticalPriority(v, *pd); ok && !cands[best].Key.IsLand() {
				fd := searchseat.NewFeed(pd.Player)
				if _, ok := fd.Observe(e); !ok {
					t.Fatal("the feed failed its boundary capture")
				}
				setup := searchprobe.PublicGame{Names: cfg.Names, Decks: cfg.Decks, Tokens: cfg.Tokens, StartingLife: cfg.StartingLife}
				r, reason := searchseat.HonestRoot(setup, fd, e, bots.RootSeed(7, pd.Seq))
				if r == nil {
					t.Fatalf("HonestRoot refused at the candidate decision: %s", reason)
				}
				root, d, feed = r, pd, fd
				break
			}
		}
		in, err := scout.Decide(context.Background(), v, *pd)
		if err != nil {
			t.Fatalf("scout could not answer %s: %v", pd.Kind, err)
		}
		if err := e.Submit(in); err != nil {
			t.Fatalf("scout submit: %v", err)
		}
		e.Advance()
	}
	if root == nil || d == nil || feed == nil {
		t.Fatal("no searchable priority decision found; the bail-out assertion would be vacuous")
	}

	setup := searchprobe.PublicGame{Names: cfg.Names, Decks: cfg.Decks, Tokens: cfg.Tokens, StartingLife: cfg.StartingLife}
	env := bots.Env{
		View:   view.Project(root.G, root, d.Player, d),
		Search: searchseat.Env{Setup: setup, Engine: root, Feed: feed},
	}

	// Precondition: with a live context the same call runs the search.
	diags = nil
	in, err := probe.DecideEnv(context.Background(), env, *d)
	if err != nil {
		t.Fatalf("DecideEnv with a live context: %v", err)
	}
	if len(diags) != 1 || diags[0].Worlds == 0 {
		t.Fatalf("the live DecideEnv did not run the search: %d diags, %+v", len(diags), diags)
	}

	// The bail: the same call on a context that is already done stops the
	// search between worlds and plays sb-tactical's pick.
	diags = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	bailed, err := probe.DecideEnv(ctx, env, *d)
	if err != nil {
		t.Fatalf("DecideEnv with a done context: %v", err)
	}
	if len(diags) != 1 {
		t.Fatalf("the bailed call emitted %d diags, want the one refused search", len(diags))
	}
	if dg := diags[0]; dg.Worlds != 0 || !strings.HasPrefix(dg.Refused, "context: ") {
		t.Fatalf("the bailed search diag %+v, want zero worlds with a context refusal", dg)
	}
	if err := d.Validate(bailed); err != nil {
		t.Fatalf("the bailed answer %+v does not validate: %v", bailed, err)
	}
	_ = in
}
