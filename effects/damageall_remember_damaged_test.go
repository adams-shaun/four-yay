package effects

import (
	"fmt"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// damageAllCreature puts a synthetic N/N creature on the battlefield for
// owner, using mkCard so no corpus file is committed for these tests.
func damageAllCreature(t *testing.T, h *fakeHost, name string, owner state.PlayerID, power, toughness int) *state.Object {
	t.Helper()
	card := mkCard(t, "Name:"+name+"\nTypes:Creature\nPT:"+
		fmt.Sprint(power)+"/"+fmt.Sprint(toughness)+"\nOracle:x\n")
	o := h.g.AddObject(card, owner)
	o.Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, owner, append(h.g.Zone(state.ZBattlefield, owner), o.ID))
	return o
}

// damageAllSource puts a synthetic non-creature permanent (so a
// ValidCards$ Creature sweep never hits it) on owner's battlefield.
func damageAllSource(t *testing.T, h *fakeHost, name string, owner state.PlayerID) *state.Object {
	t.Helper()
	card := mkCard(t, "Name:"+name+"\nTypes:Enchantment\nOracle:x\n")
	o := h.g.AddObject(card, owner)
	o.Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, owner, append(h.g.Zone(state.ZBattlefield, owner), o.ID))
	return o
}

func rememberedObjects(h *fakeHost, src state.ObjID) int {
	o := h.g.Obj(src)
	if o == nil {
		return 0
	}
	n := 0
	for _, tg := range o.Remembered {
		if !tg.IsPlayer {
			n++
		}
	}
	return n
}

func chooseRememberedEvents(h *fakeHost, src state.ObjID) [][]state.ObjID {
	var out [][]state.ObjID
	for _, e := range h.log {
		if e.Kind == events.Choose && e.Counter == "remembered" && e.Obj == src {
			out = append(out, e.IDs)
		}
	}
	return out
}

// TestDamageAllRememberDamagedObjects closes the object half of the defect:
// a DamageAll sweep with RememberDamaged$ True must write BOTH the
// resolution-local Ctx.Remembered set and the source's event-backed
// persistent list (aggravate, chaos_balor, anger_of_the_gods and the rest of
// the ten-line population read one or the other). Before the fix the
// parameter was completely unread: the creatures took the damage but no
// remember entry and no Choose event appeared.
func TestDamageAllRememberDamagedObjects(t *testing.T) {
	h := newHost(t, 2)
	src := damageAllSource(t, h, "Source", 0)
	bear := damageAllCreature(t, h, "Bear", 1, 2, 2)
	elf := damageAllCreature(t, h, "Elf", 1, 1, 1)

	// Preconditions: both victims start undamaged and the source has no
	// persistent memory, or "the sweep remembered them" would prove nothing.
	if bear.Damage != 0 || elf.Damage != 0 {
		t.Fatalf("precondition: victims start damaged: %d/%d", bear.Damage, elf.Damage)
	}
	if len(h.g.Obj(src.ID).Remembered) != 0 {
		t.Fatalf("precondition: source persistent list is not empty: %v", h.g.Obj(src.ID).Remembered)
	}

	s := sa(t, "SP$ DamageAll | ValidCards$ Creature | NumDmg$ 1 | RememberDamaged$ True")
	c := &Ctx{Source: src.ID, Controller: 0}
	Resolve(h, c, s)

	// The damage itself landed (the fix must not stop the sweep).
	if h.g.Obj(bear.ID).Damage != 1 || h.g.Obj(elf.ID).Damage != 1 {
		t.Fatalf("the sweep must still damage every creature: bear=%d elf=%d, want 1/1",
			h.g.Obj(bear.ID).Damage, h.g.Obj(elf.ID).Damage)
	}

	// Resolution-local half: each damaged object exactly once.
	for _, want := range []state.ObjID{bear.ID, elf.ID} {
		n := 0
		for _, tg := range c.Remembered {
			if !tg.IsPlayer && tg.Obj == want {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("Ctx.Remembered held object %d %d time(s), want exactly 1: %v", want, n, c.Remembered)
		}
	}

	// Persistent half: the source's event-backed list carries both objects.
	if got := rememberedObjects(h, src.ID); got != 2 {
		t.Fatalf("source persistent remembered list held %d object(s), want 2: %v", got, h.g.Obj(src.ID).Remembered)
	}

	// The persistent write is event-backed: one remembered Choose naming
	// each object.
	evs := chooseRememberedEvents(h, src.ID)
	if len(evs) != 2 {
		t.Fatalf("want exactly 2 remembered Choose events, got %d: %v", len(evs), evs)
	}
	seen := map[state.ObjID]bool{}
	for _, ids := range evs {
		if len(ids) != 1 {
			t.Fatalf("a remembered Choose must name exactly one id, got %v", ids)
		}
		seen[ids[0]] = true
	}
	if !seen[bear.ID] || !seen[elf.ID] {
		t.Fatalf("remembered Choose events named %v, want both %d and %d", seen, bear.ID, elf.ID)
	}
}

// TestDamageAllRememberDamagedPlayers closes the player half of the defect
// (flamebreak's shape): a DamageAll player sweep with RememberDamaged$ True
// must record the player in Ctx.Remembered as a player target AND persist it
// through the PlayerRef encoding -- never eventRemember(h, c, 0), which would
// emit a Choose with a garbage zero id. It rides the mixed board so the
// object and player halves are proven to coexist.
func TestDamageAllRememberDamagedPlayers(t *testing.T) {
	h := newHost(t, 2)
	src := damageAllSource(t, h, "Source", 0)
	bear := damageAllCreature(t, h, "Bear", 1, 2, 2)

	if h.g.Players[1].Life != 20 {
		t.Fatalf("precondition: seat 1 life is %d, want 20", h.g.Players[1].Life)
	}
	if len(h.g.Obj(src.ID).Remembered) != 0 {
		t.Fatalf("precondition: source persistent list is not empty: %v", h.g.Obj(src.ID).Remembered)
	}

	// ValidPlayers$ Player sweeps every living player (both seats); only the
	// opponent's creature matches ValidCards$, so the object and player
	// halves exercise different victims.
	s := sa(t, "SP$ DamageAll | ValidCards$ Creature | NumDmg$ 1 | ValidPlayers$ Player | RememberDamaged$ True")
	c := &Ctx{Source: src.ID, Controller: 0}
	Resolve(h, c, s)

	if h.g.Players[1].Life != 19 {
		t.Fatalf("seat 1 must take 1 damage (life %d, want 19)", h.g.Players[1].Life)
	}
	if h.g.Obj(bear.ID).Damage != 1 {
		t.Fatalf("the creature half must still land (damage %d, want 1)", h.g.Obj(bear.ID).Damage)
	}

	// Resolution-local half: each swept player exactly once, as a player.
	for _, p := range []state.PlayerID{0, 1} {
		n := 0
		for _, tg := range c.Remembered {
			if tg.IsPlayer && tg.Player == p {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("Ctx.Remembered recorded player %d %d time(s), want exactly 1: %v", p, n, c.Remembered)
		}
	}

	// Persistent half: the source's list decodes each player through
	// PlayerRef -- a `{Player: p, IsPlayer: true}` entry, never a zero object.
	persistent := h.g.Obj(src.ID).Remembered
	for _, p := range []state.PlayerID{0, 1} {
		n := 0
		for _, tg := range persistent {
			if tg.IsPlayer && tg.Player == p {
				n++
			}
			if !tg.IsPlayer && tg.Obj == 0 {
				t.Fatalf("the persistent list gained a zero-object entry %v (eventRemember(h,c,0) regression)", tg)
			}
		}
		if n != 1 {
			t.Fatalf("source persistent list recorded player %d %d time(s), want exactly 1: %v", p, n, persistent)
		}
	}

	// Every remembered Choose that names a player must name a real PlayerRef
	// (the object half legitimately emits a real object id), and one must
	// decode to each swept player.
	evs := chooseRememberedEvents(h, src.ID)
	var playerIDs []state.ObjID
	for _, ids := range evs {
		if len(ids) != 1 {
			t.Fatalf("a remembered Choose must name exactly one id, got %v", ids)
		}
		if p, ok := ids[0].PlayerRef(); ok {
			if ids[0] != state.PlayerRef(p) {
				t.Fatalf("decoded player %d does not re-encode: %d", p, ids[0])
			}
			playerIDs = append(playerIDs, ids[0])
		}
	}
	for _, p := range []state.PlayerID{0, 1} {
		want := state.PlayerRef(p)
		found := false
		for _, id := range playerIDs {
			if id == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("no remembered Choose decoded to player %d (want %d), got %v", p, want, evs)
		}
	}
}

// TestDamageAllReplaceDyingDefinedRegistersExile is the direct consumer of
// the remembered set on the six `DamageAll | ReplaceDyingDefined$ Remembered`
// lines: the sweep must register the battlefield-to-graveyard-to-exile Moved
// replacement over the objects it damaged, passing the remembered set to the
// matcher. Before the fix effDamageAll never called registerReplaceDying, so
// the exile was never registered.
func TestDamageAllReplaceDyingDefinedRegistersExile(t *testing.T) {
	h := newHost(t, 2)
	src := damageAllSource(t, h, "Source", 0)
	bear := damageAllCreature(t, h, "Bear", 1, 2, 2)

	if len(h.continuous) != 0 {
		t.Fatalf("precondition: host already carries %d continuous effect(s)", len(h.continuous))
	}

	s := sa(t, "SP$ DamageAll | ValidCards$ Creature | NumDmg$ 3 | RememberDamaged$ True"+
		" | ReplaceDyingDefined$ Remembered")
	c := &Ctx{Source: src.ID, Controller: 0}
	Resolve(h, c, s)

	if h.g.Obj(bear.ID).Damage != 3 {
		t.Fatalf("precondition: the bear must take 3 damage, got %d", h.g.Obj(bear.ID).Damage)
	}

	n := 0
	for _, ce := range h.continuous {
		if ce.ReplacementEvent != "Moved" {
			continue
		}
		n++
		if !strings.Contains(ce.ReplacementBody, "Destination$ Exile") {
			t.Fatalf("the Moved replacement body must exile the replaced card, got %q", ce.ReplacementBody)
		}
		if ce.ReplacementParams["ValidCard"] != "Card.IsRemembered" {
			t.Fatalf("the Moved replacement must match Card.IsRemembered, got %q", ce.ReplacementParams["ValidCard"])
		}
		if len(ce.Remembered) != 1 || ce.Remembered[0] != bear.ID {
			t.Fatalf("the replacement remembered %v, want just the bear %d", ce.Remembered, bear.ID)
		}
	}
	if n != 1 {
		t.Fatalf("want exactly one Moved replacement registered, got %d", n)
	}
}

// TestDamageAllWithoutRememberDamagedIsInert is the control: a DamageAll with
// no RememberDamaged$ remembers nothing and registers no replacement, so the
// fix does not over-fire. It asserts the sweep itself ran (the damage landed
// and an object was harmed), so a vacuous no-op cannot pass.
func TestDamageAllWithoutRememberDamagedIsInert(t *testing.T) {
	h := newHost(t, 2)
	src := damageAllSource(t, h, "Source", 0)
	bear := damageAllCreature(t, h, "Bear", 1, 2, 2)

	s := sa(t, "SP$ DamageAll | ValidCards$ Creature | NumDmg$ 1")
	c := &Ctx{Source: src.ID, Controller: 0}
	Resolve(h, c, s)

	if h.g.Obj(bear.ID).Damage != 1 {
		t.Fatalf("precondition: the sweep must have damaged the bear (got %d, want 1)", h.g.Obj(bear.ID).Damage)
	}
	if len(c.Remembered) != 0 {
		t.Fatalf("no RememberDamaged$: Ctx.Remembered must stay empty, got %v", c.Remembered)
	}
	if got := len(h.g.Obj(src.ID).Remembered); got != 0 {
		t.Fatalf("no RememberDamaged$: the persistent list must stay empty, got %v", h.g.Obj(src.ID).Remembered)
	}
	if evs := chooseRememberedEvents(h, src.ID); len(evs) != 0 {
		t.Fatalf("no RememberDamaged$: no remembered Choose may be emitted, got %v", evs)
	}
	if len(h.continuous) != 0 {
		t.Fatalf("no ReplaceDyingDefined$: no replacement may be registered, got %v", h.continuous)
	}
}
