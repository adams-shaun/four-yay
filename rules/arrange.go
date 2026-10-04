package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// handleArrange applies an answered KArrange decision -- the ordered-subset
// ask a mid-resolution library-arranging effect (RearrangeTopOfLibrary,
// Ponder; later Scry/Surveil/Dig) poses through effects.Host.Ask (Ruling
// J0). It is the engine's one KArrange handler, the mid-resolution sibling
// of handleModes.
//
// The contract it applies, from the KArrange doc comment: the chosen
// options, IN THE ORDER THE ANSWER GIVES THEM, become pile A; the options
// not chosen, in the order they were OFFERED, become pile B. Min/Max bound
// pile A's size (a RearrangeTopOfLibrary is Min == Max == N, so pile B is
// empty; Scry will pose Min == Max == 1 with the unchosen card heading to
// pile B / "bottom"). The new library is pile A, then pile B, then the
// untouched remainder beneath the top N. pile A index 0 is the card closest
// to the top -- the next card drawn.
//
// The reorder is recorded as a single events.LibraryOrder carrying the
// complete new library (Ruling J1), which is also what makes the answer
// replay-legitimate (Ruling J2): the logged intent plus this one event
// re-derive the same order on a reconstruction, and a fresh engine fed the
// same logged intents re-poses the same question.
//
// Ruling J4: pile B's destination is the shared Option.Kind, read off
// Options[0]. An arrange with a non-empty pile B the old handler would have
// placed directly beneath pile A is only correct when pile B is empty --
// (rearrange, whose Min == Max == N) -- so the handler now dispatches on
// that Kind:
//
//   - "" / "top": pile B beneath pile A, above the untouched remainder.
//     This is today's behaviour (RearrangeTopOfLibrary keeps it exactly).
//   - "bottom": pile B goes to the END of the library, BELOW the untouched
//     remainder. One events.LibraryOrder carrying pileA + remainder + pileB.
//     (The all-to-bottom kinds "hideaway_bottom" and "dig_bottom" are the
//     exception below, not this arm.)
//   - "graveyard": pile B leaves the library. Emit the events.LibraryOrder
//     for pileA + remainder FIRST, then one events.MoveZone per pile-B card,
//     in the player's pile-B order (see Rest below).
//
// Rest, the pile-B ORDER: an answer may carry Intent.Rest — a second ordered
// index list naming the complement of the chosen set, in the order the player
// wants pile B. Absent, the complement is taken in the order the options were
// OFFERED (the original contract). Restable marks the asks that accept it
// (Scry/Surveil, the only asks whose pile B can be non-empty AND ordered);
// Validate enforces the partition rule (validateRest is its one home).
//
// Ruling J5: the Kind is uniform across one decision; a decision whose
// options disagree is a programming error, never something a client can
// send, so it degrades with a Note and applies the Options[0] destination.
//
// `chosen` is handed on to resumeResolution, which re-enters the suspended
// resolution the way handleModes does -- the engine's one resume mechanism,
// not a second one. The re-entered effect (effRearrangeTopOfLibrary, or the
// new effScry/effSurveil) sees Ctx.Arrange set, skips the completed library
// and continues the per-library walk; after the final library only the
// chained SubAbility$ runs. A KArrange answer with no suspended
// resolution is only reachable from a hand-built decision, never from a real
// ask; it degrades with a Note rather than panicking, the same totality
// stance every handler takes.
func (e *Engine) handleArrange(d *decision.Decision, in decision.Intent) {
	// Every KArrange is posed inside a tape run's resolution, whose answer the
	// kernel serves (resolveBoard.Record -> arrangeAnswerRecord); one
	// answered through Submit has no resolution behind it.
	e.emit(events.Event{Kind: events.Note, Player: in.Player,
		Text: "arrange answered with no resolution suspended"})
}

// arrangeAnswerRecord applies an answered KArrange (handleArrange's contract
// above): the new library order, pile B's destination, and a Scry's bottom
// record. It is the decision kind's answer record, shared by handleArrange
// and the resolution kernel serving the answer from its tape
// (resolveBoard.Record); sa is the asking ability (the Scry record is
// identified by its API).
func arrangeAnswerRecord(e *Engine, d *decision.Decision, in decision.Intent, sa *cards.SA) {
	chosen := d.Chosen(in)
	// Pile A: the chosen options in the order the answer gave them.
	pileA := make([]state.ObjID, 0, len(chosen))
	chosenSet := make(map[int]bool, len(chosen))
	for _, o := range chosen {
		pileA = append(pileA, o.Obj)
		chosenSet[o.Index] = true
	}
	// Pile B: the options not chosen. When the answer carries Rest (the
	// player-chosen pile-B order, validated as a partition at Submit),
	// resolve it in the player's order; otherwise the legacy contract, the
	// complement in the order the options were OFFERED. ChosenRest's nil
	// (out-of-range index, unreachable past Validate) falls back too.
	var pileB []state.ObjID
	if rest := d.ChosenRest(in); rest != nil {
		for _, o := range rest {
			pileB = append(pileB, o.Obj)
		}
	}
	if pileB == nil {
		for _, o := range d.Options {
			if !chosenSet[o.Index] {
				pileB = append(pileB, o.Obj)
			}
		}
	}
	// Ruling J5: every option in one KArrange shares its Kind. A decision
	// whose options disagree is a programming error, never something a client
	// can send; degrade with a Note and apply the Options[0] destination.
	kind := ""
	if len(d.Options) > 0 {
		kind = d.Options[0].Kind
	}
	for _, o := range d.Options {
		if o.Kind != kind {
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "arrange options disagree on a destination"})
			break
		}
	}
	// The top N never exceeds the library (the asking effect clamps to
	// len(lib)), so the remainder slice is always valid. A suspended
	// resolution cannot have the library change underneath it, so d.Options'
	// N and the live library still agree.
	lib := e.G.Zone(state.ZLibrary, d.Player)
	k := len(d.Options)
	remainder := lib[k:]
	if kind == "dig_bottom" && len(d.ResumeDigPrimary) > 0 {
		// A Dig whose primary destination is the library and whose remainder
		// asks for an order has already appended the primary pile to the
		// library. Remove both temporary piles from the live order, then emit
		// the requested final order as one replay event: primary on top,
		// untouched cards in their existing order, remainder at the bottom.
		primary := make(map[state.ObjID]bool, len(d.ResumeDigPrimary))
		for _, id := range d.ResumeDigPrimary {
			primary[id] = true
		}
		window := make(map[state.ObjID]bool, len(d.Options))
		for _, o := range d.Options {
			window[o.Obj] = true
		}
		newLib := make([]state.ObjID, 0, len(lib))
		newLib = append(newLib, d.ResumeDigPrimary...)
		for _, id := range lib {
			if !primary[id] && !window[id] {
				newLib = append(newLib, id)
			}
		}
		newLib = append(newLib, pileA...)
		e.emit(events.Event{Kind: events.LibraryOrder, Player: d.Player,
			IDs: newLib, Secret: true})
		return
	}
	switch arrangeAnswerRecordCodes.Code(string(kind)) {
	case arrangeAnswerRecordAllToBottom:
		// The all-to-bottom shapes: Hideaway has already moved its one chosen
		// card to exile before this ask; Dig's default remainder never had a
		// take. Unlike Scry's selected subset, all remaining cards go to the
		// bottom and their ANSWER order is the bottom order
		// (CR 702.75a / Ancient Stirrings' "in any order").
		newLib := make([]state.ObjID, 0, len(remainder)+len(pileA))
		newLib = append(newLib, remainder...)
		newLib = append(newLib, pileA...)
		e.emit(events.Event{Kind: events.LibraryOrder, Player: d.Player,
			IDs: newLib, Secret: true})
	case arrangeAnswerRecordBottom:
		// Ruling J4: pile B goes to the END of the library, BELOW the
		// untouched remainder. One LibraryOrder carrying pileA + remainder +
		// pileB.
		newLib := make([]state.ObjID, 0, len(pileA)+len(pileB)+len(remainder))
		newLib = append(newLib, pileA...)
		newLib = append(newLib, remainder...)
		newLib = append(newLib, pileB...)
		e.emit(events.Event{Kind: events.LibraryOrder, Player: d.Player,
			IDs: newLib, Secret: true})
		// A completed Scry records its final bottom pile as the shared
		// events.Scry marker (task scrybottom). The answer is known here, so
		// Amount is the number the player actually chose to put on the
		// bottom -- never the number looked at, and 0 when they kept every
		// card on top. `triggerCount$ScryBottom` resolves it for The Temporal
		// Anchor's `DB$ Dig | DigNum$ X`, and the Scry matcher's
		// `ToBottom$ True` gate reads it. The verb is identified by the
		// suspended resolution's own SA, NOT by the option Kind: a plain
		// RearrangeTopOfLibrary (Ponder) shares the "bottom" Kind with an
		// always-empty pile B and must emit no scry record at all.
		if sa != nil && sa.API == "Scry" {
			e.emitScryRecord(events.Event{Kind: events.Scry, Player: d.Player,
				Obj: d.Source, Amount: int32(len(pileB))})
		}
	case arrangeAnswerRecordGraveyard:
		// Ruling J4: pile B leaves the library. The LibraryOrder for
		// pileA + remainder is emitted FIRST, then one MoveZone per pile-B
		// card in offered order. The order is a contract: a replay that
		// applies the moves before the reorder gets a different library.
		newLib := make([]state.ObjID, 0, len(pileA)+len(remainder))
		newLib = append(newLib, pileA...)
		newLib = append(newLib, remainder...)
		e.emit(events.Event{Kind: events.LibraryOrder, Player: d.Player,
			IDs: newLib, Secret: true})
		for _, id := range pileB {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id,
				From: state.ZLibrary, To: state.ZGraveyard, Player: d.Player})
		}
	default:
		// "" / "top": pile B beneath pile A, above the untouched remainder
		// (today's behaviour; RearrangeTopOfLibrary keeps it exactly).
		newLib := make([]state.ObjID, 0, len(pileA)+len(pileB)+len(remainder))
		newLib = append(newLib, pileA...)
		newLib = append(newLib, pileB...)
		newLib = append(newLib, remainder...)
		e.emit(events.Event{Kind: events.LibraryOrder, Player: d.Player,
			IDs: newLib, Secret: true})
	}
}

type arrangeAnswerRecordCode uint16

const (
	arrangeAnswerRecordAllToBottom arrangeAnswerRecordCode = iota + 1
	arrangeAnswerRecordBottom
	arrangeAnswerRecordGraveyard
)

var arrangeAnswerRecordCodes = state.NewStrCodes(
	state.StrEntry[arrangeAnswerRecordCode]{Key: "hideaway_bottom", Val: arrangeAnswerRecordAllToBottom},
	state.StrEntry[arrangeAnswerRecordCode]{Key: "dig_bottom", Val: arrangeAnswerRecordAllToBottom},
	state.StrEntry[arrangeAnswerRecordCode]{Key: "bottom", Val: arrangeAnswerRecordBottom},
	state.StrEntry[arrangeAnswerRecordCode]{Key: "graveyard", Val: arrangeAnswerRecordGraveyard},
)
