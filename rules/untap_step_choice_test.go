package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestPandoricaStaysTappedByChoice proves the bare K: wording is a real
// untap-step election, rather than a card-name special case or an inert
// keyword. The permanent starts tapped and on the battlefield, so both the
// ask and the retained tapped state are load-bearing assertions.
func TestPandoricaStaysTappedByChoice(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, mustCorpusCard(t, reg, "The Pandorica"))
	id := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.Tap, Obj: id})
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || !o.Tapped {
		t.Fatal("precondition: Pandorica must be a tapped battlefield permanent")
	}
	e.G.Step, e.G.Active = state.StepUntap, 0
	if e.finishUntapStep(0) {
		t.Fatal("untap scan completed without posing Pandorica's election")
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "" || len(d.Options) != 2 {
		t.Fatalf("pending = %+v, want two-option untap election", d)
	}
	if d.Options[0].Kind != "untap" || d.Options[1].Kind != "keep_tapped" {
		t.Fatalf("untap options = %+v, want untap then keep_tapped", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{1}}); err != nil {
		t.Fatal(err)
	}
	if !e.G.Obj(id).Tapped {
		t.Fatal("Pandorica untapped after choosing to keep it tapped")
	}
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Choose && ev.Obj == id && ev.Counter == "untap" && ev.Text == "keep" {
			found = true
		}
	}
	if !found {
		t.Fatalf("untap election answer was not recorded as an event: %+v", e.L.Events)
	}
}
