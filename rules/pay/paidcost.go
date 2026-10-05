package pay

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// PaidCost is a pending cast's or activation's payment slice (lasagna spec
// §9.1, the pay session's cast state): the objects each non-mana cost part
// was paid with, recorded by the ask stages and settled when the cost is
// paid. Package rules embeds it in its pendingCast, so the payment layer can
// read and announce the paid lists without the rest of the pending cast. The
// clone tags are read by rules' Clone generator (every list is plain data,
// deep-copied).
type PaidCost struct {
	// Sacs are the permanents chosen for the Sac parts, in part order.
	Sacs []state.ObjID `clone:"deep"`
	// Discards are the cards chosen for the Discard parts.
	Discards []state.ObjID `clone:"deep"`
	// Exiles are the cards chosen for the Exile parts (ExileFromHand /
	// ExileFromGrave). Nothing moves until the cost is paid, so an abort
	// cannot leave a partially paid exile on the board.
	Exiles []state.ObjID `clone:"deep"`
	// Reveals, Beholds, Taps and Blights are the objects the Reveal (and
	// RevealOrChoose), Behold, tapXType and Blight parts were paid with.
	Reveals, Beholds, Taps, Blights []state.ObjID `clone:"deep"`
	// RevealHandArm is parallel to Reveals: true for a hand card the REVEAL
	// arm announced, false for a permanent the CHOOSE arm of a RevealOrChoose
	// part elected off the battlefield. Only the true entries are announced
	// as reveals (EmitChoiceCosts); a chosen permanent is a public choice.
	// Plain Reveal parts and every other paid card append true.
	RevealHandArm []bool `clone:"deep"`
	// RevealedEmptyHand records that a whole-hand Reveal part was paid with
	// an EMPTY hand (CR 701.20a): still a public reveal, announced loudly.
	RevealedEmptyHand bool `clone:"deep"`
}

// RevealChosenText composes the public reveal line a RevealChosen<Spec> part
// prints as it is paid. The chosen player's identity is the chain-safe
// tossName -- the deck-identity Name, never the display PlayerName (the F3
// invariant every other event text keeps; view/describe.go's player label may
// prefer PlayerName, but that is a view projection, not chain text).
func RevealChosenText(g *state.Game, o *state.Object, spec string) (string, bool) {
	if o == nil {
		return "", false
	}
	if strings.EqualFold(spec, "Player") {
		for _, t := range o.Chosen {
			if t.IsPlayer {
				return "revealed the chosen player: " + SeatName(g, t.Player), true
			}
		}
		return "", false
	}
	if o.ChosenType == "" {
		return "", false
	}
	return "revealed the chosen creature type: " + o.ChosenType, true
}

// SaHasKeyword reports whether ab's Keyword$ tag (a comma list set by a
// keyword expansion, cards/keywords.go) contains want. It is the same tag read
// rules/statics.go's abilityConstraintMatches uses to recognise an
// Equip/Ninjutsu/Cycling ability, factored out so the offer and resolution
// halves cannot disagree about which keyword an ability belongs to.
func SaHasKeyword(ab *cards.SA, want string) bool {
	if ab == nil {
		return false
	}
	for kw := range strings.SplitSeq(ab.ParamStr(cards.PKKeyword), ",") {
		if strings.EqualFold(strings.TrimSpace(kw), want) {
			return true
		}
	}
	return false
}

// SeatName is the identity the toss Note's text carries: the deck-identity
// Name, else "seat N". F3 (TestPlayerNamesDoNotReachTheChain) keeps the
// per-seat PlayerName -- a display name -- out of the event chain entirely,
// and the Note is event text, so it uses the same deck identity every other
// event text already carries. (view/describe.go's player label may prefer
// PlayerName; that is a view projection, not chain text.)
func SeatName(g *state.Game, p state.PlayerID) string {
	pl := g.Players[p]
	if pl.Name != "" {
		return pl.Name
	}
	return "seat " + strconv.Itoa(int(p))
}

// TargetName is an object's display name for prompts and log notes: its
// face's name, else (a Face-less ability object) its source permanent's.
func TargetName(g *state.Game, source state.ObjID) string {
	if o := g.Obj(source); o != nil {
		if f := o.Face(); f != nil && f.Name != "" {
			return f.Name
		}
		if s := g.Obj(o.Source); s != nil {
			if sf := s.Face(); sf != nil && sf.Name != "" {
				return sf.Name
			}
		}
	}
	// Falling back to the literal word "target" (reviewer minor 7) is
	// deliberate: every caller has already given the decision a usable
	// prompt, so this is only reached for an object with no name at all --
	// an unreadable name there is better than a fabricated one.
	return "target"
}

// EmitConvokeTaps emits the Tap events an activation's or cast's announced
// Convoke/Harmonize/Improvise/waterbend contributions pay (CR 701.67a),
// followed by the waterbend ElementalBend marker. All of one announcement's
// taps are ONE cost action, so the Mode$ TapAll aggregate trigger fires once
// for the whole announcement, not once per tapped creature: the taps ride
// the BatchTap bracket (the tap-cost parts of EmitChoiceCosts' twin). It is
// a free function over the payment Engine -- not a method on rules' Engine --
// because the convoke emission is a cost-emission concern and the Engine's
// method surface is a shrink-only ratchet.
func EmitConvokeTaps(e Engine, card state.ObjID, player state.PlayerID, convoke []ConvokePayment) {
	e.Batch(BatchTap, true)
	waterbent := false
	for _, pay := range convoke {
		e.Emit(events.Event{Kind: events.Tap, Obj: pay.ID})
		waterbent = waterbent || pay.Waterbend
	}
	e.Batch(BatchTap, false)
	if waterbent {
		e.Emit(events.Event{Kind: events.ElementalBend, Obj: card, Player: player, Text: "water"})
	}
}

// EmitChoiceCosts announces a paid cost's non-mana choices as it is paid
// (moved from rules' emitChoiceCosts): the revealed and chosen cards, the
// revealed RevealChosen designations, the beheld cards (exiling a
// BeholdExile part's), the tapped creatures (a Crew activation's crewers,
// CR 702.122) and the Blight counters. ability is read lazily, after the
// earlier emits, exactly where the Crew tag is tested.
func EmitChoiceCosts(e Engine, paid *PaidCost, player state.PlayerID, card state.ObjID, cost *Cost, x int32, ability func() *cards.SA) {
	names := func(ids []state.ObjID) string {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			out = append(out, TargetName(e.Game(), id))
		}
		return strings.Join(out, ", ")
	}
	// A whole-hand Reveal part paid with an empty hand (Land Grant with no
	// other cards in hand): nothing else would appear in the log, so emit the
	// empty reveal as its own public note.
	if paid.RevealedEmptyHand {
		e.Emit(events.Event{Kind: events.Note, Player: player, Obj: card,
			Text: "revealed no cards (an empty hand) as a cost"})
	}
	if len(paid.Reveals) > 0 {
		// Split the paid list by arm: an announced hand reveal (a plain Reveal
		// card, the REVEAL arm of an either-or cost, or any legacy entry with no
		// arm recorded) is a public reveal; a permanent elected by the CHOOSE
		// arm is a public CHOICE, never a reveal of a hand card. Both ride the
		// same paid list the `Revealed$<Property>` refs read.
		var revealed, chosen []state.ObjID
		for i, id := range paid.Reveals {
			if i < len(paid.RevealHandArm) && !paid.RevealHandArm[i] {
				chosen = append(chosen, id)
			} else {
				revealed = append(revealed, id)
			}
		}
		if len(revealed) > 0 {
			e.Emit(events.Event{Kind: events.Note, Player: player, Obj: card,
				IDs: append([]state.ObjID(nil), revealed...), Text: "revealed " + names(revealed) + " as a cost"})
		}
		if len(chosen) > 0 {
			e.Emit(events.Event{Kind: events.Note, Player: player, Obj: card,
				IDs: append([]state.ObjID(nil), chosen...), Text: "chose " + names(chosen) + " as a cost"})
		}
	}
	// RevealChosen<Player>/<Type> parts: the payer's secret designation is
	// made public as the cost is paid. One public Note per part, naming the
	// designation (the chosen player's chain-safe tossName, or the chosen
	// creature type). Nothing is asked -- the choice was made earlier by the
	// Secretly$ True ChoosePlayer/ChooseType.
	for _, part := range cost.RevealChosen {
		if text, ok := RevealChosenText(e.Game(), e.Game().Obj(card), part.Spec); ok {
			e.Emit(events.Event{Kind: events.Note, Player: player, Obj: card, Text: text})
		}
	}
	if len(paid.Beholds) > 0 {
		e.Emit(events.Event{Kind: events.Note, Player: player, Obj: card,
			IDs: append([]state.ObjID(nil), paid.Beholds...), Text: "beheld " + names(paid.Beholds) + " as a cost"})
		// BeholdExile<N/Spec> parts (CostPart.ThenExile): the beheld objects
		// are exiled as the rest of the same payment. beholdCostAsk records
		// each part's N objects in part order, so the paid list is sliced by
		// part. The MoveZone carries the paying source in IDs, the event-
		// derived ExiledWith provenance the Champion cycle's "return the
		// exiled card to its owner's hand" (Defined$ ExiledWith) reads.
		at := 0
		for _, part := range cost.Behold {
			n := int(part.N)
			if at+n > len(paid.Beholds) {
				break
			}
			if part.ThenExile {
				for _, id := range paid.Beholds[at : at+n] {
					if o := e.Game().Obj(id); o != nil && o.Zone != state.ZExile {
						e.Emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZExile,
							IDs: []state.ObjID{card}, Text: "exiled as a cost"})
					}
				}
			}
			at += n
		}
	}
	// One payment's tap cost parts (paid.Taps: the tapXType<N/Spec> parts,
	// including a Crew ability's tapped crewers) tap their elected permanents
	// as ONE action, so the aggregate Mode$ TapAll trigger fires once for the
	// whole cost, not once per tapped permanent (task
	// cli-20261005T075020Z-05241a06). The bracket is the shared action
	// bracket. (Convoke/Harmonize/Improvise taps are emitted by the cast
	// path, not here.)
	e.Batch(BatchTap, true)
	for _, id := range paid.Taps {
		e.Emit(events.Event{Kind: events.Tap, Obj: id, Text: "tapped as a cost"})
	}
	e.Batch(BatchTap, false)
	// CR 702.122: the creatures that paid a Crew ability's tap cost crewed the
	// Vehicle. The crew keyword rides the minted Animate SA as `Keyword$ Crew`
	// (cards/kw_crew.go), so the tag -- not any card name -- is what marks this
	// activation: one Crew event per tapped crewer, pairing it with the source
	// Vehicle (card) for the Creature.CrewedThisTurn filter. The ordinary
	// tapXType costs of other abilities (Mossbridge Troll's regeneration, the
	// {T} cost) carry no Crew tag and record nothing.
	if SaHasKeyword(ability(), "Crew") {
		for _, id := range paid.Taps {
			e.Emit(events.Event{Kind: events.Crew, Obj: id, Player: player,
				IDs: []state.ObjID{card}})
		}
	}
	// CR 702.171: the creatures that paid a Saddle ability's tap cost saddled
	// the Mount, the exact mirror of the Crew record above. The saddle
	// keyword rides the minted Animate SA as `Keyword$ Saddle`
	// (cards/kw_saddle.go), so the tag -- not any card name -- marks this
	// activation: one Saddle event per tapped saddler, pairing it with the
	// source Mount (card). The Mode$ Saddled / BecomesSaddled matchers and the
	// ValidCrew$ filter read this pairing; the designation itself is the
	// separate AlterAttribute "Saddled" event.
	if SaHasKeyword(ability(), "Saddle") {
		for _, id := range paid.Taps {
			e.Emit(events.Event{Kind: events.Saddle, Obj: id, Player: player,
				IDs: []state.ObjID{card}})
		}
	}
	for i, id := range paid.Blights {
		if i < len(cost.Blight) {
			n := cost.Blight[i].N
			// An announced Blight<X> part's count is the announced X, not the
			// (unused) part.N.
			if cost.Blight[i].Announced {
				n = x
			}
			e.Emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "M1M1",
				Amount: n})
		}
	}
}
