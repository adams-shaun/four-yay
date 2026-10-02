package mzplay

import (
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/azmcts/clairvoyant"
	"github.com/adams-shaun/gorge/internal/mzbridge"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

func TestMain(m *testing.M) {
	clairvoyant.AllowClairvoyant()
	os.Exit(m.Run())
}

const testVocab = "dim\t64\nA\t0\tPass\nA\t1\tCast Grizzly Bears\nT\t0\tStop Choosing\nT\t1\tPlayerA\nT\t2\tPlayerB\n"

func testSetup(t testing.TB, a, b string, seed uint64, budget int) GameSetup {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	da, err := testutil.LoadRepoDeck(reg, a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := testutil.LoadRepoDeck(reg, b)
	if err != nil {
		t.Fatal(err)
	}
	vocab, err := mzbridge.ParseVocab(strings.NewReader(testVocab))
	if err != nil {
		t.Fatal(err)
	}
	seat := func(deck []*cards.Card, name string) SeatSetup {
		return SeatSetup{Deck: deck, DeckName: name, Budget: budget, BackpropDiscount: 0.99, Lambda: 0.95, Leaf: OfflineLeaf}
	}
	return GameSetup{Seed: seed, Seats: [2]SeatSetup{seat(da, a), seat(db, b)}, GoesFirst: "random", MaxTurns: 50,
		Tokens: reg.Tokens, Vocab: vocab}
}

// TestEvaluator3 pins the port of GameStateEvaluator3 against a direct
// reading of the formula off the projected view at every decision of a bot
// game, and against hand-computed values.
func TestEvaluator3(t *testing.T) {
	gs := testSetup(t, "mono-green-stompy", "mono-white-equipment", 41, 1)
	cfg := rules.Config{Seed: gs.Seed, Names: []string{"a", "b"}, Decks: [][]*cards.Card{gs.Seats[0].Deck, gs.Seats[1].Deck}, Tokens: gs.Tokens}
	e := rules.New(cfg)
	e.Advance()

	// The opening position: equal life, equal hands, no permanents.
	if v := Evaluator3(e, 0); v != 0 {
		t.Fatalf("opening position: %v, want 0", v)
	}
	if v := OfflineLeaf(e, 1); v != 0.5 {
		t.Fatalf("opening position as a leaf: %v, want 0.5", v)
	}
	// Life is worth 0.6 a point: 10 life down is tanh(-6/15). A throwaway
	// clone carries the test-only write.
	c := e.Clone()
	c.G.Players[0].Life = 10
	if got, want := Evaluator3(c, 0), math.Tanh(-6.0/15); math.Abs(got-want) > 1e-15 {
		t.Fatalf("10 life down: %v, want %v", got, want)
	}
	// Life below zero counts as zero: -5 and 0 read alike.
	c.G.Players[0].Life = -5
	neg := Evaluator3(c, 0)
	c.G.Players[0].Life = 0
	if zero := Evaluator3(c, 0); neg != zero || math.Abs(zero-math.Tanh(-12.0/15)) > 1e-15 {
		t.Fatalf("life -5 %v, life 0 %v, want both tanh(-12/15)", neg, zero)
	}
	if h := harmonic(7); math.Abs(h-(1+1.0/2+1.0/3+1.0/4+1.0/5+1.0/6+1.0/7)) > 1e-15 || harmonic(0) != 0 {
		t.Fatalf("harmonic(7) = %v", h)
	}

	viaView := func(e *rules.Engine, p state.PlayerID) float64 {
		v := view.ProjectFor(e.G, e, p, view.Omniscient, e.Pending())
		res := func(pv view.PlayerView) float64 {
			s := 0.6*math.Max(0, float64(pv.Life)) + harmonic(pv.HandSize)
			for _, c := range pv.Battlefield {
				s++
				if card, ok := testutil.CorpusRegistry(t).Lookup(c.Name); ok && !c.FaceDown {
					s += float64(card.Faces[0].Cmc())
				}
			}
			return s
		}
		return math.Tanh((res(v.Players[p]) - res(v.Players[1-p])) / 15)
	}
	rngs := searchprobe.BotRandoms(cfg.Seed, 2)
	board := botpolicy.NewBoard(2)
	checked, withPerms := 0, 0
	for step := 0; step < 4000 && !e.G.Over; step++ {
		d := e.Pending()
		if d == nil {
			break
		}
		for p := state.PlayerID(0); p < 2; p++ {
			got, want := Evaluator3(e, p), viaView(e, p)
			if math.Abs(got-want) > 1e-12 {
				t.Fatalf("step %d, seat %d: %v, the view's formula gives %v", step, p, got, want)
			}
		}
		if a, b := Evaluator3(e, 0), Evaluator3(e, 1); math.Abs(a+b) > 1e-15 {
			t.Fatalf("step %d: the two seats' values %v and %v are not opposite", step, a, b)
		}
		checked++
		if len(e.G.Zone(state.ZBattlefield, 0))+len(e.G.Zone(state.ZBattlefield, 1)) > 4 {
			withPerms++
		}
		if err := e.Submit(botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &board), d, rngs[d.Player])); err != nil {
			t.Fatal(err)
		}
	}
	if checked < 200 || withPerms < 100 {
		t.Fatalf("precondition: %d positions checked, %d with permanents", checked, withPerms)
	}
	if e.G.Over && !e.G.Draw {
		if w, l := Evaluator3(e, e.G.Winner), Evaluator3(e, 1-e.G.Winner); w != 1 || l != -1 {
			t.Fatalf("finished game: winner %v, loser %v", w, l)
		}
	}
}

// TestPlayGameRecordsAndIsReproducible plays one whole small-budget game
// twice from one seed: the two results are identical, every record is well
// formed, and the value labels are the TD-lambda pass over the root scores.
func TestPlayGameRecordsAndIsReproducible(t *testing.T) {
	gs := testSetup(t, "mono-green-stompy", "mono-white-equipment", 7, 12)
	a, err := PlayGame(gs)
	if err != nil {
		t.Fatal(err)
	}
	b, err := PlayGame(gs)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("the same setup played two different games")
	}
	if a.First < 0 || a.Turns < 2 {
		t.Fatalf("first %d, turns %d", a.First, a.Turns)
	}
	if a.Winner == NoWinner && a.Turns < gs.MaxTurns {
		t.Fatalf("no winner at turn %d of %d", a.Turns, gs.MaxTurns)
	}
	st := a.Stats
	t.Logf("turns %d winner %d first %d: %d submits, %d trivial, %d searched, %d bot; rows A %d B %d (priority %d, target %d, use %d); sims %d/%d; action hits %d/%d, target hits %d/%d",
		a.Turns, a.Winner, a.First, st.Submits, st.Trivial, st.Searches, st.Bot, len(a.Rows[0]), len(a.Rows[1]),
		st.RowsPriority, st.RowsTarget, st.RowsUse, st.Completed, st.Simulations, st.ActionHits, st.ActionCands, st.TargetHits, st.TargetCands)
	if st.Searches < 10 || st.RowsPriority < 5 || st.RowsUse == 0 {
		t.Fatalf("too little was searched: %+v", st)
	}
	if st.RowsPriority+st.RowsTarget+st.RowsUse != len(a.Rows[0])+len(a.Rows[1]) {
		t.Fatalf("row counts %d+%d+%d != %d+%d", st.RowsPriority, st.RowsTarget, st.RowsUse, len(a.Rows[0]), len(a.Rows[1]))
	}
	for seat, rows := range a.Rows {
		if len(rows) == 0 {
			t.Fatalf("seat %d recorded nothing", seat)
		}
		for i, r := range rows {
			if len(r.IDs) < 50 || r.IDs[0] != SentinelID {
				t.Fatalf("seat %d row %d: %d ids, first %d", seat, i, len(r.IDs), r.IDs[0])
			}
			for k := 1; k < len(r.IDs); k++ {
				if r.IDs[k] <= r.IDs[k-1] {
					t.Fatalf("seat %d row %d: ids not ascending at %d", seat, i, k)
				}
			}
			sum := float32(0)
			for _, p := range r.Policy {
				if p.Index < 0 || p.Index >= gs.Vocab.Dim() || p.Visits <= 0 {
					t.Fatalf("seat %d row %d: policy entry %+v", seat, i, p)
				}
				if r.Type == mzbridge.ChooseUse && p.Index > 1 {
					t.Fatalf("seat %d row %d: a yes/no record with index %d", seat, i, p.Index)
				}
				sum += p.Visits
			}
			if sum < 1 || sum > float32(gs.Seats[seat].Budget) {
				t.Fatalf("seat %d row %d: policy counts sum to %v with a budget of %d", seat, i, sum, gs.Seats[seat].Budget)
			}
			if r.Score < -1 || r.Score > 1 || r.Value < -1 || r.Value > 1 {
				t.Fatalf("seat %d row %d: score %v value %v", seat, i, r.Score, r.Value)
			}
		}
		// The labels are the backward pass from the game's result.
		want := append([]Row(nil), rows...)
		LabelValues(want, SeatWon(seat, a.Winner), gs.Seats[seat].Lambda)
		for i := range rows {
			if rows[i].Value != want[i].Value {
				t.Fatalf("seat %d row %d: value %v, the backward pass gives %v", seat, i, rows[i].Value, want[i].Value)
			}
		}
		last := rows[len(rows)-1]
		end := -1.0
		if SeatWon(seat, a.Winner) {
			end = 1
		}
		if math.Abs(last.Value-(0.95*end+0.05*last.Score)) > 1e-12 {
			t.Fatalf("seat %d: last value %v from score %v and result %v", seat, last.Value, last.Score, end)
		}
		// Opening hand (7) plus one draw per own turn at least.
		if len(a.Drawn[seat]) < 7 {
			t.Fatalf("seat %d drew %d cards", seat, len(a.Drawn[seat]))
		}
	}
	// Successive per-creature records of one declaration differ (the
	// micro-decision history and the decision text), so no two consecutive
	// yes/no records share an id set.
	for seat, rows := range a.Rows {
		for i := 1; i < len(rows); i++ {
			if rows[i].Type == mzbridge.ChooseUse && rows[i-1].Type == mzbridge.ChooseUse && reflect.DeepEqual(rows[i].IDs, rows[i-1].IDs) {
				t.Fatalf("seat %d rows %d and %d: two attacker records with one id set", seat, i-1, i)
			}
		}
	}
	// A different seed is a different game.
	gs.Seed = 8
	c, err := PlayGame(gs)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(a.Rows, c.Rows) {
		t.Fatal("two seeds played one game")
	}
}

// TestPlayGameGoesFirstAndTurnCap: goes_first forces the starting seat, and
// a game still running when max_turns ends has no winner -- which labels
// seat A's records toward -1 and seat B's toward +1 (upstream's quirk).
func TestPlayGameGoesFirstAndTurnCap(t *testing.T) {
	for _, tc := range []struct {
		first string
		want  int
	}{{"player_a", 0}, {"player_b", 1}} {
		gs := testSetup(t, "mono-green-stompy", "mono-white-equipment", 7, 4)
		gs.GoesFirst, gs.MaxTurns = tc.first, 3
		r, err := PlayGame(gs)
		if err != nil {
			t.Fatal(err)
		}
		if r.First != tc.want {
			t.Fatalf("goes_first %s: seat %d took turn 1", tc.first, r.First)
		}
		if r.Winner != NoWinner || r.Turns != 3 {
			t.Fatalf("goes_first %s: winner %d at turn %d, want no winner at the cap of 3", tc.first, r.Winner, r.Turns)
		}
		for seat, sign := range []float64{-1, 1} {
			rows := r.Rows[seat]
			if len(rows) == 0 {
				t.Fatalf("seat %d recorded nothing in 3 turns", seat)
			}
			last := rows[len(rows)-1]
			if math.Abs(last.Value-(0.95*sign+0.05*last.Score)) > 1e-12 {
				t.Fatalf("seat %d: last value %v, want the %+v-terminated label", seat, last.Value, sign)
			}
		}
	}
}

// TestPlayGameAbort: the max_minutes hook ends the game as a failure.
func TestPlayGameAbort(t *testing.T) {
	gs := testSetup(t, "mono-green-stompy", "mono-white-equipment", 7, 4)
	n := 0
	gs.Abort = func() bool { n++; return n > 20 }
	if _, err := PlayGame(gs); err != ErrAborted {
		t.Fatalf("aborted game: %v, want ErrAborted", err)
	}
}
