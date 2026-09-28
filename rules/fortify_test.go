package rules

// kw:Fortify (CR 702.67) -- the Fortification keyword, pinned on the real
// corpus carriers. Exactly two corpus card files carry K:Fortify (C.A.M.P.
// and Darksteel Garrison); the tests below drive C.A.M.P.'s own printed
// script end to end: the {3} Fortify activation attaches the artifact to a
// land (never a creature), and the fortified land's mana activation fires
// the card's own TapsForMana trigger through the Card.FortifiedBy filter
// predicate, landing the +1/+1 counter on the chosen creature.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const campDeckName = "C.A.M.P."

// fortifyCamp drives C.A.M.P.'s expanded Fortify {3} ability through the
// ordinary offer/charge path onto land, returning after the board settles.
func fortifyCamp(t *testing.T, e *Engine, camp, land state.ObjID) {
	t.Helper()
	addMana(t, e, 0, "CCC")
	opt, ok := findAbilityOption(e, camp, 0)
	if !ok {
		t.Fatal("C.A.M.P.'s Fortify not offered for {3}")
	}
	submitChoices(t, e, opt.Index)
	targetObject(t, e, land)
	passUntilStackEmpty(t, e, 20)
}

func TestCAMPFortifyAttachesToLand(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"C.A.M.P.", "Forest", "Grizzly Bears"}, nil)
	camp := findOnBoard(t, e, 0, "C.A.M.P.")
	forest := findOnBoard(t, e, 0, "Forest")
	bear := findOnBoard(t, e, 0, "Grizzly Bears")
	if got := e.G.Obj(camp); got == nil || got.Zone != state.ZBattlefield || got.AttachedTo != 0 {
		t.Fatalf("precondition: C.A.M.P. on battlefield unattached = %+v", got)
	}
	if got := e.G.Obj(forest); got == nil || got.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Forest on battlefield = %+v", got)
	}

	// Empty pool: the {3} Fortify is not offered.
	if _, ok := findAbilityOption(e, camp, 0); ok {
		t.Fatal("C.A.M.P.'s Fortify offered from an empty pool")
	}
	fortifyCamp(t, e, camp, forest)
	if e.G.Obj(camp).AttachedTo != forest {
		t.Fatalf("C.A.M.P. attached to %d, want the Forest %d", e.G.Obj(camp).AttachedTo, forest)
	}

	// The attachment target was a LAND: a second Fortify activation's ask
	// must offer the Forest and never the Grizzly Bear (CR 702.67a).
	addMana(t, e, 0, "CCC")
	opt2, ok := findAbilityOption(e, camp, 0)
	if !ok {
		t.Fatal("C.A.M.P.'s Fortify not offered a second time")
	}
	submitChoices(t, e, opt2.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Fortify target ask = %+v, want KTarget", d)
	}
	for _, o := range d.Options {
		if o.Obj == bear {
			t.Fatalf("Fortify offered a creature (%d) as an attachment target: %+v", bear, d.Options)
		}
	}
	landOffered := false
	for _, o := range d.Options {
		if o.Obj == forest {
			landOffered = true
		}
	}
	if !landOffered {
		t.Fatalf("Fortify target ask did not offer the Forest: %+v", d.Options)
	}
}

func TestCAMPFortifiedLandTapsForManaFiresCounterTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"C.A.M.P.", "Forest", "Grizzly Bears"}, nil)
	camp := findOnBoard(t, e, 0, "C.A.M.P.")
	forest := findOnBoard(t, e, 0, "Forest")
	bear := findOnBoard(t, e, 0, "Grizzly Bears")
	if got := e.G.Obj(bear).Counter("P1P1"); got != 0 {
		t.Fatalf("precondition: bear starts with %d +1/+1 counters", got)
	}
	fortifyCamp(t, e, camp, forest)
	if e.G.Obj(camp).AttachedTo != forest {
		t.Fatalf("precondition failed: C.A.M.P. attached to %d, want the Forest %d", e.G.Obj(camp).AttachedTo, forest)
	}

	// Tap the fortified land for mana: C.A.M.P.'s own
	// T:Mode$ TapsForMana | ValidCard$ Card.FortifiedBy trigger queues.
	e.resolveManaAbility(0, forest, e.availableManaAbilities(0, forest)[0], false)
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("tapping the fortified land queued %d triggers, want C.A.M.P.'s one TapsForMana trigger", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after the trigger was put on the stack: %+v, want the counter target ask", d)
	}
	targetObject(t, e, bear)
	passUntilStackEmpty(t, e, 30)
	if got := e.G.Obj(bear).Counter("P1P1"); got != 1 {
		t.Fatalf("bear +1/+1 counters after the fortified land was tapped for mana = %d, want 1", got)
	}

	// The trigger's ValidCard$ reads the LIVE attach relation: without the
	// FortifiedBy predicate (pre-fix behaviour) the same tap queues nothing.
	e = nil
	e2, _ := linkBoard(t, reg, []string{"C.A.M.P.", "Forest", "Grizzly Bears"}, nil)
	forest2 := findOnBoard(t, e2, 0, "Forest")
	e2.resolveManaAbility(0, forest2, e2.availableManaAbilities(0, forest2)[0], false)
	if len(e2.pendingTriggers) != 0 {
		t.Fatalf("tapping an UNfortified land queued %d triggers, want none", len(e2.pendingTriggers))
	}
}

func TestDarksteelGarrisonFortifiedLandBecomesTappedFiresTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"Darksteel Garrison", "Forest", "Grizzly Bears"}, nil)
	garrison := findOnBoard(t, e, 0, "Darksteel Garrison")
	forest := findOnBoard(t, e, 0, "Forest")

	// The same Fortify {3} shape, second carrier.
	addMana(t, e, 0, "CCC")
	opt, ok := findAbilityOption(e, garrison, 0)
	if !ok {
		t.Fatal("Darksteel Garrison's Fortify not offered for {3}")
	}
	submitChoices(t, e, opt.Index)
	targetObject(t, e, forest)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(garrison).AttachedTo != forest {
		t.Fatalf("Darksteel Garrison attached to %d, want the Forest %d", e.G.Obj(garrison).AttachedTo, forest)
	}

	// Its T:Mode$ Taps | ValidCard$ Land.FortifiedBy trigger fires on the
	// same activation tap (the land BECOMES tapped).
	e.resolveManaAbility(0, forest, e.availableManaAbilities(0, forest)[0], false)
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("tapping the fortified land queued %d triggers, want Darksteel Garrison's one Taps trigger", len(e.pendingTriggers))
	}
}

func TestFortifyRegisteredMakesCarriersFullySupported(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	for _, name := range []string{"C.A.M.P.", "Darksteel Garrison"} {
		c := mustCorpusCard(t, reg, name)
		if m := reg.Unsupported(c, supported); len(m) > 0 {
			t.Fatalf("%s still unsupported: %v", name, m)
		}
	}
}
