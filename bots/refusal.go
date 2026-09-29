package bots

import (
	"math/rand/v2"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/view"
)

// RefusalAnswerer is a seat whose answers the engine may refuse (a
// whole-declaration constraint the options do not publish). The host asks it
// once again before the shared fallbacks. refused is the intent the engine
// rejected; the returned intent is the seat's second attempt.
type RefusalAnswerer interface {
	AnswerRefused(v view.View, d decision.Decision, refused decision.Intent) decision.Intent
}

// Fallbacks are the ladder's non-seat rungs, in order: the minimal answer
// (pass at priority, else botpolicy.Clamp of the empty answer), then
// botpolicy.Decide on brd with PCG(d.Seq, seatIdx+1). It is exactly
// cmd/botbench/spellbench.go:262-283 lifted out, and deterministic.
//
// The returned intents carry d's Seq and Player, so a caller may Submit each
// in order without repairing them. brd is the deciding seat's board, built by
// the caller the same way a BoardSeat's board is built.
func Fallbacks(d *decision.Decision, brd botpolicy.Board, seatIdx int) []decision.Intent {
	minimal := decision.Intent{Seq: d.Seq, Player: d.Player}
	if d.Kind == decision.KPriority {
		for _, o := range d.Options {
			if o.Kind == "pass" {
				minimal.Choices = []int{o.Index}
				break
			}
		}
	} else {
		minimal = botpolicy.Clamp(d, minimal)
	}
	bot := botpolicy.Decide(brd, d, rand.New(rand.NewPCG(d.Seq, uint64(seatIdx)+1)))
	bot.Seq, bot.Player = d.Seq, d.Player
	return []decision.Intent{minimal, bot}
}
