package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// The resume-answer fields on Ctx are one-shot: the first reader the walk
// reaches is the primitive the answer belongs to, and it must consume the
// answer so a LATER reader on the same Ctx -- the next primitive on the
// chain, or the same primitive on the next iteration of an enclosing loop --
// starts fresh instead of reading the previous one's state. effCharm, the
// per-player GenericChoice loop and effPump's KWChoice$ already consume
// Ctx.Modes (fx41/fx42); these pin the two readers that did not.

// TestRepeatOptionalContinuationIsConsumedByItsRepeat: a RepeatOptional$
// "Stop" answer re-enters the Repeat with Continue=false. Before the fix the
// field stayed on the Ctx, so the NEXT Repeat on the chain (any Repeat, gated
// or counted, since effRepeat reads the field unconditionally) read the same
// stop and ran no iteration.
func TestRepeatOptionalContinuationIsConsumedByItsRepeat(t *testing.T) {
	first, second := 0, 0
	Register("TestOneShotFirstBody", func(Host, *Ctx, *cards.SA) { first++ })
	Register("TestOneShotSecondBody", func(Host, *Ctx, *cards.SA) { second++ })
	t.Cleanup(func() { unregister("TestOneShotFirstBody", "TestOneShotSecondBody") })

	card := mkCard(t, "Name:T\nTypes:Sorcery\n"+
		"A:SP$ Repeat | RepeatSubAbility$ First | RepeatOptional$ True | SubAbility$ DBSecond\n"+
		"SVar:DBSecond:DB$ Repeat | RepeatSubAbility$ Second | RepeatNum$ 2\n"+
		"SVar:First:DB$ TestOneShotFirstBody\n"+
		"SVar:Second:DB$ TestOneShotSecondBody\nOracle:x\n")
	h, c := fixtureHost(t)
	SetSVars(c, card.Faces[0].SVars)
	c.RepeatResume = &RepeatContinuation{Continue: false, Next: 1}
	Resolve(h, c, card.Faces[0].Abilities[0])
	if first != 0 {
		t.Fatalf("the stopped RepeatOptional ran its body %d times, want 0", first)
	}
	if second != 2 {
		t.Fatalf("the chained RepeatNum$ 2 Repeat ran its body %d times, want 2 -- "+
			"it read the previous Repeat's stop answer", second)
	}
	if c.RepeatResume != nil {
		t.Fatalf("Ctx.RepeatResume = %+v after the walk, want consumed", c.RepeatResume)
	}
}

// TestVillainousChoiceVictimCursorIsConsumed: effVillainousChoice derives its
// victims only while Ctx.VillainousVictims is nil and never reset the cursor
// once every victim had chosen, so a second VillainousChoice on the same Ctx
// (here, the next iteration of a Repeat) found the exhausted cursor and asked
// nobody. The per-player GenericChoice loop already resets its cursor.
func TestVillainousChoiceVictimCursorIsConsumed(t *testing.T) {
	chosen := 0
	Register("TestOneShotVillainBody", func(Host, *Ctx, *cards.SA) { chosen++ })
	t.Cleanup(func() { unregister("TestOneShotVillainBody") })

	card := mkCard(t, "Name:T\nTypes:Sorcery\n"+
		"A:SP$ Repeat | RepeatSubAbility$ DBVillain | RepeatNum$ 2\n"+
		"SVar:DBVillain:DB$ VillainousChoice | Defined$ Opponent | Choices$ VA,VB\n"+
		"SVar:VA:DB$ TestOneShotVillainBody | SpellDescription$ a\n"+
		"SVar:VB:DB$ TestOneShotVillainBody | SpellDescription$ b\nOracle:x\n")
	h, c := fixtureHost(t)
	SetSVars(c, card.Faces[0].SVars)
	Resolve(h, c, card.Faces[0].Abilities[0])
	if h.askCount != 2 {
		t.Fatalf("the opponent faced %d villainous choices, want 2 (one per iteration)", h.askCount)
	}
	if chosen != 2 {
		t.Fatalf("chosen bodies ran %d times, want 2", chosen)
	}
}
