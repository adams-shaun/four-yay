package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Issue agent-20260918T234402Z-b031583b: `Defined$ Player.<state-qualifier>`
// at resolution time resolved to NOBODY. A compound state-qualifier spelling
// such as Player.controlsCreature.powerGE4_GE1 or Player.lifeEQ13 fell through
// effects/context.go's definedSpec catch-all to the chosen-targets fallback
// (nil for these effects), so "each player who controls a creature with power
// 4 or greater draws a card" drew nothing and "each player with exactly 13
// life loses the game" lost nobody. The fix bridges the resolution-time path
// to the trigger-side player grammar (effects.MatchesPlayerSpecFrom) and
// extends that grammar with the controlsCreature./controlsPermanent.
// qualifier families.
//
// Every carrier here is a REAL corpus card in NO repo deck and NO legacy
// golden deck (re-grepped at test time by the ratchet and TestHeads gates),
// so no chain head depends on the fixture seeds below. Creature fixtures are
// freely-authored card text (never corpus .txt, per the licensing rule).

// qualifierEngine seeds seat 0's opening hand with hand (corpus card names in
// order) followed by Forests, opponent all Mountains, and drives to seat 0's
// turn-1 Main1.
func qualifierEngine(t *testing.T, reg *cards.Registry, hand ...string) (*Engine, Config) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	deck := make([]*cards.Card, 0, 40)
	for _, name := range hand {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	for len(deck) < 40 {
		deck = append(deck, forest)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 4477, Names: []string{"caster", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// bigCreature/smallCreature are freely-authored fixture cards for the power
// comparison; paleCat is the white creature Disorder's grammar counts.
const bigCreatureFixture = "Name:Big Brute\nManaCost:3 G\nTypes:Creature Beast\nPT:5/5\nOracle:x\n"
const smallCreatureFixture = "Name:Small Fry\nManaCost:G\nTypes:Creature Beast\nPT:2/2\nOracle:x\n"
const whiteCreatureFixture = "Name:Pale Cat\nManaCost:W\nTypes:Creature Cat\nPT:2/2\nOracle:x\n"
const bearFixture = "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// castAndResolve submits the cast option for id and passes priority until
// the stack empties (the resolution completed) — the spells under test pose
// no mid-resolution ask, so the flow stays in seat 0's turn-1 Main1 and the
// state can be asserted while still there. The deck is SHUFFLED, so the
// caller must measure hand sizes only AFTER searchMoveByName has moved the
// spell into hand.
func castAndResolve(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %d: %+v", id, d.Options)
	}
	submitChoices(t, e, idx)
	resolveCast(t, e)
}

func resolveCast(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 30; i++ {
		if len(e.G.Stack) == 0 {
			return
		}
		d := e.Pending()
		if d == nil {
			t.Fatalf("no pending decision while resolving (over=%v)", e.G.Over)
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected %v decision during resolution: %+v", d.Kind, d)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	t.Fatal("stack never emptied within the pass budget")
}

// drainPriority passes every priority decision the engine asks (answering a
// crossed cleanup-step discard naively, the driveToStep convention) until the
// game ends or the budget runs out -- what lets a charm answer's resolution
// (which resumes on the next engine drive) actually run.
func drainPriority(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; i < limit && !e.G.Over; i++ {
		if answerIfDiscard(t, e) {
			continue
		}
		d := e.Pending()
		if d == nil {
			return
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected %v decision while draining: %+v", d.Kind, d)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
}

// TestDefinedPlayerStateQualifierShatterTheSky is the reported symptom: "Each
// player who controls a creature with power 4 or greater draws a card. Then
// destroy all creatures." drew for NOBODY before the fix. Seat 0 controls a
// 5/5 (draws), seat 1 a 2/2 (does not); the destroy-all half still sweeps
// every creature.
func TestDefinedPlayerStateQualifierShatterTheSky(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, _ := qualifierEngine(t, reg, "Shatter the Sky")

	big := putToken(t, e, 0, bigCreatureFixture, state.ZBattlefield)
	small := putToken(t, e, 1, smallCreatureFixture, state.ZBattlefield)

	id := searchMoveByName(t, e, "Shatter the Sky", state.ZHand)
	hand0 := len(e.G.Zone(state.ZHand, 0))
	hand1 := len(e.G.Zone(state.ZHand, 1))
	addMana(t, e, 0, "WWWW")
	castAndResolve(t, e, id)

	// Seat 0's hand: spell left, one card drawn in -- net unchanged.
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand0 {
		t.Fatalf("seat 0 hand after Shatter the Sky = %d, want %d (spell cast + one draw)", got, hand0)
	}
	// Seat 1 controls no power>=4 creature: no draw.
	if got := len(e.G.Zone(state.ZHand, 1)); got != hand1 {
		t.Fatalf("seat 1 hand after Shatter the Sky = %d, want %d (no draw)", got, hand1)
	}
	// The destroy-all half still resolved: both creatures left the battlefield.
	for _, id := range []state.ObjID{big, small} {
		if o := e.G.Obj(id); o == nil || o.Zone == state.ZBattlefield {
			t.Fatalf("creature %d not destroyed (zone %+v)", id, o)
		}
	}
}

// TestDefinedPlayerStateQualifierShatterTheSkyNoQualifyingPlayer is the
// negative half: with nobody controlling a power>=4 creature the draw acts on
// nobody (and only the destroy-all resolves).
func TestDefinedPlayerStateQualifierShatterTheSkyNoQualifyingPlayer(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, _ := qualifierEngine(t, reg, "Shatter the Sky")

	putToken(t, e, 0, smallCreatureFixture, state.ZBattlefield)
	putToken(t, e, 1, smallCreatureFixture, state.ZBattlefield)

	id := searchMoveByName(t, e, "Shatter the Sky", state.ZHand)
	hand0 := len(e.G.Zone(state.ZHand, 0))
	hand1 := len(e.G.Zone(state.ZHand, 1))
	addMana(t, e, 0, "WWWW")
	castAndResolve(t, e, id)

	if got := len(e.G.Zone(state.ZHand, 0)); got != hand0-1 {
		t.Fatalf("seat 0 hand = %d, want %d (cast, NO draw)", got, hand0-1)
	}
	if got := len(e.G.Zone(state.ZHand, 1)); got != hand1 {
		t.Fatalf("seat 1 hand = %d, want %d (NO draw)", got, hand1)
	}
}

// TestDefinedPlayerStateQualifierTriskaidekaphobia pins Player.lifeEQ13 on the
// real enchantment: at seat 0's upkeep the charm is answered with the
// gain-life mode, and the seat at exactly 13 life loses the game while the
// seat at 20 does not.
func TestDefinedPlayerStateQualifierTriskaidekaphobia(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := qualifierEngine(t, reg, "Triskaidekaphobia")

	searchMoveByName(t, e, "Triskaidekaphobia", state.ZBattlefield)
	// Seat 1 to exactly 13 life; seat 0 stays at 20.
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -7})

	// Triskaidekaphobia's upkeep trigger is ValidPlayer$ You, so it fires at
	// seat 0's NEXT upkeep -- turn 3 (turn 1 seat 0, turn 2 seat 1).
	driveToStep(t, e, 3, 0, state.StepUpkeep)
	d := passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected the charm's modes ask, got %+v", d)
	}
	submitChoices(t, e, 0) // mode 0: lose, then each player gains 1 life
	drainPriority(t, e, 20)

	if !e.G.Players[1].Lost {
		t.Fatalf("seat 1 (13 life) did not lose the game")
	}
	if e.G.Players[0].Lost {
		t.Fatalf("seat 0 (20 life) lost the game; must not")
	}
}

// TestDefinedPlayerStateQualifierTriskaidekaphobiaNotAt13 is the negative
// half: no seat at exactly 13 life and nobody loses.
func TestDefinedPlayerStateQualifierTriskaidekaphobiaNotAt13(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := qualifierEngine(t, reg, "Triskaidekaphobia")

	searchMoveByName(t, e, "Triskaidekaphobia", state.ZBattlefield)
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -6}) // 14 life

	driveToStep(t, e, 3, 0, state.StepUpkeep)
	d := passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected the charm's modes ask, got %+v", d)
	}
	submitChoices(t, e, 0)
	drainPriority(t, e, 20)

	if e.G.Players[1].Lost || e.G.Players[0].Lost {
		t.Fatalf("a seat lost the game with life 20/14; nobody must (lifeEQ13 matches nobody)")
	}
}

// TestDefinedPlayerStateQualifierBondersOrnament pins controlsPermanent.
// namedBonder's Ornament on the real artifact: activating the draw ability
// draws for each player who CONTROLS a permanent named Bonder's Ornament --
// seat 0 does (draws one), seat 1 does not (hand unchanged).
func TestDefinedPlayerStateQualifierBondersOrnament(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := qualifierEngine(t, reg, "Bonder's Ornament")

	orn := searchMoveByName(t, e, "Bonder's Ornament", state.ZBattlefield)

	hand0 := len(e.G.Zone(state.ZHand, 0))
	hand1 := len(e.G.Zone(state.ZHand, 1))

	// The AB$ Draw ability is the ornament's second activated ability.
	o := e.G.Obj(orn)
	if o == nil || o.Face() == nil {
		t.Fatal("ornament missing")
	}
	idx := -1
	for i, sa := range o.Face().Abilities {
		if sa.Kind == "AB" && sa.API == "Draw" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("Bonder's Ornament has no AB$ Draw ability: %+v", o.Face().Abilities)
	}

	// Fund the {4} (the {T} taps the ornament itself) and activate.
	addMana(t, e, 0, "CCCC")
	opt := abilityOption(t, e, orn, idx)
	submitChoices(t, e, opt.Index)
	passUntilNonPriority(t, e, 20)

	if got := len(e.G.Zone(state.ZHand, 0)); got != hand0+1 {
		t.Fatalf("seat 0 hand = %d, want %d (drew one for controlling the named permanent)", got, hand0+1)
	}
	if got := len(e.G.Zone(state.ZHand, 1)); got != hand1 {
		t.Fatalf("seat 1 hand = %d, want %d (controls no Bonder's Ornament: no draw)", got, hand1)
	}
}

// TestDefinedPlayerStateQualifierDisorder is the Family-B carrier: Disorder's
// ValidPlayers$ Player.controlsCreature.White_GE1 reaches the grammar through
// damage.go's validPlayers fallback (definedSpec first, MatchesPlayerSpec
// over the living seats). Seat 0 controls a white creature (takes 2), seat 1
// does not (untouched); the white cat takes lethal damage, the bear survives.
func TestDefinedPlayerStateQualifierDisorder(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, _ := qualifierEngine(t, reg, "Disorder")

	cat := putToken(t, e, 0, whiteCreatureFixture, state.ZBattlefield)
	bear := putToken(t, e, 1, bearFixture, state.ZBattlefield)

	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life

	id := searchMoveByName(t, e, "Disorder", state.ZHand)
	addMana(t, e, 0, "RR")
	castAndResolve(t, e, id)

	if got := e.G.Players[0].Life; got != life0-2 {
		t.Fatalf("seat 0 life = %d, want %d (controls a white creature: 2 damage)", got, life0-2)
	}
	if got := e.G.Players[1].Life; got != life1 {
		t.Fatalf("seat 1 life = %d, want %d (controls no white creature)", got, life1)
	}
	if o := e.G.Obj(cat); o == nil || o.Zone == state.ZBattlefield {
		t.Fatalf("white cat should have taken lethal damage, zone=%v", o)
	}
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("non-white bear must survive Disorder, zone=%v", o)
	}
}

// TestDefinedPlayerStateQualifierDepopulate pins the MultiColor spelling on
// the real sorcery: only a seat controlling a MULTICOLORED creature draws.
func TestDefinedPlayerStateQualifierDepopulate(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, _ := qualifierEngine(t, reg, "Depopulate")

	putToken(t, e, 0, "Name:Guildmage\nManaCost:W B\nTypes:Creature Human Wizard\nPT:2/2\nOracle:x\n", state.ZBattlefield)
	putToken(t, e, 1, whiteCreatureFixture, state.ZBattlefield)

	id := searchMoveByName(t, e, "Depopulate", state.ZHand)
	hand0 := len(e.G.Zone(state.ZHand, 0))
	hand1 := len(e.G.Zone(state.ZHand, 1))
	addMana(t, e, 0, "WWWW")
	castAndResolve(t, e, id)

	if got := len(e.G.Zone(state.ZHand, 0)); got != hand0 {
		t.Fatalf("seat 0 hand = %d, want %d (multicolored creature: spell + draw)", got, hand0)
	}
	if got := len(e.G.Zone(state.ZHand, 1)); got != hand1 {
		t.Fatalf("seat 1 hand = %d, want %d (mono-white creature: no draw)", got, hand1)
	}
}
