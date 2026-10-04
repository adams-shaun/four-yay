package rules

// Kernel-era restorations of the tests W3 removed from primitives_untap_mana_test.go: the
// same behaviour driven through the resolution kernel (the only ask path).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestFellwarStoneAndChromeMoxReflectedShapesKernel(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	onBoard(t, e, 0, "Name:Fellwar Stone\nManaCost:2\nTypes:Artifact\n"+
		"A:AB$ ManaReflected | Cost$ T | ColorOrType$ Color | Valid$ Land.OppCtrl | ReflectProperty$ Produce | SpellDescription$ Add one mana of any color that a land an opponent controls could produce.\nOracle:x\n")
	onBoard(t, e, 1, mountainScript())
	e.priorityRound()
	if optionKinds(e.Pending())["activate"] != 1 {
		t.Fatalf("Fellwar Stone's activation not offered: %+v", e.Pending().Options)
	}
	castFirst(t, e, "activate")
	if e.G.Players[0].Pool[state.MR] != 1 {
		t.Fatalf("Fellwar Stone should have added one R, pool %+v", e.G.Players[0].Pool)
	}

	e2 := handEngine(t)
	chromeObj := e2.G.AddObject(card(t, chromeMoxScript), 0)
	chromeObj.Zone = state.ZHand
	e2.G.SetZone(state.ZHand, 0, append(e2.G.Zone(state.ZHand, 0), chromeObj.ID))
	chrome := chromeObj.ID
	blue := e2.G.AddObject(card(t, ancestralRecallScript), 0)
	blue.Zone = state.ZHand
	green := e2.G.AddObject(card(t, giantGrowthScript), 0)
	green.Zone = state.ZHand
	e2.G.SetZone(state.ZHand, 0, append(e2.G.Zone(state.ZHand, 0), blue.ID, green.ID))
	// Drive Chrome Mox's REAL ETB trigger through the stack. Two eligible
	// cards force the resumed KChoose path; choose the second and prove a
	// clone replays the same answer byte-for-byte.
	e2.emit(events.Event{Kind: events.MoveZone, Obj: chrome, From: state.ZHand, To: state.ZBattlefield})
	e2.putTriggersOnStack()
	// Resolve the trigger through real priority passes (a Submit-driven
	// resolution), not a direct resolveTop probe: the clone below must
	// re-execute its own resolution, and a probe's closure is bound to
	// the engine that took it.
	e2.pending = nil
	e2.priorityRound()
	d := passUntilNonPriority(t, e2, 10)
	if d == nil || d.Kind != decision.KTriggerOptional {
		t.Fatalf("Chrome Mox optional trigger did not ask at resolution: %+v", d)
	}
	if err := e2.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("accept imprint trigger: %v", err)
	}
	d = e2.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 || d.ResumeKind != "imprint" {
		t.Fatalf("expected two-card imprint choice, got %+v", d)
	}
	clone := e2.Clone()
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{1}}
	if err := e2.Submit(in); err != nil {
		t.Fatalf("choose second imprint: %v", err)
	}
	if err := clone.Submit(in); err != nil {
		t.Fatalf("replay second imprint on clone: %v", err)
	}
	if diff := diffGames(e2.G, clone.G); diff != "" {
		t.Fatalf("imprint replay diverged:\n%s", diff)
	}
	if got := e2.G.Obj(chrome).Imprinted; len(got) != 1 || got[0] != green.ID {
		t.Fatalf("Chrome Mox did not retain the chosen second imprint: %v", got)
	}
	e2.priorityRound()
	if optionKinds(e2.Pending())["activate"] != 1 {
		t.Fatalf("Chrome Mox did not offer mana from its imprinted blue card: %+v", e2.Pending())
	}
	castFirst(t, e2, "activate")
	if e2.G.Players[0].Pool[state.MG] != 1 {
		t.Fatalf("Chrome Mox did not produce the chosen imprinted card's G: %+v", e2.G.Players[0].Pool)
	}

	// CR 607.2a: the linked card stops being "the exiled card" after it
	// leaves exile. The persistent imprint ID must not follow it into another
	// zone and continue granting mana colours.
	e2.emit(events.Event{Kind: events.MoveZone, Obj: green.ID, From: state.ZExile, To: state.ZGraveyard})
	e2.emit(events.Event{Kind: events.Untap, Obj: chrome})
	e2.priorityRound()
	if optionKinds(e2.Pending())["activate"] != 0 {
		t.Fatalf("Chrome Mox still offered mana after its imprinted card left exile: %+v", e2.Pending().Options)
	}
}

// TestMysticRemoraCumulativeUpkeep proves the keyword is a real upkeep
// trigger. Its age counter is absent while the ability waits on the stack;
// only resolution places it, then opens the mana-only payment window.
func TestMysticRemoraCumulativeUpkeepKernel(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	remora := onBoard(t, e, 0, mysticRemoraScript)
	_ = onBoard(t, e, 0, mountainScript())
	e.G.Turn = 2
	e.beginTurn(0)
	if got := e.G.Obj(remora).Counter("AGE"); got != 0 {
		t.Fatalf("age counter appeared before the upkeep trigger resolved: %d", got)
	}
	e.priorityRound()
	if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Ability.API != "CumulativeUpkeep" {
		t.Fatalf("cumulative upkeep was not placed as a triggered ability: %v", e.G.Stack)
	}
	if got := e.G.Obj(remora).Counter("AGE"); got != 0 {
		t.Fatalf("age counter appeared while the trigger was still on stack: %d", got)
	}
	e.pending = nil // resolve directly, outside the pending priority window
	e.resolveTop()
	d := e.Pending()
	if d == nil || len(d.Options) != 2 || d.Options[0].Kind != "activate" || d.Options[1].Kind != "done" {
		t.Fatalf("expected cumulative mana window at resolution, got %+v", d)
	}
	if got := e.G.Obj(remora).Counter("AGE"); got != 1 {
		t.Fatalf("resolution placed %d age counters, want 1", got)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("activate mana: %v", err)
	}
	d = e.Pending()
	if d == nil || len(d.Options) != 2 || d.Options[0].Kind != "cumulative_pay" {
		t.Fatalf("expected pay-or-sacrifice after mana, got %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("pay upkeep: %v", err)
	}
	if e.G.Obj(remora).Zone != state.ZBattlefield || len(e.G.Stack) != 0 {
		t.Fatalf("paid Remora/stack = %s/%v", e.G.Obj(remora).Zone, e.G.Stack)
	}
}

func TestManaVaultTriggerCanActivateManaAndPayKernel(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	vault := onBoard(t, e, 0, "Name:Mana Vault\nManaCost:1\nTypes:Artifact\n"+
		"T:Mode$ Phase | Phase$ Upkeep | ValidPlayer$ You | OptionalDecider$ You | Execute$ TrigUntap\n"+
		"SVar:TrigUntap:AB$ Untap | Cost$ 4 | Defined$ Self\nOracle:x\n")
	for i := 0; i < 4; i++ {
		_ = onBoard(t, e, 0, mountainScript())
	}
	e.emit(events.Event{Kind: events.Tap, Obj: vault})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	// Resolve the trigger through real priority passes (a Submit-driven
	// resolution), not a direct resolveTop probe: the clone below must
	// re-execute its own resolution, and a probe's closure is bound to
	// the engine that took it.
	e.pending = nil
	e.priorityRound()
	d := passUntilNonPriority(t, e, 10)
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		d = e.Pending()
		if d == nil || d.Options[0].Kind != "activate" {
			t.Fatalf("mana payment window %d = %+v", i, d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatal(err)
		}
	}
	d = e.Pending()
	if d == nil || d.Options[0].Kind != "trigger_cost_pay" {
		t.Fatalf("paid trigger choice = %+v", d)
	}
	clone := e.Clone()
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}
	if err := e.Submit(in); err != nil {
		t.Fatal(err)
	}
	if err := clone.Submit(in); err != nil {
		t.Fatal(err)
	}
	if diff := diffGames(e.G, clone.G); diff != "" {
		t.Fatalf("trigger-cost replay diverged:\n%s", diff)
	}
	if e.G.Obj(vault).Tapped || e.G.Players[0].Pool.Total() != 0 || len(e.G.Stack) != 0 {
		t.Fatalf("paid Mana Vault result: tapped=%v pool=%v stack=%v", e.G.Obj(vault).Tapped, e.G.Players[0].Pool, e.G.Stack)
	}
}
