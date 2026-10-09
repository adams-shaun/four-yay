package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// Probe cards for the event recipes, each tried in order like the lists in
// trigger_recipes.go (spec hypothesis H4: a gorge PlaysThrough test picks the
// winner and the host replay confirms it exists in XMage).
var (
	// Opt ({U}, scry 1, draw a card) and Preordain ({U}, scry 2, draw a
	// card) scry with one top-or-bottom answer; Consider ({U}, surveil 1,
	// draw a card) is the surveil probe.
	scryProbes    = []string{"Opt", "Preordain"}
	surveilProbes = []string{"Consider"}
	// Mind Rot ({2}{B}, target player discards two cards) aimed at p0 makes
	// p0 discard the whole hand it is left holding.
	discardProbes = []string{"Mind Rot"}
	// Planeswalkers whose first loyalty ability is a plain plus ability with
	// no target: Ajani Goldmane (+1: gain 2 life), Jace Beleren (+2: each
	// player draws a card).
	loyaltyProbes = []string{"Ajani Goldmane", "Jace Beleren"}
)

// castCause is the cast of one probe as a trigger cause: the probe is in p0's
// hand and its cast step carries targets.
func castCause(reg *cards.Registry, name, probe string, targets ...string) (triggerCause, bool) {
	if probe == name {
		return triggerCause{}, false
	}
	st, ok := castProbe(reg, probe, targets...)
	if !ok {
		return triggerCause{}, false
	}
	return triggerCause{hand: []string{probe}, steps: []oraclegen.Step{st}}, true
}

// eventTriggerRecipe returns the causes for the sub-families that need more
// than one cast or attack: a dying creature, scry/surveil, damage, a loyalty
// activation and a discard. ok is false for any other sub-family, which
// triggerRecipe then treats as it always has.
func eventTriggerRecipe(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger, sub string) (causes []triggerCause, why string, ok bool) {
	add := func(c triggerCause, ok bool) {
		if ok {
			causes = append(causes, c)
		}
	}
	switch sub {
	case classLevelGainedSub:
		// "When this Class becomes level N" (CR 716.2e): the cause is the
		// level-up activator that sets the designation to N. A level >2
		// trigger first needs the prelude to N-1, then the level-N
		// activation; the trigger is put on the stack when that activation
		// resolves, which the probe's pass retry reveals.
		band := classBandLevel(t)
		if band < 2 {
			return nil, "class-level-gained without a band", true
		}
		idx := classLevelUpIndex(f, band)
		if idx < 0 {
			return nil, "class-level-gained has no level-up ability", true
		}
		prefixes, why := oraclegen.XMageAbility(f)
		if why != "" {
			return nil, "class-level-gained xmage text ambiguous", true
		}
		prefix, okp := prefixes[idx]
		if !okp {
			return nil, "class-level-gained xmage text ambiguous", true
		}
		pool, gap := activationCostIn(f.Abilities[idx].ParamStr(cards.PKCost), "battlefield", "")
		if gap != "" {
			return nil, "class-level-gained cost gap: " + gap, true
		}
		var pre []oraclegen.Step
		var preXab []string
		if band > 2 {
			var ok bool
			pre, preXab, ok = classLevelPrelude(f, name, band-1)
			if !ok {
				return nil, "class-level-gained prelude unsupported", true
			}
		}
		i := idx
		activate := oraclegen.Step{
			Op: "activate", Seat: 0, Card: "p0:" + name, Mana: pool,
			AbilityIndex: &i, Answers: activationXAnswers(f.Abilities[idx].ParamStr(cards.PKCost)),
		}
		return []triggerCause{{
			prelude: pre, preludeXAbility: preXab,
			steps:    []oraclegen.Step{activate},
			xability: []string{prefix},
		}}, "", true
	case "trigger.dies-other":
		var why string
		if causes, why = diesOtherRecipe(reg, f, name, t); why != "" {
			return nil, why, true
		}
	case "trigger.scry", "trigger.surveil":
		probes := scryProbes
		if sub == "trigger.surveil" {
			probes = surveilProbes
		}
		for _, p := range probes {
			c, ok := castCause(reg, name, p)
			// The scry/surveil choice is asked mid-resolution, so the trigger
			// is on the stack only once that choice is answered: pass_to the
			// next priority decision answers it (and is a no-op otherwise).
			c.probeSteps = append(append([]oraclegen.Step(nil), c.steps...),
				oraclegen.Step{Op: "pass", Seat: 0}, oraclegen.Step{Op: "pass", Seat: 1},
				oraclegen.Step{Op: "pass_to", Decision: "priority"})
			add(c, ok)
		}
	case "trigger.noncombat-damage":
		// Shock at p1 for "an opponent is dealt noncombat damage", at the card
		// itself for "this creature is dealt damage".
		target := "p1"
		if strings.Contains(t.ParamStr(cards.PKValidTarget), "Self") {
			if !f.IsCreature() {
				return nil, "damaged-self needs a creature", true
			}
			target = "p0:" + name
		}
		add(castCause(reg, name, shockProbe, target))
	case "trigger.loyalty-activated":
		for _, p := range loyaltyProbes {
			add(loyaltyCause(reg, name, p))
		}
	case "trigger.discarded":
		// "When you discard this card" has the card in p0's hand, where the
		// discard probe finds it as the only card left; a player-wide trigger
		// sits on the battlefield and the probe discards a Grizzly Bears.
		self := strings.Contains(t.ParamStr(cards.PKValidCard), "Self")
		if !self && name == bearsProbe {
			return nil, "discard filler is the card", true
		}
		for _, p := range discardProbes {
			c, ok := castCause(reg, name, p, "p0")
			if self {
				c.selfInHand = true
				c.hand = append(c.hand, name)
			} else {
				c.hand = append(c.hand, bearsProbe)
			}
			add(c, ok)
		}
	case levelb.CounterAddedSub:
		return counterAddedRecipe(reg, name, t)
	case levelb.TurnedFaceUpSub, levelb.TurnedFaceUpOtherSub:
		return turnedFaceUpRecipe(reg, f, name, t, sub)
	case levelb.SacrificeSub:
		return sacrificeTriggerRecipe(reg, f, name, t)
	default:
		return nil, "", false
	}
	if len(causes) == 0 {
		return nil, "probe not in corpus", true
	}
	return causes, "", true
}

// loyaltyCause activates the first plus ability of a probe planeswalker on
// p0's battlefield. The activate step names the ability by IR index and
// carries XMage's rule-text prefix (H1).
func loyaltyCause(reg *cards.Registry, name, probe string) (triggerCause, bool) {
	if probe == name {
		return triggerCause{}, false
	}
	c, ok := reg.Lookup(probe)
	if !ok || len(c.Faces) == 0 {
		return triggerCause{}, false
	}
	pf := c.Faces[0]
	prefixes, why := oraclegen.XMageAbility(pf)
	if why != "" {
		return triggerCause{}, false
	}
	for i, sa := range pf.Abilities {
		if !sa.IsActivated() || !strings.HasPrefix(sa.ParamStr(cards.PKCost), "AddCounter<") || sa.ParamStr(cards.PKValidTgts) != "" {
			continue
		}
		prefix, ok := prefixes[i]
		if !ok {
			return triggerCause{}, false
		}
		idx := i
		return triggerCause{
			battlefield: []string{probe},
			steps:       []oraclegen.Step{{Op: "activate", Seat: 0, Card: "p0:" + probe, AbilityIndex: &idx}},
			xability:    []string{prefix},
		}, true
	}
	return triggerCause{}, false
}
