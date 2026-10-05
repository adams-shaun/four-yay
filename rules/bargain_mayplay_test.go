package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestBargainTypedMayPlayAffordableOnlyAfterDiscount keeps the selected typed
// permission on the offer and prices the Bargain reduction independently of
// the unaffordable plain MayPlay cast.
func TestBargainTypedMayPlayAffordableOnlyAfterDiscount(t *testing.T) {
	e := mayPlayBase(t)
	grant := onBoardGrant(t, e, 0, "Name:Typed Grant\nTypes:Enchantment\nS:Mode$ Continuous | Affected$ Card.YouOwn | MayPlay$ True | MayPlayText$ Instant | MayPlayIgnoreColor$ True | EffectZone$ Battlefield | AffectedZone$ Graveyard\nOracle:x\n")
	if e.G.Obj(grant).Zone != state.ZBattlefield {
		t.Fatal("precondition: typed MayPlay grant is on the battlefield")
	}
	spell := e.G.AddObject(card(t, "Name:Bargain Permission Test\nManaCost:3\nTypes:Instant\nK:Bargain\nS:Mode$ ReduceCost | ValidSpell$ Spell.Bargain | Type$ Spell | ValidCard$ Card.Self | Amount$ 2 | EffectZone$ All\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"), 0)
	ids := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	ids = append(ids, spell.ID)
	e.G.SetZone(state.ZLibrary, 0, ids)
	e.emit(events.Event{Kind: events.MoveZone, Obj: spell.ID, From: state.ZLibrary, To: state.ZGraveyard})
	addMana(t, e, 0, "R")
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %+v, want priority", d)
	}
	var bargained *decision.Option
	for i := range d.Options {
		o := &d.Options[i]
		if o.Kind == "cast" && o.Obj == spell.ID {
			if o.Mode == "bargained" {
				bargained = o
			}
			if o.Mode == "mayplay" {
				t.Fatalf("plain MayPlay cast offered despite insufficient mana: %+v", *o)
			}
		}
	}
	if bargained == nil {
		t.Fatalf("Bargain-reduced cast missing from typed permission offers: %+v", d.Options)
	}
	if bargained.MayPlayPerm == "" {
		t.Fatal("bargained option lost the typed MayPlay permission")
	}
	submitChoices(t, e, bargained.Index)
	bargainAnswer(t, e, grant)
	got := e.G.Obj(spell.ID)
	if got.Zone != state.ZStack || got.CastFlags&state.FlagBargained == 0 {
		t.Fatalf("typed bargained cast = zone %s flags %#x, want stack with Bargained provenance", got.Zone, got.CastFlags)
	}
	if units := e.G.Players[0].ManaUnits(); units != ([7]state.Mana{}) {
		t.Fatalf("bargained MayPlay cast did not pay its reduced mana cost: %v", units)
	}
	permissionRecorded := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.CastInfo && ev.Obj == spell.ID && strings.Contains(ev.Counter, "perm="+bargained.MayPlayPerm) {
			permissionRecorded = true
		}
	}
	if !permissionRecorded {
		t.Fatalf("typed MayPlay permission %q was not recorded in cast provenance", bargained.MayPlayPerm)
	}
}
