package rules

// kw:Paradigm (the sos set's Lesson mechanic) — implementation tests.
//
// The mechanic is a bare keyword, K:Paradigm, on five Secrets of Strixhaven
// Lesson spells. Its behaviour is entirely rules-side: a resolved Paradigm
// spell exiles itself (rules/stack.go's spellRestZone), and while it sits in
// exile its owner may cast a free copy at the beginning of each of their first
// main phases (rules/mayplay.go's mayPlayGrantScoped via
// rules/paradigm.go's paradigmMayPlay). Neither read special-cases a card: both
// key off hasParadigm, so all five carriers behave identically.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// paradigmCarriers are the five corpus cards that print K:Paradigm, measured
// at the FORGE_REF pin (grep -rlE '^K:Paradigm$' .cards/cardsfolder).
var paradigmCarriers = []string{
	"Restoration Seminar",
	"Echocasting Symposium",
	"Decorum Dissertation",
	"Improvisation Capstone",
	"Germination Practicum",
}

// TestParadigmCensus names every carrier and proves the keyword is registered
// supported: kw:Paradigm must not appear in cards.Registry.Unsupported for any
// of the five. It also scans the corpus tree so a sixth carrier cannot appear
// unnoticed (the file set is asserted exactly).
func TestParadigmCensus(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["kw:Paradigm"] {
		t.Fatal("kw:Paradigm is not registered supported (missing effects.RegisterNonAPI call)")
	}
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	for _, name := range paradigmCarriers {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus missing Paradigm carrier %q", name)
		}
		if !c.Faces[0].HasKeyword("Paradigm") {
			t.Errorf("%s: compiled face does not carry the Paradigm keyword", name)
		}
		for _, miss := range reg.Unsupported(c, supported) {
			if miss == "kw:Paradigm" {
				t.Errorf("%s: kw:Paradigm still reported unsupported", name)
			}
		}
	}

	// Corpus-tree census: exactly these files carry the keyword line.
	dir := filepath.Join("..", ".cards", "cardsfolder")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("no corpus at %s: %v", dir, err)
	}
	got := map[string]bool{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || filepath.Ext(path) != ".txt" {
			return walkErr
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if bytes.Contains(b, []byte("K:Paradigm")) {
			got[filepath.Base(path)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("corpus walk: %v", err)
	}
	if len(got) != len(paradigmCarriers) {
		t.Errorf("corpus carries K:Paradigm in %d files (%v), want %d", len(got), got, len(paradigmCarriers))
	}
}

// paradigmCopyOffer returns the index of the free may-play "cast" option for
// the exiled Paradigm card id in the pending priority decision, or -1.
func paradigmCopyOffer(t *testing.T, e *Engine, id state.ObjID) int {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected a priority decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == id && o.Kind == "cast" && o.Mode == "mayplay" {
			return o.Index
		}
	}
	return -1
}

// TestParadigmOffersFreeCopyFromExile drives the whole mechanic on Restoration
// Seminar: the first cast resolves and exiles the card. The copy is cast "at
// the beginning of each of your first main phases" -- ONE cast per first main
// phase, and not in the main phase the original resolved in (that phase's
// beginning has already passed). At the owner's NEXT first main phase the
// ordinary may-play offer makes the exiled card a FREE castable copy; casting
// it without any mana in the pool resolves a second time and returns the card
// to exile, so the lone physical card stands in for the rules text's "cast a
// copy ... while the card remains there". After that cast the offer is gone
// for the rest of the phase: an offer that survived its own resolution was an
// unbounded free loop (cardfuzz fuzz-1003: Restoration Seminar recurring an
// Aura with nothing to enchant, Germination Practicum, Improvisation Capstone
// and Echocasting Symposium each livelocked or hit the intent cap).
func TestParadigmOffersFreeCopyFromExile(t *testing.T) {
	t.Parallel()
	// Two graveyard nonland permanents so the original cast AND the free copy
	// each have a legal target; the copy targets the second one.
	e, cfg, _ := altCostEngine(t, 1911, []string{"Restoration Seminar"}, []string{sosRelicSrc, sosBearSrc}, nil)
	relic := addToGraveyard(t, e, 0, sosRelicSrc)
	bear := addToGraveyard(t, e, 0, sosBearSrc)
	seminar := findAndMoveToHand(t, e, 0, "Restoration Seminar")

	// Precondition: both targets really sit in the graveyard before the cast.
	if o := e.G.Obj(relic); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: relic is %v, want graveyard", o)
	}
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: bear is %v, want graveyard", o)
	}

	addMana(t, e, 0, "WWWWWWW") // {5}{W}{W}
	submitChoices(t, e, castOptionFor(t, e, seminar).Index)
	answerTargetAsk(t, e, []state.ObjID{relic})
	passUntilStackEmpty(t, e, 20)

	// The first resolution exiled the spell instead of binning it.
	if o := e.G.Obj(seminar); o == nil || o.Zone != state.ZExile {
		t.Fatalf("paradigm: seminar zone = %v, want exile", o)
	}
	if o := e.G.Obj(relic); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: the relic was not returned to the battlefield")
	}
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
		t.Fatal("precondition: the bear left the graveyard before the copy could target it")
	}
	if e.G.Step != state.StepMain1 || e.G.Active != 0 {
		t.Fatalf("precondition: still in seat 0's first main phase, got turn %d seat %d step %s", e.G.Turn, e.G.Active, e.G.Step)
	}

	// The beginning of THIS first main phase has passed: no copy this turn.
	if idx := paradigmCopyOffer(t, e, seminar); idx >= 0 {
		t.Fatalf("paradigm: free copy offered in the same main phase the original resolved in (option %d)", idx)
	}

	// Seat 0's next first main phase (a two-player game: turn 3).
	driveToStep(t, e, 3, 0, state.StepMain1)
	copyIdx := paradigmCopyOffer(t, e, seminar)
	if copyIdx < 0 {
		t.Fatalf("no free may-play copy offer for the exiled Paradigm spell at the next first main phase; pending %+v", e.Pending())
	}

	// Cast it with an EMPTY mana pool: only the MayPlayWithoutManaCost$ free
	// exemption makes that legal, so a non-free offer would fail here.
	submitChoices(t, e, copyIdx)
	answerTargetAsk(t, e, []state.ObjID{bear})
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the free copy did not resolve: bear is %v, want battlefield", o)
	}
	if o := e.G.Obj(seminar); o == nil || o.Zone != state.ZExile {
		t.Fatalf("after the free copy resolved: seminar zone = %v, want exile", o)
	}

	// One copy per first main phase: the offer must not come back.
	if e.G.Step != state.StepMain1 || e.G.Active != 0 {
		t.Fatalf("precondition: still in seat 0's first main phase, got turn %d seat %d step %s", e.G.Turn, e.G.Active, e.G.Step)
	}
	if idx := paradigmCopyOffer(t, e, seminar); idx >= 0 {
		t.Fatalf("paradigm: a second free copy offered in the same first main phase (option %d) -- an unbounded free-cast loop", idx)
	}
	replayCheck(t, e, cfg)
}
