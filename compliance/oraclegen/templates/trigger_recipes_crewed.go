package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// The probe permanents the crew/saddle causes activate (ticket
// agent-20261009T174759Z-80a5bc9a). Each carries the printed keyword the
// cause taps with -- Crew on the Vehicle, Saddle on the Mount -- so its
// expanded ability is found by the Keyword$ param, and each is a plain
// permanent the setup can place beside the row's source. The tapped body is
// never a fixture: the cause elects the row's SOURCE, the only other
// creature on the battlefield, so the engine's Crew/Saddle marker event
// names it.
var (
	crewVehicleProbes = []string{"Smuggler's Copter"}
	saddleMountProbes = []string{"Bulwark Ox"}
)

// crewedTriggerRecipe builds the causes for the crew/saddle-perspective
// sub-families: the row's source crews a probe Vehicle (Mode$ Crewed),
// saddles a probe Mount (Mode$ Saddled), or -- Balthier and Fran's read --
// crews a probe Vehicle with the source and then attacks with the crewed
// Vehicle (trigger.attacks-crewed-vehicle). ok is false for any other
// sub-family.
func crewedTriggerRecipe(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger, sub string) ([]triggerCause, string, bool) {
	var probes []string
	switch sub {
	case levelb.CrewedSub:
		probes = crewVehicleProbes
	case levelb.SaddledSub:
		probes = saddleMountProbes
	case levelb.AttacksCrewedVehicleSub:
		probes = crewVehicleProbes
	default:
		return nil, "", false
	}
	var causes []triggerCause
	for _, probe := range probes {
		c, ok := crewActivationCause(reg, probe)
		if !ok {
			continue
		}
		if sub == levelb.AttacksCrewedVehicleSub {
			c.steps = append(c.steps, oraclegen.Step{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:" + probe}})
		}
		causes = append(causes, c)
	}
	if len(causes) == 0 {
		return nil, "probe not in corpus", true
	}
	return causes, "", true
}

// crewActivationCause activates probe's own Crew/Saddle ability on turn 1:
// the same activate-and-resolve shape attackActivationCause builds for a
// source that crews itself, with two differences the crew direction needs.
// The ability belongs to the PROBE card (its expanded Crew/Saddle keyword
// ability, so XMageAbility reads the probe's face), and no activation-cost
// fixtures are placed: the crew/saddle tap election must name the trigger's
// source, which is the only other creature on the battlefield, so a
// catalogue creature would give the deterministic election a different body.
// The observed-pick path of addTapXTypeAnswers carries gorge's exact
// election (the source) into XMage's cost answers.
func crewActivationCause(reg *cards.Registry, probe string) (triggerCause, bool) {
	c, ok := reg.Lookup(probe)
	if !ok || len(c.Faces) == 0 {
		return triggerCause{}, false
	}
	pf := c.Faces[0]
	head := "crew"
	if hasType(pf, "Mount") {
		head = "saddle"
	}
	abilityIndex := -1
	for i, sa := range pf.Abilities {
		if !sa.IsActivated() {
			continue
		}
		if strings.HasPrefix(strings.ToLower(sa.ParamStr(cards.PKKeyword)), head) {
			abilityIndex = i
			break
		}
	}
	if abilityIndex < 0 {
		return triggerCause{}, false
	}
	sa := pf.Abilities[abilityIndex]
	cost := sa.ParamStr(cards.PKCost)
	mana, gap := activationCostIn(cost, "battlefield", probe)
	if gap != "" {
		return triggerCause{}, false
	}
	prefixes, why := oraclegen.XMageAbility(pf)
	if why != "" {
		return triggerCause{}, false
	}
	prefix, ok := prefixes[abilityIndex]
	if !ok {
		return triggerCause{}, false
	}
	idx := abilityIndex
	activate := oraclegen.Step{Op: "activate", Seat: 0, Card: "p0:" + probe, Mana: mana, AbilityIndex: &idx, Answers: activationXAnswers(cost)}
	return triggerCause{
		battlefield:  []string{probe},
		steps:        []oraclegen.Step{activate, {Op: "resolve"}},
		xability:     []string{prefix},
		activateCost: cost,
	}, true
}
