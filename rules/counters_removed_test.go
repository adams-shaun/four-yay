// Count$CountersRemovedThisTurn <KIND> <Player> — the paid-or-lost counter
// count head (ticket count-countersremovedthisturn). Before the fix the head
// did not exist in evalCountBody and degraded to 0 through the
// unresolvable-value rule, so Blaster Hulk's per-{E} cast discount was never
// applied and Izzet Generatorium's paid-or-lost-four gate read 0 (which the
// gate's unresolvable read then failed OPEN on, over-offering the draw).
//
// The end-to-end pins run on the REAL corpus cards: Blaster Hulk's
// `S:Mode$ ReduceCost … Amount$ Count$CountersRemovedThisTurn ENERGY You`
// (the discount must track the paid total through the ordinary cast-payment
// path) and Izzet Generatorium's `CheckSVar$ … | SVarCompare$ GE4`
// (the gate must close below 4 and open at 4+), with the payments made
// through Whirler Virtuoso's real `Cost$ PayEnergy<3>` activation — the one
// event shape a PayEnergy settle emits (rules/mana.go), which is exactly the
// event the Host fold counts.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// payEnergy emits a negative player-counter change for kind — the event shape
// every PayEnergy payment (and every energy loss) produces.
func payEnergy(t *testing.T, e *Engine, p state.PlayerID, kind string, n int32) {
	t.Helper()
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: p, Counter: kind, Amount: -n})
}

// TestCountersRemovedThisTurnHeadFoldsPaidAndLost is the eval-level pin on
// Izzet Generatorium's own compiled face (its source anchors the ctx): the
// head sums this turn's negative-Amount PlayerCounterChange events of the
// kind for the named players, ignores grants, other kinds, other players,
// and resets at the TurnChange boundary.
func TestCountersRemovedThisTurnHeadFoldsPaidAndLost(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	gen := onBoardCard(t, e, 0, yourCountersCard(t, "Izzet Generatorium"))
	ctx := &effects.Ctx{Controller: 0, Source: gen}

	// Baseline: a modelled head reads an evaluated zero with nothing paid.
	if n, ok := effects.EvalCountOK(e, ctx, "Count$CountersRemovedThisTurn ENERGY You"); !ok || n != 0 {
		t.Fatalf("baseline head = %d (ok %v), want evaluated 0", n, ok)
	}
	// A grant is not a removal: +5 {E} must not count.
	seedPlayerCounter(t, e, 0, "ENERGY", 5)
	if n, _ := effects.EvalCountOK(e, ctx, "Count$CountersRemovedThisTurn ENERGY You"); n != 0 {
		t.Fatalf("head counted the +5 grant: %d, want 0", n)
	}
	// Two payments of the kind: 2 + 3 = 5.
	payEnergy(t, e, 0, "ENERGY", 2)
	payEnergy(t, e, 0, "ENERGY", 3)
	if n, ok := effects.EvalCountOK(e, ctx, "Count$CountersRemovedThisTurn ENERGY You"); !ok || n != 5 {
		t.Fatalf("head = %d (ok %v), want 5 (2 + 3 paid)", n, ok)
	}
	// Kind isolation: OIL removals are a different counter kind.
	payEnergy(t, e, 0, "OIL", 4)
	if n, _ := effects.EvalCountOK(e, ctx, "Count$CountersRemovedThisTurn ENERGY You"); n != 5 {
		t.Fatalf("head counted OIL removals: %d, want 5", n)
	}
	// Case-insensitive kind read: an upper-case energy entry sums in.
	payEnergy(t, e, 0, "energy", 1)
	if n, _ := effects.EvalCountOK(e, ctx, "Count$CountersRemovedThisTurn ENERGY You"); n != 6 {
		t.Fatalf("head after the lower-case-kind payment = %d, want 6", n)
	}
	// Player isolation: another seat's payment never counts.
	payEnergy(t, e, 1, "ENERGY", 9)
	if n, _ := effects.EvalCountOK(e, ctx, "Count$CountersRemovedThisTurn ENERGY You"); n != 6 {
		t.Fatalf("head read seat 1's payment: %d, want 6", n)
	}
	// The Opponent spelling reads the OTHER seats' totals, not seat 0's.
	if n, ok := effects.EvalCountOK(e, ctx, "Count$CountersRemovedThisTurn ENERGY Opponent"); !ok || n != 9 {
		t.Fatalf("Opponent spelling = %d (ok %v), want 9", n, ok)
	}
	// The turn boundary resets the fold to this turn's own total.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	if n, ok := effects.EvalCountOK(e, ctx, "Count$CountersRemovedThisTurn ENERGY You"); !ok || n != 0 {
		t.Fatalf("head after the TurnChange = %d (ok %v), want 0", n, ok)
	}
	payEnergy(t, e, 0, "ENERGY", 2)
	if n, _ := effects.EvalCountOK(e, ctx, "Count$CountersRemovedThisTurn ENERGY You"); n != 2 {
		t.Fatalf("head after the new turn's payment = %d, want 2", n)
	}
}

// TestCountersRemovedThisTurnObjectSpecIsUnresolvable pins the fail-closed
// verdict for the head's object-spec form (Churning Reservoir's
// `Count$CountersRemovedThisTurn OIL Card.YouCtrl+inRealZoneBattlefield/Plus.X`):
// the argument does not name a player spec, so the head stays UNRESOLVABLE
// (0, false) — never a fake evaluated zero that a gate could read as
// "measured zero removals".
func TestCountersRemovedThisTurnObjectSpecIsUnresolvable(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	res := onBoardCard(t, e, 0, yourCountersCard(t, "Churning Reservoir"))
	ctx := &effects.Ctx{Controller: 0, Source: res}
	// Precondition: the corpus SVar body is the object-spec shape the head
	// does not resolve.
	body := svarBodyOf(t, e.G.Obj(res).Face(), "CountCountersRemoved")
	if body != "Count$CountersRemovedThisTurn OIL Card.YouCtrl+inRealZoneBattlefield/Plus.X" {
		t.Fatalf("test precondition: Churning Reservoir SVar = %q", body)
	}
	// An oil-counter removal on a permanent must not leak into a count.
	seedPlayerCounter(t, e, 0, "OIL", 3)
	if n, ok := effects.EvalCountOK(e, ctx, body); ok || n != 0 {
		t.Fatalf("object-spec head = %d (ok %v), want unresolvable (0, false)", n, ok)
	}
}

// TestBlasterHulkDiscountTracksEnergyPaid is the end-to-end discount pin: the
// pool holds 6 generic R mana; the undiscounted 6{R}{R} costs 8, so the cast
// is offered only once the paid energy discounts it to at most 6 — and the
// paid mana actually drained matches the discounted total.
func TestBlasterHulkDiscountTracksEnergyPaid(t *testing.T) {
	t.Parallel()
	for _, paid := range []int32{0, 1, 2, 3} {
		t.Run("paid "+string(rune('0'+paid)), func(t *testing.T) {
			e := handEngine(t, yourCountersCard(t, "Blaster Hulk"))
			// Precondition: the static's Amount is the head this ticket
			// implements, and the pool is exactly what discriminates.
			for _, id := range e.G.Zone(state.ZHand, 0) {
				f := e.G.Obj(id).Face()
				if f.Name != "Blaster Hulk" {
					continue
				}
				if len(f.Statics) != 1 || f.Statics[0].Mode != "ReduceCost" ||
					f.Statics[0].Params["Amount"] != "Count$CountersRemovedThisTurn ENERGY You" {
					t.Fatalf("test precondition: Blaster Hulk statics = %+v", f.Statics)
				}
			}
			for _, id := range e.G.Zone(state.ZHand, 0) {
				if e.G.Obj(id).Face().Name == "Blaster Hulk" {
					if paid > 0 {
						payEnergy(t, e, 0, "ENERGY", paid)
					}
					addMana(t, e, 0, "RRRRRR")
					if n := effects.EvalCount(e, &effects.Ctx{Controller: 0, Source: id},
						"Count$CountersRemovedThisTurn ENERGY You"); n != paid {
						t.Fatalf("test precondition: head = %d, want %d", n, paid)
					}
					opt := castByName(t, e, 0, "Blaster Hulk")
					if paid < 2 {
						if opt != nil {
							t.Fatalf("paid %d: cast offered with pool 6R, want withheld (undiscounted cost 8)", paid)
						}
						return
					}
					if opt == nil {
						t.Fatalf("paid %d: cast withheld with pool 6R, want offered (discounted cost %d)", paid, 8-paid)
					}
					castObj(t, e, opt.Obj)
					if want := 6 - (8 - paid); e.G.Players[0].Pool.Total() != want {
						t.Fatalf("paid %d: pool after the cast = %d, want %d drained by the discounted cost",
							paid, e.G.Players[0].Pool.Total(), want)
					}
					for _, id := range e.G.Zone(state.ZBattlefield, 0) {
						if o := e.G.Obj(id); o != nil && o.Face().Name == "Blaster Hulk" {
							return // resolved, the right total was paid
						}
					}
					t.Fatal("Blaster Hulk did not resolve to the battlefield")
				}
			}
			t.Fatal("Blaster Hulk not in seat 0's hand")
		})
	}
}

// TestIzzetGeneratoriumGateOpensAtFourPaidOrLost is the end-to-end gate pin:
// the {T} draw is withheld while FEWER than four {E} were paid or lost this
// turn and offered once the total crosses 4 — with the payments made through
// Whirler Virtuoso's real PayEnergy<3> activation, twice (3, then 6).
func TestIzzetGeneratoriumGateOpensAtFourPaidOrLost(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	gen := onBoardCard(t, e, 0, yourCountersCard(t, "Izzet Generatorium"))
	virt := onBoardCard(t, e, 0, yourCountersCard(t, "Whirler Virtuoso"))

	// Precondition: the AB's CheckSVar/SVarCompare are the gate under test.
	ab := e.G.Obj(gen).Face().Abilities[0]
	if ab.API != "Draw" || ab.Params["CheckSVar"] != "Count$CountersRemovedThisTurn ENERGY You" ||
		ab.Params["SVarCompare"] != "GE4" {
		t.Fatalf("test precondition: Generatorium AB = %+v", ab.Params)
	}
	seedPlayerCounter(t, e, 0, "ENERGY", 9)
	addMana(t, e, 0, "")
	// The Generatorium's own R:Event$ AddCounter / ReplaceWith$ OneMore
	// replacement fires on the seeded grant too (that is the card's real
	// behaviour): 9 seeded enters as 10.
	if got := e.G.Players[0].Counter("ENERGY"); got != 10 {
		t.Fatalf("test precondition: energy after the seeded grant = %d, want 10 (the Generatorium's plus-one replacement)", got)
	}
	if _, ok := findAbilityOption(e, gen, 0); ok {
		t.Fatal("gate at 0 removed: the draw was offered, want withheld")
	}
	// First Virtuoso activation: pays 3 {E}. Gate still closed at 3.
	submitChoices(t, e, activateIndex(t, e, virt))
	passUntilStackEmpty(t, e, 20)
	addMana(t, e, 0, "")
	if got := e.G.Players[0].Counter("ENERGY"); got != 7 {
		t.Fatalf("test precondition: energy after the first payment = %d, want 7", got)
	}
	if _, ok := findAbilityOption(e, gen, 0); ok {
		t.Fatal("gate at 3 removed: the draw was offered, want withheld")
	}
	// Second activation: 6 paid — the gate opens.
	submitChoices(t, e, activateIndex(t, e, virt))
	passUntilStackEmpty(t, e, 20)
	addMana(t, e, 0, "")
	if _, ok := findAbilityOption(e, gen, 0); !ok {
		t.Fatal("gate at 6 removed: the draw was withheld, want offered")
	}
	// And the draw is real money: submitting it draws a card and leaves the
	// energy at 4 (10 after the replacement - 6 paid).
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == gen {
			idx = o.Index
		}
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
	if n := e.G.Players[0].Counter("ENERGY"); n != 4 {
		t.Fatalf("energy after the draw = %d, want 4 (the {T} cost paid nothing further)", n)
	}
	if len(e.G.Zone(state.ZHand, 0)) != 1 {
		t.Fatalf("hand after the draw = %d cards, want 1", len(e.G.Zone(state.ZHand, 0)))
	}
}
