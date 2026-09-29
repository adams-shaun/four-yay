package host

// BP-11 (spec 2026-09-28-hosted-bot-packages §11): the hosted-policy round
// table. This file links every built-in hosted policy (bots/all) so the
// tests below see the full registry, and owns the two whole-policy gates:
//
//   - TestEveryHostedPolicyIsDeterministic (§8): every registered hosted
//     entry plays two in-memory 2-seat tables from the same seed — capped at
//     150 intents for search entries (a searched decision costs ~0.2-0.7 s),
//     a whole game otherwise — and the two runs must produce identical event
//     logs and intents, each of which replays cleanly.
//   - One row per Env entry of the seat-level leak test (§5.3): the REAL
//     adapter through the REAL host Env path, with the swap fixture applied
//     to one match's live engine at the first decision the adapter wants an
//     Env for, answers the asserted decision identically from two honest
//     roots whose hidden worlds are name-identical. For the search entry the
//     asserted decision is the swap boundary itself; for az-redeal it is the
//     first decision AT OR AFTER the swap that the seat actually SEARCHED in
//     both matches (the az search skips a decision with fewer than two
//     candidates without asking its world source at all, so a swap boundary
//     there would never exercise the adapter's search forwarding).

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/bots"
	_ "github.com/adams-shaun/gorge/bots/all"
	"github.com/adams-shaun/gorge/bots/sbtactical"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// searchPolicy is the search entry's registry key. The name is spelled out
// rather than imported from bots/search because importing that package would
// LINK it, and the vocabulary test below must refuse the bench-only spellings
// regardless of what this binary links; bots/all links search here once, at
// the blank import above.
const searchPolicy = "search"

// azRedealPolicy is the az-redeal entry's registry key, spelled out for the
// same reason searchPolicy is: bots/all links the package once, above.
const azRedealPolicy = "az-redeal"

// envLeakSeed is the seed every leak row (and the az row's pre-pass) drives:
// one fixed seed is what makes the pre-pass's found boundary valid for the
// row's matches — the drive is deterministic from it.
const envLeakSeed = uint64(20260928)

// detSearchIntentsCap is the intent cap a search entry plays (§8): a whole
// game of searches would take minutes, and 150 intents already covers the
// first several combat turns of a 2-seat game — enough searched decisions to
// make nondeterminism in the teacher show up.
const detSearchIntentsCap = 150

// TestEveryHostedPolicyIsDeterministic is §8's registry gate: for each
// registered hosted entry, two in-memory 2-seat tables from the same seed
// must produce identical event logs and intents, and each log must replay
// cleanly to the recorded head. The bot entry keeps its existing golden
// check (host/host_test.go TestHostedPoliciesReplayDeterministically); this
// gate is the same property, driven through the table-policy path, for every
// entry as it lands.
func TestEveryHostedPolicyIsDeterministic(t *testing.T) {
	entries := bots.Entries()
	if len(entries) < 2 {
		t.Fatalf("the registry holds %d entries; the host must see the built-in policies (known: %v)", len(entries), bots.Names())
	}
	played := false
	for _, e := range entries {
		if e.Name == bp10SpyPolicy {
			// The BP-10 slot-test fixture registers itself in this binary
			// (host/slots_test.go); it is a harness, not a hosted policy.
			continue
		}
		played = true
		e := e
		t.Run(e.Name, func(t *testing.T) {
			const seed = uint64(20260928)
			intents := 0
			if e.Search {
				intents = detSearchIntentsCap
			}
			run := func(tag string) (protocol.MatchInfo, *events.Log) {
				opts := testOptions(t)
				opts.MaxIntents = intents // 0 = play the whole game
				// BP-13: every hosted policy is built with the same card
				// dependency a served table gets, so the registry gate covers
				// the corpus-hungry entries exactly as the host threads them
				// (a policy whose factory needs one cannot dodge the gate).
				opts.BotDeps = sampleBotDeps(t)
				if e.Search {
					// Parallelism changes latency only, never an answer
					// (searchseat.Options.Parallelism); 4 keeps the run short.
					opts.BotSearchParallelism = 4
				}
				r, err := New(opts)
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close()
				cfg := TableConfig{ID: "t1", Name: "det" + tag, Seats: 2, Decks: []string{"a", "b"},
					Seed: seed, Pace: 0, Spectator: view.Public, BotPolicy: e.Name}
				if err := r.AddTable(cfg); err != nil {
					t.Fatalf("%q: AddTable: %v", e.Name, err)
				}
				if err := r.Start("t1"); err != nil {
					t.Fatalf("%q: Start: %v", e.Name, err)
				}
				r.Wait("t1")
				r.mu.RLock()
				tbl := r.tables["t1"]
				r.mu.RUnlock()
				tbl.mu.RLock()
				if len(tbl.history) != 1 {
					t.Fatalf("%q: table kept %d matches, want 1", e.Name, len(tbl.history))
				}
				m := tbl.history[0]
				tbl.mu.RUnlock()
				m.mu.RLock()
				info, log, rulesCfg := m.info(), m.e.L.Clone(), m.cfg
				m.mu.RUnlock()
				m.mu.RLock()
				refusals, live := m.rootRefusals, m.feeds != nil
				m.mu.RUnlock()
				t.Logf("%s: %s turn %d intents %d events %d rootRefusals %d feeds %v", tag, info.State, info.Turns, len(log.Intents), len(log.Events), refusals, live)
				replayed, err := replay.Replay(log, rulesCfg)
				if err != nil {
					t.Fatalf("%q replay: %v", e.Name, err)
				}
				m.mu.RLock()
				head := info.Head
				m.mu.RUnlock()
				if got := replayed.L.Head(); got != head {
					t.Fatalf("%q replay head %s, want %s", e.Name, got, head)
				}
				return info, log
			}
			aInfo, aLog := run("a")
			bInfo, bLog := run("b")
			if intents > 0 {
				// Precondition: the capped entry actually reached the cap — a
				// truncated run that never happened would compare two empty
				// logs and pass. 150 intents is a quarter-game of decisions,
				// and the search entries have no honest way to end a 2-seat
				// game that early.
				if n := len(aLog.Intents); n != intents {
					t.Fatalf("run a logged %d intents, want the cap %d", n, intents)
				}
			} else {
				// Precondition for the whole-game entries: a real match ran,
				// so the equality below is not two no-op runs.
				if aInfo.State != protocol.MatchFinished || (aInfo.Result != "win" && aInfo.Result != "draw") {
					t.Fatalf("%q did not finish with a valid outcome: %+v", e.Name, aInfo)
				}
			}
			if !reflect.DeepEqual(aLog.Events, bLog.Events) || !reflect.DeepEqual(aLog.Intents, bLog.Intents) {
				t.Fatalf("two %q runs differ (events %d/%d, intents %d/%d, heads %s/%s, results %q/%q)",
					e.Name, len(aLog.Events), len(bLog.Events), len(aLog.Intents), len(bLog.Intents), aInfo.Head, bInfo.Head, aInfo.Result, bInfo.Result)
			}
			if aInfo.Head != bInfo.Head || aInfo.Result != bInfo.Result || !reflect.DeepEqual(aInfo.Winner, bInfo.Winner) {
				t.Fatalf("two %q runs differ in outcome: %+v vs %+v", e.Name, aInfo, bInfo)
			}
			if n := len(aLog.Intents); n < 40 {
				t.Fatalf("%q logged only %d intents; too short a game to be evidence of anything", e.Name, n)
			}
		})
	}
	if !played {
		t.Fatal("no registered entry was played: the skip list above matched everything")
	}
}

// envSpySeat wraps a REAL hosted EnvSeat and records exactly what the host
// handed each DecideEnv call, so a leak-test row can assert on the honest
// root the real adapter was handed while the adapter itself answers. Every
// half delegates to the wrapped seat; the wrapper implements bots.EnvSeat
// itself, so the host builds a feed for it and builds Envs for it exactly as
// for the bare adapter — which is what makes the wrapper transparent to the
// host path under test.
type envSpySeat struct {
	inner seat.Seat // a bots.EnvSeat at runtime; the built hosted policy
	envs  []envRecord
}

func (s *envSpySeat) wrapped() bots.EnvSeat {
	es, ok := s.inner.(bots.EnvSeat)
	if !ok {
		panic("envSpySeat wraps a non-EnvSeat policy")
	}
	return es
}

func (s *envSpySeat) WantsEnv(d *decision.Decision) bool { return s.wrapped().WantsEnv(d) }

func (s *envSpySeat) DecideEnv(ctx context.Context, env bots.Env, d decision.Decision) (decision.Intent, error) {
	rec := envRecord{seq: d.Seq, rootRef: env.RootRefused}
	if e := env.Search.Engine; e != nil {
		rec.engine, rec.g = e, e.G
		rec.actorHand = zoneNames(e.G.Zone(state.ZHand, 0), e)
		rec.oppHand = zoneNames(e.G.Zone(state.ZHand, 1), e)
		rec.actorLib = zoneNames(e.G.Zone(state.ZLibrary, 0), e)
		rec.oppLib = zoneNames(e.G.Zone(state.ZLibrary, 1), e)
	}
	in, err := s.wrapped().DecideEnv(ctx, env, d)
	rec.in = in
	s.envs = append(s.envs, rec)
	return in, err
}

func (s *envSpySeat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	return s.inner.Decide(ctx, v, d)
}

func (s *envSpySeat) DecideBoard(ctx context.Context, b botpolicy.Board, d decision.Decision) (decision.Intent, error) {
	bs, ok := s.inner.(seat.BoardSeat)
	if !ok {
		// Every hosted policy is a BoardSeat; a non-BoardSeat would be parked
		// by the host on a zero View, which is a wiring bug, not an answer.
		panic("envSpySeat wraps a non-BoardSeat policy")
	}
	return bs.DecideBoard(ctx, b, d)
}

// recorded returns the env record of the decision with the given seq, or nil.
func (s *envSpySeat) recorded(seq uint64) *envRecord {
	for i := range s.envs {
		if s.envs[i].seq == seq {
			return &s.envs[i]
		}
	}
	return nil
}

func (s *envSpySeat) envCount() int        { return len(s.envs) }
func (s *envSpySeat) innerSeat() seat.Seat { return s.inner }

// envViewSpySeat is the View-path spy of the same recording contract: it
// wraps a hosted EnvSeat that does NOT implement seat.BoardSeat (sb-tactical,
// BP-13: its WantsEnv is priority-only and every other decision rides the
// plain View path), so it deliberately carries no DecideBoard — implementing
// one would send the host down the BoardSeat branch and hand the policy a
// board it never sees in production. The Env half (WantsEnv/DecideEnv) is
// recorded identically to envSpySeat; the View half delegates to the wrapped
// seat's Decide, the exact call the host's plain branch makes. The optional
// interfaces a hosted bot may carry — seat.PaymentPlanConsumer (the
// payment-action offer the AutoPay tactical cast candidates read) and
// bots.RefusalAnswerer (the ladder's rung 1) — are forwarded, so the host
// treats the wrapper exactly as it treats the bare adapter.
type envViewSpySeat struct {
	inner seat.Seat // the built hosted policy
	envs  []envRecord
}

func (s *envViewSpySeat) innerSeat() seat.Seat { return s.inner }

func (s *envViewSpySeat) WantsEnv(d *decision.Decision) bool {
	return s.inner.(bots.EnvSeat).WantsEnv(d)
}

func (s *envViewSpySeat) DecideEnv(ctx context.Context, env bots.Env, d decision.Decision) (decision.Intent, error) {
	rec := envRecord{seq: d.Seq, rootRef: env.RootRefused}
	if e := env.Search.Engine; e != nil {
		rec.engine, rec.g = e, e.G
		rec.actorHand = zoneNames(e.G.Zone(state.ZHand, 0), e)
		rec.oppHand = zoneNames(e.G.Zone(state.ZHand, 1), e)
		rec.actorLib = zoneNames(e.G.Zone(state.ZLibrary, 0), e)
		rec.oppLib = zoneNames(e.G.Zone(state.ZLibrary, 1), e)
	}
	in, err := s.inner.(bots.EnvSeat).DecideEnv(ctx, env, d)
	rec.in = in
	s.envs = append(s.envs, rec)
	return in, err
}

func (s *envViewSpySeat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	return s.inner.Decide(ctx, v, d)
}

func (s *envViewSpySeat) WantsPaymentActions() bool {
	if pc, ok := s.inner.(seat.PaymentPlanConsumer); ok {
		return pc.WantsPaymentActions()
	}
	return false
}

func (s *envViewSpySeat) AnswerRefused(v view.View, d decision.Decision, refused decision.Intent) decision.Intent {
	return s.inner.(bots.RefusalAnswerer).AnswerRefused(v, d, refused)
}

func (s *envViewSpySeat) recorded(seq uint64) *envRecord {
	for i := range s.envs {
		if s.envs[i].seq == seq {
			return &s.envs[i]
		}
	}
	return nil
}

func (s *envViewSpySeat) envCount() int { return len(s.envs) }

// sampleBotDeps is the card dependency the host tests thread: a cards
// registry over the same sample deck pool testOptions deals, built without
// the corpus so the gate runs everywhere. A policy whose factory reads card
// facts (sb-tactical) resolves them from these cards, exactly what its own
// package's tests resolve.
func sampleBotDeps(t *testing.T) bots.Deps {
	t.Helper()
	_, decks := testutil.SampleDecks(t, 4)
	reg := cards.NewRegistry()
	for _, cs := range decks {
		for _, c := range cs {
			reg.Add(c)
		}
	}
	return bots.Deps{Cards: reg}
}

// TestHostedEnvSeatsIgnoreTheRealHiddenCardsSearch is the search row of the
// seat-level leak test (§5.3): the REAL search adapter at slot 0, through the
// REAL host Env path (projectNext under the lock, parkSeat outside it), with
// the swap fixture applied to the second match's live engine at the first
// decision the adapter wants an Env for. The decision played at the swapped
// boundary must be answered identically from two roots whose hidden worlds
// are name-identical — what the honest root deals is a function of the seat's
// observation and the seed alone, never of the real hidden cards. The spy row
// of the same property lives in botenv_test.go; this row proves it for a
// policy whose WantsEnv gates the Env path on searchseat.Eligible.
func TestHostedEnvSeatsIgnoreTheRealHiddenCardsSearch(t *testing.T) {
	t.Parallel()
	hostedEnvLeakRow(t, searchPolicy, func(actor uint64) seat.Seat {
		s, err := bots.New(searchPolicy, bots.Options{Seed: actor, SearchParallelism: 2})
		if err != nil {
			t.Fatalf("the search policy is not linked into this binary: %v", err)
		}
		return s
	}, nil)
}

// TestHostedEnvSeatsIgnoreTheRealHiddenCardsSBTactical is the sb-tactical row
// of the same property (BP-13, §5.3): the REAL sb-tactical adapter at slot 0,
// through the REAL host Env path, with the swap fixture applied to the second
// match's live engine at the first decision the adapter wants an Env for — a
// priority ask, the only kind sb-tactical's WantsEnv claims. The adapter's
// whole world source is the honest root as its payment planner, so a root
// that depended on the real hidden cards would price the plays differently
// and could answer the swapped decision differently in the two matches.
//
// The spy is envViewSpySeat, not envSpySeat: sb-tactical implements no
// DecideBoard (§5.1: a hybrid adapter leaves the plain View path in place),
// so a BoardSeat spy would exercise a host branch the policy never takes.
// The probe's target hook adds the routing assertion the §5.1 adapter rule
// exists for: at the asserted boundary the adapter's planner is the honest
// root the ENV carried — SetPlanner on the root, and never on the live
// engine — in both matches. The spy row of the same property lives in
// botenv_test.go.
func TestHostedEnvSeatsIgnoreTheRealHiddenCardsSBTactical(t *testing.T) {
	t.Parallel()
	hostedEnvLeakRowSpy(t, sbtactical.Policy, func(actor uint64) seat.Seat {
		s, err := bots.New(sbtactical.Policy, bots.Options{Seed: actor, Deps: sampleBotDeps(t)})
		if err != nil {
			t.Fatalf("the sb-tactical policy is not linked into this binary: %v", err)
		}
		return s
	}, &leakProbe{
		target: func(spy1, spy2 envSpy, boundary uint64) uint64 {
			r1, r2 := spy1.recorded(boundary), spy2.recorded(boundary)
			if r1 == nil || r2 == nil || r1.engine == nil || r2.engine == nil {
				return 0
			}
			roots := [2]*rules.Engine{r1.engine, r2.engine}
			for i, sp := range []envSpy{spy1, spy2} {
				ad, ok := sp.innerSeat().(interface{ Seat() *builtins.Seat })
				if !ok {
					t.Fatalf("the sb-tactical adapter at slot %d does not expose its wrapped seat", i)
				}
				if ad.Seat().Planner() != roots[i] {
					t.Fatalf("slot %d: the adapter's planner is not the Env's honest root: the SetPlanner routing did not run at the asserted boundary", i)
				}
			}
			return boundary
		},
	}, func(s seat.Seat) envSpy { return &envViewSpySeat{inner: s} })
}

// TestHostedEnvSeatsIgnoreTheRealHiddenCardsAZRedeal is the az-redeal row of
// the same property (BP-12, §5.3): the REAL az adapter at slot 0, through the
// REAL host Env path, with the swap fixture applied to the second match's
// live engine at the first decision the adapter wants an Env for (a searched
// kind: priority, attackers, blockers or target — the az seat's WantsEnv is
// the kind set, not a per-decision eligibility). The az seat's whole world
// source is a redeal over the honest root, so a root that depended on the
// real hidden cards would deal different worlds — and search them to
// different answers — in the two matches.
//
// The swap boundary must be a decision the seat actually SEARCHED: the az
// search skips a decision with fewer than two candidates without asking its
// world source at all (internal/azmcts/search.go: the bot's intent is played
// and no world is built), so a swap boundary there would exercise only the
// host's HonestRoot and never the adapter's DecideSearch forwarding. A
// decision only reveals it is searched by being searched — which the swap
// must precede — so azFirstSearched pre-passes one unswapped match (the
// row's seed, the row's seats: the drive is deterministic) and the row
// places the swap at the decision it found; the asserted boundary is that
// swap boundary, and the two games are never compared past it (the swapped
// hand makes the opponent bot diverge legitimately). azmcts.Watch (one Diag
// per decision of a searched kind; a test-only link — the archtest scans
// non-test imports — and this row is the only az driver in the binary's
// parallel set, so the package-level hook belongs to it alone) then
// re-verifies, after the row's own assertions, that both matches actually
// searched the asserted boundary: without that check a regression that stops
// the search would make this row vacuous. The spy row of the same property
// lives in botenv_test.go.
func TestHostedEnvSeatsIgnoreTheRealHiddenCardsAZRedeal(t *testing.T) {
	t.Parallel()
	swapSeq, pre := azFirstSearched(t, func(actor uint64) seat.Seat {
		s, err := bots.New(azRedealPolicy, bots.Options{Seed: actor})
		if err != nil {
			t.Fatalf("the az-redeal policy is not linked into this binary: %v", err)
		}
		return s
	})
	var mu sync.Mutex
	var diags []azmcts.Diag
	// searched[m][seq] holds the Diag of the decision seq that match m's az
	// seat actually searched (Stats.Searched == 1) — the check that keeps
	// this row from ever being vacuous again.
	searched := [3]map[uint64]azmcts.Diag{{}, {}, {}} // indexed by match 1|2
	prev := azmcts.Watch
	azmcts.Watch = func(dg azmcts.Diag) {
		mu.Lock()
		defer mu.Unlock()
		diags = append(diags, dg)
	}
	defer func() { azmcts.Watch = prev }()
	probe := &leakProbe{
		diagCount: func() int {
			mu.Lock()
			defer mu.Unlock()
			return len(diags)
		},
		onDecided: func(m int, seq uint64, n int) {
			mu.Lock()
			defer mu.Unlock()
			for _, dg := range diags[len(diags)-n:] {
				if dg.Kind != "" && dg.Stats.Searched == 1 {
					searched[m][seq] = dg
				}
			}
		},
		// The fixture fires at, and the row asserts on, the pre-pass's found
		// decision — the only boundary at which the az seat is known to
		// search (and the last boundary before the swapped hand makes the
		// two games legitimately diverge).
		swapAt: func(d *decision.Decision) bool { return d.Seq == swapSeq },
		target: func(spy1, spy2 envSpy, boundary uint64) uint64 {
			r1, r2 := spy1.recorded(boundary), spy2.recorded(boundary)
			if r1 == nil || r2 == nil || r1.engine == nil || r2.engine == nil {
				return 0
			}
			return boundary
		},
	}
	hostedEnvLeakRow(t, azRedealPolicy, func(actor uint64) seat.Seat {
		s, err := bots.New(azRedealPolicy, bots.Options{Seed: actor})
		if err != nil {
			t.Fatalf("the az-redeal policy is not linked into this binary: %v", err)
		}
		return s
	}, probe)
	// The asserted boundary is a decision the seat actually SEARCHED in both
	// matches. The pre-pass picked it under the same seed and seats, and the
	// drive is deterministic — but that is a claim, not a proof: a regression
	// that stops the search (the adapter no longer forwarding env.Search, a
	// config that drops the kind), or any divergence between the matches
	// before the boundary, would otherwise pass the row silently.
	mu.Lock()
	d1, ok1 := searched[1][swapSeq]
	d2, ok2 := searched[2][swapSeq]
	mu.Unlock()
	if !ok1 || !ok2 {
		t.Fatalf("the asserted boundary (seq %d, %s, %d candidates in the pre-pass) was not SEARCHED in both matches (searched %v/%v): the az adapter's DecideSearch forwarding is not exercised at the boundary and this row would be vacuous",
			swapSeq, pre.Kind, pre.Candidates, ok1, ok2)
	}
	t.Logf("asserted boundary: seq %d %s searched, %d candidates (match 2: %d) , %d simulations", swapSeq, d1.Kind, d1.Candidates, d2.Candidates, d1.Stats.Simulations)
}

// azFirstSearched is the AZ leak row's pre-pass: one unswapped match of the
// policy, driven through the REAL host Env path from the row's seed and the
// row's seats, until the az seat's own search actually runs once
// (azmcts.Watch, Stats.Searched == 1). It returns that decision's seq and
// diag. The row's swap must fire AT a searched decision — the asserted
// boundary is the same decision, and a decision only reveals it is searched
// by being searched, which the swap must precede. The drive is deterministic
// from the seed, so the seq found here is searched in the row's matches too;
// the row re-verifies that through the same hook rather than trusting it.
func azFirstSearched(t *testing.T, policyNew func(actor uint64) seat.Seat) (uint64, azmcts.Diag) {
	t.Helper()
	var mu sync.Mutex
	var diags []azmcts.Diag
	prev := azmcts.Watch
	azmcts.Watch = func(dg azmcts.Diag) {
		mu.Lock()
		defer mu.Unlock()
		diags = append(diags, dg)
	}
	defer func() { azmcts.Watch = prev }()
	r, err := New(testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(envTestTable("t1", envLeakSeed)); err != nil {
		t.Fatal(err)
	}
	r.mu.RLock()
	tbl := r.tables["t1"]
	r.mu.RUnlock()
	// The row's seats exactly: the spy wrapper the row builds delegates
	// identically and consumes the same RNG stream, so the decision stream —
	// and with it the first searched decision's seq — matches the row's.
	seats := []seat.Seat{policyNew(envLeakSeed ^ 1), seat.NewBot(envLeakSeed ^ 1)}
	m, err := r.newMatch(tbl, 0)
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.slots = seats
	m.feeds = newMatchFeeds(seats)
	m.mu.Unlock()
	brd := botpolicy.NewBoard(2)
	for steps := 0; steps < 20000; steps++ {
		mu.Lock()
		n0 := len(diags)
		mu.Unlock()
		_, pd := driveEnvStep(t, m, seats, &brd, nil)
		if pd == nil {
			t.Fatal("pre-pass: the match ended without the az seat ever searching a decision")
		}
		mu.Lock()
		var seq uint64
		var dg azmcts.Diag
		for _, d := range diags[n0:] {
			if d.Kind != "" && d.Stats.Searched == 1 && seq == 0 {
				seq, dg = pd.in.Seq, d
			}
		}
		mu.Unlock()
		submitEnvStep(t, r, tbl, m, pd)
		if seq != 0 {
			return seq, dg
		}
	}
	t.Fatal("pre-pass: the step cap was reached without the az seat ever searching a decision")
	return 0, azmcts.Diag{}
}

// leakProbe is the optional extension point of hostedEnvLeakRow: the search
// row passes a nil probe and gets the shared body verbatim. A probe row (the
// az-redeal row) uses it to attribute the policy's own search diagnostics to
// the parked Env decisions and to move the asserted boundary off the swap
// boundary — the az search skips a decision with fewer than two candidates
// without asking its world source, so the first Env decision is not
// necessarily one the seat searched to.
type leakProbe struct {
	// diagCount returns the number of the policy's search diagnostics
	// observed so far. The helper calls it immediately before and after each
	// parkSeat; with the two matches driven synchronously one step at a
	// time, the diagnostics that appeared during match m's park belong to
	// that match's decision exactly.
	diagCount func() int
	// onDecided is called after each parked decision of match m (1 or 2) at
	// seq with the number of diagnostics that arrived during its park (the
	// probe reads its own slice tail; none of the helper's business).
	onDecided func(m int, seq uint64, n int)
	// swapAt, when non-nil, replaces the swap gate: the fixture fires when it
	// returns true (instead of at the first Env-eligible decision). A probe
	// row uses it to place the swap at a decision its pre-pass proved is
	// searched — the asserted boundary is the same decision.
	swapAt func(d *decision.Decision) bool
	// target, when non-nil, moves the asserted boundary: called after each
	// full drive round once the swap is in place, it returns the seq to
	// assert on (>0) or 0 to keep driving; the drive then fails loudly if a
	// match ends or the step cap is reached without one.
	target func(spy1, spy2 envSpy, swapSeq uint64) uint64
}

// envSpy is the recording-spy interface hostedEnvLeakRowSpy drives: one
// wrapper per match. Every half (Decide / DecideEnv) delegates to the real
// hosted policy while recording the Env inputs, so the leak rows assert on
// what the REAL adapter was handed and answered.
type envSpy interface {
	seat.Seat
	recorded(seq uint64) *envRecord
	envCount() int
	innerSeat() seat.Seat
}

// probeDiags is diagCount for a possibly nil probe.
func probeDiags(p *leakProbe) int {
	if p == nil || p.diagCount == nil {
		return 0
	}
	return p.diagCount()
}

// hostedEnvLeakRow is the shared body of the leak-test rows: two live matches
// built around the REAL hosted policy, driven through the REAL host Env path
// (projectNext under the lock, parkSeat outside it), with the swap fixture
// applied to the second match's live engine at the first decision the policy
// wants an Env for. policyNew builds a FRESH seat per match: a hosted bot
// carries its own RNG state, so sharing one seat between the two driven
// matches would couple them. probe, when non-nil, attributes the policy's
// search diagnostics to the parked decisions and may move the asserted
// boundary off the swap boundary (see leakProbe).
func hostedEnvLeakRow(t *testing.T, policy string, policyNew func(actor uint64) seat.Seat, probe *leakProbe) {
	hostedEnvLeakRowSpy(t, policy, policyNew, probe, func(s seat.Seat) envSpy { return &envSpySeat{inner: s} })
}

// hostedEnvLeakRowSpy is hostedEnvLeakRow's body with the recording spy
// chosen by the caller: the Board-path rows use envSpySeat (above), the
// View-path rows (BP-13's sb-tactical row) use envViewSpySeat, whose spy
// carries no DecideBoard so the host's plain branch is exercised exactly.
func hostedEnvLeakRowSpy(t *testing.T, policy string, policyNew func(actor uint64) seat.Seat, probe *leakProbe, spyFor func(seat.Seat) envSpy) {
	const seed = envLeakSeed
	r, err := New(testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(envTestTable("t1", seed)); err != nil {
		t.Fatal(err)
	}
	r.mu.RLock()
	tbl := r.tables["t1"]
	r.mu.RUnlock()

	inner := func(actor uint64) envSpy {
		// A FRESH seat per match: a hosted bot carries its own RNG state, so
		// sharing one seat between the two driven matches would couple them
		// (the spy row builds a fresh seat.NewBot per match for the same
		// reason).
		return spyFor(policyNew(actor))
	}
	build := func(actor uint64) (*match, envSpy, []seat.Seat) {
		spy := inner(actor)
		seats := []seat.Seat{spy, seat.NewBot(actor)}
		m, err := r.newMatch(tbl, 0)
		if err != nil {
			t.Fatal(err)
		}
		m.mu.Lock()
		m.slots = seats
		m.feeds = newMatchFeeds(seats)
		m.mu.Unlock()
		return m, spy, seats
	}
	m1, spy1, seats1 := build(seed ^ 1)
	m2, spy2, seats2 := build(seed ^ 1)

	// The two matches start from the same seed and the same seats, so before
	// the fixture the deciding seat's own hand must agree — a mismatch would
	// mean the driver, not the fixture, moved hidden state.
	var swapped bool
	var targetSeq uint64
	swapFn := func(m *match, d *decision.Decision) {
		if swapped {
			return
		}
		if probe != nil && probe.swapAt != nil {
			if !probe.swapAt(d) {
				return
			}
		} else if !seats2[0].(bots.EnvSeat).WantsEnv(d) {
			return
		}
		if n := swapOpponentHidden(t, m.e, m.feeds.bySlot[0]); n > 0 {
			swapped = true
			targetSeq = d.Seq
		}
	}

	brd1, brd2 := botpolicy.NewBoard(2), botpolicy.NewBoard(2)
	assertSeq := uint64(0)
	for steps := 0; steps < 20000; steps++ {
		before1 := probeDiags(probe)
		_, pd1 := driveEnvStep(t, m1, seats1, &brd1, nil)
		after1 := probeDiags(probe)
		if pd1 == nil {
			t.Fatal("match 1 ended before the asserted boundary was reached")
		}
		if probe != nil && probe.onDecided != nil {
			probe.onDecided(1, pd1.in.Seq, after1-before1)
		}
		before2 := probeDiags(probe)
		_, pd2 := driveEnvStep(t, m2, seats2, &brd2, swapFn)
		after2 := probeDiags(probe)
		if pd2 == nil {
			t.Fatal("match 2 ended before the asserted boundary was reached")
		}
		if probe != nil && probe.onDecided != nil {
			probe.onDecided(2, pd2.in.Seq, after2-before2)
		}
		submitEnvStep(t, r, tbl, m1, pd1)
		submitEnvStep(t, r, tbl, m2, pd2)
		if probe != nil && probe.target != nil && swapped {
			// A probe row keeps driving until it has the boundary it wants.
			if want := probe.target(spy1, spy2, targetSeq); want != 0 {
				assertSeq = want
				break
			}
			continue
		}
		if rec1, rec2 := spy1.recorded(targetSeq), spy2.recorded(targetSeq); swapped && rec1 != nil && rec2 != nil {
			assertSeq = targetSeq
			break
		}
	}
	if !swapped {
		t.Fatal("fixture: no opponent hand card could be swapped at any decision the policy wants an Env for")
	}
	if assertSeq == 0 && probe != nil && probe.target != nil {
		t.Fatalf("fixture: %s: the asserted boundary (swap seq %d) never reached both DecideEnv records: the two matches diverged before it or the drive hit its cap, and the leak claim cannot be asserted", policy, targetSeq)
	}
	if assertSeq == 0 {
		assertSeq = targetSeq
	}
	t.Logf("swap target: seq %d (an Env-eligible decision); asserted boundary: seq %d", targetSeq, assertSeq)
	rec1, rec2 := spy1.recorded(assertSeq), spy2.recorded(assertSeq)
	if rec1 == nil || rec2 == nil {
		t.Fatalf("the asserted decision (seq %d) was never parked on DecideEnv (records %d/%d)",
			assertSeq, spy1.envCount(), spy2.envCount())
	}
	if rec1.engine == nil || rec2.engine == nil {
		t.Fatalf("an honest root was refused at the asserted boundary (%q / %q): the adapter was never handed a world",
			rec1.rootRef, rec2.rootRef)
	}

	// Fixture precondition: the two live positions genuinely differ in hidden
	// cards — the swap did its work and nothing public moved.
	if hiddenWorldsEqual(m1.e, m2.e) {
		t.Fatal("fixture: the two live positions hold the same hidden cards")
	}
	// Central claim: the two roots are name-identical in every hidden zone,
	// and the decision the real adapter answered is identical in both worlds.
	// The swap happened BEFORE the asserted boundary, so a root that read the
	// real hidden cards would differ here.
	if !hiddenWorldsEqual(rec1.engine, rec2.engine) {
		t.Fatalf("two roots for the same seed and feed depend on the real hidden cards:\nreal hand %v swapped %v\nreal lib %v swapped %v",
			rec1.oppHand, rec2.oppHand, rec1.oppLib, rec2.oppLib)
	}
	if !reflect.DeepEqual(rec1.in, rec2.in) {
		t.Fatalf("the two matches answered the swapped decision differently: %+v vs %+v", rec1.in, rec2.in)
	}
	// The deciding seat's own zones are untouched by the fixture and must
	// match in the roots too (the redeal keeps what the seat knows).
	if !sortedEqual(rec1.actorHand, rec2.actorHand) {
		t.Fatal("the two roots dealt the actor's own hand differently")
	}
}
