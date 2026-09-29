package manabrew

import (
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

func (t *Translator) promptMisc(d *decision.Decision, v *view.View) (mb.PromptMessage, error) {
	if len(d.Options) == 0 {
		return mb.PromptMessage{}, ErrUnmapped
	}
	pres := mb.PromptBase{Presentation: mb.PromptPresentation{Title: d.Prompt, Description: chooseConstraint(d), Targets: []mb.TargetRef{}}}
	var input mb.PromptInputData
	switch d.Kind {
	case decision.KTriggerOptional, decision.KCommanderZone:
		confirm, deny := "Yes", "No"
		if d.Kind == decision.KCommanderZone {
			confirm, deny = "Command zone", "Leave"
		}
		for _, o := range d.Options {
			if o.Kind == "yes" || o.Kind == "command_zone" || o.Kind == "apply" {
				confirm = o.Label
			}
			if o.Kind == "no" || o.Kind == "leave" || o.Kind == "decline" {
				deny = o.Label
			}
		}
		input = mb.ChooseBooleanInput{PromptBase: pres, ConfirmLabel: confirm, DenyLabel: deny}
	case decision.KReplacement:
		kind := d.Options[0].Kind
		switch kind {
		case "mana":
			colors := make([]string, 0, len(d.Options))
			for _, o := range d.Options {
				c := colorCode(o.ManaSymbol, o.Label)
				if c == "" {
					return mb.PromptMessage{}, fmt.Errorf("%w: replacement mana option %q", ErrUnmapped, o.Label)
				}
				colors = append(colors, c)
			}
			input = mb.ChooseColorInput{PromptBase: pres, ValidColors: colors, Amount: 1}
		case "apply", "decline":
			confirm, deny := "Apply", "Decline"
			for _, o := range d.Options {
				if o.Kind == "apply" {
					confirm = o.Label
				}
				if o.Kind == "decline" {
					deny = o.Label
				}
			}
			input = mb.ChooseBooleanInput{PromptBase: pres, ConfirmLabel: confirm, DenyLabel: deny}
		case "replacement":
			opts := make([]mb.SelectionOption, 0, len(d.Options))
			for _, o := range d.Options {
				opts = append(opts, mb.SelectionOption{Label: o.Label, Weight: 1})
			}
			input = mb.ChooseFromSelectionInput{PromptBase: pres, Options: opts, MinTotal: d.Min, MaxTotal: d.Max}
		default:
			return mb.PromptMessage{}, fmt.Errorf("%w: replacement option %q", ErrUnmapped, kind)
		}
	case decision.KStartingPlayer:
		opts := make([]mb.SelectionOption, 0, len(d.Options))
		for _, o := range d.Options {
			label := o.Label
			if label == "" {
				label = playerLabel(v, o.Player)
			}
			if label == "" {
				label = playerID(o.Player)
			}
			opts = append(opts, mb.SelectionOption{Label: label, Weight: 1})
		}
		input = mb.ChooseFromSelectionInput{PromptBase: pres, Options: opts, MinTotal: d.Min, MaxTotal: d.Max}
	default:
		return mb.PromptMessage{}, ErrUnmapped
	}
	return mb.PromptMessage{Kind: "prompt", AgentPrompt: mb.AgentPrompt{PromptID: promptID(d), DecidingPlayerID: playerID(d.Player), SourceCard: t.sourceCard(v, d.Source), Input: mb.PromptInput{Value: input}}}, nil
}
