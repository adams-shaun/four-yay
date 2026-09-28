package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// countRingTempts counts the RingTemptsYou events in the recorded log.
func countRingTempts(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.RingTemptsYou {
			n++
		}
	}
	return n
}

// declineTriggerCost answers a triggered-ability cost ask (the "may pay …"
// window) with the decline option.
func declineTriggerCost(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no pay ask pending after the Ring-bearer trigger resolved")
	}
	decline := -1
	for _, o := range d.Options {
		if o.Kind == "trigger_cost_decline" {
			decline = o.Index
		}
	}
	if decline < 0 {
		t.Fatalf("no decline option on the pay ask: %+v", d.Options)
	}
	submitChoices(t, e, decline)
}

// TestCR701RingTemptsYouCallOfTheRingUpkeep is the leaf the brief names,
// pinned on the real corpus card. CR 701.54a: "Each time the Ring tempts
// you, choose a creature you control. It becomes your Ring-bearer until
// another creature becomes your Ring-bearer or another player gains control
// of it." Call of the Ring's upkeep trigger resolves DB$ RingTemptsYou:
// exactly one RingTemptsYou event, the tempt count rises to 1, the first
// creature in the controller's battlefield zone order is designated
// (deterministic stand-in for CR 701.54a's choice), and the card's own
// `T:Mode$ RingTemptsYou | ValidCard$ Creature.YouCtrl` payoff trigger fires
// on the designated bearer.
func TestCR701RingTemptsYouCallOfTheRingUpkeep(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	bear := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	call := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Call of the Ring"))

	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	e.resolveTop()

	if n := countRingTempts(e); n != 1 {
		t.Fatalf("RingTemptsYou events = %d, want 1; log tail has the tempt", n)
	}
	if e.G.Players[0].RingTempted != 1 || e.G.Players[0].RingBearer != bear {
		t.Fatalf("tempted %d bearer %d, want 1/%d",
			e.G.Players[0].RingTempted, e.G.Players[0].RingBearer, bear)
	}
	// The bearer-matching payoff trigger (`Creature.YouCtrl`) queued —
	// CR 701.54d: it fires when the temptation completes.
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Source != call {
		t.Fatalf("pendingTriggers = %+v, want the Call's bearer-matching trigger", e.pendingTriggers)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API") {
			t.Fatalf("unimplemented-API note in the log: %q", ev.Text)
		}
	}

	// The payoff trigger's `AB$ Draw | Cost$ PayLife<2>` poses its pay ask;
	// decline it, then a second upkeep tempts again: the count rises to 2
	// and the bearer is NOT re-designated (the same bear — it is the
	// existing bearer; the effects-level test pins the keep-existing read
	// when it differs from first-in-zone-order).
	e.putTriggersOnStack()
	e.resolveTop()
	declineTriggerCost(t, e)
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	e.resolveTop()
	if n := countRingTempts(e); n != 2 {
		t.Fatalf("RingTemptsYou events after the second upkeep = %d, want 2", n)
	}
	if e.G.Players[0].RingTempted != 2 || e.G.Players[0].RingBearer != bear {
		t.Fatalf("after the second tempt: count %d bearer %d, want 2/%d",
			e.G.Players[0].RingTempted, e.G.Players[0].RingBearer, bear)
	}
}

// TestRingTemptsYouTriggerValidCardOtherGatesOnTheBearer pins the corpus's
// `ValidCard$ Creature.YouCtrl+Other` shape (Galadriel of Lothlórien): the
// trigger fires when a creature OTHER than the source is the chosen bearer
// and does not fire when the source itself was designated (CR 701.54a — the
// bearer choice excludes nothing, but the trigger's own intervening-if does).
func TestRingTemptsYouTriggerValidCardOtherGatesOnTheBearer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	// Negative: Galadriel is first in zone order, so she IS the bearer the
	// temptation designates — `Creature.YouCtrl+Other` fails on her own
	// designation.
	e := layerEngine(t)
	gal := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Galadriel of Lothlórien"))
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	e.emit(events.Event{Kind: events.RingTemptsYou, Player: 0, Obj: gal, Amount: 1})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("source-chosen bearer queued %d triggers, want none", len(e.pendingTriggers))
	}

	// Positive: the OTHER creature (the bear — first in zone order on a
	// fresh board where the bear is placed before her) is the bearer.
	e = layerEngine(t)
	bear := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Galadriel of Lothlórien"))
	e.emit(events.Event{Kind: events.RingTemptsYou, Player: 0, Obj: bear, Amount: 1})
	requireOneEventTrigger(t, e, "Galadriel")
}

// TestRingTemptsYouTriggerValidPlayerYouGatesOnTheTemptedSeat pins the
// `ValidPlayer$ You` shape (Nazgûl): only the source controller's own
// temptation fires it, and it fires even when no creature was designated
// (CR 701.54d — the trigger fires when the actions complete, even if some
// were impossible).
func TestRingTemptsYouTriggerValidPlayerYouGatesOnTheTemptedSeat(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Nazgûl"))
	e.emit(events.Event{Kind: events.RingTemptsYou, Player: 1, Obj: 0, Amount: 1})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("another seat's temptation queued %d triggers, want none", len(e.pendingTriggers))
	}
	e.emit(events.Event{Kind: events.RingTemptsYou, Player: 0, Obj: 0, Amount: 1})
	requireOneEventTrigger(t, e, "Nazgûl")
}

// TestRingBearerPredicateReadsThroughRealCorpusSVars pins IsRingbearer and
// the PlayerCountPropertyYou$RingTemptedYou count head through the REAL
// corpus SVar bodies: Sauron, the Necromancer's
// `SVar:X:Count$Valid Card.Self+IsRingbearer`, One Ring to Rule Them All's
// `SVar:X:Count$Valid Creature.YouCtrl+IsRingbearer$CardPower`, and Frodo,
// Adventurous Hobbit's `SVar:NumRingTempted:PlayerCountPropertyYou$RingTemptedYou`.
func TestRingBearerPredicateReadsThroughRealCorpusSVars(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	sauron := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Sauron, the Necromancer"))
	oring := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "One Ring to Rule Them All"))
	bear := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	evaluate := func(src state.ObjID, p state.PlayerID, name string) (int32, bool) {
		// The same resolution CheckSVarHolds makes: the SVar name resolves
		// against the face's table, then the BODY is evaluated.
		f := e.G.Obj(src).Face()
		body := name
		if b, ok := f.SVars[name]; ok {
			body = b
		}
		return effects.EvalCountOK(e, &effects.Ctx{Source: src, Controller: p, SVars: f.SVars}, body)
	}

	if n, ok := evaluate(sauron, 0, "X"); !ok || n != 0 {
		t.Fatalf("Sauron X before any tempt = %d/%v, want 0/true (no bearer yet)", n, ok)
	}
	if n, ok := evaluate(oring, 0, "X"); !ok || n != 0 {
		t.Fatalf("One Ring X before any tempt = %d/%v, want 0/true", n, ok)
	}

	// The Ring tempts seat 0: the bear (first in zone order) is designated.
	e.emit(events.Event{Kind: events.RingTemptsYou, Player: 0, Obj: bear, Amount: 1})
	if n, ok := evaluate(sauron, 0, "X"); !ok || n != 0 {
		t.Fatalf("Sauron X while the bear is the bearer = %d/%v, want 0/true", n, ok)
	}
	if n, ok := evaluate(oring, 0, "X"); !ok || n != 2 {
		t.Fatalf("One Ring X = %d/%v, want 2/true (the bearer's power)", n, ok)
	}
	// YouCtrl is relative to the evaluating controller: the seat-0 bearer
	// never counts for a seat-1 read.
	if n, ok := evaluate(oring, 1, "X"); !ok || n != 0 {
		t.Fatalf("One Ring X from seat 1 = %d/%v, want 0/true", n, ok)
	}

	// Frodo's count head, through the gate path itself: CheckSVarHolds on
	// the real SVar name — GE2 only holds at two or more temptations (the
	// "tempted you two or more times this game" half of Frodo's own text).
	frodo := mustCorpusCard(t, reg, "Frodo, Adventurous Hobbit")
	svars := frodo.Faces[0].SVars
	frodoGate := func(p state.PlayerID) (bool, bool) {
		return effects.CheckSVarHolds(e, &effects.Ctx{Controller: p, SVars: svars}, "NumRingTempted", "GE2")
	}
	if holds, ev := frodoGate(0); holds || !ev {
		t.Fatalf("Frodo gate after one temptation = %v/%v, want false/true", holds, ev)
	}
	e.emit(events.Event{Kind: events.RingTemptsYou, Player: 1, Obj: 0, Amount: 1})
	if holds, ev := frodoGate(0); holds || !ev {
		t.Fatalf("Frodo gate after another seat's temptation = %v/%v, want false/true", holds, ev)
	}
	e.emit(events.Event{Kind: events.RingTemptsYou, Player: 0, Obj: bear, Amount: 2})
	if holds, ev := frodoGate(0); !holds || !ev {
		t.Fatalf("Frodo gate after two temptations = %v/%v, want true/true", holds, ev)
	}
	n, ok := effects.EvalCountOK(e, &effects.Ctx{Controller: 0, SVars: svars}, "PlayerCountPropertyYou$RingTemptedYou")
	if !ok || n != 2 {
		t.Fatalf("PlayerCountPropertyYou$RingTemptedYou = %d/%v, want 2/true", n, ok)
	}
}
