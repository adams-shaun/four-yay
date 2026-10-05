package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func addKeywordPredicateFixture(t *testing.T, g *state.Game, keywords ...string) state.ObjID {
	t.Helper()
	script := "Name:Keyword fixture\nTypes:Creature Test\nPT:2/2\nOracle:test\n"
	for _, keyword := range keywords {
		script += "K:" + keyword + "\n"
	}
	card, diags := cards.ParseBytes("inline.txt", []byte(script))
	if len(diags) != 0 {
		t.Fatalf("parse inline card: %v", diags)
	}
	card.Link()
	for _, face := range card.Faces {
		face.ApplyIntrinsics()
	}
	o := g.AddObject(card, 0)
	o.Zone = state.ZBattlefield
	g.SetZone(state.ZBattlefield, 0, append(g.Zone(state.ZBattlefield, 0), o.ID))
	if o.Zone != state.ZBattlefield {
		t.Fatalf("fixture object zone = %v, want battlefield", o.Zone)
	}
	return o.ID
}

func keywordPredicateFixture(t *testing.T, keywords ...string) (*state.Game, state.ObjID) {
	t.Helper()
	g := state.NewGame([]string{"you"})
	return g, addKeywordPredicateFixture(t, g, keywords...)
}

func TestWithKeywordPredicateRecognizesUnlistedKeyword(t *testing.T) {
	g := state.NewGame([]string{"you"})
	withID := addKeywordPredicateFixture(t, g, "Hexproof")
	withoutID := addKeywordPredicateFixture(t, g)
	with, without := g.Obj(withID), g.Obj(withoutID)
	if with == nil || with.Zone != state.ZBattlefield || !with.Face().HasKeyword("Hexproof") {
		t.Fatal("precondition: battlefield fixture does not carry Hexproof")
	}
	if without == nil || without.Zone != state.ZBattlefield || without.Face().HasKeyword("Hexproof") {
		t.Fatal("precondition: comparison fixture unexpectedly carries Hexproof")
	}
	gotWith := MatchesSpec(g, "Creature.withHexproof", withID, 0)
	gotWithout := MatchesSpec(g, "Creature.withHexproof", withoutID, 0)
	if !gotWith || gotWithout || gotWith == gotWithout {
		t.Fatalf("withHexproof results for keyword/non-keyword objects = %v/%v, want true/false", gotWith, gotWithout)
	}
}

func TestWithoutKeywordPredicateRecognizesUnlistedKeyword(t *testing.T) {
	g := state.NewGame([]string{"you"})
	withID := addKeywordPredicateFixture(t, g, "Ward")
	withoutID := addKeywordPredicateFixture(t, g)
	with, without := g.Obj(withID), g.Obj(withoutID)
	if with == nil || with.Zone != state.ZBattlefield || !with.Face().HasKeyword("Ward") {
		t.Fatal("precondition: battlefield fixture does not carry Ward")
	}
	if without == nil || without.Zone != state.ZBattlefield || without.Face().HasKeyword("Ward") {
		t.Fatal("precondition: comparison fixture unexpectedly carries Ward")
	}
	gotWith := MatchesSpec(g, "Creature.withoutWard", withID, 0)
	gotWithout := MatchesSpec(g, "Creature.withoutWard", withoutID, 0)
	if gotWith || !gotWithout || gotWith == gotWithout {
		t.Fatalf("withoutWard results for keyword/non-keyword objects = %v/%v, want false/true", gotWith, gotWithout)
	}
}

func TestHasKeywordPredicateMatchesExactHead(t *testing.T) {
	g, id := keywordPredicateFixture(t, "Flashback")
	o := g.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || !o.Face().HasKeyword("Flashback") {
		t.Fatal("precondition: battlefield fixture does not carry Flashback")
	}
	gotFlash := MatchesSpec(g, "Card.hasKeywordFlash", id, 0)
	gotFlashback := MatchesSpec(g, "Card.hasKeywordFlashback", id, 0)
	if gotFlash || !gotFlashback || gotFlash == gotFlashback {
		t.Fatalf("hasKeywordFlash/Flashback on Flashback object = %v/%v, want false/true", gotFlash, gotFlashback)
	}
}

func TestUnknownKeywordPredicateFailsClosed(t *testing.T) {
	g, id := keywordPredicateFixture(t, "Hexproof")
	o := g.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || !o.Face().HasKeyword("Hexproof") {
		t.Fatal("precondition: battlefield fixture does not carry Hexproof")
	}
	unknown := UnknownPredicates("Creature.withWhatever")
	if len(unknown) != 1 || unknown[0] != "withWhatever" {
		t.Fatalf("UnknownPredicates(withWhatever) = %v, want [withWhatever]", unknown)
	}
	known := MatchesSpec(g, "Creature.withHexproof", id, 0)
	unsupported := MatchesSpec(g, "Creature.withWhatever", id, 0)
	if !known || unsupported || known == unsupported {
		t.Fatalf("known/unsupported keyword results = %v/%v, want true/false", known, unsupported)
	}
}
