package effects

// The explicit Optional$ confirm-before-pick gate for the two ChangeZone
// fetch paths Forge routes through changeHiddenOriginResolve's confirmAction:
// the hidden-hand walk (handMoveOwnersWalk, ordinary non-ForgetOther fetches
// included) and the Hidden$ True public-origin pick (effHiddenPick). The
// marker asks the decider whether to proceed BEFORE any card is picked; a
// decline poses no pick and changes nothing, while an accepted confirmation
// enters the fetch -- whose Min-0 pick may still take nothing, and whose
// empty eligible pool still confirms (Forge's gate runs before the fetch
// list is consulted). ChoiceOptional$ is the pick's own cardinality marker,
// never a confirmation, and the markerless text-may shapes stay
// confirmation-free.

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

const ocHandOptional = "DB$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | ChangeNum$ 1 | Optional$ True"

const ocHiddenOptional = "DB$ ChangeZone | Hidden$ True | Origin$ Graveyard | Destination$ Battlefield | ChangeType$ Creature | ChangeNum$ 1 | Optional$ True"

// ocHiddenFixture seats a creature and a land in seat 0's graveyard and
// returns (host, creature id, land id).
func ocHiddenFixture(t *testing.T) (*askHost, state.ObjID, state.ObjID) {
	t.Helper()
	h := &askHost{}
	h.g = state.NewGame(names(2))
	bear := mkCard(t, "Name:Grizzly Bears\nTypes:Creature\nPT:2/2\nOracle:x\n")
	isle := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	bearID := h.g.AddObject(bear, 0).ID
	isleID := h.g.AddObject(isle, 0).ID
	h.g.SetZone(state.ZGraveyard, 0, []state.ObjID{bearID, isleID})
	h.g.Obj(bearID).Zone = state.ZGraveyard
	h.g.Obj(isleID).Zone = state.ZGraveyard
	return h, bearID, isleID
}
