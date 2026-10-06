package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// CounterSpell casts a spell of our own, then counters it with the card
// while holding priority (CR 117.3c), and resolves.
var CounterSpell = Template{ID: "counter-spell", Version: 1}

// targetsSpell reports whether the card counters a spell on the stack: a
// Counter ability anywhere along a spell ability's chain (the top ability or
// a SubAbility) whose target names the stack. A card whose leading ability
// is not a Counter (Swallowed by Leviathan surveils, then counters) still
// belongs on the counter path.
func targetsSpell(f *cards.Face) bool {
	for _, sa := range f.Abilities {
		if sa.Kind != "SP" {
			continue
		}
		for s := sa; s != nil; s = s.Sub {
			if s.API == "Counter" && s.Params["ValidTgts"] != "" && oraclegen.AbilityTargetsStack(s.Params) {
				return true
			}
		}
		return false
	}
	return false
}

type precast struct {
	card, mana string
	targets    []string
	// types are the card's spell types (lower-case), used to match a stack
	// slot's filter (an "instant or sorcery" target wants Shock, not Grizzly
	// Bears); singleTarget marks a spell with exactly one target, the shape
	// a TargetType$ SpellAbility.singleTarget slot needs.
	types        []string
	singleTarget bool
	spell        bool
}

// precasts are the spells a counterspell scenario counters, one per
// colour and type a counter's filter commonly names.
var precasts = []precast{
	{card: "Disfigure", mana: "B", targets: []string{"p1:Grizzly Bears"}, types: []string{"instant"}, singleTarget: true, spell: true},
	{card: "Raise the Alarm", mana: "CW", types: []string{"instant"}, spell: true},
	{card: "Shock", mana: "R", targets: []string{"p1"}, types: []string{"instant"}, singleTarget: true, spell: true},
	{card: "Opt", mana: "U", types: []string{"instant"}, spell: true},
	{card: "Giant Growth", mana: "G", targets: []string{"p1:Grizzly Bears"}, types: []string{"instant"}, singleTarget: true, spell: true},
	{card: "Grizzly Bears", mana: "CG", types: []string{"creature"}, spell: true},
	{card: "Ornithopter", types: []string{"artifact", "creature"}, spell: true},
	{card: "Serra Angel", mana: "CCCWW", types: []string{"creature"}, spell: true},
}

// precastFits reports whether a precast can stand in for a stack slot with
// this filter: an "instant or sorcery" target needs such a spell, a creature
// target a creature spell, and a single-target slot a spell that targets
// exactly one object.
func precastFits(filter string, p precast) bool {
	if oraclegen.SlotIsStack(filter) && !p.spell {
		return false
	}
	f := strings.ToLower(filter)
	if strings.Contains(f, "instant") && !hasPrecastType(p, "instant") && !hasPrecastType(p, "sorcery") {
		return false
	}
	if strings.Contains(f, "sorcery") && !hasPrecastType(p, "sorcery") && !hasPrecastType(p, "instant") {
		// A filter naming both instant and sorcery is satisfied by either,
		// handled by the instant branch above; a sorcery-only filter wants a
		// sorcery.
		if !strings.Contains(f, "instant") {
			return false
		}
	}
	if strings.Contains(f, "singleTarget") && !p.singleTarget {
		return false
	}
	base := strings.ToLower(strings.SplitN(strings.Split(filter, "@")[0], ",", 2)[0])
	base = strings.SplitN(strings.TrimSpace(base), ".", 2)[0]
	if base == "creature" && !hasPrecastType(p, "creature") {
		return false
	}
	return true
}

func hasPrecastType(p precast, t string) bool {
	for _, x := range p.types {
		if x == t {
			return true
		}
	}
	return false
}

func counterSpell(reg *cards.Registry, f *cards.Face, name, mana string) (oraclegen.Item, *oraclegen.Skip) {
	// The extra {1} covers an optional additional cost ("behold or pay
	// {1}"), tried only when the bare cost cannot cast.
	xAns := xAnswers(f)
	slots := oraclegen.SlotSpecs(f)
	stackIdx := stackSlotIndexes(slots)
	plain := nonStackSlots(slots)
	for _, m := range []string{mana, mana + "C", mana + "CC", mana + "CCC"} {
		for _, pre := range precasts {
			if !precastFitsSlots(slots, stackIdx, pre) {
				continue
			}
			for _, fx := range oraclegen.Fixtures(reg, plain) {
				if it, ok := counterWith(reg, f, name, m, pre, fx, slots, stackIdx, xAns); ok {
					return it, nil
				}
			}
		}
	}
	return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "no spell fixture gorge can counter"}
}

func counterWith(reg *cards.Registry, f *cards.Face, name, mana string, pre precast, fx oraclegen.Fixture, slots []oraclegen.Slot, stackIdx []int, answers []oraclegen.Answer) (oraclegen.Item, bool) {
	p0, p1 := *fx.P0(), *fx.P1()
	// The counterspell first in hand, the spell it counters second: the
	// order main's fixture always used, so the scenario stays the same.
	p0 = oraclegen.WithHand(oraclegen.WithHand(p0, pre.card), physicalName(reg, name))
	targets := insertStackTargets(slots, stackIdx, fx.Targets(), pre)
	sc := oraclegen.Scenario{
		Setup: map[string]oraclegen.Seat{"p0": p0, "p1": p1},
		Steps: []oraclegen.Step{
			{Op: "cast", Seat: 0, Card: "p0:" + pre.card, Mana: pre.mana, Targets: pre.targets},
			{Op: "cast", Seat: 0, Card: "p0:" + name, Mana: mana, Targets: targets, Answers: answers},
			{Op: "resolve"},
		},
	}
	oraclegen.Baseline(sc.Setup, f)
	it := CounterSpell.item(f, name, sc)
	sc = it.Scenario
	res, ok := oraclegen.ProbeTargets(reg, sc)
	if !ok {
		return oraclegen.Item{}, false
	}
	// Rewrite the cast steps' targets to gorge's own picks (the fixture
	// over-offers) and verify the rewrite replays cleanly.
	sc, castSteps := oraclegen.ChooseTargets(sc, res.Decisions)
	res, ok = oraclegen.PlaysThrough(reg, sc)
	if !ok {
		return oraclegen.Item{}, false
	}
	it.Scenario = sc
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), castSteps)
	return it, true
}
