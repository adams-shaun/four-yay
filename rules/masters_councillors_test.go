package rules

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestMastersCouncillorsGraveyardCountStatic is the regression for the Forge
// static
//
//	S:Mode$ Continuous | Affected$ Card.Self | AddPower$ X
//	SVar:X:PlayerCountPlayers$HasPropertyHasCardsInGraveyard_Card_GE7/Times.2
//
// -- "This creature gets +2/+0 for each graveyard with seven or more cards in
// it." CR 604.3 makes this a static ability generating a continuous effect;
// the PlayerCountPlayers$ head enumerates PLAYERS whose graveyard holds at
// least seven cards, times two (the /Times.2 operand). The engine must read
// the HasPropertyHasCardsInGraveyard_<spec>_GE<n> head from the continuous
// static and compute X = 2 per qualifying graveyard.
//
// The graveyard is filled through the REAL event path (one emitted MoveZone
// per card, exactly as a live game does) rather than by poking zone slices.
// That matters: the static effect list is memoized on the log head, so an
// eventless fill that later stales only one of the derived epochs would let a
// broken read pass unnoticed. Emitting the moves exercises the read the way
// the game does and would fail if the head stopped resolving.
func TestMastersCouncillorsGraveyardCountStatic(t *testing.T) {
	t.Parallel()
	testutil.CorpusRegistry(t)
	e := handEngine(t, corpusCard(t, "Master's Councillors"))
	id := onBoardCard(t, e, 0, corpusCard(t, "Master's Councillors"))

	// Preconditions: the creature is really on the battlefield, the graveyard
	// starts empty, and the printed base is 1/3 -- so a final 3 cannot be the
	// base value masquerading as the pumped value.
	if o := e.G.Obj(id); o == nil || o.Face() == nil ||
		o.Face().Name != "Master's Councillors" || o.Zone != state.ZBattlefield {
		t.Fatalf("Master's Councillors precondition: %+v", o)
	}
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 0 {
		t.Fatalf("graveyard precondition: %d cards, want 0", got)
	}
	if p, tf := e.Derived(id).Power, e.Derived(id).Toughness; p != 1 || tf != 3 {
		t.Fatalf("printed precondition: %d/%d, want 1/3", p, tf)
	}

	// The AddPower$ X static must actually be registered: without it the
	// power check below would be vacuous.
	foundStatic := false
	for _, ce := range e.active() {
		if ce.Source == id && ce.AddPowerExpr == "X" {
			foundStatic = true
			break
		}
	}
	if !foundStatic {
		t.Fatal("Master's Councillors AddPower$ X continuous static was not registered")
	}

	// Six cards is below the threshold, so the static pays nothing yet.
	fillGraveyardByEvents(t, e, 0, 6)
	if p := e.Derived(id).Power; p != 1 {
		t.Fatalf("power with a six-card graveyard = %d, want 1 (threshold is seven)", p)
	}

	// The seventh card crosses the threshold: one qualifying graveyard, +2.
	fillGraveyardByEvents(t, e, 0, 1)
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 7 {
		t.Fatalf("graveyard precondition: %d cards, want 7", got)
	}
	if p := e.Derived(id).Power; p != 3 {
		t.Fatalf("Master's Councillors power = %d, want 3 (base 1 + 2 for one graveyard holding >=7 cards, CR 604.3)", p)
	}
}

// fillGraveyardByEvents moves n fresh cards into seat p's graveyard one
// emitted MoveZone at a time, the way a live game does, so the derived memo
// (keyed on the log head) refreshes from real game state.
func fillGraveyardByEvents(t *testing.T, e *Engine, p state.PlayerID, n int) {
	t.Helper()
	filler := card(t, "Name:Set-Audit Filler\nTypes:Instant\nOracle:x\n")
	for i := 0; i < n; i++ {
		o := e.G.AddObject(filler, p)
		e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID,
			From: state.ZLibrary, To: state.ZGraveyard, Player: p})
	}
}

// TestMastersCouncillorsGraveyardCountCensus names every corpus card that
// carries a HasCardsInGraveyard head, so the family the brief calls out is
// pinned: the brief's own census (5 files) is this test's expected list, and a
// new carrier or a lost one fails here rather than silently.
func TestMastersCouncillorsGraveyardCountCensus(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", ".cards", "cardsfolder")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Skipf("corpus not present at %s: %v", root, err)
	}
	var hits []string
	for _, dir := range entries {
		if !dir.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, dir.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", dir.Name(), err)
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".txt") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(root, dir.Name(), f.Name()))
			if err != nil {
				t.Fatalf("read %s: %v", f.Name(), err)
			}
			if strings.Contains(string(b), "HasCardsInGraveyard") {
				hits = append(hits, f.Name())
			}
		}
	}
	sort.Strings(hits)
	want := []string{
		"keeper_of_the_dead.txt",
		"masters_councillors.txt",
		"mysterious_stranger.txt",
		"the_master_of_lake_town.txt",
		"vantress_gargoyle.txt",
	}
	if strings.Join(hits, ",") != strings.Join(want, ",") {
		t.Fatalf("HasCardsInGraveyard carriers = %v, want %v", hits, want)
	}
}
