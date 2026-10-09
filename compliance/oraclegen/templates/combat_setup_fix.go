package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// combatSetupFix serves a combat requirement whose ordinary scenario fails
// because the FIXTURE, not a combat restriction, loses the card. Four
// fixture shapes, each anchored in the card's own script or in the failed
// run's board, each verified by the settled scenario itself (a fix that does
// not replay is not returned, so the caller keeps its ordinary skip):
//
//   - the card is not on the battlefield at the failed run's last snapshot:
//     setup would kill it (an X-cost or characteristic-setting power whose
//     source the fixture cannot supply). Setup counters (oraclegen.Seat
//     Counters, the same scaffolding a planeswalker's loyalty headroom rides)
//     keep it alive; both engines replay the same counters.
//   - the card sits tapped with STUN counters and its script has an
//     activated untap ability: the untap activations (one per stun counter,
//     CR 702.92x's remove-one-instead) untap it before the declare step.
//   - the card sits tapped and its script taps itself at its upkeep (DB$ Tap
//     Defined$ Self): a Divination cast draws the second card of the turn and
//     its "whenever you draw your second card each turn" untap trigger
//     untaps it.
//   - the card sits untapped but its upkeep trigger is the
//     "you may sacrifice ... if you don't, tap this" shape: the card starts
//     in hand and is moved onto the battlefield after the tap moment (the
//     move op, the same late-entry shape combat_late_entry.go serves the
//     block direction with), and the attack direction starts on turn 2 so
//     the card is never summoning-sick when it attacks.
//
// The failed ordinary result and the face drive the shape choice; nothing is
// keyed on the card's name.
func combatSetupFix(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, sc oraclegen.Scenario, failed rules.OracleResult) (oraclegen.Scenario, int, rules.OracleResult, bool) {
	perm, present := failedPerm(failed, "p0:"+name)
	switch {
	case !present:
		return settleFixed(reg, f, name, req, setupCountersScenario(sc, name, setupCountersBoost))
	case present && perm.Tapped && stunCounters(perm) > 0:
		idx, cost, ok := untapAbility(f)
		if !ok {
			return oraclegen.Scenario{}, 0, rules.OracleResult{}, false
		}
		fixed := sc
		// One untap activation per stun counter removes the counters; one
		// more (with none left) actually untaps.
		fixed.Steps = append(stunUntapSteps("p0:"+name, idx, cost, stunCounters(perm)+1), fixed.Steps...)
		return settleFixed(reg, f, name, req, fixed)
	case present && perm.Tapped && upkeepSacTapTrigger(f) && req.Face == 0:
		if _, ok := reg.Lookup(sacrificeFixture); !ok {
			return oraclegen.Scenario{}, 0, rules.OracleResult{}, false
		}
		return settleFixed(reg, f, name, req, sacTapLateScenario(f, name, req, sc))
	case present && perm.Tapped && upkeepTapSelfTrigger(f):
		if _, ok := reg.Lookup(drawSpellFixture); !ok {
			return oraclegen.Scenario{}, 0, rules.OracleResult{}, false
		}
		fixed := sc
		fixed.Steps = append(drawUntapSteps(), fixed.Steps...)
		p0 := fixed.Setup["p0"]
		p0.Hand = append(p0.Hand, drawSpellFixture)
		fixed.Setup["p0"] = p0
		return settleFixed(reg, f, name, req, fixed)
	}
	return oraclegen.Scenario{}, 0, rules.OracleResult{}, false
}

const (
	// setupCountersBoost is the +1/+1 count the died-at-setup serve adds.
	setupCountersBoost = int32(3)
	// drawSpellFixture draws two cards for {2}{U}: the self-tapped upkeep
	// creature's second-card untap rides its second draw.
	drawSpellFixture = "Divination"
	// sacrificeFixture is the artifact the upkeep "sacrifice ... if you
	// don't, tap" trigger eats so the card never taps.
	sacrificeFixture = "Ornithopter"
)

// settleFixed settles the fixed scenario; ok is false unless it replays
// cleanly, so an unhelpful fix is never returned.
func settleFixed(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, fixed oraclegen.Scenario) (oraclegen.Scenario, int, rules.OracleResult, bool) {
	n, res, ok := oraclegen.Settle(reg, fixed)
	if !ok || len(res.Fails) != 0 {
		return oraclegen.Scenario{}, 0, rules.OracleResult{}, false
	}
	return fixed, n, res, true
}

// setupCountersScenario returns sc with boost P1P1 counters on the card
// under test's placements.
func setupCountersScenario(sc oraclegen.Scenario, name string, boost int32) oraclegen.Scenario {
	fixed := sc
	p0 := fixed.Setup["p0"]
	if p0.Counters == nil {
		p0.Counters = map[string]map[string]int32{}
	}
	if p0.Counters[name] == nil {
		p0.Counters[name] = map[string]int32{}
	}
	p0.Counters[name]["P1P1"] += boost
	fixed.Setup["p0"] = p0
	return fixed
}

// failedPerm finds ref among the failed run's last snapshot's permanents.
func failedPerm(res rules.OracleResult, ref string) (rules.OracleSnapPerm, bool) {
	if len(res.Snapshots) == 0 {
		return rules.OracleSnapPerm{}, false
	}
	for _, p := range res.Snapshots[len(res.Snapshots)-1].Permanents {
		if p.Ref == ref {
			return p, true
		}
	}
	return rules.OracleSnapPerm{}, false
}

// stunCounters reads the perm's STUN count.
func stunCounters(p rules.OracleSnapPerm) int32 {
	return p.Counters["STUN"]
}

// untapAbility returns the face's activated untap ability's index into
// Face().Abilities (what the activate op's ability_index names) and the mana
// pool one activation needs from its Cost$.
func untapAbility(f *cards.Face) (int, string, bool) {
	for i, sa := range f.Abilities {
		if sa.Kind != "AB" || !strings.EqualFold(sa.API, "Untap") {
			continue
		}
		return i, costPool(sa.ParamStr(cards.PKCost)), true
	}
	return 0, "", false
}

// costPool renders the mana pool that pays one activation of cost: the
// generic amount rides the first pip colour (a pool of that colour pays both
// its own pips and the generic amount); every other pip colour gets its own
// count.
func costPool(cost string) string {
	generic, pips := 0, map[byte]int{}
	for _, tok := range strings.Fields(strings.ToUpper(cost)) {
		if v, err := strconv.Atoi(tok); err == nil {
			generic += v
			continue
		}
		if len(tok) == 1 && strings.ContainsAny(tok, "WUBRGC") {
			pips[tok[0]]++
		}
	}
	first := byte('C')
	for _, c := range []byte("WUBRGC") {
		if pips[c] > 0 {
			first = c
			break
		}
	}
	return strings.Repeat(string(first), pips[first]+generic) +
		otherPipPool(pips, first)
}

// otherPipPool renders the non-first pip colours' pools.
func otherPipPool(pips map[byte]int, first byte) string {
	var b strings.Builder
	for _, c := range []byte("WUBRGC") {
		if c != first {
			b.WriteString(strings.Repeat(string(c), pips[c]))
		}
	}
	return b.String()
}

// upkeepPhaseTrigger returns the face's upkeep trigger whose body satisfies
// pred.
func upkeepPhaseTrigger(f *cards.Face, pred func(*cards.SA) bool) *cards.Trigger {
	for i := range f.Triggers {
		tr := &f.Triggers[i]
		if !strings.EqualFold(tr.Mode, "Phase") || !strings.EqualFold(tr.ParamStr(cards.PKPhase), "Upkeep") {
			continue
		}
		if tr.Effect != nil && pred(tr.Effect) {
			return tr
		}
	}
	return nil
}

// upkeepTapSelfTrigger reports the "At the beginning of your upkeep, tap
// this creature" shape: a Phase$ Upkeep trigger whose body is DB$ Tap on the
// source itself, with no UnlessCost$ to satisfy.
func upkeepTapSelfTrigger(f *cards.Face) bool {
	return upkeepPhaseTrigger(f, func(sa *cards.SA) bool {
		return strings.EqualFold(sa.API, "Tap") && sa.ParamStr(cards.PKUnlessCost) == ""
	}) != nil
}

// upkeepSacTapTrigger reports the "you may sacrifice ... if you don't, tap
// this creature" shape: a Phase$ Upkeep trigger whose body is DB$ Tap with an
// UnlessCost$ sacrifice cost.
func upkeepSacTapTrigger(f *cards.Face) bool {
	return upkeepPhaseTrigger(f, func(sa *cards.SA) bool {
		if !strings.EqualFold(sa.API, "Tap") {
			return false
		}
		uc := sa.ParamStr(cards.PKUnlessCost)
		return uc != "" && strings.Contains(strings.ToUpper(uc), "SAC")
	}) != nil
}

// selfMustAttackStatic returns the face's self-scoped MustAttack static.
func selfMustAttackStatic(f *cards.Face) *cards.Static {
	for i := range f.Statics {
		st := &f.Statics[i]
		if !selfScope(st) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(st.Mode), "MustAttack") {
			return st
		}
	}
	return nil
}

// taxUnlessStatic returns the face's CantAttackUnless/CantBlockUnless static
// that prices a charge: its untapped presence taxes the ordinary combat
// scenario's declaration (Archangel of Tithes), or its own block a tax the
// scripted declaration never pays.
func taxUnlessStatic(f *cards.Face) *cards.Static {
	for i := range f.Statics {
		st := &f.Statics[i]
		if strings.EqualFold(strings.TrimSpace(st.Mode), "CantAttackUnless") ||
			strings.EqualFold(strings.TrimSpace(st.Mode), "CantBlockUnless") {
			if strings.TrimSpace(st.ParamStr(cards.PKCost)) != "" {
				return st
			}
		}
	}
	return nil
}

// stunUntapSteps is the untap serve's step prelude: the pool, then one
// activate+resolve per stun counter.
func stunUntapSteps(card string, idx int, pool string, n int32) []oraclegen.Step {
	steps := make([]oraclegen.Step, 0, 1+2*int(n))
	steps = append(steps, oraclegen.Step{Op: "mana", Seat: 0, Mana: pool})
	for i := int32(0); i < n; i++ {
		steps = append(steps,
			oraclegen.Step{Op: "activate", Seat: 0, Card: card, Mana: pool, AbilityIndex: &idx},
			oraclegen.Step{Op: "resolve"})
	}
	return steps
}

// drawUntapSteps is the draw serve's step prelude: the pool, the Divination
// cast, its resolve.
func drawUntapSteps() []oraclegen.Step {
	return []oraclegen.Step{
		{Op: "mana", Seat: 0, Mana: "UUU"},
		{Op: "cast", Seat: 0, Card: "p0:" + drawSpellFixture, Mana: "UUU"},
		{Op: "resolve"},
	}
}

// sacTapLateScenario is the sac-tap serve's scenario: the card starts in
// hand (so the upkeep trigger never finds it) and is moved onto the
// battlefield after the tap moment. The block direction moves it in during
// the opponent's combat and blocks with it (the combat_late_entry.go shape).
// The attack direction starts on turn 2: the move lands during the
// opponent's turn, the card's own upkeep trigger then runs with
// sacrificeFixture on the battlefield to eat (the runner's answer records it
// for the driver), and the card is neither tapped nor summoning-sick when it
// attacks; with the card under test able to be blocked by at most one
// creature (menace and friends) the fixture fields a second blocker so the
// block declaration stays legal.
func sacTapLateScenario(f *cards.Face, name string, req levelb.Requirement, sc oraclegen.Scenario) oraclegen.Scenario {
	fixed := sc
	p0 := fixed.Setup["p0"]
	p0.Battlefield = removeName(p0.Battlefield, name)
	p0.Hand = append(p0.Hand, name)
	switch req.Sub {
	case "combat.attack":
		p0.Battlefield = append(p0.Battlefield, sacrificeFixture)
		p0.Hand = appendFixtureUnique(p0.Hand, name)
		steps := []oraclegen.Step{{Op: "move", Seat: 0, Card: "p0:" + name, To: "battlefield"}}
		att := cardAt(0, name)
		for _, st := range sc.Steps {
			switch st.Op {
			case "block":
				bears := []string{combatBlocker, combatBlocker + "#2"}
				st.Blocks = make([][2]string, 0, len(bears))
				for _, b := range bears {
					st.Blocks = append(st.Blocks, [2]string{"p1:" + b, att})
				}
				steps = append(steps, st)
			default:
				steps = append(steps, st)
			}
		}
		fixed.Steps = steps
		p1 := fixed.Setup["p1"]
		p1.Battlefield = oraclegen.Repeat(combatBlocker, 2)
		fixed.Setup["p1"] = p1
		fixed.Turn = 2
	case "combat.block":
		move := oraclegen.Step{Op: "move", Seat: 0, Card: "p0:" + name, To: "battlefield"}
		fixed.Steps = append(fixed.Steps[:1:1], append([]oraclegen.Step{move}, fixed.Steps[1:]...)...)
	}
	fixed.Setup["p0"] = p0
	return fixed
}
