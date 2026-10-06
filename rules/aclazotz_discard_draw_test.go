package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const aclazotzDiscardDraw = "Name:Aclazotz, Deepest Betrayal\nManaCost:2 B B\nTypes:Creature Vampire God\nPT:4/4\n" +
	"T:Mode$ Attacks | ValidCard$ Card.Self | Execute$ TrigDiscard | TriggerDescription$ x\n" +
	"SVar:TrigDiscard:DB$ Discard | Mode$ TgtChoose | Defined$ Opponent | NumCards$ 1 | RememberDiscarded$ True | SubAbility$ DBDraw\n" +
	"SVar:DBDraw:DB$ Draw | NumCards$ X | SubAbility$ DBCleanup\n" +
	"SVar:X:PlayerCountOpponents$Amount/Minus.Remembered$Amount\n" +
	"SVar:DBCleanup:DB$ Cleanup | ClearRemembered$ True\nOracle:x\n"

func TestAclazotzDrawsForOpponentWhoCannotDiscard(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		opponentHas bool
		wantDraw    int
	}{
		{name: "empty hand", wantDraw: 1},
		{name: "one card", opponentHas: true, wantDraw: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := combatEngine(t)
			aclazotz := onBoardReady(t, e, 0, aclazotzDiscardDraw)
			// Empty the opponent's opening hand using canonical discard events.
			for _, id := range append([]state.ObjID(nil), e.G.Zone(state.ZHand, 1)...) {
				e.emit(events.Discard(id, 1))
			}
			if tc.opponentHas {
				lib := e.G.Zone(state.ZLibrary, 1)
				if len(lib) == 0 {
					t.Fatal("precondition: opponent library must supply the test card")
				}
				e.emit(events.Event{Kind: events.MoveZone, Obj: lib[0], From: state.ZLibrary, To: state.ZHand, Player: 1})
			}
			if got := len(e.G.Zone(state.ZHand, 1)); got != btoi(tc.opponentHas) {
				t.Fatalf("precondition: opponent hand has %d cards, want %d", got, btoi(tc.opponentHas))
			}
			if o := e.G.Obj(aclazotz); o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("precondition: Aclazotz %d is not on the battlefield", aclazotz)
			}

			p0HandBefore := len(e.G.Zone(state.ZHand, 0))
			start := len(e.L.Events)
			atAttack(t, e, aclazotz)
			discards, triggerPushes := 0, 0
			for _, ev := range e.L.Events[start:] {
				if events.IsDiscard(ev) && ev.Player == 1 {
					discards++
				}
				if ev.Kind == events.TriggerPush && ev.Obj == aclazotz {
					triggerPushes++
				}
			}
			handDelta := len(e.G.Zone(state.ZHand, 0)) - p0HandBefore
			if handDelta != tc.wantDraw {
				t.Fatalf("p0 hand delta is %d, want %d", handDelta, tc.wantDraw)
			}
			if triggerPushes != 1 {
				t.Fatalf("Aclazotz trigger pushes = %d, want 1", triggerPushes)
			}
			wantDiscards := btoi(tc.opponentHas)
			if discards != wantDiscards {
				t.Fatalf("opponent discarded %d cards, want %d", discards, wantDiscards)
			}
		})
	}
}

func btoi(v bool) int {
	if v {
		return 1
	}
	return 0
}
