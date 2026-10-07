package sbtactical

// BP-13 (spec 2026-09-28-hosted-bot-packages §11): the sb-tactical adapter's
// own gate. The host-level rows for the entry live in host/hosted_policies_
// test.go (determinism, leak); this file covers the adapter's contract and
// the §5.1 INFERRED claim the brief charges BP-13 with measuring:
//
//   - the registry entry refuses a zero Deps and builds with one (factory);
//   - WantsEnv is a pure function of d and fires at priority only;
//   - DecideEnv installs the honest root as the planner and the explicit nil
//     on a refusal, and its answer is one the engine accepts;
//   - AnswerRefused is verbatim builtins.Seat.Refused (the refusal ladder's
//     rung 1 must be the wrapped seat's own answer);
//   - the planner is read at priority only, measured on a smoke game by
//     counting planner calls against the decision kind each call was made
//     under (TestSBTacticalPlannerIsReadAtPriorityOnly).

import (
	"context"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	gbench "github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// sampleRegistry builds a cards registry over testutil.SampleDecks' inline
// decks, so the whole package runs corpus-free and deterministically: the
// tactical lookup resolves the sample cards' printed facts from the same
// objects the game deals.
func sampleRegistry(t *testing.T) *cards.Registry {
	t.Helper()
	_, decks := testutil.SampleDecks(t, 4)
	reg := cards.NewRegistry()
	for _, cs := range decks {
		for _, c := range cs {
			reg.Add(c)
		}
	}
	return reg
}

// testOptions is a registered-entry bots.Options over the sample registry.
func testOptions(seed uint64, reg *cards.Registry) bots.Options {
	return bots.Options{Seed: seed, Deps: bots.Deps{Cards: reg}}
}

// mustNew builds one adapter, failing the test on the factory's error.
func mustNew(t *testing.T, o bots.Options, w builtins.TacticalWeights) *hostedSeat {
	t.Helper()
	s, err := New(o, w)
	if err != nil {
		t.Fatalf("New(seed %d): %v", o.Seed, err)
	}
	return s.(*hostedSeat)
}

// TestSBTacticalRegistryEntryIsRegisteredAndRefusesZeroDeps checks the
// registry wiring: the entry is linked, declares an Env entry that is not a
// search entry with the caretaker it names, its factory refuses a zero Deps
// with its own error, and it builds EnvSeat + RefusalAnswerer with one.
func TestSBTacticalRegistryEntryIsRegisteredAndRefusesZeroDeps(t *testing.T) {
	e, ok := bots.Lookup(Policy)
	if !ok {
		t.Fatalf("%s is not registered; bots/all links %q", Policy, Policy)
	}
	if !e.Env || e.Search || e.Caretaker != "bot" {
		t.Fatalf("%s Info = Env %v Search %v caretaker %q; want Env=true Search=false caretaker=bot", Policy, e.Env, e.Search, e.Caretaker)
	}
	if _, err := bots.New(Policy, bots.Options{Seed: 1}); err == nil {
		t.Fatal("bots.New built an sb-tactical seat with a zero Deps; the factory must refuse")
	}
	reg := sampleRegistry(t)
	s, err := bots.New(Policy, testOptions(7, reg))
	if err != nil {
		t.Fatalf("bots.New(%s) with cards: %v", Policy, err)
	}
	if _, ok := s.(bots.EnvSeat); !ok {
		t.Fatalf("bots.New(%s) built %T, which does not implement bots.EnvSeat", Policy, s)
	}
	if _, ok := s.(bots.RefusalAnswerer); !ok {
		t.Fatalf("bots.New(%s) built %T, which does not implement bots.RefusalAnswerer", Policy, s)
	}
	if _, ok := s.(seat.Seat); !ok {
		t.Fatalf("bots.New(%s) built %T, which does not implement seat.Seat", Policy, s)
	}
	if !s.(bots.EnvSeat).WantsEnv(&decision.Decision{Kind: decision.KPriority}) {
		t.Fatalf("%s does not want an Env at priority: the planner would never be installed", Policy)
	}
}

// TestSBTacticalWantsEnvIsPriorityOnly pins WantsEnv over the whole closed
// decision-kind set: exactly KPriority, and nothing else. A wider set would
// build honest roots (a redeal per decision) the seat never reads; a missing
// priority would leave the planner nil for the whole game.
func TestSBTacticalWantsEnvIsPriorityOnly(t *testing.T) {
	if !wantsEnvFor(&decision.Decision{Kind: decision.KPriority}) {
		t.Fatal("WantsEnv(priority) = false; want true")
	}
	for _, k := range decision.Kinds {
		if k == decision.KPriority {
			continue
		}
		if wantsEnvFor(&decision.Decision{Kind: k}) {
			t.Errorf("WantsEnv(%s) = true; the sb-tactical planner is read at priority only", k)
		}
	}
	// Pure function of d: the same decision answers the same way, and the
	// seq/player fields it carries (the host's contract) change nothing.
	d := &decision.Decision{Kind: decision.KPriority, Seq: 9, Player: 1}
	if !wantsEnvFor(d) || wantsEnvFor(&decision.Decision{Kind: decision.KMulligan, Seq: 9, Player: 1}) {
		t.Fatal("WantsEnv is not a pure function of d.Kind")
	}
}

// driveToPriority advances a fresh engine with a plain bot until its pending
// decision is a priority ask, answering every intermediate decision (the
// mulligans, the starting-player ask) with the plain bot's own answer. It
// returns the engine and that decision with the View a plain Seat gets.
func driveToPriority(t *testing.T, cfg rules.Config) (*rules.Engine, *decision.Decision, view.View) {
	t.Helper()
	e := rules.New(cfg)
	bot := seat.NewBot(1)
	ctx := context.Background()
	for step := 0; step < 2000; step++ {
		e.Advance()
		d := e.Pending()
		if d == nil {
			t.Fatal("the game ended before a priority decision was offered")
		}
		if d.Kind == decision.KPriority {
			return e, d, view.Project(e.G, e, d.Player, d)
		}
		in, err := bot.Decide(ctx, view.Project(e.G, e, d.Player, d), *d)
		if err != nil {
			t.Fatalf("bot could not answer %s: %v", d.Kind, err)
		}
		if err := e.Submit(in); err != nil {
			t.Fatalf("engine refused the bot's %s answer: %v", d.Kind, err)
		}
	}
	t.Fatal("2000 steps without a priority decision")
	return nil, nil, view.View{}
}

// smokeConfig builds a 2-seat rules.Config over the sample decks.
func smokeConfig(t *testing.T, seed uint64) rules.Config {
	t.Helper()
	_, decks := testutil.SampleDecks(t, 2)
	return rules.Config{Seed: seed, Names: []string{"p0", "p1"}, Decks: decks}
}

// TestSBTacticalDecideEnvInstallsTheRootAsPlanner is the §5.1 routing rule
// against a real engine at a real priority decision: with the root present
// the planner is that engine (pointer-identical) and the answer is one the
// engine accepts; with the root refused the planner is the explicit nil (the
// interface-vs-pointer-nil trap) and the seat still answers, from its View
// alone, an accepted intent.
func TestSBTacticalDecideEnvInstallsTheRootAsPlanner(t *testing.T) {
	reg := sampleRegistry(t)
	e, d, v := driveToPriority(t, smokeConfig(t, 20260928))

	// Precondition: the priority ask really offers plays — a decision with no
	// options would accept any answer and prove nothing about the planner.
	if len(d.Options) == 0 {
		t.Fatal("the priority decision offers no options; the routing assertion would be vacuous")
	}

	rooted := mustNew(t, testOptions(7, reg), Hosted())
	env := bots.Env{View: v, Search: searchseat.Env{Engine: e}}
	in, err := rooted.DecideEnv(context.Background(), env, *d)
	if err != nil {
		t.Fatalf("DecideEnv with a root: %v", err)
	}
	p := rooted.Seat().Planner()
	if p == nil {
		t.Fatal("after DecideEnv with a root the planner is nil; the seat would plan nothing")
	}
	pe, ok := p.(*rules.Engine)
	if !ok {
		t.Fatalf("the installed planner is %T, not the *rules.Engine the Env carried", p)
	}
	if pe != e {
		t.Fatal("the installed planner is not the Env's honest root (pointer mismatch)")
	}
	if err := e.Submit(in); err != nil {
		t.Fatalf("the rooted DecideEnv answer was refused by the engine: %v", err)
	}

	refusedSeat := mustNew(t, testOptions(7, reg), Hosted())
	refusedEnv := bots.Env{Search: searchseat.Env{Engine: nil}, RootRefused: "no live observation feed"}
	// The rooted answer moved the first engine past its decision, so the
	// refusal half drives its own identical game up to the same priority ask.
	e2, d2, v2 := driveToPriority(t, smokeConfig(t, 20260928))
	if len(d2.Options) == 0 {
		t.Fatal("the second priority decision offers no options; the planner-less assertion would be vacuous")
	}
	inRef, err := refusedSeat.DecideEnv(context.Background(), refusedEnvWithView(refusedEnv, v2), *d2)
	if err != nil {
		t.Fatalf("DecideEnv with a refused root: %v", err)
	}
	if got := refusedSeat.Seat().Planner(); got != nil {
		t.Fatalf("after a refused root the planner is %T; want the explicit nil", got)
	}
	if err := e2.Submit(inRef); err != nil {
		t.Fatalf("the planner-less answer was refused by the engine: %v", err)
	}
}

// refusedEnvWithView sets the Env's View on a refusal-shaped Env.
func refusedEnvWithView(env bots.Env, v view.View) bots.Env {
	env.View = v
	return env
}

// TestSBTacticalRefusalDelegatesToBuiltinsRefused pins the delegation: on
// two same-seed adapters (one call each, so RNG state cannot differ), the
// adapter's AnswerRefused equals the wrapped seat's Refused verbatim — the
// host's refusal ladder rung 1 must be the wrapped seat's own answer.
func TestSBTacticalRefusalDelegatesToBuiltinsRefused(t *testing.T) {
	reg := sampleRegistry(t)
	e, d, v := driveToPriority(t, smokeConfig(t, 20260928))
	if len(d.Options) == 0 {
		t.Fatal("the priority decision offers no options; the refusal comparison would be vacuous")
	}
	refused := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{len(d.Options) + 7}}
	a := mustNew(t, testOptions(7, reg), Hosted())
	b := mustNew(t, testOptions(7, reg), Hosted())
	gotAdapter := a.AnswerRefused(v, *d, refused)
	gotInner := b.Seat().Refused(v, *d, refused)
	if gotAdapter.Seq != gotInner.Seq || gotAdapter.Player != gotInner.Player ||
		len(gotAdapter.Choices) != len(gotInner.Choices) {
		t.Fatalf("AnswerRefused = %+v, builtins.Refused = %+v: the delegation is not verbatim", gotAdapter, gotInner)
	}
	for i := range gotAdapter.Choices {
		if gotAdapter.Choices[i] != gotInner.Choices[i] {
			t.Fatalf("AnswerRefused chose %v, builtins.Refused chose %v at %d", gotAdapter.Choices, gotInner.Choices, i)
		}
	}
	if gotAdapter.Seq != d.Seq || gotAdapter.Player != d.Player {
		t.Fatalf("AnswerRefused answered seq %d player %d; want the refused decision's %d/%d",
			gotAdapter.Seq, gotAdapter.Player, d.Seq, d.Player)
	}
	_ = e
}

// countPlanner counts every planner read under the decision the engine is
// currently holding, so a seat's reads can be attributed to decision kinds
// without touching the wrapped seat. It implements both Planner and
// ScriptPlanner (the exact fallback builtins reads by interface assertion),
// delegates every read to the engine it wraps, and records the kind at the
// moment of the read.
type countPlanner struct {
	e     *rules.Engine
	calls map[decision.Kind]int
}

func (c *countPlanner) record() {
	if c.calls == nil {
		c.calls = map[decision.Kind]int{}
	}
	if d := c.e.Pending(); d != nil {
		c.calls[d.Kind]++
	}
}

func (c *countPlanner) PotentialPaymentPlans(p state.PlayerID) []rules.PotentialPlan {
	c.record()
	return c.e.PotentialPaymentPlans(p)
}

func (c *countPlanner) PotentialPlayScript(p state.PlayerID, a decision.PotentialAction, budget int) ([]rules.ScriptStep, string) {
	c.record()
	// No script offered: the seat falls back exactly as it does without the
	// exact planner, so the counts measure reads, not extra search.
	return nil, "countPlanner offers no script"
}

// TestSBTacticalPlannerIsReadAtPriorityOnly measures the §5.1 INFERRED claim
// on a smoke game: both seats' planners count every read against the
// decision kind the engine held when the read happened. The claim holds iff
// every read lands on KPriority — and the run is only evidence when the
// game really played priority decisions and the planner was really read at
// least once at one (a zero-everywhere run would prove nothing).
//
// The game is a repo-deck mirror (dimir-tempo vs itself, the corpus
// registry's own cards), not the inline sample decks: measured on the
// sample decks the tactical seat never consulted the planner at all in a
// whole 800-intent game — the engine offers everything payable as options
// or PaymentActions there, and the planner is read only for plays beyond
// the offer (the potential over-bound walk). A five-deck probe
// (equipment, goblins, tramplesaurus, dimir-tempo, avengers) read the
// planner 5-59 times per game, every read under a priority decision; the
// committed deck is the one with the densest reads on both seats. The repo
// decks are what the SB §3.4 numbers themselves were taken with.
// CorpusRegistry skips when no corpus is present, so a corpus-less gate
// reports the skip, not a green.
func TestSBTacticalPlannerIsReadAtPriorityOnly(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	deck := testutil.RepoDeck(t, reg, "dimir-tempo")
	cfg := rules.Config{Seed: 20260928, Names: []string{"p0", "p1"},
		Decks: [][]*cards.Card{deck, deck}, Tokens: reg.Tokens, NameUniverse: reg.AllCards()}
	seats := []seat.Seat{
		mustNew(t, bots.Options{Seed: 1, Deps: bots.Deps{Cards: reg}}, Hosted()),
		mustNew(t, bots.Options{Seed: 2, Deps: bots.Deps{Cards: reg}}, Hosted()),
	}
	counters := [2]*countPlanner{{}, {}}
	kindAsks := [2]map[decision.Kind]int{{}, {}}
	hooks := gbench.Hooks{
		Setup: func(e *rules.Engine) {
			for i, s := range seats {
				counters[i].e = e
				s.(*hostedSeat).Seat().SetPlanner(counters[i])
			}
		},
		Decision: func(seatIdx int, d *decision.Decision, _ decision.Intent, _ *botpolicy.Board) error {
			if seatIdx < len(kindAsks) {
				kindAsks[seatIdx][d.Kind]++
			}
			return nil
		},
	}
	o, _, err := gbench.PlayGame(cfg, seats, 0, 800, hooks)
	if err != nil {
		t.Fatalf("smoke game: %v", err)
	}
	t.Logf("outcome: %+v", o)
	for i := range counters {
		if kindAsks[i][decision.KPriority] == 0 {
			t.Fatalf("seat %d was never asked a priority decision: the run proves nothing about the planner's read kinds", i)
		}
		if n := len(kindAsks[i]); n < 3 {
			t.Fatalf("seat %d was asked only %d distinct kinds; the smoke game is too shallow to measure the claim", i, n)
		}
		if counters[i].calls[decision.KPriority] == 0 {
			t.Fatalf("seat %d's planner was never read at priority: %v — the priority-only claim would be vacuously true", i, counters[i].calls)
		}
		for k, n := range counters[i].calls {
			if k != decision.KPriority {
				t.Errorf("seat %d's planner was read %d time(s) under a %s decision: the §5.1 priority-only claim is FALSE", i, n, k)
			}
		}
		t.Logf("seat %d planner reads by kind: %v (asks by kind: %v)", i, counters[i].calls, kindAsks[i])
	}
}
