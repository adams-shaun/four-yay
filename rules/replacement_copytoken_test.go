package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// copyTokenBoard deals the named permanents and Self-Reflection to seat 0,
// puts the permanents onto the battlefield, casts Self-Reflection on the
// named copy target and resolves it. It returns the engine and the number of
// battlefield objects named copyName seat 0 controls.
func copyTokenBoard(t *testing.T, copyName string, permanents ...string) (*Engine, int) {
	t.Helper()
	hand := []string{"Self-Reflection"}
	hand = append(hand, permanents...)
	cs := make([]*cards.Card, 0, len(hand))
	for _, n := range hand {
		cs = append(cs, corpusAlternativeCard(t, n))
	}
	e := handEngineTokens(t, cs...)
	ids := append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...)
	spell := ids[0]
	var target state.ObjID
	for _, id := range ids[1:] {
		placeOnBattlefield(t, e, id)
		if e.G.Obj(id).Face().Name == copyName {
			target = id
		}
	}
	e.G.Players[0].Pool[state.MU], e.G.Players[0].Pool[state.MC] = 2, 4
	castMode(t, e, spell, "")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Self-Reflection posed no target ask: %+v", d)
	}
	pick := -1
	for i, o := range d.Options {
		if o.Obj == target {
			pick = i
		}
	}
	if pick < 0 {
		t.Fatalf("%s is not offered as Self-Reflection's target: %+v", copyName, d.Options)
	}
	submitChoices(t, e, pick)
	drainQueuedTriggers(t, e)
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == copyName {
			n++
		}
	}
	return e, n
}

// TestDoublingSeasonDoublesATokenCopy: a token copy is a created token (CR
// 111.1, 706.2), so Doubling Season's CreateToken replacement makes
// Self-Reflection create two copies of Grizzly Bears -- three Bears in all.
// Before the copy mints were proposed to the CreateToken replacements, the
// DB$ CopyPermanent CopyToken bypassed them and one copy was created.
func TestDoublingSeasonDoublesATokenCopy(t *testing.T) {
	t.Parallel()
	_, bears := copyTokenBoard(t, "Grizzly Bears", "Doubling Season", "Grizzly Bears")
	if bears != 3 {
		t.Fatalf("Grizzly Bears on the battlefield = %d, want 3 (the card plus two doubled copies)", bears)
	}
}

// TestChatterfangAddsSquirrelsToATokenCopy: Chatterfang's AddToken body
// applies to a token copy too -- the copy plus one 1/1 Squirrel -- and the
// Squirrel is a scripted extra mint the proposal emits itself.
func TestChatterfangAddsSquirrelsToATokenCopy(t *testing.T) {
	t.Parallel()
	e, bears := copyTokenBoard(t, "Grizzly Bears", "Chatterfang, Squirrel General", "Grizzly Bears")
	if bears != 2 {
		t.Fatalf("Grizzly Bears on the battlefield = %d, want 2 (the card plus one copy)", bears)
	}
	squirrels := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.IsToken && o.Face() != nil && o.Face().Name != "Grizzly Bears" {
			squirrels++
		}
	}
	if squirrels != 1 {
		t.Fatalf("Squirrel tokens = %d, want 1 (that many Squirrels for the one copy)", squirrels)
	}
}

// TestTokenCopyWithoutReplacementIsOneCopy is the control: no CreateToken
// replacement, one copy.
func TestTokenCopyWithoutReplacementIsOneCopy(t *testing.T) {
	t.Parallel()
	_, bears := copyTokenBoard(t, "Grizzly Bears", "Grizzly Bears")
	if bears != 2 {
		t.Fatalf("Grizzly Bears on the battlefield = %d, want 2", bears)
	}
}
