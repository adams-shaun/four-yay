package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func ojerCountBody(t *testing.T) string {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Ojer Axonil, Deepest Might")
	if !ok || len(card.Faces) < 2 {
		t.Fatal("corpus missing transformable Ojer")
	}
	body := card.Faces[1].SVars["X"]
	if body != "Count$NonCombatDamageThisTurn Card.Red+YouCtrl Any" {
		t.Fatalf("Ojer corpus back-face SVar X = %q", body)
	}
	return body
}

func TestOjerNonCombatDamageUsesZoneAtHit(t *testing.T) {
	body := ojerCountBody(t)
	e, ids := pcdrEngine(t, "Ojer Axonil, Deepest Might", "Grizzly Bears")
	src, recipient := ids["Ojer Axonil, Deepest Might"], ids["Grizzly Bears"]
	ctx := &effects.Ctx{Controller: 0, Source: src}
	if e.G.Obj(src).Zone != state.ZBattlefield || e.ObjectColors(e.G.Obj(src)) != "R" {
		t.Fatal("precondition: Ojer must be a red battlefield source")
	}
	// A permanent copy remains matchable while on the battlefield, but the
	// live off-battlefield copy is inert. Its earlier damage is not.
	e.G.Obj(src).IsCopy = true
	if !e.G.Obj(src).IsCopy {
		t.Fatal("precondition: source is a copy")
	}
	damageCountEvent(e, src, recipient, 0, 2, false)
	if got := effects.EvalCount(e, ctx, body); got != 2 {
		t.Fatalf("before source moves: %d, want 2", got)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: src, From: state.ZBattlefield, To: state.ZGraveyard})
	if got := e.G.Obj(src).Zone; got != state.ZGraveyard {
		t.Fatalf("precondition: source zone = %v, want graveyard, not battlefield", got)
	}
	if got := effects.EvalCount(e, ctx, body); got != 2 {
		t.Fatalf("Ojer count after source leaves = %d, want 2 from battlefield copy", got)
	}
}

func TestOjerNonCombatDamageUsesTypesAtHit(t *testing.T) {
	body := ojerCountBody(t)
	// Qualify Ojer's real Count$ source filter by a layer-granted type:
	// the hit counts while Goblin, even after the granting source leaves.
	body = strings.Replace(body, "Card.Red+YouCtrl", "Card.Red+YouCtrl+Goblin", 1)
	e, ids := pcdrEngine(t, "Ojer Axonil, Deepest Might", "Grizzly Bears")
	src, granter := ids["Ojer Axonil, Deepest Might"], ids["Grizzly Bears"]
	ctx := &effects.Ctx{Controller: 0, Source: src}
	if e.G.Obj(src).Zone != state.ZBattlefield || e.G.Obj(granter).Zone != state.ZBattlefield ||
		damageTimeHasType(e.G.Obj(src).Face().Types, "Goblin") {
		t.Fatal("precondition: source and granter are on the battlefield; Ojer does not print Goblin")
	}
	e.AddContinuous(ContinuousEffect{Source: granter, Controller: 0, Layer: LType,
		Affects: "Card.Red+YouCtrl", AddTypes: []string{"Goblin"}})
	if !damageTimeHasType(e.Derived(src).Types, "Goblin") {
		t.Fatalf("precondition: layer-4 did not grant Goblin: %v", e.Derived(src).Types)
	}
	damageCountEvent(e, src, granter, 0, 2, false)
	if got := effects.EvalCount(e, ctx, body); got == 0 {
		t.Fatal("precondition: layer-derived source type did not qualify at hit")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: granter, From: state.ZBattlefield, To: state.ZGraveyard})
	if damageTimeHasType(e.Derived(src).Types, "Goblin") {
		t.Fatalf("precondition: source still has Goblin after granter leaves: %v", e.Derived(src).Types)
	}
	if got := effects.EvalCount(e, ctx, body); got != 2 {
		t.Fatalf("Ojer count after source loses Goblin = %d, want historical 2", got)
	}
}
