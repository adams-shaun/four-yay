// Level-B observation of an unconditional CantDraw static ("Players can't
// draw cards", Mornsong Aria): p0 casts Divination with the card on the
// battlefield and its hand does not grow -- the draws the spell's resolve
// would produce are refused. The control replays the same cast without the
// card and grows the hand by exactly the spell's draw count, so the
// observation's expected hand size (control minus the draw) is derived from
// the runs; the final snapshots' hand lists are what the frozen comparison
// reads on both engines.
package templates

import (
	"fmt"
	"strconv"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// cantDrawProbe is a plain sorcery whose only effect is drawing two cards.
const cantDrawProbe = "Divination"

func cantDrawItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantDraw"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	pool, why := oraclegen.PoolFor(probeCost(reg, cantDrawProbe))
	if why != "" {
		return skip("draw probe has no payable cost: " + why)
	}
	draw, dwhy := probeDrawCount(reg, cantDrawProbe)
	if dwhy != "" {
		return skip(dwhy)
	}
	steps := []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + cantDrawProbe, Mana: pool},
		{Op: "resolve"},
	}
	hand := func(res rules.OracleResult) int {
		if len(res.Snapshots) == 0 || len(res.Snapshots[len(res.Snapshots)-1].Players) == 0 {
			return -1
		}
		return len(res.Snapshots[len(res.Snapshots)-1].Players[0].Hand)
	}
	control := staticScenario(f, name, nil, []string{cantDrawProbe}, steps)
	cres, cok := runStatic(reg, control)
	if !cok || len(cres.Fails) != 0 {
		return skip(fmt.Sprintf("control did not draw with the probe (ok=%v fails=%v)", cok, cres.Fails))
	}
	// The control drew the probe's cards; the observation must hold the hand
	// exactly `draw` below it (the cast moved one card out of p0's hand and
	// nothing back in).
	want := hand(cres) - draw
	if want < 0 {
		return skip("control hand size implausible")
	}
	sc := staticScenario(f, name, []string{name}, []string{cantDrawProbe}, steps)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip(fmt.Sprintf("scenario did not replay (ok=%v fails=%v)", ok, res.Fails))
	}
	if got := hand(res); got != want {
		return skip(fmt.Sprintf("hand is %d with the card on the battlefield, want %d", got, want))
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"614.1"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// probeDrawCount reads a plain draw spell's NumCards.
func probeDrawCount(reg *cards.Registry, name string) (int, string) {
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 || len(c.Faces[0].Abilities) == 0 {
		return 0, "draw probe has no spell ability"
	}
	n, err := strconv.Atoi(c.Faces[0].Abilities[0].ParamStr(cards.PKNumCards))
	if err != nil || n <= 0 {
		return 0, "draw probe's draw count is not a positive integer"
	}
	return n, ""
}
