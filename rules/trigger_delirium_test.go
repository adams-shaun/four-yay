package rules

// Task agent-20260919T192133Z-5a0279a8: the trigger-side `Delirium$ True`
// intervening-if (CR 207.2c, "four or more card types among cards in your
// graveyard"). rules/trigger_condition.go's triggerConditionHoldsWithSVars
// read LifeAmount$/IsPresent$/PresentCompare$/CheckSVar$/Metalcraft$/Revolt$
// but had no Delirium$ case, so every `T:... | Delirium$ True | ...` line
// fired UNCONDITIONALLY -- the over-fire direction. The measured population
// at this corpus pin is 22 raw T: lines across 21 files, all named in the
// carrier table below.
//
// The gate reuses the ONE graveyardCardTypeCount census every other Delirium
// spelling reads (rules/replacement.go's Delirium$ clause, rules/layers.go's
// Continuous gate, rules/legal.go's ability-offer gate, and the effects bare
// Condition$ Delirium gate via Host.DeliriumHolds), so the trigger and
// static directions can no longer disagree.
//
// The fixtures drive the REAL compiled corpus cards (never a re-written
// copy), so each pin is the card's actual script shape.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// deliriumTriggerCarriers is the measured population: every corpus card whose
// face carries a `T:` line with `Delirium$ True`. The list is checked against
// the corpus in TestTriggerDeliriumGatePerCorpusCarrier's precondition (both
// directions) so a pin move that adds or drops a carrier makes the table
// stale loudly. Fear of Missing Out carries TWO such triggers (ChangesZone +
// Attacks), so the walk pins each.
var deliriumTriggerCarriers = []string{
	"Angel of Deliverance",
	"Autumnal Gloom",
	"Demolisher Spawn",
	"Extricator of Sin",
	"Fear of Burning Alive",
	"Fear of Missing Out",
	"Gibbering Fiend",
	"Gouged Zealot",
	"Hand That Feeds",
	"Inexorable Blob",
	"Ishkanah, Grafwidow",
	"Manic Scribe",
	"Mournwillow",
	"Obsessive Skinner",
	"Omnivorous Flytrap",
	"Osseous Sticktwister",
	"Soul Swallower",
	"Tooth Collector",
	"Topplegeist",
	"Wickerfolk Thresher",
	"Winter, Cynical Opportunist",
}

// deliriumTriggers returns every trigger on the card's faces carrying
// Delirium$ True (there can be more than one).
func deliriumTriggers(c *cards.Card) []cards.Trigger {
	var out []cards.Trigger
	for _, f := range c.Faces {
		for _, tr := range f.Triggers {
			if strings.EqualFold(strings.TrimSpace(tr.Params["Delirium"]), "True") {
				out = append(out, tr)
			}
		}
	}
	return out
}

// TestTriggerDeliriumGatePerCorpusCarrier walks every measured carrier and
// asserts the fire-time CR 603.4 gate denies an empty graveyard and admits
// four distinct core types -- on the card's REAL trigger IR.
func TestTriggerDeliriumGatePerCorpusCarrier(t *testing.T) {
	t.Parallel()
	reg := choiceCorpusRegistry(t)
	// Precondition over the measured population, both directions: every
	// named card really carries a Delirium$ True trigger, and no OTHER corpus
	// card does, so the table cannot silently go stale when the pin moves.
	named := map[string]bool{}
	for _, name := range deliriumTriggerCarriers {
		named[name] = true
	}
	seen := map[string]bool{}
	for _, c := range reg.AllCards() {
		if len(deliriumTriggers(c)) > 0 {
			seen[c.Faces[0].Name] = true
		}
	}
	for name := range seen {
		if !named[name] {
			t.Errorf("corpus card %q carries a T: Delirium$ True trigger but is not in deliriumTriggerCarriers -- add it", name)
		}
	}
	for _, name := range deliriumTriggerCarriers {
		if !seen[name] {
			t.Errorf("deliriumTriggerCarriers names %q but the corpus card carries no Delirium$ True trigger -- stale entry", name)
		}
	}

	for _, name := range deliriumTriggerCarriers {
		name := name
		t.Run(name, func(t *testing.T) {
			trs := deliriumTriggers(choiceCorpusCard(t, name))
			if len(trs) == 0 {
				t.Fatalf("precondition: %q has no Delirium$ True trigger", name)
			}
			e, cfg, id := gateFixture(t, 960, name,
				deliriumBearSrc, deliriumPlainsSrc, deliriumBoltSrc, deliriumOathSrc)
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
			if z := e.G.Obj(id).Zone; z != state.ZBattlefield {
				t.Fatalf("precondition: source in %s, want battlefield", z)
			}
			// Deny arm: an empty graveyard is fewer than four distinct types.
			if got := len(graveyardDeliriumTypes(t, e, 0)); got != 0 {
				t.Fatalf("precondition: %d distinct graveyard types, want 0", got)
			}
			for _, tr := range trs {
				if e.triggerConditionHoldsAs(tr, id, 0) {
					t.Fatalf("Delirium$ True trigger (Mode %s) held with an empty graveyard, want denied", tr.Mode)
				}
			}
			// Admit arm: exactly four distinct core types.
			millExtras(t, e, "Delirium Bear", "Delirium Plains", "Delirium Bolt", "Delirium Oath")
			types := graveyardDeliriumTypes(t, e, 0)
			if len(types) != 4 {
				t.Fatalf("precondition: %d distinct graveyard types (%v), want 4", len(types), types)
			}
			for _, tr := range trs {
				if !e.triggerConditionHoldsAs(tr, id, 0) {
					t.Fatalf("Delirium$ True trigger (Mode %s) denied with four distinct graveyard types (%v)", tr.Mode, types)
				}
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestTriggerDeliriumValueFailsClosed pins the unreadable-value direction: a
// Delirium$ value this build cannot read as True must fail closed (the
// trigger does not fire), the same convention the Metalcraft$/Revolt$
// clauses document.
func TestTriggerDeliriumValueFailsClosed(t *testing.T) {
	t.Parallel()
	e, _, _ := gateFixture(t, 961, "Grizzly Bears",
		deliriumBearSrc, deliriumPlainsSrc, deliriumBoltSrc, deliriumOathSrc)
	millExtras(t, e, "Delirium Bear", "Delirium Plains", "Delirium Bolt", "Delirium Oath")
	if got := len(graveyardDeliriumTypes(t, e, 0)); got != 4 {
		t.Fatalf("precondition: %d distinct graveyard types, want 4", got)
	}
	for _, v := range []string{"False", "Maybe", "1"} {
		tr := cards.Trigger{Mode: "Phase", Params: map[string]string{"Delirium": v}}
		if e.triggerConditionHoldsAs(tr, 0, 0) {
			t.Errorf("Delirium$ %q held with four graveyard types, want fail-closed denial", v)
		}
	}
	// Control: the identical trigger with the readable value still admits, so
	// the loop above cannot pass vacuously.
	tr := cards.Trigger{Mode: "Phase", Params: map[string]string{"Delirium": "True"}}
	if !e.triggerConditionHoldsAs(tr, 0, 0) {
		t.Error("Delirium$ True denied with four graveyard types, want admitted")
	}
}

// TestWinterDeliriumEndStepTriggerGatesOnGraveyardTypes drives the brief's
// named deck card end to end: Winter, Cynical Opportunist's Delirium end-step
// trigger is on the stack only once the controller's graveyard holds four
// distinct core card types. With four cards but only three distinct types it
// must not queue at all (the over-fire direction the ticket closes); a fresh
// board whose graveyard holds four distinct types queues it.
func TestWinterDeliriumEndStepTriggerGatesOnGraveyardTypes(t *testing.T) {
	t.Parallel()
	t.Run("three distinct types does not queue", func(t *testing.T) {
		e, cfg, winter := gateFixture(t, 962, "Winter, Cynical Opportunist",
			deliriumBearSrc, deliriumPlainsSrc, deliriumBoltSrc,
			"Name:Delirium Bear 2\nTypes:Creature\nPT:2/2\nOracle:x\n")
		e.emit(events.Event{Kind: events.MoveZone, Obj: winter, From: state.ZHand, To: state.ZBattlefield})
		if z := e.G.Obj(winter).Zone; z != state.ZBattlefield {
			t.Fatalf("precondition: Winter in %s, want battlefield", z)
		}
		if len(deliriumTriggers(choiceCorpusCard(t, "Winter, Cynical Opportunist"))) == 0 {
			t.Fatal("precondition: Winter carries no Delirium$ True trigger")
		}
		// Four cards, three DISTINCT core types (two creatures): the census
		// counts types, not cards, so the trigger must not queue.
		millExtras(t, e, "Delirium Bear", "Delirium Bear 2", "Delirium Plains", "Delirium Bolt")
		if got := len(graveyardDeliriumTypes(t, e, 0)); got != 3 {
			t.Fatalf("precondition: %d distinct graveyard types, want 3", got)
		}
		if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 4 {
			t.Fatalf("precondition: %d graveyard cards, want 4 (types, not cards, is the gate)", got)
		}
		driveToStep(t, e, e.G.Turn, 0, state.StepEnd)
		if e.G.Step != state.StepEnd {
			t.Fatalf("precondition: step %v, want the end step", e.G.Step)
		}
		if winterTriggerOnStack(e, winter) {
			t.Fatalf("Winter's Delirium end-step trigger is on the stack with only 3 distinct graveyard types")
		}
		replayCheck(t, e, cfg)
	})
	t.Run("four distinct types queues", func(t *testing.T) {
		e, cfg, winter := gateFixture(t, 963, "Winter, Cynical Opportunist",
			deliriumBearSrc, deliriumPlainsSrc, deliriumBoltSrc, deliriumOathSrc)
		e.emit(events.Event{Kind: events.MoveZone, Obj: winter, From: state.ZHand, To: state.ZBattlefield})
		if z := e.G.Obj(winter).Zone; z != state.ZBattlefield {
			t.Fatalf("precondition: Winter in %s, want battlefield", z)
		}
		millExtras(t, e, "Delirium Bear", "Delirium Plains", "Delirium Bolt", "Delirium Oath")
		types := graveyardDeliriumTypes(t, e, 0)
		if len(types) != 4 {
			t.Fatalf("precondition: %d distinct graveyard types (%v), want 4", len(types), types)
		}
		driveToStep(t, e, e.G.Turn, 0, state.StepEnd)
		if e.G.Step != state.StepEnd {
			t.Fatalf("precondition: step %v, want the end step", e.G.Step)
		}
		if !winterTriggerOnStack(e, winter) {
			t.Fatalf("Winter's Delirium end-step trigger did not queue with four distinct graveyard types (%v)", types)
		}
		replayCheck(t, e, cfg)
	})
}

// winterTriggerOnStack reports whether any object on the stack was created by
// a triggered ability whose source is the given Winter object.
func winterTriggerOnStack(e *Engine, winter state.ObjID) bool {
	for _, id := range e.G.Stack {
		if o := e.G.Obj(id); o != nil && o.Source == winter {
			return true
		}
	}
	return false
}
