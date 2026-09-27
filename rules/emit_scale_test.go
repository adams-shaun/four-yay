package rules

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// tokenScaleBoard builds the mass-token-creation board the cardfuzz hang seed
// 6181111140895991800 reached: n Goblin creature tokens (Krenko, Mob Boss's
// doubling payoff), Krenko itself, and a Clown Car crewed into a creature by
// four live `Affected$ Card.Self` layer-4 effects (the crew's own type change,
// repeated so the self-only fast path's source list is non-trivial). The board
// is placed with onBoard (eventless, like every other direct setup in this
// package); the benchmark then emits one TokenCreate at a time, which is the
// per-event cost under measurement.
//
// Corpus cards are used so the faces carry their compiled trigger interests
// and zone summaries (trigger_zoneskip.go) exactly as a real match's do -- an
// inline `card()` with no registry binding reads as "unknown interest" and
// makes every object hot, which would measure a different (and wrong) path.
func tokenScaleBoard(tb testing.TB, n int) *Engine {
	tb.Helper()
	reg := testutil.CorpusRegistry(tb)
	goblin, ok := reg.Token("r_1_1_goblin")
	if !ok {
		tb.Skip("corpus has no r_1_1_goblin token script")
	}
	krenko, ok := reg.Lookup("Krenko, Mob Boss")
	if !ok {
		tb.Skip("corpus has no Krenko, Mob Boss")
	}
	car, ok := reg.Lookup("Clown Car")
	if !ok {
		tb.Skip("corpus has no Clown Car")
	}
	mountain, ok := reg.Lookup("Mountain")
	if !ok {
		tb.Skip("corpus has no Mountain")
	}
	decks := make([][]*cards.Card, 4)
	for i := range decks {
		decks[i] = make([]*cards.Card, 60)
		for j := range decks[i] {
			decks[i][j] = mountain
		}
	}
	cfg := Config{Seed: 6181111140895991800, Names: []string{"a", "b", "c", "d"},
		Decks:  decks,
		Tokens: map[string]*cards.Card{"r_1_1_goblin": goblin}}
	e := New(seatZeroStart(cfg))
	e.G.SetZone(state.ZHand, 0, nil)
	for i := 0; i < n; i++ {
		o := e.G.AddObject(goblin, 0)
		o.IsToken = true
		o.Zone = state.ZBattlefield
		o.SummonSick = true
		e.G.Clock++
		o.Timestamp = e.G.Clock
		e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), o.ID))
		if i == 0 {
			// Krenko sits among them (its {T} ability is why the doubling
			// happened); it carries a trigger but fires nothing on a
			// TokenCreate.
			ko := e.G.AddObject(krenko, 0)
			ko.Zone = state.ZBattlefield
			ko.SummonSick = true
			e.G.Clock++
			ko.Timestamp = e.G.Clock
			e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), ko.ID))
		}
	}
	co := e.G.AddObject(car, 0)
	co.Zone = state.ZBattlefield
	co.SummonSick = true
	e.G.Clock++
	co.Timestamp = e.G.Clock
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), co.ID))
	for i := 0; i < 4; i++ {
		e.AddContinuous(ContinuousEffect{Source: co.ID, Controller: 0, Affects: "Card.Self",
			Layer: LType, UntilEOT: true, AddTypes: []string{"Artifact", "Creature"}})
	}
	e.staticEpoch, e.activeEpoch, e.typesEpoch = -1, -1, -1
	e.refreshDerivedTypes()
	return e
}

// BenchmarkEmitTokenCreateOnLargeBoard measures one TokenCreate emitted onto a
// board that already holds n creatures. Per-event cost must stay flat as n
// grows (the emit path's whole-board scans are the cardfuzz hang's residual
// cost); this benchmark is the before/after witness.
func BenchmarkEmitTokenCreateOnLargeBoard(b *testing.B) {
	for _, n := range []int{4000, 8000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			e := tokenScaleBoard(b, n)
			benchWithoutVerify(b)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				e.emit(events.Event{Kind: events.TokenCreate, Player: 0, Text: "r_1_1_goblin"})
			}
		})
	}
}
