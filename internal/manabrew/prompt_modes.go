package manabrew

import (
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

func (t *Translator) promptModes(d *decision.Decision, v *view.View) (mb.PromptMessage, error) {
	if len(d.Options) == 0 {
		return mb.PromptMessage{}, ErrUnmapped
	}
	pres := mb.PromptBase{Presentation: mb.PromptPresentation{Title: d.Prompt, Description: chooseConstraint(d), Targets: []mb.TargetRef{}}}
	var input mb.PromptInputData
	if d.ResumeKind == "unless_pay" || d.ResumeKind == "unless_decline" || hasOptionMode(d, decision.ModeUnlessPay) || hasOptionMode(d, decision.ModeUnlessDecline) {
		confirm, deny := "Pay", "Decline"
		for _, o := range d.Options {
			if o.Mode == decision.ModeUnlessPay {
				confirm = o.Label
			}
			if o.Mode == decision.ModeUnlessDecline {
				deny = o.Label
			}
		}
		if len(d.Options) == 1 {
			opts := []mb.SelectionOption{{Label: d.Options[0].Label, Weight: 1}}
			input = mb.ChooseFromSelectionInput{PromptBase: pres, Options: opts, MinTotal: 1, MaxTotal: 1}
		} else {
			input = mb.ChooseBooleanInput{PromptBase: pres, ConfirmLabel: confirm, DenyLabel: deny}
		}
	} else if d.Options[0].Kind == "discard" {
		// effects/cardflow.go's discardAsk poses a card-selection ask under
		// decision.KModes (not KChoose, unlike every other card-selection
		// Kind this package maps -- an existing engine-side inconsistency,
		// not something to change here): one option per eligible card, Obj
		// set, Kind uniformly "discard". It is the same wire shape as
		// promptChoose's "exile"/"sacrifice"/"discard"/... branch.
		cards := make([]mb.CardDto, 0, len(d.Options))
		for _, o := range d.Options {
			cards = append(cards, t.optionCard(v, o))
		}
		input = mb.ChooseCardsInput{PromptBase: pres, Cards: cards, Min: d.Min, Max: d.Max}
	} else {
		allMode := true
		for _, o := range d.Options {
			if o.Kind != "mode" {
				allMode = false
				break
			}
		}
		if allMode {
			opts := make([]mb.SelectionOption, 0, len(d.Options))
			for _, o := range d.Options {
				opts = append(opts, mb.SelectionOption{Label: o.Label, Weight: 1, CanRepeat: d.Repeatable})
			}
			input = mb.ChooseFromSelectionInput{PromptBase: pres, Options: opts, MinTotal: d.Min, MaxTotal: d.Max}
		} else if labelledAlternatives(d) {
			// The labelled pick-one fallback (MBX-6 fix round, seed 20260929):
			// a KModes ask whose options are NOT all modal picks but are a
			// Min==Max==1 election over plain labelled alternatives --
			// effects/cardflow.go drawFor's dredge election (ResumeKind
			// "dredge": one option per legal dredger, Kind "dredge", plus the
			// ordinary draw, Kind "draw"; the labels name what each side
			// does). It maps onto chooseFromSelection like every other
			// label-only ask -- the chosen index IS the answer, the engine
			// reads the option kind off the chosen option -- and stays
			// fail-closed for anything that is not a pick-one labelled ask.
			opts := make([]mb.SelectionOption, 0, len(d.Options))
			for _, o := range d.Options {
				opts = append(opts, mb.SelectionOption{Label: o.Label, Weight: 1})
			}
			input = mb.ChooseFromSelectionInput{PromptBase: pres, Options: opts, MinTotal: 1, MaxTotal: 1}
		} else {
			return mb.PromptMessage{}, fmt.Errorf("%w: modes option %q", ErrUnmapped, d.Options[0].Kind)
		}
	}
	return mb.PromptMessage{Kind: "prompt", AgentPrompt: mb.AgentPrompt{PromptID: promptID(d), DecidingPlayerID: playerID(d.Player), SourceCard: t.sourceCard(v, d.Source), Input: mb.PromptInput{Value: input}}}, nil
}

func hasOptionMode(d *decision.Decision, mode string) bool {
	for _, o := range d.Options {
		if o.Mode == mode {
			return true
		}
	}
	return false
}
