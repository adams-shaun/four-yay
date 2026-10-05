package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func damageCountEvent(e *Engine, source, target state.ObjID, player state.PlayerID, amount int32, combat bool) {
	e.damaging, e.combatDamaging = source, combat
	e.emit(events.Event{Kind: events.Damage, Amount: amount, Player: player, Obj: target})
	e.damaging, e.combatDamaging = 0, false
}

func TestMaxCombatDamageCountUsesCorpusDamageEvents(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Sidequest: Play Blitzball")
	if !ok {
		t.Fatal("corpus missing Sidequest: Play Blitzball")
	}
	body := card.Faces[0].SVars["X"]
	if body != "Count$MaxCombatDamageThisTurn" {
		t.Fatalf("Sidequest corpus SVar X = %q", body)
	}
	e, ids := pcdrEngine(t, "Sidequest: Play Blitzball")
	source := ids["Sidequest: Play Blitzball"]
	if o := e.G.Obj(source); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Sidequest damage source must be on the battlefield")
	}
	damageCountEvent(e, source, 0, 1, 2, true)
	damageCountEvent(e, source, 0, 1, 3, true)
	damageCountEvent(e, source, 0, 2, 4, true)
	damageCountEvent(e, source, 0, 1, 8, false)
	if got := effects.EvalCount(e, &effects.Ctx{Controller: 0, Source: source}, body); got != 5 {
		t.Fatalf("Sidequest corpus count = %d, want max same-recipient combat total 5 (not 4 or 13)", got)
	}
}

func TestNumDamageCountUsesCorpusDamageEvents(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Case of the Burning Masks")
	if !ok {
		t.Fatal("corpus missing Case of the Burning Masks")
	}
	body := card.Faces[0].SVars["X"]
	if body != "Count$NumDamageThisTurn Card.YouCtrl,Emblem.YouCtrl Player,Permanent" {
		t.Fatalf("Case of the Burning Masks corpus SVar X = %q", body)
	}
	e, ids := pcdrEngine(t, "Case of the Burning Masks", "Grizzly Bears")
	source, permanent := ids["Case of the Burning Masks"], ids["Grizzly Bears"]
	if o := e.G.Obj(source); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Case source must be on the battlefield")
	}
	if o := e.G.Obj(permanent); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: permanent damage recipient must be on the battlefield")
	}
	// Combat damage avoids Ojer's unrelated noncombat-damage replacement.
	damageCountEvent(e, source, 0, 1, 3, true)
	damageCountEvent(e, source, permanent, 0, 2, true)
	if got := effects.EvalCount(e, &effects.Ctx{Controller: 0, Source: source}, body); got != 5 {
		t.Fatalf("Burning Masks corpus count = %d, want player + permanent damage 5", got)
	}
}

func TestNonCombatDamageCountUsesCorpusDamageEvents(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Ojer Axonil, Deepest Might")
	if !ok {
		t.Fatal("corpus missing Ojer Axonil, Deepest Might")
	}
	body := card.Faces[1].SVars["X"]
	if body != "Count$NonCombatDamageThisTurn Card.Red+YouCtrl Any" {
		t.Fatalf("Ojer Axonil corpus back-face SVar X = %q", body)
	}
	e, ids := pcdrEngine(t, "Ojer Axonil, Deepest Might", "Grizzly Bears")
	source, permanent := ids["Ojer Axonil, Deepest Might"], ids["Grizzly Bears"]
	if o := e.G.Obj(source); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Ojer source must be on the battlefield")
	}
	if o := e.G.Obj(permanent); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: permanent damage recipient must be on the battlefield")
	}
	// Ojer replaces the 2 noncombat player damage with 4; combat damage is excluded.
	damageCountEvent(e, source, 0, 1, 2, false)
	damageCountEvent(e, source, permanent, 0, 3, false)
	damageCountEvent(e, source, 0, 1, 7, true)
	if got := effects.EvalCount(e, &effects.Ctx{Controller: 0, Source: source}, body); got != 7 {
		t.Fatalf("Ojer corpus count = %d, want noncombat damage after replacement (4) plus permanent damage (3)", got)
	}
}

func TestDamageCountHistoryFoldsDamageAndResetsOnTurnChange(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	carrier, ok := reg.Lookup("Sidequest: Play Blitzball")
	if !ok {
		t.Fatal("corpus missing Sidequest: Play Blitzball")
	}
	body := carrier.Faces[0].SVars["X"]
	if body != "Count$MaxCombatDamageThisTurn" {
		t.Fatalf("Sidequest corpus SVar X = %q", body)
	}
	e, ids := pcdrEngine(t, "Grizzly Bears")
	source := ids["Grizzly Bears"]
	if e.G.Obj(source) == nil || e.G.Obj(source).Zone != state.ZBattlefield {
		t.Fatal("precondition: source must be a battlefield object")
	}
	damageCountEvent(e, source, 0, 1, 3, true)
	damageCountEvent(e, source, 0, 1, 2, false)
	hit := e.G.Obj(source).DamageDealtThisTurn
	if len(hit) != 2 || !hit[0].Combat || hit[1].Combat || hit[0].Amount != 3 || hit[1].Amount != 2 {
		t.Fatalf("damage event ledger = %+v, want combat 3 and noncombat 2", hit)
	}
	ctx := &effects.Ctx{Controller: 0, Source: source}
	if got := effects.EvalCount(e, ctx, body); got != 3 {
		t.Fatalf("corpus Blitzball count = %d, want max combat damage 3", got)
	}
	turn := e.G.Turn + 1
	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: turn})
	if len(e.G.Obj(source).DamageDealtThisTurn) != 0 {
		t.Fatalf("damage ledger survived TurnChange: %+v", e.G.Obj(source).DamageDealtThisTurn)
	}
}
