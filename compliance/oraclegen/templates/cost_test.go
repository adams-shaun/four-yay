package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func staticCostItem(t *testing.T, reg *cards.Registry, name string) oraclegen.Item {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("precondition: %s missing from corpus", name)
	}
	for _, req := range levelb.Requirements(c) {
		if req.Key == "static#0.0" {
			if req.Sub != "static.cost" || req.Gap != "" {
				t.Fatalf("precondition: %s requirement = %+v, want supported static.cost", name, req)
			}
			it, skip := GenerateB(reg, name, req)
			if skip != nil {
				t.Fatalf("%s: %s", name, skip.Reason)
			}
			return it
		}
	}
	t.Fatalf("precondition: %s has no static#0.0", name)
	return oraclegen.Item{}
}

// probeCast is the item's last cast step: the reduced-price probe.
func probeCast(t *testing.T, it oraclegen.Item) oraclegen.Step {
	t.Helper()
	for i := len(it.Steps) - 1; i >= 0; i-- {
		if it.Steps[i].Op == "cast" {
			return it.Steps[i]
		}
	}
	t.Fatalf("precondition: %s has no cast step: %+v", it.ID, it.Steps)
	return oraclegen.Step{}
}

// withoutStatics is an independent registry in which name has no statics, so
// the reduction the template probes is absent and nothing else changed.
func withoutStatics(reg *cards.Registry, name string) *cards.Registry {
	muted := cards.NewRegistry()
	muted.Tokens = reg.Tokens
	for _, c := range reg.Cards {
		if c.Faces[0].Name == name {
			cc := *c
			cc.Faces = append([]*cards.Face(nil), c.Faces...)
			ff := *c.Faces[0]
			ff.Statics = nil
			cc.Faces[0] = &ff
			c = &cc
		}
		muted.Add(c)
	}
	return muted
}

var costStaticCards = []string{
	"Geist of Saint Thalia", "Tam, the Possibility", "Ghalta the Immovable",
	"Ghalta the Unstoppable", "Traxos, Academy Guardian", "Wrath of the Bloodmane",
}

// TestCostStaticIsSensitiveToTheReduction: with the card's statics removed the
// exact reduced-price cast must fail, so an engine ignoring the requirement
// cannot pass the observation. Each probe's printed cost must also exceed what
// the scenario supplies, or the "reduction" would be invisible.
func TestCostStaticIsSensitiveToTheReduction(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, name := range costStaticCards {
		t.Run(name, func(t *testing.T) {
			it := staticCostItem(t, reg, name)
			if res := runSteps(t, reg, it.Scenario, it.Steps); len(res.Fails) != 0 {
				t.Fatalf("precondition: reduced-price cast fails with the static present: %v", res.Fails)
			}
			res := runSteps(t, withoutStatics(reg, name), it.Scenario, it.Steps)
			if len(res.Fails) == 0 {
				t.Errorf("%s: cast at %q succeeded with the reduction removed; probe does not exercise it", name, probeCast(t, it).Mana)
			}
		})
	}
}

// TestCostStaticProbesAreFullyScripted: every cast in the item carries the
// exact targets gorge chose (an empty list for a targeted spell would leave
// XMage's strict driver without a target), the target decisions are not also
// scripted as answers, and the item replays to an empty stack.
func TestCostStaticProbesAreFullyScripted(t *testing.T) {
	reg := loadGenRegistry(t)
	targeted := map[string]bool{"Lightning Strike": true, "Shock": true, "Wrath of the Bloodmane": true}
	for _, name := range costStaticCards {
		t.Run(name, func(t *testing.T) {
			it := staticCostItem(t, reg, name)
			if _, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok {
				t.Fatalf("item does not play through to an empty stack: %+v", it.Steps)
			}
			casts := 0
			for i, st := range it.Steps {
				if st.Op != "cast" {
					continue
				}
				casts++
				if targeted[strings.TrimPrefix(st.Card, "p0:")] {
					if len(st.Targets) != 1 || st.Targets[0] == "" {
						t.Errorf("step %d %s targets = %v, want exactly gorge's one pick", i, st.Card, st.Targets)
					}
				} else if len(st.Targets) != 0 {
					t.Errorf("step %d %s targets = %v, want none", i, st.Card, st.Targets)
				}
			}
			if casts == 0 {
				t.Fatal("precondition: no cast steps")
			}
			for i, as := range it.XAnswers {
				for _, a := range as {
					if a.Kind == "target" {
						t.Errorf("step %d re-scripts a cast step's target as an answer: %+v", i, a)
					}
				}
			}
		})
	}
	// Traxos's prelude Shock and Geist's Lightning Strike are the target-bearing casts.
	if got := staticCostItem(t, reg, "Traxos, Academy Guardian").Steps[0]; got.Card != "p0:Shock" || len(got.Targets) != 1 {
		t.Errorf("Traxos prelude = %+v, want a Shock with one chosen target", got)
	}
	if got := probeCast(t, staticCostItem(t, reg, "Geist of Saint Thalia")); len(got.Targets) != 1 {
		t.Errorf("Geist probe = %+v, want one chosen target", got)
	}
}

func TestCostStaticProbesModifiedPrice(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, spell, mana, permanent string
	}{
		{"Ghalta the Immovable", "Ghalta the Immovable", "CCCCW", "Serra Angel"},
		{"Geist of Saint Thalia", "Lightning Strike", "R", "Geist of Saint Thalia"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := staticCostItem(t, reg, tc.name)
			if it.Template != "static#0.0" || len(it.Steps) == 0 {
				t.Fatalf("identity/steps = %s/%+v", it.Template, it.Steps)
			}
			cast := probeCast(t, it)
			if cast.Op != "cast" || cast.Card != "p0:"+tc.spell || cast.Mana != tc.mana {
				t.Fatalf("cast probe = %+v, want %s at %s", cast, tc.spell, tc.mana)
			}
			found := false
			for _, p := range it.Scenario.Setup["p0"].Battlefield {
				found = found || p == tc.permanent
			}
			if !found {
				t.Fatalf("precondition: %s not on p0 battlefield: %v", tc.permanent, it.Scenario.Setup["p0"].Battlefield)
			}
			res := runSteps(t, reg, it.Scenario, it.Steps)
			if len(res.Fails) != 0 {
				t.Fatalf("reduced-price cast failed in gorge: %v", res.Fails)
			}
			if len(res.Snapshots) < 2 {
				t.Fatalf("cast did not produce a step snapshot: %v", res.Snapshots)
			}
			pool := ""
			for _, p := range res.Snapshots[1].Players {
				if p.Seat == 0 {
					pool = p.Pool
				}
			}
			if pool != "" {
				t.Errorf("p0.pool after exactly-priced cast = %q, want empty", pool)
			}
		})
	}
}

func TestCostStaticProfilesAndOpponentGap(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, name := range []string{"Tam, the Possibility", "Ghalta the Unstoppable", "Traxos, Academy Guardian", "Wrath of the Bloodmane"} {
		it := staticCostItem(t, reg, name)
		res := runSteps(t, reg, it.Scenario, it.Steps)
		if len(res.Fails) != 0 {
			t.Errorf("%s reduced-price cast failed: %v", name, res.Fails)
		}
	}
	for _, name := range []string{"Thalia, the Survivor", "Terror of the Peaks"} {
		it := staticCostItem(t, reg, name)
		castByOpponent := false
		for _, step := range it.Steps {
			castByOpponent = castByOpponent || (step.Op == "cast" && step.Seat == 1)
		}
		if !castByOpponent {
			t.Fatalf("%s probe does not cast as p1: %+v", name, it.Steps)
		}
	}
	c, ok := reg.Lookup("Aven Interrupter")
	if !ok {
		t.Fatal("precondition: Aven Interrupter missing from corpus")
	}
	for _, req := range levelb.Requirements(c) {
		if req.Key == "static#0.0" {
			if req.Sub != "static.cost" || !strings.Contains(req.Gap, "opponent-cast") {
				t.Fatalf("Aven Interrupter gap = %+v, want named opponent-cast gap", req)
			}
			if _, skip := GenerateB(reg, "Aven Interrupter", req); skip == nil || !strings.Contains(skip.Reason, "opponent-cast") {
				t.Fatalf("Aven Interrupter did not remain a named gap: %v", skip)
			}
			return
		}
	}
	t.Fatal("precondition: Aven Interrupter has no static#0.0")
}

// A gorge that ignores the reduction cannot cast at the reduced price. The
// generator still returns the item (never a Skip), so the failed cast is a
// divergence for the host pass rather than a hidden gap. The card is rebuilt
// with one inert static in place of its reduction so the slot still exists.
func TestCostStaticFailedCastStaysAGeneratedItem(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, name := range costStaticCards {
		t.Run(name, func(t *testing.T) {
			real := staticCostItem(t, reg, name)
			muted := cards.NewRegistry()
			muted.Tokens = reg.Tokens
			var req levelb.Requirement
			for _, c := range reg.Cards {
				if c.Faces[0].Name == name {
					cc := *c
					cc.Faces = append([]*cards.Face(nil), c.Faces...)
					ff := *c.Faces[0]
					ff.Statics = []cards.Static{{Mode: "Continuous", Params: map[string]string{"Mode": "Continuous"}}}
					cc.Faces[0] = &ff
					c = &cc
					req = levelb.Requirement{Key: "static#0.0", Slot: "0", Sub: "static.cost"}
				}
				muted.Add(c)
			}
			if req.Key == "" {
				t.Fatalf("precondition: %s not found", name)
			}
			it, skip := GenerateB(muted, name, req)
			if skip != nil {
				t.Fatalf("failed cast became a skip: %s", skip.Reason)
			}
			if got, want := probeCast(t, it), probeCast(t, real); got.Card != want.Card || got.Mana != want.Mana {
				t.Errorf("kept probe = %+v, want %+v", got, want)
			}
			if res := runSteps(t, muted, it.Scenario, it.Steps); len(res.Fails) == 0 {
				t.Errorf("precondition: the muted card still casts at the reduced price")
			}
		})
	}
}
