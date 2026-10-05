package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

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
	e.damaging, e.combatDamaging = source, true
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 3})
	e.combatDamaging = false
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e.damaging = 0
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
