package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The Motherlode, Excavator's paid attack trigger, blocker half, on the real
// corpus card. After paying {E}{E}{E}{E} and destroying a defending nonbasic
// land, its registered CantBlockBy restriction
// (`ValidBlocker$ Creature.withoutFlying+RememberedPlayerCtrl`,
// `RememberObjects$ TargetedController`) must make the DEFENDING player's
// nonflying creatures unable to block the Motherlode this turn -- creatures
// with flying stay legal. Before the TargetedController player capture (and
// the consultation's RememberedPlayers channel) the restriction registered
// with an EMPTY remembered set on both channels: its RememberedPlayerCtrl
// clause bound nobody, so a Grizzly Bears stayed an offered blocker while the
// Ornithopter's exclusion happened only by accident (it has flying, so it was
// never in scope). The destroy half is pinned separately by
// motherlode_excavator_attack_test.go; the object-remembered arm of the same
// registration is pinned by effect_cantblockby_test.go (Suspicious Bookcase).

// motherlodeBlockerEngine is motherlodeEngine's game plus a nonflying
// Grizzly Bears and a flying Ornithopter in seat 1's deck, moved onto the
// battlefield through real MoveZone events (before the Motherlode, so the
// ETB ask survives the pending reset) — the replayable way to give the
// defender blockers, which an eventless onBoard placement would break for
// replayCheck.
func motherlodeBlockerEngine(t *testing.T, reg *cards.Registry) (*Engine, Config, state.ObjID, state.ObjID, state.ObjID, []state.ObjID) {
	t.Helper()
	mountain := searchCorpusCard(t, reg, "Mountain")
	tower := searchCorpusCard(t, reg, "Reliquary Tower")
	deck := []*cards.Card{searchCorpusCard(t, reg, "The Motherlode, Excavator")}
	for len(deck) < 40 {
		deck = append(deck, mountain)
	}
	opp := []*cards.Card{tower, tower, tower, tower,
		searchCorpusCard(t, reg, "Grizzly Bears"), searchCorpusCard(t, reg, "Ornithopter")}
	for len(opp) < 40 {
		opp = append(opp, mountain)
	}
	cfg := seatZeroStart(Config{Seed: 9713, Names: []string{"ml", "def"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens, NameUniverse: reg.Cards})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	// The towers first, then the two creatures, and The Motherlode last: a
	// later mover's e.pending reset would discard the already-posed ETB
	// target ask.
	var towers []state.ObjID
	for i := 0; i < 4; i++ {
		towers = append(towers, searchMoveByNameSeat(t, e, 1, "Reliquary Tower", state.ZBattlefield))
	}
	bears := searchMoveByNameSeat(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	orn := searchMoveByNameSeat(t, e, 1, "Ornithopter", state.ZBattlefield)
	ml := searchMoveByName(t, e, "The Motherlode, Excavator", state.ZBattlefield)
	return e, cfg, ml, bears, orn, towers
}

// motherlodeBlockerETB answers The Motherlode's ETB opponent ask (seat 1) and
// asserts the ETB energy grant it feeds.
func motherlodeBlockerETB(t *testing.T, e *Engine) {
	t.Helper()
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
}

// motherlodeBlockerWalk declares seat 1's own empty attack on its turn 2 (its
// freshly placed creatures are unsick there and would otherwise be where the
// walk stops) and then The Motherlode's attack on seat 0's turn 3.
func motherlodeBlockerWalk(t *testing.T, e *Engine, ml state.ObjID) {
	t.Helper()
	passToKind(t, e, decision.KAttackers)
	if d := e.Pending(); d != nil && d.Kind == decision.KAttackers && d.Player == 1 {
		submitAttackersOnly(t, e)
		passToKind(t, e, decision.KAttackers)
	}
	submitAttackersOnly(t, e, ml)
}

func motherlodeCantBlockBy(e *Engine) (found bool, players []state.PlayerID) {
	for _, ce := range e.active() {
		if ce.Restriction == "CantBlockBy" {
			found = true
			players = ce.RememberedPlayers
		}
	}
	return found, players
}

func TestMotherlodeExcavatorPaidAttackRestrictsNonflyingBlockers(t *testing.T) {
	// t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ml, bears, orn, towers := motherlodeBlockerEngine(t, reg)

	// Preconditions: both blockers stand on the defender's battlefield and
	// are BOTH legal before the restriction exists; the values the test
	// distinguishes (flying vs nonflying) are live.
	if o := e.G.Obj(bears); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Grizzly Bears not on the battlefield: %+v", o)
	}
	if o := e.G.Obj(orn); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Ornithopter not on the battlefield: %+v", o)
	}
	if found, _ := motherlodeCantBlockBy(e); found {
		t.Fatal("precondition: a CantBlockBy restriction is already registered before the attack")
	}

	motherlodeBlockerETB(t, e)
	motherlodeBlockerWalk(t, e, ml)

	// Precondition the behaviour probe depends on: with the Motherlode
	// actually attacking and no restriction registered yet, BOTH blockers are
	// legal (canBlock reads a real attacker, so it is only meaningful now).
	if found, _ := motherlodeCantBlockBy(e); found {
		t.Fatal("precondition: a CantBlockBy restriction is registered before paying")
	}
	if !e.canBlock(bears, ml) || !e.canBlock(orn, ml) {
		t.Fatal("precondition: a blocker is already restricted before paying")
	}
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

	// Preconditions the assertion depends on: the tower really died and the
	// {E}{E}{E}{E} really left the pool, so the arm under test is the PAID
	// one.
	if o := e.G.Obj(towers[0]); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: the targeted tower zone = %+v, want the graveyard", o)
	}
	if got := e.G.Players[0].Counter("ENERGY"); got != 0 {
		t.Fatalf("precondition: energy after paying = %d, want 0", got)
	}

	// The registration really carries the defender: the captured player set
	// must hold exactly seat 1 (RememberObjects$ TargetedController -- the
	// controller of the destroyed land). This is the assertion that fails
	// on the broken build: both channels registered empty.
	found, players := motherlodeCantBlockBy(e)
	if !found {
		t.Fatal("no CantBlockBy restriction registered after the paid payout")
	}
	if len(players) != 1 || players[0] != 1 {
		t.Fatalf("registered CantBlockBy RememberedPlayers = %v, want [1] (the defending player)", players)
	}

	// The behaviour: the defender's nonflying creature can no longer block
	// the Motherlode; the flying one stays legal.
	if e.canBlock(bears, ml) {
		t.Fatal("Grizzly Bears (no flying) can still block after paying -- want false")
	}
	if !e.canBlock(orn, ml) {
		t.Fatal("Ornithopter (flying) must stay a legal blocker -- want true")
	}
	replayCheck(t, e, cfg)
}

func TestMotherlodeExcavatorDeclinedAttackLeavesBlockersLegal(t *testing.T) {
	// t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ml, bears, orn, towers := motherlodeBlockerEngine(t, reg)

	if o := e.G.Obj(bears); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Grizzly Bears not on the battlefield: %+v", o)
	}
	if o := e.G.Obj(orn); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Ornithopter not on the battlefield: %+v", o)
	}

	motherlodeBlockerETB(t, e)
	motherlodeBlockerWalk(t, e, ml)
	if !e.canBlock(bears, ml) || !e.canBlock(orn, ml) {
		t.Fatal("precondition: with the Motherlode attacking and nothing paid, a blocker is already restricted")
	}

	// Decline the optional pay ask: the trigger's payout (and its
	// restriction) must never run.
	sawPay, declined := false, false
	for i := 0; i < 40 && !declined; i++ {
		pd := e.Pending()
		if pd == nil {
			t.Fatalf("no decision while driving the payout (pay asked: %v)", sawPay)
		}
		switch {
		case pd.Kind == decision.KPriority:
			if !sawPay && len(e.G.Stack) == 0 {
				t.Fatalf("the combat window emptied with no pay ask: %+v", pd)
			}
			passFirst(t, e)
		case pd.Kind == decision.KChoose && len(pd.Options) > 1 &&
			pd.Options[0].Kind == "trigger_cost_pay" && pd.Options[1].Kind == "trigger_cost_decline":
			sawPay = true
			submitChoices(t, e, pd.Options[1].Index)
			declined = true
		case pd.Kind == decision.KTriggerOptional:
			submitChoices(t, e, 0)
		default:
			t.Fatalf("unexpected ask during the declined payout (pay asked: %v): %+v", sawPay, pd)
		}
	}
	if !sawPay {
		t.Fatal("the trigger_cost_pay ask never appeared (the decline arm was never exercised)")
	}
	if !declined {
		t.Fatal("the decline option was never submitted")
	}
	passUntilStackEmpty(t, e, 40)

	// Preconditions: nothing was paid, no land died, no restriction lives.
	if got := e.G.Players[0].Counter("ENERGY"); got != 4 {
		t.Fatalf("precondition: energy after declining = %d, want 4 (nothing may have been paid)", got)
	}
	for _, id := range towers {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: tower %d left the battlefield on the declined arm: %+v", id, o)
		}
	}
	if found, players := motherlodeCantBlockBy(e); found {
		t.Fatalf("a CantBlockBy restriction registered despite the decline (players %v)", players)
	}
	if !e.canBlock(bears, ml) {
		t.Fatal("declining the pay must leave the Grizzly Bears a legal blocker")
	}
	if !e.canBlock(orn, ml) {
		t.Fatal("declining the pay must leave the Ornithopter a legal blocker")
	}
	replayCheck(t, e, cfg)
}
