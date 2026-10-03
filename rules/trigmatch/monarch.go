package trigmatch

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// BecomeMonarchMatches implements Mode$ BecomeMonarch (trig:BecomeMonarch),
// the "whenever a player becomes the monarch" trigger. It fires on the
// events.MonarchChange transition effects' api:BecomeMonarch emits (the
// designation is folded game-level state, so a replay re-derives it), and
// tests ValidPlayer$ against the NEW monarch -- the event's Player.
//
// Corpus population at the current pin, measured with GNU grep over
// .cards/cardsfolder: 5 raw lines / 5 files contain "Mode$ BecomeMonarch",
// but only 4 of those are T: trigger lines -- Knights of the Black Rose,
// Custodi Lich, Garland Royal Kidnapper and Starscream Power Hungry. The 5th
// (Palace Jailer) carries the mode on an Effect-delivered command-zone SVar
// body (SVar:ComeBack) and does NOT reach this matcher: the Effect's
// Triggers$ delivery is not registered and forEachObject never scans
// ZCommand, so Palace Jailer's arm stays inert (AGENTS.md's Effect row).
// The report's missing-primitive list showed "trig:BecomeMonarch 4"
// before the registration and no entry at all after it.
//
//   - ValidPlayer$ is the shared player-spec grammar with the trigger's own
//     controller as "you" (the Drawn/RolledDie shape). "Opponent" is the
//     Knights of the Black Rose/Garland spelling, "You" the Custodi
//     Lich/Starscream one; an absent clause matches any player.
//   - BeginTurn$ is the intervening-if "if you were the monarch as the turn
//     began" (Knights of the Black Rose is the corpus's ONLY carrier, 1 of
//     38000+ card files). The value is read as the qualified player whose
//     turn-start designation the clause asserts; only "You" -- the sole
//     corpus value -- is modelled, and any other value fails closed rather
//     than matching wide. The read is the game-level TurnStartMonarch
//     snapshot events/apply.go folds at every TurnChange (the CombatsThisTurn
//     shape, no new event or field), so it is exact at the moment the trigger
//     is checked (CR 603.4) and rebuilt identically by replay.
func BecomeMonarchMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.MonarchChange {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidPlayer); v != "" && !effects.MatchesPlayerSpec(e.Game(), v, ev.Player, ctrl) {
		return false
	}
	if v := strings.TrimSpace(t.ParamStr(cards.PKBeginTurn)); v != "" {
		if !strings.EqualFold(v, "You") || !e.Game().WasMonarchAtTurnStart(ctrl) {
			return false
		}
	}
	return true
}

func init() {
	registerTrigMatcher(func(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return BecomeMonarchMatches(e, t, source, ev)
	}, "BecomeMonarch")
	effects.RegisterNonAPI("trig:BecomeMonarch")
}
