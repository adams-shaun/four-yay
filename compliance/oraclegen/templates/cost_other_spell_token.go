package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// This file serves the one cost static that reduces an ACTIVATED ABILITY cost
// of a class of tokens (Mutagen Man, Living Ooze: "Activated abilities of
// artifact tokens you control cost {1} less"). A token is not a registry
// card, so the probe cannot pick an ability from the corpus the way
// abilityCostProbes does: a token maker in the prelude creates a token of the
// class, and the activate step pays the TOKEN's own ability at the reduced
// price. The token face's XMage rule text is derived with
// oraclegen.XMageAbility, never spelled by hand, so a corpus pin move that
// renumbers the token script breaks the build instead of publishing a stale
// prefix.

// tokenAbilityTokenKeys maps a token KIND (anyTokenKind's value, the
// "<Subtype> Token" name gorge and XMage's picker use) to the corpus token
// script key whose face the probe activates. The Food token is an Artifact,
// so it pays an artifact-token reducer's class. Keyed by the token script's
// file stem, exactly as cards.Registry.Tokens is keyed
// (effects/investigate.go's clueTokenKey is the precedent).
var tokenAbilityTokenKeys = map[string]string{
	"Food": "c_a_food_sac",
}

// tokenAbilityProbes is the hand-off for a cost static whose ValidCard filter
// names a token class (otherCostProbes' "token" branch). filter is the
// static's ValidCard value; the class it names must be one anyTokenKind maps
// to a maker kind with a token script, and the token face must hold exactly
// one activated ability, or the shape is a narrowed named skip. The
// token-branch filter terms (Artifact, YouCtrl) are satisfied by the chosen
// maker's token by construction, so this builder asserts the class instead of
// running spellFilterSupported.
func tokenAbilityProbes(reg *cards.Registry, source *cards.Face, st cards.Static, filter string, base costProbe) ([]costProbe, string) {
	if !strings.EqualFold(st.Params["Type"], "Ability") {
		return nil, "token-ability probe unsupported: " + filter + " names tokens but Type$ is not Ability"
	}
	// AffectedZone$ Battlefield is the zone this probe establishes (the maker
	// puts the token there); Forge's absent value defaults to it. Any other
	// zone names a shape whose token this probe cannot place where the static
	// reads it.
	if zone := st.Params["AffectedZone"]; zone != "" && !strings.EqualFold(zone, "Battlefield") {
		return nil, "token-ability probe unsupported: AffectedZone " + zone
	}
	kind, ok := tokenAbilityKind(filter)
	if !ok {
		return nil, "token-ability probe unsupported: no token class this generator can make (" + filter + ")"
	}
	key, ok := tokenAbilityTokenKeys[kind]
	if !ok {
		return nil, "token-ability probe unsupported: no token script for " + kind
	}
	token, ok := reg.Tokens[key]
	if !ok || len(token.Faces) == 0 || token.Faces[0] == nil {
		return nil, "token-ability probe unsupported: token script " + key + " absent"
	}
	face := token.Faces[0]
	prefixes, why := oraclegen.XMageAbility(face)
	if why != "" {
		return nil, "token-ability probe unsupported: " + why
	}
	activated := 0
	for _, sa := range face.Abilities {
		if sa.IsActivated() {
			activated++
		}
	}
	// The probe activates index 0; a face with a second activated ability
	// (or none) means that ordinal is not the ability under test.
	if activated != 1 || prefixes[0] == "" {
		return nil, "token-ability probe unsupported: token face holds " + strconv.Itoa(activated) + " activated abilities"
	}
	pool, ok := tokenAbilityMana(face.Abilities[0].ParamStr(cards.PKCost))
	if !ok {
		return nil, "token-ability probe unsupported: mana part of " + face.Abilities[0].ParamStr(cards.PKCost)
	}
	p := base
	pre, ok := tokenCostPrelude(reg, []tokenNeed{{kind: kind, n: 1}})
	if !ok {
		return nil, "token-ability probe unsupported: token maker unavailable for " + kind
	}
	p.hand = append(p.hand, pre.hand...)
	p.pre = append(p.pre, pre.steps...)
	if reason := costStaticConditions(reg, st, &p); reason != "" {
		return nil, reason
	}
	reduction, reason := costStaticReduction(source, st, &p)
	if reason != "" {
		return nil, reason
	}
	mana, ok := removeGenericMana(pool, reduction)
	if !ok || mana == "" {
		return nil, "token-ability probe unsupported: reduction exceeds the mana part " + pool
	}
	p.spell = "token:" + kind
	p.mana, p.full = mana, pool
	p.activate = &costActivation{index: 0, prefix: prefixes[0]}
	p.tokenAnswers = tokenCostAnswerNames([]tokenNeed{{kind: kind, n: 1}})
	// The reduced-price activation must play through; a probe gorge cannot
	// pay at the reduced price is kept only when the full price plays (the
	// divergence direction the ability probes share).
	p.mustReplay = true
	return []costProbe{p}, ""
}

// tokenAbilityKind is the maker kind anyTokenKind maps the token class in
// filter to: the first filter term naming a class with a maker. ok is false
// when no term names one.
func tokenAbilityKind(filter string) (string, bool) {
	for _, term := range filterTerms(filter) {
		kind, ok := anyTokenKind[strings.ToLower(term)]
		if !ok {
			continue
		}
		if _, ok := activationTokenMakers[kind]; ok {
			return kind, true
		}
	}
	return "", false
}

// tokenAbilityMana is the mana pool of a token ability's Forge cost, parsed
// from its non-bracket, non-tap tokens.
func tokenAbilityMana(cost string) (string, bool) {
	part, ok := tokenAbilityManaPart(cost)
	if !ok {
		return "", false
	}
	pool, why := oraclegen.PoolFor(part)
	if why != "" || pool == "" {
		return "", false
	}
	return pool, true
}

// tokenAbilityManaPart is the mana part of a token ability's Forge cost: its
// non-bracket, non-tap tokens. A bracket token must be the self-sacrifice
// (Sac<...>) the activation itself pays; any other non-mana bracket (a
// discard, an exile) fails closed, because the probe pays no such cost.
func tokenAbilityManaPart(cost string) (string, bool) {
	var parts []string
	for _, tok := range costTokens(cost) {
		switch {
		case strings.EqualFold(tok, "T"):
		case strings.HasPrefix(tok, "Sac<"):
			if _, ok := bracketPayload(tok); !ok {
				return "", false
			}
		case strings.ContainsAny(tok, "<>"):
			return "", false
		default:
			parts = append(parts, tok)
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, " "), true
}
