package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestGalianBeastTriggerKeepsSnapshotSlot pins that a back-face "when this
// dies" trigger keeps its slot on the oracle snapshot's stack entry after the
// CR 712.8a reset put the card front face up. The reset means the source no
// longer shows the face whose line matched, so a slot lookup against the live
// face alone comes back empty and the level-B generator reads the trigger as
// never having fired (compliance/oraclegen/templates
// TestFaceOneTemplatesServeBackFaceSetup/Vincent_Valentine/trigger#1.0).
func TestGalianBeastTriggerKeepsSnapshotSlot(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Vincent Valentine")
	if !ok || len(c.Faces) < 2 || len(c.Faces[1].Triggers) == 0 {
		t.Fatalf("Vincent Valentine has no back-face trigger in the corpus")
	}
	back := c.Faces[1]

	sc := `{"name":"galian-slot","setup":{"p0":{"battlefield":["Vincent Valentine"],"back_face":["Vincent Valentine"],"hand":["Murder"]},"p1":{"battlefield":["Grizzly Bears"]}},` +
		`"steps":[{"op":"cast","seat":0,"card":"p0:Murder","mana":"CBB","targets":["p0:` + back.Name + `"]},{"op":"pass","seat":0},{"op":"pass","seat":1}]}`
	res, err := RunOracleScenarioJSON(reg, []byte(sc))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// Precondition: the dies trigger is actually on the stack in a snapshot
	// (Murder resolved, the triggered ability waits), sourced from the card.
	found := false
	for _, s := range res.Snapshots {
		for _, e := range s.Stack {
			if e.Kind != "ability" {
				continue
			}
			found = true
			if e.Trigger != "0" {
				t.Errorf("back-face dies trigger stack entry has slot %q, want \"0\"", e.Trigger)
			}
		}
	}
	if !found {
		t.Fatalf("no snapshot shows the dies trigger on the stack; the scenario never reached it")
	}
	// The card must be front face up in the graveyard while its back-face
	// trigger waits on the stack: that is the state the slot lookup runs in.
	gaveUp := false
	for _, s := range res.Snapshots {
		for _, p := range s.Players {
			if p.Seat != 0 {
				continue
			}
			for _, name := range p.Graveyard {
				if name == c.Faces[0].Name {
					gaveUp = true
				}
			}
		}
	}
	if !gaveUp {
		t.Errorf("no snapshot shows %s front face up in the graveyard; the reset never ran", c.Faces[0].Name)
	}
}
