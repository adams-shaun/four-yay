package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Incinerate is "SP$ DealDamage ... | SubAbility$ DBEffect | RememberDamaged$
// True" whose DBEffect registers a CantRegenerate static ("creature dealt
// damage this way can't be regenerated this turn"). Before Task ce1 the
// DBEffect was a Note, so a creature with a regeneration shield would survive
// Incinerate's lethal damage; the test pins that the CantRegenerate
// registration now blocks the shield consumption (an inline fixture, per the
// licensing rule).
func TestIncinerateCantRegenerate(t *testing.T) {
	t.Parallel()
	inc := card(t, "Name:Incinerate\nManaCost:1 R\nTypes:Instant\n"+
		"A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 3 | SubAbility$ DBEffect | RememberDamaged$ True\n"+
		"SVar:DBEffect:DB$ Effect | RememberObjects$ Remembered.Creature | StaticAbilities$ NoRegen\n"+
		"SVar:NoRegen:Mode$ CantRegenerate | ValidCard$ Card.IsRemembered\nOracle:x\n")
	e := handEngine(t, inc)
	e.G.Players[0].Pool[state.MR] = 1
	e.G.Players[0].Pool[state.MC] = 1
	creature := onBoard(t, e, 1, "Name:Regenerator\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	// A regeneration shield: without CantRegenerate this would be consumed and
	// the creature would survive the lethal damage.
	regenEffect(e, creature, "Regenerate", nil)
	if e.G.Obj(creature).Counter("Shield") != 1 {
		t.Fatalf("expected 1 regeneration shield, got %d", e.G.Obj(creature).Counter("Shield"))
	}

	e.askPriority(0)
	castFirst(t, e, "cast")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision for Incinerate, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == creature {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("creature not offered as Incinerate target: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 8)
	if len(e.G.Stack) != 0 {
		t.Fatalf("Incinerate did not resolve: stack %v", e.G.Stack)
	}

	// Lethal damage (3 on a 2/2) with a shield in place: SBA runs as the
	// spell resolves, and the CantRegenerate restriction must make the
	// creature die instead of consuming the shield and surviving.
	if z := e.G.Obj(creature).Zone; z != state.ZGraveyard {
		t.Fatalf("creature regenerated despite Incinerate's CantRegenerate: zone=%v", z)
	}
}
