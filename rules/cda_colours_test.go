package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The characteristic-defining SetColor$ colour read in EVERY zone (task
// cda-colours): CR 604.3/208.2 puts a CDA in every zone, so Transguild
// Courier's "CARDNAME is all colors" makes the card WUBRG in hand, on the
// stack, in the graveyard and on the battlefield, and Ghostfire's "CARDNAME
// is colorless" makes it colourless everywhere -- the printed {2}{R} never
// shows through. The read lives in effects.ColorMaskOf's CDA overwrite arm,
// which every off-battlefield colour consumer (objColors' fallback, the
// filter grammar's colour predicates, the Count$...$Colors heads) routes
// through, while rules' staticEffects withholds the resolvable self-CDA from
// its layer-5 scan emission so the battlefield read applies it exactly once.
// All tests run on REAL compiled corpus cards; no Forge script text is
// committed here except the synthetic ChosenColor bearer, which is authored
// inline the way the existing TestSetColorChosenColorStaticFailsClosed does.

// cdaMoveTo finds seat p's card named name in its hand or library and moves
// it to to with a logged MoveZone (no event when it is already there -- the
// Genesis opening hand may have dealt it already). Returns the object id.
func cdaMoveTo(t *testing.T, e *Engine, p state.PlayerID, name string, to state.Zone) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, p) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				if z != to {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
					e.pending = nil
				}
				return id
			}
		}
	}
	t.Fatalf("card %q not found in seat %d's hand/library", name, p)
	return 0
}

// TestTransguildCourierIsAllColoursInEveryZone is the filed report made
// executable: the courier's CDA SetColor$ All is read in every zone, so it
// is WUBRG at hand, on the stack, in the graveyard AND on the battlefield --
// before the fix the off-battlefield reads saw only the printed colours and
// read "" (a colourless artifact with no colour indicator), turning
// five-coloured only when it entered.
func TestTransguildCourierIsAllColoursInEveryZone(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Transguild Courier")}, []*cards.Card{})

	id := cdaMoveTo(t, e, 0, "Transguild Courier", state.ZHand)
	if got := e.objColors(e.G.Obj(id)); got != "WUBRG" {
		t.Fatalf("Transguild Courier in hand colours = %q, want \"WUBRG\"", got)
	}
	// The filter grammar reads the same base: a White predicate matches the
	// courier in hand, and it is not monocolored (five colours).
	if !effects.MatchesSpec(e.G, "Card.White", id, 0) {
		t.Fatal("Transguild Courier in hand does not match a White predicate")
	}
	if effects.MatchesSpec(e.G, "Card.MonoColor", id, 0) {
		t.Fatal("Transguild Courier in hand must not match MonoColor")
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZStack})
	e.pending = nil
	if got := e.objColors(e.G.Obj(id)); got != "WUBRG" {
		t.Fatalf("Transguild Courier on the stack colours = %q, want \"WUBRG\"", got)
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack, To: state.ZGraveyard})
	e.pending = nil
	if got := e.objColors(e.G.Obj(id)); got != "WUBRG" {
		t.Fatalf("Transguild Courier in the graveyard colours = %q, want \"WUBRG\"", got)
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZGraveyard, To: state.ZBattlefield})
	e.pending = nil
	if got := e.Colors(id); got != "WUBRG" {
		t.Fatalf("Transguild Courier on the battlefield colours = %q, want \"WUBRG\"", got)
	}
	if got := e.objColors(e.G.Obj(id)); got != "WUBRG" {
		t.Fatalf("Transguild Courier on the battlefield objColors = %q, want \"WUBRG\"", got)
	}
}

// TestGhostfireIsColourlessInEveryZone is the same defect in the opposite
// direction: Ghostfire's CDA is SetColor$ Colorless, so the printed {2}{R}
// never shows -- before the fix an off-battlefield Ghostfire read "R", a red
// spell CR says is colourless (uncountereable by "counter target red spell",
// unblockable-vs-protection-from-red). The battlefield read is unchanged
// ("" -- the scan-emitted CDA already overwrote it there; the withheld scan
// now leaves the base claim as the single application).
func TestGhostfireIsColourlessInEveryZone(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Ghostfire")}, []*cards.Card{})

	id := cdaMoveTo(t, e, 0, "Ghostfire", state.ZHand)
	if got := e.objColors(e.G.Obj(id)); got != "" {
		t.Fatalf("Ghostfire in hand colours = %q, want \"\" (colourless CDA)", got)
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZStack})
	e.pending = nil
	if got := e.objColors(e.G.Obj(id)); got != "" {
		t.Fatalf("Ghostfire on the stack colours = %q, want \"\" (colourless CDA)", got)
	}
	if effects.MatchesSpec(e.G, "Card.Red", id, 0) {
		t.Fatal("Ghostfire on the stack must not match a Red predicate")
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack, To: state.ZGraveyard})
	e.pending = nil
	if got := e.objColors(e.G.Obj(id)); got != "" {
		t.Fatalf("Ghostfire in the graveyard colours = %q, want \"\" (colourless CDA)", got)
	}

	// The battlefield base read is zone-independent: the same claim applies
	// there (a raw MoveZone is the only way an instant gets there, and the
	// read itself never cared).
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZGraveyard, To: state.ZBattlefield})
	e.pending = nil
	if got := e.Colors(id); got != "" {
		t.Fatalf("Ghostfire on the battlefield colours = %q, want \"\" (unchanged)", got)
	}
}

// TestSphinxOfTheGuildpactHexproofGateReadsTheFullSet pins the second
// resolvable All carrier and its one real rules-path consumer: while the
// Sphinx is a permanent its hexproof keyword is Hexproof:Card.MonoColor, and
// the gate's quality read is the SHARED colour read. The Sphinx itself (all
// five colours) is not monocolored; an opponent's Shock on the stack (one
// colour) IS blocked by the gate exactly as before; and an opponent's
// Ghostfire on the stack is NOT -- Ghostfire is colourless, not monocolored,
// which the pre-fix "R" off-battlefield read got wrong.
func TestSphinxOfTheGuildpactHexproofGateReadsTheFullSet(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Sphinx of the Guildpact"), lookup(t, reg, "Shock"), lookup(t, reg, "Ghostfire")},
		[]*cards.Card{})

	sphinx := cdaMoveTo(t, e, 0, "Sphinx of the Guildpact", state.ZBattlefield)
	if got := e.Colors(sphinx); got != "WUBRG" {
		t.Fatalf("Sphinx of the Guildpact permanent colours = %q, want \"WUBRG\"", got)
	}
	if !slices.ContainsFunc(e.Keywords(sphinx), func(kw string) bool {
		return len(kw) >= len("Hexproof:Card.MonoColor") && kw[:len("Hexproof:Card.MonoColor")] == "Hexproof:Card.MonoColor"
	}) {
		t.Fatalf("Sphinx derived keywords %v missing a Hexproof:Card.MonoColor form", e.Keywords(sphinx))
	}
	if e.sourceHasQuality(sphinx, "Card.MonoColor") {
		t.Fatal("the all-colours Sphinx must not read as monocolored to its own hexproof vocabulary")
	}

	shock := cdaMoveTo(t, e, 0, "Shock", state.ZStack)
	if !e.hexproofBlocksTarget(sphinx, 1, shock) {
		t.Fatal("an opponent's monocolored Shock on the stack must still be blocked by Hexproof from monocolored")
	}
	ghostfire := cdaMoveTo(t, e, 0, "Ghostfire", state.ZStack)
	if e.hexproofBlocksTarget(sphinx, 1, ghostfire) {
		t.Fatal("an opponent's Ghostfire (colourless, not monocolored) must NOT be blocked")
	}
}

// TestFacelessOneChosenColorFailsClosedOffBattlefield pins the fail-closed
// arm: Faceless One's CDA is SetColor$ ChosenColor (a commander pregame
// choice this build does not model), so the helper reports no claim and the
// card keeps its printed colours wherever it is. Its printed cost is {5} --
// colourless -- so the off-battlefield read stays "". A synthetic bearer with
// a PRINTED colour (the same authored shape
// TestSetColorChosenColorStaticFailsClosed uses, but read off the
// battlefield) shows the fail-closed direction positively: the printed green
// is kept, not overwritten away.
func TestFacelessOneChosenColorFailsClosedOffBattlefield(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	const src = "Name:Chosen Hue Bearer\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\n" +
		"S:Mode$ Continuous | Affected$ Card.Self | CharacteristicDefining$ True | SetColor$ ChosenColor | Description$ CARDNAME is the chosen color.\n" +
		"Oracle:x\n"
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Faceless One"), card(t, src)}, []*cards.Card{})

	faceless := cdaMoveTo(t, e, 0, "Faceless One", state.ZHand)
	if got := e.objColors(e.G.Obj(faceless)); got != "" {
		t.Fatalf("Faceless One in hand colours = %q, want \"\" (printed colours kept, fail closed)", got)
	}

	synthetic := cdaMoveTo(t, e, 0, "Chosen Hue Bearer", state.ZHand)
	if got := e.objColors(e.G.Obj(synthetic)); got != "G" {
		t.Fatalf("SetColor$ ChosenColor bearer in hand colours = %q, want \"G\" (printed colours kept, fail closed)", got)
	}
}

// TestCDAColoursScanWithholdsResolvableSelfCDA pins the disjoint-paths
// discipline: staticEffects must NOT emit a layer-5 OverwriteColors effect
// for a resolvable self-CDA (the base read at derivedWith's layer-5 base
// already applies it, so a scan emission would apply it twice), while a
// NON-characteristic SetColor$ static still emits from the scan and a
// later-timestamp overwrite still wins over the base claim -- Imprisoned in
// the Moon on an enchanted Transguild Courier makes it colourless.
func TestCDAColoursScanWithholdsResolvableSelfCDA(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	imprisoned := lookup(t, reg, "Imprisoned in the Moon")
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Transguild Courier"), imprisoned}, []*cards.Card{})

	courier := cdaMoveTo(t, e, 0, "Transguild Courier", state.ZBattlefield)
	if got := e.Colors(courier); got != "WUBRG" {
		t.Fatalf("Transguild Courier permanent colours = %q, want \"WUBRG\"", got)
	}
	// The scan itself carries no CDA SetColor emission for the courier.
	for _, ce := range e.staticEffects(nil) {
		if ce.Source == courier && ce.Layer == state.LColor {
			t.Fatalf("staticEffects still emits the courier's own CDA SetColor: %+v", ce)
		}
	}

	// A later-timestamp scan-emitted overwrite (the Aura's non-CDA
	// SetColor$ Colorless) still wins: the enchanted courier is colourless.
	attachCorpusAura(t, e, 0, imprisoned, courier)
	if got := e.Colors(courier); got != "" {
		t.Fatalf("Imprisoned-in-the-Moon courier colours = %q, want \"\" (later overwrite wins)", got)
	}
	found := false
	for _, ce := range e.staticEffects(nil) {
		if ce.Source == courier && ce.Layer == state.LColor {
			t.Fatalf("staticEffects still emits the courier's own CDA SetColor after the aura lands: %+v", ce)
		}
		if ce.Layer == state.LColor && ce.OverwriteColors && ce.AddColors == nil {
			if o := e.G.Obj(ce.Source); o != nil && o.Face() != nil && o.Face().Name == "Imprisoned in the Moon" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("staticEffects no longer emits the non-characteristic SetColor$ Colorless from Imprisoned in the Moon")
	}
}
