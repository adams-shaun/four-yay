package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestSetAudit_eoe_FamishedWorldsire_DevourLandKernel: Devour with a typed
// operand (`K:Devour:3:Land`) offers only LANDS and enters with three
// counters per land sacrificed (CR 702.83).
func TestSetAudit_eoe_FamishedWorldsire_DevourLandKernel(t *testing.T) {
	t.Parallel()
	e := handEngine(t,
		corpusAlternativeCard(t, "Famished Worldsire"),
		corpusAlternativeCard(t, "Forest"),
		corpusAlternativeCard(t, "Forest"),
		corpusAlternativeCard(t, "Grizzly Bears"))
	for _, name := range []string{"Forest", "Forest", "Grizzly Bears"} {
		id := searchMoveByName(t, e, name, state.ZHand)
		placeOnBattlefield(t, e, id)
	}
	ws := searchMoveByName(t, e, "Famished Worldsire", state.ZHand)
	kr9Probe(e, func() {
		e.emit(events.Event{Kind: events.MoveZone, Obj: ws, From: state.ZHand, To: state.ZBattlefield})
	})
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "sacrifice" || d.Min != 0 || d.Max != 2 {
		t.Fatalf("Devour land ask = %+v, want sacrifice 0..2 (only the two Forests)", d)
	}
	for _, o := range d.Options {
		if name := e.G.Obj(o.Obj).Face().Name; name != "Forest" {
			t.Fatalf("Devour land offered a non-land (%s)", name)
		}
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
	if got := e.G.Obj(ws).Counter("P1P1"); got != 6 {
		t.Fatalf("Famished Worldsire entered with %d P1P1, want 6 (2 lands x Devour 3)", got)
	}
}
