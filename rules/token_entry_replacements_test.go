package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// tokenEntryBoard puts the named corpus permanent onto seat 0's battlefield
// and mints one 1/1 Goblin token for seat `owner` through the ordinary
// TokenCreate emit, returning the token.
func tokenEntryBoard(t *testing.T, permanent string, owner state.PlayerID) (*Engine, *state.Object) {
	t.Helper()
	e := handEngineTokens(t, corpusAlternativeCard(t, permanent))
	id := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	drainQueuedTriggers(t, e)
	want := e.G.NextID
	e.EmitTokenCreate(events.Event{Kind: events.TokenCreate, Player: owner, Text: "r_1_1_goblin"})
	drainQueuedTriggers(t, e)
	tok := e.G.Obj(want)
	if tok == nil || tok.Zone != state.ZBattlefield || !tok.IsToken {
		t.Fatalf("precondition: the Goblin token was not created: %+v", tok)
	}
	return e, tok
}

// TestAuthorityOfTheConsulsTapsOpponentsTokens: a token created onto the
// battlefield enters it, so "Creatures your opponents control enter tapped"
// taps an opponent's creature token (CR 111.1, 614.1c) and leaves its
// controller's own token untapped. The scripted mint has no MoveZone, so the
// Moved replacement never matched it before.
func TestAuthorityOfTheConsulsTapsOpponentsTokens(t *testing.T) {
	t.Parallel()
	_, tok := tokenEntryBoard(t, "Authority of the Consuls", 1)
	if !tok.Tapped {
		t.Fatal("the opponent's Goblin token entered untapped under Authority of the Consuls")
	}
	_, own := tokenEntryBoard(t, "Authority of the Consuls", 0)
	if own.Tapped {
		t.Fatal("Authority of the Consuls tapped its controller's own token")
	}
}

// TestGrumgullyCountersOwnCreatureTokens: the K:ETBReplacement:Other
// "enters with an additional +1/+1 counter" shape reaches a creature token
// its controller creates, and not an opponent's.
func TestGrumgullyCountersOwnCreatureTokens(t *testing.T) {
	t.Parallel()
	_, tok := tokenEntryBoard(t, "Grumgully, the Generous", 0)
	if got := tok.Counter("P1P1"); got != 1 {
		t.Fatalf("own Goblin token +1/+1 counters = %d, want 1", got)
	}
	_, theirs := tokenEntryBoard(t, "Grumgully, the Generous", 1)
	if got := theirs.Counter("P1P1"); got != 0 {
		t.Fatalf("opponent's Goblin token +1/+1 counters = %d, want 0", got)
	}
}
