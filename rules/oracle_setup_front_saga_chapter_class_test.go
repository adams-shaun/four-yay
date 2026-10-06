package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// The front-face Saga chapter I test in oracle_setup_front_saga_chapter_test.go
// covers a bounce chapter (Summon: Leviathan). This covers the second chapter
// shape: a chapter that draws and pays life (Summon: Anima's chapter I, "You
// draw a card and you lose 1 life"). XMage's addCard(Zone.BATTLEFIELD) adds the
// lore counter and resolves chapter I before the setup checkpoint, so the
// setup snapshot has p0 at 19 life with one drawn card -- the same
// setupPlacementDropsTrigger fix, exercised through a different effect.
func TestOracleSetupFrontFaceSagaDrawChapterFires(t *testing.T) {
	testutil.CorpusRegistry(t)
	res, _ := runFixtureScenario(t, `{"name":"anima","setup":{"p0":{"battlefield":["Summon: Anima"],"library":["Wastes","Wastes","Wastes","Wastes","Wastes"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[]}`)
	snap := res.Snapshots[0]

	// Precondition: the Saga is on the battlefield at its front face with a
	// lore counter, so the assertions below are about a chapter I that
	// resolved and cannot pass on a placement that never happened.
	if setupPermCount(snap, "Summon: Anima") != 1 {
		t.Fatalf("Summon: Anima is not on the battlefield at setup: %+v", snap.Permanents)
	}
	if p0life := snap.Players[0].Life; p0life != 19 {
		t.Errorf("p0 life = %d at setup, want 19 (chapter I lost 1 life)", p0life)
	}
	if h := snap.Players[0].Hand; len(h) != 1 {
		t.Errorf("p0 hand = %v at setup, want one drawn card (chapter I drew)", h)
	}
}
