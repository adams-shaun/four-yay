package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// retargetEvents collects the CombatRetarget events a resolution emitted.
func retargetEvents(h *fakeHost) []events.Event {
	var out []events.Event
	for _, e := range h.log {
		if e.Kind == events.CombatRetarget {
			out = append(out, e)
		}
	}
	return out
}

// noteTexts collects the Notes a resolution emitted.
func noteTexts(h *fakeHost) []string {
	var out []string
	for _, e := range h.log {
		if e.Kind == events.Note {
			out = append(out, e.Text)
		}
	}
	return out
}

// attackingBoard builds a three-seat game with an attacking creature for
// seat 0 (g.Active) pointed at seat 1 and blocked by a seat-1 blocker — the
// board every no-host stand-in test below reads.
func attackingBoard(t *testing.T) (*fakeHost, *state.Object) {
	t.Helper()
	h := newHost(t, 3)
	attacker := h.g.AddObject(mkCard(t, "Name:Grizzly Bears\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	blocker := h.g.AddObject(mkCard(t, "Name:Memnite\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n"), 1)
	for _, id := range []state.ObjID{attacker.ID, blocker.ID} {
		h.Emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
	}
	// Mark the attack as the declare-attackers step left it: attacking
	// seat 1, blocked by the Memnite.
	h.g.Active = 0
	h.g.Obj(attacker.ID).IsAttacking = true
	h.g.Obj(attacker.ID).Attacking = 1
	h.g.Obj(attacker.ID).BlockedBy = []state.ObjID{blocker.ID}
	h.g.NoteBlockers() // written directly, not by DeclareBlockers
	return h, h.g.Obj(attacker.ID)
}

// TestChangeCombatantsNoHostKeepsTheDefender pins the R-9 stand-in: a host
// that cannot answer keeps the original defender, records exactly one Note,
// and emits no CombatRetarget.
func TestChangeCombatantsNoHostKeepsTheDefender(t *testing.T) {
	h, att := attackingBoard(t)
	before := att.BlockedBy
	Resolve(h, &Ctx{Source: att.ID, Controller: 0},
		sa(t, "DB$ ChangeCombatants | Defined$ Self | Attacking$ True"))
	if got := retargetEvents(h); len(got) != 0 {
		t.Fatalf("no-host stand-in emitted %+v, want no retarget event", got)
	}
	notes := noteTexts(h)
	if len(notes) != 1 || !strings.Contains(notes[0], "no engine host") {
		t.Fatalf("notes = %q, want exactly one no-host Note", notes)
	}
	if att.Attacking != 1 {
		t.Fatalf("attacker.Attacking = %d, want the original 1", att.Attacking)
	}
	if len(att.BlockedBy) != len(before) {
		t.Fatalf("BlockedBy = %v, want unchanged %v", att.BlockedBy, before)
	}
}

// TestChangeCombatantsUnsupportedAttackingValueIsLoud pins the out-of-scope
// Attacking$ shapes (midnight_crusader_shuttle's RememberedPlayer,
// capricopian's Player.OpponentOf CardController, portal_manipulator's
// TargetedPlayer, tahngarth_first_mate's .Defending form): one loud Note
// naming the shape, no event, no move.
func TestChangeCombatantsUnsupportedAttackingValueIsLoud(t *testing.T) {
	h, att := attackingBoard(t)
	Resolve(h, &Ctx{Source: att.ID, Controller: 0},
		sa(t, "DB$ ChangeCombatants | Defined$ Self | Attacking$ TargetedPlayer"))
	if got := retargetEvents(h); len(got) != 0 {
		t.Fatalf("out-of-scope shape emitted %+v, want no retarget event", got)
	}
	notes := noteTexts(h)
	if len(notes) != 1 || !strings.Contains(notes[0], "TargetedPlayer") {
		t.Fatalf("notes = %q, want one Note naming Attacking$ TargetedPlayer", notes)
	}
	if att.Attacking != 1 {
		t.Fatalf("attacker.Attacking = %d, want the original 1", att.Attacking)
	}
}
