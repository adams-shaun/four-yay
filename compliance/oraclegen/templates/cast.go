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
	for _, m := range []string{mana, mana + "C", mana + "CC", mana + "CCC"} {
		for _, pl := range plans {
			if it, ok := castWith(reg, f, name, m, pl.slots, pl.answers); ok {
				return it, nil
			}
		}
	}
	return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: fmt.Sprintf("no fixture gorge can cast (targets %v)", plans[0].slots)}
}

// castWith tries every fixture for one target plan. A slot that targets the
// stack needs a spell on the stack to point at, so such a plan also casts a
// precast spell first (CR 117.3c: the caster keeps priority and responds),
// exactly as the counter template does.
func castWith(reg *cards.Registry, f *cards.Face, name, mana string, slots []string, answers []oraclegen.Answer) (oraclegen.Item, bool) {
	// Extras satisfy casting conditions the target fixture does not: a
	// threshold graveyard, a creature of your own to sacrifice for a cost.
	extras := []func(*oraclegen.Fixture){
		func(*oraclegen.Fixture) {},
		func(fx *oraclegen.Fixture) {
			fx.P0().Graveyard = append(fx.P0().Graveyard, oraclegen.Repeat("Wastes", 7)...)
		},
		func(fx *oraclegen.Fixture) { fx.P0().Battlefield = append(fx.P0().Battlefield, "Llanowar Elves") },
		// A spare card in hand pays an additional "discard a card" cost
		// (Seize the Spoils, Demand Answers): the spell itself is on the
		// stack by then, so the discard needs a second card.
		func(fx *oraclegen.Fixture) { fx.P0().Hand = append(fx.P0().Hand, "Forest") },
	}
	stackIdx := stackSlotIndexes(slots)
	plain := nonStackSlots(slots)
	pres := []precast{{}}
	if len(stackIdx) > 0 {
		pres = nil
		for _, p := range precasts {
			if precastFitsSlots(slots, stackIdx, p) {
				pres = append(pres, p)
			}
		}
	}
	for _, pre := range pres {
		for _, extra := range extras {
			for _, fx := range oraclegen.Fixtures(plain) {
				extra(&fx)
				sc := buildStackScenario(f, name, mana, pre, fx, slots, stackIdx, answers)
				if n, res, ok := oraclegen.Settle(reg, sc); ok {
					for i := 0; i < n; i++ {
						sc.Steps = append(sc.Steps, oraclegen.Step{Op: "resolve"})
					}
					if yes, changed := oraclegen.MayYes(sc, res.Decisions); changed {
						if res2, ok2 := oraclegen.PlaysThrough(reg, yes); ok2 {
							sc, res = yes, res2
						}
					}
					it := CastResolve.item(name, sc)
					it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f))
					if oraclegen.SearchesLibrary(f) || strings.Contains(strings.ToLower(f.Oracle), "shuffle") {
						it.Ignore = []string{"library_top"}
					}
					return it, true
				}
			}
		}
	}
	return oraclegen.Item{}, false
}

// buildStackScenario builds the scenario for one fixture: the precast spell
// (if any) is cast first, then the card under test with its targets placed in
// slot order (a stack slot points at the precast spell on the stack).
func buildStackScenario(f *cards.Face, name, mana string, pre precast, fx oraclegen.Fixture, slots []string, stackIdx []int, answers []oraclegen.Answer) oraclegen.Scenario {
	targets := insertStackTargets(slots, stackIdx, fx.Targets(), pre)
	sc := oraclegen.Scenario{
		Setup:        map[string]oraclegen.Seat{"p0": *fx.P0(), "p1": *fx.P1()},
		SetupAnswers: oraclegen.OpeningHandAnswers(f),
		Steps:        []oraclegen.Step{{Op: "cast", Seat: 0, Card: "p0:" + name, Mana: mana, Targets: targets, Answers: answers}},
	}
	sc.Setup["p0"] = oraclegen.WithHand(sc.Setup["p0"], name)
	if pre.card != "" {
		sc.Setup["p0"] = oraclegen.WithHand(sc.Setup["p0"], pre.card)
		cast := oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:" + pre.card, Mana: pre.mana, Targets: pre.targets}
		sc.Steps = append([]oraclegen.Step{cast}, sc.Steps...)
	}
	// A target filter naming an attacking or blocking creature needs combat
	// arranged before the cast: p0 declares the attacker and, for a
	// "blocking" filter, p1 declares the block. The cast then happens in
	// the declare-blockers step, where an instant is legal.
	//
	// The block follows the attack directly (the block op advances to the
	// blockers decision itself). A pass_to step is deliberately NOT emitted:
	// it would snapshot gorge's pre-block declare-blockers state, and XMage
	// selects blockers in DeclareBlockersStep.beginStep before any player
	// gets priority, so its driver has no equivalent checkpoint to report.
	if combat := fx.CombatSteps(); len(combat) != 0 {
		sc.Steps = append(combat, sc.Steps...)
	}
	oraclegen.Baseline(sc.Setup, f)
	return sc
}

// stackSlotIndexes lists the positions in slots that draw from the stack.
func stackSlotIndexes(slots []string) []int {
	var out []int
	for i, s := range slots {
		if oraclegen.SlotIsStack(s) {
			out = append(out, i)
		}
	}
	return out
}

// nonStackSlots keeps every slot that does not draw from the stack.
func nonStackSlots(slots []string) []string {
	var out []string
	for _, s := range slots {
		if !oraclegen.SlotIsStack(s) {
			out = append(out, s)
		}
	}
	return out
}

// insertStackTargets interleaves plain-slot targets with a stack slot's ref
// to the precast spell, preserving slot order.
func insertStackTargets(slots []string, stackIdx []int, plain []string, pre precast) []string {
	out := make([]string, 0, len(slots))
	pi := 0
	for i := range slots {
		if containsInt(stackIdx, i) {
			out = append(out, "p0:"+pre.card)
			continue
		}
		if pi < len(plain) {
			out = append(out, plain[pi])
			pi++
		}
	}
	return out
}

func containsInt(xs []int, x int) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// precastFitsSlots reports whether one precast can satisfy every stack slot.
func precastFitsSlots(slots []string, stackIdx []int, p precast) bool {
	for _, i := range stackIdx {
		if !precastFits(slots[i], p) {
			return false
		}
	}
	return true
}
