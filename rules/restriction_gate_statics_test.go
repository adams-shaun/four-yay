package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// These pins deliberately use the real face statics and change the objects
// the shared IsPresent$/PresentCompare$ grammar counts.
func TestVeteranBrawlers(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	v := onBoardCard(t, e, 0, corpusCard(t, "Veteran Brawlers"))
	blocker := onBoard(t, e, 0, "Name:Attacker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	land := onBoard(t, e, 0, "Name:Untapped Land\nTypes:Land\nPT:0/0\nOracle:x\n")
	e.G.Obj(v).SummonSick = false
	e.G.Obj(blocker).SummonSick = false
	if e.G.Obj(v).Zone != state.ZBattlefield || e.G.Obj(land).Zone != state.ZBattlefield {
		t.Fatal("fixture did not put Veteran Brawlers and land on battlefield")
	}
	if !e.blockRestricted(v, blocker) {
		t.Fatal("Veteran Brawlers was blockable with an untapped land")
	}
	e.G.Obj(land).Tapped = true
	if e.blockRestricted(v, blocker) {
		t.Fatal("Veteran Brawlers remained unable to block without an untapped land")
	}
}

func TestVortexRunner(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	runner := onBoardCard(t, e, 0, corpusCard(t, "Vortex Runner"))
	blocker := onBoard(t, e, 1, "Name:Blocker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	for i := 0; i < 7; i++ {
		onBoard(t, e, 0, "Name:Runner Land\nTypes:Land\nPT:0/0\nOracle:x\n")
	}
	if e.blockRestricted(blocker, runner) {
		t.Fatal("Vortex Runner was unblockable below eight lands")
	}
	onBoard(t, e, 0, "Name:Eighth Runner Land\nTypes:Land\nPT:0/0\nOracle:x\n")
	e.staticEpoch = -1
	if !e.blockRestricted(blocker, runner) {
		t.Fatal("Vortex Runner was blockable at eight lands")
	}
}

// TestMaraudingMaulhorn drives the real corpus gate end to end: Marauding
// Maulhorn's `S:Mode$ MustAttack | ... | IsPresent$
// Card.namedAdvocate of the Beast+YouCtrl | PresentCompare$ EQ0` is a
// requirement ONLY while its controller controls no Advocate of the Beast
// (the Advocate is the card's own printed release valve). Both leaves go
// through the NORMAL attacker offer/validator path -- restrictionGame parks
// the table at turn-2 Main1, driveToStep reaches seat 0's KAttackers
// decision, attackerOptionsFor reads the offered pairs, and the declaration
// is submitted the way a seat would -- with the fixture's replay check at
// the end of each leaf (every fixture mutation rode an event).
func TestMaraudingMaulhorn(t *testing.T) {
	t.Parallel()
	t.Run("no advocate: required", func(t *testing.T) {
		maulhorn := corpusCard(t, "Marauding Maulhorn")
		e, cfg := restrictionGame(t, 6112,
			[][]*cards.Card{nil, nil, nil},
			[][]*cards.Card{{maulhorn}, nil, nil})
		m := bearOnBoard(t, e, 0, maulhorn)
		if e.G.Obj(m).Zone != state.ZBattlefield {
			t.Fatal("precondition: Marauding Maulhorn is not on the battlefield")
		}
		if !e.mustAttackRequired(m) {
			t.Fatal("Marauding Maulhorn was not required to attack with no Advocate controlled")
		}
		driveToStep(t, e, 2, 0, state.StepDeclareAttackers)
		opts := attackerOptionsFor(e, m)
		if len(opts) == 0 {
			t.Fatal("precondition: the Maulhorn was offered no attack pair")
		}
		for _, o := range opts {
			if !o.Required {
				t.Fatalf("attack pair %+v was offered NOT required with no Advocate on the board", o)
			}
		}
		// Declaring one maximal pair is legal (CR 508.1d): submit the first.
		d := e.Pending()
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opts[0].Index}}); err != nil {
			t.Fatalf("required-attacker declaration rejected: %v", err)
		}
		replayCheck(t, e, cfg)
	})
	t.Run("advocate present: not required", func(t *testing.T) {
		maulhorn := corpusCard(t, "Marauding Maulhorn")
		advocate := corpusCard(t, "Advocate of the Beast")
		e, cfg := restrictionGame(t, 6113,
			[][]*cards.Card{nil, nil, nil},
			[][]*cards.Card{{maulhorn, advocate}, nil, nil})
		m := bearOnBoard(t, e, 0, maulhorn)
		bearOnBoard(t, e, 0, advocate) // precondition: the Advocate is on the battlefield
		if e.mustAttackRequired(m) {
			t.Fatal("Marauding Maulhorn remained required with its Advocate controlled")
		}
		driveToStep(t, e, 2, 0, state.StepDeclareAttackers)
		opts := attackerOptionsFor(e, m)
		if len(opts) == 0 {
			t.Fatal("precondition: the Maulhorn was offered no attack pair")
		}
		for _, o := range opts {
			if o.Required {
				t.Fatalf("attack pair %+v was marked Required with the Advocate on the board", o)
			}
		}
		// The gate-false board must also ACCEPT an empty declaration.
		d := e.Pending()
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
			t.Fatalf("empty declaration rejected although the Advocate released the Maulhorn: %v", err)
		}
		replayCheck(t, e, cfg)
	})
}

func TestBlizzard(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusCard(t, "Blizzard"))
	blizzard := e.G.Zone(state.ZHand, 0)[0]
	addMana(t, e, 0, "GGGG")
	if castOptionNamed(e, blizzard) != nil {
		t.Fatal("Blizzard was offered without a snow land")
	}
	onBoard(t, e, 0, "Name:Snow Land\nTypes:Snow Land\nPT:0/0\nOracle:x\n")
	e.staticEpoch = -1
	e.priorityRound()
	if castOptionNamed(e, blizzard) == nil {
		t.Fatal("Blizzard was not offered with a snow land")
	}
}
