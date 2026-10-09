package templates

import (
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/state"
)

// Damage-trigger recipes. A damage trigger fires on a Damage event, so its
// cause is either an attack by a source creature the ValidSource$ filter
// accepts (combat damage to a player) or a Shock probe aimed at the
// ValidTarget$ recipient (noncombat damage to the card itself, the creature it
// is attached to, another creature, or a player). The recipe emits every
// candidate it can place and the generator keeps the first whose damage gorge's
// own trigger matcher accepts, exactly as the other event recipes do.

// damageSubject is a candidate damage source: the card itself, the creature it
// is attached to (an Equipment's or Aura's bearer), or a corpus creature the
// ValidSource$ filter accepts.
type damageSubject struct {
	name        string
	battlefield []string
	prelude     []oraclegen.Step
	cardName    string
	selfInHand  bool
	counter     bool // the cause places a +1/+1 counter on the subject
}

// combatCause builds the attack that makes the subject deal combat damage to
// p1. The trigger is put on the stack in the combat-damage step, so the probe
// stops in end-combat (as the combat-damage recipe's does) and the emitted item
// passes to main2.
func (s damageSubject) combatCause() triggerCause {
	attack := oraclegen.Step{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:" + s.name}}
	c := triggerCause{
		battlefield: append([]string(nil), s.battlefield...),
		prelude:     append([]oraclegen.Step(nil), s.prelude...),
		steps:       []oraclegen.Step{attack, {Op: "pass_to", Step: "main2"}},
		probeSteps:  []oraclegen.Step{attack, {Op: "pass_to", Step: "end-combat"}},
	}
	if s.selfInHand {
		c.selfInHand = true
		c.hand = append(c.hand, s.cardName)
	}
	if s.counter {
		c.counters = map[string]map[string]int{s.name: {"P1P1": 1}}
	}
	return c
}

// damageRecipient is a Shock target: a card ref ("p0:Name" or "p1") plus the
// setup that puts the card's Aura/Equipment on its bearer first.
type damageRecipient struct {
	target        string
	prelude       []oraclegen.Step
	cardName      string
	selfInHand    bool
	battlefield   []string
	opponentField []string
}

// shockCause casts Shock at the recipient, with the bearer setup in front.
func (r damageRecipient) shockCause(reg *cards.Registry, name string) (triggerCause, bool) {
	st, ok := castProbe(reg, shockProbe, r.target)
	if !ok {
		return triggerCause{}, false
	}
	c := triggerCause{
		hand:                []string{shockProbe},
		steps:               []oraclegen.Step{st},
		prelude:             append([]oraclegen.Step(nil), r.prelude...),
		battlefield:         append([]string(nil), r.battlefield...),
		opponentBattlefield: append([]string(nil), r.opponentField...),
	}
	if r.selfInHand {
		c.selfInHand = true
		c.hand = append(c.hand, r.cardName)
	}
	return c, true
}

// damageTriggerRecipe builds the causes of the trigger.damage sub-family. ok is
// true for every damage trigger the classifier routed here; a shape with no
// placeable source or recipient returns a named skip.
func damageTriggerRecipe(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) ([]triggerCause, string, bool) {
	src := t.ParamStr(cards.PKValidSource)
	target := t.ParamStr(cards.PKValidTarget)

	var causes []triggerCause
	// A source creature's combat damage to a player, or (with no recipient) to
	// anywhere. A trigger that demands noncombat damage is not caused by this
	// and the fire check rejects it, so the Shock candidate below still wins.
	if targetNamesPlayer(target) || strings.TrimSpace(target) == "" {
		for _, s := range damageSourceSubjects(reg, f, name, src) {
			causes = append(causes, s.combatCause())
		}
	}
	// Noncombat damage to the recipient.
	for _, r := range damageRecipients(reg, f, name, target) {
		if c, ok := r.shockCause(reg, name); ok {
			causes = append(causes, c)
		}
	}
	if len(causes) == 0 {
		return nil, "no damage source or recipient the recipe can place", true
	}
	return causes, "", true
}

// damageSourceSubjects lists the creatures that can deal the trigger's damage:
// the card itself (Card.Self), the Grizzly Bears that carries the card as an
// Equipment or Aura, or a corpus creature the ValidSource$ filter accepts.
func damageSourceSubjects(reg *cards.Registry, f *cards.Face, name, filter string) []damageSubject {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return nil
	}
	if namesSelfFold(filter) {
		if !f.IsCreature() {
			return nil
		}
		return []damageSubject{{name: name}}
	}
	if hasBearerToken(filter) {
		if s, ok := bearerDamageSubject(reg, f, name); ok {
			return []damageSubject{s}
		}
		return nil
	}
	counter := filterWantsCounter(filter)
	candidates := []string{bearsProbe}
	fp := newFilterProbe(filter, state.ZBattlefield)
	if fp.decided {
		fp.p1p1 = counter
		if bears, ok := reg.Lookup(bearsProbe); !ok || !fp.accepts(bears) {
			candidates = fp.victimProbes(reg, name, 6)
		}
	} else if kw := keywordQualifier(filter); kw != "" {
		candidates = keywordCreatures(reg, kw, name)
	}
	var out []damageSubject
	for _, c := range existingCards(reg, candidates) {
		if c == name || strings.HasPrefix(c, "A-") {
			continue
		}
		out = append(out, damageSubject{name: c, battlefield: []string{c}, counter: counter})
		if len(out) == 3 {
			break
		}
	}
	return out
}

// bearerDamageSubject is the Grizzly Bears that carries the card: an
// Equipment is attached by a prelude step, an Aura (which setup cannot place
// unattached) is cast on the Bears.
func bearerDamageSubject(reg *cards.Registry, f *cards.Face, name string) (damageSubject, bool) {
	if _, ok := reg.Lookup(bearsProbe); !ok {
		return damageSubject{}, false
	}
	s := damageSubject{name: bearsProbe, battlefield: []string{bearsProbe}}
	if oraclegen.HasType(f, "Aura") {
		cast, ok := castProbe(reg, name, "p0:"+bearsProbe)
		if !ok {
			return damageSubject{}, false
		}
		s.prelude = []oraclegen.Step{cast, {Op: "resolve"}}
		s.cardName, s.selfInHand = name, true
		return s, true
	}
	s.prelude = []oraclegen.Step{{Op: "attach", Seat: 0, Card: "p0:" + name, AttachedTo: "p0:" + bearsProbe}}
	return s, true
}

// damageRecipients lists the Shock targets the ValidTarget$ filter names: the
// card itself, a player, the creature the card enchants, or a corpus creature
// the filter accepts.
func damageRecipients(reg *cards.Registry, f *cards.Face, name, target string) []damageRecipient {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil
	}
	if namesSelfFold(target) {
		if !f.IsCreature() {
			return nil
		}
		return []damageRecipient{{target: "p0:" + name}}
	}
	if hasBearerToken(target) && targetNamesPlayer(target) {
		// An Aura enchanting a player: cast it on p1, then Shock p1.
		st, ok := castProbe(reg, name, "p1")
		if !ok {
			return nil
		}
		return []damageRecipient{{target: "p1", prelude: []oraclegen.Step{st, {Op: "resolve"}}, cardName: name, selfInHand: true}}
	}
	if hasBearerToken(target) {
		s, ok := bearerDamageSubject(reg, f, name)
		if !ok {
			return nil
		}
		return []damageRecipient{{
			target: "p0:" + bearsProbe, prelude: s.prelude,
			cardName: name, selfInHand: s.selfInHand, battlefield: s.battlefield,
		}}
	}
	if targetNamesPlayer(target) {
		return []damageRecipient{{target: "p1"}}
	}
	candidates := []string{bearsProbe}
	fp := newFilterProbe(target, state.ZBattlefield)
	if fp.decided {
		if bears, ok := reg.Lookup(bearsProbe); !ok || !fp.accepts(bears) {
			candidates = fp.victimProbes(reg, name, 6)
		}
	} else if kw := keywordQualifier(target); kw != "" {
		candidates = keywordCreatures(reg, kw, name)
	}
	var out []damageRecipient
	for _, c := range existingCards(reg, candidates) {
		if c == name || strings.HasPrefix(c, "A-") {
			continue
		}
		out = append(out, damageRecipient{target: "p0:" + c, battlefield: []string{c}})
		if len(out) == 3 {
			break
		}
	}
	return out
}

// targetNamesPlayer reports a recipient filter that names a player.
func targetNamesPlayer(target string) bool {
	return filterHasTokenFold(target, "player") || filterHasTokenFold(target, "opponent") || filterHasTokenFold(target, "you")
}

// filterWantsCounter reports a source or recipient filter a +1/+1 counter
// satisfies: HasCounters, modified, powerGTbasePower and the
// counters_GE1_P1P1 form.
func filterWantsCounter(filter string) bool {
	for _, tok := range strings.FieldsFunc(strings.ToLower(filter), func(r rune) bool { return r == '.' || r == '+' || r == ',' }) {
		switch strings.TrimSpace(tok) {
		case "hascounters", "modified", "powergtbasepower", "powernotbasepower":
			return true
		}
		if strings.HasPrefix(strings.TrimSpace(tok), "counters_ge1") {
			return true
		}
	}
	return false
}

// keywordQualifier returns the keyword a "with<Keyword>" qualifier names
// (withDeathtouch -> Deathtouch, withFlying -> Flying), or "" for a filter with
// none. The matcher's predicate census does not recognise the form, so the
// probe would otherwise accept every creature and the trigger never fire.
func keywordQualifier(filter string) string {
	for _, tok := range strings.FieldsFunc(filter, func(r rune) bool { return r == '.' || r == '+' || r == ',' }) {
		tok = strings.TrimSpace(tok)
		if len(tok) > 4 && strings.EqualFold(tok[:4], "with") {
			return tok[4:]
		}
	}
	return ""
}

// keywordCreatures lists, in sorted name order, the vanilla creatures carrying
// keyword that a combat-damage cause can attack with (no abilities to disturb
// the trigger, a fixed toughness so setup does not kill them).
func keywordCreatures(reg *cards.Registry, keyword, skip string) []string {
	names := make([]string, 0, reg.Len())
	for _, c := range reg.AllCards() {
		if len(c.Faces) != 0 {
			names = append(names, c.Faces[0].Name)
		}
	}
	sort.Strings(names)
	var out []string
	for _, n := range names {
		if n == skip || !oraclegen.XMageKnown(n) {
			continue
		}
		card, ok := reg.Lookup(n)
		if !ok || len(card.Faces) != 1 {
			continue
		}
		f := card.Faces[0]
		if !f.IsCreature() || f.IsLand() || f.Name != n || len(f.Abilities) != 0 || len(f.Triggers) != 0 || len(f.Statics) != 0 || len(f.Repls) != 0 || f.CharacteristicDefining() || f.Toughness() < 1 {
			continue
		}
		if _, ok := f.KeywordParam(keyword); !ok {
			continue
		}
		out = append(out, n)
		if len(out) == 4 {
			break
		}
	}
	return out
}
