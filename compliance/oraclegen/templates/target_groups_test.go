package templates

import (
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestCastResolveEmitsPerSlotTargetGroups: a multi-slot cast must carry the
// per-slot target shape the XMage driver needs to place a TARGET_SKIP inside
// the decision it belongs to. Allies at Last is "Up to two target creatures
// you control ... target creature an opponent controls": slot 0 is a
// Creature.YouCtrl slot capped at 2 (the fixture picks one, so it is short),
// slot 1 is an uncapped Creature.OppCtrl slot (never short). Without the
// groups the driver cannot tell which decision a trailing skip terminates.
func TestCastResolveEmitsPerSlotTargetGroups(t *testing.T) {
	reg, err := cards.LoadRegistry(cards.CachePath(filepath.Join("..", "..", "..", ".cards")))
	if err != nil {
		t.Fatalf("the generator needs the corpus: %v", err)
	}
	it, skip := Generate(reg, "Allies at Last")
	if skip != nil {
		t.Fatalf("Allies at Last skipped: %s", skip.Reason)
	}
	if len(it.Steps) == 0 {
		t.Fatal("no cast step")
	}
	st := it.Steps[0]
	// Precondition: the cast really is two-slot, so the group assertions
	// below are about a multi-slot cast and not an empty step.
	if len(st.Targets) != 2 {
		t.Fatalf("want two targets, got %v", st.Targets)
	}
	if len(st.TargetGroups) != 2 {
		t.Fatalf("want two target groups, got %+v", st.TargetGroups)
	}
	if got := st.TargetGroups[0]; len(got.Picks) != 1 || got.Max != 2 {
		t.Errorf("slot 0 = %+v, want one pick and max 2 (short)", got)
	}
	if got := st.TargetGroups[1]; len(got.Picks) != 1 || got.Max != 0 {
		t.Errorf("slot 1 = %+v, want one pick and max 0 (uncapped)", got)
	}
	// Every target ref must appear in exactly one group, so the driver's
	// addTarget walk covers the same picks the runner's Targets does.
	var grouped []string
	for _, g := range st.TargetGroups {
		grouped = append(grouped, g.Picks...)
	}
	if len(grouped) != len(st.Targets) {
		t.Fatalf("groups %v do not cover targets %v", grouped, st.Targets)
	}
	for i, tg := range st.Targets {
		if grouped[i] != tg {
			t.Errorf("group pick %d = %q, want %q", i, grouped[i], tg)
		}
	}
}

// TestSingleTargetCastCarriesNoShortGroup: a one-slot, uncapped cast (Shock)
// must not make the driver emit a skip -- groupIsShort is false -- and must
// carry no target_groups at all, preserving its existing wire bytes. Guards
// against marking every one-pick group short.
func TestSingleTargetCastCarriesNoShortGroup(t *testing.T) {
	reg, err := cards.LoadRegistry(cards.CachePath(filepath.Join("..", "..", "..", ".cards")))
	if err != nil {
		t.Fatalf("the generator needs the corpus: %v", err)
	}
	it, skip := Generate(reg, "Shock")
	if skip != nil {
		t.Fatalf("Shock skipped: %s", skip.Reason)
	}
	// Precondition: Shock really does cast at one target.
	if len(it.Steps[0].Targets) != 1 {
		t.Fatalf("Shock cast targets = %v, want one", it.Steps[0].Targets)
	}
	if len(it.Steps[0].TargetGroups) != 0 {
		t.Errorf("single-target cast carries groups: %+v", it.Steps[0].TargetGroups)
	}
	for _, g := range it.Steps[0].TargetGroups {
		if g.Max > 0 && len(g.Picks) < g.Max {
			t.Errorf("single-target cast has a short group: %+v", g)
		}
	}
}

var _ = oraclegen.TargetGroup{}
