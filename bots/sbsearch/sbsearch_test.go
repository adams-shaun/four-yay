package sbsearch

// BP-14 (spec 2026-09-28-hosted-bot-packages §11): the sb-search-lite-atk
// adapter's own gate. The host-level rows for the entry live in
// host/hosted_policies_test.go (determinism, leak); this file covers the
// adapter's contract:
//
//   - the registry entry is linked, declares Env+Search with the bot
//     caretaker, its factory refuses a zero Deps and builds the interfaces
//     the host and the refusal ladder detect;
//   - LiteAtk()/Hosted() are the registered lite-atk budget (W4, H2, Attack)
//     with this entry's Name;
//   - WantsEnv is a pure function of d and fires at priority and attackers
//     only;
//   - AnswerRefused is verbatim builtins.Seat.Refused, reached through
//     UnwrapSeat (the §11 BP-14 requirement);
//   - with the honest root refused, DecideEnv plays the INNER sb-tactical on
//     env.View -- the answer equals the wrapped tactical's own Decide and
//     differs from the forbidden sbsearch.DecideBoard default-bot path --
//     and runs no search at all;
//   - with a live root and feed, DecideEnv runs the search: a Diag fires
//     (TestSBSearchDecideEnvForwardsToTheSearch). That last row is what
//     keeps the host leak row from passing with the forwarding removed.

import (
	"context"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	gsearch "github.com/adams-shaun/gorge/internal/spellbench/sbsearch"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// sampleRegistry builds a cards registry over testutil.SampleDecks' inline
// decks, so the registry/wiring tests run corpus-free and deterministically.
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
func mustNew(t *testing.T, o bots.Options, cfg gsearch.Config) *hostedSeat {
	t.Helper()
	s, err := New(o, cfg)
	if err != nil {
		t.Fatalf("New(seed %d): %v", o.Seed, err)
	}
	return s.(*hostedSeat)
}

// TestSBSearchRegistryEntryIsRegisteredAndRefusesZeroDeps checks the
// registry wiring: the entry is linked, declares Env+Search with the bot
// caretaker, its factory refuses a zero Deps with its own error, and it
// builds EnvSeat + RefusalAnswerer + the AutoPay payment-plan opt-in with a
// registry.
func TestSBSearchRegistryEntryIsRegisteredAndRefusesZeroDeps(t *testing.T) {
	e, ok := bots.Lookup(Policy)
	if !ok {
		t.Fatalf("%s is not registered; bots/all links %q", Policy, Policy)
	}
	if !e.Env || !e.Search || e.Caretaker != "bot" {
		t.Fatalf("%s Info = Env %v Search %v caretaker %q; want Env=true Search=true caretaker=bot", Policy, e.Env, e.Search, e.Caretaker)
	}
	if _, err := bots.New(Policy, bots.Options{Seed: 1}); err == nil {
		t.Fatal("bots.New built an sb-search seat with a zero Deps; the factory must refuse")
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
	pc, ok := s.(seat.PaymentPlanConsumer)
	if !ok {
		t.Fatalf("bots.New(%s) built %T, which does not opt into payment plans; the AutoPay inner seat needs them", Policy, s)
	}
	if !pc.WantsPaymentActions() {
		t.Fatalf("%s WantsPaymentActions() = false; the inner AutoPay sb-tactical reads the host's plans", Policy)
	}
	if _, ok := s.(seat.BoardSeat); ok {
		t.Fatalf("%s implements seat.BoardSeat; §5.1 keeps this adapter on the plain View path (the fallback is the inner tactical on env.View)", Policy)
	}
}

// TestSBSearchWantsEnvIsPriorityAndAttackersOnly pins WantsEnv over the
// whole closed decision-kind set: exactly KPriority and KAttackers. A wider
// set would build honest roots (a redeal per decision) the search never
// reads; a missing one would leave the searched decision on the plain View.
func TestSBSearchWantsEnvIsPriorityAndAttackersOnly(t *testing.T) {
	if !wantsEnvFor(&decision.Decision{Kind: decision.KPriority}) {
		t.Fatal("WantsEnv(priority) = false; want true")
	}
	if !wantsEnvFor(&decision.Decision{Kind: decision.KAttackers}) {
		t.Fatal("WantsEnv(attackers) = false; want true (LiteAtk searches attacks)")
	}
	for _, k := range decision.Kinds {
		if k == decision.KPriority || k == decision.KAttackers {
			continue
		}
		if wantsEnvFor(&decision.Decision{Kind: k}) {
			t.Errorf("WantsEnv(%s) = true; sb-search searches priority and attackers only", k)
		}
	}
	// Pure function of d: seq/player change nothing.
	d := &decision.Decision{Kind: decision.KPriority, Seq: 9, Player: 1}
	if !wantsEnvFor(d) || wantsEnvFor(&decision.Decision{Kind: decision.KBlockers, Seq: 9, Player: 1}) {
		t.Fatal("WantsEnv is not a pure function of d.Kind")
	}
}

// TestSBSearchHostedIsTheLiteAtkRow pins the registered budget against the
// botbench lite-atk row: W4, a 2-turn horizon, attacks searched, and this
// entry's Name so the search's Diags label the hosted policy.
func TestSBSearchHostedIsTheLiteAtkRow(t *testing.T) {
	h := Hosted()
	if h != LiteAtk() {
		t.Fatalf("Hosted() = %+v, LiteAtk() = %+v; one function must serve both", h, LiteAtk())
	}
	if h.Worlds != 4 || h.Horizon != 2 || !h.Attack {
		t.Fatalf("LiteAtk() = W%d H%d Attack %v; want the sb-search-lite-atk row W4 H2 Attack true", h.Worlds, h.Horizon, h.Attack)
	}
	if h.Name != Policy {
		t.Fatalf("LiteAtk().Name = %q; want %q", h.Name, Policy)
	}
	want := gsearch.DefaultConfig()
	want.Worlds, want.Horizon, want.Attack = 4, 2, true
	want.Name = Policy
	if h != want {
		t.Fatalf("LiteAtk() = %+v; want DefaultConfig() with Worlds=4 Horizon=2 Attack=true Name=%q: %+v", h, Policy, want)
	}
}

// driveToPriority advances a fresh engine with a plain bot until its pending
// decision is a priority ask, answering every intermediate decision with the
// plain bot's own answer. It returns the engine, that decision and the View
// a plain Seat gets.
func driveToPriority(t *testing.T, cfg rules.Config) (*rules.Engine, *decision.Decision, view.View) {
	t.Helper()
	e := rules.New(cfg)
	bot := seat.NewBot(1)
	ctx := context.Background()
	for step := 0; step < 4000; step++ {
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
	t.Fatal("4000 steps without a priority decision")
	return nil, nil, view.View{}
}

// smokeConfig builds a 2-seat rules.Config over the sample decks.
func smokeConfig(t *testing.T, seed uint64) rules.Config {
	t.Helper()
	_, decks := testutil.SampleDecks(t, 2)
	return rules.Config{Seed: seed, Names: []string{"p0", "p1"}, Decks: decks}
}

// TestSBSearchFallbackIsTheInnerTacticalOnTheView proves §5.1's fallback
// rule: with the honest root refused, DecideEnv's answer equals the inner
// sb-tactical's own Decide on env.View (a second same-seed adapter answers
// the identical decision) and differs from the forbidden
// sbsearch.Seat.DecideBoard default-bot path, and no search runs at all.
//
// The precondition is asserted twice: the decision must offer options (or
// any answer proves nothing), and the inner tactical's answer must differ
// from DecideBoard's -- otherwise the equality below could hold with the
// adapter calling the forbidden path.
func TestSBSearchFallbackIsTheInnerTacticalOnTheView(t *testing.T) {
	reg := sampleRegistry(t)
	e, d, v := driveToPriority(t, smokeConfig(t, 20260928))
	if len(d.Options) == 0 {
		t.Fatal("the priority decision offers no options; the fallback assertion would be vacuous")
	}
	// Find a priority decision where the two candidate paths genuinely
	// differ, so "equals inner tactical" excludes "equals DecideBoard".
	var (
		found  bool
		ddot   *decision.Decision
		vdot   view.View
		brdA   botpolicy.Board
		innerA decision.Intent
		boardA decision.Intent
	)
	refInner := mustNew(t, testOptions(7, reg), Hosted())
	refBoard := mustNew(t, testOptions(7, reg), Hosted())
	brd := botpolicy.NewBoard(2)
	for step := 0; step < 200 && !found; step++ {
		if d.Kind == decision.KPriority && len(d.Options) > 0 {
			b := botpolicy.BoardFromGameInto(e.G, e, d.Player, &brd)
			// Two fresh same-seed seats, one call each: the inner tactical's
			// own answer and the forbidden DecideBoard's, on the same engine.
			ia, _ := refInner.inner.Decide(context.Background(), v, *d)
			ba, _ := refBoard.bot.DecideBoard(context.Background(), b, *d)
			if !sameIntent(ia, ba) {
				found, ddot, vdot, brdA, innerA, boardA = true, d, v, b, ia, ba
			}
		}
		if found {
			break
		}
		in, err := seat.NewBot(1).Decide(context.Background(), view.Project(e.G, e, d.Player, d), *d)
		if err != nil {
			t.Fatalf("advance: %v", err)
		}
		if err := e.Submit(in); err != nil {
			t.Fatalf("advance submit: %v", err)
		}
		e.Advance()
		nd := e.Pending()
		if nd == nil {
			break
		}
		d = nd
		v = view.Project(e.G, e, d.Player, d)
	}
	if !found {
		t.Fatal("no priority decision in the smoke game separates sb-tactical from the default bot; the fallback assertion would be vacuous")
	}

	// No search may run on the fallback path.
	var searched int
	prev := gsearch.Watch
	gsearch.Watch = func(gsearch.Diag) { searched++ }
	defer func() { gsearch.Watch = prev }()

	a := mustNew(t, testOptions(7, reg), Hosted())
	got, err := a.DecideEnv(context.Background(), bots.Env{
		View:        vdot,
		Board:       brdA,
		Search:      searchseat.Env{Engine: nil},
		RootRefused: "no live observation feed",
	}, *ddot)
	if err != nil {
		t.Fatalf("DecideEnv with a refused root: %v", err)
	}
	if !sameIntent(got, innerA) {
		t.Fatalf("fallback answer %+v != inner sb-tactical's own %+v on the same View", got, innerA)
	}
	if sameIntent(got, boardA) {
		t.Fatalf("fallback answer %+v == DecideBoard's default-bot path %+v; the adapter must play the inner sb-tactical, not sbsearch.DecideBoard", got, boardA)
	}
	if searched != 0 {
		t.Fatalf("the fallback ran %d search(es); a refused root must play the inner tactical with no redeal", searched)
	}
}

// sameIntent compares the answer fields that reach the engine (Seq and
// Player are repaired by the seats to the decision's, so they always agree).
func sameIntent(a, b decision.Intent) bool {
	if a.Seq != b.Seq || a.Player != b.Player || len(a.Choices) != len(b.Choices) {
		return false
	}
	for i := range a.Choices {
		if a.Choices[i] != b.Choices[i] {
			return false
		}
	}
	return true
}

// TestSBSearchRefusalGoesThroughUnwrapSeat pins the delegation: on two
// same-seed adapters (one call each, so RNG state cannot differ), the
// adapter's AnswerRefused equals UnwrapSeat's builtins seat's Refused
// verbatim — the host's refusal ladder rung 1 must be the wrapped seat's own
// answer, reached through the unwrap the §11 ticket names.
func TestSBSearchRefusalGoesThroughUnwrapSeat(t *testing.T) {
	reg := sampleRegistry(t)
	e, d, v := driveToPriority(t, smokeConfig(t, 20260928))
	if len(d.Options) == 0 {
		t.Fatal("the priority decision offers no options; the refusal comparison would be vacuous")
	}
	refused := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{len(d.Options) + 7}}
	a := mustNew(t, testOptions(7, reg), Hosted())
	b := mustNew(t, testOptions(7, reg), Hosted())
	gotAdapter := a.AnswerRefused(v, *d, refused)
	gotInner := b.bot.UnwrapSeat().(*builtins.Seat).Refused(v, *d, refused)
	if !sameIntent(gotAdapter, gotInner) {
		t.Fatalf("AnswerRefused = %+v, UnwrapSeat().Refused = %+v: the delegation is not verbatim", gotAdapter, gotInner)
	}
	if gotAdapter.Seq != d.Seq || gotAdapter.Player != d.Player {
		t.Fatalf("AnswerRefused answered seq %d player %d; want the refused decision's %d/%d",
			gotAdapter.Seq, gotAdapter.Player, d.Seq, d.Player)
	}
	_ = e
}

// TestSBSearchDecideEnvForwardsToTheSearch is the forwarding gate that keeps
// the host leak row honest: with a LIVE honest root and a live observation
// feed, DecideEnv must run the search, and a search is observable as one
// sbsearch.Diag (Watch is the package's own hook). A stub that always played
// the fallback would pass the host leak row (the fallback answers from the
// same roots and Views) but emits no Diag, and this test fails.
//
// The root is searchseat.HonestRoot over a real corpus game at a priority
// decision sb-tactical has a real choice at, exactly the host's construction.
// CorpusRegistry skips when no corpus is present, so a corpus-less run
// reports the skip, not a green.
func TestSBSearchDecideEnvForwardsToTheSearch(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	deck := testutil.RepoDeck(t, reg, "dimir-tempo")
	cfg := rules.Config{Seed: 20260928, Names: []string{"p0", "p1"},
		Decks: [][]*cards.Card{deck, deck}, Tokens: reg.Tokens, NameUniverse: reg.AllCards()}

	var searched []gsearch.Diag
	prev := gsearch.Watch
	gsearch.Watch = func(dg gsearch.Diag) { searched = append(searched, dg) }
	defer func() { gsearch.Watch = prev }()

	// Drive to a priority decision with sb-tactical as the plain seat, then
	// hunt for one where TacticalPriority offers a real (non-land) choice --
	// the only shape the search actually runs on.
	e := rules.New(cfg)
	e.Advance()
	lookup := builtins.NewRegistryLookup(reg)
	scout := builtins.NewTactical(builtins.AutoPay, 7, lookup, builtins.DefaultTacticalWeights())
	probe := mustNew(t, testOptions(7, reg), Hosted())
	ctx := context.Background()
	var root *rules.Engine
	var d *decision.Decision
	var v view.View
	var feed *searchseat.Feed
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
		in, err := scout.Decide(ctx, v, *pd)
		if err != nil {
			t.Fatalf("scout could not answer %s: %v", pd.Kind, err)
		}
		if err := e.Submit(in); err != nil {
			t.Fatalf("scout submit: %v", err)
		}
		e.Advance()
	}
	if root == nil || d == nil || feed == nil {
		t.Fatal("no searchable priority decision found in the scout game; the forwarding assertion would be vacuous")
	}

	setup := searchprobe.PublicGame{Names: cfg.Names, Decks: cfg.Decks, Tokens: cfg.Tokens, StartingLife: cfg.StartingLife}
	env := bots.Env{
		View:   view.Project(root.G, root, d.Player, d),
		Search: searchseat.Env{Setup: setup, Engine: root, Feed: feed},
	}
	if _, err := probe.DecideEnv(ctx, env, *d); err != nil {
		t.Fatalf("DecideEnv with a live root: %v", err)
	}
	if len(searched) == 0 {
		t.Fatal("DecideEnv with a live root and feed emitted no sbsearch.Diag: the search did not run (the adapter fell back instead of forwarding env.Search)")
	}
	t.Logf("search ran at seq %d: %+v", d.Seq, searched[0])
}
