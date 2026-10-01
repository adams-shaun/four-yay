package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
)

// The search loop's three per-simulation hot paths on WARM scratch, the
// shape a worker reaches after its first simulation: a clone into a spent
// clone's Spare, a priority offer walk on an engine that has walked before,
// and a bot board refilled into a reused Board. allocs/op is the number the
// allocation-churn work drives down.

// BenchmarkCloneIntoWarm is root.CloneInto into the previous clone's
// Release, the clone alone (no simulation steps).
func BenchmarkCloneIntoWarm(b *testing.B) {
	benchWithoutVerify(b)
	c := cloneCycleRoots[len(cloneCycleRoots)-1]
	root := cloneCycleRoot(b, c.opp, c.turn)
	var sp Spare
	sp = root.CloneInto(&sp).Release()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sp = root.CloneInto(&sp).Release()
	}
}

// BenchmarkLegalWalkWarm repeats the priority offer walk at an unchanged
// board on one engine (its scratch and memo tables already grown).
func BenchmarkLegalWalkWarm(b *testing.B) {
	benchWithoutVerify(b)
	c := cloneCycleRoots[len(cloneCycleRoots)-1]
	e := cloneCycleRoot(b, c.opp, c.turn)
	p := e.Pending().Player
	e.legalActions(p)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.legalActions(p)
	}
}

// BenchmarkBoardRefillWarm refills one reused Board from the root
// (BoardFromGameInto), the bot's per-decision board in the search env.
func BenchmarkBoardRefillWarm(b *testing.B) {
	benchWithoutVerify(b)
	c := cloneCycleRoots[len(cloneCycleRoots)-1]
	e := cloneCycleRoot(b, c.opp, c.turn)
	p := e.Pending().Player
	board := botpolicy.NewBoard(len(e.G.Players))
	botpolicy.BoardFromGameInto(e.G, e, p, &board)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		botpolicy.BoardFromGameInto(e.G, e, p, &board)
	}
}
