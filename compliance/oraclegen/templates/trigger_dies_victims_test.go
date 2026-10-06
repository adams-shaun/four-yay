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

// TestDiesOtherUnplaceableVictimsKeepNamedSkips: a victim the setup cannot
// place is skipped by name, never as a bare "did not fire".
func TestDiesOtherUnplaceableVictimsKeepNamedSkips(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key, want string }{
		{"Predator Ooze", "trigger#0.0", "dies victim must be damaged"},
		{"Teysa, Opulent Oligarch", "trigger#0.1", "dies victim must be a Clue"},
		{"Massacre Girl, Known Killer", "trigger#0.0", "dies victim must have toughness less than 1"},
		{"Ares, God of War", "trigger#0.0", "dies victim must be attacking"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("%s missing", tc.name)
			}
			for _, r := range levelb.Requirements(c) {
				if r.Key != tc.key {
					continue
				}
				if r.Sub != "trigger.dies-other" {
					t.Fatalf("precondition: %s %s classified %s", tc.name, tc.key, r.Sub)
				}
				if _, skip := GenerateB(reg, tc.name, r); skip == nil || !strings.Contains(skip.Reason, tc.want) {
					t.Fatalf("skip = %+v, want a reason naming %q", skip, tc.want)
				}
				return
			}
			t.Fatalf("precondition: %s carries no requirement %s", tc.name, tc.key)
		})
	}
}
