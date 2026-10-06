package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestExcessDamageMultiTypeThreshold pins CR 120.4a/120.10's last sentence:
// a permanent with several of creature/planeswalker/battle takes the GREATEST
// per-type excess, so it was dealt excess iff the damage exceeds the SMALLEST
// applicable threshold -- loyalty below remaining toughness and the reverse.
func TestExcessDamageMultiTypeThreshold(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		dmg     int32
		tough   int32
		loyalty int32
		want    bool
	}{
		// 3 damage: within toughness 4, beyond loyalty 2.
		{"loyalty smaller", "Name:Walker Beast\nTypes:Legendary Creature Planeswalker Beast\nPT:4/4\nLoyalty:2\nOracle:x\n", 3, 4, 2, true},
		// 3 damage: beyond toughness 2, within loyalty 5.
		{"toughness smaller", "Name:Beast Walker\nTypes:Legendary Creature Planeswalker Beast\nPT:2/2\nLoyalty:5\nOracle:x\n", 3, 2, 5, true},
		// 3 damage: within both (toughness 4, loyalty 3).
		{"within both", "Name:Sturdy Walker\nTypes:Legendary Creature Planeswalker Beast\nPT:4/4\nLoyalty:3\nOracle:x\n", 3, 4, 3, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := layerEngine(t)
			id := excessTestPermanent(t, e, tc.src, 1)
			o := e.G.Obj(id)
			if o.Zone != state.ZBattlefield || !e.IsCreature(id) || !o.Face().IsPlaneswalker() {
				t.Fatalf("precondition: zone=%v creature=%v walker=%v", o.Zone, e.IsCreature(id), o.Face().IsPlaneswalker())
			}
			if e.Toughness(id) != tc.tough || o.Counter("LOYALTY") != tc.loyalty || o.Damage != 0 {
				t.Fatalf("precondition: toughness=%d loyalty=%d damage=%d, want %d/%d/0",
					e.Toughness(id), o.Counter("LOYALTY"), o.Damage, tc.tough, tc.loyalty)
			}
			e.emit(events.Event{Kind: events.Damage, Obj: id, Amount: tc.dmg})
			if o.WasDealtExcessDamageThisTurn != tc.want {
				t.Fatalf("%d damage to toughness %d / loyalty %d: excess=%v, want %v",
					tc.dmg, tc.tough, tc.loyalty, o.WasDealtExcessDamageThisTurn, tc.want)
			}
			if tc.want {
				v := e.G.ExcessDamageVictims
				if len(v) != 1 || v[0].Obj != id || v[0].Type != excessTypeCreature|excessTypePlaneswalker || v[0].Controller != 1 {
					t.Fatalf("victim snapshot = %+v, want one creature+planeswalker entry for %d under seat 1", v, id)
				}
			}
		})
	}
}

// TestExcessDamageBattleDefense: a battle's threshold is its defense before
// the damage (CR 120.10).
func TestExcessDamageBattleDefense(t *testing.T) {
	for _, tc := range []struct {
		dmg  int32
		want bool
	}{{3, false}, {4, true}} {
		e := layerEngine(t)
		id := excessTestPermanent(t, e, "Name:Test Campaign\nTypes:Battle Campaign\nDefense:3\nOracle:x\n", 1)
		o := e.G.Obj(id)
		if o.Zone != state.ZBattlefield || !o.Face().IsBattle() || o.Counter("DEFENSE") != 3 || e.IsCreature(id) {
			t.Fatalf("precondition battle: zone=%v battle=%v defense=%d", o.Zone, o.Face().IsBattle(), o.Counter("DEFENSE"))
		}
		e.emit(events.Event{Kind: events.Damage, Obj: id, Amount: tc.dmg})
		if o.WasDealtExcessDamageThisTurn != tc.want {
			t.Fatalf("%d damage to defense 3: excess=%v, want %v", tc.dmg, o.WasDealtExcessDamageThisTurn, tc.want)
		}
	}
}

// TestExcessDamageSimultaneousBatch: hits dealt at the same time are summed
// against the pre-batch threshold (CR 120.10 "those sources together"), and
// the ExcessDamage fact follows the whole batch so its Damage events stay
// contiguous.
func TestExcessDamageSimultaneousBatch(t *testing.T) {
	e := layerEngine(t)
	id := excessTestPermanent(t, e, "Name:Four Four\nTypes:Creature Beast\nPT:4/4\nOracle:x\n", 1)
	other := excessTestPermanent(t, e, "Name:Bystander\nTypes:Creature Beast\nPT:4/4\nOracle:x\n", 1)
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield || e.Toughness(id) != 4 || o.Damage != 0 {
		t.Fatalf("precondition: zone=%v toughness=%d damage=%d", o.Zone, e.Toughness(id), o.Damage)
	}
	e.BeginDamageBatch()
	first := e.emit(events.Event{Kind: events.Damage, Obj: id, Amount: 3})
	e.emit(events.Event{Kind: events.Damage, Obj: other, Amount: 1})
	last := e.emit(events.Event{Kind: events.Damage, Obj: id, Amount: 2})
	if o.WasDealtExcessDamageThisTurn {
		t.Fatal("excess published before the batch closed")
	}
	e.EndDamageBatch()
	if !o.WasDealtExcessDamageThisTurn {
		t.Fatal("3+2 simultaneous damage to a 4/4 was not excess")
	}
	if e.G.Obj(other).WasDealtExcessDamageThisTurn {
		t.Fatal("1 damage to a 4/4 recorded as excess")
	}
	for _, ev := range e.L.Events[first.Seq:last.Seq] {
		if ev.Kind == events.ExcessDamage {
			t.Fatalf("ExcessDamage at seq %d splits the damage batch", ev.Seq)
		}
	}
}
