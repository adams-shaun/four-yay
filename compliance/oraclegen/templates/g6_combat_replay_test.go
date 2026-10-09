package templates

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// The G6 combat-replay rows (census class "combat legality (cannot
// attack/block, evasion)", .ds4/levelb-census wave3 ticket
// g6b-combat-replay, 37 rows over 12 Standard sets) skipped because the
// ordinary combat scenario could not replay. Four fixture serves clear them;
// one focused test per serve, each covering rows from the file:
//
//   - selfCombatRestrictionStatic (combat_self_restrict.go): the
//     not-offered observation a self CantAttack/CantBlock static makes honest
//     (Ketramose, Patchwork Beastie, Tiger-Dillo).
//   - the died-at-setup counters serve (combat_setup_fix.go): Goldvein
//     Hydra, Pterafractyl, Sunbird Standard's Effigy face.
//   - the tap serves: the second-card draw untap (Tiger-Seal), the stun
//     untap (Sleep-Cursed Faerie), the sac-or-tap late entry (Devouring
//     Sugarmaw).
//   - the MustAttack/tax late entry (combat_late_entry.go): Red Herring,
//     Ares, Archangel of Tithes.
//
// Every test generates the item, replays its scenario through gorge's
// rules.RunOracleScenarioJSON, and asserts the effect the scenario compares:
// the card attacked or blocked (the decision's own picks) or, for the
// observations, the Expect the item carries. Each also asserts the
// precondition the assertion depends on -- the card is an untapped
// battlefield permanent where the serve needs one, or the counters the serve
// added are actually there -- so a vacuous serve fails loudly.

// g6CombatItem looks the card's combat requirement up and generates it.
func g6CombatItem(t *testing.T, reg *cards.Registry, name, key string) oraclegen.Item {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("precondition: %q is absent from the corpus", name)
	}
	var req *levelb.Requirement
	for _, r := range levelb.Requirements(c) {
		if r.Key == key {
			r := r
			req = &r
			break
		}
	}
	if req == nil {
		t.Fatalf("precondition: %s has no %s requirement", name, key)
	}
	it, skip := GenerateB(reg, name, *req)
	if skip != nil {
		t.Fatalf("GenerateB(%s, %s) skipped: %s", name, key, skip.Reason)
	}
	return it
}

// g6Replay runs the item's scenario in gorge and fails on any fail line.
func g6Replay(t *testing.T, reg *cards.Registry, it oraclegen.Item) rules.OracleResult {
	t.Helper()
	b, err := json.Marshal(it.Scenario)
	if err != nil {
		t.Fatal(err)
	}
	res, err := rules.RunOracleScenarioJSON(reg, b)
	if err != nil {
		t.Fatalf("scenario does not run: %v", err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario does not replay: %v", res.Fails)
	}
	return res
}

// g6Perm finds ref in snap's permanents.
func g6Perm(t *testing.T, snap rules.OracleSnapshot, ref string) rules.OracleSnapPerm {
	t.Helper()
	for _, p := range snap.Permanents {
		if p.Ref == ref {
			return p
		}
	}
	t.Fatalf("precondition: %s is not on the battlefield at %s", ref, snap.Checkpoint)
	return rules.OracleSnapPerm{}
}

// g6DecisionPicks reports whether some decision of kind kind names label.
func g6DecisionPicks(res rules.OracleResult, kind, label string) bool {
	for _, d := range res.Decisions {
		if d.Kind != kind {
			continue
		}
		for _, p := range d.Picks {
			if strings.Contains(p, label) {
				return true
			}
		}
	}
	return false
}

func TestG6SelfRestrictionRowsGenerate(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	for _, row := range []struct{ name, key string }{
		{"Ketramose, the New Dawn", "combat#0.attack"},
		{"Ketramose, the New Dawn", "combat#0.block"},
		{"Patchwork Beastie", "combat#0.block"},
		{"Tiger-Dillo", "combat#0.attack"},
	} {
		it := g6CombatItem(t, reg, row.name, row.key)
		res := g6Replay(t, reg, it)
		if len(it.Scenario.Steps) == 0 || it.Scenario.Steps[len(it.Scenario.Steps)-1].Decision == "" {
			t.Fatalf("%s %s: the item must stop at a declare decision, steps %+v", row.name, row.key, it.Scenario.Steps)
		}
		// The observation's two halves: the probe is offered (the control
		// that proves the decision exists) and the card is not.
		expect := it.Scenario.Steps[len(it.Scenario.Steps)-1].Expect
		if len(expect) != 2 {
			t.Fatalf("%s %s: want two expect halves, got %d", row.name, row.key, len(expect))
		}
		probe, self := expect[0], expect[1]
		if (row.key == "combat#0.attack" && (probe.CanAttack == nil || self.CanAttack == nil)) ||
			(row.key == "combat#0.block" && (probe.CanBlock == nil || self.CanBlock == nil)) {
			t.Fatalf("%s %s: the expect halves do not name the decision kind", row.name, row.key)
		}
		if probe.Want == nil || *probe.Want != true || self.Want == nil || *self.Want != false {
			t.Fatalf("%s %s: expect wants probe=%v self=%v", row.name, row.key, probe.Want, self.Want)
		}
		// Precondition: the card sits on the battlefield at the checkpoint.
		last := res.Snapshots[len(res.Snapshots)-1]
		g6Perm(t, last, "p0:"+row.name)
	}
}

func TestG6SetupCounterRowsGenerate(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	for _, row := range []struct {
		name, key, pick string
		counters        int32
	}{
		{"Goldvein Hydra", "combat#0.attack", "Goldvein Hydra", 3},
		{"Goldvein Hydra", "combat#0.block", "Goldvein Hydra", 3},
		{"Pterafractyl", "combat#0.attack", "Pterafractyl", 3},
		// The Effigy face is what setup places and what the driver's answer
		// names.
		{"Sunbird Standard", "combat#1.attack", "Sunbird Effigy", 3},
	} {
		it := g6CombatItem(t, reg, row.name, row.key)
		res := g6Replay(t, reg, it)
		// Precondition: the serve's counters are on the card at setup, and
		// the card is a live attacker or blocker where the scenario acts.
		setup := res.Snapshots[0]
		perm := g6Perm(t, setup, "p0:"+row.name)
		if perm.Counters["P1P1"] < row.counters {
			t.Fatalf("%s %s: setup P1P1 = %d, want >= %d", row.name, row.key, perm.Counters["P1P1"], row.counters)
		}
		if !g6DecisionPicks(res, "attackers", row.pick) && !g6DecisionPicks(res, "blockers", row.pick) {
			t.Fatalf("%s %s: no attack or block declaration for the card: %v", row.name, row.key, res.Decisions)
		}
	}
}

func TestG6TapServeRowsGenerate(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	for _, row := range []struct{ name, key string }{
		{"Tiger-Seal", "combat#0.attack"},
		{"Sleep-Cursed Faerie", "combat#0.attack"},
		{"Devouring Sugarmaw", "combat#0.block"},
	} {
		it := g6CombatItem(t, reg, row.name, row.key)
		res := g6Replay(t, reg, it)
		// The card must be an untapped battlefield permanent at the declare
		// decision it acts at (the precondition the tap serves exist to
		// restore).
		last := res.Snapshots[len(res.Snapshots)-1]
		g6Perm(t, last, "p0:"+row.name)
		kind := "attackers"
		if row.key == "combat#0.block" {
			kind = "blockers"
		}
		if !g6DecisionPicks(res, kind, row.name) {
			t.Fatalf("%s %s: no %s declaration for the card: %v", row.name, row.key, kind, res.Decisions)
		}
	}
}

func TestG6LateEntryMustAttackBlockGenerate(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	for _, row := range []struct{ name string }{
		{"Red Herring"},
		{"Ares, God of War"},
		{"Archangel of Tithes"},
	} {
		it := g6CombatItem(t, reg, row.name, "combat#0.block")
		res := g6Replay(t, reg, it)
		// The late entry's shape: the card moves onto the battlefield after
		// the attack, then blocks.
		var moved bool
		for _, st := range it.Scenario.Steps {
			if st.Op == "move" && strings.Contains(st.Card, row.name) {
				moved = true
			}
			if st.Op == "pass_to" && (st.Step != "main2" || st.Active != "p1") {
				t.Fatalf("%s: block item's pass_to step=%q active=%q", row.name, st.Step, st.Active)
			}
		}
		if !moved {
			t.Fatalf("%s: block item has no move step: %+v", row.name, it.Scenario.Steps)
		}
		if !g6DecisionPicks(res, "blockers", row.name) {
			t.Fatalf("%s: no block declaration for the card: %v", row.name, res.Decisions)
		}
	}
}
