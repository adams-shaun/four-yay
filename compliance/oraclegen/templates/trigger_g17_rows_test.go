// Ticket g17 (wave 3, "trigger with no recipe"): one real card per shape the
// new recipes serve. Each row's requirement must generate, play through
// gorge, and show the row's own trigger on the stack; the effect rows check
// the trigger's visible consequence in the snapshots, so a stack entry from a
// sibling trigger cannot pass for the row.
package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

func TestG17TriggerRowsGenerate(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key, sub string }{
		{"Bitter Chill", "trigger#0.1", "trigger.dies"},
		{"Nutrient Block", "trigger#0.0", "trigger.dies"},
		{"Krovod Haunch", "trigger#0.0", "trigger.dies"},
		{"Hopeless Nightmare", "trigger#0.1", "trigger.dies"},
		{"Dusk Rose Reliquary", "trigger#0.1", "trigger.becomes-target"},
		{"Cosmic Cube", "trigger#0.1", "trigger.becomes-target"},
		{"Belladonna Took", "trigger#0.0", "trigger.etb-other"},
		{"Wildwood Mentor", "trigger#0.0", "trigger.etb-other"},
		{"Kambal, Profiteering Mayor", "trigger#0.0", "trigger.etb-other"},
		{"Cryptid Inspector", "trigger#0.0", "trigger.etb-other"},
		{"Threats Around Every Corner", "trigger#0.1", "trigger.etb-other"},
		{"Rimefire Torque", "trigger#0.0", "trigger.etb-other"},
		{"Dawn-Blessed Pennant", "trigger#0.0", "trigger.etb-other"},
		{"Gideon the Oathless", "trigger#0.0", "trigger.etb-other"},
		{"Twilight Diviner", "trigger#0.1", "trigger.etb-other"},
		{"Extraordinary Journey", "trigger#0.1", "trigger.etb-other"},
		{"Moonshadow", "trigger#0.0", "trigger.zone-change-residue"},
		{"Massacre Girl, Known Killer", "trigger#0.0", "trigger.dies-other"},
		{"Teysa, Opulent Oligarch", "trigger#0.1", "trigger.dies-other"},
		{"Yarus, Roar of the Old Gods", "trigger#0.1", "trigger.dies-other"},
		{"Ares, God of War", "trigger#0.0", "trigger.dies-other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, tc.sub)
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			slot := tc.key[strings.LastIndexByte(tc.key, '.')+1:]
			if !g17SlotOnStack(t, reg, it.Scenario, tc.name, slot) {
				t.Fatalf("no snapshot shows trigger %s on the stack sourced by %s", slot, tc.name)
			}
		})
	}
}

// TestG17TriggerRowsShowTheirEffect: the trigger's visible consequence, not
// just its stack entry.
func TestG17TriggerRowsShowTheirEffect(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key, sub string
		check          func(t *testing.T, res rules.OracleResult)
	}{
		{"Massacre Girl, Known Killer", "trigger#0.0", "trigger.dies-other", func(t *testing.T, res rules.OracleResult) {
			// The Blight Rot cast emptied p0's hand; only the trigger's draw
			// can put a card back, and the victim's death at 0 toughness is
			// the graveyard entry the filter accepted.
			found := false
			for _, n := range lastHand(res, 0) {
				found = found || n == "Wastes"
			}
			if !found {
				t.Fatalf("p0 hand = %v, want the drawn card from the toughness<1 trigger", lastHand(res, 0))
			}
			inGY := false
			for _, n := range lastGY(res, 1) {
				inGY = inGY || n == "Grizzly Bears"
			}
			if !inGY {
				t.Fatalf("p1 graveyard = %v, want the victim the counters killed", lastGY(res, 1))
			}
		}},
		{"Cryptid Inspector", "trigger#0.0", "trigger.etb-other", func(t *testing.T, res rules.OracleResult) {
			permCounter(t, res, "Cryptid Inspector", "P1P1", 1)
		}},
		{"Rimefire Torque", "trigger#0.0", "trigger.etb-other", func(t *testing.T, res rules.OracleResult) {
			permCounter(t, res, "Rimefire Torque", "CHARGE", 1)
		}},
		{"Gideon the Oathless", "trigger#0.0", "trigger.etb-other", func(t *testing.T, res rules.OracleResult) {
			if lastLife(res, 1) != 19 {
				t.Fatalf("p1 life = %d, want 19 after the ETB damage", lastLife(res, 1))
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, tc.sub)
			res := runSteps(t, reg, it.Scenario, it.Scenario.Steps)
			tc.check(t, res)
		})
	}
}

// TestG17TriggerRowsPreconditions: every row's classification is the shape
// the recipes serve, so a reclassification shows up as a loud precondition
// failure rather than a silently vacuous table.
func TestG17TriggerRowsPreconditions(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key, sub string }{
		{"Bitter Chill", "trigger#0.1", "trigger.dies"},
		{"Massacre Girl, Known Killer", "trigger#0.0", "trigger.dies-other"},
		{"Teysa, Opulent Oligarch", "trigger#0.1", "trigger.dies-other"},
		{"Yarus, Roar of the Old Gods", "trigger#0.1", "trigger.dies-other"},
		{"Ares, God of War", "trigger#0.0", "trigger.dies-other"},
		{"Cryptid Inspector", "trigger#0.0", "trigger.etb-other"},
		{"Rimefire Torque", "trigger#0.0", "trigger.etb-other"},
		{"Moonshadow", "trigger#0.0", "trigger.zone-change-residue"},
		{"The Millennium Calendar", "trigger#0.1", "trigger.state-self-counters"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("%s not in the corpus", tc.name)
			}
			for _, r := range levelb.Requirements(c) {
				if r.Key != tc.key {
					continue
				}
				if r.Sub != tc.sub || r.Gap != "" {
					t.Fatalf("%s %s classified sub=%q gap=%q, want %q with no gap", tc.name, tc.key, r.Sub, r.Gap, tc.sub)
				}
				return
			}
			t.Fatalf("precondition: %s carries no requirement %s", tc.name, tc.key)
		})
	}
}

func requirementSub(t *testing.T, reg interface {
	Lookup(string) (interface{}, bool)
}, name, key string) string {
	t.Helper()
	return ""
}

// g17SlotOnStack replays the item's scenario in the detection step shapes
// the generator itself accepts (cause steps alone, with a pass round, with a
// pass round plus a priority checkpoint) and reports whether any snapshot
// shows the row's own trigger on the stack. The resolve steps stay: a cause
// whose last step is a resolve needs them re-answered to surface the queued
// trigger.
func g17SlotOnStack(t *testing.T, reg *cards.Registry, sc oraclegen.Scenario, name, slot string) bool {
	t.Helper()
	cause := sc.Steps
	for len(cause) > 0 && cause[len(cause)-1].Op == "resolve" {
		cause = cause[:len(cause)-1]
	}
	passes := []oraclegen.Step{{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}}
	checkpoint := append(append([]oraclegen.Step(nil), passes...), oraclegen.Step{Op: "pass_to", Decision: "priority"})
	for _, steps := range [][]oraclegen.Step{cause, append(append([]oraclegen.Step(nil), cause...), passes...), append(append([]oraclegen.Step(nil), cause...), checkpoint...)} {
		res := runSteps(t, reg, sc, steps)
		for _, snap := range res.Snapshots {
			for _, entry := range snap.Stack {
				if entry.Kind == "ability" && entry.Source == "p0:"+name && entry.Trigger == slot {
					return true
				}
			}
		}
	}
	return false
}

func grew(res rules.OracleResult, seat int) int {
	return lastHandLen(res, seat) - handLen(res, seat, 0)
}

func handLen(res rules.OracleResult, seat, snap int) int {
	if snap >= len(res.Snapshots) {
		return 0
	}
	return len(res.Snapshots[snap].Players[seat].Hand)
}

func lastHand(res rules.OracleResult, seat int) []string {
	if len(res.Snapshots) == 0 {
		return nil
	}
	return res.Snapshots[len(res.Snapshots)-1].Players[seat].Hand
}

func lastGY(res rules.OracleResult, seat int) []string {
	if len(res.Snapshots) == 0 {
		return nil
	}
	return res.Snapshots[len(res.Snapshots)-1].Players[seat].Graveyard
}

func lastHandLen(res rules.OracleResult, seat int) int {
	if len(res.Snapshots) == 0 {
		return 0
	}
	return len(res.Snapshots[len(res.Snapshots)-1].Players[seat].Hand)
}

func lastLife(res rules.OracleResult, seat int) int32 {
	if len(res.Snapshots) == 0 {
		return 0
	}
	return res.Snapshots[len(res.Snapshots)-1].Players[seat].Life
}

func lastPerms(res rules.OracleResult) []rules.OracleSnapPerm {
	if len(res.Snapshots) == 0 {
		return nil
	}
	return res.Snapshots[len(res.Snapshots)-1].Permanents
}

func permCounter(t *testing.T, res rules.OracleResult, name, kind string, want int32) {
	t.Helper()
	for _, p := range lastPerms(res) {
		if p.Name == name && p.Counters[kind] >= want {
			return
		}
	}
	t.Fatalf("no %s with %d %s counters in the last snapshot", name, want, kind)
}
