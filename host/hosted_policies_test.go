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
//     to one match's live engine, answers the swapped decision identically
//     from two honest roots whose hidden worlds are name-identical.

import (
	"context"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/bots"
	_ "github.com/adams-shaun/gorge/bots/all"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/replay"
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
	})
}

// TestHostedEnvSeatsIgnoreTheRealHiddenCardsAZRedeal is the az-redeal row of
// the same property (BP-12, §5.3): the REAL az adapter at slot 0, through the
// REAL host Env path, with the swap fixture applied to the second match's
// live engine at the first decision the adapter wants an Env for (a searched
// kind: priority, attackers, blockers or target — the az seat's WantsEnv is
// the kind set, not a per-decision eligibility). The az seat's whole world
// source is a redeal over the honest root, so a root that depended on the
// real hidden cards would deal different worlds — and search them to
// different answers — in the two matches. The decision played at the swapped
// boundary must be answered identically from two roots whose hidden worlds
// are name-identical. The spy row of the same property lives in
// botenv_test.go.
func TestHostedEnvSeatsIgnoreTheRealHiddenCardsAZRedeal(t *testing.T) {
	t.Parallel()
	hostedEnvLeakRow(t, azRedealPolicy, func(actor uint64) seat.Seat {
		s, err := bots.New(azRedealPolicy, bots.Options{Seed: actor})
		if err != nil {
			t.Fatalf("the az-redeal policy is not linked into this binary: %v", err)
		}
		return s
	})
}

// hostedEnvLeakRow is the shared body of the leak-test rows: two live matches
// built around the REAL hosted policy, driven through the REAL host Env path
// (projectNext under the lock, parkSeat outside it), with the swap fixture
// applied to the second match's live engine at the first decision the policy
// wants an Env for. policyNew builds a FRESH seat per match: a hosted bot
// carries its own RNG state, so sharing one seat between the two driven
// matches would couple them.
func hostedEnvLeakRow(t *testing.T, policy string, policyNew func(actor uint64) seat.Seat) {
	const seed = uint64(20260928)
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

	inner := func(actor uint64) *envSpySeat {
		// A FRESH seat per match: a hosted bot carries its own RNG state, so
		// sharing one seat between the two driven matches would couple them
		// (the spy row builds a fresh seat.NewBot per match for the same
		// reason).
		return &envSpySeat{inner: policyNew(actor)}
	}
	build := func(actor uint64) (*match, *envSpySeat, []seat.Seat) {
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
		if swapped || !seats2[0].(bots.EnvSeat).WantsEnv(d) {
			return
		}
		if n := swapOpponentHidden(t, m.e, m.feeds.bySlot[0]); n > 0 {
			swapped = true
			targetSeq = d.Seq
		}
	}

	brd1, brd2 := botpolicy.NewBoard(2), botpolicy.NewBoard(2)
	for steps := 0; steps < 20000; steps++ {
		_, pd1 := driveEnvStep(t, m1, seats1, &brd1, nil)
		if pd1 == nil {
			t.Fatal("match 1 ended before the swapped decision was reached")
		}
		_, pd2 := driveEnvStep(t, m2, seats2, &brd2, swapFn)
		if pd2 == nil {
			t.Fatal("match 2 ended before the swapped decision was reached")
		}
		submitEnvStep(t, r, tbl, m1, pd1)
		submitEnvStep(t, r, tbl, m2, pd2)
		if rec1, rec2 := spy1.recorded(targetSeq), spy2.recorded(targetSeq); swapped && rec1 != nil && rec2 != nil {
			break
		}
	}
	if !swapped {
		t.Fatal("fixture: no opponent hand card could be swapped at any decision the search adapter wants an Env for")
	}
	t.Logf("swap target: seq %d (an Env-eligible decision)", targetSeq)
	rec1, rec2 := spy1.recorded(targetSeq), spy2.recorded(targetSeq)
	if rec1 == nil || rec2 == nil {
		t.Fatalf("the swapped decision (seq %d) was never parked on DecideEnv (records %d/%d)",
			targetSeq, len(spy1.envs), len(spy2.envs))
	}
	if rec1.engine == nil || rec2.engine == nil {
		t.Fatalf("an honest root was refused at the swapped boundary (%q / %q): the search adapter was never handed a world",
			rec1.rootRef, rec2.rootRef)
	}

	// Fixture precondition: the two live positions genuinely differ in hidden
	// cards — the swap did its work and nothing public moved.
	if hiddenWorldsEqual(m1.e, m2.e) {
		t.Fatal("fixture: the two live positions hold the same hidden cards")
	}
	// Central claim: the two roots are name-identical in every hidden zone,
	// and the decision the real adapter answered is identical in both worlds.
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
