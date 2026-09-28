// A land play whose battlefield entry is FULLY replaced (Forge's
// ReplacementResult$ Replaced: the move is discarded and the ReplaceWith$ body
// happens instead) still uses that turn's land play. Playing a land is a
// special action taken as it is announced (CR 305.1, CR 505.5b); a replacement
// changes where the land ends up, never whether it was played.
//
// The entry-boundary ETB migration moved LandPlayed behind the land's own
// MoveZone so an as-enters choice is answered as the land enters. That
// finalizer only fires for a land that actually reaches the battlefield, so a
// replaced entry used to log no LandPlayed at all -- the player kept their land
// drop -- and left the continuation armed for a later, unrelated entry of the
// same object to consume.
//
// Card text below is authored for this test in the same R:/SVar$ shape as the
// corpus cards, never copied from Forge's .cards/cardsfolder.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// replacedLandEntrySrc is a land whose own entry replacement is an explicit
// ReplacementResult$ Replaced whose ReplaceWith$ moves nothing: the land never
// reaches the battlefield and its controller gains 1 life instead.
const replacedLandEntrySrc = `Name:Vanishing Land
Types:Land
R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ RepLife | ReplacementResult$ Replaced | Description$ x
SVar:RepLife:DB$ GainLife | Defined$ You | LifeAmount$ 1
Oracle:x
`

func TestReplacedLandEntryStillUsesTheLandPlay(t *testing.T) {
	t.Parallel()
	e, _, id := newFixtureDeck(t, 137, replacedLandEntrySrc)
	driveToStep(t, e, 1, 0, state.StepMain1)

	// Preconditions: the land is in the zone the land play reads (hand), the
	// land drop is unspent, and the replacement's observable effect (life) has
	// not happened yet.
	if got := e.G.Obj(id).Zone; got != state.ZHand {
		t.Fatalf("precondition: fixture zone = %s, want hand", got)
	}
	if got := e.G.Players[0].LandsPlayed; got != 0 {
		t.Fatalf("precondition: LandsPlayed = %d, want 0 before the play", got)
	}
	lifeBefore := e.G.Players[0].Life

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("expected seat 0's priority in main1, got %+v", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "play_land" && opt.Obj == id {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no play_land option for the fixture land: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit play_land: %v", err)
	}

	// The entry really was fully replaced -- otherwise this test would be
	// asserting the ordinary path.
	if got := e.G.Obj(id).Zone; got == state.ZBattlefield {
		t.Fatal("the land entered the battlefield -- the ReplacementResult$ Replaced entry " +
			"replacement did not apply, so this test is not exercising a replaced entry")
	}
	if got, want := e.G.Players[0].Life, lifeBefore+1; got != want {
		t.Fatalf("life = %d, want %d -- the ReplaceWith$ body did not run", got, want)
	}

	// The land play is spent all the same.
	if got := e.G.Players[0].LandsPlayed; got != 1 {
		t.Fatalf("LandsPlayed = %d, want 1: playing a land uses the land play even when the "+
			"land's battlefield entry is fully replaced", got)
	}
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.LandPlayed && ev.Player == 0 {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("logged %d LandPlayed events for seat 0, want exactly 1", n)
	}

	// The continuation is disarmed, so a later entry of the same object cannot
	// consume it and log a second, misattributed land play.
	if e.etbLandPlay {
		t.Fatalf("the land-play continuation is still armed (obj %d) after a replaced entry: "+
			"a later entry of that object would log a land play that belongs to nothing",
			e.etbLandObj)
	}

	// And the player does not get a second land this turn.
	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("expected seat 0's priority after the land play, got %+v", d)
	}
	for _, opt := range d.Options {
		if opt.Kind == "play_land" {
			t.Fatalf("a second play_land option (%+v) is offered after the land play was "+
				"spent on a replaced entry", opt)
		}
	}
}
