package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestSharesColorWithOtherRemembered pins the colour twin of
// sharesCardTypeWithOther: Sphinx's Tutelage's repeat gate
// `Remembered$Valid Card.nonLand+SharesColorWithOther Remembered` counts the
// remembered (milled) nonland cards that share a colour with ANOTHER
// remembered card. Two green cards count 2 (the gate's GE2 holds); a green
// and a blue card count 0; colourless shares nothing; the candidate never
// matches itself.
func TestSharesColorWithOtherRemembered(t *testing.T) {
	g := state.NewGame([]string{"you", "them"})
	mk := func(src string) state.ObjID {
		c, d := cards.ParseBytes("t.txt", []byte(src))
		if len(d) != 0 {
			t.Fatalf("diags: %v", d)
		}
		c.Link()
		o := g.AddObject(c, 1)
		o.Zone = state.ZGraveyard
		g.SetZone(state.ZGraveyard, 1, append(g.Zone(state.ZGraveyard, 1), o.ID))
		return o.ID
	}
	bear := mk("Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	elf := mk("Name:Elf\nManaCost:G\nTypes:Creature Elf\nPT:1/1\nOracle:x\n")
	flier := mk("Name:Flier\nManaCost:1 U\nTypes:Creature Bird\nPT:1/1\nOracle:x\n")
	rock := mk("Name:Rock\nManaCost:2\nTypes:Artifact\nOracle:x\n")
	const spec = "Card.nonLand+SharesColorWithOther Remembered"
	if unk := UnknownPredicates(spec); len(unk) != 0 {
		t.Fatalf("%q reports unknown predicates %v", spec, unk)
	}
	count := func(rem ...state.ObjID) int {
		var ts []state.Target
		for _, id := range rem {
			ts = append(ts, state.Target{Obj: id})
		}
		sc := SpecContext{You: 0, Remembered: ts}
		n := 0
		for _, id := range rem {
			if MatchesSpecCtx(g, spec, id, sc) {
				n++
			}
		}
		return n
	}
	if got := count(bear, elf); got != 2 {
		t.Errorf("two green cards: %d match, want 2", got)
	}
	if got := count(bear, flier); got != 0 {
		t.Errorf("green and blue: %d match, want 0", got)
	}
	if got := count(bear, rock); got != 0 {
		t.Errorf("green and colourless: %d match, want 0", got)
	}
	if got := count(bear); got != 0 {
		t.Errorf("a lone green card: %d match, want 0 (never itself)", got)
	}
}
