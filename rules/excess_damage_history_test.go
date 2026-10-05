package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestExcessDamageHistory(t *testing.T) {
	c, diags := cards.ParseBytes("excess.txt", []byte("Name:Target\nTypes:Creature Beast\nPT:4/4\nOracle:x\n"))
	if len(diags) != 0 {
		t.Fatalf("parse fixture: %v", diags)
	}
	c.Link()
	g := state.NewGame([]string{"a", "b"})
	o := g.AddObject(c, 1)
	o.Zone = state.ZBattlefield
	g.SetZone(state.ZBattlefield, 1, []state.ObjID{o.ID})
	// Prior marked damage reduces the lethal threshold to two: exactly two
	// more damage is lethal, not excess; three is excess.
	o.Damage = 2
	if o.Zone != state.ZBattlefield || o.Damage != 2 || c.Faces[0].PT != "4/4" {
		t.Fatalf("fixture mismatch: zone=%v damage=%d PT=%q", o.Zone, o.Damage, c.Faces[0].PT)
	}
	events.Apply(g, events.Event{Kind: events.Damage, Obj: o.ID, Amount: 2})
	if o.WasDealtExcessDamageThisTurn {
		t.Fatal("ordinary damage falsely recorded as excess")
	}
	events.Apply(g, events.Event{Kind: events.ExcessDamage, Obj: o.ID})
	if !o.WasDealtExcessDamageThisTurn {
		t.Fatal("excess marker was not folded")
	}
	events.Apply(g, events.Event{Kind: events.TurnChange, Player: 0})
	if o.WasDealtExcessDamageThisTurn {
		t.Fatal("excess history survived TurnChange")
	}
}
