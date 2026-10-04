package rules

// Kernel-era restorations of the tests W3 removed from player_attach_destinations_test.go: the
// same behaviour driven through the resolution kernel (the only ask path).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestCurseOfLeechesTransformAttachesToChosenPlayer drives Curse of Leeches'
// `R:Event$ Transform | ReplaceWith$ Attach` delivery: as the permanent
// transforms into Curse of Leeches the replacement resolves the
// `DB$ Attach | Object$ Self | PlayerChoices$ Player` body, which poses a
// real KChoose user-player ask over BOTH living seats. Choosing the OTHER
// seat and asserting the Curse lands there proves the answer governs the
// attachment rather than a deterministic first-seat pick; the second subtest
// chooses the controller's own seat to prove "either seat".
func TestCurseOfLeechesTransformAttachesToChosenPlayerKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name string
		seat state.PlayerID
	}{
		{"choose_opponent", 1},
		{"choose_controller", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg, ids := dsBoard(t, reg, "Curse of Leeches")
			id := ids["Curse of Leeches"]
			o := e.G.Obj(id)
			if o.Zone != state.ZBattlefield {
				t.Fatalf("precondition: Curse of Leeches %d not on the battlefield: %+v", id, o)
			}
			if o.HasAttachedPlayer || o.AttachedPlayer != 0 {
				t.Fatalf("precondition: the Curse is already player-attached: %+v", o)
			}
			if o.Face() == nil || o.Face().Name != "Curse of Leeches" {
				t.Fatalf("precondition: wrong active face %v", o.Face())
			}

			// Transform to the back face (Leeching Lurker) first...
			e.emit(events.Event{Kind: events.FlipFace, Obj: id, Amount: 1})
			if got := e.Name(id); got != "Leeching Lurker" {
				t.Fatalf("precondition: face after first flip = %q, want Leeching Lurker", got)
			}
			// ...then transform BACK into Curse of Leeches: the destination
			// face, which is where the As-this-transforms replacement lives.
			// The transform runs as a kernel probe so its replacement's
			// ask is posed and answered through the tape.
			e.pending = nil
			e.probe(func() { e.emit(events.Event{Kind: events.FlipFace, Obj: id, Amount: 0}) })

			d := e.Pending()
			if d == nil || d.Kind != decision.KChoose {
				t.Fatalf("the transform replacement posed no PlayerChoices$ ask: %+v", d)
			}
			if d.Prompt != "Choose a player to attach this Curse to" {
				t.Fatalf("unexpected ask prompt %q", d.Prompt)
			}
			seatIdx := attachOptionForSeat(d, tc.seat)
			otherIdx := attachOptionForSeat(d, 1-tc.seat)
			if seatIdx < 0 || otherIdx < 0 {
				t.Fatalf("the pool must offer BOTH seats (want %d): %+v", tc.seat, d.Options)
			}
			if seatIdx == otherIdx {
				t.Fatalf("both seats offered at the same slot: %+v", d.Options)
			}
			submitChoices(t, e, seatIdx)
			kr7Settle(e)
			if got := e.Name(id); got != "Curse of Leeches" {
				t.Fatalf("the second flip did not land back on Curse of Leeches: %q", got)
			}
			passUntilStackEmpty(t, e, 40)

			o = e.G.Obj(id)
			if !o.HasAttachedPlayer || o.AttachedPlayer != tc.seat {
				t.Fatalf("the transformed Curse attached to %d (Has=%v), want seat %d", o.AttachedPlayer, o.HasAttachedPlayer, tc.seat)
			}
			if o.AttachedTo != 0 {
				t.Fatalf("a player attachment must not also carry an object bearer: AttachedTo=%d", o.AttachedTo)
			}
			replayCheck(t, e, cfg)
		})
	}
}
