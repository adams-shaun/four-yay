package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The Optional$ True Investigate election (Will the Wise's "each opponent
// may investigate", Nick Valentine, Private Eye's "you may investigate") and
// the RememberInvestigatingPlayers$ acceptor record, pinned end to end on
// the real corpus cards. The harness is the token-replacement one
// (rules/investigate_test.go): corpus cards by name, real corpus token
// registry, logged MoveZone moves to fire the triggers.

// investigateOptDrain drains the stack, answering every may-investigate
// election through choose (the option index to submit for that decision),
// every target ask with option 0 and every priority round with a pass. It
// reports how many elections it answered, so a test can assert the feature's
// handler actually ran (an unregistered Investigate would pose nothing).
func investigateOptDrain(t *testing.T, e *Engine, choose func(d *decision.Decision) int) int {
	t.Helper()
	asked := 0
	for i := 0; i < 120 && !e.G.Over; i++ {
		d := e.Pending()
		if d != nil && d.Kind == decision.KTarget {
			submitChoices(t, e, 0)
			continue
		}
		if d != nil && d.Kind == decision.KChoose {
			if len(e.G.Stack) == 0 {
				// A choose ask outside a resolving stack is an unrelated
				// housekeeping ask (a cleanup discard): stop here, the
				// assertions read settled state.
				return asked
			}
			idx := choose(d)
			if idx < 0 {
				t.Fatalf("election with no yes/no option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit election: %v", err)
			}
			asked++
			continue
		}
		if len(e.G.Stack) == 0 {
			return asked
		}
		passPriorityOnce(t, e)
	}
	t.Fatal("drain did not settle")
	return asked
}

// yesNoIndex returns the index of d's "yes" (or "no") option.
func yesNoIndex(d *decision.Decision, yes bool) int {
	want := "no"
	if yes {
		want = "yes"
	}
	for _, o := range d.Options {
		if o.Kind == want {
			return o.Index
		}
	}
	return -1
}

// willThreeSeatGame is tokenReplGame at three seats: seat 0's deck carries
// the given cards, seats 1 and 2 are basic-mountain opponents, the token
// registry is the real corpus one.
func willThreeSeatGame(t *testing.T, seed uint64, seat0 ...*cards.Card) (*Engine, Config) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	build := func(s uint64) Config {
		return Config{Seed: s, Names: []string{"a", "b", "c"},
			Decks: [][]*cards.Card{
				append(append([]*cards.Card{}, seat0...), mountainDeck(t, 40-len(seat0))...),
				mountainDeck(t, 40),
				mountainDeck(t, 40),
			},
			Tokens: reg.Tokens,
		}
	}
	cfg := seatZeroStart(build(seed))
	e := New(cfg)
	e.Advance()
	return e, cfg
}

// TestNickValentineOptionalInvestigateCanDecline: Nick Valentine, Private
// Eye's death trigger is "you may investigate" — a real election posed to
// the controller, not the old automatic-Clue stand-in. A decline creates no
// Clue; an acceptance creates exactly one.
func TestNickValentineOptionalInvestigateCanDecline(t *testing.T) {
	t.Parallel()
	nick := tokenReplCorpusCard(t, "Nick Valentine, Private Eye")
	for _, tc := range []struct {
		name   string
		accept bool
	}{{"decline", false}, {"accept", true}} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg := tokenReplGame(t, 91, nick)
			id := moveSeededCard(t, e, 0, nick, state.ZBattlefield)
			// Precondition: Nick is a battlefield permanent the death
			// trigger reads, and no Clue exists yet.
			if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("precondition: Nick not on the battlefield: %+v", o)
			}
			if got := countTokensNamedOnSeat(t, e, 0, "Clue Token"); got != 0 {
				t.Fatalf("precondition: %d Clue(s) already on the battlefield", got)
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
			e.pending = nil
			addMana(t, e, 0, "") // a priority round flushes the queued death trigger
			asked := investigateOptDrain(t, e, func(d *decision.Decision) int {
				return yesNoIndex(d, tc.accept)
			})
			if asked != 1 {
				t.Fatalf("precondition: %d may-investigate elections were posed (want 1) — the trigger's Investigate never asked", asked)
			}
			want := 0
			if tc.accept {
				want = 1
			}
			if got := countTokensNamedOnSeat(t, e, 0, "Clue Token"); got != want {
				t.Errorf("decline=%v: seat 0 Clue tokens = %d, want %d", !tc.accept, got, want)
			}
			// The optional election emits the ordinary investigate marker only
			// on acceptance: a decline performs no investigate, so a trigger
			// reading trig:Investigated must not fire for it.
			if got := countEvents(e, func(ev events.Event) bool {
				return ev.Kind == events.Investigate && ev.Player == 0
			}); got != want {
				t.Errorf("decline=%v: seat 0 events.Investigate markers = %d, want %d", !tc.accept, got, want)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestWillTheWiseOptionalInvestigateRemembersOnlyAcceptors: each opponent is
// posed their own election; seat 1 accepts, seat 2 declines. The decliner
// (and only the decliner) loses 1 life — the DBLoseLife sub-ability reads
// Opponent.!IsRemembered — and the controller investigates one plus the
// number of acceptors (PlayerCountRemembered$Amount), so exactly 2 Clues on
// seat 0, 1 on the accepting opponent, none on the decliner.
func TestWillTheWiseOptionalInvestigateRemembersOnlyAcceptors(t *testing.T) {
	t.Parallel()
	will := tokenReplCorpusCard(t, "Will the Wise")
	e, cfg := willThreeSeatGame(t, 92, will)
	id := moveSeededCard(t, e, 0, will, state.ZBattlefield)
	// Precondition: Will the Wise is a battlefield permanent the ETB
	// trigger reads, both opponents alive at equal life, no Clue anywhere.
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Will the Wise not on the battlefield: %+v", o)
	}
	for _, p := range []state.PlayerID{1, 2} {
		if e.G.Players[p].Lost {
			t.Fatalf("precondition: opponent seat %d already lost", p)
		}
	}
	if e.G.Players[1].Life != e.G.Players[2].Life {
		t.Fatalf("precondition: opponents start at unequal life (%d vs %d)",
			e.G.Players[1].Life, e.G.Players[2].Life)
	}
	for p := state.PlayerID(0); p < 3; p++ {
		if got := countTokensNamedOnSeat(t, e, p, "Clue Token"); got != 0 {
			t.Fatalf("precondition: seat %d already holds %d Clue(s)", p, got)
		}
	}
	addMana(t, e, 0, "") // a priority round flushes the queued ETB trigger
	pre1, pre2 := e.G.Players[1].Life, e.G.Players[2].Life
	// Seat 1's election accepts, seat 2's declines — by seat, not by order.
	asked := investigateOptDrain(t, e, func(d *decision.Decision) int {
		return yesNoIndex(d, d.Player == 1)
	})
	if asked != 2 {
		t.Fatalf("precondition: %d may-investigate elections were posed (want 2, one per opponent) — the trigger's Investigate never asked", asked)
	}
	if got := countTokensNamedOnSeat(t, e, 1, "Clue Token"); got != 1 {
		t.Errorf("accepting opponent's Clue tokens = %d, want 1", got)
	}
	if got := countTokensNamedOnSeat(t, e, 2, "Clue Token"); got != 0 {
		t.Errorf("declining opponent's Clue tokens = %d, want 0", got)
	}
	if got := countTokensNamedOnSeat(t, e, 0, "Clue Token"); got != 2 {
		t.Errorf("controller's Clue tokens = %d, want 2 (one plus the 1 acceptor)", got)
	}
	// The investigate marker follows each accepted election: exactly one for
	// the accepting opponent (seat 1), none for the decliner (seat 2).
	if got := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.Investigate && ev.Player == 1
	}); got != 1 {
		t.Errorf("accepting opponent's events.Investigate markers = %d, want 1", got)
	}
	if got := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.Investigate && ev.Player == 2
	}); got != 0 {
		t.Errorf("declining opponent's events.Investigate markers = %d, want 0", got)
	}
	life1, life2 := e.G.Players[1].Life, e.G.Players[2].Life
	if life1 != pre1 || life2 != pre2-1 {
		t.Errorf("life after the trigger: acceptor %d decliner %d, want acceptor unchanged (%d) and decliner exactly 1 behind", life1, life2, pre1)
	}
	replayCheck(t, e, cfg)
}
