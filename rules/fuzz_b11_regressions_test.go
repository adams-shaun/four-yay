package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Regressions for the cardfuzz batch11 intent-cap run and livelock (all real
// corpus cards).

// driveStackEmpty answers every pending decision (passing priority, taking
// the choice pick(d) names otherwise) until the stack is empty and a priority
// decision is pending, or the step budget runs out.
func driveStackEmpty(t *testing.T, e *Engine, budget int, pick func(d *decision.Decision) int) {
	t.Helper()
	for i := 0; i < budget; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision pending")
		}
		if d.Kind == decision.KPriority && len(e.G.Stack) == 0 {
			return
		}
		if d.Kind == decision.KPriority {
			submitPass(t, e)
			continue
		}
		submitChoices(t, e, pick(d))
	}
}

// TestMoveReplacementOrderChoiceResumesTheResolution (cardfuzz batch11 line
// 1): two Library of Leng on one battlefield both offer to put a card their
// controller discards on top of the library, so Shoal Kraken's constellation
// discard poses a CR 616.1 order choice from inside the trigger's
// resolution. The move competition's pose never marked itself
// inResolution, so the answer's tail never resumed the suspended
// resolution: the trigger stayed on the stack and resolveTop re-resolved it
// on every priority pass -- draw, discard, ask, forever -- until the intent
// cap. The answered choice now resumes the resolution, which finishes and
// leaves the stack once.
func TestMoveReplacementOrderChoiceResumesTheResolution(t *testing.T) {
	t.Parallel()
	e, cfg := b5Engine(t, "Shoal Kraken", "Library of Leng", "Library of Leng", "Glorious Anthem")
	kraken := searchMoveByName(t, e, "Shoal Kraken", state.ZBattlefield)
	searchMoveByName(t, e, "Library of Leng", state.ZBattlefield)
	searchMoveByName(t, e, "Library of Leng", state.ZBattlefield)
	searchMoveByName(t, e, "Glorious Anthem", state.ZBattlefield)
	orderAsks := 0
	driveStackEmpty(t, e, 40, func(d *decision.Decision) int {
		if d.Kind == decision.KReplacement {
			orderAsks++
		}
		return 0 // draw; first discard; first Library of Leng
	})
	if len(e.G.Stack) != 0 {
		t.Fatalf("Shoal Kraken's trigger never left the stack (depth %d)", len(e.G.Stack))
	}
	resolves := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Resolve && e.G.Obj(ev.Obj) != nil && e.G.Obj(ev.Obj).Source == kraken {
			resolves++
		}
	}
	if resolves != 1 || orderAsks != 1 {
		t.Fatalf("Shoal Kraken's trigger resolved %d times with %d order asks, want 1 and 1", resolves, orderAsks)
	}
	replayCheck(t, e, cfg)
}

// TestRedirectedMoveGetsTheOtherReplacements (cardfuzz batch11 line 2):
// Magus of the Will's effect exiles any card that would go to its
// controller's graveyard this turn and lets them cast from the graveyard.
// Mox Diamond's own "if it would enter, discard a land or put it into the
// graveyard instead" body moved it to the graveyard under the replacement
// re-entrancy guard, which skipped EVERY replacement, so Magus never saw the
// modified event: the zero-cost Mox was cast from the graveyard, fell back
// into it and was recast, forever. CR 616.1f gives the other replacements
// their opportunity on the modified event (the one already applied never
// re-applies, CR 614.5): the Mox is exiled.
func TestRedirectedMoveGetsTheOtherReplacements(t *testing.T) {
	t.Parallel()
	e, cfg := b5Engine(t, "Magus of the Will", "Mox Diamond")
	magus := searchMoveByName(t, e, "Magus of the Will", state.ZBattlefield)
	// A logged TurnChange clears summoning sickness (CR 302.6) the
	// replayable way, then the clock is parked in main phase 1.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	mox := searchMoveByName(t, e, "Mox Diamond", state.ZHand)
	addMana(t, e, 0, "BBB")
	submitChoices(t, e, abilityOption(t, e, magus, 0).Index)
	driveStackEmpty(t, e, 20, func(*decision.Decision) int { return 0 })
	// No land in hand: Mox Diamond's replacement can only put it into the
	// graveyard.
	for _, id := range append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...) {
		if e.G.Obj(id).Face().IsLand() {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary})
		}
	}
	e.pending = nil
	e.priorityRound()
	submitChoices(t, e, castOptionFor(t, e, mox).Index)
	driveStackEmpty(t, e, 20, func(*decision.Decision) int { return 0 })
	if z := e.G.Obj(mox).Zone; z != state.ZExile {
		t.Fatalf("Mox Diamond rests in %v, want exile (Magus of the Will's replacement skipped)", z)
	}
	if castOffered(e, mox) {
		t.Fatal("Mox Diamond is still castable after its replacement moved it")
	}
	replayCheck(t, e, cfg)
}
