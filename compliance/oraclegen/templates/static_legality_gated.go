package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// Level-B gated combat-legality observations (levelb/legality_gated.go names
// the shapes). A restriction whose gate is a setup-able board fact -- a
// graveyard/exile count, a Condition$ ability word, a CheckSVar$ head the
// ledger already counts, a printed-power filter or a counters gate -- is
// observed twice: with the gate HOLDS (the action is refused) and with the
// gate unheld (the control, the same action offered). The paired runs are
// what keeps the item from passing on an unconditional restriction or an
// unimplemented gate: an engine that restricted unconditionally fails the
// control, and one that never restricted fails the observation.
//
// CanAttackDefender's wall control (a plain Defender creature may not attack)
// and MustAttack's unrequired-probe control ride the same decision, so a
// "nothing happens" read there cannot be vacuous either.

// graveSeven is the seven-card graveyard Threshold reads (7+ cards).
var graveSeven = []string{"Plains", "Island", "Mountain", "Forest", "Swamp", "Grizzly Bears", "Shock"}

// deliriumGrave is four cards of distinct core types (land, creature,
// instant, sorcery) -- Delirium's 4+ distinct-type census.
var deliriumGrave = []string{"Plains", "Grizzly Bears", "Shock", "Duress"}

// gateSide is one polarity of a gate: the setup deltas and any pre-combat
// steps that make the gate hold (or not).
type gateSide struct {
	battlefield   []string
	grave         []string
	exile         []string
	hand          []string
	counters      map[string]int32 // card name -> P1P1 count
	speed         int32
	p1Battlefield []string
	steps         []oraclegen.Step
	// value, when known, is the gate-read number this side produces; the
	// resolver checks compareHolds against both sides under the compare the
	// gate is read under.
	value int32
	known bool
}

// gateSides is the holds/unholds pair a gated template needs.
type gateSides struct {
	on, off gateSide
}

// compareHolds evaluates one Forge SVarCompare$ form ("<op><int>"); an empty
// compare is Forge's default nonzero read.
func compareHolds(cmp string, v int32) bool {
	cmp = strings.TrimSpace(cmp)
	if cmp == "" {
		return v != 0
	}
	split := 0
	for split < len(cmp) && (cmp[split] < '0' || cmp[split] > '9') {
		split++
	}
	if split == 0 || split == len(cmp) {
		return false
	}
	n, err := strconv.Atoi(cmp[split:])
	if err != nil {
		return false
	}
	switch strings.ToUpper(cmp[:split]) {
	case "EQ":
		return v == int32(n)
	case "NE":
		return v != int32(n)
	case "LT":
		return v < int32(n)
	case "GT":
		return v > int32(n)
	case "LE":
		return v <= int32(n)
	case "GE":
		return v >= int32(n)
	}
	return false
}

// gateCountFixture names the fixture card whose presence moves one count of
// the present spec, or "" when the spec is not one the resolver fields.
func gateCountFixture(spec, zone string) string {
	if zone == "Graveyard" || zone == "Exile" {
		return "Plains"
	}
	low := strings.ToLower(spec)
	if strings.Contains(low, "powerge4") {
		return "Craw Wurm"
	}
	if strings.Contains(low, "wolf") {
		return "Wandering Wolf"
	}
	if strings.Contains(low, "artifact") {
		return "Ornithopter"
	}
	if strings.Contains(low, "creature") {
		return "Grizzly Bears"
	}
	return ""
}

// appendSteps returns steps with prefix in front, copying both.
func appendSteps(prefix, steps []oraclegen.Step) []oraclegen.Step {
	out := make([]oraclegen.Step, 0, len(prefix)+len(steps))
	out = append(out, prefix...)
	return append(out, steps...)
}

// castResolveSteps is the cast-and-resolve pair for a card with no mana cost
// and no targets.
func castResolveSteps(card string) []oraclegen.Step {
	return []oraclegen.Step{{Op: "cast", Seat: 0, Card: "p0:" + card}, {Op: "resolve"}}
}

func intPtr(i int) *int { return &i }

func addCounter(m map[string]int32, name string, n int32) map[string]int32 {
	if m == nil {
		m = map[string]int32{}
	}
	m[name] += n
	return m
}

// resolveGate reads st's Condition$/CheckSVar$/IsPresent$ family (plus the
// ValidCard/ValidAttacker "+" tokens) into the gate's two setups. It returns
// nil with a reason when the gate is not one it can set up: the caller keeps
// that reason in its skip, so an unresolvable gate stays visible.
func resolveGate(f *cards.Face, st *cards.Static) (*gateSides, string) {
	g := &gateSides{}
	if c := st.ParamStr(cards.PKCondition); c != "" {
		switch strings.ToLower(c) {
		case "threshold":
			g.on.grave, g.off.grave = graveSeven, graveSeven[:6]
			g.on.value, g.off.value, g.on.known, g.off.known = 1, 0, true, true
		default:
			return nil, "condition " + c
		}
	}
	if cs := st.ParamStr(cards.PKCheckSVar); cs != "" {
		body := cs
		if !strings.Contains(body, "$") {
			resolved, ok := f.SVars[strings.TrimSpace(body)]
			if !ok || strings.TrimSpace(resolved) == "" {
				return nil, "CheckSVar " + body + " has no SVar body"
			}
			body = resolved
		}
		if why := gateFromSVarBody(g, f, body, st.ParamStr(cards.PKSVarCompare)); why != "" {
			return nil, why
		}
	}
	if spec, ok := st.Param(cards.PKIsPresent); ok {
		if why := gateFromPresent(g, spec, st.ParamStr(cards.PKPresentZone), st.ParamStr(cards.PKPresentCompare)); why != "" {
			return nil, why
		}
	}
	if spec2, ok := st.Param(cards.PKIsPresent2); ok {
		if why := gateFromPresent(g, spec2, st.ParamStr(cards.PKPresentZone), st.ParamStr(cards.PKPresentCompare)); why != "" {
			return nil, why
		}
	}
	// The "+" tokens after Self: the counter gate and the printed-power
	// compares. "attacking" needs no setup (it is true at the blockers
	// checkpoint by construction).
	for _, tok := range selfFilterExtraTokens(st) {
		low := strings.ToLower(tok)
		switch {
		case low == "attacking":
			// True by construction at the blockers checkpoint.
		case low == "hascounters":
			g.on.counters = addCounter(g.on.counters, f.Name, 1)
		case strings.HasPrefix(low, "power"):
			_, n, ok := levelb.PowerFilterBound(tok)
			if !ok {
				return nil, "power filter " + tok
			}
			p, _, ptOK := parsePT(f.PT)
			if !ptOK {
				return nil, "power filter " + tok + " on a face without printed power"
			}
			switch {
			case strings.HasPrefix(low, "powerlt"):
				if p >= n {
					return nil, "power filter " + tok + " always holds"
				}
				g.off.counters = addCounter(g.off.counters, f.Name, int32(n-p))
			case strings.HasPrefix(low, "powerle"):
				if p > n {
					return nil, "power filter " + tok + " always holds"
				}
				g.off.counters = addCounter(g.off.counters, f.Name, int32(n+1-p))
			default:
				return nil, "power filter " + tok
			}
		default:
			return nil, "filter token " + tok
		}
	}
	return g, ""
}

// selfFilterExtraTokens returns the "+" qualifiers after the Self-scoped
// base of the static's ValidCard$ filter (its ValidAttacker$ when the static
// spells none).
func selfFilterExtraTokens(st *cards.Static) []string {
	filter := st.ParamStr(cards.PKValidCard)
	if filter == "" {
		filter = st.ParamStr(cards.PKValidAttacker)
	}
	return strings.Split(filter, "+")[1:]
}

// gateFromSVarBody maps one CheckSVar$ body (the resolved SVar text or the
// inline Count$ expression) onto the gate's two setups, validating each
// pair against the SVarCompare$ the static reads the value under.
func gateFromSVarBody(g *gateSides, f *cards.Face, body, cmp string) string {
	body = strings.TrimSpace(body)
	rest, ok := strings.CutPrefix(body, "Count$")
	if ok {
		switch {
		case strings.HasPrefix(rest, "Delirium."):
			// Count$Delirium.0.1 branches YES=0, NO=1: the value is 1 while
			// the graveyard holds FEWER than four distinct core types, so
			// the gate holds on the one-type grave and the control carries
			// the four.
			g.on.grave, g.off.grave = []string{"Plains"}, deliriumGrave
			g.on.value, g.off.value, g.on.known, g.off.known = 1, 0, true, true
		case strings.HasPrefix(rest, "YouDrewThisTurn"):
			// Divination draws two in one cast; the ledger counts it this
			// turn.
			g.on.hand = []string{"Divination"}
			g.on.steps = []oraclegen.Step{
				{Op: "mana", Seat: 0, Mana: "UUU"},
				{Op: "cast", Seat: 0, Card: "p0:Divination", Mana: "UUU"},
				{Op: "resolve"},
			}
			g.on.value, g.off.value, g.on.known, g.off.known = 2, 0, true, true
		case strings.HasPrefix(rest, "ThisTurnCast_Card.YouCtrl"):
			g.on.hand = []string{"Memnite", "Ornithopter"}
			g.on.steps = appendSteps(castResolveSteps("Memnite"), castResolveSteps("Ornithopter"))
			g.on.value, g.off.value, g.on.known, g.off.known = 2, 0, true, true
		case strings.HasPrefix(rest, "ThisTurnEntered_Battlefield_Artifact"):
			g.on.hand = []string{"Ornithopter"}
			g.on.steps = castResolveSteps("Ornithopter")
			g.on.value, g.off.value, g.on.known, g.off.known = 1, 0, true, true
		case strings.HasPrefix(rest, "CardCounters.ALL/Mod.2"):
			// Even/odd on the host's own counters, read at the combat
			// checkpoint AFTER the host's own main-phase counters trigger
			// (Sab-Sunen's) has resolved: setup 0 reads 1 (odd), setup 1
			// reads 2 (even).
			g.off.counters = addCounter(g.off.counters, f.Name, 1)
			g.on.value, g.off.value, g.on.known, g.off.known = 1, 0, true, true
		case strings.HasPrefix(rest, "CommittedCrimeThisTurn"):
			g.on.hand = []string{"Shock"}
			g.on.p1Battlefield = []string{"Wall of Omens"}
			g.on.steps = []oraclegen.Step{
				{Op: "mana", Seat: 0, Mana: "R"},
				{Op: "cast", Seat: 0, Card: "p0:Shock", Mana: "R", Targets: []string{"p1:Wall of Omens"}},
				{Op: "resolve"},
			}
			g.on.value, g.off.value, g.on.known, g.off.known = 1, 0, true, true
		case strings.HasPrefix(rest, "ValidGraveyard"):
			return "CheckSVar " + body + " needs fixtures this build cannot set up"
		default:
			return "CheckSVar " + body
		}
		if !compareHolds(cmp, g.on.value) || compareHolds(cmp, g.off.value) {
			return "compare " + cmp + " does not separate the setups"
		}
		return ""
	}
	prop, args, _ := strings.Cut(body, " ")
	base, propHead, ok := strings.Cut(prop, "$")
	if !ok || !strings.EqualFold(base, "PlayerCountPropertyYou") {
		return "CheckSVar " + body
	}
	switch {
	case strings.EqualFold(propHead, "SacrificedThisTurn"):
		if !strings.Contains(args, "Artifact") {
			return "CheckSVar " + body
		}
		g.on.hand = []string{"Chromatic Star"}
		// Chromatic Star's sac ability: cast it, then sacrifice it for
		// mana.
		g.on.steps = appendSteps(
			[]oraclegen.Step{{Op: "mana", Seat: 0, Mana: "R"}, {Op: "cast", Seat: 0, Card: "p0:Chromatic Star", Mana: "R"}, {Op: "resolve"}},
			[]oraclegen.Step{{Op: "mana", Seat: 0, Mana: "R"}, {Op: "activate", Seat: 0, Card: "p0:Chromatic Star", Mana: "R", AbilityIndex: intPtr(0)}})
		g.on.value, g.off.value, g.on.known, g.off.known = 1, 0, true, true
	case strings.EqualFold(propHead, "HasPropertyMaxSpeed"):
		// The gate holds while the controller has NOT reached max speed
		// (NE1 reads 0); the control starts at speed 4.
		g.off.speed = 4
		g.on.value, g.off.value, g.on.known, g.off.known = 0, 1, true, true
	default:
		return "CheckSVar " + body
	}
	if !compareHolds(cmp, g.on.value) || compareHolds(cmp, g.off.value) {
		return "compare " + cmp + " does not separate the setups"
	}
	return ""
}

// gateFromPresent maps the IsPresent$ count family onto the gate's two
// setups, validating both sides under the PresentCompare$ the engine reads
// the count with.
func gateFromPresent(g *gateSides, spec, zone, cmpRaw string) string {
	cmp := strings.TrimSpace(cmpRaw)
	if cmp == "" {
		cmp = "GE1"
	}
	op := cmp
	n := 0
	for i := 0; i < len(op); i++ {
		if op[i] >= '0' && op[i] <= '9' {
			var err error
			n, err = strconv.Atoi(op[i:])
			if err != nil {
				return "present compare " + cmp
			}
			op = op[:i]
			break
		}
	}
	fixture := gateCountFixture(spec, zone)
	if fixture == "" {
		return "present spec " + spec
	}
	fix := func(k int) []string {
		out := make([]string, 0, k)
		for i := 0; i < k; i++ {
			out = append(out, fixture)
		}
		return out
	}
	var on, off []string
	switch strings.ToUpper(op) {
	case "GE":
		if n < 1 {
			return "present compare " + cmp
		}
		on, off = fix(n), nil
	case "EQ":
		if n != 0 {
			return "present compare " + cmp
		}
		on, off = nil, fix(1)
	case "LE":
		on, off = nil, fix(n+1)
	case "LT":
		if n < 1 {
			return "present compare " + cmp
		}
		on, off = nil, fix(n)
	default:
		return "present compare " + cmp
	}
	if !compareHolds(cmp, int32(len(on))) || compareHolds(cmp, int32(len(off))) {
		return "present compare " + cmp + " does not separate the setups"
	}
	switch zone {
	case "Graveyard":
		g.on.grave, g.off.grave = on, off
	case "Exile":
		g.on.exile, g.off.exile = on, off
	case "Battlefield", "":
		g.on.battlefield, g.off.battlefield = on, off
	default:
		return "present zone " + zone
	}
	return ""
}

// applyGateSide writes one polarity's setup into the scenario: the card's
// seat, and (for the crime plans) p1's.
func applyGateSide(sc oraclegen.Scenario, name string, req levelb.Requirement, side gateSide) oraclegen.Scenario {
	p0 := sc.Setup["p0"]
	for _, b := range side.battlefield {
		p0.Battlefield = appendFixtureUnique(p0.Battlefield, b)
	}
	p0.Graveyard = append(p0.Graveyard, side.grave...)
	p0.Exile = append(p0.Exile, side.exile...)
	p0.Hand = append(p0.Hand, side.hand...)
	for card, n := range side.counters {
		p0 = oraclegen.WithCounters(p0, card, "P1P1", n)
	}
	if side.speed != 0 {
		p0.Speed = side.speed
	}
	setupBackFace(&p0, name, req)
	sc.Setup["p0"] = p0
	if len(side.p1Battlefield) != 0 {
		p1 := sc.Setup["p1"]
		for _, b := range side.p1Battlefield {
			p1.Battlefield = appendFixtureUnique(p1.Battlefield, b)
		}
		sc.Setup["p1"] = p1
	}
	sc.Steps = appendSteps(side.steps, sc.Steps)
	return sc
}

// gatedScenario is the skeleton: the card under test plus the probe fixtures
// on p0's battlefield with the gate polarity applied, and the steps to run.
func gatedScenario(f *cards.Face, name string, req levelb.Requirement, side gateSide, extras []string, steps []oraclegen.Step) oraclegen.Scenario {
	board := []string{name}
	for _, e := range extras {
		board = appendFixtureUnique(board, e)
	}
	sc := staticScenario(f, name, board, nil, steps)
	return applyGateSide(sc, name, req, side)
}

// withP1Board sets the defending seat's battlefield fixtures.
func withP1Board(sc oraclegen.Scenario, fixtures ...string) oraclegen.Scenario {
	p1 := sc.Setup["p1"]
	for _, f := range fixtures {
		p1.Battlefield = appendFixtureUnique(p1.Battlefield, f)
	}
	sc.Setup["p1"] = p1
	return sc
}

// buildLegalityItem checks the card is on the battlefield at the checkpoint
// and builds the item.
func buildLegalityItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, mode string, sc oraclegen.Scenario, res rules.OracleResult, cr []string) (oraclegen.Item, *oraclegen.Skip) {
	if !onBattlefield(res, "p0:"+name) {
		return staticSkip(name, mode, "card is not on the battlefield at the checkpoint")
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, cr, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}

// wallAttackRefused accepts either refusal message the attack op emits for a
// defender-only board: the option absent, or the declare-attackers decision
// skipped whole when no legal attacker exists.
func wallAttackRefused(fails []string) bool {
	return failsName(fails, "attack: p0:") || failsName(fails, "passed declare-attackers without being asked")
}

// failsName reports whether any fail message names want.
func failsName(fails []string, want string) bool {
	for _, f := range fails {
		if strings.Contains(f, want) {
			return true
		}
	}
	return false
}

// reqStatic returns the requirement's static.
func reqStatic(f *cards.Face, req levelb.Requirement) (*cards.Static, string) {
	slot, err := strconv.Atoi(req.Slot)
	if err != nil || slot < 0 || slot >= len(f.Statics) {
		return nil, "requirement slot names no static"
	}
	return &f.Statics[slot], ""
}

// stHasGateParams reports whether the static carries any gate key.
func stHasGateParams(st *cards.Static) bool {
	for _, k := range levelb.GatedGateKeys() {
		if st.HasParam(k) {
			return true
		}
	}
	return false
}

// gateOn is the gate-held polarity of g (empty for an ungated static).
func gateOn(g *gateSides) gateSide {
	if g == nil {
		return gateSide{}
	}
	return g.on
}

// -- the item builders -------------------------------------------------------

// gatedCantAttackItem serves "CARDNAME can't attack unless <gate>": at p0's
// declare-attackers decision the card is unoffered while the gate holds and
// offered in the gate-unheld control.
func gatedCantAttackItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantAttack"
	st, why := reqStatic(f, req)
	if st == nil {
		return staticSkip(name, mode, why)
	}
	if f.HasKeyword("Defender") {
		return staticSkip(name, mode, "Defender keeps the attack unoffered without the static")
	}
	if name == smallAttackerProbe || name == largeAttackerProbe {
		return staticSkip(name, mode, "no creature card distinct from the probes")
	}
	g, why := resolveGate(f, st)
	if g == nil {
		return staticSkip(name, mode, "gate unsupported: "+why)
	}
	probe, self := cardAt(0, smallAttackerProbe), cardAt(0, name)
	attackExpect := func(want bool) []oraclegen.Expect {
		return []oraclegen.Expect{canAttackExpect(probe, true), canAttackExpect(self, want)}
	}
	steps := func(expect []oraclegen.Expect) []oraclegen.Step {
		return []oraclegen.Step{{Op: "pass_to", Seat: 0, Step: "declare-attackers", Decision: "attackers", Expect: expect}}
	}
	sc := gatedScenario(f, name, req, g.on, []string{smallAttackerProbe}, steps(attackExpect(false)))
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, "gate-held attack stays offered: "+joinFails(res.Fails))
	}
	ctl := gatedScenario(f, name, req, g.off, []string{smallAttackerProbe}, steps(attackExpect(true)))
	if res, ok := runStatic(reg, ctl); !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, "gate-unheld control does not offer the attack: "+joinFails(res.Fails))
	}
	return buildLegalityItem(reg, f, name, req, mode, sc, res, []string{"508.1a"})
}

// gatedCantBlockItem serves "CARDNAME can't block unless <gate>": at p0's
// declare-blockers decision the card may not block the probe attacker while
// the gate holds, and may in the gate-unheld control.
func gatedCantBlockItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantBlock"
	st, why := reqStatic(f, req)
	if st == nil {
		return staticSkip(name, mode, why)
	}
	if name == blockerProbe || name == smallAttackerProbe {
		return staticSkip(name, mode, "no creature card distinct from the probes")
	}
	g, why := resolveGate(f, st)
	if g == nil {
		return staticSkip(name, mode, "gate unsupported: "+why)
	}
	attacker := cardAt(1, smallAttackerProbe)
	blockExpect := func(want bool) []oraclegen.Expect {
		return []oraclegen.Expect{
			canBlockExpect(cardAt(0, blockerProbe), attacker, true),
			canBlockExpect(cardAt(0, name), attacker, want),
		}
	}
	build := func(side gateSide, expect []oraclegen.Expect) oraclegen.Scenario {
		sc := gatedScenario(f, name, req, side, []string{blockerProbe}, []oraclegen.Step{
			{Op: "attack", Seat: 1, Defender: "p0", Attackers: []string{attacker}},
			{Op: "pass_to", Seat: 0, Decision: "blockers", Expect: expect},
		})
		return withP1Board(sc, smallAttackerProbe)
	}
	sc := build(g.on, blockExpect(false))
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, "gate-held block stays offered: "+joinFails(res.Fails))
	}
	ctl := build(g.off, blockExpect(true))
	if res, ok := runStatic(reg, ctl); !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, "gate-unheld control does not offer the block: "+joinFails(res.Fails))
	}
	return buildLegalityItem(reg, f, name, req, mode, sc, res, []string{"509.1b"})
}

// gatedUnblockableItem serves "CARDNAME can't be blocked unless <gate>"
// (CantBlockBy with the card as the attacker): no probe blocker may block it
// while the gate holds, and the same blocker may block the vanilla probe
// beside it; the gate-unheld control offers the card's block.
func gatedUnblockableItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantBlockBy"
	st, why := reqStatic(f, req)
	if st == nil {
		return staticSkip(name, mode, why)
	}
	if !f.IsCreature() || name == largeAttackerProbe || name == blockerProbe {
		return staticSkip(name, mode, "no creature card distinct from the probes")
	}
	if hasEvasion(f) {
		return staticSkip(name, mode, "another evasion would make the card unblockable without the static")
	}
	g, why := resolveGate(f, st)
	if g == nil {
		return staticSkip(name, mode, "gate unsupported: "+why)
	}
	blocker, spider, self := cardAt(1, blockerProbe), cardAt(0, largeAttackerProbe), cardAt(0, name)
	build := func(side gateSide, cardWant bool) oraclegen.Scenario {
		sc := gatedScenario(f, name, req, side, []string{largeAttackerProbe}, []oraclegen.Step{
			{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{self, spider}},
			{Op: "pass_to", Seat: 0, Decision: "blockers", Expect: []oraclegen.Expect{
				canBlockExpect(blocker, spider, true),
				canBlockExpect(blocker, self, cardWant),
			}},
		})
		return withP1Board(sc, blockerProbe)
	}
	sc := build(g.on, false)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, "gate-held block stays offered: "+joinFails(res.Fails))
	}
	ctl := build(g.off, true)
	if res, ok := runStatic(reg, ctl); !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, "gate-unheld control does not offer the block: "+joinFails(res.Fails))
	}
	return buildLegalityItem(reg, f, name, req, mode, sc, res, []string{"509.1b"})
}

// gatedCanAttackDefenderItem serves "CARDNAME can attack as though it didn't
// have defender while <gate>" on a Defender creature: the wall control (a
// plain defender may not attack) and the gate-unheld control (the card's own
// attack op fails) frame the observation, whose attack op succeeds.
func gatedCanAttackDefenderItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CanAttackDefender"
	st, why := reqStatic(f, req)
	if st == nil {
		return staticSkip(name, mode, why)
	}
	if !f.HasKeyword("Defender") || name == defenderProbe {
		return staticSkip(name, mode, "the static is vacuous without the Defender keyword")
	}
	g, why := resolveGate(f, st)
	if g == nil {
		return staticSkip(name, mode, "gate unsupported: "+why)
	}
	if res, ok := runStatic(reg, staticScenario(f, name, []string{defenderProbe}, nil, []oraclegen.Step{
		{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:" + defenderProbe}},
	})); !ok || !wallAttackRefused(res.Fails) {
		return staticSkip(name, mode, "control unexpectedly permits the plain defender's attack: "+joinFails(res.Fails))
	}
	attack := func(side gateSide) oraclegen.Scenario {
		return gatedScenario(f, name, req, side, []string{defenderProbe}, []oraclegen.Step{
			{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:" + name}},
		})
	}
	sc := attack(g.on)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, "gate-held attack did not replay: "+joinFails(res.Fails))
	}
	if res, ok := runStatic(reg, attack(g.off)); !ok || !wallAttackRefused(res.Fails) {
		return staticSkip(name, mode, "gate-unheld control does not refuse the attack: "+joinFails(res.Fails))
	}
	return buildLegalityItem(reg, f, name, req, mode, sc, res, []string{"702.3"})
}

// maxBlockersItem serves a MinMaxBlocker Max$ cap: every (blocker, attacker)
// pair naming the capped attacker carries the bound while the vanilla probe
// attacker beside it carries none; a gated cap's control shows the bound
// lifted with the gate unheld.
func maxBlockersItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "MinMaxBlocker"
	st, why := reqStatic(f, req)
	if st == nil {
		return staticSkip(name, mode, why)
	}
	n := levelb.MaxBlockerCap(st)
	if n == 0 {
		return staticSkip(name, mode, "Max$ is not a plain bound")
	}
	if name == largeAttackerProbe || name == blockerProbe {
		return staticSkip(name, mode, "no creature card distinct from the probes")
	}
	if hasEvasion(f) {
		return staticSkip(name, mode, "another evasion would make the card unblockable without the static")
	}
	g, why := resolveGate(f, st)
	if g == nil {
		return staticSkip(name, mode, "gate unsupported: "+why)
	}
	self, spider := cardAt(0, name), cardAt(0, largeAttackerProbe)
	build := func(side gateSide, selfMax *int) oraclegen.Scenario {
		sc := gatedScenario(f, name, req, side, []string{largeAttackerProbe}, []oraclegen.Step{
			{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{self, spider}},
			{Op: "pass_to", Seat: 0, Decision: "blockers", Expect: []oraclegen.Expect{
				canBlockExpectMax(cardAt(1, blockerProbe), self, true, selfMax),
				canBlockExpectMax(cardAt(1, blockerProbe), spider, true, nil),
			}},
		})
		return withP1Board(sc, blockerProbe, blockerProbe)
	}
	sc := build(gateOn(g), intPtr(n))
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, "the bound does not hold on the capped attacker: "+joinFails(res.Fails))
	}
	if stHasGateParams(st) {
		ctl := build(g.off, nil)
		if res, ok := runStatic(reg, ctl); !ok || len(res.Fails) != 0 {
			return staticSkip(name, mode, "gate-unheld control keeps the bound: "+joinFails(res.Fails))
		}
	}
	return buildLegalityItem(reg, f, name, req, mode, sc, res, []string{"509.1a"})
}

// mustAttackItem serves MustAttack on the host (CARDNAME attacks each combat
// if able): the card's attacker option carries the requirement the plain
// probe beside it lacks, and the probe-only control shows Required is not
// blanket.
func mustAttackItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "MustAttack"
	if name == smallAttackerProbe {
		return staticSkip(name, mode, "no creature card distinct from the probe")
	}
	self, probe := cardAt(0, name), cardAt(0, smallAttackerProbe)
	sc := gatedScenario(f, name, req, gateSide{}, []string{smallAttackerProbe}, []oraclegen.Step{{
		Op: "pass_to", Seat: 0, Step: "declare-attackers", Decision: "attackers",
		Expect: []oraclegen.Expect{
			canAttackExpect(self, true),
			canAttackExpect(probe, true),
			attackRequiredExpect(self, true),
			attackRequiredExpect(probe, false),
		},
	}})
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, "the requirement does not hold: "+joinFails(res.Fails))
	}
	ctl := staticScenario(f, name, []string{smallAttackerProbe}, nil, []oraclegen.Step{{
		Op: "pass_to", Seat: 0, Step: "declare-attackers", Decision: "attackers",
		Expect: []oraclegen.Expect{canAttackExpect(probe, true), attackRequiredExpect(probe, false)},
	}})
	if res, ok := runStatic(reg, ctl); !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, "probe-only control fails: "+joinFails(res.Fails))
	}
	return buildLegalityItem(reg, f, name, req, mode, sc, res, []string{"508.1d"})
}

func joinFails(fails []string) string {
	return strings.Join(fails, "; ")
}

// canBlockExpectMax is canBlockExpect with a MinMaxBlocker Max$ bound
// assertion on the (blocker, attacker) pair's option (nil asserts no bound).
func canBlockExpectMax(blocker, attacker string, want bool, max *int) oraclegen.Expect {
	e := canBlockExpect(blocker, attacker, want)
	e.CanBlock.MaxBlockers = max
	return e
}

// attackRequiredExpect asserts whether the attacker option carries the
// MustAttack requirement at the pending declare-attackers decision.
func attackRequiredExpect(attacker string, want bool) oraclegen.Expect {
	return oraclegen.Expect{AttackRequired: &oraclegen.AttackRequired{Attacker: attacker}, Want: &want}
}
