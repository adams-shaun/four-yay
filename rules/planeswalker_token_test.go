package rules

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestPlaneswalkerTokenEntersWithStartingLoyalty(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	card := mustCorpusCard(t, reg, "Jace, the Mind Sculptor")
	starting, err := strconv.Atoi(card.Faces[0].Loyalty)
	if err != nil || starting <= 0 {
		t.Fatalf("Jace starting loyalty = %q, want a positive integer", card.Faces[0].Loyalty)
	}
	e, _, ids := dsBoardWith(t, reg, nil, "Jace, the Mind Sculptor")
	// Move the source card away so the minted legendary token is the only
	// Jace on the battlefield and the legend rule cannot mask the SBA check.
	e.emit(events.Event{Kind: events.MoveZone, Obj: ids["Jace, the Mind Sculptor"],
		From: state.ZBattlefield, To: state.ZHand})
	e.G.Tokens = map[string]*cards.Card{"test_planeswalker": card}
	e.emit(events.Event{Kind: events.TokenCreate, Player: 0, Text: "test_planeswalker"})

	var token state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o != nil && o.IsToken && o.Face() != nil && o.Face().Name == card.Faces[0].Name {
			token = id
		}
	}
	if token == 0 {
		t.Fatal("TokenCreate did not put the planeswalker token on the battlefield")
	}
	if got := e.G.Obj(token).Counter("LOYALTY"); got != int32(starting) {
		t.Fatalf("planeswalker token entered with %d loyalty, want starting loyalty %d", got, starting)
	}
	e.checkStateBased()
	if got := e.G.Obj(token).Zone; got != state.ZBattlefield {
		t.Fatalf("planeswalker token left the battlefield after SBAs: zone %s", got)
	}
	found := false
	for _, option := range e.legalActions(0) {
		if (option.Kind == "activate" || option.Kind == "ability") && option.Obj == token {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("planeswalker token has no activatable loyalty ability")
	}
}
