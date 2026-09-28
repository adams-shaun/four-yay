package rules

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestSetAudit_ecl_Brigid_TransformTriggerFires(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	id := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Brigid, Clachan's Heart"))
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.FaceIdx != 0 {
		t.Fatalf("precondition: Brigid = %+v; want front face on battlefield", o)
	}

	// Put Brigid on the back face first. Transforming back into the front
	// face is the event that its ValidCard$ Card.Self trigger observes.
	e.emit(events.Event{Kind: events.FlipFace, Obj: id, Amount: 1, Text: "Transformed"})
	if got := e.G.Obj(id).FaceIdx; got != 1 {
		t.Fatalf("setup transform: face index = %d, want back face 1", got)
	}

	// This is the marker emitted by the real transform effect after applying
	// the face change. Its source is still on the battlefield.
	e.emit(events.Event{Kind: events.FlipFace, Obj: id, Amount: 0, Text: "Transformed"})
	if got := e.G.Obj(id).FaceIdx; got != 0 {
		t.Fatalf("transform precondition: face index = %d, want front face 0", got)
	}
	requireOneEventTrigger(t, e, "Brigid transform")
}

func TestTransformedTriggerCorpusCarriers(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	var names []string
	for _, c := range reg.Cards {
		if c == nil {
			continue
		}
		found := false
		for _, f := range c.Faces {
			for _, tr := range f.Triggers {
				if tr.Mode == "Transformed" {
					found = true
				}
			}
		}
		if found {
			names = append(names, c.Faces[0].Name)
		}
	}
	sort.Strings(names)
	if len(names) != 30 {
		t.Fatalf("Mode$ Transformed corpus carriers = %d, want 30: %s", len(names), strings.Join(names, "; "))
	}
	if !containsString(names, "Brigid, Clachan's Heart") {
		t.Fatalf("Brigid missing from Transformed carriers: %s", strings.Join(names, "; "))
	}
	t.Logf("Mode$ Transformed carriers (%d): %s", len(names), strings.Join(names, "; "))
}
