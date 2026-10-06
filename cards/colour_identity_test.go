package cards

import (
	"path/filepath"
	"testing"
)

// The corpus-driven identity tests are the strong ones here: colour identity
// is a claim that must hold across every one of the ~33,000 cards, and
// hand-written examples alone would not catch a derivation that was right by
// luck on five cards and wrong on a thousand.

// corpusCache hands back the whole-registry registry shared across the package
// (see compiledCorpus in corpus_test.go): the identity checks iterate all
// ~33k cards, and that registry is compiled once for every corpus test, so
// the identity sweep adds only the cheap bitmask assertions on top of a build
// the M0 gate was already paying for — no second full-corpus load.
func corpusCache(t *testing.T) *Registry {
	t.Helper()
	return compiledCorpus(t)
}

// mustLookup resolves a card by name or fails the test: a named trap card
// missing from the deliberate pinning is a fixture error a human must see.
func mustLookup(t *testing.T, r *Registry, name string) *Card {
	t.Helper()
	c, ok := r.Lookup(name)
	if !ok {
		t.Fatalf("registry has no card %q", name)
	}
	return c
}

// TestColourIdentityAcrossTheCorpus runs the whole registry through the
// derivation and pins the two properties that must hold for every card.
//
//   - Every colour in a face's mana cost is in its colour identity (a pip
//     can never fall out of the identity).
//   - Identity is a superset of the card's colour: the card's colour is its
//     colour indicator, or the colours of its casting cost, and a colour
//     identity narrower than that would be impossible.
//
// The third corpus property — C, S, P and numerics never add a colour — is
// structurally guaranteed (manaColours only ever ORs in letters that actually
// are WUBRG), so it is pinned directly in TestManaColoursSpecialSymbols
// rather than re-asserted across the corpus where it could never fail.
func TestColourIdentityAcrossTheCorpus(t *testing.T) {
	r := corpusCache(t)
	for _, c := range r.AllCards() {
		for _, f := range c.Faces {
			id := f.ColourIdentity()
			// cost pips ⊆ identity
			if cost := manaColours(f.ManaCost); cost&^id != 0 {
				t.Fatalf("%s (%s): cost pips %08b not in identity %08b", c.Path, f.Name, cost, id)
			}
			// identity ⊇ colour (colour = indicator, else cost colours)
			colour := colourIndicator(f.Colors)
			if colour == 0 {
				colour = manaColours(f.ManaCost)
			}
			if colour&^id != 0 {
				t.Fatalf("%s (%s): colour %08b not in identity %08b", c.Path, f.Name, colour, id)
			}
		}
	}
}

// TestColourIdentityPinnedTrapCards pins the derivation to five cards whose
// non-obvious identity a hand-written fixture could get wrong deliberately
// and nobody would ever notice from examples written the same way.
func TestColourIdentityPinnedTrapCards(t *testing.T) {
	r := corpusCache(t)

	// Ghalta, Primal Hunger: costs {10}{G}{G}. Its identity is exactly green,
	// not the five colours, not colourless — the generic {10} must add
	// nothing and the two {G} collapse to a single green bit. A parser that
	// treated every cost letter as a colour, or leaned on the card's colour
	// alone, would get this wrong.
	if got := mustLookup(t, r, "Ghalta, Primal Hunger").ColourIdentity(); got != ColourGreen {
		t.Fatalf("Ghalta identity = %08b, want green (%08b)", got, ColourGreen)
	}

	// Sai, Master Thopterist: costs {2}{U} and also has an activated ability
	// "{1}{U}, Sacrifice two artifacts: Draw". The {U} in that ability's cost
	// is rules text and must be in the identity alongside the cost's {U}. A
	// parser that read only the mana cost would still get {U} here, so this
	// pins that ability-text scanning is part of the derivation at all.
	if got := mustLookup(t, r, "Sai, Master Thopterist").ColourIdentity(); got != ColourBlue {
		t.Fatalf("Sai identity = %08b, want blue (%08b)", got, ColourBlue)
	}

	// Ghoulcaller Gisa: costs {3}{B}{B} and its activated ability costs {B},
	// {T}. The brief calls this the trap: at least one {B} must survive from
	// the ability's Cost$ even though the mana cost already has {B}, and the
	// word "Sacrifice" in the cost is not a colour. A parser that only ever
	// read the mana cost would be fine here, so the gate is the ability text.
	if got := mustLookup(t, r, "Ghoulcaller Gisa").ColourIdentity(); got != ColourBlack {
		t.Fatalf("Ghoulcaller Gisa identity = %08b, want black (%08b)", got, ColourBlack)
	}

	// A hybrid card — Bioshift costs the single hybrid symbol {G/U}. Its
	// identity is BOTH green and blue at once, so the derivation must add
	// every colour letter of a hybrid, not pick one.
	if got := mustLookup(t, r, "Bioshift").ColourIdentity(); got != ColourGreen|ColourBlue {
		t.Fatalf("Bioshift identity = %08b, want {G,U} (%08b)", got, ColourGreen|ColourBlue)
	}

	// A Phyrexian card — Gut Shot costs the single Phyrexian symbol {R/P}.
	// Its identity is red: the R counts, the P is not a colour. A parser
	// that treated P as a colour, or dropped the whole symbol, would disagree.
	if got := mustLookup(t, r, "Gut Shot").ColourIdentity(); got != ColourRed {
		t.Fatalf("Gut Shot identity = %08b, want red (%08b)", got, ColourRed)
	}
}

// TestManaColoursSpecialSymbols pins that none of the corpus's non-colour
// mana symbols (and no bare generic) contributes a colour. This is the
// corpus-level "C, S, P and numerics never add a colour" property exercised
// per symbol, where it can actually be isolated.
func TestManaColoursSpecialSymbols(t *testing.T) {
	neutral := map[string]uint8{"C": 0, "S": 0, "P": 0, "X": 0, "2": 0, "1": 0, "10": 0}
	for sym, want := range neutral {
		if got := manaColours(sym); got != want {
			t.Errorf("manaColours(%q) = %08b, want %08b", sym, got, want)
		}
	}
	// Slashed hybrid/Phyrexian forms contribute only their colour letter.
	if got := manaColours("2/B"); got != ColourBlack {
		t.Errorf(`manaColours("2/B") = %08b, want black`, got)
	}
	if got := manaColours("2/R 2/G"); got != ColourRed|ColourGreen {
		t.Errorf(`manaColours("2/R 2/G") = %08b, want {R,G}`, got)
	}
	if got := manaColours("W/U"); got != ColourWhite|ColourBlue {
		t.Errorf(`manaColours("W/U") = %08b, want {W,U}`, got)
	}
	// Forge comparison operators are packed (GT0), not mana costs. Treating
	// T as a mana-token character lets the leading G masquerade as green.
	if got := manaColours("GT0"); got != 0 {
		t.Errorf("manaColours(GT0) = %08b, want empty", got)
	}
}

// TestCardIdentityUnionsFaces pins that a double-faced card's identity is the
// union of both faces, never just the front one. Delver of Secrets is a DFC
// in the repo decks; the union over its faces must stay exactly blue.

// TestCardIdentityUnionsFaces pins that a double-faced card's identity is the
// union of both faces, never just the front one. Delver of Secrets is a DFC
// in the repo decks; the union over its faces must stay exactly blue.
func TestCardIdentityUnionsFaces(t *testing.T) {
	r := corpusCache(t)
	if got := mustLookup(t, r, "Delver of Secrets").ColourIdentity(); got != ColourBlue {
		t.Fatalf("Delver of Secrets identity = %08b, want blue", got)
	}
}

// TestColourIdentitySurvivesGobRoundTrip proves the derived identity is not
// lost to gob encoding and is recomputed on decode: after a fresh Save, a
// face reads the same identity a directly-parsed one does, exactly like the
// other derived fields (derive_test).
func TestColourIdentitySurvivesGobRoundTrip(t *testing.T) {
	direct, _ := ParseBytes("s.txt", []byte(
		"Name:Sample\n"+
			"ManaCost:1 G G\n"+
			"Types:Creature\n"+
			"A:AB$ Mana | Cost$ G T | Produced$ G | SpellDescription$ Add {G}.\n"+
			"Oracle:{G}, {T}: Add {G}.\n"))
	if got := direct.Faces[0].ColourIdentity(); got != ColourGreen {
		t.Fatalf("direct identity = %08b, want green", got)
	}

	r := NewRegistry()
	r.Add(direct)
	dir := t.TempDir()
	p := filepath.Join(dir, "ir.gob.gz")
	if err := r.Save(p); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRegistry(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Card(0).Faces[0].ColourIdentity(); got != ColourGreen {
		t.Fatalf("gob-routed identity = %08b, want green", got)
	}
}

// TestBasicLandsHaveEmptyIdentity checks a deliberate property, not an
// accident of skipping intrinsics: a basic land's colour identity must be
// empty so it fits any commander deck. That is exactly what excluding the
// intrinsic basic-land mana ability produces (a Mountain's "{T}: Add {R}" is
// granted by the land type, not printed rules text, and lives in reminder
// text which is not scanned).
func TestBasicLandsHaveEmptyIdentity(t *testing.T) {
	r := corpusCache(t)
	for _, name := range []string{"Mountain", "Island", "Plains", "Swamp", "Forest", "Wastes"} {
		if id := mustLookup(t, r, name).ColourIdentity(); id != 0 {
			t.Fatalf("%s colour identity = %08b, want empty", name, id)
		}
	}
}

// TestDeriveColourIdentityFromScriptFields is a unit check (no corpus) for
// the derivation's sources: the mana cost, the colour indicator, the
// brace-delimited mana symbols in the Oracle rules text and a
// characteristic-defining SetColor$. The ability Cost$/Produced$ line and the
// trigger's prose must contribute NOTHING by themselves — the same colour
// symbols only count because the Oracle rules text repeats them as printed
// rules text — and prose whose words carry an uppercase colour-initial (which
// is NOT a mana symbol) is never read as a pip, in the Oracle either.
func TestDeriveColourIdentityFromScriptFields(t *testing.T) {
	c, _ := ParseBytes("s.txt", []byte(
		"Name:Complex\n"+
			"ManaCost:2 W\n"+
			"Types:Creature\n"+
			"A:AB$ Mana | Cost$ B T | Produced$ B | SpellDescription$ A White creature being targeted by Swamp words is prose and adds no Blue.\n"+
			"T:Mode$ Tap | TriggerDescription$ Swamp and White are prose words here too.\n"+
			"Oracle:{B}, {T}: Add {B}. A White creature being targeted by Swamp words is prose and adds no Blue.\n"))
	// The ability's Cost$ B is rules text ONLY because the Oracle prints it
	// ("{B}, {T}: Add {B}."); the raw ability/param tokens contribute
	// nothing on their own. Prose colour-initial words add nothing.
	if want := ColourWhite | ColourBlack; c.Faces[0].ColourIdentity() != want {
		t.Fatalf("Complex identity = %08b, want %08b", c.Faces[0].ColourIdentity(), want)
	}

	// Prose alone (with its colour-initial words) contributes nothing, and
	// so does reminder text's mana symbols: the parenthesised example cost
	// is CR 903.4c's ignored reminder text even though it braces a colour.
	silent := []string{
		"Name:Prose\nTypes:Creature\nOracle:The word Black here, and Blue, and Red, all prose or reminder.\n",
		"Name:Reminder\nManaCost:3\nTypes:Artifact\nOracle:Each spell that would cost less than three mana to cast costs three mana to cast. (For example, a spell that would cost {1}{B} to cast costs {2}{B} to cast instead.)\n",
		"Name:Basic\nManaCost:no cost\nTypes:Basic Land Mountain\nOracle:({T}: Add {R}.)\n",
	}
	for _, src := range silent {
		c, _ := ParseBytes("s.txt", []byte(src))
		if got := c.Faces[0].ColourIdentity(); got != 0 {
			t.Fatalf("%s identity = %08b, want empty", c.Faces[0].Name, got)
		}
	}

	// A characteristic-defining SetColor$ All on the card itself (Transguild
	// Courier's "CARDNAME is all colors") is identity with no mana symbol
	// anywhere; the same SetColor$ on a static that affects OTHER permanents
	// (Leyline of the Guildpact's ability) or that is not
	// characteristic-defining is not.
	all, _ := ParseBytes("s.txt", []byte(
		"Name:All\nManaCost:4\nTypes:Artifact Creature Golem\n"+
			"S:Mode$ Continuous | Affected$ Card.Self | CharacteristicDefining$ True | SetColor$ All | Description$ CARDNAME is all colors.\n"+
			"Oracle:Transguild Courier is all colors.\n"))
	if want := ColourWhite | ColourBlue | ColourBlack | ColourRed | ColourGreen; all.Faces[0].ColourIdentity() != want {
		t.Fatalf("SetColor$ All identity = %08b, want all five colours", all.Faces[0].ColourIdentity())
	}
	other, _ := ParseBytes("s.txt", []byte(
		"Name:Other\nManaCost:G\nTypes:Enchantment\n"+
			"S:Mode$ Continuous | Affected$ Permanent.nonLand+YouCtrl | SetColor$ All | Description$ Each nonland permanent you control is all colors.\n"+
			"Oracle:Each nonland permanent you control is all colors.\n"))
	if got := other.Faces[0].ColourIdentity(); got != ColourGreen {
		t.Fatalf("a SetColor$ on other permanents is not the card's identity; got %08b, want green", got)
	}
	chosen, _ := ParseBytes("s.txt", []byte(
		"Name:Chosen\nManaCost:5\nTypes:Creature\n"+
			"S:Mode$ Continuous | Affected$ Card.Self | CharacteristicDefining$ True | SetColor$ ChosenColor | Description$ If CARDNAME is your commander, choose a color.\n"+
			"Oracle:If CARDNAME is your commander, choose a color before the game begins.\n"))
	if got := chosen.Faces[0].ColourIdentity(); got != 0 {
		t.Fatalf("a commander-only chosen colour is not a deck-checkable identity; got %08b, want empty", got)
	}
}

// TestColourIdentityConstructionRoutesAgree is the whole-corpus enforcement
// of the one-derivation-route rule: the identity the parse route ends with
// (compiledCorpus: ParseBytes' derive, Link's keyword expansion and SVar
// linking, ApplyIntrinsics' final re-derive) and the identity the gob decode
// route re-derives must be equal for every face of every card. The
// derivation reads only printed fields (ManaCost, Colors, Oracle, CDA
// statics), which Link and the intrinsic layer never change, so any
// disagreement means a route scanned construction-time-only state — exactly
// the defect that made four non-land faces (Clay Champion, Dredging Claw,
// Lashwrithe, Veteran's Powerblade) differ by route when the old derivation
// scanned post-Link ability tokens and the pre-Link parse skipped them.
func TestColourIdentityConstructionRoutesAgree(t *testing.T) {
	r := corpusCache(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "ir.gob.gz")
	if err := r.Save(p); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadRegistry(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Len() != r.Len() {
		t.Fatalf("round trip lost cards: %d != %d", loaded.Len(), r.Len())
	}
	for i, c := range r.AllCards() {
		l := loaded.Card(i)
		if l.Path != c.Path {
			t.Fatalf("round trip reordered cards at %d: %q != %q", i, l.Path, c.Path)
		}
		for j := range c.Faces {
			if c.Faces[j].Name != l.Faces[j].Name || c.Faces[j].ColourIdentity() != l.Faces[j].ColourIdentity() {
				t.Fatalf("%s face %d (%q): parse-route identity %08b != gob-route %08b", c.Path, j, c.Faces[j].Name, c.Faces[j].ColourIdentity(), l.Faces[j].ColourIdentity())
			}
		}
	}
}

// TestColourIdentityCorpusPins pins the rv2c round's five named identities on
// the real corpus, each against the CR class it exercises:
//
//   - Pox: the SVar parameters ("Amount$ G" on a land-sacrifice count) are
//     not rules text; the identity is the cost's {B}{B}{B} exactly.
//   - Trinisphere: the parenthesised example cost in its reminder text is
//     CR 903.4c's ignored reminder; the identity is empty.
//   - Lingering Souls: a keyword's cost (Flashback {1}{B}) is printed rules
//     text the Oracle carries in braces; the identity is {2}{W}+{1}{B} = W|B.
//   - Transguild Courier and Sphinx of the Guildpact: the SetColor$ All CDA
//     ("CARDNAME is all colors") is identity with no mana symbol to scan.
//   - Badlands and Mountain: a typed land's parenthesised mana ability is
//     not scanned into identity — its deck legality comes from the CR 903.5d
//     could-produce rule the deck validator enforces instead — so both keep
//     an empty identity and deck/validate remains where that is enforced.
func TestColourIdentityCorpusPins(t *testing.T) {
	r := corpusCache(t)
	for _, tc := range []struct {
		name string
		want uint8
	}{
		{"Pox", ColourBlack},
		{"Trinisphere", 0},
		{"Lingering Souls", ColourWhite | ColourBlack},
		{"Transguild Courier", ColourWhite | ColourBlue | ColourBlack | ColourRed | ColourGreen},
		{"Sphinx of the Guildpact", ColourWhite | ColourBlue | ColourBlack | ColourRed | ColourGreen},
		{"Badlands", 0},
		{"Mountain", 0},
		// Dryad Arbor's identity is its colour indicator (Colors: green),
		// not its parenthesised mana ability text — Forest -> green is
		// separately enforced in the deck validator as CR 903.5d production.
		{"Dryad Arbor", ColourGreen},
	} {
		if got := mustLookup(t, r, tc.name).ColourIdentity(); got != tc.want {
			t.Errorf("%s colour identity = %08b, want %08b", tc.name, got, tc.want)
		}
	}
}

// TestColourIndicatorMapping guards that colour-indicator values that name a
// colour map to bits, while colourless maps to none.
func TestColourIndicatorMapping(t *testing.T) {
	if got := colourIndicator("black, red"); got != ColourBlack|ColourRed {
		t.Fatalf("colourIndicator(black,red) = %08b", got)
	}
	if got := colourIndicator("colorless"); got != 0 {
		t.Fatalf("colourIndicator(colorless) = %08b", got)
	}
	if got := colourIndicator("blue"); got != ColourBlue {
		t.Fatalf("colourIndicator(blue) = %08b", got)
	}
}
