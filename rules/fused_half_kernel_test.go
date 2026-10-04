package rules

// Kernel-era restorations of the fused_half_test.go behaviour tests the W3
// legacy removal deleted: the same scenarios, driven through the resolution
// kernel (rules/resolve) instead of the suspend/resume protocol.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestFusedFarAwayAlternateHalfSuspensionAsksOnlyTheSacrifice pins the
// last-half suspension tail: a fused Far // Away whose ALTERNATE half (Away)
// suspends on its CR 701.21a sacrifice ask completes after that one ask --
// the spell leaves the stack, the chosen creature is sacrificed and Far's
// creature is in its owner's hand -- with NO spurious "Choose target"
// (ResumeKind "tgts") re-posed between the sacrifice answer and the
// completion, which the generic completion chain's fresh ctx used to trigger
// by re-deriving the object's flat target list for the re-entered half.
func TestFusedFarAwayAlternateHalfSuspensionAsksOnlyTheSacrifice(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, id := fusedFarAwayEngine(t, reg, 9412)

	bearA := splitMoveFromHandOrLibrary(t, e, 0, "Grizzly Bears")
	bearB := splitMoveFromHandOrLibrary(t, e, 1, "Grizzly Bears")
	bearC := splitMoveFromHandOrLibrary(t, e, 1, "Grizzly Bears")
	addMana(t, e, 0, "UUUUB")
	fuse := splitOption(t, e, id, "fuse")
	if fuse == nil {
		t.Fatalf("fused offer missing for Far // Away: %+v", castOptions(t, e))
	}
	submitChoices(t, e, fuse.Index)

	// Stage 0: Far's creature target -- seat 0's own bearA (returns to its
	// owner's hand), so both of seat 1's bears stay eligible when Away's
	// sacrifice ask runs and the ask is a real choice.
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("fused target stage 0: pending=%+v, want target", d)
	}
	far := -1
	for _, o := range d.Options {
		if o.Obj == bearA {
			far = o.Index
		}
	}
	if far < 0 {
		t.Fatalf("stage 0 offered no option for bear %d: %+v", bearA, d.Options)
	}
	submitChoices(t, e, far)

	// Stage 1: Away's player target -- seat 1 (the sacrificer).
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("fused target stage 1: pending=%+v, want target", d)
	}
	p1 := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			p1 = o.Index
		}
	}
	if p1 < 0 {
		t.Fatalf("stage 1 offered no player option for seat 1: %+v", d.Options)
	}
	submitChoices(t, e, p1)

	// Pass priority round the table so the fused spell resolves; the front
	// half (Far) returns bearA to hand, and the alternate half (Away)
	// suspends on its sacrifice ask posed to seat 1.
	for i := 0; i < 6; i++ {
		d = e.Pending()
		if d == nil {
			t.Fatal("no pending decision while waiting for the fused resolution")
		}
		if d.Kind != decision.KPriority {
			break
		}
		pass := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				pass = o.Index
			}
		}
		if pass < 0 {
			t.Fatalf("priority with no pass option: %+v", d.Options)
		}
		submitChoices(t, e, pass)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "sacrifice" || d.Player != 1 {
		t.Fatalf("sacrifice ask pending=%+v, want seat 1's KChoose sacrifice", d)
	}
	sac := -1
	for _, o := range d.Options {
		if o.Obj == bearB {
			sac = o.Index
		}
	}
	if sac < 0 {
		t.Fatalf("sacrifice ask offered no option for bear %d: %+v", bearB, d.Options)
	}
	_ = bearC
	submitChoices(t, e, sac)

	// The answered sacrifice must complete the fused spell with NO further
	// ask: the next pending decision is a priority (not the spurious
	// "Choose target" the generic completion used to pose).
	if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
		t.Fatalf("pending after the answered sacrifice = %+v (%s), want priority",
			d, d.ResumeKind)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack not empty after the answered sacrifice: %+v", e.G.Stack)
	}
	if z := e.G.Obj(bearA).Zone; z != state.ZHand {
		t.Fatalf("Far did not run: bearA's zone = %s, want hand", z)
	}
	if z := e.G.Obj(bearB).Zone; z != state.ZGraveyard {
		t.Fatalf("Away did not run: bearB's zone = %s, want graveyard", z)
	}
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Fatalf("resolved fused spell zone=%s, want graveyard", z)
	}
	replayCheck(t, e, cfg)
}

// TestFusedFleshBloodSubAbilityReadsItsOwnHalfTargets is review round 3's
// MAJOR pin: a SUB-ABILITY of a fused half must read the half's own targets
// as its parent list, never the stack object's whole flat target list. On the
// real corpus card Flesh // Blood, Flesh's DBPutCounter SubAbility puts
// X +1/+1 counters where X = ParentTargeted$CardPower -- the power of the
// graveyard creature Flesh exiled (2), NOT the sum of both halves' chosen
// targets (the exiled bear 2 + Blood's own target bear 2 = 4). Pre-fix
// resumeResolution bound Ctx.Targets from o.Targets (the flat list) for any
// frame that was not a half root, and fusedHalfTargets matched only the
// halves' root SAs, so DBPutCounter read 4.
func TestFusedFleshBloodSubAbilityReadsItsOwnHalfTargets(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, id, bearA, bearB, gyID := fusedFleshBloodEngine(t, reg, 7319)

	// Fused cost {3}{B}{G}{R}{G} = 3 generic + B + G + R + G: 7 mana.
	addMana(t, e, 0, "BBGGRRR")
	fuse := splitOption(t, e, id, "fuse")
	if fuse == nil {
		t.Fatalf("fused offer missing for Flesh // Blood: %+v", castOptions(t, e))
	}
	submitChoices(t, e, fuse.Index)

	// Stage 0 (Flesh): the graveyard creature card (the parked bear).
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("fused target stage 0: pending=%+v, want target", d)
	}
	gy := -1
	for _, o := range d.Options {
		if o.Obj == gyID {
			gy = o.Index
		}
	}
	if gy < 0 {
		t.Fatalf("stage 0 offered no option for the graveyard card %d: %+v", gyID, d.Options)
	}
	submitChoices(t, e, gy)

	// Stage 1 (Blood): a creature I control -- bearA.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("fused target stage 1: pending=%+v, want target", d)
	}
	blood := -1
	for _, o := range d.Options {
		if o.Obj == bearA {
			blood = o.Index
		}
	}
	if blood < 0 {
		t.Fatalf("stage 1 offered no option for bear %d: %+v", bearA, d.Options)
	}
	submitChoices(t, e, blood)

	// Resolve to the DBPutCounter target ask (its own ValidTgts$ Creature over
	// the two battlefield bears), answering with bearB.
	d = passUntilPendingKind(t, e, decision.KChoose, 30)
	if d == nil || d.ResumeKind != "tgts" {
		t.Fatalf("DBPutCounter target ask pending=%+v, want KChoose tgts", d)
	}
	for _, o := range d.Options {
		if o.Kind != "card" {
			t.Fatalf("DBPutCounter ask offered a non-card option %+v -- this is not its Creature ask", o)
		}
	}
	pick := -1
	for _, o := range d.Options {
		if o.Obj == bearB {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("DBPutCounter ask offered no option for bear %d: %+v", bearB, d.Options)
	}
	submitChoices(t, e, pick)

	// Drain the rest: Blood's Pump runs, then BloodDamage's own ValidTgts$ Any
	// ask. Answer any further mid-resolution ask (the first legal option) and
	// pass priority until the stack empties.
	for i := 0; i < 30 && len(e.G.Stack) > 0; i++ {
		d = e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining the fused resolution")
		}
		if d.Kind == decision.KPriority {
			pass := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pass = o.Index
				}
			}
			if pass < 0 {
				t.Fatalf("priority with no pass option: %+v", d.Options)
			}
			submitChoices(t, e, pass)
			continue
		}
		submitChoices(t, e, d.Options[0].Index)
	}

	// DBPutCounter placed X = the exiled card's power (2) on bearB -- never 4
	// (the sum of both halves' chosen targets, which the flat-list binding
	// produced). Blood's own DealDamage may have killed bearB; assert the
	// counter count only if it still exists, and assert the exiled card is in
	// exile (Flesh ran).
	if z := e.G.Obj(gyID).Zone; z != state.ZExile {
		t.Fatalf("Flesh did not exile the graveyard card: zone=%s, want exile", z)
	}
	if o := e.G.Obj(bearB); o != nil {
		if got := o.Counter("P1P1"); got != 2 {
			t.Fatalf("DBPutCounter placed %d counters, want 2 (the exiled card's power only; "+
				"4 means the flat both-halves list leaked into the sub-ability)", got)
		}
	}
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Fatalf("resolved fused spell zone=%s, want graveyard", z)
	}
	replayCheck(t, e, cfg)
}

// TestFusedBloodSubAbilityReadsItsOwnHalfTargets is review round 3's MAJOR
// second pin, taken on the ALTERNATE half of the same real corpus card.
// Blood's BloodDamage SubAbility asks its own ValidTgts$ Any target and deals
// NumDmg$ Y, where SVar:Y:ParentTargeted$CardPower reads the half's own
// parent target -- Blood's root target (bearA, power 2), never the sum with
// Flesh's exiled graveyard card (2+2=4). Pre-fix resumeResolution bound
// Ctx.Targets from the stack object's flat list for the sub-ability frame
// (fusedHalfTargets matched only the halves' root SAs), so Blood dealt 4.
func TestFusedBloodSubAbilityReadsItsOwnHalfTargets(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, id, bearA, bearB, gyID := fusedFleshBloodEngine(t, reg, 7319)

	// Fused cost {3}{B}{G}{R}{G}: 7 mana.
	addMana(t, e, 0, "BBGGRRR")
	fuse := splitOption(t, e, id, "fuse")
	if fuse == nil {
		t.Fatalf("fused offer missing for Flesh // Blood: %+v", castOptions(t, e))
	}
	submitChoices(t, e, fuse.Index)

	// Stage 0 (Flesh): the graveyard creature card. Stage 1 (Blood): bearA.
	for i, want := range []state.ObjID{gyID, bearA} {
		d := e.Pending()
		if d == nil || d.Kind != decision.KTarget {
			t.Fatalf("fused target stage %d: pending=%+v, want target", i, d)
		}
		found := -1
		for _, o := range d.Options {
			if o.Obj == want {
				found = o.Index
			}
		}
		if found < 0 {
			t.Fatalf("stage %d: no option for %d: %+v", i, want, d.Options)
		}
		submitChoices(t, e, found)
	}

	// Flesh's DBPutCounter sub asks its own Creature target; answer bearB (so
	// the counter placement is deterministic and bearB is the observable).
	d := passUntilPendingKind(t, e, decision.KChoose, 30)
	if d == nil || d.ResumeKind != "tgts" {
		t.Fatalf("DBPutCounter target ask pending=%+v, want KChoose tgts", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Obj == bearB {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("DBPutCounter ask offered no option for bear %d: %+v", bearB, d.Options)
	}
	submitChoices(t, e, pick)

	// Blood's BloodDamage sub asks its own ValidTgts$ Any target. Aim it at
	// seat 0's face (the option list starts with the players), so the amount
	// is read straight off the life loss and no creature dies mid-test.
	d = passUntilPendingKind(t, e, decision.KChoose, 30)
	if d == nil || d.ResumeKind != "tgts" {
		t.Fatalf("BloodDamage target ask pending=%+v, want KChoose tgts", d)
	}
	face := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 0 {
			face = o.Index
		}
	}
	if face < 0 {
		t.Fatalf("BloodDamage ask offered no seat-0 face option: %+v", d.Options)
	}
	lifeBefore := e.G.Players[0].Life
	submitChoices(t, e, face)

	for i := 0; i < 30 && len(e.G.Stack) > 0; i++ {
		d = e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining the fused resolution")
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected non-priority ask while draining: %+v", d)
		}
		pass := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				pass = o.Index
			}
		}
		if pass < 0 {
			t.Fatalf("priority with no pass option: %+v", d.Options)
		}
		submitChoices(t, e, pass)
	}

	if lost := lifeBefore - e.G.Players[0].Life; lost != 2 {
		t.Fatalf("Blood dealt %d damage, want 2 (its own target's power; "+
			"4 means the flat both-halves list leaked into the sub-ability)", lost)
	}
	if got := e.G.Obj(bearB).Counter("P1P1"); got != 2 {
		t.Fatalf("DBPutCounter placed %d counters, want 2", got)
	}
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Fatalf("resolved fused spell zone=%s, want graveyard", z)
	}
	replayCheck(t, e, cfg)
}
