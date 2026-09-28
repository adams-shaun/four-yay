package rules

// The real-corpus pin for param:api:ChooseCard.WithTotalPower: Slaughter the
// Strong casts for real and each chooser's ask carries the cumulative power
// budget as Decision.MaxSum over the options' Value (the same wire contract
// Dig's WithTotalCMC$ uses). Two halves of the cap are pinned: the budget
// NARROWS THE POOL (a creature whose own power exceeds the cap is never
// offered -- the divergence the ticket names) and the budget BINDS THE SUM
// (Decision.Validate's MaxSum contract rejects a pick whose total power
// exceeds the cap). The chained SacrificeAll Creature.!ChosenCard then
// sacrifices everything the chooser did not keep, so the cap is visible on
// the board.
//
// The real script carries no Choices$ or ControlledByPlayer$, so its implicit
// candidate pool is each chooser's own battlefield objects. Seat 1 therefore
// has a qualifying creature too: this pins the second ask's budget without
// relying on the old cross-controller pool to manufacture an ask from seat 0's
// objects.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

const (
	// The corpus card's own shape plus the mandatory two-pick bounds the real
	// script lacks (the corpus Amount default is one optional pick):
	// Mandatory$ True + Amount$ 2 is what makes the CUMULATIVE half of the
	// cap bind through Decision.Validate, and Defined$ You keeps the walk to
	// one chooser so the mandatory bounds are pinned without leaning on the
	// cross-controller pool question.
	sttMandSrc = "Name:Stt Mand\nTypes:Sorcery\n" +
		"A:SP$ ChooseCard | Defined$ You | WithTotalPower$ 4 | Amount$ 2 | Mandatory$ True | Reveal$ True | SubAbility$ SacAllOthers | SpellDescription:x\n" +
		"SVar:SacAllOthers:DB$ SacrificeAll | ValidCards$ Creature.!ChosenCard\n" +
		"Oracle:x\n"

	// The same shape under a TIGHTER cap: pool {5,3,1} leaves only {3} and
	// {1} individually affordable and NO two-pick set fits (3+1 = 4 > 3), so
	// the mandatory Min is lowered to the forced take's count -- without the
	// lowering the ask would demand 2 picks and have NO legal answer.
	sttMand3Src = "Name:Stt Mand 3\nTypes:Sorcery\n" +
		"A:SP$ ChooseCard | Defined$ You | WithTotalPower$ 3 | Amount$ 2 | Mandatory$ True | Reveal$ True | SubAbility$ SacAllOthers | SpellDescription:x\n" +
		"SVar:SacAllOthers:DB$ SacrificeAll | ValidCards$ Creature.!ChosenCard\n" +
		"Oracle:x\n"
)

// sttCreature asserts the named creature is STILL on seat p's battlefield
// (the sweep assertion depends on the pre-sweep board, so the precondition
// is checked, not assumed).
func sttCreature(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	t.Fatalf("creature %q not on seat %d's battlefield", name, p)
	return 0
}

// sttEngine builds a game whose seat-0 extras are the spell plus one
// creature per (name, PT) pair, seat-1 extras one creature per pair, the
// creatures moved to the battlefield in the given order, and asserts each
// precondition (the sweep assertions depend on the board).
func sttEngine(t *testing.T, reg *cards.Registry, spell *cards.Card, seat0, seat1 [][2]string) (*Engine, [2][]state.ObjID) {
	t.Helper()
	mk := func(pairs [][2]string) []*cards.Card {
		out := make([]*cards.Card, 0, len(pairs)+1)
		if spell != nil {
			out = append(out, spell)
		}
		for _, pr := range pairs {
			out = append(out, card(t, "Name:"+pr[0]+"\nTypes:Creature\nPT:"+pr[1]+"\nOracle:x\n"))
		}
		return out
	}
	e, _ := corpusEngineCfg(t, reg, mk(seat0), mk(seat1))
	var board [2][]state.ObjID
	for p, pairs := range [2][][2]string{seat0, seat1} {
		for _, pr := range pairs {
			id := moveByName(t, e, state.PlayerID(p), pr[0], state.ZBattlefield)
			if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("precondition: %q not on seat %d's battlefield", pr[0], p)
			}
			board[p] = append(board[p], id)
		}
	}
	return e, board
}

// sttCast casts the named spell from seat 0's hand.
func sttCast(t *testing.T, e *Engine, name string, mana string) {
	t.Helper()
	targ := moveByName(t, e, 0, name, state.ZHand)
	if mana != "" {
		addMana(t, e, 0, mana)
	} else {
		// No mana to add: still refresh the pending decision, so the cast
		// options reflect every fixture the setup moved since the last ask
		// (the same re-ask addMana's trailing priorityRound performs).
		toMain1(t, e)
		e.priorityRound()
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("before the cast, pending = %+v, want priority", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == targ {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %s: %+v", name, d.Options)
	}
	submitChoices(t, e, idx)
}

// sttAskOptions maps a pending choice ask's options to (id, price) pairs.
func sttAskOptions(d *decision.Decision) ([]state.ObjID, []int) {
	ids, vals := make([]state.ObjID, 0, len(d.Options)), make([]int, 0, len(d.Options))
	for _, o := range d.Options {
		ids = append(ids, o.Obj)
		vals = append(vals, o.Value)
	}
	return ids, vals
}

func TestSlaughterTheStrongTotalPowerCapNarrowsThePool(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	spell := lookup(t, reg, "Slaughter the Strong")
	// Seat 0: a 5/5 (alone over the cap), a 3/3, a 2/2 and a 1/1. Seat 1
	// has a qualifying 2/2 so its own chooser-controlled pool produces a real
	// second ask rather than borrowing seat 0's creatures.
	e, board := sttEngine(t, reg, spell,
		[][2]string{{"Stt Giant", "5/5"}, {"Stt Three", "3/3"}, {"Stt Two A", "2/2"}, {"Stt One", "1/1"}},
		[][2]string{{"Stt Seat One Two", "2/2"}})
	sttCast(t, e, "Slaughter the Strong", "1WW")

	// Seat 0's ask: the budget rides the decision (MaxSum 4, Budgeted so the
	// cap reads even at 0), the pick is optional (Min 0, corpus Amount
	// default 1 -> Max 1), and the pool is the affordable creatures ONLY --
	// the 5-power Giant is on the battlefield but exceeds the cap alone, so
	// offering it would be the exact divergence the ticket names.
	d := passUntilAskKind(t, e, decision.KChoose, 200)
	if d.ResumeKind != "choice" || d.Player != 0 || d.Min != 0 || d.Max != 1 {
		t.Fatalf("seat 0 ask = %+v, want an optional single choice for seat 0", d)
	}
	if !d.HasBudget() || d.MaxSum != 4 {
		t.Fatalf("seat 0 ask carries no 4-power budget: MaxSum %d Budgeted %v", d.MaxSum, d.Budgeted)
	}
	wantPool := []state.ObjID{board[0][1], board[0][2], board[0][3]} // powers 3,2,1
	ids, vals := sttAskOptions(d)
	if len(ids) != len(wantPool) {
		t.Fatalf("seat 0 options = %v (prices %v), want the affordable pool %v (the 5-power Giant must be excluded)", ids, vals, wantPool)
	}
	for i, want := range wantPool {
		if ids[i] != want {
			t.Fatalf("seat 0 option %d = %d, want %d", i, ids[i], want)
		}
		if vals[i] == 0 {
			t.Fatalf("seat 0 option %d carries no Value -- a budget ask must price every option", i)
		}
	}
	submitCardChoice(t, e, d, board[0][1]) // keep the 3/3

	// Seat 1's ask: Defined$ Player asks every player. The ask carries THEIR
	// own 4-power budget and their qualifying 2/2 is affordable. Decline (Min
	// 0 makes the empty answer legal).
	d = passUntilAskKind(t, e, decision.KChoose, 200)
	if d.Player != 1 || !d.HasBudget() || d.MaxSum != 4 {
		t.Fatalf("seat 1 ask = %+v, want seat 1's own 4-power budget", d)
	}
	for _, o := range d.Options {
		if o.Value > 4 {
			t.Fatalf("seat 1 offered option %d priced %d -- over the cap", o.Obj, o.Value)
		}
	}
	submitChoices(t, e)
	passUntilStackEmpty(t, e, 200)

	// The chained SacrificeAll Creature.!ChosenCard swept every unchosen
	// creature: only the picked 3/3 survives -- the over-the-cap Giant
	// included, which is the sweep the unenforced cap used to let players
	// dodge.
	sttCreature(t, e, 0, "Stt Three")
	if n := countBattlefieldCreatures(e, 0); n != 1 {
		t.Fatalf("seat 0 has %d creatures after the sweep, want 1", n)
	}
	for _, id := range []state.ObjID{board[0][0], board[0][2], board[0][3]} {
		if o := e.G.Obj(id); o != nil && o.Zone == state.ZBattlefield {
			t.Fatalf("creature %d (over the cap or unchosen) survived the sweep", id)
		}
	}
}

func TestChooseCardTotalPowerMandatoryTwoPickValidatesTheSum(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, board := sttEngine(t, reg, card(t, sttMandSrc),
		[][2]string{{"Stt Giant", "5/5"}, {"Stt Three", "3/3"}, {"Stt Two A", "2/2"}, {"Stt Two B", "2/2"}, {"Stt One", "1/1"}},
		[][2]string{{"Stt Their Giant", "5/5"}})
	sttCast(t, e, "Stt Mand", "")
	d := passUntilAskKind(t, e, decision.KChoose, 200)
	if d.ResumeKind != "choice" || d.Player != 0 || d.Min != 2 || d.Max != 2 {
		t.Fatalf("ask = %+v, want a mandatory two-pick choice for seat 0", d)
	}
	if !d.HasBudget() || d.MaxSum != 4 {
		t.Fatalf("ask carries no 4-power budget: MaxSum %d Budgeted %v", d.MaxSum, d.Budgeted)
	}
	wantPool := []state.ObjID{board[0][1], board[0][2], board[0][3], board[0][4]} // powers 3,2,2,1
	ids, _ := sttAskOptions(d)
	if len(ids) != len(wantPool) {
		t.Fatalf("options = %v, want %v", ids, wantPool)
	}
	for i, want := range wantPool {
		if ids[i] != want {
			t.Fatalf("option %d = %d, want %d", i, ids[i], want)
		}
	}

	// The sum rule's one home (Decision.Validate) rejects a pick whose total
	// power exceeds the budget and accepts one that fits: 3+2 = 5 > 4 is the
	// exact over-the-cap answer the ticket names, 3+1 = 4 is legal.
	illegal := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}
	if err := d.Validate(illegal); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("Validate(3+2 = 5) = %v, want a budget rejection", err)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 3}}); err != nil {
		t.Fatalf("Validate(3+1 = 4) = %v, want nil", err)
	}
	submitChoices(t, e, 0, 3) // keep the 3/3 and the 1/1
	passUntilStackEmpty(t, e, 200)

	// Seat 0 kept the 3/3 and the 1/1 (sum 4); the bears and both over-cap
	// 5/5s went to the chained SacrificeAll (its ValidCards$ has no
	// controller filter, so every unchosen creature dies).
	sttCreature(t, e, 0, "Stt Three")
	sttCreature(t, e, 0, "Stt One")
	for _, id := range []state.ObjID{board[0][0], board[0][2], board[0][3], board[1][0]} {
		if o := e.G.Obj(id); o != nil && o.Zone == state.ZBattlefield {
			t.Fatalf("creature %d (over the cap or unchosen) survived the sweep", id)
		}
	}
}

func TestChooseCardTotalPowerLowensAnImpossibleMandatoryMin(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, board := sttEngine(t, reg, card(t, sttMand3Src),
		[][2]string{{"Stt Giant", "5/5"}, {"Stt Three", "3/3"}, {"Stt One", "1/1"}}, nil)
	sttCast(t, e, "Stt Mand 3", "")
	// Affordable pool {3,1}; the forced take fits only {3} (3+1 = 4 > 3), so
	// the mandatory Min drops from 2 to 1 -- a Min-2 ask would have NO legal
	// answer and livelock the seat. Max stays at the affordable pool's size.
	d := passUntilAskKind(t, e, decision.KChoose, 200)
	if d.ResumeKind != "choice" || d.Player != 0 || d.Min != 1 || d.Max != 2 || d.MaxSum != 3 {
		t.Fatalf("ask = %+v, want Min 1 (lowered from 2), Max 2, budget 3", d)
	}
	submitChoices(t, e, 0) // keep the 3/3
	passUntilStackEmpty(t, e, 200)
	sttCreature(t, e, 0, "Stt Three")
	for _, id := range []state.ObjID{board[0][0], board[0][2]} {
		if o := e.G.Obj(id); o != nil && o.Zone == state.ZBattlefield {
			t.Fatalf("creature %d survived the sweep", id)
		}
	}
}

// countBattlefieldCreatures counts seat p's battlefield creatures.
func countBattlefieldCreatures(e *Engine, p state.PlayerID) int {
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.Zone == state.ZBattlefield {
			n++
		}
	}
	return n
}
