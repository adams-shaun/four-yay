package effects

// effect_cantgainlife_test.go — the REGISTRATION half of the Effect-delivered
// CR 614.1 CantGainLife static (task cantgainlife1): a DB$ Effect whose
// StaticAbilities$ SVar carries `Mode$ CantGainLife | ValidPlayer$ ...` must
// register a continuous restriction rules' lifeGainForbidden registered walk
// reads, not fall to the unimplemented Note (Screaming Nemesis, Stigma Lasher,
// Welcome the Darkness, Skullcrack, Atarka's Command, Call In a Professional,
// Roiling Vortex). Both directions are pinned: a readable body registers, a
// body carrying a scoping term the path does not evaluate (IsPresent$) fails
// closed to the Note.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestEffectDeliveredCantGainLifeRegisters(t *testing.T) {
	h := newHost(t, 2)
	c := &Ctx{Controller: 0, Source: 1, SVars: map[string]string{
		"CGL": "Mode$ CantGainLife | ValidPlayer$ You | Description$ You can't gain life for the rest of the game.",
	}}
	Resolve(h, c, sa(t, "DB$ Effect | StaticAbilities$ CGL | Duration$ Permanent"))
	if len(h.continuous) != 1 {
		t.Fatalf("continuous = %+v, want one CantGainLife registration", h.continuous)
	}
	ce := h.continuous[0]
	if ce.Restriction != "CantGainLife" {
		t.Fatalf("Restriction = %q, want CantGainLife", ce.Restriction)
	}
	if ce.RestrictParams["ValidPlayer"] != "You" {
		t.Fatalf("ValidPlayer = %q, want You", ce.RestrictParams["ValidPlayer"])
	}
	// Duration$ Permanent is CR 611.2a's game-lasting read: the lock must
	// carry Permanent so it survives its (one-shot spell or dying creature)
	// source and end-of-turn cleanup.
	if !ce.Permanent || ce.UntilEOT {
		t.Fatalf("Duration$ Permanent registration = %+v, want Permanent, not UntilEOT", ce)
	}
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented") {
			t.Fatalf("CantGainLife still reports unimplemented: %q", ev.Text)
		}
	}
}

// TestEffectDeliveredCantGainLifeAbsentDurationIsThisTurn pins the absent-
// Duration$ read: the corpus's four no-Duration carriers (Skullcrack, Call In
// a Professional, Atarka's Command, Roiling Vortex) all say "this turn", so
// absentDurationMeansThisTurn must give the registration UntilEOT -- not the
// source-leaves lifetime a battlefield source (Roiling Vortex's enchantment)
// would otherwise keep for the rest of the game.
func TestEffectDeliveredCantGainLifeAbsentDurationIsThisTurn(t *testing.T) {
	h := newHost(t, 2)
	c := &Ctx{Controller: 0, Source: 1, SVars: map[string]string{
		"CGL": "Mode$ CantGainLife | ValidPlayer$ Player.Opponent | Description$ Your opponents can't gain life this turn.",
	}}
	Resolve(h, c, sa(t, "DB$ Effect | StaticAbilities$ CGL"))
	if len(h.continuous) != 1 {
		t.Fatalf("continuous = %+v, want one CantGainLife registration", h.continuous)
	}
	ce := h.continuous[0]
	if ce.Restriction != "CantGainLife" || ce.RestrictParams["ValidPlayer"] != "Player.Opponent" {
		t.Fatalf("registration = %+v, want CantGainLife scoped to Player.Opponent", ce)
	}
	if !ce.UntilEOT {
		t.Fatalf("absent-Duration registration = %+v, want UntilEOT (this turn)", ce)
	}
	if ce.Permanent {
		t.Fatalf("absent-Duration registration = %+v, must not be Permanent", ce)
	}
}

// TestEffectDeliveredCantGainLifeRememberedPlayers pins the player half of
// the remembered capture: Screaming Nemesis's DBEffect RememberObjects$
// Player.IsRemembered must capture the damaged player out of the live
// Ctx.Remembered into ce.RememberedPlayers, which rules'
// restrictionPlayerSpecMatches consults for ValidPlayer$ Player.IsRemembered
// at life-gain time. Without this the lock binds nobody.
func TestEffectDeliveredCantGainLifeRememberedPlayers(t *testing.T) {
	h := newHost(t, 2)
	c := &Ctx{
		Controller: 0, Source: 1,
		Remembered: []state.Target{{Player: 1, IsPlayer: true}},
		SVars: map[string]string{
			"CGL": "Mode$ CantGainLife | ValidPlayer$ Player.IsRemembered | Description$ The damaged player can't gain life for the rest of the game.",
		},
	}
	Resolve(h, c, sa(t, "DB$ Effect | StaticAbilities$ CGL | Duration$ Permanent | RememberObjects$ Player.IsRemembered"))
	if len(h.continuous) != 1 {
		t.Fatalf("continuous = %+v, want one CantGainLife registration", h.continuous)
	}
	ce := h.continuous[0]
	if len(ce.RememberedPlayers) != 1 || ce.RememberedPlayers[0] != 1 {
		t.Fatalf("RememberedPlayers = %v, want [1] (the damaged player)", ce.RememberedPlayers)
	}
}

// TestEffectDeliveredCantGainLifeFailClosed pins the other direction: a body
// carrying a scoping term this registration path does not evaluate
// (IsPresent$) must NOT register blanket -- an unconditional lock would
// over-restrict -- so it reports the unimplemented Note instead.
func TestEffectDeliveredCantGainLifeFailClosed(t *testing.T) {
	h := newHost(t, 2)
	c := &Ctx{Controller: 0, Source: 1, SVars: map[string]string{
		"CGL": "Mode$ CantGainLife | ValidPlayer$ You | IsPresent$ Creature.YouCtrl | Description$ gated",
	}}
	Resolve(h, c, sa(t, "DB$ Effect | StaticAbilities$ CGL | Duration$ Permanent"))
	if len(h.continuous) != 0 {
		t.Fatalf("unreadable body registered: %+v", h.continuous)
	}
	found := false
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "CantGainLife") &&
			strings.Contains(ev.Text, "unimplemented") {
			found = true
		}
	}
	if !found {
		t.Fatalf("unreadable CantGainLife body produced no unimplemented Note: %+v", h.log)
	}
}
