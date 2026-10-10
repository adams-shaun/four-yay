package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// ManaExpendSub is the level-B sub-family of Mode$ ManaExpend ("Whenever you
// expend N ...", CR-expend): a normal triggered ability that fires when a
// player's per-turn cast expenditure crosses N. Its cause is a probe spell
// whose mana value spends at least N, built by the mana recipe in
// compliance/oraclegen/templates. Exported so the recipe and this classifier
// share one spelling.
const ManaExpendSub = "trigger.mana-expend"

// TapsForManaSub is the level-B sub-family of a Mode$ TapsForMana trigger
// marked Static$ True (CR 605.1b triggered mana ability): tapping a permanent
// for mana makes the source add mana. It resolves off the stack, so its
// observation is the mana pool, not a stack entry. Exported so the recipe and
// this classifier share one spelling.
const TapsForManaSub = "trigger.mana-tap"

// classifyManaTrigger names the mana-trigger shapes a p0-only recipe can
// cause: a "whenever you expend N" trigger (a probe cast spends N) and a
// Static$ True TapsForMana triggered mana ability (a probe tap for mana).
// ok is false for every other trigger, which classifyTrigger then classifies
// as before. The shapes are disjoint from the v1 ones, so running it first
// moves a requirement only between sub-families, never in or out of the
// requirement set.
func classifyManaTrigger(t *cards.Trigger) (sub string, ok bool) {
	switch t.ModeKind() {
	case cards.TriggerManaExpend:
		player := strings.TrimSpace(t.ParamStr(cards.PKPlayer))
		if player == "" || strings.EqualFold(player, "You") {
			return ManaExpendSub, true
		}
	case cards.TriggerTapsForMana:
		if strings.EqualFold(strings.TrimSpace(t.ParamStr(cards.PKStatic)), "True") && manaTapFilterServable(t) {
			return TapsForManaSub, true
		}
	}
	return "", false
}

// manaTapFilterServable reports whether the mana-tap recipe can tap a source
// the trigger's ValidCard$ accepts: a land (Land, Land.Basic), a creature (a
// mana dork), the land the source is attached to (Card.AttachedBy), or the
// artifact token the recipe's token probe creates (ManaTapTokenProbe). Any
// other filter stays a gap.
func manaTapFilterServable(t *cards.Trigger) bool {
	if ManaTapTokenProbe(t) {
		return true
	}
	filter := strings.TrimSpace(t.ParamStr(cards.PKValidCard))
	if strings.EqualFold(strings.TrimSpace(t.ParamStr(cards.PKProduced)), "C") {
		return filterHasToken(filter, "Land")
	}
	return filterHasToken(filter, "Land") || filterHasToken(filter, "AttachedBy") || filterHasToken(filter, "Creature")
}

// ManaTapTokenProbe reports whether a TapsForMana trigger is the
// artifact-token shape the mana recipe's token probe serves, and is the one
// home for that shape: the classifier admits it and the recipe picks its
// token subject from the same call. It holds when every comma alternative of
// ValidCard$ names exactly Artifact.token (Roxanne, Starfall Savant's
// `Artifact.token` is the corpus's only carrier) and the mana produced is
// absent or C (the probe token produces only C). A qualifier, a second
// alternative or any other Produced$ value fails closed to trigger.gap.
func ManaTapTokenProbe(t *cards.Trigger) bool {
	return manaTapTokenFilter(t.ParamStr(cards.PKValidCard)) && manaTapTokenProduced(t.ParamStr(cards.PKProduced))
}

// manaTapTokenFilter reports whether a filter is only `Artifact.token`
// alternatives: each comma-separated alternative splits into exactly two
// tokens, Artifact and token, with no extra qualifier.
func manaTapTokenFilter(filter string) bool {
	found := false
	for _, alt := range strings.Split(filter, ",") {
		found = true
		toks := strings.FieldsFunc(alt, func(r rune) bool { return r == '.' || r == '+' })
		if len(toks) != 2 || !strings.EqualFold(strings.TrimSpace(toks[0]), "Artifact") ||
			!strings.EqualFold(strings.TrimSpace(toks[1]), "token") {
			return false
		}
	}
	return found
}

// manaTapTokenProduced reports whether a Produced$ value is one the
// Powerstone token's `Add {C}` satisfies: absent, or C.
func manaTapTokenProduced(raw string) bool {
	raw = strings.TrimSpace(raw)
	return raw == "" || strings.EqualFold(raw, "C")
}
