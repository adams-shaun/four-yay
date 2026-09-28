package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestGarrukVeiledButcherOnlyReplacesBattlefieldDeaths distinguishes a
// creature card milled from an actual creature permanent dying. Garruk's
// replacement is specifically a "would die" effect, not a from-anywhere
// graveyard replacement.
func TestGarrukVeiledButcherOnlyReplacesBattlefieldDeaths(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	garruk := lookup(t, reg, "Garruk, Veiled Butcher")
	bear := lookup(t, reg, "Grizzly Bears")
	e, _ := corpusEngineCfg(t, reg, []*cards.Card{garruk}, []*cards.Card{bear, bear})

	garrukID := moveByName(t, e, 0, "Garruk, Veiled Butcher", state.ZBattlefield)
	if o := e.G.Obj(garrukID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Garruk is not on the battlefield: %+v", o)
	}

	var milled state.ObjID
	for _, id := range e.G.Zone(state.ZLibrary, 1) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
			milled = id
			break
		}
	}
	if milled == 0 {
		t.Fatal("opponent's Grizzly Bears was not in library before milling")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: milled, From: state.ZLibrary, To: state.ZGraveyard})
	if o := e.G.Obj(milled); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("opponent's creature card from library did not reach graveyard: %+v", o)
	}

	dying := moveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	if o := e.G.Obj(dying); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("opponent's creature permanent was not on battlefield before death: %+v", o)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: dying, From: state.ZBattlefield, To: state.ZGraveyard})
	if o := e.G.Obj(dying); o == nil || o.Zone != state.ZExile {
		t.Fatalf("battlefield death was not replaced with exile: %+v", o)
	}
}
