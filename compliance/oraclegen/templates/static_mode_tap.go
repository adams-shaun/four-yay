// Level-B observation of the Pilot-family TapPowerValue static ("This
// creature saddles Mounts and crews Vehicles as though its power were N
// greater", Cloudspire Captain and the DFT Pilot cycle). The engine reads the
// value in rules/statics_assignment.go's tapPowerValue, so the observation is
// the CREW OFFER: a Vehicle whose crew cost needs more power than the pilot
// prints is not offered at p0's main1 priority without the static, and is
// offered with it (the tapPowerValue read is what the affordability gate
// sums). The Offered expectation names the crew ability by its label, so the
// Vehicle's own mana ability cannot stand in for it; the control replays the
// identical checkpoint with the pilot removed, where the crew option must be
// absent.
package templates

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// tapVehicleProbes are the candidate Vehicles, in preference order. Each has
// Crew and no other activated ability and no enter trigger (both engines fire
// neither on a setup placement), so its crew option is the only activate
// option it ever offers. Crew values 3 and 2 cover the pilot cycle: a pilot's
// power must fall short of the crew amount, and its TapPowerValue value must
// close the gap.
var tapVehicleProbes = []string{
	"Heart of Kiran", "Midnight Mangler", "Clamorous Ironclad", "Brotherhood Vertibird",
}

// tapPowerValueItem serves the crew-offer observation for one TapPowerValue
// requirement.
func tapPowerValueItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "TapPowerValue"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	i, err := strconv.Atoi(req.Slot)
	if err != nil || i < 0 || i >= len(f.Statics) {
		return skip("requirement slot names no static")
	}
	st := f.Statics[i]
	power, _, ok := parsePT(f.PT)
	if !ok {
		return skip("the card has no printed power")
	}
	v := strings.TrimSpace(st.ParamStr(cards.PKValue))
	// contribution is the absolute value the static makes the card contribute:
	// a numeric Value$ is the additive "as though its power were N greater"
	// read, Value$ Toughness is the absolute toughness read (Interface Ace).
	contribution := power
	if strings.EqualFold(v, "Toughness") {
		if _, tough, tok := parsePT(f.PT); tok {
			contribution = tough
		} else {
			return skip("the card has no printed toughness")
		}
	} else if value, verr := strconv.Atoi(v); verr == nil {
		contribution = power + value
	} else {
		return skip("Value$ is not an integer nor Toughness")
	}
	probe, crewN := tapVehicleProbe(reg, tapVehicleProbes, power, contribution)
	if probe == "" {
		return skip("no Vehicle probe's crew amount the value closes without overshooting")
	}
	ref := "p0:" + probe
	want := true
	steps := []oraclegen.Step{{Op: "pass_to", Seat: 0, Decision: "priority", Expect: []oraclegen.Expect{{
		Offered: &oraclegen.Offered{Seat: 0, Kind: "activate", Card: ref,
			Label: "crew " + strconv.Itoa(crewN)},
		Want: &want,
	}}}}
	sc := staticScenario(f, name, []string{name, probe}, nil, steps)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip(fmt.Sprintf("crew option not offered with the card (ok=%v fails=%v)", ok, res.Fails))
	}
	// Control: the pilot removed, the Vehicle keeps its own mana ability but
	// the crew option must disappear -- the affordability sum no longer
	// reaches the crew amount.
	control := sc
	control.Setup = map[string]oraclegen.Seat{"p0": {Battlefield: []string{probe}}, "p1": {}}
	controlRes, ok := runStatic(reg, control)
	if !ok || len(controlRes.Fails) == 0 {
		return skip(fmt.Sprintf("crew option still offered without the card (ok=%v fails=%v)", ok, controlRes.Fails))
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"702.121a"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// tapVehicleProbe picks the first probe Vehicle whose crew amount n satisfies
// power < n <= contribution (the control must fail and the observation must
// hold; contribution is the absolute value the static makes the card
// contribute -- power+Value, or its Toughness).
func tapVehicleProbe(reg *cards.Registry, names []string, power, contribution int) (probe string, crew int) {
	for _, n := range names {
		c, ok := reg.Lookup(n)
		if !ok || len(c.Faces) == 0 {
			continue
		}
		if k, why := tapVehicleCrew(c.Faces[0]); why == "" && power < k && contribution >= k {
			return n, k
		}
	}
	return "", 0
}

// tapVehicleCrew reads the crew amount of a keyword-expanded Crew ability.
func tapVehicleCrew(f *cards.Face) (int, string) {
	if len(f.Abilities) != 1 {
		return 0, "not exactly one activated ability"
	}
	if !strings.EqualFold(f.Abilities[0].ParamStr(cards.PKKeyword), "Crew") {
		return 0, "the ability is not crew"
	}
	const marker = "withTotalPowerGE"
	cost := f.Abilities[0].ParamStr(cards.PKCost)
	idx := strings.Index(cost, marker)
	if idx < 0 {
		return 0, "crew cost has no total-power floor"
	}
	digits := cost[idx+len(marker):]
	end := 0
	for end < len(digits) && digits[end] >= '0' && digits[end] <= '9' {
		end++
	}
	n, err := strconv.Atoi(digits[:end])
	if err != nil || n <= 0 {
		return 0, "crew cost floor is not a positive integer"
	}
	return n, ""
}
