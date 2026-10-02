package mzbridge

import (
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// stackEntry is one stack object as the encoder names it.
type stackEntry struct {
	sv *view.StackView
	o  *state.Object
	// name is cleanString(StackObject.toString()): "Cast <card>" for a
	// spell (Spell.toString is its SpellAbility's name, SpellAbility.java:
	// 323), "stack ability (<rule>)" for an ability (StackAbility.java:62).
	name string
	// entity is Game.getEntityName for the object: the card name of a
	// spell (Spell.getName), the full name of an ability.
	entity string
	// rule is getStackAbility().getRule() (358).
	rule      string
	ability   *abilityInfo // nil when the source face is not visible
	triggered bool
	mode      string // Forge trigger mode of a triggered ability
}

// stackEntryOf names one projected stack object. A face-down spell the
// deciding seat may not look at keeps an empty name and no ability.
func (enc *Encoder) stackEntryOf(sv *view.StackView) stackEntry {
	g := enc.g
	se := stackEntry{sv: sv, o: g.Obj(sv.ID)}
	o := se.o
	if o == nil {
		se.o = &state.Object{}
		se.name, se.entity = "null", "null"
		return se
	}
	if o.Ability == nil {
		// a spell
		se.name, se.entity = "Cast "+sv.Name, sv.Name
		if sv.Card != nil && o.Face() != nil {
			fi := enc.face(o.Face())
			for i := range fi.abilities {
				if fi.abilities[i].cast {
					se.ability = &fi.abilities[i]
					se.rule = se.ability.rule
				}
			}
		}
		return se
	}
	// an ability: find it on its source's face, unless the source has gone
	// to a hidden zone
	se.triggered = state.StackKindOf(g, o) == state.StackKindTriggered
	if src := g.Obj(o.Source); src != nil && src.Face() != nil && !src.Zone.Hidden() && !src.Ephemeral() {
		fi := enc.face(src.Face())
		for i := range fi.abilities {
			if fi.abilities[i].sa == o.Ability && !fi.abilities[i].cast {
				se.ability = &fi.abilities[i]
				se.rule = se.ability.rule
				se.mode = se.ability.trigger
			}
		}
	}
	if se.ability == nil {
		switch desc := o.Ability.Params["SpellDescription"]; {
		case desc != "":
			se.rule = thisName(desc)
		case se.triggered:
			se.rule = "T:" + o.Ability.API
		default:
			se.rule = "AB:" + o.Ability.API
		}
	}
	se.name = CleanString("stack ability (" + se.rule + ")")
	se.entity = se.name
	return se
}

// encodeStack is processStack (413-424): top first, Depth from 1.
func (enc *Encoder) encodeStack(n *Node) {
	for i := range enc.stack {
		se := &enc.stack[i]
		so := n.Sub(se.name, true)                // 420
		so.AddNumericFeature("Depth", i+1, false) // 421
		enc.encodeStackObject(se, so)             // 422
	}
}

// encodeStackObject is processStackObject (353-412).
func (enc *Encoder) encodeStackObject(se *stackEntry, n *Node) {
	o := se.o
	// 355: playerId here is the encoder's owner (647)
	if se.sv.Controller == enc.viewer {
		n.Add("isController")
	}
	// 358: the rule goes to the Stack node
	n.parent.Add(se.rule)
	// 360-373
	if len(o.Targets) > 0 {
		tn := n.Sub("targets", false) // 362
		for _, t := range o.Targets {
			tf := tn.Sub(enc.entityName(t), true) // 365
			// 367-370: game.getCard(id) is null for a player, a token and
			// an ability
			if t.IsPlayer {
				continue
			}
			if to := enc.g.Obj(t.Obj); to != nil && !to.IsToken && to.Ability == nil && enc.visible(to) {
				enc.encodeCard(to, printedFace(to), tf)
			}
		}
	}
	// 375-376
	n.AddNumericFeature("Kicks", int(o.TimesKicked), false)
	// 378-384: cost tags. gorge records the announced X; the other tags
	// (kicker, additional costs) are omitted.
	if o.X > 0 {
		n.AddNumeric("X_CostTag", int(o.X))
	}
	// 386-396: the chosen modes of a modal spell
	if len(o.ChosenModes) > 0 {
		mn := n.Sub("modes", false) // 388
		for _, m := range o.ChosenModes {
			text := m
			if f := o.Face(); f != nil && se.sv.Card != nil {
				if d := svarDescription(f.SVars[m]); d != "" {
					text = thisName(d)
				}
			}
			mn.Add(CleanString(text)) // 393
		}
	}
	// 398-399
	n.AddNumericFeature("XValue", int(o.X), false)
	// 401-411
	if se.ability != nil {
		enc.encodeAbility(nil, se.ability, n) // 402, 404
	}
	if se.triggered && se.mode != "" {
		// 129: the type of the event that triggered it; gorge's name for
		// it is the Forge trigger mode
		n.Add(se.mode)
	}
	// 405-409: a spell's static abilities that work on the stack -- omitted
}

// visible reports whether the deciding seat may read o's face: it is in a
// public zone and not a face-down card the projection blanked.
func (enc *Encoder) visible(o *state.Object) bool {
	if o.Zone.Hidden() || o.Face() == nil {
		return false
	}
	if o.Zone == state.ZStack {
		for i := range enc.stack {
			if enc.stack[i].o == o {
				return enc.stack[i].sv.Kind != "spell" || enc.stack[i].sv.Card != nil
			}
		}
		return false
	}
	cv := enc.seen[o.ID]
	return cv != nil && !hiddenFace(cv)
}

// entityName is Game.getEntityName(id, playerId) (GameImpl.java:526-559)
// with playerId the encoder's owner: "PlayerA" for the owner, "PlayerB" for
// the other player, an object's name, and "null" for anything gone or not
// visible.
func (enc *Encoder) entityName(t state.Target) string {
	if t.IsPlayer {
		if t.Player == enc.viewer {
			return "PlayerA"
		}
		return "PlayerB"
	}
	for i := range enc.stack {
		if enc.stack[i].sv.ID == t.Obj {
			return enc.stack[i].entity
		}
	}
	if cv := enc.seen[t.Obj]; cv != nil {
		return cv.Name
	}
	return "null"
}
