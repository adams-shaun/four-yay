package rules

// Reference: Magic: The Gathering Comprehensive Rules, 2026-08-07.
// CR 611.3b/614.4: a battlefield static replacement must actually exist.
// CR 614.5/616.1f: each applicable replacement gets one opportunity, not
// just one replacement total. CR 616.1: the affected controller chooses.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestCR614RestInPeaceInHandCannotReplace(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Rest in Peace"}, nil)
	rip := crAbortMove(t, e, 0, "Rest in Peace", state.ZHand)
	target := crAbortMove(t, e, 0, "Delver of Secrets", state.ZBattlefield)
	guarded := false
	for _, r := range e.G.Obj(rip).Face().Repls {
		if r.Event == "Moved" && r.Params["ActiveZones"] == "Battlefield" && r.Params["Destination"] == "Graveyard" {
			guarded = true
		}
	}
	if !guarded || e.G.Obj(rip).Zone != state.ZHand {
		t.Fatal("CR 614.4 Rest in Peace seq 0: fixture changed")
	}
	start := len(e.L.Events)
	// A pending zone-change event is the replacement engine's input. This is
	// not destruction and no SBA, cast or trigger-order rule is under test.
	e.emit(events.Event{Kind: events.MoveZone, Obj: target, From: state.ZBattlefield, To: state.ZGraveyard})
	if e.G.Obj(target).Zone != state.ZGraveyard {
		t.Errorf("CR 611.3b/614.4 Rest in Peace/Delver of Secrets seq %d: Rest in Peace in hand replaced move, destination=%s; want graveyard", start, e.G.Obj(target).Zone)
	}
}

func TestCR614AllApplicableEntryReplacementsApplyOnce(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Triskelion"}, []string{"Blind Obedience"})
	orb := crAbortMove(t, e, 1, "Blind Obedience", state.ZBattlefield)
	trisk := crAbortMove(t, e, 0, "Triskelion", state.ZHand)
	orbOK, triskOK := false, false
	for _, r := range e.G.Obj(orb).Face().Repls {
		if r.Event == "Moved" && r.With != nil && r.With.API == "Tap" && r.Params["ValidCard"] == "Artifact.OppCtrl,Creature.OppCtrl" {
			orbOK = true
		}
	}
	for _, r := range e.G.Obj(trisk).Face().Repls {
		if r.Event == "Moved" && r.With != nil && r.With.API == "PutCounter" && r.With.Params["CounterNum"] == "3" {
			triskOK = true
		}
	}
	if !orbOK || !triskOK {
		t.Fatal("CR 614.5 Blind Obedience/Triskelion seq 0: missing real compiled entry replacements")
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 6})
	e.askPriority(0)
	crAbortAnswer(t, e, "Triskelion", crAbortOption(t, e, "Triskelion", "cast", trisk))
	crResolutionRound(t, e)
	// Both commute: any chosen order must finish tapped with exactly THREE
	// counters. If a future implementation asks an order, answer offered choices
	// until entry completes; never infer the expected result from those options.
	for i := 0; i < 4 && e.Pending() != nil && e.G.Obj(trisk).Zone == state.ZStack; i++ {
		d := e.Pending()
		if d.Kind == decision.KPriority || len(d.Options) == 0 {
			break
		}
		crAbortAnswer(t, e, "entry replacement order", d.Options[0].Index)
	}
	if e.G.Obj(trisk).Zone != state.ZBattlefield || !e.G.Obj(trisk).Tapped || e.G.Obj(trisk).Counter("P1P1") != 3 {
		t.Errorf("CR 614.5/616.1f Blind Obedience/Triskelion seq %d: got zone=%s tapped=%t counters=%d; want battlefield, tapped, exactly 3", len(e.L.Events), e.G.Obj(trisk).Zone, e.G.Obj(trisk).Tapped, e.G.Obj(trisk).Counter("P1P1"))
	}
}

func TestCR616AffectedControllerChoosesReplacement(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Rest in Peace", "Darksteel Colossus"}, nil)
	rip := crAbortMove(t, e, 0, "Rest in Peace", state.ZBattlefield)
	col := crAbortMove(t, e, 0, "Darksteel Colossus", state.ZBattlefield)
	ripOK, colOK := false, false
	for _, r := range e.G.Obj(rip).Face().Repls {
		if r.Event == "Moved" && r.Params["Destination"] == "Graveyard" && r.Params["ValidCard"] == "Card" && r.With != nil && r.With.Params["Destination"] == "Exile" {
			ripOK = true
		}
	}
	for _, r := range e.G.Obj(col).Face().Repls {
		if r.Event == "Moved" && r.Params["Destination"] == "Graveyard" && r.Params["ValidCard"] == "Card.Self" && r.With != nil && r.With.Params["Destination"] == "Library" {
			colOK = true
		}
	}
	if !ripOK || !colOK {
		t.Fatal("CR 616.1 Rest in Peace/Darksteel Colossus seq 0: missing competing replacements")
	}
	// No ETB trigger or priority decision owns this pending event. The two
	// replacements compete on sacrificing Colossus: exile vs owner's library.
	e.pending = nil
	e.emit(events.Event{Kind: events.MoveZone, Obj: col, From: state.ZBattlefield, To: state.ZGraveyard, Text: "sacrificed"})
	if d := e.Pending(); d == nil || d.Player != 0 || len(d.Options) < 2 || e.G.Obj(col).Zone != state.ZBattlefield {
		t.Errorf("CR 616.1 Rest in Peace/Darksteel Colossus seq %d: affected controller got no replacement choice before relocation; zone=%s pending=%+v", len(e.L.Events), e.G.Obj(col).Zone, d)
	}
}

// CR 614.4/614.5: a replacement whose applicability condition is not met does
// not apply, so the event proceeds unchanged. Island Sanctuary's Draw
// replacement is confined by ActivePhases$ Draw to its controller's draw
// step; a draw in any other step must not be replaced (CR 614.4 — the
// condition is one the replacement effect requires). Before the gate the
// parameter was unread and the replacement applied in every step of the
// controller's turn.
func TestCR614ActivePhasesRestrictsDrawReplacement(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Island Sanctuary"}, nil)
	sanctuary := crAbortMove(t, e, 0, "Island Sanctuary", state.ZBattlefield)
	guarded := false
	for _, r := range e.G.Obj(sanctuary).Face().Repls {
		if r.Event == "Draw" && r.Params["ActivePhases"] == "Draw" {
			guarded = true
		}
	}
	if !guarded || e.G.Obj(sanctuary).Zone != state.ZBattlefield {
		t.Fatal("CR 614.4 Island Sanctuary seq 0: fixture changed")
	}
	if e.G.Step == state.StepDraw {
		t.Fatalf("CR 614.4 Island Sanctuary seq 0: prerequisite step = %s, want a non-draw step", e.G.Step)
	}
	reg := testutil.CorpusRegistry(t)
	setupDrawLibrary(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	draws := countDraw(e)
	hand := len(e.G.Zone(state.ZHand, 0))
	e.emit(events.Event{Kind: events.Draw, Player: 0, Obj: e.G.Zone(state.ZLibrary, 0)[0],
		From: state.ZLibrary, To: state.ZHand, Secret: true})
	if got := countDraw(e) - draws; got != 1 {
		t.Errorf("CR 614.4 Island Sanctuary seq %d: draw delta outside the draw step = %d, want 1 (ActivePhases$ Draw unmet, replacement must not apply)", len(e.L.Events), got)
	}
	if got := len(e.G.Zone(state.ZHand, 0)) - hand; got != 1 {
		t.Errorf("CR 614.4 Island Sanctuary seq %d: hand delta = %d, want 1", len(e.L.Events), got)
	}
	for _, ce := range e.active() {
		if ce.Source == sanctuary {
			t.Errorf("CR 614.4 Island Sanctuary seq %d: CantAttack effect registered outside the draw step", len(e.L.Events))
		}
	}
}
