package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// The Alhammarret-shaped selector: ChooseFromDefinedCards$ narrows the
// ordinary ValidCards offer to the printed names of the Defined referents
// (the cards the reveal remembered). All fixtures use inline scripts, never
// committed Forge data.

// TestNameCardChooseFromDefinedOffersOnlyRememberedName is the positive
// control for the two fail-closed tests below: the SAME wiring (universe,
// memory, ValidCards$ Card.nonLand) poses the ask when a remembered
// nonland is eligible, offering exactly that name.
func TestNameCardChooseFromDefinedOffersOnlyRememberedName(t *testing.T) {
	h := &askHost{}
	h.g = namecardGameWithUniverse(t)
	bear := h.g.AddObject(mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"), 1)
	forest := h.g.AddObject(mkCard(t, "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n"), 1)
	if got := h.g.Obj(bear.ID).Face().Name; got != "Bear" {
		t.Fatalf("precondition: remembered face name = %q, want Bear", got)
	}
	if got := h.g.Obj(forest.ID).Face().Name; got != "Forest" {
		t.Fatalf("precondition: excluded face name = %q, want Forest", got)
	}
	src := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Sorcery\nOracle:x\n"), 0).ID
	Resolve(h, &Ctx{Controller: 0, Source: src,
		Remembered: []state.Target{{Obj: bear.ID}, {Obj: forest.ID}}},
		sa(t, "SP$ NameCard | ValidCards$ Card.nonLand | ChooseFromDefinedCards$ Remembered"))
	if h.asked == nil {
		t.Fatal("NameCard did not ask for a remembered nonland name")
	}
	if len(h.asked.Options) != 1 || !containsLabel(h.asked.Options, "Bear") {
		t.Fatalf("options = %+v, want exactly the remembered nonland Bear", h.asked.Options)
	}
	if containsLabel(h.asked.Options, "Forest") {
		t.Fatalf("options = %+v, the remembered land Forest must not be offered", h.asked.Options)
	}
}

// TestNameCardChooseFromDefinedStrictWhenValidCardsMatchesNothing pins the
// strict direction: ValidCards$ Card.nonLand matches ZERO cards of a
// land-only universe, and the ordinary NameCard totality fallback (a
// matched-nothing spec returning the WHOLE universe) must not resurrect the
// universe name through the defined-name intersection. The control resolve
// proves the wiring itself is live: the identical universe, memory and
// source DO ask under a filter the remembered name satisfies.
func TestNameCardChooseFromDefinedStrictWhenValidCardsMatchesNothing(t *testing.T) {
	h := &askHost{}
	h.g = state.NewGame(names(2))
	forestCard := mkCard(t, "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n")
	h.g.NameUniverse = []*cards.Card{forestCard}
	remembered := h.g.AddObject(forestCard, 1)
	if got := h.g.Obj(remembered.ID).Face().Name; got != "Forest" {
		t.Fatalf("precondition: remembered face name = %q, want Forest", got)
	}
	if len(h.g.NameUniverse) != 1 {
		t.Fatalf("precondition: universe holds %d cards, want exactly the land-only 1", len(h.g.NameUniverse))
	}
	src := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Sorcery\nOracle:x\n"), 0).ID

	// Control: under Card (unrestricted) the remembered Forest is offered,
	// proving the ask path, the universe and the remembered wiring all run.
	Resolve(h, &Ctx{Controller: 0, Source: src, Remembered: []state.Target{{Obj: remembered.ID}}},
		sa(t, "SP$ NameCard | ValidCards$ Card | ChooseFromDefinedCards$ Remembered"))
	if h.asked == nil || !containsLabel(h.asked.Options, "Forest") {
		t.Fatalf("control: expected a Forest offer, got asked=%+v", h.asked)
	}
	h.asked = nil

	// Narrow the filter to nonLand: zero universe matches. The remembered
	// Forest must NOT come back as an option via the totality fallback.
	Resolve(h, &Ctx{Controller: 0, Source: src, Remembered: []state.Target{{Obj: remembered.ID}}},
		sa(t, "SP$ NameCard | ValidCards$ Card.nonLand | ChooseFromDefinedCards$ Remembered"))
	if h.asked != nil {
		t.Fatalf("strict: posed ask %+v -- the totality fallback resurrected the land through the intersection", h.asked.Options)
	}
	if got := h.g.Obj(src).ChosenName; got != "" {
		t.Fatalf("strict: chose %q with no eligible name", got)
	}
}

// TestNameCardChooseFromDefinedUnknownSelectorFailsClosed pins the
// fail-closed direction on an unrecognised Defined spelling: Defined's
// historical fallback would act on the SA source, so with a source whose
// printed name IS a legal nonland of the universe the ask would offer a
// name that was never remembered. The control resolve proves the same
// wiring asks under the recognised Remembered spelling.
func TestNameCardChooseFromDefinedUnknownSelectorFailsClosed(t *testing.T) {
	h := &askHost{}
	h.g = namecardGameWithUniverse(t)
	bear := h.g.AddObject(mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"), 1)
	if got := h.g.Obj(bear.ID).Face().Name; got != "Bear" {
		t.Fatalf("precondition: source face name = %q, want Bear", got)
	}
	// The SA source itself is the Bear: a nonland card of the universe that
	// was never remembered. The broken Defined-fallback path offered it.
	src := h.g.AddObject(mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0).ID

	// Control: the recognised Remembered spelling with an actual memory asks.
	Resolve(h, &Ctx{Controller: 0, Source: src, Remembered: []state.Target{{Obj: bear.ID}}},
		sa(t, "SP$ NameCard | ValidCards$ Card.nonLand | ChooseFromDefinedCards$ Remembered"))
	if h.asked == nil || !containsLabel(h.asked.Options, "Bear") {
		t.Fatalf("control: expected a Bear offer, got asked=%+v", h.asked)
	}
	h.asked = nil

	// The unrecognised spelling must fail closed, not fall back to the
	// source's own (never remembered) name.
	Resolve(h, &Ctx{Controller: 0, Source: src},
		sa(t, "SP$ NameCard | ValidCards$ Card.nonLand | ChooseFromDefinedCards$ NotARealSelector"))
	if h.asked != nil {
		t.Fatalf("unknown selector: posed ask %+v -- the source fallback leaked a never-remembered name", h.asked.Options)
	}
	if got := h.g.Obj(src).ChosenName; got != "" {
		t.Fatalf("unknown selector: chose %q", got)
	}
}
