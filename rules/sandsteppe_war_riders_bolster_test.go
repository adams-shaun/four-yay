package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The bolster1 end-to-end pin on Sandsteppe War Riders' REAL compiled corpus
// card: at the beginning of combat on your turn, bolster X, where X is the
// number of differently named artifact tokens you control. Two primitives are
// pinned together -- the bolster keyword action (CR 701.36: choose a creature
// with the least toughness among creatures you control and put X +1/+1
// counters on it; a tie at the minimum is a real election) and the
// token$DifferentCardNames set-level count (distinct face names, not token
// count). Sandsteppe War Riders is in NO repo deck and NO legacy golden deck,
// so no chain head depends on this card. The fixtures are built from compiled
// corpus cards and real corpus token scripts only; no Forge script text is
// committed here.
//
// Tokens minted are the real corpus artifact tokens (c_a_treasure_sac,
// c_a_clue_draw, c_a_food_sac -- faces "Treasure Token", "Clue Token", "Food
// Token"), so the differently-named count is exercised on genuine token
// objects, and the same-name duplicate is a second Treasure mint.

// sandsteppeEngine seats seat 0 a 40-card deck containing Sandsteppe War
// Riders and `lions` Savannah Lions (2/1 -- the least-toughness creatures the
// bolster must pick over the 4/4 rider), puts all of them on the battlefield,
// and returns the engine plus the lions' ids. seatZeroStart pins the toss to
// seat 0 so the BeginCombat trigger of turn 1 is seat 0's own.
func sandsteppeEngine(t *testing.T, reg *cards.Registry, lions int) (*Engine, state.ObjID, []state.ObjID) {
	t.Helper()
	rider := searchCorpusCard(t, reg, "Sandsteppe War Riders")
	lion := searchCorpusCard(t, reg, "Savannah Lions")
	deck := []*cards.Card{rider}
	for i := 0; i < lions; i++ {
		deck = append(deck, lion)
	}
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	for i := 0; i < 8; i++ {
		deck = append(deck, forest, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, searchCorpusCard(t, reg, "Grizzly Bears"))
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 9214, Names: []string{"bolsterer", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	riderID := searchMoveByName(t, e, "Sandsteppe War Riders", state.ZBattlefield)
	lionIDs := make([]state.ObjID, 0, lions)
	for i := 0; i < lions; i++ {
		lionIDs = append(lionIDs, searchMoveByName(t, e, "Savannah Lions", state.ZBattlefield))
	}
	return e, riderID, lionIDs
}

// mintCorpusToken mints one real corpus token (by registry key) onto seat 0's
// battlefield with a logged TokenCreate, and returns the minted id.
func mintCorpusToken(t *testing.T, e *Engine, key string) state.ObjID {
	t.Helper()
	if e.G.Tokens == nil || e.G.Tokens[key] == nil {
		t.Fatalf("missing corpus token %q", key)
	}
	want := e.G.NextID
	e.emit(events.Event{Kind: events.TokenCreate, Player: 0, Text: key})
	e.pending = nil
	e.priorityRound()
	if e.G.Obj(want) == nil {
		t.Fatalf("TokenCreate for %q minted nothing", key)
	}
	return want
}

func sandsteppeRegistry(t *testing.T) *cards.Registry {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	if reg == nil {
		t.Skip("bolster corpus unavailable")
	}
	return reg
}

// driveToBeginCombat drives the (seat-0) turn to the beginning of combat and
// then lets the War Riders' Phase trigger resolve: the trigger sits on the
// stack through two priority passes, and the resolve returns the decision
// that answered it -- a counter_pick election when the least toughness is
// tied, otherwise the next ordinary priority ask.
func driveToBeginCombat(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepBeginCombat)
	for i := 0; i < 8; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no pending decision while resolving the bolster trigger")
		}
		if d.Kind != decision.KPriority {
			return d
		}
		if e.G.Step != state.StepBeginCombat {
			// The trigger resolved (or nothing was left to resolve) and play
			// moved on: done.
			return d
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	t.Fatal("the bolster trigger never resolved within the pass budget")
	return nil
}

// TestSandsteppeWarRidersBolstersDistinctTokenCount: three DIFFERENTLY named
// artifact tokens (a fourth Treasure duplicating the first's name adds
// nothing) give X = 3, and the bolster places 3 +1/+1 counters on the
// least-toughness creature -- the 2/1 Lions, not the 4/4 rider -- with no
// election posed (a unique minimum is no choice).
func TestSandsteppeWarRidersBolstersDistinctTokenCount(t *testing.T) {
	t.Parallel()
	reg := sandsteppeRegistry(t)
	e, riderID, lionIDs := sandsteppeEngine(t, reg, 1)
	for _, key := range []string{"c_a_treasure_sac", "c_a_treasure_sac", "c_a_clue_draw", "c_a_food_sac"} {
		mintCorpusToken(t, e, key)
	}
	driveToBeginCombat(t, e)
	if n := e.G.Obj(lionIDs[0]).Counter("P1P1"); n != 3 {
		t.Fatalf("Savannah Lions P1P1 = %d, want 3 (bolster X = 3 distinct token names)", n)
	}
	if n := e.G.Obj(riderID).Counter("P1P1"); n != 0 {
		t.Fatalf("Sandsteppe War Riders P1P1 = %d, want 0 (the 2/1 Lions are the least toughness)", n)
	}
}

// TestSandsteppeWarRidersSameNameTokensCountOnce: two Treasures and one Clue
// are two DIFFERENT names, not three tokens -- X = 1 after the two-token
// Treasure duplication... (two distinct names: Treasure, Clue).
func TestSandsteppeWarRidersSameNameTokensCountOnce(t *testing.T) {
	t.Parallel()
	reg := sandsteppeRegistry(t)
	e, _, lionIDs := sandsteppeEngine(t, reg, 1)
	for _, key := range []string{"c_a_treasure_sac", "c_a_treasure_sac", "c_a_clue_draw"} {
		mintCorpusToken(t, e, key)
	}
	driveToBeginCombat(t, e)
	if n := e.G.Obj(lionIDs[0]).Counter("P1P1"); n != 2 {
		t.Fatalf("Savannah Lions P1P1 = %d, want 2 (two distinct token names: Treasure, Clue)", n)
	}
}

// TestSandsteppeWarRidersTieElection: two Savannah Lions tie at toughness 1,
// so CR 701.36's "choose" is a real election -- a KChoose over the tied
// creatures, answered to the second lion, which takes the counters while the
// first takes none.
func TestSandsteppeWarRidersTieElection(t *testing.T) {
	t.Parallel()
	reg := sandsteppeRegistry(t)
	e, _, lionIDs := sandsteppeEngine(t, reg, 2)
	for _, key := range []string{"c_a_treasure_sac", "c_a_clue_draw", "c_a_food_sac"} {
		mintCorpusToken(t, e, key)
	}
	driveToBeginCombat(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "counter_pick" {
		t.Fatalf("a least-toughness tie must pose a counter_pick election, got %+v", d)
	}
	if len(d.Options) != 2 {
		t.Fatalf("election options = %d, want exactly the two tied Lions", len(d.Options))
	}
	// Answer for the SECOND lion in the option list.
	second := d.Options[1]
	if second.Obj != lionIDs[1] {
		t.Fatalf("option[1] = obj %d, want the second lion %d", second.Obj, lionIDs[1])
	}
	submitChoices(t, e, second.Index)
	if n := e.G.Obj(lionIDs[1]).Counter("P1P1"); n != 3 {
		t.Fatalf("chosen lion P1P1 = %d, want 3", n)
	}
	if n := e.G.Obj(lionIDs[0]).Counter("P1P1"); n != 0 {
		t.Fatalf("unchosen lion P1P1 = %d, want 0", n)
	}
}
