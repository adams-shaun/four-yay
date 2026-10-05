package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestUntapAllStaysOneBatchAcrossReplacementOrder(t *testing.T) {
	e := newSeats(t, 2)
	e.pending = nil
	observer := onBoard(t, e, 0, "Name:Batch Observer\nTypes:Enchantment\n"+
		"T:Mode$ UntapAll | TriggerZones$ Battlefield | ValidCards$ Permanent | ValidPlayer$ You | Execute$ Trig\n"+
		"SVar:Trig:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n")
	if o := e.G.Obj(observer); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("observer = %+v, want battlefield (vacuous trigger setup)", o)
	}
	rock1 := onBoard(t, e, 0, "Name:Rock One\nTypes:Artifact\nOracle:x\n")
	middle := onBoard(t, e, 0, "Name:Rock Two\nTypes:Artifact Creature\nPT:2/2\nOracle:x\n")
	rock3 := onBoard(t, e, 0, "Name:Rock Three\nTypes:Artifact\nOracle:x\n")
	for _, id := range []state.ObjID{rock1, middle, rock3} {
		e.emit(events.Event{Kind: events.Tap, Obj: id})
	}
	for _, id := range []state.ObjID{rock1, middle, rock3} {
		if o := e.G.Obj(id); o == nil || !o.Tapped {
			t.Fatalf("precondition: permanent %d = %+v, want tapped", id, o)
		}
	}
	for _, name := range []string{"Prevent One", "Prevent Two"} {
		onBoard(t, e, 0, "Name:"+name+"\nTypes:Enchantment\n"+
			"R:Event$ Untap | ActiveZones$ Battlefield | ValidCard$ Creature | Layer$ CantHappen\nOracle:x\n")
	}
	spell := e.G.AddObject(card(t, "Name:Untap All\nManaCost:1\nTypes:Sorcery\n"+
		"A:SP$ UntapAll | ValidCards$ Artifact\nOracle:x\n"), 0)
	spell.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 0, []state.ObjID{spell.ID})
	e.G.Stack = []state.ObjID{spell.ID}
	beforeLife := e.G.Players[0].Life

	// Real priority-driven resolution: pass until the order choice parks inside
	// api:UntapAll, then answer it. The kernel either answers in place or
	// replays from S0, whose cloned bracket state is closed; neither re-enters
	// the primitive with an already-open batch. The untap-step cursor uses the
	// same direct open-state guard (rules/turn.go), while Discard uses the same
	// depth-counted deferred bracket.
	e.priorityRound()
	for i := 0; i < 4; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			break
		}
		submitChoices(t, e, passIndex(t, d))
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || d.Source != middle {
		t.Fatalf("pending = %+v, want KReplacement for middle permanent %d", d, middle)
	}
	if len(d.Options) != 2 {
		t.Fatalf("replacement options = %+v, want both competing replacements", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	if e.G.Obj(rock1).Tapped || !e.G.Obj(middle).Tapped || e.G.Obj(rock3).Tapped {
		t.Fatalf("post-resolution tapped states: first=%v middle=%v third=%v, want false/true/false",
			e.G.Obj(rock1).Tapped, e.G.Obj(middle).Tapped, e.G.Obj(rock3).Tapped)
	}
	untaps := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Untap && (ev.Obj == rock1 || ev.Obj == rock3) {
			untaps++
		}
	}
	if untaps != 2 {
		t.Fatalf("landed Untap events for the two allowed rocks = %d, want 2", untaps)
	}
	// Promote the queued trigger so the observable push count is measured.
	e.priorityRound()
	if got := triggerPushesFor(e, observer); got != 1 {
		t.Fatalf("one api:UntapAll across a replacement-order answer pushed %d Mode$ UntapAll triggers, want 1", got)
	}
	for i := 0; i < 4; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			break
		}
		submitChoices(t, e, passIndex(t, d))
	}
	if got := e.G.Players[0].Life; got != beforeLife+1 {
		t.Fatalf("observer life = %d (before %d), want exactly one trigger's +1 life", got, beforeLife)
	}
}
