package searchprobe

import (
	"errors"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestCompileEpochsCapturesActorPositionsAndOpponentDeadlines(t *testing.T) {
	h := History{Actor: 0, Frames: []Frame{
		{
			Identities: []Identity{{ID: 1, Name: "Actor Draw", Owner: 0}},
			Events:     append([]ObservedEvent{{Kind: events.Shuffle, Player: 0}, {Kind: events.Draw, Player: 0, Obj: 1}, {Kind: events.Shuffle, Player: 1}}, repeatedDraws(1, 7)...),
		},
		{
			Identities: []Identity{{ID: 2, Name: "Public Card", Owner: 1}},
			Events:     []ObservedEvent{{Kind: events.MoveZone, Player: 1, Obj: 2, From: state.ZHand, To: state.ZBattlefield}},
		},
		{Events: []ObservedEvent{{Kind: events.Draw, Player: 1}}},
		{
			Identities: []Identity{{ID: 3, Name: "Public Card", Owner: 1}},
			Events:     []ObservedEvent{{Kind: events.PutOnStack, Player: 1, Obj: 3, From: state.ZHand, To: state.ZStack}},
		},
		{Events: []ObservedEvent{{Kind: events.MoveZone, Player: 1, Obj: 2, From: state.ZBattlefield, To: state.ZHand}}},
		{Events: []ObservedEvent{{Kind: events.MoveZone, Player: 1, Obj: 2, From: state.ZHand, To: state.ZBattlefield}}},
	}}
	epochs, err := compileEpochs(h)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := epochs[epochKey{Player: 0, Ordinal: 0}].Positions, []epochPosition{{Index: 0, Name: "Actor Draw", Unseen: true}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("actor positions = %+v want %+v", got, want)
	}
	if got, want := epochs[epochKey{Player: 1, Ordinal: 0}].Deadlines, []deadlineConstraint{{Through: 7, Name: "Public Card", Count: 1}, {Through: 8, Name: "Public Card", Count: 2}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("opponent deadlines = %+v want %+v", got, want)
	}
}

func TestCompileEpochsCapturesLaterShuffleAndArrangeWindow(t *testing.T) {
	h := History{Actor: 0, Frames: []Frame{
		{Events: []ObservedEvent{{Kind: events.Shuffle, Player: 0}}},
		{Events: []ObservedEvent{{Kind: events.Shuffle, Player: 0}}},
		{Identities: []Identity{{ID: 1, Name: "First", Owner: 0}, {ID: 2, Name: "Second", Owner: 0}}, Events: []ObservedEvent{{Kind: events.Draw, Player: 0, Obj: 1}, {Kind: events.Draw, Player: 0, Obj: 2}}},
		{
			Identities: []Identity{{ID: 3, Name: "Top A", Owner: 0}, {ID: 4, Name: "Top B", Owner: 0}},
			Decision: &ObservedDecision{Player: 0, Kind: decision.KArrange, Options: []ObservedOption{
				{Action: Action{Decision: decision.KArrange, Obj: 3}},
				{Action: Action{Decision: decision.KArrange, Obj: 4}},
			}},
		},
		{Events: []ObservedEvent{{Kind: events.LibraryOrder, Player: 0}}},
		{Identities: []Identity{{ID: 5, Name: "After Arrange", Owner: 0}}, Events: []ObservedEvent{{Kind: events.Draw, Player: 0, Obj: 5}}},
	}}
	epochs, err := compileEpochs(h)
	if err != nil {
		t.Fatal(err)
	}
	got := epochs[epochKey{Player: 0, Ordinal: 1}]
	wantPositions := []epochPosition{{Index: 0, Name: "First", Unseen: true}, {Index: 1, Name: "Second", Unseen: true}, {Index: 2, Name: "Top A", Ref: 3}, {Index: 3, Name: "Top B", Ref: 4}}
	if !reflect.DeepEqual(got.Positions, wantPositions) || got.ArrangeWindows != 1 {
		t.Fatalf("later epoch = %+v want positions=%+v arrange=1", got, wantPositions)
	}
	if !reflect.DeepEqual(got.Unguided, []string{"library_order"}) {
		t.Fatalf("unguided = %v", got.Unguided)
	}
}

func TestCompileEpochsMarksNonDrawLibraryMutationUnguidedUntilShuffle(t *testing.T) {
	h := History{Actor: 0, Frames: []Frame{
		{Events: []ObservedEvent{{Kind: events.Shuffle, Player: 0}}},
		{Events: []ObservedEvent{{Kind: events.MoveZone, Player: 0, From: state.ZLibrary, To: state.ZHand}}},
		{Identities: []Identity{{ID: 1, Name: "Unknown Position", Owner: 0}}, Events: []ObservedEvent{{Kind: events.Draw, Player: 0, Obj: 1}}},
		{Events: []ObservedEvent{{Kind: events.Shuffle, Player: 0}}},
		{Identities: []Identity{{ID: 2, Name: "Known Again", Owner: 0}}, Events: []ObservedEvent{{Kind: events.Draw, Player: 0, Obj: 2}}},
	}}
	epochs, err := compileEpochs(h)
	if err != nil {
		t.Fatal(err)
	}
	first := epochs[epochKey{Player: 0, Ordinal: 0}]
	if len(first.Positions) != 0 || !reflect.DeepEqual(first.Unguided, []string{"library_mutation"}) {
		t.Fatalf("first epoch = %+v", first)
	}
	second := epochs[epochKey{Player: 0, Ordinal: 1}]
	if got, want := second.Positions, []epochPosition{{Index: 0, Name: "Known Again", Unseen: true}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("second positions = %+v want %+v", got, want)
	}
}

func repeatedDraws(player state.PlayerID, n int) []ObservedEvent {
	out := make([]ObservedEvent, n)
	for i := range out {
		out[i] = ObservedEvent{Kind: events.Draw, Player: player}
	}
	return out
}

// MoveZone.Player can name the effect controller rather than the owner. Using
// its cursor drops a public hand requirement when only the owner's is reliable.
func TestCompileEpochsHandExitUsesObservedOwner(t *testing.T) {
	h := History{Actor: 0, Frames: []Frame{
		{Events: append([]ObservedEvent{{Kind: events.Shuffle, Player: 1}}, repeatedDraws(1, 7)...)},
		{Identities: []Identity{{ID: 1, Name: "Required", Owner: 1}}, Events: []ObservedEvent{{Kind: events.MoveZone, Player: 0, Obj: 1, From: state.ZHand, To: state.ZExile}}},
	}}
	epochs, err := compileEpochs(h)
	if err != nil {
		t.Fatal(err)
	}
	want := []deadlineConstraint{{Through: 7, Name: "Required", Count: 1}}
	if got := epochs[epochKey{Player: 1}].Deadlines; !reflect.DeepEqual(got, want) {
		t.Fatalf("owner's public hand deadline = %+v want %+v", got, want)
	}
}

func TestCompileEpochsLibraryMutationDoesNotTrustEventPlayer(t *testing.T) {
	for _, known := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown owner", true: "public owner"}[known], func(t *testing.T) {
			mutation := Frame{Events: []ObservedEvent{{Kind: events.MoveZone, Player: 1, From: state.ZLibrary, To: state.ZHand}}}
			if known {
				mutation.Identities = []Identity{{ID: 1, Name: "Removed", Owner: 0}}
				mutation.Events[0].Obj = 1
			}
			h := History{Actor: 0, Frames: []Frame{
				{Events: []ObservedEvent{{Kind: events.Shuffle, Player: 0}, {Kind: events.Shuffle, Player: 1}}}, mutation,
				{Identities: []Identity{{ID: 2, Name: "Unpositioned", Owner: 0}}, Events: []ObservedEvent{{Kind: events.Draw, Player: 0, Obj: 2}}},
			}}
			epochs, err := compileEpochs(h)
			if err != nil {
				t.Fatal(err)
			}
			if ep := epochs[epochKey{Player: 0}]; len(ep.Positions) != 0 || len(ep.Unguided) != 1 {
				t.Fatalf("actor positional reliability survived mutation: %+v", ep)
			}
			if got := len(epochs[epochKey{Player: 1}].Unguided); (got > 0) == known {
				t.Fatalf("other epoch unguided=%d with public owner=%v", got, known)
			}
		})
	}
}

func TestCompileEpochsRetainsKnownPhysicalReferences(t *testing.T) {
	h := History{Actor: 0, Frames: []Frame{
		{Identities: []Identity{{ID: 1, Name: "Duplicate", Owner: 0}, {ID: 2, Name: "Duplicate", Owner: 0}}, Events: []ObservedEvent{{Kind: events.Shuffle, Player: 0}}},
		{Events: []ObservedEvent{{Kind: events.MoveZone, Obj: 1, From: state.ZHand, To: state.ZLibrary}, {Kind: events.MoveZone, Obj: 2, From: state.ZHand, To: state.ZLibrary}, {Kind: events.Shuffle, Player: 0}}},
		{Events: []ObservedEvent{{Kind: events.Draw, Player: 0, Obj: 1}}, Decision: &ObservedDecision{Player: 0, Kind: decision.KArrange, Options: []ObservedOption{{Action: Action{Decision: decision.KArrange, Obj: 2}}}}},
	}}
	epochs, err := compileEpochs(h)
	if err != nil {
		t.Fatal(err)
	}
	want := []epochPosition{{Index: 0, Name: "Duplicate", Ref: 1}, {Index: 1, Name: "Duplicate", Ref: 2}}
	if got := epochs[epochKey{Player: 0, Ordinal: 1}].Positions; !reflect.DeepEqual(got, want) {
		t.Fatalf("lost physical identity: got %+v want %+v", got, want)
	}
}

func TestCompileEpochsCountsFirstPublicExitOnceWithinBurst(t *testing.T) {
	h := History{Actor: 0, Frames: []Frame{
		{Events: append([]ObservedEvent{{Kind: events.Shuffle, Player: 1}}, repeatedDraws(1, 7)...)},
		{Identities: []Identity{{ID: 1, Name: "Bounce", Owner: 1}}, Events: []ObservedEvent{
			{Kind: events.MoveZone, Player: 1, Obj: 1, From: state.ZHand, To: state.ZBattlefield},
			{Kind: events.MoveZone, Player: 1, Obj: 1, From: state.ZBattlefield, To: state.ZHand},
			{Kind: events.MoveZone, Player: 1, Obj: 1, From: state.ZHand, To: state.ZBattlefield},
		}},
	}}
	epochs, err := compileEpochs(h)
	if err != nil {
		t.Fatal(err)
	}
	want := []deadlineConstraint{{Through: 7, Name: "Bounce", Count: 1}}
	if got := epochs[epochKey{Player: 1}].Deadlines; !reflect.DeepEqual(got, want) {
		t.Fatalf("repeated public object counted twice: %+v", got)
	}
}

func TestCompileEpochsRejectsNamelessCardConstraints(t *testing.T) {
	for _, kind := range []string{"actor draw", "actor arrange", "opponent public hand exit"} {
		for _, unguided := range []bool{false, true} {
			name := kind
			if unguided {
				name += " after positional invalidation"
			}
			t.Run(name, func(t *testing.T) {
				owner := state.PlayerID(0)
				if kind == "opponent public hand exit" {
					owner = 1
				}
				fact := Frame{Identities: []Identity{{ID: 1, Owner: owner}}}
				if unguided {
					fact.Events = append(fact.Events, ObservedEvent{Kind: events.LibraryOrder, Player: owner})
				}
				switch kind {
				case "actor draw":
					fact.Events = append(fact.Events, ObservedEvent{Kind: events.Draw, Player: 0, Obj: 1})
				case "actor arrange":
					fact.Decision = &ObservedDecision{Player: 0, Kind: decision.KArrange, Options: []ObservedOption{{Action: Action{Decision: decision.KArrange, Obj: 1}}}}
				case "opponent public hand exit":
					fact.Events = append(fact.Events, ObservedEvent{Kind: events.MoveZone, Player: 0, Obj: 1, From: state.ZHand, To: state.ZExile})
				}
				h := History{Actor: 0, Frames: []Frame{{Events: append([]ObservedEvent{{Kind: events.Shuffle, Player: 0}, {Kind: events.Shuffle, Player: 1}}, repeatedDraws(1, 7)...)}, fact}}
				epochs, err := compileEpochs(h)
				var failure *Failure
				if !errors.As(err, &failure) || failure.Kind != "contradictory" || epochs != nil {
					t.Fatalf("nameless card fact did not fail closed: epochs=%+v err=%v", epochs, err)
				}
			})
		}
	}
}

func TestCompileEpochsRejectsSameExactRefAtDifferentPositions(t *testing.T) {
	h := History{Actor: 0, Frames: []Frame{
		{Identities: []Identity{{ID: 1, Name: "Duplicate", Owner: 0}, {ID: 2, Name: "Duplicate", Owner: 0}}, Events: []ObservedEvent{{Kind: events.Shuffle, Player: 0}}},
		{Events: []ObservedEvent{{Kind: events.Draw, Player: 0, Obj: 1}, {Kind: events.Draw, Player: 0, Obj: 1}}},
	}}
	epochs, err := compileEpochs(h)
	var failure *Failure
	if epochs != nil || !errors.As(err, &failure) || failure.Kind != "contradictory" {
		t.Fatalf("same physical object assigned twice: epochs=%+v err=%v", epochs, err)
	}
	// The same card name on two distinct physical references remains legal.
	h.Frames[1].Events[1].Obj = 2
	if _, err := compileEpochs(h); err != nil {
		t.Fatalf("distinct duplicate-name cards rejected: %v", err)
	}
}

func TestCompileEpochsOnlyPreservesSupportedAnsweredArrange(t *testing.T) {
	for _, shape := range []string{"supported", "missing answer", "foreign answer", "duplicate answer", "mixed destinations", "graveyard", "extra order", "unmodelled order"} {
		t.Run(shape, func(t *testing.T) {
			a := Action{Decision: decision.KArrange, Kind: "bottom", Obj: 1}
			b := Action{Decision: decision.KArrange, Kind: "bottom", Obj: 2}
			h := History{Actor: 0, Answers: map[int][]Action{1: {a}}, Frames: []Frame{
				{Events: []ObservedEvent{{Kind: events.Shuffle, Player: 0}}},
				{Board: Board{Players: []BoardPlayer{{Seat: 0, LibrarySize: 4}}}, Identities: []Identity{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}}, Decision: &ObservedDecision{Player: 0, Kind: decision.KArrange, Max: 2, Options: []ObservedOption{{Action: a}, {Action: b}}}},
				{Identities: []Identity{{ID: 3, Name: "C"}}, Events: []ObservedEvent{{Kind: events.LibraryOrder, Player: 0}, {Kind: events.Draw, Player: 0, Obj: 1}, {Kind: events.Draw, Player: 0, Obj: 3}}},
			}}
			switch shape {
			case "missing answer":
				h.Answers = nil
			case "foreign answer":
				h.Answers[1][0].Obj = 99
			case "duplicate answer":
				h.Answers[1] = []Action{a, a}
			case "mixed destinations":
				h.Frames[1].Decision.Options[1].Action.Kind = "top"
			case "graveyard":
				h.Frames[1].Decision.Options[0].Action.Kind = "graveyard"
				h.Frames[1].Decision.Options[1].Action.Kind = "graveyard"
				h.Answers[1][0].Kind = "graveyard"
			case "extra order":
				h.Frames[2].Events = append([]ObservedEvent{{Kind: events.LibraryOrder, Player: 0}}, h.Frames[2].Events...)
			case "unmodelled order":
				h.Frames[1].Decision = nil
			}
			epochs, err := compileEpochs(h)
			if err != nil {
				t.Fatal(err)
			}
			ep := epochs[epochKey{Player: 0}]
			found := false
			for _, p := range ep.Positions {
				if p.Name == "C" {
					found = p.Index == 2
				}
			}
			if shape == "supported" {
				if !found || len(ep.Unguided) != 0 {
					t.Fatalf("supported order lost guidance: %+v", ep)
				}
			} else if found || len(ep.Unguided) == 0 {
				t.Fatalf("invalid/unmodelled order retained guidance: %+v", ep)
			}
		})
	}
}
