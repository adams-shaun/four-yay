package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/state"
)

func castFamilySub(sub string) bool {
	switch sub {
	case "trigger.spell-cast-opponent", "trigger.spell-cast-opponent-turn", "trigger.spell-cast-self-cast", "trigger.commit-crime", "trigger.ability-activated":
		return true
	}
	return false
}

// castFamilyRecipe builds turn-1 causes for the cast-family trigger shapes.
// Gorge's own trigger matcher is the final arbiter of each probe.
func castFamilyRecipe(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger, sub string) (causes []triggerCause, why string, ok bool) {
	add := func(c triggerCause, yes bool) {
		if yes {
			causes = append(causes, c)
		}
	}
	switch sub {
	case "trigger.spell-cast-opponent":
		probes := []string{shockProbe, "Opt", "Swords to Plowshares", "Path to Exile"}
		fp := newFilterProbe(t.ParamStr(cards.PKValidCard), state.ZStack)
		probes = filterAcceptedFirst(reg, probes, fp)
		for _, probe := range probes {
			card, exists := reg.Lookup(probe)
			if !exists || len(card.Faces) == 0 || !hasType(card.Faces[0], "Instant") {
				continue
			}
			step, yes := castProbe(reg, probe)
			if !yes {
				continue
			}
			step.Seat, step.Card = 1, "p1:"+probe
			add(triggerCause{opponentHand: []string{probe}, steps: []oraclegen.Step{{Op: "pass", Seat: 0}, step}}, true)
		}
	case "trigger.spell-cast-opponent-turn":
		// ValidActivatingPlayer$ You with OpponentTurn$ True: the trigger
		// watches p0's own casts, but only during p1's turn, which a plain
		// turn-1 cast never satisfies. The cause passes to p1's main phase
		// and casts an instant there; the mana pool is the same outside-
		// effect seed every cast cause carries.
		for _, probe := range opponentTurnCastProbes {
			card, exists := reg.Lookup(probe.name)
			if !exists || len(card.Faces) == 0 || !hasType(card.Faces[0], "Instant") {
				continue
			}
			st, ok := castProbe(reg, probe.name)
			if !ok {
				continue
			}
			if probe.target != "" {
				st.Targets = []string{probe.target}
			}
			add(triggerCause{
				hand: []string{probe.name},
				// p1 holds priority first in its own main phase; one pass
				// hands it to p0, where the cast step needs it.
				steps: []oraclegen.Step{{Op: "pass_to", Step: "main1", Active: "p1"}, {Op: "pass", Seat: 1}, st},
			}, true)
		}
	case "trigger.spell-cast-self-cast":
		step, yes := castProbe(reg, name)
		if yes {
			step.Mana, why = oraclegen.PoolFor(f.ManaCost)
			if why != "" {
				return nil, why, true
			}
			base := triggerCause{selfInHand: true, hand: []string{name}, steps: []oraclegen.Step{step}}
			causes = append(causes, base)
			for _, prelude := range conditionPreludes(reg, t.Params, f.SVars) {
				causes = append(causes, applyPrelude(base, prelude))
			}
		}
	case "trigger.commit-crime":
		add(castCause(reg, name, shockProbe, "p1"))
	case "trigger.ability-activated":
		probe := "Prodigal Sorcerer"
		if strings.Contains(strings.ToLower(t.ParamStr(cards.PKValidSA)), "exhaust") {
			probe = "Rangers' Refueler"
		}
		add(activatedCause(reg, name, probe, t))
	default:
		return nil, "", false
	}
	if len(causes) == 0 {
		return nil, "probe not in corpus or no matching ability", true
	}
	return causes, "", true
}

func hasType(f *cards.Face, typ string) bool {
	for _, printed := range f.Types {
		if strings.EqualFold(printed, typ) {
			return true
		}
	}
	return false
}

// opponentTurnCastProbes are the instants p0 casts during p1's turn for an
// OpponentTurn$ True spell-cast trigger; the burn probes need the opponent as
// their target.
var opponentTurnCastProbes = []struct{ name, target string }{
	{"Shock", "p1"}, {"Lightning Bolt", "p1"}, {"Divination", ""}, {"Opt", ""},
}

// activatedCause activates the first probe ability matching ValidSA. The
// generated IR ability index and XMage prefix are carried by the activate step.

func activatedCause(reg *cards.Registry, name, probe string, t *cards.Trigger) (triggerCause, bool) {
	card, ok := reg.Lookup(probe)
	if !ok || len(card.Faces) == 0 {
		return triggerCause{}, false
	}
	pf := card.Faces[0]
	prefixes, why := oraclegen.XMageAbility(pf)
	if why != "" {
		return triggerCause{}, false
	}
	wanted := strings.ToLower(t.ParamStr(cards.PKValidSA))
	for i, sa := range pf.Abilities {
		if !sa.IsActivated() {
			continue
		}
		exhaust := strings.EqualFold(sa.ParamStr(cards.PKExhaust), "True")
		targeted := sa.ParamStr(cards.PKValidTgts) != ""
		if strings.Contains(wanted, "exhaust") && !exhaust || !strings.Contains(wanted, "exhaust") && !targeted {
			continue
		}
		cost := sa.ParamStr(cards.PKCost)
		mana, gap := activationCostIn(cost, "battlefield", "")
		if gap != "" {
			continue
		}
		prefix, exists := prefixes[i]
		if !exists {
			continue
		}
		idx := i
		step := oraclegen.Step{Op: "activate", Seat: 0, Card: "p0:" + probe, Mana: mana, AbilityIndex: &idx}
		if targeted {
			step.Targets = []string{"p1"}
		}
		setup := oraclegen.Seat{}
		addActivationCostFixtures(&setup, name, cost, activationX(cost))
		return triggerCause{battlefield: append([]string{probe}, setup.Battlefield...), steps: []oraclegen.Step{step}, xability: []string{prefix}, activateCost: cost}, true
	}
	return triggerCause{}, false
}
