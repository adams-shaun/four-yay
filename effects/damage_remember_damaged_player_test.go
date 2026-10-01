package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Task agent-20261001T013350Z-826f1c20: a DealDamage with RememberDamaged$
// True that damages a PLAYER must remember that player on both halves of the
// remembered state -- the resolution-local Ctx.Remembered set AND the source
// object's event-backed persistent list -- the same way rememberPlayerBothHalves
// already records RememberDiscardingPlayers$ (Professor Onyx, Snort). Before
// the fix the four player-recipient emit arms in effects/damage.go skipped the
// remember write entirely, so Screaming Nemesis's follow-up DBEffect
// RememberObjects$ Player.IsRemembered captured nobody.

// playerRememberCount counts {Player: p, IsPlayer: true} entries in ts.
func playerRememberCount(ts []state.Target, p state.PlayerID) int {
	n := 0
	for _, t := range ts {
		if t.IsPlayer && t.Player == p {
			n++
		}
	}
	return n
}

// persistentRememberedPlayers reads the source object's event-backed
// remembered list back through a LATER, independent resolution's Defined$
// Player.IsRemembered (the persistent-list read definedSpec's Player.IsRemembered
// arm makes) -- the way Screaming Nemesis's registered restriction resolves
// the clause after the reflecting resolution is gone.
func persistentRememberedPlayers(t *testing.T, h *fakeHost, src state.ObjID) []state.PlayerID {
	t.Helper()
	if h.g.Obj(src) == nil {
		t.Fatal("precondition: the source object is gone")
	}
	return definedPlayers(h, &Ctx{Source: src, Controller: 0},
		sa(t, "SP$ Draw | Defined$ Player.IsRemembered"))
}

// rememberedChooseIDs counts the remembered Choose events the log carries and
// asserts none of them encodes a bare object id where a PlayerRef belongs
// (eventRemember(h, c, 0) from an unguarded player entry is the bug class).
func rememberedChooseIDs(t *testing.T, h *fakeHost) int {
	t.Helper()
	n := 0
	for _, ev := range h.log {
		if ev.Kind == events.Choose && ev.Counter == "remembered" {
			for _, id := range ev.IDs {
				if id == 0 {
					t.Fatalf("a remembered Choose event carries id 0 (garbage encoding): %+v", ev)
				}
			}
			n++
		}
	}
	return n
}

// TestDealDamageRememberDamagedPlayerBothHalves is the single-recipient arm
// (Screaming Nemesis's exact shape): DealDamage 3 to Player.Opponent with
// RememberDamaged$ True must record the damaged player on the transient
// Ctx.Remembered set exactly once AND on the source's persistent list, and a
// later independent resolution's Player.IsRemembered must find them.
func TestDealDamageRememberDamagedPlayerBothHalves(t *testing.T) {
	h := newHost(t, 2)
	src := battlefield(t, h, damageCarrier)
	h.g.Players[1].Life = 17
	c := &Ctx{Source: src, Controller: 0}
	if h.g.Players[1].Life != 17 {
		t.Fatalf("precondition: seat 1 life = %d, want 17", h.g.Players[1].Life)
	}
	if len(c.Remembered) != 0 {
		t.Fatalf("precondition: Ctx.Remembered starts with %d entries, want 0", len(c.Remembered))
	}
	if o := h.g.Obj(src); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: source zone = %v, want on the battlefield", o)
	}
	if len(h.g.Obj(src).Remembered) != 0 {
		t.Fatalf("precondition: source persistent remembered list = %v, want empty", h.g.Obj(src).Remembered)
	}

	Resolve(h, c, sa(t, "SP$ DealDamage | Defined$ Player.Opponent | NumDmg$ 3 | RememberDamaged$ True"))

	if h.g.Players[1].Life != 14 {
		t.Fatalf("seat 1 life after the deal = %d, want 14 (the damage must actually land)", h.g.Players[1].Life)
	}
	if got := playerRememberCount(c.Remembered, 1); got != 1 {
		t.Fatalf("Ctx.Remembered holds player 1 %d time(s), want exactly 1: %v", got, c.Remembered)
	}
	if got := playerRememberCount(h.g.Obj(src).Remembered, 1); got != 1 {
		t.Fatalf("source persistent list holds player 1 %d time(s), want exactly 1: %v",
			got, h.g.Obj(src).Remembered)
	}
	if n := rememberedChooseIDs(t, h); n != 1 {
		t.Fatalf("the deal emitted %d remembered Choose events, want exactly 1", n)
	}
	// A later, independent resolution reads the player off the persistent list.
	if got := persistentRememberedPlayers(t, h, src); len(got) != 1 || got[0] != 1 {
		t.Fatalf("a later ability's Defined$ Player.IsRemembered read %v, want [1]", got)
	}
}

// TestDealDamageRememberDamagedPlayerSingleVsObject covers a mixed recipient
// set in ONE call -- a player and an opponent creature both damaged by the
// same DealDamage. Both must be remembered; the player halves dedup across a
// second deal of the same call, the object entries do not (their arm appends
// raw), which is exactly the asymmetry the fix introduces for players.
func TestDealDamageRememberDamagedPlayerSingleVsObject(t *testing.T) {
	h := newHost(t, 2)
	src := battlefield(t, h, damageCarrier)
	opp := h.g.AddObject(mkCard(t, "Name:Opp Bear\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	h.g.Obj(opp.ID).Zone = state.ZBattlefield
	h.g.Players[1].Life = 20
	c := &Ctx{Source: src, Controller: 0, Targets: []state.Target{
		{Player: 1, IsPlayer: true}, {Obj: opp.ID},
	}, TargetsOffered: true}
	if h.g.Obj(opp.ID) == nil || h.g.Obj(opp.ID).Zone != state.ZBattlefield {
		t.Fatal("precondition: the opponent creature is not on the battlefield")
	}
	if len(c.Remembered) != 0 {
		t.Fatalf("precondition: Ctx.Remembered starts with %d entries, want 0", len(c.Remembered))
	}

	line := "SP$ DealDamage | ValidTgts$ Any | NumDmg$ 2 | RememberDamaged$ True"
	Resolve(h, c, sa(t, line))
	if h.g.Players[1].Life != 18 {
		t.Fatalf("seat 1 life = %d, want 18 (the player took 2)", h.g.Players[1].Life)
	}
	if got := h.g.Obj(opp.ID).Damage; got != 2 {
		t.Fatalf("opponent creature damage = %d, want 2 (the object took 2)", got)
	}
	if got := playerRememberCount(c.Remembered, 1); got != 1 {
		t.Fatalf("Ctx.Remembered holds player 1 %d time(s), want 1: %v", got, c.Remembered)
	}
	if got := playerRememberCount(h.g.Obj(src).Remembered, 1); got != 1 {
		t.Fatalf("source persistent list holds player 1 %d time(s), want 1: %v",
			got, h.g.Obj(src).Remembered)
	}
	objSeen := 0
	for _, tg := range c.Remembered {
		if !tg.IsPlayer && tg.Obj == opp.ID {
			objSeen++
		}
	}
	if objSeen != 1 {
		t.Fatalf("Ctx.Remembered holds the damaged object %d time(s), want 1: %v", objSeen, c.Remembered)
	}

	// A second identical deal: the player halves dedup on both sides, the
	// object arm appends raw.
	Resolve(h, c, sa(t, line))
	if got := playerRememberCount(c.Remembered, 1); got != 1 {
		t.Fatalf("after a second deal, Ctx.Remembered holds player 1 %d time(s), want 1 (dedup): %v",
			got, c.Remembered)
	}
	if got := playerRememberCount(h.g.Obj(src).Remembered, 1); got != 1 {
		t.Fatalf("after a second deal, the persistent list holds player 1 %d time(s), want 1 (dedup): %v",
			got, h.g.Obj(src).Remembered)
	}
}

// TestDealDamageRememberDamagedPlayerDivided covers the DividedAsYouChoose$
// arm: the player recipient of its share is remembered the same way.
func TestDealDamageRememberDamagedPlayerDivided(t *testing.T) {
	h := newHost(t, 3)
	src := battlefield(t, h, damageCarrier)
	h.g.Players[1].Life = 20
	h.g.Players[2].Life = 20
	c := &Ctx{Source: src, Controller: 0, Targets: []state.Target{
		{Player: 1, IsPlayer: true}, {Player: 2, IsPlayer: true},
	}, TargetsOffered: true}
	if h.g.Players[1].Life != 20 || h.g.Players[2].Life != 20 {
		t.Fatal("precondition: the two recipients do not start at 20 life")
	}
	if len(c.Remembered) != 0 {
		t.Fatalf("precondition: Ctx.Remembered starts with %d entries, want 0", len(c.Remembered))
	}

	// 3 divided round-robin over two chosen players in target order: 2 then 1.
	Resolve(h, c, sa(t, "SP$ DealDamage | ValidTgts$ Player | DividedAsYouChoose$ 3 | RememberDamaged$ True"))
	if h.g.Players[1].Life != 18 {
		t.Fatalf("seat 1 life = %d, want 18 (round-robin's first share is 2)", h.g.Players[1].Life)
	}
	if h.g.Players[2].Life != 19 {
		t.Fatalf("seat 2 life = %d, want 19 (round-robin's second share is 1)", h.g.Players[2].Life)
	}
	for _, p := range []state.PlayerID{1, 2} {
		if got := playerRememberCount(c.Remembered, p); got != 1 {
			t.Fatalf("Ctx.Remembered holds player %d %d time(s), want 1: %v", p, got, c.Remembered)
		}
		if got := playerRememberCount(h.g.Obj(src).Remembered, p); got != 1 {
			t.Fatalf("source persistent list holds player %d %d time(s), want 1: %v",
				p, got, h.g.Obj(src).Remembered)
		}
	}
}

// TestDealDamageRememberDamagedPlayerMultiSource covers the emitFromEachSource
// arm (Missy's "each artifact creature you control deals 1 damage to that
// opponent" shape): two source creatures each deal 1 to the opponent, and the
// damaged player is remembered ONCE despite two emissions.
func TestDealDamageRememberDamagedPlayerMultiSource(t *testing.T) {
	h := newHost(t, 2)
	src := battlefield(t, h, damageCarrier)
	s1 := h.g.AddObject(mkCard(t, "Name:Damager One\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
	h.g.Obj(s1.ID).Zone = state.ZBattlefield
	s2 := h.g.AddObject(mkCard(t, "Name:Damager Two\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
	h.g.Obj(s2.ID).Zone = state.ZBattlefield
	h.g.Players[1].Life = 20
	c := &Ctx{Source: src, Controller: 0,
		Remembered: []state.Target{{Obj: s1.ID}, {Obj: s2.ID}}}
	if h.g.Obj(s1.ID) == nil || h.g.Obj(s2.ID) == nil ||
		h.g.Obj(s1.ID).Zone != state.ZBattlefield || h.g.Obj(s2.ID).Zone != state.ZBattlefield {
		t.Fatal("precondition: the two DamageSource$ creatures are not both on the battlefield")
	}
	if len(c.Remembered) != 2 || c.Remembered[0].Obj != s1.ID || c.Remembered[1].Obj != s2.ID {
		t.Fatalf("precondition: the DamageSource$ Remembered pool = %v, want exactly the two source creatures", c.Remembered)
	}

	Resolve(h, c, sa(t, "SP$ DealDamage | DamageSource$ Remembered |"+
		" Defined$ Player.Opponent | NumDmg$ 1 | RememberDamaged$ True"))
	if h.g.Players[1].Life != 18 {
		t.Fatalf("seat 1 life = %d, want 18 (each of the two sources dealt 1)", h.g.Players[1].Life)
	}
	if got := playerRememberCount(c.Remembered, 1); got != 1 {
		t.Fatalf("Ctx.Remembered holds player 1 %d time(s), want exactly 1 across both emissions: %v",
			got, c.Remembered)
	}
	if got := playerRememberCount(h.g.Obj(src).Remembered, 1); got != 1 {
		t.Fatalf("source persistent list holds player 1 %d time(s), want exactly 1: %v",
			got, h.g.Obj(src).Remembered)
	}
}

// TestDamageResolveRememberDamagedPlayer covers the DamageResolve flush arm
// (Serpentine Spike's shape): the marking DealDamage defers, the DB$
// DamageResolve flush carries RememberDamaged$ True, and the flushed PLAYER
// mark is remembered on both halves with a correct PlayerRef encoding (a
// bare eventRemember(h, c, t.Obj) from a player entry would emit id 0).
func TestDamageResolveRememberDamagedPlayer(t *testing.T) {
	h := newHost(t, 2)
	src := battlefield(t, h, damageCarrier)
	h.g.Players[1].Life = 20
	c := &Ctx{Source: src, Controller: 0, Targets: []state.Target{{Player: 1, IsPlayer: true}}}
	if len(c.PendingDamage) != 0 {
		t.Fatalf("precondition: Ctx starts with %d pending marks, want 0", len(c.PendingDamage))
	}
	if len(c.Remembered) != 0 {
		t.Fatalf("precondition: Ctx.Remembered starts with %d entries, want 0", len(c.Remembered))
	}

	Resolve(h, c, sa(t, "SP$ DealDamage | Defined$ Player.Opponent | NumDmg$ 2 | DamageMap$ True"))
	if len(c.Remembered) != 0 {
		t.Fatalf("the MARKING call remembered %v, want nothing (DamageMap defers the whole rider)", c.Remembered)
	}
	Resolve(h, c, sa(t, "DB$ DamageResolve | RememberDamaged$ True"))
	if h.g.Players[1].Life != 18 {
		t.Fatalf("seat 1 life after the flush = %d, want 18", h.g.Players[1].Life)
	}
	if got := playerRememberCount(c.Remembered, 1); got != 1 {
		t.Fatalf("after the flush, Ctx.Remembered holds player 1 %d time(s), want 1: %v", got, c.Remembered)
	}
	if got := playerRememberCount(h.g.Obj(src).Remembered, 1); got != 1 {
		t.Fatalf("after the flush, the persistent list holds player 1 %d time(s), want 1: %v",
			got, h.g.Obj(src).Remembered)
	}
	if n := rememberedChooseIDs(t, h); n != 1 {
		t.Fatalf("the flush emitted %d remembered Choose events, want exactly 1", n)
	}
	if got := persistentRememberedPlayers(t, h, src); len(got) != 1 || got[0] != 1 {
		t.Fatalf("a later ability's Defined$ Player.IsRemembered read %v, want [1]", got)
	}
}

// TestEffectRememberedPlayersPlayerIsRemembered pins the RememberObjects$
// selector: an Effect created INSIDE the resolving resolution reads its
// remembered player out of the live Ctx.Remembered set; an empty set yields
// nobody (fail closed), never a guess from the persistent list.
func TestEffectRememberedPlayersPlayerIsRemembered(t *testing.T) {
	h := newHost(t, 2)
	src := battlefield(t, h, damageCarrier)
	s := sa(t, "DB$ Effect | RememberObjects$ Player.IsRemembered")
	c := &Ctx{Source: src, Controller: 0, Remembered: []state.Target{{Player: 1, IsPlayer: true}}}
	got := effectRememberedPlayers(h, c, s)
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("effectRememberedPlayers(Player.IsRemembered) = %v, want [1] from the live Ctx.Remembered", got)
	}

	empty := effectRememberedPlayers(h, &Ctx{Source: src, Controller: 0}, s)
	if len(empty) != 0 {
		t.Fatalf("effectRememberedPlayers with an empty remembered set = %v, want none (fail closed)", empty)
	}
}
