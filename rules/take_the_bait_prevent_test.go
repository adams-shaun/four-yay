package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Task agent-20260918T210307Z-963a7aba (filed as
// deck-gap-effect-bodyless-prevent): Take the Bait's prevention half — an
// Effect SA (SP$ Effect | ReplacementEffects$ RPrevent) whose named R: body
// is a BODYLESS Prevent$ True DamageDone line with IsCombat$ True and
// ValidTarget$ You,Planeswalker.YouCtrl. The registration machinery landed
// with task dponce1 (Selfless Squire, rules/damage_prevented_once_test.go);
// this file pins the SECOND carrier shape, which exercises three parameters
// Selfless Squire's simpler line does not: IsCombat$ (only combat damage is
// prevented), ValidTarget$ naming a PLAYER and a planeswalker clause, and
// the Effect's own cast-window riders (OpponentTurn$ True |
// ActivationPhases$ BeginCombat->EndCombat) that must not suppress the
// registration once the cast itself is legal.
//
// The fixture is the real corpus card (testutil.CorpusRegistry), cast for
// real at an opponent's BeginCombat step, and the damage is seeded the way
// replacement_damage_counter_test.go does: engine-side damaging/combatDamaging
// set around a raw Damage emit.

const takeTheBaitAggressor = "Name:Bait Aggressor\nTypes:Creature\nPT:2/2\nOracle:x\n"

// takeTheBaitWalker is an inline planeswalker fixture: the ValidTarget$
// planeswalker clause must preserve damage to it too.
const takeTheBaitWalker = "Name:Fixture Walker\nTypes:Planeswalker\nLoyalty:3\nOracle:x\n"

// takeTheBaitBoard seats 0 with the real Take the Bait plus an inline
// planeswalker, and seat 1 with an inline aggressor, and drives to seat 1's
// turn 2 BeginCombat step — the window Take the Bait's own riders require.
// It returns the engine, its config, the walker's object id and the
// aggressor's object id.
func takeTheBaitBoard(t *testing.T, reg *cards.Registry) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	spell := mustCorpusCard(t, reg, "Take the Bait")
	walkerCard := card(t, takeTheBaitWalker)
	aggressorCard := card(t, takeTheBaitAggressor)
	deck0 := append([]*cards.Card{spell, walkerCard}, mountainDeck(t, 38)...)
	deck1 := append([]*cards.Card{aggressorCard}, mountainDeck(t, 39)...)
	cfg := Config{Seed: 73, Names: []string{"a", "b"},
		Tokens: reg.Tokens,
		Decks:  [][]*cards.Card{deck0, deck1}}
	e := New(cfg)
	// Put the spell into seat 0's hand, the walker onto seat 0's battlefield
	// (Move's CR 306.5b entry grant gives it its printed loyalty 3 -- a walker
	// in hand has no loyalty, so damage to it could never be observed), and
	// the aggressor onto seat 1's battlefield, all with logged moves
	// (replayCheck reconstructs them).
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == 0 && o.Card == spell {
			e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZHand})
		}
		if o.Owner == 0 && o.Card == walkerCard {
			e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZBattlefield})
		}
		if o.Owner == 1 && o.Card == aggressorCard {
			e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZBattlefield})
			o.SummonSick = false
		}
	}
	walker := findByName(e, "Fixture Walker", 0)
	aggressor := findByName(e, "Bait Aggressor", 1)
	// Reach seat 1's BeginCombat, answering every decision along the way.
	for i := 0; i < 400; i++ {
		if e.G.Turn == 2 && e.G.Active == 1 && e.G.Step == state.StepBeginCombat {
			return e, cfg, walker, aggressor
		}
		if e.G.Over {
			t.Fatalf("game ended before reaching seat 1's BeginCombat")
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
		}
		if d == nil {
			t.Fatal("no decision pending while driving")
		}
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e)
		case decision.KChoose:
			ch := make([]int, 0, d.Min)
			for j := 0; j < int(d.Min); j++ {
				ch = append(ch, d.Options[j].Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: ch}); err != nil {
				t.Fatalf("submit choose: %v", err)
			}
		case decision.KAttackers, decision.KBlockers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		case decision.KTarget:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit target: %v", err)
			}
		default:
			// Any other decision kind (one the engine may gain at a step this
			// drive crosses) is answered with its first Min options rather
			// than failing the fixture spuriously; the assertions below are
			// what this test pins, not the drive.
			ch := make([]int, 0, d.Min)
			for j := 0; j < int(d.Min) && j < len(d.Options); j++ {
				ch = append(ch, d.Options[j].Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: ch}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		}
	}
	t.Fatal("did not reach seat 1's BeginCombat within the drive budget")
	return nil, Config{}, 0, 0
}

// fundTakeTheBait tops seat 0's pool with {2}{R}{W} and drives to seat 0's
// own priority so the pending decision offers the cast. Called while already
// parked at the combat step (a pool does not survive a step boundary): the
// active player (seat 1) is asked first, so seat 1's pass is answered to hand
// priority to seat 0.
func fundTakeTheBait(t *testing.T, e *Engine) {
	t.Helper()
	for _, sym := range []string{"C", "C", "R", "W"} {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: sym, Amount: 1})
	}
	e.priorityRound()
	for i := 0; i < 4; i++ {
		d := e.Pending()
		if d == nil || d.Player == 0 {
			return
		}
		submitPass(t, e)
	}
	t.Fatal("seat 0 never received priority at the combat step")
}

// hasActiveDamageReplacement reports whether any Effect-created DamageDone
// replacement is currently active — the registration the fix performs.
func hasActiveDamageReplacement(e *Engine) bool {
	for _, ce := range e.active() {
		if ce.ReplacementEvent == "DamageDone" {
			return true
		}
	}
	return false
}

// TestTakeTheBaitPreventsCombatDamageNotNonCombat pins the whole prevention
// contract of Take the Bait's Effect-carried bodyless Prevent$ True line:
//
//   - combat damage to seat 0 is fully prevented (life unchanged) and
//     recorded as a prevention Note;
//   - combat damage to seat 0's planeswalker is prevented too
//     (ValidTarget$'s Planeswalker.YouCtrl clause) — its loyalty is intact;
//   - NON-combat damage to seat 0 is NOT prevented (IsCombat$ True).
//
// It also asserts the fix's negative: no "continuous replacement
// unimplemented" Note, i.e. the bodyless line registered rather than falling
// to effEffect's loud-Note arm.
func TestTakeTheBaitPreventsCombatDamageNotNonCombat(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, walker, aggressor := takeTheBaitBoard(t, reg)
	fundTakeTheBait(t, e)
	bait := findByName(e, "Take the Bait", 0)
	castObj(t, e, bait)
	if hasNote(e, "continuous replacement unimplemented (RPrevent)") {
		t.Fatal("Take the Bait's bodyless Prevent$ True line fell to the unimplemented Note")
	}
	if !hasActiveDamageReplacement(e) {
		t.Fatal("no active Effect-created DamageDone replacement after Take the Bait resolved")
	}

	life0 := e.G.Players[0].Life
	// Preconditions the planeswalker assertion depends on: the walker is on
	// the battlefield under seat 0 with its printed loyalty, and the damage
	// dealt to it (2) is less than that loyalty, so an UNPREVENTED hit would
	// visibly lower it (3 -> 1) without the zero-loyalty SBA removing it.
	w := e.G.Obj(walker)
	if w.Zone != state.ZBattlefield || w.Controller != 0 {
		t.Fatalf("walker zone=%v controller=%d, want battlefield under seat 0", w.Zone, w.Controller)
	}
	loyalty := w.Counter("LOYALTY")
	if loyalty != 3 {
		t.Fatalf("walker loyalty = %d, want its printed 3 (Move's entry grant)", loyalty)
	}

	// Combat damage to the player: prevented.
	e.damaging, e.combatDamaging = aggressor, true
	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 3})
	e.damaging, e.combatDamaging = 0, false
	if got := e.G.Players[0].Life; got != life0 {
		t.Fatalf("seat 0 life = %d, want %d: combat damage was not prevented", got, life0)
	}
	if c := countPreventionNotes(e, 3); c != 1 {
		t.Fatalf("logged %d prevention Note(s) with amount 3, want 1", c)
	}

	// Combat damage to the planeswalker: prevented.
	e.damaging, e.combatDamaging = aggressor, true
	e.emit(events.Event{Kind: events.Damage, Obj: walker, Amount: 2})
	e.damaging, e.combatDamaging = 0, false
	if got := e.G.Obj(walker).Counter("LOYALTY"); got != loyalty {
		t.Fatalf("walker loyalty = %d, want %d: combat damage to the planeswalker was not prevented", got, loyalty)
	}
	if c := countPreventionNotes(e, 2); c != 1 {
		t.Fatalf("logged %d prevention Note(s) with amount 2, want 1", c)
	}

	// Non-combat damage: NOT prevented — IsCombat$ True scopes the promise.
	e.damaging, e.combatDamaging = aggressor, false
	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 4})
	e.damaging, e.combatDamaging = 0, false
	if got := e.G.Players[0].Life; got != life0-4 {
		t.Fatalf("seat 0 life = %d, want %d: non-combat damage must not be prevented (IsCombat$ True)", got, life0-4)
	}
	if c := countPreventionNotes(e, 4); c != 0 {
		t.Fatalf("logged %d prevention Note(s) with amount 4, want 0 for non-combat damage", c)
	}

	replayCheck(t, e, cfg)
}
