package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestEffectRememberObjectsSelectors pins the object selectors
// effectRemembered's switch previously left unresolved: an Effect line that
// names RememberedLKI, TriggeredAttackerLKICopy, TriggeredTargetLKICopy,
// ThisTargetedCard, Valid <filter> or DelayTriggerRemembered must capture the
// referent that selector names, NOT fall back to the source or the chosen
// targets, and a missing/stale referent must capture nothing.
//
// Each subtest anchors on a REAL corpus Effect-level SA so a corpus pin move
// that changes the spelling fails here, and each asserts its own
// precondition: the compared objects are on the battlefield and the answer
// genuinely differs from the source and the chosen target, so a vacuous board
// cannot pass the assertion.
func TestEffectRememberObjectsSelectors(t *testing.T) {
	// battlefield places one freshly parsed object on the battlefield and
	// returns its id.
	battlefield := func(h *fakeHost, p state.PlayerID, src string) state.ObjID {
		t.Helper()
		o := h.g.AddObject(mkCard(t, src), p)
		h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
		if got := h.g.Obj(o.ID); got == nil || got.Zone != state.ZBattlefield {
			t.Fatalf("precondition: %s not on the battlefield: %+v", src, got)
		}
		return o.ID
	}
	// assertCaptured requires got to hold exactly the ids in want, in order.
	assertCaptured := func(t *testing.T, got []state.ObjID, want ...state.ObjID) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("captured %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("captured %v, want %v", got, want)
			}
		}
	}

	t.Run("Valid", func(t *testing.T) {
		// Kill Switch's DBEffect: RememberObjects$ Valid Artifact.Other.
		card, sa := corpusSA(t, "Kill Switch", "DBEffect")
		if sa.Params["RememberObjects"] != "Valid Artifact.Other" {
			t.Fatalf("corpus selector changed: %q", sa.Params["RememberObjects"])
		}
		h := newHost(t, 2)
		srcObj := h.g.AddObject(card, 0)
		h.Emit(events.Event{Kind: events.MoveZone, Obj: srcObj.ID, From: state.ZLibrary, To: state.ZBattlefield})
		src := srcObj.ID
		other := battlefield(h, 0, "Name:OtherArtifact\nTypes:Artifact\nOracle:x\n")
		creature := battlefield(h, 0, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
		// Precondition: the source is itself an Artifact and the "other"
		// artifact differs from both the source and the unrelated creature,
		// so an over-broad or source-including capture fails the assertion.
		if src == other || other == creature {
			t.Fatal("precondition: compared objects must be distinct")
		}
		ctx := &Ctx{Source: src, Controller: 0}
		assertCaptured(t, effectRemembered(h, ctx, sa), other)
	})

	t.Run("ThisTargetedCard", func(t *testing.T) {
		// Kor Dirge's DBEffect: RememberObjects$ ThisTargetedCard.
		_, sa := corpusSA(t, "Kor Dirge", "DBEffect")
		if sa.Params["RememberObjects"] != "ThisTargetedCard" {
			t.Fatalf("corpus selector changed: %q", sa.Params["RememberObjects"])
		}
		h := newHost(t, 2)
		src := battlefield(h, 0, "Name:Src\nTypes:Enchantment\nOracle:x\n")
		chosen := battlefield(h, 0, "Name:Chosen\nTypes:Creature\nPT:1/1\nOracle:x\n")
		other := battlefield(h, 0, "Name:Other\nTypes:Creature\nPT:1/1\nOracle:x\n")
		if chosen == other {
			t.Fatal("precondition: chosen and other must differ")
		}
		// The Effect's OWN pre-asked target (PickedTargets) outranks the
		// resolution-level Ctx.Targets: Ctx.Targets deliberately holds a
		// different object, so a fallback to it is caught.
		ctx := &Ctx{Source: src, Controller: 0,
			Targets:       []state.Target{{Obj: other}},
			PickedTargets: []state.Target{{Obj: chosen}}}
		assertCaptured(t, effectRemembered(h, ctx, sa), chosen)
		// With no outstanding pre-ask the resolution's own targets are read.
		plain := &Ctx{Source: src, Controller: 0, Targets: []state.Target{{Obj: chosen}}}
		assertCaptured(t, effectRemembered(h, plain, sa), chosen)
	})

	t.Run("RememberedLKI", func(t *testing.T) {
		// Reflector Mage's DBEffect: RememberObjects$ RememberedLKI &
		// RememberedOwner (the player selector must not become an object).
		_, sa := corpusSA(t, "Reflector Mage", "DBEffect")
		if sa.Params["RememberObjects"] != "RememberedLKI & RememberedOwner" {
			t.Fatalf("corpus selector changed: %q", sa.Params["RememberObjects"])
		}
		h := newHost(t, 2)
		src := battlefield(h, 0, "Name:Src\nTypes:Creature\nPT:2/2\nOracle:x\n")
		remembered := battlefield(h, 0, "Name:Remembered\nTypes:Creature\nPT:1/1\nOracle:x\n")
		target := battlefield(h, 0, "Name:Target\nTypes:Creature\nPT:1/1\nOracle:x\n")
		if remembered == target {
			t.Fatal("precondition: remembered and target must differ")
		}
		// The LKI group reads the resolution's own memory (with no seeded
		// capture, rememberedExcludingCapture is Ctx.Remembered), NOT the
		// chosen targets.
		ctx := &Ctx{Source: src, Controller: 0,
			Remembered: []state.Target{{Obj: remembered}},
			Targets:    []state.Target{{Obj: target}}}
		assertCaptured(t, effectRemembered(h, ctx, sa), remembered)

		// The fire-time capture is EXCLUDED: a seeded source capture that
		// nothing explicitly re-remembered must resolve to nothing, not the
		// raw Remembered slice (Nurturing Pixie).
		captureOnly := &Ctx{Source: src, Controller: 0,
			Remembered: []state.Target{{Obj: src}},
			Captured:   []state.Target{{Obj: src}},
			Targets:    []state.Target{{Obj: target}}}
		assertCaptured(t, effectRemembered(h, captureOnly, sa))

		// An explicit later remember of a DIFFERENT object survives the
		// capture exclusion.
		mixed := &Ctx{Source: src, Controller: 0,
			Remembered: []state.Target{{Obj: src}, {Obj: remembered}},
			Captured:   []state.Target{{Obj: src}},
			Targets:    []state.Target{{Obj: target}}}
		assertCaptured(t, effectRemembered(h, mixed, sa), remembered)
	})

	t.Run("TriggeredAttackerLKICopy", func(t *testing.T) {
		// Writ of Passage's TrigUnblockable: an Attacks trigger's Effect.
		_, sa := corpusSA(t, "Writ of Passage", "TrigUnblockable")
		if sa.Params["RememberObjects"] != "TriggeredAttackerLKICopy" {
			t.Fatalf("corpus selector changed: %q", sa.Params["RememberObjects"])
		}
		h := newHost(t, 2)
		src := battlefield(h, 0, "Name:Src\nTypes:Enchantment Aura\nOracle:x\n")
		attacker := battlefield(h, 0, "Name:Attacker\nTypes:Creature\nPT:2/2\nOracle:x\n")
		target := battlefield(h, 0, "Name:Target\nTypes:Creature\nPT:1/1\nOracle:x\n")
		if attacker == target {
			t.Fatal("precondition: attacker and target must differ")
		}
		// The trigger captured the attacker; the chosen target list names a
		// DIFFERENT object, so a fallback to it is caught.
		ctx := &Ctx{Source: src, Controller: 0,
			Remembered:    []state.Target{{Obj: attacker}},
			Targets:       []state.Target{{Obj: target}},
			PickedTargets: []state.Target{{Obj: target}}}
		assertCaptured(t, effectRemembered(h, ctx, sa), attacker)

		// No captured attacker: nothing, never the source or the targets.
		empty := &Ctx{Source: src, Controller: 0, Targets: []state.Target{{Obj: target}}}
		assertCaptured(t, effectRemembered(h, empty, sa))

		// A stale id (no longer in the game) is not registered as a guessed
		// object.
		stale := &Ctx{Source: src, Controller: 0, Remembered: []state.Target{{Obj: state.ObjID(1 << 30)}}}
		assertCaptured(t, effectRemembered(h, stale, sa))
	})

	t.Run("TriggeredTargetLKICopy", func(t *testing.T) {
		// Neko-Te's DBNekoTe: a DamageDone trigger's Effect.
		_, sa := corpusSA(t, "Neko-Te", "DBNekoTe")
		if sa.Params["RememberObjects"] != "TriggeredTargetLKICopy" {
			t.Fatalf("corpus selector changed: %q", sa.Params["RememberObjects"])
		}
		h := newHost(t, 2)
		src := battlefield(h, 0, "Name:Src\nTypes:Artifact Equipment\nOracle:x\n")
		damaged := battlefield(h, 0, "Name:Damaged\nTypes:Creature\nPT:1/1\nOracle:x\n")
		bearer := battlefield(h, 0, "Name:Bearer\nTypes:Creature\nPT:1/1\nOracle:x\n")
		target := battlefield(h, 0, "Name:Target\nTypes:Creature\nPT:1/1\nOracle:x\n")
		if damaged == target {
			t.Fatal("precondition: damaged and target must differ")
		}
		// The spell-style fallback: no Attached bearer role, so the fired
		// DamageDone trigger's Remembered (the damaged creature) is read.
		ctx := &Ctx{Source: src, Controller: 0,
			Remembered: []state.Target{{Obj: damaged}},
			Targets:    []state.Target{{Obj: target}}}
		assertCaptured(t, effectRemembered(h, ctx, sa), damaged)

		// The Attached bearer role is preferred when the trigger captured one
		// (Enormous Energy Blade's shape).
		attached := &Ctx{Source: src, Controller: 0,
			Remembered:     []state.Target{{Obj: damaged}},
			TriggerContext: TriggerContext{TriggerBearer: bearer}}
		assertCaptured(t, effectRemembered(h, attached, sa), bearer)
	})

	t.Run("DelayTriggerRemembered", func(t *testing.T) {
		// Wall of Stolen Identity's DBEffect.
		_, sa := corpusSA(t, "Wall of Stolen Identity", "DBEffect")
		if sa.Params["RememberObjects"] != "DelayTriggerRemembered" {
			t.Fatalf("corpus selector changed: %q", sa.Params["RememberObjects"])
		}
		h := newHost(t, 2)
		src := battlefield(h, 0, "Name:Src\nTypes:Creature\nPT:0/0\nOracle:x\n")
		captured := battlefield(h, 0, "Name:Captured\nTypes:Creature\nPT:2/2\nOracle:x\n")
		other := battlefield(h, 0, "Name:Other\nTypes:Creature\nPT:1/1\nOracle:x\n")
		if captured == other {
			t.Fatal("precondition: captured and other must differ")
		}
		// The delayed registration's own capture outranks the fired event's
		// Ctx.Remembered (which names a different object here).
		ctx := &Ctx{Source: src, Controller: 0,
			TriggerContext: TriggerContext{DelayedRemembered: []state.Target{{Obj: captured}}},
			Remembered:     []state.Target{{Obj: other}}}
		assertCaptured(t, effectRemembered(h, ctx, sa), captured)

		// A player captured by the delayed registration stays a player: it is
		// never coerced into an object id.
		withPlayer := &Ctx{Source: src, Controller: 0,
			TriggerContext: TriggerContext{DelayedRemembered: []state.Target{{Player: 1, IsPlayer: true}, {Obj: captured}}}}
		assertCaptured(t, effectRemembered(h, withPlayer, sa), captured)
	})
}
