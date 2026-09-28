package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestCopiedGrantedTriggerKeepsItsLineAcrossStackCopyAndClone verifies that a
// copy of a granted trigger retains the trigger-line clauses used by
// resolution. The original wrapper is removed so only the copied wrapper can
// produce the optional and cost asks.
func TestCopiedGrantedTriggerKeepsItsLineAcrossStackCopyAndClone(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 907, mendicantCoreSrc, artifactSpellSrc, plainSifterSrc)
	moveSeeded(t, e, 0, mendicantCoreSrc, state.ZBattlefield)
	moveSeeded(t, e, 0, artifactSpellSrc, state.ZHand)
	addMana(t, e, 0, "UUU")
	castCardNow(t, e, "Test Trinket Spell")
	spell := spellOnStack(t, e, "Test Trinket Spell")
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZStack {
		t.Fatalf("cast spell is not on the stack: %d", spell)
	}

	var triggerID state.ObjID
	for _, id := range e.G.Stack {
		o := e.G.Obj(id)
		if o == nil || o.Ability == nil {
			continue
		}
		line, ok := e.triggerForAbilityObject(id, o)
		if ok && line.Params["OptionalDecider"] == "You" && line.Params["Execute"] == "TrigCopy" {
			triggerID = id
			break
		}
	}
	if triggerID == 0 {
		t.Fatal("granted AddTrigger$ wrapper with OptionalDecider$ and TrigCopy is not on the stack")
	}
	if got := e.triggerLines[triggerID].Params["OptionalDecider"]; got != "You" {
		t.Fatalf("granted trigger line OptionalDecider$ = %q, want You", got)
	}

	// StackCopy is an event fold; the new wrapper must inherit engine-only
	// trigger provenance just as it inherits TriggerContext.
	e.emit(events.Event{Kind: events.StackCopy, Obj: triggerID, Player: 0})
	copyID := e.G.Stack[len(e.G.Stack)-1]
	if copyID == triggerID || e.G.Obj(copyID) == nil || e.G.Obj(copyID).Zone != state.ZStack {
		t.Fatalf("StackCopy produced invalid wrapper %d from %d", copyID, triggerID)
	}
	if _, ok := e.triggerForAbilityObject(copyID, e.G.Obj(copyID)); !ok {
		t.Fatal("StackCopy of granted trigger lost its trigger line")
	}

	// Remove the original wrapper; the cloned pending stack now proves that
	// the copied wrapper alone carries the trigger line into Clone().
	e.emit(events.Event{Kind: events.MoveZone, Obj: triggerID, From: state.ZStack, To: state.ZExile, Player: 0})
	clone := e.Clone()
	cloneCopy := clone.G.Obj(copyID)
	if cloneCopy == nil || cloneCopy.Zone != state.ZStack {
		t.Fatalf("cloned copied trigger zone = %v, want stack", cloneCopy.Zone)
	}
	line, ok := clone.triggerForAbilityObject(copyID, cloneCopy)
	if !ok || line.Params["OptionalDecider"] != "You" || line.Params["Execute"] != "TrigCopy" {
		t.Fatalf("cloned granted trigger line = %+v, found=%v; want OptionalDecider You and Execute TrigCopy", line, ok)
	}
	if clone.triggerLines[copyID].Params == nil {
		t.Fatal("cloned trigger line lost its parameter map")
	}

	sawOptional, sawPay := drainCopyGrants(t, clone, 40, true, true)
	if !sawOptional || !sawPay {
		t.Fatalf("copied granted trigger asks: Optional=%v Cost=%v, want both", sawOptional, sawPay)
	}
	if got := copyCount(clone, spell); got != 1 {
		t.Fatalf("copied granted trigger made %d spell copies, want 1", got)
	}
}
