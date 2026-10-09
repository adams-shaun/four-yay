package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// cantBeItem generates the item and checks the last step carrying an
// assertion is a single want=false Offered assertion of kind at seat, then
// returns it. The opponent-turn shapes end with a pass PAIR (p0 passes, p1
// yields), and the offer is checked at the checkpoint after p0's pass, so
// the assertion rides the first pass, not literally the last step.
func cantBeItem(t *testing.T, name, key, sub, kind string, seat int) (it oraclegen.Item, card string) {
	t.Helper()
	reg := loadGenRegistry(t)
	item := staticItemFor(t, reg, name, key, sub)
	last := -1
	for i := len(item.Steps) - 1; i >= 0; i-- {
		if len(item.Steps[i].Expect) > 0 {
			last = i
			break
		}
	}
	if last < 0 {
		t.Fatalf("%s carries no offered assertion: %+v", name, item.Steps)
	}
	st := item.Steps[last]
	if len(st.Expect) != 1 || st.Expect[0].Offered == nil || st.Expect[0].Want == nil || *st.Expect[0].Want {
		t.Fatalf("%s lacks a want=false offered assertion: %+v", name, st.Expect)
	}
	o := st.Expect[0].Offered
	if o.Kind != kind || o.Seat != seat {
		t.Fatalf("%s asserts %+v, want %s at p%d", name, o, kind, seat)
	}
	res, ok := runStatic(reg, item.Scenario)
	if !ok || len(res.Fails) != 0 {
		t.Fatalf("%s: scenario does not hold in gorge: %v", name, res.Fails)
	}
	// The opponent-turn shape is the pass PAIR: a lone pass step is not a
	// pass pattern the XMage driver supports (measured on the 2026-10-09
	// census: "unsupported pass pattern at step 0"). Seat is the seat the
	// offer is checked for; the pair is p0's pass then the other seat's.
	if seat == 1 {
		if len(item.Steps) != 2 || item.Steps[0].Op != "pass" || item.Steps[0].Seat != 0 ||
			item.Steps[1].Op != "pass" || item.Steps[1].Seat != 1 {
			t.Fatalf("%s: steps %+v, want the p0/p1 pass pair the driver expresses", name, item.Steps)
		}
	}
	// The assertion binds: the same scenario claiming the option IS offered fails.
	flipped := item.Scenario
	flipped.Steps = append([]oraclegen.Step(nil), item.Steps...)
	fl := &flipped.Steps[last]
	fl.Expect = append([]oraclegen.Expect(nil), fl.Expect...)
	fl.Expect[0].Want = boolPtr(true)
	if res, ok := runStatic(reg, flipped); !ok || len(res.Fails) == 0 {
		t.Fatalf("%s: the want=true variant also passes, so the observation cannot fail", name)
	}
	return item, o.Card
}

func TestCantBeCastOpponentTurnKutzil(t *testing.T) {
	reg := loadGenRegistry(t)
	_, card := cantBeItem(t, "Kutzil, Malamet Exemplar", "static#0.0", "static.cant-be-cast-opponent-turn", "cast", 1)
	if card != "p1:Gut Shot" {
		t.Fatalf("probe %q", card)
	}
	it := staticItemFor(t, reg, "Kutzil, Malamet Exemplar", "static#0.0", "static.cant-be-cast-opponent-turn")
	if got := it.Setup["p1"].Hand; len(got) != 1 || got[0] != "Gut Shot" {
		t.Fatalf("precondition: p1 hand %v", got)
	}
	if why := sourceRemovedControlFails(reg, it.Scenario, it.Card); why != "" {
		t.Fatalf("control does not offer Gut Shot to p1: %s", why)
	}
}

func TestCantBeActivatedOpponentTurnGrandAbolisher(t *testing.T) {
	reg := loadGenRegistry(t)
	cantBeItem(t, "Grand Abolisher", "static#0.1", "static.cant-be-activated-opponent-turn", "activate", 1)
	it := staticItemFor(t, reg, "Grand Abolisher", "static#0.1", "static.cant-be-activated-opponent-turn")
	if why := sourceRemovedControlFails(reg, it.Scenario, it.Card); why != "" {
		t.Fatalf("control does not offer Mogg Fanatic to p1: %s", why)
	}
}

func TestCantBeActivatedAllClarionConqueror(t *testing.T) {
	reg := loadGenRegistry(t)
	cantBeItem(t, "Clarion Conqueror", "static#0.0", "static.cant-be-activated-all", "activate", 0)
	it := staticItemFor(t, reg, "Clarion Conqueror", "static#0.0", "static.cant-be-activated-all")
	if why := sourceRemovedControlFails(reg, it.Scenario, it.Card); why != "" {
		t.Fatalf("control does not offer Mogg Fanatic: %s", why)
	}
}

func TestCantBeActivatedEnchantedSanctumAndPetrify(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key string }{
		{"Stuck in Summoner's Sanctum", "static#0.0"},
		{"Petrify", "static#0.2"},
	} {
		it := staticItemFor(t, reg, tc.name, tc.key, "static.cant-be-activated-enchanted")
		cantBeItem(t, tc.name, tc.key, "static.cant-be-activated-enchanted", "activate", 0)
		if it.Steps[0].Op != "cast" || it.Steps[0].Card != "p0:"+tc.name || it.Steps[0].Targets[0] != "p0:Mogg Fanatic" {
			t.Fatalf("%s: first step %+v, want the Aura cast onto Mogg Fanatic", tc.name, it.Steps[0])
		}
		// Control: the same checkpoint with no Aura cast.
		ctl := it.Scenario
		ctl.Setup = cloneOracleSetup(ctl.Setup)
		p0 := ctl.Setup["p0"]
		p0.Hand = removeFixture(p0.Hand, tc.name)
		ctl.Setup["p0"] = p0
		ctl.Steps = append([]oraclegen.Step(nil), ctl.Steps[2:]...)
		ctl.Steps[0].Expect = append([]oraclegen.Expect(nil), ctl.Steps[0].Expect...)
		ctl.Steps[0].Expect[0].Want = boolPtr(true)
		if res, ok := runStatic(reg, ctl); !ok || len(res.Fails) != 0 {
			t.Fatalf("%s: control does not offer Mogg Fanatic: %v", tc.name, res.Fails)
		}
	}
}

func TestCantBeCastFirstTurns(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, name := range []string{"Jace Reawakened", "Spider-Man 2099"} {
		it, _ := cantBeItem(t, name, "static#0.0", "static.cant-be-cast-first-turns", "cast", 0)
		if len(it.Steps) != 2 || it.Steps[0].Op != "mana" {
			t.Fatalf("%s: steps %+v, want mana then the turn-1 offer", name, it.Steps)
		}
		_ = reg
	}
}

func TestStaticCantBeNamedSkips(t *testing.T) {
	reg := loadGenRegistry(t)
	for name, want := range map[string]string{
		// Sorcerous Spyglass's castable shape is served
		// (static.cant-be-activated-named); the land that names as it
		// enters keeps the gap.
		"Petrified Hamlet": "chosen-name needs an as-enters name choice",
	} {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("%s not in the corpus", name)
		}
		found := false
		for _, r := range levelb.Requirements(c) {
			if r.Family == "static" && strings.Contains(r.Gap, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s carries no static requirement with gap %q", name, want)
		}
	}
}

func TestCantBeCastLimitHighNoon(t *testing.T) {
	reg := loadGenRegistry(t)
	it, card := cantBeItem(t, "High Noon", "static#0.0", "static.cant-be-cast-limit", "cast", 0)
	if card != "p0:Memnite" {
		t.Fatalf("second-spell probe %q", card)
	}
	// Precondition: the first spell IS cast before the observation, so the
	// refusal below is the second-spell limit, not a blanket lockout.
	if it.Steps[0].Op != "cast" || it.Steps[0].Card != "p0:Ornithopter" {
		t.Fatalf("first step %+v, want the Ornithopter cast", it.Steps[0])
	}
	if why := sourceRemovedControlFails(reg, it.Scenario, it.Card); why != "" {
		t.Fatalf("control does not offer Memnite: %s", why)
	}
}

// TestCantBeCastOpponentTurnJenniferWaltersBackFace pins that the back-face
// requirement is observed on the back face: the replay's battlefield names The
// Sensational She-Hulk for static#1.0 and Jennifer Walters for static#0.0.
func TestCantBeCastOpponentTurnJenniferWaltersBackFace(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		key, onBattlefield string
		backFace           bool
	}{
		{"static#0.0", "Jennifer Walters", false},
		{"static#1.0", "The Sensational She-Hulk", true},
	} {
		it := staticItemFor(t, reg, "Jennifer Walters", tc.key, "static.cant-be-cast-opponent-turn")
		if got := containsString(it.Setup["p0"].BackFace, "Jennifer Walters"); got != tc.backFace {
			t.Fatalf("%s: back_face %v, want in-list=%v", tc.key, it.Setup["p0"].BackFace, tc.backFace)
		}
		res, ok := runStatic(reg, it.Scenario)
		if !ok || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
			t.Fatalf("%s: scenario does not hold: %v", tc.key, res.Fails)
		}
		found := false
		for _, perm := range res.Snapshots[len(res.Snapshots)-1].Permanents {
			if perm.Controller == 0 && perm.Name == tc.onBattlefield {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: p0's battlefield has no %q in the final snapshot", tc.key, tc.onBattlefield)
		}
	}
}
