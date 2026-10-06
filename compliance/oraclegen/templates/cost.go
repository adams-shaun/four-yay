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
	spell, mana         string
	battlefield         []string
	opponentBattlefield []string
	graveyard           []string
	exile               []string
	hand                []string
	first               *oraclegen.Step
	firstNoResolve      bool
	// pre are setup-time steps (an attach) run before the probe step.
	pre []oraclegen.Step
	// preXAbility is parallel to pre: the XMage rule-text prefix of a prelude
	// activate step (a Class level-up), "" on every other prelude step.
	preXAbility []string
	// seat adjusts p0's setup after the permanents are placed (a counter).
	seat func(*oraclegen.Seat)
	// activate makes the probe step an activation of one of the probe
	// permanent's abilities instead of a cast.
	activate *costActivation
	// targeted offers the probe cast a surplus player target; gorge's own
	// target decision rewrites it to the exact pick before the item is kept.
	targeted bool
	targets  []string
	// precast is a spell p0 casts and holds priority over, so a probe that
	// must target a spell on the stack (a counterspell, or a discount whose
	// ValidTarget$ names Spell.*) has a legal stack target. It is cast with
	// no pass in between (CR 117.3c), exactly counterSpell's own-priority
	// shape.
	precast *precast
	// castMode elects a cast option by its Mode ("bargained" for Bargain);
	// answers scripts the mid-cast asks that election poses (the sacrifice).
	castMode   string
	answers    []oraclegen.Answer
	mustReplay bool
	skipReason string
	// full is the probe's printed price, set when the probe is a candidate
	// chosen from the corpus: a candidate gorge pays in full but refuses at the
	// reduced price is a reduction gorge does not apply, which must surface as a
	// divergence, not as a skipped row.
	full     string
	opponent bool
}

func costStatic(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	idx, err := strconv.Atoi(req.Slot)
	if err != nil || idx < 0 || idx >= len(f.Statics) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "cost static index " + req.Slot}
	}
	st := f.Statics[idx]
	// Each profile chooses a simple card whose printed cost makes the
	// reduction visible and supplies only the prerequisites the static needs.
	var cands []costProbe
	switch name {
	case "Geist of Saint Thalia":
		// Lightning Strike is {1}{R}: the generic {1} is what the reduction
		// removes, so {R} alone casts it only while Geist's static applies.
		cands = []costProbe{{spell: "Lightning Strike", mana: "R", battlefield: []string{name}, targeted: true}}
	case "Tam, the Possibility":
		cands = []costProbe{{spell: "Jace Beleren", mana: "UU", battlefield: []string{name}}}
	case "Ghalta the Immovable":
		cands = []costProbe{{spell: name, mana: "CCCCW", hand: []string{name}, battlefield: []string{"Serra Angel"}}}
	case "Ghalta the Unstoppable":
		cands = []costProbe{{spell: name, mana: "CCCCG", hand: []string{name}, battlefield: []string{"Serra Angel"}}}
	case "Traxos, Academy Guardian":
		first := oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:Shock", Mana: "R", Targets: []string{"p1"}}
		cands = []costProbe{{spell: name, mana: "CU", hand: []string{name}, first: &first}}
	case "Wrath of the Bloodmane":
		cands = []costProbe{{spell: name, mana: "CR", hand: []string{name}, battlefield: []string{"Tam, the Possibility"}}}
	case "Thalia, the Survivor":
		cands = []costProbe{{spell: "Shock", mana: "CR", battlefield: []string{name}, targeted: true, opponent: true}}
	case "Terror of the Peaks":
		cands = []costProbe{{spell: "Shock", mana: "R", battlefield: []string{name}, targeted: true, opponent: true}}
	default:
		ps, pgap, handled := parameterCostProbes(reg, f, name, idx)
		if len(ps) > 0 {
			cands = ps
			break
		}
		if handled && pgap != "" {
			return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: costStaticGap(f.Statics[idx], pgap)}
		}
		var gap string
		if cands, gap = otherCostProbes(reg, f, name, idx); len(cands) == 0 {
			return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: costStaticGap(f.Statics[idx], gap)}
		}
	}
	slots := oraclegen.SlotSpecs(f)
	fixtures := oraclegen.Fixtures(reg, slots)
	if len(fixtures) == 0 {
		fixtures = []oraclegen.Fixture{{}}
	}
	// The first fixture is the one every probe uses. A probe the item must
	// replay (mustReplay) is kept only when gorge can cast or activate it at
	// the reduced price; the next candidate is tried otherwise.
	for _, p := range cands {
		it := costProbeItem(reg, f, name, req, idx, p, fixtures[0])
		if p.mustReplay {
			if _, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok {
				continue
			}
		}
		return it, nil
	}
	for _, p := range cands {
		if p.full == "" {
			continue
		}
		fullPrice := p
		fullPrice.mana = p.full
		if _, ok := oraclegen.PlaysThrough(reg, costProbeItem(reg, f, name, req, idx, fullPrice, fixtures[0]).Scenario); ok {
			return costProbeItem(reg, f, name, req, idx, p, fixtures[0]), nil
		}
	}
	if len(cands) > 0 && cands[0].mustReplay {
		reason := cands[0].skipReason
		if reason == "" {
			switch {
			case st.Params["Cost"] != "" && st.ModeKind() == cards.StaticRaiseCost:
				reason = "raise cost payment " + st.Params["Cost"]
			case st.Params["ValidTarget"] != "":
				reason = "target fixture unavailable (" + st.Params["ValidTarget"] + ")"
			case st.Params["ValidSpell"] != "":
				reason = "spell fixture unavailable (" + st.Params["ValidSpell"] + ")"
			default:
				reason = "reduced-cost cast fixture unavailable"
			}
		}
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "cost static probe not supported: " + reason}
	}
	return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "cost static has no fixture"}
}

// parameterCostProbes derives the own-spell cost shapes from the static
// itself. handled is false for a static that is not an own-spell cost shape;
// a handled static with no probe carries the named gap that says why.
func parameterCostProbes(reg *cards.Registry, f *cards.Face, name string, idx int) (probes []costProbe, gap string, handled bool) {
	st := f.Statics[idx]
	if !strings.EqualFold(st.Params["ValidCard"], "Card.Self") || !strings.EqualFold(st.Params["Type"], "Spell") || (st.Params["EffectZone"] != "" && !strings.EqualFold(st.Params["EffectZone"], "All")) {
		return nil, "", false
	}
	base, why := oraclegen.PoolFor(f.ManaCost)
	if why != "" {
		return nil, "", false
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
			if strings.Contains(cost, "BeholdExile<1/Elf>") {
				p.hand = appendUnique(p.hand, elfBeholdFixture(reg)...)
			}
		}
		// An additional cost the generator cannot pay (Waterbend, Blight,
		// ChooseCard, a behold type with no fixture) must not become a probe
		// whose precondition is false.
		p.mustReplay = true
		return []costProbe{p}, "", true
	}
	if st.ModeKind() != cards.StaticReduceCost {
		return nil, "", false
	}
	if st.Params["ValidSpell"] != "" && !strings.EqualFold(st.Params["ValidSpell"], "Spell.Bargain") {
		return nil, "", false
	}
	if strings.EqualFold(st.Params["ValidSpell"], "Spell.Bargain") {
		// Bargain (CR 702.166) is a cast MODE: elect it, then answer the
		// mid-cast choose that names the artifact to sacrifice. Ornithopter
		// is a token-free artifact in p0's setup. A bargained counter still
		// needs a spell on the stack to target (Ice Out).
		p.battlefield = appendUnique(p.battlefield, "Ornithopter")
		p.castMode = "bargained"
		p.answers = append(p.answers, oraclegen.Answer{Kind: "choose", Pick: []string{"Ornithopter"}})
		if filter := stackTargetFilter(f); filter != "" {
			pre, reason := costStackPrecast(filter)
			if reason != "" {
				return nil, reason, true
			}
			p.precast = pre
		}
	}
	// The amount, the count it tallies and every gate come from the static's
	// own parameters (costConditionProbes); unknown grammars are named gaps.
	probes, reduction, gap := costConditionProbes(reg, f, st, name, p)
	if gap != "" {
		return nil, gap, true
	}
	if target := st.Params["ValidTarget"]; target != "" {
		if strings.Contains(target, "tapped") {
			for i := range probes {
				probes[i].battlefield = appendUnique(probes[i].battlefield, "Grizzly Bears")
			}
		} else {
			for i := range probes {
				var targetGap string
				probes[i], targetGap = costTargetProbe(reg, f, st, probes[i])
				if targetGap != "" {
					return nil, targetGap, true
				}
			}
		}
	}
	if available := strings.Count(base, "C"); reduction > available {
		reduction = available
	}
	mana, ok := removeGenericMana(base, reduction)
	if !ok {
		return nil, "", false
	}
	for i := range probes {
		probes[i].mana = mana
		probes[i].mustReplay = true
	}
	return probes, "", true
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
		if !oraclegen.XMageKnown(firstName(c)) || strings.Contains(firstName(c), "'") {
			continue
		}
		if len(c.Faces) == 0 {
			continue
		}
		for _, face := range c.Faces {
			if !oraclegen.XMageKnown(face.Name) {
				continue
			}
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
