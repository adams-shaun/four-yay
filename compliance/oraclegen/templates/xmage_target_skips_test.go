package templates

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestXMageOptionalTargetSkipControls: Rise from the Wreck is the one shape
// that gains an explicit skip -- four independent literal 0..1 graveyard
// objects with the Mount object (index 1) empty -- and the multi-target
// controls keep their legacy payload (no field, so the driver keeps its
// trailing skip). Rise's gorge-side targets and settling are unchanged, and
// neither the skip field nor "[target_skip]" reaches gorge's Raw scenario.
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
	if got := strings.Join(cast.Targets, "|"); got != "p0:Grizzly Bears|p0:Adrestia|p0:Hill Giant" {
		t.Fatalf("cast targets %v, want Creature, Vehicle, vanilla in order", cast.Targets)
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
	if len(it.XTargetSkips) != len(it.Scenario.Steps) || castIdx < 0 {
		t.Fatalf("XTargetSkips has %d entries for %d steps (cast at %d)", len(it.XTargetSkips), len(it.Scenario.Steps), castIdx)
	}
	want := []oraclegen.XTargetSkip{{At: 1, Slot: 1}}
	if got := it.XTargetSkips[castIdx]; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("cast step skips = %v, want %v: the empty Mount object sits between Creature and Vehicle", got, want)
	}
	for i, list := range it.XTargetSkips {
		if i != castIdx && len(list) != 0 {
			t.Errorf("step %d (%s) carries skips %v", i, it.Scenario.Steps[i].Op, list)
		}
	}
	if err := oraclegen.CheckTargetSkips(slots, it.Scenario.Steps, it.XTargetSkips); err != nil {
		t.Errorf("Rise's plan fails its own validation: %v", err)
	}
	full, _ := json.Marshal(it)
	if !strings.Contains(string(full), `"xmage_target_skips":[`) {
		t.Errorf("Item JSON lacks xmage_target_skips: %s", full)
	}
	if raw := string(it.Raw()); strings.Contains(raw, "xmage_target_skips") || strings.Contains(raw, "target_skip") {
		t.Errorf("Raw scenario leaks the XMage-only skip: %s", raw)
	}
	if n, res, ok := oraclegen.Settle(reg, it.Scenario); !ok || n == 0 {
		t.Fatalf("Rise from the Wreck does not settle: fails=%v", res.Fails)
	}

	t.Run("reject unproven object shape", func(t *testing.T) {
		ambiguous := append([]oraclegen.Slot(nil), slots...)
		ambiguous[0].ZeroOrOne = false
		if ambiguous[0].ZeroOrOne == slots[0].ZeroOrOne {
			t.Fatal("precondition: correspondence metadata must differ")
		}
		// The engine still reads Rise's real abilities and can cast it;
		// only the fixture-to-XMage correspondence is now unproven.
		if _, ok := castWithProbes(reg, card.Faces[0], it.Card, cast.Mana, ambiguous, nil, nil); ok {
			t.Fatal("an omitted object with unproven correspondence fell back to the legacy queue")
		}
	})

	// Controls: each has several targets but no omitted independent 0..1
	// object, so the legacy payload (no skip plan) must stay.
	controls := []struct {
		card    string
		targets []string
	}{
		{"Pull Through the Weft", []string{"p0:Grizzly Bears", "p0:Serra Angel"}},
		{"Rhino's Rampage", []string{"p0:Grizzly Bears", "p1:Grizzly Bears"}},
		{"Allies at Last", []string{"p0:Grizzly Bears", "p1:Grizzly Bears"}},
		{"Coordinated Clobbering", []string{"p0:Grizzly Bears", "p1:Grizzly Bears"}},
		{"Terrific Team-Up", []string{"p0:Grizzly Bears", "p1:Grizzly Bears"}},
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
}
