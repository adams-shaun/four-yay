//go:build manabrew

package mbtest

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/bench"
	manabrew "github.com/adams-shaun/gorge/internal/manabrew"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/protocol"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
)

// TestManaBrewReplayEquivalent covers scoping spec §8 item 5, both bullets,
// for a handful of seeded 2-seat games (kept small: HTTP and the golden
// NDJSON transcript are MB-11's job, not this ticket's):
//
//  1. "replay.Replay(cfg, log) reproduces the head": the game played once
//     through TranslatingSeat/MockClient produces a (Config, Log) that
//     replay.Replay reruns to the identical chain head -- the ordinary
//     replay-determinism guarantee, exercised here against a log the
//     ManaBrew mapping (not a bare bot) produced.
//  2. "The same seed played natively with the same underlying policy ...
//     yields the identical head": a second, independent game -- a fresh
//     engine, the same Config and seed, a fresh MockClient built from the
//     same seed -- played again through TranslatingSeat reaches the
//     identical head. "Natively" here means "in process, with no wire
//     transport in between" (HTTP/JSON is explicitly out of this ticket's
//     scope; MB-11's golden-transcript test is what proves the actual wire
//     encoding round-trips) -- so this is the reproducibility half of "the
//     adapter is a pure bijection on the answers it forwards": the SAME
//     seed and the SAME policy must not depend on anything but themselves,
//     including nothing left over from the first playthrough (no shared
//     mutable state, no ambient randomness).
func TestManaBrewReplayEquivalent(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	names := testutil.LegacyDeckNames()
	const seats = 2

	for _, seed := range []uint64{1, 2, 3, 4, 5} {
		playerNames := make([]string, seats)
		playerNames[0] = names[int(seed)%len(names)]
		playerNames[1] = names[(int(seed)+1)%len(names)]
		decks := make([][]*cards.Card, seats)
		decks[0] = testutil.RepoDeck(t, reg, playerNames[0])
		decks[1] = testutil.RepoDeck(t, reg, playerNames[1])
		cfg := rules.Config{Seed: seed, Names: playerNames, Decks: decks, Tokens: reg.Tokens, NameUniverse: reg.Cards}

		e1 := playOneReplayGame(t, cfg, seed)
		re, err := replay.Replay(e1.L, cfg)
		if err != nil {
			t.Fatalf("seed %d: replay.Replay: %v", seed, err)
		}
		if re.L.Head() != e1.L.Head() {
			t.Fatalf("seed %d: replay.Replay head %s != original head %s", seed, re.L.Head(), e1.L.Head())
		}

		e2 := playOneReplayGame(t, cfg, seed)
		if e2.L.Head() != e1.L.Head() {
			t.Fatalf("seed %d: second in-process playthrough head %s != first playthrough head %s (the adapter is not reproducible at a fixed seed)",
				seed, e2.L.Head(), e1.L.Head())
		}

		// MB-14: the synthetic reveal/dice ack prompts are read-only over the
		// log -- Synthetic never feeds anything back to the engine, so
		// exercising it here, after replay equivalence is already proven for
		// the ordinary decision mapping, cannot itself change e1/e2's heads.
		// It is exercised against a REAL game's log (not a hand-built
		// fixture, which synthetic_test.go already covers) so a shape
		// mismatch against what a genuine game emits would fail here.
		assertSyntheticRoundTrips(t, e1, seed)
	}
}

// assertSyntheticRoundTrips converts e's whole log to the wire Event shape
// (protocol.EventFrom, the same conversion host/viewat.go's EventsSeat
// applies) and feeds it through a fresh Translator's Synthetic. Every prompt
// it mints must be a well-formed revealCards or diceRolled prompt whose
// matching acknowledgement AcknowledgeSynthetic accepts -- the round trip a
// real transport would perform once per prompt, without ever reaching
// SubmitIntent.
func assertSyntheticRoundTrips(t *testing.T, e *rules.Engine, seed uint64) {
	t.Helper()
	evs := make([]protocol.EventBody, len(e.L.Events))
	for i, ev := range e.L.Events {
		evs[i] = protocol.EventBody{Event: protocol.EventFrom(ev)}
	}
	tr := manabrew.New("replay", int64(seed), nil)
	msgs := tr.Synthetic(evs, nil)
	for _, msg := range msgs {
		pm, ok := msg.Value.(mb.PromptMessage)
		if !ok {
			t.Fatalf("seed %d: synthetic message type = %T, want PromptMessage", seed, msg.Value)
		}
		var ack mb.PromptOutputValue
		switch pm.Input.Value.(type) {
		case mb.RevealCardsInput:
			ack = mb.RevealCardsAcknowledged{}
		case mb.DiceRolledInput:
			ack = mb.DiceRolledAcknowledged{}
		default:
			t.Fatalf("seed %d: synthetic prompt type = %s, want revealCards or diceRolled", seed, pm.Input.Value.PromptType())
		}
		resp := mb.ClientMessage{Value: mb.ClientResponse{Kind: "response", PromptID: pm.PromptID,
			Action: mb.PromptOutput{Type: pm.Input.Value.PromptType(), Output: mb.PromptOutputData{Value: ack}}}}
		if err := manabrew.AcknowledgeSynthetic(resp, pm); err != nil {
			t.Fatalf("seed %d: AcknowledgeSynthetic(%s) = %v, want nil", seed, pm.Input.Value.PromptType(), err)
		}
	}
}

func playOneReplayGame(t *testing.T, cfg rules.Config, seed uint64) *rules.Engine {
	t.Helper()
	client := NewSeededRandomClient(seed)
	census := NewCensus()
	seats_ := make([]seat.Seat, len(cfg.Names))
	for i := range seats_ {
		seats_[i] = NewTranslatingSeat("replay", int64(seed), client, census)
	}
	_, e, err := bench.PlayGame(cfg, seats_, censusMaxTurns, censusMaxIntents, bench.Hooks{})
	if err != nil {
		t.Fatalf("seed %d: PlayGame: %v", seed, err)
	}
	if n := census.TotalUnmapped() + census.TotalRejected(); n > 0 {
		t.Fatalf("seed %d: %d unmapped/rejected decisions during replay-equivalence play: unmapped=%v rejected=%v",
			seed, n, census.Unmapped, census.Rejected)
	}
	return e
}
