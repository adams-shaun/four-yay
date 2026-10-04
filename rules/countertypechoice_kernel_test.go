package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestGrimdancerCounterTypeChoice drives its real ETB replacement. The one
// answer must be two distinct individual kinds, never a composite kind.
func TestGrimdancerCounterTypeChoice(t *testing.T) {
	t.Parallel()
	grim := mustCorpusCardT(t, "Grimdancer")
	e, cfg := tokenReplGame(t, 919, grim)
	toMain1(t, e)
	id := state.ObjID(0)
	for _, z := range []state.Zone{state.ZLibrary, state.ZHand} {
		for _, oid := range e.G.Zone(z, 0) {
			if o := e.G.Obj(oid); o != nil && o.Face() != nil && o.Face().Name == grim.Faces[0].Name {
				id = oid
				break
			}
		}
		if id != 0 {
			break
		}
	}
	if id == 0 {
		t.Fatal("precondition: Grimdancer is not in the seeded library")
	}
	from := state.ZLibrary
	if e.G.Obj(id).Zone == state.ZHand {
		from = state.ZHand
	}
	e.pending = nil
	e.probe(func() {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: state.ZBattlefield})
	})
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "counter_kinds" || d.Min != 2 || d.Max != 2 || len(d.Options) != 3 {
		t.Fatalf("Grimdancer choice = %+v, want exactly two of three kinds", d)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index, d.Options[0].Index}}); err == nil {
		t.Fatal("precondition: duplicate kind answer unexpectedly validates")
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[2].Index)
	if got := e.G.Obj(id).Counter(d.Options[0].Label); got != 1 {
		t.Fatalf("first chosen counter = %d, want 1", got)
	}
	if got := e.G.Obj(id).Counter(d.Options[2].Label); got != 1 {
		t.Fatalf("second chosen counter = %d, want 1", got)
	}
	if got := e.G.Obj(id).Counter("Menace,Deathtouch,Lifelink"); got != 0 {
		t.Fatalf("composite counter = %d, want 0", got)
	}
	replayCheck(t, e, cfg)
}
