package rules

import (
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/state"
)

// TestBotAdNauseamStopsOnEmptyLibrary is the the-epic-storm:uw-control
// livelock (botbench -seed 1304): the bot answered Ad Nauseam's
// RepeatOptional$ election "Repeat" whenever its life was above 5, so a
// library of 0-mana-value cards (lands here) drained without a life loss and
// the bot then kept repeating a body that reveals nothing -- a zero-life
// LifeChange, the Cleanup Note, and another election, until the engine's
// 1000-iteration cap. Repeating with an empty library is legal but makes no
// progress, so the bot must stop once the library is empty.
func TestBotAdNauseamStopsOnEmptyLibrary(t *testing.T) {
	t.Parallel()
	e, cfg, id, caster := corpusCardConfig(t, 6103, "Ad Nauseam")
	addMana(t, e, caster, "BBBCC")
	lib := len(e.G.Zone(state.ZLibrary, caster))
	// Precondition: the library holds only 0-mana-value lands, so the bot's
	// life gate alone never stops the repeat.
	if lib == 0 || e.G.Players[caster].Life <= 5 {
		t.Fatalf("precondition: library %d, life %d", lib, e.G.Players[caster].Life)
	}

	d := castAndReachElection(t, e, id, -1)
	r := rand.New(rand.NewPCG(1, 2))
	elections := 0
	for d != nil && d.ResumeKind == "repeat_optional" {
		elections++
		if elections > lib+1 {
			t.Fatalf("bot answered %d repeat elections with a %d-card library: it repeats a no-progress body", elections, lib)
		}
		in := botpolicy.Decide(botpolicy.BoardFromGame(e.G, e, d.Player), d, r)
		submitChoices(t, e, in.Choices...)
		d = e.Pending()
	}
	if got := len(e.G.Zone(state.ZLibrary, caster)); got != 0 {
		t.Fatalf("bot stopped with %d cards left at life %d; want the whole 0-MV library taken", got, e.G.Players[caster].Life)
	}
	passUntilStackEmpty(t, e, 30)
	replayCheck(t, e, cfg)
}
