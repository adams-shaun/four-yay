package templates

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// CostStatic probes a cost static by casting either a qualifying spell or its
// own source at the reduced price. The exact price is intentional: a gorge
// refusal remains a generated item and becomes a host replay divergence, not a
// generator Skip that could hide the behavior under test.
var CostStatic = Template{ID: "static.cost", Version: 1}

type costProbe struct {
	spell, mana string
	battlefield []string
	graveyard   []string
	exile       []string
	hand        []string
	first       *oraclegen.Step
	// targeted offers the probe cast a surplus player target; gorge's own
	// target decision rewrites it to the exact pick before the item is kept.
	targeted   bool
	mustReplay bool
}

func costStatic(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	idx, err := strconv.Atoi(req.Slot)
	if err != nil || idx < 0 || idx >= len(f.Statics) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "cost static index " + req.Slot}
	}
	// Each profile chooses a simple card whose printed cost makes the
	// reduction visible and supplies only the prerequisites the static needs.
	var p costProbe
	switch name {
	case "Geist of Saint Thalia":
		// Lightning Strike is {1}{R}: the generic {1} is what the reduction
		// removes, so {R} alone casts it only while Geist's static applies.
		p = costProbe{spell: "Lightning Strike", mana: "R", battlefield: []string{name}, targeted: true}
	case "Tam, the Possibility":
		p = costProbe{spell: "Jace Beleren", mana: "UU", battlefield: []string{name}}
	case "Ghalta the Immovable":
		p = costProbe{spell: name, mana: "CCCCW", hand: []string{name}, battlefield: []string{"Serra Angel"}}
	case "Ghalta the Unstoppable":
		p = costProbe{spell: name, mana: "CCCCG", hand: []string{name}, battlefield: []string{"Serra Angel"}}
	case "Traxos, Academy Guardian":
		first := oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:Shock", Mana: "R", Targets: []string{"p1"}}
		p = costProbe{spell: name, mana: "CU", hand: []string{name}, first: &first}
	case "Wrath of the Bloodmane":
		p = costProbe{spell: name, mana: "CR", hand: []string{name}, battlefield: []string{"Tam, the Possibility"}}
	default:
		var ok bool
		p, ok = parameterCostProbe(reg, f, name, idx)
		if !ok {
			reason := "cost static probe not supported"
			if strings.EqualFold(f.Statics[idx].Params["ValidCard"], "Card.Self") {
				reason += ": unsupported self-cost shape"
			}
			return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: reason}
		}
	}
	slots := oraclegen.SlotSpecs(f)
	fixtures := oraclegen.Fixtures(reg, slots)
	if len(fixtures) == 0 {
		fixtures = []oraclegen.Fixture{{}}
	}
	for _, fx := range fixtures {
		p0 := *fx.P0()
		p1 := *fx.P1()
		p0.Battlefield = appendUnique(p0.Battlefield, p.battlefield...)
		p0.Graveyard = appendUnique(p0.Graveyard, p.graveyard...)
		p0.Exile = appendUnique(p0.Exile, p.exile...)
		p0.Hand = append(p0.Hand, p.hand...)
		p0.Hand = appendUnique(p0.Hand, p.spell)
		if p.first != nil && p.first.Card == "p0:Shock" {
			p0.Hand = appendUnique(p0.Hand, "Shock")
		}
		sc := oraclegen.Scenario{
			Setup:        map[string]oraclegen.Seat{"p0": p0, "p1": p1},
			SetupAnswers: oraclegen.OpeningHandAnswers(f),
		}
		sc.Steps = append(sc.Steps, fx.CombatSteps()...)
		sc.Steps = append(sc.Steps, fx.Prelude()...)
		if p.first != nil {
			sc.Steps = append(sc.Steps, *p.first, oraclegen.Step{Op: "resolve"})
		}
		targets := fx.Targets()
		if p.targeted {
			targets = []string{"p1"}
		}
		if strings.Contains(f.Statics[idx].Params["ValidTarget"], "tapped") {
			for _, target := range targets {
				tapFixtureTarget(target, &p0, &p1)
			}
			sc.Setup["p0"], sc.Setup["p1"] = p0, p1
		}
		cast := oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:" + p.spell, Mana: p.mana, Targets: targets}
		sc.Steps = append(sc.Steps, cast)
		oraclegen.Baseline(sc.Setup, f)
		it := oraclegen.NewLevelBItem(name, req.Key, CostStatic.Version, []string{"601.2"}, sc)
		// Settle plays a temporary copy with resolve steps, so the target
		// rewrite is validated against a scenario whose stack empties. The
		// settled copy is what is kept: every cast carries gorge's exact
		// targets (cast steps' target decisions travel through castSpell and
		// are not scripted a second time). When gorge cannot cast at the
		// reduced price Settle fails and the unnormalized item is returned
		// as is, so the failed cast surfaces as a divergence, not a Skip.
		if n, res, ok := oraclegen.Settle(reg, sc); ok {
			settled := sc
			settled.Steps = append([]oraclegen.Step(nil), sc.Steps...)
			for i := 0; i < n; i++ {
				settled.Steps = append(settled.Steps, oraclegen.Step{Op: "resolve"})
			}
			settled, castSteps := oraclegen.ChooseTargets(settled, res.Decisions)
			if res2, ok2 := oraclegen.PlaysThrough(reg, settled); ok2 {
				settled.Name, settled.CR, settled.Why = it.Name, it.CR, it.Why
				it.Scenario = settled
				it.XAnswers = oraclegen.XAnswersForScenario(res2, settled, nil, castSteps)
			}
		}
		if p.mustReplay {
			if _, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok {
				return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "cost static probe not supported: prerequisite fixture unavailable"}
			}
		}
		return it, nil
	}
	return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "cost static has no fixture"}
}

// parameterCostProbe derives the simple own-spell cost shapes from the static
// itself. Unsupported conditions fail with a specific skip rather than
// silently guessing a fixture.
func parameterCostProbe(reg *cards.Registry, f *cards.Face, name string, idx int) (costProbe, bool) {
	st := f.Statics[idx]
	if !strings.EqualFold(st.Params["ValidCard"], "Card.Self") || !strings.EqualFold(st.Params["Type"], "Spell") || (st.Params["EffectZone"] != "" && !strings.EqualFold(st.Params["EffectZone"], "All")) {
		return costProbe{}, false
	}
	base, why := oraclegen.PoolFor(f.ManaCost)
	if why != "" {
		return costProbe{}, false
	}
	p := costProbe{spell: name, hand: []string{name}, mana: base}
	if st.ModeKind() == cards.StaticRaiseCost {
		var increase int
		if _, err := fmt.Sscanf(st.Params["Amount"], "%d", &increase); err == nil && increase > 0 {
			p.mana = strings.Repeat("C", increase) + base
		} else if st.Params["Amount"] == "IncreaseCost" && st.Params["Cost"] != "" {
			if extra, why := oraclegen.PoolFor(st.Params["Cost"]); why == "" {
				p.mana = extra + base
			}
		}
		if cost := st.Params["Cost"]; strings.Contains(cost, "BeholdExile<1/") {
			for typ, card := range beholdFixture {
				if strings.Contains(cost, "BeholdExile<1/"+typ+">") {
					p.hand = append(p.hand, card)
					break
				}
			}
		}
		return p, true
	}
	if st.ModeKind() != cards.StaticReduceCost {
		return costProbe{}, false
	}
	reduction := 0
	amount := st.Params["Amount"]
	switch {
	case strings.HasPrefix(st.Params["KeywordLine"], "Affinity:"):
		typ := strings.TrimPrefix(st.Params["KeywordLine"], "Affinity:")
		// A small, deterministic board turns affinity on without depending on
		// the corpus-specific maximum reduction.
		fixtures := affinityFixtures(reg, typ, 3)
		if len(fixtures) < 3 {
			return costProbe{}, false
		}
		p.battlefield = fixtures
		reduction = 3
	case strings.HasPrefix(amount, "__kwAffinity"):
		return costProbe{}, false
	case amount == "X" || amount == "Y" || amount == "Z":
		// Recognised count descriptions get a real matching zone/permanent
		// fixture; unknown formulas are not represented by a guessed 1.
		desc := strings.ToLower(st.Params["Description"])
		switch {
		case strings.Contains(desc, "instant and sorcery card in your graveyard"):
			p.graveyard = []string{"Opt", "Shock"}
		case strings.Contains(desc, "artifact and/or creature card in your graveyard"):
			p.graveyard = []string{"Silver Myr"}
		case strings.Contains(desc, "creature card you own in exile and in your graveyard"):
			p.graveyard = []string{"Grizzly Bears"}
		case strings.Contains(desc, "each color among permanents you control"):
			p.battlefield = []string{"Grizzly Bears", "Mischievous Snappers", "Goblin Piker"}
		case strings.Contains(desc, "greatest mana value among elementals you control"):
			p.battlefield = affinityFixtures(reg, "Elemental", 1)
			if len(p.battlefield) == 0 {
				return costProbe{}, false
			}
		case strings.Contains(desc, "each cave you control"):
			p.battlefield = affinityFixtures(reg, "Cave", 1)
			if len(p.battlefield) == 0 {
				return costProbe{}, false
			}
			p.graveyard = []string{"Wastes"}
		default:
			return costProbe{}, false
		}
		reduction = 1
	case amount != "":
		if _, err := fmt.Sscanf(amount, "%d", &reduction); err != nil || reduction < 1 {
			return costProbe{}, false
		}
	default:
		return costProbe{}, false
	}
	// Conditions are served only when this generator can establish their
	// truth from scenario state. Unknown condition grammars remain named gaps.
	if cond := strings.ToLower(st.Params["Condition"]); cond != "" {
		if cond == "delirium" {
			p.graveyard = appendUnique(p.graveyard, "Wastes", "Opt", "Grizzly Bears", "Silver Myr")
		} else {
			return costProbe{}, false
		}
	}
	if strings.Contains(strings.ToLower(st.Params["Description"]), "during your turn") {
		// The generated cast is p0's turn.
	} else if st.Params["CheckSVar"] != "" && st.Params["Amount"] != "X" && st.Params["Amount"] != "Y" && st.Params["Amount"] != "Z" {
		return costProbe{}, false
	}
	if st.Params["ValidSpell"] != "" {
		return costProbe{}, false
	}
	present := st.Params["IsPresent"]
	if present != "" {
		head := strings.SplitN(present, ".", 2)[0]
		fixtures := map[string]string{"Otter": "Mischievous Snappers", "Frog": "Frog Lizard", "Creature": "Grizzly Bears", "Artifact": "Silver Myr", "Kithkin": "Kithkin Greatheart", "land": "Forest", "Land": "Forest"}
		card := fixtures[head]
		if card != "" {
			p.battlefield = appendUnique(p.battlefield, card)
		}
	}
	if target := st.Params["ValidTarget"]; target != "" {
		// SlotSpecs/Fixtures provides the target objects for ValidTarget. Do not
		// replace those object refs with the player target used by legacy probes.
		if strings.Contains(target, "tapped") {
			p.battlefield = appendUnique(p.battlefield, "Grizzly Bears")
		}
	}
	if available := strings.Count(base, "C"); reduction > available {
		reduction = available
	}
	var ok bool
	p.mana, ok = removeGenericMana(base, reduction)
	if !ok {
		return costProbe{}, false
	}
	p.mustReplay = true
	return p, true
}

func affinityFixtures(reg *cards.Registry, typ string, count int) []string {
	// Sort by printed name so fixture selection does not depend on corpus
	// compilation order. Distinct permanents are required: appendUnique removes
	// duplicate names from setup and each permanent reduces the cost once.
	cardsInOrder := append([]*cards.Card(nil), reg.Cards...)
	firstName := func(c *cards.Card) string {
		if len(c.Faces) == 0 || c.Faces[0] == nil {
			return ""
		}
		return c.Faces[0].Name
	}
	sort.Slice(cardsInOrder, func(i, j int) bool {
		return firstName(cardsInOrder[i]) < firstName(cardsInOrder[j])
	})
	var fixtures []string
	for _, c := range cardsInOrder {
		if strings.HasPrefix(firstName(c), "A-") || strings.Contains(firstName(c), "\"") || strings.Contains(firstName(c), "'") {
			continue
		}
		if len(c.Faces) == 0 {
			continue
		}
		for _, face := range c.Faces {
			for _, cardType := range face.Types {
				if strings.EqualFold(cardType, typ) {
					fixtures = appendUnique(fixtures, face.Name)
					break
				}
			}
			if len(fixtures) == count {
				return fixtures
			}
		}
	}
	return fixtures
}

func removeGenericMana(pool string, n int) (string, bool) {
	for n > 0 {
		i := strings.IndexByte(pool, 'C')
		if i < 0 {
			return "", false
		}
		pool = pool[:i] + pool[i+1:]
		n--
	}
	return pool, true
}

func tapFixtureTarget(ref string, p0, p1 *oraclegen.Seat) {
	owner, card, ok := strings.Cut(ref, ":")
	if !ok {
		return
	}
	switch owner {
	case "p0":
		p0.Tapped = appendUnique(p0.Tapped, card)
	case "p1":
		p1.Tapped = appendUnique(p1.Tapped, card)
	}
}

func appendUnique(dst []string, names ...string) []string {
	for _, n := range names {
		found := false
		for _, old := range dst {
			if old == n {
				found = true
				break
			}
		}
		if !found {
			dst = append(dst, n)
		}
	}
	return dst
}
