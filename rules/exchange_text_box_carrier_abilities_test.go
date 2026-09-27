package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestExchangeTextBoxExchangeOfWordsAbilityAndExpiry(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	sourceCard := mustCorpusCard(t, reg, "Exchange of Words")
	aCard := mustCorpusCard(t, reg, "Goblin Piledriver")
	bCard := mustCorpusCard(t, reg, "Serra Angel")
	e := layerEngine(t)
	source := onBoardCard(t, e, 0, sourceCard)
	a := onBoardCard(t, e, 0, aCard)
	b := onBoardCard(t, e, 1, bCard)
	textA, textB := assertTextBoxPrecondition(t, e, a, b)
	beforeA, beforeB := e.Derived(a), e.Derived(b)
	if !e.HasKeyword(a, "Protection from blue") || !e.HasKeyword(b, "Flying") || !e.HasKeyword(b, "Vigilance") {
		t.Fatalf("precondition: expected different corpus keyword boxes, got %v / %v", e.Keywords(a), e.Keywords(b))
	}

	effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0, Targets: []state.Target{{Obj: a}, {Obj: b}}},
		&cards.SA{API: "ExchangeTextBox", Params: map[string]string{
			"Defined": "Targeted", "Duration": "AsLongAsInPlay",
		}})
	assertSwappedAndCharacteristicsPreserved(t, e, a, b, textA, textB, beforeA, beforeB)
	if e.HasKeyword(a, "Protection from blue") || !e.HasKeyword(a, "Flying") || !e.HasKeyword(a, "Vigilance") {
		t.Fatalf("A keywords after exchange = %v, want B's", e.Keywords(a))
	}
	if !e.HasKeyword(b, "Protection from blue") || e.HasKeyword(b, "Flying") {
		t.Fatalf("B keywords after exchange = %v, want A's", e.Keywords(b))
	}

	// This carrier explicitly lasts only as long as its enchantment remains.
	e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: state.ZBattlefield, To: state.ZGraveyard})
	if got := e.Text(a); got != textA {
		t.Fatalf("A text after source leaves = %q, want printed %q", got, textA)
	}
	if got := e.Text(b); got != textB {
		t.Fatalf("B text after source leaves = %q, want printed %q", got, textB)
	}
	if !e.HasKeyword(a, "Protection from blue") || e.HasKeyword(a, "Flying") {
		t.Fatalf("A keywords after source leaves = %v, want its own", e.Keywords(a))
	}
}

func TestExchangeTextBoxDeadpoolTradingCardAbilityAndDuration(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	deadpool := mustCorpusCard(t, reg, "Deadpool, Trading Card")
	other := mustCorpusCard(t, reg, "Goblin Piledriver")
	e := layerEngine(t)
	a := onBoardCard(t, e, 0, deadpool)
	b := onBoardCard(t, e, 1, other)
	textA, textB := assertTextBoxPrecondition(t, e, a, b)
	beforeA, beforeB := e.Derived(a), e.Derived(b)
	if e.HasKeyword(a, "Protection from blue") || !e.HasKeyword(b, "Protection from blue") {
		t.Fatalf("precondition: expected different corpus keyword boxes, got %v / %v", e.Keywords(a), e.Keywords(b))
	}

	// No Duration$ means the exchanged box is indefinite; the source leaving
	// must not remove the exchange from the other creature.
	effects.Resolve(e, &effects.Ctx{Source: a, Controller: 0, Chosen: []state.Target{{Obj: b}}, ChosenValid: true},
		&cards.SA{API: "ExchangeTextBox", Params: map[string]string{"Defined": "Self & ChosenCard"}})
	assertSwappedAndCharacteristicsPreserved(t, e, a, b, textA, textB, beforeA, beforeB)
	if !e.HasKeyword(a, "Protection from blue") || e.HasKeyword(b, "Protection from blue") {
		t.Fatalf("keywords after exchange = %v / %v, want exchanged boxes", e.Keywords(a), e.Keywords(b))
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: a, From: state.ZBattlefield, To: state.ZGraveyard})
	if got := e.Text(b); got != textA {
		t.Fatalf("after Deadpool leaves, other text = %q, want Deadpool's indefinite box %q", got, textA)
	}
	if e.HasKeyword(b, "Protection from blue") {
		t.Fatalf("after Deadpool leaves, other keywords = %v, want Deadpool's box", e.Keywords(b))
	}
}
