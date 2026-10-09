package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestTriggerObjectsAttackersGreatestCardPower pins the GreatestCardPower
// property arm (task greatestcardpower) over the tek1 TriggerObjectsAttackers
// referent set: Aloy, Savior of Meridian's "discover X, where X is the
// greatest power among them" and Shriekwood Devourer's "untap up to X lands,
// where X is the greatest power among those creatures". The arm is the MAX
// over the referent set, never the sum: Asp 4/5 plus Bear 2/2 reads 4, not 6.
func TestTriggerObjectsAttackersGreatestCardPower(t *testing.T) {
	h := newHost(t, 2)
	asp := mkCard(t, "Name:Asp\nManaCost:4 G\nTypes:Creature Snake\nPT:4/5\nOracle:x\n")
	bear := mkCard(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	aspObj := h.g.AddObject(asp, 0)
	bearObj := h.g.AddObject(bear, 0)
	aspObj.Zone = state.ZBattlefield
	bearObj.Zone = state.ZBattlefield
	aspObj.Controller = 0
	bearObj.Controller = 0
	// Preconditions: both referents really are on the battlefield and their
	// powers really differ, so max (4) cannot pass as a sum (6) or as the
	// last object's power (2).
	if aspObj.Zone != state.ZBattlefield || bearObj.Zone != state.ZBattlefield {
		t.Fatalf("precondition: both attackers must be on the battlefield, got %v / %v", aspObj.Zone, bearObj.Zone)
	}
	if p := aspObj.Face().Power(); p != 4 {
		t.Fatalf("precondition: Asp power = %d, want 4", p)
	}
	if p := bearObj.Face().Power(); p != 2 {
		t.Fatalf("precondition: Bear power = %d, want 2", p)
	}

	one := &Ctx{Controller: 0, TriggerContext: TriggerContext{TriggerAttackers: []state.ObjID{aspObj.ID}}}
	if got, ok := EvalCountOK(h, one, "TriggerObjectsAttackers$GreatestCardPower"); !ok || got != 4 {
		t.Errorf("one attacker: EvalCountOK = (%d, %v), want (4, true)", got, ok)
	}

	both := &Ctx{Controller: 0, TriggerContext: TriggerContext{TriggerAttackers: []state.ObjID{aspObj.ID, bearObj.ID}}}
	if got, ok := EvalCountOK(h, both, "TriggerObjectsAttackers$GreatestCardPower"); !ok || got != 4 {
		t.Errorf("two attackers: EvalCountOK = (%d, %v), want (4, true) -- max, not the summed 6 or the last 2", got, ok)
	}
	// The summed sibling still sums, so the two property reads cannot be the
	// same arm: CardPower over the same set is 6.
	if got, ok := EvalCountOK(h, both, "TriggerObjectsAttackers$CardPower"); !ok || got != 6 {
		t.Errorf("sanity: TriggerObjectsAttackers$CardPower = (%d, %v), want (6, true)", got, ok)
	}
	// The /Op suffix applies to the reduced value, not per object.
	if got, ok := EvalCountOK(h, both, "TriggerObjectsAttackers$GreatestCardPower/Twice"); !ok || got != 8 {
		t.Errorf("op tail: EvalCountOK = (%d, %v), want (8, true)", got, ok)
	}
	// An UNBOUND context (no TriggerContext.TriggerAttackers) fails closed to
	// zero but stays resolvable -- never the Remembered fallback, which here
	// holds both attackers and would read 4.
	unbound := &Ctx{Controller: 0, Remembered: []state.Target{{Obj: aspObj.ID}, {Obj: bearObj.ID}}}
	if got, ok := EvalCountOK(h, unbound, "TriggerObjectsAttackers$GreatestCardPower"); !ok || got != 0 {
		t.Errorf("unbound: EvalCountOK = (%d, %v), want (0, true) -- not the Remembered fallback", got, ok)
	}
}

// TestRememberedLKIGreatestCardPower pins the same property over the
// RememberedLKI ref spelling (Shadowgrange Archfiend: "You gain life equal to
// the greatest power among creatures sacrificed this way"). The remembered
// set is the sacrificed batch already in the graveyard at the read, so the
// off-battlefield printed-face read applies; the LKI half pins the
// substitution of the trigger's pre-move clone when the remembered object IS
// the zone-change LKI.
func TestRememberedLKIGreatestCardPower(t *testing.T) {
	h := newHost(t, 2)
	beast := mkCard(t, "Name:Beast\nTypes:Creature Beast\nPT:3/4\nOracle:x\n")
	hound := mkCard(t, "Name:Hound\nTypes:Creature Hound\nPT:2/2\nOracle:x\n")
	rock := mkCard(t, "Name:Rock\nTypes:Artifact\nOracle:x\n")
	bo := h.g.AddObject(beast, 1)
	ho := h.g.AddObject(hound, 1)
	ro := h.g.AddObject(rock, 1)
	// Re-fetch by id: AddObject returns a pointer into the arena slice, which
	// a later append can reallocate.
	h.g.Obj(bo.ID).AddCounter("P1P1", 2) // derived power 5
	src := mkCard(t, "Name:Source\nTypes:Creature Demon\nPT:8/4\nOracle:x\n")
	so := h.g.AddObject(src, 0)
	// The sacrificed batch: SacrificeAll has already moved them to the
	// graveyard by the time DBLifeGain reads RememberedLKI.
	for _, id := range []state.ObjID{bo.ID, ho.ID, ro.ID} {
		h.g.Obj(id).Zone = state.ZGraveyard
	}
	c := &Ctx{Source: so.ID, Controller: 0,
		Remembered: []state.Target{{Obj: bo.ID}, {Obj: ho.ID}, {Obj: ro.ID}}}
	// Preconditions: the batch is really remembered and the values really
	// differ, so max (5) cannot pass as a sum (7) or as the last object (0).
	if got := EvalCount(h, c, "RememberedLKI$Amount"); got != 3 {
		t.Fatalf("precondition: RememberedLKI$Amount = %d, want 3", got)
	}
	if got := EvalCount(h, c, "RememberedLKI$CardPower"); got != 7 {
		t.Fatalf("precondition: RememberedLKI$CardPower = %d, want the summed 7", got)
	}
	if got, ok := EvalCountOK(h, c, "RememberedLKI$GreatestCardPower"); !ok || got != 5 {
		t.Errorf("RememberedLKI$GreatestCardPower = (%d, %v), want (5, true) -- max, not the summed 7", got, ok)
	}

	// The LKI substitution: the beast's pre-move clone carries its two +1/+1
	// counters while the live graveyard object has lost them (CR 122.6), so
	// 5 vs 3 tells the snapshot read from the live read.
	liveBeast := h.g.Obj(bo.ID)
	snapshot := liveBeast.CloneDeep()
	liveBeast.Counters = nil
	if got := refPower(h, liveBeast, false); got != 3 {
		t.Fatalf("precondition: live beast power = %d, want 3 (counters must be cleared)", got)
	}
	if got := refPower(h, &snapshot, true); got != 5 {
		t.Fatalf("precondition: LKI beast power = %d, want 5 (clone must carry the counters)", got)
	}
	lkiCtx := &Ctx{Source: so.ID, Controller: 0, LKI: &snapshot,
		Remembered: []state.Target{{Obj: bo.ID}}}
	if got, ok := EvalCountOK(h, lkiCtx, "RememberedLKI$GreatestCardPower"); !ok || got != 5 {
		t.Errorf("LKI: RememberedLKI$GreatestCardPower = (%d, %v), want (5, true) -- the clone, not the live 3", got, ok)
	}
	liveCtx := *lkiCtx
	liveCtx.LKI = nil
	if got, ok := EvalCountOK(h, &liveCtx, "RememberedLKI$GreatestCardPower"); !ok || got != 3 {
		t.Errorf("no LKI: RememberedLKI$GreatestCardPower = (%d, %v), want (3, true)", got, ok)
	}
}
