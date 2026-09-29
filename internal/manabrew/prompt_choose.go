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
	// The two mana-payment windows (rules/announce_pay.go's announced CR
	// 601.2g window and rules/cast.go's legacy manaWindowAsk) are KChoose
	// decisions whose option list mixes several Kinds ("mana"/"activate"
	// alongside OptAutoFill/OptCancelCast/"done"), which the uniform-Kind
	// gate below would otherwise reject outright as ErrUnmapped. Route them
	// to promptPayment first: the announce window always carries
	// ManaPayment, and the legacy window's discriminator is its own fixed
	// shape (prompt_payment.go's manaWindowAsk doc), an "activate" option
	// alongside a "done" option.
	if isManaPaymentWindow(d) {
		return t.promptPayment(d, v)
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

// parseChooseNumber maps chooseNumber's numberDecision (§6.4) for the two
// promptChoose shapes that build it -- a contiguous "x"/"number" range --
// back to the one option whose Amount equals the chosen value.
func (t *Translator) parseChooseNumber(out mb.PromptOutputValue, p *Pending) Outcome {
	d := p.Decision
	cur := p.Prompt.PromptID
	dec, ok := out.(mb.NumberDecision)
	if !ok {
		return Outcome{Err: errCode(mb.CodeWrongPromptType, "not a numberDecision answer", idPtr(cur))}
	}
	if dec.ChosenNumber == nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, "chooseNumber requires a chosenNumber", idPtr(cur))}
	}
	idx := -1
	for _, o := range d.Options {
		if o.Amount == *dec.ChosenNumber {
			idx = o.Index
			break
		}
	}
	if idx < 0 {
		return Outcome{Err: errCode(mb.CodeInvalidShape,
			fmt.Sprintf("%d is not an offered value", *dec.ChosenNumber), idPtr(cur))}
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}
	if err := d.Validate(in); err != nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
	}
	return Outcome{Intent: &in}
}

// parseChooseCards maps chooseCards's chosenCardIds (§6.4) back to the
// offered options by the card each carries in Obj, reusing the same
// card-id-keyed matcher the KArrange scry parser uses (matchCardIDs):
// unlike an action-id list, a chooseCards prompt's Cards carry no separate
// per-option display id, only the card itself.
func (t *Translator) parseChooseCards(out mb.PromptOutputValue, p *Pending) Outcome {
	d := p.Decision
	cur := p.Prompt.PromptID
	dec, ok := out.(mb.ChooseCardsDecision)
	if !ok {
		return Outcome{Err: errCode(mb.CodeWrongPromptType, "not a chooseCardsDecision answer", idPtr(cur))}
	}
	choices, err := matchCardIDs(d, dec.ChosenCardIDs)
	if err != nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}
	if err := d.Validate(in); err != nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
	}
	return Outcome{Intent: &in}
}

// parseChooseColor maps chooseColor's colorDecision (§6.4): chosenColors is a
// count per colour code, expanded into that many repeats of the matching
// option's index. The map is walked in sorted key order -- never Go's own
// map range order -- so the resulting Choices list, and therefore any event
// it feeds, is deterministic (AGENTS.md's "no map range where iteration
// order can reach an event").
func (t *Translator) parseChooseColor(out mb.PromptOutputValue, p *Pending) Outcome {
	d := p.Decision
	cur := p.Prompt.PromptID
	dec, ok := out.(mb.ColorDecision)
	if !ok {
		return Outcome{Err: errCode(mb.CodeWrongPromptType, "not a colorDecision answer", idPtr(cur))}
	}
	colors := make([]string, 0, len(dec.ChosenColors))
	for c := range dec.ChosenColors {
		colors = append(colors, c)
	}
	sort.Strings(colors)
	choices := make([]int, 0, len(colors))
	for _, c := range colors {
		n := dec.ChosenColors[c]
		idx := -1
		for _, o := range d.Options {
			if colorCode(o.ManaSymbol, o.Label) == c {
				idx = o.Index
				break
			}
		}
		if idx < 0 {
			return Outcome{Err: errCode(mb.CodeInvalidShape, fmt.Sprintf("color %q is not offered", c), idPtr(cur))}
		}
		for i := 0; i < n; i++ {
			choices = append(choices, idx)
		}
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}
	if err := d.Validate(in); err != nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
	}
	return Outcome{Intent: &in}
}

// parseChooseBoolean maps chooseBoolean's decision{value} (§6.4) -- shared by
// promptChoose's yes/no/asunblocked shape, promptModes' unless-pay shape and
// promptMisc's trigger-optional/commander-zone/replacement-apply shapes --
// back to whichever option is the confirm or deny side (booleanOptionIndex).
func (t *Translator) parseChooseBoolean(out mb.PromptOutputValue, p *Pending) Outcome {
	d := p.Decision
	cur := p.Prompt.PromptID
	dec, ok := out.(mb.BooleanDecision)
	if !ok {
		return Outcome{Err: errCode(mb.CodeWrongPromptType, "not a decision (chooseBoolean) answer", idPtr(cur))}
	}
	idx, err := booleanOptionIndex(d, dec.Value)
	if err != nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}
	if err := d.Validate(in); err != nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
	}
	return Outcome{Intent: &in}
}

// booleanOptionIndex mirrors the confirm/deny discovery every chooseBoolean
// builder (promptChoose, promptModes, promptMisc) uses, so a boolean answer
// resolves to exactly the option each builder's own label promised:
//   - an explicit Kind "yes"/"command_zone"/"apply", or Option.Mode
//     ModeUnlessPay, is the confirm (true) side;
//   - an explicit Kind "no"/"leave"/"decline", or Option.Mode
//     ModeUnlessDecline, is the deny (false) side;
//   - a plain two-option list that carries none of those (the KChoose
//     "asunblocked" election, rules/combat.go's askNextCombatAsk, whose two
//     options are both literally Kind "asunblocked" and carry no marker of
//     their own) falls back to that ask's fixed build order: index 0 is the
//     declined election ("assign normally"), index 1 is the accepted one
//     ("assign as though not blocked").
func booleanOptionIndex(d *decision.Decision, value bool) (int, error) {
	confirm, deny := -1, -1
	for _, o := range d.Options {
		switch {
		case o.Kind == "yes", o.Kind == "command_zone", o.Kind == "apply", o.Mode == decision.ModeUnlessPay:
			confirm = o.Index
		case o.Kind == "no", o.Kind == "leave", o.Kind == "decline", o.Mode == decision.ModeUnlessDecline:
			deny = o.Index
		}
	}
	if confirm < 0 && deny < 0 && len(d.Options) == 2 {
		deny, confirm = d.Options[0].Index, d.Options[1].Index
	}
	if value {
		if confirm < 0 {
			return 0, fmt.Errorf("no confirm option is offered")
		}
		return confirm, nil
	}
	if deny < 0 {
		return 0, fmt.Errorf("no deny option is offered")
	}
	return deny, nil
}

// parseChooseFromSelection maps chooseFromSelection's selectionDecision
// (§6.4) back to the offered options: every ChooseFromSelectionInput this
// package builds (promptChoose's several kinds, promptModes, promptMisc's
// replacement/starting-player shapes) lists its SelectionOptions in the same
// order as d.Options, so ChosenIndices is Choices with no remapping --
// Decision.Validate is what catches an out-of-range or duplicate index.
func (t *Translator) parseChooseFromSelection(out mb.PromptOutputValue, p *Pending) Outcome {
	d := p.Decision
	cur := p.Prompt.PromptID
	dec, ok := out.(mb.SelectionDecision)
	if !ok {
		return Outcome{Err: errCode(mb.CodeWrongPromptType, "not a selectionDecision answer", idPtr(cur))}
	}
	choices := make([]int, len(dec.ChosenIndices))
	copy(choices, dec.ChosenIndices)
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}
	if err := d.Validate(in); err != nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
	}
	return Outcome{Intent: &in}
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
