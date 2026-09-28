package rules

// Regressions for the engine gaps the cardfuzz ability-coverage audit
// (fuzz-cov2) separated from genuinely rare game states, each pinned on real
// corpus cards:
//
//   - a printed Mode$ Phase trigger resolved with its own SOURCE as its
//     Remembered set (Tombstone Stairwell destroyed itself every end step);
//   - an entering permanent's ChangesZone triggers were matched before its
//     "Updated" entry replacement ran, so `Permanent.tapped` never saw an
//     enters-tapped land (Amulet of Vigor);
//   - SVarCompare$ NE<n> was unread and failed closed (Spark Fiend);
//   - Count$LifeYouLostThisTurn and Count$Party were unmodelled and failed
//     closed (Luminarch Ascension; the 39 party carriers);
//   - the firstTurnControlled property was unrecognised and failed closed
//     (Rocket Launcher).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// stepTriggers enters step with seat active as the active player and
// returns the number of queued triggers the StepChange matched.
func stepTriggers(e *Engine, step state.Step, active state.PlayerID) int {
	e.G.Active = active
	e.pending = nil
	e.emit(events.Event{Kind: events.StepChange, Step: step})
	return len(e.pendingTriggers)
}

// TestPhaseTriggerRemembersSourcesListNotSource: Tombstone Stairwell's
// "at the beginning of each end step, destroy all tokens created with it"
// (DestroyAll Card.IsRemembered) must destroy only what the Stairwell
// remembered -- nothing, here -- and never the Stairwell itself.
func TestPhaseTriggerRemembersSourcesListNotSource(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	id := onBoardCard(t, e, 0, corpusCard(t, "Tombstone Stairwell"))
	if n := stepTriggers(e, state.StepEnd, 0); n != 1 {
		t.Fatalf("end-step triggers queued = %d, want the Stairwell's one", n)
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the end-step trigger", e.G.Stack)
	}
	e.resolveTop()
	if z := e.G.Obj(id).Zone; z != state.ZBattlefield {
		t.Fatalf("Tombstone Stairwell is in %s after its own end-step trigger, want battlefield", z)
	}
}

// TestEntersTappedPermanentMatchesTappedTrigger: Guildless Commons "enters
// tapped" through an Updated replacement; Amulet of Vigor's "whenever a
// permanent you control enters tapped, untap it" must see it tapped.
func TestEntersTappedPermanentMatchesTappedTrigger(t *testing.T) {
	t.Parallel()
	e, _, landID := newFixtureDeck(t, 202, corpusCardText(t, "g/guildless_commons.txt"))
	amuletID := onBoardCard(t, e, 0, corpusCard(t, "Amulet of Vigor"))
	driveToStep(t, e, 1, 0, state.StepMain1)
	d := e.Pending()
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "play_land" && opt.Obj == landID {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no play_land option for Guildless Commons: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	if !e.G.Obj(landID).Tapped {
		t.Fatal("precondition: Guildless Commons did not enter tapped")
	}
	// Guildless Commons' own ETB trigger and Amulet of Vigor's both match
	// the entry, so seat 0 orders them; before the fix only the land's own
	// trigger matched and no order was asked.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTriggerOrder {
		t.Fatalf("pending = %+v, want the trigger-order ask for the land's and Amulet of Vigor's triggers", d)
	}
	amulet := false
	for _, o := range d.Options {
		if o.Obj == amuletID {
			amulet = true
		}
	}
	if !amulet {
		t.Fatalf("Amulet of Vigor's trigger is not among %+v", d.Options)
	}
	// Amulet of Vigor's trigger resolves first (the land's own trigger
	// returns a land -- possibly itself -- to hand).
	var order []int
	for _, o := range d.Options {
		if o.Obj != amuletID {
			order = append(order, o.Index)
		}
	}
	for _, o := range d.Options {
		if o.Obj == amuletID {
			order = append(order, o.Index)
		}
	}
	submitChoices(t, e, order...)
	top := e.G.Obj(e.G.Stack[len(e.G.Stack)-1])
	if top.Source != amuletID {
		t.Fatalf("Amulet of Vigor's trigger is not on top of the stack %v", e.G.Stack)
	}
	e.resolveTop()
	if o := e.G.Obj(landID); o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatal("Amulet of Vigor did not untap the enters-tapped land")
	}
}

// TestSVarCompareNE: Spark Fiend's upkeep roll is gated `CheckSVar$ Safe |
// SVarCompare$ NE0` over SVar:Safe:Number$1 -- it fires until a roll stores 0.
func TestSVarCompareNE(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	onBoardCard(t, e, 0, corpusCard(t, "Spark Fiend"))
	if n := stepTriggers(e, state.StepUpkeep, 0); n != 1 {
		t.Fatalf("upkeep triggers queued = %d, want Spark Fiend's roll", n)
	}
}

// TestCountLifeYouLostThisTurn: Luminarch Ascension's opponent-end-step
// quest counter is gated on Count$LifeYouLostThisTurn EQ0.
func TestCountLifeYouLostThisTurn(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	onBoardCard(t, e, 0, corpusCard(t, "Luminarch Ascension"))
	if n := stepTriggers(e, state.StepEnd, 1); n != 1 {
		t.Fatalf("no life lost: triggers queued = %d, want Luminarch Ascension's one", n)
	}
	e2 := layerEngine(t)
	onBoardCard(t, e2, 0, corpusCard(t, "Luminarch Ascension"))
	e2.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -2})
	if n := stepTriggers(e2, state.StepEnd, 1); n != 0 {
		t.Fatalf("2 life lost: triggers queued = %d, want none", n)
	}
}

// TestCountParty: CR 700.8 -- one creature per role, a Changeling filling
// whichever role is left.
func TestCountParty(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	party := func() int32 {
		n, ok := effects.EvalCountOK(e, &effects.Ctx{Controller: 0}, "Count$Party")
		if !ok {
			t.Fatal("Count$Party is not evaluated")
		}
		return n
	}
	if n := party(); n != 0 {
		t.Fatalf("empty board party = %d", n)
	}
	onBoardCard(t, e, 0, corpusCard(t, "Soul Warden"))
	onBoardCard(t, e, 0, corpusCard(t, "Soul Warden"))
	if n := party(); n != 1 {
		t.Fatalf("two Clerics: party = %d, want 1", n)
	}
	onBoardCard(t, e, 0, corpusCard(t, "Prodigal Sorcerer"))
	onBoardCard(t, e, 0, corpusCard(t, "Changeling Outcast"))
	if n := party(); n != 3 {
		t.Fatalf("Cleric, Cleric, Wizard, Changeling: party = %d, want 3", n)
	}
	onBoardCard(t, e, 1, corpusCard(t, "Changeling Outcast"))
	if n := party(); n != 3 {
		t.Fatalf("an opponent's Changeling joined the party: %d", n)
	}
}

// TestFirstTurnControlled: Rocket Launcher's `IsPresent$
// Card.Self+!firstTurnControlled` -- controlled continuously since the
// controller's most recent turn began.
func TestFirstTurnControlled(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	id := onBoardCard(t, e, 0, corpusCard(t, "Rocket Launcher"))
	sc := effects.SpecContext{Source: id, You: 0}
	o := e.G.Obj(id)
	o.SummonSick = true
	if effects.MatchesSpecCtx(e.G, "Card.Self+!firstTurnControlled", id, sc) {
		t.Fatal("a permanent that just came under control matched !firstTurnControlled")
	}
	o.SummonSick = false
	if !effects.MatchesSpecCtx(e.G, "Card.Self+!firstTurnControlled", id, sc) {
		t.Fatal("a permanent controlled since the turn began did not match !firstTurnControlled")
	}
}
