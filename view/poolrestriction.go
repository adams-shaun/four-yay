package view

import (
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// This file renders a restricted-mana batch for the seat's pool readout. It
// deliberately does NOT re-implement the payment grammar: whether a payment
// may spend a batch is decided in rules/ (restrictValidMatches), and view/
// does not import rules/. What lives here is a light formatter for the
// common Forge RestrictValid$ shapes the corpus carries, with the raw Valid$
// string as an honest fallback for anything it does not recognise.

// poolRestrictions projects a seat's restricted batches. nil (an absent JSON
// key) when the seat holds none, or when none of its batches carry a spend
// limit -- a batch with an empty Valid (an AddsNoCounter$-only batch,
// Boseiju) imposes no spend restriction and certifies nothing about why a
// cast was refused, so it is not annotated. The returned slice preserves the
// state's batch order (deterministic, never a map range).
func poolRestrictions(buf []PoolRestrictionView, g *state.Game, rs []state.ManaRestriction, memo *textMemo) []PoolRestrictionView {
	out := buf[:0] // the previous projection's storage (ProjectInto's reuse)
	for _, r := range rs {
		text := restrictionText(g, r.Source, r.Valid, memo)
		if text == "" {
			continue
		}
		out = append(out, PoolRestrictionView{Color: r.Color, Amount: r.Amount, Text: text})
	}
	if len(out) == 0 {
		return nil // no restricted batch: absent, as an append from nil leaves it
	}
	return out
}

// restrictionText is humanizeRestrictValid memoised in memo (when non-nil):
// the sentence is a pure function of the Valid$ string and the source's
// chosen type, the one game fact humanizeSpec reads.
func restrictionText(g *state.Game, source state.ObjID, valid string, memo *textMemo) string {
	if memo == nil {
		return humanizeRestrictValid(g, source, valid)
	}
	slot, out, ok := memo.lookup(memoRestrict, valid, sourceChosenType(g, source))
	if ok {
		return out
	}
	return slot.store(memoRestrict, valid, sourceChosenType(g, source), humanizeRestrictValid(g, source, valid))
}

// sourceChosenType is the restriction source's chosen type ("" when there is
// no such object), as humanizeSpec reads it.
func sourceChosenType(g *state.Game, source state.ObjID) string {
	if g != nil && source != 0 {
		if o := g.Obj(source); o != nil {
			return o.ChosenType
		}
	}
	return ""
}

// humanizeRestrictValid renders a batch's RestrictValid$ as a sentence, or ""
// when there is no restriction to state. A comma-separated Valid$ is OR
// semantics (rules/stack.go's restrictValidMatches), so the alternatives are
// joined with " or ". If ANY alternative is a shape the formatter does not
// model, the whole string falls back to the raw Valid$ -- a half-prosed,
// half-raw mixture would read as though the raw half were prose.
func humanizeRestrictValid(g *state.Game, source state.ObjID, valid string) string {
	valid = strings.TrimSpace(valid)
	if valid == "" {
		return ""
	}
	terms := strings.Split(valid, ",")
	parts := make([]string, 0, len(terms))
	for _, term := range terms {
		p, ok := humanizeRestrictTerm(g, source, strings.TrimSpace(term))
		if !ok {
			return valid
		}
		parts = append(parts, p)
	}
	return "spend only to " + strings.Join(parts, " or ")
}

// humanizeRestrictTerm humanises one comma-separated alternative. ok is false
// for a shape it does not model, so the caller can fall back honestly.
func humanizeRestrictTerm(g *state.Game, source state.ObjID, term string) (string, bool) {
	if term == "" {
		return "", false
	}
	kind, spec, dotted := strings.Cut(term, ".")
	if !dotted {
		switch term {
		case "Spell":
			return "cast a spell", true
		case "Activated", "nonSpell":
			return "activate an ability", true
		default:
			return "", false
		}
	}
	switch kind {
	case "Spell":
		phrase, ok := humanizeSpec(g, source, spec, "spell")
		if !ok {
			return "", false
		}
		return "cast " + phrase, true
	case "Activated":
		phrase, ok := humanizeSpec(g, source, spec, "ability")
		if !ok {
			return "", false
		}
		return "activate " + phrase, true
	default:
		return "", false
	}
}

// humanizeSpec turns a dotted term's spec into a noun phrase ("a creature
// spell", "a Demon creature spell", "an artifact ability"), resolving
// ChosenType against the producing permanent's recorded choice. ok is false
// for any qualifier that is not a card type, supertype or colour -- the
// honest raw-string fallback -- so exotic provenance tokens
// (wasCastFromYourHand, MultiColor, ...) never become misleading prose.
func humanizeSpec(g *state.Game, source state.ObjID, spec, noun string) (string, bool) {
	chosen := sourceChosenType(g, source)
	var types []string
	hasChosen := false
	for q := range strings.SplitSeq(spec, "+") {
		q = strings.TrimSpace(q)
		switch {
		case q == "" || q == "inZoneBattlefield":
			// A zone predicate on the ability's own source; it is not part
			// of the noun phrase.
		case q == "ChosenType":
			hasChosen = true
		case knownSpecWord(q):
			types = append(types, strings.ToLower(q))
		default:
			return "", false
		}
	}
	// A bare `Spell.`/`Activated.` spec (restriction on the class alone) is
	// already handled by the bare-term branch; treat it as a plain noun.
	if hasChosen {
		if chosen == "" {
			// The source has left the battlefield or recorded no type: name
			// the shape honestly rather than inventing one.
			if len(types) == 0 {
				return "a creature " + noun + " of the chosen type", true
			}
			phrase := strings.Join(types, " ")
			return article(phrase) + " " + phrase + " " + noun + " of the chosen type", true
		}
		phrase := chosen
		if len(types) > 0 {
			phrase += " " + strings.Join(types, " ")
		}
		return article(phrase) + " " + phrase + " " + noun, true
	}
	if len(types) == 0 {
		return article(noun) + " " + noun, true
	}
	phrase := strings.Join(types, " ")
	return article(phrase) + " " + phrase + " " + noun, true
}

// knownSpecWord reports whether a spec qualifier is a card type, supertype or
// colour that reads naturally as a noun modifier. Anything else fails closed
// to the raw Valid$ fallback.
func knownSpecWord(q string) bool {
	switch strings.ToLower(q) {
	case "artifact", "battle", "creature", "enchantment", "instant", "land",
		"planeswalker", "sorcery", "tribal", "kindred",
		"basic", "legendary", "snow", "world",
		"white", "blue", "black", "red", "green", "colorless":
		return true
	}
	return false
}

// article picks "a" or "an" for a noun phrase by its first word's initial.
func article(phrase string) string {
	if phrase == "" {
		return "a"
	}
	switch phrase[0] {
	case 'a', 'A', 'e', 'E', 'i', 'I', 'o', 'O', 'u', 'U':
		return "an"
	}
	return "a"
}
