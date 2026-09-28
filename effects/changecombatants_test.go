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

// TestChangeCombatantsAnsweredRetargetsAndUnblocks drives the answered
// re-entry directly through the "choice" transport: the answered defender
// re-points the attack and clears BlockedBy.
func TestChangeCombatantsAnsweredRetargetsAndUnblocks(t *testing.T) {
	h, att := attackingBoard(t)
	Resolve(h, &Ctx{Source: att.ID, Controller: 0,
		Choice: []state.Target{{Player: 2, IsPlayer: true}}, ChoiceDone: true, ChoiceTarget: 0},
		sa(t, "DB$ ChangeCombatants | Defined$ Self | Attacking$ True"))
	got := retargetEvents(h)
	if len(got) != 1 || got[0].Obj != att.ID || got[0].Player != 2 {
		t.Fatalf("retarget events = %+v, want one naming %d -> seat 2", got, att.ID)
	}
	if att.Attacking != 2 {
		t.Fatalf("attacker.Attacking = %d, want 2", att.Attacking)
	}
	if len(att.BlockedBy) != 0 {
		t.Fatalf("BlockedBy = %v, want cleared (the re-pointed attack is unblocked)", att.BlockedBy)
	}
	if got := noteTexts(h); len(got) != 0 {
		t.Fatalf("answered path emitted notes %q, want none", got)
	}
}

// TestChangeCombatantsKeepCurrentDefenderIsATrueNoOp pins Forge's
// addToCombat guard on the answered re-entry: answering the attacker's
// CURRENT defender (blocked, per the attackingBoard fixture) emits NO
// CombatRetarget and preserves BlockedBy — clearing it would silently
// unblock an attack whose defender did not change and send its damage to
// the player instead of the blocker.
func TestChangeCombatantsKeepCurrentDefenderIsATrueNoOp(t *testing.T) {
	h, att := attackingBoard(t)
	before := att.BlockedBy
	Resolve(h, &Ctx{Source: att.ID, Controller: 0,
		Choice: []state.Target{{Player: 1, IsPlayer: true}}, ChoiceDone: true, ChoiceTarget: 0},
		sa(t, "DB$ ChangeCombatants | Defined$ Self | Attacking$ True"))
	if got := retargetEvents(h); len(got) != 0 {
		t.Fatalf("keep-current answer emitted %+v, want no retarget event", got)
	}
	if att.Attacking != 1 {
		t.Fatalf("attacker.Attacking = %d, want the original 1", att.Attacking)
	}
	if len(att.BlockedBy) != len(before) {
		t.Fatalf("BlockedBy = %v, want unchanged %v", att.BlockedBy, before)
	}
	if got := noteTexts(h); len(got) != 0 {
		t.Fatalf("keep-current answer emitted notes %q, want none", got)
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

// TestChangeCombatantsWindshaperAsksEachAttacker pins the Defined$ Valid
// Creature.attacking sweep (Windshaper Planetar's shape): two attackers are
// asked one after the other through the same SA — the answered re-entry
// skips the first (already answered) attacker and poses the second's ask.
func TestChangeCombatantsWindshaperAsksEachAttacker(t *testing.T) {
	h := newHost(t, 3)
	a1 := h.g.AddObject(mkCard(t, "Name:Vanquisher\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	a2 := h.g.AddObject(mkCard(t, "Name:Sentinel\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	for _, id := range []state.ObjID{a1.ID, a2.ID} {
		h.Emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
	}
	h.g.Active = 0
	for _, o := range []*state.Object{h.g.Obj(a1.ID), h.g.Obj(a2.ID)} {
		o.IsAttacking = true
		o.Attacking = 1
	}
	line := "DB$ ChangeCombatants | Defined$ Valid Creature.attacking | Optional$ True | Attacking$ True"
	s := sa(t, line)
	// First pass: the Optional note, then attacker 0's ask suspends.
	Resolve(h, &Ctx{Source: a1.ID, Controller: 0}, s)
	if got := retargetEvents(h); len(got) != 0 {
		t.Fatalf("suspended pass emitted %+v, want nothing yet", got)
	}
	notes := noteTexts(h)
	// The no-host path never suspends, so pass 1 answers BOTH attackers
	// deterministically: the Optional stand-in Note, then one no-host Note
	// per attacker, in Defined$ walk order.
	if len(notes) != 3 || !strings.Contains(notes[0], "Optional$ True") ||
		!strings.Contains(notes[1], "Vanquisher") || !strings.Contains(notes[2], "Sentinel") {
		t.Fatalf("pass-1 notes = %q, want the Optional stand-in then one no-host Note per attacker", notes)
	}
	pass1 := len(notes)
	// Re-enter with attacker 0 answered (re-pointed at seat 2): attacker 0
	// consumes the answer and emits; attacker 1 poses (and, hostless,
	// declines) its own fresh ask; the Optional stand-in Note does not repeat.
	Resolve(h, &Ctx{Source: a1.ID, Controller: 0,
		Choice: []state.Target{{Player: 2, IsPlayer: true}}, ChoiceDone: true, ChoiceTarget: 0}, s)
	got := retargetEvents(h)
	if len(got) != 1 || got[0].Obj != a1.ID || got[0].Player != 2 {
		t.Fatalf("retarget events = %+v, want one naming %d -> seat 2", got, a1.ID)
	}
	notes = noteTexts(h)
	if len(notes) != pass1+1 || !strings.Contains(notes[pass1], "Sentinel") {
		t.Fatalf("pass-2 notes = %q, want exactly attacker 1's no-host Note", notes)
	}
}
