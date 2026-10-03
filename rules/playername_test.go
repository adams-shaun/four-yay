package rules

import "testing"

// TestPlayerNamesDoNotReachTheChain is the F3 invariant that guards B3's
// name/deck split at the engine layer: a per-seat PlayerName is a display
// name (view.PlayerView.Name, protocol.SeatInfo.Name) and must never reach
// event text, because the event log is the replay-hashed chain. Two engines
// built from one Config differing ONLY in PlayerNames must produce identical
// chain heads at every step, including after a full game is driven to Over.
//
// The engine still puts the deck identity (Config.Names) in event text, so
// routing a PlayerName into a MatchDraw or a trigger's description would move
// a chain head — exactly the regression this test exists to catch. It builds
// its own engines (two seats, decks just big enough to deck out in a handful
// of turns) and never touches the TestHeads acceptance games.
func TestPlayerNamesDoNotReachTheChain(t *testing.T) {
	t.Parallel()
	// smallDeckGame returns a played (Advanced) engine plus the Config that
	// produced it; reusing that Config guarantees the two runs differ in
	// nothing but PlayerNames.
	eBase, cfg := smallDeckGame(t, 2, 8)

	cfgWith := cfg
	cfgWith.PlayerNames = []string{"Alice", "Bob"}
	eWith := New(cfgWith)
	eWith.Advance()

	// Genesis alone must leave both chains equal (PlayerNames never reach the
	// opening-deal events).
	if eBase.L.Head() != eWith.L.Head() {
		t.Fatalf("head differs after genesis: %s vs %s", eBase.L.Head(), eWith.L.Head())
	}

	// Drive both to Over with the same naive decisions and demand the heads
	// stay identical the whole way through.
	driveToOver(t, eBase, 200)
	driveToOver(t, eWith, 200)

	if eBase.L.Head() != eWith.L.Head() {
		t.Fatalf("heads diverged after a full game:\n  with PlayerNames:  %s\n  without (deck):   %s",
			eWith.L.Head(), eBase.L.Head())
	}
	if !eBase.G.Over || !eWith.G.Over {
		t.Fatalf("games did not reach Over (base Over=%v, with Over=%v)", eBase.G.Over, eWith.G.Over)
	}
}
