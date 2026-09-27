package rules

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file pins the CAST/ACTIVATION half of TargetingPlayer$: when a spell's
// SP$ body or an activated AB$ body carries `TargetingPlayer$ Player.Opponent`
// (or the bare `Opponent` spelling), the target ask must be answered by an
// opponent, not by the ability's controller. The trigger/resolution half
// (rules/targetingplayer_test.go's Bladegriff Prototype) reaches askTarget;
// the cast/activation half reaches targetAsk (rules/cast.go), which before
// this never read TargetingPlayer$ at all, so the spell silently asked its
// caster.
//
// The opponent is chosen by the explicit engine contract shared with the
// trigger resolver (rules/stack.go targetAskChooser -> targetChooserFromSpec):
// the first living seat in AliveFrom(0) other than the controller. Forge's
// "an opponent's choice" does not name which opponent when several are alive,
// so this deterministic pick is stated rather than improvised. Target
// LEGALITY stays the ability controller's: the option list is computed from
// pc.player exactly as before, and only the answering seat moves.

// targetingChooserBoard seats `seats` players at Main 1 of seat 0's first
// turn. Seat 0's deck opens with the named corpus carrier plus one Grizzly
// Bears and basics; every other seat's deck holds one Grizzly Bears and
// Mountains. Callers move the carrier/bears onto the battlefield with
// searchMoveByNameSeat.
func targetingChooserBoard(t *testing.T, reg *cards.Registry, seats int, carrier string) (*Engine, Config) {
	t.Helper()
	mountain := searchCorpusCard(t, reg, "Mountain")
	forest := searchCorpusCard(t, reg, "Forest")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck0 := []*cards.Card{searchCorpusCard(t, reg, carrier), bear}
	for i := 0; i < 8; i++ {
		deck0 = append(deck0, forest, mountain)
	}
	for len(deck0) < 40 {
		deck0 = append(deck0, bear)
	}
	decks := [][]*cards.Card{deck0}
	names := []string{"caster"}
	for i := 1; i < seats; i++ {
		d := []*cards.Card{bear}
		for len(d) < 40 {
			d = append(d, mountain)
		}
		decks = append(decks, d)
		names = append(names, "opponent-"+strconv.Itoa(i))
	}
	cfg := seatZeroStart(Config{Seed: 7731, Names: names, Decks: decks, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// saWithTargetingPlayer returns the compiled SA of card whose TargetingPlayer$
// equals spec and whose Kind equals kind ("SP", "AB", ...). It is the fixture
// precondition guard: a build that lost the parameter would otherwise pass
// every test below vacuously with the ask defaulted to the controller.
func saWithTargetingPlayer(card *cards.Card, kind, spec string) *cards.SA {
	for _, f := range card.Faces {
		for _, sa := range f.Abilities {
			if sa.Kind == kind && sa.Params["TargetingPlayer"] == spec {
				return sa
			}
		}
	}
	return nil
}

// abilityOptionFor returns the priority "ability" option for obj (any index).
func abilityOptionFor(t *testing.T, e *Engine, obj state.ObjID) decision.Option {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("not at priority: %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == obj {
			return o
		}
	}
	t.Fatalf("no ability option for %d: %+v", obj, d.Options)
	return decision.Option{}
}

// optionForObj returns the target-ask option naming obj.
func targetOptionForObj(t *testing.T, d *decision.Decision, obj state.ObjID) decision.Option {
	t.Helper()
	for _, o := range d.Options {
		if o.Obj == obj {
			return o
		}
	}
	t.Fatalf("target ask offers no option for %d: %+v", obj, d.Options)
	return decision.Option{}
}

// passToNextOwnTurn drives to seat 0's next Main 1 so a permanent placed this
// turn is no longer summoning sick and its tap ability (Preacher/Echo Chamber)
// becomes legal without a direct, replay-invisible state write. One turn per
// seat, so a three-seat game's next own turn is turn+3.
func passToNextOwnTurn(t *testing.T, e *Engine) {
	t.Helper()
	driveToStep(t, e, e.G.Turn+int32(len(e.G.Players)), 0, state.StepMain1)
}

// TestTargetingPlayerOpponentActivatedAbilityIsAnsweredByOpponent casts no
// spell: it activates Preacher, whose AB$ GainControl carries
// `TargetingPlayer$ Player.Opponent`, and asserts the REAL activation flow's
// target ask is posed to seat 1 with the legality set still computed from
// seat 0. It then confirms the answering opponent cannot submit an index
// outside the offered options.
func TestTargetingPlayerOpponentActivatedAbilityIsAnsweredByOpponent(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	carrier := mustCorpusCard(t, reg, "Preacher")
	if sa := saWithTargetingPlayer(carrier, "AB", "Player.Opponent"); sa == nil {
		t.Fatal("Preacher's compiled AB no longer carries TargetingPlayer$ Player.Opponent -- fixture premise broken")
	}
	e, cfg := targetingChooserBoard(t, reg, 2, "Preacher")
	preacher := searchMoveByNameSeat(t, e, 0, "Preacher", state.ZBattlefield)
	// Preacher entered this turn, so its {T} ability is not yet legal; pass to
	// the next own turn (over the opponent's) with no creatures on the board so
	// no attackers decision interrupts the drive. The bears are placed after.
	passToNextOwnTurn(t, e)
	own := searchMoveByNameSeat(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	opp := searchMoveByNameSeat(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	if preacher == 0 || own == 0 || opp == 0 {
		t.Fatalf("fixtures missing: preacher=%d own=%d opp=%d", preacher, own, opp)
	}
	// Preconditions the assertion depends on: distinct controllers and a
	// carrier that is on the battlefield untapped (the activation cost is T).
	if e.G.Obj(own).Controller == e.G.Obj(opp).Controller {
		t.Fatal("candidate fixture does not contain different controllers")
	}
	if o := e.G.Obj(preacher); o == nil || o.Zone != state.ZBattlefield || o.Tapped || o.SummonSick {
		t.Fatalf("Preacher precondition failed: %+v", o)
	}

	submitChoices(t, e, abilityOptionFor(t, e, preacher).Index)

	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("activation ask = %+v, want the KTarget for Preacher's ability", d)
	}
	// The defect: the ask must go to an opponent (seat 1), not the activator.
	if d.Player != 1 {
		t.Fatalf("target ask posed to seat %d, want opponent seat 1", d.Player)
	}
	// Legality is unchanged -- still computed from seat 0, so BOTH seats'
	// bears are offered.
	targetOptionForObj(t, d, own)
	targetOptionForObj(t, d, opp)
	// The answering seat cannot name an option that was never offered.
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{len(d.Options)}}); err == nil {
		t.Fatal("chooser can submit a target not present in the legal option set")
	}

	// A legal answer from the opponent completes the activation and hands
	// priority back to the ACTIVATOR, never to the answering opponent.
	submitChoices(t, e, targetOptionForObj(t, d, own).Index)
	if o := e.G.Obj(preacher); o == nil || !o.Tapped {
		t.Fatalf("Preacher was not tapped by the activation: %+v", o)
	}
	passUntilStackEmpty(t, e, 20)
	// Seat 0 gained control of its own bear (the legal answer), so the
	// activation really resolved.
	if got := e.G.Obj(own).Controller; got != 0 {
		t.Fatalf("chosen bear controller = %d, want seat 0", got)
	}
	replayCheck(t, e, cfg)
}

// TestTargetingPlayerOpponentSelectionAskIsControllerFacing asserts that the
// controller, not a hard-coded first opponent, chooses the answering seat.
func TestTargetingPlayerOpponentSelectionAskIsControllerFacing(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, _ := targetingChooserBoard(t, reg, 3, "Preacher")
	preacher := searchMoveByNameSeat(t, e, 0, "Preacher", state.ZBattlefield)
	passToNextOwnTurn(t, e)
	searchMoveByNameSeat(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	searchMoveByNameSeat(t, e, 2, "Grizzly Bears", state.ZBattlefield)

	// Precondition: two living opponents and the controller is 0.
	alive := e.G.AliveFrom(0)
	if len(alive) != 3 || alive[0] != 0 {
		t.Fatalf("fixture wants 3 living seats starting at 0, got %v", alive)
	}
	if e.G.Obj(preacher).Controller != 0 {
		t.Fatalf("Preacher controller = %d, want seat 0", e.G.Obj(preacher).Controller)
	}

	submitChoices(t, e, abilityOptionFor(t, e, preacher).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "opp_pick" {
		t.Fatalf("ask = %+v, want the controller's opponent-selection ask", d)
	}
	if d.Player != 0 || len(d.Options) != 2 || d.Options[0].Player == d.Options[1].Player {
		t.Fatalf("selection options = %+v, want distinct opponents offered to controller 0", d)
	}
}

// TestTargetingPlayerOpponentSkipsDeadFirstOpponent is the sole-opponent
// contract: seat 1 has left, so seat 2 answers directly without an extra ask.
func TestTargetingPlayerOpponentSkipsDeadFirstOpponent(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, _ := targetingChooserBoard(t, reg, 3, "Preacher")
	preacher := searchMoveByNameSeat(t, e, 0, "Preacher", state.ZBattlefield)
	passToNextOwnTurn(t, e)
	searchMoveByNameSeat(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	searchMoveByNameSeat(t, e, 2, "Grizzly Bears", state.ZBattlefield)

	// Precondition: seat 1 is alive before the loss, so the skip is a real
	// transition and not a board that never had a first opponent.
	if e.G.Players[1].Lost {
		t.Fatal("seat 1 already lost before the fixture marked it")
	}
	e.emit(events.Event{Kind: events.PlayerLost, Player: 1, Text: "conceded"})
	if !e.G.Players[1].Lost {
		t.Fatal("PlayerLost did not mark seat 1 lost")
	}
	if len(e.G.AliveFrom(0)) != 2 {
		t.Fatalf("alive after seat 1 left = %v, want two seats", e.G.AliveFrom(0))
	}

	submitChoices(t, e, abilityOptionFor(t, e, preacher).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("ask = %+v, want a target ask", d)
	}
	if d.Player != 2 {
		t.Fatalf("chooser with seat 1 lost = %d, want seat 2", d.Player)
	}
}
