package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// choose_source_predicates_test.go pins the two predicate families the
// ChooseSource row names ("colour-source and shadow predicates fail closed",
// deflecting_palm's `Card,Emblem` / circle_of_protection_red's
// `Card.RedSource` / circle_of_protection_shadow's
// `Card.ChosenCardStrict+withShadow`) plus the source-scoped one-shot self-
// exile ender the Effect's OWN Triggers$ body and the registering chain use.

// TestSourceScopedSelfExileEndsOnlyEffectRegistrations pins the Stamp-zero
// frame: the self-exile idiom run under Ctx.EffectFrame{Source: src} (no
// per-registration stamp) ends every Effect-created registration from that
// source and leaves the same source's printed statics -- and every other
// source's registrations -- untouched.
func TestSourceScopedSelfExileEndsOnlyEffectRegistrations(t *testing.T) {
	h := newHost(t, 2)
	src := h.g.AddObject(mkCard(t, "Name:Frame\nTypes:Instant\nOracle:x\n"), 0)
	h.AddContinuous(state.ContinuousEffect{Source: src.ID, Timestamp: 7, Controller: 0, Affects: "Card.Self", FromEffect: true})
	h.AddContinuous(state.ContinuousEffect{Source: src.ID, Timestamp: 8, Controller: 0, Affects: "Card.Self"})
	h.AddContinuous(state.ContinuousEffect{Source: 9999, Timestamp: 7, Controller: 1, Affects: "Card.Self", FromEffect: true})
	if len(h.continuous) != 3 {
		t.Fatalf("precondition: %d registrations, want 3", len(h.continuous))
	}

	c := &Ctx{Source: src.ID, Controller: 0, EffectFrame: EffectFrame{Source: src.ID}}
	Resolve(h, c, sa(t, "DB$ ChangeZone | Defined$ Self | Origin$ Command | Destination$ Exile"))

	if len(h.continuous) != 2 {
		t.Fatalf("registrations = %+v, want only the Effect-created one from %d dropped", h.continuous, src.ID)
	}
	for _, ce := range h.continuous {
		if ce.Source == src.ID && ce.FromEffect {
			t.Fatalf("the source-scoped frame survived its self-exile: %+v", ce)
		}
	}
	if got := h.g.Obj(src.ID).Zone; got == state.ZExile {
		t.Fatalf("the effect source was exiled as a card")
	}
}

// TestColourSourcePredicatesMatch pins `Card.<Colour>Source` (CR 700.7's "a
// red source"): the candidate's colour characteristics contain the colour.
// The red creature matches, the blue creature and the colourless artifact do
// not — the two directions, so a predicate that returned a constant cannot
// pass.
func TestColourSourcePredicatesMatch(t *testing.T) {
	h := newHost(t, 2)
	red := h.g.AddObject(mkCard(t, "Name:Red\nManaCost:R\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	blue := h.g.AddObject(mkCard(t, "Name:Blue\nManaCost:U\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	colorless := h.g.AddObject(mkCard(t, "Name:Rock\nManaCost:2\nTypes:Artifact\nOracle:x\n"), 1)
	for _, id := range []state.ObjID{red.ID, blue.ID, colorless.ID} {
		h.g.Obj(id).Zone = state.ZBattlefield
	}

	if !choiceMatches(h, h.g, &Ctx{Controller: 0}, "Card.RedSource", h.g.Obj(red.ID)) {
		t.Fatal("a red creature did not match Card.RedSource")
	}
	if choiceMatches(h, h.g, &Ctx{Controller: 0}, "Card.RedSource", h.g.Obj(blue.ID)) {
		t.Fatal("a blue creature matched Card.RedSource")
	}
	if choiceMatches(h, h.g, &Ctx{Controller: 0}, "Card.RedSource", h.g.Obj(colorless.ID)) {
		t.Fatal("a colourless artifact matched Card.RedSource")
	}
	if !choiceMatches(h, h.g, &Ctx{Controller: 0}, "Card.ColorlessSource", h.g.Obj(colorless.ID)) {
		t.Fatal("a colourless artifact did not match Card.ColorlessSource")
	}

	// circle_of_protection_red's gate: `Card.ChosenCardStrict+RedSource`
	// requires BOTH the chosen identity and the colour, so a chosen blue
	// creature must not match a red-source gate whose chosen list holds it.
	chosenRed := &Ctx{Controller: 0, Chosen: []state.Target{{Obj: red.ID}}, ChosenValid: true}
	if !choiceMatches(h, h.g, chosenRed, "Card.ChosenCardStrict+RedSource", h.g.Obj(red.ID)) {
		t.Fatal("the chosen red source did not match Card.ChosenCardStrict+RedSource")
	}
	chosenBlue := &Ctx{Controller: 0, Chosen: []state.Target{{Obj: blue.ID}}, ChosenValid: true}
	if choiceMatches(h, h.g, chosenRed, "Card.ChosenCardStrict+RedSource", h.g.Obj(blue.ID)) {
		t.Fatal("a non-chosen blue creature matched Card.ChosenCardStrict+RedSource")
	}
	if choiceMatches(h, h.g, chosenBlue, "Card.ChosenCardStrict+RedSource", h.g.Obj(red.ID)) {
		t.Fatal("a red creature outside the chosen set matched Card.ChosenCardStrict+RedSource")
	}
}

// TestShadowPredicatesMatch pins circle_of_protection_shadow's shape: the
// keyword predicate `withShadow` reads the layer-derived keyword list
// (SpecContext.ExtraKeywords) and matches a creature that has shadow, while
// the compound `Card.ChosenCardStrict+withShadow` requires BOTH the chosen
// identity and the keyword -- a non-chosen shadow creature and a chosen
// non-shadow creature both fail, so a predicate that dropped either half
// cannot pass.
func TestShadowPredicatesMatch(t *testing.T) {
	h := newHost(t, 2)
	shadow := h.g.AddObject(mkCard(t, "Name:Shade\nManaCost:B\nTypes:Creature\nPT:1/1\nK:Shadow\nOracle:x\n"), 1)
	plain := h.g.AddObject(mkCard(t, "Name:Plain\nManaCost:G\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	for _, id := range []state.ObjID{shadow.ID, plain.ID} {
		h.g.Obj(id).Zone = state.ZBattlefield
	}
	// Precondition: the two candidates differ -- exactly one has the shadow
	// keyword in its printed face, which is what the derived list mirrors.
	if !h.g.Obj(shadow.ID).Face().HasKeyword("Shadow") || h.g.Obj(plain.ID).Face().HasKeyword("Shadow") {
		t.Fatalf("precondition: shadow=%v plain=%v", h.g.Obj(shadow.ID).Face().HasKeyword("Shadow"), h.g.Obj(plain.ID).Face().HasKeyword("Shadow"))
	}

	shadowCtx := SpecContext{You: 0, ExtraKeywords: []string{"Shadow"}}
	plainCtx := SpecContext{You: 0, ExtraKeywords: []string{"Flying"}}
	if !MatchesSpecCtx(h.g, "Creature.withShadow", shadow.ID, shadowCtx) {
		t.Fatal("a shadow creature did not match Creature.withShadow with the derived keyword bound")
	}
	if MatchesSpecCtx(h.g, "Creature.withShadow", plain.ID, plainCtx) {
		t.Fatal("a non-shadow creature matched Creature.withShadow")
	}
	// The resolution-time path the corpus's ChooseCard uses
	// (circle_of_protection_shadow's `Choices$ Creature.withShadow`): with
	// no derived list bound, the printed-keyword predicate must still admit
	// a printed shadow creature.
	if !choiceMatches(h, h.g, &Ctx{Controller: 0}, "Creature.withShadow", h.g.Obj(shadow.ID)) {
		t.Fatal("a printed shadow creature did not match Creature.withShadow through choiceMatches")
	}
	if choiceMatches(h, h.g, &Ctx{Controller: 0}, "Creature.withShadow", h.g.Obj(plain.ID)) {
		t.Fatal("a non-shadow creature matched Creature.withShadow through choiceMatches")
	}

	chosenShadow := SpecContext{You: 0, ExtraKeywords: []string{"Shadow"}, Chosen: []state.Target{{Obj: shadow.ID}}, ChosenValid: true}
	if !MatchesSpecCtx(h.g, "Card.ChosenCardStrict+withShadow", shadow.ID, chosenShadow) {
		t.Fatal("the chosen shadow creature did not match Card.ChosenCardStrict+withShadow")
	}
	// A chosen NON-shadow creature: the keyword half must fail.
	if MatchesSpecCtx(h.g, "Card.ChosenCardStrict+withShadow", plain.ID, chosenShadow) {
		t.Fatal("a chosen non-shadow creature matched Card.ChosenCardStrict+withShadow")
	}
	// A shadow creature that was NOT chosen: the chosen half must fail.
	chosenPlain := SpecContext{You: 0, ExtraKeywords: []string{"Shadow"}, Chosen: []state.Target{{Obj: plain.ID}}, ChosenValid: true}
	if MatchesSpecCtx(h.g, "Card.ChosenCardStrict+withShadow", shadow.ID, chosenPlain) {
		t.Fatal("a non-chosen shadow creature matched Card.ChosenCardStrict+withShadow")
	}
}
