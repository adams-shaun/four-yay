package manabrew

import (
	"strconv"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The id mint (scoping spec §6.1): every ManaBrew identifier this translator
// emits goes through one of these functions, so a client-visible id can never
// drift from the mapping the spec pins.
//
// All of them are pure functions of their argument: the same gorge object or
// player mints the same ManaBrew id for the whole match (ObjID and PlayerID
// are stable for the match's lifetime; CR 400.7's incarnation tracking is
// state.Object.Incarnation's business and never changes the id), and two
// different objects never mint the same id.

// playerID mints a ManaBrew player id: "player-<seat>".
func playerID(p state.PlayerID) string {
	return "player-" + strconv.FormatUint(uint64(p), 10)
}

// cardID mints a ManaBrew card / permanent id from a state.ObjID: "o<ObjID>".
// This is parity with the native wire, which already exposes ObjID. The id is
// stable across zone changes; an object's identity never changes.
func cardID(id state.ObjID) string {
	return "o" + strconv.FormatUint(uint64(id), 10)
}

// stackID mints a ManaBrew stack-object id: "s<ObjID>". TargetRef kind
// "spell" points at one of these.
func stackID(id state.ObjID) string {
	return "s" + strconv.FormatUint(uint64(id), 10)
}

// hiddenCardID mints a positional hidden-entry id for a card whose gorge
// identity must not be exposed: "h-<zone>-<owner>-<i>", where i is the
// card's index within its zone list. It carries no ObjID -- the ManaBrew
// HiddenCard union member has only the id -- so a hidden card's position in
// its zone is the only fact the id carries. A zone-list insertion or removal
// shifts later indices; that is inherent to the published shape.
func hiddenCardID(zone string, owner state.PlayerID, i int) string {
	return "h-" + zone + "-" + strconv.FormatUint(uint64(owner), 10) + "-" + strconv.Itoa(i)
}

// actionID mints a ManaBrew actionId for a priority Option: "opt-<index>".
func actionID(index int) string {
	return "opt-" + strconv.Itoa(index)
}

// payActionID mints the actionId of an announce-able cast's PaymentAction:
// "pay-<PaymentAction.ID>" (spec §6.1).
func payActionID(id string) string {
	return "pay-" + id
}

// promptID mints a ManaBrew promptId from the decision's Seq (spec §6.1):
// stable across reconnect and caretaker, and a stale answer is simply a
// different Seq. A gorge Seq is far below 2^53 (a JS safe integer).
func promptID(d *decision.Decision) int64 { return int64(d.Seq) }

// The exported aliases below are the SAME mints, re-exposed for the mbtest
// reachability helper (MBX-3: internal/manabrew/mbtest/reach.go), which must
// build the inverse of TranslateResponse -- the client message that selects
// a given native option -- and therefore needs to name options with exactly
// the ids the prompt minted. A helper that re-implemented the mint by its own
// rule could silently drift from this file; going through these aliases keeps
// one home, and the reachability census is the drift alarm if one ever
// appears (a mint change makes the helper's ids stop being advertised and
// the census fails).

// PlayerID is the exported playerID mint ("player-<seat>").
func PlayerID(p state.PlayerID) string { return playerID(p) }

// CardID is the exported cardID mint ("o<ObjID>").
func CardID(id state.ObjID) string { return cardID(id) }

// StackID is the exported stackID mint ("s<ObjID>").
func StackID(id state.ObjID) string { return stackID(id) }

// ActionID is the exported actionID mint ("opt-<index>").
func ActionID(index int) string { return actionID(index) }

// PayActionID is the exported payActionID mint ("pay-<id>").
func PayActionID(id string) string { return payActionID(id) }

// AttackTargetID is the exported attackTargetID mint: the wire ref id of an
// attacker option's target (the battle/planeswalker permanent for a
// permanent attack, the defender's player id for a player attack).
func AttackTargetID(opt decision.Option) string { return attackTargetID(opt) }

// ColorCode is the exported colorCode lookup: the wire colour code a colour
// option (ManaSymbol, label) resolves to.
func ColorCode(symbol, label string) string { return colorCode(symbol, label) }
