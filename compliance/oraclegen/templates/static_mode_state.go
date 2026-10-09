// Level-B observations for the state-reading static modes (ticket
// levelb-remaining-static-modes): the mana pool across a step end, damage
// across the cleanup step, the legend rule over duplicate permanents, a copy
// spell pointed at an uncopyable spell, and a counter that never lands on an
// enchanted host. Every item proves its claim the way the other static
// templates do: a control replay without the card shows the ordinary
// behaviour, the observation replay with the card live shows the change.
package templates

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// poolOf reads one seat's mana pool from a snapshot ("WUBRGC" letters).
func poolOf(s rules.OracleSnapshot, seat int) string {
	for _, p := range s.Players {
		if p.Seat == seat {
			return p.Pool
		}
	}
	return ""
}

// permCount counts the battlefield permanents named name in the last snapshot.
func permCount(res rules.OracleResult, name string) int {
	n := 0
	if len(res.Snapshots) == 0 {
		return 0
	}
	for _, p := range res.Snapshots[len(res.Snapshots)-1].Permanents {
		if p.Name == name {
			n++
		}
	}
	return n
}

// permByName returns one battlefield permanent by name in the last snapshot.
func permByName(res rules.OracleResult, name string) (rules.OracleSnapPerm, bool) {
	if len(res.Snapshots) == 0 {
		return rules.OracleSnapPerm{}, false
	}
	for _, p := range res.Snapshots[len(res.Snapshots)-1].Permanents {
		if p.Name == name {
			return p, true
		}
	}
	return rules.OracleSnapPerm{}, false
}

// graveHasP0 reports whether the last snapshot's p0 graveyard names card.
func graveHasP0(res rules.OracleResult, name string) bool {
	if len(res.Snapshots) == 0 || len(res.Snapshots[len(res.Snapshots)-1].Players) == 0 {
		return false
	}
	for _, g := range res.Snapshots[len(res.Snapshots)-1].Players[0].Graveyard {
		if g == name {
			return true
		}
	}
	return false
}

// manaTypeLetterFor maps a ManaType$ colour word to its pool letter.
func manaTypeLetterFor(word string) string {
	switch strings.ToLower(word) {
	case "white":
		return "W"
	case "blue":
		return "U"
	case "black":
		return "B"
	case "red":
		return "R"
	case "green":
		return "G"
	case "colorless":
		return "C"
	}
	return ""
}

// unspentManaItem serves UnspentMana (Electro, Assaulting Battery): with the
// card on the battlefield, red mana added mid-step survives the end of the
// step (CR 500.4); the control without the card shows the pool empty at the
// same checkpoint. The observation's precondition sits on the mana op's own
// snapshot: the pool held the mana when it was added.
func unspentManaItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "UnspentMana"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	st, _ := staticSlotOf(f, req)
	letter := manaTypeLetterFor(st.Params["ManaType"])
	if letter == "" {
		return skip("ManaType$ is not one colour word")
	}
	steps := []oraclegen.Step{
		{Op: "mana", Seat: 0, Mana: letter},
		{Op: "pass_to", Seat: 0, Step: "main2"},
	}
	control := staticScenario(f, name, nil, nil, steps)
	cres, ok := runStatic(reg, control)
	if !ok || len(cres.Fails) != 0 || poolOf(cres.Snapshots[len(cres.Snapshots)-1], 0) != "" {
		return skip(fmt.Sprintf("control pool is not empty at main2 (ok=%v fails=%v)", ok, cres.Fails))
	}
	sc := withBackFace(staticScenario(f, name, []string{name}, nil, steps), name, req)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip(fmt.Sprintf("observation does not replay (ok=%v fails=%v)", ok, res.Fails))
	}
	held := false
	for _, s := range res.Snapshots {
		if poolOf(s, 0) == letter {
			held = true
		}
	}
	if !held {
		return skip("the mana op did not put the mana in the pool")
	}
	if poolOf(res.Snapshots[len(res.Snapshots)-1], 0) != letter {
		return skip("the mana did not survive the step end")
	}
	if !onBattlefield(res, "p0:"+name) {
		return skip("card is not on the battlefield at the checkpoint")
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"500.4"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// noCleanupBlocker is the control's blocker: a creature the same attack
// damages but whose marked damage the cleanup step clears (it carries no
// NoCleanupDamage static).
const noCleanupBlocker = "Giant Spider"

// noCleanupAttacker is the 2-power attacker both runs share.
const noCleanupAttacker = "Grizzly Bears"

// noCleanupDamageItem serves NoCleanupDamage (Ancient Adamantoise): combat
// damage marked on the card survives the cleanup step and is still marked in
// turn 2's main phase; the control shows a plain blocker's damage cleared.
func noCleanupDamageItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "NoCleanupDamage"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	mkSteps := func(blocker string) []oraclegen.Step {
		return []oraclegen.Step{
			{Op: "attack", Seat: 1, Defender: "p0", Attackers: []string{"p1:" + noCleanupAttacker}},
			{Op: "block", Seat: 0, Blocks: [][2]string{{"p0:" + blocker, "p1:" + noCleanupAttacker}}},
			{Op: "pass_to", Seat: 0, Step: "main2"},
			{Op: "pass_to", Seat: 0, Step: "main1"},
		}
	}
	// Control: the plain blocker takes the same 2 damage and the cleanup step
	// clears it (turn 2's main1 snapshot shows damage 0).
	control := staticScenario(f, name, []string{noCleanupBlocker}, nil, mkSteps(noCleanupBlocker))
	cres, ok := runStatic(reg, control)
	if !ok || len(cres.Fails) != 0 {
		return skip(fmt.Sprintf("control does not replay (ok=%v fails=%v)", ok, cres.Fails))
	}
	cp, ok := permByName(cres, noCleanupBlocker)
	if !ok || cp.Damage != 0 {
		return skip(fmt.Sprintf("control damage is not cleared by cleanup (damage %d)", cp.Damage))
	}
	// Observation: the card under test blocks instead; its damage is still
	// marked in turn 2's main1 snapshot.
	sc := withBackFace(staticScenario(f, name, []string{name}, nil, mkSteps(name)), name, req)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip(fmt.Sprintf("observation does not replay (ok=%v fails=%v)", ok, res.Fails))
	}
	op, ok := permByName(res, name)
	if !ok || op.Damage != 2 {
		return skip(fmt.Sprintf("the card's damage is not marked across cleanup (damage %d)", op.Damage))
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"514.2"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// legendSpiderProbe is the legendary Spider both runs duplicate: one copy
// placed at setup, the second cast from hand.
const legendSpiderProbe = "Lady Spider, Maybelle Reilly"

// ignoreLegendRuleItem serves IgnoreLegendRule (Spider-Verse): the setup
// places two same-named legendary probes; the legend rule keeps one and puts
// the other in the graveyard before turn 1 (CR 704.5j), while with the card on
// the battlefield both stay.
func ignoreLegendRuleItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "IgnoreLegendRule"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	st, _ := staticSlotOf(f, req)
	typ := legendTypeOf(st.ParamStr(cards.PKValidCard))
	probe, ok := legendProbeFor(reg, typ)
	if !ok {
		return skip("no legendary " + typ + " probe with a plain cast the fixture can duplicate")
	}
	// Control: the legend rule keeps one copy and puts the other in the
	// graveyard.
	control := staticScenario(f, name, nil, nil, nil)
	control.Setup["p0"] = oraclegen.Seat{Battlefield: []string{probe, probe}}
	cres, ok := runStatic(reg, control)
	if !ok || len(cres.Fails) != 0 {
		return skip(fmt.Sprintf("control does not replay (ok=%v fails=%v)", ok, cres.Fails))
	}
	if permCount(cres, probe) != 1 || !graveHasP0(cres, probe) {
		return skip("control does not show the legend rule applying")
	}
	// Observation: both copies stay.
	sc := staticScenario(f, name, []string{name}, nil, nil)
	sc = withBackFace(sc, name, req)
	p0 := sc.Setup["p0"]
	p0.Battlefield = append(p0.Battlefield, probe, probe)
	sc.Setup["p0"] = p0
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip(fmt.Sprintf("observation does not replay (ok=%v fails=%v)", ok, res.Fails))
	}
	if permCount(res, probe) != 2 {
		return skip("the duplicate was not both kept with the card on the battlefield")
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"704.5j"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// legendTypeOf reads the type word of a <Type>.YouCtrl legend-exemption filter.
func legendTypeOf(vc string) string {
	low := strings.ToLower(vc)
	if i := strings.Index(low, "."); i > 0 {
		return low[:i]
	}
	return ""
}

// withExiledCopy returns s with one exiled copy of the probe, the duplicate
// the move op brings onto the battlefield.
func withExiledCopy(s oraclegen.Seat, probe string) oraclegen.Seat {
	s.Exile = []string{probe}
	return s
}

// legendProbeFor returns the corpus's legendary creature of the given type
// the legend fixture duplicates. Known probes only: an open-ended corpus scan
// would make the item's cost dependent on registry iteration order.
func legendProbeFor(reg *cards.Registry, typ string) (string, bool) {
	if typ != "spider" {
		return "", false
	}
	c, ok := reg.Lookup(legendSpiderProbe)
	if !ok || len(c.Faces) == 0 || !c.Faces[0].IsCreature() || !c.Faces[0].IsLegendary() {
		return "", false
	}
	return legendSpiderProbe, true
}

// copySourceProbe and copyTargetProbe are the CantBeCopied pair: a plain
// instant the copy spell CAN copy (the control), and Reverberate, the copy
// spell itself.
const (
	copySourceProbe = "Shock"
	copyTargetProbe = "Reverberate"
)

// cantBeCopiedItem serves CantBeCopied (Choreographed Sparks): the control
// shows a copy spell pointed at a plain instant minting the copy on the stack
// (two Shock spells after it resolves); the observation points the same copy
// spell at the card and no copy is minted.
func cantBeCopiedItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantBeCopied"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	st, _ := staticSlotOf(f, req)
	if !strings.EqualFold(st.ParamStr(cards.PKEffectZone), "Stack") {
		return skip("the static is not an EffectZone$ Stack spell static")
	}
	passPair := []oraclegen.Step{{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}, {Op: "pass_to", Seat: 0, Decision: "priority"}}
	// Control: the copy spell resolves onto the Shock and the copy is minted
	// on the stack.
	control := staticScenario(f, name, nil, []string{copySourceProbe, copyTargetProbe}, append([]oraclegen.Step{
		{Op: "mana", Seat: 0, Mana: "RRR"},
		{Op: "cast", Seat: 0, Card: "p0:" + copySourceProbe, Targets: []string{"p1"}},
		{Op: "cast", Seat: 0, Card: "p0:" + copyTargetProbe, Targets: []string{"p0:" + copySourceProbe}},
	}, passPair...))
	cres, ok := runStatic(reg, control)
	if !ok || len(cres.Fails) != 0 {
		return skip(fmt.Sprintf("control does not replay (ok=%v fails=%v)", ok, cres.Fails))
	}
	if stackCountOf(cres, copySourceProbe) != 2 {
		return skip(fmt.Sprintf("control does not show the copy minted (stack %v)", stackSources(cres)))
	}
	// Observation: the same copy spell points at the card on the stack; no
	// copy is minted.
	sc := staticScenario(f, name, nil, []string{copySourceProbe, name, copyTargetProbe}, append([]oraclegen.Step{
		{Op: "mana", Seat: 0, Mana: "RRRRR"},
		{Op: "cast", Seat: 0, Card: "p0:" + copySourceProbe, Targets: []string{"p1"}},
		{Op: "cast", Seat: 0, Card: "p0:" + name},
		{Op: "cast", Seat: 0, Card: "p0:" + copyTargetProbe, Targets: []string{"p0:" + name}},
	}, passPair...))
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip(fmt.Sprintf("observation does not replay (ok=%v fails=%v)", ok, res.Fails))
	}
	if stackCountOf(res, name) != 1 {
		return skip(fmt.Sprintf("the copy spell still minted a copy of the card (stack %v)", stackSources(res)))
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"707.10"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// stackSources lists the stack entries' source refs in the last snapshot.
func stackSources(res rules.OracleResult) []string {
	if len(res.Snapshots) == 0 {
		return nil
	}
	var out []string
	for _, e := range res.Snapshots[len(res.Snapshots)-1].Stack {
		out = append(out, e.Source)
	}
	return out
}

// stackCountOf counts the last snapshot's stack entries whose source ref names
// name (the "#N" ordinal of a duplicate ref stripped).
func stackCountOf(res rules.OracleResult, name string) int {
	n := 0
	for _, s := range stackSources(res) {
		if i := strings.IndexByte(s, '#'); i >= 0 {
			s = s[:i]
		}
		if s == "p0:"+name {
			n++
		}
	}
	return n
}

// putCounterHost is the enchanted creature the counter never lands on.
const putCounterHost = "Giant Spider"

// putCounterSpell is the plain one-counter spell both runs cast at the host.
const putCounterSpell = "Basri's Solidarity"

// cantPutCounterItem serves CantPutCounter (Blossombind): with the Aura
// attached, the counter spell resolves but no +1/+1 counter lands on the
// enchanted host; the control (the Aura absent) shows the counter landing.
func cantPutCounterItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantPutCounter"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	host := putCounterHost
	auraMana, why := oraclegen.PoolFor(f.ManaCost)
	if why != "" {
		return skip("aura mana: " + why)
	}
	probeMana := "WW"
	countersOf := func(res rules.OracleResult) int32 {
		p, ok := permByName(res, host)
		if !ok {
			return -1
		}
		return p.Counters["P1P1"]
	}
	// Control: the Aura absent, the counter lands.
	control := staticScenario(f, name, []string{host}, []string{putCounterSpell}, []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + putCounterSpell, Mana: probeMana},
		{Op: "resolve"},
	})
	cres, ok := runStatic(reg, control)
	if !ok || len(cres.Fails) != 0 || countersOf(cres) != 1 {
		return skip(fmt.Sprintf("control does not put the counter on the host (counters %d, ok=%v fails=%v)", countersOf(cres), ok, cres.Fails))
	}
	// Observation: the Aura is cast onto the host first; the counter never
	// lands.
	sc := staticScenario(f, name, []string{host}, []string{name, putCounterSpell}, []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + name, Mana: auraMana, Targets: []string{"p0:" + host}},
		{Op: "resolve"},
		{Op: "cast", Seat: 0, Card: "p0:" + putCounterSpell, Mana: probeMana},
		{Op: "resolve"},
	})
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip(fmt.Sprintf("observation does not replay (ok=%v fails=%v)", ok, res.Fails))
	}
	if countersOf(res) != 0 {
		return skip(fmt.Sprintf("the counter landed on the enchanted host (counters %d)", countersOf(res)))
	}
	if !onBattlefield(res, "p0:"+name) {
		return skip("the Aura is not attached at the checkpoint")
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"701.25a"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}
