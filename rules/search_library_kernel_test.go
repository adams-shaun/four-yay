package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestEvolvingWildsSearchPosesHiddenChoose: activating Evolving Wilds pays
// its sacrifice cost and poses the hidden-library search as a 0..1 KChoose
// offering exactly the basic lands (restored from the legacy-only
// TestEvolvingWildsSearchPosesHiddenChooseAndSuspends; the suspension
// assertion is gone with the protocol).
func TestEvolvingWildsSearchPosesHiddenChoose(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Evolving Wilds")
	wilds := searchMoveByName(t, e, "Evolving Wilds", state.ZBattlefield)
	want := basicLibraryIDs(e)
	d := activateSearch(t, e, wilds)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("pending = %+v, want a search KChoose", d)
	}
	if d.Min != 0 || d.Max != 1 {
		t.Fatalf("Min/Max = %d/%d, want 0/1", d.Min, d.Max)
	}
	if !slices.Equal(optionIDs(d), want) {
		t.Fatalf("option ids = %v, want exactly basic lands %v", optionIDs(d), want)
	}
	if e.G.Obj(wilds).Zone != state.ZGraveyard {
		t.Fatalf("Evolving Wilds zone = %s, want graveyard (the sacrifice cost was paid)", e.G.Obj(wilds).Zone)
	}
}
