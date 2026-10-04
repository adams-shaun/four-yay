package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestGuardianOfNewBenaliaEnlistedTriggerScr is the Mode$ Enlisted listener
// end to end on its real corpus carrier: Guardian enlists the Hill Giant, its
// `T:Mode$ Enlisted | Execute$ TrigScry` fires, and the resolution poses the
// scry-2 KArrange over the library's top cards.
func TestGuardianOfNewBenaliaEnlistedTriggerScr(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, guardian, giant := enlistEngine(t, reg, lookup(t, reg, "Guardian of New Benalia"))
	driveToExertTurn(t, e)
	driveEnlistAttack(t, e, guardian, 1)

	if n := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.TriggerPush }); n != 1 {
		t.Fatalf("TriggerPush events = %d, want 1 (Guardian's Mode$ Enlisted trigger)", n)
	}
	// Pass priority through the real flow until the Enlisted body poses its
	// scry ask mid-resolution.
	d := passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KArrange || len(d.Options) != 2 {
		t.Fatalf("pending = %+v, want the Enlisted body's scry-2 KArrange over 2 cards", d)
	}
	if d.Player != 0 {
		t.Fatalf("scry player = %d, want 0 (the library owner)", d.Player)
	}
	submitChoices(t, e, 0, 1)
	passUntilStackEmpty(t, e, 40)
	if !e.G.Obj(giant).Tapped {
		t.Fatal("the enlisted Hill Giant was not tapped")
	}
	replayCheck(t, e, cfg)
}
