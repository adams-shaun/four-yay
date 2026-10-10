package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// agent-20261009T174731Z-42d5e0f4: The Beamtown Bullies' real corpus
// activation targets "target opponent whose turn it is"
// (ValidTgts$ Player.Opponent+Active). Before the dotless `+` fix the
// matcher read the bare `Active` clause as an independent spec with an
// unknown base and offered nobody.

// TestBeamtownBulliesTargetOfferIsTheActiveOpponent: with the Bullies on
// seat 0's battlefield and seat 1's turn under way, seat 0's activation ask
// offers exactly seat 1 -- an opponent AND the active player -- and never
// the activating controller.
func TestBeamtownBulliesTargetOfferIsTheActiveOpponent(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	bullies := choiceCorpusCard(t, "The Beamtown Bullies")
	obj := e.G.AddObject(bullies, 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: obj.ID, From: state.ZLibrary, To: state.ZBattlefield})
	// Precondition: the Bullies sit on the battlefield under the asker's
	// control, and it really is seat 1's turn -- an Opponent+Active offer is
	// empty otherwise and the pin would prove nothing.
	if o := e.G.Obj(obj.ID); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: the Bullies are not seat 0's battlefield permanent: %+v", e.G.Obj(obj.ID))
	}
	e.G.Active = 1
	if e.G.Active != 1 {
		t.Fatalf("precondition: active player = %d, want 1", e.G.Active)
	}
	var sa *cards.SA
	for _, a := range bullies.Faces[0].Abilities {
		if a.ParamStr(cards.PKValidTgts) == "Player.Opponent+Active" {
			sa = a
			break
		}
	}
	if sa == nil {
		t.Fatal("precondition: the Bullies' Player.Opponent+Active activation is missing")
	}
	var got []state.PlayerID
	for _, c := range e.candidatesFor(0, obj.ID, 0, sa, true) {
		if c.kind == "player" {
			got = append(got, c.player)
		}
	}
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("Player.Opponent+Active offer = %v, want exactly [1] (the activating seat 0 excluded)", got)
	}
}
