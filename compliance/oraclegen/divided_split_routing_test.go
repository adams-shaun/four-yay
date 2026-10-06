package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// Damage-split routing is keyed on the split's own recipients, not on its step
// and seat. A divided target ask (distribute counters, divided damage) and the
// engine's later "damage_split" KChoose are paired only when they name the same
// target set, so an unrelated same-step split cannot steal the target's
// answers and a split's allocation is emitted at the target ask's own queue
// position, ahead of that ask's chain skips.

// splitDecision builds the engine's damage_split KChoose over refs, one option
// index repeated per point of damage (PickIdx/Picks/PickRefs parallel).
func splitDecision(refs []string, pickIdx []int) rules.OracleDecision {
	picks := make([]string, len(pickIdx))
	pickRefs := make([]string, len(pickIdx))
	for k, i := range pickIdx {
		picks[k] = refs[i]
		pickRefs[k] = refs[i]
	}
	return rules.OracleDecision{Step: 1, Kind: "choose_n", Resume: "damage_split",
		Options: len(refs), Min: len(pickIdx), Max: len(pickIdx),
		Picks: picks, PickIdx: pickIdx, PickRefs: pickRefs}
}

// TestReviewCounterFollowedByUnrelatedDamageSplit: two triggers resolving in
// one step, the first distributing counters over two creatures and the second
// dealing divided damage to two players. The damage split's recipients do not
// match the counter target's, so it must not consume the counter's answers:
// both allocations are emitted, in decision order.
func TestReviewCounterFollowedByUnrelatedDamageSplit(t *testing.T) {
	counter := targetAsk([]string{"p0:Bears#1", "p0:Bears#2"}, 1, 3)
	counter.Divided = 3
	damage := targetAsk([]string{"p0", "p1"}, 1, 3)
	damage.Divided = 3
	split := splitDecision([]string{"p0", "p1"}, []int{0, 0, 1})
	// Precondition: the two target asks name different recipient sets, or the
	// split would legitimately belong to the counter ask and this case would
	// not exercise the bug.
	if sameRecipients(counter.PickRefs, split.PickRefs) {
		t.Fatalf("precondition: counter and damage recipients differ: %v vs %v", counter.PickRefs, split.PickRefs)
	}
	got := routed(t, []rules.OracleDecision{counter, damage, split}, 2)
	want := []XAnswer{
		{0, "target", "p0:Bears#1^X=2"}, {0, "target", "p0:Bears#2^X=1"},
		{0, "target", "p0^X=2"}, {0, "target", "p1^X=1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("counter trigger then damage trigger in same resolve step: got %v want %v", got, want)
	}
}

// TestReviewDividedDamageBeforeUnposedChainSkip: a divided-damage target whose
// ability has a further "up to N" chain slot gorge settled without posing. The
// allocation must be emitted at the target ask's position, followed by the
// skip; a skip before the allocation is a slot XMage reads where its
// TargetAmount answer belongs.
func TestReviewDividedDamageBeforeUnposedChainSkip(t *testing.T) {
	damage := targetAsk([]string{"p0", "p1"}, 1, 3)
	damage.Divided = 3
	damage.UnposedSlots = 1
	split := splitDecision([]string{"p0", "p1"}, []int{0, 0, 1})
	got := routed(t, []rules.OracleDecision{damage, split}, 2)
	want := []XAnswer{
		{0, "target", "p0^X=2"}, {0, "target", "p1^X=1"},
		{0, "target", "[target_skip]"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("divided root before optional chain slot: got %v want %v", got, want)
	}
}

// TestDividedSplitPairingIsPerRecipient: two divided-damage asks in one step
// with different recipients, each followed by its own split. Each allocation
// must land with its own target ask; a greedy step/seat search would send both
// to the last split.
func TestDividedSplitPairingIsPerRecipient(t *testing.T) {
	first := targetAsk([]string{"p0:Bears#1", "p0:Bears#2"}, 1, 3)
	first.Divided = 3
	firstSplit := splitDecision([]string{"p0:Bears#1", "p0:Bears#2"}, []int{0, 1, 1})
	second := targetAsk([]string{"p1:Grizzly Bears", "p1:Serra Angel"}, 1, 3)
	second.Divided = 3
	secondSplit := splitDecision([]string{"p1:Grizzly Bears", "p1:Serra Angel"}, []int{0, 1, 1})
	got := routed(t, []rules.OracleDecision{first, firstSplit, second, secondSplit}, 2)
	want := []XAnswer{
		{0, "target", "p0:Bears#1^X=1"}, {0, "target", "p0:Bears#2^X=2"},
		{0, "target", "p1:Grizzly Bears^X=1"}, {0, "target", "p1:Serra Angel^X=2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("two divided asks in one step: got %v want %v", got, want)
	}
}

// TestBareDamageSplitStillEmitsItsShares: a cast-step divided spell (Fury,
// Forked Bolt) poses its targets through castSpell, so the resolve-step split
// has no preceding target decision and must still emit its own shares. This is
// the shape the existing damage-split tests pin; kept here to prove the
// pairing did not suppress the unpaired split.
func TestBareDamageSplitStillEmitsItsShares(t *testing.T) {
	split := splitDecision([]string{"p1:Grizzly Bears", "p1:Serra Angel"}, []int{0, 0, 1})
	got := routed(t, []rules.OracleDecision{split}, 2)
	want := []XAnswer{
		{0, "target", "p1:Grizzly Bears^X=2"}, {0, "target", "p1:Serra Angel^X=1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bare damage split: got %v want %v", got, want)
	}
}
