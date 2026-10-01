package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestCompoundRememberedRestrictionDoesNotOverapply is fix F2: a restriction
// whose valid-spec carries IsRemembered combined with an extra predicate
// (Card.IsRemembered+Creature, a + AND or a , OR list) must never over-apply
// to a remembered object that fails the extra predicate. The mechanism is now
// the general filter's IsRemembered (the compound resolves faithfully through
// the object matcher with the registered remembered set bound), but the
// pinned behaviour is the same one F2 guarded: the remembered non-creature is
// NOT blocked, and a bare IsRemembered spec still blocks a remembered object
// of ANY type.
func TestCompoundRememberedRestrictionDoesNotOverapply(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	// A remembered permanent that fails the compound's extra "Creature"
	// predicate: an artifact, not a creature.
	artifact := onBoard(t, e, 0, "Name:Bauble\nManaCost:0\nTypes:Artifact\nOracle:x\n")
	e.AddContinuous(ContinuousEffect{
		Source: artifact, Controller: 0,
		Restriction: "CantTarget",
		RestrictParams: map[string]string{
			"ValidTarget": "Card.IsRemembered+Creature",
			"Activator":   "Player.Opponent",
		},
		Remembered: []state.ObjID{artifact},
		Duration:   "UntilTheEndOfYourNextTurn",
	})
	// The object is remembered, so a naive remembered-set match would return
	// true (over-applying: it is not a creature). The compound's Creature half
	// must exclude it.
	if e.restrictionBlocksTarget(artifact, 1) {
		t.Fatal("compound IsRemembered spec over-applied to a remembered non-creature")
	}
	// And a bare IsRemembered spec (the only shape the corpus uses) must keep
	// working: a remembered object of ANY type is blocked by it.
	e.AddContinuous(ContinuousEffect{
		Source: artifact, Controller: 0,
		Restriction:    "CantTarget",
		RestrictParams: map[string]string{"ValidTarget": "Card.IsRemembered", "Activator": "Player.Opponent"},
		Remembered:     []state.ObjID{artifact},
		Duration:       "UntilTheEndOfYourNextTurn",
	})
	if !e.restrictionBlocksTarget(artifact, 1) {
		t.Fatal("bare IsRemembered spec should still block a remembered object")
	}
}

// TestCompoundRememberedEffectStaticRegistersAndDoesNotOverapply is the
// registration side of the IsRemembered upgrade: a DB$ Effect whose CantTarget
// static carries a compound IsRemembered spec used to fall through to the
// honest Note (the remembered-set match could not resolve the extra
// predicate); the general filter now implements IsRemembered, so the static
// registers for real and applies exactly to the remembered objects that ALSO
// satisfy the extra predicate -- here the remembered Elf, a creature. A
// remembered object that FAILS the extra predicate is covered by
// TestCompoundRememberedRestrictionDoesNotOverapply above (the restriction
// that test registers directly cannot be produced by a corpus Pump target,
// which is creature-only). No corpus card reaches this shape, so it is purely
// a guard.
func TestCompoundRememberedEffectStaticRegistersAndDoesNotOverapply(t *testing.T) {
	t.Parallel()
	guard := card(t, "Name:Guard\nManaCost:G\nTypes:Instant\n"+
		"A:SP$ Pump | ValidTgts$ Creature | NumAtt$ +2 | NumDef$ +2 | SubAbility$ DBEffect\n"+
		"SVar:DBEffect:DB$ Effect | Defined$ Targeted | Duration$ UntilTheEndOfYourNextTurn | StaticAbilities$ Guard | RememberObjects$ Targeted\n"+
		"SVar:Guard:Mode$ CantTarget | ValidTarget$ Card.IsRemembered+Creature | Activator$ Player.Opponent\nOracle:x\n")
	e := handEngine(t, guard)
	e.G.Players[0].Pool[state.MG] = 1
	creature := onBoard(t, e, 0, "Name:Elf\nManaCost:G\nTypes:Creature Elf\nPT:2/2\nOracle:x\n")
	stranger := onBoard(t, e, 0, "Name:Stranger\nManaCost:G\nTypes:Creature Ape\nPT:2/2\nOracle:x\n")

	e.askPriority(0)
	castFirst(t, e, "cast")
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Obj == creature {
			idx = o.Index
		}
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 8)
	// The compound IsRemembered static now registers a real restriction.
	var ce *ContinuousEffect
	for i := range e.continuous {
		if e.continuous[i].Restriction == "CantTarget" {
			ce = &e.continuous[i]
		}
	}
	if ce == nil {
		t.Fatal("compound IsRemembered static did not register a restriction")
	}
	// It applies to the remembered creature that satisfies BOTH halves and
	// never to a non-remembered creature.
	if !e.restrictionApplies(ce, creature) {
		t.Fatal("compound must apply to the remembered creature (both halves hold)")
	}
	if e.restrictionApplies(ce, stranger) {
		t.Fatal("compound applied to a non-remembered creature")
	}
}
