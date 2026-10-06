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
	if strings.Contains(strings.ToLower(f.Oracle), "blight x") {
		// The fixture's only guaranteed creature is Llanowar Elves (1/1),
		// which bounds Soul Immolation's payable blight value to one.
		return []oraclegen.Answer{{Kind: "choose", Pick: []string{"X = 1"}}}
	}
	if strings.Contains(" "+f.ManaCost+" ", " X ") {
		x := oraclegen.XValue
		// A TargetMin$ X spell needs X distinct candidates before payment.
		// The generic one-slot fixture guarantees one permanent, not two.
		for _, sa := range f.Abilities {
			if sa.Kind == "SP" && sa.Params["TargetMin"] == "X" {
				x = 1
				break
			}
		}
		return []oraclegen.Answer{{Kind: "choose", Pick: []string{fmt.Sprintf("X = %d", x)}}}
	}
	return nil
}

func castResolve(reg *cards.Registry, f *cards.Face, name, mana string) (oraclegen.Item, *oraclegen.Skip) {
	return castResolveWith(reg, f, name, mana, nil)
}

// castResolveWith is castResolve with probe permanents added to p0's
// battlefield in every fixture it tries. nil probes is castResolve exactly.
func castResolveWith(reg *cards.Registry, f *cards.Face, name, mana string, probes []string) (oraclegen.Item, *oraclegen.Skip) {
	// Charm plans enumerate legal mode combinations in Choices$ order. Each
	// plan carries all selected chains and one answer containing every pick.
	type plan struct {
		slots   []oraclegen.Slot
		answers []oraclegen.Answer
	}
	xAns := xAnswers(f)
	plans := []plan{{slots: oraclegen.SlotSpecs(f), answers: xAns}}
	if combos := oraclegen.CharmCombinations(f); len(combos) > 0 {
		plans = nil
		for _, combo := range combos {
			labels := make([]string, 0, len(combo.Modes))
			usable := true
			for _, mode := range combo.Modes {
				if attachesOnReturn(f, mode.SVar()) {
					// XMage asks which creature the returned Aura/Equipment
					// attaches to, an ask the scenario cannot script yet.
					usable = false
					break
				}
				labels = append(labels, mode.Label())
			}
			if usable {
				answers := append([]oraclegen.Answer{{Kind: "modes", Pick: labels}}, xAns...)
				plans = append(plans, plan{slots: combo.Slots, answers: answers})
			}
		}
	}
	manas := []string{mana, mana + "C", mana + "CC", mana + "CCC"}
	if hasWaterbendCost(f) {
		manas = append(manas, mana+"CCCC", mana+"CCCCC")
	}
	for _, m := range manas {
		for _, pl := range plans {
			if it, ok := castWithProbes(reg, f, name, m, pl.slots, pl.answers, probes); ok {
				return it, nil
			}
		}
	}
	var targets []string
	if len(plans) > 0 {
		targets = filterStrings(plans[0].slots)
	}
	return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: fmt.Sprintf("no fixture gorge can cast (targets %v)", targets)}
}

func filterStrings(slots []oraclegen.Slot) []string {
	out := make([]string, 0, len(slots))
	for _, s := range slots {
		out = append(out, s.Filter)
	}
	return out
}

// castWith tries every fixture for one target plan. A slot that targets the
// stack needs a spell on the stack to point at, so such a plan also casts a
// precast spell first (CR 117.3c: the caster keeps priority and responds),
// exactly as the counter template does.
func castWith(reg *cards.Registry, f *cards.Face, name, mana string, slots []oraclegen.Slot, answers []oraclegen.Answer) (oraclegen.Item, bool) {
	return castWithProbes(reg, f, name, mana, slots, answers, nil)
}

// castWithProbes is castWith with probe permanents added to p0's battlefield
// after each fixture extra, so a static the card brings is observable on
// them. nil probes is castWith exactly.
func castWithProbes(reg *cards.Registry, f *cards.Face, name, mana string, slots []oraclegen.Slot, answers []oraclegen.Answer, probes []string) (oraclegen.Item, bool) {
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
	if beholdType := requiredBeholdType(f); beholdType != "" {
		if candidate := beholdFixture[beholdType]; candidate != "" {
			extras = append(extras, func(fx *oraclegen.Fixture) {
				fx.P0().Hand = appendFixtureUnique(fx.P0().Hand, candidate)
			})
		}
	}
	if tapCount := requiredTapCount(f); tapCount > 0 {
		extras = append(extras, func(fx *oraclegen.Fixture) {
			for _, permanent := range []string{"Llanowar Elves", "Forest", "Ornithopter", "Wastes"}[:tapCount] {
				fx.P0().Battlefield = appendFixtureUnique(fx.P0().Battlefield, permanent)
			}
		})
	}
	// Collect evidence X pays the total mana value of the selected targets.
	// Supply enough graveyard mana value for a four-slot cast rather than
	// treating a reversed payment as an empty-stack success.
	for _, sa := range f.Abilities {
		if sa.Kind == "SP" && strings.Contains(sa.Params["Cost"], "CollectEvidence<X>") {
			extras = append(extras, func(fx *oraclegen.Fixture) {
				fx.P0().Graveyard = append(fx.P0().Graveyard, "Serra Angel", "Hill Giant", "Grizzly Bears")
			})
			break
		}
	}
	// An additional cost that sacrifices a creature whose damage provenance
	// the board must already carry (Treacherous Greed: "sacrifice a creature
	// that dealt damage this turn"). Setup cannot stamp dealt-damage
	// provenance, so the fixture puts a creature under the caster and has it
	// attack unblocked, passing to end of combat so the combat damage lands
	// before the cast in the post-combat main phase.
	if sacrificesDamageDealtCreature(f) {
		extras = append(extras, func(fx *oraclegen.Fixture) {
			fx.P0().Battlefield = appendFixtureUnique(fx.P0().Battlefield, "Grizzly Bears")
			fx.AddPrelude(
				oraclegen.Step{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:Grizzly Bears"}},
				oraclegen.Step{Op: "pass_to", Step: "end-combat"},
			)
		})
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
			for _, fx := range oraclegen.Fixtures(reg, plain) {
				extra(&fx)
				for _, probe := range probes {
					fx.P0().Battlefield = appendFixtureUnique(fx.P0().Battlefield, probe)
				}
				sc := buildStackScenario(f, name, physicalName(reg, name), mana, pre, fx, slots, stackIdx, answers)
				if oraclegen.ShufflesBackAndDraws(f) {
					oraclegen.UniformShuffleSetup(&sc, name)
				}
				if n, res, ok := oraclegen.Settle(reg, sc); ok {
					for i := 0; i < n; i++ {
						sc.Steps = append(sc.Steps, oraclegen.Step{Op: "resolve"})
					}
					// The fixture over-offers targets; rewrite each cast step to
					// exactly gorge's picks (the target decisions) so XMage's
					// castSpell sees a target list that matches the ability, and
					// verify the rewrite replays cleanly.
					sc, castSteps := oraclegen.ChooseTargets(sc, res.Decisions)
					res, ok = oraclegen.PlaysThrough(reg, sc)
					if !ok {
						continue
					}
					if yes, changed := oraclegen.MayYes(sc, res.Decisions); changed {
						if res2, ok2 := oraclegen.PlaysThrough(reg, yes); ok2 {
							sc, res = yes, res2
						}
					}
					it := CastResolve.item(name, sc)
					it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), castSteps)
					if n := oraclegen.OptionalCostCastNo(f, mana); n > 0 {
						// XMage asks "pay the additional cost?" at the head of the
						// cast; gorge offered it as a declineable cast option, so
						// answer the ask explicitly.
						it.XAnswers = oraclegen.PrependCastNo(it.XAnswers, sc, name, n)
					}
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

// These fixture cards provide the creature type required by the named
// BeholdExile costs. Keep this data explicit so the fixture is type-correct.
var beholdFixture = map[string]string{
	"Kithkin":   "Kithkin Greatheart",
	"Elemental": "Mulldrifter",
	"Goblin":    "Goblin Guide",
	"Merfolk":   "Vodalian Merchant",
}

func requiredBeholdType(f *cards.Face) string {
	for _, st := range f.Statics {
		cost := st.Params["Cost"]
		if i := strings.Index(cost, "BeholdExile<1/"); i >= 0 {
			tail := cost[i+len("BeholdExile<1/"):]
			if j := strings.IndexByte(tail, '>'); j >= 0 {
				return tail[:j]
			}
		}
	}
	return ""
}

// sacrificesDamageDealtCreature reports whether the spell's additional cost
// sacrifices a creature whose damage provenance the board must carry, e.g.
// Treacherous Greed's `Sac<1/Creature.dealtDamageThisTurn/...>`.
func sacrificesDamageDealtCreature(f *cards.Face) bool {
	for _, sa := range f.Abilities {
		if sa.Kind == "SP" && strings.Contains(sa.Params["Cost"], "dealtDamageThisTurn") {
			return true
		}
	}
	return false
}

func requiredTapCount(f *cards.Face) int {
	for _, sa := range f.Abilities {
		if sa.Kind != "SP" {
			continue
		}
		cost := sa.Params["Cost"]
		if i := strings.Index(cost, "tapXType<"); i >= 0 {
			tail := cost[i+len("tapXType<"):]
			if j := strings.IndexByte(tail, '/'); j >= 0 {
				var n int
				fmt.Sscanf(tail[:j], "%d", &n)
				return n
			}
		}
	}
	return 0
}

func hasWaterbendCost(f *cards.Face) bool {
	for _, st := range f.Statics {
		if strings.Contains(st.Params["Cost"], "Waterbend<5>") {
			return true
		}
	}
	return false
}

func appendFixtureUnique(xs []string, name string) []string {
	for _, x := range xs {
		if x == name {
			return xs
		}
	}
	return append(xs, name)
}

// buildStackScenario builds the scenario for one fixture: the precast spell
// (if any) is cast first, then the card under test with its targets placed in
// slot order (a stack slot points at the precast spell on the stack). hand
// is the physical card setup deals (physicalName).
func buildStackScenario(f *cards.Face, name, hand, mana string, pre precast, fx oraclegen.Fixture, slots []oraclegen.Slot, stackIdx []int, answers []oraclegen.Answer) oraclegen.Scenario {
	targets := insertStackTargets(slots, stackIdx, fx.Targets(), pre)
	turn := 0
	if oraclegen.RequiresTurnFour(name) {
		turn = 7
	}
	sc := oraclegen.Scenario{
		Setup:        map[string]oraclegen.Seat{"p0": *fx.P0(), "p1": *fx.P1()},
		Turn:         turn,
		SetupAnswers: oraclegen.OpeningHandAnswers(f),
		Steps:        []oraclegen.Step{{Op: "cast", Seat: 0, Card: "p0:" + name, Mana: mana, Targets: targets, Answers: answers}},
	}
	sc.Setup["p0"] = oraclegen.WithHand(sc.Setup["p0"], hand)
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
	// Prelude steps (a token-maker, an Aura, a this-turn move) run first,
	// in the main phase, before any combat and the precast spell.
	if pre := fx.Prelude(); len(pre) > 0 {
		sc.Steps = append(append([]oraclegen.Step(nil), pre...), sc.Steps...)
	}
	oraclegen.Baseline(sc.Setup, f)
	return sc
}

// stackSlotIndexes lists the positions in slots that draw from the stack.
func stackSlotIndexes(slots []oraclegen.Slot) []int {
	var out []int
	for i, s := range slots {
		if oraclegen.SlotIsStack(s.Filter) {
			out = append(out, i)
		}
	}
	return out
}

// nonStackSlots keeps every slot that does not draw from the stack.
func nonStackSlots(slots []oraclegen.Slot) []oraclegen.Slot {
	var out []oraclegen.Slot
	for _, s := range slots {
		if !oraclegen.SlotIsStack(s.Filter) {
			out = append(out, s)
		}
	}
	return out
}

// insertStackTargets interleaves plain-slot targets with a stack slot's ref
// to the precast spell, preserving slot order.
func insertStackTargets(slots []oraclegen.Slot, stackIdx []int, plain []string, pre precast) []string {
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
func precastFitsSlots(slots []oraclegen.Slot, stackIdx []int, p precast) bool {
	for _, i := range stackIdx {
		if !precastFits(slots[i].Filter, p) {
			return false
		}
	}
	return true
}

// attachesOnReturn reports whether a charm mode's ability chain puts a card
// onto the battlefield attached to a chosen object (an AttachedTo$ param).
func attachesOnReturn(f *cards.Face, svar string) bool {
	for name := svar; name != ""; {
		body := f.SVars[name]
		if strings.Contains(body, "AttachedTo$") {
			return true
		}
		name = ""
		for _, part := range strings.Split(body, "|") {
			if k, v, ok := strings.Cut(strings.TrimSpace(part), "$"); ok && strings.TrimSpace(k) == "SubAbility" {
				name = strings.TrimSpace(v)
			}
		}
	}
	return false
}
