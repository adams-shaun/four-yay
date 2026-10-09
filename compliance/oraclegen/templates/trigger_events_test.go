package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// eventTriggerCases is one real FRA card per recipe shape. probe is the card
// the cause steps cast or activate (every probe is listed for host
// confirmation, spec H4); inHand marks a card that starts in p0's hand.
var eventTriggerCases = []struct {
	name, key, sub, probe string
	inHand                bool
}{
	{"Edgar, Ancient Bloodlord", "trigger#0.0", "trigger.dies-other", "Murder", false},
	{"Gardenize", "trigger#0.0", "trigger.dies-other", "Murder", false},
	{"Ferocity of the Hunt", "trigger#0.0", "trigger.dies-other", "Murder", true},
	{"Denzilore Fatehold", "trigger#0.0", "trigger.scry", "Opt", false},
	{"Denzilore Fatehold", "trigger#0.1", "trigger.surveil", "Consider", false},
	{"Master of Barbs", "trigger#0.0", "trigger.noncombat-damage", "Shock", false},
	{"Hexhaven Invigorator", "trigger#0.0", "trigger.noncombat-damage", "Shock", false},
	{"Fblthp, Impossibly Lost", "trigger#0.0", "trigger.combat-damage-all", "", false},
	{"Ajani Unrelenting", "trigger#0.0", "trigger.loyalty-activated", "Ajani Goldmane", false},
	{"Inspired Tethermage", "trigger#0.0", "trigger.loyalty-activated", "Ajani Goldmane", false},
	{"Titanbones, Towering Heart", "trigger#0.1", "trigger.discarded", "Mind Rot", true},
	{"Tinybones, Pocket Nuisance", "trigger#0.1", "trigger.discarded", "Mind Rot", false},
	{"Jiang Yanggu, Alone", "trigger#0.0", "trigger.attacks-one-target", "", false},
	{"Gardenize", "trigger#0.1", "trigger.phase", "", false},
	{"The Theorist, Jace Beleren", "trigger#0.0", "trigger.phase", "", false},
}

func inZone(zone []string, name string) bool {
	for _, n := range zone {
		if n == name {
			return true
		}
	}
	return false
}

// firedOnStack replays the item's cause (its trailing resolves dropped, a combat-damage
// or phase pass_to cut at its checkpoint) and reports whether a snapshot shows
// an ability sourced by name. The three variants mirror the generator's own
// probe: the bare cause, the cause with both players passing so a cast spell
// resolves, and that plus a pass_to the next priority decision, which answers
// a scry/surveil/discard choice asked mid-resolution.
func firedOnStack(t *testing.T, sc oraclegen.Scenario, name string) bool {
	t.Helper()
	reg := loadGenRegistry(t)
	steps := sc.Steps
	for len(steps) > 0 && steps[len(steps)-1].Op == "resolve" {
		steps = steps[:len(steps)-1]
	}
	var cause []oraclegen.Step
	for _, st := range steps {
		if st.Op == "pass_to" && st.Step == "main2" && len(cause) > 0 && cause[len(cause)-1].Op == "attack" {
			st.Step = "end-combat"
		}
		cause = append(cause, st)
	}
	passes := []oraclegen.Step{{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}}
	answered := append(append([]oraclegen.Step(nil), passes...), oraclegen.Step{Op: "pass_to", Decision: "priority"})
	for _, tail := range [][]oraclegen.Step{nil, passes, answered} {
		res := runSteps(t, reg, sc, append(append([]oraclegen.Step(nil), cause...), tail...))
		if len(res.Fails) == 0 && stackHasSource(res.Snapshots, name) {
			return true
		}
	}
	return false
}

// TestEventTriggerRecipes serves one card per new recipe and proves each item
// (1) is classified into the sub-family, (2) plays through gorge, (3) names
// its probe in the cause steps, (4) has the card where the recipe puts it, and
// (5) fires the card's trigger -- which the same replay with the card removed
// does not.
func TestEventTriggerRecipes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range eventTriggerCases {
		t.Run(tc.name+"/"+tc.key, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, tc.sub)
			p0 := it.Scenario.Setup["p0"]
			onBf, inHand := inZone(p0.Battlefield, tc.name), inZone(p0.Hand, tc.name)
			if tc.inHand && (onBf || !inHand) || !tc.inHand && (!onBf || inHand) {
				t.Fatalf("precondition: %s battlefield=%v hand=%v, want inHand=%v", tc.name, p0.Battlefield, p0.Hand, tc.inHand)
			}
			if tc.probe != "" {
				found := false
				for _, st := range it.Scenario.Steps {
					found = found || strings.HasSuffix(st.Card, ":"+tc.probe)
				}
				if !found {
					t.Fatalf("no step names probe %s: %+v", tc.probe, it.Scenario.Steps)
				}
			}
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			if it.Scenario.Steps[len(it.Scenario.Steps)-1].Op != "resolve" {
				t.Fatalf("last step is not resolve: %+v", it.Scenario.Steps)
			}
			if !firedOnStack(t, it.Scenario, tc.name) {
				t.Fatalf("%s's ability never reached the stack", tc.name)
			}
			control := it.Scenario
			control.Setup = map[string]oraclegen.Seat{}
			for k, seat := range it.Scenario.Setup {
				seat.Battlefield = without(seat.Battlefield, tc.name)
				seat.Hand = without(seat.Hand, tc.name)
				control.Setup[k] = seat
			}
			if res, ok := oraclegen.PlaysThrough(reg, control); ok && len(res.Fails) == 0 && stackHasSource(res.Snapshots, tc.name) {
				t.Fatalf("control: %s's ability is on the stack without the card in play", tc.name)
			}
		})
	}
}

func without(zone []string, name string) []string {
	var out []string
	for _, n := range zone {
		if n != name {
			out = append(out, n)
		}
	}
	return out
}

// TestEventTriggerRecipeShapes pins the shape details a host replay depends
// on: the aura is cast on the Bears before the destroy probe, the loyalty
// activate step names an ability index and carries XMage's rule-text prefix,
// the discard probe targets p0, the lone attacker is alone, and the new phase
// checkpoints.
func TestEventTriggerRecipeShapes(t *testing.T) {
	reg := loadGenRegistry(t)

	aura := triggerRequirement(t, reg, "Ferocity of the Hunt", "trigger#0.0", "trigger.dies-other").Scenario.Steps
	if len(aura) < 4 || aura[0].Op != "cast" || aura[0].Card != "p0:Ferocity of the Hunt" ||
		len(aura[0].Targets) != 1 || aura[0].Targets[0] != "p0:Grizzly Bears" || aura[1].Op != "resolve" ||
		aura[2].Op != "cast" || aura[2].Card != "p0:Murder" || aura[2].Targets[0] != "p0:Grizzly Bears" {
		t.Fatalf("aura steps = %+v, want aura cast on Bears, resolve, Murder on Bears", aura)
	}

	loy := triggerRequirement(t, reg, "Way of the Paradox", "trigger#0.1", "trigger.loyalty-activated")
	act := loy.Scenario.Steps[0]
	if act.Op != "activate" || act.AbilityIndex == nil || len(loy.XAbility) != len(loy.Scenario.Steps) || loy.XAbility[0] == "" {
		t.Fatalf("loyalty activate step = %+v xability=%v", act, loy.XAbility)
	}
	for i, x := range loy.XAbility[1:] {
		if x != "" {
			t.Fatalf("XAbility[%d] = %q on a non-activate step", i+1, x)
		}
	}

	disc := triggerRequirement(t, reg, "Tinybones, Pocket Nuisance", "trigger#0.1", "trigger.discarded")
	if c := disc.Scenario.Steps[0]; c.Op != "cast" || len(c.Targets) != 1 || c.Targets[0] != "p0" {
		t.Fatalf("discard probe step = %+v, want Mind Rot at p0", c)
	}
	if !inZone(disc.Scenario.Setup["p0"].Hand, "Grizzly Bears") {
		t.Fatalf("player-wide discard has no card to discard: %+v", disc.Scenario.Setup["p0"])
	}

	one := triggerRequirement(t, reg, "Yuriko, Blade of the Mighty", "trigger#0.0", "trigger.attacks-one-target").Scenario.Steps[0]
	if one.Op != "attack" || len(one.Attackers) != 1 || one.Defender != "p1" {
		t.Fatalf("attack step = %+v, want one attacker at p1", one)
	}

	main1 := triggerRequirement(t, reg, "Gardenize", "trigger#0.1", "trigger.phase").Scenario.Steps
	if len(main1) != 3 || main1[0].Step != "main2" || main1[1].Step != "main1" || main1[1].Active != "p0" {
		t.Fatalf("main1 steps = %+v, want pass_to main2 then pass_to main1 active p0", main1)
	}
	opp := triggerRequirement(t, reg, "The Theorist, Jace Beleren", "trigger#0.0", "trigger.phase").Scenario.Steps[0]
	if opp.Step != "draw" || opp.Active != "p1" {
		t.Fatalf("opponent draw step = %+v, want draw active p1", opp)
	}
}

// TestEventTriggerGapsStayGaps: the opponent's own-turn trigger has no recipe
// (Solarium Sentry's opponent cast is served now, by the cast-family recipe),
// and Emrakul's cast trigger is settled by level A, so no level-B scenario is
// asked for it. Gideon the Oathless's opponent-loyalty line is served now
// (trigger.ability-activated-opponent); Way of the Mind Sculptor's
// CountersRemovedToPay gate has no cause.
func TestEventTriggerGapsStayGaps(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key string }{
		{"Way of the Mind Sculptor", "trigger#0.1"},
	} {
		c, _ := reg.Lookup(tc.name)
		found := false
		for _, r := range levelb.Requirements(c) {
			if r.Key != tc.key {
				continue
			}
			found = true
			if r.Gap == "" || !strings.HasPrefix(r.Sub, "trigger.gap:") {
				t.Fatalf("%s %s = %+v, want a trigger gap", tc.name, tc.key, r)
			}
			if _, skip := GenerateB(reg, tc.name, r); skip == nil {
				t.Fatalf("%s %s generated an item", tc.name, tc.key)
			}
		}
		if !found {
			t.Fatalf("%s has no requirement %s", tc.name, tc.key)
		}
	}
	c, _ := reg.Lookup("Emrakul, the Exigent Doom")
	for _, r := range levelb.Requirements(c) {
		if r.Key == "trigger#0.0" {
			if !r.CoveredByA || r.Gap != "" {
				t.Fatalf("Emrakul trigger = %+v, want covered by level A", r)
			}
			if _, skip := GenerateB(reg, c.Faces[0].Name, r); skip == nil || !strings.Contains(skip.Reason, "covered by level A") {
				t.Fatalf("Emrakul GenerateB skip = %v, want covered by level A", skip)
			}
			return
		}
	}
	t.Fatal("Emrakul has no trigger#0.0")
}
