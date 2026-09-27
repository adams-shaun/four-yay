package cards

import "testing"

// The Outlast keyword expansion (CR 702.107a). K:Outlast:<cost> mints one
// ordinary activated ability -- Cost$ T <cost>, PutCounter P1P1 on Self,
// SorcerySpeed$ True -- exactly the machinery the Level up expander uses.
// The fixtures embed the real corpus scripts' keyword lines (never a
// committed .txt).

// TestOutlastExpandsToASorcerySpeedTapActivation pins the minted ability's
// exact shape on Abzan Battle Priest's real keyword line: one Ability whose
// Cost carries the {T} tap plus the printed mana, whose body puts exactly one
// P1P1 counter on Self, and whose SorcerySpeed$ rider is the CR 702.107a
// "only as a sorcery" clause.
func TestOutlastExpandsToASorcerySpeedTapActivation(t *testing.T) {
	f := expanded(t, "Name:Abzan Battle Priest\nManaCost:3 W\nTypes:Creature Human Cleric\nPT:3/2\nK:Outlast:W\nOracle:x\n")
	if len(f.Abilities) != 1 {
		t.Fatalf("want exactly one minted ability, got %+v", f.Abilities)
	}
	ab := f.Abilities[0]
	for key, want := range map[string]string{
		"Cost":         "T W",
		"Defined":      "Self",
		"CounterType":  "P1P1",
		"CounterNum":   "1",
		"SorcerySpeed": "True",
		"Keyword":      "Outlast",
	} {
		if ab.Params[key] != want {
			t.Fatalf("%s = %q, want %q (%+v)", key, ab.Params[key], want, ab.Params)
		}
	}
	if ab.API != "PutCounter" {
		t.Fatalf("API = %q, want PutCounter", ab.API)
	}
	if ab.Params["KeywordLine"] != "Outlast:W" {
		t.Fatalf("KeywordLine = %q, want the full keyword line", ab.Params["KeywordLine"])
	}
}

// TestOutlastDropsTrailingDescription covers a keyword line carrying a
// display field after a second colon: the cost is everything up to the SECOND
// colon, the tail is display text only.
func TestOutlastDropsTrailingDescription(t *testing.T) {
	f := expanded(t, "Name:Snowy\nManaCost:2\nTypes:Snow Creature\nPT:2/2\nK:Outlast:2 W:some display text\nOracle:x\n")
	if len(f.Abilities) != 1 {
		t.Fatalf("%+v", f.Abilities)
	}
	if got := f.Abilities[0].Params["Cost"]; got != "T 2 W" {
		t.Fatalf("Cost = %q, want %q", got, "T 2 W")
	}
}

// TestOutlastIsIdempotent: a second Link() of an already-expanded face must
// not append a second copy of the same keyword line's ability (the
// KeywordLine idempotence check every expander shares).
func TestOutlastIsIdempotent(t *testing.T) {
	c, diags := ParseBytes("k.txt", []byte("Name:Abzan Battle Priest\nManaCost:3 W\nTypes:Creature Human Cleric\nPT:3/2\nK:Outlast:W\nOracle:x\n"))
	if len(diags) > 0 {
		t.Fatal(diags)
	}
	if d := c.Link(); len(d) > 0 {
		t.Fatal(d)
	}
	if d := c.Link(); len(d) > 0 {
		t.Fatal(d)
	}
	n := 0
	for _, ab := range c.Faces[0].Abilities {
		if ab.Params["KeywordLine"] == "Outlast:W" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("Outlast expanded %d times across two Link() calls, want 1", n)
	}
}
