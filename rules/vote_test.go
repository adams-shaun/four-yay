package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
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

// resolveVote puts the synthetic vote spell on the stack and resolves it
// with the given per-voter answers (voter order = AliveFrom(0) order: the
// caster first), then pops the spent spell off the stack so the drain below
// sees only the queued Vote trigger. The caster is seat 0.
func resolveVote(t *testing.T, e *Engine, votes []int) {
	t.Helper()
	resolveVoteBy(t, e, 0, votes)
}

// resolveVoteBy is resolveVote with an explicit caster (the spell's
// controller). It matters for trig:Vote: Defined$ Player resolves voters in
// AliveFrom(caster) order, and the same/diff referents must anchor on the
// CARRIER CONTROLLER, not this caster -- the ordinary multiplayer case is an
// opponent casting the vote while you control Erestor/Model of Unity/Grudge
// Keeper.
func resolveVoteBy(t *testing.T, e *Engine, caster state.PlayerID, votes []int) {
	t.Helper()
	vc := voteSpell(t)
	src := e.G.AddObject(vc, caster)
	src.Zone = state.ZStack
	e.G.SetZone(state.ZStack, caster, []state.ObjID{src.ID})
	ctx := &effects.Ctx{Source: src.ID, Controller: caster,
		SVars: vc.Faces[0].SVars, Votes: votes}
	effects.Resolve(e, ctx, vc.Faces[0].Abilities[0])
	e.emit(events.Event{Kind: events.MoveZone, Obj: src.ID,
		From: state.ZStack, To: state.ZGraveyard})
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

// TestErestorVoteFinishedTreasureScryAndDraw is trig:Vote's end-to-end leaf
// on the real corpus card: seat 0's Erestor of the Council on the
// battlefield, a three-player vote answered [0,1,0] (the caster and seat 2
// voted AChoice, seat 1 voted BChoice). The carrier fires the trigger once;
// TrigTreasure gives exactly the like-voting opponent (seat 2) one Treasure
// (TokenOwner$ TriggeredOpponentVotedSame), DBScry's ScryNum$ X reads
// SVar:X:TriggeredPlayersOpponentVotedDiff$Amount = 1 (one KArrange option),
// and the chained DBDraw draws for the trigger's controller. The vote
// outcome's own draw (the winner body) makes it two Draw events total.
// Control: an all-same vote [0,0,0] discriminates -- a Treasure under EACH
// same-voting opponent, an empty diff set so scry 0 poses no KArrange
// (AskEmpty), and the same two draws.
func TestErestorVoteFinishedTreasureScryAndDraw(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	run := func(votes []int, wantSame [3]int, wantScry int) {
		e, _ := voteCarrierEngine(t, reg, "Erestor of the Council")
		carrier := enterCarrier(t, e, "Erestor of the Council")
		before := drawsFor(e, 0)
		resolveVote(t, e, votes)
		scry := drainVoteTrigger(t, e, carrier)
		for p, want := range wantSame {
			if got := tokensNamed(e, state.PlayerID(p), "Treasure"); got != want {
				t.Fatalf("votes %v: seat %d has %d Treasures, want %d", votes, p, got, want)
			}
		}
		if wantScry == 0 {
			if scry != nil {
				t.Fatalf("votes %v: scry 0 posed an ask (%+v), want none (AskEmpty)", votes, scry)
			}
		} else {
			if scry == nil {
				t.Fatalf("votes %v: no scry ask, want KArrange over %d card(s)", votes, wantScry)
			}
			if len(scry.Options) != wantScry || scry.Player != 0 {
				t.Fatalf("votes %v: scry ask = %+v, want %d option(s) for seat 0 (X = the diff count)",
					votes, scry, wantScry)
			}
		}
		if got := drawsFor(e, 0) - before; got != 2 {
			t.Fatalf("votes %v: seat 0 drew %d, want 2 (the vote outcome's draw + the trigger's DBDraw)", votes, got)
		}
		if got := voteFinishedNotes(e); got != 1 {
			t.Fatalf("votes %v: %d canonical vote-finished Notes, want 1", votes, got)
		}
	}
	run([]int{0, 1, 0}, [3]int{0, 0, 1}, 1)
	run([]int{0, 0, 0}, [3]int{0, 1, 1}, 0)
}

// TestModelOfUnityScrysTheLikeVotingOpponent is the second carrier: Model of
// Unity's DB$ Scry | Defined$ TriggeredOpponentVotedSame & You | ScryNum$ 2 |
// Optional$ True. With the vote split [0,1,0] the same set holds seat 2, so
// the first scry election and arrange ask go to seat 2 (two options).
// Control: an all-diff vote [0,1,1] empties the same set, so the first ask
// goes to seat 0 (You), still two options -- the compound referent's You
// half is exact.
func TestModelOfUnityScrysTheLikeVotingOpponent(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	run := func(votes []int, wantPlayer state.PlayerID) {
		e, _ := voteCarrierEngine(t, reg, "Model of Unity")
		carrier := enterCarrier(t, e, "Model of Unity")
		resolveVote(t, e, votes)
		scry := drainVoteTrigger(t, e, carrier)
		if scry == nil {
			t.Fatalf("votes %v: no scry ask, want KArrange for seat %d", votes, wantPlayer)
		}
		if scry.Player != wantPlayer || len(scry.Options) != 2 {
			t.Fatalf("votes %v: scry ask = %+v, want 2 options for seat %d",
				votes, scry, wantPlayer)
		}
	}
	run([]int{0, 1, 0}, 2)
	run([]int{0, 1, 1}, 0)
}

// TestGrudgeKeeperDiffVotersLoseLife is the diff-only carrier: Grudge
// Keeper's DB$ LoseLife | Defined$ TriggeredOpponentVotedDiff | LifeAmount$ 2
// takes exactly 2 life from each diff-voting opponent. Main [0,1,0]: seat 1
// only. Control [0,0,0] (all same): the diff set is empty and nobody loses
// life.
func TestGrudgeKeeperDiffVotersLoseLife(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	run := func(votes []int, wantLoss bool) {
		e, _ := voteCarrierEngine(t, reg, "Grudge Keeper")
		carrier := enterCarrier(t, e, "Grudge Keeper")
		before := [3]int32{}
		for i := range e.G.Players {
			before[i] = e.G.Players[i].Life
		}
		resolveVote(t, e, votes)
		drainVoteTrigger(t, e, carrier)
		for i := range e.G.Players {
			lost := before[i] - e.G.Players[i].Life
			want := int32(0)
			if wantLoss && i == 1 {
				want = 2
			}
			if lost != want {
				t.Fatalf("votes %v: seat %d lost %d life, want %d", votes, i, lost, want)
			}
		}
	}
	run([]int{0, 1, 0}, true)
	run([]int{0, 0, 0}, false)
}

// TestErestorVoteReferentsAnchorOnCarrierController is the review's MAJOR
// regression: the same/diff sets are relative to the TRIGGER SOURCE'S
// CONTROLLER, never the vote caster. Erestor is controlled by seat 0 while
// seat 1 casts the vote (the ordinary multiplayer case). Voter order is
// AliveFrom(1) = [seat1, seat2, seat0]; Ctx.Votes = [1,0,1] means seat 1 and
// Erestor's controller (seat 0) voted option 1 and seat 2 voted option 0.
// The carrier controller's ballot (option 1) anchors the split: same = [seat1]
// and diff = [seat2]. So exactly ONE Treasure is minted under seat 1 and the
// scry reads X = 1. The caster-relative bug minted the Treasure under seat 0
// (the controller, not an opponent) and none for the real like-voting
// opponent, so this leaf FAILS against it. Control: an all-option-1 vote
// makes both opponents same (two Treasures, no scry).
func TestErestorVoteReferentsAnchorOnCarrierController(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	run := func(votes []int, wantTreasure [3]int, wantScry int) {
		e, _ := voteCarrierEngine(t, reg, "Erestor of the Council")
		carrier := enterCarrier(t, e, "Erestor of the Council")
		resolveVoteBy(t, e, 1, votes)
		scry := drainVoteTrigger(t, e, carrier)
		for p, want := range wantTreasure {
			if got := tokensNamed(e, state.PlayerID(p), "Treasure"); got != want {
				t.Fatalf("votes %v: seat %d has %d Treasures, want %d", votes, p, got, want)
			}
		}
		if wantScry == 0 {
			if scry != nil {
				t.Fatalf("votes %v: scry 0 posed an ask, want none", votes)
			}
		} else if scry == nil || len(scry.Options) != wantScry || scry.Player != 0 {
			t.Fatalf("votes %v: scry ask = %+v, want %d option(s) for seat 0 (the diff count)",
				votes, scry, wantScry)
		}
	}
	// seat1 and the controller (seat0) voted 1, seat2 voted 0: same=[seat1],
	// diff=[seat2].
	run([]int{1, 0, 1}, [3]int{0, 1, 0}, 1)
	// All voted option 1: same=[seat1, seat2], diff=[] -- a Treasure under each.
	run([]int{1, 1, 1}, [3]int{0, 1, 1}, 0)
}

// TestGrudgeKeeperVoteReferentsAnchorOnCarrierController is the second
// carrier under the same caster != controller shape: seat 1 casts, Grudge
// Keeper is seat 0's. Voter order [seat1, seat2, seat0] with Ctx.Votes
// [1,0,1]: the controller voted 1, seat 2 voted 0 -> diff=[seat2], so seat 2
// loses 2 and nobody else does. The caster-relative bug drained seat 0 (the
// controller) instead.
func TestGrudgeKeeperVoteReferentsAnchorOnCarrierController(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := voteCarrierEngine(t, reg, "Grudge Keeper")
	carrier := enterCarrier(t, e, "Grudge Keeper")
	before := [3]int32{}
	for i := range e.G.Players {
		before[i] = e.G.Players[i].Life
	}
	resolveVoteBy(t, e, 1, []int{1, 0, 1})
	drainVoteTrigger(t, e, carrier)
	for i := range e.G.Players {
		lost := before[i] - e.G.Players[i].Life
		want := int32(0)
		if i == 2 {
			want = 2
		}
		if lost != want {
			t.Fatalf("seat %d lost %d life, want %d (diff = [seat2] relative to Grudge Keeper's controller)",
				i, lost, want)
		}
	}
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
