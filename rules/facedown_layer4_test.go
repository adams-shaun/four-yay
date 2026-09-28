package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// Face-down permanents and the layer-4 type walk (CR 708.5 + CR 613.1c): a
// face-down battlefield permanent's type set is the CR 708.5 base
// ({Creature}, or a folded FaceDownSetType$), and layer-4 effects modify that
// base like any other type set. typeCharacteristics used to return the base
// BEFORE the layer-4 walk, so a Maskwood Nexus (Affected$ Creature.YouCtrl |
// AddAllCreatureTypes$ True) never reached a manifested or cloaked 2/2 -- the
// code contradicted its own comment. These tests pin that the walk now runs
// while the printed face stays hidden behind CR 708.5.
//
// The corpus cards are REAL compiled cards and the Goblin/Land lord statics
// are inline fixtures (no Forge script text is committed). No repo deck
// carries a manifest/cloak carrier or a Maskwood/manland carrier, so these
// tests do not move the golden heads.

// creatureFixture is an inline vanilla creature for a Reality Shift target.
const creatureFixture = "Name:Layer4 Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// landLordFixture is an inline layer-4 "all creature types" static scoped to
// Lands: it proves the walk reaches a face-down FaceDownSetType$ base that is
// not a creature (Yedora's Forest).
const landLordFixture = "Name:Land Lord\nTypes:Enchantment\n" +
	"S:Mode$ Continuous | Affected$ Land | AddAllCreatureTypes$ True\nOracle:x\n"

// facedownLayer4Engine builds a replay-safe two-seat game (every card sits in
// a Config deck, so a log-only replay reconstructs it) with seat 0 the
// protagonist. Real corpus cards and inline fixtures are both accepted; the
// rest of each deck is Forest.
func facedownLayer4Engine(t *testing.T, reg *cards.Registry, seat0, seat1 []*cards.Card) (*Engine, Config) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	fill := func(d []*cards.Card) []*cards.Card {
		out := append([]*cards.Card(nil), d...)
		for len(out) < 40 {
			out = append(out, forest)
		}
		return out
	}
	cfg := seatZeroStart(Config{Seed: 7701, Names: []string{"fd", "opp"},
		Decks: [][]*cards.Card{fill(seat0), fill(seat1)}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// TestMaskwoodNexusGrantsEveryCreatureTypeToAFaceDownCreature is the ticket's
// carrier end to end. Seat 0 controls a Maskwood Nexus and (after Reality
// Shift exiles seat 0's own creature) a face-down manifested Forest. The
// manifested 2/2 must derive every creature type through the layer-4 walk --
// including through a later Goblin lord's Affected$ -- while its printed
// Forest/Land types and its colours stay hidden (CR 708.5).
//
// The triage premap said the manifested card ends up controlled by seat 0
// "via castAtOpponent" against seat 1's creature; measured, that shape lands
// the manifest under seat 1 (Reality Shift's DefinedPlayer$ TargetedController
// is the exiled creature's controller). Targeting seat 0's OWN creature is
// the same defect with the controller matching the brief's intent, and the
// separate scoping control below covers the seat-1 shape.
func TestMaskwoodNexusGrantsEveryCreatureTypeToAFaceDownCreature(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := facedownLayer4Engine(t, reg,
		[]*cards.Card{
			lookup(t, reg, "Reality Shift"),
			lookup(t, reg, "Maskwood Nexus"),
			lookup(t, reg, "Grizzly Bears"),
			card(t, goblinLordFixture),
		},
		nil)
	moveByName(t, e, 0, "Maskwood Nexus", state.ZBattlefield)
	moveByName(t, e, 0, "Goblin Lord", state.ZBattlefield)
	target := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	// Seat 0's library is almost all Forests; the manifest takes the top card.
	top := e.G.Zone(state.ZLibrary, 0)[0]
	if name := e.G.Obj(top).Face().Name; name != "Forest" {
		t.Fatalf("fixture wants a Forest on top of seat 0's library, got %q", name)
	}

	castAtOpponent(t, e, "Reality Shift", target, "UU")
	base := e.G.Obj(top)
	if base.Zone != state.ZBattlefield || !base.FaceDown || base.Controller != 0 {
		t.Fatalf("manifested Forest: zone=%s FaceDown=%v Controller=%d, want battlefield/true/0",
			base.Zone, base.FaceDown, base.Controller)
	}

	d := e.Derived(top)
	// The layer-4 walk reaches the face-down base: every creature type.
	for _, want := range arbitrarySubtypes {
		if !slices.Contains(d.Types, want) {
			t.Fatalf("face-down creature's derived types missing %q: %v", want, d.Types)
		}
	}
	// The vocabulary is the positive creature-subtype set; no non-creature
	// word can leak onto it.
	for _, bad := range nonCreatureTypeWords {
		if slices.Contains(d.Types, bad) {
			t.Fatalf("face-down creature's derived types carry non-creature word %q", bad)
		}
	}
	// The Goblin lord's layer-7 Affected$ sees the granted subtype through
	// ExtraTypes, so its pump lands: 2/2 + 1/1.
	if d.Power != 3 || d.Toughness != 3 {
		t.Fatalf("face-down creature through a Goblin lord = %d/%d, want 3/3", d.Power, d.Toughness)
	}
	// CR 708.5: the printed face stays hidden. A type GRANT is a layer-4
	// continuous effect, not the printed characteristics, and a manifested
	// Forest's colours are empty.
	if slices.Contains(d.Types, "Forest") || slices.Contains(d.Types, "Land") {
		t.Fatalf("face-down creature's derived types leaked the printed face: %v", d.Types)
	}
	if d.Colors != "" {
		t.Fatalf("face-down creature colours = %q, want colourless", d.Colors)
	}
	if !e.IsCreature(top) {
		t.Fatal("manifested Forest is not a creature")
	}
	noUnimplementedManifest(t, e)
	replayCheck(t, e, cfg)
}

// TestMaskwoodDoesNotGrantToAnOpponentsFaceDownCreature is the scoping
// control: the same manifested 2/2, but controlled by seat 1 (Reality Shift
// cast at seat 1's own creature), is NOT reached by seat 0's Maskwood Nexus
// (Affected$ Creature.YouCtrl). The grant is real and controller-scoped, so
// the positive test above is not "face-down objects get every type anyway".
func TestMaskwoodDoesNotGrantToAnOpponentsFaceDownCreature(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := facedownLayer4Engine(t, reg,
		[]*cards.Card{lookup(t, reg, "Reality Shift"), lookup(t, reg, "Maskwood Nexus")},
		[]*cards.Card{lookup(t, reg, "Grizzly Bears")})
	moveByName(t, e, 0, "Maskwood Nexus", state.ZBattlefield)
	bear := moveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	top := e.G.Zone(state.ZLibrary, 1)[0]

	castAtOpponent(t, e, "Reality Shift", bear, "UU")
	o := e.G.Obj(top)
	if o.Zone != state.ZBattlefield || !o.FaceDown || o.Controller != 1 {
		t.Fatalf("manifested card: zone=%s FaceDown=%v Controller=%d, want battlefield/true/1",
			o.Zone, o.FaceDown, o.Controller)
	}
	d := e.Derived(top)
	if slices.Contains(d.Types, "Goblin") {
		t.Fatalf("seat 0's Maskwood Nexus granted types to seat 1's face-down creature: %v", d.Types)
	}
	if len(d.Types) != 1 || d.Types[0] != "Creature" {
		t.Fatalf("opponent's face-down creature types = %v, want exactly [Creature]", d.Types)
	}
	replayCheck(t, e, cfg)
}

// TestFaceDownSetTypeBaseReceivesTheLayer4Walk pins the Yedora shape: a
// face-down `Land & Forest` set-type object (CR 708.5's FaceDownSetType$
// replacement) keeps its folded base words AND admits the layer-4 walk. The
// inline Land-scoped all-creature-types static reaches it, so the derived
// list carries the base [Land Forest] plus every creature subtype.
func TestFaceDownSetTypeBaseReceivesTheLayer4Walk(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := facedownLayer4Engine(t, reg,
		[]*cards.Card{
			lookup(t, reg, "Yedora, Grave Gardener"),
			lookup(t, reg, "Llanowar Elves"),
			card(t, landLordFixture),
		},
		nil)
	searchMoveByName(t, e, "Yedora, Grave Gardener", state.ZBattlefield)
	elves := searchMoveByName(t, e, "Llanowar Elves", state.ZBattlefield)
	// No layer-4 static in play yet: the base is exactly the folded set type.
	killCreature(t, e, elves, 2)
	if got := e.Derived(elves).Types; len(got) != 2 || got[0] != "Land" || got[1] != "Forest" {
		t.Fatalf("face-down Forest types before the grant = %v, want [Land Forest]", got)
	}
	// Now add the Land-scoped layer-4 grant: the walk must reach the folded
	// base while keeping both base words.
	moveByName(t, e, 0, "Land Lord", state.ZBattlefield)
	d := e.Derived(elves)
	if !slices.Contains(d.Types, "Land") || !slices.Contains(d.Types, "Forest") {
		t.Fatalf("face-down Forest lost its folded base words: %v", d.Types)
	}
	for _, want := range arbitrarySubtypes {
		if !slices.Contains(d.Types, want) {
			t.Fatalf("face-down Forest's derived types missing %q (layer-4 walk skipped): %v", want, d.Types)
		}
	}
	replayCheck(t, e, cfg)
}

// TestFaceDownNoLayer4StaticStillDerivesTheBase is the no-layer-4 control the
// brief names: TestRealityShiftManifestsFaceDown already pins the plain
// [Creature] base, and this asserts the mirrored plain path -- a face-down
// Forest with no layer-4 static -- is untouched by the fix.
func TestFaceDownNoLayer4StaticStillDerivesTheBase(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := facedownLayer4Engine(t, reg,
		[]*cards.Card{lookup(t, reg, "Reality Shift"), lookup(t, reg, "Grizzly Bears")},
		nil)
	target := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	top := e.G.Zone(state.ZLibrary, 0)[0]
	if name := e.G.Obj(top).Face().Name; name != "Forest" {
		t.Fatalf("fixture wants a Forest on top of seat 0's library, got %q", name)
	}
	castAtOpponent(t, e, "Reality Shift", target, "UU")
	if got := e.Derived(top).Types; len(got) != 1 || got[0] != "Creature" {
		t.Fatalf("manifested Forest with no layer-4 static types = %v, want exactly [Creature]", got)
	}
	replayCheck(t, e, cfg)
}
