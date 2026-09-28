// RememberCounteredCMC$ (task counter-cmc): a Counter that carries the rider
// remembers the countered spell's mana VALUE, and the chained SubAbility$
// reads it back through SVar:X:Count$RememberedNumber -- Electrosiphon's
// "Counter target spell. You get an amount of {E} (energy counters) equal to
// its mana value." Before the fix the rider was unread, so the X read the
// list-length channel and every carrier paid out zero.
//
// The filing card is the real corpus card, resolved end to end through the
// engine's own cast/target/resolve path against an opponent spell on the
// stack.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// beastSrc is the CMC-5 opponent spell the counter eats. Its mana value is
// the whole point of the test, so the precondition asserts the printed cost.
const beast5Src = "Name:Big Beast\nManaCost:3 G G\nTypes:Creature Beast\nPT:5/5\nOracle:x\n"

// putOppSpellOnStack mints a spell object of c controlled by seat 1 on the
// stack, the same direct-state licence validStackFixture takes.
func putOppSpellOnStack(t *testing.T, e *Engine, c *cards.Card) state.ObjID {
	t.Helper()
	o := e.G.AddObject(c, 1)
	o.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 1, []state.ObjID{o.ID})
	return o.ID
}

// castElectrosiphonAt casts seat 0's Electrosiphon from hand at the named
// stack spell through the engine's real cast -> target ask -> resolve path,
// and returns after the counter resolved.
func castElectrosiphonAt(t *testing.T, e *Engine, siphon, target state.ObjID) {
	t.Helper()
	e.G.Players[0].Pool[state.MU] = 2
	e.G.Players[0].Pool[state.MR] = 1
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, siphon))
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target decision after casting Electrosiphon: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == target {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("the stack spell was not offerable as the counter target: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passOnce(t, e) // caster's priority back (CR 117.3c)
	passOnce(t, e) // seat 1 passes; the counter resolves
}

// TestElectrosiphonEnergyEqualsManaValue is the filing test: countering the
// CMC-5 spell gives five energy counters, and countering a CMC-2 spell gives
// two -- the remembered NUMBER is the mana value, not a count of remembered
// entries.
func TestElectrosiphonEnergyEqualsManaValue(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		spellSrc string
		wantMV   int32
		wantE    int32
	}{
		{"cmc 5", beast5Src, 5, 5},
		{"cmc 2", "Name:Small Beast\nManaCost:G G\nTypes:Creature Beast\nPT:2/2\nOracle:x\n", 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			siphon := tokenReplCorpusCard(t, "Electrosiphon")
			if got := siphon.Faces[0].Cmc(); got != 3 {
				t.Fatalf("precondition: Electrosiphon's own mana value is %d, want 3 (U U R)", got)
			}
			e := counterHands(t, []*cards.Card{siphon}, nil, nil, nil)
			siphonID := handObj(t, e, 0, "Electrosiphon")
			spell := putOppSpellOnStack(t, e, card(t, tc.spellSrc))
			if got := e.G.Obj(spell).Face().Cmc(); got != tc.wantMV {
				t.Fatalf("precondition: the countered spell's mana value is %d, want %d", got, tc.wantMV)
			}
			castElectrosiphonAt(t, e, siphonID, spell)

			// Precondition: the Counter really resolved on this target -- the
			// countered MoveZone is the handler's own record.
			if o := e.G.Obj(spell); o.Zone != state.ZGraveyard {
				t.Fatalf("countered spell went to %s, want graveyard", o.Zone)
			}
			countered := false
			for _, ev := range e.L.Events {
				if ev.Kind == events.MoveZone && ev.Obj == spell &&
					ev.From == state.ZStack && ev.To == state.ZGraveyard && ev.Text == "countered" {
					countered = true
				}
			}
			if !countered {
				t.Fatal("no countered MoveZone Stack->Graveyard: the Counter never ran on the target")
			}
			if got := e.G.Players[0].Counter("ENERGY"); got != tc.wantE {
				t.Fatalf("countering the %d-mana-value spell gave %d energy, want %d (RememberCounteredCMC$ must remember the mana value)", tc.wantMV, got, tc.wantE)
			}
			if got := e.G.Players[1].Counter("ENERGY"); got != 0 {
				t.Fatalf("opponent gained %d energy, want 0 (leak)", got)
			}
		})
	}
}
