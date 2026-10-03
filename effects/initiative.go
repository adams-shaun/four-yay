package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
)

func init() { Register("TakeInitiative", effTakeInitiative) }

// effTakeInitiative implements DB$/SP$/AB$ TakeInitiative (CR 726, "The
// Initiative"): the resolving player takes the initiative designation and
// then ventures into Undercity.
//
// CR 726.3: only one player can have the initiative at a time; taking it
// moves the single designation, so the transition is recorded as one
// events.InitiativeChange (folded in events/apply.go), exactly the
// api:BecomeMonarch shape.
//
// CR 726.2's last inherent ability -- "Whenever a player takes the
// initiative, that player ventures into Undercity" -- is folded into the
// resolution here: the vEFFECT re-enters the ordinary Venture primitive with
// Dungeon$ Undercity (CR 726.2, CR 701.49d's explicit "venture into
// [quality]" form), so the marker movement, the room ability it queues and
// the completion state-based action all ride the dungeon machinery already
// built rather than a parallel path. This applies even when the taker
// already had the initiative (CR 726.5: the venture triggers but no second
// designation is created -- the single holder assignment already models
// that).
//
// CR 726.4 (the holder leaving the game hands the initiative to the active
// player) is not implemented, the same scope api:BecomeMonarch left for
// CR 725.4.
func effTakeInitiative(h Host, c *Ctx, sa *cards.SA) {
	players := definedPlayers(h, c, sa)
	if len(players) == 0 {
		return
	}
	for _, p := range players {
		if p < 0 || int(p) >= len(h.Game().Players) {
			continue
		}
		h.Emit(events.Event{Kind: events.InitiativeChange, Player: p})
	}
	// CR 726.2: taking the initiative ventures into Undercity. The venture
	// reuses the exact primitive a printed `DB$ Venture | Dungeon$ Undercity`
	// resolves through, carrying the taker selector across so a
	// `Defined$ <player>` TakeInitiative ventures for the same player(s).
	venture := &cards.SA{Kind: "DB", API: "Venture", Params: map[string]string{"Dungeon": "Undercity"}}
	if d := sa.ParamStr(cards.PKDefined); d != "" {
		venture.Params["Defined"] = d
	}
	effVenture(h, c, venture)
}
