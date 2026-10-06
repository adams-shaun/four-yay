package events

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// meldFixtureCard parses a freely authored fixture (never corpus text).
func meldFixtureCard(t *testing.T, src string) *cards.Card {
	t.Helper()
	c, _ := cards.ParseBytes("meld.txt", []byte(src))
	if c == nil {
		t.Fatal("fixture did not parse")
	}
	c.Link()
	return c
}

// meldFixture builds a game where seat 0 owns the result card ("Left Half",
// whose ALTERNATE face is "Joined Whole") and seat 1 owns its partner
// ("Right Half"); both are exiled, the precondition every Meld event has.
func meldFixture(t *testing.T) (g *state.Game, res, partner state.ObjID) {
	t.Helper()
	left := meldFixtureCard(t, "Name:Left Half\nManaCost:1 W\nTypes:Creature Angel\nPT:1/1\nAlternateMode:Meld\nOracle:x\n\nALTERNATE\n\nName:Joined Whole\nManaCost:no cost\nTypes:Creature Angel\nPT:8/8\nOracle:y\n")
	right := meldFixtureCard(t, "Name:Right Half\nManaCost:2 W\nTypes:Creature Angel\nPT:2/2\nAlternateMode:Meld\nOracle:z\n")
	g = state.NewGame([]string{"a", "b"})
	r, p := g.AddObject(left, 0), g.AddObject(right, 1)
	res, partner = r.ID, p.ID
	for _, o := range []*state.Object{r, p} {
		g.SetZone(state.ZLibrary, o.Owner, append(g.Zone(state.ZLibrary, o.Owner), o.ID))
		Apply(g, Event{Kind: MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZExile})
	}
	return g, res, partner
}

func TestMeldFoldPairsAndSplits(t *testing.T) {
	g, res, partner := meldFixture(t)
	if g.Obj(res).Face().Name != "Left Half" {
		t.Fatalf("precondition: result face %q", g.Obj(res).Face().Name)
	}
	Apply(g, Event{Kind: Meld, Obj: res, IDs: []state.ObjID{partner}, Player: 1, Amount: 1, Text: MeldText})
	o := g.Obj(res)
	if o.Face().Name != "Joined Whole" || o.MeldedWith != partner || o.Controller != 1 {
		t.Fatalf("after Meld: face=%q meldedWith=%d controller=%d", o.Face().Name, o.MeldedWith, o.Controller)
	}
	if g.Obj(partner).Zone != state.ZCeased || slices.Contains(g.Zone(state.ZExile, 1), partner) {
		t.Fatal("the partner card must be parked off the exile list")
	}
	Apply(g, Event{Kind: MoveZone, Obj: res, From: state.ZExile, To: state.ZBattlefield})
	if !slices.Contains(g.Zone(state.ZBattlefield, 1), res) {
		t.Fatalf("the melded permanent entered under seat %d, want the melding seat 1", g.Obj(res).Controller)
	}
	Apply(g, Event{Kind: MoveZone, Obj: res, From: state.ZBattlefield, To: state.ZGraveyard})
	if !slices.Contains(g.Zone(state.ZGraveyard, 0), res) || !slices.Contains(g.Zone(state.ZGraveyard, 1), partner) {
		t.Fatalf("split: seat0 grave %v, seat1 grave %v; want each card in its owner's graveyard", g.Zone(state.ZGraveyard, 0), g.Zone(state.ZGraveyard, 1))
	}
	if o := g.Obj(res); o.MeldedWith != 0 || o.Face().Name != "Left Half" {
		t.Fatalf("after the split: meldedWith=%d face=%q", o.MeldedWith, o.Face().Name)
	}
	if g.Obj(partner).EnteredFrom != state.ZBattlefield {
		t.Fatalf("partner entered from %s, want battlefield", g.Obj(partner).EnteredFrom)
	}
}

// TestMeldFoldGuards: a Meld naming the front face, a partner outside
// exile, or an already-melded result folds nothing.
func TestMeldFoldGuards(t *testing.T) {
	g, res, partner := meldFixture(t)
	Apply(g, Event{Kind: Meld, Obj: res, IDs: []state.ObjID{partner}, Player: 0, Amount: 0, Text: MeldText})
	if g.Obj(res).MeldedWith != 0 || g.Obj(partner).Zone != state.ZExile {
		t.Fatal("a Meld naming the front face must not pair")
	}
	Apply(g, Event{Kind: MoveZone, Obj: partner, From: state.ZExile, To: state.ZGraveyard})
	Apply(g, Event{Kind: Meld, Obj: res, IDs: []state.ObjID{partner}, Player: 0, Amount: 1, Text: MeldText})
	if g.Obj(res).MeldedWith != 0 || g.Obj(res).FaceIdx != 0 || g.Obj(partner).Zone != state.ZGraveyard {
		t.Fatal("a Meld whose partner left exile must not pair")
	}
}

// TestMeldFoldReplacedEntrySplits: an entry an "instead" replacement sends
// from exile to another zone (the result card never reaches the
// battlefield) splits the pair exactly like a departure.
func TestMeldFoldReplacedEntrySplits(t *testing.T) {
	g, res, partner := meldFixture(t)
	Apply(g, Event{Kind: Meld, Obj: res, IDs: []state.ObjID{partner}, Player: 0, Amount: 1, Text: MeldText})
	if g.Obj(res).MeldedWith != partner {
		t.Fatal("precondition: the pair did not meld")
	}
	Apply(g, Event{Kind: MoveZone, Obj: res, From: state.ZExile, To: state.ZHand})
	if !slices.Contains(g.Zone(state.ZHand, 0), res) || !slices.Contains(g.Zone(state.ZHand, 1), partner) {
		t.Fatalf("hands %v / %v; want each card in its owner's hand", g.Zone(state.ZHand, 0), g.Zone(state.ZHand, 1))
	}
	if o := g.Obj(res); o.MeldedWith != 0 || o.FaceIdx != 0 || o.Controller != o.Owner {
		t.Fatalf("after the split: meldedWith=%d face=%d controller=%d", o.MeldedWith, o.FaceIdx, o.Controller)
	}
}
