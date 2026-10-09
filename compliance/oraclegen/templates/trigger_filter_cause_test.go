package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/state"
)

// TestTriggerCauseFollowsTheFilter: a trigger whose cause recipe used a fixed
// probe the trigger's own filter rejects is served by a cause the filter
// accepts, and gorge shows the card's own trigger on the stack. Each case pins
// the shape the filter names, so a regression to the old fixed probe (or to the
// old card-as-target default) fails loudly rather than skipping.
func TestTriggerCauseFollowsTheFilter(t *testing.T) {
	reg := loadGenRegistry(t)
	cases := []struct {
		name, key, sub string
		// filter is the trigger parameter the cause's object must satisfy.
		filter string
		// inGraveyard marks a source whose trigger functions from the graveyard,
		// so the precondition checks the graveyard rather than the battlefield.
		inGraveyard bool
		check       func(t *testing.T, it oraclegen.Item)
	}{
		{
			// ValidAttackers$ Dinosaur.YouCtrl: a Dinosaur must attack. The
			// source is an enchantment, so the old recipe's Grizzly Bears (not a
			// Dinosaur) never fired the trigger.
			"Poetic Ingenuity", "trigger#0.0", "trigger.attacks", "Dinosaur.YouCtrl", false,
			func(t *testing.T, it oraclegen.Item) {
				assertAttackProbeMatches(t, reg, it, "Dinosaur.YouCtrl")
			},
		},
		{
			// ValidCard$ Mount.YouCtrl,Vehicle.YouCtrl: a Mount or Vehicle must
			// attack, not the source Human Druid.
			"Miriam, Herd Whisperer", "trigger#0.0", "trigger.attacks", "Mount.YouCtrl,Vehicle.YouCtrl", false,
			func(t *testing.T, it oraclegen.Item) {
				assertAttackProbeMatches(t, reg, it, "Mount.YouCtrl,Vehicle.YouCtrl")
			},
		},
		{
			// The source is a Wall (Defender), so it can never be the attacker:
			// another creature the filter accepts must attack.
			"Fire Navy Trebuchet", "trigger#0.0", "trigger.attacks", "Creature.YouCtrl", false,
			func(t *testing.T, it oraclegen.Item) {
				atk := attackStep(t, it.Scenario.Steps).Attackers
				if len(atk) != 1 {
					t.Fatalf("attackers = %v, want exactly one", atk)
				}
				if strings.TrimPrefix(atk[0], "p0:") == "Fire Navy Trebuchet" {
					t.Fatalf("attacker %s is the Wall source, which cannot attack", atk[0])
				}
			},
		},
		{
			// TriggerZones$ Graveyard: the source is in the graveyard, so a Rat
			// the filter accepts must attack instead.
			"Persistent Marshstalker", "trigger#0.0", "trigger.attacks", "Creature.YouCtrl+Rat", true,
			func(t *testing.T, it oraclegen.Item) {
				assertAttackProbeMatches(t, reg, it, "Creature.YouCtrl+Rat")
			},
		},
		{
			// ValidCard$ Detective.YouCtrl: a Detective must enter. The curated
			// probe list carried no Detective, so nothing fired.
			"Case of the Pilfered Proof", "trigger#0.0", "trigger.etb-other", "Detective.YouCtrl", false,
			func(t *testing.T, it oraclegen.Item) {
				assertProbeMatches(t, reg, castCardName(t, it.Scenario.Steps), "Detective.YouCtrl")
			},
		},
		{
			// TargetsValid$ Creature.YouCtrl+inZoneBattlefield: the probe spell
			// must target a creature you control. The old recipe targeted the
			// enchantment source, an illegal Shock/Giant Growth target.
			"Leyline of Resonance", "trigger#0.0", "trigger.spell-cast", "Instant,Sorcery", false,
			func(t *testing.T, it oraclegen.Item) {
				target := firstCastTarget(t, it.Scenario.Steps)
				if !strings.HasPrefix(target, "p0:") {
					t.Fatalf("probe target %s is not a permanent you control", target)
				}
				assertProbeMatches(t, reg, target, "Creature.YouCtrl")
			},
		},
		{
			// The dies trigger fires behind a sibling Valiant "becomes the
			// target" trigger, so the destroy spell needs a second pass cycle
			// before the dies trigger reaches the stack.
			"Heartfire Hero", "trigger#0.1", "trigger.dies", "Card.Self", false,
			func(t *testing.T, it oraclegen.Item) {
				if !triggerSlotOnStackDeep(t, reg, it.Scenario, "Heartfire Hero", "1") {
					t.Fatalf("Heartfire Hero's dies trigger (slot 1) never reaches the stack: %+v", it.Scenario.Steps)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, tc.sub)
			p0 := it.Scenario.Setup["p0"]
			if tc.inGraveyard {
				if !containsString(p0.Graveyard, tc.name) || containsString(p0.Battlefield, tc.name) {
					t.Fatalf("precondition: %s must be in the graveyard only: graveyard=%v battlefield=%v", tc.name, p0.Graveyard, p0.Battlefield)
				}
			} else if !containsString(p0.Battlefield, tc.name) {
				t.Fatalf("precondition: trigger source %s is not on p0's battlefield %v", tc.name, p0.Battlefield)
			}
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("scenario does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			if !triggerShownOnStack(t, reg, it.Scenario, tc.name) {
				t.Fatalf("%s's trigger never appears on the stack: %+v", tc.name, it.Scenario.Steps)
			}
			tc.check(t, it)
		})
	}
}

// assertAttackProbeMatches: the item's attack sends exactly one attacker and
// the trigger's own filter accepts it.
func assertAttackProbeMatches(t *testing.T, reg *cards.Registry, it oraclegen.Item, filter string) {
	t.Helper()
	atk := attackStep(t, it.Scenario.Steps).Attackers
	if len(atk) != 1 {
		t.Fatalf("attackers = %v, want exactly one", atk)
	}
	assertProbeMatches(t, reg, atk[0], filter)
}

// assertProbeMatches fails unless the engine's own matcher accepts the named
// probe for filter -- the exact reading the trigger uses, so a probe the
// trigger's filter rejects (the old fixed Grizzly Bears) fails here.
func assertProbeMatches(t *testing.T, reg *cards.Registry, ref, filter string) {
	t.Helper()
	name := strings.TrimPrefix(strings.TrimPrefix(ref, "p0:"), "p1:")
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		t.Fatalf("precondition: probe %s not in the corpus", name)
	}
	fp := newFilterProbe(filter, state.ZBattlefield)
	if !fp.decided {
		t.Fatalf("precondition: filter %q is undecided, cannot assert", filter)
	}
	if !fp.accepts(c) {
		t.Fatalf("probe %s is rejected by filter %q", name, filter)
	}
}

// castCardName is the probe spell the item casts, without its seat prefix.
func castCardName(t *testing.T, steps []oraclegen.Step) string {
	t.Helper()
	for _, st := range steps {
		if st.Op == "cast" {
			return strings.TrimPrefix(st.Card, "p0:")
		}
	}
	t.Fatalf("precondition: scenario has no cast step: %+v", steps)
	return ""
}

// firstCastTarget is the target of the item's first cast step that names one.
func firstCastTarget(t *testing.T, steps []oraclegen.Step) string {
	t.Helper()
	for _, st := range steps {
		if st.Op == "cast" && len(st.Targets) > 0 {
			return st.Targets[0]
		}
	}
	t.Fatalf("precondition: no cast step names a target: %+v", steps)
	return ""
}

// triggerSlotOnStackDeep strips the item's trailing resolves and replays the
// cause with two and four pass cycles, checking the trigger at the requested
// slot reaches the stack. A dies trigger behind a sibling needs the second
// cycle; the plain two-pass form is tried first so nothing else changes.
func triggerSlotOnStackDeep(t *testing.T, reg *cards.Registry, sc oraclegen.Scenario, name, slot string) bool {
	t.Helper()
	var cause []oraclegen.Step
	for _, st := range sc.Steps {
		if st.Op != "resolve" {
			cause = append(cause, st)
		}
	}
	two := []oraclegen.Step{{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}}
	variants := [][]oraclegen.Step{
		append(append([]oraclegen.Step(nil), cause...), two...),
		append(append(append([]oraclegen.Step(nil), cause...), two...), two...),
	}
	for _, steps := range variants {
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
