package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The Spawner> control-referent chain, pinned on the real corpus card
// (spawnercontrol). The Motherlode, Excavator's attack ability pays
// {E}{E}{E}{E} and then destroys "target nonbasic land defending player
// controls" through
// `ValidTgts$ Land.nonBasic+ControlledBy Spawner>TriggeredDefendingPlayer`.
// Before the chain arm the referent grammar had no `Spawner>` case: the
// predicate classified unknown, the ValidTgts census failed closed to an
// EMPTY candidate set, and the destroy body kept today's empty-set no-op --
// the paid energy did nothing, silently. The immediate trigger's
// TriggerContext rides into the Destroy body's census, so the stripped inner
// ref `TriggeredDefendingPlayer` resolves against the same context the
// body's other specs use. Dokuchi Silencer and the six sibling corpus cards
// carry the same shape (the `Spawner>TriggeredTarget` inner refs are pinned
// at the effects level in spawner_control_referent_test.go).

// motherlodeEngine builds a two-seat game: seat 0's deck holds The Motherlode,
// Excavator over mountains; seat 1's holds four Reliquary Towers (nonbasic
// lands) over mountains. Both movers log real MoveZone events, so the flow
// replays from the log.
func motherlodeEngine(t *testing.T, reg *cards.Registry) (*Engine, Config, state.ObjID, []state.ObjID) {
	t.Helper()
	mountain := searchCorpusCard(t, reg, "Mountain")
	tower := searchCorpusCard(t, reg, "Reliquary Tower")
	deck := []*cards.Card{searchCorpusCard(t, reg, "The Motherlode, Excavator")}
	for len(deck) < 40 {
		deck = append(deck, mountain)
	}
	opp := make([]*cards.Card, 0, 40)
	for i := 0; i < 4; i++ {
		opp = append(opp, tower)
	}
	for len(opp) < 40 {
		opp = append(opp, mountain)
	}
	cfg := seatZeroStart(Config{Seed: 9713, Names: []string{"ml", "def"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens, NameUniverse: reg.Universe()})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	// The towers first: a later mover's e.pending reset would discard the
	// already-posed ETB target ask, so The Motherlode moves last.
	var towers []state.ObjID
	for i := 0; i < 4; i++ {
		towers = append(towers, searchMoveByNameSeat(t, e, 1, "Reliquary Tower", state.ZBattlefield))
	}
	ml := searchMoveByName(t, e, "The Motherlode, Excavator", state.ZBattlefield)
	return e, cfg, ml, towers
}

func TestMotherlodeExcavatorAttackPayoutDestroysDefendingNonbasicLand(t *testing.T) {
	// t.Parallel()
	reg := testutil.CorpusRegistry(t)

	// Precondition: the card really carries the chain under test.
	mlCard := searchCorpusCard(t, reg, "The Motherlode, Excavator")
	trigSA := cards.ResolveSVar(mlCard.Faces[0].SVars, "TrigDestroy")
	if trigSA == nil || trigSA.API != "Destroy" {
		t.Fatalf("corpus precondition: TrigDestroy is %+v", trigSA)
	}
	if got := trigSA.Params["ValidTgts"]; got != "Land.nonBasic+ControlledBy Spawner>TriggeredDefendingPlayer" {
		t.Fatalf("corpus precondition: ValidTgts$ = %q, want the Spawner> chain", got)
	}

	e, cfg, ml, towers := motherlodeEngine(t, reg)

	// The Motherlode's ETB asks for a target opponent; seat 1 is offered and
	// chosen, and its ETB energy grant then counts seat 1's four nonbasic
	// lands.
	d := passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want The Motherlode's opponent target ask", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("seat 1 was not offered as the opponent target: %+v", d.Options)
	}
	submitChoices(t, e, pick)
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Players[0].Counter("ENERGY"); got != 4 {
		t.Fatalf("precondition: ETB energy = %d, want 4 (four nonbasic lands under seat 1)", got)
	}

	// Preconditions: The Motherlode and all four towers stand on the
	// battlefield -- the board the attack payout reads.
	if o := e.G.Obj(ml); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: The Motherlode not on the battlefield: %+v", o)
	}
	for _, id := range towers {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: tower %d not on the battlefield: %+v", id, o)
		}
	}

	// Attack and drive the payout: declare The Motherlode, answer the
	// optional pay ask with option 0 (pay {E}{E}{E}{E}), and wait for the
	// Destroy body's target ask.
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, ml)
	sawPay, sawTarget := false, false
	for i := 0; i < 40 && !sawTarget; i++ {
		pd := e.Pending()
		if pd == nil {
			t.Fatalf("no decision while driving the payout (pay asked: %v)", sawPay)
		}
		switch {
		case pd.Kind == decision.KPriority:
			if !sawPay && len(e.G.Stack) == 0 {
				t.Fatalf("the combat window emptied with no pay ask and no destroy ask: %+v", pd)
			}
			passFirst(t, e)
		case pd.Kind == decision.KChoose && len(pd.Options) > 0 && pd.Options[0].Kind == "trigger_cost_pay":
			sawPay = true
			chooseKindOption(t, e, pd, "trigger_cost_pay")
		case pd.Kind == decision.KTarget && sawPay:
			// CR 603.12: the "when you do" half is a reflexive triggered
			// ability, so its target is chosen as it is put on the stack.
			sawTarget = true
		case pd.Kind == decision.KTriggerOptional:
			submitChoices(t, e, 0)
		default:
			t.Fatalf("unexpected ask during the payout (pay asked: %v): %+v", sawPay, pd)
		}
	}
	if !sawPay {
		t.Fatal("the trigger_cost_pay ask never appeared")
	}
	if !sawTarget {
		t.Fatal("after paying 4 energy, the Destroy body never posed its target ask (the empty-set no-op)")
	}
	pd := e.Pending()
	if pd == nil || pd.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want the destroy target ask", pd)
	}
	tpick := -1
	for _, o := range pd.Options {
		if o.Obj == towers[0] {
			tpick = o.Index
		}
	}
	if tpick < 0 {
		t.Fatalf("seat 1's Reliquary Tower not offered: %+v", pd.Options)
	}
	submitChoices(t, e, tpick)
	passUntilStackEmpty(t, e, 40)

	// The land really left the battlefield, the other three towers stayed,
	// and the paid energy really left the pool.
	if o := e.G.Obj(towers[0]); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the chosen tower zone = %+v, want the graveyard (the Destroy body never acted)", o)
	}
	for _, id := range towers[1:] {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("tower %d moved: %+v (only the targeted land must die)", id, o)
		}
	}
	if got := e.G.Players[0].Counter("ENERGY"); got != 0 {
		t.Fatalf("energy after paying = %d, want 0 (the {E}{E}{E}{E} was really paid)", got)
	}
	moved := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == towers[0] && ev.To == state.ZGraveyard {
			moved = true
		}
	}
	if !moved {
		t.Fatal("no MoveZone to the graveyard was logged for the tower")
	}
	// The chain must have RESOLVED, not failed closed loudly: no
	// unresolvable/fail-closed Note naming the Spawner> shape.
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && (strings.Contains(ev.Text, "unresolvable") || strings.Contains(ev.Text, "Spawner")) {
			t.Fatalf("fail-closed note during the payout: %q", ev.Text)
		}
	}
	replayCheck(t, e, cfg)
}
