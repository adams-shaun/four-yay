package rules

// The generic ValidTgts$ pre-ask is CONSUMED by chosenTargetsFor before the
// body runs. When the body then asks again on its own, the pre-ask's answer
// must still be in hand for the rest of the resolution; under the removed
// suspend/resume protocol it was not, the pre-ask was re-posed, and the two
// asks alternated forever.
//
// That livelock was once fixed, for MoveCounter alone, by the per-object
// moveCounterAsk cursor (the movecounter1 fix). It is a defect of the SHARED
// pre-ask, not of MoveCounter, and the live corpus carrier is Kozilek's
// Command: its CharmNum$ 2 election can pick `DBScry` (`DB$ Scry | ScryNum$ X
// | ValidTgts$ Player`) alongside another targeting mode, so the stack
// object's one undivided target list is not DBScry's player, the pre-ask
// fires, and the Scry's own KArrange is the second ask -- which loops
// arrange -> tgts -> arrange until the livelock watcher panics.
//
// The carrier below is a SYNTHETIC script of the same shape (never a .cards
// file -- the licensing rule), reduced to the minimum that reproduces it: a
// trigger whose Execute$ root carries no ValidTgts$ and whose depth-1
// SubAbility$ is a ValidTgts$-bearing Scry. The Scry's link target is now
// announced at placement (CR 603.3d, KTarget/trig_sub) and recorded as the
// shared SubPreAsk; at resolution the Scry reads that record instead of
// re-posing, and asks its KArrange -- the second ask the fix must survive.
// Kozilek's Command reaches the identical pair through its Charm election;
// this fixture reaches it without needing an announced X, a two-mode election
// or a 19-card library.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// scryPreAskScript: the root DB$ Draw carries no ValidTgts$, but the depth-1
// DB$ Scry does, so the trigger's placement ask announces that link's target
// and the Scry's resolution consumes that record before asking its KArrange.
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
// ValidTgts$ pre-ask (now the placement trig_sub announcement) stays answered
// across a LATER ask of the same SA, so the pre-ask is posed exactly ONCE and
// the arrange answer completes the resolution instead of re-posing it.
//
// Losing the pre-ask answer across the arrange ask makes this leaf fail on the
// second ask (and, driven further, the livelock watcher panics) -- it is not a
// leaf that passes against a no-op.
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

	// First ask: the chain link's placement announcement for the depth-1 Scry.
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "trig_sub" {
		t.Fatalf("pending = %+v, want the placement trig_sub target ask for the Scry link", d)
	}
	submitChoices(t, e, 0)

	// Second ask: the Scry's own KArrange, posed by the body the pre-ask
	// answer unblocked once the trigger resolves. Pass priority until it
	// surfaces. That it is posed at all is what makes this fixture the
	// two-ask shape the defect needs.
	d = e.Pending()
	for i := 0; i < 30 && d != nil && d.Kind == decision.KPriority; i++ {
		for _, o := range d.Options {
			if o.Kind == "pass" {
				submitChoices(t, e, o.Index)
				break
			}
		}
		d = e.Pending()
	}
	if d == nil || d.Kind != decision.KArrange || d.ResumeKind != "arrange" {
		t.Fatalf("pending = %+v, want the Scry's KArrange", d)
	}
	submitChoices(t, e)

	// The fix: the arrange answer completes the resolution. Without it the
	// pre-ask is re-posed and the pair alternates forever.
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget && d.ResumeKind == "trig_sub" {
		t.Fatal("the arrange answer re-posed the placement pre-ask: the pre-ask answer was lost (livelock)")
	}
	if got := asksOfKind(e, n0, decision.KTarget); got != 1 {
		t.Fatalf("chain pre-ask posed %d times, want exactly 1", got)
	}
	if got := asksOfKind(e, n0, decision.KArrange); got != 1 {
		t.Fatalf("KArrange posed %d times, want exactly 1", got)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v after the arrange answer, want the resolution drained", e.G.Stack)
	}
	replayCheck(t, e, cfg)
}
