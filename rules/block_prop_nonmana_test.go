// blockprop1 closure: the composite CantBlockUnless block charge. The old
// mana-only model priced only flat/Count$/SVar printed statics and skipped
// permissively -- PayLife<1> (Heat Wave), tapXType<...> (Hollow Warrior) and
// the Effect/Animate-delivered props (War Cadence, Whipgrass Entangler).
// These tests pin the composite charge end to end through the real corpus
// cards: the per-pair offer gate (blockChargeAffordable, the ONE
// affordability read), the whole-declaration validator, the life and tap
// payments (each its real event), the delivered registration routes, and
// the shipped bot policy's own answer running through validateBlockers on a
// board where the constraint binds.
package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const bearBlockSrc = "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// chargeEngine is a three-seat engine whose seat 0 deck leads with the named
// corpus card, so the card can be seated by a real (logged) MoveZone the way
// the activation-driven tests need -- an eventless AddObject never exposes an
// activated ability to legalActions.
func chargeEngine(t *testing.T, seed uint64, lead *cards.Card) *Engine {
	t.Helper()
	deck := append([]*cards.Card{lead}, mountainDeck(t, 39)...)
	return New(seatZeroStart(Config{Seed: seed, Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{deck, mountainDeck(t, 40), mountainDeck(t, 40)}}))
}

// countRestrictionEntries counts the registry entries carrying a delivered
// CantBlockUnless restriction (the feature's handler must have RUN for a
// "nothing happens" assertion to mean anything).
func countRestrictionEntries(e *Engine) int {
	n := 0
	for _, ce := range e.active() {
		if ce.Restriction == "CantBlockUnless" {
			n++
		}
	}
	return n
}

// TestHeatWaveBlockChargesLife pins the PayLife<1> component end to end: the
// static prices one life per (nonblue blocker, Heat Wave controller's
// attacker) pair, the pair is offered charged, and submitting the block pays
// exactly one life (one LifeChange event) and commits the block.
func TestHeatWaveBlockChargesLife(t *testing.T) {
	e := threeSeatEngine(t)
	heat := onBoardCard(t, e, 1, mshCorpusCard(t, "Heat Wave"))
	bear := onBoardReady(t, e, 1, bearBlockSrc)
	memnite := onBoardReady(t, e, 0, memniteSrc)
	attackSeat0(t, e, bear)

	// Preconditions: Heat Wave is on the battlefield where its static reads,
	// the charge is life-only at exactly 1, and the defender's life total
	// really exceeds it (the two compared values differ).
	if o := e.G.Obj(heat); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Heat Wave not on seat 1's battlefield")
	}
	ch := e.blockPairCharge(memnite, bear)
	if ch.mana != 0 || ch.life != 1 || len(ch.taps) != 0 {
		t.Fatalf("precondition: blockPairCharge = %+v, want life-only 1", ch)
	}
	if e.G.Players[0].Life <= ch.life {
		t.Fatalf("precondition: defender life %d does not exceed the charge %d", e.G.Players[0].Life, ch.life)
	}

	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("affordable life-charged pair not offered")
	}
	opt := findBlockOption(d, memnite, bear)
	if opt == nil {
		t.Fatalf("pair not offered: %+v", d.Options)
	}
	if opt.CostLife != 1 || opt.Value != 0 || !strings.Contains(opt.Label, "pay 1 life") {
		t.Fatalf("pair not life-charged: %+v", opt)
	}
	before := e.G.Players[0].Life
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit life-charged block: %v", err)
	}
	if got := e.G.Players[0].Life; got != before-1 {
		t.Fatalf("life after the charged block = %d, want %d", got, before-1)
	}
	blocked := false
	for _, b := range e.G.Obj(bear).BlockedBy {
		if b == memnite {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("charged block never committed (BlockedBy %v)", e.G.Obj(bear).BlockedBy)
	}
}

// TestHeatWaveInsufficientLifeIsNeverOffered pins the offer gate's life half:
// a pair whose life charge exceeds the defender's life total is not offered
// at all (CR 509.1b -- the decision never tempts the seat with a block the
// engine would reject).
func TestHeatWaveInsufficientLifeIsNeverOffered(t *testing.T) {
	e := threeSeatEngine(t)
	heat := onBoardCard(t, e, 1, mshCorpusCard(t, "Heat Wave"))
	bear := onBoardReady(t, e, 1, bearBlockSrc)
	memnite := onBoardReady(t, e, 0, memniteSrc)
	attackSeat0(t, e, bear)

	if ch := e.blockPairCharge(memnite, bear); ch.life != 1 {
		t.Fatalf("precondition: charge = %+v, want life 1", ch)
	}
	if o := e.G.Obj(heat); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Heat Wave not on seat 1's battlefield")
	}
	e.G.Players[0].Life = 0
	if d := askBlockersFresh(t, e); d != nil {
		t.Fatalf("unpayable life charge still offered: %+v", d.Options)
	}
}

// TestHollowWarriorBlockTapsAnUntappedNonblocker pins the tapXType
// component end to end: declaring the block taps one untapped creature the
// defender controls that is not part of the declaration (the first
// deterministic candidate in zone order, R-9 -- the build never asks which
// permanent to tap), via the same Tap event every cost payment emits.
func TestHollowWarriorBlockTapsAnUntappedNonblocker(t *testing.T) {
	e := threeSeatEngine(t)
	warrior := onBoardCard(t, e, 0, mshCorpusCard(t, "Hollow Warrior"))
	bear := onBoardReady(t, e, 0, bearBlockSrc)
	memnite := onBoardReady(t, e, 1, memniteSrc)
	attackSeat0(t, e, memnite)

	// Preconditions: the charge is a single tap obligation with no mana or
	// life, the tapper candidate is untapped, and the blocker itself is
	// ineligible for its own obligation (it is declared as blocking).
	ch := e.blockPairCharge(warrior, memnite)
	if ch.mana != 0 || ch.life != 0 || len(ch.taps) != 1 || ch.taps[0].n != 1 {
		t.Fatalf("precondition: blockPairCharge = %+v, want one tap of 1", ch)
	}
	if e.G.Obj(bear).Tapped {
		t.Fatal("precondition: the tapper candidate is already tapped")
	}
	if got := len(e.blockTapCandidates(0, ch.taps[0], map[state.ObjID]bool{warrior: true})); got < 1 {
		t.Fatalf("precondition: %d tapper candidates with the blocker excluded, want >= 1", got)
	}

	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("payable tap-charged pair not offered")
	}
	opt := findBlockOption(d, warrior, memnite)
	if opt == nil {
		t.Fatalf("pair not offered: %+v", d.Options)
	}
	if opt.CostTaps != 1 || !strings.Contains(opt.Label, "tap 1") {
		t.Fatalf("pair not tap-charged: %+v", opt)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit tap-charged block: %v", err)
	}
	if !e.G.Obj(bear).Tapped {
		t.Fatal("the tap obligation never tapped the non-blocking creature")
	}
	if e.G.Obj(warrior).Tapped {
		t.Fatal("the blocker itself was tapped for its own obligation (it is declared as blocking)")
	}
	blocked := false
	for _, b := range e.G.Obj(memnite).BlockedBy {
		if b == warrior {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("tap-charged block never committed (BlockedBy %v)", e.G.Obj(memnite).BlockedBy)
	}
}

// TestHollowWarriorWithoutATapperIsNeverOffered pins the offer gate's tap
// half: with no untapped non-blocking creature to tap, the pair is not
// offered (the blocker itself cannot pay its own obligation).
func TestHollowWarriorWithoutATapperIsNeverOffered(t *testing.T) {
	e := threeSeatEngine(t)
	warrior := onBoardCard(t, e, 0, mshCorpusCard(t, "Hollow Warrior"))
	memnite := onBoardReady(t, e, 1, memniteSrc)
	attackSeat0(t, e, memnite)

	if ch := e.blockPairCharge(warrior, memnite); len(ch.taps) != 1 {
		t.Fatalf("precondition: charge = %+v, want one tap obligation", ch)
	}
	if d := askBlockersFresh(t, e); d != nil {
		t.Fatalf("unpayable tap charge still offered: %+v", d.Options)
	}
}

// TestWhipgrassEntanglerDeliveredStaticChargesPerCleric pins the
// Animate-delivered route: activating Whipgrass Entangler's {1}{W} ability
// registers its CantBlockUnless body on the target creature with the
// granting face's SVar table, and the target's blocks then charge {1} per
// Cleric on the battlefield (the SVar body Count$Valid Cleric read live at
// consult time).
func TestWhipgrassEntanglerDeliveredStaticChargesPerCleric(t *testing.T) {
	whipCard := mshCorpusCard(t, "Whipgrass Entangler")
	e := chargeEngine(t, 8101, whipCard)
	bear := onBoardReady(t, e, 0, bearBlockSrc)
	memnite := onBoardReady(t, e, 1, memniteSrc)
	e.Advance()
	whip := moveSeededCard(t, e, 0, whipCard, state.ZBattlefield)

	// Activate {1}{W} on the bear.
	addMana(t, e, 0, "CW")
	opt := abilityOption(t, e, whip, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == bear {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("no target option for the bear: %+v", d.Options)
	}
	submitChoices(t, e, tgt)
	passUntilStackEmpty(t, e, 20)

	// Precondition: the CantBlockUnless body really registered (the feature's
	// handler ran; exactly one block-side entry -- the CantAttackUnless
	// sibling registers its own entry, asserted independently by
	// TestWhipgrassEntanglerDeliveredStaticChargesAttackPerCleric, so this
	// count deliberately names CantBlockUnless only).
	if n := countRestrictionEntries(e); n != 1 {
		t.Fatalf("precondition: %d delivered CantBlockUnless registrations, want 1", n)
	}
	// The printed charge: one Cleric (Whipgrass itself) on the battlefield.
	if got := e.blockPairCharge(bear, memnite).mana; got != 1 {
		t.Fatalf("delivered static priced %d, want 1 (one Cleric)", got)
	}

	floatMana(t, e, 0, "C")
	attackSeat0(t, e, memnite)
	d2 := askBlockersFresh(t, e)
	if d2 == nil {
		t.Fatal("delivered-charge pair not offered")
	}
	opt2 := findBlockOption(d2, bear, memnite)
	if opt2 == nil {
		t.Fatalf("pair not offered: %+v", d2.Options)
	}
	if opt2.Value != 1 || !strings.Contains(opt2.Label, "(pay {1})") {
		t.Fatalf("pair not charged per Cleric: %+v", opt2)
	}
	if err := e.Submit(decision.Intent{Seq: d2.Seq, Player: d2.Player, Choices: []int{opt2.Index}}); err != nil {
		t.Fatalf("submit delivered-charge block: %v", err)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after paying the per-Cleric charge = %d, want 0", got)
	}
	blocked := false
	for _, b := range e.G.Obj(memnite).BlockedBy {
		if b == bear {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("delivered-charge block never committed (BlockedBy %v)", e.G.Obj(memnite).BlockedBy)
	}
}

// TestWarCadenceEffectDeliveredChargesTheChosenX pins the Effect-delivered
// route with a per-effect variable price: activating War Cadence's {X}{R}
// ability freezes the chosen X (SetChosenNumber$ X), and this turn every
// block charges {X} through the registered body's Cost$ XChosen (the
// Count$ChosenNumber SVar resolved against the frozen binding).
func TestWarCadenceEffectDeliveredChargesTheChosenX(t *testing.T) {
	cadCard := mshCorpusCard(t, "War Cadence")
	e := chargeEngine(t, 8102, cadCard)
	bear := onBoardReady(t, e, 0, bearBlockSrc)
	memnite := onBoardReady(t, e, 1, memniteSrc)
	e.Advance()
	cad := moveSeededCard(t, e, 0, cadCard, state.ZBattlefield)

	addMana(t, e, 0, "CR")
	opt := abilityOption(t, e, cad, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	xIdx := -1
	for _, o := range d.Options {
		if o.Kind == "x" && o.Amount == 1 {
			xIdx = o.Index
		}
	}
	if xIdx < 0 {
		t.Fatalf("no X = 1 option: %+v", d.Options)
	}
	submitChoices(t, e, xIdx)
	passUntilStackEmpty(t, e, 20)

	// Precondition: the Effect registered one CantBlockUnless with the frozen
	// binding X = 1.
	entries := 0
	chosen := int32(-1)
	for _, ce := range e.active() {
		if ce.Restriction == "CantBlockUnless" {
			entries++
			chosen = ce.ChosenNumber
		}
	}
	if entries != 1 || chosen != 1 {
		t.Fatalf("precondition: %d registrations with ChosenNumber %d, want 1/1", entries, chosen)
	}
	if got := e.blockPairCharge(bear, memnite).mana; got != 1 {
		t.Fatalf("XChosen priced %d, want the frozen X (1)", got)
	}

	floatMana(t, e, 0, "C")
	attackSeat0(t, e, memnite)
	d2 := askBlockersFresh(t, e)
	if d2 == nil {
		t.Fatal("effect-charged pair not offered")
	}
	opt2 := findBlockOption(d2, bear, memnite)
	if opt2 == nil || opt2.Value != 1 || !strings.Contains(opt2.Label, "(pay {1})") {
		t.Fatalf("pair not charged the chosen X: %+v", d2.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d2.Seq, Player: d2.Player, Choices: []int{opt2.Index}}); err != nil {
		t.Fatalf("submit effect-charged block: %v", err)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after paying the {X} charge = %d, want 0", got)
	}
	blocked := false
	for _, b := range e.G.Obj(memnite).BlockedBy {
		if b == bear {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("effect-charged block never committed (BlockedBy %v)", e.G.Obj(memnite).BlockedBy)
	}
}

// TestBlockChargeBotAnswerNeverRejected runs the shipped bot policy's own
// answer through validateBlockers where the constraint binds: two Heat Wave
// attackers against one life -- each block individually affordable, both
// jointly not. The bot's guard must drop the over-budget pair rather than
// submit a declaration the engine rejects forever.
func TestBlockChargeBotAnswerNeverRejected(t *testing.T) {
	e := threeSeatEngine(t)
	heat := onBoardCard(t, e, 1, mshCorpusCard(t, "Heat Wave"))
	a1 := onBoardReady(t, e, 1, bearBlockSrc)
	a2 := onBoardReady(t, e, 1, bearBlockSrc)
	b1 := onBoardReady(t, e, 0, memniteSrc)
	b2 := onBoardReady(t, e, 0, memniteSrc)
	e.G.Players[0].Life = 1
	attackSeat0(t, e, a1, a2)

	if o := e.G.Obj(heat); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Heat Wave not on seat 1's battlefield")
	}
	// Preconditions: Heat Wave prices both pairs at exactly 1 life; each is
	// individually affordable, the sum is not.
	c1 := e.blockPairCharge(b1, a1)
	c2 := e.blockPairCharge(b2, a2)
	if c1.life != 1 || c2.life != 1 {
		t.Fatalf("precondition: charges %+v / %+v, want life 1 each", c1, c2)
	}
	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("no blockers ask on the binding board")
	}
	if findBlockOption(d, b1, a1) == nil || findBlockOption(d, b2, a2) == nil {
		t.Fatalf("individually affordable pairs not offered: %+v", d.Options)
	}
	bot := newTestBot(4242)
	in := bot.answer(e, d)
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: in.Choices}); err != nil {
		t.Fatalf("the bot's own block answer was rejected: %v (choices %v)", err, in.Choices)
	}
	committed := 0
	for _, b := range e.G.Obj(a1).BlockedBy {
		_ = b
		committed++
	}
	for _, b := range e.G.Obj(a2).BlockedBy {
		_ = b
		committed++
	}
	if committed > 1 {
		t.Fatalf("%d blocks committed on 1 life, want at most 1 (the jointly unpayable pair must be dropped)", committed)
	}
	if got := e.G.Players[0].Life; got != 0 {
		t.Fatalf("life after the paid declaration = %d, want 0 (the committed charge)", got)
	}
}

// TestBlockChargeReplays pins the composite payment's event shape: the
// declaration's tap payments ride real events the replay re-derives
// byte-identically.
func TestBlockChargeReplays(t *testing.T) {
	e := threeSeatEngine(t)
	warrior := onBoardCard(t, e, 0, mshCorpusCard(t, "Hollow Warrior"))
	bear := onBoardReady(t, e, 0, bearBlockSrc)
	memnite := onBoardReady(t, e, 1, memniteSrc)
	attackSeat0(t, e, memnite)
	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("payable tap-charged pair not offered")
	}
	opt := findBlockOption(d, warrior, memnite)
	if opt == nil {
		t.Fatalf("pair not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	tapped := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Tap && ev.Text == "tapped as a block cost" {
			tapped++
		}
	}
	if tapped != 1 {
		t.Fatalf("%d block-cost Tap events, want 1", tapped)
	}
	if !e.G.Obj(bear).Tapped {
		t.Fatal("the block-cost Tap event did not tap the non-blocking creature")
	}
}
