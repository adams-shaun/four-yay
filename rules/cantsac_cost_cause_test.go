package rules

// cantsac1 r2: the COST-path cause semantics for the three cost sites r1
// left on costCauseNone, defined per the costCause table in rules/layers.go:
//
//   - a ward cost is demanded by the ward trigger (CR 702.22), so its cause
//     is costCauseTriggered;
//   - a cumulative-upkeep payment is demanded by the upkeep trigger
//     (CR 702.25a), so its Sac arm's candidate walk is costCauseTriggered --
//     and that walk consults the CantSacrifice gate at all now (the
//     cumulative-upkeep/echo Sac action path never did);
//   - an unless payment is a resolution-election payment, never a cast or
//     activation cost, so its cause is costCauseResolution, which no
//     readable ValidCause$ base admits (fail closed, permissive).
//
// Consequence for the corpus's two ForCost$ True carriers (Angel of
// Jubilation, Yasharn -- both `ValidCause$ Spell,Activated`): they scope to
// the cast/activation cost sites only and correctly do NOT block a ward,
// unless or upkeep payment. The trigger-demand enforcement itself is pinned
// by hand-built `ValidCause$ Triggered` carriers -- the corpus has none, so
// they are fixture-only, the same way staticBearFixture is.
//
// Every corpus leaf drives a REAL corpus card through the ordinary
// offer/payment paths; the ward and upkeep leaves end replay-verified (leaf
// E mirrors the existing TestUnlessCostTresserhornPaysSacLifeAndDraw drive,
// whose board is placed eventlessly, so it carries the same no-replayCheck
// shape that test has). Each leaf asserts its own precondition: the objects
// are in the zones the rule reads, the control board offers the payment the
// restricted board must change, and the unit-level discrimination asserts
// run against the carrier's REAL static params so a reverted registration
// fails loudly.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// cantsacTrigCreatureFixture blocks every creature from a cost sacrifice a
// triggered ability demands. Fixture-only: the corpus's ForCost$ True
// carriers are both `ValidCause$ Spell,Activated`.
const cantsacTrigCreatureFixture = "Name:Ward Warden\nManaCost:1 W\nTypes:Creature Human Cleric\nPT:1/1\n" +
	"S:Mode$ CantSacrifice | ValidCard$ Creature | ValidCause$ Triggered | ForCost$ True | Description$ Creatures can't be sacrificed to pay a cost a triggered ability demands.\nOracle:x\n"

// cantsacTrigLandFixture blocks every land from a cost sacrifice a triggered
// ability demands -- the Polar Kraken leaf's carrier, scoped to lands so the
// upkeep trigger's forced settle (which sacrifices the Kraken itself) is not
// contested by the static.
const cantsacTrigLandFixture = "Name:Upkeep Warden\nManaCost:1 W\nTypes:Creature Human Cleric\nPT:1/1\n" +
	"S:Mode$ CantSacrifice | ValidCard$ Land | ValidCause$ Triggered | ForCost$ True | Description$ Lands can't be sacrificed to pay a cost a triggered ability demands.\nOracle:x\n"

// cantsacPaymentCreature is the ward leaf's sacrifice-candidate fixture.
const cantsacPaymentCreature = "Name:Payment\nTypes:Creature\nPT:1/1\nOracle:x\n"

// cantsacWardGame parks a two-seat game at turn-2 Main 1 with both boards
// placed through emitted moves (so the game replays) and seat 1's targeting
// prop still in its library. The ward leaves drive the targeting spell the
// way rules/combat_keywords_test.go's ward tests do: the cause is PutOnStack
// + TargetsChosen seed events, so no cast/mana plumbing is involved and the
// ward trigger is the only machinery under test.
func cantsacWardGame(t *testing.T, seed uint64, board0, board1 []*cards.Card) (*Engine, Config) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append(append([]*cards.Card{}, board0...), mountainDeck(t, 40-len(board0))...),
			append(append([]*cards.Card{}, board1...), mountainDeck(t, 40-len(board1))...),
		}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	for seat, bs := range [][]*cards.Card{board0, board1} {
		want := make(map[*cards.Card]bool, len(bs))
		for _, c := range bs {
			want[c] = true
		}
		moved := 0
		for _, id := range append(append([]state.ObjID(nil), e.G.Zone(state.ZHand, state.PlayerID(seat))...),
			e.G.Zone(state.ZLibrary, state.PlayerID(seat))...) {
			o := e.G.Obj(id)
			if o == nil || !want[o.Card] {
				continue
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZBattlefield})
			moved++
		}
		if moved != len(bs) {
			t.Fatalf("seat %d: only %d of %d board cards dealt", seat, moved, len(bs))
		}
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	return e, cfg
}

// cantsacWardCause seeds a targeting spell from seat 1's library at the
// warded creature and resolves the queue, leaving the engine at the ward pay
// election (asserted, so a board that never asks fails loudly).
func cantsacWardCause(t *testing.T, e *Engine, warded state.ObjID) {
	t.Helper()
	cause := e.G.Zone(state.ZLibrary, 1)[0]
	e.emit(events.Event{Kind: events.PutOnStack, Obj: cause, Player: 1, From: state.ZLibrary, To: state.ZStack})
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: cause, IDs: []state.ObjID{warded}})
	e.putTriggersOnStack()
	e.resolveTop()
	if d := e.Pending(); d == nil || d.ResumeKind != "unless_pay" {
		t.Fatalf("the ward pay election did not surface: %+v", d)
	}
}

// cantsacDrainStack drives the engine after a ward payment: the ward's paid
// sacrifice kills the payment creature, whose death fires Vein Ripper's own
// drain trigger (its target ask is answered with its only option), and the
// priority passes resolve what is left of the stack.
func cantsacDrainStack(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 60; i++ {
		if len(e.G.Stack) == 0 {
			return
		}
		d := e.Pending()
		if d == nil {
			return
		}
		switch d.Kind {
		case decision.KPriority:
			passPriorityOnce(t, e)
		case decision.KTarget:
			if len(d.Options) == 0 {
				t.Fatalf("target ask with no options: %+v", d)
			}
			submitChoices(t, e, d.Options[0].Index)
		default:
			t.Fatalf("unexpected %v decision while draining the stack: %+v", d.Kind, d)
		}
	}
	t.Fatal("the stack never drained")
}

// cantsacUpkeepGame parks seat 0 at turn-2 Main 1 with the board cards on
// the battlefield through emitted moves (so the game replays).
func cantsacUpkeepGame(t *testing.T, seed uint64, board []*cards.Card) (*Engine, Config) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{append(append([]*cards.Card{}, board...), mountainDeck(t, 40-len(board))...), mountainDeck(t, 40)},
		Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	want := make(map[*cards.Card]bool, len(board))
	for _, c := range board {
		want[c] = true
	}
	moved := 0
	for _, id := range append(append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...), e.G.Zone(state.ZLibrary, 0)...) {
		o := e.G.Obj(id)
		if o == nil || !want[o.Card] {
			continue
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZBattlefield})
		moved++
	}
	if moved != len(board) {
		t.Fatalf("seat 0: only %d of %d board cards dealt", moved, len(board))
	}
	return e, cfg
}

// TestCantSacCostCauseAdmitsTable is the cause-filter regression: the
// readable ValidCause$ bases on the cost path admit exactly the cause each
// names, and nothing else -- including the two corpus carriers' shape.
func TestCantSacCostCauseAdmitsTable(t *testing.T) {
	t.Parallel()
	spellActivated := "Spell,Activated"
	if !causeCostAdmits(spellActivated, costCauseSpell) {
		t.Error("Spell,Activated must admit a spell-cast cost")
	}
	if !causeCostAdmits(spellActivated, costCauseActivated) {
		t.Error("Spell,Activated must admit an activation cost")
	}
	for _, c := range []costCause{costCauseNone, costCauseTriggered, costCauseResolution} {
		if causeCostAdmits(spellActivated, c) {
			t.Errorf("Spell,Activated must not admit cause %d", c)
		}
	}
	if !causeCostAdmits("Triggered", costCauseTriggered) {
		t.Error("Triggered must admit a trigger-demanded payment")
	}
	for _, c := range []costCause{costCauseNone, costCauseSpell, costCauseActivated, costCauseResolution} {
		if causeCostAdmits("Triggered", c) {
			t.Errorf("Triggered must not admit cause %d", c)
		}
	}
	for _, spec := range []string{"Spell.Instant", "Spell.OppCtrl", "Triggered.YouCtrl", "SpellAbility", "Ability"} {
		if causeCostAdmits(spec, costCauseSpell) || causeCostAdmits(spec, costCauseActivated) ||
			causeCostAdmits(spec, costCauseTriggered) {
			t.Errorf("qualified/unknown base %q must fail closed", spec)
		}
	}
	if causeCostAdmits("", costCauseSpell) {
		t.Error("an empty spec must fail closed")
	}
}
