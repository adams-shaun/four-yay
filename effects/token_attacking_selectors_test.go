package effects

// The non-True TokenAttacking$ selector forms (task
// agent-20260920T064028Z-a7f115d7). The engine's player-defender grammar
// (effects.definedSpec) must resolve the player half of every corpus spelling
// -- Remembered x5, RememberedPlayer x3, TriggeredAttackedTarget x4,
// TriggeredDefender x1 -- and the object half (`Valid <filter>`, the "or a
// planeswalker they control" arm) must degrade loudly rather than silently.
// Adeline, Resplendent Cathar's real corpus SA is pinned end to end in
// rules/adeline_token_attacking_test.go; these are the sibling spellings at
// the primitive.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func notesContaining(h *fakeHost, frag string) int {
	n := 0
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, frag) {
			n++
		}
	}
	return n
}

// TestTokenAttackingRememberedPlayerAttacksTheRememberedSeat pins the
// `RememberedPlayer` spelling (Adeline's bare form) and the RepeatEach binding
// it names: the resolution's remembered PLAYER is the defender.
func TestTokenAttackingRememberedPlayerAttacksTheRememberedSeat(t *testing.T) {
	h, c := fixtureHostWithTokens(t)
	c.Remembered = []state.Target{{Player: 1, IsPlayer: true}}
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Token",
		Params: map[string]string{"TokenScript": "r_1_1_goblin", "TokenTapped": "True", "TokenAttacking": "RememberedPlayer"}})
	bf := h.Game().Zone(state.ZBattlefield, c.Controller)
	if len(bf) != 1 {
		t.Fatalf("battlefield = %v, want 1 token", bf)
	}
	o := h.Game().Obj(bf[0])
	if !o.Tapped || !o.IsAttacking || o.Attacking != 1 {
		t.Fatalf("token tapped=%v attacking=%v defender=%d, want tapped, attacking seat 1", o.Tapped, o.IsAttacking, o.Attacking)
	}
	if attacks := countKind(h, events.TokenAttacks); attacks != 1 {
		t.Fatalf("%d TokenAttacks events, want 1", attacks)
	}
	if n := notesContaining(h, "not implemented"); n != 0 {
		t.Fatalf("%d degrade Notes on a resolved selector, want 0; log=%+v", n, h.log)
	}
}

// TestTokenAttackingRememberedAttacksTheRememberedSeat pins the bare
// `Remembered` spelling at the primitive (5 corpus carriers, four of them
// RepeatEach-driven).
func TestTokenAttackingRememberedAttacksTheRememberedSeat(t *testing.T) {
	h, c := fixtureHostWithTokens(t)
	c.Remembered = []state.Target{{Player: 1, IsPlayer: true}}
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Token",
		Params: map[string]string{"TokenScript": "r_1_1_goblin", "TokenTapped": "True", "TokenAttacking": "Remembered"}})
	bf := h.Game().Zone(state.ZBattlefield, c.Controller)
	if len(bf) != 1 {
		t.Fatalf("battlefield = %v, want 1 token", bf)
	}
	o := h.Game().Obj(bf[0])
	if !o.IsAttacking || o.Attacking != 1 {
		t.Fatalf("token attacking=%v defender=%d, want attacking seat 1", o.IsAttacking, o.Attacking)
	}
}

// TestTokenAttackingRememberedIgnoresRememberedCards pins the plain-Remembered
// rule: a remembered CARD contributes no seat, so the token degrades (no
// attacker) instead of attacking the card's controller.
func TestTokenAttackingRememberedIgnoresRememberedCards(t *testing.T) {
	h, c := fixtureHostWithTokens(t)
	c.Remembered = []state.Target{{Obj: 1}}
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Token",
		Params: map[string]string{"TokenScript": "r_1_1_goblin", "TokenAttacking": "Remembered"}})
	bf := h.Game().Zone(state.ZBattlefield, c.Controller)
	if len(bf) != 1 {
		t.Fatalf("battlefield = %v, want 1 token", bf)
	}
	o := h.Game().Obj(bf[0])
	if o.IsAttacking || o.Attacking != 0 {
		t.Fatalf("token attacking=%v defender=%d, want unmarked (a remembered card is not a seat)", o.IsAttacking, o.Attacking)
	}
	if n := notesContaining(h, "TokenAttacking$ Remembered is not implemented"); n != 1 {
		t.Fatalf("%d degrade notes, want exactly 1; log=%+v", n, h.log)
	}
}

// TestTokenAttackingTriggeredAttackedTargetAttacksThatPlayer pins the
// `TriggeredAttackedTarget` spelling (4 corpus carriers): the trigger's
// captured attacked-player role is the defender.
func TestTokenAttackingTriggeredAttackedTargetAttacksThatPlayer(t *testing.T) {
	h, c := fixtureHostWithTokens(t)
	c.AttackedTarget = state.Target{Player: 1, IsPlayer: true}
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Token",
		Params: map[string]string{"TokenScript": "r_1_1_goblin", "TokenAttacking": "TriggeredAttackedTarget"}})
	bf := h.Game().Zone(state.ZBattlefield, c.Controller)
	if len(bf) != 1 {
		t.Fatalf("battlefield = %v, want 1 token", bf)
	}
	o := h.Game().Obj(bf[0])
	if !o.IsAttacking || o.Attacking != 1 {
		t.Fatalf("token attacking=%v defender=%d, want attacking seat 1", o.IsAttacking, o.Attacking)
	}
}

// TestTokenAttackingTriggeredDefenderAttacksTheDefender pins the
// `TriggeredDefender` spelling (the 4/4 Angel carrier): the defending player
// the firing Attacks trigger captured is the defender.
func TestTokenAttackingTriggeredDefenderAttacksTheDefender(t *testing.T) {
	h, c := fixtureHostWithTokens(t)
	c.DefendingPlayer = state.Target{Player: 1, IsPlayer: true}
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Token",
		Params: map[string]string{"TokenScript": "r_1_1_goblin", "TokenAttacking": "TriggeredDefender"}})
	bf := h.Game().Zone(state.ZBattlefield, c.Controller)
	if len(bf) != 1 {
		t.Fatalf("battlefield = %v, want 1 token", bf)
	}
	o := h.Game().Obj(bf[0])
	if !o.IsAttacking || o.Attacking != 1 {
		t.Fatalf("token attacking=%v defender=%d, want attacking seat 1", o.IsAttacking, o.Attacking)
	}
}

// TestTokenAttackingPlaneswalkerArmDegradesLoud is the "or a planeswalker they
// control" half a player-only defender cannot represent: Adeline's
// `RememberedPlayer & Valid Planeswalker.ControlledBy Remembered` attacks the
// remembered player and says the planeswalker arm is unmodelled -- exactly
// once, and never the generic does-not-attack degrade.
func TestTokenAttackingPlaneswalkerArmDegradesLoud(t *testing.T) {
	h, c := fixtureHostWithTokens(t)
	c.Remembered = []state.Target{{Player: 1, IsPlayer: true}}
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Token",
		Params: map[string]string{
			"TokenScript":    "r_1_1_goblin",
			"TokenTapped":    "True",
			"TokenAttacking": "RememberedPlayer & Valid Planeswalker.ControlledBy Remembered",
		}})
	bf := h.Game().Zone(state.ZBattlefield, c.Controller)
	if len(bf) != 1 {
		t.Fatalf("battlefield = %v, want 1 token", bf)
	}
	o := h.Game().Obj(bf[0])
	if !o.Tapped || !o.IsAttacking || o.Attacking != 1 {
		t.Fatalf("token tapped=%v attacking=%v defender=%d, want tapped, attacking the remembered seat 1", o.Tapped, o.IsAttacking, o.Attacking)
	}
	if n := notesContaining(h, "planeswalker arm is not implemented"); n != 1 {
		t.Fatalf("%d planeswalker-arm notes, want exactly 1; log=%+v", n, h.log)
	}
	if n := notesContaining(h, "is not implemented; the token enters but does not attack"); n != 0 {
		t.Fatalf("%d does-not-attack notes, want none (the player half resolved); log=%+v", n, h.log)
	}
}

// TestTokenAttackingPlaneswalkerOnlyArmDegradesLoud: with no player arm at
// all, the caller must not invent a seat from the planeswalker's controller --
// the token enters unmarked under the generic degrade.
func TestTokenAttackingPlaneswalkerOnlyArmDegradesLoud(t *testing.T) {
	h, c := fixtureHostWithTokens(t)
	c.Remembered = []state.Target{{Player: 1, IsPlayer: true}}
	Resolve(h, c, &cards.SA{Kind: "DB", API: "Token",
		Params: map[string]string{
			"TokenScript":    "r_1_1_goblin",
			"TokenAttacking": "Valid Planeswalker.ControlledBy Remembered",
		}})
	bf := h.Game().Zone(state.ZBattlefield, c.Controller)
	if len(bf) != 1 {
		t.Fatalf("battlefield = %v, want 1 token", bf)
	}
	o := h.Game().Obj(bf[0])
	if o.IsAttacking || o.Attacking != 0 {
		t.Fatalf("token attacking=%v defender=%d, want unmarked (no player arm)", o.IsAttacking, o.Attacking)
	}
	if n := notesContaining(h, "is not implemented; the token enters but does not attack"); n != 1 {
		t.Fatalf("%d does-not-attack notes, want exactly 1; log=%+v", n, h.log)
	}
}
