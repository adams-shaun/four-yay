package mzplay

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
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
//	FlashbackAbility           "Flashback <cost>"
//	EquipAbility               "Equip <cost>"
//	an activated ability       its rule text, "{this}" for the source's name
//
// rule is the encoder's rule text for an activated ability (mzbridge
// .Encoder.ActivatedRule), "" when the ability has no printed index;
// flashback is the card's flashback cost in XMage's symbols, "" when it has
// none.
//
// A cast in any other payment mode is "Cast <card name>" too: in XMage a
// kicker is an optional cost asked for after the spell ability was chosen,
// and a card played through a permission (from the graveyard, from the top
// of the library) is still its own SpellAbility. Two gorge candidates that
// differ only in such a mode therefore share one action index, and their
// visits add, as the visits of upstream's children sharing an index do.
func ActionLabel(a searchbench.LiveAction, rule, flashback string) string {
	switch a.Kind {
	case "pass":
		return "Pass"
	case "cast":
		if a.Mode == "flashback" && flashback != "" {
			return "Flashback " + flashback
		}
		return "Cast " + a.Name
	case "land":
		return "Play " + a.Name
	case "ability":
		if i := strings.Index(rule, ": Equip "); i > 0 {
			return "Equip " + rule[:i]
		}
		if rule != "" {
			return rule
		}
	}
	if a.Label != "" {
		return a.Label
	}
	return "?"
}

// flashbackCost is face f's flashback cost in XMage's symbol text
// ("{2}{B}{B}"), "" when the face has no Flashback keyword or its cost is
// not plain mana.
func flashbackCost(f *cards.Face) string {
	if f == nil {
		return ""
	}
	for _, kw := range f.Keywords {
		cost, ok := strings.CutPrefix(kw, "Flashback:")
		if !ok {
			continue
		}
		var b strings.Builder
		for _, tok := range strings.Fields(cost) {
			switch {
			case len(tok) == 1 && strings.Contains("WUBRGCX", tok):
				b.WriteString("{" + tok + "}")
			case len(tok) == 2 && strings.Contains("WUBRG", tok[:1]) && strings.Contains("WUBRGP", tok[1:]):
				b.WriteString("{" + tok[:1] + "/" + tok[1:] + "}")
			default:
				if _, err := strconv.Atoi(tok); err != nil {
					return ""
				}
				b.WriteString("{" + tok + "}")
			}
		}
		return b.String()
	}
	return ""
}

// actionIndexer maps a label onto the vocabulary. A label with a slot of its
// own takes it. Two rewrites are tried for a label without one, and used only
// when the result IS a vocabulary entry, so neither can move a label onto a
// slot upstream would not have given that text:
//
//   - the self-reference: current Oracle wording says "this creature",
//     "this land", "this artifact" where XMage's rule says "{this}";
//   - reminder text: XMage's rule keeps a keyword ability's italic reminder
//     ("{T}: Surveil 1. <i>(Look at ...)</i>"), which gorge's text does not
//     carry. The alias is taken only when exactly one vocabulary entry
//     reduces to the label once its reminder is removed.
//
// Anything else lands in the vocabulary's hashed tail, as an unknown string
// does upstream.
type actionIndexer struct {
	vocab *mzbridge.Vocab
	alias map[string]string // reminder-stripped label -> the vocabulary's label; lookup only
}

func newActionIndexer(v *mzbridge.Vocab) *actionIndexer {
	ix := &actionIndexer{vocab: v, alias: map[string]string{}}
	dup := map[string]bool{} // membership only
	for _, label := range v.ActionLabels() {
		i := strings.Index(label, " <i>(")
		if i < 0 || !strings.HasSuffix(label, ")</i>") {
			continue
		}
		short := label[:i]
		if v.KnownAction(short) {
			continue
		}
		if _, seen := ix.alias[short]; seen {
			dup[short] = true
			continue
		}
		ix.alias[short] = label
	}
	for short := range dup {
		delete(ix.alias, short)
	}
	return ix
}

var selfReferences = []string{"this creature", "this land", "this artifact", "this enchantment", "this permanent", "this token",
	"this Aura", "this Equipment", "this Vehicle", "this card"}

// loyaltyCost rewrites a planeswalker ability's cost into XMage's form
// ("+1: ...", "-3: ..."): gorge's rule text may carry it bracketed ("[+1]:",
// "[−3]:") or as the Forge cost token ("AddCounter<1/LOYALTY>:",
// "SubCounter<3/LOYALTY>:"). ok is false when label has no such cost.
func loyaltyCost(label string) (string, bool) {
	colon := strings.Index(label, ": ")
	if colon < 0 {
		return "", false
	}
	cost, rest := label[:colon], label[colon:]
	switch {
	case strings.HasPrefix(cost, "[") && strings.HasSuffix(cost, "]"):
		cost = strings.ReplaceAll(cost[1:len(cost)-1], "\u2212", "-")
		if cost == "0" || cost == "" || !strings.ContainsAny(cost[:1], "+-0123456789") {
			return "", false
		}
		return cost + rest, true
	case strings.HasPrefix(cost, "AddCounter<") && strings.HasSuffix(cost, "/LOYALTY>"):
		return "+" + cost[len("AddCounter<"):len(cost)-len("/LOYALTY>")] + rest, true
	case strings.HasPrefix(cost, "SubCounter<") && strings.HasSuffix(cost, "/LOYALTY>"):
		return "-" + cost[len("SubCounter<"):len(cost)-len("/LOYALTY>")] + rest, true
	}
	return "", false
}

// resolve returns the label the vocabulary is indexed by and whether it has
// a slot of its own. source is the card's name: a rule text that names its
// own card by the name's short form ("Kellan" for "Kellan, Planar
// Trailblazer") is tried with "{this}" in its place.
func (ix *actionIndexer) resolve(label, source string) (string, bool) {
	known := func(l string) (string, bool) {
		if ix.vocab.KnownAction(l) {
			return l, true
		}
		if full, ok := ix.alias[l]; ok {
			return full, true
		}
		return "", false
	}
	if l, ok := known(label); ok {
		return l, true
	}
	variants := []string{label}
	if l, ok := loyaltyCost(label); ok {
		variants = append(variants, l)
	}
	for _, v := range variants {
		if strings.Contains(v, "this ") {
			w := v
			for _, ref := range selfReferences {
				w = strings.ReplaceAll(w, ref, "{this}")
			}
			variants = append(variants, w)
		}
	}
	if short, _, ok := strings.Cut(source, ","); ok && short != "" {
		for _, v := range variants {
			if strings.Contains(v, short) {
				variants = append(variants, strings.ReplaceAll(v, short, "{this}"))
			}
		}
	}
	for _, v := range variants[1:] {
		if l, ok := known(v); ok {
			return l, true
		}
	}
	return label, false
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
