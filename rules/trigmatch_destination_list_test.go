package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

const dourPortMageDestinationListScript = `Name:Dour Port-Mage Destination Watcher
ManaCost:1 U
Types:Creature Merfolk Wizard
PT:1/3
T:Mode$ ChangesZoneAll | Destination$ Ante,Command,Exile,Hand,Library | ValidCards$ Creature.Other+YouCtrl | Origin$ Battlefield | TriggerZones$ Battlefield | Execute$ TrigDraw
SVar:TrigDraw:DB$ Draw | NumCards$ 1
Oracle:x
`

const graveyardExileDestinationListScript = `Name:Graveyard Exile Destination Watcher
ManaCost:1 B
Types:Creature Zombie
PT:2/2
T:Mode$ ChangesZone | Destination$ Graveyard,Exile | ValidCard$ Creature.Other | Origin$ Battlefield | TriggerZones$ Battlefield | Execute$ TrigDraw
SVar:TrigDraw:DB$ Draw | NumCards$ 1
Oracle:x
`

const anyDestinationScript = `Name:Any Destination Watcher
ManaCost:1 U
Types:Creature Wizard
PT:1/1
T:Mode$ ChangesZone | Destination$ Any | ValidCard$ Creature.Other | Origin$ Battlefield | TriggerZones$ Battlefield | Execute$ TrigDraw
SVar:TrigDraw:DB$ Draw | NumCards$ 1
Oracle:x
`

func TestDestinationListDourPortMageFiresOnHandAndExile(t *testing.T) {
	for _, to := range []state.Zone{state.ZHand, state.ZExile} {
		t.Run(to.String(), func(t *testing.T) {
			t.Parallel()
			e := combatEngine(t)
			src := onBoardCard(t, e, 0, card(t, dourPortMageDestinationListScript))
			mover := onBoardCard(t, e, 0, card(t, originListMoverScript))
			if src == mover || e.G.Obj(src).Zone != state.ZBattlefield || e.G.Obj(mover).Zone != state.ZBattlefield {
				t.Fatal("precondition: distinct watcher and mover must both be on the battlefield")
			}
			before := len(e.pendingTriggers)
			movedFrom(t, e, mover, state.ZBattlefield, to)
			queuedTriggers(t, e, before, 1, "Dour Port-Mage leaving creature for "+to.String())
		})
	}
}

func TestDestinationListDourPortMageExcludesGraveyard(t *testing.T) {
	e := combatEngine(t)
	src := onBoardCard(t, e, 0, card(t, dourPortMageDestinationListScript))
	mover := onBoardCard(t, e, 0, card(t, originListMoverScript))
	if src == mover || e.G.Obj(src).Zone != state.ZBattlefield || e.G.Obj(mover).Zone != state.ZBattlefield {
		t.Fatal("precondition: distinct watcher and mover must both be on the battlefield")
	}
	before := len(e.pendingTriggers)
	movedFrom(t, e, mover, state.ZBattlefield, state.ZGraveyard)
	queuedTriggers(t, e, before, 0, "Dour Port-Mage excludes dying")
}

func TestDestinationListChangesZoneListFiresOnExile(t *testing.T) {
	e := combatEngine(t)
	src := onBoardCard(t, e, 0, card(t, graveyardExileDestinationListScript))
	mover := onBoardCard(t, e, 1, card(t, originListMoverScript))
	if src == mover || e.G.Obj(src).Zone != state.ZBattlefield || e.G.Obj(mover).Zone != state.ZBattlefield {
		t.Fatal("precondition: distinct watcher and mover must both be on the battlefield")
	}
	before := len(e.pendingTriggers)
	movedFrom(t, e, mover, state.ZBattlefield, state.ZExile)
	queuedTriggers(t, e, before, 1, "ChangesZone Graveyard,Exile list admits exile")
}

func TestDestinationListAnyWildcardAdmitsEveryDestination(t *testing.T) {
	for _, to := range []state.Zone{state.ZHand, state.ZExile, state.ZGraveyard} {
		t.Run(to.String(), func(t *testing.T) {
			t.Parallel()
			e := combatEngine(t)
			src := onBoardCard(t, e, 0, card(t, anyDestinationScript))
			mover := onBoardCard(t, e, 1, card(t, originListMoverScript))
			if src == mover || e.G.Obj(src).Zone != state.ZBattlefield || e.G.Obj(mover).Zone != state.ZBattlefield {
				t.Fatal("precondition: distinct watcher and mover must both be on the battlefield")
			}
			before := len(e.pendingTriggers)
			movedFrom(t, e, mover, state.ZBattlefield, to)
			queuedTriggers(t, e, before, 1, "Destination$ Any admits "+to.String())
		})
	}
}
