package templates

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestXMageOptionalTargetSkipControls: Rise from the Wreck's four independent
// literal 0..1 graveyard slots now all have matching objects, including the
// Mount at index 1, so no XMage target skip is needed. The multi-target
// controls keep their legacy payload (no field). Neither the skip field nor
// "[target_skip]" reaches gorge's Raw scenario.
func TestXMageOptionalTargetSkipControls(t *testing.T) {
	reg := testutil.CorpusRegistry(t)

	it, skip := Generate(reg, "Rise from the Wreck")
	if skip != nil {
		t.Fatalf("Rise from the Wreck: %s", skip.Reason)
	}
	card, ok := reg.Lookup("Rise from the Wreck")
	if !ok {
		t.Fatal("Rise from the Wreck not in corpus")
	}
	slots := oraclegen.SlotSpecs(card.Faces[0])
	if len(slots) != 4 {
		t.Fatalf("Rise has %d slots, want 4 (Creature, Mount, Vehicle, NoAbilities)", len(slots))
	}
	for i, s := range slots {
		if !s.Optional || !s.ZeroOrOne {
			t.Errorf("slot %d %q: Optional=%v ZeroOrOne=%v, want both (an independent 0..1 object)", i, s.Filter, s.Optional, s.ZeroOrOne)
		}
	}
	cast := castStep(t, it, "Rise from the Wreck")
	if got := strings.Join(cast.Targets, "|"); got != "p0:Grizzly Bears|p0:Alacrian Jaguar|p0:Adrestia|p0:Hill Giant" {
		t.Fatalf("cast targets %v, want Creature, Mount, Vehicle, vanilla in order", cast.Targets)
	}
	for _, ref := range cast.Targets {
		if !containsString(it.Setup["p0"].Graveyard, strings.TrimPrefix(ref, "p0:")) {
			t.Fatalf("precondition: target %s is not in the caster's graveyard", ref)
		}
	}
	var castIdx = -1
	for i, st := range it.Scenario.Steps {
		if st.Op == "cast" && st.Card == "p0:Rise from the Wreck" {
			castIdx = i
		}
	}
	if castIdx < 0 {
		t.Fatalf("Rise from the Wreck cast step not found (XTargetSkips has %d entries for %d steps)", len(it.XTargetSkips), len(it.Scenario.Steps))
	}
	if len(it.XTargetSkips) != 0 {
		if len(it.XTargetSkips) != len(it.Scenario.Steps) {
			t.Fatalf("XTargetSkips has %d entries for %d steps", len(it.XTargetSkips), len(it.Scenario.Steps))
		}
		if got := it.XTargetSkips[castIdx]; len(got) != 0 {
			t.Fatalf("cast step skips = %v, want none because the Mount slot is served", got)
		}
	}
	for i, list := range it.XTargetSkips {
		if i != castIdx && len(list) != 0 {
			t.Errorf("step %d (%s) carries skips %v", i, it.Scenario.Steps[i].Op, list)
		}
	}
	if len(it.XTargetSkips) != 0 {
		if err := oraclegen.CheckTargetSkips(slots, it.Scenario.Steps, it.XTargetSkips); err != nil {
			t.Errorf("Rise's plan fails its own validation: %v", err)
		}
	}
	full, _ := json.Marshal(it)
	if strings.Contains(string(full), `"xmage_target_skips"`) {
		t.Errorf("Item JSON should omit the empty xmage_target_skips field: %s", full)
	}
	if raw := string(it.Raw()); strings.Contains(raw, "xmage_target_skips") || strings.Contains(raw, "target_skip") {
		t.Errorf("Raw scenario leaks the XMage-only skip: %s", raw)
	}
	if n, res, ok := oraclegen.Settle(reg, it.Scenario); !ok || n == 0 {
		t.Fatalf("Rise from the Wreck does not settle: fails=%v", res.Fails)
	}

	// Controls: each has several targets but no slot XMage needs an explicit
	// skip for (every object reaches its maximum), so the legacy payload (no
	// skip plan) must stay.
	controls := []struct {
		card    string
		targets []string
	}{
		{"Pull Through the Weft", []string{"p0:Grizzly Bears", "p0:Serra Angel"}},
		{"Rhino's Rampage", []string{"p0:Grizzly Bears", "p1:Grizzly Bears"}},
	}
	for _, c := range controls {
		ci, skip := Generate(reg, c.card)
		if skip != nil {
			t.Errorf("%s: %s", c.card, skip.Reason)
			continue
		}
		if ci.XTargetSkips != nil {
			t.Errorf("%s gained an explicit skip plan %v; its legacy trailing skip must stay", c.card, ci.XTargetSkips)
		}
		cj, _ := json.Marshal(ci)
		if strings.Contains(string(cj), "xmage_target_skips") {
			t.Errorf("%s payload mentions xmage_target_skips", c.card)
		}
		got := castStep(t, ci, c.card).Targets
		if len(got) < 2 {
			t.Errorf("%s cast names %v: the control needs several targets to mean anything", c.card, got)
		}
		if c.targets != nil && strings.Join(got, "|") != strings.Join(c.targets, "|") {
			t.Errorf("%s targets = %v, want %v", c.card, got, c.targets)
		}
	}

	// Terrific Team-Up and Allies at Last carry a multi-pick 1..2 first slot
	// filled with one pick beside a second object. Whether XMage needs an
	// explicit skip for the first object depends on its live candidate set:
	// an object whose candidates are exhausted completes by itself
	// (TargetImpl.isChoiceCompleted's moreSelectCount == 0), so the plan
	// exists only where the scenario offers more own creatures than the slot
	// took -- the static#0.0 rows' setups, not the one-own-creature
	// cast-resolve one.
	ci, skip := Generate(reg, "Terrific Team-Up")
	if skip != nil {
		t.Fatalf("Terrific Team-Up: %s", skip.Reason)
	}
	got := castStep(t, ci, "Terrific Team-Up").Targets
	if strings.Join(got, "|") != "p0:Grizzly Bears|p1:Grizzly Bears" {
		t.Fatalf("cast targets = %v, want own creature then opponent's", got)
	}
	if ci.XTargetSkips != nil {
		t.Fatalf("cast-resolve plan = %v, want none: its setup offers one own creature, so the slot's candidates are exhausted and XMage completes the object itself", ci.XTargetSkips)
	}
	for _, c := range []string{"Terrific Team-Up", "Allies at Last"} {
		si := levelBItem(t, c, "static#0.0")
		st := si.Scenario.Steps[0]
		if st.Op != "cast" || strings.Join(st.Targets, "|") != "p0:Grizzly Bears|p1:Grizzly Bears" {
			t.Fatalf("%s static#0.0 first step = %+v, want a cast with own creature then opponent's", c, st)
		}
		card, ok := reg.Lookup(c)
		if !ok {
			t.Fatalf("%s not in corpus", c)
		}
		slots := oraclegen.SlotSpecs(card.Faces[0])
		if len(slots) != 2 || slots[0].Max != 2 {
			t.Fatalf("precondition: %s slots = %+v, want a 2-object plan with a 1..2 first slot", c, slots)
		}
		castIdx := -1
		for i, s := range si.Scenario.Steps {
			if s.Op == "cast" && s.Card == "p0:"+c {
				castIdx = i
			}
		}
		if castIdx < 0 || len(si.XTargetSkips) != len(si.Scenario.Steps) {
			t.Fatalf("%s: plan not parallel to its %d steps (cast %d): %v", c, len(si.Scenario.Steps), castIdx, si.XTargetSkips)
		}
		want := []oraclegen.XTargetSkip{{At: 1, Slot: 0}}
		if len(si.XTargetSkips[castIdx]) != 1 || si.XTargetSkips[castIdx][0] != want[0] {
			t.Fatalf("%s plan = %v, want %v", c, si.XTargetSkips[castIdx], want)
		}
		if err := oraclegen.CheckTargetSkips(slots, si.Scenario.Steps, si.XTargetSkips); err != nil {
			t.Errorf("%s plan fails its own validation: %v", c, err)
		}
		if raw := string(si.Raw()); strings.Contains(raw, "xmage_target_skips") || strings.Contains(raw, "target_skip") {
			t.Errorf("%s Raw scenario leaks the XMage-only skip: %s", c, raw)
		}
	}
}
