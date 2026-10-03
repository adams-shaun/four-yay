package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/trigmatch"
	"github.com/adams-shaun/gorge/state"
)

// A player recipient on a noncombat Damage event is not the combat-specific
// TriggeredDefendingPlayer role. Strip CombatDamage$ from the real Electryte
// trigger here so this test exercises the role binding itself, not that
// separate trigger qualifier.
func TestTriggeredDefendingPlayerDoesNotBindNoncombatRecipient(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, _, ids := dsBoard(t, reg, "Electryte")
	source := ids["Electryte"]
	if o := e.G.Obj(source); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Electryte must be on the battlefield")
	}
	trig := fx20TriggerWithParam(t, e, source, "Player.TriggeredDefendingPlayer")
	if trig.Params["ValidTarget"] != "Player.TriggeredDefendingPlayer" {
		t.Fatalf("precondition: unexpected ValidTarget %q", trig.Params["ValidTarget"])
	}
	trig.Params = cloneStringMap(trig.Params)
	delete(trig.Params, "CombatDamage")
	e.combatDamaging = false
	e.dmgSrcOverride = source

	if trigmatch.DamageMatches(boardOf(e), trig, source, events.Event{Kind: events.Damage, Player: 1}) {
		t.Fatal("noncombat damage recipient must not be treated as a defending player")
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
