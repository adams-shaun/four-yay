package mzplay

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func TestParseDCK(t *testing.T) {
	src := "NAME:Top player FDN deck 00001 WB (player WR 0.64)\r\n" +
		"1 [FDN:101] Ajani, Caller of the Pride\n" +
		"8 [FDN:272] Plains\n" +
		"\n" +
		"# a comment\n" +
		"2 [SPG:74] Fire // Ice\n" +
		"SB: 3 [FDN:5] Day of Judgment\n" +
		"3 Swamp\n" +
		"LAYOUT MAIN:(1,2)(NONE,false,50)|([FDN:101])\n" +
		"LAYOUT SIDEBOARD:(1,1)(NONE,false,50)|()\n"
	d, err := ParseDCK(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	want := DeckList{
		Name: "Top player FDN deck 00001 WB (player WR 0.64)",
		Main: []DeckEntry{{1, "FDN", "101", "Ajani, Caller of the Pride"}, {8, "FDN", "272", "Plains"},
			{2, "SPG", "74", "Fire // Ice"}, {3, "", "", "Swamp"}},
		Side: []DeckEntry{{3, "FDN", "5", "Day of Judgment"}},
	}
	if !reflect.DeepEqual(d, want) {
		t.Fatalf("deck:\n got %+v\nwant %+v", d, want)
	}
	if d.MainCount() != 14 {
		t.Fatalf("main count %d, want 14", d.MainCount())
	}
}

func TestParseDCKRefusesMalformedLines(t *testing.T) {
	for _, src := range []string{
		"x [FDN:1] Plains\n",
		"0 [FDN:1] Plains\n",
		"2 [FDN:1]\n",
		"2 [FDN 1] Plains\n",
		"Plains\n",
	} {
		if _, err := ParseDCK(strings.NewReader(src)); err == nil {
			t.Errorf("%q parsed", src)
		}
	}
}

type fakeLookup map[string]*cards.Card

func (f fakeLookup) Lookup(name string) (*cards.Card, bool) { c, ok := f[name]; return c, ok }

// Resolve expands counts in list order and reports every name the corpus
// does not hold, once each, without building a short deck silently.
func TestDeckListResolve(t *testing.T) {
	plains, bear := &cards.Card{}, &cards.Card{}
	reg := fakeLookup{"Plains": plains, "Grizzly Bears": bear}
	d := DeckList{Main: []DeckEntry{{Count: 2, Name: "Plains"}, {Count: 1, Name: "Grizzly Bears"},
		{Count: 3, Name: "Nonexistent Card"}, {Count: 1, Name: "Plains"}, {Count: 1, Name: "Nonexistent Card"}, {Count: 1, Name: "Another Missing"}}}
	got, missing := d.Resolve(reg)
	if want := []*cards.Card{plains, plains, bear, plains}; !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved %d cards, want %d in list order", len(got), len(want))
	}
	if want := []string{"Nonexistent Card", "Another Missing"}; !reflect.DeepEqual(missing, want) {
		t.Fatalf("missing %v, want %v", missing, want)
	}
}

func TestDeckStem(t *testing.T) {
	for in, want := range map[string]string{
		"/a/b/FDN_top_00001_WB.dck": "FDN_top_00001_WB",
		`C:\decks\x.y.dck`:          "x.y",
		"noext":                     "noext",
		"dir.d/noext":               "", // upstream's substring throws here
	} {
		if got := DeckStem(in); got != want {
			t.Errorf("DeckStem(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChooseDeck(t *testing.T) {
	pool := []string{"a", "b", "c"}
	// sequential: game i plays line i, wrapping (ParallelDataGenerator.chooseDeck).
	for i, want := range []string{"a", "b", "c", "a"} {
		if got := ChooseDeck(pool, "sequential", i, nil); got != want {
			t.Errorf("sequential game %d: %q, want %q", i, got, want)
		}
	}
	// random: one draw per call from the game's stream.
	draws := []int{2, 0}
	next := func(n int) int { v := draws[0]; draws = draws[1:]; return v % n }
	if a, b := ChooseDeck(pool, "random", 7, next), ChooseDeck(pool, "random", 7, next); a != "c" || b != "a" {
		t.Errorf("random: %q %q, want c a", a, b)
	}
}
