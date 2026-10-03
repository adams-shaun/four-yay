package templates

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// CastResolve casts the spell with exactly its mana in pool, with legal
// targets on the board, and resolves the stack.
var CastResolve = Template{ID: "cast-resolve", Version: 1}

// xAnswers scripts X for a spell with X in its cost.
func xAnswers(f *cards.Face) []oraclegen.Answer {
	if strings.Contains(" "+f.ManaCost+" ", " X ") {
		return []oraclegen.Answer{{Kind: "choose", Pick: []string{fmt.Sprintf("X = %d", oraclegen.XValue)}}}
	}
	return nil
}

func castResolve(reg *cards.Registry, f *cards.Face, name, mana string) (oraclegen.Item, *oraclegen.Skip) {
	// A charm is generated mode by mode: the first mode some fixture can
	// cast, with the mode scripted so both engines take it.
	type plan struct {
		slots   []string
		answers []oraclegen.Answer
	}
	xAns := xAnswers(f)
	plans := []plan{{slots: oraclegen.TargetSlots(f), answers: xAns}}
	if modes := oraclegen.CharmModes(f); len(modes) > 0 {
		plans = nil
		for _, m := range modes {
			plans = append(plans, plan{slots: oraclegen.ChainSlots(f, m.SVar()), answers: append([]oraclegen.Answer{{Kind: "modes", Pick: []string{m.Label()}}}, xAns...)})
		}
	}
	for _, m := range []string{mana, mana + "C"} {
		for _, pl := range plans {
			if it, ok := castWith(reg, f, name, m, pl.slots, pl.answers); ok {
				return it, nil
			}
		}
	}
	return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: fmt.Sprintf("no fixture gorge can cast (targets %v)", plans[0].slots)}
}

// castWith tries every fixture for one target plan.
func castWith(reg *cards.Registry, f *cards.Face, name, mana string, slots []string, answers []oraclegen.Answer) (oraclegen.Item, bool) {
	// Extras satisfy casting conditions the target fixture does not: a
	// threshold graveyard, a creature of your own to sacrifice for a cost.
	extras := []func(*oraclegen.Fixture){
		func(*oraclegen.Fixture) {},
		func(fx *oraclegen.Fixture) {
			fx.P0().Graveyard = append(fx.P0().Graveyard, oraclegen.Repeat("Wastes", 7)...)
		},
		func(fx *oraclegen.Fixture) { fx.P0().Battlefield = append(fx.P0().Battlefield, "Llanowar Elves") },
	}
	var all []oraclegen.Fixture
	for _, extra := range extras {
		for _, fx := range oraclegen.Fixtures(slots) {
			extra(&fx)
			all = append(all, fx)
		}
	}
	for _, fx := range all {
		sc := oraclegen.Scenario{
			Setup: map[string]oraclegen.Seat{"p0": *fx.P0(), "p1": *fx.P1()},
			Steps: []oraclegen.Step{{Op: "cast", Seat: 0, Card: "p0:" + name, Mana: mana, Targets: fx.Targets(), Answers: answers}},
		}
		sc.Setup["p0"] = oraclegen.WithHand(sc.Setup["p0"], name)
		oraclegen.Baseline(sc.Setup, f)
		if n, res, ok := oraclegen.Settle(reg, sc); ok {
			for i := 0; i < n; i++ {
				sc.Steps = append(sc.Steps, oraclegen.Step{Op: "resolve"})
			}
			// "May" is answered yes (spec section 6): a declined optional
			// pick is re-scripted to take the first offered option, and the
			// scenario kept only if gorge still plays it through.
			if yes, changed := oraclegen.MayYes(sc, res.Decisions); changed {
				if res2, ok2 := oraclegen.PlaysThrough(reg, yes); ok2 {
					sc, res = yes, res2
				}
			}
			it := CastResolve.item(name, sc)
			it.XAnswers = oraclegen.XAnswers(res.Decisions, len(sc.Steps), oraclegen.ModeNumbers(f))
			if oraclegen.SearchesLibrary(f) || strings.Contains(strings.ToLower(f.Oracle), "shuffle") {
				it.Ignore = []string{"library_top"}
			}
			return it, true
		}
	}
	return oraclegen.Item{}, false
}
