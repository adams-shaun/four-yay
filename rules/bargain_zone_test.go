package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestBargainMayPlayFromGraveyard(t *testing.T) {
	e := mayPlayBase(t)
	grant := onBoardGrant(t, e, 0, "Name:Grant\nTypes:Enchantment\nS:Mode$ Continuous | Affected$ Card.YouOwn | MayPlay$ True | MayPlayWithoutManaCost$ True | EffectZone$ Battlefield | AffectedZone$ Graveyard\nOracle:x\n")
	if e.G.Obj(grant).Zone != state.ZBattlefield {
		t.Fatal("precondition: MayPlay grant is not on the battlefield")
	}
	spell := e.G.AddObject(card(t, "Name:Barter Bolt\nManaCost:3\nTypes:Instant\nK:Bargain\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"), 0)
	ids := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	ids = append(ids, spell.ID)
	e.G.SetZone(state.ZLibrary, 0, ids)
	e.emit(events.Event{Kind: events.MoveZone, Obj: spell.ID, From: state.ZLibrary, To: state.ZGraveyard})
	artifact := e.G.AddObject(card(t, "Name:Sacrifice Rock\nTypes:Artifact\nOracle:x\n"), 0)
	ids = append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	ids = append(ids, artifact.ID)
	e.G.SetZone(state.ZLibrary, 0, ids)
	e.emit(events.Event{Kind: events.MoveZone, Obj: artifact.ID, From: state.ZLibrary, To: state.ZBattlefield})
	e.priorityRound()
	if e.G.Obj(spell.ID).Zone != state.ZGraveyard {
		t.Fatal("precondition: Bargain spell is not in graveyard")
	}
	d := e.Pending()
	if d == nil {
		t.Fatal("missing priority decision")
	}
	for _, option := range d.Options {
		if option.Kind == "cast" && option.Obj == spell.ID && option.Mode == "bargained" {
			submitChoices(t, e, option.Index)
			if pending := e.Pending(); pending == nil || pending.Kind != decision.KChoose {
				t.Fatalf("non-hand bargained cast did not ask for sacrifice: %+v", pending)
			}
			bargainAnswer(t, e, artifact.ID)
			if got := e.G.Obj(spell.ID); got.Zone != state.ZStack || got.CastFlags&state.FlagBargained == 0 {
				t.Fatalf("bargained free may-play cast = zone %s flags %#x, want stack with FlagBargained", got.Zone, got.CastFlags)
			}
			if got := e.G.Obj(artifact.ID); got.Zone != state.ZGraveyard {
				t.Fatalf("bargain sacrifice zone = %s, want graveyard", got.Zone)
			}
			return
		}
	}
	t.Fatalf("missing non-hand Bargain option: %+v", d.Options)
}
