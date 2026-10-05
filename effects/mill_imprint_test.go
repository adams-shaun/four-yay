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
// card to the source's persistent Imprinted list through events.Imprint, so
// a chained `Defined$ Imprinted` / `IsImprinted` sub reads exactly the
// milled cards (OTJ Patient Naturalist).
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
	imp := map[state.ObjID]bool{}
	for _, id := range o.Imprinted {
		imp[id] = true
	}
	for _, id := range lib {
		if !imp[id] {
			t.Errorf("milled card %d not imprinted (got %v)", id, o.Imprinted)
		}
	}
	seen := 0
	for _, e := range h.log {
		if e.Kind == events.Imprint && e.Obj == src {
			seen++
		}
	}
	if seen != 3 {
		t.Errorf("Imprint events = %d, want one per milled card (3)", seen)
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
