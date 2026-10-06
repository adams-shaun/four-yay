package rules_test

import (
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil/feedback"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// fb-20261006T100405Z: at turn 9, main1, seat 0 held "Name Sticker" Goblin
// ({2}{R}) and Treasonous Ogre ({3}{R}) with two mana sources (Badlands,
// Command Tower) untapped, and Rakdos, the Muscle in the command zone. The
// hand card rendered a disabled "(tap other mana first)" row even though the
// planner had PROVED those three casts unpayable -- no amount of tapping the
// two sources reaches them. The engine already knew (PotentialPaymentPlans
// "insufficient"); the wire did not carry it, so the client indexed them as
// plan-less hand casts. This test pins the projection: the three
// over-costed casts are marked Payable=false while the two affordable ones
// (Chthonian Nightmare, Songs of the Damned) carry no verdict.
func TestFeedbackRepro20261006T100405Z_4be7e4f7(t *testing.T) {
	e, oracle := engineWithBuiltDecision(t, filepath.Join("testdata", "feedback", "20261006T100405Z-4be7e4f7"))

	// Precondition: the capture point really is the reported priority window.
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("precondition: want seat 0's priority decision, got %+v", d)
	}
	if e.G.Turn != 9 || e.G.Step != state.StepMain1 {
		t.Fatalf("precondition: want turn 9 main1, got turn %d step %s", e.G.Turn, e.G.Step)
	}
	// Precondition: resolve the report's cards by name in the zones the
	// annotation reads (hand, and the command zone for Rakdos).
	byName := map[string]state.ObjID{}
	for _, z := range []state.Zone{state.ZHand, state.ZCommand} {
		for _, id := range e.G.Zone(z, 0) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil {
				byName[o.Face().Name] = id
			}
		}
	}
	ogre, ok := byName["Treasonous Ogre"]
	if !ok {
		t.Fatalf("precondition: Treasonous Ogre not in seat 0's hand/command: names %v", byName)
	}
	goblin, ok := byName[`"Name Sticker" Goblin`]
	if !ok {
		t.Fatalf("precondition: \"Name Sticker\" Goblin not in seat 0's hand/command")
	}
	rakdos, ok := byName["Rakdos, the Muscle"]
	if !ok {
		t.Fatalf("precondition: Rakdos, the Muscle not in seat 0's command zone")
	}
	nightmare, ok := byName["Chthonian Nightmare"]
	if !ok {
		t.Fatalf("precondition: Chthonian Nightmare not in seat 0's hand")
	}
	songs, ok := byName["Songs of the Damned"]
	if !ok {
		t.Fatalf("precondition: Songs of the Damned not in seat 0's hand")
	}

	// The oracle: the planner's own verdicts. The three over-costed casts
	// are PROVEN unpayable; the two affordable ones are payable.
	reason := map[state.ObjID]string{}
	// (on a Clone taken before the builder ran: the planner fills the
	// decision's cast-plan memo, which the projection under test must get from
	// the offer builder alone.)
	for _, p := range oracle.PotentialPaymentPlans(0) {
		if p.Action.Kind == "cast" {
			reason[p.Action.Obj] = p.Reason
		}
	}
	for _, id := range []state.ObjID{ogre, goblin, rakdos} {
		if reason[id] != "insufficient" {
			t.Fatalf("precondition: planner verdict for obj %d (%s) = %q, want \"insufficient\"", id, e.G.Obj(id).Face().Name, reason[id])
		}
	}
	if reason[nightmare] == "insufficient" || reason[songs] == "insufficient" {
		t.Fatalf("precondition: a payable cast is \"insufficient\": nightmare=%q songs=%q", reason[nightmare], reason[songs])
	}

	// The projection under test: PotentialActions carries the same verdict.
	payable := map[state.ObjID]*bool{}
	for _, a := range e.PotentialActions(0) {
		if a.Kind == "cast" {
			payable[a.Obj] = a.Payable
		}
	}
	for _, id := range []state.ObjID{ogre, goblin, rakdos} {
		p, ok := payable[id]
		if !ok {
			t.Fatalf("precondition: obj %d (%s) absent from potential_actions", id, e.G.Obj(id).Face().Name)
		}
		if p == nil || *p {
			t.Errorf("obj %d (%s): Payable = %v, want a proven false", id, e.G.Obj(id).Face().Name, boolPtrString(p))
		}
	}
	for _, id := range []state.ObjID{nightmare, songs} {
		if p, ok := payable[id]; ok && p != nil {
			t.Errorf("obj %d (%s): Payable = %v, want nil (no proof of unpayability)", id, e.G.Obj(id).Face().Name, boolPtrString(p))
		}
	}

	// The client's view carries the same verdict: view.Project calls exactly
	// this engine method (view/view.go PotentialActions), so the seat's
	// potential_actions on the wire is where the disable comes from.
	pv := view.Project(e.G, e, 0, e.Pending())
	var seat0 *view.PlayerView
	for i := range pv.Players {
		if pv.Players[i].ID == 0 {
			seat0 = &pv.Players[i]
			break
		}
	}
	if seat0 == nil {
		t.Fatalf("precondition: view carries no seat 0")
	}
	viewPayable := map[state.ObjID]*bool{}
	for _, a := range seat0.PotentialActions {
		if a.Kind == "cast" {
			viewPayable[a.Obj] = a.Payable
		}
	}
	for _, id := range []state.ObjID{ogre, goblin} {
		if p := viewPayable[id]; p == nil || *p {
			t.Errorf("view obj %d (%s): Payable = %v, want a proven false", id, e.G.Obj(id).Face().Name, boolPtrString(p))
		}
	}
}

// engineWithBuiltDecision replays the report to its final priority decision the
// way a live host reaches it: an earlier priority decision of seat 0 was already
// projected (view.Project reads PotentialActions, which demands the full
// potential walk the offer builder then shares), and the final decision's offer
// builder (EnsurePaymentActions, run by host/match.go before it projects the
// view) has run. PotentialActions reads the builder's cast verdicts and plans
// nothing itself, so a bare replay (no builder) would carry no verdict.
func engineWithBuiltDecision(t *testing.T, dir string) (built, unbuilt *rules.Engine) {
	t.Helper()
	l, cfg, _, err := feedback.Load(dir)
	if err != nil {
		t.Fatalf("feedback: %v", err)
	}
	for k := len(l.Intents) - 1; k > 0; k-- {
		e, err := replay.ReplayTo(l, cfg, k)
		if err != nil {
			t.Fatalf("replay to intent %d: %v", k, err)
		}
		if d := e.Pending(); d == nil || d.Kind != decision.KPriority || d.Player != 0 {
			continue
		}
		e.PotentialActions(0) // the host's earlier projection: demands the full walk
		for i := k; i < len(l.Intents); i++ {
			if err := e.Submit(l.Intents[i]); err != nil {
				t.Fatalf("replay intent %d: %v", i, err)
			}
		}
		unbuilt = e.Clone()
		if len(e.EnsurePaymentActions()) == 0 {
			t.Fatalf("precondition: the offer builder produced no payment actions")
		}
		return e, unbuilt
	}
	t.Fatalf("no earlier priority decision of seat 0 to project")
	return nil, nil
}

func boolPtrString(p *bool) string {
	if p == nil {
		return "nil"
	}
	if *p {
		return "true"
	}
	return "false"
}
