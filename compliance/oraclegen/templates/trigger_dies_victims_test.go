package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// victimOf returns the permanent the item's cause destroys: the target of its
// last cast step, as "p<seat>:<name>", and the probe that casts it.
func victimOf(t *testing.T, it oraclegen.Item) (target, probe string) {
	t.Helper()
	for _, st := range it.Scenario.Steps {
		if st.Op == "cast" && len(st.Targets) == 1 {
			target, probe = st.Targets[0], strings.TrimPrefix(st.Card, "p0:")
		}
	}
	if target == "" {
		t.Fatalf("precondition: no cast step with a target: %+v", it.Scenario.Steps)
	}
	return target, probe
}

// slotOf is the trigger slot a requirement key names ("trigger#0.1" -> "1").
func slotOf(key string) string { return key[strings.LastIndex(key, ".")+1:] }

// TestDiesOtherVictimsFollowTheFilter generates real cards whose filter names
// a side, a card type or a qualifier, and proves for each that the victim is
// on the side and of the type the filter names, that the destroy probe fits
// the victim, that the item plays through gorge, and that the trigger reaches
// the stack on the cause (and not without the card).
func TestDiesOtherVictimsFollowTheFilter(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key string
		side      string // seat of the victim
		typ       string // type the victim must have
		notBears  bool   // the filter rejects the Grizzly Bears, so the victim is another card
		probes    []string
		counter   bool
	}{
		{"Massacre Wurm", "trigger#0.1", "p1", "Creature", false, []string{"Murder", "Doom Blade", "Terror"}, false},
		{"Vein Ripper", "trigger#0.0", "p0", "Creature", false, []string{"Murder"}, false},
		{"Great Fierce Bee", "trigger#0.0", "p0", "Creature", false, []string{"Murder"}, false},
		{"Chainsaw", "trigger#0.1", "p0", "Creature", false, []string{"Murder"}, false},
		{"Ashiok's Reaper", "trigger#0.0", "p0", "Enchantment", false, []string{"Disenchant", "Naturalize"}, false},
		{"Wicked Visitor", "trigger#0.0", "p0", "Enchantment", false, []string{"Disenchant", "Naturalize"}, false},
		{"Krenko, Baron of Tin Street", "trigger#0.0", "p0", "Artifact", false, []string{"Murder"}, false},
		{"Boggart Cursecrafter", "trigger#0.0", "p0", "Creature", true, []string{"Murder"}, false},
		{"Akki Ember-Keeper", "trigger#0.0", "p0", "Creature", false, []string{"Murder"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, "trigger.dies-other")
			target, probe := victimOf(t, it)
			seat, victim, _ := strings.Cut(target, ":")
			if seat != tc.side {
				t.Fatalf("victim %s is on %s, want %s", target, seat, tc.side)
			}
			// The victim stands on its side's battlefield.
			if !inZone(it.Scenario.Setup[seat].Battlefield, victim) {
				t.Fatalf("victim %s is not on %s's battlefield: p0=%v p1=%v", victim, seat, it.Scenario.Setup["p0"].Battlefield, it.Scenario.Setup["p1"].Battlefield)
			}
			vc, ok := reg.Lookup(victim)
			if !ok {
				t.Fatalf("victim %s not in the corpus", victim)
			}
			vf := vc.Faces[0]
			if !oraclegen.HasType(vf, tc.typ) || tc.notBears && victim == bearsProbe {
				t.Fatalf("victim %s is not a %s (or is the Bears the filter rejects)", victim, tc.typ)
			}
			if !inZone(tc.probes, probe) {
				t.Fatalf("destroy probe %s, want one of %v", probe, tc.probes)
			}
			if tc.counter && it.Scenario.Setup[seat].Counters[victim]["P1P1"] != 1 {
				t.Fatalf("victim %s carries no counter: %+v", victim, it.Scenario.Setup[seat])
			}
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			slot := slotOf(tc.key)
			if !stackedAfterCause(t, reg, it.Scenario, tc.name, slot) {
				t.Fatalf("%s trigger %s never reaches the stack", tc.name, slot)
			}
			control := it.Scenario
			control.Setup = map[string]oraclegen.Seat{}
			for k, s := range it.Scenario.Setup {
				s.Battlefield = without(s.Battlefield, tc.name)
				control.Setup[k] = s
			}
			if stackedAfterCause(t, reg, control, tc.name, slot) {
				t.Fatalf("control: %s's trigger is on the stack without the card in play", tc.name)
			}
		})
	}
}

// TestDiesOtherEquipmentIsAttachedBeforeTheKill: Lead Pipe's trigger names the
// equipped creature, so the Equipment is attached to the Bears by a prelude
// step ahead of the destroy probe.
func TestDiesOtherEquipmentIsAttachedBeforeTheKill(t *testing.T) {
	reg := loadGenRegistry(t)
	it := triggerRequirement(t, reg, "Lead Pipe", "trigger#0.0", "trigger.dies-other")
	steps := it.Scenario.Steps
	attach, cast := -1, -1
	for i, st := range steps {
		switch st.Op {
		case "attach":
			attach = i
			if st.Card != "p0:Lead Pipe" || st.AttachedTo != "p0:Grizzly Bears" {
				t.Fatalf("attach step = %+v, want Lead Pipe onto the Bears", st)
			}
		case "cast":
			cast = i
		}
	}
	if attach < 0 || cast < 0 || attach > cast {
		t.Fatalf("steps = %+v, want an attach before the destroy cast", steps)
	}
	if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
		t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
	}
	if !stackedAfterCause(t, reg, it.Scenario, "Lead Pipe", "0") {
		t.Fatalf("Lead Pipe's trigger never reaches the stack")
	}
}

// TestDiesOtherDamagedVictimKeepsNamedSkip: the one victim qualifier no
// cause here serves — "damaged" — keeps its named skip, never a bare "did
// not fire" (ticket g17 serves the other victim qualifiers).
func TestDiesOtherDamagedVictimKeepsNamedSkip(t *testing.T) {
	reg := loadGenRegistry(t)
	c, ok := reg.Lookup("Trophy Hunter")
	if !ok {
		t.Fatalf("Trophy Hunter missing")
	}
	for _, r := range levelb.Requirements(c) {
		if r.Key != "trigger#0.0" {
			continue
		}
		if r.Sub != "trigger.dies-other" {
			t.Fatalf("precondition: Trophy Hunter trigger#0.0 classified %s", r.Sub)
		}
		if _, skip := GenerateB(reg, "Trophy Hunter", r); skip == nil || !strings.Contains(skip.Reason, "dies victim must be damaged") {
			t.Fatalf("skip = %+v, want a reason naming %q", skip, "dies victim must be damaged")
		}
		return
	}
	t.Fatalf("precondition: Trophy Hunter carries no requirement trigger#0.0")
}

// TestDiesOtherSpecialVictimsAreServed: a dies-other victim qualifier the
// placed-victim walk cannot satisfy (ticket g17) is served by its dedicated
// cause, and the scenario names the mechanism — the Clue dies as a token
// after its maker, a Blight Rot cast takes the victim to 0 toughness, an
// attacking victim dies mid-combat — mirroring
// TestTriggerETBProbeSpecialFilters' mechanism assertions.
func TestDiesOtherSpecialVictimsAreServed(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key string
		served    func(t *testing.T, it oraclegen.Item)
	}{
		{"Teysa, Opulent Oligarch", "trigger#0.1", func(t *testing.T, it oraclegen.Item) {
			// The Clue maker's death creates the token; an artifact destroy
			// then kills the token, which is what the trigger reads.
			tokenKilled := false
			for _, st := range it.Scenario.Steps {
				if st.Op == "cast" {
					for _, tg := range st.Targets {
						tokenKilled = tokenKilled || strings.Contains(tg, "token:Clue")
					}
				}
			}
			if !tokenKilled {
				t.Fatalf("no step kills the Clue token: steps = %+v", it.Scenario.Steps)
			}
		}},
		{"Massacre Girl, Known Killer", "trigger#0.0", func(t *testing.T, it oraclegen.Item) {
			// The counters take the victim to 0 toughness on the side the
			// filter names, so the state-based action sends it to the
			// graveyard.
			countered := false
			for _, st := range it.Scenario.Steps {
				if st.Op == "cast" && strings.Contains(st.Card, "Blight Rot") {
					for _, tg := range st.Targets {
						countered = countered || tg == "p1:Grizzly Bears"
					}
				}
			}
			if !countered {
				t.Fatalf("no Blight Rot cast counters p1's victim: steps = %+v", it.Scenario.Steps)
			}
		}},
		{"Ares, God of War", "trigger#0.0", func(t *testing.T, it oraclegen.Item) {
			// The victim attacks and is destroyed mid-combat, so its LKI
			// carries the attacking state the filter reads.
			attacked := false
			destroyed := false
			for _, st := range it.Scenario.Steps {
				if st.Op == "attack" {
					for _, a := range st.Attackers {
						attacked = attacked || a == "p0:Grizzly Bears"
					}
				}
				if st.Op == "cast" {
					for _, tg := range st.Targets {
						destroyed = destroyed || tg == "p0:Grizzly Bears"
					}
				}
			}
			if !attacked || !destroyed {
				t.Fatalf("attack step or victim destroy missing: steps = %+v", it.Scenario.Steps)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, "trigger.dies-other")
			tc.served(t, it)
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
		})
	}
}
