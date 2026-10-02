//go:build manabrew

package mbtest

// MBX-4's in-process leg: the ManaBrew wire is a lossless transport for a
// REAL bot. For every MB-8 repo-deck game (the census seed scheme: seeds
// seats*1000+g at 2 and 4 seats over the Legacy decks), the same production
// bot (seat.NewBot at the game's seed) plays the game twice:
//
//   - natively, the plain seat.Bot;
//   - through the wire, a BotWireSeat whose every answer is converted to a
//     ManaBrew response with ResponseForIntent and played back through
//     mb.Encode/mb.Decode + Translator.TranslateResponse.
//
// The two games must finish with the same event-log chain head, the same
// outcome and the same intent stream. On any mismatch, firstDivergence
// (mirroring internal/spellbench/v2engine/parity_test.go's helper) replays
// the common intent prefix on a fresh engine and names the decision where
// the runs part. The census of the wire run must also record zero unmapped
// prompts and zero rejections: a game that never got an answer onto the
// wire would not be a parity proof at all.

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
)

// intentRec is one answer the parity harness saw reach the engine: which
// seat answered, the ask it was answering (kind, seq) and the intent that
// went in. Recorded by bench.Hooks.Decision, which fires after the seat has
// answered and before Submit -- the exact point the two transports are
// comparable.
type intentRec struct {
	seat int
	kind decision.Kind
	seq  uint64
	in   decision.Intent
}

// intents lifts the plain intent stream out of the records, for
// firstDivergence.
func intents(recs []intentRec) []decision.Intent {
	out := make([]decision.Intent, len(recs))
	for i, r := range recs {
		out[i] = r.in
	}
	return out
}

// parityHooks returns the Hooks that record every submitted intent.
func parityHooks(recs *[]intentRec) bench.Hooks {
	return bench.Hooks{Decision: func(seatIdx int, d *decision.Decision, in decision.Intent, _ *botpolicy.Board) error {
		*recs = append(*recs, intentRec{seat: seatIdx, kind: d.Kind, seq: d.Seq, in: decision.CloneIntent(in)})
		return nil
	}}
}

// playBotParityGame plays one game over the given seats with the recording
// hooks, returning the outcome, the engine (for its log) and the recorded
// intent stream. The caps are the census watchdogs; a stall or an abort
// fails the game outright in the caller.
func playBotParityGame(t *testing.T, cfg rules.Config, seats func(int) seat.Seat) (bench.Outcome, *rules.Engine, []intentRec) {
	t.Helper()
	ss := make([]seat.Seat, len(cfg.Names))
	for i := range ss {
		ss[i] = seats(i)
	}
	var recs []intentRec
	o, e, err := bench.PlayGame(cfg, ss, censusMaxTurns, censusMaxIntents, parityHooks(&recs))
	if err != nil {
		t.Fatalf("PlayGame: %v", err)
	}
	if bench.IsAbort(o.StallOn) {
		t.Fatalf("engine abort (%s): %s", o.StallOn, o.Livelock)
	}
	return o, e, recs
}

// TestManaBrewNativeParity runs the MB-8 game set twice per game -- native
// bot vs the same bot over the ManaBrew mapping -- and asserts the chain
// heads, outcomes and intent streams are identical.
func TestManaBrewNativeParity(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	games := 0
	for _, nseats := range []int{2, 4} {
		for g := 0; g < censusGamesPerSeatCount; g++ {
			seed := uint64(nseats)*1000 + uint64(g)
			names := testutil.LegacyDeckNames()
			playerNames := make([]string, nseats)
			decks := make([][]*cards.Card, nseats)
			for i := 0; i < nseats; i++ {
				playerNames[i] = names[(int(seed)+i)%len(names)]
				decks[i] = testutil.RepoDeck(t, reg, playerNames[i])
			}
			cfg := rules.Config{Seed: seed, Names: playerNames, Decks: decks, Tokens: reg.Tokens, NameUniverse: reg.Cards}

			// Native: the production bot, seat.Bot directly.
			oN, eN, rN := playBotParityGame(t, cfg, func(int) seat.Seat { return seat.NewBot(seed) })

			// Wire: the same bot seed behind BotWireSeat.
			census := NewCensus()
			oW, eW, rW := playBotParityGame(t, cfg, func(int) seat.Seat { return NewBotWireSeat("parity", int64(seed), seed, census) })
			games++

			// The census of the wire run must be clean: every answer went
			// through the wire and nothing was refused.
			if n := census.TotalUnmapped(); n > 0 {
				t.Fatalf("seed %d seats %d: %d unmapped prompt(s): %v", seed, nseats, n, census.Unmapped)
			}
			if n := census.TotalRejected(); n > 0 {
				t.Fatalf("seed %d seats %d: the bot's answers were refused %d time(s): %v", seed, nseats, n, census.Rejected)
			}
			if len(rN) == 0 || len(rW) == 0 {
				t.Fatalf("seed %d seats %d: recorded %d native / %d wire intents; a game with no answers proves nothing",
					seed, nseats, len(rN), len(rW))
			}
			headN, headW := eN.L.Head(), eW.L.Head()
			if headN == "" || headW == "" {
				t.Fatalf("seed %d seats %d: empty chain head (native %q wire %q)", seed, nseats, headN, headW)
			}
			if headN != headW {
				t.Fatalf("seed %d seats %d: chain heads differ: native %s, wire %s\n%s",
					seed, nseats, headN, headW, firstDivergence(cfg, intents(rN), intents(rW)))
			}
			if oN.Draw != oW.Draw || oN.WinnerSeat != oW.WinnerSeat {
				t.Fatalf("seed %d seats %d: outcomes differ: native draw=%v winner=%d, wire draw=%v winner=%d",
					seed, nseats, oN.Draw, oN.WinnerSeat, oW.Draw, oW.WinnerSeat)
			}
			// The intent streams must agree element by element, not just in
			// their final hash: this names the first differing ASK if they
			// ever do not (a head mismatch above is already fatal, so this
			// is the belt to the head's braces).
			if len(rN) != len(rW) {
				t.Fatalf("seed %d seats %d: intent streams differ in length: native %d, wire %d",
					seed, nseats, len(rN), len(rW))
			}
			for i := range rN {
				a, b := rN[i], rW[i]
				if a.kind != b.kind || a.seq != b.seq || a.seat != b.seat ||
					fmt.Sprint(a.in.Choices, a.in.Rest, a.in.Payment != nil) != fmt.Sprint(b.in.Choices, b.in.Rest, b.in.Payment != nil) {
					t.Fatalf("seed %d seats %d: intents part at index %d (%s seq %d seat %d): native %v, wire %v\n%s",
						seed, nseats, i, a.kind, a.seq, a.seat, a.in.Choices, b.in.Choices,
						firstDivergence(cfg, intents(rN), intents(rW)))
				}
			}
		}
	}
	if games == 0 {
		t.Fatal("no games ran")
	}
	t.Logf("parity: %d game(s), native and ManaBrew wire byte-identical at every chain head", games)
}

// firstDivergence replays the common prefix of two intent streams on a fresh
// engine and describes the decision where they part. Mirrors
// internal/spellbench/v2engine/parity_test.go's helper of the same name; the
// spellbench copy is test-only, so the shape is reproduced here rather than
// imported.
func firstDivergence(cfg rules.Config, a, b []decision.Intent) string {
	i := 0
	for i < len(a) && i < len(b) && fmt.Sprint(a[i].Choices, a[i].Rest, a[i].Payment != nil) == fmt.Sprint(b[i].Choices, b[i].Rest, b[i].Payment != nil) {
		i++
	}
	e := rules.New(cfg)
	e.Advance()
	for k := 0; k < i; k++ {
		in := a[k]
		if d := e.Pending(); d != nil {
			in.Seq = d.Seq
		}
		if err := e.Submit(in); err != nil {
			return fmt.Sprintf("replay failed at %d: %v", k, err)
		}
	}
	d := e.Pending()
	if d == nil {
		return fmt.Sprintf("streams part at intent %d with no pending decision", i)
	}
	var opts []string
	for _, o := range d.Options {
		opts = append(opts, fmt.Sprintf("%d:%s/%s(obj %d)", o.Index, o.Kind, o.Label, o.Obj))
	}
	ca, cb := "none", "none"
	if i < len(a) {
		ca = fmt.Sprint(a[i].Choices, a[i].Rest)
	}
	if i < len(b) {
		cb = fmt.Sprint(b[i].Choices, b[i].Rest)
	}
	return fmt.Sprintf("first divergence at intent %d (turn %d %s): %s p%d min %d max %d options %v: native %s, wire %s",
		i, e.G.Turn, e.G.Step, d.Kind, d.Player, d.Min, d.Max, opts, ca, cb)
}
