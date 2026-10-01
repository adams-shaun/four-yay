package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// tgt is a KTarget test helper tuple: option kind, object id, and owning
// seat (0 = the deciding seat itself, 1 = the only opponent these tests
// use).
type tgt struct {
	kind string
	obj  state.ObjID
	pl   state.PlayerID
}

// targetDecision builds a KTarget decision for seat 0 from option tuples
// and returns the option indices Decide chose. The option's index is its
// position in opts, matching how the engine numbers options (Index ==
// position, the invariant the targeting heuristic's deterministic tiebreak
// relies on).
func targetDecision(b Board, options []tgt, min, max int) ([]int, *decision.Decision) {
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KTarget, Min: min, Max: max}
	for _, o := range options {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: o.kind, Obj: o.obj, Player: o.pl})
	}
	return Decide(b, &d, rng(1)).Choices, &d
}

// face is the opponent seat 1 as a player-target tuple.
func face() tgt { return tgt{kind: "player", pl: 1} }

// opp names one of seat 1's creatures as a permanent-target tuple.
func opp(obj state.ObjID) tgt { return tgt{kind: "permanent", obj: obj, pl: 1} }

// objAt is a test convenience: the object id of option index i, with an
// out-of-range index reading 0 (no such object).
func objAt(d *decision.Decision, i int) state.ObjID {
	if i < 0 || i >= len(d.Options) {
		return 0
	}
	return d.Options[i].Obj
}

// mine names one of seat 0's own creatures as a permanent-target tuple.
func mine(obj state.ObjID) tgt { return tgt{kind: "permanent", obj: obj, pl: 0} }

// TestTargetNeverOwnWhileOpponentOffered is R1: the deciding seat never
// targets its own permanent while any opposing option exists, even when its
// own creature is the most threatening permanent on offer. Handed an "any
// target" choice of its own 5/5, the opponent's 2/2 and the opponent's
// face, the bot must decline the 5/5 and take an opponent target -- the old
// policy's two defects (prefer the face, or take Options[0], which here is
// the seat's own creature) both fail this, and so does a greedy self-target.
func TestTargetNeverOwnWhileOpponentOffered(t *testing.T) {
	b := boardOf(atk(1, 5, 5), def(1, 2, 2))
	got, d := targetDecision(b, []tgt{mine(101), opp(201), face()}, 1, 1)
	if len(got) != 1 {
		t.Fatalf("target = %v, want exactly one choice", got)
	}
	idx := got[0]
	if d.Options[idx].Obj == 101 {
		t.Fatalf("targeted own creature (option %d = %+v), want an opponent target", idx, d.Options[idx])
	}
	if d.Options[idx].Player != 1 {
		t.Fatalf("targeted option %d (%+v), want an opponent (seat 1) target", idx, d.Options[idx])
	}
}

// TestTargetEveryOptionIsOursFallsBackToBestOwn is R1's other side: when
// every legal target is the deciding seat's own (a pump, an aura, a
// recursion that offers only their own board), the bot does not hand the
// engine an empty or invalid answer — it points at its own best creature,
// ranked the same way, so the self-serving effect hits the permanent that
// matters most.
func TestTargetEveryOptionIsOursFallsBackToBestOwn(t *testing.T) {
	b := boardOf(atk(1, 2, 2), atk(2, 4, 4))
	got, d := targetDecision(b, []tgt{mine(101), mine(102)}, 1, 1)
	if len(got) != 1 {
		t.Fatalf("all-own target = %v, want exactly one choice\n", got)
	}
	if objAt(d, got[0]) != 102 {
		t.Errorf("all-own target = option %d (obj %d), want the 4/4 (obj 102)", got[0], objAt(d, got[0]))
	}
}

// TestTargetPreferCreatureOverFace is R2: when the opponent has a
// battlefield creature worth an option, the bot targets it over the
// opponent's face — the removal spell must stop the board, not throw 3
// damage at the face. With no creature on board the face is the only
// sensible pick, so the fallback to a player option is covered too.
func TestTargetPreferCreatureOverFace(t *testing.T) {
	// The 6/6 is offered at index 1, the face at index 0; a positional
	// (Options[0]) pick would hit the face and fail this. The opponent is
	// at a healthy 20 life, so the face is merely legal and loses to the
	// board (the do-not-suicide half of the dp3 face rule).
	b := boardOf(def(1, 6, 6))
	b.Life.Set(1, 20)
	got, d := targetDecision(b, []tgt{face(), opp(201)}, 1, 1)
	if len(got) != 1 || objAt(d, got[0]) != 201 {
		t.Errorf("board creature preferred over face = %v (obj %d), want the 6/6 (obj 201)", got, objAt(d, got[0]))
	}
	// No creature at all: the face.
	b2 := boardOf()
	got2, d2 := targetDecision(b2, []tgt{face()}, 1, 1)
	if len(got2) != 1 || d2.Options[got2[0]].Kind != "player" {
		t.Errorf("no-creature fallback = %v, want the opponent player", got2)
	}
}

// TestTargetRankByThreatNotPower is R3: opposing creatures rank by threat()
// — remTough, Tapped and evasion keywords on top of power — not by raw
// power. A healthy 3/3 outranks a tapped 4/1 one hit from death even though
// 4 > 3, and a 2/2 flier outranks an unblockable 3/3 despite the lower
// power+toughness. The first option in each decision is the lesser target,
// so both the Options[0] mutation and a raw-Power metric would pick the
// wrong one and fail.
func TestTargetRankByThreatNotPower(t *testing.T) {
	// 4/1 tapped with 1 damage (threat 12) at index 0, healthy 3/3 (threat
	// 21) at index 1: raw power says the 4/1 (4 > 3), threat says the 3/3.
	b := boardOf(
		fact{state.ObjID(202), Creature{Power: 4, Toughness: 1, Damage: 1, Tapped: true, Controller: 1}},
		def(1, 3, 3),
	)
	got, d := targetDecision(b, []tgt{opp(202), opp(201)}, 1, 1)
	if len(got) != 1 || objAt(d, got[0]) != 201 {
		t.Errorf("threat-ranked target = obj %d, want the healthy 3/3 (obj 201), not the dying 4/1", objAt(d, got[0]))
	}
	// A 2/2 flier (threat 32) beats a ground 3/3 (threat 21) despite pt
	// 4 < 6 — evasion is priced, not guessed at.
	b = boardOf(def(1, 3, 3), def(2, 2, 2, "Flying"))
	got, d = targetDecision(b, []tgt{opp(201), opp(202)}, 1, 1)
	if len(got) != 1 || objAt(d, got[0]) != 202 {
		t.Errorf("evasion-ranked target = obj %d, want the 2/2 flier (obj 202)", objAt(d, got[0]))
	}
}

// TestTargetHonoursMin is R5's first half: a decision demanding two targets
// gets exactly two, ranked — never one handed to clamp to top up. Two of
// three creatures' choices are what make a multi-target removal work.
func TestTargetHonoursMin(t *testing.T) {
	b := boardOf(def(1, 2, 2), def(2, 5, 5), def(3, 1, 1))
	got, d := targetDecision(b, []tgt{opp(201), opp(202), opp(203)}, 2, 2)
	if len(got) != 2 {
		t.Fatalf("2-required target = %v, want exactly two choices", got)
	}
	if objAt(d, got[0]) != 202 || objAt(d, got[1]) != 201 {
		t.Errorf("two ranked targets = objs {%d, %d}, want {202, 201} (the 5/5 then the 2/2)", objAt(d, got[0]), objAt(d, got[1]))
	}
}

// TestTargetHonoursMax is R5's second half: an "up to N" decision never
// exceeds Max. A may-target (Min 0, Max 1) opts into its single best
// offer, and a Min 3 decision picks all three. clamp is never the thing
// that corrects the count.
func TestTargetHonoursMax(t *testing.T) {
	b := boardOf(def(1, 2, 2), def(2, 5, 5), def(3, 1, 1))
	// Min 0, Max 1: a may-target fires its single best offer.
	got, _ := targetDecision(b, []tgt{opp(201), opp(202), opp(203)}, 0, 1)
	if len(got) > 1 {
		t.Errorf("up-to-one target = %v, want at most one choice", got)
	}
	// Min 0, Max 2: an "up to two" removal fires its full legal width.
	got2, d2 := targetDecision(b, []tgt{opp(201), opp(202), opp(203)}, 0, 2)
	if len(got2) != 2 {
		t.Fatalf("up-to-two target = %v, want both offers taken", got2)
	}
	if objAt(d2, got2[0]) != 202 || objAt(d2, got2[1]) != 201 {
		t.Errorf("up-to-two targets = objs {%d, %d}, want {202, 201}", objAt(d2, got2[0]), objAt(d2, got2[1]))
	}
	// Min 3, Max 3: forced to take all three.
	got3, d3 := targetDecision(b, []tgt{opp(201), opp(202), opp(203)}, 3, 3)
	if len(got3) != 3 {
		t.Fatalf("three-required target = %v, want exactly three choices", got3)
	}
	checked := map[state.ObjID]bool{}
	for _, i := range got3 {
		checked[d3.Options[i].Obj] = true
	}
	for _, want := range []state.ObjID{201, 202, 203} {
		if !checked[want] {
			t.Errorf("3-required target = %v, missing obj %d", got3, want)
		}
	}
}

// TestTargetUnreadableOptionIsLowRank is R4: an option whose Obj is in no
// zone this Board can read is a low rank, not a crash. A decision that
// offers a battlefield creature beside an invisible object picks the
// creature, and a decision whose every option is invisible still answers
// (validating) rather than panicking.
func TestTargetUnreadableOptionIsLowRank(t *testing.T) {
	b := boardOf(def(1, 3, 3))
	// A battlefield-visible 3/3 (obj 201) beside an object with no census
	// facts (obj 999 — a graveyard card, a battlefield artifact, an exile
	// object ...).
	got, d := targetDecision(b, []tgt{opp(999), opp(201)}, 1, 1)
	if err := d.Validate(decision.Intent{Seq: 1, Player: 0, Choices: got}); err != nil {
		t.Fatalf("intent %v failed Validate: %v", got, err)
	}
	if len(got) != 1 || objAt(d, got[0]) != 201 {
		t.Errorf("readable creature preferred over unreadable = obj %d, want 201", objAt(d, got[0]))
	}
	// Every option unreadable: still a valid, total answer.
	b2 := boardOf()
	got2, d2 := targetDecision(b2, []tgt{opp(999), opp(998)}, 1, 1)
	if err := d2.Validate(decision.Intent{Seq: 1, Player: 0, Choices: got2}); err != nil {
		t.Fatalf("all-unreadable target %v failed Validate: %v", got2, err)
	}
}

// TestChooseTargetsHonoursMinAndMaxDirect calls the targeting branch
// directly, outside Decide, so clamp cannot mask the count (R5): clamp's
// top-up fills an under-filled choice with the lowest-index unused option —
// position order, the very thing this policy replaces — which is why a
// Min-2 decision must produce both targets itself. The decide-level
// TestTargetHonoursMin keeps the integrated guarantee; this one is the
// mutation target for "ignore Min".
func TestChooseTargetsHonoursMinAndMaxDirect(t *testing.T) {
	b := boardOf(def(1, 2, 2), def(2, 5, 5), def(3, 1, 1))
	opts := []decision.Option{
		{Index: 0, Kind: "permanent", Obj: 201, Player: 1},
		{Index: 1, Kind: "permanent", Obj: 202, Player: 1},
		{Index: 2, Kind: "permanent", Obj: 203, Player: 1},
	}
	if ch := b.chooseTargets(&decision.Decision{Player: 0, Kind: decision.KTarget, Min: 2, Max: 2, Options: opts}); len(ch) != 2 {
		t.Fatalf("chooseTargets(Min 2) = %v, want exactly two targets without clamp", ch)
	}
	if ch := b.chooseTargets(&decision.Decision{Player: 0, Kind: decision.KTarget, Min: 0, Max: 1, Options: opts}); len(ch) > 1 {
		t.Fatalf("chooseTargets(Min 0 Max 1) = %v, want at most one without clamp", ch)
	}
	if ch := b.chooseTargets(&decision.Decision{Player: 0, Kind: decision.KTarget, Min: 3, Max: 3, Options: opts}); len(ch) != 3 {
		t.Fatalf("chooseTargets(Min 3) = %v, want three targets without clamp", ch)
	}
}

// TestTargetNeverTakesTwoOfOneGroup pins the pick loop's group discipline
// (pfpe1): Decision.Validate's mutual-exclusion rule makes two options
// sharing one non-empty Group mutually exclusive -- the TargetsForEachPlayer$
// shape (one target per controller) and the cross-mode Charm ask both attach
// it -- so a multi-target answer that took two same-group options would be
// rejected and Clamp could never repair it (a wedging answer). The ranked
// pick skips a represented group exactly the way Clamp's own top-up does.
// The Havoc Eater shape: Max 2, one opponent with TWO strong creatures and
// the second opponent with one weaker -- the two same-group bears must not
// both be taken even though they out-rank the other group's option.
func TestTargetNeverTakesTwoOfOneGroup(t *testing.T) {
	b := boardOf(def(1, 5, 5), def(2, 5, 5), def(3, 1, 1))
	opts := []decision.Option{
		{Index: 0, Kind: "permanent", Obj: 201, Player: 1, Group: "target-controller-1"},
		{Index: 1, Kind: "permanent", Obj: 202, Player: 1, Group: "target-controller-1"},
		{Index: 2, Kind: "permanent", Obj: 203, Player: 2, Group: "target-controller-2"},
	}
	ch := b.chooseTargets(&decision.Decision{Player: 0, Kind: decision.KTarget, Min: 0, Max: 2, Options: opts})
	if len(ch) != 2 {
		t.Fatalf("chooseTargets(Min 0 Max 2) = %v, want two targets (the full width)", ch)
	}
	taken := map[string]bool{}
	for _, c := range ch {
		g := opts[c].Group
		if taken[g] {
			t.Fatalf("answer %v took two options of group %q", ch, g)
		}
		taken[g] = true
	}
	if !taken["target-controller-2"] {
		t.Fatalf("answer %v left the second controller untargeted", ch)
	}
}

// TestTargetSizeIncludesOwnPlayer is a determinism guard on the corner
// where a "player" option names the deciding seat itself (never produced
// by askTarget, but a valid wire shape): it is treated as an own option,
// so it is only ever chosen when nothing opposing is offered, and the
// answer validates.
// TestTargetLowLifeDoesNotClaimBurnReach is the inverted half of the
// removed goal-scoring: the policy cannot read the effect that produced the
// KTarget decision, so it cannot prove a targetable-at-player effect deals
// any damage at all. A player at 2 life is not therefore a win target --
// when a creature is also a legal target the bot answers the board, exactly
// as it does for a healthy player. The face is a neutral target, chosen only
// when nothing else is offered (see faceScore).
func TestTargetLowLifeDoesNotClaimBurnReach(t *testing.T) {
	b := boardOf(def(1, 5, 5))
	b.Life.Set(1, 2) // a burn "would" reach -- but the policy cannot prove the effect deals damage
	got, d := targetDecision(b, []tgt{opp(201), face()}, 1, 1)
	if len(got) != 1 || objAt(d, got[0]) != 201 {
		t.Fatalf("target = %v (obj %d), want the 5/5 (obj 201) even at 2 life: low life is not provable burn reach", got, objAt(d, got[0]))
	}
}

// TestTargetCreatureOverHealthyFace is the "do not suicide into the board"
// guard: away from any claimed lethal, the face is merely legal, so the bot
// answers the best battlefield creature rather than throwing the effect at
// a face that can take it. This holds for a healthy player and, since the
// policy cannot prove the effect reaches the player, for a low-life player
// too (TestTargetLowLifeDoesNotClaimBurnReach).
func TestTargetCreatureOverHealthyFace(t *testing.T) {
	b := boardOf(def(1, 6, 6))
	b.Life.Set(1, 18) // healthy
	got, d := targetDecision(b, []tgt{face(), opp(201)}, 1, 1)
	if len(got) != 1 || objAt(d, got[0]) != 201 {
		t.Fatalf("target = obj %d, want the 6/6 (obj 201) over the healthy face", objAt(d, got[0]))
	}
}

// TestTargetFaceIsNeutralAcrossLife is the graded half of the withdrawn
// goal-scoring rule: the face's value was to climb as the opponent neared
// dead, so a player at 15 life kept the board over the face while a player
// at 2 flipped it. With the effect unread the face is neutral at every life
// total, so the board stays preferred at 15, at 2 and everywhere between --
// the same conservative answer, never a claimed kill the policy cannot
// prove.
func TestTargetFaceIsNeutralAcrossLife(t *testing.T) {
	for _, life := range []int32{20, 15, 2, 0} {
		b := boardOf(def(1, 4, 4))
		b.Life.Set(1, life)
		got, d := targetDecision(b, []tgt{opp(201), face()}, 1, 1)
		if len(got) != 1 || objAt(d, got[0]) != 201 {
			t.Errorf("at %d life target = %v (obj %d), want the 4/4 (obj 201): the face is neutral, not burn reach", life, got, objAt(d, got[0]))
		}
	}
}

// TestTargetCommanderClockIsNotSpellReach pins the CR 903.10 property the
// old rule got wrong: the 21 commander-damage clock counts only the COMBAT
// damage one commander has dealt, so ordinary non-combat spell damage never
// advances it. An opponent at 20 life with 19 commander damage is therefore
// not a burn target -- pointing a spell at their face touches neither the
// life track nor the combat-damage clock with a number the policy can read
// -- so the board is still preferred over the face.
func TestTargetCommanderClockIsNotSpellReach(t *testing.T) {
	b := boardOf(def(1, 3, 3))
	b.Life.Set(1, 20)
	b.Commanders = TableOf(map[state.ObjID]Commander{
		300: {Damage: map[state.PlayerID]int32{1: 19}},
	})
	got, d := targetDecision(b, []tgt{opp(201), face()}, 1, 1)
	if len(got) != 1 || objAt(d, got[0]) != 201 {
		t.Fatalf("target = %v (obj %d), want the 3/3 (obj 201): commander-damage proximity is not spell burn reach", got, objAt(d, got[0]))
	}
}

// TestTargetFaceStillChosenWhenNoBoard is the neutral policy's positive
// half: when no opposing creature is a legal target, the face remains a
// valid, preferred pick -- a burn at a face, a drain, or a life-gain that
// only targets players all offer only the player. Removing the face entirely
// would leave such a decision to clamp, so this pins that the neutral face
// is still chosen.
func TestTargetFaceStillChosenWhenNoBoard(t *testing.T) {
	b := boardOf()
	got, d := targetDecision(b, []tgt{face()}, 1, 1)
	if len(got) != 1 || d.Options[got[0]].Kind != "player" {
		t.Fatalf("target = %v, want the opponent player: the face is still the pick when it is the only target", got)
	}
}

// TestTargetShapeIncludesOwnPlayer is a determinism guard on the corner
// where a "player" option names the deciding seat itself (never produced
// by askTarget, but a valid wire shape): it is treated as an own option,
// so it is only ever chosen when nothing opposing is offered, and the
// answer validates.
func TestTargetShapeIncludesOwnPlayer(t *testing.T) {
	b := boardOf(def(1, 3, 3))
	got, d := targetDecision(b, []tgt{{kind: "player", pl: 0}, face(), opp(201)}, 1, 1)
	if err := d.Validate(decision.Intent{Seq: 1, Player: 0, Choices: got}); err != nil {
		t.Fatalf("intent %v failed Validate: %v", got, err)
	}
	if len(got) != 1 || d.Options[got[0]].Player != 1 {
		t.Errorf("target with own-player present = option %d (%+v), want an opponent target", got[0], d.Options[got[0]])
	}
}
