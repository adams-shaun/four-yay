package bots

import (
	"context"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// EnvSeat is a hosted seat that answers some decisions from host-built
// inputs richer than a View or Board. The host detects the interface; an
// Entry with Env set must build one.
type EnvSeat interface {
	seat.Seat
	// WantsEnv reports whether d should get an Env. It must be a pure
	// function of d; false costs the host no redeal.
	WantsEnv(d *decision.Decision) bool
	// DecideEnv answers from env. It must not retain env.Search.Engine or
	// env.Search.Feed past its return.
	DecideEnv(ctx context.Context, env Env, d decision.Decision) (decision.Intent, error)
}

// Env is one decision's host-built input.
type Env struct {
	View  view.View       // the actor's projection, exactly what a plain Seat gets
	Board botpolicy.Board // the actor's board (BoardFromGameInto)
	// Search.Setup is the declared public game. Search.Engine is the HONEST
	// ROOT (a hypothetical redeal of the live position), or nil when the
	// redeal refused. Search.Board equals Board. Search.Feed is the actor's
	// feed, read-only.
	Search      searchseat.Env
	RootRefused string // why Search.Engine is nil ("" when it is set)
}

// RootSeed derives one honest root's deal seed from the per-seat seed and the
// decision sequence number (BP-05, spec 2026-09-28-hosted-bot-packages §5.1).
// It is the one derivation the bench and the host share: the caller passes
// the seed the seat was built from (the game seed XOR the seat index plus
// one, the SeatCtor convention) and the deciding decision's sequence number,
// and searchseat.HonestRoot redeals the live position at that seed -- so a
// root is a pure function of the seat's observation feed and the decision
// index: replayable, and unrelated between decisions.
func RootSeed(seatSeed, seq uint64) [2]uint64 {
	return [2]uint64{
		mix64(seatSeed ^ (seq + 0x243f6a8885a308d3)),
		mix64(seatSeed + (seq+1)*0x13198a2e03707344),
	}
}

// mix64 is the splitmix64 finalizer (MurmurHash3's fmix64): avalanche so
// nearby (seatSeed, seq) pairs do not produce nearby deals.
func mix64(z uint64) uint64 {
	z ^= z >> 33
	z *= 0xff51afd7ed558ccd
	z ^= z >> 33
	z *= 0xc4ceb9fe1a85ec53
	z ^= z >> 33
	return z
}
