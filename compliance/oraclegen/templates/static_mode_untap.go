// Level-B observation of an UntapOtherPlayer static: the observed permanent
// is tapped mid-scenario -- a creature (the card, or Prop Room's probe
// creature) by attacking in turn 3's combat, Bender's Waterskin by activating
// its own mana ability on turn 3 -- and then untapped during the OTHER
// player's untap step, a step where a player untaps only their own permanents
// (CR 502.2). Setup-tapped is not enough: the placement precedes turn 1, so
// the card's own controller untaps it before the first checkpoint. The final
// snapshot (the pass_to that stops in p1's untap step) carries the tapped
// flags, so the frozen comparison reads the claim on both engines. The
// Card.Self shape taps a second p0 creature (the witness) by the same attack,
// and the static must leave it tapped; the source-removed control replay
// leaves its own attacker tapped, so the assertion is the static's doing and
// not p1's untap step blanket-untapping p0's permanents.
package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// untapWitnessProbe is the second p0 creature the Card.Self shape taps by the
// same turn and which the static must leave tapped.
const untapWitnessProbe = "Grizzly Bears"

func untapOtherPlayerItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "UntapOtherPlayer"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		return skip("card not in corpus")
	}
	i, err := strconv.Atoi(req.Slot)
	if err != nil || i < 0 || i >= len(f.Statics) {
		return skip("requirement slot names no static")
	}
	st := f.Statics[i]
	if strings.Contains(st.ParamStr(cards.PKValidCard), "YouCtrl") && !strings.Contains(st.ParamStr(cards.PKValidCard), "Self") {
		// "Untap each creature you control" (Prop Room): the static's card is
		// the room's second door (cast, never placed) and a probe creature is
		// the observed permanent; every p0 creature is covered, so the control
		// is the source-removed replay.
		return untapOtherPlayerProbeItem(reg, c, f, name, req)
	}
	if !strings.Contains(st.ParamStr(cards.PKValidCard), "Self") {
		return skip("ValidCard$ " + st.ParamStr(cards.PKValidCard) + " is neither the card nor a you-control filter")
	}
	cardRef := "p0:" + name
	// Turn 3 (p0's second turn) main1: tap the card, attack the witness.
	head := []oraclegen.Step{
		{Op: "pass_to", Seat: 0, Step: "main2"},
		{Op: "pass_to", Seat: 0, Step: "main1", Active: "p0"},
	}
	var tap, attack []oraclegen.Step
	attackers := []string{"p0:" + untapWitnessProbe}
	if oraclegen.HasType(f, "Creature") {
		attackers = append(attackers, cardRef)
		attack = []oraclegen.Step{{Op: "attack", Seat: 0, Defender: "p1", Attackers: attackers}}
	} else {
		idx := -1
		for ai := range f.Abilities {
			if cards.IsManaAbilityAPI(f.Abilities[ai].API) {
				idx = ai
				break
			}
		}
		if idx < 0 {
			return skip("the card is neither a creature nor mana-ability tapped")
		}
		tap = []oraclegen.Step{{Op: "activate", Seat: 0, Card: cardRef, AbilityIndex: &idx}}
		attack = []oraclegen.Step{{Op: "attack", Seat: 0, Defender: "p1", Attackers: attackers}}
	}
	steps := append(append(append(append([]oraclegen.Step(nil), head...), tap...), attack...),
		oraclegen.Step{Op: "pass_to", Seat: 0, Step: "main2"},
		oraclegen.Step{Op: "pass_to", Seat: 0, Step: "upkeep", Active: "p1"},
	)
	sc := staticScenario(f, name, []string{name, untapWitnessProbe}, nil, steps)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip("scenario did not replay: " + strings.Join(res.Fails, " "))
	}
	if tapped, ok := untapSnapshot(res, cardRef); !ok || tapped {
		return skip("the card is not untapped in p1's untap step")
	}
	if ctl, ok := untapSnapshot(res, "p0:"+untapWitnessProbe); !ok || !ctl {
		return skip("the witness creature is not still tapped")
	}
	// Control: the static's card removed from the battlefield; the identical
	// steps (the witness attacks, the card's tap is gone) must leave the
	// witness tapped at p1's untap step.
	controlSteps := untapControlSteps(steps, cardRef)
	control := staticScenario(f, name, []string{untapWitnessProbe}, nil, controlSteps)
	cres, cok := runStatic(reg, control)
	if !cok || len(cres.Fails) != 0 {
		return skip("the source-removed control did not replay: " + strings.Join(cres.Fails, " "))
	}
	if tapped, ok := untapSnapshot(cres, "p0:"+untapWitnessProbe); !ok || !tapped {
		return skip("the control's witness is not still tapped in p1's untap step")
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"502.2"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// untapControlSteps rewrites the steps for the source-removed control: the
// attack step loses the card, and the card's own tap activation is dropped.
func untapControlSteps(steps []oraclegen.Step, cardRef string) []oraclegen.Step {
	out := make([]oraclegen.Step, 0, len(steps))
	for _, st := range steps {
		if st.Op == "activate" && st.Card == cardRef {
			continue
		}
		if st.Op == "attack" {
			att := make([]string, 0, len(st.Attackers))
			for _, a := range st.Attackers {
				if a != cardRef {
					att = append(att, a)
				}
			}
			if len(att) == 0 {
				continue
			}
			st.Attackers = att
		}
		out = append(out, st)
	}
	return out
}

// untapSnapshot reads the tapped flag of one battlefield ref in the last
// snapshot.
func untapSnapshot(res rules.OracleResult, ref string) (tapped, ok bool) {
	if len(res.Snapshots) == 0 {
		return false, false
	}
	return snapTapped(res.Snapshots[len(res.Snapshots)-1], ref)
}

// untapOtherPlayerProbeItem serves Prop Room's shape: the room's second door
// casts, a probe creature attacks and is tapped, and the probe must be
// untapped in p1's untap step; the source-removed control leaves it tapped.
func untapOtherPlayerProbeItem(reg *cards.Registry, c *cards.Card, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "UntapOtherPlayer"
	const probe = "Grizzly Bears"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	steps := []oraclegen.Step{
		{Op: "pass_to", Seat: 0, Step: "main2"},
		{Op: "pass_to", Seat: 0, Step: "main1", Active: "p0"},
		{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:" + probe}},
		{Op: "pass_to", Seat: 0, Step: "main2"},
		{Op: "pass_to", Seat: 0, Step: "upkeep", Active: "p1"},
	}
	var sc oraclegen.Scenario
	if req.Face > 0 && levelb.IsRoomCard(c) {
		pool, why := oraclegen.PoolFor(f.ManaCost)
		if why != "" {
			return skip("the door's cost is not payable: " + why)
		}
		prelude := []oraclegen.Step{
			{Op: "cast", Seat: 0, Card: "p0:" + f.Name, Mana: pool},
			{Op: "resolve"},
		}
		sc = staticScenario(f, name, []string{probe}, []string{name}, append(prelude, steps...))
	} else {
		sc = staticScenario(f, name, []string{name, probe}, nil, steps)
	}
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip("scenario did not replay: " + strings.Join(res.Fails, " "))
	}
	if tapped, ok := untapSnapshot(res, "p0:"+probe); !ok || tapped {
		return skip("the probe is not untapped in p1's untap step")
	}
	// Control: the room removed (its door uncast), the probe's own attack and
	// p1's untap step leave it tapped.
	control := staticScenario(f, name, []string{probe}, nil, steps)
	cres, cok := runStatic(reg, control)
	if !cok || len(cres.Fails) != 0 {
		return skip("the control did not replay: " + strings.Join(cres.Fails, " "))
	}
	if tapped, ok := untapSnapshot(cres, "p0:"+probe); !ok || !tapped {
		return skip("the control's probe is not still tapped")
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"502.2"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}
