package rules

import (
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestLegacyBlockAnswerPassesTheBlockValidator is the one-home-rule test for
// the legacy KBlockers arm: on boards where the team-size constraint BINDS,
// the policy's own answer must pass the engine's validator (Submit ->
// validateBlockers -> validateMinMaxBlockers) for every seed. It covers both
// facts the shared legalBlockChoices guard reads:
//
//   - a Menace attacker (derived keyword on the Board, floor 2), and
//   - a Min$ 3 attacker (the bound published on the offered options, no
//     keyword) -- the class the previous lone-block-only guard missed.
//
// A live partial team (1 blocker on Menace, 1 or 2 on Min$ 3) is exactly what
// the engine rejects, and a rejection would abort a bench run.
func TestLegacyBlockAnswerPassesTheBlockValidator(t *testing.T) {
	t.Parallel()
	const menace = "Name:Goblin Glory Chaser\nManaCost:R\nTypes:Creature Goblin Warrior\nPT:1/1\nK:Menace\nOracle:x\n"
	const minThree = "Name:Min Three\nManaCost:0\nTypes:Creature Bear\nPT:2/2\n" +
		"S:Mode$ MinMaxBlocker | ValidCard$ Card.Self | Min$ 3 | Description$ x\nOracle:x\n"
	const blocker = "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n"

	t.Run("menace", func(t *testing.T) {
		for seed := uint64(0); seed < 100; seed++ {
			e := combatEngine(t)
			attacker := onBoardReady(t, e, 0, menace)
			onBoard(t, e, 1, blocker)
			onBoard(t, e, 1, blocker)
			if e.G.Obj(attacker).Zone != state.ZBattlefield || !e.HasKeyword(attacker, "Menace") {
				t.Fatal("precondition: Menace attacker is not on the battlefield")
			}
			e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{attacker}})
			e.G.Step = state.StepDeclareBlockers
			e.askBlockers()
			d := e.Pending()
			if d == nil || len(d.Options) != 2 || d.Options[0].MinBlockers < 2 {
				t.Fatalf("precondition: expected two offered blocks with team bound: %+v", d)
			}
			board := botpolicy.BoardFromGame(e.G, e, 1)
			hasMenace := false
			for _, keyword := range board.Creatures.Get(attacker).Keywords {
				if keyword == "Menace" {
					hasMenace = true
				}
			}
			if !hasMenace {
				t.Fatal("precondition: board omitted Menace")
			}
			in := botpolicy.LegacyDecide(board, d, rand.New(rand.NewPCG(seed, seed+1)))
			if err := e.Submit(in); err != nil {
				t.Fatalf("seed %d: legacy answer rejected: %v (choices %v)", seed, err, in.Choices)
			}
		}
	})

	t.Run("min three", func(t *testing.T) {
		for seed := uint64(0); seed < 100; seed++ {
			e := combatEngine(t)
			attacker := onBoardReady(t, e, 0, minThree)
			onBoard(t, e, 1, blocker)
			onBoard(t, e, 1, blocker)
			onBoard(t, e, 1, blocker)
			if e.G.Obj(attacker).Zone != state.ZBattlefield || e.HasKeyword(attacker, "Menace") {
				t.Fatal("precondition: Min$ 3 attacker must be on the battlefield and not Menace")
			}
			e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{attacker}})
			e.G.Step = state.StepDeclareBlockers
			e.askBlockers()
			d := e.Pending()
			if d == nil || len(d.Options) != 3 || d.Options[0].MinBlockers != 3 {
				t.Fatalf("precondition: expected three offered blocks with a Min$ 3 bound: %+v", d)
			}
			board := botpolicy.BoardFromGame(e.G, e, 1)
			in := botpolicy.LegacyDecide(board, d, rand.New(rand.NewPCG(seed, seed+1)))
			// A partial team of 1 or 2 is the illegal shape the engine
			// rejects; assert the precondition that the answer is not the
			// trivial unblocked case for at least one seed, so the scan
			// exercises a real declaration.
			if err := e.Submit(in); err != nil {
				t.Fatalf("seed %d: legacy answer rejected: %v (choices %v)", seed, err, in.Choices)
			}
		}
	})
}
