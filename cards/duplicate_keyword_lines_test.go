package cards

import "testing"

// TestDuplicateKeywordLinesExpandTwice pins CR 702.108b and the corpus cards
// that print a keyword line twice: prowess triggers separately per instance
// (Thor Odinson, Ruric Thar Biomagus, Khenra Spellspear), and the same holds
// for a NON-prowess triggered instance (Scurry of Squirrels prints "Myriad,
// myriad"). Forge expands every K: line on its own, so a face with two equal
// lines must hold two expansions -- the previous text-keyed idempotence
// collapsed them to one. A re-Link of the same face must then add nothing:
// the idempotence key is the line's INSTANCE (its index in f.Keywords), not
// the line's text.
func TestDuplicateKeywordLinesExpandTwice(t *testing.T) {
	// Thor Odinson's face is exactly this shape: a creature printing Prowess
	// on two separate K: lines. Assert the precondition before trusting the
	// trigger count below.
	f := expanded(t, "Name:Thor\nManaCost:2 R\nTypes:Creature God Warrior\nPT:3/4\nK:Prowess\nK:Prowess\nOracle:x\n")
	if got := len(f.Keywords); got != 2 {
		t.Fatalf("precondition: %d keyword lines, want 2", got)
	}
	if f.Keywords[0] != "Prowess" || f.Keywords[1] != "Prowess" {
		t.Fatalf("precondition: keywords = %v, want two Prowess lines", f.Keywords)
	}
	if got := len(f.Triggers); got != 2 {
		t.Fatalf("two K:Prowess lines expanded to %d triggers, want 2 (CR 702.108b)", got)
	}
	for i, tr := range f.Triggers {
		if tr.Params["Keyword"] != "Prowess" || tr.Params["Mode"] != "SpellCast" {
			t.Fatalf("trigger %d not a Prowess SpellCast: %+v", i, tr.Params)
		}
	}

	// A second Link() over the same face must not add a third pulse.
	if d := f.link("dup.txt"); len(d) > 0 {
		t.Fatal(d)
	}
	if got := len(f.Triggers); got != 2 {
		t.Fatalf("a second Link expanded again: %d triggers, want 2", got)
	}
}

// TestDuplicateStaticKeywordLinesExpandTwice covers the sibling expanders
// that mint a STATIC rather than a trigger (Affinity, Undaunted): they used a
// private scan instead of the shared has("S", ...) arm, so the index-keyed
// idempotence would have missed them. Two identical lines now expand twice,
// matching Forge, and a re-Link still adds nothing.
func TestDuplicateStaticKeywordLinesExpandTwice(t *testing.T) {
	f := expanded(t, "Name:Aff\nManaCost:4\nTypes:Artifact\nK:Affinity:Artifact\nK:Affinity:Artifact\nK:Undaunted\nK:Undaunted\nOracle:x\n")
	if got := len(f.Statics); got != 4 {
		t.Fatalf("two Affinity + two Undaunted lines expanded to %d statics, want 4", got)
	}
	byLine := map[string]int{}
	for _, st := range f.Statics {
		byLine[st.Params["KeywordLine"]]++
	}
	if byLine["Affinity:Artifact"] != 2 || byLine["Undaunted"] != 2 {
		t.Fatalf("static counts per line = %v, want each line twice", byLine)
	}
	if d := f.link("dup.txt"); len(d) > 0 {
		t.Fatal(d)
	}
	if got := len(f.Statics); got != 4 {
		t.Fatalf("a second Link expanded again: %d statics, want 4", got)
	}
}
