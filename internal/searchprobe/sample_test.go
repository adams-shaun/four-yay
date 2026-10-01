package searchprobe

import (
	"encoding/json"
	"errors"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

func TestRejectionBucketClassifiesObservedShapeWithoutIdentity(t *testing.T) {
	cases := []struct {
		name string
		got  Frame
		want Frame
		out  RejectionBucket
	}{
		{
			name: "public land",
			got:  Frame{Identities: []Identity{{ID: 8, Name: "Island", Owner: 1}}, Events: []ObservedEvent{{Kind: events.MoveZone, Obj: 8, From: state.ZHand, To: state.ZBattlefield}}},
			want: Frame{Identities: []Identity{{ID: 8, Name: "Swamp", Owner: 1}}, Events: []ObservedEvent{{Kind: events.MoveZone, Obj: 8, From: state.ZHand, To: state.ZBattlefield}}},
			out:  RejectionBucket{Frame: 5, Component: "identities", Shape: "hand_to_battlefield"},
		},
		{
			name: "spell cast",
			got:  Frame{Identities: []Identity{{ID: 9, Name: "A", Owner: 1}}, Events: []ObservedEvent{{Kind: events.PutOnStack, Obj: 9, From: state.ZHand, To: state.ZStack}}},
			want: Frame{Events: []ObservedEvent{{Kind: events.Priority, Player: 0}}},
			out:  RejectionBucket{Frame: 7, Component: "identities", Shape: "hand_to_stack"},
		},
		{
			name: "event kind",
			got:  Frame{Events: []ObservedEvent{{Kind: events.Priority}}},
			want: Frame{Events: []ObservedEvent{{Kind: events.Resolve}}},
			out:  RejectionBucket{Frame: 9, Component: "events", Shape: "priority_to_stack_resolve"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rejectionBucket(5+map[string]int{"spell cast": 2, "event kind": 4}[tc.name], tc.got, tc.want); got != tc.out {
				t.Fatalf("got %+v want %+v", got, tc.out)
			}
		})
	}
}

func TestOpponentBoardsReusePerPlayerMaps(t *testing.T) {
	e := &rules.Engine{G: state.NewGame([]string{"a", "b"})}
	boards := newOpponentBoards(2)
	wantCards := reflect.ValueOf(boards[1].Cards).Pointer()
	wantLife := reflect.ValueOf(boards[1].Life).Pointer()

	first := opponentBoard(e, 1, boards)
	second := opponentBoard(e, 1, boards)
	if reflect.ValueOf(first.Cards).Pointer() != wantCards || reflect.ValueOf(second.Cards).Pointer() != wantCards {
		t.Fatal("opponent board card map was reallocated")
	}
	if reflect.ValueOf(first.Life).Pointer() != wantLife || reflect.ValueOf(second.Life).Pointer() != wantLife {
		t.Fatal("opponent board life map was reallocated")
	}
}

func TestReserveObservedPrefixChangesOnlyLogCapacity(t *testing.T) {
	e := &rules.Engine{L: events.NewLog(17)}
	e.L.Append(events.Event{Kind: events.Note, Text: "existing"})
	head, length := e.L.Head(), len(e.L.Events)
	h := History{Frames: []Frame{
		{Events: make([]ObservedEvent, 4)},
		{Events: make([]ObservedEvent, 7)},
	}}
	e.L.Reserve(observedPrefixEvents(h))
	if len(e.L.Events) != length || e.L.Head() != head {
		t.Fatalf("reserve changed log content: len=%d/%d head=%s/%s", len(e.L.Events), length, e.L.Head(), head)
	}
	if cap(e.L.Events) < 11 {
		t.Fatalf("reserved capacity = %d, want at least 11", cap(e.L.Events))
	}
}

func TestRejectionShapeIgnoresEarlierMatchingIdentities(t *testing.T) {
	for _, kind := range []events.Kind{events.PutOnStack, events.Note} {
		got := Frame{Identities: []Identity{{ID: 1, Name: "Matching Land", Owner: 1}, {ID: 2, Name: "Different", Owner: 1}}, Events: []ObservedEvent{{Kind: events.MoveZone, Obj: 1, From: state.ZHand, To: state.ZBattlefield}}}
		want := Frame{Identities: []Identity{{ID: 1, Name: "Matching Land", Owner: 1}, {ID: 2, Name: "Expected", Owner: 1}}, Events: append([]ObservedEvent(nil), got.Events...)}
		shape := "other"
		if kind == events.PutOnStack {
			got.Events = append(got.Events, ObservedEvent{Kind: kind, Obj: 2, From: state.ZHand, To: state.ZStack})
			shape = "hand_to_stack"
		} else {
			got.Events = append(got.Events, ObservedEvent{Kind: kind, IDs: []uint32{2}})
		}
		want.Events = append([]ObservedEvent(nil), got.Events...)
		if bucket := rejectionBucket(4, got, want); bucket.Shape != shape {
			t.Fatalf("matching land obscured differing identity: kind=%v got=%s want=%s", kind, bucket.Shape, shape)
		}
	}
}

func TestHandToStackCauseUsesOwnedIdentityHistory(t *testing.T) {
	knownGot := map[uint32]Identity{4: {ID: 4, Name: "Expected", Owner: 1}}
	knownWant := map[uint32]Identity{9: {ID: 9, Name: "Expected", Owner: 1}}
	want := Frame{Events: []ObservedEvent{{Kind: events.PutOnStack, Obj: 9, From: state.ZHand, To: state.ZStack}}}

	if got := handToStackCause(
		Frame{Identities: []Identity{{ID: 5, Name: "Competitor", Owner: 1}}, Events: []ObservedEvent{{Kind: events.PutOnStack, Obj: 5, From: state.ZHand, To: state.ZStack}}},
		want, knownGot, knownWant,
	); got != (HandToStackCauses{PolicyCompetition: 1}) {
		t.Fatalf("competing-name cause = %+v", got)
	}
	if got := handToStackCause(
		Frame{Events: []ObservedEvent{{Kind: events.PutOnStack, Obj: 4, From: state.ZHand, To: state.ZStack}}},
		want, knownGot, knownWant,
	); got != (HandToStackCauses{ObserverReference: 1}) {
		t.Fatalf("same-name reference cause = %+v", got)
	}
	if got := handToStackCause(Frame{Events: []ObservedEvent{{Kind: events.Priority}}}, want, knownGot, knownWant); got != (HandToStackCauses{ObservedCastMissing: 1}) {
		t.Fatalf("missing stack move cause = %+v", got)
	}
	if got := handToStackCause(want, Frame{Events: []ObservedEvent{{Kind: events.Priority}}}, knownWant, knownGot); got != (HandToStackCauses{HypotheticalExtraCast: 1}) {
		t.Fatalf("extra hypothetical stack move cause = %+v", got)
	}
}

func TestStackRejectionContextNormalizesPhaseActionAndConstraint(t *testing.T) {
	knownGot := map[uint32]Identity{5: {ID: 5, Name: "Unexpected", Owner: 1}}
	knownWant := map[uint32]Identity{}
	got := Frame{
		Board:  Board{Step: "main1"},
		Events: []ObservedEvent{{Kind: events.PutOnStack, Obj: 5, From: state.ZHand, To: state.ZStack}},
	}
	want := Frame{
		Board:  Board{Step: "main1"},
		Events: []ObservedEvent{{Kind: events.Priority, Player: 1}},
	}
	wantBucket := StackRejectionContext{Cause: "hypothetical_extra_cast", Step: "main1", ExpectedAction: "pass", Constraint: "no_observed_cast", Count: 1}
	if gotBucket := stackRejectionContext(got, want, knownGot, knownWant, "no_observed_cast"); gotBucket != wantBucket {
		t.Fatalf("context = %+v, want %+v", gotBucket, wantBucket)
	}
}

func TestStackConstraintContextsDistinguishSupportedAndUnguidedEpochs(t *testing.T) {
	h := History{Actor: 0, Frames: []Frame{
		{Events: []ObservedEvent{{Kind: events.Shuffle, Player: 1}}},
		{Identities: []Identity{{ID: 1, Name: "Expected", Owner: 1}}, Events: []ObservedEvent{{Kind: events.PutOnStack, Obj: 1, From: state.ZHand, To: state.ZStack}}},
		{Events: []ObservedEvent{{Kind: events.Shuffle, Player: 1}}},
		{Identities: []Identity{{ID: 2, Name: "Later", Owner: 1}}, Events: []ObservedEvent{{Kind: events.PutOnStack, Obj: 2, From: state.ZHand, To: state.ZStack}}},
	}}
	epochs := map[epochKey]epochConstraints{
		{Player: 1}:             {Deadlines: []deadlineConstraint{{Through: 7, Name: "Expected", Count: 1}}},
		{Player: 1, Ordinal: 1}: {Unguided: []string{"library_mutation"}},
	}
	want := []string{"", "supported", "", "unguided"}
	if got := stackConstraintContexts(h, epochs); !reflect.DeepEqual(got, want) {
		t.Fatalf("contexts = %v, want %v", got, want)
	}
}

func TestSamplerRejectionHistogramCountsAndSorts(t *testing.T) {
	setup, h := samplingHistory(t)
	// A board no replay can reproduce (the observed board with one bit of
	// its digests flipped, as if a life total had read 999).
	h.Frames[3].Board.Sum[0] ^= 1
	h.Frames[3].Board.Stripped[0] ^= 1
	result, err := Sample(setup, h, SampleOptions{Seed: 991, Attempts: 3, Worlds: 1, MaxSubmits: 5000})
	if err != nil {
		t.Fatal(err)
	}
	want := []RejectionBucket{{Frame: 3, Component: "board", Shape: "state", Count: 3}}
	if !reflect.DeepEqual(result.Rejections, want) {
		t.Fatalf("rejections = %+v want %+v", result.Rejections, want)
	}
	// Exercise the actual aggregation path with multiple keys, repeated keys,
	// and ties at each sort level, in deliberately reversed insertion order.
	for _, bucket := range []RejectionBucket{
		{Frame: 3, Component: "identities", Shape: "hand_to_stack"},
		{Frame: 3, Component: "identities", Shape: "hand_to_battlefield"},
		{Frame: 1, Component: "action", Shape: "mismatch"},
		{Frame: 3, Component: "events", Shape: "count"},
		{Frame: 3, Component: "identities", Shape: "hand_to_stack"},
		{Frame: 1, Component: "action", Shape: "mismatch"},
	} {
		addRejection(&result, bucket)
	}
	want = []RejectionBucket{
		{Frame: 1, Component: "action", Shape: "mismatch", Count: 2},
		{Frame: 3, Component: "board", Shape: "state", Count: 3},
		{Frame: 3, Component: "events", Shape: "count", Count: 1},
		{Frame: 3, Component: "identities", Shape: "hand_to_battlefield", Count: 1},
		{Frame: 3, Component: "identities", Shape: "hand_to_stack", Count: 2},
	}
	if !reflect.DeepEqual(result.Rejections, want) {
		t.Fatalf("multi-key aggregation/order = %+v want %+v", result.Rejections, want)
	}
	var reversed SampleResult
	for i := len(want) - 1; i >= 0; i-- {
		for n := 0; n < want[i].Count; n++ {
			addRejection(&reversed, want[i])
		}
	}
	if !reflect.DeepEqual(result.Rejections, reversed.Rejections) {
		t.Fatal("histogram depends on insertion order")
	}
}

func samplingHistory(t *testing.T) (PublicGame, History) {
	t.Helper()
	e := observationEngine(t, 17)
	c := NewCollector(0)
	h := History{Actor: 0, Answers: make(map[int][]Action)}
	r := rand.New(rand.NewPCG(7, 19))
	start := 0
	for i := 0; i < 50; i++ {
		frame, err := c.Capture(e, e.L.Events[start:])
		if err != nil {
			t.Fatal(err)
		}
		h.Frames = append(h.Frames, frame)
		d := e.Pending()
		if i >= 20 && d.Player == 0 {
			break
		}
		in := botpolicy.Decide(botpolicy.BoardFromGame(e.G, e, d.Player), d, r)
		if d.Player == 0 {
			h.Answers[i], err = c.Actions(d, in)
			if err != nil {
				t.Fatal(err)
			}
		}
		start = len(e.L.Events)
		if err := e.SubmitHypothetical(in); err != nil {
			t.Fatal(err)
		}
	}
	if h.Frames[len(h.Frames)-1].Decision == nil {
		t.Fatal("fixture failed to select actor root")
	}
	// This is the fixture's PUBLIC deck composition, not an extraction of its
	// hidden zones. All cards are identical in the synthetic experiment.
	definition, ds := cards.ParseBytes("public-fixture", []byte("Name:Mountain\nTypes:Basic Land Mountain\nOracle:Fixture.\n"))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	definition.Link()
	for _, f := range definition.Faces {
		f.ApplyIntrinsics()
	}
	deck := make([]*cards.Card, 20)
	for i := range deck {
		deck[i] = definition
	}
	return PublicGame{Names: []string{"a", "b"}, Decks: [][]*cards.Card{deck, deck}}, h
}

func TestSamplerClassifiesContradictoryAndUnsupportedInput(t *testing.T) {
	setup, h := samplingHistory(t)
	h.Frames[0].Identities[0].Name = "Not In Public Deck"
	_, err := Sample(setup, h, SampleOptions{Seed: 1, Attempts: 4, Worlds: 4, MaxSubmits: 5000})
	var failure *Failure
	if !errors.As(err, &failure) || failure.Kind != "contradictory" {
		t.Fatalf("contradiction: %v", err)
	}
	setup, h = samplingHistory(t)
	setup.Decks[0] = setup.Decks[0][:6]
	_, err = Sample(setup, h, SampleOptions{Seed: 1, Attempts: 4, Worlds: 4, MaxSubmits: 5000})
	if !errors.As(err, &failure) || failure.Kind != "unsupported" {
		t.Fatalf("unsupported: %v", err)
	}
}

func TestSamplerReconstructsFullPrefixAndReplaysWorlds(t *testing.T) {
	setup, h := samplingHistory(t)
	result, err := Sample(setup, h, SampleOptions{Seed: 991, Attempts: 8, Worlds: 4, MaxSubmits: 5000})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Worlds) != 4 || result.Accepted != 8 || result.ESS < 7.99 {
		t.Fatalf("sampling result: %+v", result)
	}
	for _, w := range result.Worlds {
		e, err := rules.NewHypothetical(w.Config, w.Engine.ChanceTranscript())
		if err != nil {
			t.Fatal(err)
		}
		if err := e.AdvanceHypothetical(); err != nil {
			t.Fatal(err)
		}
		for _, in := range w.Engine.L.Intents {
			if err := e.SubmitHypothetical(in); err != nil {
				t.Fatal(err)
			}
		}
		if e.L.Head() != w.Engine.L.Head() || e.RNGDraws() != w.Engine.RNGDraws() {
			t.Fatal("selected world replay failed")
		}
	}
}

func TestSamplerRejectsChangedPrefixAndHonorsSubmitBudget(t *testing.T) {
	setup, h := samplingHistory(t)
	// A board no replay can reproduce (the observed board with one bit of
	// its digests flipped, as if a life total had read 999).
	h.Frames[3].Board.Sum[0] ^= 1
	h.Frames[3].Board.Stripped[0] ^= 1
	result, err := Sample(setup, h, SampleOptions{Seed: 991, Attempts: 8, Worlds: 4, MaxSubmits: 5000})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Worlds) != 0 || result.Accepted != 0 || result.PrefixRejected != 8 {
		t.Fatalf("accepted impossible prefix: %+v", result)
	}
	_, h = samplingHistory(t)
	result, err = Sample(setup, h, SampleOptions{Seed: 991, Attempts: 8, Worlds: 4, MaxSubmits: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Worlds) != 0 || result.BudgetExhausted != 8 || result.Submits != 8 {
		t.Fatalf("ignored work cap: %+v", result)
	}
}

func TestSamplerUsesOnlyHistoryAndExplicitSeed(t *testing.T) {
	setup, h := samplingHistory(t)
	before, _ := json.Marshal(h)
	opts := SampleOptions{Seed: 991, Attempts: 8, Worlds: 4, MaxSubmits: 5000}
	a, err := Sample(setup, h, opts)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Sample(setup, h, opts)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(h)
	if string(before) != string(after) {
		t.Fatal("sampler modified history")
	}
	if len(a.Worlds) != len(b.Worlds) || len(a.Worlds) != 4 {
		t.Fatal("nondeterministic or empty sampling")
	}
	for i := range a.Worlds {
		if a.Worlds[i].Engine.L.Head() != b.Worlds[i].Engine.L.Head() || !reflect.DeepEqual(a.Worlds[i].Engine.ChanceTranscript(), b.Worlds[i].Engine.ChanceTranscript()) {
			t.Fatal("sample not deterministic")
		}
	}
}

func TestResampledDuplicateWorldsAreIndependent(t *testing.T) {
	setup, h := samplingHistory(t)
	for seed := uint64(0); seed < 20; seed++ {
		result, err := Sample(setup, h, SampleOptions{Seed: seed, Attempts: 4, Worlds: 4, MaxSubmits: 5000})
		if err != nil {
			t.Fatal(err)
		}
		if result.Duplicates == 0 {
			continue
		}
		for i, a := range result.Worlds {
			for j, b := range result.Worlds {
				if i != j && (a.Engine == b.Engine || a.Observer == b.Observer) {
					t.Fatal("resampled worlds alias")
				}
			}
		}
		return
	}
	t.Fatal("fixture never selected a duplicate")
}
