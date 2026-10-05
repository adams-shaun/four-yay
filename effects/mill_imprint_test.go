package effects

import (
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// millImprintCarriers pins every corpus card whose Mill SA carries
// Imprint$ True. Forge's MillEffect records each milled card in the source's
// persistent imprintedCards association, so a chained
// `ChangeZone ... ChangeType$ ...IsImprinted` sub finds exactly the milled
// cards (OTJ Patient Naturalist's "put a land card from among the milled
// cards into your hand").
var millImprintCarriers = []string{
	"Ballad of the Black Flag",
	"Blanchwood Prowler",
	"Fallaji Archaeologist",
	"Patient Naturalist",
	"Ravenous Gigamole",
	"Sivriss, Nightmare Speaker",
}

// TestMillImprintCensus walks every corpus Mill SA and asserts the set of
// cards carrying Imprint$ True equals millImprintCarriers exactly: a new
// carrier fails loudly, and a stale entry fails too.
func TestMillImprintCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	got := map[string]bool{}
	sawMill := 0
	visit := func(name string, sa *cards.SA) {
		if sa == nil || sa.API != "Mill" {
			return
		}
		sawMill++
		if isTrue(sa.ParamStr(cards.PKImprint)) {
			got[name] = true
		}
	}
	for _, card := range reg.Cards {
		for _, f := range card.Faces {
			for _, sa := range f.Abilities {
				visit(f.Name, sa)
			}
			names := make([]string, 0, len(f.SVars))
			for n := range f.SVars {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				visit(f.Name, cards.ResolveSVar(f.SVars, n))
			}
		}
	}
	if sawMill < 100 {
		t.Fatalf("census saw only %d Mill SAs: the scan is not reading the corpus", sawMill)
	}
	want := map[string]bool{}
	for _, n := range millImprintCarriers {
		want[n] = true
	}
	var added, removed []string
	for n := range got {
		if !want[n] {
			added = append(added, n)
		}
	}
	for n := range want {
		if !got[n] {
			removed = append(removed, n)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	if len(added) > 0 {
		t.Errorf("new Mill Imprint$ True carriers: %v", added)
	}
	if len(removed) > 0 {
		t.Errorf("pinned Mill Imprint$ carriers gone (stale entries): %v", removed)
	}
	t.Logf("%d Mill SAs, %d Imprint$ carriers", sawMill, len(got))
}

// TestMillImprintRecordsMilled pins that Imprint$ True joins every milled
// card to the source's persistent SeekFound list through events.Imprint with
// the non-zone-gated "seek-found" tag, so a chained
// `Defined$ Imprinted` / `IsImprinted` sub reads exactly the milled cards
// (OTJ Patient Naturalist) even though they landed in the graveyard. The
// ordinary Imprinted list is deliberately exile-gated (CR 607.2a), so a mill
// that recorded there would satisfy `o.Imprinted` yet never match.
func TestMillImprintRecordsMilled(t *testing.T) {
	h, src, lib := riderBoard(t, riderBear, riderLand, riderHalo)
	sa := &cards.SA{API: "Mill", Params: map[string]string{
		"Defined": "You", "NumCards": "3", "Imprint": "True",
	}}
	effMill(h, &Ctx{Source: src, Controller: 0}, sa)
	// Precondition: the mill actually moved every card, so the imprint
	// assertions below cannot pass vacuously on an empty mill.
	for _, id := range lib {
		if o := h.g.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("card %d not milled (zone %v); log=%+v", id, o, h.log)
		}
	}
	o := h.g.Obj(src)
	if o == nil {
		t.Fatalf("source gone")
	}
	for _, id := range lib {
		found := false
		for _, got := range o.SeekFound {
			if got == id {
				found = true
			}
		}
		if !found {
			t.Errorf("milled card %d not in source SeekFound (got %v)", id, o.SeekFound)
		}
	}
	// The exile-gated ordinary list must NOT carry the graveyard cards: a
	// regression back to a bare events.Imprint would make the chained
	// ChangeType$ ...IsImprinted lookup fail while this list looked right.
	if len(o.Imprinted) != 0 {
		t.Errorf("milled cards recorded in exile-gated Imprinted: %v", o.Imprinted)
	}
	seen := 0
	for _, e := range h.log {
		if e.Kind == events.Imprint && e.Obj == src {
			seen++
			if e.Text != "seek-found" {
				t.Errorf("mill Imprint event tag = %q, want seek-found", e.Text)
			}
		}
	}
	if seen != 3 {
		t.Errorf("Imprint events = %d, want one per milled card (3)", seen)
	}
}

// TestMillImprintFeedsChainedChangeZone drives the whole OTJ Patient Naturalist
// shape: `Mill NumCards$ 3 Imprint$ True` then
// `ChangeZone Origin$ Graveyard,Exile Destination$ Hand ChangeType$
// Land.YouOwn+IsImprinted`. The milled land must reach the hand. Without the
// non-zone-gated seek-found channel the chained filter matches nothing and the
// land stays in the graveyard -- the Player Naturalist by a wrong Treasure.
func TestMillImprintFeedsChainedChangeZone(t *testing.T) {
	// Top of library: a land among two nonlands, exactly "a land from among
	// the milled cards".
	h, src, ids := riderBoard(t, riderBear, riderLand, riderBear)
	land := ids[1]
	mill := &cards.SA{API: "Mill", Params: map[string]string{
		"Defined": "You", "NumCards": "3", "Imprint": "True",
	}}
	effMill(h, &Ctx{Source: src, Controller: 0}, mill)
	// Precondition: the land is milled and in the graveyard before the
	// chained pickup, else the assertion below is vacuous.
	if o := h.g.Obj(land); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("land not milled to graveyard (zone %v); log=%+v", o, h.log)
	}
	change := &cards.SA{API: "ChangeZone", Params: map[string]string{
		"Origin": "Graveyard,Exile", "Destination": "Hand",
		"ChangeType": "Land.YouOwn+IsImprinted", "Hidden": "True", "Mandatory": "True",
	}}
	effChangeZone(h, &Ctx{Source: src, Controller: 0}, change)
	if o := h.g.Obj(land); o == nil || o.Zone != state.ZHand {
		t.Fatalf("milled land did not reach hand (zone %v); log=%+v", o, h.log)
	}
	// The two nonlands must stay milled: the filter takes only the land.
	for _, id := range []state.ObjID{ids[0], ids[2]} {
		if o := h.g.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Errorf("nonland %d left the graveyard (zone %v)", id, o)
		}
	}
}

// TestMillWithoutImprintRecordsNothing is the "nothing happens" companion:
// a Mill with no Imprint$ must not touch the source's Imprinted list, and
// the mill's own handler must still run (the cards are in the graveyard and
// no unimplemented note was emitted), so the assertion above is about the
// param and not about a Mill that silently did nothing.
func TestMillWithoutImprintRecordsNothing(t *testing.T) {
	h, src, lib := riderBoard(t, riderBear, riderLand, riderHalo)
	sa := &cards.SA{API: "Mill", Params: map[string]string{
		"Defined": "You", "NumCards": "3",
	}}
	effMill(h, &Ctx{Source: src, Controller: 0}, sa)
	for _, id := range lib {
		if o := h.g.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("card %d not milled; the handler did not run", id)
		}
	}
	if o := h.g.Obj(src); len(o.Imprinted) != 0 {
		t.Errorf("Mill without Imprint$ recorded %v", o.Imprinted)
	}
}
