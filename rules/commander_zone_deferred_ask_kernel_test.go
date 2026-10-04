package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestCommanderZoneAskIsNotOverwrittenByLaterResolutionAskKernel is the
// botbench livelock shape: a spell exiles the commander (parking the CR 903.9
// move and asking its owner) and the same resolution then asks "its
// controller may search". The commander-zone choice is posed first and never
// overwritten; its answer moves the commander (command zone on accept, exile
// on decline); the search choice follows; exactly one commander-zone ask is
// logged and a log-only replay agrees.
func TestCommanderZoneAskIsNotOverwrittenByLaterResolutionAskKernel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		choice int
		want   state.Zone
	}{
		{"accept", 0, state.ZCommand},
		{"decline", 1, state.ZExile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg := colourIdentityGame(t, 11, FormatCommander, card(t, tinyCmdSrc), nil, card(t, exileThenSearchFixtureSrc))
			toMain1(t, e)
			cmd := fieldCommander(t, e, 0, 0)
			spell := moveToHandByName(t, e, 0, "Fixture Banish Then Search")
			e.priorityRound()
			opt := castByName(t, e, 0, "Fixture Banish Then Search")
			if opt == nil {
				t.Fatal("fixture spell not castable")
			}
			submit(t, e, opt.Index)
			d := e.Pending()
			if d == nil || d.Kind != decision.KTarget {
				t.Fatalf("pending after cast = %v, want the target ask", pendingKind(d))
			}
			picked := -1
			for _, o := range d.Options {
				if o.Obj == cmd {
					picked = o.Index
				}
			}
			if picked < 0 {
				t.Fatalf("commander not offered as a target: %+v", d.Options)
			}
			submit(t, e, picked)
			d = passPriorityUntilNonPriority(t, e)
			if d.Kind != decision.KCommanderZone {
				t.Fatalf("pending after the exile = %v, want the commander-zone choice", pendingKind(d))
			}
			if got := e.G.Obj(cmd).Zone; got != state.ZBattlefield {
				t.Fatalf("commander zone before the answer = %v, want battlefield (the move is parked)", got)
			}
			submit(t, e, tc.choice)
			if got := e.G.Obj(cmd).Zone; got != tc.want {
				t.Fatalf("commander zone after answer = %v, want %v", got, tc.want)
			}
			d = e.Pending()
			if d == nil || d.Kind != decision.KChoose {
				t.Fatalf("pending after the commander-zone answer = %v, want the deferred search choice", pendingKind(d))
			}
			for i := 0; i < 10 && e.Pending() != nil && e.Pending().Kind != decision.KPriority; i++ {
				submit(t, e, 0)
			}
			if got := e.G.Obj(spell).Zone; got != state.ZGraveyard {
				t.Fatalf("spell zone after resolution = %v, want graveyard", got)
			}
			asks := 0
			for _, ev := range e.L.Events {
				if ev.Kind == events.DecisionAsk && ev.Text == string(decision.KCommanderZone) {
					asks++
				}
			}
			if asks != 1 {
				t.Fatalf("commander-zone asks = %d, want 1", asks)
			}
			re := commanderReplayFromLog(t, cfg, e.L.Events)
			if got := re.Obj(cmd).Zone; got != tc.want {
				t.Fatalf("log-only replay commander zone = %v, want %v", got, tc.want)
			}
		})
	}
}
