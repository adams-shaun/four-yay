package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// testCommanderSrc is a minimal, behaviour-free commander: a no-ability
// artifact that this task's genesis simply parks in the command zone. The
// Commander milestone's other tasks (the tax, CR 903.9, commander damage)
// make the commander actually do things; here it only has to be a
// distinguishable object in the right zone.
const testCommanderSrc = `Name:Crown
ManaCost:0
Types:Legendary Artifact Creature
Oracle:x
`

// commanderDeckPair builds two 40-card decks, each with a commander at
// index 0 and a mountain elsewhere.
func commanderDeckPair(t *testing.T) (deck0, deck1 []*cards.Card) {
	t.Helper()
	cmd := card(t, testCommanderSrc)
	m := mountainDeck(t, 39)
	deck0 = append([]*cards.Card{cmd}, append([]*cards.Card(nil), m...)...)
	deck1 = append([]*cards.Card{cmd}, append([]*cards.Card(nil), m...)...)
	return
}

func zoneHas(ids []state.ObjID, want state.ObjID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// TestCommanderGenesisPlacesCommandersBeforeTheDeal is the m30 genesis
// contract (and the mutation guard for the ZCommand placement itself): the
// objects Config.Commanders names for each seat land in ZCommand -- not the
// library, not the opening hand -- sized bookkeeping is populated, the
// configured starting life takes effect, and a replay folded from the same
// (Config, Log) reproduces the identical command zone.
func TestCommanderGenesisPlacesCommandersBeforeTheDeal(t *testing.T) {
	t.Parallel()
	deck0, deck1 := commanderDeckPair(t)
	cfg := Config{
		Seed: 11, Names: []string{"a", "b"},
		Decks:        [][]*cards.Card{deck0, deck1},
		Commanders:   [][]int{{0}, {0}},
		StartingLife: 40,
		Format:       FormatCommander,
	}
	e := New(cfg)
	e.Advance()

	// (c) StartingLife takes effect.
	if e.G.Players[0].Life != 40 || e.G.Players[1].Life != 40 {
		t.Fatalf("life = %d/%d, want 40/40", e.G.Players[0].Life, e.G.Players[1].Life)
	}

	// (a) each commander is in the command zone, the object its Config index
	// named (deck[0], since AddObject runs in deck order).
	cmd0 := e.G.Zone(state.ZCommand, 0)
	cmd1 := e.G.Zone(state.ZCommand, 1)
	if len(cmd0) != 1 || len(cmd1) != 1 {
		t.Fatalf("command zone sizes = %d/%d, want 1/1", len(cmd0), len(cmd1))
	}
	cid0, cid1 := cmd0[0], cmd1[0]

	// (b) not in the library, and not in the opening hand.
	for seat, id := range map[state.PlayerID]state.ObjID{0: cid0, 1: cid1} {
		if zoneHas(e.G.Zone(state.ZLibrary, seat), id) || zoneHas(e.G.Zone(state.ZHand, seat), id) {
			t.Fatalf("seat %d commander is in library or opening hand (zone contains it)", seat)
		}
	}
	// Library shrank by exactly one per seat (40 - 1 commander - 7 hand).
	if got := len(e.G.Zone(state.ZLibrary, 0)); got != 32 {
		t.Fatalf("library size = %d, want 32 (commander must not be shuffled/dealt)", got)
	}

	// Bookkeeping: Commanders in Config order, CmdCasts parallel, CmdDamage
	// sized to the match-wide count (2 commanders), indexed by the dense
	// commander index assigned at genesis.
	if !reflect.DeepEqual(e.G.Players[0].Commanders, []state.ObjID{cid0}) {
		t.Fatalf("seat 0 Commanders = %v, want [%d]", e.G.Players[0].Commanders, cid0)
	}
	if len(e.G.Players[0].CmdCasts) != 1 || len(e.G.Players[0].CmdCasts) != len(e.G.Players[0].Commanders) {
		t.Fatalf("seat 0 CmdCasts = %v, want one entry parallel to Commanders", e.G.Players[0].CmdCasts)
	}
	for p := state.PlayerID(0); p < 2; p++ {
		if len(e.G.Players[p].CmdDamage) != 2 {
			t.Fatalf("seat %d CmdDamage sized %d, want 2 (match-wide commander count)", p, len(e.G.Players[p].CmdDamage))
		}
	}

	// (d) the same Config replayed from its own (Config, Log) reproduces the
	// identical command zone and bookkeeping.
	re, err := replayFor(cfg, e.L)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !reflect.DeepEqual(re.G.Zone(state.ZCommand, 0), e.G.Zone(state.ZCommand, 0)) ||
		!reflect.DeepEqual(re.G.Zone(state.ZCommand, 1), e.G.Zone(state.ZCommand, 1)) {
		t.Fatalf("replay command zone differs: got %v/%v want %v/%v",
			re.G.Zone(state.ZCommand, 0), re.G.Zone(state.ZCommand, 1),
			e.G.Zone(state.ZCommand, 0), e.G.Zone(state.ZCommand, 1))
	}
	if !reflect.DeepEqual(re.G.Players[0].Commanders, e.G.Players[0].Commanders) ||
		!reflect.DeepEqual(re.G.Players[1].Commanders, e.G.Players[1].Commanders) {
		t.Fatalf("replay Commanders differ from the live game's")
	}
}

// TestCommanderGenesisDefaultsToTwentyLife asserts that a Config that never
// sets StartingLife (zero value) keeps the existing 20, and that commander
// placement is keyed off Config.Commanders alone.
func TestCommanderGenesisDefaultsToTwentyLife(t *testing.T) {
	t.Parallel()
	deck0, deck1 := commanderDeckPair(t)
	cfg := Config{Seed: 12, Names: []string{"a", "b"},
		Decks:      [][]*cards.Card{deck0, deck1},
		Commanders: [][]int{{0}, {0}},
	}
	e := New(cfg)
	if e.G.Players[0].Life != 20 || e.G.Players[1].Life != 20 {
		t.Fatalf("life = %d/%d, want the default 20/20", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	if len(e.G.Zone(state.ZCommand, 0)) != 1 || len(e.G.Zone(state.ZCommand, 1)) != 1 {
		t.Fatal("commanders must still be placed when StartingLife is the zero value")
	}
}

// TestCommanderIndexOutOfRangeDegrades is the mutation guard for the
// Commanders index-range validation: an index out of range for its deck is
// degraded (skipped), never a panic -- the same degrade-don't-crash stance
// New already takes for more decks than seats.
func TestCommanderIndexOutOfRangeDegrades(t *testing.T) {
	t.Parallel()
	deck0, deck1 := commanderDeckPair(t)
	// seat 0 names index 5 (valid, < 40); seat 1 names index 99 (out of range).
	cfg := Config{Seed: 13, Names: []string{"a", "b"},
		Decks:      [][]*cards.Card{deck0, deck1},
		Commanders: [][]int{{5}, {99}},
	}
	e := New(cfg)
	e.Advance()

	if got := len(e.G.Zone(state.ZCommand, 0)); got != 1 {
		t.Fatalf("seat 0 (valid index) command zone = %d, want 1", got)
	}
	if got := len(e.G.Zone(state.ZCommand, 1)); got != 0 {
		t.Fatalf("seat 1 (out-of-range index) command zone = %d, want 0 (skipped, not crash)", got)
	}
	// Only one commander survives validation, so CmdDamage is sized 1.
	for p := state.PlayerID(0); p < 2; p++ {
		if len(e.G.Players[p].CmdDamage) != 1 {
			t.Fatalf("seat %d CmdDamage sized %d, want 1", p, len(e.G.Players[p].CmdDamage))
		}
	}
}

// TestCommandZoneIsPublic is the mutation guard for Hidden(): the command
// zone is public information, visible to every seat -- a change that makes
// it Hidden redacts it from every other viewer.
func TestCommandZoneIsPublic(t *testing.T) {
	t.Parallel()
	if state.ZCommand.Hidden() {
		t.Fatal("ZCommand must not be Hidden: the command zone is public information")
	}
	if state.ZCommand.String() != "command" {
		t.Fatalf("ZCommand.String() = %q, want \"command\"", state.ZCommand.String())
	}
	if !state.ZCommand.Valid() {
		t.Fatal("ZCommand must be a Valid zone")
	}
	if state.ZCommand <= state.ZStack {
		t.Fatalf("ZCommand must be appended AFTER ZStack (zone %d > %d) so existing zone values are unchanged",
			state.ZCommand, state.ZStack)
	}
}
