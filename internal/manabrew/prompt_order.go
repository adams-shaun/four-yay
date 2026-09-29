package manabrew

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

// promptOrder (scoping spec §6.3): KTriggerOrder -> reorder. One ReorderItem
// per offered trigger, in Option order; the item id is the generic
// actionID(index) mint (the same one prompt_priority.go uses), so an answer
// is matched back to its option purely by id, independent of display order.
//
// The DIRECTION FLIP the ticket names lives entirely in the RESPONSE half
// (parseTriggerOrder below), not here: gorge's KTriggerOrder doc comment says
// Options carry no resolution order of their own -- the player's answer
// determines placement -- so the prompt's Items array can be built in plain
// Option order with no meaning lost. What must not get backwards is the
// translation of a submitted "first resolves first" order into gorge's
// "Choices[0] is placed FIRST, therefore resolves LAST" placement order
// (CR 603.3b): that translation is the one reversal, applied once, in
// parseTriggerOrder.
func (t *Translator) promptOrder(d *decision.Decision, v *view.View) (mb.PromptMessage, error) {
	items := make([]mb.ReorderItem, len(d.Options))
	for i, opt := range d.Options {
		items[i] = mb.ReorderItem{ID: actionID(opt.Index), Card: t.sourceCard(v, opt.Obj), Oracle: opt.Label}
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

// parseTriggerOrder maps reorderDecision{orderedIds} for a KTriggerOrder
// prompt (§6.3, §6.4): OrderedIDs[0] is the trigger the client wants to
// resolve FIRST. gorge places Intent.Choices[0] on the stack FIRST, and a
// stack resolves LIFO, so the trigger that must resolve first has to be the
// LAST element of Choices. Reversing once here is the whole contract; get it
// backwards and every two-or-more-trigger order silently swaps end to end,
// which is exactly why the ticket wants a live-engine test on it
// (TestTriggerOrderFirstIdResolvesFirst).
func (t *Translator) parseTriggerOrder(out mb.PromptOutputValue, p *Pending) Outcome {
	d := p.Decision
	cur := p.Prompt.PromptID
	dec, ok := out.(mb.ReorderDecision)
	if !ok {
		return Outcome{Err: errCode(mb.CodeWrongPromptType, "not a reorderDecision answer", idPtr(cur))}
	}
	idxs, err := matchActionIDs(d, dec.OrderedIDs)
	if err != nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
	}
	choices := make([]int, len(idxs))
	for i, idx := range idxs {
		choices[len(idxs)-1-i] = idx
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}
	if err := d.Validate(in); err != nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
	}
	return Outcome{Intent: &in}
}

// matchActionIDs reverses actionID's "opt-<index>" mint for a whole ordered
// id list, shared by the reorder parsers (trigger order and the KArrange
// full-order/hideaway_bottom/dig_bottom shape): a well-formed id names an
// offered option purely by its encoded index, independent of any array
// position, so this never needs the option list's Obj at all.
func matchActionIDs(d *decision.Decision, ids []string) ([]int, error) {
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		rest, ok := strings.CutPrefix(id, "opt-")
		if !ok {
			return nil, fmt.Errorf("id %q does not name an offered option", id)
		}
		idx, err := strconv.Atoi(rest)
		if err != nil || idx < 0 || idx >= len(d.Options) {
			return nil, fmt.Errorf("id %q does not name an offered option", id)
		}
		out = append(out, idx)
	}
	return out, nil
}
