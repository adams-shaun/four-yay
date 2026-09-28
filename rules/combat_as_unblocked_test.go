package rules

// asunblk1: stat:AssignCombatDamageAsUnblocked (CR 509's optional "assign as
// though it weren't blocked"). The static is collected through activeStatics
// (rules/statics.go asUnblockedStaticMatches), its controller election is
// asked per damage pass through the combat damage step's continuation state
// (rules/combat.go asUnblockedNeeding / askNextCombatAsk /
// handleAsUnblockedElection, before the division queue), and an accepted
// election routes the attacker's whole power to the defending player in
// damageStep's chosenElection case -- blockers still hit back.
//
// Every leaf pins a REAL corpus card (the same convention
// rules/combat_restriction_statics_test.go keeps) and drives the ordinary
// declare/submit paths, so the asks land in the event log as
// DecisionAsk/DecisionMade exactly like every other combat decision.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// answerElection submits the pending as-unblocked election: index 0 declines
// (assign normally), index 1 accepts (assign as though not blocked).
func answerElection(t *testing.T, e *Engine, accept bool) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the as-unblocked election KChoose, got %+v", d)
	}
	if len(d.Options) != 2 {
		t.Fatalf("election options = %d, want 2 (decline / accept): %+v", len(d.Options), d.Options)
	}
	idx := 0
	if accept {
		idx = 1
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit election: %v", err)
	}
}

// submitBlockerPairs declares (attacker, blocker) pairs explicitly -- unlike
// submitBlockersOnly, which pairs each named blocker with the first attacker
// option that offers it -- so a two-attacker fixture can control which guard
// blocks which attacker.
func submitBlockerPairs(t *testing.T, e *Engine, pairs ...[2]state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	var choices []int
	for _, pr := range pairs {
		idx := -1
		for _, o := range d.Options {
			if o.Obj == pr[1] && o.Attacker == pr[0] {
				idx = o.Index
				break
			}
		}
		if idx < 0 {
			t.Fatalf("no block option pairing attacker %d with blocker %d: %+v", pr[0], pr[1], d.Options)
		}
		choices = append(choices, idx)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		t.Fatalf("submit blocker pairs: %v", err)
	}
	drainCombatPriority(t, e)
}

// electionAskedAfter reports whether a DecisionAsk was appended to the log
// after marker (the election ask is the first decision the combat damage
// step poses once its pass begins).
func electionAskedAfter(t *testing.T, e *Engine, marker int, player state.PlayerID) bool {
	t.Helper()
	for _, ev := range e.L.Events[marker:] {
		if ev.Kind == events.DecisionAsk && ev.Player == player {
			return true
		}
	}
	return false
}

func TestAsUnblockedElectionClonesWithTheCombatPass(t *testing.T) {
	t.Parallel()
	// The election queues live in combatRound (combat.go), the combat damage
	// step's continuation state: a Clone taken while an election is pending
	// must own its own electQueue/doneElect, and the two engines must be
	// able to answer differently without touching each other.
	reg := testutil.CorpusRegistry(t)
	e := combatEngine(t)
	atk := onBoard(t, e, 0, "Name:Brute\nManaCost:3 G\nTypes:Creature Beast\nPT:3/6\nOracle:x\n")
	e.G.Obj(atk).SummonSick = false
	might := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Indomitable Might"))
	e.emit(events.Event{Kind: events.Attach, Obj: might, IDs: []state.ObjID{atk}})
	blk := onBoard(t, e, 1, "Name:Guard\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/7\nOracle:x\n")

	e.askAttackers()
	submitAttackers(t, e, atk)
	submitBlockers(t, e, blk)

	c := e.Clone()
	if len(c.combatRound.electQueue) != 1 || c.combatRound.electQueue[0] != atk {
		t.Fatalf("clone lost the pending election queue: %+v", c.combatRound.electQueue)
	}

	answerElection(t, e, true)  // original accepts
	answerElection(t, c, false) // clone declines

	if got := e.G.Players[1].Life; got != 14 {
		t.Fatalf("original defending player life = %d, want 14 (accepted)", got)
	}
	if got := e.G.Obj(blk).Damage; got != 0 {
		t.Fatalf("original blocker damage = %d, want 0", got)
	}
	if got := c.G.Players[1].Life; got != 20 {
		t.Fatalf("clone defending player life = %d, want 20 (declined)", got)
	}
	if got := c.G.Obj(blk).Damage; got != 6 {
		t.Fatalf("clone blocker damage = %d, want 6 (ordinary blocked assignment)", got)
	}
}

func TestIndomitableMightElectionDeclinedAssignsNormally(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := combatEngine(t)
	// 3/6 base: the attached Might's +3/+3 static makes the attacker 6/9, so
	// the block is observable both ways (the 2/7 guard survives a 6 hit).
	atk := onBoard(t, e, 0, "Name:Brute\nManaCost:3 G\nTypes:Creature Beast\nPT:3/6\nOracle:x\n")
	e.G.Obj(atk).SummonSick = false
	might := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Indomitable Might"))
	e.emit(events.Event{Kind: events.Attach, Obj: might, IDs: []state.ObjID{atk}})
	blk := onBoard(t, e, 1, "Name:Guard\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/7\nOracle:x\n")

	if pw := e.Power(atk); pw != 6 {
		t.Fatalf("attacker power = %d, want 6 (3 base + Might's 3)", pw)
	}

	e.askAttackers()
	submitAttackers(t, e, atk)
	marker := len(e.L.Events)
	submitBlockers(t, e, blk)

	if !electionAskedAfter(t, e, marker, 0) {
		t.Fatal("no DecisionAsk was appended for the as-unblocked election")
	}
	answerElection(t, e, false)

	// Declined: the ordinary blocked assignment -- the blocker takes the
	// power, the defending player takes nothing from the attacker, and the
	// blocker still hits back.
	if got := e.G.Obj(blk).Damage; got != 6 {
		t.Fatalf("blocker damage = %d, want 6 (the attacker's power)", got)
	}
	if got := e.G.Obj(atk).Damage; got != 2 {
		t.Fatalf("attacker damage = %d, want 2 (the blocker hit back)", got)
	}
	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("defending player life = %d, want 20 (an election is not a Trample)", got)
	}
	// The answer is in the log too.
	found := false
	for _, ev := range e.L.Events[marker:] {
		if ev.Kind == events.DecisionMade && ev.Player == 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("no DecisionMade was appended for the election answer")
	}
}

func TestIndomitableMightElectionAcceptedAssignsAsUnblocked(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := combatEngine(t)
	// Lifelink on the attacker so the lifelink attribution check has
	// something to bite on: an accepted election's player assignment keeps
	// the attacker's controller as the lifelink source.
	atk := onBoard(t, e, 0, "Name:Brute\nManaCost:3 G\nTypes:Creature Beast\nPT:3/6\nK:Lifelink\nOracle:x\n")
	e.G.Obj(atk).SummonSick = false
	might := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Indomitable Might"))
	e.emit(events.Event{Kind: events.Attach, Obj: might, IDs: []state.ObjID{atk}})
	blk := onBoard(t, e, 1, "Name:Guard\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/7\nOracle:x\n")

	e.askAttackers()
	submitAttackers(t, e, atk)
	marker := len(e.L.Events)
	submitBlockers(t, e, blk)

	if !electionAskedAfter(t, e, marker, 0) {
		t.Fatal("no DecisionAsk was appended for the as-unblocked election")
	}
	answerElection(t, e, true)

	// Accepted: the defending player takes the full power, the blocker takes
	// NOTHING from the attacker, lifelink still attributed from the
	// attacker's controller, and the blocker still hits back.
	if got := e.G.Players[1].Life; got != 14 {
		t.Fatalf("defending player life = %d, want 14 (20 - the attacker's 6)", got)
	}
	if got := e.G.Players[0].Life; got != 26 {
		t.Fatalf("attacking player life = %d, want 26 (20 + 6 lifelink; the blocker's hit back damages the creature, not the player)", got)
	}
	if got := e.G.Obj(blk).Damage; got != 0 {
		t.Fatalf("blocker damage = %d, want 0 (the election rerouted the attacker's damage)", got)
	}
	if got := e.G.Obj(atk).Damage; got != 2 {
		t.Fatalf("attacker damage = %d, want 2 (the blocker hit back)", got)
	}
}

func TestRhoxSelfStaticMayAssignAsUnblocked(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := combatEngine(t)
	rhox := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Rhox"))
	e.G.Obj(rhox).SummonSick = false
	blk := onBoard(t, e, 1, "Name:Guard\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/7\nOracle:x\n")

	e.askAttackers()
	submitAttackers(t, e, rhox)
	marker := len(e.L.Events)
	submitBlockers(t, e, blk)

	if !electionAskedAfter(t, e, marker, 0) {
		t.Fatal("no DecisionAsk was appended for Rhox's as-unblocked election")
	}
	answerElection(t, e, true)

	if got := e.G.Players[1].Life; got != 15 {
		t.Fatalf("defending player life = %d, want 15 (20 - Rhox's 5)", got)
	}
	if got := e.G.Obj(blk).Damage; got != 0 {
		t.Fatalf("blocker damage = %d, want 0", got)
	}
	if got := e.G.Obj(rhox).Damage; got != 2 {
		t.Fatalf("Rhox damage = %d, want 2 (the blocker hit back)", got)
	}
}

func TestSiegeBehemothElectionGatedOnAttacking(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	t.Run("attacking: every controlled attacker is offered the election", func(t *testing.T) {
		e := combatEngine(t)
		behemoth := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Siege Behemoth"))
		e.G.Obj(behemoth).SummonSick = false
		friend := onBoard(t, e, 0, "Name:Pal\nManaCost:2 G\nTypes:Creature Beast\nPT:3/4\nOracle:x\n")
		e.G.Obj(friend).SummonSick = false
		guard1 := onBoard(t, e, 1, "Name:Guard1\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/8\nOracle:x\n")
		guard2 := onBoard(t, e, 1, "Name:Guard2\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/8\nOracle:x\n")

		e.askAttackers()
		submitAttackers(t, e, behemoth, friend)
		marker := len(e.L.Events)
		submitBlockerPairs(t, e, [2]state.ObjID{behemoth, guard1}, [2]state.ObjID{friend, guard2})

		// Two elections pending, one at a time: the Behemoth itself (its
		// ValidCard$ Creature.YouCtrl admits itself) and the friend.
		for i, id := range []state.ObjID{behemoth, friend} {
			d := e.Pending()
			if d == nil || d.Kind != decision.KChoose {
				t.Fatalf("election %d: expected the as-unblocked election KChoose, got %+v", i, d)
			}
			if d.Source != id {
				t.Fatalf("election %d is for attacker %d, want %d", i, d.Source, id)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{1}}); err != nil {
				t.Fatalf("election %d: submit: %v", i, err)
			}
		}
		if !electionAskedAfter(t, e, marker, 0) {
			t.Fatal("no DecisionAsk was appended for the as-unblocked elections")
		}

		// Both accepted: the defending player takes 7 + 3, the blockers take
		// nothing, and both blockers still hit back.
		if got := e.G.Players[1].Life; got != 10 {
			t.Fatalf("defending player life = %d, want 10 (20 - 7 - 3)", got)
		}
		for _, id := range []state.ObjID{guard1, guard2} {
			if got := e.G.Obj(id).Damage; got != 0 {
				t.Fatalf("blocker %d damage = %d, want 0", id, got)
			}
		}
		if got := e.G.Obj(behemoth).Damage; got != 2 {
			t.Fatalf("behemoth damage = %d, want 2 (its blocker hit back)", got)
		}
		if got := e.G.Obj(friend).Damage; got != 2 {
			t.Fatalf("friend damage = %d, want 2 (its blocker hit back)", got)
		}
	})

	t.Run("not attacking: another attacker gets no election", func(t *testing.T) {
		e := combatEngine(t)
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Siege Behemoth")) // present, NOT attacking
		friend := onBoard(t, e, 0, "Name:Pal\nManaCost:2 G\nTypes:Creature Beast\nPT:3/4\nOracle:x\n")
		e.G.Obj(friend).SummonSick = false
		blk := onBoard(t, e, 1, "Name:Guard\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/8\nOracle:x\n")

		e.askAttackers()
		submitAttackers(t, e, friend)
		submitBlockers(t, e, blk)

		// The IsPresent$ Card.Self+attacking gate fails (the Behemoth stayed
		// home), so no election is posed: the friend assigns normally and the
		// damage step runs straight through.
		if got := e.G.Obj(blk).Damage; got != 3 {
			t.Fatalf("blocker damage = %d, want 3 (the ordinary blocked assignment)", got)
		}
		if got := e.G.Obj(friend).Damage; got != 2 {
			t.Fatalf("friend damage = %d, want 2 (the blocker hit back)", got)
		}
		if got := e.G.Players[1].Life; got != 20 {
			t.Fatalf("defending player life = %d, want 20", got)
		}
	})
}
