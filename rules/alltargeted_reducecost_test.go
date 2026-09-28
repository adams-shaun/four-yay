// Task alltargeted1: the Count$ head AllTargeted$ was unread -- every body
// built on it evaluated to 0, so an ability whose own ReduceCost$ names such
// an SVar never discounted. This file pins the surviving end-to-end carrier
// on the real corpus card: Raft Security Officer's {2},{T} tap ability costs
// {1} when its single target has power 3 or less, and {2} when it does not.
//
// The two other AllTargeted$ corpus carriers stay dead here, for reasons the
// head registration cannot fix (recorded in AGENTS.md's Known
// approximations): Wayta, Trainer Prodigy's Count$Compare Y EQ2.2.0 needs the
// sub-ability (DB$ Fight) target pre-asked at activation, and Urgent
// Necropsy's CollectEvidence<X> cost token is unparsed by ParseCost, so its
// AllTargeted$CardManaCost body has no consumer on a cost path.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const (
	alltargetedDorkSrc  = "Name:Dork\nManaCost:G\nTypes:Creature Dork\nPT:1/1\nOracle:x\n"
	alltargetedBruteSrc = "Name:Brute\nManaCost:3 R\nTypes:Creature Ogre\nPT:4/4\nOracle:x\n"
)

// raftSecurityOfficerGame builds a 2-seat game with the real corpus Raft
// Security Officer plus a 1/1 and a 4/4 fixture creature on seat 0's
// battlefield, driven to turn 3's Main1 so the officer is past summoning
// sickness (its cost carries a T part) with a fresh priority decision.
func raftSecurityOfficerGame(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	raftCard, ok := reg.Lookup("Raft Security Officer")
	if !ok {
		t.Fatal("Raft Security Officer not found in the compiled corpus registry")
	}
	cfg := Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{raftCard, card(t, alltargetedDorkSrc), card(t, alltargetedBruteSrc)}, mountainDeck(t, 37)...),
			mountainDeck(t, 40),
		},
		Tokens: reg.Tokens,
	}
	e := New(seatZeroStart(cfg))
	e.Advance()
	raftID := findAndMoveToHand(t, e, 0, "Raft Security Officer")
	moveToBattlefield(t, e, raftID)
	dorkID := moveSeeded(t, e, 0, alltargetedDorkSrc, state.ZBattlefield)
	bruteID := moveSeeded(t, e, 0, alltargetedBruteSrc, state.ZBattlefield)
	// moveSeeded cleared the stale genesis priority ask; re-drive and pass
	// one full round so the officer is past summoning sickness (its T cost).
	e.pending = nil
	e.Advance()
	driveToStep(t, e, 3, 0, state.StepMain1)
	return e, cfg, raftID, dorkID, bruteID
}

// activateRaftTargeting floats two generic, submits the officer's ability
// option, and answers the target ask with target. After the answer the
// payment has already run (CR 601.2c before 601.2h), so the caller can read
// the charged pool directly.
func activateRaftTargeting(t *testing.T, e *Engine, raftID, target state.ObjID) {
	t.Helper()
	addMana(t, e, 0, "CC")
	opt := abilityOption(t, e, raftID, 0)
	submitChoices(t, e, opt.Index)
	answerKTarget(t, e, target)
}

func TestRaftSecurityOfficerReducesOnLowPowerTarget(t *testing.T) {
	t.Parallel()
	e, cfg, raftID, dorkID, _ := raftSecurityOfficerGame(t, 91)
	activateRaftTargeting(t, e, raftID, dorkID)
	if got := e.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("pool after activating on the 1/1 = %d, want 1 ({2} floated, {1} charged)", got)
	}
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack after activation = %d objects, want 1", len(e.G.Stack))
	}
	passUntilStackEmpty(t, e, 20)
	if !e.G.Obj(dorkID).Tapped {
		t.Fatal("the 1/1 target was not tapped")
	}
	replayCheck(t, e, cfg)
}

func TestRaftSecurityOfficerNoReductionOnHighPowerTarget(t *testing.T) {
	t.Parallel()
	e, cfg, raftID, _, bruteID := raftSecurityOfficerGame(t, 92)
	activateRaftTargeting(t, e, raftID, bruteID)
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after activating on the 4/4 = %d, want 0 (full {2} charged)", got)
	}
	passUntilStackEmpty(t, e, 20)
	if !e.G.Obj(bruteID).Tapped {
		t.Fatal("the 4/4 target was not tapped")
	}
	replayCheck(t, e, cfg)
}
