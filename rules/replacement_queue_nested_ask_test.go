package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A resolving ability that returns several lands at once, each of which meets
// a non-commuting entry competition (Horizon Explorer's enters-untapped body
// against the land's own enters-tapped body), parks one CR 616.1 order ask per
// land on the replChoices queue and suspends its resolution on e.resume. When
// the body chosen for one land ASKS (a shock land's "pay 2 life or it enters
// tapped" UnlessCost) while later lands' competitions are still queued, that
// nested Engine.Ask overwrote e.resume with its own frame; once the nested
// answer completed, the suspended resolution's frame was gone, and the next
// order answer called resumeResolution(nil) -- the botbench panic
// (cavalry-charge vs pro-shaper, seed 7149: Lumra, Bellow of the Woods
// returning Overgrown Tomb, Mirrorpool, Spire Garden, ... under Horizon
// Explorer). The suspended resolution must survive the nested ask and resume
// exactly once, after the last queued competition is answered.

// returnLandsSrc is a Lumra-shaped test-local trigger host: whenever its
// controller draws, return every land card from their graveyard to the
// battlefield tapped, then gain 1 life -- the sub-ability that proves the
// suspended chain's continuation runs exactly once.
const returnLandsSrc = "Name:Land Returner\nTypes:Creature\nPT:1/1\n" +
	"T:Mode$ Drawn | ValidCard$ Card.YouCtrl | TriggerZones$ Battlefield | Execute$ TrigReturn | TriggerDescription$ Whenever you draw a card, return all land cards from your graveyard to the battlefield tapped.\n" +
	"SVar:TrigReturn:DB$ ChangeZoneAll | ChangeType$ Land.YouCtrl | Origin$ Graveyard | Destination$ Battlefield | Tapped$ True | SubAbility$ DBGain\n" +
	"SVar:DBGain:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"

// shockLandSrc is the shock-land entry shape: an Updated tap body gated on an
// UnlessCost the entering land's controller is asked about.
const shockLandSrc = "Name:Test Shock\nTypes:Land\n" +
	"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ DBTap | ReplacementResult$ Updated | Description$ As CARDNAME enters, you may pay 2 life. If you don't, it enters tapped.\n" +
	"SVar:DBTap:DB$ Tap | ETB$ True | Defined$ Self | UnlessCost$ PayLife<2> | UnlessPayer$ You\nOracle:x\n"

func TestReplacementQueueSurvivesNestedAskMidResolution(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		lands []string
	}{
		// The nested ask is posed while later competitions are queued.
		{"queued competitions behind the nested ask", []string{shockLandSrc, horizonTaplandSrc, horizonTaplandSrc}},
		// The nested ask is posed by the last (only) competition: the
		// suspended resolution chains behind the nested frame.
		{"nested ask on the last competition", []string{horizonTaplandSrc, shockLandSrc}},
		{"lone competition asks", []string{shockLandSrc}},
	} {
		t.Run(tc.name, func(t *testing.T) { replacementQueueNestedAsk(t, tc.lands) })
	}
}

func replacementQueueNestedAsk(t *testing.T, landSrcs []string) {
	e := layerEngine(t)
	e.pending = nil
	onBoard(t, e, 0, horizonExplorerSrc)
	host := onBoard(t, e, 0, returnLandsSrc)
	var lands []state.ObjID
	for _, src := range landSrcs {
		o := e.G.AddObject(card(t, src), 0)
		o.Zone = state.ZGraveyard
		e.G.SetZone(state.ZGraveyard, 0, append(e.G.Zone(state.ZGraveyard, 0), o.ID))
		lands = append(lands, o.ID)
	}

	drawn := e.G.Zone(state.ZLibrary, 0)[0]
	e.emit(events.Event{Kind: events.Draw, Player: 0, Obj: drawn, From: state.ZLibrary, To: state.ZHand, Secret: true})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Source != host {
		t.Fatalf("stack = %v, want the returner's one trigger", e.G.Stack)
	}
	trig := e.G.Stack[0]
	e.resolveTop()

	orderAsks, modeAsks := 0, 0
	for i := 0; i < 20; i++ {
		d := e.Pending()
		if d == nil || (d.Kind != decision.KReplacement && d.Kind != decision.KModes) {
			break
		}
		if d.Kind == decision.KReplacement {
			orderAsks++
		} else {
			modeAsks++
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatalf("submit %s: %v", d.Kind, err)
		}
	}
	if orderAsks < len(landSrcs) || modeAsks < 1 {
		t.Fatalf("order asks = %d, mode asks = %d; want one order ask per land and the shock land's nested ask", orderAsks, modeAsks)
	}
	for _, id := range lands {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("land %d zone = %+v, want Battlefield", id, o)
		}
	}
	for _, id := range e.G.Stack {
		if id == trig {
			t.Fatal("the suspended trigger is still on the stack after every queued competition was answered")
		}
	}
	if e.resume != nil {
		t.Fatalf("a stale resume frame survived the completed resolution: %+v", e.resume)
	}
	resolves := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Resolve && ev.Obj == trig {
			resolves++
		}
	}
	if resolves != 1 {
		t.Fatalf("trigger resolved %d times, want exactly once", resolves)
	}
	gains := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.LifeChange && ev.Player == 0 && ev.Amount == 1 {
			gains++
		}
	}
	if gains != 1 {
		t.Fatalf("the trigger's GainLife continuation ran %d times, want exactly once", gains)
	}
}
