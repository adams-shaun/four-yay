package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

func plotZoneEngineForTest(t *testing.T, fblthp bool, top *cards.Card) (*Engine, state.ObjID) {
	t.Helper()
	e := handEngine(t)
	if fblthp {
		onBoardCard(t, e, 0, corpusCardByName(t, "Fblthp, Lost on the Range"))
	}
	o := e.G.AddObject(top, 0)
	o.Zone = state.ZLibrary
	e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{o.ID}, e.G.Zone(state.ZLibrary, 0)...))
	e.G.Players[0].Pool[state.MC] = 1
	e.G.Players[0].Pool[state.MU] = 1
	return e, o.ID
}

func TestPlotZoneOffersGrantedPlotFromLibraryTop(t *testing.T) {
	t.Parallel()
	bear := card(t, "Name:Bear\nManaCost:1 U\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, id := plotZoneEngineForTest(t, true, bear)
	if got := e.G.Zone(state.ZLibrary, 0)[0]; got != id {
		t.Fatalf("precondition: library top=%d, want object %d", got, id)
	}
	raw, ok := e.derivedKeywordParamH(id, kwHeadOf("Plot"))
	if !ok || raw != "CardManaCost" {
		t.Fatalf("precondition: derived Plot=%q, %v; want CardManaCost", raw, ok)
	}
	printed := pay.RawBaseCost(asPayer(e), 0, id)
	parsedToken := ParseCost("CardManaCost")
	if reflect.DeepEqual(printed, parsedToken) {
		t.Fatalf("precondition: printed mana cost %+v unexpectedly equals parsed CardManaCost %+v", printed, parsedToken)
	}
	if !plotZoneAllowed(e, id) {
		svs := e.activeStatics("PlotZone")
		matches := false
		if len(svs) > 0 {
			matches = e.matchesSpec(svs[0].ParamStr(cards.PKValidCard), id, e.staticSpecCtx(svs[0]))
		}
		t.Fatalf("precondition: Fblthp PlotZone static did not select the top card: statics=%+v directMatch=%v", svs, matches)
	}
	idx := findMode(e.legalActions(0), "plot")
	if idx < 0 || e.legalActions(0)[idx].Obj != id {
		t.Fatalf("plot option not offered for the top library card: %+v", e.legalActions(0))
	}
	if !reflect.DeepEqual(printed, ParseCost("1 U")) {
		t.Fatalf("plot cost=%+v, want printed {1}{U}", printed)
	}
	castMode(t, e, id, "plot")
	o := e.G.Obj(id)
	if o.Zone != state.ZExile || o.PlottedTurn != e.G.Turn {
		t.Fatalf("plot result zone=%s plottedTurn=%d turn=%d", o.Zone, o.PlottedTurn, e.G.Turn)
	}
	if findMode(e.legalActions(0), "plot_cast") >= 0 {
		t.Fatal("plotted card was castable for free during the turn it was plotted")
	}
	e.beginTurn(0)
	plotDriveToStep(t, e, e.G.Turn, 0, state.StepMain1)
	if e.G.Turn <= o.PlottedTurn || findMode(e.legalActions(0), "plot_cast") < 0 {
		t.Fatalf("plotted card was not castable for free on a later turn: turn=%d plottedTurn=%d options=%+v", e.G.Turn, o.PlottedTurn, e.legalActions(0))
	}
}

func TestPlotZoneDoesNotOfferWithoutMatchingStaticOrForLand(t *testing.T) {
	t.Parallel()
	nonland := card(t, "Name:Bear\nManaCost:1 U\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, id := plotZoneEngineForTest(t, false, nonland)
	if got := e.G.Zone(state.ZLibrary, 0)[0]; got != id {
		t.Fatalf("precondition: library top=%d, want object %d", got, id)
	}
	if findMode(e.legalActions(0), "plot") >= 0 {
		t.Fatal("offered PlotZone action without an active PlotZone static")
	}

	land := card(t, "Name:Island\nManaCost:\nTypes:Basic Land Island\nOracle:x\n")
	e, id = plotZoneEngineForTest(t, true, land)
	if got := e.G.Zone(state.ZLibrary, 0)[0]; got != id {
		t.Fatalf("precondition: library top=%d, want object %d", got, id)
	}
	if plotZoneAllowed(e, id) {
		t.Fatal("precondition: Fblthp ValidCard unexpectedly selected a land")
	}
	if findMode(e.legalActions(0), "plot") >= 0 {
		t.Fatalf("offered PlotZone action for land top card: %+v", e.legalActions(0))
	}
}
