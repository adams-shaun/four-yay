package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Ertha Jo copies her own Mercenary token's tap-pump targeting her: the
// original and the copy each give +1/+0 (2/4 -> 4 power).
func TestErthaJoAbilityCastTargetsValidEndToEndKernel(t *testing.T) {
	t.Parallel()
	ertha := corpusAlternativeCard(t, "Ertha Jo, Frontier Mentor")
	// The ETB trigger's TokenScript$ key resolves against Game.Tokens (Config
	// Tokens), which handEngine does not populate -- inject the corpus
	// registry's token table before the entry drain resolves the ETB.
	e := handEngine(t, ertha)
	e.G.Tokens = testutil.CorpusRegistry(t).Tokens
	bf := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: bf, From: state.ZHand, To: state.ZBattlefield})
	drainQueuedTriggers(t, e)
	for _, r := range "WWRR" {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: string(r), Amount: 1})
	}
	e.priorityRound()
	erthaID := bf
	merc := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.IsToken && o.Face() != nil && strings.Contains(o.Face().Name, "Mercenary") {
			merc = id
		}
	}
	if merc == 0 {
		t.Fatal("Ertha Jo's ETB did not create a Mercenary token")
	}
	activateTargetsValid(t, e, merc, 0, erthaID)
	if n := triggersFromSource(e, erthaID); n != 1 {
		t.Fatalf("Ertha Jo queued %d triggers for an ability targeting her, want 1", n)
	}
	kr9Drain(t, e, 60, nil)
	if got := e.Power(erthaID); got != 4 {
		t.Fatalf("Ertha Jo power %d, want 4 (printed 2/4 + the activation's and the copy's +1/+0)", got)
	}
}
