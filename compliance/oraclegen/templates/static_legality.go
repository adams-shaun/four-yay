package templates

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

const defenderProbe = "Wall of Omens"
const smallAttackerProbe = "Savannah Lions"
const blockerProbe = "Grizzly Bears"
const activatedProbe = "Mogg Fanatic"
const largeAttackerProbe = "Giant Spider"

func canAttackDefenderItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	// Surveillance's SVar is false until its surveil ability resolves. Exercise
	// that condition before asking whether it is offered as an attacker.
	if name == "Surveillance Phantasm" {
		idx := 0
		steps := []oraclegen.Step{{Op: "activate", Seat: 0, Card: "p0:" + name, Mana: "UUUU", AbilityIndex: &idx}, {Op: "resolve"}, {Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:" + name}}}
		sc := staticScenario(f, name, []string{name, "Island", "Island", "Island", "Island"}, nil, steps)
		res, ok := runStatic(reg, sc)
		if !ok || len(res.Fails) != 0 {
			return staticSkip(name, "CanAttackDefender", fmt.Sprintf("surveil-enabled attack did not replay: %v", res.Fails))
		}
		// Same card, same attack, minus the surveil activation: the SVar is
		// false so the defender must not be offered as an attacker.
		control := staticScenario(f, name, []string{name, "Island", "Island", "Island", "Island"}, nil, steps[2:])
		controlRes, controlOK := runStatic(reg, control)
		if !controlOK || len(controlRes.Fails) == 0 || (!strings.Contains(strings.Join(controlRes.Fails, " "), "attack: p0:"+name) && !strings.Contains(strings.Join(controlRes.Fails, " "), "passed declare-attackers without being asked")) {
			return staticSkip(name, "CanAttackDefender", "no-surveil control did not reject the attack")
		}
		it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"702.3"}, sc)
		it.Compare = []string{"offered"}
		it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
		return it, nil
	}
	// A defender is the control: it cannot attack without the source static.
	steps := []oraclegen.Step{{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:" + defenderProbe}}}
	control := staticScenario(f, name, []string{defenderProbe}, nil, steps)
	controlRes, ok := runStatic(reg, control)
	if !ok || len(controlRes.Fails) == 0 || (!strings.Contains(strings.Join(controlRes.Fails, " "), "attack: p0:"+defenderProbe) && !strings.Contains(strings.Join(controlRes.Fails, " "), "passed declare-attackers without being asked")) {
		return staticSkip(name, "CanAttackDefender", fmt.Sprintf("control unexpectedly permits defender attack: %v", controlRes.Fails))
	}
	sc := staticScenario(f, name, []string{name, defenderProbe}, nil, steps)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return staticSkip(name, "CanAttackDefender", "defender attack not offered with source")
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"702.3"}, sc)
	it.Compare = []string{"offered"}
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

func cantBlockByItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	ref := "p0:" + smallAttackerProbe
	steps := []oraclegen.Step{
		{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{ref, "p0:" + largeAttackerProbe}},
		{Op: "pass_to", Seat: 0, Decision: "blockers"},
	}
	want := false
	expect := []oraclegen.Expect{{CanBlock: &oraclegen.CanBlock{Blocker: "p1:" + blockerProbe, Attacker: ref}, Want: &want}}
	steps[len(steps)-1].Expect = expect
	control := staticScenario(f, name, []string{smallAttackerProbe, largeAttackerProbe}, nil, steps)
	controlSeat := control.Setup["p1"]
	controlSeat.Battlefield = []string{blockerProbe}
	control.Setup["p1"] = controlSeat
	control.Steps[len(control.Steps)-1].Expect[0].Want = boolPtr(true)
	if res, ok := runStatic(reg, control); !ok || len(res.Fails) != 0 {
		return staticSkip(name, "CantBlockBy", fmt.Sprintf("control does not offer the block: %v", res.Fails))
	}
	steps[len(steps)-1].Expect[0].Want = &want
	sc := staticScenario(f, name, []string{name, smallAttackerProbe, largeAttackerProbe}, nil, steps)
	p1 := sc.Setup["p1"]
	p1.Battlefield = []string{blockerProbe}
	sc.Setup["p1"] = p1
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return staticSkip(name, "CantBlockBy", fmt.Sprintf("block remains offered with source (ok=%v fails=%v)", ok, res.Fails))
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"509.1"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

func cantBeActivatedItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	// Yuriko's static applies to all non-mana abilities. Mogg Fanatic gives
	// the checkpoint an observable, zero-mana activated ability.
	steps := []oraclegen.Step{{Op: "pass_to", Seat: 0, Step: "begin-combat"}}
	want := false
	steps[0].Expect = []oraclegen.Expect{{Offered: &oraclegen.Offered{Seat: 0, Kind: "activate", Card: "p0:" + activatedProbe}, Want: &want}}
	sc := staticScenario(f, name, []string{name, activatedProbe}, nil, steps)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return staticSkip(name, "CantBeActivated", "non-mana ability remains offered in combat")
	}
	if why := sourceRemovedControlFails(reg, sc, name); why != "" {
		return staticSkip(name, "CantBeActivated", "begin-combat control does not offer ability: "+why)
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"602.2"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

func staticCastOffer(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, combat bool) (oraclegen.Item, *oraclegen.Skip) {
	var grave []string
	want := true
	if name == "Proft, Sinister Mastermind" {
		want = false
	}
	spell := name
	if combat {
		// Floating mana empties between steps and lands' mana is only offered
		// as separate abilities, so a paid spell is never offered at
		// begin-combat. Gut Shot's Phyrexian {R/P} is payable with life.
		spell = "Gut Shot"
	}
	seat := oraclegen.Seat{Hand: []string{spell}, Graveyard: grave}
	if combat {
		seat.Battlefield = append(seat.Battlefield, name)
	}
	land := "Plains"
	if name == "Proft, Sinister Mastermind" {
		land = "Swamp"
	}
	for i := 0; i < 5; i++ {
		seat.Battlefield = append(seat.Battlefield, land)
	}
	stepName := "main1"
	if combat {
		stepName, want = "begin-combat", false
	}
	pool := "BBB"
	step := oraclegen.Step{Op: "pass_to", Seat: 0, Step: stepName, Expect: []oraclegen.Expect{{Offered: &oraclegen.Offered{Seat: 0, Kind: "cast", Card: "p0:" + spell}, Want: &want}}}
	if !combat {
		step.Decision = "priority"
	}
	sc := oraclegen.Scenario{Name: name, CR: []string{"601.2"}, Why: "generated level-B scenario", Setup: map[string]oraclegen.Seat{"p0": seat, "p1": {}}, Steps: []oraclegen.Step{step}}
	if !combat {
		sc.Steps = []oraclegen.Step{{Op: "mana", Seat: 0, Mana: pool}, step}
	}
	oraclegen.Baseline(sc.Setup, f)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return staticSkip(name, "CantBeCast", fmt.Sprintf("cast offer assertion does not hold (ok=%v fails=%v snaps=%+v)", ok, res.Fails, res.Snapshots))
	}
	if combat {
		if why := sourceRemovedControlFails(reg, sc, name); why != "" {
			return staticSkip(name, "CantBeCast", "begin-combat control does not offer cast: "+why)
		}
	}
	if name == "Proft, Sinister Mastermind" {
		controlSeat := seat
		controlSeat.Graveyard = []string{"Plains", "Mountain", "Island", "Forest", "Swamp", "Shock", "Grizzly Bears"}
		control := oraclegen.Scenario{Name: name + " control", CR: []string{"601.2"}, Why: "control", Setup: map[string]oraclegen.Seat{"p0": controlSeat, "p1": {}}, Steps: []oraclegen.Step{{Op: "mana", Seat: 0, Mana: "BBB"}, {Op: "pass_to", Seat: 0, Decision: "priority", Expect: []oraclegen.Expect{{Offered: &oraclegen.Offered{Seat: 0, Kind: "cast", Card: "p0:" + name}, Want: boolPtr(true)}}}}}
		oraclegen.Baseline(control.Setup, f)
		if controlRes, ok := runStatic(reg, control); !ok || len(controlRes.Fails) != 0 {
			return staticSkip(name, "CantBeCast", fmt.Sprintf("seven-card graveyard control does not offer cast: %v snaps=%+v", controlRes.Fails, controlRes.Snapshots))
		}
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"601.2"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// sourceRemovedControlFails replays sc at the IDENTICAL checkpoint with the
// static's source gone from p0's battlefield and the final offered assertion
// flipped to want=true. It returns "" when the option is offered there, so
// the observation's want=false can only be the static's doing.
func sourceRemovedControlFails(reg *cards.Registry, sc oraclegen.Scenario, source string) string {
	control := sc
	control.Setup = make(map[string]oraclegen.Seat, len(sc.Setup))
	for k, v := range sc.Setup {
		control.Setup[k] = v
	}
	p0 := control.Setup["p0"]
	p0.Battlefield = nil
	removed := false
	for _, perm := range sc.Setup["p0"].Battlefield {
		if perm == source && !removed {
			removed = true
			continue
		}
		p0.Battlefield = append(p0.Battlefield, perm)
	}
	if !removed {
		return "source " + source + " not on the battlefield"
	}
	control.Setup["p0"] = p0
	control.Steps = append([]oraclegen.Step(nil), sc.Steps...)
	last := &control.Steps[len(control.Steps)-1]
	last.Expect = append([]oraclegen.Expect(nil), last.Expect...)
	if len(last.Expect) != 1 || last.Expect[0].Offered == nil {
		return "last step has no single offered assertion"
	}
	last.Expect[0].Want = boolPtr(true)
	res, ok := runStatic(reg, control)
	if !ok {
		return "control did not run"
	}
	if len(res.Fails) != 0 {
		return fmt.Sprintf("%v", res.Fails)
	}
	return ""
}

func staticSkip(name, mode, why string) (oraclegen.Item, *oraclegen.Skip) {
	return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
}

func boolPtr(v bool) *bool { return &v }
