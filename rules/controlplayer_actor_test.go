package rules

// CR 722 (formerly 720) acting-player conformance for a controlled turn.
//
// controlPlayerRedirect hands every decision the controlled seat would be
// offered to its controller: the controller ANSWERS. But the actions the
// answer selects are still the CONTROLLED player's (CR 722.1 -- the
// controller makes the choices and decisions the controlled player is allowed
// to make; the controlled player still plays the land, activates the mana
// ability and casts the spell). The priority option list is built for the
// controlled seat (askPriority's legalActions(p)), so every handler behind it
// must run for that seat too.
//
// The cardfuzz class this pins (sig "priority option # (activate \"Activate
// Mountain for mana\") cannot be performed: the source has no activatable
// mana ability", nine seeds across Mindslaver, Emrakul the Promised End,
// Secret of Bloodbending and Cruel Entertainment): the offer listed the
// controlled seat's lands, the Submit guard and every handler ran as the
// answering controller, and the controller has no mana ability on the
// controlled seat's Mountain -- so the bot's first mana tap on a controlled
// turn was rejected and the match died. The same mismatch cast the
// controlled seat's hand as the controller and planned its payments from the
// controller's (empty) mana base.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// mindslaverControlledTurn activates seat 0's real Mindslaver on seat 1 and
// drives to seat 1's controlled Main1. Seat 1 gets two Mountains on the
// battlefield and a Raging Goblin in hand first, so the controlled turn has
// a mana ability, a land drop and a castable spell to offer. Each named
// seat-1 card is put in seat 1's hand; their ids are returned in order.
func mindslaverControlledTurn(t *testing.T, hand ...string) (*Engine, Config, []state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	var extras1 []*cards.Card
	for _, n := range hand {
		extras1 = append(extras1, lookup(t, reg, n))
	}
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Mindslaver")}, extras1)
	slaver := moveByName(t, e, 0, "Mindslaver", state.ZBattlefield)
	moveByName(t, e, 1, "Mountain", state.ZBattlefield)
	moveByName(t, e, 1, "Mountain", state.ZBattlefield)
	var ids []state.ObjID
	for _, n := range hand {
		ids = append(ids, handByName(t, e, 1, n))
	}
	addMana(t, e, 0, "CCCC")
	e.Advance()

	submitChoices(t, e, abilityOption(t, e, slaver, 0).Index)
	for i := 0; i < 10; i++ {
		d := e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			break
		}
		switch d.Kind {
		case decision.KTarget:
			idx := indexOfPlayerOption(d, 1)
			if idx < 0 {
				t.Fatalf("Mindslaver target ask omitted seat 1: %+v", d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("target seat 1: %v", err)
			}
		case decision.KChoose:
			submitChoices(t, e, d.Options[0].Index)
		default:
			t.Fatalf("unexpected decision while activating Mindslaver: %+v", d)
		}
	}
	passUntilStackEmpty(t, e, 20)
	if ctl, ok := e.G.ControlledBy[1]; !ok || ctl != 0 {
		t.Fatalf("precondition: Mindslaver did not grant seat 0 control of seat 1: %+v", e.G.ControlledBy)
	}
	driveToTurn(t, e, 2, 1)
	d := e.Pending()
	if e.G.Active != 1 || d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("precondition: seat 1's controlled Main1 priority posed to controller 0, got active=%d %+v", e.G.Active, d)
	}
	if e.G.Priority != 1 {
		t.Fatalf("precondition: the priority is seat 1's own (e.G.Priority=%d)", e.G.Priority)
	}
	return e, cfg, ids
}

// handByName returns seat p's card named name, moving it from the library
// into the hand when it was not drawn.
func handByName(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZHand, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	return moveByName(t, e, p, name, state.ZHand)
}

func priorityOptionFor(t *testing.T, d *decision.Decision, kind string, obj state.ObjID) decision.Option {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == kind && (obj == 0 || o.Obj == obj) {
			return o
		}
	}
	t.Fatalf("no %q option for obj %d in %+v", kind, obj, d.Options)
	return decision.Option{}
}

// TestControlledTurnOfferedOptionsPassTheSubmitGuard is the cardfuzz repro:
// every action the controlled seat's priority offers must be accepted by the
// Submit guard (priorityOptionStale) -- the offer and the guard are ONE
// membership, whoever answers.
func TestControlledTurnOfferedOptionsPassTheSubmitGuard(t *testing.T) {
	e, _, _ := mindslaverControlledTurn(t, "Raging Goblin")
	d := e.Pending()
	activates := 0
	for _, o := range d.Options {
		if o.Kind == "activate" {
			activates++
			if ob := e.G.Obj(o.Obj); ob == nil || ob.Controller != 1 {
				t.Fatalf("activate option names %d, not a seat-1 permanent", o.Obj)
			}
		}
		if err := e.validatePriorityChoice(d, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
			t.Errorf("offered option rejected by the Submit guard: %v", err)
		}
	}
	if activates == 0 {
		t.Fatal("precondition: the controlled seat's Mountains offered no mana activation")
	}
}

// TestControlledTurnActionsActForTheControlledSeat answers the controlled
// turn's actions as the controller and checks each lands on seat 1: the
// Mountain's mana goes to seat 1's pool, the land drop is seat 1's, and the
// Raging Goblin is cast by and enters under seat 1. The log replays.
func TestControlledTurnActionsActForTheControlledSeat(t *testing.T) {
	e, cfg, ids := mindslaverControlledTurn(t, "Raging Goblin")
	goblin := ids[0]

	// Mana ability: the cardfuzz shape.
	d := e.Pending()
	act := priorityOptionFor(t, d, "activate", 0)
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{act.Index}}); err != nil {
		t.Fatalf("controller tapping the controlled seat's Mountain: %v", err)
	}
	if got := e.G.Players[1].Pool.Total(); got != 1 {
		t.Fatalf("seat 1's pool = %d after its Mountain tapped, want 1", got)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("seat 0 (controller) pool = %d, want 0: the mana is the controlled seat's", got)
	}

	// Land drop: the controlled seat plays its own land.
	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("after the tap, want the controller answering seat 1's priority, got %+v", d)
	}
	land := priorityOptionFor(t, d, "play_land", 0)
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{land.Index}}); err != nil {
		t.Fatalf("controller playing the controlled seat's land: %v", err)
	}
	if o := e.G.Obj(land.Obj); o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("played land zone=%s controller=%d, want seat 1's battlefield", o.Zone, o.Controller)
	}
	if e.G.Players[1].LandsPlayed != 1 || e.G.Players[0].LandsPlayed != 0 {
		t.Fatalf("lands played: seat0=%d seat1=%d, want the drop charged to seat 1",
			e.G.Players[0].LandsPlayed, e.G.Players[1].LandsPlayed)
	}

	// Spell: cast from seat 1's hand with seat 1's floating R.
	d = e.Pending()
	cast := priorityOptionFor(t, d, "cast", goblin)
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{cast.Index}}); err != nil {
		t.Fatalf("controller casting the controlled seat's Raging Goblin: %v", err)
	}
	for i := 0; i < 20 && e.G.Obj(goblin).Zone != state.ZBattlefield; i++ {
		d = e.Pending()
		if d == nil {
			t.Fatal("no decision while casting the goblin")
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				break
			}
			submitChoices(t, e, priorityOptionFor(t, d, "pass", 0).Index)
			continue
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	if o := e.G.Obj(goblin); o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("Raging Goblin zone=%s controller=%d, want seat 1's battlefield (CR 722: the controlled player casts it)", o.Zone, o.Controller)
	}
	replayCheck(t, e, cfg)
}

// TestControlledTurnPaymentPlansUseTheControlledSeatsMana is the planner
// sibling: EnsurePaymentActions builds the controlled seat's cast offers, so
// it must plan them from that seat's mana base, and a planned Submit by the
// controller must validate and cast for the controlled seat.
func TestControlledTurnPaymentPlansUseTheControlledSeatsMana(t *testing.T) {
	e, cfg, ids := mindslaverControlledTurn(t, "Raging Goblin")
	goblin := ids[0]
	d := e.Pending()
	var action *decision.PaymentAction
	for i, a := range e.EnsurePaymentActions() {
		if a.Cast.Object == goblin {
			action = &e.EnsurePaymentActions()[i]
		}
	}
	if action == nil || len(action.Plans) == 0 {
		t.Fatalf("no payment plan for the controlled seat's Raging Goblin: %+v", action)
	}
	plan := action.Plans[0]
	for _, a := range plan.Activations {
		if o := e.G.Obj(a.Source); o == nil || o.Controller != 1 {
			t.Fatalf("plan taps %d, not a seat-1 mana source", a.Source)
		}
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player,
		Payment: &decision.PaymentSelection{ActionID: action.ID, Plan: decision.ClonePaymentPlan(plan)}}); err != nil {
		t.Fatalf("controller submitting the controlled seat's planned cast: %v", err)
	}
	for i := 0; i < 20 && e.G.Obj(goblin).Zone != state.ZBattlefield; i++ {
		d = e.Pending()
		if d == nil || d.Kind != decision.KPriority || len(e.G.Stack) == 0 {
			break
		}
		submitChoices(t, e, priorityOptionFor(t, d, "pass", 0).Index)
	}
	if o := e.G.Obj(goblin); o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("planned Raging Goblin zone=%s controller=%d, want seat 1's battlefield", o.Zone, o.Controller)
	}
	replayCheck(t, e, cfg)
}

// TestControlledTurnPonderArrangesAndShufflesTheControlledLibrary is the
// mid-resolution sibling: the controlled seat's Ponder asks its arrange and
// its "you may shuffle" of the controller, but both act on the CASTER's
// library (CR 722.1). The arrange handler read the answering seat's library
// and the resume point recorded the answering seat as the shuffling player,
// so the controller's library was rearranged and shuffled instead.
func TestControlledTurnPonderArrangesAndShufflesTheControlledLibrary(t *testing.T) {
	e, cfg, ids := mindslaverControlledTurn(t, "Ponder")
	ponder := ids[0]
	addMana(t, e, 1, "U")
	d := e.Pending()
	submitChoices(t, e, priorityOptionFor(t, d, "cast", ponder).Index)
	mark := len(e.L.Events)
	sawArrange, sawShuffleAsk := false, false
	for i := 0; i < 30; i++ {
		d = e.Pending()
		if d == nil {
			t.Fatal("no decision while Ponder resolves")
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				break
			}
			submitChoices(t, e, priorityOptionFor(t, d, "pass", 0).Index)
			continue
		}
		if d.Player != 0 || d.Acting() != 1 {
			t.Fatalf("Ponder's %s ask: answering %d acting %d, want controller 0 acting for 1", d.Kind, d.Player, d.Acting())
		}
		switch {
		case d.Kind == decision.KArrange:
			sawArrange = true
			for _, o := range d.Options {
				if ob := e.G.Obj(o.Obj); ob == nil || ob.Owner != 1 || ob.Zone != state.ZLibrary {
					t.Fatalf("Ponder arranges %d (owner %d), not seat 1's library", o.Obj, ob.Owner)
				}
			}
			submitChoices(t, e, allIndexes(d)...)
		default:
			yes := -1
			for _, o := range d.Options {
				if o.Kind == "yes" {
					yes = o.Index
				}
			}
			if yes < 0 {
				submitChoices(t, e, d.Options[0].Index)
				continue
			}
			sawShuffleAsk = true
			submitChoices(t, e, yes)
		}
	}
	if !sawArrange || !sawShuffleAsk {
		t.Fatalf("Ponder asks seen: arrange=%v shuffle=%v", sawArrange, sawShuffleAsk)
	}
	shuffles := 0
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Shuffle {
			shuffles++
			if ev.Player != 1 {
				t.Fatalf("Ponder shuffled seat %d's library, want the caster seat 1's", ev.Player)
			}
		}
	}
	if shuffles != 1 {
		t.Fatalf("Ponder's accepted shuffle emitted %d shuffles, want 1", shuffles)
	}
	replayCheck(t, e, cfg)
}

func allIndexes(d *decision.Decision) []int {
	out := make([]int, len(d.Options))
	for i, o := range d.Options {
		out[i] = o.Index
	}
	return out
}
