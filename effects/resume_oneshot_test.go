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
