package effects

// CR 720.6 fail-closed coverage for `api:ControlPlayer`'s effect entry point.
// The interval and the decision redirect are engine-side
// (rules/controlplayer_cr720_test.go); what this pins is that the effect emits
// a real ControlPlayerChange grant for a target opponent and grants nothing
// when the resolved target is the effect's own controller.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestControlPlayerEffectGrantsTargetOpponent(t *testing.T) {
	h := newHost(t, 2)
	sa := sa(t, "AB$ ControlPlayer | ValidTgts$ Opponent")
	effControlPlayer(h, &Ctx{Controller: 0,
		Targets: []state.Target{{Player: 1, IsPlayer: true}}}, sa)
	ctl, ok := h.g.ControlledBy[1]
	if !ok || ctl != 0 {
		t.Fatalf("ControlledBy = %+v, want seat 1 controlled by 0", h.g.ControlledBy)
	}
	if len(h.log) != 1 || h.log[0].Kind != events.ControlPlayerChange {
		t.Fatalf("grant events = %+v", h.log)
	}
	if len(h.log[0].IDs) != 1 {
		t.Fatalf("grant ids = %+v", h.log[0].IDs)
	}
	if p, isPlayer := h.log[0].IDs[0].PlayerRef(); !isPlayer || p != 1 {
		t.Fatalf("grant controlled seat = %+v", h.log[0].IDs)
	}
}

func TestControlPlayerEffectRejectsSelfTarget(t *testing.T) {
	h := newHost(t, 2)
	sa := sa(t, "AB$ ControlPlayer | ValidTgts$ Any")
	effControlPlayer(h, &Ctx{Controller: 0,
		Targets: []state.Target{{Player: 0, IsPlayer: true}}}, sa)
	if len(h.g.ControlledBy) != 0 {
		t.Fatalf("a self-target granted control: %+v", h.g.ControlledBy)
	}
	found := false
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "own controller") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no fail-closed Note for the self-target: %+v", h.log)
	}
}
