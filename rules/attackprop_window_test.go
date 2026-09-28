// CantAttackUnless (rules/attack_cost.go) payment-window membership and
// per-attacker pricing. The window used to admit only a permanent with
// EXACTLY ONE free, priceable mana ability, so a multi-ability dual land
// could not pay an attack prop at all, and the price used to be read without
// the attacking creature, so a RememberingAttacker$ static (Nils, Discipline
// Enforcer) could not price its per-attacker charge.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// dualFreeLandScript is a land with TWO free-to-tap mana abilities (a Volcanic
// Island's shape): either ability can be activated while the land is
// untapped, so the permanent is a legitimate single tap source but yields one
// unit, not two.
func dualFreeLandScript(t testing.TB) *cards.Card {
	return card(t, "Name:Dual Vale\nTypes:Land\n"+
		"A:AB$ Mana | Cost$ T | Produced$ U | Oracle:x\n"+
		"A:AB$ Mana | Cost$ T | Produced$ R | Oracle:x\n")
}

// attackPropSeatDual is attackPropSeat with the payer's mana supplied by n
// dualFreeLandScript lands instead of Mountains.
func attackPropSeatDual(t *testing.T, propName string, nDuals int) (*Engine, state.ObjID) {
	t.Helper()
	cfg := Config{Seed: 717, Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40)}}
	e := New(cfg)
	onBoardCard(t, e, 0, mshCorpusCard(t, propName))
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:0/4\nOracle:x\n")
	for i := 0; i < nDuals; i++ {
		onBoardCard(t, e, 1, dualFreeLandScript(t))
	}
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers
	return e, bear
}

// findAttackOption returns the offered pair (attacker, defender, Value) or nil.
func findAttackOption(d *decision.Decision, obj state.ObjID, def state.PlayerID) *decision.Option {
	if d == nil {
		return nil
	}
	for i := range d.Options {
		o := &d.Options[i]
		if o.Obj == obj && o.Player == def {
			return o
		}
	}
	return nil
}

// TestAttackPropPaysWithAMultiAbilityManaSource pins the window widening:
// Baird, Steward of Argive charges {1}, the payer's only source is one
// untapped land with TWO free mana abilities, and the pair must be offered,
// paid by tapping that one land, and the attack committed. Before the fix the
// membership required exactly one free ability, so the land was excluded, the
// budget was zero, and the pair was never offered.
func TestAttackPropPaysWithAMultiAbilityManaSource(t *testing.T) {
	t.Parallel()
	e, bear := attackPropSeatDual(t, "Baird, Steward of Argive", 1)

	// PRECONDITION: the payer has exactly one untapped land with two free,
	// priceable abilities, and the pool is empty so the window IS the route.
	land := e.G.Zone(state.ZBattlefield, 1)[0]
	for _, id := range e.G.Zone(state.ZBattlefield, 1) {
		if e.G.Obj(id).Face().Name == "Dual Vale" {
			land = id
		}
	}
	if n := e.windowManaUnits(1); len(n) != 1 || n[0].freeCount != 2 {
		t.Fatalf("precondition: windowManaUnits(1) = %+v, want one unit with freeCount 2", n)
	}
	if got := e.G.Players[1].Pool.Total(); got != 0 {
		t.Fatalf("precondition: pool = %d, want 0", got)
	}
	if got := len(e.attackManaSources(1)); got != 2 {
		t.Fatalf("precondition: attackManaSources(1) = %d, want 2 (one option per ability)", got)
	}

	e.askAttackers()
	d := e.Pending()
	opt := findAttackOption(d, bear, 0)
	if opt == nil {
		t.Fatalf("a multi-ability dual land cannot pay {1}, pair not offered: %+v", d.Options)
	}
	if opt.Value != 1 {
		t.Fatalf("pair priced at Value %d, want 1", opt.Value)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit charged attack: %v", err)
	}
	pay := e.Pending()
	if pay == nil || pay.Kind != decision.KChoose || len(pay.Options) == 0 ||
		pay.Options[0].Kind != "attack_mana" {
		t.Fatalf("expected the attack payment window, got %+v", pay)
	}
	if len(pay.Options) != 2 {
		t.Fatalf("window offered %d tap options, want 2 (one per ability)", len(pay.Options))
	}
	if err := e.Submit(decision.Intent{Seq: pay.Seq, Player: pay.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("tap the dual land: %v", err)
	}
	if got := e.G.Players[1].Pool.Total(); got != 0 {
		t.Fatalf("pool after paying {1} = %d, want 0 (the tax was paid, not stranded)", got)
	}
	if o := e.G.Obj(land); !o.Tapped {
		t.Fatal("the dual land was not tapped as the payment")
	}
	if o := e.G.Obj(bear); !o.IsAttacking {
		t.Fatal("charged attacker was never declared: the {1} was not paid")
	}
	drainCombatPriority(t, e)
}

// TestAttackPropPaysWithAMultiAbilityChoiceSource pins the same widening in
// the choice-shaped walk: one untapped land with TWO free "Any" abilities.
// windowManaUnits excludes choice-shaped production, so this source is
// attackChoiceManaSources' domain; before the fix it required len(free) == 1
// and dropped the land entirely.
func TestAttackPropPaysWithAMultiAbilityChoiceSource(t *testing.T) {
	t.Parallel()
	e, bear := attackPropSeatDual(t, "Baird, Steward of Argive", 0)
	anyLand := card(t, "Name:Dual Cavern\nTypes:Land\n"+
		"A:AB$ Mana | Cost$ T | Produced$ Any | Oracle:x\n"+
		"A:AB$ Mana | Cost$ T | Produced$ Any | Oracle:x\n")
	land := onBoardCard(t, e, 1, anyLand)

	// PRECONDITION: the source has two free choice-shaped abilities and the
	// shared (plain) walk intentionally reports none for it.
	if got := len(e.windowManaUnits(1)); got != 0 {
		t.Fatalf("precondition: windowManaUnits(1) = %d, want 0 (choice-shaped is not its domain)", got)
	}
	if got := len(e.attackManaSources(1)); got != 2 {
		t.Fatalf("precondition: attackManaSources(1) = %d, want 2 (one option per Any ability)", got)
	}

	e.askAttackers()
	d := e.Pending()
	opt := findAttackOption(d, bear, 0)
	if opt == nil {
		t.Fatalf("a two-Any land cannot pay {1}, pair not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit charged attack: %v", err)
	}
	pay := e.Pending()
	if pay == nil || pay.Kind != decision.KChoose || len(pay.Options) == 0 ||
		pay.Options[0].Kind != "attack_mana" {
		t.Fatalf("expected the attack payment window, got %+v", pay)
	}
	if err := e.Submit(decision.Intent{Seq: pay.Seq, Player: pay.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("tap the dual Cavern: %v", err)
	}
	if got := e.G.Players[1].Pool.Total(); got != 0 {
		t.Fatalf("pool after paying {1} = %d, want 0", got)
	}
	if o := e.G.Obj(land); !o.Tapped {
		t.Fatal("the two-Any land was not tapped as the payment")
	}
	if o := e.G.Obj(bear); !o.IsAttacking {
		t.Fatal("charged attacker was never declared")
	}
	drainCombatPriority(t, e)
}

// TestNilsPricesPerAttackerCounters pins the RememberingAttacker$ shape with
// the REAL corpus card: Nils, Discipline Enforcer's
// `S:Mode$ CantAttackUnless | ValidCard$ Creature.HasCounters | ... |
// Cost$ X | RememberingAttacker$ True` with `SVar:X:Remembered$CardCounters.ALL`
// charges each attacking creature {X} where X is THAT creature's counters.
// The representative must be priced at {1} and the counterless sibling at {0}.
func TestNilsPricesPerAttackerCounters(t *testing.T) {
	t.Parallel()
	cfg := Config{Seed: 718, Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40)}}
	e := New(cfg)
	onBoardCard(t, e, 0, mshCorpusCard(t, "Nils, Discipline Enforcer"))
	countered := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	counterless := onBoardReady(t, e, 1, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	onBoardCard(t, e, 1, card(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n"))
	e.emit(events.Event{Kind: events.CounterChange, Obj: countered, Counter: "P1P1", Amount: 1})
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers

	// PRECONDITION: the countered attacker really carries one counter, the
	// static source is on seat 0's battlefield, and the compared values
	// actually differ (the counterless sibling must be free).
	if got := e.G.Obj(countered).Counter("P1P1"); got != 1 {
		t.Fatalf("precondition: countered bear has %d counters, want 1", got)
	}
	if got := e.G.Obj(counterless).Counter("P1P1"); got != 0 {
		t.Fatalf("precondition: counterless bear has %d counters, want 0", got)
	}
	if e.G.Obj(countered).Zone != state.ZBattlefield || e.G.Obj(counterless).Zone != state.ZBattlefield {
		t.Fatal("precondition: both attackers must be on the battlefield")
	}

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	cOpt := findAttackOption(d, countered, 0)
	if cOpt == nil {
		t.Fatalf("countered attacker's pair not offered (one Mountain can pay {1}): %+v", d.Options)
	}
	if cOpt.Value != 1 {
		t.Fatalf("countered attacker priced at Value %d, want 1 (its one counter)", cOpt.Value)
	}
	fOpt := findAttackOption(d, counterless, 0)
	if fOpt == nil {
		t.Fatalf("counterless attacker's pair not offered: %+v", d.Options)
	}
	if fOpt.Value != 0 {
		t.Fatalf("counterless attacker priced at Value %d, want 0 (ValidCard$ Creature.HasCounters excludes it)", fOpt.Value)
	}
	drainCombatPriority(t, e)
}

// TestAttackPropSingleDualLandCannotStretchToTwo pins the wedge guard against
// the widened membership: one permanent offers TWO alternatives but taps only
// ONCE, so it produces one unit, not two. Ghostly Prison charges {2}; with a
// single dual land whose two free abilities each make one unit, the budget is
// 1 and the pair must NOT be offered. If attackBudget summed the alternatives
// it would report 2 and offer the pair, then the window would tap the land
// once (1 unit), find no source left, and ABORT the declaration -- the
// stranding defect the wedge guard exists to prevent.
func TestAttackPropSingleDualLandCannotStretchToTwo(t *testing.T) {
	t.Parallel()
	e, bear := attackPropSeatDual(t, "Ghostly Prison", 1)

	// PRECONDITION: the land really offers two alternatives and the pool is
	// empty, so the only question is whether those alternatives are summed.
	if got := len(e.attackManaSources(1)); got != 2 {
		t.Fatalf("precondition: attackManaSources(1) = %d, want 2 alternatives", got)
	}
	if got := e.G.Players[1].Pool.Total(); got != 0 {
		t.Fatalf("precondition: pool = %d, want 0", got)
	}
	if got := e.attackBudget(1); got != 1 {
		t.Fatalf("attackBudget(1) = %d, want 1 (one permanent taps once)", got)
	}

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	if opt := findAttackOption(d, bear, 0); opt != nil {
		t.Fatalf("a single two-ability land was offered for {2} (it cannot reach it): %+v", opt)
	}
	if o := e.G.Obj(bear); o.IsAttacking {
		t.Fatal("attacker declared without being submitted")
	}
}
