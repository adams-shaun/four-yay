package rules

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The Animate colour/keyword/type-strip parameters (task
// inbox-paramcensus-animate-colors): Colors$ and OverwriteColors$ are the
// manland family's explicit colour identity (Celestial Colonnade's "white and
// blue", Forbidding Watchtower's "white", Restless Cottage's "black and
// green"), Keywords$ is the animation's keyword grant (Celestial Colonnade's
// flying and vigilance), and RemoveCreatureTypes$ True strips the base face's
// creature-type subtypes before the animation's own Types$ land (Mishra's
// Factory, and Figure of Destiny whose Kithkin base must not stack under the
// animation). All tests run on REAL compiled corpus cards; no Forge script
// text is committed here.

// animateManlandCases drives one Animate activation per card from a real
// corpus script and asserts the resulting layer-derived characteristics.
var animateManlandCases = []struct {
	card       string
	mana       string // pool symbols addMana funds for the animation cost
	colors     string // want Derived.Colors ("" = colourless)
	keywords   []string
	extraTypes []string // creature types the animation adds
	pt         [2]int32
}{
	{"Celestial Colonnade", "WWWWUU", "WU", []string{"Flying", "Vigilance"},
		[]string{"Elemental"}, [2]int32{4, 4}},
	{"Forbidding Watchtower", "WW", "W", nil,
		[]string{"Soldier"}, [2]int32{1, 5}},
	{"Restless Cottage", "BBGG", "BG", nil,
		[]string{"Horror"}, [2]int32{4, 4}},
	{"Mishra's Factory", "C", "", nil,
		[]string{"Assembly-Worker"}, [2]int32{2, 2}},
}

func TestAnimateManlandsCarryTheirGrantedColoursKeywordsAndTypes(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, tc := range animateManlandCases {
		t.Run(tc.card, func(t *testing.T) {
			e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, tc.card)}, []*cards.Card{})
			id := moveByName(t, e, 0, tc.card, state.ZBattlefield)
			addMana(t, e, 0, tc.mana)
			submitChoices(t, e, animateAbilityOption(t, e, id).Index)
			settleActivation(t, e)

			d := e.Derived(id)
			if d.Colors != tc.colors {
				t.Fatalf("%s animated colours = %q, want %q", tc.card, d.Colors, tc.colors)
			}
			if got := e.Colors(id); got != tc.colors {
				t.Fatalf("%s Engine.Colors = %q, want %q", tc.card, got, tc.colors)
			}
			for _, kw := range tc.keywords {
				if !e.HasKeyword(id, kw) {
					t.Fatalf("%s animated keywords %v missing %q", tc.card, d.Keywords, kw)
				}
			}
			for _, want := range append(slices.Clone(tc.extraTypes), "Creature", "Land") {
				if !slices.Contains(d.Types, want) {
					t.Fatalf("%s animated types %v missing %q", tc.card, d.Types, want)
				}
			}
			if !e.IsCreature(id) {
				t.Fatalf("%s animated is not a creature", tc.card)
			}
			if d.Power != tc.pt[0] || d.Toughness != tc.pt[1] {
				t.Fatalf("%s animated P/T = %d/%d, want %d/%d", tc.card, d.Power, d.Toughness, tc.pt[0], tc.pt[1])
			}
		})
	}
}

// TestAnimateStripsBaseCreatureTypes pins RemoveCreatureTypes$ True against a
// base face that CARRIES a creature type: Figure of Destiny (Creature Kithkin)
// animated into a Kithkin Spirit keeps the Creature card type, loses the
// printed Kithkin subtype to the strip, and gains the animation's Kithkin and
// Spirit from its Types$ -- so the printed subtype does not stack under the
// animation, and comes back when the animation expires.
func TestAnimateStripsBaseCreatureTypes(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Figure of Destiny")}, []*cards.Card{})
	id := moveByName(t, e, 0, "Figure of Destiny", state.ZBattlefield)
	addMana(t, e, 0, "RRWW")
	submitChoices(t, e, animateAbilityOption(t, e, id).Index)
	settleActivation(t, e)

	d := e.Derived(id)
	want := []string{"Creature", "Kithkin", "Spirit"}
	if !slices.Equal(d.Types, want) {
		t.Fatalf("animated Figure of Destiny types = %v, want %v (printed Kithkin stripped, animation's added)", d.Types, want)
	}
	if d.Power != 2 || d.Toughness != 2 {
		t.Fatalf("animated Figure of Destiny P/T = %d/%d, want 2/2", d.Power, d.Toughness)
	}
}

// TestDerivedColorsLayerCompose pins the layer-5 composition itself (beyond
// any one card): a plain AddColors effect extends the face's colours in
// timestamp order, an OverwriteColors effect replaces everything before it
// (including an overwrite to colourless -- the Colors$ Colorless shape), and
// a later add extends the overwritten set. WUBRG order is the output order.
func TestDerivedColorsLayerCompose(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Grizzly Bears")}, []*cards.Card{})
	id := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	if got := e.Colors(id); got != "G" {
		t.Fatalf("Grizzly Bears colours = %q, want \"G\"", got)
	}
	e.AddContinuous(state.ContinuousEffect{Source: id, Affects: "Card.Self", Layer: state.LColor, AddColors: []string{"U"}})
	if got := e.Colors(id); got != "UG" {
		t.Fatalf("after add U: %q, want \"UG\" (WUBRG order)", got)
	}
	e.AddContinuous(state.ContinuousEffect{Source: id, Affects: "Card.Self", Layer: state.LColor, AddColors: []string{"B", "W"}, OverwriteColors: true})
	if got := e.Colors(id); got != "WB" {
		t.Fatalf("after overwrite WB: %q, want \"WB\" (face G and add U replaced)", got)
	}
	e.AddContinuous(state.ContinuousEffect{Source: id, Affects: "Card.Self", Layer: state.LColor, AddColors: []string{"R"}})
	if got := e.Colors(id); got != "WBR" {
		t.Fatalf("after add R: %q, want \"WBR\"", got)
	}
	e.AddContinuous(state.ContinuousEffect{Source: id, Affects: "Card.Self", Layer: state.LColor, OverwriteColors: true})
	if got := e.Colors(id); got != "" {
		t.Fatalf("after overwrite to colourless: %q, want \"\"", got)
	}
}

// TestRemoveCreatureTypesStripsOnlySubtypes pins the strip's boundary on a
// direct layer effect: card types (Creature, Artifact, Land) and supertypes
// (Legendary, Snow) survive, subtypes go, and the animation's own AddTypes
// land after the strip.
func TestRemoveCreatureTypesStripsOnlySubtypes(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Boggart Ram-Gang")}, []*cards.Card{})
	id := moveByName(t, e, 0, "Boggart Ram-Gang", state.ZBattlefield)
	e.AddContinuous(state.ContinuousEffect{Source: id, Affects: "Card.Self", Layer: state.LType,
		RemoveCreatureTypes: true, AddTypes: []string{"Golem"}})
	d := e.Derived(id)
	if !slices.Equal(d.Types, []string{"Creature", "Golem"}) {
		t.Fatalf("types after strip+add = %v, want [Creature Golem] (goblin subtypes stripped)", d.Types)
	}
}

// settleActivation drains an ability activation to resolution: a hybrid-pip
// cost (Figure of Destiny's {R/W}) poses a KChoose payment ask AFTER the
// activation is submitted and BEFORE the ability reaches the stack, so the
// driver answers any such ask with its first option and only then passes
// both seats -- the same shape passUntilStackEmpty cannot see, because an
// un-paid activation leaves the stack empty.
func settleActivation(t *testing.T, e *Engine) {
	t.Helper()
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose && len(d.Options) > 0 {
		submitChoices(t, e, d.Options[0].Index)
	}
	passUntilStackEmpty(t, e, 30)
}

// animateAbilityOption finds the AB$ Animate activated-ability option for id
// in the pending priority decision (fatal when absent -- a manland whose
// animation is not offered is a corpus/legality regression, not a test bug).
func animateAbilityOption(t *testing.T, e *Engine, id state.ObjID) decision.Option {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		t.Fatalf("animated fixture %d has no face", id)
	}
	idx := -1
	for i, sa := range o.Face().Abilities {
		if sa.Kind == "AB" && sa.API == "Animate" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("%s carries no AB$ Animate ability", o.Face().Name)
	}
	return abilityOption(t, e, id, idx)
}

// TestAnimateColorlessWithoutOverwriteKeepsColoursAndNotes pins the
// raging_spirit shape end to end on the real corpus card: Colors$ Colorless
// WITHOUT OverwriteColors$ is an add of the empty set -- a no-op this build
// does not implement (the corpus line means "becomes colorless") -- so the
// activation resolves, emits the unimplemented Note, and leaves the printed
// colours alone. Before the fail-closed round this was a silent no-op.
func TestAnimateColorlessWithoutOverwriteKeepsColoursAndNotes(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Raging Spirit")}, []*cards.Card{})
	id := moveByName(t, e, 0, "Raging Spirit", state.ZBattlefield)
	addMana(t, e, 0, "CC")
	submitChoices(t, e, animateAbilityOption(t, e, id).Index)
	settleActivation(t, e)

	if got := e.Colors(id); got != "R" {
		t.Fatalf("Raging Spirit colours after its Colors$-Colorless animation = %q, want \"R\" (printed colours kept)", got)
	}
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "Colorless without OverwriteColors$") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no Colorless-without-OverwriteColors Note in the log")
	}
}

// TestDerivedColorsSkipsMalformedColourElements pins the layer-5 walk's
// robustness leaf from review round 2: state.ContinuousEffect is exported,
// so a malformed AddColors element (empty, or not a WUBRG letter) must be
// skipped -- never an index panic. Valid elements in the same list still
// land.
func TestDerivedColorsSkipsMalformedColourElements(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Grizzly Bears")}, []*cards.Card{})
	id := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	e.AddContinuous(state.ContinuousEffect{Source: id, Affects: "Card.Self", Layer: state.LColor,
		AddColors: []string{"", "X", "U"}})
	if got := e.Colors(id); got != "UG" {
		t.Fatalf("colours with malformed elements = %q, want \"UG\" (invalid elements skipped, U landed)", got)
	}
}

// TestSetColorStaticAmpersandListMakesWitnessProtectionBearerGreenWhite pins
// the second list separator the corpus uses. Witness Protection carries
// `SetColor$ Green & White` -- a " & " list, the same grammar the SAME
// static line's `AddType$ Creature & Citizen` uses and rules' statList
// already splits -- so the enchanted creature is green AND white, not the
// printed blue. Before the shared parser learned " & ", "Green & White" was
// one unrecognised word, the read failed closed, and the bearer kept its
// printed colour (the review's MAJOR finding).
func TestSetColorStaticAmpersandListMakesWitnessProtectionBearerGreenWhite(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	witness := mustCorpusCard(t, reg, "Witness Protection")
	elemental := mustCorpusCard(t, reg, "Air Elemental") // a blue creature
	e := corpusEngine(t, reg, []*cards.Card{witness, elemental}, []*cards.Card{})

	bearer := moveByName(t, e, 0, "Air Elemental", state.ZBattlefield)
	if got := e.Colors(bearer); got != "U" {
		t.Fatalf("Air Elemental colours before Witness Protection = %q, want \"U\"", got)
	}
	attachCorpusAura(t, e, 0, witness, bearer)

	if got := e.Colors(bearer); got != "WG" {
		t.Fatalf("enchanted permanent colours under SetColor$ Green & White = %q, want \"WG\"", got)
	}
}

// TestSetColorStaticMakesImprisonedBearerColourless is the brief's card pin:
// Imprisoned in the Moon's `SetColor$ Colorless` static is a layer-5 colour
// SET, so the enchanted permanent's derived colours become the empty set (it
// is colourless), and the change is visible to a real colour-based rules
// path -- protection's sourceHasQuality reads the DERIVED colours through
// e.objColors, so a black creature stops counting as black for a bearer with
// "Protection from black". Before the SetColor$ read the enchanted creature
// kept its printed black and White Knight's protection still applied.
func TestSetColorStaticMakesImprisonedBearerColourless(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	imprisoned := mustCorpusCard(t, reg, "Imprisoned in the Moon")
	specter := mustCorpusCard(t, reg, "Hypnotic Specter") // a black creature
	knight := mustCorpusCard(t, reg, "White Knight")      // Protection from black
	e := corpusEngine(t, reg, []*cards.Card{imprisoned, specter, knight}, []*cards.Card{})

	bearer := moveByName(t, e, 0, "Hypnotic Specter", state.ZBattlefield)
	wKnight := moveByName(t, e, 0, "White Knight", state.ZBattlefield)
	if got := e.Colors(bearer); got != "B" {
		t.Fatalf("Hypnotic Specter colours before Imprisoned = %q, want \"B\"", got)
	}
	if !e.protectedFrom(wKnight, bearer) {
		t.Fatal("White Knight must be protected from the black Hypnotic Specter before Imprisoned")
	}

	attachCorpusAura(t, e, 0, imprisoned, bearer)

	if got := e.Colors(bearer); got != "" {
		t.Fatalf("enchanted permanent colours after Imprisoned SetColor$ Colorless = %q, want \"\" (colourless)", got)
	}
	if e.protectedFrom(wKnight, bearer) {
		t.Fatal("White Knight must stop being protected from the now-colourless enchanted permanent")
	}
	// The layer-4 half of the same static still applies alongside the colour
	// set: the permanent is a Land and has lost its card types.
	if d := e.Derived(bearer); !slices.Contains(d.Types, "Land") {
		t.Fatalf("enchanted permanent types = %v, want a Land among them", d.Types)
	}
}

// TestSetColorAllStaticMakesLeylinePermanentsAllColours pins the other
// vocabulary arm on a real corpus card: Leyline of the Guildpact's
// `SetColor$ All` makes each nonland permanent its controller owns all five
// colours (WUBRG), not just its printed ones.
func TestSetColorAllStaticMakesLeylinePermanentsAllColours(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	leyline := mustCorpusCard(t, reg, "Leyline of the Guildpact")
	bears := mustCorpusCard(t, reg, "Grizzly Bears")
	e := corpusEngine(t, reg, []*cards.Card{leyline, bears}, []*cards.Card{})

	moveByName(t, e, 0, "Leyline of the Guildpact", state.ZBattlefield)
	id := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	if got := e.Colors(id); got != "WUBRG" {
		t.Fatalf("Grizzly Bears under Leyline of the Guildpact = %q, want \"WUBRG\"", got)
	}
}

// TestAddColorStaticExtendsColours pins the sibling parameter to SetColor$:
// Angelic Armaments' `AddColor$ White` is a layer-5 colour ADD ("in
// addition to its other colors"), so a green Grizzly Bears becomes green and
// white rather than white alone. Before the AddColor$ read the equipment's
// pump and keyword landed but the colour did nothing.
func TestAddColorStaticExtendsColours(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	armaments := mustCorpusCard(t, reg, "Angelic Armaments")
	bears := mustCorpusCard(t, reg, "Grizzly Bears")
	e := corpusEngine(t, reg, []*cards.Card{armaments, bears}, []*cards.Card{})

	bearer := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	if got := e.Colors(bearer); got != "G" {
		t.Fatalf("Grizzly Bears colours before Armaments = %q, want \"G\"", got)
	}
	attachCorpusAura(t, e, 0, armaments, bearer)
	if got := e.Colors(bearer); got != "WG" {
		t.Fatalf("Grizzly Bears under AddColor$ White = %q, want \"WG\" (white added, green kept)", got)
	}
}

// TestSetColorChosenColorStaticFailsClosed pins the unparseable arm: a
// `SetColor$ ChosenColor` static (Alloy Golem, Faceless One, Clara Oswald --
// they ask their controller for a colour before the game) must fail closed,
// leaving the affected object's printed colours alone rather than
// overwriting them with the parser's empty prefix. Authored inline so it
// does not depend on a commander-pregame choice this build does not model.
func TestSetColorChosenColorStaticFailsClosed(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const src = "Name:Chosen Hue Bearer\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\n" +
		"S:Mode$ Continuous | Affected$ Card.Self | SetColor$ ChosenColor | Description$ CARDNAME is the chosen color.\n" +
		"Oracle:x\n"
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Grizzly Bears")}, []*cards.Card{})
	id := putToken(t, e, 0, src, state.ZBattlefield)
	if got := e.Colors(id); got != "G" {
		t.Fatalf("bearer under SetColor$ ChosenColor = %q, want \"G\" (printed colours kept, fail closed)", got)
	}
}
