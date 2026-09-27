package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestExchangeTextBoxExchangeOfWords(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	cardDef := mustCorpusCard(t, reg, "Exchange of Words")
	script := cardDef.Faces[0].SVars["DBExchangeText"]
	if script != "DB$ ExchangeTextBox | ValidTgts$ Creature | TargetMin$ 2 | TargetMax$ 2 | Duration$ AsLongAsInPlay" {
		t.Fatalf("Exchange of Words DBExchangeText = %q", script)
	}
	creatureA := mustCorpusCard(t, reg, "Goblin Piledriver")
	creatureB := mustCorpusCard(t, reg, "Serra Angel")
	e := layerEngine(t)
	source := onBoardCard(t, e, 0, cardDef)
	a := onBoardCard(t, e, 0, creatureA)
	b := onBoardCard(t, e, 1, creatureB)
	textA, textB := assertTextBoxPrecondition(t, e, a, b)
	beforeA, beforeB := e.Derived(a), e.Derived(b)

	effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0, Targets: []state.Target{{Obj: a}, {Obj: b}}},
		&cards.SA{API: "ExchangeTextBox", Params: map[string]string{
			"ValidTgts": "Creature", "TargetMin": "2", "TargetMax": "2", "Duration": "AsLongAsInPlay", "Defined": "Targeted",
		}})
	assertSwappedAndCharacteristicsPreserved(t, e, a, b, textA, textB, beforeA, beforeB)

	// Duration$ follows the enchantment, not either affected creature.
	e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: state.ZBattlefield, To: state.ZGraveyard})
	if got := e.Text(a); got != creatureA.Faces[0].Oracle {
		t.Fatalf("after Exchange of Words leaves play, A text = %q, want printed %q", got, creatureA.Faces[0].Oracle)
	}
	if got := e.Text(b); got != creatureB.Faces[0].Oracle {
		t.Fatalf("after Exchange of Words leaves play, B text = %q, want printed %q", got, creatureB.Faces[0].Oracle)
	}
}

func TestExchangeTextBoxDeadpoolTradingCard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	deadpool := mustCorpusCard(t, reg, "Deadpool, Trading Card")
	script := deadpool.Faces[0].SVars["DBExchangeText"]
	if script != "DB$ ExchangeTextBox | Defined$ Self & ChosenCard" {
		t.Fatalf("Deadpool DBExchangeText = %q", script)
	}
	otherCard := mustCorpusCard(t, reg, "Goblin Piledriver")
	e := layerEngine(t)
	a := onBoardCard(t, e, 0, deadpool)
	b := onBoardCard(t, e, 1, otherCard)
	textA, textB := assertTextBoxPrecondition(t, e, a, b)
	beforeA, beforeB := e.Derived(a), e.Derived(b)

	// The corpus omits Duration$: under CR 611.2a the exchange has no
	// stated end, apart from the affected objects changing zones.
	effects.Resolve(e, &effects.Ctx{Source: a, Controller: 0, Chosen: []state.Target{{Obj: b}}, ChosenValid: true},
		&cards.SA{API: "ExchangeTextBox", Params: map[string]string{"Defined": "Self & ChosenCard"}})
	assertSwappedAndCharacteristicsPreserved(t, e, a, b, textA, textB, beforeA, beforeB)
	e.emit(events.Event{Kind: events.MoveZone, Obj: a, From: state.ZBattlefield, To: state.ZGraveyard})
	if got := e.Text(b); got != textA {
		t.Fatalf("after Deadpool leaves play, other text = %q, want Deadpool's indefinite box %q", got, textA)
	}
}

func assertTextBoxPrecondition(t *testing.T, e *Engine, a, b state.ObjID) (string, string) {
	t.Helper()
	for _, id := range []state.ObjID{a, b} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: object %d must be on battlefield, got %+v", id, o)
		}
	}
	textA, textB := e.Text(a), e.Text(b)
	if textA == textB {
		t.Fatalf("precondition: source text boxes must differ, both are %q", textA)
	}
	return textA, textB
}

func assertSwappedAndCharacteristicsPreserved(t *testing.T, e *Engine, a, b state.ObjID, textA, textB string, beforeA, beforeB Derived) {
	t.Helper()
	if got := e.Text(a); got != textB {
		t.Fatalf("A text after exchange = %q, want B's text %q", got, textB)
	}
	if got := e.Text(b); got != textA {
		t.Fatalf("B text after exchange = %q, want A's text %q", got, textA)
	}
	afterA, afterB := e.Derived(a), e.Derived(b)
	if beforeA.Name != afterA.Name || beforeA.Power != afterA.Power || beforeA.Toughness != afterA.Toughness ||
		beforeA.Colors != afterA.Colors || !sameStringSet(beforeA.Types, afterA.Types) {
		t.Fatalf("A non-text characteristics changed: before=%+v after=%+v", beforeA, afterA)
	}
	if beforeB.Name != afterB.Name || beforeB.Power != afterB.Power || beforeB.Toughness != afterB.Toughness ||
		beforeB.Colors != afterB.Colors || !sameStringSet(beforeB.Types, afterB.Types) {
		t.Fatalf("B non-text characteristics changed: before=%+v after=%+v", beforeB, afterB)
	}
}
