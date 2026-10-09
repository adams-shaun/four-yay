package templates

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// TestCostStaticSVarAmountProbes covers the G11 SVar-amount cost-static rows
// that needed a fixture the probe could not build:
//
//   - Rottenmouth Viper announces the Sac<X> count itself (Amount$ X over
//     SVar X = Count$xPaid): the probe announces X = 2, sacrifices two
//     nonland permanents and pays the printed cost twice less.
//   - Suspicious Detonation's gate counts artifacts sacrificed this turn
//     (CheckSVar$ over PlayerCountPropertyYou$SacrificedThisTurn Artifact);
//     the prelude sacrifices Ornithopter, an artifact creature, not the
//     shared creature fixture.
//   - The Lord of the Eagles reduces by the total power of its controller's
//     flying creatures (Amount$ X over Count$Valid
//     Creature.YouCtrl+withFlying$CardPower); the prelude places Serra
//     Angel, whose 4 printed power is the reduction.
//   - Bite Down on Crime's amount is the cast's own CR 601.2b election
//     (Amount$ Z over SVar Z = Count$OptionalGenericCostPaid.2.0): the probe
//     casts through the optional-cost variant (cast_mode "optionalcost")
//     with the evidence prelude (Colossal Dreadmaw, mana value 6, the exact
//     evidence) in the graveyard, at the EXACT reduced price the paid
//     branch gives.
//
// Each row's probe must pay the EXACT reduced price (the pool the cast step
// adds is empty after payment) and fail once the card's own statics are
// removed, so a probe paying the printed cost cannot pass.
func TestCostStaticSVarAmountProbes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key, mana, printed string
		reduction                int
		// board is what the fixture's amount/gate setup places on p0's
		// battlefield; prelude is a spell the setup casts before the probe
		// ("Village Rites" for the artifact sacrifice), "" when the cast
		// itself carries the setup (the announced Sac<X>); graveyard is the
		// evidence prelude the election probe carries (Bite Down on Crime).
		board     []string
		prelude   string
		graveyard []string
		// castMode elects a cast option for the probe step (the
		// optional-cost variant); "" is the ordinary cast.
		castMode string
		// answers scripts the probe cast's mid-cast asks (the X
		// announcement and the sacrifice); nil lets the runner's
		// deterministic fallback answer.
		answers []oraclegen.Answer
	}{
		{"Rottenmouth Viper", "static#0.0", "CCCB", "CCCCCB", 2,
			[]string{"Ornithopter", "Sol Ring"}, "", nil, "", []oraclegen.Answer{
				{Kind: "choose", Pick: []string{"X = 2"}},
				{Kind: "choose", Pick: []string{"p0:Ornithopter", "p0:Sol Ring"}},
			}},
		{"Suspicious Detonation", "static#0.0", "CR", "CCCCR", 3,
			[]string{"Ornithopter"}, "Village Rites", nil, "", nil},
		{"The Lord of the Eagles", "static#0.0", "CCCUU", "CCCCCCCUU", 4,
			[]string{"Serra Angel"}, "", nil, "", nil},
		{"Bite Down on Crime", "static#0.1", "CG", "CCCG", 2,
			nil, "", []string{"Colossal Dreadmaw"}, "optionalcost", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it, skip := GenerateB(reg, tc.name, costRequirement(t, reg, tc.name, tc.key))
			if skip != nil {
				t.Fatalf("GenerateB: %s", skip.Reason)
			}
			probe := probeStep(t, it)
			if probe.Op != "cast" || probe.Card != "p0:"+tc.name || probe.Mana != tc.mana {
				t.Fatalf("probe = %s %s paying %q, want cast %s paying %q", probe.Op, probe.Card, probe.Mana, tc.name, tc.mana)
			}
			if probe.CastMode != tc.castMode {
				t.Fatalf("probe cast_mode = %q, want %q", probe.CastMode, tc.castMode)
			}
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("precondition: probe %s absent", tc.name)
			}
			pool, why := oraclegen.PoolFor(c.Faces[0].ManaCost)
			if why != "" || pool != tc.printed {
				t.Fatalf("precondition: %s prints %q (%s), table says %q", tc.name, pool, why, tc.printed)
			}
			if got := len(tc.printed) - len(tc.mana); got != tc.reduction {
				t.Fatalf("precondition: table prices differ by %d, want the static's reduction %d", got, tc.reduction)
			}
			foundSource := false
			for _, card := range it.Scenario.Setup["p0"].Hand {
				foundSource = foundSource || card == tc.name
			}
			if !foundSource {
				t.Fatalf("precondition: reduction source %q absent from hand (Card.Self is its own probe)", tc.name)
			}
			bf := it.Scenario.Setup["p0"].Battlefield
			for _, want := range tc.board {
				found := false
				for _, card := range bf {
					found = found || card == want
				}
				if !found {
					t.Fatalf("precondition: fixture %q absent from p0's battlefield %v", want, bf)
				}
			}
			for _, want := range tc.graveyard {
				found := false
				for _, card := range it.Scenario.Setup["p0"].Graveyard {
					found = found || card == want
				}
				if !found {
					t.Fatalf("precondition: fixture %q absent from p0's graveyard %v", want, it.Scenario.Setup["p0"].Graveyard)
				}
			}
			probeIdx := -1
			for i, st := range it.Steps {
				if st.Op == "cast" && st.Card == "p0:"+tc.name {
					probeIdx = i
				}
			}
			if probeIdx < 0 {
				t.Fatalf("precondition: no probe cast step: %+v", it.Steps)
			}
			if tc.prelude != "" && !hasPreludeCast(it.Steps, probeIdx, tc.prelude) {
				t.Fatalf("precondition: %s prelude cast absent: %+v", tc.prelude, it.Steps)
			}
			// The probe must pay the EXACT reduced price: the pool the cast
			// step adds is empty right after payment. A static gorge
			// under-applies (a wrong amount, the spell counting itself)
			// would leave mana behind and fail here even though the step
			// plays.
			if res := runSteps(t, reg, it.Scenario, it.Steps); len(res.Fails) != 0 {
				t.Fatalf("reduced-price probe fails with the static present: %v", res.Fails)
			}
			for _, snap := range probeSnapshots(t, reg, it.Scenario) {
				if snap.Checkpoint != fmt.Sprintf("step %d (cast)", probeIdx) {
					continue
				}
				if got := snap.Players[0].Pool; got != "" {
					t.Fatalf("probe pool %q left after payment, want the exact reduced price %q", got, tc.mana)
				}
			}
			if res := runSteps(t, withoutStatics(reg, tc.name), it.Scenario, it.Steps); len(res.Fails) == 0 {
				t.Fatalf("probe is not sensitive to %s's cost reduction", tc.name)
			}
		})
	}
}

// hasPreludeCast reports whether a cast of prelude precedes the probe cast
// at steps[probeIdx].
func hasPreludeCast(steps []oraclegen.Step, probeIdx int, prelude string) bool {
	for i, st := range steps {
		if i < probeIdx && st.Op == "cast" && st.Card == "p0:"+prelude {
			return true
		}
	}
	return false
}

// probeSnapshots replays the scenario once and returns the checkpoint
// snapshots the probe's pool assert reads.
func probeSnapshots(t *testing.T, reg *cards.Registry, sc oraclegen.Scenario) []rules.OracleSnapshot {
	t.Helper()
	return runSteps(t, reg, sc, sc.Steps).Snapshots
}
