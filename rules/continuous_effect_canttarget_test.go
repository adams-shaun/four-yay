package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Vines of Vastwood's real script is an SP$ Pump whose SubAbility$ is a DB$
// Effect registering a CantTarget static ("target creature can't be the
// target of spells or abilities your opponents control this turn") against a
// StaticAbilities$ SVar. Before Task ce1 that DB$ Effect was a Note, so the
// restriction never registered and the creature stayed targetable by the
// opponent; the test below pins that the restriction is now real and bites
// the opponent's targeting (an inline fixture -- the licensing rule forbids a
// corpus .txt in a test, and the kicker is irrelevant to the CantTarget
// registration so it is dropped to keep the fixture minimal).
func TestVinesOfVastwoodCantTargetBitesOpponent(t *testing.T) {
	t.Parallel()
	vines := card(t, "Name:Vines of Vastwood\nManaCost:G\nTypes:Instant\n"+
		"A:SP$ Pump | ValidTgts$ Creature | NumAtt$ +4 | NumDef$ +4 | SubAbility$ DBEffect\n"+
		"SVar:DBEffect:DB$ Effect | Defined$ Targeted | StaticAbilities$ STCantTarget | RememberObjects$ Targeted\n"+
		"SVar:STCantTarget:Mode$ CantTarget | ValidTarget$ Card.IsRemembered | Activator$ Player.Opponent\nOracle:x\n")
	e := handEngine(t, vines)
	e.G.Players[0].Pool[state.MG] = 1
	creature := onBoard(t, e, 1, "Name:Elf\nManaCost:G\nTypes:Creature Elf\nPT:2/2\nOracle:x\n")

	e.askPriority(0)
	castFirst(t, e, "cast")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision for Vines, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == creature {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("creature not offered as Vines target: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 8)
	if len(e.G.Stack) != 0 {
		t.Fatalf("Vines did not resolve: stack %v", e.G.Stack)
	}
	if e.Power(creature) != 6 {
		t.Fatalf("pump did not land: power %d, want 6", e.Power(creature))
	}

	// Seat 1 is an opponent of the Vines caster (seat 0), so the restriction
	// must withhold the creature from seat 1's targeting options. Because it
	// is Shock's only legal target, the cast itself must not be offered.
	// Put Shock directly into seat 1's hand (handEngine seeds every non-
	// fixture seat with Mountains, so moveSeeded/addToHand cannot find a
	// Shock there -- the deck has none).
	shockCard := card(t, "Name:Shock\nManaCost:R\nTypes:Instant\n"+
		"A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 2\nOracle:x\n")
	sh := e.G.AddObject(shockCard, 1)
	sh.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, append(e.G.Zone(state.ZHand, 1), sh.ID))
	e.G.Players[1].Pool[state.MR] = 1
	e.askPriority(1)
	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected opponent priority, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == sh.ID {
			t.Fatalf("opponent's Shock was offered despite having no legal target: %+v", d.Options)
		}
	}
}

// TestVinesOfVastwoodCantTargetStillAllowsTheCaster is the mirror side of the
// bite: the Activator$ Player.Opponent scoping means the caster's own spells
// are untouched, so seat 0 can still target the creature it just granted the
// restriction to.
func TestVinesOfVastwoodCantTargetStillAllowsTheCaster(t *testing.T) {
	t.Parallel()
	vines := card(t, "Name:Vines of Vastwood\nManaCost:G\nTypes:Instant\n"+
		"A:SP$ Pump | ValidTgts$ Creature | NumAtt$ +4 | NumDef$ +4 | SubAbility$ DBEffect\n"+
		"SVar:DBEffect:DB$ Effect | Defined$ Targeted | StaticAbilities$ STCantTarget | RememberObjects$ Targeted\n"+
		"SVar:STCantTarget:Mode$ CantTarget | ValidTarget$ Card.IsRemembered | Activator$ Player.Opponent\nOracle:x\n")
	shock := card(t, "Name:Shock\nManaCost:R\nTypes:Instant\n"+
		"A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 2\nOracle:x\n")
	e := handEngine(t, vines, shock)
	e.G.Players[0].Pool[state.MG] = 1
	e.G.Players[0].Pool[state.MR] = 1
	creature := onBoard(t, e, 1, "Name:Elf\nManaCost:G\nTypes:Creature Elf\nPT:2/2\nOracle:x\n")

	e.askPriority(0)
	castFirst(t, e, "cast")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision for Vines, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == creature {
			idx = o.Index
		}
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 8)

	// The caster (seat 0) can still Shock the creature it Vined.
	e.askPriority(0)
	castFirst(t, e, "cast")
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected caster's Shock target decision, got %+v", d)
	}
	sawElf := false
	for _, o := range d.Options {
		if o.Obj == creature {
			sawElf = true
		}
	}
	if !sawElf {
		t.Fatalf("caster's own Shock omitted the Vined creature: %+v", d.Options)
	}
}
