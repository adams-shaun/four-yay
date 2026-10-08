package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// namecardMvUniverse is the three-card universe the dynamic-filter tests pin:
// two creatures of DIFFERENT printed mana values (Grizzly Bears 1 G -> mv 2,
// Hill Giant 3 R -> mv 4) and a land, so a `Creature.cmcEQ<ctx>` predicate
// selects exactly one creature and cannot pass by accident on a tie.
func namecardMvUniverse(t *testing.T) []*cards.Card {
	t.Helper()
	bears := mkCard(t, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	giant := mkCard(t, "Name:Hill Giant\nManaCost:3 R\nTypes:Creature Giant\nPT:3/3\nOracle:x\n")
	waste := mkCard(t, "Name:Wasteland\nTypes:Land\nOracle:x\n")
	if bears.Faces[0].Cmc() == giant.Faces[0].Cmc() {
		t.Fatalf("precondition: fixtures share mana value %d, so an mv-filter cannot discriminate", bears.Faces[0].Cmc())
	}
	if bears.Faces[0].Cmc() != 2 || giant.Faces[0].Cmc() != 4 {
		t.Fatalf("precondition: mana values = %d, %d; want 2 and 4", bears.Faces[0].Cmc(), giant.Faces[0].Cmc())
	}
	return []*cards.Card{bears, giant, waste}
}

func namecardGameWithMvUniverse(t *testing.T) *state.Game {
	t.Helper()
	g := state.NewGame(names(2))
	g.NameUniverse = cards.UniverseOf(namecardMvUniverse(t))
	return g
}

// chosenNameEvent returns the single Choose/name event's text, or "" when the
// log holds anything other than exactly one such event.
func chosenNameEvent(log []events.Event) string {
	out := ""
	n := 0
	for _, ev := range log {
		if ev.Kind == events.Choose && ev.Counter == "name" {
			n++
			out = ev.Text
		}
	}
	if n != 1 {
		return ""
	}
	return out
}

// TestRandomNameCardResolvesPaidXFilter pins Variable Solutions' shape: a
// random NameCard whose ValidCards$ right-hand side is the resolution's paid
// X. All six corpus carriers spell it `Creature.cmcEQX`; before the resolving
// context was threaded through the universe walk the predicate failed every
// card, the strict path returned nil and effNameCard's R-9 legacy fallback
// silently named the top of the caster's library.
func TestRandomNameCardResolvesPaidXFilter(t *testing.T) {
	h := &askHost{}
	h.g = namecardGameWithMvUniverse(t)
	src := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Sorcery\nOracle:x\n"), 0).ID
	// A library top whose name is NOT in the universe: the legacy fallback
	// names it, so a regression reads as this exact string.
	lib := h.g.AddObject(mkCard(t, "Name:Legacy Top\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0).ID
	h.Emit(events.Event{Kind: events.MoveZone, Obj: lib, From: state.ZHand, To: state.ZLibrary, Player: 0})

	Resolve(h, &Ctx{Controller: 0, Source: src, X: 2},
		sa(t, "SP$ NameCard | AtRandom$ True | ValidCards$ Creature.cmcEQX"))

	if h.asked != nil {
		t.Fatalf("AtRandom NameCard asked the player: %+v", h.asked)
	}
	if h.n != 1 {
		t.Fatalf("engine Rand calls = %d, want 1", h.n)
	}
	got := h.g.Obj(src).ChosenName
	if got != "Grizzly Bears" {
		t.Fatalf("ChosenName = %q, want the mv-2 creature Grizzly Bears (legacy fallback would be Legacy Top)", got)
	}
	// Precondition the assertion above depends on: the OTHER creature's mana
	// value differs from X, so an unrestricted lottery could not land here
	// deterministically AND a wrong-branch match would be visible.
	if mv := h.g.NameUniverse.Card(1).Faces[0].Cmc(); mv == 2 {
		t.Fatalf("precondition: Hill Giant mana value = %d, want != 2", mv)
	}
	if ev := chosenNameEvent(h.log); ev != "Grizzly Bears" {
		t.Fatalf("Choose/name event = %q (log=%+v), want Grizzly Bears", ev, h.log)
	}
}

// TestRandomNameCardResolvesSVarBodyFilter pins the second half of the
// two-shape SVar:X reading: a non-`Count$xPaid` SVar:X body is a fixed value
// evaluated through EvalCountOK (Nightmare Unmaking's
// SVar:X:Count$ValidHand Card.YouOwn), so the filter must use the EVALUATED
// value, not the zero paid X.
func TestRandomNameCardResolvesSVarBodyFilter(t *testing.T) {
	h := &askHost{}
	h.g = namecardGameWithMvUniverse(t)
	src := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Sorcery\nOracle:x\n"), 0).ID
	saLine := sa(t, "SP$ NameCard | AtRandom$ True | ValidCards$ Creature.cmcEQX")

	// The evaluated body counts the cards in hand, so a four-card hand makes
	// X resolve to 4 -- Hill Giant's mana value -- while the paid X is zero.
	hand := make([]state.ObjID, 0, 4)
	for i := 0; i < 4; i++ {
		hand = append(hand, h.g.AddObject(mkCard(t, "Name:Pitch\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0).ID)
	}
	h.g.SetZone(state.ZHand, 0, hand)
	body := "Count$ValidHand Card.YouOwn"
	pre := &Ctx{Controller: 0, Source: src, Host: h}
	if n, ok := EvalCountOK(h, pre, body); !ok || n != 4 {
		t.Fatalf("precondition: %s evaluated to %d, %v; want 4, true", body, n, ok)
	}
	_resolve := func(src state.ObjID, body string) string {
		Resolve(h, &Ctx{Controller: 0, Source: src, SVars: map[string]string{"X": body}}, saLine)
		return h.g.Obj(src).ChosenName
	}
	if got := _resolve(src, body); got != "Hill Giant" {
		t.Fatalf("SVar:X body ChosenName = %q, want Hill Giant (the evaluated body value 4)", got)
	}

	// A two-card hand evaluates to 2, selecting the other creature, so the
	// name really came from the body rather than a constant.
	h.g.SetZone(state.ZHand, 0, hand[:2])
	src3 := h.g.AddObject(mkCard(t, "Name:Source3\nTypes:Sorcery\nOracle:x\n"), 0).ID
	if got := _resolve(src3, body); got != "Grizzly Bears" {
		t.Fatalf("SVar:X body (2-card hand) ChosenName = %q, want Grizzly Bears", got)
	}
}

// TestRandomNameCardResolvesRuntimePublishedFilter pins Ornate Imitations'
// shape: `Creature.cmcEQM` where M is published per-repeat by a
// `DB$ StoreSVar | SVar$ M | Type$ CountSVar | Expression$ M/Plus.1` chain and
// stored on the source object's RuntimeSVars. The resolver reads it through
// runtimePublished, so the filter must too.
func TestRandomNameCardResolvesRuntimePublishedFilter(t *testing.T) {
	h := &askHost{}
	h.g = namecardGameWithMvUniverse(t)
	src := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Sorcery\nOracle:x\n"), 0).ID
	// The StoreSVar publication, through the real event fold.
	h.Emit(events.Event{Kind: events.StoreSVar, Obj: src, Text: "M", Amount: 4})
	if v, ok := h.g.Obj(src).RuntimeSVars["M"]; !ok || v != 4 {
		t.Fatalf("precondition: RuntimeSVars[M] = %d, %v; want 4, true", v, ok)
	}

	// Ornate Imitations' chain runs inside a resolving walk that also carries
	// the printed SVar table the loop publishes M from; that table is what
	// installs the numeric-RHS resolver, and M itself comes from the runtime
	// publication above.
	Resolve(h, &Ctx{Controller: 0, Source: src, SVars: map[string]string{"Repeat": "Count$Compare Y GT 0"}},
		sa(t, "SP$ NameCard | AtRandom$ True | ValidCards$ Creature.cmcEQM"))

	if got := h.g.Obj(src).ChosenName; got != "Hill Giant" {
		t.Fatalf("RuntimeSVars M ChosenName = %q, want Hill Giant (mv 4)", got)
	}
	if ev := chosenNameEvent(h.log); ev != "Hill Giant" {
		t.Fatalf("Choose/name event = %q (log=%+v), want Hill Giant", ev, h.log)
	}
}

// TestRandomNameCardDistinctResolvedValuesDoNotShareMemo pins the cache key's
// missing dimension: nameFilterKey holds the spec STRING, not the resolved
// value, so a memo that admitted resolver-bound specs would serve the mv-2
// list for a resolution whose X is 4 (and vice versa). Two resolutions of the
// same spec in one process must produce two different correct sets.
func TestRandomNameCardDistinctResolvedValuesDoNotShareMemo(t *testing.T) {
	h := &askHost{}
	h.g = namecardGameWithMvUniverse(t)
	saLine := sa(t, "SP$ NameCard | AtRandom$ True | ValidCards$ Creature.cmcEQX")

	src2 := h.g.AddObject(mkCard(t, "Name:Source2\nTypes:Sorcery\nOracle:x\n"), 0).ID
	Resolve(h, &Ctx{Controller: 0, Source: src2, X: 2}, saLine)
	if got := h.g.Obj(src2).ChosenName; got != "Grizzly Bears" {
		t.Fatalf("X=2 ChosenName = %q, want Grizzly Bears", got)
	}

	src4 := h.g.AddObject(mkCard(t, "Name:Source4\nTypes:Sorcery\nOracle:x\n"), 0).ID
	Resolve(h, &Ctx{Controller: 0, Source: src4, X: 4}, saLine)
	if got := h.g.Obj(src4).ChosenName; got != "Hill Giant" {
		t.Fatalf("X=4 ChosenName = %q, want Hill Giant: a resolver-bound spec shared the X=2 memo", got)
	}
	// Precondition: the two answers really differ, so a shared memo cannot
	// have produced both.
	if h.g.Obj(src2).ChosenName == h.g.Obj(src4).ChosenName {
		t.Fatal("precondition: both resolutions chose the same name")
	}
}
