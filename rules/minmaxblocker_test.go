package rules

// minmaxblocker1: S:Mode$ MinMaxBlocker, the CR 509.1a block-count
// restriction static ("can't be blocked by more than one creature" and
// "can't be blocked except by N or more creatures"). The reader is
// rules/statics.go's minMaxBlockerBounds (ValidCard$ matched against the
// ATTACKER, gates through continuousGateHolds, literal Min$/Max$ bounds);
// the whole-declaration enforcement is rules/combat.go's
// validateMinMaxBlockers (behind Submit's validateBlockers) and the
// option-list filter in askBlockers.
//
// The corpus carriers the brief names are driven as REAL compiled cards:
// Troll of Khazad-dûm (Min$ 3), Krosan Vorine (Max$ 1) and Tromokratis
// (Min$ All). The non-self scope and the Min-impossible option filter use an
// inline authored fixture, because a scoped static's own static line is rules
// expression, not a Forge card script.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// corpusBoardID finds the battlefield object whose face has this exact name.
func corpusBoardID(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	t.Fatalf("no %q on seat %d's battlefield", name, p)
	return 0
}

// minMaxBlockDecision parks the table in seat 1's declare-blockers step with
// the named attacker (seat 0) declared against seat 1, then asks for blocks.
func minMaxBlockDecision(t *testing.T, e *Engine, attacker state.ObjID) *decision.Decision {
	t.Helper()
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{attacker}})
	e.G.Step = state.StepDeclareBlockers
	e.askBlockers()
	return e.Pending()
}

// submitBlocks submits the first n offered block options and returns Submit's
// error. The engine's own cross-product list is the source, so the answer is
// exactly "n distinct offered blockers block the attacker".
func submitBlocks(t *testing.T, e *Engine, d *decision.Decision, n int) error {
	t.Helper()
	if n > len(d.Options) {
		t.Fatalf("asked for %d blockers but only %d options are offered", n, len(d.Options))
	}
	choices := make([]int, n)
	for i := range choices {
		choices[i] = d.Options[i].Index
	}
	return e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices})
}

// TestMinMaxBlockerCorpusTrollMinThree is the brief's core leaf on the real
// compiled card: Troll of Khazad-dûm's `Min$ 3` refuses a two-creature block
// (CR 509.1a: the declaration is illegal) and admits the three-creature one.
// The unblocked declaration is always legal.
func TestMinMaxBlockerCorpusTrollMinThree(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	troll := searchCorpusCard(t, reg, "Troll of Khazad-dûm")
	bear := card(t, staticBearFixture)

	for _, tc := range []struct {
		name    string
		blocks  int
		wantErr bool
	}{
		{name: "unblocked", blocks: 0},
		{name: "two blockers is illegal", blocks: 2, wantErr: true},
		{name: "three blockers is legal", blocks: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := restrictionGame(t, 7301,
				[][]*cards.Card{nil, nil},
				[][]*cards.Card{{troll}, {bear, bear, bear}})
			attacker := corpusBoardID(t, e, 0, "Troll of Khazad-dûm")
			d := minMaxBlockDecision(t, e, attacker)
			if d == nil || d.Kind != decision.KBlockers {
				t.Fatalf("no KBlockers decision: %+v", d)
			}
			if len(d.Options) != 3 {
				t.Fatalf("three bears offered %d block options, want 3: %+v", len(d.Options), d.Options)
			}
			// The CR 509.1a bound is published on every block option so a
			// rules-ignorant client (the bot policy included) can answer
			// legally: Min$ 3 rides min_blockers.
			for i, o := range d.Options {
				if o.MinBlockers != 3 || o.MaxBlockers != 0 {
					t.Fatalf("option %d bounds = (%d,%d), want (3,0): %+v", i, o.MinBlockers, o.MaxBlockers, o)
				}
			}
			beforeIntents, beforeEvents := len(e.L.Intents), len(e.L.Events)
			err := submitBlocks(t, e, d, tc.blocks)
			if tc.wantErr {
				if err == nil {
					t.Fatal("a Min$ 3 attacker was blocked by only two creatures")
				}
				if e.Pending() != d || len(e.L.Intents) != beforeIntents || len(e.L.Events) != beforeEvents {
					t.Fatal("the rejected declaration consumed the pending decision or changed the log")
				}
				return
			}
			if err != nil {
				t.Fatalf("legal declaration with %d blockers was rejected: %v", tc.blocks, err)
			}
		})
	}
}

// TestMinMaxBlockerCorpusKrosanVorineMaxOne pins the dual bound on the real
// card: Krosan Vorine's `Max$ 1` refuses two blockers and admits one.
func TestMinMaxBlockerCorpusKrosanVorineMaxOne(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	vorine := searchCorpusCard(t, reg, "Krosan Vorine")
	bear := card(t, staticBearFixture)

	t.Run("one blocker is legal", func(t *testing.T) {
		e, _ := restrictionGame(t, 7302,
			[][]*cards.Card{nil, nil},
			[][]*cards.Card{{vorine}, {bear, bear}})
		attacker := corpusBoardID(t, e, 0, "Krosan Vorine")
		d := minMaxBlockDecision(t, e, attacker)
		if d == nil || d.Kind != decision.KBlockers || len(d.Options) != 2 {
			t.Fatalf("expected two block options, got %+v", d)
		}
		if d.Options[0].MaxBlockers != 1 || d.Options[0].MinBlockers != 0 {
			t.Fatalf("Max$ 1 bounds = (%d,%d), want (0,1)", d.Options[0].MinBlockers, d.Options[0].MaxBlockers)
		}
		if err := submitBlocks(t, e, d, 1); err != nil {
			t.Fatalf("one blocker was rejected: %v", err)
		}
	})

	t.Run("two blockers is illegal", func(t *testing.T) {
		e, _ := restrictionGame(t, 7303,
			[][]*cards.Card{nil, nil},
			[][]*cards.Card{{vorine}, {bear, bear}})
		attacker := corpusBoardID(t, e, 0, "Krosan Vorine")
		d := minMaxBlockDecision(t, e, attacker)
		if d == nil || d.Kind != decision.KBlockers || len(d.Options) != 2 {
			t.Fatalf("expected two block options, got %+v", d)
		}
		before := len(e.L.Events)
		if err := submitBlocks(t, e, d, 2); err == nil {
			t.Fatal("a Max$ 1 attacker was blocked by two creatures")
		}
		if e.Pending() != d || len(e.L.Events) != before {
			t.Fatal("the rejected declaration consumed the pending decision or changed the log")
		}
	})
}

// TestMinMaxBlockerCorpusTromokratisMinAll pins Min$ All on the real card:
// Tromokratis can be blocked only if EVERY legal blocker the defending player
// controls takes part, so one of two possible bears is illegal and both are
// legal. With no lands and one bear the single-bear declaration is the whole
// legal answer.
func TestMinMaxBlockerCorpusTromokratisMinAll(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	tromokratis := searchCorpusCard(t, reg, "Tromokratis")
	bear := card(t, staticBearFixture)

	t.Run("all two bears is legal", func(t *testing.T) {
		e, _ := restrictionGame(t, 7304,
			[][]*cards.Card{nil, nil},
			[][]*cards.Card{{tromokratis}, {bear, bear}})
		attacker := corpusBoardID(t, e, 0, "Tromokratis")
		d := minMaxBlockDecision(t, e, attacker)
		if d == nil || d.Kind != decision.KBlockers || len(d.Options) != 2 {
			t.Fatalf("expected two block options, got %+v", d)
		}
		if err := submitBlocks(t, e, d, 2); err != nil {
			t.Fatalf("the all-blockers declaration was rejected: %v", err)
		}
	})

	t.Run("a partial block is illegal", func(t *testing.T) {
		e, _ := restrictionGame(t, 7305,
			[][]*cards.Card{nil, nil},
			[][]*cards.Card{{tromokratis}, {bear, bear}})
		attacker := corpusBoardID(t, e, 0, "Tromokratis")
		d := minMaxBlockDecision(t, e, attacker)
		if d == nil || d.Kind != decision.KBlockers || len(d.Options) != 2 {
			t.Fatalf("expected two block options, got %+v", d)
		}
		if err := submitBlocks(t, e, d, 1); err == nil {
			t.Fatal("Min$ All admitted a partial block")
		}
	})

	t.Run("unblocked is legal", func(t *testing.T) {
		// The restriction constrains WHO may block, never forces a block: an
		// unblocked declaration is always legal.
		e, _ := restrictionGame(t, 7307,
			[][]*cards.Card{nil, nil},
			[][]*cards.Card{{tromokratis}, {bear, bear}})
		attacker := corpusBoardID(t, e, 0, "Tromokratis")
		d := minMaxBlockDecision(t, e, attacker)
		if d == nil || d.Kind != decision.KBlockers {
			t.Fatalf("no blockers decision: %+v", d)
		}
		if err := submitBlocks(t, e, d, 0); err != nil {
			t.Fatalf("the unblocked declaration was rejected: %v", err)
		}
	})

	t.Run("a creature that cannot block makes it unblockable", func(t *testing.T) {
		// CR 509.1a / the oracle's parenthetical: if ANY creature the
		// defending player controls doesn't block it, it can't be blocked.
		// A tapped creature cannot block, so the whole declaration is
		// impossible and no pairs are offered -- not even the untapped
		// bear's.
		e, _ := restrictionGame(t, 7308,
			[][]*cards.Card{nil, nil},
			[][]*cards.Card{{tromokratis}, {bear, bear}})
		attacker := corpusBoardID(t, e, 0, "Tromokratis")
		tapped := false
		for _, id := range e.G.Zone(state.ZBattlefield, 1) {
			if !tapped {
				e.emit(events.Event{Kind: events.Tap, Obj: id})
				tapped = true
			}
		}
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{attacker}})
		e.G.Step = state.StepDeclareBlockers
		e.askBlockers()
		if d := e.Pending(); d != nil && d.Kind == decision.KBlockers {
			t.Fatalf("a Min$ All attacker with an unable creature posed a blockers decision: %+v", d)
		}
	})
}

// TestMinMaxBlockerMinImpossibleOffersNoOptions pins the askBlockers filter:
// a Min$ 3 attacker with only two legal blockers can never be legally blocked,
// so its pairs are never offered and the defender is skipped to the forced
// empty declaration (a decision nobody could answer differently is not posed).
func TestMinMaxBlockerMinImpossibleOffersNoOptions(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	troll := searchCorpusCard(t, reg, "Troll of Khazad-dûm")
	bear := card(t, staticBearFixture)
	e, _ := restrictionGame(t, 7306,
		[][]*cards.Card{nil, nil},
		[][]*cards.Card{{troll}, {bear, bear}})
	attacker := corpusBoardID(t, e, 0, "Troll of Khazad-dûm")

	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{attacker}})
	e.G.Step = state.StepDeclareBlockers
	e.askBlockers()
	if d := e.Pending(); d != nil && d.Kind == decision.KBlockers {
		t.Fatalf("an unmeetable Min$ 3 posed a blockers decision: %+v", d)
	}
	// The forced empty declaration was still recorded, so the step is
	// complete and combat can advance to damage.
	declared := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.DeclareBlockers {
			declared = true
		}
	}
	if !declared {
		t.Fatal("no empty DeclareBlockers was recorded for the unmeetable attacker")
	}
}

// TestMinMaxBlockerNonSelfScope pins that ValidCard$ is matched against the
// ATTACKER, not the static's host: a static on one permanent granting a bound
// to a different creature (the Creature.YouCtrl team shape) restricts that
// creature's block count. The lord is seat 0's; seat 0's attacking bear is
// bounded, and a seat-1 bear (not the lord's controller's) is not.
func TestMinMaxBlockerNonSelfScope(t *testing.T) {
	t.Parallel()
	const lord = "Name:MinMax Lord\nManaCost:1\nTypes:Creature Soldier\nPT:1/1\n" +
		"S:Mode$ MinMaxBlocker | ValidCard$ Creature.YouCtrl | Max$ 1 | Description$ x\nOracle:x\n"
	const bear = staticBearFixture
	e := combatEngine(t)
	onBoardReady(t, e, 0, lord)
	mine := onBoardReady(t, e, 0, bear)
	onside := onBoardReady(t, e, 1, bear)

	if _, _, _, maxOK, _ := e.minMaxBlockerBounds(mine); !maxOK {
		t.Fatal("the lord's Creature.YouCtrl bound did not reach its controller's creature")
	}
	if _, _, _, maxOK, _ := e.minMaxBlockerBounds(onside); maxOK {
		t.Fatal("the lord's Creature.YouCtrl bound reached another seat's creature")
	}

	// End to end: seat 0's bounded bear attacks seat 1, which has two bears;
	// batting two is illegal, one is legal.
	onBoard(t, e, 1, bear)
	d := minMaxBlockDecision(t, e, mine)
	if d == nil || d.Kind != decision.KBlockers || len(d.Options) != 2 {
		t.Fatalf("want two block options, got %+v", d)
	}
	if err := submitBlocks(t, e, d, 2); err == nil {
		t.Fatal("the team Max$ 1 bound admitted two blockers")
	}
	if err := submitBlocks(t, e, d, 1); err != nil {
		t.Fatalf("one blocker was rejected: %v", err)
	}
}

// TestMinMaxBlockerInlineDeclaration pins the full declaration path with an
// authored fixture (no corpus): a self Max$ 1 and a self Min$ 3 on the SAME
// attacker compose (0 or exactly [3..1] would be empty, so only 0 is legal),
// and independently the Max$ 1 refuses two blockers while one is legal.
func TestMinMaxBlockerInlineDeclaration(t *testing.T) {
	t.Parallel()
	const maxOne = "Name:Max One\nManaCost:0\nTypes:Creature Bear\nPT:2/2\n" +
		"S:Mode$ MinMaxBlocker | ValidCard$ Card.Self | Max$ 1 | Description$ x\nOracle:x\n"
	const minThree = "Name:Min Three\nManaCost:0\nTypes:Creature Bear\nPT:2/2\n" +
		"S:Mode$ MinMaxBlocker | ValidCard$ Card.Self | Min$ 3 | Description$ x\nOracle:x\n"
	const bear = staticBearFixture

	t.Run("Max 1 admits one and refuses two", func(t *testing.T) {
		e := combatEngine(t)
		attacker := onBoardReady(t, e, 0, maxOne)
		onBoard(t, e, 1, bear)
		onBoard(t, e, 1, bear)
		d := minMaxBlockDecision(t, e, attacker)
		if d == nil || len(d.Options) != 2 {
			t.Fatalf("want 2 options, got %+v", d)
		}
		if err := submitBlocks(t, e, d, 1); err != nil {
			t.Fatalf("one blocker rejected: %v", err)
		}
	})

	t.Run("Min 3 refuses one and admits three", func(t *testing.T) {
		e := combatEngine(t)
		attacker := onBoardReady(t, e, 0, minThree)
		onBoard(t, e, 1, bear)
		onBoard(t, e, 1, bear)
		onBoard(t, e, 1, bear)
		d := minMaxBlockDecision(t, e, attacker)
		if d == nil || len(d.Options) != 3 {
			t.Fatalf("want 3 options, got %+v", d)
		}
		if err := submitBlocks(t, e, d, 1); err == nil {
			t.Fatal("Min 3 admitted one blocker")
		}
		if err := submitBlocks(t, e, d, 3); err != nil {
			t.Fatalf("Min 3 rejected three blockers: %v", err)
		}
	})
}
