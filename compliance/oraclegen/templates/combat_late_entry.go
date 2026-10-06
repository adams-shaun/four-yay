package templates

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// lateEntryBlock is the combat.block scenario for a creature that does not
// survive to the opponent's turn from a setup placement (Ball Lightning's
// "At the beginning of the end step, sacrifice it" fires at p0's own turn-1
// end step, so p1's attack finds no blocker). The card starts in p0's hand
// and is put onto the battlefield (the runner's and the driver's `move` op)
// after p1 declares its attack, then blocks. ok is false unless the ordinary
// placement really loses the card before p1's attack: a card that is on the
// battlefield then and still cannot block keeps the ordinary skip.
func lateEntryBlock(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Scenario, bool) {
	if req.Sub != "combat.block" || req.Face != 0 || !f.IsCreature() {
		return oraclegen.Scenario{}, false
	}
	attack := oraclegen.Step{Op: "attack", Seat: 1, Defender: "p0", Attackers: []string{"p1:" + combatBlocker}}
	probe := combatScenario(f, name, req, []oraclegen.Step{attack})
	if res, ok := runStatic(reg, probe); !ok || onBattlefield(res, "p0:"+name) {
		return oraclegen.Scenario{}, false
	}
	sc := combatScenario(f, name, req, []oraclegen.Step{
		attack,
		{Op: "move", Seat: 0, Card: "p0:" + name, To: "battlefield"},
		{Op: "block", Seat: 0, Blocks: [][2]string{{"p0:" + name, "p1:" + combatBlocker}}},
		{Op: "pass_to", Seat: 0, Step: "main2", Active: "p1"},
	})
	p0 := sc.Setup["p0"]
	p0.Battlefield = removeName(p0.Battlefield, name)
	p0.Hand = append(p0.Hand, name)
	sc.Setup["p0"] = p0
	return sc, true
}

func removeName(names []string, name string) []string {
	var out []string
	for _, n := range names {
		if n != name {
			out = append(out, n)
		}
	}
	return out
}
