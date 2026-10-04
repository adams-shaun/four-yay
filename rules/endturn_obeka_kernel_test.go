package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Obeka is controlled by seat 0 on seat 1's turn: the ACTIVE player, not
// Obeka's controller, must decide whether to end the turn.
func TestEndTurnObekaActivePlayerMayDeclineOrAccept(t *testing.T) {
	for _, tc := range []struct {
		name   string
		seed   uint64
		answer int
		ends   bool
	}{
		{"decline", 8821, 1, false},
		{"accept", 8822, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg, id := gateFixture(t, tc.seed, "Obeka, Brute Chronologist")
			id = gateMoveFromLibrary(t, e, "Obeka, Brute Chronologist", state.ZBattlefield)
			o := e.G.Obj(id)
			if o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 ||
				o.Face() == nil || len(o.Face().Abilities) == 0 ||
				o.Face().Abilities[0].API != "EndTurn" ||
				o.Face().Abilities[0].Params["Defined"] != "ActivePlayer" ||
				o.Face().Abilities[0].Params["Optional"] != "True" {
				t.Fatalf("precondition: Obeka is not an Optional$ ActivePlayer EndTurn permanent: %+v", o)
			}
			driveToStep(t, e, 2, 1, state.StepMain1)
			if e.G.Active != 1 || e.G.Turn != 2 {
				t.Fatalf("precondition: active = %d, turn = %d, want seat 1 turn 2", e.G.Active, e.G.Turn)
			}
			e.emit(events.Event{Kind: events.AbilityPush, Obj: id, Player: 0, Amount: 0})
			if len(e.G.Stack) != 1 {
				t.Fatalf("precondition: Obeka ability not on stack: %v", e.G.Stack)
			}
			e.pending = nil // the resolution runs from a quiet engine, as from a pass
			e.resolveTop()
			d := e.Pending()
			if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "endturn_optional" ||
				d.Player != 1 || d.Min != 1 || d.Max != 1 || len(d.Options) != 2 ||
				d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
				t.Fatalf("Obeka election = %+v, want active seat 1 yes/no", d)
			}
			if countEvents(e, func(ev events.Event) bool { return ev.Kind == events.EndTurn }) != 0 {
				t.Fatal("turn ended before active player answered")
			}
			submitChoices(t, e, tc.answer)
			n := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.EndTurn })
			if tc.ends {
				if n != 1 || e.G.Step != state.StepCleanup || len(e.G.Stack) != 0 {
					t.Fatalf("accept: EndTurn events = %d, step = %s, stack = %v; want 1, cleanup, empty", n, e.G.Step, e.G.Stack)
				}
			} else if n != 0 || e.G.Step != state.StepMain1 || len(e.G.Stack) != 0 {
				t.Fatalf("decline: EndTurn events = %d, step = %s, stack = %v; want 0, main1, empty", n, e.G.Step, e.G.Stack)
			}
			replayCheck(t, e, cfg)
		})
	}
}
