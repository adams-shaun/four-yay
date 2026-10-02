//go:build manabrew

package mbtest

// MBX-7: a live vote card played through TranslatingSeat with the MBX-3
// reachability check. The reported bug: casting Council's Judgment posed a
// KChoose (api:Vote) the translator did not map; the client's answer came
// back invalidShape and the seat then waited forever. The unit half of the
// fix is pinned in internal/manabrew (prompt_vote_fallback_test.go); this
// file plays the vote for real -- a seeded game whose seats cast the vote
// spell and its ballot fodder through the ManaBrew wire, every voter
// answering through TranslatingSeat -- for the two ballot shapes the
// corpus's vote cards pose:
//
//   - Council's Judgment: the CARD ballot (askCardVote, options Kind
//     "vote_card" -- the reported card).
//   - Mob Verdict: the PLAYER ballot (effPlayerVote, options Kind "player").
//
// Both games assert the MB-8 census recorded no unmapped pose, no rejected
// answer and no fallback (the votes go through their SPECIFIC prompts,
// never the MBX-7 generic fallback), that the MBX-3 sweep reached every
// vote option (reach key "choose:vote_card" / "choose:player", zero
// unreachable keys in the whole game), and that the vote itself RESOLVED
// the way the card reads: the most-voted permanent is exiled; the
// most-voted players took their Secret council damage. The seats cast via
// the CastSeekerPolicy -- which builds mana and casts like a real client
// (never wasting pool outside its own main phases) -- so the game is
// deterministic and independent of seed quirks: seed 1 plays the same game
// every run.

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
)

// voteGameCaps are the bench watchdogs for one vote game. The spell goes
// down at the seat's first turn with 3 untapped mana sources (the policy
// lands every drop and activates greedily within its own main phases), the
// votes follow immediately, so a game that has not resolved its vote within
// 30 turns never will -- the cap keeps a broken card from burning the
// suite.
const (
	voteGameMaxTurns   = 30
	voteGameMaxIntents = 12000
)

// voteAsk is one posed vote decision and the answer its seat gave, recorded
// through bench.Hooks.Decision (the final submitted intent, reach overrides
// included).
type voteAsk struct {
	voter    state.PlayerID
	optKinds []string // first-occurrence Option.Kind values
	choices  []int
	votedObj state.ObjID    // when the ballot is card-shaped: the voted permanent
	votedPl  state.PlayerID // when the ballot is player-shaped: the voted player
}

// voteObs records what one vote game did.
type voteObs struct {
	spell       string
	plans       [][]string
	secretVotes int
	votes       []voteAsk
}

// ballotVotes returns obs's vote asks whose ballot is the given shape.
func ballotVotes(obs *voteObs, kind string) []voteAsk {
	out := make([]voteAsk, 0, len(obs.votes))
	for _, va := range obs.votes {
		if len(va.optKinds) > 0 && va.optKinds[0] == kind {
			out = append(out, va)
		}
	}
	return out
}

// voteHooks records every vote ask and its final answer.
func voteHooks(t *testing.T, obs *voteObs) bench.Hooks {
	return bench.Hooks{
		Decision: func(seatIdx int, d *decision.Decision, in decision.Intent, _ *botpolicy.Board) error {
			if d.Kind == decision.KChoose && d.ResumeKind == "" {
				allDiscard := len(d.Options) >= 2
				for _, o := range d.Options {
					if o.Kind != "discard" {
						allDiscard = false
						break
					}
				}
				if allDiscard {
					obs.secretVotes++
				}
			}
			if d.ResumeKind != "vote" {
				return nil
			}
			va := voteAsk{voter: d.Player, optKinds: optionKinds(d.Options), choices: append([]int(nil), in.Choices...)}
			for _, o := range d.Options {
				if len(va.choices) == 0 || o.Index != va.choices[0] {
					continue
				}
				if o.Kind == "vote_card" {
					va.votedObj = o.Obj
				}
				if o.Kind == "player" {
					va.votedPl = o.Player
				}
			}
			obs.votes = append(obs.votes, va)
			return nil
		},
	}
}

// voteDecks builds the three decks: seat 0 casts the spell, seats 1/2 feed
// it a ballot. pairs are (name, count); every deck is exactly 40 cards.
func voteDecks(t *testing.T, reg *cards.Registry, decks [][][2]any) [][]*cards.Card {
	t.Helper()
	out := make([][]*cards.Card, len(decks))
	for i, spec := range decks {
		n := 0
		for _, pair := range spec {
			name, cnt := pair[0].(string), pair[1].(int)
			card, ok := reg.Lookup(name)
			if !ok {
				t.Fatalf("vote fixture: corpus has no %q", name)
			}
			for j := 0; j < cnt; j++ {
				out[i] = append(out[i], card)
			}
			n += cnt
		}
		if n != 40 {
			t.Fatalf("vote fixture: seat %d deck is %d cards, want exactly 40", i, n)
		}
	}
	return out
}

// playVoteGame plays one vote game through TranslatingSeat (+ ReachSeat when
// reach is non-nil) with CastSeekerPolicy seats and voteHooks. It returns
// the finished engine and the recorded vote asks. census may be nil.
func playVoteGame(t *testing.T, cfg rules.Config, census *Census, reach *Reach, obs *voteObs) *rules.Engine {
	t.Helper()
	seats := make([]seat.Seat, len(cfg.Decks))
	for i := range seats {
		s := NewTranslatingSeat("vote", 1, NewFirstLegalClient(), census)
		if obs.plans != nil && i < len(obs.plans) {
			s.Pick = CastSeekerPolicy(obs.plans[i]...)
		}
		if reach != nil {
			seats[i] = NewReachSeat(s, reach, uint64(i)+1)
			continue
		}
		seats[i] = s
	}
	outcome, e, err := bench.PlayGame(cfg, seats, voteGameMaxTurns, voteGameMaxIntents, voteHooks(t, obs))
	if err != nil {
		t.Fatalf("PlayGame: %v", err)
	}
	if bench.IsAbort(outcome.StallOn) {
		t.Fatalf("engine abort (%s): %s", outcome.StallOn, outcome.Livelock)
	}
	if census != nil {
		census.AddGame()
	}
	return e
}

// notOnBattlefield reports whether the object id is absent from every
// seat's battlefield.
func notOnBattlefield(e *rules.Engine, id state.ObjID) bool {
	for _, p := range e.G.Players {
		for _, bid := range e.G.Zone(state.ZBattlefield, p.ID) {
			if bid == id {
				return false
			}
		}
	}
	return true
}

// assertVoteCensus asserts the shared invariants over one vote game's
// census: nothing unmapped, nothing rejected, and (because these votes have
// SPECIFIC prompts since MBX-7) no fallback pose either.
func assertVoteCensus(t *testing.T, census *Census) {
	t.Helper()
	if census.Games == 0 {
		t.Fatal("census recorded zero games -- the .cards corpus is not reachable in this worktree")
	}
	if n := census.TotalUnmapped(); n > 0 {
		t.Fatalf("census recorded %d unmapped decision(s): %v", n, census.Unmapped)
	}
	if n := census.TotalRejected(); n > 0 {
		t.Fatalf("census recorded %d rejected answer(s): %v", n, census.Rejected)
	}
	if n := census.TotalFallback(); n > 0 {
		t.Fatalf("census recorded %d fallback prompt(s): the vote must go through its SPECIFIC prompt, not the generic fallback: %v",
			n, census.Fallback)
	}
}

// TestVoteCouncilsJudgmentThroughWire plays Council's Judgment (the
// reported card) through TranslatingSeat + ReachSeat: the card ballot is
// mapped to chooseCards, every ballot entry is selectable, no answer is
// rejected, and the most-voted permanent is exiled.
func TestVoteCouncilsJudgmentThroughWire(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cfg := rules.Config{Seed: 1, Names: []string{"caster", "voter1", "voter2"},
		Decks: voteDecks(t, reg, [][][2]any{
			{{"Council's Judgment", 6}, {"Plains", 34}},
			{{"Grizzly Bears", 12}, {"Forest", 28}},
			{{"Grizzly Bears", 12}, {"Forest", 28}},
		}), Tokens: reg.Tokens}

	// Game A: behaviour, no reach -- the vote answers are the policy's /
	// first-legal's own, so the exile outcome is deterministic.
	censusA := NewCensus()
	obsA := &voteObs{spell: "Council's Judgment", plans: [][]string{{"Council's Judgment"}, {"Grizzly Bears"}, {"Grizzly Bears"}}}
	e := playVoteGame(t, cfg, censusA, nil, obsA)
	assertVoteCensus(t, censusA)
	t.Logf("councils-judgment (no reach): %s; secret votes=%d", censusA.Summary(), obsA.secretVotes)

	votes := ballotVotes(obsA, "vote_card")
	if len(votes) < 3 {
		t.Fatalf("only %d card-ballot vote ask(s) posed, want >= 3 (one per voter): %+v", len(votes), obsA.votes)
	}
	for i, va := range votes {
		if len(va.optKinds) != 1 || va.optKinds[0] != "vote_card" {
			t.Fatalf("vote ask %d option kinds %v, want exactly [vote_card]", i, va.optKinds)
		}
		if va.votedObj == 0 {
			t.Fatalf("vote ask %d recorded no ballot permanent", i)
		}
	}
	// Tally the actual votes and assert the card's outcome: every permanent
	// voted for the most is off the battlefield (Council's Judgment exiles
	// each nonland permanent voted for the most). Precondition for the
	// tally: each recorded votedObj was a battlefield permanent of the game
	// (known here because the ballot offered it and the engine accepted the
	// answer).
	tally := map[state.ObjID]int{}
	for _, va := range votes {
		tally[va.votedObj]++
	}
	top := 0
	for _, n := range tally {
		if n > top {
			top = n
		}
	}
	if top < 1 {
		t.Fatal("no votes tallied")
	}
	exiled := 0
	for id, n := range tally {
		if n != top {
			continue
		}
		if !notOnBattlefield(e, id) {
			t.Fatalf("permanent %d got the most votes (%d) and is still on the battlefield -- the vote did not resolve", id, n)
		}
		exiled++
	}
	if exiled == 0 {
		t.Fatal("no most-voted permanent identified")
	}
	_ = e

	// Game B: the MBX-3 reachability check over the same game.
	censusB := NewCensus()
	r := NewReach()
	obsB := &voteObs{spell: "Council's Judgment", plans: [][]string{{"Council's Judgment"}, {"Grizzly Bears"}, {"Grizzly Bears"}}}
	playVoteGame(t, cfg, censusB, r, obsB)
	assertVoteCensus(t, censusB)
	t.Logf("councils-judgment (reach): %s; reach %s", censusB.Summary(), r.Summary())
	if r.Tried["choose:vote_card"] == 0 {
		t.Fatal("reachability never swept the vote ask (choose:vote_card): tried=0")
	}
	if reason := r.ReasonFor("choose:vote_card"); reason != "" {
		t.Fatalf("the vote option kind is unreachable through the wire: %s", reason)
	}
	if keys := r.UnreachableKeys(); len(keys) != 0 {
		t.Fatalf("reachability recorded unreachable option kinds in the vote game: %v (first reason: %s)",
			keys, r.ReasonFor(keys[0]))
	}
}

// TestVoteMobVerdictThroughWire plays Mob Verdict (the second api:Vote
// shape, effPlayerVote's PLAYER ballot) through TranslatingSeat + ReachSeat:
// the player ballot maps to chooseBoardTargets, every ballot player is
// selectable, and the voted players take their Secret council damage.
func TestVoteMobVerdictThroughWire(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cfg := rules.Config{Seed: 1, Names: []string{"caster", "voter1", "voter2"},
		Decks: voteDecks(t, reg, [][][2]any{
			{{"Mob Verdict", 6}, {"Mountain", 34}},
			{{"Mountain", 40}},
			{{"Mountain", 40}},
		}), Tokens: reg.Tokens}

	// The vote itself is the only ask that matters here: seat 0 needs only
	// 2 mana for the Mob Verdict, and the ballot is players, so no fodder is
	// required; the policy seats pass everything except their own mana.
	censusA := NewCensus()
	obsA := &voteObs{spell: "Mob Verdict", plans: [][]string{{"Mob Verdict"}, {}, {}}}
	e := playVoteGame(t, cfg, censusA, nil, obsA)
	assertVoteCensus(t, censusA)
	t.Logf("mob-verdict (no reach): %s", censusA.Summary())

	votes := ballotVotes(obsA, "player")
	if len(votes) < 1 {
		t.Fatalf("no player-ballot vote ask posed (the visible ballot the chooseBoardTargets mapping serves): %+v", obsA.votes)
	}
	for i, va := range votes {
		if len(va.optKinds) != 1 || va.optKinds[0] != "player" {
			t.Fatalf("vote ask %d option kinds %v, want exactly [player]", i, va.optKinds)
		}
		if va.votedPl >= state.PlayerID(3) {
			t.Fatalf("vote ask %d recorded no voted player", i)
		}
	}
	// The three SECRET votes are posed as card-shaped picks (the hand-card
	// disguise that keeps the ballot hidden on the wire) -- assert they were
	// posed and answered too: one per voter.
	if obsA.secretVotes < 3 {
		t.Fatalf("only %d secret vote ask(s) posed, want >= 3 (one per voter)", obsA.secretVotes)
	}
	// Assert the card's outcome: every player the visible ballot voted for
	// took their Secret council damage (2 per vote, per the card), and the
	// total damage across the table is a positive multiple of 2.
	total := int32(0)
	lives := map[state.PlayerID]int32{}
	for _, p := range e.G.Players {
		total += p.Life
		lives[p.ID] = p.Life
	}
	for i, va := range votes {
		// A vote for the caster draws the caster cards (Secret council); only
		// votes for a NON-caster player deal that player 2 per vote.
		if va.votedPl == 0 {
			continue
		}
		if lives[va.votedPl] >= 20 {
			t.Fatalf("vote ask %d voted player %d, whose life %d is still the opening 20 -- the vote did not resolve", i, va.votedPl, lives[va.votedPl])
		}
		if dmg := 20 - lives[va.votedPl]; dmg%2 != 0 {
			t.Fatalf("voted player %d took %d damage, not a multiple of the card's 2-per-vote", va.votedPl, dmg)
		}
	}
	dmg := 3*20 - total
	if dmg <= 0 {
		t.Fatalf("no Secret council damage landed: total life %d, want < %d (lives %v)", total, 3*20, lives)
	}
	if dmg%2 != 0 {
		t.Fatalf("total damage %d is not a multiple of the card's 2-per-vote (lives %v)", dmg, lives)
	}
	t.Logf("mob-verdict life totals: %v", lives)

	// Game B: the MBX-3 reachability check over the same game.
	censusB := NewCensus()
	obsB := &voteObs{spell: "Mob Verdict", plans: [][]string{{"Mob Verdict"}, {}, {}}}
	r := NewReach()
	playVoteGame(t, cfg, censusB, r, obsB)
	assertVoteCensus(t, censusB)
	t.Logf("mob-verdict (reach): %s; reach %s", censusB.Summary(), r.Summary())
	if r.Tried["choose:player"] == 0 {
		t.Fatal("reachability never swept the vote ask (choose:player): tried=0")
	}
	if reason := r.ReasonFor("choose:player"); reason != "" {
		t.Fatalf("the vote option kind is unreachable through the wire: %s", reason)
	}
	if keys := r.UnreachableKeys(); len(keys) != 0 {
		t.Fatalf("reachability recorded unreachable option kinds in the vote game: %v (first reason: %s)",
			keys, r.ReasonFor(keys[0]))
	}
}
