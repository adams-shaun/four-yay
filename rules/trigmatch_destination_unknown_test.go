package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

const unknownDestinationScript = `Name:Unknown Destination Watcher
ManaCost:1 U
Types:Creature Wizard
PT:1/1
T:Mode$ ChangesZone | Destination$ Ante | ValidCard$ Creature.Other | Origin$ Battlefield | TriggerZones$ Battlefield | Execute$ TrigDraw
SVar:TrigDraw:DB$ Draw | NumCards$ 1
Oracle:x
`

func TestDestinationUnknownDoesNotTrigger(t *testing.T) {
	for _, to := range []state.Zone{state.ZHand, state.ZExile, state.ZGraveyard} {
		t.Run(to.String(), func(t *testing.T) {
			e := combatEngine(t)
			src := onBoardCard(t, e, 0, card(t, unknownDestinationScript))
			mover := onBoardCard(t, e, 1, card(t, originListMoverScript))
			if src == mover || e.G.Obj(src).Zone != state.ZBattlefield || e.G.Obj(mover).Zone != state.ZBattlefield {
				t.Fatal("precondition: distinct watcher and mover must both be on the battlefield")
			}
			before := len(e.pendingTriggers)
			movedFrom(t, e, mover, state.ZBattlefield, to)
			queuedTriggers(t, e, before, 0, "unknown Destination$ Ante must admit no destination")
		})
	}
}

func TestDestinationUnknownAnySiblingStillTriggers(t *testing.T) {
	e := combatEngine(t)
	src := onBoardCard(t, e, 0, card(t, anyDestinationScript))
	mover := onBoardCard(t, e, 1, card(t, originListMoverScript))
	if src == mover || e.G.Obj(src).Zone != state.ZBattlefield || e.G.Obj(mover).Zone != state.ZBattlefield {
		t.Fatal("precondition: distinct watcher and mover must both be on the battlefield")
	}
	before := len(e.pendingTriggers)
	movedFrom(t, e, mover, state.ZBattlefield, state.ZHand)
	queuedTriggers(t, e, before, 1, "Destination$ Any control admits hand")
}
