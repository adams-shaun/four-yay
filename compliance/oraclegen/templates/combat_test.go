package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

func TestCombatAttackAndBlockItemsPlay(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		keyword  string
		minPower int
	}{
		{"Deathtouch", 1}, {"First strike", 2}, {"Trample", 3}, {"Lifelink", 1}, {"Flying", 1},
	} {
		t.Run(strings.ToLower(strings.ReplaceAll(tc.keyword, " ", "_")), func(t *testing.T) {
			it, face := combatKeywordItem(t, reg, tc.keyword, tc.minPower, "combat.attack")
			assertCombatSetup(t, it)
			res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			if !ok || len(res.Fails) != 0 {
				t.Fatalf("%s does not play through gorge: ok=%v fails=%v", face.Name, ok, res.Fails)
			}
			main2 := combatMain2(t, res)
			if tc.keyword == "Flying" && hasCombatStep(it.Scenario, "block") {
				t.Fatalf("flying attacker unexpectedly emitted a block step: %+v", it.Scenario.Steps)
			}
			if tc.keyword == "Deathtouch" && hasCombatStep(it.Scenario, "block") && !graveyardHas(main2, 1, combatBlocker) {
				t.Fatalf("blocked deathtouch attacker did not kill the bear: %+v", main2.Players)
			}
			if tc.keyword == "First strike" && hasCombatStep(it.Scenario, "block") && !graveyardHas(main2, 1, combatBlocker) {
				t.Fatalf("first-strike attacker did not kill the bear: %+v", main2.Players)
			}
			if tc.keyword == "Trample" && hasCombatStep(it.Scenario, "block") && playerLife(main2, 1) >= 20 {
				t.Fatalf("blocked trample attacker dealt no excess damage: %+v", main2.Players)
			}
			if tc.keyword == "Lifelink" && playerLife(main2, 0) <= 20 {
				t.Fatalf("lifelink attacker did not gain life: %+v", main2.Players)
			}
		})
	}

	t.Run("defender_blocks", func(t *testing.T) {
		it, _ := combatKeywordItem(t, reg, "Defender", 0, "combat.block")
		assertCombatSetup(t, it)
		if !hasCombatStep(it.Scenario, "block") {
			t.Fatalf("defender item has no block operation: %+v", it.Scenario.Steps)
		}
		res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
		if !ok || len(res.Fails) != 0 {
			t.Fatalf("defender item does not play through gorge: ok=%v fails=%v", ok, res.Fails)
		}
		blocked := false
		for _, snap := range res.Snapshots {
			if snap.Step != "declare-blockers" {
				continue
			}
			for _, p := range snap.Permanents {
				if p.Ref == "p0:"+it.Card && p.Blocking {
					blocked = true
				}
			}
		}
		if !blocked {
			t.Fatal("no checkpoint shows the defender blocking Grizzly Bears")
		}
		combatMain2(t, res)
	})
}

func combatKeywordItem(t *testing.T, reg *cards.Registry, keyword string, minPower int, sub string) (oraclegen.Item, *cards.Face) {
	t.Helper()
	root := "../../../compliance/printed"
	has := func(n string) bool { _, ok := reg.Lookup(n); return ok }
	folded := compliance.FoldedNames(reg)
	for _, set := range activateCensusSets {
		printed, err := compliance.LoadPrinted(root, set)
		if err != nil {
			t.Fatal(err)
		}
		for _, printedName := range printed.Cards {
			name, ok := compliance.CorpusNameFold(has, folded, printedName)
			if !ok {
				continue
			}
			c, _ := reg.Lookup(name)
			for _, f := range c.Faces {
				if !f.IsCreature() || !hasCombatKeyword(f, keyword) {
					continue
				}
				if minPower > 0 {
					p, _, ok := parsePT(f.PT)
					if !ok || p < minPower {
						continue
					}
				}
				for _, req := range levelb.Requirements(c) {
					if req.Sub != sub || req.Face < 0 || req.Face >= len(c.Faces) || c.Faces[req.Face] != f {
						continue
					}
					it, skip := GenerateB(reg, name, req)
					if skip == nil {
						return it, f
					}
				}
			}
		}
	}
	t.Fatalf("no level-A-set creature with %s has a served %s requirement", keyword, sub)
	return oraclegen.Item{}, nil
}

func hasCombatKeyword(f *cards.Face, keyword string) bool {
	for _, k := range f.Keywords {
		if strings.EqualFold(k, keyword) {
			return true
		}
	}
	return false
}

func assertCombatSetup(t *testing.T, it oraclegen.Item) {
	t.Helper()
	contains := func(xs []string, want string) bool {
		for _, x := range xs {
			if x == want {
				return true
			}
		}
		return false
	}
	if !contains(it.Scenario.Setup["p0"].Battlefield, it.Card) {
		t.Fatalf("precondition: %s is not on p0's battlefield: %+v", it.Card, it.Scenario.Setup)
	}
	if !contains(it.Scenario.Setup["p1"].Battlefield, combatBlocker) {
		t.Fatalf("precondition: fixture blocker is not on p1's battlefield: %+v", it.Scenario.Setup)
	}
}

func hasCombatStep(sc oraclegen.Scenario, op string) bool {
	for _, st := range sc.Steps {
		if st.Op == op {
			return true
		}
	}
	return false
}

func combatMain2(t *testing.T, res rules.OracleResult) rules.OracleSnapshot {
	t.Helper()
	for _, snap := range res.Snapshots {
		if snap.Step == "main2" {
			return snap
		}
	}
	t.Fatalf("no main2 checkpoint in gorge result: %+v", res.Snapshots)
	return rules.OracleSnapshot{}
}

func graveyardHas(s rules.OracleSnapshot, seat int, name string) bool {
	for _, p := range s.Players {
		if p.Seat == seat {
			for _, c := range p.Graveyard {
				if strings.EqualFold(c, name) {
					return true
				}
			}
		}
	}
	return false
}

func playerLife(s rules.OracleSnapshot, seat int) int32 {
	for _, p := range s.Players {
		if p.Seat == seat {
			return p.Life
		}
	}
	return 0
}
