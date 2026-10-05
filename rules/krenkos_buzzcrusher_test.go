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
	search := cards.ResolveSVar(buzzcrusher.Faces[0].SVars, "DBSearch")
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
		e.emit(events.Event{Kind: events.MoveZone, Obj: landID, From: state.ZBattlefield,
			To: state.ZGraveyard, Text: "destroyed"})
		if e.G.Obj(landID).Zone != state.ZGraveyard {
			t.Fatalf("precondition: test land zone = %v, want graveyard", e.G.Obj(landID).Zone)
		}
		ctx := &effects.Ctx{Source: e.G.Zone(state.ZBattlefield, 0)[0], Controller: 0,
			Remembered: []state.Target{{Player: 1, IsPlayer: true}, {Obj: landID}}}
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
