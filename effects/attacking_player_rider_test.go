package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestAttackingPlayerRider is the effects leaf for Forge's
// Card.attacking <PlayerSpec> rider: the candidate is attacking the player the
// player spec names, You = the evaluating controller, resolved through the
// player grammar's ONE home. The semantics this pins is the trap the plus
// spelling hides: Oviya, Automech Artisan's "each creature that's attacking
// one of your opponents has trample" reads the DEFENDER (state.Object.Attacking,
// CR 508.1) and INCLUDES your own attacking creature, while
// `attacking+Opponent` ("attacking" AND "opponent-controlled") does not. It
// also pins the entity-not-seat rule: a creature attacking an opponent's
// PLANESWALKER names that opponent's seat in Attacking but its defender is a
// permanent, not a player, so it must not match. Asserting the preconditions
// (who attacks whom, which seat each creature is controlled by) keeps the test
// from passing vacuously if the rider matched nothing.
func TestAttackingPlayerRider(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	g := state.NewGame([]string{"you", "them", "third"})

	own := g.Obj(corpusObject(t, reg, g, "Grizzly Bears").ID)
	own.IsAttacking, own.Attacking = true, 1
	// An opponent's creature attacking YOU: the defender is seat 0 (you),
	// so `attacking Opponent` must NOT match it.
	theirs := g.Obj(corpusObject(t, reg, g, "Grizzly Bears").ID)
	theirs.Controller, theirs.IsAttacking, theirs.Attacking = 1, true, 0
	// An opponent's creature attacking the THIRD seat (an opponent of you):
	// the rider must match it (CR 800.4a free-for-all: any opponent's
	// defender qualifies).
	theirsFar := g.Obj(corpusObject(t, reg, g, "Grizzly Bears").ID)
	theirsFar.Controller, theirsFar.IsAttacking, theirsFar.Attacking = 1, true, 2
	idle := g.Obj(corpusObject(t, reg, g, "Grizzly Bears").ID)
	if !own.IsAttacking || own.Controller != 0 || own.Attacking != 1 ||
		!theirs.IsAttacking || theirs.Controller != 1 || theirs.Attacking != 0 ||
		!theirsFar.IsAttacking || theirsFar.Controller != 1 || theirsFar.Attacking != 2 ||
		idle.IsAttacking {
		t.Fatalf("precondition failed: attacking setup is wrong (own=%+v theirs=%+v theirsFar=%+v idle.IsAttacking=%v)",
			own, theirs, theirsFar, idle.IsAttacking)
	}

	sc := SpecContext{You: 0}
	if !MatchesObjectCtx(g, "Creature.attacking Opponent", own, sc) {
		t.Error("Creature.attacking Opponent must match your OWN creature attacking an opponent (Oviya's wording)")
	}
	if MatchesObjectCtx(g, "Creature.attacking Opponent", theirs, sc) {
		t.Error("Creature.attacking Opponent must not match a creature attacking YOU")
	}
	if !MatchesObjectCtx(g, "Creature.attacking Opponent", theirsFar, sc) {
		t.Error("Creature.attacking Opponent must match an opponent's creature attacking another opponent of yours")
	}
	if MatchesObjectCtx(g, "Creature.attacking Opponent", idle, sc) {
		t.Error("Creature.attacking Opponent must not match a non-attacking creature")
	}
	// Player: any defender qualifies (every attack names a seat).
	if !MatchesObjectCtx(g, "Creature.attacking Player", own, sc) ||
		!MatchesObjectCtx(g, "Creature.attacking Player", theirs, sc) {
		t.Error("Creature.attacking Player must match every attacking creature")
	}
	if MatchesObjectCtx(g, "Creature.attacking Player", idle, sc) {
		t.Error("Creature.attacking Player must not match a non-attacking creature")
	}
	// A creature attacking an opponent's PLANESWALKER: AttackingBattle carries
	// the planeswalker and Attacking names its controller's seat (CR 508.1),
	// so a seat-only rider would match `attacking Opponent` -- but Forge
	// compares the defender ENTITY, and a planeswalker is not a player, so it
	// must NOT match either spelling.
	jace := corpusObject(t, reg, g, "Jace Beleren")
	jace.Controller = 1
	pwAtk := g.Obj(corpusObject(t, reg, g, "Grizzly Bears").ID)
	pwAtk.Controller, pwAtk.IsAttacking, pwAtk.Attacking, pwAtk.AttackingBattle = 1, true, 1, jace.ID
	if pwAtk.AttackingBattle == 0 || jace.ID == 0 {
		t.Fatal("precondition failed: planeswalker attack carries no AttackingBattle")
	}
	if MatchesObjectCtx(g, "Creature.attacking Opponent", pwAtk, sc) {
		t.Error("Creature.attacking Opponent must not match a creature attacking a planeswalker (the defender is not a player)")
	}
	if MatchesObjectCtx(g, "Creature.attacking Player", pwAtk, sc) {
		t.Error("Creature.attacking Player must not match a creature attacking a planeswalker")
	}
	// Negated: a non-attacking creature is not attacking an opponent.
	if got, ok := matchPredicate(g, "!attacking Opponent", idle, sc); !ok || !got {
		t.Errorf("!attacking Opponent on a non-attacking creature = %v,%v, want true,true", got, ok)
	}
	// The plus spelling is a DIFFERENT grammar (attacking AND
	// opponent-controlled): it must not widen the rider onto your own
	// attacking creature, and the rider must not drag the plus spelling
	// with it.
	if MatchesObjectCtx(g, "Creature.attacking+Opponent", own, sc) {
		t.Error("Creature.attacking+Opponent must not match your own attacking creature (it is the opponent-CONTROLLED grammar)")
	}
	if !MatchesObjectCtx(g, "Creature.attacking+Opponent", theirsFar, sc) {
		t.Error("Creature.attacking+Opponent must still match an opponent-controlled attacking creature")
	}

	// The census and the matcher share the one recogniser.
	for _, spec := range []string{
		"Creature.attacking Opponent", "Creature.attacking Player",
		"Creature.!attacking Opponent", "Card.attacking Opponent",
	} {
		if got := UnknownPredicates(spec); len(got) != 0 {
			t.Errorf("UnknownPredicates(%q) = %v, want empty", spec, got)
		}
	}
	// The out-of-scope shapes stay loud: an argument that is not a
	// player-spec base must keep reporting as unknown, never go silent.
	for _, spec := range []string{
		"Creature.attacking Valid Planeswalker.OppCtrl", "Creature.attacking ChosenPlayer",
		"Creature.attacking RememberedPlayer", "Creature.attacking EnchantedPlayer",
		"Creature.attacking TriggeredAttackedTarget",
	} {
		if got := UnknownPredicates(spec); len(got) == 0 {
			t.Errorf("UnknownPredicates(%q) = empty, want the argument still reported", spec)
		}
	}
}
