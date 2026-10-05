package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestInfernoTrapDamageCountUsesCorpusDamageEvents(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	trap, ok := reg.Lookup("Inferno Trap")
	if !ok {
		t.Fatal("corpus missing Inferno Trap")
	}
	body := trap.Faces[0].SVars["CreaturesDmg"]
	if body != "Count$NumDamageThisTurn Creature You" {
		t.Fatalf("Inferno Trap corpus SVar CreaturesDmg = %q", body)
	}
	e, ids := pcdrEngine(t, "Inferno Trap", "Grizzly Bears", "Wolverine, Best There Is")
	trapID := ids["Inferno Trap"]
	first, second := ids["Grizzly Bears"], ids["Wolverine, Best There Is"]
	if o := e.G.Obj(trapID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Inferno Trap must be on the battlefield")
	}
	for _, source := range []state.ObjID{first, second} {
		o := e.G.Obj(source)
		if o == nil || o.Zone != state.ZBattlefield || !o.EffectiveIsCreature() || o.Controller != 0 {
			t.Fatal("precondition: each counted source must be a creature controlled by Inferno Trap's controller")
		}
	}
	if first == second {
		t.Fatal("precondition: creature sources must have distinct identities")
	}
	// Case is a battlefield noncreature source; its damage must not contribute.
	noncreatureCard, ok := reg.Lookup("Case of the Burning Masks")
	if !ok {
		t.Fatal("corpus missing Case of the Burning Masks")
	}
	noncreature := onBoardCard(t, e, 0, noncreatureCard)
	if o := e.G.Obj(noncreature); o == nil || o.Zone != state.ZBattlefield || o.EffectiveIsCreature() {
		t.Fatal("precondition: excluded source must be a battlefield noncreature")
	}
	damageCountEvent(e, first, 0, 0, 2, true)
	damageCountEvent(e, first, 0, 0, 3, true)
	damageCountEvent(e, second, 0, 0, 1, true)
	damageCountEvent(e, noncreature, 0, 0, 7, true)
	if got := effects.EvalCount(e, &effects.Ctx{Controller: 0, Source: trapID}, body); got != 2 {
		t.Fatalf("Inferno Trap corpus count = %d, want two distinct creature sources (not three hits or the noncreature source)", got)
	}
}

func TestWolverineDamageCountUsesCorpusDamageEvents(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Wolverine, Best There Is")
	if !ok {
		t.Fatal("corpus missing Wolverine, Best There Is")
	}
	body := card.Faces[0].SVars["Y"]
	if body != "Count$NumDamageThisTurn Card.Self Creature.Other" {
		t.Fatalf("Wolverine corpus SVar Y = %q", body)
	}
	e, ids := pcdrEngine(t, "Wolverine, Best There Is", "Grizzly Bears")
	wolf, victim := ids["Wolverine, Best There Is"], ids["Grizzly Bears"]
	if o := e.G.Obj(wolf); o == nil || o.Zone != state.ZBattlefield || !o.EffectiveIsCreature() || o.Controller != 0 {
		t.Fatal("precondition: Wolverine must be a battlefield creature controlled by seat 0")
	}
	if o := e.G.Obj(victim); o == nil || o.Zone != state.ZBattlefield || !o.EffectiveIsCreature() || victim == wolf {
		t.Fatal("precondition: recipient must be a distinct battlefield creature")
	}
	secondVictim := onBoardCard(t, e, 1, card)
	if o := e.G.Obj(secondVictim); o == nil || o.Zone != state.ZBattlefield || !o.EffectiveIsCreature() || secondVictim == wolf {
		t.Fatal("precondition: second recipient must be a distinct battlefield creature")
	}
	damageCountEvent(e, wolf, victim, 0, 2, true)
	damageCountEvent(e, wolf, victim, 0, 3, true)
	damageCountEvent(e, wolf, secondVictim, 1, 1, true)
	if got := effects.EvalCount(e, &effects.Ctx{Controller: 0, Source: wolf}, body); got != 1 {
		t.Fatalf("Wolverine corpus count = %d, want one source despite multiple hits and recipients", got)
	}

	// Isolate the negative source/recipient checks: another creature's damage
	// to a creature and Wolverine's damage to itself must both fail the filter.
	e, ids = pcdrEngine(t, "Wolverine, Best There Is", "Grizzly Bears")
	wolf, victim = ids["Wolverine, Best There Is"], ids["Grizzly Bears"]
	otherSource := onBoardCard(t, e, 0, damageCountCorpusCard(t, "Wolverine, Best There Is"))
	if wolf == otherSource || victim == wolf || e.G.Obj(otherSource).Zone != state.ZBattlefield || e.G.Obj(victim).Zone != state.ZBattlefield {
		t.Fatal("precondition: negative-case source, Wolverine, and recipient must be distinct battlefield objects")
	}
	damageCountEvent(e, otherSource, victim, 0, 4, true)
	damageCountEvent(e, wolf, wolf, 0, 2, true)
	if got := effects.EvalCount(e, &effects.Ctx{Controller: 0, Source: wolf}, body); got != 0 {
		t.Fatalf("Wolverine corpus count for damage by another source or to itself = %d, want zero", got)
	}
}

func damageCountCorpusCard(t *testing.T, name string) *cards.Card {
	t.Helper()
	card, ok := testutil.CorpusRegistry(t).Lookup(name)
	if !ok {
		t.Fatalf("corpus missing %s", name)
	}
	return card
}
