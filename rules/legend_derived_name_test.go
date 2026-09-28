package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const derivedNameLegend = "Name:Fenric\nManaCost:2 G\nTypes:Legendary Creature Horror\nPT:3/3\nOracle:x\n"

func runFenricChapterII(t *testing.T, e *Engine, saga, target state.ObjID) {
	t.Helper()
	answerQuiet(t, e, 80) // resolve chapter I's entry trigger
	e.emit(events.Event{Kind: events.CounterChange, Obj: saga, Counter: "LORE", Amount: 1})
	d := drainToTargetAsk(t, e, 120)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("chapter II did not ask for its Animate target: %+v", d)
	}
	if idx := indexOfObjOption(d, target); idx < 0 {
		t.Fatalf("Isamaru was not offered to chapter II: %+v", d.Options)
	} else {
		submitChoices(t, e, idx)
	}
	// This fixture has no duplicate pair yet, so the shared quiet drain can
	// settle Chapter II without consuming the later regression choice.
	answerQuiet(t, e, 120)
	d = e.Pending()
	if d != nil && d.Kind == decision.KChoose && d.Prompt == "Choose which Fenric to keep; the rest are put into their owners' graveyards" {
		return
	}
}

func assertLegendaryOnBoard(t *testing.T, e *Engine, ids ...state.ObjID) {
	t.Helper()
	for _, id := range ids {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: permanent %d is not on battlefield: %+v", id, o)
		}
		if o.Controller != 0 {
			t.Fatalf("precondition: permanent %d controlled by %d, want 0", id, o.Controller)
		}
		if !legendaryUnderLayers(e, id) {
			t.Fatalf("precondition: permanent %d is not legendary under layers", id)
		}
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("precondition: expected two distinct permanents, got %v", ids)
	}
}

func TestLegendRuleUsesDerivedName(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	fenric := lookup(t, reg, "The Curse of Fenric")
	isamaru := lookup(t, reg, "Isamaru, Hound of Konda")
	twin, diags := cards.ParseBytes("derived-legend-fenric.txt", []byte(derivedNameLegend))
	if len(diags) != 0 {
		t.Fatalf("parse inline Fenric legend: %v", diags)
	}
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{fenric, isamaru, twin}, nil)
	saga := moveByName(t, e, 0, "The Curse of Fenric", state.ZBattlefield)
	target := moveByName(t, e, 0, "Isamaru, Hound of Konda", state.ZBattlefield)
	if !e.G.Obj(target).Face().IsLegendary() {
		t.Fatal("precondition: Isamaru's printed face is not legendary")
	}
	if o := e.G.Obj(saga); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Saga is not on battlefield: %+v", o)
	}
	runFenricChapterII(t, e, saga, target)
	other := moveByName(t, e, 0, "Fenric", state.ZBattlefield)
	assertLegendaryOnBoard(t, e, target, other)
	// Direct fixture placement does not consume the game's opening priority ask.
	// Clear that unrelated ask so the explicit SBA pass can pose its decision.
	e.pending = nil
	e.checkStateBased()
	if printed := e.G.Obj(target).Face().Name; printed == e.G.Obj(other).Face().Name {
		t.Fatalf("precondition: printed names unexpectedly match: %q", printed)
	}
	if a, b := e.Name(target), e.Name(other); a != b || a != "Fenric" {
		t.Fatalf("precondition: derived names = %q and %q, want both Fenric", a, b)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("same-derived-name legends did not pose an exact two-member choice: %+v", d)
	}
	got := map[state.ObjID]bool{}
	for _, o := range d.Options {
		got[o.Obj] = true
	}
	if len(got) != 2 || !got[target] || !got[other] {
		t.Fatalf("legend choice IDs = %v, want exactly %d and %d", got, target, other)
	}
	// Keep the inline Fenric; Isamaru must leave via the legend rule.
	keep := -1
	for _, o := range d.Options {
		if o.Obj == other {
			keep = o.Index
		}
	}
	if keep < 0 {
		t.Fatalf("Fenric absent from choice: %+v", d.Options)
	}
	submitChoices(t, e, keep)
	if e.G.Obj(other).Zone != state.ZBattlefield || e.G.Obj(target).Zone != state.ZGraveyard {
		t.Fatalf("legend answer kept %d at %s and moved %d to %s; want keep Fenric and bin Isamaru", other, e.G.Obj(other).Zone, target, e.G.Obj(target).Zone)
	}
	replayCheck(t, e, cfg)
}

func TestLegendRuleDistinctDerivedNames(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	fenric := lookup(t, reg, "The Curse of Fenric")
	isamaru := lookup(t, reg, "Isamaru, Hound of Konda")
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{fenric, isamaru, isamaru}, nil)
	saga := moveByName(t, e, 0, "The Curse of Fenric", state.ZBattlefield)
	target := moveByName(t, e, 0, "Isamaru, Hound of Konda", state.ZBattlefield)
	runFenricChapterII(t, e, saga, target)
	other := moveByName(t, e, 0, "Isamaru, Hound of Konda", state.ZBattlefield)
	assertLegendaryOnBoard(t, e, target, other)
	// Clear the unrelated opening priority ask before explicitly checking SBAs.
	e.pending = nil
	e.checkStateBased()
	if a, b := e.G.Obj(target).Face().Name, e.G.Obj(other).Face().Name; a != b {
		t.Fatalf("precondition: printed names differ: %q and %q", a, b)
	}
	if a, b := e.Name(target), e.Name(other); a == b || a != "Fenric" || b != "Isamaru, Hound of Konda" {
		t.Fatalf("precondition: derived names = %q and %q, want Fenric and Isamaru", a, b)
	}
	e.checkStateBased()
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose && d.Prompt == "Choose which Isamaru, Hound of Konda to keep; the rest are put into their owners' graveyards" {
		t.Fatalf("different-derived-name legends incorrectly posed a duplicate choice: %+v", d)
	}
	if groups := e.legendGroups(); len(groups) != 0 {
		t.Fatalf("different-derived-name legends grouped: %+v", groups)
	}
	if e.G.Obj(target).Zone != state.ZBattlefield || e.G.Obj(other).Zone != state.ZBattlefield {
		t.Fatal("distinct current names should both remain on the battlefield")
	}
	replayCheck(t, e, cfg)
}
