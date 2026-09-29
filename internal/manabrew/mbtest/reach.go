package mbtest

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"sync"

	"context"

	"github.com/adams-shaun/gorge/decision"
	manabrew "github.com/adams-shaun/gorge/internal/manabrew"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

// MBX-3 reachability support: the MB-8 census proves the FIRST legal answer
// to every posed prompt is accepted; it does not prove every native
// decision.Option can be SELECTED from ManaBrew. These helpers close that
// gap:
//
//   - ResponseForIntent is the inverse of TranslateResponse: given the
//     pending prompt and the native intent that selects one (or several)
//     options, it builds the ManaBrew client message a client would send to
//     select exactly it -- built ONLY from what the prompt advertises (the
//     prompt's own ids, candidates, card ids, colour codes, bounds), so an
//     option the prompt itself dropped cannot be "reached" by construction.
//   - ProbeReach sweeps one posed decision: for every option (and, for
//     multi-select asks, a seeded sample of legal combinations) it builds
//     the response, round-trips it through mb.Encode/mb.Decode, translates
//     it back, and requires the identical intent. The game itself always
//     plays the first-legal answer; the sweep never answers.
//   - ReachSeat wraps a TranslatingSeat and runs the sweep after each
//     answer, folding results into a Reach counter set.
//   - TestManaBrewReachability (reach_test.go) plays the same 20 repo-deck
//     games as the MB-8 census over ReachSeats and asserts the ratchet:
//     every observed unreachable (decision kind, option kind) must be in an
//     explicit allowlist, and every allowlist entry must be observed.
//
// It is test support: it imports internal/manabrew, protocol/manabrew,
// decision and view, the same tier TranslatingSeat already uses.

// Reach counts, per (decision kind, option kind), how many native options
// were probed, how many were proven reachable through the wire, and how many
// were not -- with the first reason seen for each kind pair. It is safe for
// concurrent use; keys are rendered in sorted order wherever they reach a
// log line or an assertion.
type Reach struct {
	mu          sync.Mutex
	Games       int
	Tried       map[string]int
	Reached     map[string]int
	Unreachable map[string]int
	// Reasons holds the first reason string observed per unreachable key.
	Reasons map[string]string
}

// NewReach returns an empty Reach.
func NewReach() *Reach {
	return &Reach{
		Tried:       map[string]int{},
		Reached:     map[string]int{},
		Unreachable: map[string]int{},
		Reasons:     map[string]string{},
	}
}

func (r *Reach) addGame() {
	r.mu.Lock()
	r.Games++
	r.mu.Unlock()
}

// record folds one option's final per-decision tally.
func (r *Reach) record(key, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Tried[key]++
	if reason == "" {
		r.Reached[key]++
		return
	}
	r.Unreachable[key]++
	if _, ok := r.Reasons[key]; !ok {
		r.Reasons[key] = reason
	}
}

// UnreachableKeys returns the keys with at least one unreachable option,
// sorted (deterministic for assertions).
func (r *Reach) UnreachableKeys() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	keys := make([]string, 0, len(r.Unreachable))
	for k := range r.Unreachable {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ReasonFor returns the first reason recorded for key.
func (r *Reach) ReasonFor(key string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Reasons[key]
}

// Summary renders the one-line reachability tally for a test log line.
func (r *Reach) Summary() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	tried, reached, unreach := 0, 0, 0
	for _, v := range r.Tried {
		tried += v
	}
	for _, v := range r.Reached {
		reached += v
	}
	for _, v := range r.Unreachable {
		unreach += v
	}
	return fmt.Sprintf("games=%d tried=%d reached=%d unreachable=%d unreachableKeys=%d", r.Games, tried, reached, unreach, len(r.Unreachable))
}

// Detail renders one sorted "<key> tried=<n> unreachable=<n>" line per key
// with any activity, for the census log line.
func (r *Reach) Detail() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	keys := make([]string, 0, len(r.Tried))
	for k := range r.Tried {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s tried=%d unreachable=%d", k, r.Tried[k], r.Unreachable[k]))
	}
	return strings.Join(parts, "; ")
}

// reachKey is the census key: "<decision kind>:<option kind>".
func reachKey(d *decision.Decision, optKind string) string {
	return string(d.Kind) + ":" + optKind
}

// optionAt returns the option whose Index is idx.
func optionAt(d *decision.Decision, idx int) *decision.Option {
	for i := range d.Options {
		if d.Options[i].Index == idx {
			return &d.Options[i]
		}
	}
	return nil
}

// optionPos returns the option's POSITION in d.Options (the position the
// prompt builders aligned their Candidates/Options lists with).
func optionPos(d *decision.Decision, idx int) int {
	for i, o := range d.Options {
		if o.Index == idx {
			return i
		}
	}
	return -1
}

// ResponseForIntent builds the ManaBrew client message that answers p's open
// prompt by selecting exactly want -- the inverse of TranslateResponse. It
// is built from the prompt's own advertised data (ids, refs, card ids,
// colour codes, bounds): an option the prompt dropped cannot be expressed,
// which is exactly the reachability defect the census counts. The caller
// round-trips the message through mb.Encode/mb.Decode and
// TranslateResponse; the census requires the identical intent back.
func ResponseForIntent(p *manabrew.Pending, want decision.Intent) (mb.ClientMessage, error) {
	d := p.Decision
	if d == nil {
		return mb.ClientMessage{}, errors.New("no open decision")
	}
	if want.Seq != d.Seq || want.Player != d.Player {
		return mb.ClientMessage{}, fmt.Errorf("want intent names seq %d player %d, decision is seq %d player %d",
			want.Seq, want.Player, d.Seq, d.Player)
	}
	mk := func(out mb.PromptOutputValue) mb.ClientMessage {
		return mb.ClientMessage{Value: mb.ClientResponse{Kind: "response", PromptID: p.Prompt.PromptID,
			Action: mb.PromptOutput{Type: p.Prompt.Input.Value.PromptType(), Output: mb.PromptOutputData{Value: out}}}}
	}
	switch in := p.Prompt.Input.Value.(type) {
	case mb.ChooseActionInput:
		out, directive, err := chooseActionAnswer(in, d, want)
		if err != nil {
			return mb.ClientMessage{}, err
		}
		if directive {
			// The concede option is answered with an out-of-band directive,
			// never a prompt response (G-2's answer half).
			return mb.ClientMessage{Value: mb.ClientDirective{Kind: "directive",
				Directive: mb.DirectiveInput{Type: "concede"}}}, nil
		}
		return mk(out), nil
	case mb.ChooseBoardTargetsInput:
		out, err := boardTargetsAnswer(in, d, want)
		if err != nil {
			return mb.ClientMessage{}, err
		}
		return mk(out), nil
	case mb.ChooseAttackersInput:
		out, err := attackersAnswer(in, d, want)
		if err != nil {
			return mb.ClientMessage{}, err
		}
		return mk(out), nil
	case mb.ChooseBlockersInput:
		out, err := blockersAnswer(in, d, want)
		if err != nil {
			return mb.ClientMessage{}, err
		}
		return mk(out), nil
	case mb.MulliganInput:
		out, err := mulliganAnswer(d, want)
		if err != nil {
			return mb.ClientMessage{}, err
		}
		return mk(out), nil
	case mb.MulliganPutBackInput:
		out, err := mulliganPutBackAnswer(in, d, want)
		if err != nil {
			return mb.ClientMessage{}, err
		}
		return mk(out), nil
	case mb.ChooseNumberInput:
		out, err := chooseNumberAnswer(in, d, want)
		if err != nil {
			return mb.ClientMessage{}, err
		}
		return mk(out), nil
	case mb.ChooseCardsInput:
		out, err := chooseCardsAnswer(in, d, want)
		if err != nil {
			return mb.ClientMessage{}, err
		}
		return mk(out), nil
	case mb.ChooseColorInput:
		out, err := chooseColorAnswer(in, d, want)
		if err != nil {
			return mb.ClientMessage{}, err
		}
		return mk(out), nil
	case mb.ChooseBooleanInput:
		out, err := booleanAnswer(d, want)
		if err != nil {
			return mb.ClientMessage{}, err
		}
		return mk(out), nil
	case mb.ChooseFromSelectionInput:
		out, err := selectionAnswer(in, d, want)
		if err != nil {
			return mb.ClientMessage{}, err
		}
		return mk(out), nil
	case mb.ScryInput:
		out, err := scryAnswer(in, d, want)
		if err != nil {
			return mb.ClientMessage{}, err
		}
		return mk(out), nil
	case mb.ReorderInput:
		out, err := reorderAnswer(in, d, want)
		if err != nil {
			return mb.ClientMessage{}, err
		}
		return mk(out), nil
	case mb.PayManaCostInput:
		out, err := payManaCostAnswer(in, d, want)
		if err != nil {
			return mb.ClientMessage{}, err
		}
		return mk(out), nil
	default:
		return mb.ClientMessage{}, fmt.Errorf("prompt %s is not an answerable decision prompt", p.Prompt.Input.Value.PromptType())
	}
}

// actionAdvertisedAvail reports whether a chooseAction prompt's Actions list
// names id; actionAdvertisedPay the same for a payManaCost window's (the two
// prompt shapes advertise actions with different types).
func actionAdvertisedAvail(actions []mb.AvailableAction, id string) bool {
	for _, a := range actions {
		if a.ID == id {
			return true
		}
	}
	return false
}

func actionAdvertisedPay(actions []mb.PaymentAction, id string) bool {
	for _, a := range actions {
		if a.ID == id {
			return true
		}
	}
	return false
}

// chooseActionAnswer maps a priority answer: an Announce selects the
// advertised "pay-<id>" action; a pass option answers with passOutput; the
// concede option (never advertised as an action, G-2) answers with the
// concede directive; everything else selects its advertised "opt-<index>"
// action.
func chooseActionAnswer(in mb.ChooseActionInput, d *decision.Decision, want decision.Intent) (mb.PromptOutputValue, bool, error) {
	if want.Announce != nil {
		id := manabrew.PayActionID(want.Announce.ActionID)
		if !actionAdvertisedAvail(in.Actions, id) {
			return nil, false, fmt.Errorf("payment action %s is not advertised", id)
		}
		return mb.ActOutput{ActionID: id}, false, nil
	}
	if want.Payment != nil {
		return nil, false, errors.New("the wire carries no plan-witness selection; only the announce action is expressible")
	}
	if len(want.Choices) != 1 {
		return nil, false, fmt.Errorf("a priority answer selects one option, want has %d", len(want.Choices))
	}
	opt := optionAt(d, want.Choices[0])
	if opt == nil {
		return nil, false, fmt.Errorf("option %d is not offered", want.Choices[0])
	}
	switch opt.Kind {
	case "pass":
		return mb.PassOutput{}, false, nil
	case "concede":
		return nil, true, nil
	default:
		id := manabrew.ActionID(opt.Index)
		if !actionAdvertisedAvail(in.Actions, id) {
			return nil, false, fmt.Errorf("action %s is not advertised", id)
		}
		return mb.ActOutput{ActionID: id}, false, nil
	}
}

// boardTargetsAnswer maps boardTargets{chosen}: the ref for each chosen
// option, taken from the Candidates list the prompt built in option order.
func boardTargetsAnswer(in mb.ChooseBoardTargetsInput, d *decision.Decision, want decision.Intent) (mb.PromptOutputValue, error) {
	refs := make([]mb.TargetRef, 0, len(want.Choices))
	for _, idx := range want.Choices {
		pos := optionPos(d, idx)
		if pos < 0 || pos >= len(in.Candidates) {
			return nil, fmt.Errorf("option %d is not advertised as a candidate", idx)
		}
		refs = append(refs, in.Candidates[pos])
	}
	return mb.BoardTargetsDecision{Chosen: refs}, nil
}

func attackerAdvertised(attackers []mb.AttackerOptionDto, attackerID, targetID string) bool {
	for _, a := range attackers {
		if a.AttackerID != attackerID {
			continue
		}
		return slices.Contains(a.ValidTargetIDs, targetID)
	}
	return false
}

// attackersAnswer maps declareAttackers: one assignment per chosen option,
// response order kept; each (attacker, target) pair must be one the prompt
// advertised under that attacker.
func attackersAnswer(in mb.ChooseAttackersInput, d *decision.Decision, want decision.Intent) (mb.PromptOutputValue, error) {
	assignments := make([]mb.AttackerAssignment, 0, len(want.Choices))
	for _, idx := range want.Choices {
		opt := optionAt(d, idx)
		if opt == nil {
			return nil, fmt.Errorf("option %d is not offered", idx)
		}
		aid := manabrew.CardID(opt.Obj)
		tid := manabrew.AttackTargetID(*opt)
		if !attackerAdvertised(in.Attackers, aid, tid) {
			return nil, fmt.Errorf("attack %s -> %s is not advertised", aid, tid)
		}
		assignments = append(assignments, mb.AttackerAssignment{AttackerID: aid, TargetID: tid})
	}
	return mb.DeclareAttackersDecision{Assignments: assignments}, nil
}

func blockerAdvertised(attackers []mb.BlockableAttackerDto, blockerID, attackerID string) bool {
	for _, a := range attackers {
		if a.AttackerID != attackerID {
			continue
		}
		return slices.Contains(a.ValidBlockerIDs, blockerID)
	}
	return false
}

// blockersAnswer maps declareBlockers: one (blocker, attacker) assignment
// per chosen option, response order kept, advertised pairs only.
func blockersAnswer(in mb.ChooseBlockersInput, d *decision.Decision, want decision.Intent) (mb.PromptOutputValue, error) {
	assignments := make([]mb.BlockerAssignment, 0, len(want.Choices))
	for _, idx := range want.Choices {
		opt := optionAt(d, idx)
		if opt == nil {
			return nil, fmt.Errorf("option %d is not offered", idx)
		}
		bid := manabrew.CardID(opt.Obj)
		aid := manabrew.CardID(opt.Attacker)
		if !blockerAdvertised(in.Attackers, bid, aid) {
			return nil, fmt.Errorf("block %s -> %s is not advertised", bid, aid)
		}
		assignments = append(assignments, mb.BlockerAssignment{BlockerID: bid, AttackerID: aid})
	}
	return mb.DeclareBlockersDecision{Assignments: assignments}, nil
}

// mulliganAnswer maps mulliganDecision{keep}: keep answers the "keep"
// option, mulligan the "mulligan" one.
func mulliganAnswer(d *decision.Decision, want decision.Intent) (mb.PromptOutputValue, error) {
	if len(want.Choices) != 1 {
		return nil, fmt.Errorf("a keep ask answers one option, want has %d", len(want.Choices))
	}
	opt := optionAt(d, want.Choices[0])
	if opt == nil {
		return nil, fmt.Errorf("option %d is not offered", want.Choices[0])
	}
	var keep bool
	switch opt.Kind {
	case "keep":
		keep = true
	case "mulligan":
		keep = false
	default:
		return nil, fmt.Errorf("option kind %q is not a keep/mulligan answer", opt.Kind)
	}
	return mb.MulliganDecision{Keep: keep}, nil
}

// mulliganPutBackAnswer maps mulliganPutBackDecision: one hand card id per
// chosen "bottom" option, each one the prompt advertised.
func mulliganPutBackAnswer(in mb.MulliganPutBackInput, d *decision.Decision, want decision.Intent) (mb.PromptOutputValue, error) {
	ids := make([]string, 0, len(want.Choices))
	for _, idx := range want.Choices {
		opt := optionAt(d, idx)
		if opt == nil {
			return nil, fmt.Errorf("option %d is not offered", idx)
		}
		id := manabrew.CardID(opt.Obj)
		if !slices.Contains(in.HandCardIDs, id) {
			return nil, fmt.Errorf("hand card %s is not advertised", id)
		}
		ids = append(ids, id)
	}
	return mb.MulliganPutBackDecision{CardIDs: ids}, nil
}

// chooseNumberAnswer maps numberDecision{chosenNumber}: the chosen option's
// Amount, which must sit inside the range the prompt advertised.
func chooseNumberAnswer(in mb.ChooseNumberInput, d *decision.Decision, want decision.Intent) (mb.PromptOutputValue, error) {
	if len(want.Choices) != 1 {
		return nil, fmt.Errorf("a number ask answers one option, want has %d", len(want.Choices))
	}
	opt := optionAt(d, want.Choices[0])
	if opt == nil {
		return nil, fmt.Errorf("option %d is not offered", want.Choices[0])
	}
	if opt.Amount < in.Min || opt.Amount > in.Max {
		return nil, fmt.Errorf("value %d is outside the advertised range [%d,%d]", opt.Amount, in.Min, in.Max)
	}
	v := opt.Amount
	return mb.NumberDecision{ChosenNumber: &v}, nil
}

// chooseCardsAnswer maps chooseCardsDecision: one advertised card id per
// chosen option.
func chooseCardsAnswer(in mb.ChooseCardsInput, d *decision.Decision, want decision.Intent) (mb.PromptOutputValue, error) {
	ids := make([]string, 0, len(want.Choices))
	for _, idx := range want.Choices {
		opt := optionAt(d, idx)
		if opt == nil {
			return nil, fmt.Errorf("option %d is not offered", idx)
		}
		id := manabrew.CardID(opt.Obj)
		advertised := false
		for _, c := range in.Cards {
			if c.ID == id {
				advertised = true
				break
			}
		}
		if !advertised {
			return nil, fmt.Errorf("card %s is not advertised", id)
		}
		ids = append(ids, id)
	}
	return mb.ChooseCardsDecision{ChosenCardIDs: ids}, nil
}

// chooseColorAnswer maps colorDecision{chosenColors}: one count per colour,
// each colour one the prompt advertised, each option resolving a colour at
// all (an option whose colour the prompt dropped is not expressible).
func chooseColorAnswer(in mb.ChooseColorInput, d *decision.Decision, want decision.Intent) (mb.PromptOutputValue, error) {
	counts := map[string]int{}
	for _, idx := range want.Choices {
		opt := optionAt(d, idx)
		if opt == nil {
			return nil, fmt.Errorf("option %d is not offered", idx)
		}
		c := manabrew.ColorCode(opt.ManaSymbol, opt.Label)
		if c == "" || !slices.Contains(in.ValidColors, c) {
			return nil, fmt.Errorf("colour %q is not advertised", c)
		}
		counts[c]++
	}
	return mb.ColorDecision{ChosenColors: counts}, nil
}

// booleanSide mirrors the confirm/deny discovery booleanOptionIndex does (the
// same rule every chooseBoolean builder uses), so the value built here
// selects exactly the chosen option. An option that carries neither an
// explicit side nor the two-option fallback order is not expressible.
func booleanSide(d *decision.Decision, opt *decision.Option) (bool, error) {
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
	if confirm < 0 || deny < 0 {
		return false, fmt.Errorf("option kind %q carries no expressible boolean side", opt.Kind)
	}
	switch opt.Index {
	case confirm:
		return true, nil
	case deny:
		return false, nil
	default:
		return false, fmt.Errorf("option %d is neither the confirm nor the deny side", opt.Index)
	}
}

// booleanAnswer maps decision{value} (chooseBoolean): the chosen option's
// side.
func booleanAnswer(d *decision.Decision, want decision.Intent) (mb.PromptOutputValue, error) {
	if len(want.Choices) != 1 {
		return nil, fmt.Errorf("a boolean ask answers one option, want has %d", len(want.Choices))
	}
	opt := optionAt(d, want.Choices[0])
	if opt == nil {
		return nil, fmt.Errorf("option %d is not offered", want.Choices[0])
	}
	value, err := booleanSide(d, opt)
	if err != nil {
		return nil, err
	}
	return mb.BooleanDecision{Value: value}, nil
}

// selectionAnswer maps selectionDecision: the chosen indices ARE the option
// indices (parseChooseFromSelection remaps nothing), but each must be one
// the prompt advertised -- a prompt that lists fewer options than the
// decision offers (the look_ack single-option ask) cannot express the rest.
func selectionAnswer(in mb.ChooseFromSelectionInput, d *decision.Decision, want decision.Intent) (mb.PromptOutputValue, error) {
	for _, idx := range want.Choices {
		if idx < 0 || idx >= len(in.Options) {
			return nil, fmt.Errorf("option %d is not advertised by the prompt (%d options)", idx, len(in.Options))
		}
	}
	return mb.SelectionDecision{ChosenIndices: slices.Clone(want.Choices)}, nil
}

// scryAnswer maps scryDecision{zoneCardIds}: pile A from the chosen options
// (in order), pile B from want.Rest when it is present, otherwise the
// complement in offered order (the legacy contract the parser itself
// builds). Every id must be one the prompt advertised.
func scryAnswer(in mb.ScryInput, d *decision.Decision, want decision.Intent) (mb.PromptOutputValue, error) {
	idsFor := func(choices []int) ([]string, error) {
		ids := make([]string, 0, len(choices))
		for _, idx := range choices {
			opt := optionAt(d, idx)
			if opt == nil {
				return nil, fmt.Errorf("option %d is not offered", idx)
			}
			id := manabrew.CardID(opt.Obj)
			advertised := false
			for _, c := range in.Cards {
				if c.ID == id {
					advertised = true
					break
				}
			}
			if !advertised {
				return nil, fmt.Errorf("card %s is not advertised", id)
			}
			ids = append(ids, id)
		}
		return ids, nil
	}
	pileA, err := idsFor(want.Choices)
	if err != nil {
		return nil, err
	}
	rest := want.Rest
	if rest == nil {
		rest = make([]int, 0, len(d.Options)-len(want.Choices))
		for _, opt := range d.Options {
			if !slices.Contains(want.Choices, opt.Index) {
				rest = append(rest, opt.Index)
			}
		}
	}
	pileB, err := idsFor(rest)
	if err != nil {
		return nil, err
	}
	return mb.ScryDecision{ZoneCardIDs: [][]string{pileA, pileB}}, nil
}

// reorderAnswer maps reorderDecision{orderedIds}: the chosen options' action
// ids in answer order -- reversed for KTriggerOrder, whose parser applies
// the one CR 603.3b direction flip (the client's first-resolves-first list
// is Choices' LIFO placement order backwards).
func reorderAnswer(in mb.ReorderInput, d *decision.Decision, want decision.Intent) (mb.PromptOutputValue, error) {
	ids := make([]string, 0, len(want.Choices))
	for _, idx := range want.Choices {
		id := manabrew.ActionID(idx)
		advertised := false
		for _, it := range in.Items {
			if it.ID == id {
				advertised = true
				break
			}
		}
		if !advertised {
			return nil, fmt.Errorf("item %s is not advertised", id)
		}
		ids = append(ids, id)
	}
	if d.Kind == decision.KTriggerOrder {
		slices.Reverse(ids)
	}
	return mb.ReorderDecision{OrderedIDs: ids}, nil
}

// payManaCostAnswer maps the payManaCost outputs: a per-step activation
// option answers act with its advertised id; autofill answers pay{auto};
// "done" answers pay{auto:false} when the window advertises a confirm; the
// cancel option answers cancel (the parser answers with the engine's own
// truth, G-3).
func payManaCostAnswer(in mb.PayManaCostInput, d *decision.Decision, want decision.Intent) (mb.PromptOutputValue, error) {
	if len(want.Choices) != 1 {
		return nil, fmt.Errorf("a payment window answers one option, want has %d", len(want.Choices))
	}
	opt := optionAt(d, want.Choices[0])
	if opt == nil {
		return nil, fmt.Errorf("option %d is not offered", want.Choices[0])
	}
	switch opt.Kind {
	case "mana", "activate", decision.OptUndoTap:
		id := manabrew.ActionID(opt.Index)
		if !actionAdvertisedPay(in.Actions, id) {
			return nil, fmt.Errorf("action %s is not advertised", id)
		}
		return mb.ActOutput{ActionID: id}, nil
	case decision.OptAutoFill:
		advertised := false
		for _, a := range in.Actions {
			if a.Type == "autofill" {
				advertised = true
				break
			}
		}
		if !advertised {
			return nil, errors.New("autofill is not advertised")
		}
		return mb.PayOutput{Auto: true}, nil
	case "done":
		if !in.CanConfirmFromPool {
			return nil, errors.New("done is not advertised (CanConfirmFromPool)")
		}
		return mb.PayOutput{Auto: false}, nil
	case decision.OptCancelCast:
		return mb.CancelOutput{}, nil
	default:
		return nil, fmt.Errorf("option kind %q is not a payment-window answer", opt.Kind)
	}
}

// sameIntent reports whether the translated intent is the want intent. The
// one documented re-ordering -- chooseColor's parser walks the colour map in
// sorted code order, so a multi-colour answer comes back in canonical colour
// order -- is compared through that canonical form; everything else must be
// exactly equal, order included.
func sameIntent(p *manabrew.Pending, want, got decision.Intent) bool {
	if want.Seq != got.Seq || want.Player != got.Player {
		return false
	}
	if len(want.Choices) != len(got.Choices) {
		return false
	}
	if p.Prompt.Input.Value.PromptType() == "chooseColor" {
		d := p.Decision
		if !slices.Equal(colorCanonical(d, want.Choices), colorCanonical(d, got.Choices)) {
			return false
		}
	} else if !slices.Equal(want.Choices, got.Choices) {
		return false
	}
	if !slices.Equal(want.Rest, got.Rest) {
		return false
	}
	if (want.Announce == nil) != (got.Announce == nil) {
		return false
	}
	if want.Announce != nil && want.Announce.ActionID != got.Announce.ActionID {
		return false
	}
	if (want.Payment == nil) != (got.Payment == nil) {
		return false
	}
	return true
}

// colorCanonical sorts a choices list by each choice's colour code (stable),
// which is exactly the order parseChooseColor emits. Options are looked up
// by index; an unknown index sorts last via "?", which can never match a
// real code ("?" is not in the WUBRGC vocabulary), so a collapsed or lost
// index still fails the comparison.
func colorCanonical(d *decision.Decision, choices []int) []int {
	out := slices.Clone(choices)
	code := func(i int) string {
		o := optionAt(d, i)
		if o == nil {
			return "?"
		}
		return manabrew.ColorCode(o.ManaSymbol, o.Label)
	}
	sort.SliceStable(out, func(a, b int) bool { return code(out[a]) < code(out[b]) })
	return out
}

// reachVerdict is the outcome of one probe attempt.
type reachVerdict struct {
	// state: 1 the wire selected exactly want; 0 the wire selected exactly
	// want but the answer is not by itself legal (a multi-select ask needs
	// more picks; the wire expressed it); -1 the wire could not select it.
	state  int
	reason string
}

func vReached() reachVerdict { return reachVerdict{state: 1} }

func vMappable() reachVerdict { return reachVerdict{state: 0} }

func vUnreachable(reason string) reachVerdict { return reachVerdict{state: -1, reason: reason} }

// attemptSelect builds the response that should select want, round-trips it
// through the real wire codec and the translator, and reports what came
// back. It is exactly the trip TranslatingSeat.Decide gives a real answer.
//
// The verdict depends on whether want is itself a legal answer:
//   - a LEGAL answer must come back as the identical intent (an error, a
//     missing intent or a mismatch is a wire defect);
//   - an ILLEGAL answer (e.g. a single pick on a Min==Max==N multi-select
//     ask) is refused by the parser's own Decision.Validate by design, so
//     identity cannot be verified through the parser; the option is instead
//     mapped faithfully if the prompt itself can carry it, and its
//     reachability is established by the combination sweep (which samples
//     legal answers and identity-checks every member).
func attemptSelect(tr *manabrew.Translator, p *manabrew.Pending, want decision.Intent) reachVerdict {
	legal := p.Decision.Validate(want) == nil
	msg, err := ResponseForIntent(p, want)
	if err != nil {
		return vUnreachable("no-response: " + err.Error())
	}
	raw, err := mb.Encode(msg)
	if err != nil {
		return vUnreachable("encode: " + err.Error())
	}
	var wireResp mb.ClientMessage
	if _, err := mb.Decode(raw, &wireResp); err != nil {
		return vUnreachable("decode: " + err.Error())
	}
	outcome := tr.TranslateResponse(wireResp, p, p.Decision.Player)
	if outcome.Err != nil {
		if !legal {
			// The wire carried the answer; the parser refused it exactly as
			// it refuses every illegal answer. Not a wire defect.
			return vMappable()
		}
		return vUnreachable(fmt.Sprintf("rejected %s: %s", outcome.Err.Code, outcome.Err.Message))
	}
	if !legal {
		return vMappable()
	}
	if outcome.Intent == nil {
		return vUnreachable("translate returned no intent")
	}
	if !sameIntent(p, want, *outcome.Intent) {
		return vUnreachable(fmt.Sprintf("mismatch: want choices %v rest %v; got choices %v rest %v",
			want.Choices, want.Rest, outcome.Intent.Choices, outcome.Intent.Rest))
	}
	if err := p.Decision.Validate(want); err != nil {
		// Mapped faithfully, but this answer is not by itself legal (a
		// multi-select ask needs more picks): the combination sweep covers
		// the option's reachability. Not a wire defect.
		return vMappable()
	}
	return vReached()
}

// reachState is the per-option tally inside one decision.
type reachState struct {
	reached  bool
	unreach  string
	mappable bool
}

func (s *reachState) fold(pos int, v reachVerdict) {
	if pos < 0 {
		return
	}
	switch v.state {
	case 1:
		s.reached = true
		s.mappable = true
	case 0:
		s.mappable = true
	default:
		if s.unreach == "" {
			s.unreach = "unreachable: " + v.reason
		}
	}
}

// foldCombo folds a combination attempt's verdict into every member. A
// combination the wire cannot express is a per-option defect too; one it
// can express, at a size the validator accepts, reaches its members.
func (s *reachState) foldCombo(pos int, v reachVerdict) {
	if pos < 0 {
		return
	}
	if v.state == 1 {
		s.reached = true
		s.mappable = true
		return
	}
	if v.state == 0 {
		s.mappable = true
		return
	}
	if s.unreach == "" {
		s.unreach = "unreachable-in-combo: " + v.reason
	}
}

// reachComboAttempts caps how many legal combinations one multi-select
// decision samples.
const reachComboAttempts = 8

// reachComboContainTries caps the attempts to build a legal-sized sample
// combination containing one specific option.
const reachComboContainTries = 4

// reachCombosApply reports whether d's answer space includes legal answers
// with more than one pick, so a combination sweep adds signal beyond the
// single-pick sweep.
func reachCombosApply(d *decision.Decision) bool {
	if len(d.Options) < 2 {
		return false
	}
	switch d.Kind {
	case decision.KAttackers, decision.KBlockers:
		return true
	}
	return d.Min > 1 || d.Max > 1
}

// reachIsPermutationAsk reports a full-order ask (KTriggerOrder, the KArrange
// single-list shapes): every legal answer is a permutation of all options.
func reachIsPermutationAsk(d *decision.Decision) bool {
	return d.Min == d.Max && d.Min == len(d.Options)
}

// reachSampleCombos draws a seeded sample of candidate multi-select answers:
// for a full-order ask, permutations; otherwise random subsets at the
// decision's size bounds and midpoint, PLUS one combination containing each
// option (so every option's reachability is identity-checked through at
// least one legal answer whenever one exists at the sampled size); and for a
// Restable scry ask one partition with an explicit pile-B order. Legality is
// the caller's Decision.Validate gate (illegal samples are skipped, not
// counted).
func reachSampleCombos(d *decision.Decision, rng *rand.Rand) []decision.Intent {
	n := len(d.Options)
	wants := make([]decision.Intent, 0, reachComboAttempts)
	mk := func(choices, rest []int) decision.Intent {
		return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices, Rest: rest}
	}
	if reachIsPermutationAsk(d) {
		for i := 0; i < 2 && len(wants) < reachComboAttempts; i++ {
			perm := rng.Perm(n)
			choices := make([]int, n)
			for j, pos := range perm {
				choices[j] = d.Options[pos].Index
			}
			wants = append(wants, mk(choices, nil))
		}
		return wants
	}
	lo, hi := d.Min, d.Max
	if lo < 1 {
		lo = 1
	}
	if hi > n || hi == 0 {
		hi = n
	}
	if lo > hi {
		lo = hi
	}
	var sizes []int
	if lo == hi {
		sizes = []int{lo}
	} else {
		sizes = []int{lo, (lo + hi) / 2, hi}
	}
	for _, s := range sizes {
		for i := 0; i < 2 && len(wants) < reachComboAttempts; i++ {
			wants = append(wants, mk(reachSubsetChoices(d, s, rng), nil))
		}
	}
	// Per-option containment: one combination naming this option, so an
	// option the random subsets miss still gets an identity-checked legal
	// answer. (A full-order ask is covered above: its permutations contain
	// every option.)
	for _, opt := range d.Options {
		if len(wants) >= reachComboAttempts {
			break
		}
		for i := 0; i < reachComboContainTries; i++ {
			s := lo + rng.IntN(hi-lo+1)
			choices := append([]int{opt.Index}, reachSubsetChoices(d, s-1, rng)...)
			if !d.Repeatable {
				choices = dedupeIndices(choices)
				if len(choices) > hi {
					choices = choices[:hi]
				}
			}
			if len(choices) < lo || len(choices) > hi {
				continue
			}
			wants = append(wants, mk(choices, nil))
			break
		}
	}
	if d.Restable && hi < n {
		perm := rng.Perm(n)
		s := lo
		if s > n-1 {
			s = n - 1
		}
		choices := make([]int, 0, s)
		rest := make([]int, 0, n-s)
		for k := 0; k < n; k++ {
			if k < s {
				choices = append(choices, d.Options[perm[k]].Index)
			} else {
				rest = append(rest, d.Options[perm[k]].Index)
			}
		}
		wants = append(wants, mk(choices, rest))
	}
	return wants
}

// reachSubsetChoices draws s option indices, with repetition when the ask is
// repeatable, as a shuffled distinct subset otherwise.
func reachSubsetChoices(d *decision.Decision, s int, rng *rand.Rand) []int {
	n := len(d.Options)
	if s <= 0 {
		return nil
	}
	var choices []int
	if d.Repeatable {
		choices = make([]int, 0, s)
		for k := 0; k < s; k++ {
			choices = append(choices, d.Options[rng.IntN(n)].Index)
		}
		return choices
	}
	perm := rng.Perm(n)
	choices = make([]int, 0, s)
	for k := 0; k < s && k < n; k++ {
		choices = append(choices, d.Options[perm[k]].Index)
	}
	return choices
}

// dedupeIndices removes repeated indices, keeping first occurrences in order.
func dedupeIndices(xs []int) []int {
	out := xs[:0]
	for i, x := range xs {
		if !slices.Contains(xs[:i], x) {
			out = append(out, x)
		}
	}
	return out
}

// ProbeReach sweeps one posed decision for option reachability. It never
// answers; play continues with whatever the seat's own client answered.
//
// Per option: a single pick of it is probed. For a multi-select decision, a
// seeded sample of legal combinations is probed too. An option counts as
// REACHED when some probed legal answer selects exactly it after the wire
// round trip, or when its single pick was mapped faithfully (the wire
// expressed it; only a legal combination containing it can be submitted).
// It counts as UNREACHABLE when a probe that should select it maps to a
// different intent, is rejected, or cannot be built from the prompt at all.
func ProbeReach(tr *manabrew.Translator, p *manabrew.Pending, rng *rand.Rand, r *Reach) {
	d := p.Decision
	states := make([]reachState, len(d.Options))
	// Single picks.
	for _, opt := range d.Options {
		want := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}
		v := attemptSelect(tr, p, want)
		states[optionPos(d, opt.Index)].fold(optionPos(d, opt.Index), v)
	}
	// The priority decision's payment actions: the announce action is
	// expressible ("pay-<id>"); the plan witness is native-only and is not
	// an Option, so it is not part of the per-option ratchet.
	for _, a := range d.PaymentActions {
		key := reachKey(d, "payment_action")
		want := decision.Intent{Seq: d.Seq, Player: d.Player, Announce: &decision.AnnounceSelection{ActionID: a.ID}}
		v := attemptSelect(tr, p, want)
		switch v.state {
		case 1:
			r.record(key, "")
		case -1:
			r.record(key, "unreachable: "+v.reason)
		}
	}
	// Combinations.
	if reachCombosApply(d) {
		for _, want := range reachSampleCombos(d, rng) {
			if err := d.Validate(want); err != nil {
				continue // an illegal sample proves nothing either way
			}
			v := attemptSelect(tr, p, want)
			if v.state == 1 {
				for _, idx := range want.Choices {
					states[optionPos(d, idx)].foldCombo(optionPos(d, idx), v)
				}
				for _, idx := range want.Rest {
					states[optionPos(d, idx)].foldCombo(optionPos(d, idx), v)
				}
				continue
			}
			if v.state == -1 {
				members := append(slices.Clone(want.Choices), want.Rest...)
				for _, idx := range members {
					states[optionPos(d, idx)].foldCombo(optionPos(d, idx), v)
				}
			}
		}
	}
	// Fold the per-option tally into the Reach counters.
	for i, opt := range d.Options {
		key := reachKey(d, opt.Kind)
		if states[i].unreach != "" {
			r.record(key, states[i].unreach)
			continue
		}
		r.record(key, "")
	}
}

// ReachSeat answers exactly like the TranslatingSeat it wraps, and after
// each answer runs the reachability sweep over the prompt the SAME decision
// built. The sweep is pure (it reads the prompt and the pending decision and
// writes only the Reach counters), so running it after the answer cannot
// change the game; the Prompt rebuild is the same deterministic call Decide
// just made.
type ReachSeat struct {
	*TranslatingSeat
	Reach *Reach
	rng   *rand.Rand
}

// NewReachSeat wraps base with the reachability sweep. seed seeds the
// combination sampler (a math/rand/v2 PCG source, per the box rule: seeded
// source only).
func NewReachSeat(base *TranslatingSeat, reach *Reach, seed uint64) *ReachSeat {
	return &ReachSeat{TranslatingSeat: base, Reach: reach, rng: rand.New(rand.NewPCG(seed, 0x5beef))}
}

// Decide implements seat.Seat: the wrapped seat answers, then the sweep runs.
func (s *ReachSeat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	intent, err := s.TranslatingSeat.Decide(ctx, v, d)
	s.probe(v, d)
	return intent, err
}

func (s *ReachSeat) probe(v view.View, d decision.Decision) {
	msg, err := s.Translator.Prompt(&d, &v)
	if err != nil {
		// The prompt unmapped; the MB-8 census already counts it. Nothing to
		// sweep.
		return
	}
	pending := &manabrew.Pending{Prompt: msg, Decision: &d, View: v}
	ProbeReach(s.Translator, pending, s.rng, s.Reach)
}
