package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

func targetAsk(refs []string, min, max int) rules.OracleDecision {
	return rules.OracleDecision{Step: 1, Kind: "target", Options: 4, Picks: refs, PickRefs: refs, Min: min, Max: max, Via: "answer"}
}

// A divided-amount trigger target (Armament Dragon's distribute three counters)
// is XMage's TargetAmount: the answer is "<ref>^X=<share>", and nothing
// follows it -- chooseTargetAmount completes once the shares reach the total,
// so a trailing skip would be an unconsumed leftover.
func TestDividedTriggerTargetCarriesItsShare(t *testing.T) {
	d := targetAsk([]string{"p0:Armament Dragon"}, 1, 3)
	d.Divided = 3
	got := routed(t, []rules.OracleDecision{d}, 2)
	want := []XAnswer{{0, "target", "p0:Armament Dragon^X=3"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("single divided target: got %v want %v", got, want)
	}
	// Precondition: without the divided total the same ask is the plain pick
	// plus the "up to 3" skip, so the case above exercises the new routing.
	d.Divided = 0
	if got := routed(t, []rules.OracleDecision{d}, 2); !reflect.DeepEqual(got, []XAnswer{{0, "target", "Armament Dragon"}, {0, "target", "[target_skip]"}}) {
		t.Fatalf("plain target ask changed: %v", got)
	}
}

// A divided player target (Gandalf, Spark Starter's damage) keeps the seat ref;
// the driver turns "p0^X=3" into XMage's "targetPlayer=<name>^X=3".
func TestDividedTriggerPlayerTargetKeepsSeatRef(t *testing.T) {
	d := targetAsk([]string{"p0"}, 1, 3)
	d.Divided = 3
	got := routed(t, []rules.OracleDecision{d}, 2)
	if want := []XAnswer{{0, "target", "p0^X=3"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("divided player target: got %v want %v", got, want)
	}
}

// Several counter recipients take the engine's round-robin split (no
// decision carries it); several damage recipients take the engine's own
// damage_split decision, so the target decision adds nothing in front of it.
func TestDividedTriggerTargetsSplitLikeTheEngine(t *testing.T) {
	counters := targetAsk([]string{"p0:Bears#1", "p0:Bears#2"}, 1, 3)
	counters.Divided = 3
	got := routed(t, []rules.OracleDecision{counters}, 2)
	want := []XAnswer{{0, "target", "p0:Bears#1^X=2"}, {0, "target", "p0:Bears#2^X=1"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-robin counters: got %v want %v", got, want)
	}

	damage := targetAsk([]string{"p0", "p1"}, 1, 3)
	damage.Divided = 3
	split := rules.OracleDecision{Step: 1, Kind: "choose_n", Resume: "damage_split", Options: 2, Min: 3, Max: 3,
		Picks: []string{"a", "a", "b"}, PickIdx: []int{0, 0, 1}, PickRefs: []string{"p0", "p0", "p1"}}
	got = routed(t, []rules.OracleDecision{damage, split}, 2)
	want = []XAnswer{{0, "target", "p0^X=2"}, {0, "target", "p1^X=1"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("divided damage: got %v want %v", got, want)
	}
}

// Sting, Bilbo's Sword / Cloak and Dagger, Entwined: the player target is
// followed by an "up to one creature" slot gorge settled without posing.
// XMage still asks it (a Min 0 target is always choosable), so it is skipped.
func TestUnposedUpToSlotIsSkippedAfterPlayerTarget(t *testing.T) {
	d := targetAsk([]string{"p1"}, 1, 1)
	d.UnposedSlots = 1
	got := routed(t, []rules.OracleDecision{d}, 2)
	want := []XAnswer{{0, "target", "p1"}, {0, "target", "[target_skip]"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("player target + unposed slot: got %v want %v", got, want)
	}
	d.UnposedSlots = 0
	if got := routed(t, []rules.OracleDecision{d}, 2); !reflect.DeepEqual(got, []XAnswer{{0, "target", "p1"}}) {
		t.Fatalf("no unposed slot must add nothing: %v", got)
	}
}
