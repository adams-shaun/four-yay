package manabrew

import (
	"fmt"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

// promptChoose maps a KChoose ask onto its wire prompt. The bool it returns
// is the MBX-7 fallback flag: true exactly when the ask had no specific
// mapping (an option shape this package does not know) and the prompt is
// fallbackChoose's generic, always-answerable selection over the native
// option labels instead. The MB-8 census rejects a pose that ERRORS
// (ErrUnmapped) -- that is the unanswerable prompt that deadlocked the ManaBrew
// seat on Council's Judgment -- so no live KChoose returns one any more:
// every miss falls back, and the census counts the fallback separately
// (mbtest.Census.Fallback) so the unmapped counter stays the honest
// "no prompt at all" register.
func (t *Translator) promptChoose(d *decision.Decision, v *view.View) (mb.PromptMessage, bool, error) {
	// An option-less KChoose (Min 0, e.g. an empty ballot) poses the generic
	// selection with no options: a Min-0 client answer (an empty selection)
	// is exactly the engine's own legal answer, so the prompt is answerable
	// whenever the engine's ask is.
	if len(d.Options) == 0 {
		return t.fallbackChoose(d, v, true)
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
		msg, err := t.promptPayment(d, v)
		return msg, false, err
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
		return mb.PromptMessage{Kind: "prompt", AgentPrompt: mb.AgentPrompt{PromptID: promptID(d), DecidingPlayerID: playerID(d.Player), SourceCard: t.sourceCard(v, d.Source), Input: mb.PromptInput{Value: in}}}, false, nil
	}
	// A mid-resolution target/player pick (effects/targets_ask.go's
	// poseTargetsAsk, resumed as "tgts" or "choice", and the many
	// "choice"-resumed asks in effects/choose_control.go): a KChoose whose
	// options are plain TARGET REFERENCES -- Option.Kind uniformly "player"
	// (a player reference) or "card" (a permanent/card reference), freely
	// mixed, distinct from damage_split's identical Kind vocabulary only by
	// what the ask MEANS (damage_split is caught above by its own
	// ResumeKind first). This is exactly promptTarget's own KTarget shape,
	// reused here because these asks are modelled as KChoose rather than
	// KTarget (an existing engine-side inconsistency, not something to
	// change here) -- so it maps onto the identical chooseBoardTargets wire
	// prompt (MB-8 census fb-20260929: found via real repo-deck games, seed
	// 8004, ResumeKind "tgts").
	if isTargetRefOptions(d.Options) {
		intent, hostile := targetingIntent(d.TargetEffect)
		in := boardTargetInput(d, intent, hostile)
		return mb.PromptMessage{Kind: "prompt", AgentPrompt: mb.AgentPrompt{PromptID: promptID(d), DecidingPlayerID: playerID(d.Player), SourceCard: t.sourceCard(v, d.Source), Input: mb.PromptInput{Value: in}}}, false, nil
	}
	// The flexible-pip announcement ask (rules/cumulative.go's pipAnnounceAsk,
	// shared by cast-time hybrid/Phyrexian pips, Echo and cumulative upkeep):
	// "choose how to pay this mana symbol" offers one option per alternative
	// FACE, each carrying its own Kind ("pay_W"/"pay_U"/.../"pay_generic"/
	// "pay_life") -- a mixed-Kind list the uniform gate below would otherwise
	// reject, even though it is always Min==Max==1 over plain labelled
	// alternatives (never a card or player), so it maps onto
	// chooseFromSelection exactly like the other label-only asks below.
	// The Convoke/Harmonize/Improvise/Waterbend announcement ask
	// (rules/cast.go convokeAsk): one option per legal contribution ("Tap
	// <creature> for W", "... for 1", ...), mixed Kinds, Min 0 up to the
	// cost's outstanding slot count, each battlefield object's options in
	// one payment Group so one object is committed to at most one payment.
	// The payment GROUPS are not expressible on the wire (spec G-4:
	// Validate is the fence, rejections counted in the census), but the ask
	// itself is a plain multi-select over labelled alternatives:
	// chooseFromSelection by index (chooseCards would be ambiguous -- the
	// same creature carries several options), the engine's own
	// validateCastContributions gating the answer.
	if isCastContributionOptions(d.Options) {
		options := make([]mb.SelectionOption, 0, len(d.Options))
		for _, o := range d.Options {
			options = append(options, mb.SelectionOption{Label: o.Label, Weight: 1})
		}
		in := mb.PromptInputData(mb.ChooseFromSelectionInput{PromptBase: pres, Options: options, MinTotal: d.Min, MaxTotal: d.Max})
		return mb.PromptMessage{Kind: "prompt", AgentPrompt: mb.AgentPrompt{PromptID: promptID(d), DecidingPlayerID: playerID(d.Player), SourceCard: t.sourceCard(v, d.Source), Input: mb.PromptInput{Value: in}}}, false, nil
	}
	if allPayPipOptions(d.Options) {
		options := make([]mb.SelectionOption, 0, len(d.Options))
		for _, o := range d.Options {
			options = append(options, mb.SelectionOption{Label: o.Label, Weight: 1})
		}
		in := mb.PromptInputData(mb.ChooseFromSelectionInput{PromptBase: pres, Options: options, MinTotal: d.Min, MaxTotal: d.Max})
		return mb.PromptMessage{Kind: "prompt", AgentPrompt: mb.AgentPrompt{PromptID: promptID(d), DecidingPlayerID: playerID(d.Player), SourceCard: t.sourceCard(v, d.Source), Input: mb.PromptInput{Value: in}}}, false, nil
	}
	// The api:ChangeText word ask (effects/changetext.go effChangeText,
	// ResumeKind "changetext"): one option per word candidate, Kind
	// "changetext_from"/"changetext_to" freely mixed (a two-half ask names
	// one candidate list per half, Min==Max==2; a one-half ask is uniform
	// and Min==Max==1). Every option is a plain labelled alternative and
	// the engine reads each answered option's Kind off the answer itself
	// (effChangeText applies the answer in place), so any d.Min distinct
	// picks is a well-formed answer and a lopsided one (two candidates
	// from the same half) falls back deterministically engine-side
	// instead of re-asking (the effect's documented malformed-answer
	// contract).
	if changetextOptions(d.Options) {
		options := make([]mb.SelectionOption, 0, len(d.Options))
		for _, o := range d.Options {
			options = append(options, mb.SelectionOption{Label: o.Label, Weight: 1})
		}
		in := mb.PromptInputData(mb.ChooseFromSelectionInput{PromptBase: pres, Options: options, MinTotal: d.Min, MaxTotal: d.Max})
		return mb.PromptMessage{Kind: "prompt", AgentPrompt: mb.AgentPrompt{PromptID: promptID(d), DecidingPlayerID: playerID(d.Player), SourceCard: t.sourceCard(v, d.Source), Input: mb.PromptInput{Value: in}}}, false, nil
	}
	kind := d.Options[0].Kind
	election := electionKinds[kind]
	uniform := true
	for _, o := range d.Options {
		if o.Kind != kind && !(election && electionKinds[o.Kind]) {
			uniform = false
			break
		}
	}
	if !uniform {
		// A mixed-Kind list none of the recognised mixed shapes above claims:
		// MBX-7's class closure. Before this ticket such a list returned
		// ErrUnmapped -- the unanswerable prompt that deadlocked a ManaBrew
		// seat -- so any new option shape a card poses falls back to the
		// generic label selection instead, answered by native option index.
		return t.fallbackChoose(d, v, true)
	}
	var in mb.PromptInputData
	switch kind {
	case "color", "mana":
		// "mana" is shared by two different asks (rules/mana_activation.go):
		// a genuine colour pick (a Produced$ Combo ability flattened into one
		// option per colour, every option carrying a real ManaSymbol), and
		// "choose a mana ability of <source>" when a permanent has several
		// mana abilities that are not all flattenable colours (a plain
		// activation pick whose Label is the ability's own description, e.g.
		// "Tap: Add {C}", with no ManaSymbol at all). Only the FIRST shape
		// maps onto chooseColor; every option must resolve a colour for that
		// -- silently keeping only the options that happen to, and dropping
		// the rest, would narrow what the client is offered below what the
		// engine actually allows. The second shape is a plain labelled pick,
		// -> chooseFromSelection, exactly like the other label-only asks
		// below.
		colors := make([]string, 0, 6)
		seen := map[string]bool{}
		allColor := true
		for _, o := range d.Options {
			c := colorCode(o.ManaSymbol, o.Label)
			if c == "" {
				allColor = false
				break
			}
			if !seen[c] {
				colors = append(colors, c)
				seen[c] = true
			}
		}
		if allColor && len(colors) > 0 {
			sort.Strings(colors)
			in = mb.ChooseColorInput{PromptBase: pres, ValidColors: colors, Amount: d.Min, RepeatAllowed: d.Repeatable}
		} else {
			options := make([]mb.SelectionOption, 0, len(d.Options))
			for _, o := range d.Options {
				options = append(options, mb.SelectionOption{Label: o.Label, Weight: 1})
			}
			in = mb.ChooseFromSelectionInput{PromptBase: pres, Options: options, MinTotal: d.Min, MaxTotal: d.Max}
		}
	case "yes", "no", "asunblocked", "trigger_cost_pay", "trigger_cost_decline":
		// trigger_cost_pay/trigger_cost_decline are rules/cumulative.go's
		// optional triggered-cost election ("pay <cost label>?" -- pay or
		// decline), the same two-sided shape.
		confirm, deny := "Yes", "No"
		for _, o := range d.Options {
			if o.Kind == "yes" || o.Kind == "trigger_cost_pay" {
				confirm = o.Label
			}
			if o.Kind == "no" || o.Kind == "trigger_cost_decline" {
				deny = o.Label
			}
		}
		in = booleanElection(pres, d, confirm, deny)
	case "x", "number", "move_counter":
		// "move_counter" is effects/counters.go MoveCounter's CounterNum$ Any
		// amount pick (ResumeKind "move_counter"): one option per legal
		// amount, 0..max, each carrying the number in Amount -- the same
		// value-pick shape as the x/number asks, mapped onto chooseNumber
		// (parseChooseNumber reads the answer back off the option whose
		// Amount equals the chosen value).
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
	case "exile", "sacrifice", "discard", "search", "dig", "keep",
		"exilecost", "revealcost", "beholdcost", "returncost", "hand_move", "hidden_pick", "reveal", "vote_card":
		// "vote_card" is api:Vote's candidate ballot (effects/misc.go's
		// askCardVote, MBX-7): one option per ballot permanent, Obj the
		// permanent's id, Label its name, Player its controller (CR 400.2,
		// the fact the voter's own policy needs to prefer a foreign permanent
		// over its own). It maps onto chooseCards like the other card-pick
		// asks: distinct permanents have distinct ids, so every ballot entry
		// is selectable and parseChooseCards maps each pick back to its
		// option index. The controller is presentation-only here (a
		// CardDto has no controller field); the prompt's Title carries the
		// card's own VoteMessage ("for a nonland permanent you don't control").
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
	case "enlist", "proliferate":
		// rules/enlist.go askNextEnlist's CR 702.160a may-election (Min 0,
		// Max 1) and effects/counters.go effProliferate's CR 701.27
		// any-number recipient pick (Min 0, Max len(eligible)): both are a
		// labelled multi-select whose options are NOT uniformly objects
		// (proliferate mixes permanent options carrying Obj with player
		// options carrying only Player, and enlist's decline option carries
		// no Obj), so neither the object pick (the default arm's
		// allOptionsHaveObjects) nor the Min==1 labelled fallback
		// (labelledAlternatives) reaches them. They map onto
		// chooseFromSelection by index -- an empty answer IS the decline
		// (Min is 0), which is exactly what the mock answers, and the
		// engine's resume reads each chosen option's own Obj/Player. The
		// legal-answer rule stays Decision.Validate, the one home.
		options := make([]mb.SelectionOption, 0, len(d.Options))
		for _, o := range d.Options {
			options = append(options, mb.SelectionOption{Label: o.Label, Weight: 1})
		}
		in = mb.ChooseFromSelectionInput{PromptBase: pres, Options: options, MinTotal: d.Min, MaxTotal: d.Max}
	case "name", "type", "dungeon", "room", "roll", "look_ack", "choice", "vote":
		// "vote" is api:Vote's fixed-list ballot (effects/misc.go's
		// askFixedVote, MBX-7): one option per named choice (the SVar names a
		// card's Choices$ list holds, e.g. Expropriate's branches), with a
		// SYNTHETIC Obj (index+1) that is not any view object -- which is why
		// it must map onto the label-only selection, never chooseCards.
		// parseChooseFromSelection maps ChosenIndices straight back to option
		// indexes.
		options := make([]mb.SelectionOption, 0, len(d.Options))
		for _, o := range d.Options {
			options = append(options, mb.SelectionOption{Label: o.Label, Weight: 1})
		}
		in = mb.ChooseFromSelectionInput{PromptBase: pres, Options: options, MinTotal: d.Min, MaxTotal: d.Max}
	default:
		// The object-pick fallback (MBX-6): a uniform-kind pick this switch
		// has no kind-specific shape for whose options all carry an object
		// reference is a card-selection over those objects
		// (effects/counters.go putCounterChoose's "counter_pick": pick
		// Min..Max battlefield objects, each takes the full CounterNum$) ->
		// chooseCards, answered by object identity so same-named creatures
		// stay distinct. Every other unmapped shape -- plain labelled
		// alternatives such as rules/cast.go's additional-cost pick (kind
		// "altaddcost"), or any option shape this package does not know --
		// falls back to MBX-7's generic label selection, which is
		// always-answerable and cannot widen what Decision.Validate accepts.
		if allOptionsHaveObjects(d.Options) {
			cards := make([]mb.CardDto, 0, len(d.Options))
			for _, o := range d.Options {
				cards = append(cards, t.optionCard(v, o))
			}
			in = mb.ChooseCardsInput{PromptBase: pres, Cards: cards, Min: d.Min, Max: d.Max}
		} else {
			return t.fallbackChoose(d, v, true)
		}
	}
	return mb.PromptMessage{Kind: "prompt", AgentPrompt: mb.AgentPrompt{PromptID: promptID(d), DecidingPlayerID: playerID(d.Player), SourceCard: t.sourceCard(v, d.Source), Input: mb.PromptInput{Value: in}}}, false, nil
}

// fallbackChoose is the MBX-7 class closer: any KChoose this package has no
// specific mapping for -- a mixed-Kind list no recognised shape claims, an
// unknown single Kind, or (degenerately) an option-less ask -- still poses
// an ANSWERABLE prompt instead of the ErrUnmapped error that left a ManaBrew
// seat waiting forever: a generic chooseFromSelection over the native option
// labels in option order. parseChooseFromSelection maps a selectionDecision's
// ChosenIndices straight back to the native option indexes (no remapping),
// and Decision.Validate is the one legality fence, so the fallback cannot
// widen what the engine accepts -- it only makes every offered option
// selectable. The returned bool is the fallback flag the census counts
// (mbtest.Census.Fallback) so a fallback pose stays visible even though the
// unmapped counter now stays at zero.
func (t *Translator) fallbackChoose(d *decision.Decision, v *view.View, fallback bool) (mb.PromptMessage, bool, error) {
	pres := mb.PromptBase{Presentation: mb.PromptPresentation{Title: d.Prompt, Description: chooseConstraint(d), Targets: []mb.TargetRef{}}}
	options := make([]mb.SelectionOption, 0, len(d.Options))
	for _, o := range d.Options {
		label := o.Label
		if label == "" {
			label = fmt.Sprintf("option %d", o.Index)
		}
		options = append(options, mb.SelectionOption{Label: label, Weight: 1, CanRepeat: d.Repeatable})
	}
	in := mb.PromptInputData(mb.ChooseFromSelectionInput{PromptBase: pres, Options: options, MinTotal: d.Min, MaxTotal: d.Max})
	return mb.PromptMessage{Kind: "prompt", AgentPrompt: mb.AgentPrompt{PromptID: promptID(d), DecidingPlayerID: playerID(d.Player), SourceCard: t.sourceCard(v, d.Source), Input: mb.PromptInput{Value: in}}}, fallback, nil
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
//   - an explicit Kind "yes"/"command_zone"/"apply"/"madness_exile"/
//     "trigger_cost_pay", or Option.Mode ModeUnlessPay, is the confirm
//     (true) side;
//   - an explicit Kind "no"/"leave"/"decline"/"madness_graveyard"/
//     "trigger_cost_decline", or Option.Mode ModeUnlessDecline, is the deny
//     (false) side;
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
		case o.Kind == "yes", o.Kind == "command_zone", o.Kind == "apply", o.Kind == "madness_exile", o.Kind == "trigger_cost_pay", o.Mode == decision.ModeUnlessPay:
			confirm = o.Index
		case o.Kind == "no", o.Kind == "leave", o.Kind == "decline", o.Kind == "madness_graveyard", o.Kind == "trigger_cost_decline", o.Mode == decision.ModeUnlessDecline:
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

// electionKinds are the option kinds that pair as a two-sided election
// (confirm/deny) and may appear MIXED in one option list: the plain
// yes/no optional election, and rules/cumulative.go's triggered-cost ask
// (trigger_cost_pay / trigger_cost_decline). The uniform-kind gate lets
// these kinds mix; the switch's election case maps the pair onto
// booleanElection, whose confirm/deny discovery booleanOptionIndex mirrors
// on the response side.
var electionKinds = map[string]bool{
	"yes": true, "no": true,
	"trigger_cost_pay": true, "trigger_cost_decline": true,
}

// isCastContributionOptions reports whether every option of the ask is a
// Convoke/Harmonize/Improvise/Waterbend payment contribution (rules/cast.go
// convokeAsk's option vocabulary). The gate for promptChoose's
// cast-contribution branch.
func isCastContributionOptions(opts []decision.Option) bool {
	if len(opts) == 0 {
		return false
	}
	for _, o := range opts {
		if !strings.HasPrefix(o.Kind, "convoke_") &&
			o.Kind != "harmonize" && o.Kind != "improvise_generic" && o.Kind != "waterbend_generic" {
			return false
		}
	}
	return true
}

// changetextOptions reports whether every option of the ask is an
// api:ChangeText word candidate (effects/changetext.go's
// "changetext_from"/"changetext_to" vocabulary, both halves mixed). The
// gate for promptChoose's routed changetext branch, which also covers a
// uniform one-half list the switch would otherwise see first.
func changetextOptions(opts []decision.Option) bool {
	if len(opts) == 0 {
		return false
	}
	for _, o := range opts {
		if o.Kind != "changetext_from" && o.Kind != "changetext_to" {
			return false
		}
	}
	return true
}

// allOptionsHaveObjects reports whether every option carries an object
// reference (Obj != 0) -- the gate for promptChoose's default-arm
// object-pick fallback (chooseCards by object identity).
func allOptionsHaveObjects(opts []decision.Option) bool {
	if len(opts) == 0 {
		return false
	}
	for _, o := range opts {
		if o.Obj == 0 {
			return false
		}
	}
	return true
}

// labelledAlternatives reports whether d is a pick-one ask (Min == Max == 1,
// not repeatable) whose options are plain labelled alternatives: every one
// carries a label and no ManaSymbol. It is the gate for promptChoose's
// labelled-alternatives fallback (see the !uniform branch there).
func labelledAlternatives(d *decision.Decision) bool {
	if d.Min != 1 || d.Max != 1 || d.Repeatable {
		return false
	}
	for _, o := range d.Options {
		if o.Label == "" || o.ManaSymbol != "" {
			return false
		}
	}
	return true
}

// booleanElection poses a two-sided election (yes/no, apply/decline, pay/
// decline, madness cast/discard) as a chooseBoolean only when BOTH sides are
// actually offered. rules/altcast.go's madness cast ask offers just the
// decline side whenever casting is not legal (offerCastable fails), and any
// future single-sided election would do the same; a boolean prompt that
// advertises a confirm label the decision does not carry would invite a
// client to answer true into a guaranteed rejection (booleanOptionIndex has
// no confirm to map it to). A one-sided ask goes out as a one-option
// chooseFromSelection instead (MinTotal=MaxTotal=1) -- the same shape the
// single-option branches of prompt_choose and prompt_modes already use -- so
// parseChooseFromSelection maps the only legal answer back by option index.
// Builders discover the two labels the same way booleanOptionIndex discovers
// the two option kinds, so prompt and parser cannot drift apart.
func booleanElection(pres mb.PromptBase, d *decision.Decision, confirm, deny string) mb.PromptInputData {
	if len(d.Options) < 2 {
		opts := make([]mb.SelectionOption, 0, len(d.Options))
		for _, o := range d.Options {
			label := o.Label
			if label == "" {
				label = o.Kind
			}
			opts = append(opts, mb.SelectionOption{Label: label, Weight: 1})
		}
		return mb.ChooseFromSelectionInput{PromptBase: pres, Options: opts, MinTotal: 1, MaxTotal: 1}
	}
	return mb.ChooseBooleanInput{PromptBase: pres, ConfirmLabel: confirm, DenyLabel: deny}
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

// chooseConstraint renders the decision's legal-answer bounds as the
// presentation description every ManaBrew client sees. The wording is a
// CONTRACT: mbtest's MockClient parses "Total value must be at least N."
// and "Total value must not exceed N." to honour a Decision.MinSum/MaxSum
// the wire shape itself cannot express (spec gap G-4), so rewording these
// sentences changes what the mock can answer.
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
	name := strings.TrimPrefix(o.Label, "Discard ")
	if c := findCard(v, o.Obj); c != nil {
		if visible, ok := t.visibleCard(*c).Value.(mb.VisibleCard); ok {
			card := visible.CardDto
			if card.Identity.Name == "" && name != "" {
				card.Identity.Name = name
			}
			return card
		}
	}
	return mb.CardDto{ID: cardID(o.Obj), Identity: mb.CardIdentity{Name: name}, Types: []string{}, Subtypes: []string{}, Supertypes: []string{}, Choices: []mb.CardChoiceDto{}, AttachmentIDs: []string{}, MergedCardIDs: []string{}, Color: []string{}, Counters: map[string]int{}}
}

// allPayPipOptions reports whether every option of a KChoose decision is one
// of pipAnnounceAsk's flexible-pip alternatives ("pay_W".."pay_C",
// "pay_generic", "pay_life"): a mixed-Kind list that is still a plain
// labelled pick, never a card or player reference.
func allPayPipOptions(opts []decision.Option) bool {
	if len(opts) == 0 {
		return false
	}
	for _, o := range opts {
		if !strings.HasPrefix(o.Kind, "pay_") {
			return false
		}
	}
	return true
}

// isTargetRefOptions reports whether every option of a KChoose decision is a
// plain target reference: Option.Kind uniformly "player" or "card" (freely
// mixed). It is checked ahead of the damage_split ResumeKind branch's own
// identical Kind vocabulary (that check runs first and returns), so this
// only ever catches an ordinary target/player pick.
func isTargetRefOptions(opts []decision.Option) bool {
	if len(opts) == 0 {
		return false
	}
	for _, o := range opts {
		if o.Kind != "player" && o.Kind != "card" {
			return false
		}
	}
	return true
}
