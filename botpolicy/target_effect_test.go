package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// effectTargetDecision is targetDecision's twin for the effect-aware path:
// it builds a KTarget decision for seat 0 whose TargetEffect names a
// DealDamage primitive with the given literal damage amount (nil = the
// amount is unknown/absent), and returns the option indices Decide chose
// together with the decision, so a test can inspect the wire shape too.
func effectTargetDecision(b Board, dmg *int, options []tgt, min, max int) ([]int, *decision.Decision) {
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KTarget, Min: min, Max: max,
		TargetEffect: &decision.TargetEffect{API: "DealDamage",
			Damage: &decision.DamageEffect{Amount: dmg}}}
	for _, o := range options {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: o.kind, Obj: o.obj, Player: o.pl})
	}
	return Decide(b, &d, rng(1)).Choices, &d
}

// intp is a tiny helper to take the address of a literal int for the
// DamageEffect.Amount field (a *int, null when unknown).
func intp(v int) *int { return &v }

// TestTargetEffectUnknownDamageIsNotLethal is the most important test in
// the ft1 round: when the damage fact is ABSENT (the amount is null, as it
// is for X/dynamic/SVar/invalid amounts), the bot must not treat the
// opponent as lethal -- it must not aim at their face just because they are
// about to die to some damage the engine cannot read. The opponent here is
// at 2 life beside a creature; the unknown effect must leave the face at
// the neutral board-over-face rank and answer the creature, exactly as the
// effect-blind policy always did. This is the "unknown is no, never assume
// lethal" guard.
func TestTargetEffectUnknownDamageIsNotLethal(t *testing.T) {
	b := boardOf(def(1, 6, 6))
	b.Life.Set(0, 20)
	b.Life.Set(1, 2) // about to die -- but the damage amount is unknown (nil)
	got, d := effectTargetDecision(b, nil, []tgt{face(), opp(201)}, 1, 1)
	if len(got) != 1 || objAt(d, got[0]) != 201 {
		t.Fatalf("unknown damage = obj %d, want the creature (obj 201): unknown must not be treated as lethal face", objAt(d, got[0]))
	}
}

// TestTargetEffectUnknownApiIsNotLethal is the other unknown: an API that
// is not a recognised direct-damage primitive (damage is absent on the
// wire) must not turn the face into a lethal target either. The bot builds
// on only the facts tf1 exposes -- a DealDamage/DamageAll primitive with a
// literal amount -- and treats every other shape as unreadable.
func TestTargetEffectUnknownApiIsNotLethal(t *testing.T) {
	b := boardOf(def(1, 6, 6))
	b.Life.Set(0, 20)
	b.Life.Set(1, 2)
	// A Draw effect: no damage, whatever a script says.
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1,
		TargetEffect: &decision.TargetEffect{API: "Draw"},
		Options:      []decision.Option{{Index: 0, Kind: "player", Obj: 0, Player: 1}, {Index: 1, Kind: "permanent", Obj: 201, Player: 1}}}
	got := Decide(b, &d, rng(1)).Choices
	if len(got) != 1 || objAt(&d, got[0]) != 201 {
		t.Fatalf("Draw effect = obj %d, want the creature (obj 201): a non-damage API is not burn reach", objAt(&d, got[0]))
	}
}

// TestTargetEffectKnownNonLethalDamageKeepsBoardOverFace is the positive
// half of the unknown-not-lethal rule once the amount IS known: a bolt that
// cannot reach the opponent's 20 life must not be thrown at their face
// either. With a readable non-lethal effect, the answer is still the board
// -- the known damage is the one thing the policy can prove, and it proves
// it is not a kill.
func TestTargetEffectKnownNonLethalDamageKeepsBoardOverFace(t *testing.T) {
	b := boardOf(def(1, 6, 6))
	b.Life.Set(0, 20)
	b.Life.Set(1, 20)
	got, d := effectTargetDecision(b, intp(3), []tgt{face(), opp(201)}, 1, 1)
	if len(got) != 1 || objAt(d, got[0]) != 201 {
		t.Fatalf("known non-lethal damage = obj %d, want the creature (obj 201) over the healthy face", objAt(d, got[0]))
	}
}

// TestTargetEffectThreatRemoval is tier 1: an opponent creature that may
// kill the deciding seat this round (its power reaches the seat's life) and
// that the damage can actually kill is the removal target of first choice,
// before the face, before a value kill -- even when spare mana would make a
// value kill legal. The seat is at 4 life; the 4/3 both threatens it and
// dies to a 3-damage bolt, while the 2/2 is only a disinterested value
// kill. The 4/3 wins on the threat tier.
func TestTargetEffectThreatRemoval(t *testing.T) {
	b := boardOf(def(1, 4, 3), def(2, 2, 2))
	b.Life.Set(0, 4) // the 4/3 would take the seat to 0 this round
	b.Life.Set(1, 20)
	b.Pool[state.MC] = 1 // spare mana, so the 2/2 would be a legal value kill
	got, d := effectTargetDecision(b, intp(3), []tgt{face(), opp(201), opp(202)}, 1, 1)
	if len(got) != 1 || objAt(d, got[0]) != 201 {
		t.Fatalf("threat removal = obj %d (options %v), want the threatening 4/3 (obj 201)", objAt(d, got[0]), choicesObj(got, d))
	}
}

// TestTargetEffectLethalFace is tier 2: with no must-answer threat on the
// board, the opponent's face becomes the target when the damage is
// potentially lethal to them. The seat is healthy (no threat) and the
// opponent is at 2 life; a 3-damage effect reaches them, so the face
// outranks the creature it could alternatively kill.
func TestTargetEffectLethalFace(t *testing.T) {
	b := boardOf(def(1, 2, 2))
	b.Life.Set(0, 20) // healthy, so the 2/2 is not a threat to the seat
	b.Life.Set(1, 2)  // lethal to a 3-damage bolt
	b.Pool[state.MC] = 0
	got, d := effectTargetDecision(b, intp(3), []tgt{face(), opp(201)}, 1, 1)
	if len(got) != 1 || d.Options[got[0]].Kind != "player" {
		t.Fatalf("lethal face = option %d (%+v), want the opponent player target", got[0], d.Options[got[0]])
	}
}

// TestTargetEffectThreatBeforeLethalFace pins the brief's stated order of
// the tiers: a threat that may kill the seat this round (tier 1) is the
// first pick even when the same spell could also deal lethal to the
// opponent's face (tier 2). The seat is at 4 life and the opponent at 2, so
// both apply; the 4/3 threat wins.
func TestTargetEffectThreatBeforeLethalFace(t *testing.T) {
	b := boardOf(def(1, 4, 3))
	b.Life.Set(0, 4) // the 4/3 is a must-answer threat
	b.Life.Set(1, 2) // and the effect is also lethal to the opponent's face
	got, d := effectTargetDecision(b, intp(3), []tgt{face(), opp(201)}, 1, 1)
	if len(got) != 1 || objAt(d, got[0]) != 201 {
		t.Fatalf("threat-vs-lethal-face = obj %d, want the threat (obj 201): tier 1 precedes tier 2", objAt(d, got[0]))
	}
}

// TestTargetEffectValueKillOnlyWithSpareMana is tier 3 and its gate: with
// spare (unspent floating) mana the removal is spent on the most powerful
// opponent creature it can actually kill -- here the 2/2, not the 5/5 the
// bolt cannot kill; but when mana is tight the value kill does NOT fire and
// the policy answers the ordinary board-first threat (the 5/5), because a
// removal is not spent on value when the mana could be doing something
// else. The two halves of the test differ only in the pool, so the gate is
// what flips the answer, not the board.
func TestTargetEffectValueKillOnlyWithSpareMana(t *testing.T) {
	// 5/5 is not killable by 3 damage (remTough 5 > 3); the 2/2 is (2 <= 3).
	b := boardOf(def(1, 5, 5), def(2, 2, 2))
	b.Life.Set(0, 20)
	b.Life.Set(1, 20) // not lethal to a 3-damage bolt; no threat to a healthy seat

	// Spare floating mana: the value kill fires -- the strongest killable
	// creature (the 2/2) beats the unkillable 5/5 the old policy would have
	// wasted the bolt on.
	b.Pool[state.MC] = 1
	got, d := effectTargetDecision(b, intp(3), []tgt{face(), opp(201), opp(202)}, 1, 1)
	if len(got) != 1 || objAt(d, got[0]) != 202 {
		t.Fatalf("spare-mana value kill = obj %d (options %v), want the killable 2/2 (obj 202)", objAt(d, got[0]), choicesObj(got, d))
	}

	// Tight mana (empty pool): tier 3 does NOT fire, so the unkillable but
	// more threatening 5/5 is answered instead -- the removal is not spent
	// on value when the mana is committed.
	b.Pool[state.MC] = 0
	got, d = effectTargetDecision(b, intp(3), []tgt{face(), opp(201), opp(202)}, 1, 1)
	if len(got) != 1 || objAt(d, got[0]) != 201 {
		t.Fatalf("tight-mana target = obj %d (options %v), want the untargetable-value 5/5 (obj 201): tier 3 must not fire without spare mana", objAt(d, got[0]), choicesObj(got, d))
	}
}

// choicesObj renders the chosen option indices as their object ids for a
// test failure message.
func choicesObj(ch []int, d *decision.Decision) []state.ObjID {
	out := make([]state.ObjID, 0, len(ch))
	for _, i := range ch {
		if i >= 0 && i < len(d.Options) {
			out = append(out, d.Options[i].Obj)
		}
	}
	return out
}
