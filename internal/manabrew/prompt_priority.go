package manabrew

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

// promptPriority (scoping spec §6.3): every priority option except pass and
// concede is an AvailableAction; pass is the pass OUTPUT, concede the
// directive. d.PaymentActions rides as pay-<id> cast actions so the
// announce-then-pay arm (§6.1) can be named before the pool is filled
// (payment-plan ticket's concern; the id mapping costs nothing here).
func (t *Translator) promptPriority(d *decision.Decision, v *view.View) (mb.PromptMessage, error) {
	actions := make([]mb.AvailableAction, 0, len(d.Options))
	for _, opt := range d.Options {
		switch opt.Kind {
		case "pass", "concede":
			// The pass and the concession are answered as outputs/directive,
			// never as act targets (§6.3's mapping).
			continue
		case "play_land":
			// G-1: PlayCardMode is undefined upstream; gorge's land play is
			// a cast with mode "play".
			actions = append(actions, mb.AvailableAction{ID: actionID(opt.Index), Type: "cast",
				CardID: cardID(opt.Obj), Mode: "play", Label: opt.Label})
		case "cast":
			actions = append(actions, mb.AvailableAction{ID: actionID(opt.Index), Type: "cast",
				CardID: cardID(opt.Obj), Mode: opt.Mode, Label: opt.Label})
		case "activate":
			actions = append(actions, mb.AvailableAction{ID: actionID(opt.Index), Type: "activateAbility",
				CardID: cardID(opt.Obj), AbilityIndex: opt.Ability, Description: opt.Label,
				IsManaAbility: true, ProducedMana: manaProductions(v, opt.Obj)})
		case "ability", "granted", "station", "unlock", "specialize", "turn_face_up":
			actions = append(actions, mb.AvailableAction{ID: actionID(opt.Index), Type: "activateAbility",
				CardID: cardID(opt.Obj), AbilityIndex: opt.Ability, Description: opt.Label})
		default:
			// A later priority option kind folds into activateAbility,
			// labelled (§5.2's potentialAction rule).
			actions = append(actions, mb.AvailableAction{ID: actionID(opt.Index), Type: "activateAbility",
				CardID: cardID(opt.Obj), AbilityIndex: opt.Ability, Description: opt.Label})
		}
	}
	for _, a := range d.PaymentActions {
		// The id is namespaced pay-<ID> (payActionID in ids.go) so a client
		// that echoes it back can never collide with an opt-<index> action.
		actions = append(actions, mb.AvailableAction{ID: payActionID(a.ID), Type: "cast",
			CardID: cardID(a.Cast.Object), Label: a.Label})
	}
	prompt := mb.AgentPrompt{PromptID: promptID(d), DecidingPlayerID: playerID(d.Player),
		SourceCard: t.sourceCard(v, d.Source),
		Input:      mb.PromptInput{Value: mb.ChooseActionInput{Actions: actions}}}
	return mb.PromptMessage{Kind: "prompt", AgentPrompt: prompt}, nil
}

// passOptionIndex is the index of the decision's pass option, or -1.
func passOptionIndex(d *decision.Decision) int {
	for _, opt := range d.Options {
		if opt.Kind == "pass" {
			return opt.Index
		}
	}
	return -1
}

// ConcedeOptionIndex is the index of the decision's concede option, or -1.
func ConcedeOptionIndex(d *decision.Decision) int {
	for _, opt := range d.Options {
		if opt.Kind == "concede" {
			return opt.Index
		}
	}
	return -1
}

// PassPolicy is the per-connection auto-pass rule (§6.5). A pass response
// installs it; the transport answers each of this seat's later priority
// prompts with AutoPass until the policy stops. The policy is cleared by the
// first non-priority prompt (ShouldPass says so) and after a rewind (the
// transport clears it when it submits an UndoRequest) -- gorge has no
// server-side auto-pass, so the rule exists only here.
//
// A zero PassPolicy (a plain pass response) is inactive: the transport may
// still store it, ShouldPass just reports false forever.
type PassPolicy struct {
	UntilPlayer  string      `json:"until_player,omitempty"`
	UntilPhase   mb.StepKind `json:"until_phase,omitempty"`
	ExhaustStack bool        `json:"exhaust_stack,omitempty"`
	// stackAtPass records the stack ids the pass was sent against, so the
	// §6.5 early stop ("a stack object id that was not there when the pass
	// was sent appears") can fire. Empty when ExhaustStack is false.
	stackAtPass []string `json:"-"`
}

// newPassPolicy builds the policy a pass response carries. An until clause
// must name a player id the adapter minted and a non-empty phase.
func newPassPolicy(out mb.PassOutput, v view.View) (PassPolicy, error) {
	p := PassPolicy{ExhaustStack: out.ExhaustStack}
	if out.Until != nil {
		if _, ok := parsePlayerID(out.Until.PlayerID); !ok {
			return PassPolicy{}, fmt.Errorf("pass until player %q is not a player id", out.Until.PlayerID)
		}
		if out.Until.Phase == "" {
			return PassPolicy{}, fmt.Errorf("pass until requires a phase")
		}
		p.UntilPlayer = out.Until.PlayerID
		p.UntilPhase = out.Until.Phase
	}
	if p.ExhaustStack {
		for _, s := range v.Stack {
			p.stackAtPass = append(p.stackAtPass, stackID(s.ID))
		}
	}
	return p, nil
}

// stopped reports whether the policy's until clause or exhaust stack has
// been reached in v.
func (p PassPolicy) stopped(v view.View) bool {
	if p.UntilPlayer != "" {
		if playerID(v.Active) == p.UntilPlayer && stepKind(v.Step) == p.UntilPhase {
			return true
		}
	}
	if p.ExhaustStack {
		if len(v.Stack) == 0 {
			return true
		}
		seen := make(map[string]bool, len(p.stackAtPass))
		for _, id := range p.stackAtPass {
			seen[id] = true
		}
		for _, s := range v.Stack {
			if !seen[stackID(s.ID)] {
				// A stack object that appeared after the pass: stop early
				// (§6.5) so the player sees what is resolving.
				return true
			}
		}
	}
	return false
}

// ShouldPass reports whether the policy still auto-passes at the pending
// decision. It is false for a non-priority prompt (the rule's clearing edge:
// the transport shows that prompt and clears the policy), for a prompt for a
// decision the policy has nothing to say about, and whenever a stop
// condition has been reached.
func (p PassPolicy) ShouldPass(d *decision.Decision, v view.View) bool {
	if d == nil || d.Kind != decision.KPriority {
		return false
	}
	if p.UntilPlayer == "" && !p.ExhaustStack {
		return false
	}
	return !p.stopped(v)
}

// AutoPass is the translator's AutoPass arm (spec §5.1's signature): the
// pass intent the policy wants submitted at the pending prompt, and whether
// it wants one. The intent is an ordinary logged pass, so replay is
// unaffected.
func (t *Translator) AutoPass(p *PassPolicy, d *decision.Decision, v view.View) (decision.Intent, bool) {
	if p == nil || !p.ShouldPass(d, v) {
		return decision.Intent{}, false
	}
	idx := passOptionIndex(d)
	if idx < 0 {
		return decision.Intent{}, false
	}
	return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}, true
}

// parseChooseActionPass maps the pass output: an ordinary logged pass intent
// plus the policy the response installs. It needs the view the pass was
// sent against (Pending.View) for the exhaustStack snapshot.
func (t *Translator) parseChooseActionPass(out mb.PassOutput, p *Pending) Outcome {
	pol, err := newPassPolicy(out, p.View)
	if err != nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(p.Prompt.PromptID))}
	}
	idx := passOptionIndex(p.Decision)
	if idx < 0 {
		return Outcome{Err: errCode(mb.CodeInvalidShape, "the open decision offers no pass", idPtr(p.Prompt.PromptID))}
	}
	in := decision.Intent{Seq: p.Decision.Seq, Player: p.Decision.Player, Choices: []int{idx}}
	if err := p.Decision.Validate(in); err != nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(p.Prompt.PromptID))}
	}
	return Outcome{Intent: &in, Policy: &pol}
}

// parseChooseActionUndo maps restoreSnapshot (§6.4): the checkpoint is the
// current promptId, meaning "undo my last answer"; anything else is
// invalidShape. Whether the table supports undo at all is the transport's
// call (single-human-seat tables only).
func (t *Translator) parseChooseActionUndo(out mb.RestoreSnapshotOutput, p *Pending) Outcome {
	cur := p.Prompt.PromptID
	if out.CheckpointID != cur {
		return Outcome{Err: errCode(mb.CodeInvalidShape,
			fmt.Sprintf("restoreSnapshot checkpoint %d is not the open prompt %d", out.CheckpointID, cur), idPtr(cur))}
	}
	return Outcome{Undo: &UndoRequest{}}
}

// parseChooseActionAct maps act{actionId}: opt-<index> names an advertised
// option action; pay-<id> names a payment action and becomes Intent.Announce
// (§6.1). Anything else was never advertised.
func (t *Translator) parseChooseActionAct(out mb.ActOutput, p *Pending) Outcome {
	d := p.Decision
	cur := p.Prompt.PromptID
	if rest, ok := strings.CutPrefix(out.ActionID, "opt-"); ok {
		idx, err := strconv.Atoi(rest)
		if err != nil || idx < 0 || idx >= len(d.Options) || d.Options[idx].Kind == "pass" || d.Options[idx].Kind == "concede" {
			return Outcome{Err: errCode(mb.CodeUnknownActionID,
				fmt.Sprintf("action %q is not advertised", out.ActionID), idPtr(cur))}
		}
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}
		if err := d.Validate(in); err != nil {
			return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
		}
		return Outcome{Intent: &in}
	}
	if rest, ok := strings.CutPrefix(out.ActionID, "pay-"); ok {
		offered := false
		for _, a := range d.PaymentActions {
			if a.ID == rest {
				offered = true
				break
			}
		}
		if !offered {
			return Outcome{Err: errCode(mb.CodeUnknownActionID,
				fmt.Sprintf("action %q is not advertised", out.ActionID), idPtr(cur))}
		}
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Announce: &decision.AnnounceSelection{ActionID: rest}}
		if err := d.Validate(in); err != nil {
			return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
		}
		return Outcome{Intent: &in}
	}
	return Outcome{Err: errCode(mb.CodeUnknownActionID,
		fmt.Sprintf("action %q is not advertised", out.ActionID), idPtr(cur))}
}
