package manabrew

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

// arrangeView is a two-card library-top projection for the arrange prompt
// tests: option 0 names card o1 (Island), option 1 names card o2 (Bear).
func arrangeView() view.View {
	return view.View{Viewer: 0, Turn: 1, Step: "main1", Active: 0, Priority: 0, Players: []view.PlayerView{
		{ID: 0, Name: "Alice", Life: 20, LibrarySize: 30},
		{ID: 1, Name: "Bob", Life: 20},
	}}
}

// TestArrangeScry covers the split shape (Option.Kind "bottom"): a Scry-2's
// pile A/pile B ask maps to scry{zones:[libraryTop, libraryBottom]}, and the
// response's two zone lists resolve back into Choices (pile A, in the
// player's order) and, only when Restable, Rest (pile B's order).
func TestArrangeScry(t *testing.T) {
	v := arrangeView()
	d := &decision.Decision{Seq: 5, Player: 0, Kind: decision.KArrange, Min: 0, Max: 2, Prompt: "Scry 2",
		Options: []decision.Option{
			{Index: 0, Kind: "bottom", Label: "Island", Obj: 1},
			{Index: 1, Kind: "bottom", Label: "Bear", Obj: 2},
		}}
	tr := New("table", 1, nil)

	msg, err := tr.promptArrange(d, &v)
	if err != nil {
		t.Fatalf("promptArrange: %v", err)
	}
	in, ok := msg.Input.Value.(mb.ScryInput)
	if !ok {
		t.Fatalf("promptArrange (split) did not build a scry input: %#v", msg.Input.Value)
	}
	if len(in.Zones) != 2 || in.Zones[0] != mb.DestinationLibraryTop || in.Zones[1] != mb.DestinationLibraryBottom {
		t.Fatalf("scry zones = %v, want [libraryTop libraryBottom]", in.Zones)
	}
	if len(in.Cards) != 2 {
		t.Fatalf("scry cards = %d, want 2", len(in.Cards))
	}

	// Keep the Island on top (pile A), send the Bear to the bottom (pile B),
	// with no Rest (the ask is not Restable): Choices = [0], Rest empty.
	pending := &Pending{Prompt: msg, Decision: d}
	outcome := tr.parseArrangeScry(mb.ScryDecision{ZoneCardIDs: [][]string{{cardID(1)}, {cardID(2)}}}, pending)
	if outcome.Err != nil {
		t.Fatalf("parseArrangeScry: %s: %s", outcome.Err.Code, outcome.Err.Message)
	}
	if outcome.Intent == nil {
		t.Fatal("parseArrangeScry returned no intent")
	}
	if got := outcome.Intent.Choices; len(got) != 1 || got[0] != 0 {
		t.Fatalf("Choices = %v, want [0] (Island stays on top)", got)
	}
	if len(outcome.Intent.Rest) != 0 {
		t.Fatalf("Rest = %v, want empty: the ask is not Restable, so pile B's order is the legacy default", outcome.Intent.Rest)
	}

	// A Restable ask carries the player's pile-B order through as Rest.
	d.Restable = true
	pending = &Pending{Prompt: msg, Decision: d}
	outcome = tr.parseArrangeScry(mb.ScryDecision{ZoneCardIDs: [][]string{{cardID(1)}, {cardID(2)}}}, pending)
	if outcome.Err != nil {
		t.Fatalf("parseArrangeScry (restable): %s: %s", outcome.Err.Code, outcome.Err.Message)
	}
	if got := outcome.Intent.Rest; len(got) != 1 || got[0] != 1 {
		t.Fatalf("Restable Rest = %v, want [1]", got)
	}
}

// TestArrangeReorder covers the single-list shape (Option.Kind "" / "top",
// a full RearrangeTopOfLibrary/Ponder reorder): the answer's OrderedIDs map
// DIRECTLY onto Choices, position for position -- no flip, unlike
// KTriggerOrder, because gorge's own pile-A-index-0-is-closest-to-the-top
// convention already agrees with ManaBrew's "first id ends on top".
func TestArrangeReorder(t *testing.T) {
	v := arrangeView()
	d := &decision.Decision{Seq: 6, Player: 0, Kind: decision.KArrange, Min: 2, Max: 2, Prompt: "Rearrange the top two cards",
		Options: []decision.Option{
			{Index: 0, Kind: "", Label: "Island", Obj: 1},
			{Index: 1, Kind: "", Label: "Bear", Obj: 2},
		}}
	tr := New("table", 1, nil)

	msg, err := tr.promptArrange(d, &v)
	if err != nil {
		t.Fatalf("promptArrange: %v", err)
	}
	in, ok := msg.Input.Value.(mb.ReorderInput)
	if !ok {
		t.Fatalf("promptArrange (full order) did not build a reorder input: %#v", msg.Input.Value)
	}
	if len(in.Items) != 2 {
		t.Fatalf("reorder items = %d, want 2", len(in.Items))
	}
	if in.Items[0].ID != actionID(0) || in.Items[1].ID != actionID(1) {
		t.Fatalf("reorder items are not in Option order: %+v", in.Items)
	}

	// Put the Bear on top: OrderedIDs = [item for option 1, item for option 0].
	pending := &Pending{Prompt: msg, Decision: d}
	outcome := tr.parseArrangeReorder(mb.ReorderDecision{OrderedIDs: []string{actionID(1), actionID(0)}}, pending)
	if outcome.Err != nil {
		t.Fatalf("parseArrangeReorder: %s: %s", outcome.Err.Code, outcome.Err.Message)
	}
	if outcome.Intent == nil {
		t.Fatal("parseArrangeReorder returned no intent")
	}
	if got := outcome.Intent.Choices; len(got) != 2 || got[0] != 1 || got[1] != 0 {
		t.Fatalf("Choices = %v, want [1 0] (Bear on top, Island second): no reversal", got)
	}
}

// TestArrangeHideawayBottomIsReorder covers the all-to-bottom deviation
// (Ruling J4/J5): "hideaway_bottom" is a single ordered list even though
// every offered card's destination is the bottom, not the top.
func TestArrangeHideawayBottomIsReorder(t *testing.T) {
	v := arrangeView()
	d := &decision.Decision{Seq: 7, Player: 0, Kind: decision.KArrange, Min: 2, Max: 2, Prompt: "Order cards going to the bottom",
		Options: []decision.Option{
			{Index: 0, Kind: "hideaway_bottom", Label: "Island", Obj: 1},
			{Index: 1, Kind: "hideaway_bottom", Label: "Bear", Obj: 2},
		}}
	tr := New("table", 1, nil)
	msg, err := tr.promptArrange(d, &v)
	if err != nil {
		t.Fatalf("promptArrange: %v", err)
	}
	if _, ok := msg.Input.Value.(mb.ReorderInput); !ok {
		t.Fatalf("hideaway_bottom must map to reorder, got %#v", msg.Input.Value)
	}
}
