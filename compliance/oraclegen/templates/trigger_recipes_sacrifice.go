package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/state"
)

// sacrificeSpells pair a spell that makes its controller sacrifice with the
// permanent it is answered with and a decoy, so the sacrifice is a real choice
// between two permanents (XMage poses the pick even where gorge's own log would
// drop a sole candidate). Village Rites sacrifices a creature; Deadly Dispute
// ({1}{B}) an artifact or a creature, as a cost.
var sacrificeSpells = []struct{ spell, victim, decoy string }{
	{"Village Rites", bearsProbe, "Llanowar Elves"},
	{"Deadly Dispute", "Ornithopter", "Llanowar Elves"},
}

// sacrificeTriggerRecipe builds the causes of the trigger.sacrificed
// sub-family: a token maker's prelude for a token filter, Village Rites /
// Deadly Dispute for a creature or artifact victim the filter accepts, and the
// card's own sacrifice ability for a self filter. Every cause's steps stop at
// the spell, leaving the trigger on the stack for the generator's own settling
// to resolve.
func sacrificeTriggerRecipe(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) ([]triggerCause, string, bool) {
	filter := t.ParamStr(cards.PKValidCard)
	var causes []triggerCause
	if kind := sacrificeTokenWord(filter); kind != "" {
		c, why := tokenSacrificeCause(reg, kind)
		if why != "" {
			return nil, why, true
		}
		causes = append(causes, c)
	}
	causes = append(causes, fixtureSacrificeCauses(reg, name, filter)...)
	if namesSelfFold(filter) {
		if c, ok := selfSacrificeCause(f, name); ok {
			causes = append(causes, c)
		}
	}
	if len(causes) == 0 {
		if namesSelfFold(filter) {
			return nil, "sacrificed self needs its own sacrifice ability", true
		}
		return nil, "no sacrifice victim the filter accepts", true
	}
	return causes, "", true
}

// sacrificeTokenWord names the token kind a sacrifice filter asks for, or ""
// for a filter that names no token. The generic "token" qualifier maps to Food,
// an artifact token that pays every artifact, permanent and creature token
// base.
func sacrificeTokenWord(filter string) string {
	switch {
	case hasToken(filter, "food"):
		return "Food"
	case hasToken(filter, "clue"):
		return "Clue"
	case hasToken(filter, "treasure"):
		return "Treasure"
	case hasToken(filter, "token"):
		return "Food"
	}
	return ""
}

// tokenSacrificeCause mints a token with a maker's cast-and-resolve prelude,
// then casts Deadly Dispute (whose artifact-or-creature sacrifice cost a token
// artifact pays) answered with the token's name, so the sacrifice is the token
// the trigger's filter names. A decoy artifact keeps the pick a real choice.
func tokenSacrificeCause(reg *cards.Registry, kind string) (triggerCause, string) {
	if _, ok := activationTokenMakers[kind]; !ok {
		return triggerCause{}, "sacrifice token " + kind + " has no maker"
	}
	cast, ok := castProbe(reg, "Deadly Dispute")
	if !ok {
		return triggerCause{}, "Deadly Dispute unavailable"
	}
	cast.Answers = []oraclegen.Answer{{Kind: "choose", Pick: []string{kind + " Token"}}}
	pre, ok := tokenCostPrelude(reg, []tokenNeed{{kind: kind, n: 1}})
	if !ok {
		return triggerCause{}, "no token maker for " + kind
	}
	return triggerCause{
		hand:        append(append([]string(nil), pre.hand...), "Deadly Dispute"),
		battlefield: []string{"Ornithopter"},
		prelude:     pre.steps,
		steps:       []oraclegen.Step{cast},
	}, ""
}

// fixtureSacrificeCauses pairs each sacrifice spell with the fixture the
// filter accepts. The matcher picks the victim and whose side it sits on (an
// OppCtrl filter has p1 cast the spell).
func fixtureSacrificeCauses(reg *cards.Registry, name, filter string) []triggerCause {
	var causes []triggerCause
	fp := newFilterProbe(filter, state.ZBattlefield)
	for _, fx := range sacrificeSpells {
		victim, ok := reg.Lookup(fx.victim)
		if !ok || fx.victim == name || len(victim.Faces) == 0 {
			continue
		}
		fp.controller = 0
		ownSide := fp.accepts(victim)
		fp.controller = 1
		oppSide := fp.accepts(victim)
		if !ownSide && !oppSide {
			continue
		}
		decoy := fx.decoy
		if decoy == name || decoy == fx.victim {
			decoy = ""
		}
		cast, ok := castProbe(reg, fx.spell)
		if !ok {
			continue
		}
		cast.Answers = []oraclegen.Answer{{Kind: "choose", Pick: []string{fx.victim}}}
		field := []string{fx.victim}
		if decoy != "" {
			field = append(field, decoy)
		}
		if ownSide {
			causes = append(causes, triggerCause{hand: []string{fx.spell}, battlefield: field, steps: []oraclegen.Step{cast}})
			continue
		}
		cast.Seat, cast.Card = 1, "p1:"+fx.spell
		causes = append(causes, triggerCause{opponentHand: []string{fx.spell}, opponentBattlefield: field, steps: []oraclegen.Step{{Op: "pass", Seat: 0}, cast}})
	}
	return causes
}

// selfSacrificeCause activates the card's own `Sac<1/CARDNAME>` ability: the
// activate step names the ability by IR index and carries XMage's rule-text
// prefix, exactly as the attack-activation cause does.
func selfSacrificeCause(f *cards.Face, name string) (triggerCause, bool) {
	prefixes, why := oraclegen.XMageAbility(f)
	if why != "" {
		return triggerCause{}, false
	}
	for i, sa := range f.Abilities {
		cost := sa.ParamStr(cards.PKCost)
		if !sa.IsActivated() || !costSacrificesSelf(cost) {
			continue
		}
		pool, gap := activationCostIn(cost, "battlefield", "")
		prefix, ok := prefixes[i]
		if gap != "" || !ok {
			continue
		}
		idx := i
		step := oraclegen.Step{Op: "activate", Seat: 0, Card: "p0:" + name, Mana: pool, AbilityIndex: &idx, Answers: activationXAnswers(cost)}
		return triggerCause{steps: []oraclegen.Step{step}, xability: []string{prefix}, activateCost: cost}, true
	}
	return triggerCause{}, false
}

// costSacrificesSelf reports a Forge cost with a Sac<N/CARDNAME> part.
func costSacrificesSelf(cost string) bool {
	for _, tok := range costTokens(cost) {
		if strings.HasPrefix(tok, "Sac<") && sacSelf(tok) {
			return true
		}
	}
	return false
}

// namesSelfFold reports whether a filter selects the source card itself.
func namesSelfFold(filter string) bool { return filterHasTokenFold(filter, "self") }

// filterHasTokenFold reports whether a filter carries want (lower case) as a
// '.'/'+'/',' separated token, ignoring case.
func filterHasTokenFold(filter, want string) bool {
	for _, tok := range strings.FieldsFunc(strings.ToLower(filter), func(r rune) bool { return r == '.' || r == '+' || r == ',' }) {
		if strings.TrimSpace(tok) == want {
			return true
		}
	}
	return false
}
