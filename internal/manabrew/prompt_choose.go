package manabrew

import (
	"fmt"
	"sort"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

func (t *Translator) promptChoose(d *decision.Decision, v *view.View) (mb.PromptMessage, error) {
	if len(d.Options) == 0 {
		return mb.PromptMessage{}, ErrUnmapped
	}
	pres := mb.PromptBase{Presentation: mb.PromptPresentation{Title: d.Prompt, Description: chooseConstraint(d), Targets: []mb.TargetRef{}}}
	// The DividedAsYouChoose$ ask (effects/damage.go:97-166, spec §6.3's
	// "damage_split" row) is a real KChoose whose options name the chosen
	// targets themselves -- Option.Kind is "card" or "player", freely mixed,
	// never the literal string "damage_split". Its ResumeKind is the only
	// marker, so it is recognised before the uniform-option-kind gate below
	// (which a mixed card/player option list would otherwise fail).
	if d.ResumeKind == "damage_split" {
		options := make([]mb.SelectionOption, 0, len(d.Options))
		for _, o := range d.Options {
			options = append(options, mb.SelectionOption{Label: o.Label, Weight: 1, CanRepeat: d.Repeatable})
		}
		in := mb.PromptInputData(mb.ChooseFromSelectionInput{PromptBase: pres, Options: options, MinTotal: d.Min, MaxTotal: d.Max})
		return mb.PromptMessage{Kind: "prompt", AgentPrompt: mb.AgentPrompt{PromptID: promptID(d), DecidingPlayerID: playerID(d.Player), SourceCard: t.sourceCard(v, d.Source), Input: mb.PromptInput{Value: in}}}, nil
	}
	kind := d.Options[0].Kind
	mixedYesNo := kind == "yes" || kind == "no"
	for _, o := range d.Options {
		if o.Kind != kind && !(mixedYesNo && (o.Kind == "yes" || o.Kind == "no")) {
			return mb.PromptMessage{}, ErrUnmapped
		}
	}
	var in mb.PromptInputData
	switch kind {
	case "color", "mana":
		colors := make([]string, 0, 6)
		seen := map[string]bool{}
		for _, o := range d.Options {
			c := colorCode(o.ManaSymbol, o.Label)
			if c != "" && !seen[c] {
				colors = append(colors, c)
				seen[c] = true
			}
		}
		if len(colors) == 0 {
			return mb.PromptMessage{}, ErrUnmapped
		}
		sort.Strings(colors)
		in = mb.ChooseColorInput{PromptBase: pres, ValidColors: colors, Amount: d.Min, RepeatAllowed: d.Repeatable}
	case "yes", "no", "asunblocked":
		if d.ResumeKind == "look_ack" || (len(d.Options) == 1 && kind == "yes") {
			in = mb.ChooseFromSelectionInput{PromptBase: pres, Options: []mb.SelectionOption{{Label: d.Options[0].Label, Weight: 1}}, MinTotal: 1, MaxTotal: 1}
			break
		}
		confirm, deny := "Yes", "No"
		for _, o := range d.Options {
			if o.Kind == "yes" {
				confirm = o.Label
			}
			if o.Kind == "no" {
				deny = o.Label
			}
		}
		in = mb.ChooseBooleanInput{PromptBase: pres, ConfirmLabel: confirm, DenyLabel: deny}
	case "x", "number":
		vals := make([]int, 0, len(d.Options))
		for _, o := range d.Options {
			vals = append(vals, o.Amount)
		}
		min, max := vals[0], vals[0]
		contiguous := true
		for _, n := range vals {
			if n < min {
				min = n
			}
			if n > max {
				max = n
			}
		}
		if max-min+1 != len(vals) {
			contiguous = false
		}
		if contiguous {
			in = mb.ChooseNumberInput{PromptBase: pres, Min: min, Max: max}
		} else {
			options := make([]mb.SelectionOption, 0, len(d.Options))
			for _, o := range d.Options {
				options = append(options, mb.SelectionOption{Label: o.Label, Weight: o.Amount})
			}
			in = mb.ChooseFromSelectionInput{PromptBase: pres, Options: options, MinTotal: d.Min, MaxTotal: d.Max}
		}
	case "exile", "sacrifice", "discard", "search", "dig", "keep":
		cards := make([]mb.CardDto, 0, len(d.Options))
		for _, o := range d.Options {
			cards = append(cards, t.optionCard(v, o))
		}
		in = mb.ChooseCardsInput{PromptBase: pres, Cards: cards, Min: d.Min, Max: d.Max}
	case "damage_split", "division":
		options := make([]mb.SelectionOption, 0, len(d.Options))
		for _, o := range d.Options {
			options = append(options, mb.SelectionOption{Label: o.Label, Weight: 1, CanRepeat: d.Repeatable})
		}
		in = mb.ChooseFromSelectionInput{PromptBase: pres, Options: options, MinTotal: d.Min, MaxTotal: d.Max}
	case "name", "type", "dungeon", "room", "roll", "look_ack", "choice":
		options := make([]mb.SelectionOption, 0, len(d.Options))
		for _, o := range d.Options {
			options = append(options, mb.SelectionOption{Label: o.Label, Weight: 1})
		}
		in = mb.ChooseFromSelectionInput{PromptBase: pres, Options: options, MinTotal: d.Min, MaxTotal: d.Max}
	default:
		return mb.PromptMessage{}, fmt.Errorf("%w: choose option kind %q", ErrUnmapped, kind)
	}
	return mb.PromptMessage{Kind: "prompt", AgentPrompt: mb.AgentPrompt{PromptID: promptID(d), DecidingPlayerID: playerID(d.Player), SourceCard: t.sourceCard(v, d.Source), Input: mb.PromptInput{Value: in}}}, nil
}

func colorCode(symbol, label string) string {
	switch symbol {
	case "W", "U", "B", "R", "G", "C":
		return symbol
	}
	switch label {
	case "white", "White", "W":
		return "W"
	case "blue", "Blue", "U":
		return "U"
	case "black", "Black", "B":
		return "B"
	case "red", "Red", "R":
		return "R"
	case "green", "Green", "G":
		return "G"
	case "colorless", "Colorless", "C":
		return "C"
	}
	return ""
}

func chooseConstraint(d *decision.Decision) string {
	if d == nil {
		return ""
	}
	text := fmt.Sprintf("Choose %d to %d.", d.Min, d.Max)
	if d.HasBudget() {
		text += fmt.Sprintf(" Total value must not exceed %d.", d.MaxSum)
	}
	if d.MinSum > 0 {
		text += fmt.Sprintf(" Total value must be at least %d.", d.MinSum)
	}
	if d.GroupLimit > 1 || len(d.GroupLimits) > 0 {
		text += " Group selection limits apply."
	}
	if d.Repeatable {
		text += " Choices may be repeated."
	}
	return text
}

func (t *Translator) optionCard(v *view.View, o decision.Option) mb.CardDto {
	if c := findCard(v, o.Obj); c != nil {
		if visible, ok := t.visibleCard(*c).Value.(mb.VisibleCard); ok {
			return visible.CardDto
		}
	}
	return mb.CardDto{ID: cardID(o.Obj), Identity: mb.CardIdentity{}, Types: []string{}, Subtypes: []string{}, Supertypes: []string{}, Choices: []mb.CardChoiceDto{}, AttachmentIDs: []string{}, MergedCardIDs: []string{}, Color: []string{}, Counters: map[string]int{}}
}
