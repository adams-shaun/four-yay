// Delivered CantAttackUnless: the attack-prop registration and consultation
// for Effect-delivered (Sivitri, Dragon Master's +1; Forbidding Spirit;
// Summon: Yojimbo; War Tax) and Animate-delivered (Whipgrass Entangler)
// bodies. Before this the mode was not in effEffect's restriction switch, so
// Sivitri's +1 registered nothing and the attack was free; even a registered
// body was invisible because attackPairCharge walked only the printed
// activeStatics list. These tests pin the real corpus cards end to end
// through the engine's own activation and declare-attackers decisions.
package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// deliveredCantAttackEntries counts the registry entries carrying a delivered
// CantAttackUnless restriction -- the feature's handler must have RUN for a
// "nothing happens" assertion to mean anything.
func deliveredCantAttackEntries(e *Engine) int {
	n := 0
	for _, ce := range e.active() {
		if ce.Restriction == "CantAttackUnless" {
			n++
		}
	}
	return n
}

// driveToOwnTurn drives to the next main1 of seat `active` strictly after
// `afterTurn`, answering the decisions a turn crossing poses without
// attacking (empty declarations), so no combat damage can end the game early
// and the turn clock is the only thing that moves. Cleanup discards are
// answered through answerIfDiscard (a multi-choice ask driveToStepAll's
// single-choice arm cannot satisfy).
func driveToOwnTurn(t *testing.T, e *Engine, afterTurn int32, active state.PlayerID) {
	t.Helper()
	for i := 0; i < 8000; i++ {
		if e.G.Turn > afterTurn && e.G.Active == active && e.G.Step == state.StepMain1 {
			return
		}
		if e.G.Over {
			t.Fatalf("game ended before reaching seat %d's next turn after %d", active, afterTurn)
		}
		if answerIfDiscard(t, e) {
			continue
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e)
		case decision.KAttackers, decision.KBlockers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		default:
			n := d.Min
			if n > len(d.Options) {
				t.Fatalf("decision %+v wants %d choices, only %d offered", d.Kind, n, len(d.Options))
			}
			choices := make([]int, 0, n)
			for j := 0; j < n; j++ {
				choices = append(choices, d.Options[j].Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		}
	}
	t.Fatalf("did not reach seat %d's next turn after %d", active, afterTurn)
}

// activateSivitriPlusOne seats Sivitri via a real logged move, activates the
// +1 through the engine's own priority decision, and drains it off the stack.
// It asserts the precondition that Sivitri really accumulated a loyalty
// counter (the +1 resolved), not merely that an option existed.
func activateSivitriPlusOne(t *testing.T, e *Engine, sivitri *cards.Card) state.ObjID {
	t.Helper()
	e.Advance()
	id := moveSeededCard(t, e, 0, sivitri, state.ZBattlefield)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Face().Loyalty == "" {
		t.Fatalf("precondition: Sivitri is not a planeswalker on seat 0's battlefield")
	}
	addMana(t, e, 0, "")
	before := e.G.Obj(id).Counter("LOYALTY")
	submitChoices(t, e, abilityOption(t, e, id, 0).Index)
	passUntilStackEmpty(t, e, 30)
	if after := e.G.Obj(id).Counter("LOYALTY"); after != before+1 {
		t.Fatalf("precondition: Sivitri +1 loyalty %d -> %d, want +1", before, after)
	}
	return id
}

// TestSivitriDeliveredCantAttackUnlessChargesLife pins the whole delivered
// route on the real Sivitri, Dragon Master: the +1 registers a live
// CantAttackUnless restriction (not a Note), the tax is 2 life per attacking
// creature against Sivitri's controller AND their planeswalker, an unrelated
// defender is untaxed, a payer who cannot afford the life is never offered
// the pair, and the tax expires at the beginning of the controller's next
// turn.
func TestSivitriDeliveredCantAttackUnlessChargesLife(t *testing.T) {
	t.Parallel()
	sivCard := mshCorpusCard(t, "Sivitri, Dragon Master")
	// A second planeswalker Sivitri's controller controls, so the
	// Planeswalker.YouCtrl half of the Target$ is exercised against a real
	// walker while an unrelated player (seat 2) remains untaxed.
	walkerSrc := "Name:Sivitri's Second Walker\nManaCost:2\nTypes:Legendary Planeswalker Test\nLoyalty:3\nOracle:x\n"

	e := chargeEngine(t, 9101, sivCard)
	siv := activateSivitriPlusOne(t, e, sivCard)
	walker := onBoardCard(t, e, 0, card(t, walkerSrc))
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	// Precondition: the +1 registered a LIVE CantAttackUnless restriction
	// rather than falling to the unimplemented Note, and the restriction is
	// on seat 0 (Sivitri's controller).
	if n := deliveredCantAttackEntries(e); n != 1 {
		t.Fatalf("precondition: %d delivered CantAttackUnless registrations, want 1 (the +1 must register, not Note)", n)
	}
	// Sivitri and its sibling walker are both on the battlefield where the
	// restriction's Target$ reads them.
	if o := e.G.Obj(siv); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Sivitri left the battlefield")
	}
	if o := e.G.Obj(walker); o == nil || o.Zone != state.ZBattlefield || !o.Face().IsPlaneswalker() {
		t.Fatal("precondition: the second planeswalker is not on seat 0's battlefield")
	}

	// The tax: 2 life against Sivitri's controller, and none against seat 2.
	charged := e.attackPairCharge(bear, 0, 0)
	if charged.life != 2 || charged.mana != 0 || len(charged.taps) != 0 || charged.unpriceable {
		t.Fatalf("attackPairCharge(bear, Sivitri's controller) = %+v, want life-only 2", charged)
	}
	if free := e.attackPairCharge(bear, 2, 0); !free.zero() || free.unpriceable {
		t.Fatalf("attackPairCharge(bear, unrelated seat 2) = %+v, want zero (the Target$ names only Sivitri's controller and their walkers)", free)
	}
	// The two compared values really differ: the payer's life exceeds the tax.
	if e.G.Players[1].Life <= charged.life {
		t.Fatalf("precondition: payer life %d does not exceed the charge %d", e.G.Players[1].Life, charged.life)
	}

	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	// The player pair and the planeswalker pair are both offered, both
	// charged the same 2 life; the unrelated defender's pair is not offered
	// at all (it does not exist for this attacker on this board -- assert
	// absence by defender).
	optPlayer := findAttackOption(d, bear, 0)
	if optPlayer == nil || optPlayer.CostLife != 2 || !strings.Contains(optPlayer.Label, "pay 2 life") {
		t.Fatalf("Sivitri's controller's pair not life-charged: %+v", d.Options)
	}
	var optWalker *decision.Option
	for i := range d.Options {
		o := &d.Options[i]
		if o.Obj == bear && o.Player == 0 && o.Battle == walker {
			optWalker = o
		}
	}
	if optWalker == nil {
		t.Fatalf("the planeswalker pair was not offered (Target$ Planeswalker.YouCtrl): %+v", d.Options)
	}
	if optWalker.CostLife != 2 {
		t.Fatalf("planeswalker pair charged %d life, want 2", optWalker.CostLife)
	}
	if optSeat2 := findAttackOption(d, bear, 2); optSeat2 != nil && optSeat2.CostLife != 0 {
		t.Fatalf("unrelated defender pair charged %d life, want 0: %+v", optSeat2.CostLife, optSeat2)
	}

	// Paying the tax commits the attack and spends exactly 2 life.
	before := e.G.Players[1].Life
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{optPlayer.Index}}); err != nil {
		t.Fatalf("submit life-charged attack: %v", err)
	}
	if got := e.G.Players[1].Life; got != before-2 {
		t.Fatalf("life after the charged attack = %d, want %d", got, before-2)
	}
	if o := e.G.Obj(bear); o == nil || !o.IsAttacking {
		t.Fatal("the life-charged attacker was never declared")
	}
	drainCombatPriority(t, e)

	// Expiry: UntilYourNextTurn ends at the beginning of seat 0's next turn
	// (the third turn after this one at a three-seat table: seat 1, seat 2,
	// then seat 0 again). Drive there; the restriction must be gone, so the
	// same pair now charges nothing.
	boundary := e.G.Turn
	driveToOwnTurn(t, e, boundary, 0)
	if live := deliveredCantAttackEntries(e); live != 0 {
		t.Fatalf("precondition: the restriction survived into seat 0's next turn (%d entries)", live)
	}
	if after := e.attackPairCharge(bear, 0, 0); !after.zero() {
		t.Fatalf("tax still charged after expiry: %+v", after)
	}
}

// TestWarTaxDeliveredCantAttackUnlessChargesChosenX pins the Effect-delivered
// variable price: activating War Tax's {X}{U} freezes the chosen X
// (SetChosenNumber$ X) and this turn every attack charges {X} through the
// registered body's Cost$ XChosen (the Count$ChosenNumber SVar resolved
// against the frozen binding). The zero/default and the selected prices
// differ, so a missed binding cannot pass.
func TestWarTaxDeliveredCantAttackUnlessChargesChosenX(t *testing.T) {
	t.Parallel()
	tax := mshCorpusCard(t, "War Tax")
	e := chargeEngine(t, 9102, tax)
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.Advance()
	taxID := moveSeededCard(t, e, 0, tax, state.ZBattlefield)

	addMana(t, e, 0, "CCCU")
	submitChoices(t, e, abilityOption(t, e, taxID, 0).Index)
	d := e.Pending()
	xIdx := -1
	for _, o := range d.Options {
		if o.Kind == "x" && o.Amount == 3 {
			xIdx = o.Index
		}
	}
	if xIdx < 0 {
		t.Fatalf("no X = 3 option: %+v", d.Options)
	}
	submitChoices(t, e, xIdx)
	passUntilStackEmpty(t, e, 30)

	// Precondition: the Effect registered one CantAttackUnless with the
	// frozen binding X = 3.
	entries := 0
	chosen := int32(-1)
	for _, ce := range e.active() {
		if ce.Restriction == "CantAttackUnless" {
			entries++
			chosen = ce.ChosenNumber
		}
	}
	if entries != 1 || chosen != 3 {
		t.Fatalf("precondition: %d registrations with ChosenNumber %d, want 1/3", entries, chosen)
	}
	// The zero/default price (0, an unbound Count$ChosenNumber read) and the
	// frozen price (3) differ, so a missing binding fails here.
	if got := e.attackPairCharge(bear, 0, 0).mana; got != 3 {
		t.Fatalf("XChosen priced %d, want the frozen X (3)", got)
	}

	floatMana(t, e, 1, "CCC")
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers
	e.askAttackers()
	d2 := e.Pending()
	opt := findAttackOption(d2, bear, 0)
	if opt == nil || opt.Value != 3 || !strings.Contains(opt.Label, "(pay {3} per creature)") {
		t.Fatalf("pair not charged the chosen X: %+v", d2.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d2.Seq, Player: d2.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit effect-charged attack: %v", err)
	}
	if got := e.G.Players[1].Pool.Total(); got != 0 {
		t.Fatalf("pool after paying the chosen-X attack tax = %d, want 0", got)
	}
	if o := e.G.Obj(bear); o == nil || !o.IsAttacking {
		t.Fatal("the chosen-X-charged attacker was never declared")
	}
	drainCombatPriority(t, e)
}

// TestWhipgrassEntanglerDeliveredStaticChargesAttackPerCleric pins the
// Animate-delivered attack half of Whipgrass Entangler: activating its {1}{W}
// ability registers BOTH its CantBlockUnless directive AND its
// CantAttackUnless sibling on the target, and the target's attacks then charge
// {1} per Cleric on the battlefield (the SVar body Count$Valid Cleric read
// live at consult time).
func TestWhipgrassEntanglerDeliveredStaticChargesAttackPerCleric(t *testing.T) {
	t.Parallel()
	whipCard := mshCorpusCard(t, "Whipgrass Entangler")
	e := chargeEngine(t, 9103, whipCard)
	bear := onBoardReady(t, e, 1, bearBlockSrc)
	e.Advance()
	whip := moveSeededCard(t, e, 0, whipCard, state.ZBattlefield)

	// Activate {1}{W} on the bear (seat 1's creature).
	addMana(t, e, 0, "CW")
	submitChoices(t, e, abilityOption(t, e, whip, 0).Index)
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
	passUntilStackEmpty(t, e, 30)

	// Precondition: the CantAttackUnless sibling really registered on the
	// bear, with the granting face's SVar table; exactly one, so the block
	// sibling cannot be miscounted here.
	if n := deliveredCantAttackEntries(e); n != 1 {
		t.Fatalf("precondition: %d delivered CantAttackUnless registrations, want 1", n)
	}
	// The printed charge: one Cleric (Whipgrass itself) on the battlefield.
	if got := e.attackPairCharge(bear, 0, 0).mana; got != 1 {
		t.Fatalf("delivered attack static priced %d, want 1 (one Cleric)", got)
	}

	floatMana(t, e, 1, "C")
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers
	e.askAttackers()
	d2 := e.Pending()
	if d2 == nil || d2.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d2)
	}
	opt := findAttackOption(d2, bear, 0)
	if opt == nil || opt.Value != 1 || !strings.Contains(opt.Label, "(pay {1} per creature)") {
		t.Fatalf("pair not charged per Cleric: %+v", d2.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d2.Seq, Player: d2.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit delivered-charge attack: %v", err)
	}
	if got := e.G.Players[1].Pool.Total(); got != 0 {
		t.Fatalf("pool after paying the per-Cleric attack charge = %d, want 0", got)
	}
	if o := e.G.Obj(bear); o == nil || !o.IsAttacking {
		t.Fatal("the per-Cleric-charged attacker was never declared")
	}
	drainCombatPriority(t, e)
}

// TestSivitriDeliveredInsufficientLifeIsNeverOffered pins the fail-closed
// half on the real card: a payer who cannot pay the 2 life never sees the
// pair, and Sivitri's +1 really registered.
func TestSivitriDeliveredInsufficientLifeIsNeverOffered(t *testing.T) {
	t.Parallel()
	sivCard := mshCorpusCard(t, "Sivitri, Dragon Master")
	e := chargeEngine(t, 9104, sivCard)
	activateSivitriPlusOne(t, e, sivCard)
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	if n := deliveredCantAttackEntries(e); n != 1 {
		t.Fatalf("precondition: %d delivered CantAttackUnless registrations, want 1", n)
	}
	ch := e.attackPairCharge(bear, 0, 0)
	if ch.life != 2 {
		t.Fatalf("precondition: charge = %+v, want life 2", ch)
	}
	e.G.Players[1].Life = 1
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers
	e.askAttackers()
	d := e.Pending()
	// The pair must not be offered at Sivitri's controller: no option paying
	// into seat 0. A decline/pass decision may exist, so only the charged
	// option's absence is asserted.
	if d != nil && d.Kind == decision.KAttackers {
		if opt := findAttackOption(d, bear, 0); opt != nil {
			t.Fatalf("unpayable life charge still offered: %+v", opt)
		}
	}
}

// TestSivitriDeliveredRegistrationEmitsNoUnimplementedNote pins the negative:
// resolving the +1 must NOT emit the "continuous effect CantAttackUnless
// unimplemented" Note that the pre-fix registration branch produced, and must
// instead register a live restriction.
func TestSivitriDeliveredRegistrationEmitsNoUnimplementedNote(t *testing.T) {
	t.Parallel()
	sivCard := mshCorpusCard(t, "Sivitri, Dragon Master")
	e := chargeEngine(t, 9105, sivCard)
	e.Advance()
	id := moveSeededCard(t, e, 0, sivCard, state.ZBattlefield)
	addMana(t, e, 0, "")
	submitChoices(t, e, abilityOption(t, e, id, 0).Index)
	passUntilStackEmpty(t, e, 30)

	if n := deliveredCantAttackEntries(e); n != 1 {
		t.Fatalf("precondition: %d delivered CantAttackUnless registrations, want 1", n)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "CantAttackUnless") {
			t.Fatalf("registration emitted an unimplemented Note: %q", ev.Text)
		}
	}
}
