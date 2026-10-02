package mzplay

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/mzbridge"
	"github.com/adams-shaun/gorge/internal/searchbench"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// The strings upstream identifies a decision and its answers by.

// StopChoosing is the target vocabulary's "no (more) targets" entry
// (TargetImpl.STOP_CHOOSING through GameImpl.getEntityName).
const StopChoosing = "Stop Choosing"

// objectName is the name upstream's getEntityName gives an object: its
// current face's name.
func objectName(e *rules.Engine, id state.ObjID) string {
	if o := e.G.Obj(id); o != nil && o.Face() != nil {
		return o.Face().Name
	}
	return ""
}

// TargetName is GameImpl.getEntityName for a target option as seat self
// sees it: "PlayerA" for self and "PlayerB" for the other player (the
// vocabulary's indices 1 and 2), else the object's name.
func TargetName(e *rules.Engine, o decision.Option, self state.PlayerID) string {
	if o.Obj != 0 {
		return objectName(e, o.Obj)
	}
	if o.Player == self {
		return "PlayerA"
	}
	return "PlayerB"
}

// ActionLabel is the ability.toString() upstream's ActionEncoder looks a
// priority action up by (ActionEncoder.getActionIndex):
//
//	PassAbility                "Pass"
//	SpellAbility               "Cast <card name>"
//	PlayLandAbility            "Play <card name>"
//	an activated ability       its rule text, "{this}" for the source's name
//
// rule is the encoder's rule text for an activated ability (mzbridge
// .Encoder.ActivatedRule), "" when the ability has no printed index. A cast
// in a payment mode (flashback, kicker) and an ability without a rule have
// no upstream string gorge can reproduce; they keep a stable label of their
// own, which lands in the vocabulary's hashed tail.
func ActionLabel(a searchbench.LiveAction, rule string) string {
	switch a.Kind {
	case "pass":
		return "Pass"
	case "cast":
		if a.Mode != "" {
			return "Cast " + a.Name + " using " + a.Mode
		}
		return "Cast " + a.Name
	case "land":
		return "Play " + a.Name
	case "ability":
		if rule != "" {
			return rule
		}
	}
	if a.Label != "" {
		return a.Label
	}
	return "?"
}

// decisionHead is the upstream decision a gorge decision is encoded as: the
// ActionType and the decision text StateEncoder.processState hashes as root
// features (MCTSPlayer.java: "priority"; chooseUse's message; "<source
// rule>:Choose a target:<target name>"; a choice's message).
//
// A whole attack or block declaration is encoded as its FIRST per-creature
// question, with no answers given yet: the state a leaf of the search stands
// at before any creature has been asked.
func decisionHead(e *rules.Engine, d *decision.Decision) (mzbridge.ActionType, string) {
	if d == nil {
		return mzbridge.Priority, "priority"
	}
	switch {
	case d.Kind == decision.KPriority:
		return mzbridge.Priority, "priority"
	case d.Kind == decision.KAttackers:
		for _, o := range d.Options {
			if o.Obj != 0 {
				return mzbridge.ChooseUse, attackText(objectName(e, o.Obj))
			}
		}
		return mzbridge.ChooseUse, attackText("")
	case d.Kind == decision.KBlockers:
		for _, o := range d.Options {
			if o.Obj != 0 {
				return mzbridge.ChooseTarget, blockText(objectName(e, o.Obj))
			}
		}
		return mzbridge.ChooseTarget, blockText("")
	case d.Kind == decision.KTarget:
		return mzbridge.ChooseTarget, targetText(e, d)
	}
	return mzbridge.MakeChoice, d.Prompt
}

// attackText is ComputerPlayer.selectAttackersOneAtATime's chooseUse message.
func attackText(name string) string { return "attack with: " + name + "?" }

// blockText is the decision text of selectBlockersOneAtATime's makeChoice:
// the fake source ability's rule (ChooseCreatureToBlockAbility: "choose which
// creature to block for <blocker>"), ":Choose a target:", and the target
// name of TargetAttackingCreature ("attacking creature").
func blockText(name string) string {
	return "choose which creature to block for " + name + ":Choose a target:attacking creature"
}

// targetText stands in for "<source.getRule()>:Choose a target:<target
// name>": gorge has neither XMage string, so the source is its card name
// and the target description is the decision's own prompt.
func targetText(e *rules.Engine, d *decision.Decision) string {
	src := "null"
	if n := objectName(e, d.Source); n != "" {
		src = n
	}
	return src + ":Choose a target:" + d.Prompt
}
