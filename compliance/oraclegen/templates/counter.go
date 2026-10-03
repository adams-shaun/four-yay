package templates

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// CounterSpell casts a spell of our own, then counters it with the card
// while holding priority (CR 117.3c), and resolves.
var CounterSpell = Template{ID: "counter-spell", Version: 1}

// targetsSpell reports whether the spell's first target is a spell on the
// stack (TargetType$ Spell, a Counter).
func targetsSpell(f *cards.Face) bool {
	for _, sa := range f.Abilities {
		if sa.Kind == "SP" {
			return sa.Params["TargetType"] == "Spell" || (sa.API == "Counter" && sa.Params["ValidTgts"] != "")
		}
	}
	return false
}

type precast struct {
	card, mana string
	targets    []string
}

// precasts are the spells a counterspell scenario counters, one per
// colour and type a counter's filter commonly names.
var precasts = []precast{
	{"Disfigure", "B", []string{"p1:Grizzly Bears"}},
	{"Raise the Alarm", "CW", nil},
	{"Shock", "R", []string{"p1"}},
	{"Opt", "U", nil},
	{"Giant Growth", "G", []string{"p1:Grizzly Bears"}},
	{"Grizzly Bears", "CG", nil},
	{"Ornithopter", "", nil},
	{"Serra Angel", "CCCWW", nil},
}

func counterSpell(reg *cards.Registry, f *cards.Face, name, mana string) (oraclegen.Item, *oraclegen.Skip) {
	// The extra {1} covers an optional additional cost ("behold or pay
	// {1}"), tried only when the bare cost cannot cast.
	xAns := xAnswers(f)
	for _, m := range []string{mana, mana + "C"} {
		for _, pre := range precasts {
			if it, ok := counterWith(reg, f, name, m, pre, xAns); ok {
				return it, nil
			}
		}
	}
	return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "no spell fixture gorge can counter"}
}

func counterWith(reg *cards.Registry, f *cards.Face, name, mana string, pre precast, answers []oraclegen.Answer) (oraclegen.Item, bool) {
	sc := oraclegen.Scenario{
		Setup: map[string]oraclegen.Seat{"p0": {Hand: []string{name, pre.card}}, "p1": {}},
		Steps: []oraclegen.Step{
			{Op: "cast", Seat: 0, Card: "p0:" + pre.card, Mana: pre.mana, Targets: pre.targets},
			{Op: "cast", Seat: 0, Card: "p0:" + name, Mana: mana, Targets: []string{"p0:" + pre.card}, Answers: answers},
			{Op: "resolve"},
		},
	}
	oraclegen.Baseline(sc.Setup, f)
	res, ok := oraclegen.PlaysThrough(reg, sc)
	if !ok {
		return oraclegen.Item{}, false
	}
	it := CounterSpell.item(name, sc)
	it.XAnswers = oraclegen.XAnswers(res.Decisions, len(sc.Steps), oraclegen.ModeNumbers(f))
	return it, true
}
