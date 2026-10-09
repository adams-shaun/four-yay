package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// The cast-provenance spell-cast triggers name a predicate the ordinary probe
// cast from p0's hand can never satisfy: a cast from exile
// (Card.wasCastFromExile, and the negated Card.!wasCastFromYourHand) or an
// Adventure spell face (Card.Adventure). The engine's trigger matcher already
// evaluates both predicates (rules/cast_provenance.go, effects/filter.go);
// only the CAUSE was missing, so the row skipped with "spell-cast unsupported
// <x> provenance" (spellCastNarrowSkip). Each cause below produces the cast
// the predicate needs, and gorge's own matcher decides whether it fires.
//
// The ownership family (Card.YouDontOwn, Spell.YouDontOwn) is deliberately
// not handled: p0 has no way to cast a p1-owned card without an
// exile-and-grant effect the fixture setup cannot build, and Gonti's
// `ValidSAonCard$ Spell.YouDontOwn` is not modelled by the engine at all
// (trigmatch.spellValidSAonCardMatches fails it closed). See the ticket's
// report Issues.
func spellCastProvenanceCauses(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) []triggerCause {
	filter := strings.ToLower(strings.ReplaceAll(
		t.ParamStr(cards.PKValidCard)+","+t.ParamStr(cards.PKValidSAonCard), " ", ""))
	prelude := setupResolvePrelude(f)
	var out []triggerCause
	if strings.Contains(filter, "wascastfromexile") || strings.Contains(filter, "!wascastfromyourhand") {
		if c, ok := spellCastFromExileCause(reg, name); ok {
			c.prelude = prelude
			out = append(out, c)
		}
	}
	if strings.Contains(filter, "adventure") && filterNamesAdventure(filter) {
		if c, ok := spellCastAdventureCause(reg, name); ok {
			c.prelude = prelude
			out = append(out, c)
		}
	}
	return out
}

// filterNamesAdventure reports whether the filter carries the `Adventure`
// provenance predicate as its own dot/plus/comma-separated token. It keeps
// the distinct `AdventureCard` predicate (the card IS an Adventure card, a
// characteristic, not a cast provenance) from being mistaken for it.
func filterNamesAdventure(filter string) bool {
	for _, seg := range strings.FieldsFunc(filter, func(r rune) bool {
		return r == '.' || r == '+' || r == ','
	}) {
		if seg == "adventure" {
			return true
		}
	}
	return false
}

// setupResolvePrelude returns the two passes that clear a trigger queued at
// the start of p0's first main phase. A generated (xmageFixture) scenario
// deliberately stops at the FIRST priority in turn 1's main1 even with a
// non-empty stack (rules/oracle_run.go's build), so a beginning-of-first-
// main-phase trigger (Shadow of the Goblin's "Unreliable Visions") sits on the
// stack at the setup checkpoint and a sorcery-speed cast cannot be offered
// until it resolves. It returns nil when the face has no such trigger, so
// every other card's item is unchanged.
func setupResolvePrelude(f *cards.Face) []oraclegen.Step {
	for i := range f.Triggers {
		t := &f.Triggers[i]
		if t.Mode != "Phase" || !strings.EqualFold(strings.TrimSpace(t.ParamStr(cards.PKPhase)), "Main1") {
			continue
		}
		if vp := strings.TrimSpace(t.ParamStr(cards.PKValidPlayer)); vp != "" &&
			!strings.EqualFold(vp, "You") && !strings.EqualFold(vp, "Player") {
			continue
		}
		return []oraclegen.Step{{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}}
	}
	return nil
}

// spellCastFromExileCause casts a probe card out of p0's exile. The probe's
// OWN Continuous static grants the cast from exile (Misthollow Griffin,
// Eternal Scourge), which the engine's may-play walk offers; the trigger's
// wasCastFromExile / !wasCastFromYourHand provenance then holds. The probe is
// a creature, so the sorcery-speed turn-1 main phase offers it.
func spellCastFromExileCause(reg *cards.Registry, name string) (triggerCause, bool) {
	for _, probe := range []string{"Misthollow Griffin", "Eternal Scourge"} {
		if probe == name {
			continue
		}
		card, ok := reg.Lookup(probe)
		if !ok || len(card.Faces) == 0 || !card.Faces[0].IsCreature() {
			continue
		}
		pool, why := oraclegen.PoolFor(card.Faces[0].ManaCost)
		if why != "" {
			continue
		}
		return triggerCause{
			exile: []string{probe},
			steps: []oraclegen.Step{{Op: "cast", Seat: 0, Card: "p0:" + probe, Mana: pool}},
		}, true
	}
	return triggerCause{}, false
}

// spellCastAdventureCause casts an Adventure spell face from p0's hand. Setup
// deals the PHYSICAL card under its front name; the cast step names the
// Adventure face, which the runner binds to the engine's "adventure_alt"
// offer (rules/oracle_run_faces.go). The trigger's Card.Adventure filter then
// matches the spell on the stack.
func spellCastAdventureCause(reg *cards.Registry, name string) (triggerCause, bool) {
	for _, parent := range []string{"Beanstalk Wurm", "Bonecrusher Giant", "Brazen Borrower", "Curious Pair"} {
		if parent == name {
			continue
		}
		card, ok := reg.Lookup(parent)
		if !ok || card.AlternateMode != "Adventure" || len(card.Faces) < 2 {
			continue
		}
		face := requestedFace(card, card.Faces[1].Name)
		if face == nil || face == card.Faces[0] {
			continue
		}
		pool, why := oraclegen.PoolFor(face.ManaCost)
		if why != "" {
			continue
		}
		return triggerCause{
			hand:  []string{parent},
			steps: []oraclegen.Step{{Op: "cast", Seat: 0, Card: "p0:" + face.Name, Mana: pool}},
		}, true
	}
	return triggerCause{}, false
}
