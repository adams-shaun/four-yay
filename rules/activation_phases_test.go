package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// activation_phases_test.go pins the ActivationPhases$ cast/activation
// window restriction (task actphase1) and its rider qualifiers:
// PlayerTurn$, OpponentTurn$, ActivationFirstCombat$. The gate is a pure
// offer-time read (activationPhasesOK, rules/activation_phases.go)
// consulted through spellTimingOK (every way a card is cast) and the AB$
// offer loop / mana-ability gate (every way an ability is activated).
//
// The Illusionist's Gambit and Berserk tests run the REAL corpus cards
// (searchTestRegistry) — no Forge script text is committed; the small
// fixture cards are authored inline.

const actPhaseAllyBearSrc = "Name:Ally Bear\nManaCost:G\nTypes:Creature Bear\nPT:1/1\nOracle:x\n"

const actPhaseUpkeepProbeSrc = "Name:Upkeep Probe\nManaCost:2 G\nTypes:Creature Bear\nPT:2/2\n" +
	"A:AB$ Pump | Cost$ 1 | ValidTgts$ Creature.YouCtrl | NumAtt$ +1 | ActivationPhases$ Upkeep | PlayerTurn$ True | SpellDescription$ probe\n" +
	"Oracle:x\n"

const actPhaseNonsenseProbeSrc = "Name:Window Probe\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\n" +
	"A:SP$ Pump | ValidTgts$ Creature | NumAtt$ +1 | ActivationPhases$ Nonsense | SpellDescription$ x\n" +
	"A:AB$ Pump | Cost$ 1 | ValidTgts$ Creature.YouCtrl | NumAtt$ +1 | ActivationPhases$ Nonsense | ActivationFirstCombat$ Sometimes | SpellDescription$ x\n" +
	"Oracle:x\n"

// activationDeck builds a seat-zero-start engine whose seat-0 deck leads
// with s0's cards and whose seat-1 deck leads with s1's, both padded to 40
// with Mountains; the opponent plays Mountains only.
func activationDeck(t *testing.T, seed uint64, s0, s1 []*cards.Card) (*Engine, Config) {
	t.Helper()
	for len(s0) < 40 {
		s0 = append(s0, mountainDeck(t, 1)...)
	}
	for len(s1) < 40 {
		s1 = append(s1, mountainDeck(t, 1)...)
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"caster", "opponent"},
		Decks: [][]*cards.Card{s0, s1}, Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// driveToBlockersPriority passes priority decisions (declining every attack
// declaration the way a human declines) until seat p holds priority in the
// declare-blockers step, which is the step both combat pins read. It fails
// on any other decision kind rather than guess.
func driveToBlockersPriority(t *testing.T, e *Engine, p state.PlayerID) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if answerIfDiscard(t, e) {
			continue
		}
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision pending")
		}
		if d.Kind == decision.KPriority && d.Player == p && e.G.Step == state.StepDeclareBlockers {
			return
		}
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e)
		case decision.KAttackers:
			submitAttackersOnly(t, e)
		default:
			t.Fatalf("unexpected %s decision at step %s: %+v", d.Kind, e.G.Step, d)
		}
	}
	t.Fatalf("did not reach seat %d priority in the declare-blockers step", p)
}

// abilityFor returns seat p's ability option for the object id, or nil when
// the pending priority decision does not offer it.
func abilityFor(t *testing.T, e *Engine, p state.PlayerID, id state.ObjID) *decision.Option {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != p {
		t.Fatalf("not at seat %d priority: %+v", p, d)
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == id {
			return &o
		}
	}
	return nil
}

// TestIllusionistsGambitCastableOnlyOnDeclareBlockers is the real corpus
// carrier: `ActivationPhases$ Declare Blockers | OpponentTurn$ True`. The
// Gambit is NOT offered at Main1 of the opponent's turn (the right turn,
// the wrong step — the phase gate is what withholds it) and IS offered on
// the opponent-turn declare-blockers step. The bear attacker exists because
// an attack-less combat never reaches the declare-blockers step at all (the
// engine skips it with no attackers), so the pin needs a live attack to
// drive to. Offer-only: the Gambit's RemoveFromCombat resolution is
// unrelated machinery this pin does not drive.
func TestIllusionistsGambitCastableOnlyOnDeclareBlockers(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	gambit := searchCorpusCard(t, reg, "Illusionist's Gambit")
	forest := searchCorpusCard(t, reg, "Forest")
	e, cfg := activationDeck(t, 9311, []*cards.Card{gambit, forest, forest, forest, forest, forest, forest, forest},
		[]*cards.Card{card(t, actPhaseAllyBearSrc), mountainDeck(t, 1)[0]})
	searchMoveByName(t, e, "Illusionist's Gambit", state.ZHand)
	bear := putCreature(t, e, 1, actPhaseAllyBearSrc)
	fundPool(t, e, "CCUU") // re-asks priority after the seeded move

	// Opponent's turn (seat 1 active), Main1: right turn, wrong step.
	driveToStep(t, e, 2, 1, state.StepMain1)
	submitPass(t, e) // seat 1 passes; seat 0 holds priority at the same step
	fundPool(t, e, "CCUU")
	if castByName(t, e, 0, "Illusionist's Gambit") != nil {
		t.Fatal("the Gambit was offered at the opponent's Main1 -- the Declare Blockers window did not bind")
	}

	// The same opponent turn, attacking, at the declare-blockers step.
	driveToStep(t, e, 2, 1, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, bear)
	driveToBlockersPriority(t, e, 0)
	fundPool(t, e, "CCUU")
	if e.G.Step != state.StepDeclareBlockers || e.G.Active != 1 {
		t.Fatalf("expected the opponent's declare-blockers step, got step %s active %d", e.G.Step, e.G.Active)
	}
	if castByName(t, e, 0, "Illusionist's Gambit") == nil {
		t.Fatalf("the Gambit was not offered on the opponent-turn declare-blockers step: %+v", e.Pending())
	}
	replayCheck(t, e, cfg)
}

// TestBerserkFirstCombatOnly pins ActivationFirstCombat$ on the real corpus
// card (Berserk: `Upkeep->Declare Blockers | ActivationFirstCombat$ True`):
// offered in the turn's FIRST combat, withheld once a second combat has
// begun (Aurelia's own attack trigger grants one). Offer-only.
func TestBerserkFirstCombatOnly(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := activationDeck(t, 9312,
		[]*cards.Card{lookup(t, reg, "Aurelia, the Warleader"), lookup(t, reg, "Berserk"),
			mountainDeck(t, 1)[0], mountainDeck(t, 1)[0], mountainDeck(t, 1)[0]},
		mountainDeck(t, 40))
	aurelia := moveByName(t, e, 0, "Aurelia, the Warleader", state.ZBattlefield)
	searchMoveByName(t, e, "Berserk", state.ZHand)

	// Aurelia has haste, so turn 1 runs a real combat.
	driveToStep(t, e, e.G.Turn, 0, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, aurelia)
	driveToBlockersPriority(t, e, 0)
	fundPool(t, e, "G")
	if got := e.G.CombatsThisTurn; got != 1 {
		t.Fatalf("CombatsThisTurn = %d, want 1 in the first combat", got)
	}
	if castByName(t, e, 0, "Berserk") == nil {
		t.Fatalf("Berserk was not offered in the first combat's declare-blockers step: %+v", e.Pending())
	}

	// Pass through combat 1 (Aurelia's attack trigger grants an extra
	// combat) into combat 2, where ActivationFirstCombat$ withholds.
	passAll(t, e, 200)
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected the extra combat's attackers ask, got %+v (step %s)", d, e.G.Step)
	}
	if got := e.G.CombatsThisTurn; got != 2 {
		t.Fatalf("CombatsThisTurn = %d, want 2 in the extra combat", got)
	}
	submitAttackersOnly(t, e)
	// An attack-less combat never reaches the declare-blockers step, so the
	// second-combat withhold is read at the CR 508.2 priority window right
	// after the empty declaration — still inside Berserk's
	// Upkeep->Declare Blockers window, which is the point.
	fundPool(t, e, "G")
	if e.G.Step != state.StepDeclareAttackers || e.G.CombatsThisTurn != 2 {
		t.Fatalf("expected the extra combat's declare-attackers window (combats 2), got step %s combats %d", e.G.Step, e.G.CombatsThisTurn)
	}
	if castByName(t, e, 0, "Berserk") != nil {
		t.Fatal("Berserk was offered in the SECOND combat -- ActivationFirstCombat$ did not bind")
	}
	replayCheck(t, e, cfg)
}

// TestActivationPhasesUpkeepAbilityOfferedOnlyInUpkeep pins the AB$ half
// with a synthetic: `ActivationPhases$ Upkeep | PlayerTurn$ True` is offered
// on the controller's own upkeep and withheld at Main1 of the same turn.
func TestActivationPhasesUpkeepAbilityOfferedOnlyInUpkeep(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 9313, actPhaseUpkeepProbeSrc)
	probe := putCreature(t, e, 0, actPhaseUpkeepProbeSrc)
	fundPool(t, e, "C") // re-asks priority after the seeded move

	// Seat 0's next own upkeep is turn 3 (turn 2 is the opponent's).
	driveToStep(t, e, 3, 0, state.StepUpkeep)
	fundPool(t, e, "C")
	fundPool(t, e, "C")
	if abilityFor(t, e, 0, probe) == nil {
		t.Fatalf("the Upkeep-window ability was not offered in the upkeep: %+v", e.Pending())
	}

	driveToStep(t, e, 3, 0, state.StepMain1)
	fundPool(t, e, "C")
	if abilityFor(t, e, 0, probe) != nil {
		t.Fatal("the Upkeep-window ability was offered at Main1")
	}
	replayCheck(t, e, cfg)
}

// TestActivationPhasesUnresolvableSpecWithheld pins the fail-closed
// direction: a spec naming no engine step withholds the cast AND the
// ability entirely (never widened), and a rider whose value is not "True"
// does the same.
func TestActivationPhasesUnresolvableSpecWithheld(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 9314, actPhaseNonsenseProbeSrc)
	probe := putCreature(t, e, 0, actPhaseNonsenseProbeSrc)
	fundPool(t, e, "CC")
	if castByName(t, e, 0, "Window Probe") != nil {
		t.Fatal("the nonsense-window spell was offered at Main1 -- fail-closed did not bind")
	}
	if abilityFor(t, e, 0, probe) != nil {
		t.Fatal("the nonsense-window ability was offered at Main1 -- fail-closed did not bind")
	}
	replayCheck(t, e, cfg)
}

// silence unused-import guards if the fixtures above stop needing them.
var _ = events.ManaAdd
