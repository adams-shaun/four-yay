package templates

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclediff"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// staticSurvivingAuraItem retries a lethal -X/-X Aura with a setup-attached
// Gigantosaurus. The larger host survives, leaving its changed P/T observable.
func staticSurvivingAuraItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	if name != "Dead Weight" && name != "Swampsnare Trap" {
		return oraclegen.Item{}, false
	}
	const host = "Gigantosaurus"
	mana, why := oraclegen.PoolFor(f.ManaCost)
	if why != "" {
		return oraclegen.Item{}, false
	}
	base, skip := castResolveWith(reg, f, name, mana, []string{host}, nil)
	if skip != nil {
		return oraclegen.Item{}, false
	}
	sc := base.Scenario
	sc.Steps = append([]oraclegen.Step(nil), base.Steps...)
	for i := range sc.Steps {
		if sc.Steps[i].Op == "cast" && sc.Steps[i].Card == "p0:"+name {
			sc.Steps[i].Targets = []string{"p0:" + host}
		}
	}
	res, err := rules.RunOracleScenarioJSON(reg, oraclegen.Item{Scenario: sc}.Raw())
	if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		return oraclegen.Item{}, false
	}
	probes := staticProbeSpecs(reg, []string{host, staticProbe})
	if !staticObserved(res.Snapshots[len(res.Snapshots)-1], f, name, st, probes) {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"611.3", "613"}, sc)
	it.Compare = []string{oraclediff.CompareKeywords}
	return it, true
}

// staticPreserveETBProbe changes only an ETB target answer for this static
// template's newly attempted candidate. Optional targets are declined; required
// targets are redirected to the opposing seat's Grizzly Bears.
func staticPreserveETBProbe(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static, base oraclegen.Item, res rules.OracleResult) (oraclegen.Item, bool) {
	if !oraclegen.HasType(f, "Equipment") || !staticAffectsAttachment(st.ParamStr(cards.PKAffected)) {
		return oraclegen.Item{}, false
	}
	var target *rules.OracleDecision
	for i := range res.Decisions {
		d := &res.Decisions[i]
		if d.Kind == "target" && d.Via == "answer" && d.Min >= 0 {
			target = d
			break
		}
	}
	if target == nil || target.Step < 0 || target.Step >= len(base.Steps) {
		return oraclegen.Item{}, false
	}
	sc := base.Scenario
	sc.Steps = append([]oraclegen.Step(nil), base.Steps...)
	answer := oraclegen.Answer{Kind: "target"}
	if target.Min > 0 {
		answer.Pick = []string{"p1:" + staticProbe}
	}
	sc.Steps[target.Step].Answers = []oraclegen.Answer{answer}
	trial := oraclegen.Item{Scenario: sc}
	checked, err := rules.RunOracleScenarioJSON(reg, trial.Raw())
	if err != nil || len(checked.Fails) != 0 || len(checked.Snapshots) == 0 {
		return oraclegen.Item{}, false
	}
	probes := staticProbeSpecs(reg, []string{staticProbe})
	if !staticObserved(checked.Snapshots[len(checked.Snapshots)-1], f, name, st, probes) {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"611.3", "613"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(checked, sc, oraclegen.ModeNumbers(f), nil)
	it.Ignore = base.Ignore
	it.XAbility = base.XAbility
	return it, true
}
