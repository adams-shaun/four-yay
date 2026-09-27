package rules

import (
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestSnowblindToughnessTerminates pins the fatal P/T recursion fixed for the
// autopay fuzz finding "fatal: stack overflow @ effects.definedSpec >
// effects.knownDefinedTargets > effects.refTargets > effects.evalRefProperty >
// effects.evalCountExprOK > effects.EvalCount > effects.NumResolved >
// effects.Num".
//
// Snowblind's second continuous static is
//
//	AddPower$ -NotAttackingX | AddToughness$ -NotAttackingY
//	NotAttackingY:SVar$EnchantedDef/LimitMax.NotAttackingX
//	EnchantedDef:SVar$EnchantedY
//	EnchantedY:Enchanted$CardToughness/Minus.1
//
// so the layer-7 static that modifies the enchanted creature's toughness
// reads that same creature's toughness to size its own modifier. Before the
// fix, effects.refToughness called Host.Toughness unconditionally, so the read
// re-entered the whole layer-7 walk for the object already being derived and
// recursed until the goroutine stack limit. A stack overflow is a fatal error
// no recover catches: it kills the process, not just the match.
//
// The class guard is the in-progress layer-7 frame: a CardPower/CardToughness
// read of a battlefield object that the engine is CURRENTLY deriving resolves
// from the frame's pre-effect value (CR 613's "value before this effect"),
// through the single refPower/refToughness chokepoint every such read shares
// (Enchanted$, Equipped$, Self, Defined$ and the diffPower/diffToughness
// heads alike). The child process performs the read so the suite survives if
// the guard is removed; the parent asserts the read returned.
//
// Grizzly Bears (2/2) enchanted by Snowblind with one Snow-Covered Forest
// under its controller: X=1 (NotAttackingX counts the snow land), Y =
// min(1, toughness-1) = 1 measured against the in-progress 2/2, so the Bears
// are 1/1.
func TestSnowblindToughnessTerminates(t *testing.T) {
	if os.Getenv("GORGE_SNOWBLIND_CHILD") == "1" {
		debug.SetMaxStack(64 << 20)
		e := layerEngine(t)
		bear := onBoardCard(t, e, 1, corpusCard(t, "Grizzly Bears"))
		forest := onBoardCard(t, e, 1, corpusCard(t, "Snow-Covered Forest"))
		aura := onBoardCard(t, e, 0, corpusCard(t, "Snowblind"))
		e.emit(events.Event{Kind: events.Attach, Obj: aura, IDs: []state.ObjID{bear}})
		// Preconditions the assertion depends on: the read only recurses when
		// the Aura is actually attached to a live battlefield creature, and
		// the snow land is under that creature's controller.
		obj := e.G.Obj(bear)
		if obj == nil || obj.Zone != state.ZBattlefield {
			t.Fatal("setup: Grizzly Bears is not a live battlefield permanent")
		}
		if a := e.G.Obj(aura); a == nil || a.AttachedTo != bear {
			t.Fatalf("setup: Snowblind is not attached to the Bears (aura=%+v)", a)
		}
		if f := e.G.Obj(forest); f == nil || f.Controller != obj.Controller {
			t.Fatal("setup: Snow-Covered Forest is not controlled by the enchanted creature's controller")
		}
		if p, tough := e.Power(bear), e.Toughness(bear); p != 1 || tough != 1 {
			t.Fatalf("Snowblind on Grizzly Bears with one snow land: %d/%d, want 1/1 (X=1, Y=min(1,2-1)=1)", p, tough)
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestSnowblindToughnessTerminates$", "-test.count=1")
	cmd.Env = append(os.Environ(), "GORGE_SNOWBLIND_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := string(out)
		if i := strings.Index(msg, "goroutine "); i > 0 && len(msg) > i+1500 {
			msg = msg[:i+1500]
		}
		t.Fatalf("reading Snowblind's enchanted creature's toughness did not return: %v\n%s", err, msg)
	}
}
