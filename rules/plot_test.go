package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// findMode returns the index of the option carrying mode in opts, or -1.
func findMode(opts []decision.Option, mode string) int {
	for i, o := range opts {
		if o.Mode == mode {
			return i
		}
	}
	return -1
}

// plotDriveToStep passes priority (answering nothing else) until turn/active
// /step is reached, the way driveToStep does but tolerating the moments where
// the engine has not yet materialised its next priority decision.
func plotDriveToStep(t *testing.T, e *Engine, turn int32, active state.PlayerID, step state.Step) {
	t.Helper()
	for i := 0; i < 4000; i++ {
		if e.G.Turn == turn && e.G.Active == active && e.G.Step == step {
			return
		}
		if e.G.Over {
			t.Fatalf("game ended before reaching turn %d seat %d step %s", turn, active, step)
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("non-priority decision %+v while driving to turn %d seat %d step %s",
				d, turn, active, step)
		}
		pass := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				pass = o.Index
			}
		}
		if pass < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pass}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	t.Fatalf("did not reach turn %d seat %d step %s within the pass budget", turn, active, step)
}

// plotEngine puts Lock and Load in seat 0's hand at main phase with the pool
// the plot cost needs, and asserts the plot offer is on the table (the
// precondition every later assertion depends on).
func plotEngine(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	e := handEngine(t, corpusAlternativeCard(t, "Lock and Load"))
	lal := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 3
	e.G.Players[0].Pool[state.MU] = 1
	if findMode(e.legalActions(0), "plot") < 0 {
		t.Fatalf("Lock and Load was never offered a plot action: %+v", e.legalActions(0))
	}
	return e, lal
}

// TestPlotLockAndLoadPlotsThenCastsFreeOnALaterTurn is the brief's scenario
// end to end: Lock and Load ({2}{U}, K:Plot:3 U) is plotted on turn 1 -- the
// ACTION pays {3}{U}, exiles it and records the plotted designation with NO
// counters (CR 701.34b's restriction is "on a later turn", not Suspend's
// counter count) -- is NOT castable for free on the turn it was plotted, and
// is castable for free from turn 2's main phase, resolving to the graveyard.
func TestPlotLockAndLoadPlotsThenCastsFreeOnALaterTurn(t *testing.T) {
	t.Parallel()
	e, lal := plotEngine(t)
	castMode(t, e, lal, "plot")
	o := e.G.Obj(lal)
	// CR 701.34a: the action paid {3}{U}, exiled the card face up, and gave
	// it the plotted designation stamped with the CURRENT turn. There are NO
	// TIME counters -- Plot is not Suspend.
	if o.Zone != state.ZExile || o.Counter("TIME") != 0 || o.PlottedTurn != e.G.Turn || e.G.Turn < 1 {
		t.Fatalf("plot did not exile with the plotted designation and no counters: zone=%s time=%d plottedTurn=%d turn=%d",
			o.Zone, o.Counter("TIME"), o.PlottedTurn, e.G.Turn)
	}
	if p := e.G.Players[0].Pool; p[state.MC] != 0 || p[state.MU] != 0 {
		t.Fatalf("plot cost was not paid: pool %+v", p)
	}
	// CR 701.34b: "on a later turn". The permission must NOT exist on the
	// turn the card was plotted, even at main phase with an empty stack.
	plotDriveToStep(t, e, e.G.Turn, 0, state.StepMain1)
	if findMode(e.legalActions(0), "plot_cast") >= 0 {
		t.Fatal("a plotted card was offered its free cast on the turn it was plotted (CR 701.34b requires a later turn)")
	}
	// Advance to the next turn: now the standing sorcery-timing permission is
	// on the table.
	e.beginTurn(0)
	nextTurn := e.G.Turn
	plotDriveToStep(t, e, nextTurn, 0, state.StepMain1)
	if e.G.Turn <= o.PlottedTurn {
		t.Fatalf("precondition: expected a later turn, turn=%d plottedTurn=%d", e.G.Turn, o.PlottedTurn)
	}
	idx := findMode(e.legalActions(0), "plot_cast")
	if idx < 0 {
		t.Fatalf("plotted card on a later turn was never offered its free cast: %+v", e.legalActions(0))
	}
	// The permission holds even after a priority round passes it: it is
	// re-offered every priority round (it is not a one-shot ask).
	pass := -1
	for _, opt := range e.Pending().Options {
		if opt.Kind == "pass" {
			pass = opt.Index
		}
	}
	if err := e.Submit(decision.Intent{Seq: e.Pending().Seq, Player: 0, Choices: []int{pass}}); err != nil {
		t.Fatalf("submit pass: %v", err)
	}
	e.Advance()
	if findMode(e.legalActions(0), "plot_cast") < 0 {
		t.Fatal("the plotted cast permission did not survive a passed priority round")
	}
	// The cast is free: the pool is still empty, and the cast still begins.
	castMode(t, e, lal, "plot_cast")
	if o := e.G.Obj(lal); o.Zone != state.ZStack {
		t.Fatalf("accepted plot_cast left the card in %s, want stack", o.Zone)
	}
	if p := e.G.Players[0].Pool; p[state.MC] != 0 || p[state.MU] != 0 {
		t.Fatalf("the plotted cast paid mana: pool %+v", p)
	}
	// CR 701.34c: leaving exile for the stack ends the plotted designation.
	if got := e.G.Obj(lal).PlottedTurn; got != 0 {
		t.Fatalf("the plotted designation survived the card leaving exile: PlottedTurn=%d", got)
	}
	before := len(e.G.Zone(state.ZHand, 0))
	e.resolveTop()
	if o := e.G.Obj(lal); o.Zone != state.ZGraveyard {
		t.Fatalf("resolved plotted Lock and Load in %s, want graveyard", o.Zone)
	}
	// The spell's own SA resolved: it drew exactly one more card than the
	// pre-resolution hand held (the bare !CastSaSource exclusion counts the
	// plot cast itself out, so the X draw is 0).
	if got := len(e.G.Zone(state.ZHand, 0)); got != before+1 {
		t.Fatalf("after the resolved plotted cast the hand holds %d cards, want %d", got, before+1)
	}
}

// TestPlotZeroManaValueStillWaitsForALaterTurn pins the exact defect the
// counter model produced: with TIME counters keyed to mana value, a
// zero-mana-value plotted card carried zero counters and was castable the
// same turn. The later-turn rule is mana-value independent.
func TestPlotZeroManaValueStillWaitsForALaterTurn(t *testing.T) {
	t.Parallel()
	src := "Name:Free Plot\nManaCost:0\nTypes:Sorcery\nK:Plot:1 U\nOracle:x\n"
	e := handEngine(t, card(t, src))
	id := e.G.Zone(state.ZHand, 0)[0]
	if cmc := e.G.Obj(id).Face().Cmc(); cmc != 0 {
		t.Fatalf("precondition: synthetic carrier mana value = %d, want 0", cmc)
	}
	e.G.Players[0].Pool[state.MC] = 1
	e.G.Players[0].Pool[state.MU] = 1
	if findMode(e.legalActions(0), "plot") < 0 {
		t.Fatal("the zero-MV plot carrier was never offered its plot action")
	}
	castMode(t, e, id, "plot")
	if o := e.G.Obj(id); o.Zone != state.ZExile || o.PlottedTurn != e.G.Turn {
		t.Fatalf("zero-MV plot did not record the designation: zone=%s plottedTurn=%d", o.Zone, o.PlottedTurn)
	}
	plotDriveToStep(t, e, e.G.Turn, 0, state.StepMain1)
	if findMode(e.legalActions(0), "plot_cast") >= 0 {
		t.Fatal("a zero-mana-value plotted card was offered its free cast the same turn")
	}
	e.beginTurn(0)
	plotDriveToStep(t, e, e.G.Turn, 0, state.StepMain1)
	if findMode(e.legalActions(0), "plot_cast") < 0 {
		t.Fatal("a zero-mana-value plotted card was not offered its free cast on a later turn")
	}
}

func TestPlotActionOnlyAtSorceryTiming(t *testing.T) {
	t.Parallel()
	fast := card(t, "Name:Fast Plot\nManaCost:1 U\nTypes:Instant\nK:Plot:1 U\nOracle:x\n")
	e := handEngine(t, fast)
	fastID := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 1
	e.G.Players[0].Pool[state.MU] = 1
	// At main phase the plot action is offered; the plot gate is the
	// engine's sorcery-speed bool, NOT the instant face's own timing.
	if findMode(e.legalActions(0), "plot") < 0 {
		t.Fatal("the instant's plot action was withheld at sorcery timing")
	}
	// Upkeep is not sorcery timing even though the card is an instant.
	e.G.Step = state.StepUpkeep
	if findMode(e.legalActions(0), "plot") >= 0 {
		t.Fatal("the plot action was offered outside sorcery timing")
	}
	// Restore main phase so the payment stage below can run.
	e.G.Step = state.StepMain1
	castMode(t, e, fastID, "plot")
	if o := e.G.Obj(fastID); o.Zone != state.ZExile || o.Counter("TIME") != 0 || o.PlottedTurn != e.G.Turn {
		t.Fatalf("plotted instant exiled wrong: zone=%s time=%d plottedTurn=%d", o.Zone, o.Counter("TIME"), o.PlottedTurn)
	}
}

func TestPlotFreeCastFollowsSorceryTimingWhateverTheFace(t *testing.T) {
	t.Parallel()
	// A plotted card's cast follows sorcery timing even for a face whose own
	// type would allow instant-speed casting: drive the plotted synthetic
	// instant to a later turn and confirm the offer only exists at main
	// phase.
	fast := card(t, "Name:Fast Plot\nManaCost:1 U\nTypes:Instant\nK:Plot:1 U\nOracle:x\n")
	e := handEngine(t, fast)
	fastID := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 1
	e.G.Players[0].Pool[state.MU] = 1
	castMode(t, e, fastID, "plot")
	e.beginTurn(0)
	// Still at the next turn's upkeep: no offer (upkeep is not sorcery
	// timing, and the face's instant type must not widen the gate).
	e.G.Step = state.StepUpkeep
	if findMode(e.legalActions(0), "plot_cast") >= 0 {
		t.Fatal("the plotted instant's free cast was offered at upkeep")
	}
	plotDriveToStep(t, e, e.G.Turn, 0, state.StepMain1)
	if findMode(e.legalActions(0), "plot_cast") < 0 {
		t.Fatal("the plotted instant's free cast was never offered at sorcery timing on a later turn")
	}
}

func TestPlotProvenanceGateBlocksArbitraryExile(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Lock and Load"))
	lal := e.G.Zone(state.ZHand, 0)[0]
	// Exile the card by hand with no plot ACTION: no plotted designation and
	// no counters -- an arbitrary exiled Plot carrier must never be offered
	// the free cast.
	e.emit(events.Event{Kind: events.MoveZone, Obj: lal, From: state.ZHand, To: state.ZExile})
	o := e.G.Obj(lal)
	if o.Zone != state.ZExile || o.Counter("TIME") != 0 || o.PlottedTurn != 0 {
		t.Fatalf("precondition: arbitrary exile shape wrong: zone=%s time=%d plottedTurn=%d",
			o.Zone, o.Counter("TIME"), o.PlottedTurn)
	}
	if findMode(e.legalActions(0), "plot_cast") >= 0 {
		t.Fatal("an arbitrarily exiled Plot carrier was offered the plotted cast")
	}
	// A later turn still offers nothing: the designation, not the turn, is
	// the permission.
	e.beginTurn(0)
	plotDriveToStep(t, e, e.G.Turn, 0, state.StepMain1)
	if findMode(e.legalActions(0), "plot_cast") >= 0 {
		t.Fatal("an arbitrarily exiled Plot carrier was offered the plotted cast on a later turn")
	}
}

// TestPlotDesignationSharedAttributePath pins that the permission is the
// AlterAttribute "Plotted" designation itself, not a rules-private marker:
// granting it directly on an exiled card (the shape the corpus's
// DB$ AlterAttribute | Attributes$ Plotted family emits once the effect side
// models it) makes the free cast appear on a later turn, and clearing it
// removes the permission.
func TestPlotDesignationSharedAttributePath(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Lock and Load"))
	lal := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: lal, From: state.ZHand, To: state.ZExile})
	e.emit(events.Event{Kind: events.AlterAttribute, Obj: lal, Text: "Plotted", Amount: 1})
	if got := e.G.Obj(lal).PlottedTurn; got != e.G.Turn {
		t.Fatalf("AlterAttribute Plotted did not stamp the turn: PlottedTurn=%d turn=%d", got, e.G.Turn)
	}
	e.beginTurn(0)
	plotDriveToStep(t, e, e.G.Turn, 0, state.StepMain1)
	if findMode(e.legalActions(0), "plot_cast") < 0 {
		t.Fatal("a card with the Plotted designation was not offered its free cast on a later turn")
	}
	// Removing the designation (Activate$ False's -1) withdraws the offer.
	e.emit(events.Event{Kind: events.AlterAttribute, Obj: lal, Text: "Plotted", Amount: -1})
	if got := e.G.Obj(lal).PlottedTurn; got != 0 {
		t.Fatalf("AlterAttribute Plotted -1 did not clear the designation: PlottedTurn=%d", got)
	}
	if findMode(e.legalActions(0), "plot_cast") >= 0 {
		t.Fatal("the free cast survived the plotted designation being removed")
	}
}

// TestPlotDesignationClearedOnExileDeparture pins CR 701.34c's end condition:
// the designation is dropped as the card leaves exile, so a later effect that
// exiles the same card again does not revive the free-cast permission.
func TestPlotDesignationClearedOnExileDeparture(t *testing.T) {
	t.Parallel()
	e, lal := plotEngine(t)
	castMode(t, e, lal, "plot")
	if got := e.G.Obj(lal).PlottedTurn; got == 0 {
		t.Fatalf("precondition: plot did not set the designation: PlottedTurn=%d", got)
	}
	// Leave exile (the distant graveyard) and return to exile by an ordinary
	// move that grants nothing.
	e.emit(events.Event{Kind: events.MoveZone, Obj: lal, From: state.ZExile, To: state.ZGraveyard})
	if got := e.G.Obj(lal).PlottedTurn; got != 0 {
		t.Fatalf("leaving exile did not clear the plotted designation: PlottedTurn=%d", got)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: lal, From: state.ZGraveyard, To: state.ZExile})
	e.beginTurn(0)
	plotDriveToStep(t, e, e.G.Turn, 0, state.StepMain1)
	if findMode(e.legalActions(0), "plot_cast") >= 0 {
		t.Fatal("a re-exiled card revived the plotted free cast")
	}
}

func TestPlotOfferNeedsPayableCost(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Lock and Load"))
	// The pool is empty: CR 701.34a's action pays {3}{U}, so the offer must
	// be withheld, not stranded into an unpayable cast.
	if findMode(e.legalActions(0), "plot") >= 0 {
		t.Fatal("the plot action was offered with no mana to pay it")
	}
}
