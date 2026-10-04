package effects

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestDelayedTriggerKnownKeysSorted: the unread lookup binary-searches the
// table.
func TestDelayedTriggerKnownKeysSorted(t *testing.T) {
	if !slices.IsSorted(delayedTriggerKnownKeys[:]) || len(slices.Compact(slices.Clone(delayedTriggerKnownKeys[:]))) != len(delayedTriggerKnownKeys) {
		t.Fatal("delayedTriggerKnownKeys must be sorted and duplicate-free")
	}
}

// TestCompileDelayedTrigger pins the compiled shapes the resolution reads:
// the scalar riders, the three registration texts and the unread report.
func TestCompileDelayedTrigger(t *testing.T) {
	sa := &cards.SA{API: "DelayedTrigger", Params: map[string]string{
		"Mode": "Phase", "Phase": "End of Turn", "ValidPlayer": " You ", "IsPresent": "Card.IsTriggerRemembered",
		"PresentZone": "Exile", "PresentCompare": "EQ1", "Execute": " TrigReturn ", "NextTurn": "True",
		"RememberChain": "false", "RememberObjects": " Targeted ", "ThisTurn": "True", "Static": "True",
		"Hidden": "True",
	}}
	p := DelayedTriggerOf(sa)
	if p.Mode != "Phase" || p.Phase != "End of Turn" || p.Execute != "TrigReturn" || !p.NextTurn ||
		!p.RememberChainFalse || p.RememberObjects != "Targeted" || !p.ThisTurn || p.Static != "True" {
		t.Fatalf("scalars = %+v", p)
	}
	if want := "End of Turn|VP=You|IP=Card.IsTriggerRemembered|PZ=Exile|PC=EQ1"; p.PhaseText != want {
		t.Fatalf("PhaseText = %q, want %q", p.PhaseText, want)
	}
	if want := "Phase:Mode$ Phase | ValidPlayer$ You | ThisTurn$ True | Static$ True | IsPresent$ Card.IsTriggerRemembered | PresentCompare$ EQ1 | PresentZone$ Exile"; p.EventText != want {
		t.Fatalf("EventText = %q, want %q", p.EventText, want)
	}
	if want := "SpellCast:Mode$ SpellCast | ValidPlayer$ You | Static$ True"; p.SpellCastText != want {
		t.Fatalf("SpellCastText = %q, want %q", p.SpellCastText, want)
	}
	if !slices.Equal(p.Unread, []string{"Hidden"}) {
		t.Fatalf("unread = %v, want [Hidden]", p.Unread)
	}
	if DelayedTriggerOf(sa) != p {
		t.Fatal("front cache missed the same Params map")
	}
	q := DelayedTriggerOf(&cards.SA{API: "DelayedTrigger", Params: map[string]string{"Mode": "Phase", "RememberChain": "True"}})
	if q.NextTurn || q.RememberChainFalse || q.ThisTurn || q.PhaseText != "" || q.Unread != nil {
		t.Fatalf("absent shapes = %+v", q)
	}
}

// TestDelayedTriggerOfIsAllocationFree: a configured record or a front-cache
// hit allocates nothing.
func TestDelayedTriggerOfIsAllocationFree(t *testing.T) {
	bound := &cards.SA{API: "DelayedTrigger", Params: map[string]string{"Mode": "Phase", "Phase": "Upkeep", "Execute": "X"}}
	f := NewSAFacts(bound)
	f.Publish()
	cached := &cards.SA{API: "DelayedTrigger", Params: map[string]string{"Mode": "Phase", "Phase": "End of Turn"}}
	DelayedTriggerOf(cached)
	if n := allocsPerRun(100, func() {
		_ = DelayedTriggerOf(bound)
		_ = DelayedTriggerOf(cached)
	}); n != 0 {
		t.Fatalf("DelayedTriggerOf allocated %v objects per run; want 0", n)
	}
	if f.DelayedTrigger == nil || !f.DelayedTrigger.boundTo(bound.Params) {
		t.Fatal("NewSAFacts did not compile the DelayedTrigger half")
	}
}
