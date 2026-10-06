package rules

import "testing"

// A front-face Saga placed by an xmageFixture scenario's setup enters with its
// lore counter and fires chapter I there, as XMage's addCard does: the
// compliance replay measured Summon: Anima's chapter I (p0 at 19 life), Summon: Titan's (five Wastes in p0's graveyard) and
// Summon: Knights of Round's (three Knight tokens) at XMage's setup checkpoint. Only a
// back-face placement drops the chapter trigger
// (TestOracleSetupBackFacePlacementFiresNoChapterTrigger).
func TestOracleSetupFrontFaceSagaFiresChapterI(t *testing.T) {
	lib := `"library":["Wastes","Wastes","Wastes","Wastes","Wastes","Wastes","Wastes","Wastes"]`
	opp := `"p1":{"battlefield":["Grizzly Bears"]}`

	res, _ := runFixtureScenario(t, `{"name":"anima","setup":{"p0":{"battlefield":["Summon: Anima"],`+lib+`},`+opp+`},"steps":[]}`)
	snap := res.Snapshots[0]
	if setupPermCount(snap, "Summon: Anima") != 1 {
		t.Fatalf("Summon: Anima is not on the battlefield at setup: %+v", snap.Permanents)
	}
	if got := snap.Players[0].Life; got != 19 {
		t.Errorf("Summon: Anima: p0 life = %d after setup, want 19 (chapter I did not fire)", got)
	}

	res, _ = runFixtureScenario(t, `{"name":"titan","setup":{"p0":{"battlefield":["Summon: Titan"],`+lib+`},`+opp+`},"steps":[]}`)
	snap = res.Snapshots[0]
	if setupPermCount(snap, "Summon: Titan") != 1 {
		t.Fatalf("Summon: Titan is not on the battlefield at setup: %+v", snap.Permanents)
	}
	if got := len(snap.Players[0].Graveyard); got != 5 {
		t.Errorf("Summon: Titan: p0 graveyard = %v after setup, want 5 milled Wastes (chapter I did not fire)", snap.Players[0].Graveyard)
	}

	res, _ = runFixtureScenario(t, `{"name":"knights","setup":{"p0":{"battlefield":["Summon: Knights of Round"],`+lib+`},`+opp+`},"steps":[]}`)
	snap = res.Snapshots[0]
	tokens := 0
	for _, p := range snap.Permanents {
		if p.Token {
			tokens++
		}
	}
	if tokens != 3 {
		t.Errorf("Summon: Knights of Round: %d tokens after setup, want 3 (chapter I did not fire)", tokens)
	}
}
