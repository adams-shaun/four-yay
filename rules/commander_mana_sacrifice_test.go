package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// altarFixtureSrc is Phyrexian Altar's shape, authored freely: a mana
// ability whose only cost is sacrificing a creature, producing one mana of
// any colour -- so its resolution poses a colour choice right after the
// sacrifice is paid.
const altarFixtureSrc = `Name:Fixture Altar
ManaCost:3
Types:Artifact
A:AB$ Mana | Cost$ Sac<1/Creature> | Produced$ Any | SpellDescription$ Add one mana of any color.
Oracle:Sacrifice a creature: Add one mana of any color.
`

// TestManaAbilitySacrificingCommanderWaitsForCommandZoneChoice is the
// botbench livelock (commander, foundations-reign-of-dragons vs
// rakdos-muscle-scam-exe, seed 105 game 1): Phyrexian Altar sacrificing
// Rakdos, the Muscle -- the controller's own commander and its only
// creature. The sacrifice cost parks the CR 903.9 commander-zone move and
// asks the owner, but the mana ability then posed its colour choice on top,
// overwriting the commander-zone decision. The parked move was never
// emitted, so the commander stayed on the battlefield, every later
// activation's park was dropped by the queue's dedup, and the altar made
// free mana forever (the bot activated it until the livelock watcher
// aborted the game).
//
// The fix: a mana ability whose cost payment posed a decision (the
// commander-zone ask) waits for it, and resolves its mana effect -- the
// colour choice -- once that answer lands.
func TestManaAbilitySacrificingCommanderWaitsForCommandZoneChoice(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		choice int
		want   state.Zone
	}{
		{"accept", 0, state.ZCommand},
		{"decline", 1, state.ZGraveyard},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg := colourIdentityGame(t, 11, FormatCommander, card(t, tinyCmdSrc), nil, card(t, altarFixtureSrc))
			toMain1(t, e)
			cmd := fieldCommander(t, e, 0, 0)
			altar := moveToBattlefieldByName(t, e, 0, "Fixture Altar")
			e.priorityRound()

			activateMana(t, e, altar)

			d := e.Pending()
			if d == nil || d.Kind != decision.KCommanderZone {
				t.Fatalf("pending after the altar's sacrifice = %v, want the commander-zone choice (the colour ask must not overwrite it)", pendingKind(d))
			}
			if got := e.G.Players[0].Pool.Total(); got != 0 {
				t.Fatalf("pool = %d before the commander-zone answer, want 0 (the mana effect waits for the cost)", got)
			}
			submit(t, e, tc.choice)

			if got := e.G.Obj(cmd).Zone; got != tc.want {
				t.Fatalf("commander zone after answer = %v, want %v", got, tc.want)
			}
			d = e.Pending()
			if d == nil || d.Kind != decision.KChoose {
				t.Fatalf("pending after the commander-zone answer = %v, want the altar's colour choice", pendingKind(d))
			}
			submit(t, e, 2) // Add B
			if got := e.G.Players[0].Pool[state.MB]; got != 1 {
				t.Fatalf("pool B = %d, want 1", got)
			}
			d = e.Pending()
			if d == nil || d.Kind != decision.KPriority {
				t.Fatalf("pending after the colour answer = %v, want priority", pendingKind(d))
			}
			for _, o := range d.Options {
				if o.Obj == altar {
					t.Fatalf("altar still offered with no creature left to sacrifice: %+v", o)
				}
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
			// A replay from Config + log alone reproduces the move and the mana
			// (fieldCommander's direct summoning-sickness clear is unlogged, so
			// the whole-state replayCheck diff does not apply here).
			re := commanderReplayFromLog(t, cfg, e.L.Events)
			if got := re.Obj(cmd).Zone; got != tc.want {
				t.Fatalf("log-only replay commander zone = %v, want %v", got, tc.want)
			}
			if got := re.Players[0].Pool[state.MB]; got != 1 {
				t.Fatalf("log-only replay pool B = %d, want 1", got)
			}
		})
	}
}

// pendingKind names a pending decision's kind for a failure message (the
// full struct is unreadably long).
func pendingKind(d *decision.Decision) string {
	if d == nil {
		return "<none>"
	}
	return string(d.Kind) + " " + d.Prompt
}
