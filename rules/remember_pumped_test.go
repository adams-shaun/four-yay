package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// TestChemistersTrickRememberPumpedFeedsMustAttack is the real-card,
// end-to-end proof that effPump's `RememberPumped$ True` read populates the
// resolution-local Remembered set the chained sub-ability consumes.
//
// Chemister's Trick's pump carries `RememberPumped$ True` and its
// `SubAbility$ DBAnimate` is an Effect whose `StaticAbilities$ MustAttack`
// line scopes to `ValidCreature$ Card.IsRemembered` through
// `RememberObjects$ Remembered` -- i.e. exactly the resolution-local set the
// pump wrote. With the read missing the Effect captures nothing, so the
// pumped creature never becomes a required attacker and the unpumped one
// must never become one.
//
// The chain ends with `DBCleanup | ClearRemembered$ True`, so the live
// `Ctx.Remembered`/source list are deliberately empty once the whole chain
// resolves; the local half is therefore asserted against the pump resolved
// WITHOUT its sub-chain, and the downstream half against the Effect's own
// captured set (registered before the cleanup runs).
func TestChemistersTrickRememberPumpedFeedsMustAttack(t *testing.T) {
	reg := choiceCorpusRegistry(t)
	trick := choiceCorpusCard(t, "Chemister's Trick")
	pumpedSrc := "Name:Marked Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	otherSrc := "Name:Unmarked Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e := corpusEngine(t, reg, []*cards.Card{trick},
		[]*cards.Card{card(t, pumpedSrc), card(t, otherSrc)})

	// Precondition: the compiled real card still has the shape this test
	// asserts on -- a Pump whose sub-ability is the Card.IsRemembered Effect.
	top := trick.Faces[0].Abilities[0]
	if top.API != "Pump" || top.Sub == nil || top.Sub.API != "Effect" {
		t.Fatalf("precondition: Chemister's Trick top ability = %q sub=%v, want Pump -> Effect", top.API, top.Sub)
	}
	if _, ok := top.Params["RememberPumped"]; !ok {
		t.Fatal("precondition: Chemister's Trick's pump no longer carries RememberPumped$")
	}
	if top.Sub.Params["RememberObjects"] != "Remembered" {
		t.Fatalf("precondition: sub-ability RememberObjects = %q, want Remembered (the set this test exercises)",
			top.Sub.Params["RememberObjects"])
	}

	src := findCardObj(t, e, 0, "Chemister's Trick", state.ZHand)
	pumped := moveSeeded(t, e, 1, pumpedSrc, state.ZBattlefield)
	other := moveSeeded(t, e, 1, otherSrc, state.ZBattlefield)
	if pumped == other || e.G.Obj(pumped).Zone != state.ZBattlefield || e.G.Obj(other).Zone != state.ZBattlefield {
		t.Fatal("precondition: two distinct battlefield creatures are required")
	}
	if e.attackRequirements(pumped).any() || e.attackRequirements(other).any() {
		t.Fatal("precondition: neither creature may start under an attack requirement")
	}

	// Resolution-local half: resolve the pump alone (Sub trimmed) so the
	// chain's own `DBCleanup | ClearRemembered$ True` cannot mask what the
	// pump wrote. Only the pumped creature is in the set.
	pumpOnly := *top
	pumpOnly.Sub = nil
	localCtx := &effects.Ctx{Source: src, Controller: 0, TargetsOffered: true,
		Targets: []state.Target{{Obj: pumped}}, SVars: trick.Faces[0].SVars}
	effects.Resolve(e, localCtx, &pumpOnly)

	foundLocal := false
	for _, tg := range localCtx.Remembered {
		if tg.IsPlayer {
			continue
		}
		if tg.Obj == other {
			t.Fatalf("unpumped creature %d is in ctx.Remembered", other)
		}
		if tg.Obj == pumped {
			foundLocal = true
		}
	}
	if !foundLocal {
		t.Fatal("ctx.Remembered does not contain the pumped creature")
	}
	// The pump actually applied: a -2/-0 effect is scoped to the victim.
	applied := false
	for _, ce := range e.active() {
		if ce.Source == pumped && ce.AddPower == -2 {
			applied = true
		}
	}
	if !applied {
		t.Fatal("precondition: no -2/-0 pump effect is scoped to the victim, so the object was not actually pumped")
	}
	// Persistent half: the source's event-backed list, rebuilt from the log.
	foundPersistent := false
	for _, tg := range e.G.Obj(src).Remembered {
		if !tg.IsPlayer && tg.Obj == pumped {
			foundPersistent = true
		}
	}
	if !foundPersistent {
		t.Fatalf("source's event-backed Remembered does not contain the pumped creature: %+v", e.G.Obj(src).Remembered)
	}

	// Downstream consumer: resolve the real chain. The Effect's
	// `ValidCreature$ Card.IsRemembered` MustAttack line binds only the
	// remembered creature, captured before the chain's ClearRemembered runs.
	ctx := &effects.Ctx{Source: src, Controller: 0, TargetsOffered: true,
		Targets: []state.Target{{Obj: pumped}}, SVars: trick.Faces[0].SVars}
	effects.Resolve(e, ctx, top)

	if !e.attackRequirements(pumped).any() {
		t.Fatal("pumped creature has no attack requirement: the downstream Card.IsRemembered filter saw an empty remembered set")
	}
	if e.attackRequirements(other).any() {
		t.Fatal("unpumped creature gained an attack requirement")
	}

	// The handler ran: a registered MustAttack continuous effect carries the
	// pumped creature -- not an "unimplemented" Note standing in for it.
	sawMustAttack := false
	for _, ce := range e.active() {
		if ce.Restriction != "MustAttack" {
			continue
		}
		for _, id := range ce.Remembered {
			if id == pumped {
				sawMustAttack = true
			}
			if id == other {
				t.Fatalf("MustAttack effect remembered the unpumped creature %d", other)
			}
		}
	}
	if !sawMustAttack {
		t.Fatal("no registered MustAttack effect carries the pumped creature")
	}
}

// TestParamCensusReadsPumpRememberPumped pins the census half of the ticket:
// effPump now reads RememberPumped$, so the parameter census derives the read
// for api:Pump, and no repo-deck card is reported unsupported for
// param:api:Pump.RememberPumped. TestParamCensusDetectsADeletedConsumer
// proves the same derivation by pretending a consumer was deleted.
func TestParamCensusReadsPumpRememberPumped(t *testing.T) {
	base, d := measureParamCensus(t, nil)
	if d == nil || !d.api["Pump"]["RememberPumped"] {
		t.Fatalf("param census no longer derives the RememberPumped$ read for api:Pump -- the reader in effPump was deleted")
	}
	for card, labels := range base.labels {
		for _, l := range labels {
			if l == "param:api:Pump.RememberPumped" {
				t.Fatalf("%s is reported unsupported for param:api:Pump.RememberPumped despite the read", card)
			}
		}
	}
}
