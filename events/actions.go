package events

import (
	"strings"

	"github.com/adams-shaun/gorge/state"
)

const (
	discardText     = "discarded"
	discardCostText = "discarded as a cost"

	// ReturnCostText is the canonical zone-change marker for a permanent
	// returned to its owner's hand as a COST (Forge CostReturn.doPayment's
	// moveToHand; Return<N/Spec> tokens). Cost provenance is distinct so the
	// ConditionDefined$ Returned group (Wonderscape Sage) can enumerate
	// exactly the permanents this activation's own cost returned, the way
	// IsDiscardCost enumerates a cost discard.
	ReturnCostText = "returned to hand as a cost"
)

// CountersRemainMove marks a battlefield departure whose source has a
// CountersRemain static. It uses MoveZone's otherwise-unused Counter payload
// so replay folds the same counter-preserving move without a new event field.
const CountersRemainMove = "counters-remain:"

// MarkCountersRemainMove preserves an existing MoveZone Counter payload while
// tagging the move for deterministic replay.
func MarkCountersRemainMove(counter string) string {
	return CountersRemainMove + counter
}

// CountersRemainMovePayload removes the tag and reports whether it was set.
func CountersRemainMovePayload(counter string) (string, bool) {
	payload, ok := strings.CutPrefix(counter, CountersRemainMove)
	return payload, ok
}

// cyclingDiscardPrefix marks the Counter field of a discard-as-cost event paid
// for a CYCLING ability (CR 702.29). Counter is unused on a hand->graveyard
// MoveZone (the face-down/cloak entry and exile markers are read only for a
// battlefield or exile destination), so it is the one free payload slot on the
// existing event. The prefix keeps the tag from colliding with any Counter
// value a non-cycling discard could ever carry.
const cyclingDiscardPrefix = "cycling:"

// DiscardCostCycling returns the discard-as-cost event for a card discarded as
// the cost of the cycling ability named by keyword (Forge's K:Cycling ->
// "Cycling", K:TypeCycling -> "TypeCycling"). The ability's name is the
// provenance the discard carries: a Mode$ Cycled trigger matches THIS, not the
// discarded card's printed face, so an ability granted in a layer fires it
// while an unrelated cost discard of a card that merely prints Cycling does
// not (CR 702.29d; CR 603.2's event requirement).
func DiscardCostCycling(obj state.ObjID, keyword string) Event {
	ev := DiscardCost(obj)
	ev.Counter = cyclingDiscardPrefix + keyword
	return ev
}

// IsCyclingDiscard reports whether ev is a discard-as-cost event paid for a
// cycling ability, returning the ability keyword that named it. It is false
// for every other discard, including an ordinary cost discard of a card whose
// printed face carries Cycling.
func IsCyclingDiscard(ev Event) (string, bool) {
	if !IsDiscardCost(ev) || !strings.HasPrefix(ev.Counter, cyclingDiscardPrefix) {
		return "", false
	}
	return strings.TrimPrefix(ev.Counter, cyclingDiscardPrefix), true
}

// Discard returns the canonical zone-change event for a discard effect or
// cleanup discard. Keeping the action marker here means every producer and
// consumer agrees on what distinguishes a discard from an ordinary hand move;
// replacement effects may change To while preserving the marker. Mode$ Random
// additionally carries its one-based eligible-hand choice index in Amount;
// ordinary discards leave Amount zero. Apply folds the chosen Obj's move, so
// the random choice and its state change share a single replayable event.
func Discard(obj state.ObjID, player state.PlayerID) Event {
	return Event{Kind: MoveZone, Obj: obj, From: state.ZHand, To: state.ZGraveyard,
		Player: player, Text: discardText}
}

// DiscardCost returns the canonical zone-change event for discarding a card as
// a cost. Cost provenance is distinct so a trigger requiring ValidCause$ does
// not attribute a cost to an unrelated object already on the stack.
func DiscardCost(obj state.ObjID) Event {
	return Event{Kind: MoveZone, Obj: obj, From: state.ZHand, To: state.ZGraveyard,
		Text: discardCostText}
}

// IsDiscard reports whether ev is one of the canonical discard moves. The
// destination is deliberately not part of the test: a replacement can redirect
// the discarded card while the action that caused the move remains a discard.
func IsDiscard(ev Event) bool {
	if ev.Kind != MoveZone || ev.From != state.ZHand {
		return false
	}
	return ev.Text == discardText || ev.Text == discardCostText
}

// IsDiscardCost reports whether ev records the cost form of a discard.
func IsDiscardCost(ev Event) bool {
	return ev.Kind == MoveZone && ev.From == state.ZHand && ev.Text == discardCostText
}

// ReturnCost returns the canonical zone-change event for a permanent returned
// to its owner's hand as a cost (Forge CostReturn.doPayment's moveToHand).
// Every producer uses it so the return-cost provenance the ConditionDefined$
// Returned group reads and the Return<N/Spec> payment agree on one marker.
func ReturnCost(obj state.ObjID, from state.Zone) Event {
	return Event{Kind: MoveZone, Obj: obj, From: from, To: state.ZHand, Text: ReturnCostText}
}

// IsReturnCost reports whether ev records the cost form of a return to hand.
func IsReturnCost(ev Event) bool {
	return ev.Kind == MoveZone && ev.To == state.ZHand && ev.Text == ReturnCostText
}

// EvidenceCostText is the canonical zone-change marker for a card collected as
// evidence (CR 701.30's collect-evidence action; Forge's CostCollectEvidence).
// Every producer uses it so the collect-evidence payment and the
// ConditionDefined$ Collected group agree on one marker. It is distinct from
// the ward-evidence payment's text, which is not the resolving spell's own
// collect-evidence cost.
const EvidenceCostText = "collected as evidence"

// EvidenceCost returns the canonical zone-change event for exiling a card as a
// collect-evidence cost (CR 701.30b): the card moves from its owner's
// graveyard to exile carrying the evidence marker.
func EvidenceCost(obj state.ObjID) Event {
	return Event{Kind: MoveZone, Obj: obj, From: state.ZGraveyard, To: state.ZExile,
		Text: EvidenceCostText}
}

// IsEvidenceCost reports whether ev records a collect-evidence payment: a
// graveyard-to-exile move carrying the evidence marker. A ward evidence
// payment uses a different text and never matches (it is not the resolving
// spell's own collect-evidence cost).
func IsEvidenceCost(ev Event) bool {
	return ev.Kind == MoveZone && ev.To == state.ZExile && ev.Text == EvidenceCostText
}

// CostMoveKind selects which cost-action marker a cost-window read enumerates.
// It lives here, in the package that owns the markers, so both effects (the
// Host role that declares the read) and rules (the Engine that implements it)
// name one shared enum instead of each declaring its own. One kind per cost
// action, so a new cost-provenance window adds a row here and to CostMoveMatcher
// rather than a new Host method (internal/codeshape's hostMethodCount is
// shrink-only).
type CostMoveKind int

const (
	// CostMoveDiscard is the cost discards (IsDiscardCost) — the
	// ConditionDefined$ Discarded group's cost-discard channel.
	CostMoveDiscard CostMoveKind = iota
	// CostMoveReturn is the cost returns to hand (IsReturnCost) — the
	// ConditionDefined$ Returned group's channel.
	CostMoveReturn
	// CostMoveEvidence is the cards exiled as a collect-evidence cost
	// (IsEvidenceCost) — the ConditionDefined$ Collected group's channel.
	CostMoveEvidence
)

// CostMoveMatcher returns the event predicate CostMoveKind selects, or nil for
// an unknown kind. A nil result is a programming error, not a corpus shape: a
// caller treats it as an empty window (a resolved zero) rather than admitting
// every cost move.
func CostMoveMatcher(k CostMoveKind) func(Event) bool {
	switch k {
	case CostMoveDiscard:
		return IsDiscardCost
	case CostMoveReturn:
		return IsReturnCost
	case CostMoveEvidence:
		return IsEvidenceCost
	}
	return nil
}

// milledText is the action marker a mill's zone change carries. Like the
// discard/sacrifice markers it is carried on MoveZone's Text field, which is
// otherwise unused for a library->graveyard move. A mill is a library->
// graveyard move with this marker, so the Mode$ Milled / Mode$ MilledAll
// triggers can tell a mill apart from an ordinary move (CR 701.17a).
const milledText = "milled"

// Mill returns the canonical zone-change event for milling a card (CR
// 701.17a): the top card of player's library put into their graveyard. Every
// mill producer uses it so the Milled/MilledAll triggers and the mill itself
// agree on the marker.
func Mill(obj state.ObjID, player state.PlayerID) Event {
	return Event{Kind: MoveZone, Obj: obj, From: state.ZLibrary, To: state.ZGraveyard,
		Player: player, Text: milledText}
}

// IsMill reports a completed mill (CR 701.17a): a marked card move from
// library into graveyard. A replacement redirecting or preventing that move
// means the card was never milled, even if action provenance is retained.
func IsMill(ev Event) bool {
	return ev.Kind == MoveZone && ev.From == state.ZLibrary && ev.To == state.ZGraveyard && ev.Text == milledText
}

const sacrificeText = "sacrificed"

// Sacrifice returns the canonical zone-change event for sacrificing a
// permanent (CR 701.21a), whether as a cost or by an effect. Every producer
// uses it so the Sacrificed trigger and a replacement redirecting the move
// agree on the marker.
func Sacrifice(obj state.ObjID) Event {
	return Event{Kind: MoveZone, Obj: obj, From: state.ZBattlefield, To: state.ZGraveyard, Text: sacrificeText}
}

// IsSacrifice reports whether ev is a sacrifice: a permanent moved from the
// battlefield under the "sacrificed" action marker (CR 701.21a). Like
// IsDiscard it ignores the destination, which a replacement may change.
func IsSacrifice(ev Event) bool {
	return ev.Kind == MoveZone && ev.From == state.ZBattlefield && ev.Text == sacrificeText
}

// IsAlterAttribute reports whether ev is the AlterAttribute grant for the
// named attribute ("Saddled", "Plotted", ...), the designation the trigger
// matchers for Mode$ Becomes* read. The attribute name is a parameter so the
// call sites' literal arguments are not a per-word dispatch.
func IsAlterAttribute(ev Event, attr string) bool {
	return ev.Kind == AlterAttribute && ev.Text == attr
}

// ActionMarker returns the action marker a zone change carries when it is a
// sacrifice or a discard, and "" for every other event. A replacement that
// substitutes a different move for such an event records this marker so the
// substituted move can carry it (see CarryAction). A cycling cost discard's
// provenance (its Counter = "cycling:<keyword>") is appended after a NUL, so
// CarryAction can restore it: CR 614.6 keeps the substituted move the same
// action, cycling cause included.
func ActionMarker(ev Event) string {
	if IsSacrifice(ev) || IsDiscard(ev) {
		if ev.Counter != "" {
			return ev.Text + actionCauseSep + ev.Counter
		}
		return ev.Text
	}
	return ""
}

// actionCauseSep separates an action marker from the cause payload ActionMarker
// appends. A NUL can never appear in a marker literal ("sacrificed",
// "discarded", "discarded as a cost") or in a cycling Counter, so the split is
// unambiguous.
const actionCauseSep = "\x00"

// CarryAction re-labels the move a replacement substituted for an action
// event. CR 614.6: the replaced event never happens, but the modified event
// is still the same action -- a permanent sacrificed "into exile instead" was
// sacrificed (CR 701.21a), and a card discarded "onto the battlefield instead"
// was discarded (CR 701.9a). Only the replaced object's first move out of the
// zone the action moved it from is re-labelled, and only when that move has
// no marker of its own, so a later move of the same object by the same
// replacement body stays an ordinary zone change.
func CarryAction(marker string, obj state.ObjID, ev Event) Event {
	cause := ""
	if i := strings.IndexByte(marker, actionCauseSep[0]); i >= 0 {
		marker, cause = marker[:i], marker[i+1:]
	}
	if marker == "" || obj == 0 || ev.Kind != MoveZone || ev.Obj != obj || ev.Text != "" {
		return ev
	}
	var from state.Zone
	switch marker {
	case sacrificeText:
		from = state.ZBattlefield
	case discardText, discardCostText:
		from = state.ZHand
	default:
		return ev
	}
	if ev.From != from {
		return ev
	}
	ev.Text = marker
	if cause != "" && ev.Counter == "" {
		ev.Counter = cause
	}
	return ev
}
