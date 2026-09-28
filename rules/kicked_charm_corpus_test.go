package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// kickedCorpusModeAsk drives the REAL corpus Inscription of Abundance through
// the real cast flow -- priority, the cast option (its kicked/unkicked Mode is
// CR 601.2b's announced additional cost), then castModeAsk's mode
// announcement -- and returns the posed KModes decision. A Grizzly Bears on
// seat 0's battlefield makes the "+1/+1 counters" and "fights" modes
// targetable, so all three modes are legal and the bounds are the card's own
// MinCharmNum$/CharmNum$ (0..3 kicked, 1..1 unkicked).
func kickedCorpusModeAsk(t *testing.T, kicked bool) *decision.Decision {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	inscription := lookup(t, reg, "Inscription of Abundance")
	bears := lookup(t, reg, "Grizzly Bears")

	e, _ := corpusEngineCfg(t, reg,
		[]*cards.Card{inscription, bears}, nil)
	// Precondition: the real card carries the Kicker keyword and the
	// Count$Kicked-backed bounds the ticket names, so a corpus-pin move that
	// reshapes it fails here rather than silently passing.
	f := e.G.Obj(moveByName(t, e, 0, "Inscription of Abundance", state.ZHand)).Face()
	if f == nil || !f.HasKeyword("Kicker") {
		t.Fatalf("precondition: corpus Inscription of Abundance lacks Kicker: %+v", f)
	}
	if got := f.SVars["X"]; got != "Count$Kicked.0.1" {
		t.Fatalf("precondition: SVar:X = %q, want Count$Kicked.0.1", got)
	}
	if got := f.SVars["Y"]; got != "Count$Kicked.3.1" {
		t.Fatalf("precondition: SVar:Y = %q, want Count$Kicked.3.1", got)
	}
	id := moveByName(t, e, 0, "Inscription of Abundance", state.ZHand)
	moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	addMana(t, e, 0, "GGGGG")
	want := ""
	if kicked {
		want = "kicked"
	}
	idx := -1
	for _, o := range castOptions(t, e) {
		if o.Obj == id && o.Mode == want {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Inscription of Abundance with mode %q: %+v", want, castOptions(t, e))
	}
	submitChoices(t, e, idx)
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("kicked=%v: want the cast-time KModes ask, got %+v", kicked, d)
	}
	if d.ResumeKind != "cast_modes" {
		t.Fatalf("kicked=%v: mode ask ResumeKind = %q, want cast_modes", kicked, d.ResumeKind)
	}
	return d
}

// TestInscriptionOfAbundanceCorpusModeBounds pins the real corpus card the
// ticket names: kicked offers 0..3 modes (Count$Kicked.0.1 / Count$Kicked.3.1),
// unkicked exactly one. This is the corpus-backed counterpart to
// TestKickedCharmModeBoundsInTheRealCast's inline shape.
func TestInscriptionOfAbundanceCorpusModeBounds(t *testing.T) {
	t.Parallel()
	if d := kickedCorpusModeAsk(t, true); d.Min != 0 || d.Max != 3 {
		t.Fatalf("kicked mode bounds = %d..%d, want 0..3", d.Min, d.Max)
	}
	if d := kickedCorpusModeAsk(t, false); d.Min != 1 || d.Max != 1 {
		t.Fatalf("unkicked mode bounds = %d..%d, want 1..1", d.Min, d.Max)
	}
}
