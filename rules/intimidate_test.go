package rules

import (
	"strings"
	"testing"
)

// CR 702.13a: an Intimidate attacker can be blocked only by artifact
// creatures and/or creatures that share a colour with it. Pinned on the real
// corpus carrier Sepulchral Primordial (a mono-black Avatar), with three
// prospective blockers: an artifact creature (allowed), a black creature --
// sharing the attacker's only colour (allowed) -- and a white creature
// (neither artifact nor shared colour: denied).
func TestIntimidateBlocksOnlyArtifactsAndSharedColors(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	// Seat 1 attacks seat 0, so seat 0's creatures are the prospective
	// blockers (the same shape TestHorsemanshipCanBlockOnlyHorsemanshipAttackers
	// practises).
	primordial := onBoardCard(t, e, 1, corpusKeywordCard(t, "Sepulchral Primordial"))
	e.G.Obj(primordial).IsAttacking, e.G.Obj(primordial).Attacking = true, 0
	if !e.HasKeyword(primordial, "Intimidate") {
		t.Fatal("Sepulchral Primordial does not carry Intimidate")
	}
	if !e.G.Obj(primordial).IsAttacking || e.G.Obj(primordial).Attacking != 0 {
		t.Fatal("attacker is not set attacking seat 0")
	}
	artifact := onBoard(t, e, 0, "Name:Golem\nManaCost:3\nTypes:Artifact Creature\nPT:1/1\nOracle:x\n")
	black := onBoard(t, e, 0, "Name:Priest\nManaCost:B\nTypes:Creature\nPT:1/1\nOracle:x\n")
	white := onBoard(t, e, 0, "Name:Soldier\nManaCost:W\nTypes:Creature\nPT:1/1\nOracle:x\n")
	// Precondition: the two colour reads really differ on the derived
	// (layer-5) read the rule consumes, so the shared-colour arm below is
	// exercised rather than vacuous.
	if !strings.ContainsRune(e.objColors(e.G.Obj(black)), 'B') || strings.ContainsRune(e.objColors(e.G.Obj(white)), 'B') {
		t.Fatalf("blocker colours not as expected: black=%q white=%q",
			e.objColors(e.G.Obj(black)), e.objColors(e.G.Obj(white)))
	}
	if !e.canBlock(artifact, primordial) {
		t.Fatal("artifact creature could not block an Intimidate attacker")
	}
	if !e.canBlock(black, primordial) {
		t.Fatal("black creature (shares the attacker's colour) could not block an Intimidate attacker")
	}
	if e.canBlock(white, primordial) {
		t.Fatal("white creature blocked an Intimidate mono-black attacker")
	}
}
