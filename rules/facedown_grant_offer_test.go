package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestFaceDownGrantorOffersNoGrantedManaAbility pins cardfuzz batch9 line 1
// on the real corpus pair: Citanul Hierophants ("Creatures you control have
// '{T}: Add {G}.'") entered the battlefield FACE DOWN, so its printed
// Continuous static does not exist (CR 708.8). The priority offer read the
// legal-actions walk's cached action statics, whose scan had no face-down
// gate, and offered "Activate Grizzly Bears for mana"; the activation read
// the fresh activeStatics walk, which does gate face-down sources, found no
// mana ability and returned without an event -- the bot re-chose the no-op
// option until the livelock watcher fired.
//
// Face down: no granted mana option, and every offered option submits to an
// observable effect. Face up (the control): the grant is offered and
// activating it adds {G}.
func TestFaceDownGrantorOffersNoGrantedManaAbility(t *testing.T) {
	t.Parallel()
	for _, faceDown := range []bool{true, false} {
		name := "face_up"
		if faceDown {
			name = "face_down"
		}
		t.Run(name, func(t *testing.T) {
			hiero := tokenReplCorpusCard(t, "Citanul Hierophants")
			bears := tokenReplCorpusCard(t, "Grizzly Bears")
			e, cfg := tokenReplGame(t, 9181, hiero, bears)
			toMain1(t, e)
			var hieroID state.ObjID
			for _, z := range []state.Zone{state.ZLibrary, state.ZHand} {
				for _, id := range e.G.Zone(z, 0) {
					if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Citanul Hierophants" && hieroID == 0 {
						ev := events.Event{Kind: events.MoveZone, Obj: id, Player: 0, From: z, To: state.ZBattlefield}
						if faceDown {
							ev.Counter, ev.Secret = "entered_face_down", true
						}
						e.emit(ev)
						hieroID = id
					}
				}
			}
			if hieroID == 0 || e.G.Obj(hieroID).FaceDown != faceDown {
				t.Fatalf("Citanul Hierophants face-down = %v, want %v", e.G.Obj(hieroID).FaceDown, faceDown)
			}
			bearID := moveSeededCard(t, e, 0, bears, state.ZBattlefield)
			e.priorityRound()
			gainsDriveToStep(t, e, 3, 0, state.StepMain1)

			e.pending = nil
			e.askPriority(0)
			var offer *decision.Option
			for _, o := range e.Pending().Options {
				if o.Kind == "activate" && o.Obj == bearID {
					o := o
					offer = &o
				}
			}
			assertEveryPriorityOptionActs(t, e)
			if faceDown {
				if offer != nil {
					t.Fatalf("face-down Hierophants still grants Grizzly Bears a mana ability: offered %+v", *offer)
				}
				replayCheck(t, e, cfg)
				return
			}
			if offer == nil {
				t.Fatalf("face-up Hierophants grants Grizzly Bears no mana ability")
			}
			before := len(e.L.Events)
			submitChoices(t, e, offer.Index)
			added := false
			for _, ev := range e.L.Events[before:] {
				if ev.Kind == events.ManaAdd && ev.Player == 0 {
					added = true
				}
			}
			if !added {
				t.Fatalf("activating the granted mana ability added no mana")
			}
			replayCheck(t, e, cfg)
		})
	}
}
