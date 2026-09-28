// CantBlockUnless (the block-prop static, rules/attack_cost.go's
// blockPairCharge/blockPayWindow): the block half of the "can't attack or
// block unless you pay {N}" family (Qal Sisma Behemoth, Oppressive Rays,
// Awesome Presence, Hipparion, Myr Prototype, Cowed by Wisdom). Pinned end
// to end through the declare-blockers decision: the per-(blocker, attacker)
// offer filter, the pool payment, the tap-payment window, the whole-
// declaration budget bound and the bot's own answer through validateBlockers.
package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// attackSeat0 makes seat 1 (the engine's active player) attack seat 0 with
// the named battlefield creatures: the engine's own per-defender event shape
// (events.DeclareAttackers' Player IS the defender -- rules/combat.go
// finishAttackers groups one event per defending player), then parks the
// game in the declare-blockers step. Every attacker must already be on seat
// 1's battlefield; the precondition is asserted, not assumed.
func attackSeat0(t *testing.T, e *Engine, attackers ...state.ObjID) {
	t.Helper()
	e.G.Active = 1
	for _, id := range attackers {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: attacker %d is not on seat 1's battlefield", id)
		}
		o.SummonSick = false
	}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: attackers})
	e.G.Step = state.StepDeclareBlockers
}

// askBlockersFresh re-opens the declare-blockers ask from scratch: the
// blockerRound is one-shot (its cursor advances past each defender), so a
// second ask with a different board needs the round state reset first.
func askBlockersFresh(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	e.blockerRound = blockerRound{}
	e.askBlockers()
	d := e.Pending()
	if d != nil && d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	return d
}

// findBlockOption returns the offered option naming (blocker, attacker), or
// nil when that pair was not offered.
func findBlockOption(d *decision.Decision, blocker, attacker state.ObjID) *decision.Option {
	for i := range d.Options {
		o := &d.Options[i]
		if o.Obj == blocker && o.Attacker == attacker {
			return o
		}
	}
	return nil
}

const memniteSrc = "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n"

// TestQalSismaBehemothBlockCharge pins the plain literal price end to end:
// with an empty pool the {2} pair is never offered, with {2} floating it is
// offered priced, and submitting it pays the pool and commits the block.
func TestQalSismaBehemothBlockCharge(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	qal := onBoardCard(t, e, 0, mshCorpusCard(t, "Qal Sisma Behemoth"))
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:0/4\nOracle:x\n")
	attackSeat0(t, e, bear)

	// Preconditions: the static prices the pair at exactly {2}, and the pool
	// (the budget the offer filter reads) really is empty.
	if got := e.blockPairCharge(qal, bear).mana; got != 2 {
		t.Fatalf("precondition: blockPairCharge = %d, want 2 (the static must price)", got)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("precondition: pool = %d, want empty", e.G.Players[0].Pool.Total())
	}
	if d := askBlockersFresh(t, e); d != nil {
		t.Fatalf("unaffordable pair offered with an empty pool: %+v", d.Options)
	}

	// {2} floating: the pair is offered, priced, and paying it commits the
	// block and empties the pool.
	floatMana(t, e, 0, "RR")
	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("affordable pair not offered with {2} floating")
	}
	if d.MaxSum != 2 {
		t.Fatalf("decision MaxSum = %d, want the {2} budget", d.MaxSum)
	}
	opt := findBlockOption(d, qal, bear)
	if opt == nil {
		t.Fatalf("pair not offered: %+v", d.Options)
	}
	if opt.Value != 2 || !strings.Contains(opt.Label, "(pay {2})") {
		t.Fatalf("pair not priced: %+v", opt)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit charged block: %v", err)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after paying the {2} block cost = %d, want 0", got)
	}
	blocked := false
	for _, b := range e.G.Obj(bear).BlockedBy {
		if b == qal {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("charged block never committed (BlockedBy %v)", e.G.Obj(bear).BlockedBy)
	}
}

// TestQalSismaBehemothBlockTapWindow pins the payment window over counted
// sources: an empty pool and two untapped Mountains ask one tap at a time,
// the block commits exactly when the pool covers {2}, and no source is
// over-tapped.
func TestQalSismaBehemothBlockTapWindow(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	qal := onBoardCard(t, e, 0, mshCorpusCard(t, "Qal Sisma Behemoth"))
	var mountains []state.ObjID
	for i := 0; i < 2; i++ {
		mountains = append(mountains, onBoardCard(t, e, 0,
			card(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")))
	}
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:0/4\nOracle:x\n")
	attackSeat0(t, e, bear)

	// Preconditions: the price is {2}, the pool is empty, and exactly two
	// one-unit sources stand behind the window.
	if got := e.blockPairCharge(qal, bear).mana; got != 2 {
		t.Fatalf("precondition: blockPairCharge = %d, want 2", got)
	}
	if e.G.Players[0].Pool.Total() != 0 || len(e.attackManaSources(0)) != 2 {
		t.Fatalf("precondition: pool %d, counted sources %d, want 0 and 2",
			e.G.Players[0].Pool.Total(), len(e.attackManaSources(0)))
	}

	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("pair not offered although the window can pay {2}")
	}
	opt := findBlockOption(d, qal, bear)
	if opt == nil {
		t.Fatalf("pair not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit block: %v", err)
	}
	for round := 0; round < 2; round++ {
		pay := e.Pending()
		if pay == nil || pay.Kind != decision.KChoose || len(pay.Options) == 0 ||
			pay.Options[0].Kind != "block_mana" {
			t.Fatalf("round %d: expected the block payment window, got %+v", round, pay)
		}
		if pay.Player != 0 {
			t.Fatalf("payment window posed to seat %d, want the defending seat 0", pay.Player)
		}
		if err := e.Submit(decision.Intent{Seq: pay.Seq, Player: pay.Player, Choices: []int{0}}); err != nil {
			t.Fatalf("tap source: %v", err)
		}
	}
	if pay := e.Pending(); pay != nil && pay.Kind == decision.KChoose && len(pay.Options) > 0 && pay.Options[0].Kind == "block_mana" {
		t.Fatalf("window should have completed at exactly {2}, still asking: %+v", pay)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after paying = %d, want 0", got)
	}
	for _, id := range mountains {
		if !e.G.Obj(id).Tapped {
			t.Fatalf("Mountain %d not tapped as the payment", id)
		}
	}
	blocked := false
	for _, b := range e.G.Obj(bear).BlockedBy {
		if b == qal {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("paid block never committed (BlockedBy %v)", e.G.Obj(bear).BlockedBy)
	}
}

// TestAwesomePresencePricesItsEnchantedAttacker pins the round-2 fix: the
// aura's static has NO ValidCard$ -- an absent one is an unconditional
// blocker match, and the Attacker$ scope alone decides which attackers the
// price rides. Before the fix the static was discarded before the Attacker$
// match, so the enchanted attacker was blockable for free.
func TestAwesomePresencePricesItsEnchantedAttacker(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:0/4\nOracle:x\n")
	wurm := onBoardCard(t, e, 1, mshCorpusCard(t, "Craw Wurm"))
	e.G.Obj(wurm).SummonSick = false
	blocker := onBoardCard(t, e, 0, card(t, memniteSrc))
	aura := onBoardCard(t, e, 0, mshCorpusCard(t, "Awesome Presence"))
	e.G.Obj(aura).AttachedTo = bear
	attackSeat0(t, e, bear, wurm)

	// Preconditions: the aura is attached to the bear on the battlefield, the
	// enchanted attacker prices {3} and the unenchanted one prices 0.
	if a := e.G.Obj(aura); a.Zone != state.ZBattlefield || a.AttachedTo != bear {
		t.Fatalf("precondition: aura zone %s attached %d, want battlefield/%d", a.Zone, a.AttachedTo, bear)
	}
	if got := e.blockPairCharge(blocker, bear).mana; got != 3 {
		t.Fatalf("precondition: charge vs the enchanted attacker = %d, want 3 (fix under test)", got)
	}
	if got := e.blockPairCharge(blocker, wurm).mana; got != 0 {
		t.Fatalf("precondition: charge vs the unenchanted attacker = %d, want 0", got)
	}

	// Empty pool: the enchanted pair is not offered; the free one is.
	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("the free pair should still be offered with an empty pool")
	}
	if findBlockOption(d, blocker, bear) != nil {
		t.Fatalf("unaffordable enchanted pair offered with an empty pool: %+v", d.Options)
	}
	free := findBlockOption(d, blocker, wurm)
	if free == nil || free.Value != 0 {
		t.Fatalf("free pair missing or priced: %+v", free)
	}

	// {3} floating: the enchanted pair is offered priced and pays from the
	// pool when submitted.
	floatMana(t, e, 0, "RRR")
	d = askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("enchanted pair not offered with {3} floating")
	}
	opt := findBlockOption(d, blocker, bear)
	if opt == nil || opt.Value != 3 {
		t.Fatalf("enchanted pair not offered at {3}: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit enchanted block: %v", err)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after paying the {3} block cost = %d, want 0", got)
	}
	blocked := false
	for _, b := range e.G.Obj(bear).BlockedBy {
		if b == blocker {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("enchanted block never committed (BlockedBy %v)", e.G.Obj(bear).BlockedBy)
	}
}

// TestHipparionPricesOnlyPowerGE3Attackers pins the Attacker$ scope on a
// self-static: Hipparion's ValidCard$ Card.Self charges only against
// power-3-or-greater attackers; the small one is always free.
func TestHipparionPricesOnlyPowerGE3Attackers(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	hip := onBoardCard(t, e, 0, mshCorpusCard(t, "Hipparion"))
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:0/4\nOracle:x\n")
	wurm := onBoardCard(t, e, 1, mshCorpusCard(t, "Craw Wurm"))
	e.G.Obj(wurm).SummonSick = false
	attackSeat0(t, e, bear, wurm)

	// Preconditions: the price splits by attacker power (wurm 6 >= 3, bear 0).
	if got := e.blockPairCharge(hip, wurm).mana; got != 1 {
		t.Fatalf("precondition: charge vs the {6} power attacker = %d, want 1", got)
	}
	if got := e.blockPairCharge(hip, bear).mana; got != 0 {
		t.Fatalf("precondition: charge vs the {0} power attacker = %d, want 0", got)
	}

	// Empty pool: the free pair is offered, the charged one is not.
	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("the free pair should still be offered with an empty pool")
	}
	if findBlockOption(d, hip, wurm) != nil {
		t.Fatalf("unaffordable pair offered with an empty pool: %+v", d.Options)
	}
	if findBlockOption(d, hip, bear) == nil {
		t.Fatalf("free pair not offered: %+v", d.Options)
	}

	// {1} floating: both pairs are offered and the charged one pays.
	floatMana(t, e, 0, "R")
	d = askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("pairs not offered with {1} floating")
	}
	opt := findBlockOption(d, hip, wurm)
	if opt == nil || opt.Value != 1 {
		t.Fatalf("charged pair not offered at {1}: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit charged block: %v", err)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after paying the {1} block cost = %d, want 0", got)
	}
	blocked := false
	for _, b := range e.G.Obj(wurm).BlockedBy {
		if b == hip {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("charged block never committed (BlockedBy %v)", e.G.Obj(wurm).BlockedBy)
	}
}

// TestMyrPrototypePricesPerCounter pins the SVar-priced shape over its own
// source: Cost$ Y with SVar:Y:Count$CardCounters.P1P1 reads the Prototype's
// own +1/+1 counters, so two counters charge {2} and zero counters block
// free even with an empty pool.
func TestMyrPrototypePricesPerCounter(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	counted := onBoardCard(t, e, 0, mshCorpusCard(t, "Myr Prototype"))
	plain := onBoardCard(t, e, 0, mshCorpusCard(t, "Myr Prototype"))
	e.emit(events.Event{Kind: events.CounterChange, Obj: counted, Counter: "P1P1", Amount: 2})
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:0/4\nOracle:x\n")
	attackSeat0(t, e, bear)

	// Preconditions: the counter really landed (the SVar reads it) and the
	// two Prototypes' prices differ.
	if e.G.Obj(counted).Counter("P1P1") != 2 {
		t.Fatalf("precondition: counted Prototype has %d P1P1 counters, want 2", e.G.Obj(counted).Counter("P1P1"))
	}
	if got, want := e.blockPairCharge(counted, bear).mana, int32(2); got != want {
		t.Fatalf("precondition: counted Prototype charges %d, want %d (SVar must resolve)", got, want)
	}
	if got := e.blockPairCharge(plain, bear).mana; got != 0 {
		t.Fatalf("precondition: plain Prototype charges %d, want 0", got)
	}

	// Empty pool: the {2} pair is not offered, the free one is.
	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("the free Prototype's pair should still be offered with an empty pool")
	}
	if findBlockOption(d, counted, bear) != nil {
		t.Fatalf("unaffordable {2} pair offered with an empty pool: %+v", d.Options)
	}
	if findBlockOption(d, plain, bear) == nil {
		t.Fatalf("free pair not offered: %+v", d.Options)
	}

	// {2} floating: the counted pair is offered priced and pays.
	floatMana(t, e, 0, "RR")
	d = askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("counted pair not offered with {2} floating")
	}
	opt := findBlockOption(d, counted, bear)
	if opt == nil || opt.Value != 2 {
		t.Fatalf("counted pair not offered at {2}: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit counted block: %v", err)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after paying the {2} block cost = %d, want 0", got)
	}
	blocked := false
	for _, b := range e.G.Obj(bear).BlockedBy {
		if b == counted {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("counted block never committed (BlockedBy %v)", e.G.Obj(bear).BlockedBy)
	}
}

// TestCowedByWisdomPricesPerHandCard pins the SVar-priced shape over the
// static's controller: Cost$ Y with SVar:Y:Count$ValidHand Card.YouOwn reads
// the enchanted creature's controller's hand, one generic per card.
func TestCowedByWisdomPricesPerHandCard(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	blocker := onBoardCard(t, e, 0, card(t, memniteSrc))
	aura := onBoardCard(t, e, 0, mshCorpusCard(t, "Cowed by Wisdom"))
	e.G.Obj(aura).AttachedTo = blocker
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:0/4\nOracle:x\n")
	attackSeat0(t, e, bear)

	// Preconditions: the bearer is where the spec reads it, the hand is
	// non-empty (the price must be distinguishable from a free block), and
	// the static prices that many generic.
	hand := len(e.G.Zone(state.ZHand, 0))
	if a := e.G.Obj(aura); a.Zone != state.ZBattlefield || a.AttachedTo != blocker {
		t.Fatalf("precondition: aura zone %s attached %d, want battlefield/%d", a.Zone, a.AttachedTo, blocker)
	}
	if hand == 0 {
		t.Fatal("precondition: the defending seat's hand is empty -- the SVar price would be indistinguishable from free")
	}
	if got := e.blockPairCharge(blocker, bear).mana; got != int32(hand) {
		t.Fatalf("precondition: charge = %d, want %d (one per hand card)", got, hand)
	}

	// Floating exactly the hand count: the pair is offered priced and the
	// pool empties on submission.
	floatMana(t, e, 0, strings.Repeat("R", hand))
	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("pair not offered although the pool covers the hand price")
	}
	opt := findBlockOption(d, blocker, bear)
	if opt == nil || opt.Value != hand {
		t.Fatalf("pair not offered at {%d}: %+v", hand, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit hand-priced block: %v", err)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after paying = %d, want 0", got)
	}
	blocked := false
	for _, b := range e.G.Obj(bear).BlockedBy {
		if b == blocker {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("hand-priced block never committed (BlockedBy %v)", e.G.Obj(bear).BlockedBy)
	}
}

// TestBlockPropDeclarationTotalBounded pins the whole-declaration belt: two
// {2} blockers under a {2} budget are each offered individually, but a
// declaration choosing both exceeds the affordable total and validateBlockers
// rejects it while keeping the pending decision answerable with one block.
func TestBlockPropDeclarationTotalBounded(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	first := onBoardCard(t, e, 0, mshCorpusCard(t, "Qal Sisma Behemoth"))
	second := onBoardCard(t, e, 0, mshCorpusCard(t, "Qal Sisma Behemoth"))
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:0/4\nOracle:x\n")
	attackSeat0(t, e, bear)
	floatMana(t, e, 0, "RR")

	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("pairs not offered although each is individually affordable")
	}
	// Preconditions: both {2} pairs are on the list and the decision carries
	// the {2} budget, so the constraint really binds on a two-block answer.
	if d.MaxSum != 2 {
		t.Fatalf("decision MaxSum = %d, want the {2} budget", d.MaxSum)
	}
	one, two := findBlockOption(d, first, bear), findBlockOption(d, second, bear)
	if one == nil || two == nil || one.Value != 2 || two.Value != 2 {
		t.Fatalf("expected two {2} pairs offered, got %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{one.Index, two.Index}}); err == nil {
		t.Fatal("a {4} declaration was accepted under a {2} budget")
	}
	if e.Pending() != d {
		t.Fatalf("rejected declaration consumed the pending decision: got %+v", e.Pending())
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{one.Index}}); err != nil {
		t.Fatalf("pending decision not answerable with one affordable block: %v", err)
	}
	blocked := 0
	for _, b := range e.G.Obj(bear).BlockedBy {
		if b == first || b == second {
			blocked++
		}
	}
	if blocked != 1 || e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("after the legal answer: %d blocks (want 1), pool %d (want 0)", blocked, e.G.Players[0].Pool.Total())
	}
}

// TestBlockPropBotAnswerNeverLivelocks runs the real bot's own answers
// through the real Submit path on a board where the budget binds: two {2}
// blockers, a lethal 6/4 attacker, {1} floating plus one Mountain (budget
// {2}). Whatever the combat heuristic wants, Clamp must trim it to a
// declaration validateBlockers accepts, the payment window must carry the
// rest, and the pending decision must always be consumed.
func TestBlockPropBotAnswerNeverLivelocks(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	var qals []state.ObjID
	for i := 0; i < 2; i++ {
		qals = append(qals, onBoardCard(t, e, 0, mshCorpusCard(t, "Qal Sisma Behemoth")))
	}
	wurm := onBoardCard(t, e, 1, mshCorpusCard(t, "Craw Wurm"))
	e.G.Obj(wurm).SummonSick = false
	mountain := onBoardCard(t, e, 0, card(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n"))
	floatMana(t, e, 0, "R")
	e.G.Players[0].Life = 2
	attackSeat0(t, e, wurm)

	d := askBlockersFresh(t, e)
	// Preconditions: the budget is {2} ({1} floating + one Mountain), both
	// {2} pairs are offered, and the attacker is lethal enough that the bot
	// must want to block.
	if d == nil {
		t.Fatal("precondition: no blockers decision although each pair is affordable")
	}
	if d.MaxSum != 2 {
		t.Fatalf("precondition: MaxSum = %d, want the {2} budget", d.MaxSum)
	}
	for _, qal := range qals {
		if o := findBlockOption(d, qal, wurm); o == nil || o.Value != 2 {
			t.Fatalf("precondition: {2} pair for blocker %d missing: %+v", qal, d.Options)
		}
	}
	if e.G.Players[0].Life != 2 {
		t.Fatal("precondition: defender must be at 2 life so the heuristic wants the block")
	}

	bot := newTestBot(7)
	for i := 0; i < 16; i++ {
		d := e.Pending()
		if d == nil || e.G.Step != state.StepDeclareBlockers || d.Kind == decision.KPriority {
			break
		}
		in := bot.answer(e, d)
		if err := e.Submit(in); err != nil {
			t.Fatalf("bot's own %v answer %v rejected by the engine (the decision is never consumed: livelock): %v",
				d.Kind, in.Choices, err)
		}
	}
	blocked := 0
	for _, b := range e.G.Obj(wurm).BlockedBy {
		for _, qal := range qals {
			if b == qal {
				blocked++
			}
		}
	}
	if blocked != 1 {
		t.Fatalf("%d Qals block the wurm, want exactly the 1 the {2} budget can pay for (BlockedBy %v)", blocked, e.G.Obj(wurm).BlockedBy)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after the bot's paid block = %d, want 0 ({1} floating + the tapped Mountain)", got)
	}
	if !e.G.Obj(mountain).Tapped {
		t.Fatal("the Mountain was not tapped for the rest of the charge")
	}
}
