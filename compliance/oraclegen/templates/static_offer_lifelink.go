package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

const (
	offerKwLifelink = "Lifelink"
	offerZoneStack  = "Stack"
)

// staticSpellLifelinkItem serves "<instants and sorceries> you control have
// lifelink" (AddKeyword$ Lifelink, AffectedZone$ Stack). A spell on the stack
// is no permanent, so no snapshot shows its keyword; the grant shows in what
// the spell does: a Shock dealing damage gains its caster life. The scenario
// casts the source, then the damage spell; the control casts the same
// spell without the source. It is served only when the caster gains life with
// the source and not without.
func staticSpellLifelinkItem(reg *cards.Registry, c *cards.Card, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	if !strings.EqualFold(st.ParamStr(cards.PKAddKeyword), offerKwLifelink) || !strings.EqualFold(st.ParamStr(cards.PKAffectedZone), offerZoneStack) {
		return oraclegen.Item{}, false
	}
	affected := st.ParamStr(cards.PKAffected)
	// The damage spell the grant covers: a Shock at the opponent for an
	// instant or sorcery filter, a Firebending Lesson at the opponent's
	// creature for a Lesson filter (its damage still gains life).
	spell, target := "Shock", "p1"
	switch {
	case strings.Contains(affected, "Instant") || strings.Contains(affected, "Sorcery"):
	case strings.Contains(affected, "Lesson"):
		spell, target = "Firebending Lesson", "p1:"+staticProbe
	default:
		return oraclegen.Item{}, false
	}
	if _, ok := reg.Lookup(spell); !ok {
		return oraclegen.Item{}, false
	}
	base, why := staticBase(reg, c, f, name, req, staticProbePlan{}, nil)
	if why != "" {
		return oraclegen.Item{}, false
	}
	shock := []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + spell, Mana: "R", Targets: []string{target}},
		{Op: "resolve"},
	}
	with := base.Scenario
	with.Setup = withSpellInHand(with.Setup, spell, "")
	with.Steps = append(append([]oraclegen.Step(nil), base.Steps...), shock...)
	without := base.Scenario
	without.Setup = withSpellInHand(without.Setup, spell, name)
	without.Steps = shock
	res, ok := runStatic(reg, with)
	if !ok || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		return oraclegen.Item{}, false
	}
	cres, ok := runStatic(reg, without)
	if !ok || len(cres.Fails) != 0 || len(cres.Snapshots) == 0 {
		return oraclegen.Item{}, false
	}
	if lifeOf(res, 0) <= lifeOf(cres, 0) || lifeOf(res, 1) != lifeOf(cres, 1) {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"611.3", "702.15"}, with)
	it.XAnswers = oraclegen.XAnswersForScenario(res, with, oraclegen.ModeNumbers(f), nil)
	return it, true
}

// withSpellInHand is setup with spell added to p0's hand and drop (when
// named) taken out of it: the control has no source card to cast.
func withSpellInHand(setup map[string]oraclegen.Seat, spell, drop string) map[string]oraclegen.Seat {
	out := make(map[string]oraclegen.Seat, len(setup))
	for k, v := range setup {
		out[k] = v
	}
	p0 := out["p0"]
	hand := append([]string(nil), p0.Hand...)
	if drop != "" {
		hand = removeFixtureOnce(hand, drop)
	}
	p0.Hand = appendFixtureUnique(hand, spell)
	out["p0"] = p0
	return out
}

// lifeOf is seat's life total at the last snapshot of res.
func lifeOf(res rules.OracleResult, seat int) int32 {
	for _, p := range res.Snapshots[len(res.Snapshots)-1].Players {
		if p.Seat == seat {
			return p.Life
		}
	}
	return 0
}
