package view_test

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// aggroSrc is a cheap red creature: the classifier should read the red mana
// symbol and the <=2 curve as aggro/burn.
const aggroSrc = "Name:Test Goblin\nManaCost:R\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n"

// controlSrc is a blue counterspell: blue + a Counter spell ability.
const controlSrc = "Name:Test Denial\nManaCost:1 U\nTypes:Instant\n" +
	"A:SP$ Counter | TargetType$ Spell | ValidTgts$ Card | Oracle:x\n"

func parseArchetypeCard(t *testing.T, src string) *cards.Card {
	t.Helper()
	c, diags := cards.ParseBytes("archetype-fixture.txt", []byte(src))
	if len(diags) > 0 {
		t.Fatalf("fixture parse: %v", diags)
	}
	c.Link()
	return c
}

// TestOppArchetypePosteriorFromRevealedCards is the exposure ratchet's own
// evidence: it builds an opponent whose public zones carry aggressive red
// cards and asserts the viewer's seat projects a posterior that names aggro,
// while the opponent's own view of itself carries none (the fact is an
// OPPONENT posterior).
func TestOppArchetypePosteriorFromRevealedCards(t *testing.T) {
	g := state.NewGame([]string{"Viewer", "Opp"})
	for i := 0; i < 3; i++ {
		c := parseArchetypeCard(t, aggroSrc)
		o := g.AddObject(c, 1)
		events.Apply(g, events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZHand, To: state.ZBattlefield})
	}
	c := parseArchetypeCard(t, aggroSrc)
	o := g.AddObject(c, 1)
	events.Apply(g, events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZHand, To: state.ZGraveyard})

	v := view.ProjectFor(g, nil, 0, view.Seat, (*decision.Decision)(nil))

	// Precondition: the opponent really has revealed cards in its public
	// zones, so a nil posterior would mean the classifier never ran.
	opp := v.Players[1]
	if len(opp.Battlefield)+len(opp.Graveyard) != 4 {
		t.Fatalf("precondition: opponent revealed %d cards, want 4", len(opp.Battlefield)+len(opp.Graveyard))
	}
	if opp.Archetype == nil {
		t.Fatal("opponent has no archetype posterior despite 4 revealed cards")
	}
	if !opp.Archetype.Known {
		t.Fatalf("posterior not Known: %+v", opp.Archetype)
	}
	if opp.Archetype.Seen != 4 {
		t.Fatalf("posterior Seen=%d, want 4", opp.Archetype.Seen)
	}
	if opp.Archetype.Top != "aggro" {
		t.Fatalf("posterior Top=%q (scores %v), want aggro", opp.Archetype.Top, opp.Archetype.Scores)
	}
	if opp.Archetype.TopScore <= 0.5 {
		t.Fatalf("posterior TopScore=%v, want >0.5", opp.Archetype.TopScore)
	}
	sum := 0.0
	for _, p := range opp.Archetype.Scores {
		if p < 0 {
			t.Fatalf("negative posterior entry %v", p)
		}
		sum += p
	}
	if sum < 0.999 || sum > 1.001 {
		t.Fatalf("posterior scores sum to %v, want 1", sum)
	}

	// The viewer's OWN seat carries no posterior: it is an opponent fact.
	if v.Players[0].Archetype != nil {
		t.Fatalf("viewer's own seat carries a posterior: %+v", v.Players[0].Archetype)
	}
}

// TestArchetypePosteriorIgnoresHiddenAndFacedownCards proves the classifier
// reads only REVEALED cards: a card in the opponent's hidden hand contributes
// nothing, and neither does a face-down exile.
func TestArchetypePosteriorIgnoresHiddenAndFacedownCards(t *testing.T) {
	g := state.NewGame([]string{"Viewer", "Opp"})
	hidden := parseArchetypeCard(t, aggroSrc)
	ho := g.AddObject(hidden, 1)
	g.SetZone(state.ZHand, 1, []state.ObjID{ho.ID})

	faceDown := parseArchetypeCard(t, aggroSrc)
	fo := g.AddObject(faceDown, 1)
	events.Apply(g, events.Event{Kind: events.MoveZone, Obj: fo.ID, From: state.ZHand, To: state.ZExile})
	g.Obj(fo.ID).FaceDown = true

	v := view.ProjectFor(g, nil, 0, view.Seat, (*decision.Decision)(nil))
	opp := v.Players[1]
	if len(opp.Exile) != 1 || !opp.Exile[0].FaceDown {
		t.Fatalf("precondition: face-down exile not projected face-down: %+v", opp.Exile)
	}
	if opp.Archetype != nil {
		t.Fatalf("hidden/face-down cards produced a posterior: %+v", opp.Archetype)
	}
}

// TestArchetypePosteriorAbsentForSpectator pins that a viewer naming no real
// seat is never handed a per-opponent posterior: there is no "opponent" for
// a spectator to have one about.
func TestArchetypePosteriorAbsentForSpectator(t *testing.T) {
	g := state.NewGame([]string{"Viewer", "Opp"})
	c := parseArchetypeCard(t, aggroSrc)
	o := g.AddObject(c, 1)
	events.Apply(g, events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZHand, To: state.ZBattlefield})

	v := view.ProjectFor(g, nil, view.NoSeat, view.Public, (*decision.Decision)(nil))
	for i := range v.Players {
		if v.Players[i].Archetype != nil {
			t.Fatalf("spectator got a posterior for seat %d: %+v", i, v.Players[i].Archetype)
		}
	}
}

// TestArchetypePosteriorReadsControlSpellClass is the sibling classifier
// path: a blue counterspell must land on control/tempo, proving the API
// class, not just the colour, reaches the posterior.
func TestArchetypePosteriorReadsControlSpellClass(t *testing.T) {
	g := state.NewGame([]string{"Viewer", "Opp"})
	for i := 0; i < 2; i++ {
		c := parseArchetypeCard(t, controlSrc)
		o := g.AddObject(c, 1)
		events.Apply(g, events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZHand, To: state.ZGraveyard})
	}
	v := view.ProjectFor(g, nil, 0, view.Seat, (*decision.Decision)(nil))
	opp := v.Players[1]
	if len(opp.Graveyard) != 2 {
		t.Fatalf("precondition: graveyard %+v", opp.Graveyard)
	}
	if opp.Archetype == nil || !opp.Archetype.Known {
		t.Fatalf("no known posterior: %+v", opp.Archetype)
	}
	ctrl := opp.Archetype.Scores["control"] + opp.Archetype.Scores["tempo"]
	if ctrl <= opp.Archetype.Scores["aggro"] {
		t.Fatalf("counterspells did not outweigh aggro: %v", opp.Archetype.Scores)
	}
}
