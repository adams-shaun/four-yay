package manabrew

import (
	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

// promptTarget (scoping spec §6.3): one candidate per option, in option
// order; the intent comes from TargetEffect.API/Removal/Damage so a
// ManaBrew client can colour the ask without learning any rules, and the
// hostile bit is derived from the same intent (unknown APIs fall back to
// "friendly", the conservative default the intent enum documents).
func (t *Translator) promptTarget(d *decision.Decision, v *view.View) (mb.PromptMessage, error) {
	intent, hostile := targetingIntent(d.TargetEffect)
	cands := make([]mb.TargetRef, 0, len(d.Options))
	for _, opt := range d.Options {
		cands = append(cands, mb.TargetRef{Kind: targetRefKind(opt.Kind), ID: targetRefID(opt),
			Intent: intent, Oracle: opt.Label})
	}
	in := mb.ChooseBoardTargetsInput{
		PromptBase:    mb.PromptBase{Presentation: mb.PromptPresentation{Title: d.Prompt, Targets: cands}},
		Candidates:    cands,
		Hostile:       hostile,
		Intent:        intent,
		MinTargets:    d.Min,
		MaxTargets:    d.Max,
		ChosenTargets: []mb.TargetRef{},
		Cancellable:   false,
	}
	return mb.PromptMessage{Kind: "prompt", AgentPrompt: mb.AgentPrompt{
		PromptID: promptID(d), DecidingPlayerID: playerID(d.Player),
		SourceCard: t.sourceCard(v, d.Source),
		Input:      mb.PromptInput{Value: in}}}, nil
}

// targetingIntent is the §6.3 TargetEffect.API → TargetingIntent table: one
// row per recognised API or removal kind, and a conservative default. It is
// PRESENTATION only -- the engine's own gates still rule the answer.
func targetingIntent(e *decision.TargetEffect) (mb.TargetingIntent, bool) {
	if e == nil {
		return "", false
	}
	// The removal kind is the most specific classification (a ChangeZone's
	// modelled destination), so it wins over the API table; damage is last
	// (DealDamage/DamageAll set Removal absent but Damage present).
	var intent mb.TargetingIntent
	if e.Removal != nil {
		intent = removalIntent(e.Removal.Kind)
	}
	if intent == "" {
		intent = intentFromAPI(e.API)
	}
	if intent == "" && e.Damage != nil {
		intent = mb.IntentDamage
	}
	if intent == "" && e.Removal != nil {
		// An unrecognised removal kind is still hostile by shape.
		return mb.IntentDestroy, true
	}
	return intent, hostileIntent(intent)
}

// intentFromAPI maps the compiled primitive names the effects registry
// registers (the row set is closed over Register("...") calls).
func intentFromAPI(api string) mb.TargetingIntent {
	switch api {
	case "Counter":
		return mb.IntentCounter
	case "Draw", "Dig", "Seek":
		return mb.IntentDraw
	case "Mill":
		return mb.IntentMill
	case "Discard":
		return mb.IntentDiscard
	case "Pump", "PumpAll":
		return mb.IntentBuff
	case "GainLife":
		return mb.IntentHeal
	case "LoseLife":
		return mb.IntentLoseLife
	case "Tap", "TapAll", "TapOrUntap":
		return mb.IntentTap
	case "Untap", "UntapAll":
		return mb.IntentUntap
	case "CopyPermanent", "CopySpellAbility":
		return mb.IntentCopy
	case "GainControl", "GainControlVariant":
		return mb.IntentGainControl
	case "Fight":
		return mb.IntentFight
	case "Attach":
		return mb.IntentAttach
	case "Destroy", "DestroyAll":
		return mb.IntentDestroy
	case "Sacrifice", "SacrificeAll":
		return mb.IntentSacrifice
	case "ChangeZone", "ChangeZoneAll":
		// Removal alone does not imply hostility; the removal kind decides
		// when TargetEffect classified it.
		return mb.IntentExile
	default:
		return ""
	}
}

// removalIntent maps RemovalEffect.Kind.
func removalIntent(kind string) mb.TargetingIntent {
	switch kind {
	case "destroy":
		return mb.IntentDestroy
	case "sacrifice":
		return mb.IntentSacrifice
	case "exile", "command":
		return mb.IntentExile
	case "bounce":
		return mb.IntentBounce
	case "graveyard", "library":
		return mb.IntentMill
	default:
		return ""
	}
}

// hostileIntent is the hostile half of the §6.3 table: intents a defender
// should fear. attack/block stay out -- those kinds are answered only by
// combat prompts, never by a target prompt.
func hostileIntent(i mb.TargetingIntent) bool {
	switch i {
	case mb.IntentDamage, mb.IntentDestroy, mb.IntentSacrifice, mb.IntentExile,
		mb.IntentMill, mb.IntentDiscard, mb.IntentCounter, mb.IntentLoseLife,
		mb.IntentTap, mb.IntentGainControl, mb.IntentFight, mb.IntentDebuff,
		mb.IntentAttack, mb.IntentBlock, mb.IntentHostile:
		return true
	}
	return false
}

// targetRefKind maps a target option's kind onto the wire ref kind.
func targetRefKind(optKind string) mb.TargetRefKind {
	switch optKind {
	case "player":
		return mb.RefPlayer
	case "spell", "trigger", "ability":
		return mb.RefSpell
	default:
		// permanent, graveyard, and any later card-shaped kind.
		return mb.RefCard
	}
}

// targetRefID maps a target option onto the wire ref id (§6.1's id mints).
func targetRefID(opt decision.Option) string {
	switch opt.Kind {
	case "player":
		return playerID(opt.Player)
	case "spell", "trigger", "ability":
		return stackID(opt.Obj)
	default:
		return cardID(opt.Obj)
	}
}

// parseBoardTargets maps boardTargets{chosen}: each chosen ref is matched
// against the candidates the prompt offered (kind and id), in response
// order; the match's candidate position IS the option index. An unknown ref
// or a repeat is what Decision.Validate says (invalidShape), so the fence
// stays in one place.
func (t *Translator) parseBoardTargets(out mb.PromptOutputValue, p *Pending) Outcome {
	d := p.Decision
	cur := p.Prompt.PromptID
	dec, ok := out.(mb.BoardTargetsDecision)
	if !ok {
		return Outcome{Err: errCode(mb.CodeWrongPromptType, "not a boardTargets answer", idPtr(cur))}
	}
	in, ok := p.Prompt.Input.Value.(mb.ChooseBoardTargetsInput)
	if !ok {
		return Outcome{Err: errCode(mb.CodeInvalidShape, "the open prompt is not a board-target ask", idPtr(cur))}
	}
	choices := make([]int, 0, len(dec.Chosen))
	for _, ref := range dec.Chosen {
		idx := -1
		for i, cand := range in.Candidates {
			if cand.Kind == ref.Kind && cand.ID == ref.ID {
				idx = d.Options[i].Index
				break
			}
		}
		if idx < 0 {
			return Outcome{Err: errCode(mb.CodeInvalidShape,
				"target "+ref.ID+" is not offered", idPtr(cur))}
		}
		choices = append(choices, idx)
	}
	intent := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}
	if err := d.Validate(intent); err != nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), idPtr(cur))}
	}
	return Outcome{Intent: &intent}
}
