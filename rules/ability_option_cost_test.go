package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestAbilityOptionCarriesItsCost: a non-mana activated ability's "ability"
// option carries its offer-time cost in Forge notation, so a client can tell
// apart a card's abilities whose labels share the card-name prefix (the
// planeswalker wheel showed "Jace, the Mind Sculptor: ..." on every button).
// The cost is the same composed cost AbilityCosts projects onto the card.
func TestAbilityOptionCarriesItsCost(t *testing.T) {
	t.Parallel()
	src := "Name:Sailor\nManaCost:U\nTypes:Creature Spirit\nPT:1/1\n" +
		"A:AB$ Draw | Cost$ 3 U | NumCards$ 1 | Defined$ You | SpellDescription$ Draw a card.\n" +
		"A:AB$ Draw | Cost$ 1 PayLife<2> | NumCards$ 1 | Defined$ You | SpellDescription$ Draw another card.\n" +
		"Oracle:x\n"
	e, _, id := newFixtureDeck(t, 34, src)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	addMana(t, e, 0, "UUUU")
	e.Advance()
	want := []string{"3 U", "1 PayLife<2>"}
	costs := e.AbilityCosts(0, id)
	for i, w := range want {
		opt := abilityOption(t, e, id, i)
		if opt.Cost != w {
			t.Errorf("ability %d option cost = %q, want %q (label %q)", i, opt.Cost, w, opt.Label)
		}
		if i < len(costs) && costs[i] != opt.Cost {
			t.Errorf("ability %d option cost %q disagrees with AbilityCosts %q", i, opt.Cost, costs[i])
		}
	}
}
