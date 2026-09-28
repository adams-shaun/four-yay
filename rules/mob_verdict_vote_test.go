package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// mobVerdictVoteNotes counts the deferred "votes for" reveal Notes emitted
// so far (the player ballot defers them until every voter has answered).
func mobVerdictVoteNotes(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.HasPrefix(ev.Text, "votes for ") {
			n++
		}
	}
	return n
}

// mobVerdictVoteMove moves the corpus card named by `name` from seat p's hand
// or library to zone `to` (search_library_test.go's helper is seat-0 only, and
// this scenario needs seat 1's creature for Mob Verdict's "each creature that
// player controls" half).
func mobVerdictVoteMove(t *testing.T, e *Engine, p state.PlayerID, name string, to state.Zone) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, p) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == name {
				if z != to {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
				}
				e.pending = nil
				e.priorityRound()
				return id
			}
		}
	}
	t.Fatalf("corpus card %q absent from seat %d hand/library", name, p)
	return 0
}

// mobVerdictEngine builds a four-seat table whose seat 0 holds Mob Verdict and
// whose seat 1 has an Ancient Brontodon (9/9, so it survives the votes and its
// damage counters are observable). Every seat starts on the same pinned seed
// with seat 0 the CR 103.1 toss winner, so the caster and the voter order are
// fixed.
func mobVerdictEngine(t *testing.T, reg *cards.Registry) (*Engine, Config) {
	t.Helper()
	mv := searchCorpusCard(t, reg, "Mob Verdict")
	brontodon := searchCorpusCard(t, reg, "Ancient Brontodon")
	mountain := searchCorpusCard(t, reg, "Mountain")
	makeDeck := func(first *cards.Card) []*cards.Card {
		deck := []*cards.Card{first}
		for len(deck) < 40 {
			deck = append(deck, mountain)
		}
		return deck
	}
	cfg := seatZeroStart(Config{Seed: 7711, Names: []string{"a", "b", "c", "d"},
		Decks:  [][]*cards.Card{makeDeck(mv), makeDeck(brontodon), makeDeck(mountain), makeDeck(mountain)},
		Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	// Seat 1's Ancient Brontodon is the "each creature that player controls"
	// half of the damage clause; seat 0's own creatures (there are none) are
	// the control for the ValidCards$ scoping.
	mobVerdictVoteMove(t, e, 1, "Ancient Brontodon", state.ZBattlefield)
	return e, cfg
}

// TestMobVerdictPlayerBallotDamageAndDrawPerVote is the end-to-end pin for
// api:Vote's PLAYER ballot (task votepb1): SP$ Vote | Defined$ Player |
// Secret$ True | VotePlayer$ Other | StoreVoteNum$ True | SubAbility$
// DBRepeatOpp, whose two AmountFromVotes$ RepeatEach bodies read the per-player
// tally (SVar$Votes/Times.2 damage, NumCards$ Votes draw).
//
// At a four-seat table each seat votes once, secretly and in voter order from
// the caster:
//
//	seat 0 -> seat 1, seat 1 -> seat 0, seat 2 -> seat 1, seat 3 -> seat 1
//
// so seat 1 receives three votes and seat 0 one, seats 2 and 3 none. The card
// must then deal 2 damage per vote received (seat 1 -6, seat 0/2/3 unchanged)
// to that player and each creature they control (the Brontodon carries exactly
// 6), and seat 0 must draw one card for its single vote. Before this the
// player ballot recorded a bare "votes for " Note per voter and resolved
// nothing, so no damage or draw happened at all.
func TestMobVerdictPlayerBallotDamageAndDrawPerVote(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := mobVerdictEngine(t, reg)

	mv := mobVerdictVoteMove(t, e, 0, "Mob Verdict", state.ZHand)
	addMana(t, e, 0, "RRRR")
	d := castFixture(t, e, mv, -1)

	life1 := e.G.Players[1].Life
	brontodon := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZBattlefield, 1) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Ancient Brontodon" {
			brontodon = id
		}
	}
	if brontodon == 0 {
		t.Fatal("seat 1's Ancient Brontodon is not on the battlefield")
	}
	hand0 := len(e.G.Zone(state.ZHand, 0))

	// The ballot is `Other`: every option is a seat, and the voter themselves
	// is never offered. Voter order is AliveFrom(seat 0): 0, 1, 2, 3.
	wantVoters := []state.PlayerID{0, 1, 2, 3}
	// Each voter's chosen ballot entry, as an option index into its own offer
	// list (which is universe minus the voter):
	//   seat 0 options [1,2,3] -> 0 names seat 1
	//   seat 1 options [0,2,3] -> 0 names seat 0
	//   seat 2 options [0,1,3] -> 1 names seat 1
	//   seat 3 options [0,1,3] -> 1 names seat 1
	wantPick := []int{0, 0, 1, 1}
	for i, voter := range wantVoters {
		if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "vote" {
			t.Fatalf("voter %d: pending = %+v, want a vote KChoose", i, d)
		}
		if d.Player != voter {
			t.Fatalf("ask %d went to seat %d, want voter %d", i, d.Player, voter)
		}
		if d.Min != 1 || d.Max != 1 {
			t.Fatalf("ask %d range = %d..%d, want 1..1", i, d.Min, d.Max)
		}
		for _, o := range d.Options {
			if o.Kind != "player" {
				t.Fatalf("ask %d option kind = %q, want \"player\"", i, o.Kind)
			}
			if o.Player == voter {
				t.Fatalf("ask %d offered the voter (seat %d) their own seat", i, voter)
			}
		}
		if len(d.Options) != 3 {
			t.Fatalf("ask %d offered %d options, want the 3 other seats", i, len(d.Options))
		}
		// The ballot is SECRET: only the voter's own projected view carries
		// the pending decision -- another seat sees nothing, and the vote
		// content itself stays hidden by construction (deferred Notes,
		// asserted below). The omniscient spectator carries a read-only COPY
		// of the pending ask since the spectator-decision contract
		// (approx row 33) -- it shows the ballot's shape, never a vote.
		// The option labels are the ballot entries' F3-safe seat identities.
		for _, o := range d.Options {
			if o.Label != e.G.Players[o.Player].Name {
				t.Fatalf("ask %d option for seat %d label = %q, want the deck identity %q",
					i, o.Player, o.Label, e.G.Players[o.Player].Name)
			}
		}
		if other := view.Project(e.G, e, (voter+1)%4, d); other.Decision != nil {
			t.Fatalf("voter %d's secret ballot leaked to seat %d: %+v", i, (voter+1)%4, other.Decision)
		}
		if omniscient := view.ProjectFor(e.G, e, view.NoSeat, view.Omniscient, d); omniscient.Decision == nil ||
			len(omniscient.Decision.Options) != len(d.Options) {
			t.Fatalf("voter %d's pending ballot missing from the omniscient spectator's read-only copy: %+v", i, omniscient.Decision)
		}
		submitChoices(t, e, d.Options[wantPick[i]].Index)
		// The reveal is DEFERRED: no "votes for" Note exists until the LAST
		// voter has answered, then all four land at once.
		wantNotes := 0
		if i == len(wantPick)-1 {
			wantNotes = len(wantPick)
		}
		if got := mobVerdictVoteNotes(e); got != wantNotes {
			t.Fatalf("after voter %d: %d reveal Notes, want %d", i, got, wantNotes)
		}
		d = e.Pending()
	}

	// The fourth answer completed the ballot synchronously: DBRepeatOpp then
	// DBRepeatYou ran inside the same Submit, so the whole card has resolved
	// and priority has returned -- no further driving is needed (and any
	// extra pass would only hide a chain that ran late).
	if d != nil && d.Kind != decision.KPriority {
		t.Fatalf("after the fourth vote: pending = %+v, want priority", d)
	}

	// Damage per vote received: 3 votes on seat 1 -> 6 damage, and the
	// Brontodon (a creature seat 1 controls) takes the same 6.
	if got := e.G.Players[1].Life; got != life1-6 {
		t.Fatalf("seat 1 life = %d, want %d (2 damage per vote x 3 votes)", got, life1-6)
	}
	if o := e.G.Obj(brontodon); o == nil || o.Damage != 6 {
		t.Fatalf("seat 1's Brontodon = %+v, want 6 damage from 3 votes", o)
	}
	// Seats 2 and 3 received no votes, so they take nothing from DBRepeatOpp;
	// seat 0 drew instead (below), never damage.
	for _, p := range []state.PlayerID{0, 2, 3} {
		if got := e.G.Players[p].Life; got != 20 {
			t.Fatalf("seat %d life = %d, want untouched 20 (no votes received)", p, got)
		}
	}
	// Draw per vote received: seat 0's single vote draws exactly one card
	// (DBRepeatYou on the controller's own tally).
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand0+1 {
		t.Fatalf("seat 0 hand = %d cards, want %d (one draw for one vote)", got, hand0+1)
	}
	replayCheck(t, e, cfg)
}
