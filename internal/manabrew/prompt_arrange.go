package manabrew

import (
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

// promptArrange (scoping spec §6.3): KArrange splits into two wire shapes,
// told apart by the shared Option.Kind every option in one decision carries
// (Ruling J5, rules/arrange.go):
//
//   - "bottom", "graveyard", "exile", "hand": a genuine two-pile split (a
//     Scry/Surveil-style ask). -> scry, with the library-top zone first and
//     the named destination second (§6.1's ScryDestination mint).
//   - anything else -- "", "top" (a full RearrangeTopOfLibrary/Ponder
//     reorder, pile B empty) and the two all-to-bottom kinds
//     "hideaway_bottom"/"dig_bottom" (every offered card goes to ONE
//     destination and only the order matters) -- -> reorder, one item per
//     offered card.
//
// DIRECTION: unlike KTriggerOrder, neither wire shape needs a flip. The
// KArrange doc comment (decision/decision.go) says pile A index 0 -- the
// first CHOSEN option, in chosen order -- ends up closest to the top, the
// next card drawn; ManaBrew's own reorder/scry contracts say the same thing
// ("the first id ends on top"). rules/arrange.go's handleArrange builds pile
// A directly from Intent.Choices in answer order, with no placement
// inversion (KArrange has no stack to invert against), so Choices IS pile
// A/top order already.
func (t *Translator) promptArrange(d *decision.Decision, v *view.View) (mb.PromptMessage, error) {
	if arrangeIsSplit(d) {
		return t.promptArrangeScry(d, v)
	}
	return t.promptArrangeReorder(d, v)
}

// arrangeIsSplit reports whether d's shared Option.Kind names a genuine
// two-pile destination (the scry shape) rather than a single-list reorder.
// An empty option list (unreachable from a real ask; Min/Max would be 0)
// defaults to reorder, the shape that degrades most gracefully (an ordinary
// empty item list).
//
// Min == Max always means reorder, REGARDLESS of Kind: a mandatory total
// move sends every offered card to the SAME single destination (only the
// order is chosen), even when that destination happens to reuse a Kind
// string a genuine split also uses -- effects/cardflow.go's NumCards$ "look
// at N, put them all on the bottom in order" ask poses Min == Max == k with
// Option.Kind "bottom", the identical string a Scry/Surveil pile-B
// destination carries, but there is no second pile at all: unlike it,
// Scry/Surveil always pose Min 0 (effects/cardflow.go's effLookAndArrange),
// which is what actually signals "some subset may go elsewhere" (MB-8
// census fb-20260929, found by playing real repo-deck games: this ask was
// misrouted to promptArrangeScry, which does not carry the Min bound at all
// on the wire, so a "put nothing on top" answer -- always legal for a real
// Scry/Surveil -- came back invalidShape here for want 3..3).
func arrangeIsSplit(d *decision.Decision) bool {
	if len(d.Options) == 0 || d.Min == d.Max {
		return false
	}
	switch d.Options[0].Kind {
	case "bottom", "graveyard", "exile", "hand":
		return true
	default:
		// "" / "top" (full reorder) and "hideaway_bottom" / "dig_bottom"
		// (all-to-one-destination, order only) all stay a single list.
		return false
	}
}

// arrangeDestination maps a split ask's shared Option.Kind onto the second
// ScryDestination (the first is always libraryTop, the pile-A destination).
func arrangeDestination(kind string) mb.ScryDestination {
	switch kind {
	case "graveyard":
		return mb.DestinationGraveyard
	case "exile":
		return mb.DestinationExile
	case "hand":
		return mb.DestinationHand
	default:
		// "bottom", and the totality default for anything else reaching
		// this branch (arrangeIsSplit's own switch already narrows the set).
		return mb.DestinationLibraryBottom
	}
}

// promptArrangeScry builds the split ask: every offered card once, and the
// two destinations pile A/B may land in. The prompt itself carries no pile
// assignment -- ZoneCardIDs in the answer is the whole partition.
func (t *Translator) promptArrangeScry(d *decision.Decision, v *view.View) (mb.PromptMessage, error) {
	cards := make([]mb.CardDto, 0, len(d.Options))
	for _, opt := range d.Options {
		cards = append(cards, t.optionCard(v, opt))
	}
	dest := arrangeDestination(d.Options[0].Kind)
	in := mb.ScryInput{
		PromptBase: mb.PromptBase{Presentation: mb.PromptPresentation{Title: d.Prompt, Targets: []mb.TargetRef{}}},
		Cards:      cards,
		Zones:      []mb.ScryDestination{mb.DestinationLibraryTop, dest},
	}
	return mb.PromptMessage{Kind: "prompt", AgentPrompt: mb.AgentPrompt{
		PromptID: promptID(d), DecidingPlayerID: playerID(d.Player),
		SourceCard: t.sourceCard(v, d.Source),
		Input:      mb.PromptInput{Value: in}}}, nil
}

// promptArrangeReorder builds the single-list ask: one ReorderItem per
// offered card, in Option order (no flip -- see the promptArrange doc).
func (t *Translator) promptArrangeReorder(d *decision.Decision, v *view.View) (mb.PromptMessage, error) {
	items := make([]mb.ReorderItem, len(d.Options))
	for i, opt := range d.Options {
		card := t.optionCard(v, opt)
		items[i] = mb.ReorderItem{ID: actionID(opt.Index), Card: &card, Oracle: opt.Label}
	}
	in := mb.ReorderInput{
		PromptBase: mb.PromptBase{Presentation: mb.PromptPresentation{Title: d.Prompt, Targets: []mb.TargetRef{}}},
		Items:      items,
	}
	return mb.PromptMessage{Kind: "prompt", AgentPrompt: mb.AgentPrompt{
		PromptID: promptID(d), DecidingPlayerID: playerID(d.Player),
		SourceCard: t.sourceCard(v, d.Source),
		Input:      mb.PromptInput{Value: in}}}, nil
}

// parseArrangeReorder maps reorderDecision{orderedIds} for the KArrange
// single-list shape: OrderedIDs[i] IS Choices[i], no reversal (see the
// promptArrange doc's DIRECTION note).
func (t *Translator) parseArrangeReorder(out mb.PromptOutputValue, p *Pending) Outcome {
	d := p.Decision
	cur := p.Prompt.PromptID
	dec, ok := out.(mb.ReorderDecision)
	if !ok {
		return Outcome{Err: errCode(mb.CodeWrongPromptType, "not a reorderDecision answer", idPtr(cur))}
	}
	choices, err := matchActionIDs(d, dec.OrderedIDs)
	if err != nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}
	if err := d.Validate(in); err != nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
	}
	return Outcome{Intent: &in}
}

// parseArrangeScry maps scryDecision{zoneCardIds}, parallel to the prompt's
// Zones ([libraryTop, <destination>]): ZoneCardIDs[0] is pile A (Choices, in
// the player's order); ZoneCardIDs[1] is pile B, carried as Intent.Rest only
// when the ask is Restable (§6.3: "Rest when Restable, otherwise its order
// is dropped" -- a non-Restable ask still gets the correct legacy-contract
// pile B, built server-side from the complement in offered order, exactly as
// an empty Rest already means).
func (t *Translator) parseArrangeScry(out mb.PromptOutputValue, p *Pending) Outcome {
	d := p.Decision
	cur := p.Prompt.PromptID
	dec, ok := out.(mb.ScryDecision)
	if !ok {
		return Outcome{Err: errCode(mb.CodeWrongPromptType, "not a scryDecision answer", idPtr(cur))}
	}
	if len(dec.ZoneCardIDs) < 1 {
		return Outcome{Err: errCode(mb.CodeInvalidShape, "scryDecision carries no zones", idPtr(cur))}
	}
	choices, err := matchCardIDs(d, dec.ZoneCardIDs[0])
	if err != nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}
	if d.Restable && len(dec.ZoneCardIDs) > 1 && len(dec.ZoneCardIDs[1]) > 0 {
		rest, err := matchCardIDs(d, dec.ZoneCardIDs[1])
		if err != nil {
			return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
		}
		in.Rest = rest
	}
	if err := d.Validate(in); err != nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
	}
	return Outcome{Intent: &in}
}

// matchCardIDs resolves a list of ManaBrew card ids against d's options by
// their Obj (cardID's mint), in the given order. Used by the scry parser,
// where an option is identified by the card it carries rather than by a
// display-position id (a scry prompt's Cards list carries no separate
// per-option action id).
func matchCardIDs(d *decision.Decision, ids []string) ([]int, error) {
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		idx := -1
		for _, opt := range d.Options {
			if cardID(opt.Obj) == id {
				idx = opt.Index
				break
			}
		}
		if idx < 0 {
			return nil, fmt.Errorf("card %s is not offered", id)
		}
		out = append(out, idx)
	}
	return out, nil
}
