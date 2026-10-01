package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// grantDecision builds a KTarget decision for seat 0 carrying a TargetEffect
// with the given granted static modes, and returns the option indices Decide
// chose. It mirrors targetDecision but threads the polarity-carrying payload
// the engine's describeTargetEffect now publishes for an Effect grant.
func grantDecision(b Board, options []tgt, statics []string) ([]int, *decision.Decision) {
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1,
		TargetEffect: &decision.TargetEffect{API: "Effect", Statics: statics}}
	for _, o := range options {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: o.kind, Obj: o.obj, Player: o.pl})
	}
	return Decide(b, &d, rng(1)).Choices, &d
}

// TestTargetGrantAimsOwnNotOpponent is the fb-20260927T153930Z regression:
// Whirler Rogue's "target creature can't be blocked this turn" is a one-way
// BOON, so a bot handed the seat's own 5/5, an opponent's 2/2 and the
// opponent's face must point the grant at its OWN creature. Before the
// polarity branch the plain path (R1/R3) led with the opponent and aimed the
// "unblockable" grant at the enemy's board -- measured in the report at seq
// 4237-4257, where the seat spent two artifacts to make an OPPONENT's Serra
// Avenger unblockable. The opponent's 2/2 is the higher-threat FOREIGN option
// (and the 5/5 is listed first, so a positional pick fails too), so this test
// fails for every pre-fix ranker, not just the greedy-foreign one.
func TestTargetGrantAimsOwnNotOpponent(t *testing.T) {
	b := boardOf(atk(1, 5, 5), def(1, 2, 2))
	got, d := grantDecision(b, []tgt{mine(101), opp(201), face()}, []string{"CantBlockBy"})
	if len(got) != 1 {
		t.Fatalf("grant target = %v, want exactly one choice", got)
	}
	// Precondition: the offered set really does present a foreign option, so
	// a "picked own" result is not the all-own fallback passing by accident.
	if len(d.Options) < 2 || d.Options[1].Player != 1 {
		t.Fatalf("precondition: no opposing option offered: %+v", d.Options)
	}
	if d.Options[got[0]].Obj != 101 {
		t.Fatalf("grant aimed at option %d (%+v), want the seat's own 5/5 (obj 101)",
			got[0], d.Options[got[0]])
	}
}

// TestTargetGrantTotalityPicksWeakestOpponent is the totality floor of the
// grant-polarity rule (R1B, target.go): a posed KTarget decision MUST be
// answered. The ability scorer's A1c decline (botpolicy/ability.go) refuses
// the ordinary no-own-creature activation before any cost is paid, so the
// only way a bot reaches a boon ask with no own option is an activation
// whose COST consumed the seat's last own creature between the activation
// and the ask. For that residue the branch hands the boon to the LEAST
// threatening foreign option -- a boon's value to its receiver scales with
// the creature's worth, so the weakest receiver loses the least -- never
// the gift-maximising "opponent's best creature" the previous fallback
// aimed at. The 6/6 is also the LOWEST index, so a positional top-up would
// land on it too; only the harm-minimising pick lands on the 2/2.
func TestTargetGrantTotalityPicksWeakestOpponent(t *testing.T) {
	b := boardOf(def(2, 6, 6), def(1, 2, 2))
	got, d := grantDecision(b, []tgt{opp(202), opp(201), face()}, []string{"CantBlockBy"})
	if len(got) != 1 {
		t.Fatalf("no-own grant target = %v, want exactly one choice", got)
	}
	// Precondition: the board offers no own creature, so the totality floor
	// is genuinely exercised, and the two foreign threats really differ.
	if _, ok := b.Creatures.Lookup(101); ok {
		t.Fatal("precondition: board unexpectedly offers an own creature")
	}
	if b.Creatures.Get(201).threat() == b.Creatures.Get(202).threat() {
		t.Fatal("precondition: foreign threats are equal; the test cannot distinguish the rank")
	}
	if objAt(d, got[0]) != 201 {
		t.Fatalf("totality grant target = obj %d (%+v), want the WEAKEST opponent creature (the 2/2, obj 201), not the 6/6 gift",
			objAt(d, got[0]), d.Options[got[0]])
	}
}

// TestTargetWithoutGrantStaticsKeepsHostilePolarity guards that the fix is
// ADDITIVE: the same option set with NO granted statics (an unknown or
// removal-shaped Effect) keeps today's R1 lead-with-the-opponent behaviour,
// so the removal path -- and every unreadable Effect -- is unchanged. The
// own 5/5 is offered first, so a wrong self-pick would also fail here.
func TestTargetWithoutGrantStaticsKeepsHostilePolarity(t *testing.T) {
	b := boardOf(atk(1, 5, 5), def(1, 2, 2))
	got, d := grantDecision(b, []tgt{mine(101), opp(201), face()}, nil)
	if len(got) != 1 {
		t.Fatalf("unknown-effect target = %v, want exactly one choice", got)
	}
	if d.Options[got[0]].Player != 1 {
		t.Fatalf("unknown-effect path picked option %d (%+v), want an opponent target (R1 unchanged)",
			got[0], d.Options[got[0]])
	}
	// And a mode outside the beneficial set (the harmful CantRegenerate) must
	// stay on the hostile path too.
	got2, d2 := grantDecision(b, []tgt{mine(101), opp(201), face()}, []string{"CantRegenerate"})
	if len(got2) != 1 || d2.Options[got2[0]].Player != 1 {
		t.Fatalf("harmful-mode grant = %v (%+v), want an opponent target",
			got2, d2.Options)
	}
}

// TestTargetGrantPicksHighestThreatOwn is the ranker half: with several own
// creatures the boon goes to the most threatening one (threat(), not option
// order), the same value ranking R3 uses on the hostile path. The 4/4 flier
// outranks the 3/3 and is offered LAST, so a positional pick fails.
func TestTargetGrantPicksHighestThreatOwn(t *testing.T) {
	b := boardOf(atk(1, 3, 3), atk(2, 4, 4, "Flying"))
	got, d := grantDecision(b, []tgt{mine(101), mine(102)}, []string{"CantBlockBy"})
	if len(got) != 1 {
		t.Fatalf("all-own grant target = %v, want exactly one choice", got)
	}
	if objAt(d, got[0]) != 102 {
		t.Fatalf("grant target = obj %d, want the 4/4 flier (obj 102)", objAt(d, got[0]))
	}
	// Precondition: the two own threats really do differ.
	if b.Creatures.Get(101).threat() == b.Creatures.Get(102).threat() {
		t.Fatal("precondition: own threats are equal; the test cannot distinguish the rank")
	}
}
