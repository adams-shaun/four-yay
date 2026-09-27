package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestNurturingPixieDeclinedReturnNoCounter drives the real corpus Nurturing
// Pixie end to end. Its ETB is "return up to one target non-Faerie, nonland
// permanent you control to its owner's hand. If a permanent was returned this
// way, put a +1/+1 counter on CARDNAME", spelled as an optional
// (TargetMin$ 0) ChangeZone with RememberLKI$ True and a chained PutCounter
// gated on `ConditionDefined$ RememberedLKI | ConditionPresent$
// Card.Permanent`. Because the trigger's fire-time capture is the Pixie
// itself (a permanent), a gate that reads the raw captured set is satisfied
// with nothing returned -- the reported bug. Both branches assert the
// decisive precondition (the offered target really sits in the expected zone)
// so a vacuous board fails loudly.
func TestNurturingPixieDeclinedReturnNoCounter(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pixie := mustCorpusCard(t, reg, "Nurturing Pixie")
	// A real non-Faerie nonland permanent to return, seeded in the deck so
	// every move is a logged MoveZone event.
	bear := card(t, "Name:Grizzly Bears\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	// --- negative branch: the optional return is DECLINED.
	e, cfg := tokenReplGame(t, 733, pixie, bear)
	pixieID := moveSeededCard(t, e, 0, pixie, state.ZBattlefield)
	bearID := moveSeededCard(t, e, 0, bear, state.ZBattlefield)
	// Preconditions: the Pixie entered and is a permanent; the bear is a
	// legal target sitting on the battlefield; the Pixie starts uncountered.
	if o := e.G.Obj(pixieID); o == nil || o.Zone != state.ZBattlefield || !o.Face().IsCreature() {
		t.Fatalf("precondition: Pixie not a battlefield creature (%+v)", o)
	}
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: bear not on the battlefield")
	}
	if got := e.G.Obj(pixieID).Counter("P1P1"); got != 0 {
		t.Fatalf("precondition: Pixie starts with %d +1/+1 counters, want 0", got)
	}

	// The ETB triggers when it entered; queue and run it. The target ask must
	// actually surface (the choice was really offered), and declining is the
	// empty answer TargetMin$ 0 makes legal.
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the Pixie ETB's target ask, got %+v", d)
	}
	if d.Min != 0 {
		t.Fatalf("target ask Min = %d, want 0 (TargetMin$ 0)", d.Min)
	}
	offered := false
	for _, o := range d.Options {
		if o.Obj == bearID {
			offered = true
		}
	}
	if !offered {
		t.Fatalf("legal bear not offered as a return target: %+v", d.Options)
	}
	submitChoices(t, e) // empty answer: decline the optional return
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Obj(bearID).Zone; got != state.ZBattlefield {
		t.Fatalf("declined branch: bear zone = %v, want battlefield (nothing was returned)", got)
	}
	if got := e.G.Obj(pixieID).Counter("P1P1"); got != 0 {
		t.Fatalf("declined branch: Pixie got %d +1/+1 counters, want 0 (nothing was returned)", got)
	}
	replayCheck(t, e, cfg)

	// --- positive branch: a real non-Faerie nonland permanent IS returned.
	e2, cfg2 := tokenReplGame(t, 733, pixie, bear)
	pixieID = moveSeededCard(t, e2, 0, pixie, state.ZBattlefield)
	bearID = moveSeededCard(t, e2, 0, bear, state.ZBattlefield)
	if o := e2.G.Obj(bearID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition (positive): bear not on the battlefield")
	}
	if got := e2.G.Obj(pixieID).Counter("P1P1"); got != 0 {
		t.Fatalf("precondition (positive): Pixie starts with %d counters, want 0", got)
	}
	e2.priorityRound()
	d = e2.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the Pixie ETB's target ask (positive), got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == bearID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("legal bear not offered as a return target (positive): %+v", d.Options)
	}
	submitChoices(t, e2, idx)
	passUntilStackEmpty(t, e2, 20)

	if got := e2.G.Obj(bearID).Zone; got != state.ZHand {
		t.Fatalf("positive branch: bear zone = %v, want hand (it was really returned)", got)
	}
	if got := e2.G.Obj(pixieID).Counter("P1P1"); got != 1 {
		t.Fatalf("positive branch: Pixie has %d +1/+1 counters, want exactly 1", got)
	}
	replayCheck(t, e2, cfg2)
}
