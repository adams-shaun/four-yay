package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestCavalierOfThornsDiesPaysTheExileAndResumes (exg1 class C): the dies
// trigger's `Cost$ ExileAnyGrave<1/Card.TriggeredNewCard>` opens the
// triggered-cost window with a real pay; answering it exiles the CAVALIER
// (the triggering card -- never a graveyard decoy), then the body's target
// ask runs and the chosen card moves on top of the library. Declining leaves
// the Cavalier in the graveyard and the body unexecuted.
func TestCavalierOfThornsDiesPaysTheExileAndResumes(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := exileAnyGraveBoard(t, reg, "Cavalier of Thorns", "", "Bear Cub", "")
	cavalier := ids["Cavalier of Thorns"]
	decoy := ids["Bear Cub"]
	// A mountain for the body's "another target card from your graveyard" --
	// moved in from the library so its identity is known.
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 1})
	var mountain state.ObjID
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Mountain" {
			mountain = id
			break
		}
	}
	if mountain == 0 {
		t.Fatal("no mountain in the library")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: mountain, From: state.ZLibrary, To: state.ZGraveyard})
	if e.G.Obj(cavalier).Zone != state.ZBattlefield {
		t.Fatalf("cavalier zone = %v", e.G.Obj(cavalier).Zone)
	}
	// The ETB dig ("reveal the top five, put a land onto the battlefield")
	// resolves first; answer its pick, then kill it so the dies trigger's
	// window opens.
	e.putTriggersOnStack()
	e.pending = nil
	e.resolveTop()
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose {
		t.Fatalf("the ETB dig did not ask: %+v", d)
	} else {
		submitChoices(t, e, d.Options[0].Index)
	}
	// (Priority returns to the active player after the dig resolution; the
	// kill below does not need that decision answered.)
	e.emit(events.Event{Kind: events.MoveZone, Obj: cavalier, From: state.ZBattlefield,
		To: state.ZGraveyard, Text: "destroyed"})
	// The body's target ("another target card from your graveyard") is
	// pre-asked at trigger PLACEMENT (the body SA carries ValidTgts$), before
	// the trigger ever resolves -- so answer it while the trigger is still
	// being placed, then resolve the trigger into its payment window.
	e.putTriggersOnStack()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("the dies trigger did not pre-ask its body target: %+v", d)
	}
	var tpick []int
	for _, o := range d.Options {
		if o.Obj == mountain {
			tpick = append(tpick, o.Index)
		}
	}
	if len(tpick) != 1 {
		t.Fatalf("placement target ask lost the mountain: %+v", d.Options)
	}
	submitChoices(t, e, tpick[0])
	e.pending = nil
	e.resolveTop()
	d = kr5WaitForWindow(t, e)
	if len(d.Options) < 2 || d.Options[0].Kind != "trigger_cost_pay" || d.Options[1].Kind != "trigger_cost_decline" {
		t.Fatalf("Cavalier's dies trigger did not open a pay/decline window: %+v", d)
	}
	if !strings.Contains(d.Options[0].Label, "Exile 1 card") {
		t.Fatalf("pay option label lost the cost: %q", d.Options[0].Label)
	}
	if strings.ContainsAny(d.Options[0].Label, "<>") {
		t.Fatalf("pay option label leaks raw cost syntax: %q", d.Options[0].Label)
	}
	submitChoices(t, e, d.Options[0].Index)
	// Exactly the triggering card pays: no graveyard pick ask may open -- the
	// window records the singleton match (the Cavalier, never the Bear decoy)
	// -- and the pre-asked body then moves the mountain with no further ask.
	if e.G.Obj(cavalier).Zone != state.ZExile {
		t.Fatalf("paying did not exile the Cavalier: %v", e.G.Obj(cavalier).Zone)
	}
	if e.G.Obj(decoy).Zone != state.ZGraveyard {
		t.Fatalf("the Bear decoy moved: %v", e.G.Obj(decoy).Zone)
	}
	kr5Settle(e)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(mountain).Zone != state.ZLibrary {
		t.Fatalf("the chosen card did not move back to the library: %v", e.G.Obj(mountain).Zone)
	}
	if countMoves(e.L.Events, cavalier, state.ZExile) != 1 {
		t.Fatal("the Cavalier's exile is not a single logged move")
	}
	replayCheck(t, e, cfg)

	// The decline arm: "Do not pay" leaves the Cavalier in the graveyard and
	// the body unexecuted -- nothing else moves.
	e2, cfg2, ids2 := exileAnyGraveBoard(t, reg, "Cavalier of Thorns", "", "", "")
	cav2 := ids2["Cavalier of Thorns"]
	var mt2 state.ObjID
	for _, id := range e2.G.Zone(state.ZLibrary, 0) {
		if o := e2.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Mountain" {
			mt2 = id
			break
		}
	}
	e2.emit(events.Event{Kind: events.MoveZone, Obj: mt2, From: state.ZLibrary, To: state.ZGraveyard})
	// The ETB dig resolves first (as in the pay arm), so only the dies
	// trigger is pending at the kill.
	e2.putTriggersOnStack()
	e2.pending = nil
	e2.resolveTop()
	if d := e2.Pending(); d == nil || d.Kind != decision.KChoose {
		t.Fatalf("decline engine: the ETB dig did not ask: %+v", d)
	} else {
		submitChoices(t, e2, d.Options[0].Index)
	}
	e2.emit(events.Event{Kind: events.MoveZone, Obj: cav2, From: state.ZBattlefield,
		To: state.ZGraveyard, Text: "destroyed"})
	e2.putTriggersOnStack()
	if d := e2.Pending(); d == nil || d.Kind != decision.KTarget {
		t.Fatalf("decline engine: no placement target ask: %+v", d)
	} else {
		submitChoices(t, e2, d.Options[0].Index)
	}
	e2.pending = nil
	e2.resolveTop()
	d = kr5WaitForWindow(t, e2)
	if d.Options[0].Kind != "trigger_cost_pay" {
		t.Fatalf("decline engine's window = %+v", d)
	}
	mark := len(e2.L.Events)
	submitChoices(t, e2, d.Options[1].Index)
	kr5Settle(e2)
	passUntilStackEmpty(t, e2, 20)
	if e2.G.Obj(cav2).Zone != state.ZGraveyard {
		t.Fatalf("a declined exile-cost window still moved the Cavalier: %v", e2.G.Obj(cav2).Zone)
	}
	if e2.G.Obj(mt2).Zone != state.ZGraveyard {
		t.Fatalf("a declined window still ran the body: mountain at %v", e2.G.Obj(mt2).Zone)
	}
	for _, ev := range e2.L.Events[mark:] {
		if ev.Kind == events.MoveZone && ev.From != state.ZStack {
			t.Fatalf("declined window emitted a move: %+v", ev)
		}
	}
	replayCheck(t, e2, cfg2)
}

// TestDoombotHarbingerExileCostWindowPaysTheExile (exg1 class B): the
// ImmediateTrigger body's `Cost$ ExileAnyGrave<1/Card.TriggeredNewCard>` is
// paid with the REAL exile (the triggering card leaves the graveyard) and the
// body then runs; a decline leaves the Harbinger in the graveyard and the
// body unexecuted -- never the old one-generic charge.
func TestDoombotHarbingerExileCostWindowPaysTheExile(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := exileAnyGraveBoard(t, reg, "Doombot Harbinger", "Bear Cub", "", "")
	harb := ids["Doombot Harbinger"]
	creature := ids["Bear Cub"]
	if e.G.Obj(creature).Zone != state.ZGraveyard {
		t.Fatalf("creature fodder zone = %v", e.G.Obj(creature).Zone)
	}
	// The ETB mill trigger resolves first, then the dies trigger's window
	// opens as that trigger resolves.
	e.putTriggersOnStack()
	e.pending = nil
	e.resolveTop()
	kr5DeclineOptional(t, e) // the ETB's "you may mill four"
	e.emit(events.Event{Kind: events.MoveZone, Obj: harb, From: state.ZBattlefield,
		To: state.ZGraveyard, Text: "destroyed"})
	e.putTriggersOnStack()
	e.pending = nil
	e.resolveTop() // the dies trigger opens its payment window
	d := e.Pending()
	if d == nil || len(d.Options) < 2 || d.Options[0].Kind != "trigger_cost_pay" {
		t.Fatalf("Doombot's dies trigger did not open its window: %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	if e.G.Obj(harb).Zone != state.ZExile {
		t.Fatalf("paying did not exile the Harbinger: %v", e.G.Obj(harb).Zone)
	}
	// The body: return target creature card from your graveyard to hand. The
	// reflexive trigger is placed by the turn loop after the window's run.
	kr5Settle(e)
	d = e.Pending()
	var tpick []int
	for _, o := range d.Options {
		if o.Obj == creature {
			tpick = append(tpick, o.Index)
		}
	}
	if len(tpick) != 1 {
		t.Fatalf("body target ask lost the creature: %+v", d.Options)
	}
	submitChoices(t, e, tpick[0])
	kr5Settle(e)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(creature).Zone != state.ZHand {
		t.Fatalf("the body did not return the creature to hand: %v", e.G.Obj(creature).Zone)
	}
	replayCheck(t, e, cfg)

	// Decline: the Harbinger stays in the graveyard, the creature stays there
	// too.
	e2, cfg2, ids2 := exileAnyGraveBoard(t, reg, "Doombot Harbinger", "Bear Cub", "", "")
	harb2 := ids2["Doombot Harbinger"]
	creature2 := ids2["Bear Cub"]
	e2.putTriggersOnStack()
	e2.pending = nil
	e2.resolveTop()
	kr5DeclineOptional(t, e2)
	e2.emit(events.Event{Kind: events.MoveZone, Obj: harb2, From: state.ZBattlefield,
		To: state.ZGraveyard, Text: "destroyed"})
	e2.putTriggersOnStack()
	e2.pending = nil
	e2.resolveTop()
	d = e2.Pending()
	if d == nil || d.Options[0].Kind != "trigger_cost_pay" {
		t.Fatalf("decline engine's window = %+v", d)
	}
	mark := len(e2.L.Events)
	submitChoices(t, e2, d.Options[1].Index)
	kr5Settle(e2)
	passUntilStackEmpty(t, e2, 20)
	if e2.G.Obj(harb2).Zone != state.ZGraveyard {
		t.Fatalf("a declined window still exiled the Harbinger: %v", e2.G.Obj(harb2).Zone)
	}
	if e2.G.Obj(creature2).Zone != state.ZGraveyard {
		t.Fatalf("a declined window still ran the body: %v", e2.G.Obj(creature2).Zone)
	}
	for _, ev := range e2.L.Events[mark:] {
		if ev.Kind == events.MoveZone && ev.From != state.ZStack {
			t.Fatalf("declined window emitted a move: %+v", ev)
		}
	}
	replayCheck(t, e2, cfg2)
}

// kr5WaitForWindow is waitForWindow under the resolution kernel: each
// resolveTop runs as a kernel probe from a quiet engine, so the
// triggered-cost window's ask is posed inside the run that serves it.
func kr5WaitForWindow(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	for i := 0; i < 4; i++ {
		d := e.Pending()
		if d != nil && d.Kind == decision.KChoose && len(d.Options) > 0 &&
			(d.Options[0].Kind == "trigger_cost_pay" || d.Options[0].Kind == "trigger_cost_decline") {
			return d
		}
		if d != nil && d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision waiting for the window: %+v", d)
		}
		if len(e.G.Stack) == 0 {
			t.Fatal("stack empty but the triggered-cost window never opened")
		}
		e.pending = nil
		e.resolveTop()
	}
	t.Fatal("the triggered-cost window never opened")
	return nil
}

// kr5DeclineOptional answers a posed optional ("you may") ask with its
// decline, failing if what is posed is not one.
func kr5DeclineOptional(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind == decision.KPriority {
		return
	}
	for _, o := range d.Options {
		if o.Kind == "no" || o.Kind == "decline" {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("pending %s ask has no decline option: %+v", d.Kind, d.Options)
}
