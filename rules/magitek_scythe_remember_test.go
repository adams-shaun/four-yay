package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Magitek Scythe is the RememberAttached$ carrier whose payoff is a
// condition gate, not a filter exclusion: its ETB trigger attaches the
// Scythe to a creature you control and the chained DBPump carries
// `ConditionDefined$ Remembered | ConditionPresent$ Card | ConditionCompare$
// GE1` — the pump only runs when the attach actually remembered its object.
// Before the read the Remembered set stayed empty and the gate failed
// closed, so "that creature gains first strike until end of turn" never
// landed. Novel Nunchaku's ImmediateTrigger and Thorin's damage trigger ride
// the same gate shape; unexpected_request's delayed Unattach and Breath of
// Fury's UntapAll/AddPhase do too.
func TestMagitekScytheRememberAttachedUnlocksThePump(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Magitek Scythe", "Grizzly Bears")
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	id := searchMoveByName(t, e, "Magitek Scythe", state.ZHand)
	addMana(t, e, 0, "CCCCC")

	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Magitek Scythe: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passPriorityUntilAsk(t, e, "Choose a target for Magitek Scythe") // the cast resolves

	// The optional ETB trigger's target ask names the bear, then its
	// OptionalDecider$ You election applies the trigger.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the trigger's target ask, got %+v", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == bear {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("the bear was not offered: %+v", d.Options)
	}
	submitChoices(t, e, tgt)
	passPriorityUntilAsk(t, e, "") // the spell resolves; the trigger's election follows
	d = e.Pending()
	if d == nil || d.Kind != decision.KTriggerOptional || len(d.Options) != 2 {
		t.Fatalf("expected the trigger's optional election, got %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index) // yes — attach and pump

	if o := e.G.Obj(id); o == nil || o.AttachedTo != bear {
		t.Fatalf("the Scythe did not attach to the bear: %+v", e.G.Obj(id))
	}
	if !slices.Contains(e.Derived(bear).Keywords, "First Strike") {
		t.Fatalf("bearer keywords missing first strike (derived: %v) — the ConditionDefined$ Remembered gate did not see the remembered attach", e.Derived(bear).Keywords)
	}
	replayCheck(t, e, cfg)
}
