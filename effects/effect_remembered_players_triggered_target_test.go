package effects

// effect_remembered_players_triggered_target_test.go — the RememberObjects$
// TriggeredTarget selector of effectRememberedPlayers (effects/staticline.go).
//
// A DB$/AB$ `Effect | RememberObjects$ TriggeredTarget` names the PLAYER the
// firing trigger's event targeted (the damaged player for Stigma Lasher's
// DamageDone | ValidTarget$ Player). Before the case existed the switch had no
// TriggeredTarget arm, so the capture yielded nothing, the registered
// CantGainLife restriction's ValidPlayer$ Player.IsRemembered bound nobody,
// and the damaged player still gained life. The role read is the direct
// c.TriggerTarget (not definedSpec's fallback): an OBJECT recipient must
// contribute nobody, never an unrelated chosen player target.

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

const rememberedTriggeredTargetCarrier = "Name:Stigma Probe\nTypes:Creature\nPT:2/2\nOracle:x\n"

func TestEffectRememberedPlayersTriggeredTarget(t *testing.T) {
	h := newHost(t, 2)
	src := battlefield(t, h, rememberedTriggeredTargetCarrier)
	s := sa(t, "DB$ Effect | RememberObjects$ TriggeredTarget")

	// A player recipient: the DamageDone-to-player role
	// (rules/trigger_referents.go binds ev.Obj==0 to player(ev.Player)) is a
	// player target, so the damaged player is captured.
	c := &Ctx{Source: src, Controller: 0,
		TriggerContext: TriggerContext{TriggerTarget: state.Target{Player: 1, IsPlayer: true}}}
	got := effectRememberedPlayers(h, c, s)
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("effectRememberedPlayers(TriggeredTarget) = %v, want [1] (the damaged player)", got)
	}

	// An object recipient (Stigma Lasher's damage to a CREATURE): the trigger
	// role is an object, not a player, so NO player is captured -- even though
	// an unrelated player was chosen as a target this resolution. This is the
	// direct read the helper must make rather than definedSpec's
	// TriggeredTarget arm, which falls back to c.Targets.
	other := h.g.AddObject(mkCard(t, rememberedTriggeredTargetCarrier), 1)
	c = &Ctx{Source: src, Controller: 0,
		Targets:        []state.Target{{Player: 1, IsPlayer: true}},
		TriggerContext: TriggerContext{TriggerTarget: state.Target{Obj: other.ID}}}
	got = effectRememberedPlayers(h, c, s)
	if len(got) != 0 {
		t.Fatalf("effectRememberedPlayers(TriggeredTarget object recipient) = %v, want none", got)
	}

	// An absent binding (zero TriggerTarget) yields nobody (fail closed).
	empty := effectRememberedPlayers(h, &Ctx{Source: src, Controller: 0}, s)
	if len(empty) != 0 {
		t.Fatalf("effectRememberedPlayers with no trigger role = %v, want none (fail closed)", empty)
	}
}

// TestEffectRememberedPlayersTriggeredTargetRealCard reads Stigma Lasher's
// actual TrigEffect body, so a corpus change to its RememberObjects$ spelling
// makes this fixture loud rather than silently dropping the case.
func TestEffectRememberedPlayersTriggeredTargetRealCard(t *testing.T) {
	h := newHost(t, 2)
	src := battlefield(t, h, rememberedTriggeredTargetCarrier)
	_, body := corpusSA(t, "Stigma Lasher", "TrigEffect")
	if got := body.Params["RememberObjects"]; got != "TriggeredTarget" {
		t.Fatalf("Stigma Lasher TrigEffect RememberObjects = %q, want TriggeredTarget", got)
	}
	c := &Ctx{Source: src, Controller: 0,
		TriggerContext: TriggerContext{TriggerTarget: state.Target{Player: 1, IsPlayer: true}}}
	got := effectRememberedPlayers(h, c, body)
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("effectRememberedPlayers(Stigma Lasher TrigEffect) = %v, want [1]", got)
	}
}
