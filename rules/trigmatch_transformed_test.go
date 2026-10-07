package rules

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestSetAudit_ecl_Brigid_TransformTriggerFires(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	brigid := mustCorpusCard(t, reg, "Brigid, Clachan's Heart")
	if reg.Tokens["gw_1_1_kithkin"] == nil {
		t.Fatal("precondition: Kithkin token script missing")
	}
	e := New(seatZeroStart(Config{Seed: 1, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{append([]*cards.Card{brigid}, mountainDeck(t, 39)...), mountainDeck(t, 40)},
		Tokens: reg.Tokens}))
	id := moveByName(t, e, 0, "Brigid, Clachan's Heart", state.ZBattlefield)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.FaceIdx != 0 {
		t.Fatalf("precondition: Brigid = %+v; want front face on battlefield", o)
	}
	kithkins := func() int {
		n := 0
		for _, ev := range e.L.Events {
			if ev.Kind == events.TokenCreate && ev.Text == "gw_1_1_kithkin" {
				n++
			}
		}
		return n
	}
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("entry queued %d triggers, want one", len(e.pendingTriggers))
	}
	zallDrain(t, e)
	if got := kithkins(); got != 1 {
		t.Fatalf("entry Kithkin TokenCreate = %d, want 1", got)
	}

	// Drive the real SVar on each face, not a fabricated FlipFace. The back
	// face does not carry the trigger; transforming INTO the front does.
	for _, want := range []uint8{1, 0} {
		o = e.G.Obj(id)
		sa := cards.ResolveSVar(o.Face().SVars, "TrigTransform")
		if sa == nil || sa.Params["Mode"] != "Transform" {
			t.Fatalf("precondition: face %d TrigTransform = %+v", o.FaceIdx, sa)
		}
		e.resolveAbility(id, 0, nil, sa, o.Face().SVars)
		if got := e.G.Obj(id).FaceIdx; got != want {
			t.Fatalf("transform face = %d, want %d", got, want)
		}
		if e.G.Obj(id).Zone != state.ZBattlefield {
			t.Fatal("transform source left battlefield")
		}
		if want == 1 && len(e.pendingTriggers) != 0 {
			t.Fatalf("back face queued %d triggers, want zero", len(e.pendingTriggers))
		}
	}
	requireOneEventTrigger(t, e, "Brigid transforms into front face")
	zallDrain(t, e)
	if got := kithkins(); got != 2 {
		t.Fatalf("Kithkin TokenCreate events after the transform = %d, want 2", got)
	}
}

func TestTransformedTriggerCorpusCarriers(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	var names []string
	for _, c := range reg.AllCards() {
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
