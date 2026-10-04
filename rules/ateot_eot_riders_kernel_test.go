package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestAtEOTPurphorosHandMoveIsSacrificedAtEOTKernel pins the exact-Hand
// ChangeZone dispatch carrying an AtEOT$ rider: Purphoros's printed {2}{R}
// ability (Optional$ You) poses its confirm gate, then the hand_move pick; the
// picked Goblin Piker enters with exactly one DelayedRegister and the next end
// step sacrifices it while Purphoros stays.
func TestAtEOTPurphorosHandMoveIsSacrificedAtEOTKernel(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := ateotEngine(t, reg, "Purphoros, Bronze-Blooded", "Goblin Piker")
	purph := ateotFind(t, e, "Purphoros, Bronze-Blooded", 0)
	ateotTo(t, e, purph, state.ZLibrary, state.ZBattlefield)
	gob := ateotFind(t, e, "Goblin Piker", 0)
	if e.G.Obj(gob).Zone != state.ZHand {
		ateotTo(t, e, gob, state.ZLibrary, state.ZHand)
	}
	card := searchCorpusCard(t, reg, "Purphoros, Bronze-Blooded")
	var sa *cards.SA
	for _, ab := range card.Faces[0].Abilities {
		if ab.API == "ChangeZone" {
			sa = ab
		}
	}
	if sa == nil {
		t.Fatal("Purphoros has no ChangeZone ability")
	}
	kr4Resolve(e, func() *effects.Ctx {
		return &effects.Ctx{Source: purph, Controller: 0, SVars: card.Faces[0].SVars}
	}, sa)
	d := e.Pending()
	if d == nil || d.ResumeKind != "hand_move_confirm" {
		t.Fatalf("pending %+v, want the hand_move_confirm gate (Optional$ You asks before the pick)", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "hand_move" {
		t.Fatalf("pending %+v, want the hand_move KChoose", d)
	}
	submitChoices(t, e, kr4Option(t, d, gob))
	kr4Settle(e)
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(gob); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("the picked piker is %+v, want on seat 0's battlefield", o)
	}
	registrations := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.DelayedRegister && ev.Obj == gob {
			registrations++
		}
	}
	if registrations != 1 {
		t.Fatalf("the hand move emitted %d DelayedRegister events for the piker, want exactly 1", registrations)
	}
	replayCheck(t, e, cfg)

	ateotDriveToStep(t, e, e.G.Turn, e.G.Active, state.StepEnd)
	for e.putTriggersOnStack() {
		answerTriggerOrders(t, e)
		passUntilStackEmpty(t, e, 20)
	}
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(gob).Zone; z != state.ZGraveyard {
		t.Fatalf("the hand-moved piker survived the end step (zone %s), want it sacrificed", z)
	}
	if z := e.G.Obj(purph).Zone; z != state.ZBattlefield {
		t.Fatalf("Purphoros itself moved at the end step (zone %s), want it kept", z)
	}
	replayCheck(t, e, cfg)
}
