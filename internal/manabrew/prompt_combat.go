package manabrew

import (
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// promptCombat (scoping spec §6.3): attackers are grouped per creature (the
// engine offers one option per creature×defender pair), blockers are the
// per-defender (blocker, attacker) pairs the engine asks one defender at a
// time. The per-attacker CR 509.1a/509.1c bounds ride the BlockableAttacker
// so a rules-ignorant client can size its declaration before the engine's
// own declaration gate rejects it.
func (t *Translator) promptCombat(d *decision.Decision, v *view.View) (mb.PromptMessage, error) {
	switch d.Kind {
	case decision.KAttackers:
		return t.promptAttackers(d, v)
	case decision.KBlockers:
		return t.promptBlockers(d, v)
	default:
		return mb.PromptMessage{}, ErrUnmapped
	}
}

// promptAttackers builds chooseAttackers. It needs the seat view: the
// attack-target labels and the planeswalker/battle kinds come from the
// permanent's projected type line, which is exactly the projection the
// client would render anyway.
func (t *Translator) promptAttackers(d *decision.Decision, v *view.View) (mb.PromptMessage, error) {
	if v == nil {
		return mb.PromptMessage{}, fmt.Errorf("manabrew: chooseAttackers needs the seat view for attack-target labels")
	}
	attackers := make([]mb.AttackerOptionDto, 0, len(d.Options))
	targets := make([]mb.AttackTargetDto, 0, 2)
	seenTarget := make(map[string]bool)
	seenAttacker := make(map[state.ObjID]int) // first-occurrence position in attackers
	for _, opt := range d.Options {
		refID := attackTargetID(opt)
		if !seenTarget[refID] {
			seenTarget[refID] = true
			targets = append(targets, t.attackTarget(opt, v))
		}
		if pos, ok := seenAttacker[opt.Obj]; ok {
			attackers[pos].ValidTargetIDs = append(attackers[pos].ValidTargetIDs, refID)
			attackers[pos].MustAttack = attackers[pos].MustAttack || opt.Required
			continue
		}
		seenAttacker[opt.Obj] = len(attackers)
		attackers = append(attackers, mb.AttackerOptionDto{
			AttackerID:     cardID(opt.Obj),
			ValidTargetIDs: []string{refID},
			MustAttack:     opt.Required,
		})
	}
	in := mb.ChooseAttackersInput{Attackers: attackers, AttackTargets: targets}
	return mb.PromptMessage{Kind: "prompt", AgentPrompt: mb.AgentPrompt{
		PromptID: promptID(d), DecidingPlayerID: playerID(d.Player),
		Input: mb.PromptInput{Value: in}}}, nil
}

// attackTargetID is the wire ref id of an attacker option's target: the
// battle/planeswalker permanent for a permanent attack, the defender's
// player id for a player attack.
func attackTargetID(opt decision.Option) string {
	if opt.Battle != 0 {
		return cardID(opt.Battle)
	}
	return playerID(opt.Player)
}

// attackTarget projects one attack target's label and kind from the view.
// The kind is derived from the permanent's projected type half, not from a
// guess: a permanent that projects neither Battle nor Planeswalker falls
// back to planeswalker, which is what CR 310.7's permanent-attack family
// has been in every non-battle case so far.
func (t *Translator) attackTarget(opt decision.Option, v *view.View) mb.AttackTargetDto {
	if opt.Battle != 0 {
		c := findCard(v, opt.Battle)
		kind := "planeswalker"
		label := ""
		if c != nil {
			if hasType(c, "Battle") {
				kind = "battle"
			}
			label = c.Printing.Name
			if label == "" {
				label = c.Name
			}
		}
		return mb.AttackTargetDto{ID: cardID(opt.Battle), Label: label, Kind: kind}
	}
	return mb.AttackTargetDto{ID: playerID(opt.Player), Label: playerLabel(v, opt.Player), Kind: "player"}
}

// promptBlockers builds chooseBlockers: one BlockableAttacker per distinct
// attacker (first-occurrence order), with the CR 509.1a MinMaxBlocker
// bounds and the CR 509.1c must-be-blocked flag the options carried
// server-side (Option.AttackMust), the blockers valid against that
// attacker, and every blocker id offered. A MaxBlockers of 0 (the omitempty
// zero) means "no ceiling", so the pointer is set only above zero.
//
// The engine's blocker options are blocker-major (one creature's candidate
// attackers contiguous, then the next creature's), NOT attacker-major, so
// a repeated attacker's options are scattered through the option list.
// Each option must therefore join ITS OWN attacker's group via a
// first-occurrence position map (the same shape promptAttackers uses) --
// the earlier "append to the last group" shortcut silently moved a
// blocker's later attackers onto whichever attacker happened to be last
// (MBX-4 seed 2004: option [4: 82->36] landed in o39's ValidBlockerIDs, so
// a real client could never block o82 against o36).
func (t *Translator) promptBlockers(d *decision.Decision, v *view.View) (mb.PromptMessage, error) {
	attackers := make([]mb.BlockableAttackerDto, 0, len(d.Options))
	blockers := make([]string, 0, len(d.Options))
	seenAttacker := make(map[string]int) // attacker ref -> first-occurrence position in attackers
	seenBlocker := make(map[state.ObjID]bool)
	for _, opt := range d.Options {
		aid := cardID(opt.Attacker)
		if pos, ok := seenAttacker[aid]; ok {
			last := &attackers[pos]
			last.MustBeBlocked = last.MustBeBlocked || opt.AttackMust
			last.ValidBlockerIDs = append(last.ValidBlockerIDs, cardID(opt.Obj))
		} else {
			seenAttacker[aid] = len(attackers)
			attackers = append(attackers, mb.BlockableAttackerDto{
				AttackerID:      aid,
				ValidBlockerIDs: []string{cardID(opt.Obj)},
				MustBeBlocked:   opt.AttackMust,
			})
			if opt.MinBlockers > 0 {
				attackers[len(attackers)-1].MinBlockers = opt.MinBlockers
			}
			if opt.MaxBlockers > 0 {
				attackers[len(attackers)-1].MaxBlockers = &opt.MaxBlockers
			}
		}
		if !seenBlocker[opt.Obj] {
			seenBlocker[opt.Obj] = true
			blockers = append(blockers, cardID(opt.Obj))
		}
	}
	in := mb.ChooseBlockersInput{
		Attackers:           attackers,
		AvailableBlockerIDs: blockers,
	}
	return mb.PromptMessage{Kind: "prompt", AgentPrompt: mb.AgentPrompt{
		PromptID: promptID(d), DecidingPlayerID: playerID(d.Player),
		Input: mb.PromptInput{Value: in}}}, nil
}

// parseAttackers maps declareAttackers: each assignment names one offered
// (attacker, target) option; the response order is the declaration order.
// An unknown pairing is invalidShape ("not offered"), not unknownActionId --
// the ids are refs the prompt minted, not action ids.
func (t *Translator) parseAttackers(out mb.PromptOutputValue, p *Pending) Outcome {
	d := p.Decision
	cur := p.Prompt.PromptID
	dec, ok := out.(mb.DeclareAttackersDecision)
	if !ok {
		return Outcome{Err: errCode(mb.CodeWrongPromptType, "not a declareAttackers answer", idPtr(cur))}
	}
	choices := make([]int, 0, len(dec.Assignments))
	for _, a := range dec.Assignments {
		idx := -1
		for _, opt := range d.Options {
			if cardID(opt.Obj) != a.AttackerID || attackTargetID(opt) != a.TargetID {
				continue
			}
			idx = opt.Index
			break
		}
		if idx < 0 {
			return Outcome{Err: errCode(mb.CodeInvalidShape,
				fmt.Sprintf("attack %s -> %s is not offered", a.AttackerID, a.TargetID), idPtr(cur))}
		}
		choices = append(choices, idx)
	}
	intent := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}
	if err := d.Validate(intent); err != nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
	}
	return Outcome{Intent: &intent}
}

// parseBlockers maps declareBlockers the same way: one offered
// (blocker, attacker) pair per assignment, response order kept.
func (t *Translator) parseBlockers(out mb.PromptOutputValue, p *Pending) Outcome {
	d := p.Decision
	cur := p.Prompt.PromptID
	dec, ok := out.(mb.DeclareBlockersDecision)
	if !ok {
		return Outcome{Err: errCode(mb.CodeWrongPromptType, "not a declareBlockers answer", idPtr(cur))}
	}
	choices := make([]int, 0, len(dec.Assignments))
	for _, a := range dec.Assignments {
		idx := -1
		for _, opt := range d.Options {
			if cardID(opt.Obj) == a.BlockerID && cardID(opt.Attacker) == a.AttackerID {
				idx = opt.Index
				break
			}
		}
		if idx < 0 {
			return Outcome{Err: errCode(mb.CodeInvalidShape,
				fmt.Sprintf("block %s -> %s is not offered", a.BlockerID, a.AttackerID), idPtr(cur))}
		}
		choices = append(choices, idx)
	}
	intent := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}
	if err := d.Validate(intent); err != nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
	}
	return Outcome{Intent: &intent}
}
