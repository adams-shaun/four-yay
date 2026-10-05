package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestKrenkosBuzzcrusherSearchRequiresADestroyedLand pins the real card's
// ChangeZone sub-ability: a RepeatEach player binding alone is not a destroyed
// land, while a remembered destroyed land makes its controller the searcher.
func TestKrenkosBuzzcrusherSearchRequiresADestroyedLand(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	buzzcrusher := mustCorpusCard(t, reg, "Krenko's Buzzcrusher")
	destroy := cards.ResolveSVar(buzzcrusher.Faces[0].SVars, "DBDestroy")
	search := cards.ResolveSVar(buzzcrusher.Faces[0].SVars, "DBSearch")
	if destroy == nil || destroy.API != "Destroy" {
		t.Fatalf("precondition: DBDestroy = %+v, want the real Destroy ability", destroy)
	}
	if search == nil || search.API != "ChangeZone" {
		t.Fatalf("precondition: DBSearch = %+v, want the real ChangeZone ability", search)
	}
	land := card(t, "Name:Test Nonbasic Land\nTypes:Land\nOracle:x\n")

	t.Run("no destroyed land", func(t *testing.T) {
		e, _ := linkBoard(t, reg, []string{"Krenko's Buzzcrusher"}, nil)
		ctx := &effects.Ctx{Source: e.G.Zone(state.ZBattlefield, 0)[0], Controller: 0,
			Remembered: []state.Target{{Player: 1, IsPlayer: true}}}
		e.probe(func() { effects.Resolve(e, ctx, search) })
		if d := e.Pending(); d != nil {
			t.Fatalf("search was offered without a destroyed land: %+v", d)
		}
		for _, ev := range e.L.Events {
			if ev.Kind == events.Note && ev.Text == "unimplemented API ChangeZone" {
				t.Fatal("precondition: ChangeZone handler did not run")
			}
		}
	})

	t.Run("destroyed land controller searches", func(t *testing.T) {
		e, _ := linkBoard(t, reg, []string{"Krenko's Buzzcrusher"}, nil)
		landID := onBoardCard(t, e, 1, land)
		ctx := &effects.Ctx{Source: e.G.Zone(state.ZBattlefield, 0)[0], Controller: 0,
			Remembered:    []state.Target{{Player: 1, IsPlayer: true}},
			RepeatSubject: state.Target{Obj: landID}}
		e.probe(func() { effects.Resolve(e, ctx, destroy) })
		if e.G.Obj(landID).Zone != state.ZGraveyard {
			t.Fatalf("precondition: DBDestroy left test land in %v, want graveyard", e.G.Obj(landID).Zone)
		}
		found := false
		for _, remembered := range ctx.Remembered {
			if remembered.Obj == landID {
				found = true
			}
		}
		if !found {
			t.Fatal("precondition: DBDestroy did not remember the destroyed land")
		}
		e.probe(func() { effects.Resolve(e, ctx, search) })
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			t.Fatalf("destroyed land did not offer the optional search: %+v", d)
		}
		if d.Player != 1 {
			t.Fatalf("search decision player = %d, want destroyed-land controller 1", d.Player)
		}
	})
}
