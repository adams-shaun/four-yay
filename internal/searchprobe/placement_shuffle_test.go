package searchprobe

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A ChangeZoneAll into a library with no LibraryPosition$ takes Forge's TOP
// default and emits one Secret LibraryOrder (golgari_thug2). When the script
// shuffles that same library immediately afterwards, the placement leaks
// nothing -- CR 701.20 destroys the order -- so it must not cost the
// pre-shuffle epoch its guidance. These tests pin the pairing rule in
// compileEpochs: a placement is only a leak once something else observes or
// mutates the library before the erasing shuffle.

func placementPrelude() Frame {
	// Genesis: shuffle, then draw one card so a later redraw of it carries an
	// exact physical reference.
	return Frame{
		Identities: []Identity{{ID: 1, Name: "Duplicate", Owner: 0}},
		Events:     []ObservedEvent{{Kind: events.Shuffle, Player: 0}, {Kind: events.Draw, Player: 0, Obj: 1}},
	}
}

func TestCompileEpochsPlacementShufflePairLeavesPreShuffleEpochGuided(t *testing.T) {
	h := History{Actor: 0, Frames: []Frame{
		placementPrelude(),
		// The placement is immediately erased by a shuffle of the same
		// library, in the same frame.
		{Events: []ObservedEvent{{Kind: events.LibraryOrder, Player: 0}, {Kind: events.Shuffle, Player: 0}}},
		{Identities: []Identity{{ID: 2, Name: "Duplicate", Owner: 0}}, Events: []ObservedEvent{{Kind: events.Draw, Player: 0, Obj: 1}}},
	}}
	epochs, err := compileEpochs(h)
	if err != nil {
		t.Fatal(err)
	}
	// Precondition: the placement and the erasing shuffle both fired, and the
	// draw after the shuffle is in the post-shuffle epoch.
	pre := epochs[epochKey{Player: 0, Ordinal: 0}]
	if len(pre.Positions) == 0 {
		t.Fatalf("setup did not populate the pre-shuffle epoch: %+v", pre)
	}
	if got := pre.Unguided; len(got) != 0 {
		t.Fatalf("erased placement unguided the pre-shuffle epoch: %v", got)
	}
	// The post-shuffle redraw of the known object must still carry its exact
	// reference: the pairing did not knock the cursor's reliability out.
	post := epochs[epochKey{Player: 0, Ordinal: 1}]
	want := []epochPosition{{Index: 0, Name: "Duplicate", Ref: 1}}
	if !reflect.DeepEqual(post.Positions, want) {
		t.Fatalf("post-shuffle epoch lost exact reference: got %+v want %+v", post.Positions, want)
	}
}

func TestCompileEpochsPlacementShufflePairCrossesFrameBoundary(t *testing.T) {
	// The same shape with the placement at one frame's end and the shuffle at
	// the next frame's start still pairs.
	h := History{Actor: 0, Frames: []Frame{
		placementPrelude(),
		{Events: []ObservedEvent{{Kind: events.LibraryOrder, Player: 0}}},
		{Events: []ObservedEvent{{Kind: events.Shuffle, Player: 0}}},
	}}
	epochs, err := compileEpochs(h)
	if err != nil {
		t.Fatal(err)
	}
	if got := epochs[epochKey{Player: 0, Ordinal: 0}].Unguided; len(got) != 0 {
		t.Fatalf("cross-frame pairing unguided the pre-shuffle epoch: %v", got)
	}
}

func TestCompileEpochsUnpairedPlacementStillUnguides(t *testing.T) {
	// Shared setup: a placement in frame 1 followed by the various events that
	// observe or mutate the same library before any erasing shuffle. Each must
	// fall back to today's conservative result.
	cases := []struct {
		name   string
		frames []Frame
	}{
		{
			name:   "draw before shuffle",
			frames: []Frame{{Events: []ObservedEvent{{Kind: events.LibraryOrder, Player: 0}}}, {Events: []ObservedEvent{{Kind: events.Draw, Player: 0, Obj: 1}}}},
		},
		{
			name:   "second placement",
			frames: []Frame{{Events: []ObservedEvent{{Kind: events.LibraryOrder, Player: 0}}}, {Events: []ObservedEvent{{Kind: events.LibraryOrder, Player: 0}}}},
		},
		{
			name:   "move out of the library",
			frames: []Frame{{Events: []ObservedEvent{{Kind: events.LibraryOrder, Player: 0}}}, {Events: []ObservedEvent{{Kind: events.MoveZone, Player: 0, Obj: 1, From: state.ZLibrary, To: state.ZHand}}}},
		},
		{
			name:   "no following shuffle at all",
			frames: []Frame{{Events: []ObservedEvent{{Kind: events.LibraryOrder, Player: 0}}}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := History{Actor: 0, Frames: append([]Frame{placementPrelude()}, tc.frames...)}
			epochs, err := compileEpochs(h)
			if err != nil {
				t.Fatal(err)
			}
			pre := epochs[epochKey{Player: 0, Ordinal: 0}]
			// Precondition: the epoch exists and the placement fired.
			if len(pre.Positions) == 0 {
				t.Fatalf("setup did not populate the pre-shuffle epoch: %+v", pre)
			}
			found := false
			for _, reason := range pre.Unguided {
				if reason == "library_order" {
					found = true
				}
			}
			if !found {
				t.Fatalf("unpaired placement left the epoch guided: %+v", pre)
			}
		})
	}
}

func TestCompileEpochsOtherPlayersEventDoesNotConsumePlacement(t *testing.T) {
	// A shuffle or draw by player 1 says nothing about player 0's library, so
	// it must not resolve player 0's pending placement: player 0's next draw
	// still unguides.
	h := History{Actor: 0, Frames: []Frame{
		placementPrelude(),
		{Events: []ObservedEvent{{Kind: events.LibraryOrder, Player: 0}, {Kind: events.Shuffle, Player: 1}, {Kind: events.Draw, Player: 1}}},
		{Events: []ObservedEvent{{Kind: events.Draw, Player: 0, Obj: 1}}},
	}}
	epochs, err := compileEpochs(h)
	if err != nil {
		t.Fatal(err)
	}
	pre := epochs[epochKey{Player: 0, Ordinal: 0}]
	found := false
	for _, reason := range pre.Unguided {
		if reason == "library_order" {
			found = true
		}
	}
	if !found {
		t.Fatalf("another player's event consumed the pending placement: %+v", pre)
	}
}
