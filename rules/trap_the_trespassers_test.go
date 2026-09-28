package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// trapEngine builds a two-seat table whose seat 0 holds Trap the Trespassers
// and whose seat 1 has a Grizzly Bears on the battlefield -- the single ballot
// entry Trap's `VoteCard$ Creature.YouDontCtrl` admits.
func trapEngine(t *testing.T, reg *cards.Registry) (*Engine, Config) {
	t.Helper()
	trap := searchCorpusCard(t, reg, "Trap the Trespassers")
	bears := searchCorpusCard(t, reg, "Grizzly Bears")
	mountain := searchCorpusCard(t, reg, "Mountain")
	makeDeck := func(first *cards.Card) []*cards.Card {
		deck := []*cards.Card{first}
		for len(deck) < 40 {
			deck = append(deck, mountain)
		}
		return deck
	}
	cfg := seatZeroStart(Config{Seed: 7711, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{makeDeck(trap), makeDeck(bears)},
		Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	bearsID := mobVerdictVoteMove(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	_ = bearsID
	return e, cfg
}

// trapStunCounters reads the STUN counters on the Bears.
func trapStunCounters(e *Engine, bears state.ObjID) int {
	o := e.G.Obj(bears)
	if o == nil {
		return -1
	}
	n := 0
	for _, c := range o.Counters {
		if c.Kind == "STUN" {
			n += int(c.N)
		}
	}
	return n
}

// TestTrapTheTrespassersOneStunCounterPerVote is the card-ballot pin for
// StoreVoteNum$ + RememberVotedObjects$ (report issue #5, review round 2):
// SP$ Vote | VoteCard$ Creature.YouDontCtrl | StoreVoteNum$ True |
// RememberVotedObjects$ True | SubAbility$ DBRepeatStun.
//
// Forge's StoreVoteNum branch is authoritative: the most-votes remember path
// does NOT run, so the resolution's Remembered holds exactly the voted
// objects (RememberVotedObjects$'s votes.keySet()), each ONCE. The Bears are
// the ballot's only entry, both voters vote for it = 2 votes, and
// DBRepeatStun's `DefinedCards$ Remembered` loop must therefore run its body
// ONCE, putting exactly CounterNum$ Votes = 2 stun counters on the Bears and
// tapping it. With the double append (most-votes + voted) the loop ran
// TWICE and put 4.
func TestTrapTheTrespassersOneStunCounterPerVote(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := trapEngine(t, reg)

	trap := mobVerdictVoteMove(t, e, 0, "Trap the Trespassers", state.ZHand)
	addMana(t, e, 0, "UUU")
	// The card ballot's per-player vote CHOICE is still the deterministic
	// no-ask stand-in (M4): both voters take the ballot's single entry
	// without any ask, so the spell resolves through the ordinary stack
	// drain -- the pin is on the REMEMBER/TALLY side, not on an ask.
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == trap {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Trap: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)

	bears := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZBattlefield, 1) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
			bears = id
		}
	}
	if bears == 0 {
		t.Fatal("seat 1's Grizzly Bears is not on the battlefield")
	}

	// One loop iteration over the single voted subject: exactly 2 stun
	// counters, and tapped.
	if got := trapStunCounters(e, bears); got != 2 {
		t.Fatalf("Bears stun counters = %d, want 2 (one per vote, one loop pass)", got)
	}
	if o := e.G.Obj(bears); o == nil || !o.Tapped {
		t.Fatalf("Bears = %+v, want tapped by DBTap", o)
	}
	replayCheck(t, e, cfg)
}
