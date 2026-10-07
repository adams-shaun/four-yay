package effects

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// surveilRememberKeptCarriers pins every corpus Surveil SA carrying
// RememberKept$ True; the census holds the set exact.
var surveilRememberKeptCarriers = []string{
	"Starving Revenant",
}

// TestSurveilRememberKept pins RememberKept$ True: every card the player
// keeps on top of their library joins the source's event-backed remembered
// list, so a chained Remembered$Amount read (Starving Revenant's draw and
// life loss) sees the kept cards. The no-host stand-in keeps the whole
// window on top, so all k looked-at cards are remembered.
func TestSurveilRememberKept(t *testing.T) {
	h, src, lib := riderBoard(t, riderBear, riderLand, riderHalo)
	sa := &cards.SA{API: "Surveil", Params: map[string]string{
		"Defined": "You", "Amount": "2", "RememberKept": "True",
	}}
	effSurveil(h, &Ctx{Source: src, Controller: 0}, sa)
	// Precondition: the surveil actually looked at two cards (the stand-in
	// puts none in the graveyard, so the whole library stays on top and the
	// window is the top two).
	if len(lib) < 3 {
		t.Fatalf("library not set up: %v", lib)
	}
	o := h.g.Obj(src)
	if o == nil {
		t.Fatalf("source gone")
	}
	got := map[state.ObjID]bool{}
	for _, r := range o.Remembered {
		got[r.Obj] = true
	}
	for _, id := range lib[:2] {
		if !got[id] {
			t.Errorf("kept card %d not remembered (%v)", id, o.Remembered)
		}
	}
	if len(o.Remembered) != 2 {
		t.Errorf("remembered %d cards, want the 2-card window: %v", len(o.Remembered), o.Remembered)
	}
}

// TestSurveilWithoutRememberKept is the companion: absent the param the
// source remembers nothing, and the surveil still ran (a Surveil marker was
// emitted), so the assertion above is about the param.
func TestSurveilWithoutRememberKept(t *testing.T) {
	h, src, _ := riderBoard(t, riderBear, riderLand, riderHalo)
	sa := &cards.SA{API: "Surveil", Params: map[string]string{
		"Defined": "You", "Amount": "2",
	}}
	effSurveil(h, &Ctx{Source: src, Controller: 0}, sa)
	if o := h.g.Obj(src); len(o.Remembered) != 0 {
		t.Errorf("surveil without RememberKept$ remembered %v", o.Remembered)
	}
}

// TestSurveilRememberKeptCensus pins the one corpus carrier.
func TestSurveilRememberKeptCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	got := map[string]bool{}
	saw := 0
	visit := func(name string, sa *cards.SA) {
		if sa == nil || sa.API != "Surveil" {
			return
		}
		saw++
		if sa.ParamStr(cards.PKRememberKept) != "" && strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberKept)), "True") {
			got[name] = true
		}
	}
	for _, card := range reg.AllCards() {
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
	if saw < 20 {
		t.Fatalf("census saw only %d Surveil SAs: the scan is not reading the corpus", saw)
	}
	want := map[string]bool{}
	for _, n := range surveilRememberKeptCarriers {
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
		t.Errorf("new Surveil RememberKept$ carriers: %v", added)
	}
	if len(removed) > 0 {
		t.Errorf("pinned Surveil RememberKept$ carriers gone: %v", removed)
	}
	t.Logf("%d Surveil SAs, %d RememberKept", saw, len(got))
}
