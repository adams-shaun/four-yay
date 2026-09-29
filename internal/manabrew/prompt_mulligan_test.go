package manabrew

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

// smallHandView is a view whose viewer (player 0) holds a two-card hand:
// an Island (o1) and a Bear (o3).
func smallHandView() view.View {
	return view.View{Viewer: 0, Turn: 1, Step: "mulligan", Active: 0, Priority: 0, Players: []view.PlayerView{
		{ID: 0, Name: "Alice", Life: 20, HandSize: 2, LibrarySize: 53, Hand: []view.CardView{
			{ID: 1, Name: "Island", Printing: view.Printing{Name: "Island"}, Types: "Basic Land — Island", Owner: 0, Controller: 0},
			{ID: 3, Name: "Bear", Printing: view.Printing{Name: "Bear"}, Types: "Creature — Bear", Owner: 0, Controller: 0},
		}},
		{ID: 1, Name: "Bob", Life: 20, HandSize: 0, LibrarySize: 55},
	}}
}

// TestMulliganKeepPromptAndResponse covers the London keep/mulligan ask:
// the hand ids come from the deciding player's own hand projection, and the
// keep/mulligan answers map to the two options.
func TestMulliganKeepPromptAndResponse(t *testing.T) {
	tr := New("table", 2, nil)
	v := smallHandView()
	// Precondition: player 0's own hand projection is what the prompt reads.
	if len(v.Players[0].Hand) != 2 {
		t.Fatalf("fixture must carry a two-card hand, got %d", len(v.Players[0].Hand))
	}
	d := &decision.Decision{Seq: 30, Player: 0, Kind: decision.KMulligan, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "keep", Label: "keep"},
		{Index: 1, Kind: "mulligan", Label: "mulligan"},
	}}
	msg := pendingMustBuild(t, d, v)
	in, ok := msg.Input.Value.(mb.MulliganInput)
	if !ok {
		t.Fatalf("input type = %s, want mulligan", msg.Input.Value.PromptType())
	}
	if !reflect.DeepEqual(in.HandCardIDs, []string{"o1", "o3"}) {
		t.Fatalf("handCardIds = %v, want the seat's hand in hand order", in.HandCardIDs)
	}
	if in.MulliganCount != 0 {
		t.Fatalf("mulliganCount = %d, want the documented 0 (not derivable from the decision)", in.MulliganCount)
	}
	p := pendingFor(d, v)
	o := tr.TranslateResponse(respFor(p, mb.MulliganDecision{Keep: true}), p, 0)
	if !reflect.DeepEqual(mustIntent(t, o).Choices, []int{0}) {
		t.Fatal("keep must answer the keep option")
	}
	o = tr.TranslateResponse(respFor(p, mb.MulliganDecision{Keep: false}), p, 0)
	if !reflect.DeepEqual(mustIntent(t, o).Choices, []int{1}) {
		t.Fatal("mulligan must answer the mulligan option")
	}
	// A seat whose allowance is spent keeps only: a mulligan answer is
	// invalidShape, not an engine intent.
	spent := &decision.Decision{Seq: 30, Player: 0, Kind: decision.KMulligan, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "keep", Label: "keep"},
	}}
	sp := pendingFor(spent, v)
	wantErrCode(t, tr.TranslateResponse(respFor(sp, mb.MulliganDecision{Keep: false}), sp, 0), mb.CodeInvalidShape)
}

// TestMulliganPutBackPromptAndResponse covers the bottoming ask: the
// parallel hand ids and card projections, the count, and the response
// mapping including the count fence.
func TestMulliganPutBackPromptAndResponse(t *testing.T) {
	tr := New("table", 2, nil)
	v := smallHandView()
	d := &decision.Decision{Seq: 31, Player: 0, Kind: decision.KMulligan, Min: 2, Max: 2, Prompt: "Bottom two", Options: []decision.Option{
		{Index: 0, Kind: "bottom", Label: "Island", Obj: 1},
		{Index: 1, Kind: "bottom", Label: "Bear", Obj: 3},
	}}
	msg := pendingMustBuild(t, d, v)
	in, ok := msg.Input.Value.(mb.MulliganPutBackInput)
	if !ok {
		t.Fatalf("input type = %s, want mulliganPutBack", msg.Input.Value.PromptType())
	}
	if !reflect.DeepEqual(in.HandCardIDs, []string{"o1", "o3"}) {
		t.Fatalf("handCardIds = %v", in.HandCardIDs)
	}
	// Precondition: the parallel list must carry the hand's own names.
	if len(in.Cards) != 2 || in.Cards[0].Identity.Name == "" || in.Cards[1].Identity.Name == "" {
		t.Fatalf("cards = %#v, want the hand's projected identities", in.Cards)
	}
	if in.Cards[0].Identity.Name == in.Cards[1].Identity.Name {
		t.Fatalf("fixture hand must name distinct cards, got %q twice", in.Cards[0].Identity.Name)
	}
	if in.Count != 2 {
		t.Fatalf("count = %d, want the decision's Min", in.Count)
	}
	p := pendingFor(d, v)
	o := tr.TranslateResponse(respFor(p, mb.MulliganPutBackDecision{CardIDs: []string{"o1", "o3"}}), p, 0)
	if !reflect.DeepEqual(mustIntent(t, o).Choices, []int{0, 1}) {
		t.Fatal("bottoming both cards must map to both options")
	}
	// A card not in the hand is invalidShape.
	wantErrCode(t, tr.TranslateResponse(respFor(p, mb.MulliganPutBackDecision{CardIDs: []string{"o1", "o9"}}), p, 0), mb.CodeInvalidShape)
	// The wrong count is Decision.Validate's text riding invalidShape.
	wantErrCode(t, tr.TranslateResponse(respFor(p, mb.MulliganPutBackDecision{CardIDs: []string{"o1"}}), p, 0), mb.CodeInvalidShape)
}
