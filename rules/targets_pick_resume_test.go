package rules

// The generic ValidTgts$ pre-ask (task mvts1, the "tgts" resume arm) is
// CONSUMED by chosenTargetsFor before the body runs, and every resume builds
// a fresh Ctx. So when the body then suspends on an ask of its own, the next
// resume re-enters the SA from its top with no answer and re-poses the
// pre-ask -- and the two asks alternate forever.
//
// That livelock was fixed once, for MoveCounter alone, by the per-object
// moveCounterAsk cursor (the movecounter1 fix). It is a defect of the SHARED
// pre-ask, not of MoveCounter, and the live corpus carrier is Kozilek's
// Command: its CharmNum$ 2 election can pick `DBScry` (`DB$ Scry | ScryNum$ X
// | ValidTgts$ Player`) alongside another targeting mode, so the stack
// object's one undivided target list is not DBScry's player, the pre-ask
// fires at resolution, and the Scry's own KArrange is the second ask -- which
// loops arrange -> tgts -> arrange until the livelock watcher panics.
//
// The carrier below is a SYNTHETIC script of the same shape (never a .cards
// file -- the licensing rule), reduced to the minimum that reproduces it: a
// trigger whose Execute$ root carries no ValidTgts$ (so the CR 603.3c
// placement ask covers nothing) and whose depth-1 SubAbility$ is a
// ValidTgts$-bearing Scry (so the pre-ask fires there, and the Scry then
// suspends on its KArrange). Kozilek's Command reaches the identical pair
// through its Charm election; this fixture reaches it without needing an
// announced X, a two-mode election or a 19-card library.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// scryPreAskScript: the root DB$ Draw carries no ValidTgts$, so the trigger's
// placement ask is posed over nothing and the depth-1 DB$ Scry arrives at
// resolution with the shared pre-ask still owed.
const scryPreAskScript = "Name:Scry Probe\nManaCost:1 U\nTypes:Creature Human Wizard\nPT:1/1\n" +
	"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigRoot | TriggerDescription$ x\n" +
	"SVar:TrigRoot:DB$ Draw | Defined$ You | NumCards$ 0 | SubAbility$ DBScry\n" +
	"SVar:DBScry:DB$ Scry | ScryNum$ 2 | ValidTgts$ Player\n" +
	"Oracle:x\n"

// asksOfKind counts the decision_ask events of one kind since n0.
func asksOfKind(e *Engine, n0 int, kind decision.Kind) int {
	n := 0
	for _, ev := range e.L.Events[n0:] {
		if ev.Kind == events.DecisionAsk && ev.Text == string(kind) {
			n++
		}
	}
	return n
}

// TestTargetsPickSurvivesALaterSuspension pins the fix: an answered generic
// ValidTgts$ pre-ask is re-seeded into the fresh Ctx of a LATER resume of the
// same SA, so the pre-ask is posed exactly ONCE and the arrange answer
// completes the resolution instead of re-entering the pre-ask.
//
// Reverting rules/resolution.go's recordTargetsPick/seedTargetsPick pair
// makes this leaf fail on the second `choose` ask (and, driven further, the
// livelock watcher panics) -- it is not a leaf that passes against a no-op.
func TestTargetsPickSurvivesALaterSuspension(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 7401, scryPreAskScript)
	n0 := len(e.L.Events)
	id := putCreature(t, e, 0, scryPreAskScript)

	// Preconditions the rule reads: the probe is on the battlefield, seat 0's
	// library holds at least the two cards the Scry looks at (or the KArrange
	// is refused as unanswerable and nothing suspends), and no ask is owed.
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: probe zone = %+v, want battlefield", e.G.Obj(id))
	}
	if got := len(e.G.Zone(state.ZLibrary, 0)); got < 2 {
		t.Fatalf("precondition: seat 0 library = %d cards, want at least the 2 the Scry looks at", got)
	}
	e.pending = nil
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want exactly the probe's ETB trigger", e.G.Stack)
	}
	e.resolveTop()

	// First ask: the shared ValidTgts$ pre-ask for the depth-1 Scry.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "tgts" {
		t.Fatalf("pending = %+v, want the generic ValidTgts$ pre-ask (KChoose/tgts)", d)
	}
	submitChoices(t, e, 0)

	// Second ask: the Scry's own KArrange, posed by the body the pre-ask
	// answer unblocked. That it is posed at all is what makes this fixture
	// the two-ask shape the defect needs.
	d = e.Pending()
	if d == nil || d.Kind != decision.KArrange || d.ResumeKind != "arrange" {
		t.Fatalf("pending = %+v, want the Scry's KArrange", d)
	}
	submitChoices(t, e)

	// The fix: the arrange answer completes the resolution. Without it the
	// re-entry re-poses the pre-ask and the pair alternates forever.
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose && d.ResumeKind == "tgts" {
		t.Fatal("the arrange answer re-posed the ValidTgts$ pre-ask: the answer was not re-seeded (livelock)")
	}
	if got := asksOfKind(e, n0, decision.KChoose); got != 1 {
		t.Fatalf("ValidTgts$ pre-ask posed %d times, want exactly 1", got)
	}
	if got := asksOfKind(e, n0, decision.KArrange); got != 1 {
		t.Fatalf("KArrange posed %d times, want exactly 1", got)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v after the arrange answer, want the resolution drained", e.G.Stack)
	}
	replayCheck(t, e, cfg)
}

// TestCloneCopiesTargetsPickCursor pins the deep copy of the new cursor
// (rules/clone.go, the moveCounterAsk discipline it joins): a clone taken
// while such a resolution is suspended on its second ask must carry the
// answered pre-ask forward AND own its own storage, or the clone re-poses
// the pre-ask and its decision stream diverges from the original's.
func TestCloneCopiesTargetsPickCursor(t *testing.T) {
	t.Parallel()
	names, decks := testutil.SampleDecks(t, 2)
	e := New(Config{Seed: 5, Names: names, Decks: decks})
	e.Advance()
	e.targetsPickAsk = map[state.ObjID]map[string][]state.Target{
		7: {"DB$ Scry | ScryNum$ 2 | ValidTgts$ Player": {{Player: 1, IsPlayer: true}}},
	}
	c := e.Clone()
	got := c.targetsPickAsk[7]["DB$ Scry | ScryNum$ 2 | ValidTgts$ Player"]
	if len(got) != 1 || !got[0].IsPlayer || got[0].Player != 1 {
		t.Fatalf("clone dropped or mangled the targetsPickAsk cursor: %+v", c.targetsPickAsk[7])
	}
	// In-place mutation of the clone's entries must never reach the original.
	got[0] = state.Target{Obj: 99}
	c.targetsPickAsk[7]["other line"] = nil
	orig := e.targetsPickAsk[7]
	if len(orig) != 1 || orig["DB$ Scry | ScryNum$ 2 | ValidTgts$ Player"][0].Obj != 0 ||
		!orig["DB$ Scry | ScryNum$ 2 | ValidTgts$ Player"][0].IsPlayer {
		t.Fatalf("clone shares the targetsPickAsk storage with the original: %+v", orig)
	}
}
