package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// A back-face placement of a Saga CREATURE (Jecht's Braska's Final Aeon,
// Joshua's Phoenix, Warden of Fire) is placed by XMage's addCard with its entry
// lore counter, and chapter I fires and resolves during the setup drive; the
// setup checkpoint holds chapter II pending. Only a back-face NON-Saga's chapter
// trigger is dropped (setupBackFaceDropsChapter); the entry fold queues none
// there, so that arm is a guard with no observable case in the corpus. These pin the two chapter I
// shapes the FIN level-B rows measured (Jecht combat#1.*, Joshua combat#1.*).

// Braska's chapter I: "Each opponent discards a card and you draw a card". p1
// has no hand, so XMage's snapshot is p0 hand [Wastes] (the drawn filler).
func TestOracleSetupBackFaceSagaDrawChapterFires(t *testing.T) {
	testutil.CorpusRegistry(t)
	res, _ := runFixtureScenario(t, `{"name":"jecht","setup":{"p0":{"battlefield":["Jecht, Reluctant Guardian"],"back_face":["Jecht, Reluctant Guardian"],"library":["Wastes","Wastes","Wastes","Wastes","Wastes"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[]}`)
	snap := res.Snapshots[0]

	// Precondition: the placed permanent is the back-face Saga creature, not
	// the front face, with its entry lore counter.
	var braska *OracleSnapPerm
	for i := range snap.Permanents {
		if snap.Permanents[i].Name == "Braska's Final Aeon" {
			braska = &snap.Permanents[i]
		}
	}
	if braska == nil {
		t.Fatalf("Braska's Final Aeon is not on the battlefield at setup: %+v", snap.Permanents)
	}
	if braska.Counters["LORE"] != 2 {
		t.Errorf("Braska's LORE = %d at setup, want 2 (entry counter 1 + phase-begin counter 1; XMage snapshot)", braska.Counters["LORE"])
	}
	if h := snap.Players[0].Hand; len(h) != 1 || h[0] != "Wastes" {
		t.Errorf("p0 hand = %v at setup, want [Wastes] (chapter I drew)", h)
	}
}

// Phoenix's chapter I: "Rising Flames -- 2 damage to each opponent"; the face
// has lifelink, so the controller gains 2: XMage's snapshot is p0 22 / p1 18.
func TestOracleSetupBackFaceSagaDamageChapterFires(t *testing.T) {
	testutil.CorpusRegistry(t)
	res, _ := runFixtureScenario(t, `{"name":"joshua","setup":{"p0":{"battlefield":["Joshua, Phoenix's Dominant"],"back_face":["Joshua, Phoenix's Dominant"],"library":["Wastes","Wastes","Wastes","Wastes","Wastes"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[]}`)
	snap := res.Snapshots[0]

	if setupPermCount(snap, "Phoenix, Warden of Fire") != 1 {
		t.Fatalf("Phoenix, Warden of Fire is not on the battlefield at setup: %+v", snap.Permanents)
	}
	if setupPermCount(snap, "Joshua, Phoenix's Dominant") != 0 {
		t.Fatalf("the front face is on the battlefield at setup: %+v", snap.Permanents)
	}
	if p0, p1 := snap.Players[0].Life, snap.Players[1].Life; p0 != 22 || p1 != 18 {
		t.Errorf("life = p0 %d / p1 %d at setup, want 22 / 18 (chapter I dealt 2, lifelink gained 2)", p0, p1)
	}
}
