package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// trig:Vote carriers (Erestor of the Council, Model of Unity, Grudge
// Keeper) under the kernel. The vote's ballots are no longer seeded through
// a Ctx seam: every voter is posed its real private "vote" ask and answered
// with the scenario's pick.

// kr9ResolveVote is kr9ResolveVoteBy with seat 0 casting.
func kr9ResolveVote(t *testing.T, e *Engine, votes []int) {
	t.Helper()
	kr9ResolveVoteBy(t, e, 0, votes)
}

// kr9ResolveVoteBy puts the synthetic vote spell on the stack under caster,
// resolves it as a kernel probe, answers voter i's ballot ask (voters in
// AliveFrom(caster) order) with option votes[i], then moves the spent spell
// off the stack so the drain sees only the queued Vote trigger.
func kr9ResolveVoteBy(t *testing.T, e *Engine, caster state.PlayerID, votes []int) {
	t.Helper()
	vc := voteSpell(t)
	src := e.G.AddObject(vc, caster)
	src.Zone = state.ZStack
	e.G.SetZone(state.ZStack, caster, []state.ObjID{src.ID})
	sa := vc.Faces[0].Abilities[0]
	kr9Probe(e, func() {
		effects.Resolve(e, &effects.Ctx{Source: src.ID, Controller: caster, SVars: vc.Faces[0].SVars}, sa)
	})
	kr9AnswerBallots(t, e, caster, votes)
	e.emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZStack, To: state.ZGraveyard})
}

// kr9AnswerBallots answers the posed per-voter "vote" asks in order: voter i
// (AliveFrom(first)) takes option votes[i]; a negative entry submits the
// empty (UpTo$) ballot.
func kr9AnswerBallots(t *testing.T, e *Engine, first state.PlayerID, votes []int) {
	t.Helper()
	voters := e.G.AliveFrom(first)
	for i, v := range votes {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "vote" {
			t.Fatalf("voter %d: pending = %+v, want the vote ask", i, d)
		}
		if d.Player != voters[i] {
			t.Fatalf("voter %d: ask is seat %d's, want seat %d's (AliveFrom(%d) order)", i, d.Player, voters[i], first)
		}
		if v < 0 {
			submitChoices(t, e)
			continue
		}
		submitChoices(t, e, d.Options[v].Index)
	}
	if d := e.Pending(); d != nil && d.ResumeKind == "vote" {
		t.Fatalf("a vote ask is still pending after %d ballots: %+v", len(votes), d)
	}
}

func TestErestorVoteFinishedTreasureScryAndDrawKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	run := func(votes []int, wantSame [3]int, wantScry int) {
		e, _ := voteCarrierEngine(t, reg, "Erestor of the Council")
		carrier := enterCarrier(t, e, "Erestor of the Council")
		before := drawsFor(e, 0)
		kr9ResolveVote(t, e, votes)
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

func TestModelOfUnityScrysTheLikeVotingOpponentKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	run := func(votes []int, wantPlayer state.PlayerID) {
		e, _ := voteCarrierEngine(t, reg, "Model of Unity")
		carrier := enterCarrier(t, e, "Model of Unity")
		kr9ResolveVote(t, e, votes)
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

func TestGrudgeKeeperDiffVotersLoseLifeKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	run := func(votes []int, wantLoss bool) {
		e, _ := voteCarrierEngine(t, reg, "Grudge Keeper")
		carrier := enterCarrier(t, e, "Grudge Keeper")
		before := [3]int32{}
		for i := range e.G.Players {
			before[i] = e.G.Players[i].Life
		}
		kr9ResolveVote(t, e, votes)
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

func TestErestorVoteReferentsAnchorOnCarrierControllerKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	run := func(votes []int, wantTreasure [3]int, wantScry int) {
		e, _ := voteCarrierEngine(t, reg, "Erestor of the Council")
		carrier := enterCarrier(t, e, "Erestor of the Council")
		kr9ResolveVoteBy(t, e, 1, votes)
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

func TestGrudgeKeeperVoteReferentsAnchorOnCarrierControllerKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := voteCarrierEngine(t, reg, "Grudge Keeper")
	carrier := enterCarrier(t, e, "Grudge Keeper")
	before := [3]int32{}
	for i := range e.G.Players {
		before[i] = e.G.Players[i].Life
	}
	kr9ResolveVoteBy(t, e, 1, []int{1, 0, 1})
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
