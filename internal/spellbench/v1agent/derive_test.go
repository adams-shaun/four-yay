package v1agent

import (
	"os"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/spellbench"
)

// TestHintFreeFilesNameNoCard: the generic build's knowledge files carry
// no card or deck name, so nothing in them can be pool-specific.
func TestHintFreeFilesNameNoCard(t *testing.T) {
	var names []string
	for _, k := range kernelCards {
		names = append(names, k.Name)
	}
	ids, err := spellbench.CatalogIDs(spellbench.PauperKernel)
	if err != nil {
		t.Fatal(err)
	}
	names = append(names, ids...)
	for _, f := range []string{"derive.go", "generic.go"} {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range names {
			if strings.Contains(string(src), `"`+n+`"`) || strings.Contains(string(src), n+" ") && len(n) > 6 && strings.Contains(n, " ") {
				t.Errorf("%s names %q", f, n)
			}
		}
	}
}

// TestDerivedHintsMatchTheTable: the derived profile reproduces the
// hand-written role, timing and polarity for the pool's cards, except
// where the derivation deliberately differs (listed).
func TestDerivedHintsMatchTheTable(t *testing.T) {
	// fields the derivation deliberately reads differently; see derive.go
	differs := map[string]string{
		"Blood Token":              "role: tokens are never cast",
		"End the Festivities":      "dmg: the sweeper's damage is read per play",
		"Galvanic Blast":           "dmg: evaluated at the board (metalcraft)",
		"Experimental Synthesizer": "ab: a sorcery-speed value ability waits for main 2, not the end step",
		"Sacred Cat":               "ab: per zone (the embalm is a graveyard ability)",
		"Twisted Landscape":        "ab: per zone (cycling from hand, fetch on the battlefield)",
		"Troll of Khazad-dum":      "bonus: read from the observed minimum-blockers keyword",
		"Overgrown Battlement":     "bonus: scaling mana is an engine",
		"Saruli Caretaker":         "bonus: a mana creature",
		"Wall of Roots":            "bonus: a mana creature",
		"Tinder Wall":              "bonus: a disposable mana body",
		"Writhing Chrysalis":       "bonus: token engine",
	}
	for name, h := range hints {
		if _, ok := differs[name]; ok {
			continue
		}
		d := profileFor(name).hint
		if h.role != d.role || h.ab != d.ab || h.tapped != d.tapped || h.flash != d.flash || h.sacCost != d.sacCost ||
			(h.pol != 0 && h.pol != d.pol) || (h.dmg != 0 && h.dmg != d.dmg) || h.bonus != d.bonus {
			t.Errorf("%s: hint %+v, derived %+v", name, h, d)
		}
	}
}

// TestDeckStyleFromCards: the aggro/burn reading from decklists agrees
// with the hinted build's deck-name table on the pool (and calls Terror,
// which that table never saw, neither).
func TestDeckStyleFromCards(t *testing.T) {
	want := map[string]deckStyle{
		"Burn": {burn: true, aggro: true}, "Rally": {burn: true, aggro: true},
		"Affinity": {aggro: true}, "Elves": {aggro: true},
		"Wildfire": {}, "Spy": {}, "CawGates": {}, "Faeries": {}, "Terror": {},
	}
	for id, w := range want {
		got := styleOf(deckCounts(id))
		if got.burn != w.burn || got.aggro != w.aggro {
			t.Errorf("%s: style %+v, want burn=%v aggro=%v", id, got, w.burn, w.aggro)
		}
	}
}

// TestGenericComboMatchesSpyCombo replays the Spy combo scenario through
// the derived recognition.
func TestGenericComboMatchesSpyCombo(t *testing.T) {
	for _, gen := range []bool{false, true} {
		tac := NewTactical(TacticalOptions{})
		tac.gen = gen
		tac.GameStart(&GameStart{Seat: "p0", CatalogIDs: []string{"Spy", "Burn"}})
		combo := func(b *Board) bool {
			if gen {
				return tac.comboG(b, false)
			}
			return tac.spyCombo(b, false)
		}
		b := &Board{Seat: "p0", OppSeat: "p1", Me: 0, Opp: 1, Life: [2]int{20, 20}, byArena: map[uint32]*KCard{}}
		land := func(id uint32, name string) *KCard {
			c := &KCard{Name: name}
			c.Stable = KRef{ArenaID: id, Owner: "p0", Controller: "p0", Zone: "Battlefield"}
			c.Characteristics.Types.Land = true
			return c
		}
		b.Mine = []*KCard{land(1, "Swamp"), land(2, "Forest"), land(3, "Forest"),
			testCreature(10, "Wall of Roots", "p0", 0, 5, false), testCreature(11, "Saruli Caretaker", "p0", 0, 3, false)}
		if combo(b) {
			t.Fatalf("gen=%v: combo on with a Forest still in the library", gen)
		}
		b.Mine = append(b.Mine, land(4, "Forest"))
		if !combo(b) {
			t.Fatalf("gen=%v: combo off with no land left", gen)
		}
		b.Mine = b.Mine[:3]
		b.Mine = append(b.Mine, land(4, "Forest"), testCreature(10, "Wall of Roots", "p0", 0, 5, false))
		if combo(b) {
			t.Fatalf("gen=%v: combo on without three creatures for the flashback", gen)
		}
	}
}

// TestGenericOppPump: the derived opposing pump equals the named one on a
// board with an untapped Timberwatch-style elf pump.
func TestGenericOppPump(t *testing.T) {
	b := &Board{Seat: "p0", OppSeat: "p1", Me: 0, Opp: 1, Life: [2]int{20, 20}, byArena: map[uint32]*KCard{}}
	b.Theirs = []*KCard{
		testCreature(1, "Timberwatch Elf", "p1", 1, 2, false),
		testCreature(2, "Llanowar Elves", "p1", 1, 1, false),
		testCreature(3, "Elvish Mystic", "p1", 1, 1, true),
	}
	hinted, gen := NewTactical(TacticalOptions{}), NewGeneric(TacticalOptions{})
	if h, g := hinted.oppPump(b), gen.oppPump(b); h != 3 || g != 3 {
		t.Fatalf("oppPump hinted %d generic %d, want 3", h, g)
	}
	b.Theirs[0].SummoningSick = true
	if g := gen.oppPump(b); g != 0 {
		t.Fatalf("a summoning-sick tapper pumps %d", g)
	}
}

// TestCounterFits: counter restrictions come from the card data.
func TestCounterFits(t *testing.T) {
	gen := NewGeneric(TacticalOptions{})
	b := &Board{Seat: "p0", OppSeat: "p1", Me: 0, Opp: 1, byArena: map[uint32]*KCard{}}
	id := func(name string) uint16 {
		for i := range kernelCards {
			if kernelCards[i].Name == name {
				return uint16(i)
			}
		}
		t.Fatalf("no kernel card %q", name)
		return 0
	}
	spell := func(name string) *KStackItem {
		return &KStackItem{Source: KRef{CardDBID: id(name)}, Controller: "p1", Kind: "spell"}
	}
	eff := func(name string) *EffectFact { return profileFor(name).plays[0].eff }
	cases := []struct {
		counter, target string
		lands           int
		want            bool
	}{
		{"Dispel", "Lightning Bolt", 0, true},
		{"Dispel", "Llanowar Elves", 0, false},
		{"Spell Pierce", "Lightning Bolt", 0, true},
		{"Spell Pierce", "Lightning Bolt", 2, false},
		{"Spell Pierce", "Llanowar Elves", 0, false},
		{"Force Spike", "Llanowar Elves", 1, false},
		{"Counterspell", "Llanowar Elves", 5, true},
		{"Envelop", "Chain Lightning", 0, true},
		{"Envelop", "Lightning Bolt", 0, false},
	}
	for _, c := range cases {
		b.Theirs = nil
		for i := 0; i < c.lands; i++ {
			l := &KCard{Name: "Forest"}
			l.Characteristics.Types.Land = true
			b.Theirs = append(b.Theirs, l)
		}
		if got := gen.counterFits(b, eff(c.counter), spell(c.target), c.counter); got != c.want {
			t.Errorf("%s on %s with %d open lands: %v, want %v", c.counter, c.target, c.lands, got, c.want)
		}
	}
	// a flash creature's ETB counter: mana value up to the subtype count,
	// the creature itself included
	sprite := profileFor("Spellstutter Sprite")
	if sprite.counterETB == nil {
		t.Fatal("no ETB counter derived")
	}
	b.Theirs = nil
	b.Mine = []*KCard{testCreature(5, "Faerie Seer", "p0", 1, 1, false)}
	if !gen.counterFits(b, sprite.counterETB, spell("Counterspell"), "Spellstutter Sprite") {
		t.Error("two faeries should counter a two-mana spell")
	}
	if gen.counterFits(b, sprite.counterETB, spell("Rally at the Hornburg"), "Spellstutter Sprite") == false {
		t.Error("two faeries should counter Rally (mana value 2)")
	}
	if gen.counterFits(b, sprite.counterETB, spell("Guttersnipe"), "Spellstutter Sprite") {
		t.Error("two faeries cannot counter a three-mana spell")
	}
}

// TestGenericReadsNoHints plays the recorded fixtures with the generic
// build and asserts the hint table is never read.
func TestGenericReadsNoHints(t *testing.T) {
	for _, fx := range []string{"testdata/block.json", "testdata/bolt_target.json"} {
		raw, err := os.ReadFile(fx)
		if err != nil {
			t.Fatal(err)
		}
		d, err := ParseDecision(raw)
		if err != nil {
			t.Fatal(err)
		}
		for _, deck := range []string{"Burn", "Elves", "Spy", "Unknown"} {
			g := NewGeneric(TacticalOptions{})
			g.GameStart(&GameStart{Seat: d.ActingSeat, CatalogIDs: []string{deck, deck}})
			before := hintLookups
			if got := g.Choose(d); got < 0 || got >= len(d.Candidates) {
				t.Fatalf("%s: pick %d out of range", fx, got)
			}
			// score every candidate kind the fixture offers, not only the pick
			b := NewBoard(d)
			for i := range d.Candidates {
				g.score(d, b, i)
			}
			if hintLookups != before {
				t.Fatalf("%s (%s): generic build read the hint table %d times", fx, deck, hintLookups-before)
			}
		}
	}
}

// TestBurnReach: the face damage castable this turn counts affordable
// burn and a land-sacrifice alternative cost.
func TestBurnReach(t *testing.T) {
	g := NewGeneric(TacticalOptions{})
	b := &Board{Seat: "p0", OppSeat: "p1", Me: 0, Opp: 1, Life: [2]int{20, 6}, byArena: map[uint32]*KCard{}}
	mountain := func(tapped bool) *KCard {
		c := &KCard{Name: "Mountain", Tapped: tapped}
		c.Characteristics.Types.Land = true
		return c
	}
	b.Mine = []*KCard{mountain(true), mountain(true), mountain(false)}
	b.Hand = []KHandCard{{Name: "Fireblast"}, {Name: "Lightning Bolt"}, {Name: "Lightning Bolt"}}
	// Fireblast by sacrificing two Mountains (4), one Bolt from the one
	// untapped Mountain (3)
	if got := g.burnReach(b); got != 7 {
		t.Fatalf("reach %d, want 7", got)
	}
	b.Mine = b.Mine[2:]
	if got := g.burnReach(b); got != 3 {
		t.Fatalf("reach with one land %d, want 3", got)
	}
}
