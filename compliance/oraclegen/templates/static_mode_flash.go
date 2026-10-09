// Level-B observation of a CastWithFlash static: a spell the grant covers is
// cast at instant speed on the OTHER player's main phase. Without the static
// the cast is not offered there (a sorcery-speed spell in someone else's main
// phase, CR 117.1a timing), so the identical steps with the source absent are
// the control. Two shapes:
//
//   - the card's own cast (ValidCard$ Card.Self, Serpent of the Pass): the
//     condition parameters (IsPresent$, PresentZone$, PresentCompare$) are
//     made true by the fixture, and the control removes the condition's
//     cards -- the static sits on the card being cast, so removing the card
//     would remove the cast itself;
//   - another spell's cast (Valley Floodcaller, Whirlwing Stormbrood, Radagast
//     of Rhosgobel): a probe spell in p0's hand, the static's card on p0's
//     battlefield, and the control removes the static's card.
//
// Both engines replay the same steps: p1's main1 reached by pass_to, p1's
// pass handing p0 priority there, then p0's cast and its resolve.
package templates

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// flashSpellProbes are the candidate probe spells, in preference order.
// Divination covers every noncreature sorcery filter (Card.nonCreature,
// Sorcery,Dragon); Grizzly Bears covers Creature. Both are plain casts with
// no target and no alternative cost.
var flashSpellProbes = []string{"Divination", "Grizzly Bears"}

// flashPresentProbes maps an IsPresent$ creature word to a probe permanent
// whose presence makes the grant live (Illusion Spinners' Faerie).
var flashPresentProbes = map[string]string{
	"faerie": "Faerie Miscreant",
}

// flashLessonProbes are the Lesson cards a graveyard-count IsPresent$ setup
// places (Serpent of the Pass's PresentCompare$ GE3, PresentZone$ Graveyard).
var flashLessonProbes = []string{"Waterbending Lesson", "Water Whip", "Whirlwind Technique"}

func castWithFlashItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CastWithFlash"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	i, err := strconv.Atoi(req.Slot)
	if err != nil || i < 0 || i >= len(f.Statics) {
		return skip("requirement slot names no static")
	}
	st := f.Statics[i]
	self := strings.Contains(st.ParamStr(cards.PKValidCard), "Self")
	hops := []oraclegen.Step{
		// Reach p1's main phase (the first main1 whose active seat is p1) and
		// pass once more: the stop leaves p1 holding priority, and the pass
		// hands it to p0, who casts there.
		{Op: "pass_to", Seat: 0, Step: "main1", Active: "p1"},
		{Op: "pass", Seat: 1},
	}
	if self {
		return castWithFlashSelfItem(reg, f, name, req, st, hops)
	}
	return castWithFlashOtherItem(reg, f, name, req, st, hops)
}

// castWithFlashOtherItem casts a probe spell on p1's main phase with the
// static's card on p0's battlefield; the control removes the card.
func castWithFlashOtherItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static, hops []oraclegen.Step) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CastWithFlash"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	for _, probe := range flashSpellProbes {
		pool, why := oraclegen.PoolFor(probeCost(reg, probe))
		if why != "" {
			continue
		}
		steps := append([]oraclegen.Step(nil), hops...)
		steps = append(steps,
			oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:" + probe, Mana: pool},
			oraclegen.Step{Op: "resolve"},
		)
		sc := staticScenario(f, name, []string{name}, []string{probe}, steps)
		res, ok := runStatic(reg, sc)
		if !ok || len(res.Fails) != 0 {
			continue
		}
		// Control: without the static the probe is not offered in p1's main
		// phase. The run must fail at the cast step for exactly that reason.
		control := staticScenario(f, name, nil, []string{probe}, steps)
		cres, _ := runStatic(reg, control)
		if len(cres.Fails) == 0 || !strings.Contains(strings.Join(cres.Fails, " "), "not offered") {
			return skip("the p1-main control did not refuse the probe cast: " + strings.Join(cres.Fails, " "))
		}
		it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"117.1a"}, sc)
		it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
		return it, nil
	}
	return skip("no probe spell's cast the grant admits replays")
}

// castWithFlashSelfItem casts the card itself on p1's main phase. The
// condition parameters must be true at setup: an IsPresent$ creature word
// places its probe permanent, a Graveyard PresentZone$ count places that many
// Lesson probes. The control removes the condition's cards.
func castWithFlashSelfItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static, hops []oraclegen.Step) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CastWithFlash"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	pool, why := oraclegen.PoolFor(f.ManaCost)
	if why != "" {
		return skip("the card's own cast cost is not payable: " + why)
	}
	var present []string
	var gy []string
	if spec, ok := st.Param(cards.PKIsPresent); ok {
		present, gy, why = flashConditionCards(reg, st, spec)
		if why != "" {
			return skip(why)
		}
	}
	steps := append([]oraclegen.Step(nil), hops...)
	steps = append(steps,
		oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:" + name, Mana: pool},
		oraclegen.Step{Op: "resolve"},
	)
	sc := staticScenario(f, name, present, []string{name}, steps)
	if len(gy) > 0 {
		p0 := sc.Setup["p0"]
		p0.Graveyard = append(p0.Graveyard, gy...)
		sc.Setup["p0"] = p0
	}
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip(fmt.Sprintf("the card's p1-main cast did not replay (ok=%v fails=%v)", ok, res.Fails))
	}
	// Control: the condition's cards gone (an empty battlefield, an empty
	// graveyard). The static sits on the card being cast, so the condition is
	// the only thing a control can remove.
	control := staticScenario(f, name, nil, []string{name}, steps)
	cres, _ := runStatic(reg, control)
	if len(cres.Fails) == 0 || !strings.Contains(strings.Join(cres.Fails, " "), "not offered") {
		return skip("the condition-removed control did not refuse the cast: " + strings.Join(cres.Fails, " "))
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"117.1a"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// flashConditionCards turns an IsPresent$ condition into setup cards: a
// battlefield probe of the named creature type (the fixed Faerie probe when
// the word is Faerie, else a quiet corpus card of that subtype), or that many
// Lesson cards for a Graveyard count.
func flashConditionCards(reg *cards.Registry, st cards.Static, spec string) (present, gy []string, why string) {
	zone := strings.ToLower(st.ParamStr(cards.PKPresentZone))
	if zone == "" || zone == "battlefield" {
		words := strings.FieldsFunc(spec, func(r rune) bool { return r == '.' || r == '+' })
		for _, w := range words {
			low := strings.ToLower(w)
			if p, ok := flashPresentProbes[low]; ok {
				return []string{p}, nil, ""
			}
			if low == "creature" || low == "card" {
				continue
			}
			if p, ok := oraclegen.SubtypeCard(reg, w); ok {
				return []string{p}, nil, ""
			}
		}
		return nil, nil, "IsPresent$ " + spec + " has no probe permanent"
	}
	if zone != "graveyard" {
		return nil, nil, "PresentZone$ " + st.ParamStr(cards.PKPresentZone) + " has no setup"
	}
	cmp := st.ParamStr(cards.PKPresentCompare)
	n, err := strconv.Atoi(strings.TrimPrefix(cmp, "GE"))
	if err != nil || n <= 0 || n > len(flashLessonProbes) {
		return nil, nil, "PresentCompare$ " + cmp + " has no Lesson setup"
	}
	return nil, flashLessonProbes[:n], ""
}

// probeCost returns a probe spell's cast cost.
func probeCost(reg *cards.Registry, name string) string {
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		return ""
	}
	return c.Faces[0].ManaCost
}
