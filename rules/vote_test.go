package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// voteCarrierEngine builds a three-seat corpus game with the named vote
// trigger carrier on seat 0's battlefield (everything else Mountains), at
// Main1 of the opening turn. Three seats are the smallest table that can
// split a vote into a same AND a diff opponent: the caster plus two
// opponents.
func voteCarrierEngine(t *testing.T, reg *cards.Registry, carrier string) (*Engine, Config) {
	t.Helper()
	c, ok := reg.Lookup(carrier)
	if !ok {
		t.Fatalf("corpus fixture: %s missing", carrier)
	}
	deck := []*cards.Card{c}
	m, ok := reg.Lookup("Mountain")
	if !ok {
		t.Fatal("corpus fixture: Mountain missing")
	}
	for len(deck) < 40 {
		deck = append(deck, m)
	}
	cfg := seatZeroStart(Config{Seed: 4212, Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{deck, mountainDeck(t, 40), mountainDeck(t, 40)}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// enterCarrier moves the named carrier card from seat 0's hand to the
// battlefield and returns its object id.
func enterCarrier(t *testing.T, e *Engine, carrier string) state.ObjID {
	t.Helper()
	return moveByName(t, e, 0, carrier, state.ZBattlefield)
}

// voteSpell is the synthetic voting spell the tests resolve by hand
// (`SP$ Vote | Defined$ Player | Choices$ AChoice,BChoice`, winner body a
// plain draw): a real corpus vote spell would work, but the spell itself is
// not what is under test -- the carrier and the three triggers are. The
// deterministic stand-in would give every voter option 0, so Ctx.Votes is
// the seam (the same one TestPathOfTheAnimistTiedVoteRunsTheTiedBranch
// uses) that makes a same/diff split reachable.
func voteSpell(t *testing.T) *cards.Card {
	t.Helper()
	c, err := cards.ParseBytes("vote.txt", []byte(
		"Name:Test Vote\nTypes:Sorcery\n"+
			"A:SP$ Vote | Defined$ Player | Choices$ AChoice,BChoice\n"+
			"SVar:AChoice:DB$ Draw\n"+
			"SVar:BChoice:DB$ Draw\nOracle:x\n"))
	if err != nil {
		t.Fatalf("parse vote spell: %v", err)
	}
	c.Link()
	return c
}

// voteTriggerOnStack returns the stack object of the Vote trigger minted for
// the carrier permanent, 0 when none is on the stack.
func voteTriggerOnStack(e *Engine, carrier state.ObjID) state.ObjID {
	for _, sid := range e.G.Stack {
		o := e.G.Obj(sid)
		if o == nil || o.Ability == nil || o.Source != carrier {
			continue
		}
		if _, isTrig := state.TriggerOf(e.G, o); isTrig {
			return sid
		}
	}
	return 0
}

// drainVoteTrigger resolves the single Mode$ Vote trigger the canonical
// vote-finished Note queued for carrierID: the trigger is not stacked until
// a priority round runs, and (target-less) it resolves only once every seat
// has passed priority over it -- the ordinary CR 117 flow, not a push-time
// resolution. The drain passes, accepts any optional scry election, answers
// the body's KArrange when ScryNum$ offers one, and stops once the trigger
// object has left the stack. Returns the first scry decision, if any.
func drainVoteTrigger(t *testing.T, e *Engine, carrierID state.ObjID) *decision.Decision {
	t.Helper()
	var scry *decision.Decision
	trig := state.ObjID(0)
	for i := 0; i < 400; i++ {
		if trig == 0 {
			trig = voteTriggerOnStack(e, carrierID)
			if trig == 0 {
				if d := e.Pending(); d == nil {
					e.priorityRound()
				} else if d.Kind == decision.KPriority {
					passOnce(t, e)
				} else {
					t.Fatalf("unexpected decision %v before the Vote trigger stacked: %+v", d.Kind, d)
				}
				continue
			}
		}
		if o := e.G.Obj(trig); o == nil || o.Zone != state.ZStack {
			return scry
		}
		d := e.Pending()
		switch {
		case d == nil:
			e.priorityRound()
		case d.Kind == decision.KTriggerOrder:
			var order []int
			for j := range d.Options {
				order = append(order, j)
			}
			submitChoices(t, e, order...)
		case d.Kind == decision.KArrange:
			if scry == nil {
				scry = d
			}
			submitChoices(t, e, 0)
		case d.Kind == decision.KChoose && d.ResumeKind == "scry_optional":
			if len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
				t.Fatalf("optional scry election = %+v, want yes/no", d)
			}
			submitChoices(t, e, 0)
		case d.Kind == decision.KPriority:
			passOnce(t, e)
		default:
			t.Fatalf("unexpected decision %v while resolving the Vote trigger: %+v", d.Kind, d)
		}
	}
	t.Fatal("vote trigger drain did not converge")
	return nil
}

// drawsFor counts the Draw events the log records for one seat.
func drawsFor(e *Engine, p state.PlayerID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Draw && ev.Player == p {
			n++
		}
	}
	return n
}

// voteFinishedNotes counts the canonical vote-finished Notes in the log.
func voteFinishedNotes(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if _, _, _, ok := effects.VoteFinishedResult(ev); ok {
			n++
		}
	}
	return n
}

// TestVoteFinishedCarrierGatedOnVoteTriggerFaces is the head-safety gate's
// own leaf, driven through REAL casts so the replay check means something: a
// Council's Judgment vote resolved with NO Mode$ Vote trigger face on any
// battlefield emits no canonical vote-finished Note -- exactly the golden
// acceptance games' shape (they resolve Council's Judgment votes with no
// carrier on the board), so recorded games that resolve votes never change.
// Erestor entering turns the carrier on, and the same vote emits exactly one
// Note whose trigger resolves (the like-voting opponent gets a Treasure).
func TestVoteFinishedCarrierGatedOnVoteTriggerFaces(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := miscHandsEngine(t, reg,
		[]string{"Council's Judgment", "Council's Judgment", "Erestor of the Council"}, nil,
		nil, []string{"Grizzly Bears", "Grizzly Bears"})
	// OFF: no Mode$ Vote face anywhere on the battlefield.
	addMana(t, e, 0, "CWW")
	judgment := miscHandObj(t, e, 0, "Council's Judgment")
	submitChoices(t, e, miscCastOption(t, e, judgment))
	passUntilStackEmpty(t, e, 30)
	if got := voteFinishedNotes(e); got != 0 {
		t.Fatalf("%d canonical vote-finished Notes with no Mode$ Vote face on the battlefield, want 0", got)
	}
	// Per-voter notes are unchanged by the carrier either way.
	votes := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.HasPrefix(ev.Text, "votes for ") {
			votes++
		}
	}
	if votes != 2 {
		t.Fatalf("%d per-voter notes, want one per voting player (2)", votes)
	}
	// ON: Erestor enters, the next vote emits exactly one carrier and the
	// trigger resolves -- the like-voting opponent (the deterministic
	// stand-in gives every voter the ballot's first option, the caster among
	// them) creates its Treasure.
	erestor := miscMoveByName(t, e, 0, "Erestor of the Council", state.ZBattlefield)
	if erestor == 0 {
		t.Fatal("Erestor not moved")
	}
	addMana(t, e, 0, "CWW")
	judgment = miscHandObj(t, e, 0, "Council's Judgment")
	submitChoices(t, e, miscCastOption(t, e, judgment))
	passUntilStackEmpty(t, e, 30)
	if got := voteFinishedNotes(e); got != 1 {
		t.Fatalf("%d canonical vote-finished Notes with Erestor on the battlefield, want 1", got)
	}
	if got := tokensNamed(e, 1, "Treasure"); got != 1 {
		t.Fatalf("seat 1 has %d Treasures, want 1 (Erestor's resolved vote trigger)", got)
	}
	replayCheck(t, e, cfg)
}

// TestGrudgeKeeperEmptyBallotDrainsNobody is the empty-ballot regression
// (review finding) end to end on a real card-ballot cast: Council's Judgment
// with NO eligible permanent on any opponent's battlefield (its VoteCard$
// filters to a nonland permanent you don't control; the opponents control
// only Mountains). Nobody could vote for anything, so both List$ sets are
// empty and Grudge Keeper's Defined$ TriggeredOpponentVotedDiff acts on
// nobody -- the trigger still fires (the canonical carrier is emitted), but
// no opponent loses life. Before the fix every voting opponent landed in the
// diff set and each drained 2.
func TestGrudgeKeeperEmptyBallotDrainsNobody(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := miscHandsEngine(t, reg,
		[]string{"Council's Judgment"}, nil,
		[]string{"Grudge Keeper"}, nil)
	addMana(t, e, 0, "CWW")
	judgment := miscHandObj(t, e, 0, "Council's Judgment")
	submitChoices(t, e, miscCastOption(t, e, judgment))
	miscPass(t, e)
	passUntilStackEmpty(t, e, 30)

	if got := voteFinishedNotes(e); got != 1 {
		t.Fatalf("%d canonical vote-finished Notes, want 1 (the trigger fires even with no ballot)", got)
	}
	for i := range e.G.Players {
		if e.G.Players[i].Life != 20 {
			t.Fatalf("seat %d life = %d after an empty-ballot vote, want 20 (diff set empty)",
				i, e.G.Players[i].Life)
		}
	}
	replayCheck(t, e, cfg)
}
