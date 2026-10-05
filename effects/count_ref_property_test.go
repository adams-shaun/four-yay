package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// The <Ref>$<Property> count family the DamageSource$ work rides on: before
// the evaluator these bodies evaluated to zero (Kiku's Shadow dealt no
// damage at all), so every branch here pins a formerly-dead head answering
// its object's real property.
func TestRefPropertyCounts(t *testing.T) {
	h := newHost(t, 2)
	power := mkCard(t, "Name:Ox\nTypes:Creature Ox\nPT:3/4\nOracle:x\n")
	cheap := mkCard(t, "Name:Dork\nTypes:Creature Dork\nPT:1/1\nManaCost:2\nOracle:x\n")
	ao := h.g.AddObject(power, 0)
	bo := h.g.AddObject(cheap, 0)
	a, b := ao.ID, bo.ID
	h.g.Obj(a).AddCounter("P1P1", 2)
	c := &Ctx{Source: a, Controller: 0, Targets: []state.Target{{Obj: a}, {Obj: b}},
		Remembered: []state.Target{{Obj: b}}}
	for _, tc := range []struct {
		expr string
		want int32
	}{
		// Targeted$CardPower sums over the targets (3+2 counters, 1): Vein
		// Drinker's second half; ParentTargeted$CardPower reads the same
		// list; TriggeredCard$CardPower reads the Remembered objects.
		{"Targeted$CardPower", 3 + 2 + 1},
		{"ParentTargeted$CardPower", 3 + 2 + 1},
		{"TriggeredCard$CardPower", 1},
		{"Targeted$CardToughness", 4 + 2 + 1},
		{"Targeted$CardManaCost", 2},
		{"TriggeredCard$CardManaCost", 2},
		// Remembered$Amount stays the remembered-entry count (the pre-existing
		// head, unbroken); Remembered$CardPower is the new family on the same
		// prefix.
		{"Remembered$Amount", 1},
		{"Remembered$CardPower", 1},
		// The Valid head counts referenced objects matching a card spec;
		// unknown predicates inside the spec fail closed (never match).
		{"Remembered$Valid Creature", 1},
		{"Remembered$Valid NonexistentType", 0},
		// AllTargeted$ binds this engine's Ctx.Targets (the root SA's chosen
		// targets) -- the faithful-as-available reading of Forge's whole-chain
		// union, whose sub-ability targets this engine defers to resolution
		// (AGENTS.md's Known approximations). Rows share Targeted's fixture.
		{"AllTargeted$CardPower", 3 + 2 + 1},
		{"AllTargeted$CardManaCost", 2},
		{"AllTargeted$Valid Creature.powerLE3", 1}, // Dork (1) matches, Ox (5) does not
		{"AllTargeted$CardPower/Twice", (3 + 2 + 1) * 2},
		// An unknown ref or property stays zero (the conservative no-op), and
		// a /Op suffix applies through the ordinary arithmetic.
		{"Remembered$ChromaSource", 0},
		{"TriggeredTarget$LifeTotal", 0},
		{"UnknownRef$CardPower", 0},
		{"Targeted$CardPower/Twice", (3 + 2 + 1) * 2},
	} {
		if got := EvalCount(h, c, tc.expr); got != tc.want {
			t.Errorf("%s = %d, want %d", tc.expr, got, tc.want)
		}
	}
}

// mkPlayerZoneCard parses one inline card and places it in a player's zone
// (the filter_test.go board helper's mkIn, local to this file).
func mkPlayerZoneCard(t *testing.T, g *state.Game, owner state.PlayerID, zone state.Zone, src string) {
	t.Helper()
	c, d := cards.ParseBytes("t.txt", []byte(src))
	if len(d) != 0 {
		t.Fatalf("diags: %v", d)
	}
	c.Link()
	for _, f := range c.Faces {
		f.ApplyIntrinsics()
	}
	o := g.AddObject(c, owner)
	o.Zone = zone
	g.SetZone(zone, owner, append(g.Zone(zone, owner), o.ID))
}

// TestTargetedPlayerRefProperty pins the player-valued half of the
// <Ref>$<Property> count family (task tgtplayer1): before the evaluator,
// TargetedPlayer$/ThisTargetedPlayer$ bodies fell through every head and
// answered zero, so Knollspine Dragon drew nothing and The Mouth of Sauron
// always amassed 0. The referenced player is the filter's You for the Valid
// family -- The Mouth of Sauron's `Instant.YouOwn,Sorcery.YouOwn` must count
// the TARGET's own graveyard cards, not the resolving controller's.
func TestTargetedPlayerRefProperty(t *testing.T) {
	g, ids := board(t)
	// The referenced player is seat 1: their life, hand, library, graveyard
	// and battlefield are each a distinct non-default size so a wrong
	// perspective (You = the resolving controller, seat 0) cannot pass.
	g.Players[0].Life = 11
	g.Players[1].Life = 17
	g.Players[1].Counters = []state.Counter{{Kind: "POISON", N: 3}}
	for i := 0; i < 2; i++ {
		mkPlayerZoneCard(t, g, 1, state.ZHand, "Name:Doodad\nManaCost:1\nTypes:Artifact\nOracle:x\n")
	}
	for i := 0; i < 3; i++ {
		mkPlayerZoneCard(t, g, 1, state.ZLibrary, "Name:Filler\nManaCost:2\nTypes:Sorcery\nOracle:x\n")
	}
	// Two of the target player's OWN spells sit in their graveyard.
	mkPlayerZoneCard(t, g, 1, state.ZGraveyard, "Name:Trick\nManaCost:U\nTypes:Instant\nOracle:x\n")
	mkPlayerZoneCard(t, g, 1, state.ZGraveyard, "Name:Ritual2\nManaCost:R\nTypes:Sorcery\nOracle:x\n")
	h := &fakeHost{g: g,
		dmgTaken:  map[state.PlayerID]int32{1: 6},
		lifeLost:  map[state.PlayerID]int32{1: 4},
		discarded: map[state.PlayerID]int32{1: 5}}
	player := state.Target{Player: 1, IsPlayer: true}
	obj := state.Target{Obj: ids["myBear"]}
	base := &Ctx{Source: ids["myBear"], Controller: 0, Targets: []state.Target{obj, player}}
	for _, tc := range []struct {
		expr string
		want int32
		ctx  *Ctx
	}{
		// The definable property table.
		{"TargetedPlayer$LifeTotal", 17, base},
		{"TargetedPlayer$CardsInHand", 2, base},
		{"TargetedPlayer$CardsInLibrary", 3, base},
		{"TargetedPlayer$CardsInGraveyard", 2, base},
		{"TargetedPlayer$CreaturesInPlay", 1, base}, // theirBig, the target's only creature
		{"TargetedPlayer$Valid Creature.YouCtrl", 1, base},
		{"TargetedPlayer$ValidGraveyard Instant.YouOwn,Sorcery.YouOwn", 2, base},
		{"TargetedPlayer$ValidGraveyard Instant.YouOwn", 1, base},
		{"TargetedPlayer$DamageThisTurn", 6, base},
		{"TargetedPlayer$LifeLostThisTurn", 4, base},
		{"TargetedPlayer$CardsDiscardedThisTurn", 5, base},
		{"TargetedPlayer$Counters.Poison", 3, base},
		// The sibling ThisTargetedPlayer spelling reads the same list.
		{"ThisTargetedPlayer$CardsInHand", 2, base},
		// /Op suffixes apply through the ordinary arithmetic.
		{"TargetedPlayer$LifeTotal/HalfUp", 9, base},
		{"TargetedPlayer$DamageThisTurn/Twice", 12, base},
		// PickedTargets (the generic pre-ask's answered set) outranks the
		// resolution-level Targets -- effects/context.go's Defined$ precedence;
		// seat 0's life is deliberately different.
		{"TargetedPlayer$LifeTotal", 17, &Ctx{Source: ids["myBear"], Controller: 0,
			Targets:       []state.Target{{Player: 0, IsPlayer: true}},
			PickedTargets: []state.Target{player}}},
		// Out-of-scope properties and an unknown ref stay zero (fall through unresolved).
		{"TargetedPlayer$StartingLife", 0, base},
		{"TargetedPlayer$DomainPlayer", 0, base},
		{"TargetedPlayer$CardsDrawn", 0, base},
		{"TargetedPlayer$Amount", 1, base},
		{"BogusPlayer$CardsInHand", 0, base},
		// A body naming no player target counts nothing (the object target in
		// the fixture's list is skipped, never dereferenced).
		{"TargetedPlayer$LifeTotal", 0, &Ctx{Source: ids["myBear"], Controller: 0,
			Targets: []state.Target{obj}}},
	} {
		if got := EvalCount(h, tc.ctx, tc.expr); got != tc.want {
			t.Errorf("%s = %d, want %d", tc.expr, got, tc.want)
		}
	}
}
