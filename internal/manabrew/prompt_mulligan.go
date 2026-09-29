package manabrew

import (
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

// promptMulligan (scoping spec §6.3): London's two asks. The keep/mulligan
// ask carries the seat's hand ids (the seat reads its own hand); the
// bottoming ask carries the full card projection of that hand and the
// bottoming count. The two are told apart by the options' kinds ("keep" and
// "mulligan" versus one "bottom" option per hand card).
func (t *Translator) promptMulligan(d *decision.Decision, v *view.View) (mb.PromptMessage, error) {
	for _, opt := range d.Options {
		if opt.Kind == "keep" || opt.Kind == "mulligan" {
			return t.promptMulliganKeep(d, v)
		}
	}
	return t.promptMulliganBottom(d, v)
}

// handViews is the deciding player's hand projection, in hand order. A nil
// view or an absent hand yields nothing -- the keep ask degenerates to an
// empty handCardIds list rather than an error, because the answer (keep) is
// still legal.
func (t *Translator) handViews(d *decision.Decision, v *view.View) []view.CardView {
	if v == nil {
		return nil
	}
	for _, p := range v.Players {
		if p.ID == d.Player && p.Hand != nil {
			return p.Hand
		}
	}
	return nil
}

// promptMulliganKeep builds the London keep/mulligan ask. MulliganCount
// (mulligans taken so far) is not carried on the decision -- the engine
// keeps it in the mulligan round's own state, which the adapter cannot see
// -- so it reports 0 (G-7 style: absent information becomes a default, the
// prompt text and the option list stay the source of truth).
func (t *Translator) promptMulliganKeep(d *decision.Decision, v *view.View) (mb.PromptMessage, error) {
	hand := t.handViews(d, v)
	ids := make([]string, 0, len(hand))
	for i := range hand {
		ids = append(ids, cardID(hand[i].ID))
	}
	in := mb.MulliganInput{HandCardIDs: ids, MulliganCount: 0}
	return mb.PromptMessage{Kind: "prompt", AgentPrompt: mb.AgentPrompt{
		PromptID: promptID(d), DecidingPlayerID: playerID(d.Player),
		Input: mb.PromptInput{Value: in}}}, nil
}

// promptMulliganBottom builds the London bottoming ask: the parallel
// handCardIds and card projection lists, and the count to bottom (d.Min,
// which the engine sets == d.Max).
func (t *Translator) promptMulliganBottom(d *decision.Decision, v *view.View) (mb.PromptMessage, error) {
	hand := t.handViews(d, v)
	ids := make([]string, 0, len(hand))
	cards := make([]mb.CardDto, 0, len(hand))
	for i := range hand {
		ids = append(ids, cardID(hand[i].ID))
		card, ok := t.visibleCard(hand[i]).Value.(mb.VisibleCard)
		if !ok {
			continue
		}
		cards = append(cards, card.CardDto)
	}
	if len(ids) != len(cards) {
		// The parallel lists must stay parallel; a projection that could not
		// decode is a bug, so say so rather than send misaligned lists.
		return mb.PromptMessage{}, fmt.Errorf("manabrew: mulliganPutBack hand projection is not decodable")
	}
	in := mb.MulliganPutBackInput{HandCardIDs: ids, Cards: cards, Count: d.Min}
	return mb.PromptMessage{Kind: "prompt", AgentPrompt: mb.AgentPrompt{
		PromptID: promptID(d), DecidingPlayerID: playerID(d.Player),
		Input: mb.PromptInput{Value: in}}}, nil
}

// parseMulligan maps mulliganDecision{keep}: keep answers the "keep" option;
// mulligan answers the "mulligan" option and is refused (invalidShape) when
// the engine offered keep only, because the allowance is spent.
func (t *Translator) parseMulligan(out mb.PromptOutputValue, p *Pending) Outcome {
	d := p.Decision
	cur := p.Prompt.PromptID
	dec, ok := out.(mb.MulliganDecision)
	if !ok {
		return Outcome{Err: errCode(mb.CodeWrongPromptType, "not a mulliganDecision answer", idPtr(cur))}
	}
	kind := "keep"
	if !dec.Keep {
		kind = "mulligan"
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == kind {
			idx = opt.Index
			break
		}
	}
	if idx < 0 {
		return Outcome{Err: errCode(mb.CodeInvalidShape,
			fmt.Sprintf("no %q option is offered", kind), idPtr(cur))}
	}
	intent := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}
	if err := d.Validate(intent); err != nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
	}
	return Outcome{Intent: &intent}
}

// parseMulliganPutBack maps mulliganPutBackDecision: each cardId names one
// "bottom" option; the count is what Decision.Validate says (Min == Max).
func (t *Translator) parseMulliganPutBack(out mb.PromptOutputValue, p *Pending) Outcome {
	d := p.Decision
	cur := p.Prompt.PromptID
	dec, ok := out.(mb.MulliganPutBackDecision)
	if !ok {
		return Outcome{Err: errCode(mb.CodeWrongPromptType, "not a mulliganPutBackDecision answer", idPtr(cur))}
	}
	choices := make([]int, 0, len(dec.CardIDs))
	for _, cid := range dec.CardIDs {
		idx := -1
		for _, opt := range d.Options {
			if cardID(opt.Obj) == cid {
				idx = opt.Index
				break
			}
		}
		if idx < 0 {
			return Outcome{Err: errCode(mb.CodeInvalidShape,
				fmt.Sprintf("card %s is not in the hand", cid), idPtr(cur))}
		}
		choices = append(choices, idx)
	}
	intent := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}
	if err := d.Validate(intent); err != nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
	}
	return Outcome{Intent: &intent}
}
