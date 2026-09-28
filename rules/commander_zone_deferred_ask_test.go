package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// exileThenSearchFixtureSrc is Path to Exile's shape, authored freely: exile
// a target creature, then a SECOND step of the same resolution poses a
// decision -- the exiled creature's controller may search for a basic land.
const exileThenSearchFixtureSrc = `Name:Fixture Banish Then Search
ManaCost:0
Types:Instant
A:SP$ ChangeZone | Origin$ Battlefield | Destination$ Exile | ValidTgts$ Creature | SubAbility$ DBFetch | SpellDescription$ Banish a creature; its controller may fetch a basic land.
SVar:DBFetch:DB$ ChangeZone | Optional$ True | Origin$ Library | Destination$ Battlefield | Tapped$ True | ChangeType$ Land.Basic | DefinedPlayer$ TargetedController | ShuffleNonMandatory$ True
Oracle:Banish a creature; its controller may fetch a basic land.
`

// TestCommanderZoneAskIsNotOverwrittenByLaterResolutionAsk is the botbench
// livelock (commander, foundations-calling-all-angels vs
// rakdos-muscle-scam-exe, seed 9702): Path to Exile exiled Rakdos, the
// Muscle. The exile parked the CR 903.9 move and asked Rakdos's owner, but
// the same resolution's "its controller may search" then posed its own
// choice ON TOP of that ask. The commander-zone decision was never answered,
// the parked move never emitted, Rakdos stayed on the battlefield, and every
// later park of it (Phyrexian Altar sacrificing it) was dropped by the
// cmdZone dedup -- so the altar made mana for free forever.
//
// The fix: an ask posed while a commander-zone choice is outstanding waits
// behind it and is posed once that answer has been applied.
func TestCommanderZoneAskIsNotOverwrittenByLaterResolutionAsk(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		choice int
		want   state.Zone
	}{
		{"accept", 0, state.ZCommand},
		{"decline", 1, state.ZExile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg := colourIdentityGame(t, 11, FormatCommander, card(t, tinyCmdSrc), nil, card(t, exileThenSearchFixtureSrc))
			toMain1(t, e)
			cmd := fieldCommander(t, e, 0, 0)
			spell := moveToHandByName(t, e, 0, "Fixture Banish Then Search")
			e.priorityRound()

			opt := castByName(t, e, 0, "Fixture Banish Then Search")
			if opt == nil {
				t.Fatal("fixture spell not castable")
			}
			submit(t, e, opt.Index)
			d := e.Pending()
			if d == nil || d.Kind != decision.KTarget {
				t.Fatalf("pending after cast = %v, want the target ask", pendingKind(d))
			}
			picked := -1
			for _, o := range d.Options {
				if o.Obj == cmd {
					picked = o.Index
				}
			}
			if picked < 0 {
				t.Fatalf("commander not offered as a target: %+v", d.Options)
			}
			submit(t, e, picked)

			d = passPriorityUntilNonPriority(t, e)
			if d.Kind != decision.KCommanderZone {
				t.Fatalf("pending after the exile = %v, want the commander-zone choice (the search ask must not overwrite it)", pendingKind(d))
			}
			if got := e.G.Obj(cmd).Zone; got != state.ZBattlefield {
				t.Fatalf("commander zone before the answer = %v, want battlefield (the move is parked)", got)
			}
			submit(t, e, tc.choice)

			if got := e.G.Obj(cmd).Zone; got != tc.want {
				t.Fatalf("commander zone after answer = %v, want %v", got, tc.want)
			}
			d = e.Pending()
			if d == nil || d.Kind != decision.KChoose {
				t.Fatalf("pending after the commander-zone answer = %v, want the deferred search choice", pendingKind(d))
			}
			// Finish the resolution: every remaining ask takes its first
			// option (fetch a land, shuffle), then the spell is gone.
			for i := 0; i < 10 && e.Pending() != nil && e.Pending().Kind != decision.KPriority; i++ {
				submit(t, e, 0)
			}
			if got := e.G.Obj(spell).Zone; got != state.ZGraveyard {
				t.Fatalf("spell zone after resolution = %v, want graveyard", got)
			}
			asks := 0
			for _, ev := range e.L.Events {
				if ev.Kind == events.DecisionAsk && ev.Text == string(decision.KCommanderZone) {
					asks++
				}
			}
			if asks != 1 {
				t.Fatalf("commander-zone asks = %d, want 1", asks)
			}
			re := commanderReplayFromLog(t, cfg, e.L.Events)
			if got := re.Obj(cmd).Zone; got != tc.want {
				t.Fatalf("log-only replay commander zone = %v, want %v", got, tc.want)
			}
		})
	}
}

// moveToHandByName moves the named seeded deck card from the library to
// seat p's hand through a LOGGED MoveZone, so log-only replay reconstructs it.
func moveToHandByName(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZLibrary, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
			return id
		}
	}
	for _, id := range e.G.Zone(state.ZHand, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	t.Fatalf("card %q not found in seat %d's library or hand", name, p)
	return 0
}
