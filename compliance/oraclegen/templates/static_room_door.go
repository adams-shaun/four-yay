// The Room-door cast base: a static on a Room's second door is served by
// casting that door (CR 709.5's "you may cast either half"), because a Room
// placed by setup has no unlocked door in either engine. The scenario starts
// with the Room card in p0's hand, casts the requirement's face by its own
// face name (the runner binds it to room_alt the way the trigger recipes do),
// and resolves it, so the door is unlocked on the battlefield before the
// static's ordinary observation runs.
package templates

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// snapTapped reports whether ref is a battlefield permanent whose snapshot
// shows the tapped flag.
func snapTapped(s rules.OracleSnapshot, ref string) (bool, bool) {
	for _, p := range s.Permanents {
		if p.Ref == ref {
			return p.Tapped, true
		}
	}
	return false, false
}

// roomDoorCastBase is the staticContinuous cast base for a face-1 Room
// requirement: the same shape as the ordinary cast base, with the face named
// by its own face name and the card dealt into p0's hand instead of priced
// from the catalogue name.
func roomDoorCastBase(reg *cards.Registry, c *cards.Card, f *cards.Face, name string, req levelb.Requirement, probes []string) (oraclegen.Item, string) {
	mana, why := oraclegen.PoolFor(f.ManaCost)
	if why != "" {
		return oraclegen.Item{}, why
	}
	steps := []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + f.Name, Mana: mana},
		{Op: "resolve"},
	}
	p0 := oraclegen.Seat{Hand: []string{name}}
	for _, probe := range probes {
		p0.Battlefield = appendFixtureUnique(p0.Battlefield, probe)
	}
	sc := oraclegen.Scenario{
		Setup:        map[string]oraclegen.Seat{"p0": p0, "p1": {}},
		SetupAnswers: oraclegen.OpeningHandAnswers(f),
		Steps:        steps,
	}
	oraclegen.Baseline(sc.Setup, f)
	return StaticApplies.item(f, name, sc), ""
}

// roomPanharmoniconScenario is panharmoniconRun.scenario with the card's
// arrival being the second door's cast: the listener (and any cause steps)
// then run with the door unlocked on the battlefield. ok is false when the
// door's cost is not payable.
func roomPanharmoniconScenario(f *cards.Face, name string, req levelb.Requirement, r panharmoniconRun) (oraclegen.Scenario, bool) {
	pool, why := oraclegen.PoolFor(f.ManaCost)
	if why != "" {
		return oraclegen.Scenario{}, false
	}
	prelude := []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + f.Name, Mana: pool},
		{Op: "resolve"},
	}
	var bf []string
	if r.setupListener {
		bf = append(bf, r.listener)
	}
	var hand []string
	if r.hand != "" {
		hand = []string{r.hand}
	}
	return staticScenario(f, name, bf, append([]string{name}, hand...), append(prelude, r.steps...)), true
}
