package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestLoseManaCorpusCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	var got []string
	for _, card := range reg.Cards {
		for _, face := range card.Faces {
			for _, repl := range face.Repls {
				if repl.Event == "LoseMana" {
					got = append(got, face.Name)
				}
			}
		}
	}
	slices.Sort(got)
	want := []string{"Horizon Stone", "Kruphix, God of Horizons", "Omnath, Locus of All", "Omnath, Locus of the Void", "Ozai, the Phoenix King"}
	if !slices.Equal(got, want) {
		t.Fatalf("LoseMana carrier census = %v, want %v", got, want)
	}
}

func TestHorizonStoneConvertsManaAtBoundary(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := realCardEngine(t, reg, 8675309, "Horizon Stone")
	if e.G.Obj(ids[0]) == nil || e.G.Obj(ids[0]).Zone != state.ZBattlefield {
		t.Fatal("Horizon Stone must be on the battlefield before the boundary")
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Amount: 2, Counter: "R"})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Amount: 1, Counter: "R", Text: events.ManaPersistentText("")})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 1, Amount: 1, Counter: "R"})
	if e.G.Players[0].Pool[state.MR] != 3 || e.G.Players[1].Pool[state.MR] != 1 {
		t.Fatal("mana-pool precondition not established")
	}
	e.finishStepBoundary(state.StepMain1, state.StepBeginCombat)
	if got := e.G.Players[0].Pool; got[state.MC] != 2 || got[state.MR] != 1 || e.G.Players[0].PersistentMana[state.MR] != 1 {
		t.Fatalf("Horizon Stone pool after boundary = %v, want two colorless", got)
	}
	if got := e.G.Players[1].Pool.Total(); got != 0 {
		t.Fatalf("opponent's unmodified pool = %d, want cleared", got)
	}
	replayCheck(t, e, cfg)
}

func TestOzaiManaPoolThresholdKeywords(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := realCardEngine(t, reg, 8675310, "Ozai, the Phoenix King")
	ozai := e.G.Obj(ids[0])
	if ozai == nil || ozai.Zone != state.ZBattlefield {
		t.Fatal("Ozai must be on the battlefield before checking its static")
	}
	for i := 0; i < 5; i++ {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Amount: 1, Counter: "G"})
	}
	if e.G.Players[0].Pool.Total() != 5 || e.HasKeyword(ids[0], "Flying") || e.HasKeyword(ids[0], "Indestructible") {
		t.Fatal("five mana must be below Ozai's six-mana threshold")
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Amount: 1, Counter: "G"})
	if e.G.Players[0].Pool.Total() != 6 || !e.HasKeyword(ids[0], "Flying") || !e.HasKeyword(ids[0], "Indestructible") {
		t.Fatal("six unspent mana must grant Ozai flying and indestructible")
	}
	replayCheck(t, e, cfg)
}

func TestLoseManaWithoutCarrierStillClears(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, _ := realCardEngine(t, reg, 8675311)
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Amount: 1, Counter: "R"})
	e.finishStepBoundary(state.StepMain1, state.StepBeginCombat)
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("ordinary boundary left %d mana", got)
	}
	replayCheck(t, e, cfg)
}

func TestLoseManaVocabularyPinned(t *testing.T) {
	if got := cards.ReplEventOf("LoseMana"); got != cards.ReplLoseMana {
		t.Fatalf("LoseMana code = %d, want %d", got, cards.ReplLoseMana)
	}
	if cards.ReplLoseMana != 26 {
		t.Fatalf("LoseMana ordinal = %d, want appended code 26", cards.ReplLoseMana)
	}
}
