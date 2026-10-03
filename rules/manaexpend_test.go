// trig:ManaExpend — "Whenever you expend N ..." (the Bloomburrow Commander
// expend keyword).
//
// The end-to-end pins run on the REAL corpus card Teapot Slinger (whose T:
// line is `Mode$ ManaExpend | Amount$ 4 | Player$ You | ... Execute$
// DealDamage`, body `DB$ DealDamage | NumDmg$ 2 | Defined$ Opponent`): the
// per-turn cast-spend tally folds from the pay-time FlagManaExpendCast
// CastInfo (rules/cast.go's payCast), the crossing matcher is
// trigmatch.ManaExpendMatches (rules/trigmatch/cast.go), and the emission gate is
// manaExpendReaderOut.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// expendVanilla is a no-ability spell of the given generic cost, the vehicle
// that spends pool mana on a cast without any side effects of its own.
func expendVanilla(t *testing.T, e *Engine, cost string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, "Name:Expend Vehicle "+cost+"\nTypes:Sorcery\nManaCost:"+cost+"\nOracle:x\n"), 0)
	o.Zone = state.ZHand
	ids := append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...)
	e.G.SetZone(state.ZHand, 0, append(ids, o.ID))
	return o.ID
}

// manaExpendedOf reads the engine's per-turn ManaExpend tally for a seat.
// It is engine scratch (rules/cast.go's manaExpended), deliberately NOT event
// state: the tally counts every cast of the turn, including casts made before
// a carrier entered the battlefield (which emit no wake-up CastInfo), so it
// is the crossing base the matcher reads. Tests assert it directly.
func manaExpendedOf(e *Engine, p state.PlayerID) int32 { return e.manaExpendTotal(p) }

// castExpendVehicle casts the vehicle through the ordinary beginCast flow and
// asserts it reached the stack paid (the precondition every later assertion
// rides on: the tally only moves on a payment that actually happened).
func castExpendVehicle(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	castMode(t, e, id, "")
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZStack {
		t.Fatalf("vehicle did not reach the stack paid: %+v", o)
	}
}

// settleExpendTrigger resolves the vehicle on top of the stack, then places
// and resolves whatever triggers the payment queued.
func settleExpendTrigger(t *testing.T, e *Engine) {
	t.Helper()
	if top := e.G.Obj(e.G.Stack[len(e.G.Stack)-1]); top != nil && top.Zone == state.ZStack {
		e.resolveTop()
	}
	for i := 0; i < 10; i++ {
		if len(e.pendingTriggers) == 0 {
			return
		}
		e.putTriggersOnStack()
		if len(e.G.Stack) == 0 {
			return
		}
		e.resolveTop()
	}
	t.Fatalf("expend triggers did not settle: %d pending still", len(e.pendingTriggers))
}

// expendTeapot puts the real corpus Teapot Slinger on seat 0's battlefield
// and returns its object id.
func expendTeapot(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	obj := e.G.Zone(state.ZHand, 0)[0]
	if oo := e.G.Obj(obj); oo == nil || oo.Face() == nil || oo.Face().Name != "Teapot Slinger" {
		t.Fatalf("test precondition: hand must hold Teapot Slinger, got %+v", oo)
	}
	placeOnBattlefield(t, e, obj)
	if placed := e.G.Obj(obj); placed == nil || placed.Zone != state.ZBattlefield {
		t.Fatalf("test precondition: Teapot Slinger not on the battlefield after the move")
	}
	return obj
}

// TestTeapotSlingerExpendFourDealsDamage is the crossing pin: a 3-mana cast
// leaves the tally below the threshold and fires nothing; the 4-mana cast
// crosses 0->3->7 (spending 3 then 4 expends past 4 exactly once) and the
// trigger deals its 2 damage to EACH opponent; a later 2-mana cast is
// at-or-above the threshold already and fires nothing.
func TestTeapotSlingerExpendFourDealsDamage(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Teapot Slinger"))
	teapot := expendTeapot(t, e)
	if life := e.G.Players[1].Life; life != 20 {
		t.Fatalf("test precondition: opponent life = %d, want 20", life)
	}

	// Cast 1: three mana. Tally 0->3, below the threshold, no trigger.
	first := expendVanilla(t, e, "3")
	e.G.Players[0].Pool[state.MC] = 10
	castExpendVehicle(t, e, first)
	if got := manaExpendedOf(e, 0); got != 3 {
		t.Fatalf("after the 3-mana cast tally = %d, want 3", got)
	}
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("3-mana cast queued %d triggers, want 0 (threshold not crossed)", len(e.pendingTriggers))
	}
	e.resolveTop()

	// Cast 2: four mana. Tally 3->7, crossing 4 exactly once.
	second := expendVanilla(t, e, "4")
	castExpendVehicle(t, e, second)
	if got := manaExpendedOf(e, 0); got != 7 {
		t.Fatalf("after the 4-mana cast tally = %d, want 7", got)
	}
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("crossing cast queued %d triggers, want 1", len(e.pendingTriggers))
	}
	trig := e.pendingTriggers[0]
	if trig.Source != teapot || trig.Controller != 0 || trig.Granted {
		t.Fatalf("queued trigger is not Teapot Slinger's printed ManaExpend: %+v", trig)
	}
	settleExpendTrigger(t, e)
	if life := e.G.Players[1].Life; life != 18 {
		t.Fatalf("after the expend-4 trigger opponent life = %d, want 18 (2 damage to each opponent)", life)
	}
	if life := e.G.Players[0].Life; life != 20 {
		t.Fatalf("caster's own life = %d, want 20 (the trigger hits only opponents)", life)
	}

	// Cast 3: two more mana. Tally 7->9, prev 7 already >= 4: no re-fire.
	third := expendVanilla(t, e, "2")
	castExpendVehicle(t, e, third)
	if got := manaExpendedOf(e, 0); got != 9 {
		t.Fatalf("after the 2-mana cast tally = %d, want 9", got)
	}
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("at-or-above-threshold cast queued %d triggers, want 0 (no second crossing)", len(e.pendingTriggers))
	}
	settleExpendTrigger(t, e)
	if life := e.G.Players[1].Life; life != 18 {
		t.Fatalf("opponent life after the non-crossing cast = %d, want 18", life)
	}
}

// TestTeapotSlingerExpendResetsEachTurn pins the per-turn window: after the
// threshold was crossed in turn 1, a TurnChange clears every seat's tally and
// the same crossing fires again in turn 2.
func TestTeapotSlingerExpendResetsEachTurn(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Teapot Slinger"))
	expendTeapot(t, e)
	e.G.Players[0].Pool[state.MC] = 20

	first := expendVanilla(t, e, "4")
	castExpendVehicle(t, e, first)
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("turn-1 crossing cast queued %d triggers, want 1", len(e.pendingTriggers))
	}
	settleExpendTrigger(t, e)
	if got := manaExpendedOf(e, 0); got != 4 {
		t.Fatalf("turn 1 tally = %d, want 4", got)
	}

	// Turn 2: the boundary resets the tally for every seat.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
	if got := manaExpendedOf(e, 0); got != 0 {
		t.Fatalf("after TurnChange tally = %d, want 0 (per-turn tally)", got)
	}

	second := expendVanilla(t, e, "4")
	castExpendVehicle(t, e, second)
	if got := manaExpendedOf(e, 0); got != 4 {
		t.Fatalf("turn-2 tally = %d, want 4 (the tally restarted)", got)
	}
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("turn-2 crossing cast queued %d triggers, want 1 (the threshold resets)", len(e.pendingTriggers))
	}
	settleExpendTrigger(t, e)
}

// TestTeapotSlingerExpendIgnoresAbilityMana pins the spells-only half of the
// definition ("total mana to cast spells"): an activated ability's {2}
// payment moves the pool but not the tally, so the NEXT spell's 1 mana is the
// expenditure that crosses 4. If ability spend counted, the tally would
// already sit at 5 before that cast and the trigger would not fire.
func TestTeapotSlingerExpendIgnoresAbilityMana(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Teapot Slinger"))
	expendTeapot(t, e)
	e.G.Players[0].Pool[state.MC] = 10

	loom := e.G.AddObject(card(t, "Name:Test Loom\nTypes:Artifact\nA:AB$ Draw | Cost$ 2 | Defined$ You | NumCards$ 1\nOracle:x\n"), 0)
	loom.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append([]state.ObjID{loom.ID}, e.G.Zone(state.ZBattlefield, 0)...))

	// Cast 1: three mana; tally 3.
	spell := expendVanilla(t, e, "3")
	castExpendVehicle(t, e, spell)
	e.resolveTop()
	if got := manaExpendedOf(e, 0); got != 3 {
		t.Fatalf("after the 3-mana cast tally = %d, want 3", got)
	}

	// Activate the {2} draw ability: pool moves, tally must not.
	e.beginActivation(0, decisionOptionAbility(loom.ID, 0))
	if top := e.G.Obj(e.G.Stack[len(e.G.Stack)-1]); top == nil || top.Zone != state.ZStack {
		t.Fatalf("test precondition: the {2} ability did not reach the stack")
	}
	if got := manaExpendedOf(e, 0); got != 3 {
		t.Fatalf("after the {2} ability activation tally = %d, want 3 (abilities never expend)", got)
	}
	e.resolveTop()

	// Cast 2: one mana. Tally 3->4: the crossing fires here, proving the
	// ability's 2 mana stayed out of the tally.
	small := expendVanilla(t, e, "1")
	castExpendVehicle(t, e, small)
	if got := manaExpendedOf(e, 0); got != 4 {
		t.Fatalf("after the 1-mana cast tally = %d, want 4", got)
	}
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("the crossing cast queued %d triggers, want 1", len(e.pendingTriggers))
	}
	settleExpendTrigger(t, e)
	if life := e.G.Players[1].Life; life != 18 {
		t.Fatalf("opponent life = %d, want 18 (the expend-4 trigger fired)", life)
	}
}

// TestManaExpendSilentWithoutTheCarrierOut pins the emission gate from the
// other side: with no ManaExpend carrier on the battlefield the pay-time
// wake-up stamp does not exist at all, so the trigger scan never runs and no
// trigger fires — the heads-safety contract (no golden game changes an
// event). The engine tally still accumulates (it is scratch, and a carrier
// that enters later MUST see the pre-entry spend), but no EVENT records it:
// the assertion is on the log, not the scratch.
func TestManaExpendSilentWithoutTheCarrierOut(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Teapot Slinger"))
	e.G.Players[0].Pool[state.MC] = 10

	before := len(e.L.Events)
	spell := expendVanilla(t, e, "4")
	castExpendVehicle(t, e, spell)
	// Scratch accumulated the unconditional spend (that is what a later-entry
	// carrier reads), but no FlagManaExpendCast wake-up event was emitted.
	if got := manaExpendedOf(e, 0); got != 4 {
		t.Fatalf("with no carrier out the scratch tally = %d, want 4 (unconditional accumulator)", got)
	}
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.CastInfo && events.FlagsFrom(ev.Counter)&state.FlagManaExpendCast != 0 {
			t.Fatalf("no carrier out but a ManaExpend wake-up CastInfo was emitted: %+v", ev)
		}
	}
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("no carrier out but %d triggers queued", len(e.pendingTriggers))
	}
	e.resolveTop()
}

// TestManaExpendUnknownAmountFailsClosed pins the matcher's fail-closed read:
// a trigger line whose Amount$ is not a positive literal never fires, even on
// a real crossing the tally recorded. The twin is a hand-built fixture (an
// inline Teapot-shaped script, Amount$ X): the corpus card's face is SHARED
// with every parallel test, so mutating it in place would leak the broken
// threshold into them.
func TestManaExpendUnknownAmountFailsClosed(t *testing.T) {
	t.Parallel()
	fake := card(t, "Name:Expend Twin Broken\nTypes:Creature Raccoon Warrior\nPT:2/2\nT:Mode$ ManaExpend | Amount$ X | Player$ You | TriggerZones$ Battlefield | Execute$ DealDamage | TriggerDescription$ x\nSVar:DealDamage:DB$ DealDamage | NumDmg$ 2 | Defined$ Opponent\nOracle:x\n")
	e := handEngine(t, fake)
	twin := e.G.Zone(state.ZHand, 0)[0]
	placeOnBattlefield(t, e, twin)
	e.G.Players[0].Pool[state.MC] = 10

	spell := expendVanilla(t, e, "4")
	castExpendVehicle(t, e, spell)
	if got := manaExpendedOf(e, 0); got != 4 {
		t.Fatalf("test precondition: the crossing stamp left the tally = %d, want 4", got)
	}
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("an unreadable Amount$ queued %d triggers, want 0 (fail closed)", len(e.pendingTriggers))
	}
	e.resolveTop()
}

// TestTeapotSlingerExpendCarrierEntryTurnCrossesOnPreEntrySpend pins the
// MAJOR defect the round-2 review found: the tally is a fact of the TURN, not
// of "casts made while the carrier was already out". A carrier that enters
// mid-turn MUST see the casts made before it entered. Here 3 mana is spent on
// a cast with no carrier out (emitting no wake-up event), Teapot Slinger then
// enters, and a 1-mana cast completes the real expend-4 (3 + 1 = 4) with the
// carrier out -- the crossing that fires the trigger is the engine's
// pre-entry spend plus this cast, which a gated-only tally would miss
// (undercount to 1, fire nothing).
func TestTeapotSlingerExpendCarrierEntryTurnCrossesOnPreEntrySpend(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Teapot Slinger"))
	e.G.Players[0].Pool[state.MC] = 20

	// Pre-entry cast: 3 mana with NO carrier out. No wake-up event, but the
	// engine tally must record it.
	pre := expendVanilla(t, e, "3")
	castExpendVehicle(t, e, pre)
	if got := manaExpendedOf(e, 0); got != 3 {
		t.Fatalf("test precondition: pre-entry tally = %d, want 3", got)
	}
	e.resolveTop()

	// Now the carrier enters mid-turn.
	expendTeapot(t, e)

	// The 1-mana cast completes the real expend-4 with the carrier out.
	post := expendVanilla(t, e, "1")
	castExpendVehicle(t, e, post)
	if got := manaExpendedOf(e, 0); got != 4 {
		t.Fatalf("after the 1-mana cast tally = %d, want 4 (3 pre-entry + 1)", got)
	}
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("the crossing cast queued %d triggers, want 1 (pre-entry spend counts)", len(e.pendingTriggers))
	}
	settleExpendTrigger(t, e)
	if life := e.G.Players[1].Life; life != 18 {
		t.Fatalf("opponent life = %d, want 18 (the expend-4 trigger fired)", life)
	}
}

// TestTeapotSlingerExpendNoSpuriousFireOnEntryTurn pins the OTHER direction of
// the same defect: a crossing that happened during a PRE-entry cast (before
// the carrier was on the battlefield) must NOT re-fire when a later cast is
// made with the carrier out. 4 mana is spent pre-entry (the real expend-4
// happened then, with no carrier to trigger) and 4 more after Teapot enters:
// the tallied total is 8 and the pre-payment base is 4, so there is no
// crossing at 4 and the trigger must stay silent. A gated-only tally would
// undercount the base to 0, compute prev=0 < 4 <= total=4, and fire
// spuriously.
func TestTeapotSlingerExpendNoSpuriousFireOnEntryTurn(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Teapot Slinger"))
	e.G.Players[0].Pool[state.MC] = 20

	// Pre-entry cast: 4 mana; the real expend-4 happens with no carrier out.
	pre := expendVanilla(t, e, "4")
	castExpendVehicle(t, e, pre)
	if got := manaExpendedOf(e, 0); got != 4 {
		t.Fatalf("test precondition: pre-entry tally = %d, want 4", got)
	}
	e.resolveTop()

	expendTeapot(t, e)

	// A second 4-mana cast: total 8, base 4. No crossing at 4.
	post := expendVanilla(t, e, "4")
	castExpendVehicle(t, e, post)
	if got := manaExpendedOf(e, 0); got != 8 {
		t.Fatalf("after the post-entry 4-mana cast tally = %d, want 8", got)
	}
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("post-entry cast queued %d triggers, want 0 (the expend-4 happened pre-entry)", len(e.pendingTriggers))
	}
	settleExpendTrigger(t, e)
	if life := e.G.Players[1].Life; life != 20 {
		t.Fatalf("opponent life = %d, want 20 (no spurious expend trigger)", life)
	}
}

// decisionOptionAbility builds the ability-activation option beginActivation
// takes (the flat pile index the offer loop enumerates; a one-ability
// artifact is index 0).
func decisionOptionAbility(id state.ObjID, idx int) decision.Option {
	return decision.Option{Kind: "ability", Obj: id, Ability: idx}
}
