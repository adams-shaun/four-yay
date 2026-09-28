package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Path of Discovery end to end (task explore1, the brief's named deck carrier):
// "Whenever a creature you control enters, it explores." — an Enchantment
// whose ChangesZone trigger body is `DB$ Explore | Defined$ TriggeredCardLKICopy`,
// so the exploring creature is the one that just entered (the trigger's
// captured TriggerCard / Remembered), not the Enchantment itself. Both
// branches of CR 701.35a are pinned: a revealed land goes to the hand with no
// counter; a revealed nonland puts a +1/+1 counter on the entering creature and
// poses the "put it back or into your graveyard" election. Each case also
// folds the recorded scenario events onto a clone of the pre-scenario state
// through events.Apply and asserts the whole state.Game replays exactly
// (the regeneration_test/battle_test fold idiom — every mutation on this path
// is event-derived, never engine memory).
const pathOfDiscoveryCard = "Path of Discovery"

// pathOfDiscoveryDeck mirrors chainAskDeck but returns the Config so a replay
// can be built from it. The entering creature is named "Grizzly Bears" (the
// deck's filler).
func pathOfDiscoveryDeck(t *testing.T, reg *cards.Registry, fixtures ...string) (*Engine, Config) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := make([]*cards.Card, 0, 40)
	for _, name := range fixtures {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	for i := 0; i < 8; i++ {
		deck = append(deck, forest, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = forest
	}
	cfg := seatZeroStart(Config{Seed: 7712, Names: []string{"protagonist", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// pathOfDiscoveryBoard seats seat 0 with Path of Discovery plus one creature,
// casts the Enchantment, and returns:
//   - the engine (with the Enchantment on the battlefield, the creature in hand),
//   - a clone of the game taken BEFORE the scenario's own casting, and the
//     index into the log where the scenario's events begin (the fold idiom's
//     baseline), and
//   - the id of the creature still in hand.
//
// Every mutation the caller then makes is thus inside the folded window.
func pathOfDiscoveryBoard(t *testing.T, reg *cards.Registry) (*Engine, *state.Game, int, state.ObjID) {
	t.Helper()
	e, _ := pathOfDiscoveryDeck(t, reg, pathOfDiscoveryCard, "Grizzly Bears")
	pod := searchMoveByName(t, e, pathOfDiscoveryCard, state.ZHand)
	creature := searchMoveByName(t, e, "Grizzly Bears", state.ZHand)
	// Baseline: clone before the scenario's first effect, and remember where
	// the scenario's events start in the log.
	baseline := e.G.Clone()
	start := len(e.L.Events)
	addMana(t, e, 0, "GGGG")
	castCardByName(t, e, pod)
	passUntil(t, e, func() bool {
		o := e.G.Obj(pod)
		return o != nil && o.Zone == state.ZBattlefield
	})
	// Precondition: the Enchantment is on the battlefield. Everything below
	// depends on its ChangesZone trigger being live.
	if o := e.G.Obj(pod); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Path of Discovery not on the battlefield: %+v", o)
	}
	if o := e.G.Obj(creature); o == nil || o.Zone != state.ZHand {
		t.Fatalf("the entering creature must start in hand: %+v", o)
	}
	return e, baseline, start, creature
}

// assertFoldsExactly folds every event the engine emitted since start onto
// baseline through events.Apply and asserts the result equals the live game
// byte for byte. A divergence means some mutation on the path bypassed the
// event stream.
func assertFoldsExactly(t *testing.T, baseline *state.Game, start int, e *Engine) {
	t.Helper()
	for _, ev := range e.L.Events[start:] {
		events.Apply(baseline, ev)
	}
	if !reflect.DeepEqual(baseline, e.G) {
		t.Fatal("the explore scenario does not replay exactly from its events")
	}
}

// TestPathOfDiscoveryLandRevealGoesToHand is the land branch: the entering
// creature explores, the revealed Forest moves library -> hand, and no +1/+1
// counter is put on it. The record is the Amount-1 land shape.
func TestPathOfDiscoveryLandRevealGoesToHand(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, baseline, start, cr := pathOfDiscoveryBoard(t, reg)
	// A land on top means the explore takes the no-election shape.
	top := arrangeLibraryTop(t, e, "Forest")
	forest := top[0]

	addMana(t, e, 0, "GG")
	castCardByName(t, e, cr)
	// The bear enters; Path of Discovery's trigger fires and the explore runs.
	passUntil(t, e, func() bool { return len(exploreRecords(e)) == 1 })

	// Precondition: the explored creature is the one that just entered, not
	// the Enchantment. If the trigger body resolved Defined$ to the wrong
	// object this fails before the branch checks below.
	recs := exploreRecords(e)
	if recs[0].Obj != cr {
		t.Fatalf("explorer = %d, want the entering creature %d (Defined$ TriggeredCardLKICopy)", recs[0].Obj, cr)
	}
	if recs[0].Amount != 1 || recs[0].Player != 0 {
		t.Fatalf("record = %+v, want the Amount-1 land shape for player 0", recs[0])
	}
	if len(recs[0].IDs) != 1 || recs[0].IDs[0] != forest {
		t.Fatalf("record IDs = %+v, want the arranged Forest %d", recs[0].IDs, forest)
	}
	if got := e.G.Obj(forest); got == nil || got.Zone != state.ZHand {
		t.Fatalf("revealed Forest zone = %+v, want the hand", got)
	}
	if got := counterOn(t, e, cr, "P1P1"); got != 0 {
		t.Fatalf("entering creature P1P1 counters = %d, want 0 on the land branch", got)
	}
	assertFoldsExactly(t, baseline, start, e)
}

// TestPathOfDiscoveryNonlandPutsCounterAndElection is the nonland branch: the
// revealed nonland poses the LCI election, the answered "back on top" keeps it
// there, and the +1/+1 counter lands on the entering creature.
func TestPathOfDiscoveryNonlandPutsCounterAndElection(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, baseline, start, cr := pathOfDiscoveryBoard(t, reg)
	top := arrangeLibraryTop(t, e, "Grizzly Bears")
	bear := top[0]

	addMana(t, e, 0, "GG")
	castCardByName(t, e, cr)
	// The nonland explore poses the destination election.
	de := passUntilAsk(t, e)
	if de == nil || de.Kind != decision.KChoose || de.ResumeKind != "explore" {
		t.Fatalf("election = %+v, want KChoose with ResumeKind explore", de)
	}
	if de.Min != 1 || de.Max != 1 || len(de.Options) != 2 {
		t.Fatalf("election bounds/options = %d..%d over %+v, want 1..1 over two", de.Min, de.Max, de.Options)
	}
	if de.Options[0].Kind != "graveyard" || de.Options[1].Kind != "top" {
		t.Fatalf("option order = %+v, want graveyard on 0 and top on 1", de.Options)
	}
	if de.Options[0].Obj != bear || de.Options[1].Obj != bear {
		t.Fatalf("options = %+v, want both naming the revealed bear %d", de.Options, bear)
	}
	submitChoices(t, e, de.Options[1].Index) // put it back on top

	if got := counterOn(t, e, cr, "P1P1"); got != 1 {
		t.Fatalf("entering creature P1P1 counters = %d, want 1 from the explore", got)
	}
	if got := e.G.Zone(state.ZLibrary, 0); len(got) == 0 || got[0] != bear {
		t.Fatalf("library top = %+v, want the bear put back on top", got)
	}
	recs := exploreRecords(e)
	if len(recs) != 1 || recs[0].Obj != cr || recs[0].Amount != 0 {
		t.Fatalf("records = %+v, want one Amount-0 record naming the entering creature", recs)
	}
	assertFoldsExactly(t, baseline, start, e)
}

// TestPathOfDiscoveryNonlandGraveyardElection is the other election arm: the
// revealed nonland is binned to the graveyard after the counter.
func TestPathOfDiscoveryNonlandGraveyardElection(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, baseline, start, cr := pathOfDiscoveryBoard(t, reg)
	top := arrangeLibraryTop(t, e, "Grizzly Bears")
	bear := top[0]

	addMana(t, e, 0, "GG")
	castCardByName(t, e, cr)
	de := passUntilAsk(t, e)
	if de == nil || de.ResumeKind != "explore" {
		t.Fatalf("election = %+v, want the explore resume ask", de)
	}
	submitChoices(t, e, de.Options[0].Index) // into the graveyard

	if got := counterOn(t, e, cr, "P1P1"); got != 1 {
		t.Fatalf("entering creature P1P1 counters = %d, want 1 from the explore", got)
	}
	if got := e.G.Obj(bear); got == nil || got.Zone != state.ZGraveyard {
		t.Fatalf("revealed bear zone = %+v, want the graveyard", got)
	}
	assertFoldsExactly(t, baseline, start, e)
}
