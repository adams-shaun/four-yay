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

// Exert (CR 702.100, task exert1) pinned end to end on real corpus cards.
// Combat Celebrant drives the main flow: its static is the corpus's one
// gated carrier (`IsPresent$ Creature.Self+notExertedThisTurn`) and its
// rider chains UntapAll (StrictlyOther) into DB$ AddPhase, both already
// real. Watchful Naga is an ungated carrier with a Draw rider. Games whose
// every resource was created through real decisions replay byte-identically
// (replayCheck); the one fixture that mutates the pool directly says so and
// skips that pin.

// exertBearSrc is the synthetic 2/2 Bear fixture (never a corpus .txt, per
// the licensing rule): a vanilla creature for the untap-all rider to untap.
const exertBearSrc = "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// exertEngine builds a two-seat Mountain-deck game with the extras on seat
// 0's deck, at Main1 of turn 1.
func exertEngine(t *testing.T, reg *cards.Registry, extras ...*cards.Card) (*Engine, Config) {
	t.Helper()
	return addPhaseEngine(t, reg, extras, []*cards.Card{})
}

// exertCount counts Exert events: positive selects the exert itself
// (Amount >= 0), negative the untap-step consume marker (Amount -1).
func exertCount(e *Engine, positive bool) int {
	return countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.Exert && (ev.Amount >= 0) == positive
	})
}

// exertPending asserts the pending decision is the exert election for id and
// returns it.
func exertPending(t *testing.T, e *Engine, id state.ObjID) *decision.Decision {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 ||
		d.Options[0].Kind != "exert" || d.Options[1].Obj != id {
		t.Fatalf("pending = %+v, want the exert election for object %d", d, id)
	}
	return d
}

// declineExerts answers every pending exert election with the decline
// (option 0), failing if any ask names a forbidden id. Used to pin the gate
// by absence while walking an ungated carrier's election.
func declineExerts(t *testing.T, e *Engine, never state.ObjID) {
	t.Helper()
	for i := 0; i < 10; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "exert" {
			return
		}
		if never != 0 {
			for _, o := range d.Options {
				if o.Obj == never {
					t.Fatalf("exert election offered the gated object %d", never)
				}
			}
		}
		submitChoices(t, e, 0)
	}
	t.Fatal("the exert election never ended")
}

// TestCombatCelebrantExertUntapAllRiderAndExtraCombat is the leaf the brief
// names: Combat Celebrant attacks, the exert ask is offered and answered
// yes, the rider untaps every OTHER creature you control (the Celebrant
// itself stays tapped -- StrictlyOther), and the extra combat splices after
// the current one. In the extra combat the once-exerted Celebrant is NOT
// offered again (the notExertedThisTurn gate), and the whole game replays
// byte-identically.
func TestCombatCelebrantExertUntapAllRiderAndExtraCombat(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := exertEngine(t, reg, lookup(t, reg, "Combat Celebrant"), card(t, exertBearSrc))
	cc := moveByName(t, e, 0, "Combat Celebrant", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Bear", state.ZBattlefield)
	driveToStep(t, e, 3, 0, state.StepMain1)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, cc, bear)

	// The exert election is pending: option 0 the decline, option 1 the
	// Celebrant. Accept it.
	exertPending(t, e, cc)
	submitChoices(t, e, 1)
	if !e.G.Obj(cc).ExertedThisTurn || !e.G.Obj(cc).ExertSkipUntap {
		t.Fatal("the Exert fold did not stamp both lifetimes")
	}
	if got := exertCount(e, true); got != 1 {
		t.Fatalf("%d Exert events, want 1", got)
	}
	// The election ends (the Bear carries no static); the queued rider is
	// placed and resolves at the priority round the Advance loop drives.
	passUntilStackEmpty(t, e, 40)
	if len(e.G.ExtraPhases) != 1 {
		t.Fatalf("ExtraPhases = %+v, want one combat grant", e.G.ExtraPhases)
	}
	if e.G.Obj(bear).Tapped {
		t.Fatal("the rider did not untap the other attacking creature")
	}
	if !e.G.Obj(cc).Tapped {
		t.Fatal("the rider untapped the Celebrant itself (StrictlyOther unread)")
	}
	// The offer gate (CR 702.100a "hasn't been exerted this turn"): the
	// once-exerted Celebrant is no longer offerable. The extra combat's
	// attackers ask follows; the Celebrant itself cannot even attack (it is
	// tapped), so the Bear carries the second attack and the election must
	// not open at all.
	if e.exertOfferHolds(cc) {
		t.Fatal("the once-exerted Celebrant is still offerable (the notExertedThisTurn gate is unread)")
	}
	passToKind(t, e, decision.KAttackers)
	if !e.G.ExtraPhases[0].Consumed {
		t.Fatalf("grant = %+v, want consumed at the extra combat", e.G.ExtraPhases[0])
	}
	submitAttackersOnly(t, e, bear)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after the extra combat's declaration: %+v, want priority (no exert election may open)", d)
	}
	replayCheck(t, e, cfg)
}

// TestCombatCelebrantExertDeclineLeavesStateUnchanged pins the decline path:
// the ask is offered and declined, and nothing in the game state moves -- no
// Exert event, no trigger, no flag, no extra combat.
func TestCombatCelebrantExertDeclineLeavesStateUnchanged(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := exertEngine(t, reg, lookup(t, reg, "Combat Celebrant"), card(t, exertBearSrc))
	cc := moveByName(t, e, 0, "Combat Celebrant", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Bear", state.ZBattlefield)
	driveToStep(t, e, 3, 0, state.StepMain1)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, cc, bear)
	exertPending(t, e, cc)
	submitChoices(t, e, 0)
	if e.G.Obj(cc).ExertedThisTurn || e.G.Obj(cc).ExertSkipUntap {
		t.Fatal("a declined exert stamped a flag")
	}
	if got := exertCount(e, false) + exertCount(e, true); got != 0 {
		t.Fatalf("%d Exert events after a decline, want 0", got)
	}
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("%d rider triggers queued after a decline", len(e.pendingTriggers))
	}
	// The election ended; the combat's own priority is pending, with no
	// extra combat grant anywhere and the attackers' CR 508.1f taps intact.
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after the decline: %+v, want the combat priority", d)
	}
	if len(e.G.ExtraPhases) != 0 {
		t.Fatalf("ExtraPhases = %+v, want none (no rider fired)", e.G.ExtraPhases)
	}
	if !e.G.Obj(cc).Tapped || !e.G.Obj(bear).Tapped {
		t.Fatal("the attackers' taps moved without an untap")
	}
	replayCheck(t, e, cfg)
}

// TestExertedCreatureSkipsUntapStepButEffectsUntapIt pins the two untap
// lifetimes apart: an untap EFFECT untaps an exerted creature (CR 702.100b
// names only the untap step, so effects.TryUntap is never gated), while the
// controller's next untap step skips it and consumes the window exactly once.
func TestExertedCreatureSkipsUntapStepButEffectsUntapIt(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	wand := card(t, "Name:Untap Wand\nManaCost:1\nTypes:Artifact\nOracle:x\n"+
		"A:AB$ UntapAll | Cost$ 1 | ValidCards$ Creature.YouCtrl | SpellDescription$ Untap all creatures you control.\n")
	e, _ := exertEngine(t, reg, lookup(t, reg, "Combat Celebrant"), card(t, exertBearSrc), wand)
	cc := moveByName(t, e, 0, "Combat Celebrant", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Bear", state.ZBattlefield)
	moveByName(t, e, 0, "Untap Wand", state.ZBattlefield)
	driveToStep(t, e, 3, 0, state.StepMain1)
	// Tap the Bear so the rider's untap-all has something real to untap.
	e.emit(events.Event{Kind: events.Tap, Obj: bear})
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, cc)
	exertPending(t, e, cc)
	submitChoices(t, e, 1)
	passUntilStackEmpty(t, e, 40)
	if e.G.Obj(bear).Tapped {
		t.Fatal("the rider did not untap the Bear")
	}
	if !e.G.Obj(cc).Tapped {
		t.Fatal("the rider untapped the Celebrant itself")
	}
	// The extra combat is pending an attackers ask: decline it (CR 508.8
	// skips the combat), so the turn walks to Main2 without another exert.
	passToKind(t, e, decision.KAttackers)
	submitNoAttackers(t, e)
	driveToStep(t, e, 3, 0, state.StepMain2)
	// An untap EFFECT untaps the exerted Celebrant: the skip flag is a
	// turn-step gate, never a TryUntap gate. (The pool is set directly, not
	// through addMana -- we are past Main1, where addMana's drive lives --
	// so this test deliberately does not replayCheck; the exert-containing
	// replay pin is the two tests above.)
	e.G.Players[0].Pool[state.MC] = 2
	var opt decision.Option
	for _, o := range e.legalActions(0) {
		if o.Kind == "ability" {
			opt = o
			break
		}
	}
	if opt.Kind != "ability" {
		t.Fatal("the Untap Wand's ability was not offered")
	}
	e.beginActivation(0, opt)
	// The activation needed no announcement ask; it sits on the stack, with
	// the ordinary priority pending beneath it. Resolve it directly (the
	// msh trigger tests' resolveTop pattern).
	e.resolveTop() // the Untap Wand's UntapAll
	if e.G.Obj(cc).Tapped {
		t.Fatal("an untap effect could not untap an exerted creature")
	}
	// Tap it again so the next untap step has a skip to show, then cross the
	// turn boundary (driveToStep answers the cleanup discard if the hand
	// runs over).
	e.emit(events.Event{Kind: events.Tap, Obj: cc})
	// The controller's next untap step is the start of their next turn --
	// turn 5 in a two-player game (turn 4 is the opponent's).
	driveToStep(t, e, 5, 0, state.StepMain1)
	// Seat 0's next untap step passed the exerted Celebrant: skipped, and
	// the window consumed exactly once.
	if !e.G.Obj(cc).Tapped {
		t.Fatal("the exerted creature untapped during the next untap step (the skip did not bite)")
	}
	if got := exertCount(e, false); got != 1 {
		t.Fatalf("%d Exert consume events, want 1", got)
	}
	if e.G.Obj(cc).ExertSkipUntap {
		t.Fatal("the skip window was not consumed at the untap step")
	}
	if e.G.Obj(cc).ExertedThisTurn {
		t.Fatal("ExertedThisTurn survived the turn boundary")
	}
}

// TestUngatedExertCarrierOffersEveryCombat pins the other half of the offer
// gate: Watchful Naga carries no IsPresent$, so it is offered in the extra
// combat too even after being exerted earlier the same turn -- and its
// Trigger$ rider (DB$ Draw) fires on the exert it accepted in the first
// combat.
func TestUngatedExertCarrierOffersEveryCombat(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := exertEngine(t, reg, lookup(t, reg, "Combat Celebrant"), lookup(t, reg, "Watchful Naga"))
	cc := moveByName(t, e, 0, "Combat Celebrant", state.ZBattlefield)
	naga := moveByName(t, e, 0, "Watchful Naga", state.ZBattlefield)
	driveToStep(t, e, 3, 0, state.StepMain1)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, cc, naga)
	// Two offers in the declaration's own order: the Celebrant first, then
	// the Naga. Accept both.
	exertPending(t, e, cc)
	submitChoices(t, e, 1)
	exertPending(t, e, naga)
	hand := len(e.G.Zone(state.ZHand, 0))
	submitChoices(t, e, 1)
	if got := exertCount(e, true); got != 2 {
		t.Fatalf("%d Exert events, want 2", got)
	}
	// Two riders from one controller (the Celebrant's untap-all+extra-combat
	// and the Naga's draw) queue for the same priority round: the drain asks
	// their order. Answer with the first ordering, then resolve the stack.
	if d := e.Pending(); d != nil && d.Kind == decision.KTriggerOrder {
		// The order decision takes a full permutation of the n options.
		idx := make([]int, 0, len(d.Options))
		for _, o := range d.Options {
			idx = append(idx, o.Index)
		}
		submitChoices(t, e, idx...)
	}
	passUntilStackEmpty(t, e, 40)
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand+1 {
		t.Fatalf("hand = %d cards, want %d (the Naga's exert draw never fired)", got, hand+1)
	}
	if len(e.G.ExtraPhases) != 1 {
		t.Fatalf("ExtraPhases = %+v, want one combat grant", e.G.ExtraPhases)
	}
	// The extra combat: the ungated Naga is offered again, the gated
	// Celebrant is not. Decline the Naga's second offer.
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, naga)
	declineExerts(t, e, cc)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after the extra combat's declaration: %+v, want priority", d)
	}
	replayCheck(t, e, cfg)
}

// TestNotExertedThisTurnPredicateIsRecognised pins the predicate's
// recognition contract: the matcher and UnknownPredicates share one
// classifier, so the gate spec's predicate is known to both and a bogus
// sibling stays unknown (the fail-closed census).
func TestNotExertedThisTurnPredicateIsRecognised(t *testing.T) {
	t.Parallel()
	if got := effects.UnknownPredicates("Creature.Self+notExertedThisTurn"); len(got) != 0 {
		t.Fatalf("UnknownPredicates(Creature.Self+notExertedThisTurn) = %v, want none", got)
	}
	if got := effects.UnknownPredicates("Creature.Self+notExertedTomorrow"); len(got) != 1 || got[0] != "notExertedTomorrow" {
		t.Fatalf("UnknownPredicates(bogus) = %v, want [notExertedTomorrow]", got)
	}
}
