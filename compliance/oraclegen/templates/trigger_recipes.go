package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// triggerCause is one candidate way of making a trigger's cause happen on
// turn 1 with ops the XMage driver already has (cast, attack, pass_to).
type triggerCause struct {
	hand        []string                  // probe cards added to p0's hand
	battlefield []string                  // extra p0 permanents (an attacker for a non-creature card)
	tapped      []string                  // extra p0 permanents that start tapped
	graveyard   []string                  // extra p0 graveyard cards
	counters    map[string]map[string]int // counters on setup permanents
	prelude     []oraclegen.Step          // steps before the actual trigger cause
	steps       []oraclegen.Step          // the cause steps emitted into the item
	probeSteps  []oraclegen.Step          // steps whose snapshots show the trigger on the stack; nil means steps
	selfInHand  bool                      // the card starts in p0's hand (a "when you discard this card" trigger), not on the battlefield
	xability    []string                  // XMage rule-text prefix per step (an activate step); nil when no step activates
	castSelfX   bool                      // the card is cast from hand with X first (an X creature that setup would leave 0/0)
}

// Probe cards, each named with why. Spec hypothesis H4: the probe exists in
// XMage's database; the host replay confirms it. Each list is tried in order
// and the first that plays through gorge and fires the trigger wins.
var (
	// Murder ({1}{B}{B}, destroy target creature) is unconditional; Doom
	// Blade and Terror are the fallbacks and refuse black / artifact cards.
	destroyProbes = []string{"Murder", "Doom Blade", "Terror"}
	// Angel's Mercy ({2}{W}{W}, gain 7 life) and Revitalize ({1}{W}, gain 3
	// and draw) gain life with no choice to answer; Healing Salve asks a mode.
	lifegainProbes = []string{"Angel's Mercy", "Revitalize", "Healing Salve"}
	// Divination ({2}{U}, draw two) is the plain draw; Concentrate and
	// Inspiration are the fallbacks.
	drawProbes = []string{"Divination", "Concentrate", "Inspiration"}
	// Shock (instant, {R}, 2 damage to any target) is the instant/sorcery
	// cast cause and Grizzly Bears ({1}{G}) the creature one; Giant Growth
	// ({G}) is the becomes-target cause. All three are level-A fixtures.
	shockProbe, bearsProbe, growthProbe = "Shock", "Grizzly Bears", "Giant Growth"
)

// castProbe builds the cast step of probe, or false when the probe is not in
// the corpus or its mana cost has no pool.
func castProbe(reg *cards.Registry, probe string, targets ...string) (oraclegen.Step, bool) {
	c, ok := reg.Lookup(probe)
	if !ok || len(c.Faces) == 0 {
		return oraclegen.Step{}, false
	}
	pool, why := oraclegen.PoolFor(c.Faces[0].ManaCost)
	if why != "" {
		return oraclegen.Step{}, false
	}
	return oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:" + probe, Mana: pool, Targets: targets}, true
}

// triggerRecipe returns the candidate causes for one trigger sub-family, or
// the reason none exists for this card.
func triggerRecipe(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger, sub string) ([]triggerCause, string) {
	var out []triggerCause
	cast := func(probe string, targets ...string) {
		if c, ok := castCause(reg, name, probe, targets...); ok {
			out = append(out, c)
		}
	}
	creature := f.IsCreature()
	switch sub {
	case "trigger.etb-other":
		return etbProbeCauses(reg, name, t)
	case "trigger.dies":
		if !creature {
			return nil, "dies needs a creature"
		}
		for _, p := range destroyProbes {
			cast(p, "p0:"+name)
		}
	case "trigger.attacks", "trigger.attacks-one-target", "trigger.combat-damage", "trigger.combat-damage-all":
		combatDamage := sub == "trigger.combat-damage" || sub == "trigger.combat-damage-all"
		attacker, extra := "p0:"+name, []string(nil)
		if !creature {
			if sub == "trigger.combat-damage" {
				return nil, "combat-damage needs a creature"
			}
			attacker, extra = "p0:"+bearsProbe, []string{bearsProbe}
		}
		attack := oraclegen.Step{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{attacker}}
		c := triggerCause{battlefield: extra, steps: []oraclegen.Step{attack}}
		if combatDamage {
			// The trigger resolves inside the pass to main2, so the emitted
			// item shows no stack; the probe stops in end-combat, where the
			// trigger is still on it.
			c.steps = append(c.steps, oraclegen.Step{Op: "pass_to", Step: "main2"})
			c.probeSteps = []oraclegen.Step{attack, {Op: "pass_to", Step: "end-combat"}}
		}
		out = append(out, c)
	case "trigger.spell-cast":
		return spellCastProbeCauses(reg, name, t), ""
	case "trigger.becomes-target":
		if !creature {
			return nil, "becomes-target needs a creature"
		}
		cast(growthProbe, "p0:"+name)
	case "trigger.life-gained":
		for _, p := range lifegainProbes {
			cast(p)
		}
	case "trigger.drawn":
		for _, p := range drawProbes {
			if c, ok := castCause(reg, name, p); ok {
				out = append(out, withDrawCheckpoint(c))
			}
		}
	case "trigger.phase":
		step, ok := phaseStep(t.ParamStr(cards.PKPhase))
		if !ok {
			return nil, "unsupported phase"
		}
		active := ""
		if step == "upkeep" || step == "draw" || step == "main1" {
			// p0's turn-1 upkeep/draw/main1 have passed. For "each player"
			// and "each opponent" phase triggers, stop at p1's first matching
			// step on turn 2; for You, wait for p0's next turn (turn 3).
			if vp := t.ParamStr(cards.PKValidPlayer); strings.EqualFold(vp, "Player") || strings.EqualFold(vp, "Opponent") {
				active = "p1"
			} else {
				active = "p0"
			}
		}
		steps := []oraclegen.Step{{Op: "pass_to", Step: step, Active: active}}
		if step == "main1" {
			// The game starts in turn 1's main1, which pass_to would stop
			// in at once: leave it first, then wait for p0's next main1.
			steps = append([]oraclegen.Step{{Op: "pass_to", Step: "main2"}}, steps...)
		}
		base := triggerCause{steps: steps}
		out = append(out, base)
		for _, condition := range conditionPreludes(reg, t.Params, f.SVars) {
			candidate := base
			candidate.hand = append(append([]string(nil), base.hand...), condition.hand...)
			candidate.battlefield = append(append([]string(nil), base.battlefield...), condition.battlefield...)
			candidate.tapped = append([]string(nil), condition.tapped...)
			candidate.graveyard = append([]string(nil), condition.graveyard...)
			candidate.counters = condition.counters
			candidate.prelude = append([]oraclegen.Step(nil), condition.steps...)
			out = append(out, candidate)
		}
	default:
		if causes, why, ok := eventTriggerRecipe(reg, f, name, t, sub); ok {
			return causes, why
		}
		return nil, "no recipe for " + sub
	}
	if len(out) == 0 {
		return nil, "probe not in corpus"
	}
	return out, ""
}

// phaseStep maps the Phase$ vocabulary admitted by levelb to ParseStep's
// scenario spelling.
func phaseStep(phase string) (string, bool) {
	switch phase {
	case "BeginCombat":
		return "begin-combat", true
	case "End of Turn":
		return "end", true
	case "Main1":
		return "main1", true
	case "Upkeep":
		return "upkeep", true
	case "Draw":
		return "draw", true
	default:
		return "", false
	}
}
