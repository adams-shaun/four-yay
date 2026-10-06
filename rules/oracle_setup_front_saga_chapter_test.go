package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// XMage's addCard(Zone.BATTLEFIELD) adds a front-face Saga's lore counter and
// chapter I fires and resolves before the setup checkpoint. Summon: Leviathan's
// chapter I returns Grizzly Bears to its owner's hand, so p1's hand holds the
// Bears at setup (the Summon: Leviathan/trigger#0.0/v1 XMage verdict) and the
// Bears are off the battlefield.
func TestOracleSetupFrontFaceSagaFiresChapterI(t *testing.T) {
	testutil.CorpusRegistry(t)
	res, _ := runFixtureScenario(t, `{"name":"leviathan","setup":{"p0":{"battlefield":["Summon: Leviathan"]},"p1":{"battlefield":["Grizzly Bears"],"hand":["Shock"]}},"steps":[]}`)
	snap := res.Snapshots[0]
	if setupPermCount(snap, "Summon: Leviathan") != 1 {
		t.Fatalf("Summon: Leviathan is not on the battlefield at setup: %+v", snap.Permanents)
	}
	if h := snap.Players[1].Hand; len(h) != 2 || h[0] != "Grizzly Bears" || h[1] != "Shock" {
		t.Errorf("p1 hand = %v at setup, want [Grizzly Bears Shock] (chapter I bounced the Bears)", h)
	}
	if n := setupPermCount(snap, "Grizzly Bears"); n != 0 {
		t.Errorf("Grizzly Bears still on the battlefield at setup (%d); chapter I did not resolve", n)
	}
}
