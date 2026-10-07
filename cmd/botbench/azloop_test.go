package main

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/seat"
)

// TestAZSeatDoesNotReEquipForever replays the exact game that hung the
// Stage 0 az25 arm (2026-09-27): pair uw-tempo:mono-white-equipment, game
// index 3 of `-seed 90000000` (seed 90000003), where side A (az, 25
// clairvoyant simulations) sits seat 1 and plays mono-white-equipment. On
// turn 22 an Auriok Steelshaper made Bonesplitter's Equip free, and the az
// seat's priority candidates offered "Bonesplitter: Equip 1" onto the
// Phyrexian Germ it was already attached to. The search picked that re-equip
// over the bot's pass, and again at the next priority, forever: the turn
// never advanced (measured 11,700 intents in 10 minutes, all on turn 22 main
// 2), each cycle paying two searched decisions, so the -max-intents cap took
// ~15 minutes per such game at 25 sims. The default bot declines exactly this
// activation (botpolicy A1, equipNoOp; A5, the per-turn activation budget);
// the az candidates must too. The same game finishes in ~700 intents once the
// no-op is not a candidate, so a 3000-intent cap is ~4x headroom and a
// stalled outcome is the regression.
func TestAZSeatDoesNotReEquipForever(t *testing.T) {
	if testing.Short() {
		t.Skip("plays one full searched game")
	}
	dir := corpusDirOrSkip(t)
	saveAZ(t)
	azWorldArg = "clairvoyant"
	azCfg.Search.Sims = 25
	if err := azFrontDoor("bot", "az", nil, azSeatsFromHosted); err != nil {
		t.Fatalf("azFrontDoor: %v", err)
	}
	reg, err := testutil.OpenCorpusRegistry(dir)
	if err != nil {
		t.Fatalf("corpus: %v", err)
	}
	names := []string{"uw-tempo", "mono-white-equipment"}
	decks := make([][]*cards.Card, 2)
	for i, n := range names {
		if decks[i], err = testutil.LoadRepoDeck(reg, n); err != nil {
			t.Fatalf("deck %s: %v", n, err)
		}
	}
	const seed = 90000003
	cfg := buildGameConfig(seed, names, decks, nil, false)
	cfg.Tokens = reg.Tokens
	cfg.NameUniverse = reg.AllCards()
	// runMatrixTraced's per-seat seed derivation: seat s gets seed^(s+1).
	seats := []seat.Seat{policies["bot"](seed ^ 1), policies["az"](seed ^ 2)}
	o, _, err := playMatchOnce(cfg, []string{"bot", "az"}, seats, 200, 3000, nil, nil)
	if err != nil {
		t.Fatalf("playMatchOnce: %v", err)
	}
	if o.isStalled() {
		t.Fatalf("game stalled on %q at turn %d after %d intents: the az seat is repeating a non-progressing action", o.stallOn, o.turns, o.intents)
	}
}

// TestMaxTurnIntentsStallsAFrozenTurn pins the -max-turn-intents watchdog:
// a turn that asks more decisions than the cap ends the game as an
// "intents" stall (the frozen-turn kind the stall notice tallies) carrying a
// diagnostic that names the cap and the turn, instead of spending the whole
// -max-intents budget first. A cap no turn reaches changes nothing.
func TestMaxTurnIntentsStallsAFrozenTurn(t *testing.T) {
	cfg := livelockCfg(t)
	saved := maxTurnIntents
	t.Cleanup(func() { maxTurnIntents = saved })
	newSeats := func() []seat.Seat { return []seat.Seat{policies["bot"](1), policies["bot"](2)} }

	maxTurnIntents = 0
	want, _, err := playMatchOnce(cfg, []string{"bot", "bot"}, newSeats(), 200, 20000, nil, nil)
	if err != nil || want.isStalled() {
		t.Fatalf("uncapped game: %v, stalled %q", err, want.stallOn)
	}
	maxTurnIntents = 2000
	got, _, err := playMatchOnce(cfg, []string{"bot", "bot"}, newSeats(), 200, 20000, nil, nil)
	if err != nil || got != want {
		t.Fatalf("the default cap changed a healthy game: %+v, want %+v (err %v)", got, want, err)
	}
	maxTurnIntents = 3
	o, _, err := playMatchOnce(cfg, []string{"bot", "bot"}, newSeats(), 200, 20000, nil, nil)
	if err != nil {
		t.Fatalf("playMatchOnce: %v", err)
	}
	if o.stallOn != "intents" || !strings.Contains(o.livelock, "-max-turn-intents: 3 decisions on turn") {
		t.Fatalf("outcome stallOn %q diag %q, want an intents stall naming -max-turn-intents", o.stallOn, o.livelock)
	}
}
