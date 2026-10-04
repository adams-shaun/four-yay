package rules

// Kernel-era restoration of TestReplacementQueueSurvivesNestedAskMidResolution
// (replacement_queue_nested_ask_test.go): several lands returned at once, each
// with a CR 616.1 entry competition, one of whose bodies asks (a shock land's
// pay-2-life): every order ask and the nested ask are posed, every land
// enters, and the trigger resolves (and its GainLife rider runs) exactly once.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestReplacementQueueSurvivesNestedAskMidResolutionKernel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		lands []string
	}{
		// The nested ask is posed while later competitions are queued.
		{"queued competitions behind the nested ask", []string{shockLandSrc, horizonTaplandSrc, horizonTaplandSrc}},
		// The nested ask is posed by the last (only) competition: the
		// suspended resolution chains behind the nested frame.
		{"nested ask on the last competition", []string{horizonTaplandSrc, shockLandSrc}},
		{"lone competition asks", []string{shockLandSrc}},
	} {
		t.Run(tc.name, func(t *testing.T) { kr7ReplacementQueueNestedAsk(t, tc.lands) })
	}
}

func kr7ReplacementQueueNestedAsk(t *testing.T, landSrcs []string) {
	e := layerEngine(t)
	e.pending = nil
	onBoard(t, e, 0, horizonExplorerSrc)
	host := onBoard(t, e, 0, returnLandsSrc)
	var lands []state.ObjID
	for _, src := range landSrcs {
		o := e.G.AddObject(card(t, src), 0)
		o.Zone = state.ZGraveyard
		e.G.SetZone(state.ZGraveyard, 0, append(e.G.Zone(state.ZGraveyard, 0), o.ID))
		lands = append(lands, o.ID)
	}

	drawn := e.G.Zone(state.ZLibrary, 0)[0]
	e.emit(events.Event{Kind: events.Draw, Player: 0, Obj: drawn, From: state.ZLibrary, To: state.ZHand, Secret: true})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Source != host {
		t.Fatalf("stack = %v, want the returner's one trigger", e.G.Stack)
	}
	trig := e.G.Stack[0]
	e.pending = nil // resolve directly, outside the pending priority window
	e.resolveTop()

	orderAsks, modeAsks := 0, 0
	for i := 0; i < 20; i++ {
		d := e.Pending()
		if d == nil || (d.Kind != decision.KReplacement && d.Kind != decision.KModes) {
			break
		}
		if d.Kind == decision.KReplacement {
			orderAsks++
		} else {
			modeAsks++
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatalf("submit %s: %v", d.Kind, err)
		}
	}
	if orderAsks < len(landSrcs) || modeAsks < 1 {
		t.Fatalf("order asks = %d, mode asks = %d; want one order ask per land and the shock land's nested ask", orderAsks, modeAsks)
	}
	for _, id := range lands {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("land %d zone = %+v, want Battlefield", id, o)
		}
	}
	for _, id := range e.G.Stack {
		if id == trig {
			t.Fatal("the suspended trigger is still on the stack after every queued competition was answered")
		}
	}
	resolves := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Resolve && ev.Obj == trig {
			resolves++
		}
	}
	if resolves != 1 {
		t.Fatalf("trigger resolved %d times, want exactly once", resolves)
	}
	gains := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.LifeChange && ev.Player == 0 && ev.Amount == 1 {
			gains++
		}
	}
	if gains != 1 {
		t.Fatalf("the trigger's GainLife continuation ran %d times, want exactly once", gains)
	}
}
