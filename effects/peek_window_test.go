package effects

import (
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const (
	peekJace = "Name:Jace Beleren\nTypes:Legendary Planeswalker Jace\nLoyalty:3\nOracle:x\n"
	peekIsle = "Name:Island\nTypes:Basic Land Island\nOracle:x\n"
	peekBear = "Name:Grizzly Bears\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	peekBolt = "Name:Lightning Bolt\nManaCost:R\nTypes:Instant\nOracle:x\n"
)

// TestPeekAndRevealWindowFiltersWithinWindow pins the peek window: with
// PeekAmount$ 1 the filter sees only the top card, so a creature anywhere
// below a nonmatching top card is NOT revealed (ECL Gathering Stone's
// reported defect). Pre-fix RevealValid$ filtered the whole library and then
// truncated, so the first creature below the window was revealed instead.
func TestPeekAndRevealWindowFiltersWithinWindow(t *testing.T) {
	h, src, lib := riderBoard(t, peekBolt, peekIsle, peekBear)
	sa := &cards.SA{API: "PeekAndReveal", Params: map[string]string{
		"Defined": "You", "PeekAmount": "1", "RevealValid": "Creature",
	}}
	effReveal(h, &Ctx{Source: src, Controller: 0}, sa)
	// Precondition: the deck really holds a creature below the window, so
	// the assertion is about the window and cannot pass vacuously.
	if o := h.g.Obj(lib[2]); o == nil || o.Face() == nil || !matchesCreature(o) {
		t.Fatalf("card below the window is not a creature: %+v", o)
	}
	for _, e := range h.log {
		if e.Kind == events.Note {
			t.Errorf("top card was %s (not a creature) but a reveal landed: %+v", h.g.Obj(lib[0]).Face().Name, e.IDs)
		}
	}
}

// TestPeekAndRevealWindowRevealsMatchingInsideWindow is the companion: a
// matching card inside the window IS revealed, so the test above is about
// the window boundary and not a disabled reveal.
func TestPeekAndRevealWindowRevealsMatchingInsideWindow(t *testing.T) {
	h, src, lib := riderBoard(t, peekJace, peekBear, peekBolt)
	sa := &cards.SA{API: "PeekAndReveal", Params: map[string]string{
		"Defined": "You", "PeekAmount": "2", "RevealValid": "Creature",
	}}
	effReveal(h, &Ctx{Source: src, Controller: 0}, sa)
	// Precondition: the creature sits at position 2 inside the two-card
	// window.
	if o := h.g.Obj(lib[1]); o == nil || o.Face() == nil || o.Face().Name != "Grizzly Bears" {
		t.Fatalf("expected the creature at window position 2: %+v", o)
	}
	found := false
	for _, e := range h.log {
		if e.Kind == events.Note {
			for _, id := range e.IDs {
				if id == lib[1] {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("creature inside the peek window was not revealed; log=%+v", h.log)
	}
}

// TestPeekAndRevealImprintRevealed pins ImprintRevealed$ True: every
// revealed card joins the source's "seek-found" association so a chained
// `Card.IsImprinted` over the LIBRARY finds it (BLB Portent of Calamity's
// RepeatTypesFrom$). The cards are still in the library, so the ordinary
// (exile-only) Imprinted list would not reach them.
func TestPeekAndRevealImprintRevealed(t *testing.T) {
	h, src, lib := riderBoard(t, peekBolt, peekIsle)
	sa := &cards.SA{API: "PeekAndReveal", Params: map[string]string{
		"Defined": "You", "PeekAmount": "2", "ImprintRevealed": "True",
	}}
	effReveal(h, &Ctx{Source: src, Controller: 0}, sa)
	// Precondition: both cards were revealed publicly, so there is an
	// imprint to inspect.
	revealed := false
	for _, e := range h.log {
		if e.Kind == events.Note && len(e.IDs) == 2 {
			revealed = true
		}
	}
	if !revealed {
		t.Fatalf("no two-card reveal happened; log=%+v", h.log)
	}
	seek := map[state.ObjID]bool{}
	if o := h.g.Obj(src); o != nil {
		for _, id := range o.SeekFound {
			seek[id] = true
		}
	}
	for _, id := range lib {
		if !seek[id] {
			t.Errorf("revealed card %d not in source SeekFound (%v)", id, h.g.Obj(src).SeekFound)
		}
	}
	imp := 0
	for _, e := range h.log {
		if e.Kind == events.Imprint && e.Text == "seek-found" && e.Obj == src {
			imp++
		}
	}
	if imp != 1 {
		t.Errorf("seek-found Imprint events = %d, want 1", imp)
	}
}

// peekImprintCarriers pins the PeekAndReveal cards that carry
// ImprintRevealed$ True; the census below holds the set exact.
var peekImprintCarriers = []string{
	"Atraxa, Grand Unifier",
	"Djinn of Wishes",
	"Gandalf, Westward Voyager",
	"Omnath, Locus of All",
	"Portent of Calamity",
	"Priority Boarding",
	"Sin Prodder",
	"Truth or Tale",
}

// TestPeekAndRevealCensus walks every corpus Reveal-family SA and pins the
// set of PeekAndReveal carriers that combine a window (PeekAmount$) with a
// filter (RevealValid$/RevealType$/RevealAllValid$), plus the
// ImprintRevealed$ carriers. A new carrier of either class fails loudly.
func TestPeekAndRevealCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	filtered := map[string]bool{}
	imprinted := map[string]bool{}
	sawPeek := 0
	visit := func(name string, sa *cards.SA) {
		if sa == nil || sa.API != "PeekAndReveal" {
			return
		}
		sawPeek++
		if sa.ParamStr(cards.PKRevealValid) != "" || sa.ParamStr(cards.PKRevealType) != "" ||
			sa.ParamStr(cards.PKRevealAllValid) != "" {
			filtered[name] = true
		}
		if rawParamText(sa, "ImprintRevealed").Text != "" {
			imprinted[name] = true
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
	if sawPeek < 100 {
		t.Fatalf("census saw only %d PeekAndReveal SAs: the scan is not reading the corpus", sawPeek)
	}
	if len(filtered) < 20 {
		t.Fatalf("filtered PeekAndReveal carriers = %d, expected the kinship family", len(filtered))
	}
	want := map[string]bool{}
	for _, n := range peekImprintCarriers {
		want[n] = true
	}
	var added, removed []string
	for n := range imprinted {
		if !want[n] {
			added = append(added, n)
		}
	}
	for n := range want {
		if !imprinted[n] {
			removed = append(removed, n)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	if len(added) > 0 {
		t.Errorf("new PeekAndReveal ImprintRevealed$ carriers: %v", added)
	}
	if len(removed) > 0 {
		t.Errorf("pinned PeekAndReveal ImprintRevealed$ carriers gone: %v", removed)
	}
	t.Logf("%d PeekAndReveal SAs, %d filtered, %d ImprintRevealed", sawPeek, len(filtered), len(imprinted))
}

// matchesCreature is the tiny type read the precondition uses.
func matchesCreature(o *state.Object) bool {
	return o.Face() != nil && peekHasType(o.Face().Types, "Creature")
}

func peekHasType(ts []string, want string) bool {
	for _, x := range ts {
		if x == want {
			return true
		}
	}
	return false
}
