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
	t.Parallel()
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

// TestSnowblindCounterExcludedFromAmount pins the CR 613.4 ordering the
// self-reference frame must honour: counters are layer 7d and apply AFTER
// every 7c modify, so Snowblind's amount (a 7c modify reading the enchanted
// creature's own toughness) must size itself from the PRE-counter value.
//
// Grizzly Bears (2/2) with one +1/+1 counter, enchanted by Snowblind, with
// three Snow-Covered Forests under its controller: X=3 (NotAttackingX counts
// the lands), Y = min(X, toughness-1). The pre-counter toughness is 2, so
// EnchantedY=1 and Y=1: the Bears are pumped to 2/2-3/1, i.e. 0/1 without the
// counter, then the 7d counter makes them 0/2. Reading a frame that already
// folded the counter in (toughness 3) yields Y=min(3,2)=2 and the wrong 0/1.
//
// The read runs in a child process so removing the guard fails by stack
// overflow without taking the suite down.
func TestSnowblindCounterExcludedFromAmount(t *testing.T) {
	t.Parallel()
	if os.Getenv("GORGE_SNOWBLIND_COUNTER_CHILD") == "1" {
		debug.SetMaxStack(64 << 20)
		e := layerEngine(t)
		bear := onBoardCard(t, e, 1, corpusCard(t, "Grizzly Bears"))
		aura := onBoardCard(t, e, 0, corpusCard(t, "Snowblind"))
		e.emit(events.Event{Kind: events.Attach, Obj: aura, IDs: []state.ObjID{bear}})
		for i := 0; i < 3; i++ {
			onBoardCard(t, e, 1, corpusCard(t, "Snow-Covered Forest"))
		}
		e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "P1P1", Amount: 1})
		// Preconditions: the counter is really on the Bears, the Aura is
		// attached, and the three lands are under the Bears' controller. A
		// vacuous setup (no counter, no lands) would make the assertion
		// meaningless.
		obj := e.G.Obj(bear)
		if obj == nil || obj.Zone != state.ZBattlefield {
			t.Fatal("setup: Grizzly Bears is not a live battlefield permanent")
		}
		if got := obj.Counter("P1P1"); got != 1 {
			t.Fatalf("setup: Grizzly Bears has %d +1/+1 counters, want exactly 1", got)
		}
		if a := e.G.Obj(aura); a == nil || a.AttachedTo != bear {
			t.Fatalf("setup: Snowblind is not attached to the Bears (aura=%+v)", a)
		}
		snow := 0
		for _, id := range e.G.Zone(state.ZBattlefield, obj.Controller) {
			if lo := e.G.Obj(id); lo != nil && lo.Face() != nil && lo.Face().Name == "Snow-Covered Forest" {
				snow++
			}
		}
		if snow != 3 {
			t.Fatalf("setup: Bears' controller has %d Snow-Covered Forests on the battlefield, want 3", snow)
		}
		if p, tough := e.Power(bear), e.Toughness(bear); p != 0 || tough != 2 {
			t.Fatalf("Snowblind on Grizzly Bears with a +1/+1 counter and three snow lands: %d/%d, want 0/2 (X=3, pre-counter toughness 2, Y=min(3,2-1)=1, then +1/+1)", p, tough)
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestSnowblindCounterExcludedFromAmount$", "-test.count=1")
	cmd.Env = append(os.Environ(), "GORGE_SNOWBLIND_COUNTER_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := string(out)
		if i := strings.Index(msg, "goroutine "); i > 0 && len(msg) > i+1500 {
			msg = msg[:i+1500]
		}
		t.Fatalf("reading Snowblind's amount with a counter present did not return correctly: %v\n%s", err, msg)
	}
}

// TestSelfReferentialPTTerminates is the class guard's second selector: the
// in-progress frame protects every CardPower/CardToughness read of an object
// the walk is currently deriving, not just Snowblind's Enchanted$ spelling.
// Two variants run in one child: a creature whose own static reads
// Self$CardToughness (the ref family the fatal crash signature named through
// definedSpec), and an Equipment whose static pumps its bearer by
// Equipped$CardToughness. With no guard either read re-enters
// chars.PT for the same object and overflows the stack; with it, the
// 2/2 basis adds its own pre-counter toughness 2 and the body is 2/4.
func TestSelfReferentialPTTerminates(t *testing.T) {
	t.Parallel()
	if os.Getenv("GORGE_SELFPTC_CHILD") == "1" {
		debug.SetMaxStack(64 << 20)
		// Self$ selector: a synthetic creature pumping its own toughness.
		e := layerEngine(t)
		bear := onBoard(t, e, 0, "Name:Selftest Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nS:Mode$ Continuous | Affected$ Card.Self | AddToughness$ X | Description$ x\nSVar:X:Self$CardToughness\nOracle:x\n")
		obj := e.G.Obj(bear)
		if obj == nil || obj.Zone != state.ZBattlefield {
			t.Fatal("setup: Selftest Bear is not a live battlefield permanent")
		}
		if p, tough := e.Power(bear), e.Toughness(bear); p != 2 || tough != 4 {
			t.Fatalf("Self$CardToughness pump on a 2/2 body: %d/%d, want 2/4 (2 + pre-counter toughness 2)", p, tough)
		}
		// Equipped$ selector: the same class under the other attachment ref.
		e2 := layerEngine(t)
		bear2 := onBoard(t, e2, 0, "Name:Bear2\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
		eq := onBoard(t, e2, 0, "Name:Scale\nManaCost:1\nTypes:Artifact Equipment\nK:Equip:1\nS:Mode$ Continuous | Affected$ Card.AttachedBy | AddToughness$ X | Description$ x\nSVar:X:Equipped$CardToughness\nOracle:x\n")
		e2.emit(events.Event{Kind: events.Attach, Obj: eq, IDs: []state.ObjID{bear2}})
		b2 := e2.G.Obj(bear2)
		if b2 == nil || b2.Zone != state.ZBattlefield {
			t.Fatal("setup: Bear2 is not a live battlefield permanent")
		}
		if a := e2.G.Obj(eq); a == nil || a.AttachedTo != bear2 {
			t.Fatalf("setup: Scale is not attached to Bear2 (equipment=%+v)", a)
		}
		if p, tough := e2.Power(bear2), e2.Toughness(bear2); p != 2 || tough != 4 {
			t.Fatalf("Equipped$CardToughness pump on a 2/2 body: %d/%d, want 2/4 (2 + pre-counter toughness 2)", p, tough)
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestSelfReferentialPTTerminates$", "-test.count=1")
	cmd.Env = append(os.Environ(), "GORGE_SELFPTC_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := string(out)
		if i := strings.Index(msg, "goroutine "); i > 0 && len(msg) > i+1500 {
			msg = msg[:i+1500]
		}
		t.Fatalf("a static reading its own affected object's toughness did not return: %v\n%s", err, msg)
	}
}
