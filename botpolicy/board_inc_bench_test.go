package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// benchPosition plays a bot game to its first main-phase decision at or after
// decision n with a populated board, returning the engine there.
func benchPosition(b *testing.B, n int) (*rules.Engine, *decision.Decision) {
	reg := testutil.CorpusRegistry(b)
	legacy := testutil.LegacyDeckNames()
	e := rules.New(rules.Config{Seed: 77, Names: legacy[:2],
		Decks: [][]*cards.Card{testutil.RepoDeck(b, reg, legacy[0]), testutil.RepoDeck(b, reg, legacy[1])}, Tokens: reg.Tokens})
	e.Advance()
	board := NewBoard(2)
	r := rng(77)
	for i := 0; !e.G.Over && e.Pending() != nil; i++ {
		d := e.Pending()
		if i >= n && e.G.Step.IsMain() && d.Kind == decision.KPriority {
			return e, d
		}
		if err := e.Submit(Decide(BoardFromGameInto(e.G, e, d.Player, &board), d, r)); err != nil {
			b.Fatal(err)
		}
		e.Advance()
	}
	b.Fatal("game ended first")
	return nil, nil
}

// BenchmarkBoardFillReused refills one Board at an unchanged state: every
// derivation is reused, so this prices the table rebuild alone.
func BenchmarkBoardFillReused(b *testing.B) {
	saved := boardIncVerify
	boardIncVerify = false
	defer func() { boardIncVerify = saved }()
	e, d := benchPosition(b, 150)
	board := NewBoard(2)
	BoardFromGameInto(e.G, e, d.Player, &board)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BoardFromGameInto(e.G, e, d.Player, &board)
	}
}

// BenchmarkBoardFillLineageSwitch alternates two clones of one state, so every
// refill crosses a lineage and re-derives every row.
func BenchmarkBoardFillLineageSwitch(b *testing.B) {
	saved := boardIncVerify
	boardIncVerify = false
	defer func() { boardIncVerify = saved }()
	e, d := benchPosition(b, 150)
	c := e.Clone()
	board := NewBoard(2)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i&1 == 0 {
			BoardFromGameInto(e.G, e, d.Player, &board)
		} else {
			BoardFromGameInto(c.G, c, d.Player, &board)
		}
	}
}

// BenchmarkDecideMain prices Decide at a main-phase priority decision.
func BenchmarkDecideMain(b *testing.B) {
	e, d := benchPosition(b, 150)
	board := NewBoard(2)
	bd := BoardFromGameInto(e.G, e, d.Player, &board)
	r := rng(1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Decide(bd, d, r)
	}
}
