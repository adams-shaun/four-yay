package manabrew

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

// promptPayment (scoping spec §6.3): the mid-cast mana window -> payManaCost,
// for BOTH shapes gorge poses (rules/announce_pay.go's announcedManaWindowAsk
// and rules/cast.go's legacy manaWindowAsk). Both are a KChoose decision
// whose Source is the card being paid for; they are told apart only by
// ManaPayment being present (announce) or nil (legacy) -- and even that
// distinction does not change how this function builds the prompt, because
// every field it reads comes off Options and the view, not off ManaPayment
// itself (ManaPayment.Cost/Owed/Pool have no PayManaCostInput field to land
// in; CardID/CardName/ManaCost come from the view's own printed-card
// projection, keeping one code path for both windows).
//
// Actions lists only the per-step activation options ("mana" the announce
// window's per-(source,ability,colour) offer, "activate" the legacy
// window's per-source offer, and "undo_tap"), mirroring promptPriority's own
// exclusion of pass/concede from its Actions list: "done", "autofill" and
// "cancel_cast" are answered through pay/cancel outputs instead of act, so
// listing them as actionable "act" targets would be misleading. autofill
// still gets a row so the client can show what it taps.
//
// CanConfirmFromPool is true exactly when a "done" option is offered: the
// legacy window always offers one (Done finalises whatever is currently
// payable), the announce window only for its K'rrik pay-life case
// (rules/announce_pay.go's costPayableClassLife branch) -- so the field
// tracks the real per-window affordance without needing a second one.
//
// Cancel is not separately advertised (there is no wire field for it): a
// client may always SEND cancel, and the parser below answers with the
// engine's own truth -- the legacy window never offers "cancel_cast", so a
// legacy cancel comes back invalidShape, which is exactly G-3's documented
// behaviour ("payManaCost.cancel is refused on non-AutoMana tables").
func (t *Translator) promptPayment(d *decision.Decision, v *view.View) (mb.PromptMessage, error) {
	card := findCard(v, d.Source)
	name, cost := "", ""
	if card != nil {
		name = card.Printing.Name
		cost = card.ManaCost
	}
	actions := make([]mb.PaymentAction, 0, len(d.Options))
	for _, opt := range d.Options {
		switch opt.Kind {
		case "mana", "activate":
			actions = append(actions, mb.PaymentAction{ID: actionID(opt.Index), Type: "activateAbility",
				CardID: cardID(opt.Obj), AbilityIndex: opt.Ability, Description: opt.Label,
				IsManaAbility: true, ProducedMana: manaProductions(v, opt.Obj)})
		case decision.OptUndoTap:
			actions = append(actions, mb.PaymentAction{ID: actionID(opt.Index), Type: "undoMana",
				CardID: cardID(opt.Obj), Description: opt.Label})
		case decision.OptAutoFill:
			actions = append(actions, mb.PaymentAction{ID: actionID(opt.Index), Type: "autofill", Description: opt.Label})
		}
	}
	in := mb.PayManaCostInput{
		PromptBase:         mb.PromptBase{Presentation: mb.PromptPresentation{Title: d.Prompt, Targets: []mb.TargetRef{}}},
		CardID:             cardID(d.Source),
		CardName:           name,
		ManaCost:           cost,
		CanConfirmFromPool: hasOptionKind(d, "done"),
		Actions:            actions,
	}
	return mb.PromptMessage{Kind: "prompt", AgentPrompt: mb.AgentPrompt{
		PromptID: promptID(d), DecidingPlayerID: playerID(d.Player),
		SourceCard: t.sourceCard(v, d.Source),
		Input:      mb.PromptInput{Value: in}}}, nil
}

// isManaPaymentWindow discriminates a mid-cast mana payment KChoose decision
// (-> payManaCost) from every other KChoose shape promptChoose otherwise
// handles: the announced CR 601.2g window always carries ManaPayment; the
// legacy pre-announce window (rules/cast.go's manaWindowAsk) never does, but
// always offers exactly the pair "activate" (a per-source activation) and
// "done" (finalise), the one shape that Kind combination can mean.
func isManaPaymentWindow(d *decision.Decision) bool {
	if d.ManaPayment != nil {
		return true
	}
	return hasOptionKind(d, "activate") && hasOptionKind(d, "done")
}

// hasOptionKind reports whether d offers an option of exactly this Kind.
func hasOptionKind(d *decision.Decision, kind string) bool {
	return optionIndexForKind(d, kind) >= 0
}

// optionIndexForKind is the index of d's first option of this Kind, or -1.
// Every payment-window Kind this file reads is offered at most once per
// decision (rules/announce_pay.go and rules/cast.go append each of "done",
// "autofill" and "cancel_cast" at most once).
func optionIndexForKind(d *decision.Decision, kind string) int {
	for _, opt := range d.Options {
		if opt.Kind == kind {
			return opt.Index
		}
	}
	return -1
}

// matchPaymentActionID reverses actionID's "opt-<index>" mint for an act
// response, requiring the resolved option's Kind be one of the per-step
// activation kinds this prompt advertises in Actions (never "done",
// "autofill" or "cancel_cast", which arrive through their own output types).
func matchPaymentActionID(d *decision.Decision, id string, kinds ...string) (int, bool) {
	rest, ok := strings.CutPrefix(id, "opt-")
	if !ok {
		return -1, false
	}
	idx, err := strconv.Atoi(rest)
	if err != nil || idx < 0 || idx >= len(d.Options) {
		return -1, false
	}
	for _, k := range kinds {
		if d.Options[idx].Kind == k {
			return idx, true
		}
	}
	return -1, false
}

// parsePayManaCost maps the payManaCost outputs (§6.4): act names an
// advertised per-step activation by id; pay{auto} selects "autofill" (true)
// or "done" (false) by Kind, whichever the window actually offers; cancel
// selects "cancel_cast" when offered, or invalidShape when not (G-3).
func (t *Translator) parsePayManaCost(out mb.PromptOutputValue, p *Pending) Outcome {
	d := p.Decision
	cur := p.Prompt.PromptID
	switch o := out.(type) {
	case mb.ActOutput:
		idx, ok := matchPaymentActionID(d, o.ActionID, "mana", "activate", decision.OptUndoTap)
		if !ok {
			return Outcome{Err: errCode(mb.CodeUnknownActionID,
				fmt.Sprintf("action %q is not advertised", o.ActionID), idPtr(cur))}
		}
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}
		if err := d.Validate(in); err != nil {
			return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
		}
		return Outcome{Intent: &in}
	case mb.PayOutput:
		kind := "done"
		if o.Auto {
			kind = decision.OptAutoFill
		}
		idx := optionIndexForKind(d, kind)
		if idx < 0 {
			return Outcome{Err: errCode(mb.CodeInvalidShape,
				fmt.Sprintf("%q is not offered on this payment window", kind), idPtr(cur))}
		}
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}
		if err := d.Validate(in); err != nil {
			return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
		}
		return Outcome{Intent: &in}
	case mb.CancelOutput:
		idx := optionIndexForKind(d, decision.OptCancelCast)
		if idx < 0 {
			return Outcome{Err: errCode(mb.CodeInvalidShape,
				"cancel is not offered on this payment window", idPtr(cur))}
		}
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}
		if err := d.Validate(in); err != nil {
			return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
		}
		return Outcome{Intent: &in}
	default:
		return Outcome{Err: errCode(mb.CodeWrongPromptType,
			fmt.Sprintf("prompt payManaCost does not take a %s response", out.OutputType()), idPtr(cur))}
	}
}
