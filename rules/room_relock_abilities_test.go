package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// A locked designation removes that face's activated text, including mana
// abilities, without changing the flat ability indexes used by replay.
func TestRoomLockedCastFaceHidesActivatedAbilitiesUntilUnlocked(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	room := card(t, "Name:Ability Door\nManaCost:0\nTypes:Enchantment Room\nA:AB$ Draw | Cost$ 0 | NumCards$ 1\nA:AB$ Mana | Cost$ T | Produced$ W\nOracle:x\nALTERNATE\nName:Quiet Door\nManaCost:0\nTypes:Enchantment Room\nOracle:x\nAlternateMode:Split\n")
	e := corpusEngine(t, reg, []*cards.Card{room}, nil)
	id := moveByName(t, e, 0, "Ability Door", state.ZBattlefield)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || !isRoom(o) || !o.DoorUnlocked(int(o.FaceIdx)) || len(o.Face().Abilities) < 2 {
		t.Fatalf("precondition: cast face lacks unlocked activated text: %+v", o)
	}
	offered := func() bool {
		for _, opt := range e.legalActions(0) {
			if opt.Kind == "ability" && opt.Obj == id {
				return true
			}
		}
		return false
	}
	if !offered() || len(e.availableManaAbilities(0, id)) == 0 {
		t.Fatal("precondition: unlocked face abilities are not available")
	}
	e.emit(events.Event{Kind: events.DoorLock, Obj: id, Amount: int32(o.FaceIdx) + 1})
	if e.G.Obj(id).DoorUnlocked(int(o.FaceIdx)) {
		t.Fatal("lock did not change the cast face designation")
	}
	if offered() || len(e.availableManaAbilities(0, id)) != 0 {
		t.Fatal("locked cast face still offers activated or mana ability")
	}
	// A stale option must also be rejected by the activation path.
	e.beginActivation(0, decision.Option{Obj: id, Ability: 0})
	if e.cast != nil {
		t.Fatal("locked cast face accepted a stale ability option")
	}
	e.emit(events.Event{Kind: events.DoorUnlock, Obj: id, Amount: int32(o.FaceIdx) + 1})
	if !e.G.Obj(id).DoorUnlocked(int(o.FaceIdx)) || !offered() || len(e.availableManaAbilities(0, id)) == 0 {
		t.Fatal("unlock did not restore the cast face's activated abilities")
	}
}

func TestRoomRepeatFaceUnlockDoesNotQueueUnlockDoorTrigger(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	room := card(t, "Name:Quiet Door\nManaCost:0\nTypes:Enchantment Room\nOracle:x\nALTERNATE\nName:Triggered Door\nManaCost:0\nTypes:Enchantment Room\nT:Mode$ UnlockDoor | ThisDoor$ True | Execute$ Gain\nSVar:Gain:DB$ GainLife | LifeAmount$ 1 | Defined$ You\nOracle:x\nAlternateMode:Split\n")
	e := corpusEngine(t, reg, []*cards.Card{room}, nil)
	id := moveByName(t, e, 0, "Quiet Door", state.ZBattlefield)
	o := e.G.Obj(id)
	if o == nil || !isRoom(o) || o.DoorUnlocked(1) || len(o.Card.Faces[1].Triggers) == 0 || o.Card.Faces[1].Triggers[0].Mode != "UnlockDoor" {
		t.Fatalf("precondition: alternate locked face has no UnlockDoor trigger: %+v", o)
	}
	before := len(e.pendingTriggers)
	e.emit(events.Event{Kind: events.DoorUnlock, Obj: id, Amount: 2})
	if !o.DoorUnlocked(1) || len(e.pendingTriggers) <= before {
		t.Fatal("first unlock did not queue the face trigger")
	}
	queued := len(e.pendingTriggers)
	e.emit(events.Event{Kind: events.DoorUnlock, Obj: id, Amount: 2})
	if len(e.pendingTriggers) != queued {
		t.Fatalf("repeated unlock queued %d more triggers", len(e.pendingTriggers)-queued)
	}
}
