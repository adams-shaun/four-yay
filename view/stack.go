package view

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// StackView is one object on the stack. Kind is "spell", "trigger" (an
// object minted by a TriggerPush) or "ability" (any other ability object).
type StackView struct {
	ID         state.ObjID    `json:"id"`
	Kind       string         `json:"kind"`
	Name       string         `json:"name"` // card name; for an ability, its source's name
	Text       string         `json:"text"` // what it does, in the card's own words, when known
	Controller state.PlayerID `json:"controller"`
	Source     state.ObjID    `json:"source,omitempty"` // ability only: the permanent it came from
	// Targets is a public list (Ruling T23-u): non-nil, "[]" not "null",
	// even when nothing has been targeted yet.
	Targets []TargetView `json:"targets"`
	Card    *CardView    `json:"card,omitempty"` // spell only
	// Optional and Decider are Ruling VW-1: an optional triggered ability on
	// the stack, awaiting its resolution-time yes/no (CR 603.5), reports that
	// it is optional and names the seat that answers it. Decider is nil
	// unless Optional.
	Optional bool            `json:"optional"`
	Decider  *state.PlayerID `json:"decider,omitempty"`
	// X is the announced value of X for this spell or ability (CR 107.3), 0
	// (omitted) when it has none; Kicks is how many times it was kicked
	// (CR 702.33 / 702.43: 1 for a plain kicked cast, the multikicker count
	// otherwise), 0 for a copy or an unkicked cast; Modes are the SVar names
	// of the sub-abilities a modal spell or trigger chose (CR 601.2b), in
	// execution order. Public cast-time facts every seat can read off the
	// stack; all omitted when empty, so an ordinary stack object serialises
	// byte-identically to before these fields existed.
	X     int32    `json:"x,omitempty"`
	Kicks int32    `json:"kicks,omitempty"`
	Modes []string `json:"modes,omitempty"`
}

// castDetail fills sv's X, Kicks and Modes from the stack object.
func castDetail(sv *StackView, o *state.Object) {
	sv.X = o.X
	sv.Kicks = o.TimesKicked
	if sv.Kicks == 0 && o.CastFlags&state.FlagKicked != 0 {
		sv.Kicks = 1
	}
	if len(o.ChosenModes) > 0 {
		sv.Modes = append([]string(nil), o.ChosenModes...)
	}
}

// TargetView is one chosen target: exactly one of Obj and Player means
// anything, discriminated by IsPlayer — the same shape as state.Target.
type TargetView struct {
	Obj      state.ObjID    `json:"obj,omitempty"`
	Player   state.PlayerID `json:"player"`
	IsPlayer bool           `json:"is_player"`
	// Label is what the object was allowed to target, in the card's own
	// words: its TgtPrompt$ when it has one ("Select any target"), else its
	// ValidTgts$ ("Creature"). Empty when the object declares neither.
	Label string `json:"label,omitempty"`
}

// stackViews maps the stack's own object ids to StackViews, bottom to top,
// into buf's storage (refill; each slot's Targets, Card and Decider storage
// is reused). Always non-nil (Ruling T23-u).
//
// viewer and revealFaceDown redact a face-down SPELL (CR 708.4: a
// face-down cast's spell has no name, no types and no abilities while it
// sits on the stack): every viewer but the caster sees a blank spell band
// -- no name, no text, no printed card -- exactly the redaction the
// battlefield's cardViews applies to a face-down permanent. The FaceDown
// state bit rides the PutOnStack's entry marker (rules/cast.go pushCast);
// no ordinary spell ever carries it, so every unrelated stack band is
// byte-identical.
func (p *projector) stackViews(buf []StackView, ids []state.ObjID, revealFaceDown bool) []StackView {
	g := p.g
	out := refill(buf, len(ids))
	for _, id := range ids {
		o := g.Obj(id)
		if o == nil {
			continue
		}
		n := len(out)
		out = out[:n+1]
		sv := &out[n]
		// The slot's previous storage (ProjectInto's reuse).
		targets, card, decider := sv.Targets, sv.Card, sv.Decider
		if o.Ability != nil {
			// An ability object has no Face (Ruling F3): Card == nil, set
			// by events/apply.go's TriggerPush case. Its display name is
			// the face name of the permanent it came from, or "Ability"
			// when that permanent is also gone (supplement §2). Kind
			// distinguishes a TriggerPush object from any other ability
			// object (an activated ability, once the engine enumerates
			// them) so a client can render them differently.
			kind := "ability"
			// The stamped kind is the authority (state.StackKindOf falls
			// back to the same triggerLine lookup the view used to make
			// itself): a trigger whose T: line the lookup cannot find (a
			// delayed or keyword-minted one) is still a trigger.
			if state.StackKindOf(g, o) == state.StackKindTriggered {
				kind = "trigger"
			}
			*sv = StackView{
				ID: id, Kind: kind, Name: abilityName(g, o), Text: abilityText(g, o, p.text),
				Controller: o.Controller, Source: o.Source, Targets: targetViews(targets, o.Targets, o.SubTargets, targetLabel(o)),
			}
			castDetail(sv, o)
			// Ruling VW-1: an optional triggered ability on the stack awaiting
			// its resolution-time yes/no reports its optionality and decider
			// here, where the ability actually is. The engine derives it (it
			// is the one place the OptionalDecider$ spec grammar lives, via
			// deciderFromSpec); the view only asks, never re-derives it.
			if p.ch != nil {
				if opt, who := p.ch.StackOptional(id); opt {
					sv.Optional = true
					if decider == nil {
						decider = new(state.PlayerID)
					}
					*decider = who
					sv.Decider = decider
				}
			}
			// The ability object has no face of its own (Ruling F3), so the
			// artwork a client shows for a trigger/ability band has to come
			// from the permanent it was minted from -- the same cardView the
			// spell branch uses for its own id. Fill Card only while that
			// source is a visible object: the source may have left the
			// battlefield to a hidden zone (a "leaves the battlefield"
			// trigger whose card is now in its owner's hand) or be gone
			// entirely, and neither may be exposed here. This is the same
			// projection everything else uses, never a hand-built view.
			if src := g.Obj(o.Source); src != nil && src.Face() != nil && !src.Zone.Hidden() && !src.Ephemeral() {
				if card == nil {
					card = new(CardView)
				}
				p.cardView(card, o.Source)
				sv.Card = card
			}
			continue
		}
		*sv = StackView{ID: id, Kind: "spell", Controller: o.Controller, Targets: targetViews(targets, o.Targets, o.SubTargets, targetLabel(o))}
		if f := o.Face(); f != nil {
			if o.FaceDown && !revealFaceDown && p.viewer != o.Controller {
				// CR 708.4: the face-down spell's printed identity is hidden
				// from everyone but its controller. The controller's own view
				// falls through to the printed band below.
				continue
			}
			castDetail(sv, o)
			sv.Name = f.Name
			sv.Text = spellText(f, p.text)
			if card == nil {
				card = new(CardView)
			}
			p.cardView(card, id)
			sv.Card = card
		}
	}
	return out
}

// abilityName is the face name of the permanent an ability object came
// from, or "Ability" when that permanent is gone too (supplement §2).
func abilityName(g *state.Game, o *state.Object) string {
	if src := g.Obj(o.Source); src != nil {
		if f := src.Face(); f != nil && f.Name != "" {
			return f.Name
		}
	}
	return "Ability"
}

// triggerLine finds the T: line an ability object was minted from: the
// trigger on its source's active face whose Effect is exactly o.Ability
// (events/apply.go's TriggerPush case sets it so). The classification is
// state.TriggerOf, shared with rules' TargetType$ target legality so the
// view's Kind and the engine's legality cannot disagree.
func triggerLine(g *state.Game, o *state.Object) (cards.Trigger, bool) {
	return state.TriggerOf(g, o)
}

// abilityText finds the T: line an ability object was minted from (see
// triggerLine) and returns its TriggerDescription$. Falling back to the
// SA's own SpellDescription$/StackDescription$ covers an activated ability
// (a later milestone) or a source that changed face since the trigger
// matched (rules/trigger.go's triggerOf documents the same caveat). The
// text is then placeholder-substituted with the same display name the
// StackView.Name uses -- the source's face name, or "Ability" when the
// source is gone (see abilityName and substitutePlaceholders).
func abilityText(g *state.Game, o *state.Object, memo *textMemo) string {
	text := ""
	if t, ok := triggerLine(g, o); ok {
		if d := t.Params["TriggerDescription"]; d != "" {
			text = d
		}
	}
	if text == "" && o.Ability != nil {
		if d := o.Ability.Params["SpellDescription"]; d != "" {
			text = d
		}
		if text == "" {
			if d := o.Ability.Params["StackDescription"]; d != "" {
				text = d
			}
		}
	}
	return memo.substitute(text, abilityName(g, o))
}

// spellText is SpellDescription$ of the face's own cast ability, falling
// back to the printed Oracle text, with Forge's self-reference placeholders
// substituted by the card's own name (see substitutePlaceholders).
func spellText(f *cards.Face, memo *textMemo) string {
	text := f.Oracle
	if sa := f.SpellAbility(); sa != nil {
		if d := sa.Params["SpellDescription"]; d != "" {
			text = d
		}
	}
	return memo.substitute(text, f.Name)
}

// targetViews copies an object's chosen targets. Object.Remembered is NEVER
// projected here or anywhere else in this package — it can name a
// hidden-zone object (e.g. the card a "whenever you draw" trigger
// remembered) — only Targets, which is a player-visible choice already.
// label (from targetLabel) is stamped on every entry: the object declared
// one set of legal targets, not one per chosen target. Always non-nil
// (Ruling T23-u).
func targetViews(buf []TargetView, targets, subTargets []state.Target, label string) []TargetView {
	out := refill(buf, len(targets)+len(subTargets))
	for _, t := range targets {
		out = append(out, TargetView{Obj: t.Obj, Player: t.Player, IsPlayer: t.IsPlayer, Label: label})
	}
	// The SubAbility$ links' targets, announced on cast with the root's (CR
	// 601.2c): a client draws them like any other target of the object.
	for _, t := range subTargets {
		out = append(out, TargetView{Obj: t.Obj, Player: t.Player, IsPlayer: t.IsPlayer, Label: label})
	}
	return out
}

// targetLabel is the object's own description of what it targets, from the
// first SA in its chain (the SA itself, then SubAbility$ links) that
// declares TgtPrompt$, else the first that declares ValidTgts$.
func targetLabel(o *state.Object) string {
	var sa *cards.SA
	if o.Ability != nil {
		sa = o.Ability
	} else if f := o.Face(); f != nil {
		sa = f.SpellAbility()
	}
	valid := ""
	for s := sa; s != nil; s = s.Sub {
		if p := s.Params["TgtPrompt"]; p != "" {
			return p
		}
		if valid == "" {
			valid = s.Params["ValidTgts"]
		}
	}
	return valid
}

// pendingViews copies the engine's pending-trigger queue into the wire
// shape, in the same order: R3. Always non-nil (Ruling T23-u), including
// when called with nil (Project's own default before a real Chars, if any,
// overwrites it).
func pendingViews(buf []PendingView, pts []state.PendingTrigger) []PendingView {
	out := refill(buf, len(pts))
	for _, pt := range pts {
		n := len(out)
		out = out[:n+1]
		pv := &out[n]
		who := pv.Decider // the slot's previous storage (ProjectInto's reuse)
		*pv = PendingView{Source: pt.Source, Controller: pt.Controller, Label: pt.Label, Optional: pt.Optional}
		if pt.Optional {
			if who == nil {
				who = new(state.PlayerID)
			}
			*who = pt.Decider
			pv.Decider = who
		}
	}
	return out
}
