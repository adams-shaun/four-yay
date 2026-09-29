package manabrew

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

func TestOptionCardCarriesHiddenCardNames(t *testing.T) {
	v := arrangeView()
	search := &decision.Decision{Seq: 278, Player: 0, Kind: decision.KChoose, Min: 0, Max: 1,
		Prompt: "Search a library: choose up to 1 card(s)", Options: []decision.Option{
			{Index: 0, Kind: "search", Label: "Underground Sea", Obj: 5},
			{Index: 1, Kind: "search", Label: "Island", Obj: 6},
			{Index: 2, Kind: "search", Label: "Swamp", Obj: 1},
			{Index: 3, Kind: "search", Label: "Volcanic Island", Obj: 4},
		}}
	msg := pendingMustBuild(t, search, v)
	in, ok := msg.Input.Value.(mb.ChooseCardsInput)
	if !ok {
		t.Fatalf("search input = %T, want ChooseCardsInput", msg.Input.Value)
	}
	if len(v.Players[0].Battlefield) != 0 || v.Players[0].LibraryTop != nil {
		t.Fatal("search setup unexpectedly exposes library cards in the view")
	}
	if len(in.Cards) != len(search.Options) {
		t.Fatalf("search cards = %d, want %d", len(in.Cards), len(search.Options))
	}
	for i, card := range in.Cards {
		if got, want := card.Identity.Name, search.Options[i].Label; got != want {
			t.Errorf("search card %d name = %q, want option label %q", i, got, want)
		}
	}

	scry := &decision.Decision{Seq: 5, Player: 0, Kind: decision.KArrange, Min: 0, Max: 2, Prompt: "Scry 2",
		Options: []decision.Option{{Index: 0, Kind: "bottom", Label: "Island", Obj: 41}, {Index: 1, Kind: "bottom", Label: "Bear", Obj: 42}}}
	scryMsg, err := New("table", 1, nil).promptArrange(scry, &v)
	if err != nil {
		t.Fatalf("promptArrange: %v", err)
	}
	scryInput, ok := scryMsg.Input.Value.(mb.ScryInput)
	if !ok {
		t.Fatalf("scry input = %T, want ScryInput", scryMsg.Input.Value)
	}
	if len(scryInput.Cards) != len(scry.Options) {
		t.Fatalf("scry cards = %d, want %d", len(scryInput.Cards), len(scry.Options))
	}
	for i, card := range scryInput.Cards {
		if got, want := card.Identity.Name, scry.Options[i].Label; got != want {
			t.Errorf("scry card %d name = %q, want option label %q", i, got, want)
		}
	}

	// The visible hand object exists in precisely the zone findCard reads.
	visible := view.CardView{ID: 99, Name: "view name", Printing: view.Printing{Name: "View Name"}, Types: "Land"}
	v.Players[0].Hand = []view.CardView{visible}
	visibleOption := decision.Option{Index: 0, Kind: "search", Label: "Different option label", Obj: state.ObjID(99)}
	if len(v.Players[0].Hand) != 1 || v.Players[0].Hand[0].ID != visibleOption.Obj || visibleOption.Label == v.Players[0].Hand[0].Printing.Name {
		t.Fatal("visible-card control setup must put a differently named object in the viewer's hand")
	}
	got := New("table", 1, nil).optionCard(&v, visibleOption)
	if got.Identity.Name != "View Name" {
		t.Fatalf("visible card name = %q, want view name to win", got.Identity.Name)
	}

	discard := decision.Option{Index: 0, Kind: "discard", Label: "Discard Grizzly Bears", Obj: 100}
	if got := New("table", 1, nil).optionCard(&v, discard); got.Identity.Name != "Grizzly Bears" {
		t.Fatalf("discard fallback name = %q, want bare Grizzly Bears", got.Identity.Name)
	}
}
