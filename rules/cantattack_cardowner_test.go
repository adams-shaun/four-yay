package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// playerClause extracts the actual PLAYER half of a corpus restriction body.
// The planeswalker half is deliberately excluded: it blocks the same defender
// once an owner-planeswalker is present and would mask a player-only defect.
func playerClause(t *testing.T, body string) string {
	t.Helper()
	for _, part := range strings.Split(body, "|") {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "Target$ ") {
			continue
		}
		for _, clause := range strings.Split(strings.TrimPrefix(part, "Target$ "), ",") {
			clause = strings.TrimSpace(clause)
			if !strings.HasPrefix(clause, "Planeswalker.") {
				return clause
			}
		}
	}
	t.Fatalf("precondition: corpus restriction has no player Target$: %q", body)
	return ""
}

// TestCantAttackPlayerCardOwner proves Xantcha's exact PLAYER-only
// Player.CardOwner target clause blocks an attack against its owner once the
// creature has changed controllers, with no owner planeswalker present to
// make the walker half (TestCantAttackWalkerControlledByCardOwner) the cause.
func TestCantAttackPlayerCardOwner(t *testing.T) {
	e := layerEngine(t)
	x := corpusCard(t, "Xantcha, Sleeper Agent")
	if len(x.Faces[0].Statics) == 0 {
		t.Fatal("precondition: Xantcha has no statics")
	}
	target := x.Faces[0].Statics[1].Params["Target"]
	clause := playerClause(t, "Target$ "+target)
	if clause != "Player.CardOwner" {
		t.Fatalf("precondition: corpus player clause = %q, want %q", clause, "Player.CardOwner")
	}

	id := onBoardCard(t, e, 0, x)
	e.emit(events.Event{Kind: events.ControlChange, Obj: id, Player: 1})
	// Preconditions the restriction rule actually reads: Xantcha is on the
	// battlefield, its owner and controller genuinely differ, and the owner
	// has no planeswalker (so the walker half cannot be the blocker).
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Xantcha must be on the battlefield: %+v", o)
	}
	if o.Owner == o.Controller {
		t.Fatalf("precondition: owner must differ from controller: owner=%v controller=%v", o.Owner, o.Controller)
	}
	if o.Owner != 0 || o.Controller != 1 {
		t.Fatalf("precondition: want owner 0 / controller 1, got owner=%v controller=%v", o.Owner, o.Controller)
	}
	for _, wid := range e.G.Zone(state.ZBattlefield, o.Owner) {
		if w := e.G.Obj(wid); w != nil && faceHasType(w, "Planeswalker") {
			t.Fatalf("precondition: owner 0 must control no planeswalker, found %+v", w)
		}
	}

	// Register Xantcha's exact player-only clause so the player half is
	// independently observable through attackBlocked, with the walker half
	// deliberately absent.
	e.AddContinuous(ContinuousEffect{Source: id, Controller: 1, Restriction: "CantAttack",
		RestrictParams: map[string]string{"ValidCard": "Card.Self", "Target": clause}})

	if !e.attackBlocked(id, 0, 0) {
		t.Fatal("Xantcha could attack its owner: Player.CardOwner clause not enforced")
	}
	if e.attackBlocked(id, 1, 0) {
		t.Fatal("Player.CardOwner clause also blocked Xantcha's controller")
	}
}
