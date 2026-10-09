package templates

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/state"
)

// tapSpellProbe ({1}{W} instant: tap target creature, draw a card) taps a
// creature that attacking cannot tap (a vigilant one, or an opponent's).
const tapSpellProbe = "Pressure Point"

func tapCombatSub(sub string) bool {
	switch sub {
	case "trigger.tapped", "trigger.attacks-attached", "trigger.blocks", "trigger.blocked", "trigger.opponent-attacks":
		return true
	}
	return false
}

// tapSubject is the p0 creature a tap or combat trigger is about: the card
// itself, a creature its filter accepts, or the Grizzly Bears that carries the
// card as its Equipment or Aura.
type tapSubject struct {
	name        string
	battlefield []string
	prelude     []oraclegen.Step
	cardName    string
	selfInHand  bool // the card is cast onto the subject (an Aura cannot be placed unattached)
}

// cause lays the subject's setup over c.
func (s tapSubject) cause(c triggerCause) triggerCause {
	c.battlefield = append(append([]string(nil), c.battlefield...), s.battlefield...)
	c.prelude = append(append([]oraclegen.Step(nil), c.prelude...), s.prelude...)
	if s.selfInHand {
		c.selfInHand = true
		c.hand = append(append([]string(nil), c.hand...), s.cardName)
	}
	return c
}

func tapFilterText(t *cards.Trigger) string {
	if t.ModeKind() == cards.TriggerTapAll {
		return t.ParamStr(cards.PKValidCards)
	}
	return t.ParamStr(cards.PKValidCard)
}

func hasBearerToken(filter string) bool {
	lower := strings.ToLower(filter)
	return strings.Contains(lower, "attachedby") || strings.Contains(lower, "equippedby") || strings.Contains(lower, "enchantedby")
}

func hasToken(filter, tok string) bool {
	for _, alt := range strings.FieldsFunc(filter, func(r rune) bool { return r == '.' || r == '+' || r == ',' }) {
		if strings.EqualFold(alt, tok) {
			return true
		}
	}
	return false
}

// tapSubjects lists the p0 creatures that can be the subject of filter, in
// the order they are tried. Gorge's own matcher accepts or rejects each
// corpus creature (filter probe), so the type words in the filter are never
// parsed here.
func tapSubjects(reg *cards.Registry, f *cards.Face, name, filter string) []tapSubject {
	switch {
	case hasBearerToken(filter):
		if _, ok := reg.Lookup(bearsProbe); !ok {
			return nil
		}
		subject := tapSubject{name: bearsProbe, battlefield: []string{bearsProbe}}
		if hasType(f, "Aura") {
			// An unattached Aura on the battlefield is put into the
			// graveyard before the cause runs, so it is cast on the Bears.
			cast, ok := castProbe(reg, name, "p0:"+bearsProbe)
			if !ok {
				return nil
			}
			subject.prelude = []oraclegen.Step{cast, {Op: "resolve"}}
			subject.cardName, subject.selfInHand = name, true
			return []tapSubject{subject}
		}
		subject.prelude = []oraclegen.Step{{Op: "attach", Seat: 0, Card: "p0:" + name, AttachedTo: "p0:" + bearsProbe}}
		return []tapSubject{subject}
	case hasToken(filter, "Self"):
		if !f.IsCreature() {
			return nil
		}
		return []tapSubject{{name: name}}
	}
	return typedSubjects(reg, name, filter)
}

// typedSubjects is the Bears when the filter takes them, otherwise the corpus
// creatures the filter accepts.
func typedSubjects(reg *cards.Registry, name, filter string) []tapSubject {
	candidates := []string{bearsProbe}
	if fp := newFilterProbe(filter, state.ZBattlefield); fp.decided {
		if bears, ok := reg.Lookup(bearsProbe); ok && !fp.accepts(bears) {
			candidates = fp.victimProbes(reg, name, 8)
		}
	}
	var out []tapSubject
	for _, c := range existingCards(reg, candidates) {
		// Alchemy rebalanced cards ("A-...") are not in XMage's database.
		if strings.HasPrefix(c, "A-") || len(out) == 3 {
			continue
		}
		out = append(out, tapSubject{name: c, battlefield: []string{c}})
	}
	return out
}

func p0Attack(attackers ...string) oraclegen.Step {
	return oraclegen.Step{Op: "attack", Seat: 0, Defender: "p1", Attackers: attackers}
}

var tapAmountRE = regexp.MustCompile(`(?i)^ge(\d+)$`)

// tapCombatRecipe builds the causes for the tap and combat-declaration
// sub-families: an attack, a block, an attachment or a tap spell. ok is false
// for any other sub-family.
func tapCombatRecipe(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger, sub string) (causes []triggerCause, why string, ok bool) {
	if !tapCombatSub(sub) {
		return nil, "", false
	}
	card := t.ParamStr(cards.PKValidCard)
	switch sub {
	case "trigger.tapped":
		filter := tapFilterText(t)
		if hasToken(filter, "OppCtrl") {
			// "Whenever you tap an untapped creature an opponent controls":
			// p0 casts the tap spell at p1's Bears.
			if c, yes := castCause(reg, name, tapSpellProbe, "p1:"+bearsProbe); yes {
				c.opponentBattlefield = []string{bearsProbe}
				causes = append(causes, c)
			}
			break
		}
		for _, s := range tapSubjects(reg, f, name, filter) {
			causes = append(causes, s.cause(triggerCause{steps: []oraclegen.Step{p0Attack("p0:" + s.name)}}))
			if c, yes := castCause(reg, name, tapSpellProbe, "p0:"+s.name); yes {
				causes = append(causes, s.cause(c))
			}
		}
	case "trigger.attacks-attached":
		for _, s := range tapSubjects(reg, f, name, card) {
			causes = append(causes, s.cause(triggerCause{steps: []oraclegen.Step{p0Attack("p0:" + s.name)}}))
		}
	case "trigger.blocks":
		blockerFilter := card
		attackerFilter := ""
		if t.ModeKind() != cards.TriggerBlocks {
			blockerFilter = t.ParamStr(cards.PKValidBlocker)
			attackerFilter = card
		}
		attackers := []string{bearsProbe}
		if attackerFilter != "" {
			attackers = nil
			for _, a := range typedSubjects(reg, name, attackerFilter) {
				attackers = append(attackers, a.name)
			}
		}
		for _, s := range tapSubjects(reg, f, name, blockerFilter) {
			for _, a := range attackers {
				causes = append(causes, s.cause(triggerCause{
					opponentBattlefield: []string{a},
					steps: []oraclegen.Step{
						{Op: "attack", Seat: 1, Defender: "p0", Attackers: []string{"p1:" + a}},
						{Op: "block", Seat: 0, Blocks: [][2]string{{"p0:" + s.name, "p1:" + a}}},
					},
				}))
			}
		}
	case "trigger.blocked":
		for _, s := range tapSubjects(reg, f, name, card) {
			causes = append(causes, s.cause(triggerCause{
				opponentBattlefield: []string{combatBlocker},
				steps: []oraclegen.Step{
					p0Attack("p0:" + s.name),
					{Op: "block", Seat: 1, Blocks: [][2]string{{"p1:" + combatBlocker, "p0:" + s.name}}},
				},
			}))
		}
	case "trigger.opponent-attacks":
		need := 1
		if m := tapAmountRE.FindStringSubmatch(t.ParamStr(cards.PKValidAttackersAmount)); m != nil {
			need, _ = strconv.Atoi(m[1])
		}
		// Tomik, Wielder of Law's attacker-count gate is a CheckSVar$
		// (Count$ValidAll Creature.attackingYouOrYourPWLKI) with an
		// SVarCompare$ floor: send that many attackers so the engine's
		// SVar evaluation clears the floor.
		if t.ParamStr(cards.PKCheckSVar) != "" {
			if m := tapAmountRE.FindStringSubmatch(strings.ToLower(t.ParamStr(cards.PKSVarCompare))); m != nil {
				if n, err := strconv.Atoi(m[1]); err == nil && n > need {
					need = n
				}
			}
		}
		attackers := []string{bearsProbe}
		for _, filler := range attackFillers {
			if len(attackers) >= need {
				break
			}
			attackers = appendFixtureUnique(attackers, filler)
		}
		attackers = existingCards(reg, attackers)
		steps := make([]string, len(attackers))
		for i, a := range attackers {
			steps[i] = "p1:" + a
		}
		causes = append(causes, triggerCause{
			opponentBattlefield: attackers,
			steps:               []oraclegen.Step{{Op: "attack", Seat: 1, Defender: "p0", Attackers: steps}},
		})
	}
	if len(causes) == 0 {
		return nil, "probe not in corpus or no matching subject", true
	}
	return causes, "", true
}
