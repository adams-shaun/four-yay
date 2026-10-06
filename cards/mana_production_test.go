package cards

import (
	"path/filepath"
	"testing"
)

// mpOf parses a one-line script and returns the face's mana production, the
// way the view and the bot policy both read it (off the compiled face). It
// applies the intrinsic layer first, exactly as corpus load does, so a basic
// land's subtype-granted ability is present -- the projection itself does no
// subtype special-casing.
func mpOf(t *testing.T, src string) ManaProduction {
	t.Helper()
	c, err := ParseBytes("x/x.txt", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	f := c.Faces[0]
	f.ApplyIntrinsics()
	return f.ManaProduction()
}

// TestManaProductionBasicLand is the easy 60%: a basic land carries no
// mana ability in the script; the intrinsic layer grants it one from its
// subtype, and the projection picks it up the same way it would a scripted
// ability -- no subtype special-casing in the projection itself.
func TestManaProductionBasicLand(t *testing.T) {
	mp := mpOf(t, "Name:Plains\nManaCost:no cost\nTypes:Basic Land Plains\nOracle:x\n")
	if mp.IsZero() {
		t.Fatal("a basic Plains must project a production, not zero")
	}
	if !mp.ProducesColour(0) { // W
		t.Errorf("Plains should produce white; got %v", mp.Colour)
	}
	if mp.Colour[0] != 1 || mp.Any {
		t.Errorf("Plains = %v, want exactly one white, not flexible", mp)
	}
}

// TestManaProductionDual sums both intrinsic halves: a Tundra (Plains + Island)
// is granted a {W} ability and a {U} ability, and tapping it runs both, so the
// projection is {W}{U} -- the exact mana the pool receives.
func TestManaProductionDual(t *testing.T) {
	mp := mpOf(t, "Name:Tundra\nManaCost:no cost\nTypes:Land Plains Island\nOracle:x\n")
	if !mp.ProducesColour(0) || !mp.ProducesColour(1) {
		t.Fatalf("Tundra should produce white and blue; got %v", mp.Colour)
	}
	if mp.DistinctColours() != 2 || mp.Any {
		t.Errorf("Tundra = %v, want two distinct colours, not flexible", mp)
	}
}

// TestManaProductionAmount counts an Amount$ multiplier: a colourless rock
// that taps for {C}{C} projects two colourless, and a {G}{G}{G} producer
// three green.
func TestManaProductionAmount(t *testing.T) {
	c := mpOf(t, "Name:Rock\nTypes:Artifact\nA:AB$ Mana | Cost$ T | Produced$ C | Amount$ 2 | Oracle:x\n")
	if c.Colour[5] != 2 || c.Any {
		t.Errorf("colourless rock = %v, want two colourless, not flexible", c)
	}
	g := mpOf(t, "Name:Elf\nTypes:Creature\nA:AB$ Mana | Cost$ T | Produced$ G | Amount$ 3 | Oracle:x\n")
	if g.Colour[4] != 3 {
		t.Errorf("green producer = %v, want three green", g.Colour)
	}
}

// TestManaProductionAnyIsHonest projects the real alternatives of
// "Produced$ Any": one possible unit in each WUBRG slot, with Any marking
// that the source supplies one of those alternatives rather than all five.
func TestManaProductionAnyIsHonest(t *testing.T) {
	mp := mpOf(t, "Name:Cavern\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ Any | Oracle:x\n")
	want := [6]int32{1, 1, 1, 1, 1, 0}
	if mp.Colour != want {
		t.Errorf("Any production = %v, want all five real colour alternatives", mp.Colour)
	}
	if !mp.Any {
		t.Error("Any production must be flagged as flexible/conditional")
	}
	for i := 0; i < 5; i++ {
		if !mp.ProducesColour(i) {
			t.Errorf("Any production must report producible colour %d", i)
		}
	}
}

// TestManaProductionComboCountsOnlyItsTokens pins the fb-windgrace fix: a
// "Combo R G" choice counts ONLY the colours its tokens name -- the rune
// walk of the word "Combo" itself (five phantom colourless) is gone. It
// remains Any because a Combo production is a script-level choice, never a
// plain colour string.
func TestManaProductionComboMirrorsExecutor(t *testing.T) {
	mp := mpOf(t, "Name:Shock\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ Combo R G | Oracle:x\n")
	if !mp.Any || mp.Colour[3] != 1 || mp.Colour[4] != 1 || mp.Colour[5] != 0 {
		t.Errorf("Combo R G = %v, want Any with R, G, and no colourless", mp)
	}
}

// TestManaProductionChosenClaimsNothing pins the fail-closed half: a word
// that is not a symbol token ("Chosen"; the same shape as "ColorIdentity",
// "Special ...") claims NO mana at all -- never the colourless its runes
// used to be counted as. The flag stays: the choice is unmodelled.
func TestManaProductionChosenMirrorsExecutor(t *testing.T) {
	mp := mpOf(t, "Name:Chosen\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ Chosen | Oracle:x\n")
	if !mp.Any || mp.Colour[5] != 0 {
		t.Errorf("Chosen = %v, want Any with no mana claimed", mp)
	}
}

// TestProducedCounts pins cards.ProducedCounts, the one Produced$ parse the
// per-face projection (ManaProduction.add) and rules' addAvailable both
// fold through, on the shapes the fb-windgrace feedback report measured:
// the corpus-real "Combo ColorIdentity" (Command Tower / Arcane Signet) that
// counted 18 phantom colourless, the "Combo B R" dual (Blackcleave Cliffs)
// that counted 5, and the token grammar itself.
func TestProducedCounts(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want [6]int32
		any  bool
	}{
		{"blank defaults to one colourless and a flag", "", [6]int32{0, 0, 0, 0, 0, 1}, true},
		{"Any reports every possible colour", "Any", [6]int32{1, 1, 1, 1, 1, 0}, true},
		{"Combo Any reports every possible colour", "Combo Any", [6]int32{1, 1, 1, 1, 1, 0}, true},
		{"plain colour", "B", [6]int32{0, 0, 1, 0, 0, 0}, false},
		{"same-symbol token counts two", "RR", [6]int32{0, 0, 0, 2, 0, 0}, false},
		{"space-separated tokens survive", "R G", [6]int32{0, 0, 0, 1, 1, 0}, false},
		{"five-colour literal", "W U B R G", [6]int32{1, 1, 1, 1, 1, 0}, false},
		{"colourless token", "C", [6]int32{0, 0, 0, 0, 0, 1}, false},
		{"braces are stripped", "{B}{R}", [6]int32{0, 0, 1, 1, 0, 0}, false},
		// The fb-windgrace shapes:
		{"Combo dual names its colours only", "Combo B R", [6]int32{0, 0, 1, 1, 0, 0}, true},
		{"Combo triple", "Combo W U B", [6]int32{1, 1, 1, 0, 0, 0}, true},
		{"ColorIdentity claims nothing", "Combo ColorIdentity", [6]int32{}, true},
		{"ColorID claims nothing", "Combo ColorID", [6]int32{}, true},
		{"Chosen reports every possible colour", "Combo R Chosen", [6]int32{1, 1, 1, 1, 1, 0}, true},
		{"Special word claims nothing", "Special EachColorAmong_ExiledWith", [6]int32{}, true},
		{"a token containing a letter is rejected whole", "Combo NotedColors", [6]int32{}, true},
	}
	for _, tc := range cases {
		counts, any := ProducedCounts(tc.in)
		if counts != tc.want || any != tc.any {
			t.Errorf("%s: ProducedCounts(%q) = %v, %v; want %v, %v", tc.name, tc.in, counts, any, tc.want, tc.any)
		}
	}
}

// TestManaProductionCommandTowerShape pins the wire value the feedback
// report caught: a commander-identity land ("Produced$ Combo ColorIdentity")
// must project NO colourless -- the 18 phantom units came from counting the
// runes of the words "Combo" and "ColorIdentity".
func TestManaProductionCommandTowerShape(t *testing.T) {
	mp := mpOf(t, "Name:Command Tower\nManaCost:no cost\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ Combo ColorIdentity | Oracle:x\n")
	if mp.Colour != [6]int32{} || !mp.Any {
		t.Errorf("Command Tower shape = %v, want zero counts with the Any flag", mp.Colour)
	}
}

// TestManaProductionTalismanKeepsItsRealColourless pins that a REAL
// colourless ability (Talisman of Indulgence's "Produced$ C") is kept while
// its sibling Combo choice loses its phantom -- the talisman projects
// exactly one B, one R, one C.
func TestManaProductionTalismanKeepsItsRealColourless(t *testing.T) {
	mp := mpOf(t, "Name:Talisman\nTypes:Artifact\n"+
		"A:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ Add {C}.\n"+
		"A:AB$ Mana | Cost$ T | Produced$ Combo B R | SpellDescription$ Add {B} or {R}.\nOracle:x\n")
	want := [6]int32{0, 0, 1, 1, 0, 1}
	if mp.Colour != want || !mp.Any {
		t.Errorf("talisman = %v, want %v with Any", mp.Colour, want)
	}
}

// TestManaProductionDistinctColoursUnmovedByPhantomFix pins the Any-semantics
// guarantee: because a Combo choice keeps its flag, DistinctColours reports
// 5 for it exactly as before the fix -- botpolicy's coloured rankings must
// not move.
func TestManaProductionDistinctColoursUnmovedByPhantomFix(t *testing.T) {
	mp := mpOf(t, "Name:Dual\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ Combo B R | Oracle:x\n")
	if mp.DistinctColours() != 5 {
		t.Errorf("Combo dual DistinctColours = %d, want 5 (the Any flag is unchanged)", mp.DistinctColours())
	}
	if !mp.ProducesColour(2) || !mp.ProducesColour(3) {
		t.Errorf("Combo dual must still list its real colours: %v", mp.Colour)
	}
}

// Both cache collections must reconstruct the unexported derived field.
func TestManaProductionDerivedRoundTrip(t *testing.T) {
	r := NewRegistry()
	for _, src := range []string{
		"Name:Dual\nTypes:Land Plains Island\n",
		"Name:Rock\nTypes:Artifact\nA:AB$ Mana | Produced$ Combo Any | Amount$ 2\n",
		"Name:Empty\nTypes:Creature\n",
	} {
		c, _ := ParseBytes("fixture", []byte(src))
		c.Link()
		f := c.Faces[0]
		f.ApplyIntrinsics()
		want := f.ManaProduction()
		f.ApplyIntrinsics()
		if got := f.ManaProduction(); got != want {
			t.Fatalf("repeated intrinsics: %v != %v", got, want)
		}
		r.Add(c)
		r.Tokens[f.Name] = c
	}
	path := filepath.Join(t.TempDir(), "ir.gob.gz")
	if err := r.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range r.AllCards() {
		f := c.Faces[0]
		var want ManaProduction
		for _, a := range f.ManaAbilities() {
			want.add(a)
		}
		for _, got := range []ManaProduction{f.ManaProduction(), back.Card(i).Faces[0].ManaProduction(), back.Tokens[f.Name].Faces[0].ManaProduction()} {
			if got != want {
				t.Errorf("%s: got %v, want %v", f.Name, got, want)
			}
		}
		if n := testing.AllocsPerRun(100, func() {
			if f.ManaProduction() != want {
				panic("production changed")
			}
		}); n != 0 {
			t.Errorf("%s: getter allocates %g times", f.Name, n)
		}
	}
}

func TestManaProductionAnyIsMostFlexible(t *testing.T) {
	if got := (ManaProduction{Any: true}).DistinctColours(); got != 5 {
		t.Errorf("Any DistinctColours = %d, want 5", got)
	}
}

// TestManaProductionUnknownAmountDoesNotClaimColour is the bl1 fix for the
// tap gate believing mana that never arrives: a mana ability whose Amount$
// is a non-literal expression ("X", "Y", "UrzaAmount", a Count$ expression,
// "Sacrificed$...") has an amount the projection cannot statically price.
// The old collector defaulted such an amount to 1 and so recorded a colour
// slot the pool is never promised; the honest projection claims no colour
// from it (the executor's Num resolves it through the SVar/count/$X
// machinery to zero on a plain tap-for-mana activation, or to a count the
// projection has no context for). Index 1 is blue (U); index 4 is green.
func TestManaProductionUnknownAmountDoesNotClaimColour(t *testing.T) {
	mp := mpOf(t, "Name:ManaBug\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ U | Amount$ X | Oracle:x\n")
	if mp.ProducesColour(1) {
		t.Errorf("Amount$ X source claims a blue pip the pool is not promised: %v", mp.Colour)
	}
	if mp.Colour[1] != 0 {
		t.Errorf("Amount$ X source records %d blue, want 0 (production the executor will not deliver)", mp.Colour[1])
	}
	if mp.Colour[4] != 0 {
		t.Errorf("Amount$ X source records %d green, want 0", mp.Colour[4])
	}
}
