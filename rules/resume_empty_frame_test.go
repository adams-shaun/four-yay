package rules

// A continuation frame with nothing left to run (spike S3, legacy defect 1).
//
// effects.Resolve reports every enclosing loop that suspends through
// SuspendContinuation, and buildContinuationChain turns each report into a
// frame that resumes at that loop's sa.Sub. When the enclosing SA is the LAST
// link of its chain -- Devour Intellect's DB$ Branch, whose nested
// FalseSubAbility$ discard asks; Capital Punishment's Vote, whose chosen
// outcome's discard asks; a mass move whose as-enters choice parks -- sa.Sub
// is nil, and the frame used to resume with no sub-ability at all. The
// resolution still finished, but only after logging the "mid-resolution
// answer resumed with no sub-ability recorded" degradation Note, which is
// reserved for a hand-built ask that never set ResumeSA. The dual-run fuzz
// (tape kernel vs legacy, 10k cardfuzz games) found 20 games diverging on it.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const noSubAbilityNote = "mid-resolution answer resumed with no sub-ability recorded"

func countNoSubNotes(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == noSubAbilityNote {
			n++
		}
	}
	return n
}

// TestBranchNestedDiscardResumesWithoutDegradation casts the real Devour
// Intellect (corpus): SP$ Pump -> DB$ Branch -> FalseSubAbility$ TgtChoose
// discard. The Branch is the chain's last link, so its continuation is empty.
func TestBranchNestedDiscardResumesWithoutDegradation(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 7301, []string{"Devour Intellect"}, nil, nil)
	id := findCardObj(t, e, 0, "Devour Intellect", state.ZHand)
	addMana(t, e, 0, "B")
	d := castFixture(t, e, id, 1)
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "discard" || d.Player != 1 {
		t.Fatalf("pending = %+v, want seat 1's TgtChoose discard pick", d)
	}
	pick := d.Options[len(d.Options)-1].Obj
	submitChoices(t, e, d.Options[len(d.Options)-1].Index)
	passUntilStackEmpty(t, e, 20)
	if n := countNoSubNotes(e); n != 0 {
		t.Fatalf("resume logged %d %q Notes, want 0", n, noSubAbilityNote)
	}
	if z := e.G.Obj(pick).Zone; z != state.ZGraveyard {
		t.Fatalf("chosen card zone = %s, want graveyard", z)
	}
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Fatalf("Devour Intellect zone = %s, want graveyard", z)
	}
	replayCheck(t, e, cfg)
}

// TestEmptyFrameKeepsLaterContinuation pins the other half of the fix: only
// the EMPTY frame is dropped. A synthetic Branch whose own SubAbility$ is a
// life loss must still run that sub exactly once after the nested discard.
func TestEmptyFrameKeepsLaterContinuation(t *testing.T) {
	t.Parallel()
	src := "Name:Test Branch Tail\nManaCost:B\nTypes:Sorcery\n" +
		"A:SP$ Pump | ValidTgts$ Opponent | SubAbility$ DBBranch\n" +
		"SVar:DBBranch:DB$ Branch | BranchConditionSVar$ Y | BranchConditionSVarCompare$ GE1 | FalseSubAbility$ DBDiscard | TrueSubAbility$ DBDiscard | SubAbility$ DBLose\n" +
		"SVar:DBDiscard:DB$ Discard | Defined$ Targeted | NumCards$ 1 | Mode$ TgtChoose\n" +
		"SVar:DBLose:DB$ LoseLife | Defined$ Targeted | LifeAmount$ 2\n" +
		"SVar:Y:Count$xPaid\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 7302, src)
	addMana(t, e, 0, "B")
	life := e.G.Players[1].Life
	d := castFixture(t, e, id, 1)
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "discard" || d.Player != 1 {
		t.Fatalf("pending = %+v, want seat 1's TgtChoose discard pick", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if got := life - e.G.Players[1].Life; got != 2 {
		t.Fatalf("seat 1 lost %d life, want the Branch's own SubAbility$ to run once (2)", got)
	}
	if n := countNoSubNotes(e); n != 0 {
		t.Fatalf("resume logged %d %q Notes, want 0", n, noSubAbilityNote)
	}
	replayCheck(t, e, cfg)
}

// TestBuildContinuationChainDropsEmptyPlainFrames pins the builder rule
// directly: a plain report whose SA ends its chain makes no frame, a bound one
// hands its Remembered on to the next frame (here the tail), and every other
// report keeps its frame.
func TestBuildContinuationChainDropsEmptyPlainFrames(t *testing.T) {
	t.Parallel()
	e := &Engine{}
	last := &cards.SA{API: "Branch"}
	next := &cards.SA{API: "LoseLife"}
	withSub := &cards.SA{API: "Pump", Sub: next}
	rem := []state.Target{{Obj: 42}}

	tail := &resumePoint{kind: "", sa: next}
	if head := e.buildContinuationChain([]contFrame{{sa: last, bound: true, remembered: rem}}, 7, tail); head != tail {
		t.Fatalf("an empty plain report built a frame: head=%+v", head)
	}
	if !tail.loopBound || len(tail.loopRemembered) != 1 || tail.loopRemembered[0].Obj != 42 {
		t.Fatalf("the dropped bound frame's Remembered was not handed on: %+v", tail)
	}

	head := e.buildContinuationChain([]contFrame{{sa: last}, {sa: withSub}}, 7, nil)
	if head == nil || head.sa != next || head.outer != nil {
		t.Fatalf("chain = %+v, want exactly one frame resuming at the non-empty report's Sub", head)
	}
	if head.loopBound {
		t.Fatal("an unbound empty report bound the next frame")
	}

	rep := &cards.SA{API: "RepeatEach"}
	head = e.buildContinuationChain([]contFrame{{sa: rep, repeat: &repeatCursor{}}}, 7, nil)
	if head == nil || head.kind != "repeat" || head.sa != rep {
		t.Fatalf("a payload report with no Sub was dropped: %+v", head)
	}
}
