// stat:UnspentMana — "You don't lose unspent <colour> mana as steps and
// phases end" (CR 500.4 with the card's exception; Leyline Tyrant, Omnath
// Locus of Mana, Upwelling, ...). The keep decision is made at the ONE
// ManaClear emit site (rules/turn.go's finishStepBoundary, unspentManaKeep),
// rides the ManaClear event's Text as keep letters, and the ManaClear fold
// (events/apply.go) spares the protected slots and their restriction
// batches. Both delivery routes are pinned here: the printed S: face static
// (Leyline Tyrant, Upwelling) and the boundaries themselves are driven
// through the real priority flow, because setStep skips the boundary while
// a decision is pending.
package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// replaySince applies every event emitted since the snapshot to the cloned
// game and asserts byte-identical state — the keep decision rides a live
// static walk, so the ManaClear fold's Text handling must replay identically
// from the events alone.
func replaySince(t *testing.T, e *Engine, replayed *state.Game, start int) {
	t.Helper()
	for _, ev := range e.L.Events[start:] {
		events.Apply(replayed, ev)
	}
	if !reflect.DeepEqual(replayed, e.G) {
		t.Fatal("the kept-mana boundary does not replay exactly from the events")
	}
}

// passOneBoundary passes priority until one ManaClear fires (the CR 500.4
// boundary this whole feature rides), then returns — so the pool assertions
// read the state exactly one boundary past where the caller left it, never a
// second boundary later.
func passOneBoundary(t *testing.T, e *Engine) {
	t.Helper()
	mark := len(e.L.Events)
	for i := 0; i < 20; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision pending and no ManaClear fired (events %d..%d)", mark, len(e.L.Events))
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("non-priority decision %+v while waiting for the boundary", d)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d.Options)
		}
		submitChoices(t, e, idx)
		for _, ev := range e.L.Events[mark:] {
			if ev.Kind == events.ManaClear {
				return
			}
		}
	}
	t.Fatal("no ManaClear fired within 20 priority passes")
}

// bankAtEndStep drives to the current turn's end step, seats the given pool
// on the protagonist's side, and passes one boundary (the end step leaving).
// The precondition every keep assertion rides on: the boundary's ManaClear
// really fired with a non-empty pool in place.
func bankAtEndStep(t *testing.T, e *Engine, pool state.Mana) state.Mana {
	t.Helper()
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepEnd)
	e.G.Players[0].Pool = pool
	if e.G.Players[0].Pool.Total() == 0 {
		t.Fatalf("test precondition: pool to bank is empty: %+v", pool)
	}
	passOneBoundary(t, e)
	return e.G.Players[0].Pool
}

// TestLeylineTyrantRedManaSurvivesBoundaries is the brief's Done pin: the
// controller's floating {R}{R} survives the end-step boundary (and a further
// boundary after it) while the same pool's {G} and the opponent's mana still
// empty — banking red for the card's death trigger is its whole reason to
// exist.
func TestLeylineTyrantRedManaSurvivesBoundaries(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	tyrant := onBoardCard(t, e, 0, corpusAlternativeCard(t, "Leyline Tyrant"))
	if o := e.G.Obj(tyrant); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("test precondition: Leyline Tyrant not on the battlefield: %+v", o)
	}

	pool := bankAtEndStep(t, e, state.Mana{state.MR: 2, state.MG: 1})
	if pool[state.MR] != 2 {
		t.Fatalf("after the end-step boundary red pool = %d, want 2 (the UnspentMana static must bank it)", pool[state.MR])
	}
	if pool[state.MG] != 0 {
		t.Fatalf("after the end-step boundary green pool = %d, want 0 (only the named colour is protected)", pool[state.MG])
	}

	// ValidPlayer$ You scopes the static to its controller: the opponent's
	// red mana still emptied at the same boundary.
	if pool := e.G.Players[1].Pool; pool[state.MR] != 0 {
		t.Fatalf("opponent red pool after the boundary = %d, want 0 (ValidPlayer$ You scopes to the controller)", pool[state.MR])
	}

	// A further boundary keeps banking (steps and phases end, plural). A red
	// RESTRICTION batch (Klauth-style provenance) rides the same keep: the
	// restriction is not time-bounded, only the emptying is.
	e.G.Players[0].RestrictedMana = []state.ManaRestriction{{Color: "R", Amount: 2, Valid: "CastSpell"}}
	// Replay anchor: everything from here on is event-sourced (the banked
	// boundary re-writes the same {2R} the events keep, so the direct write
	// is a no-op the clone reproduces).
	replayed := e.G.Clone()
	start := len(e.L.Events)
	pool = bankAtEndStep(t, e, pool)
	if pool[state.MR] != 2 {
		t.Fatalf("after the second boundary red pool = %d, want 2", pool[state.MR])
	}
	if len(e.G.Players[0].RestrictedMana) != 1 || e.G.Players[0].RestrictedMana[0].Color != "R" {
		t.Fatalf("the red restriction batch did not survive the boundary: %+v", e.G.Players[0].RestrictedMana)
	}
	replaySince(t, e, replayed, start)
}

// TestKeptBoundaryKeepsTaggedRestrictedBatches pins the fold's batch-slot
// decode: a restricted batch stores its producing ManaAdd.Counter verbatim —
// "SR" (snow red), "TreasureR" (typed red) — so the keep must read the
// batch's slot through state.ManaSlot, the canonical full-counter decoder the
// payment paths in rules/stack.go use. A ManaIndex(Color[0]) read turns the
// tag letter ('S'/'T') into the colourless slot and silently DROPS a
// protected red batch's spend restriction at the very boundary the keep
// exists for, while its units survive as unrestricted pool mana — a legal-
// payment change after the boundary, exactly what the keep must not do. The
// non-red CaveW batch keeps proving the filter still discriminates.
func TestKeptBoundaryKeepsTaggedRestrictedBatches(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	tyrant := onBoardCard(t, e, 0, corpusAlternativeCard(t, "Leyline Tyrant"))
	if o := e.G.Obj(tyrant); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("test precondition: Leyline Tyrant not on the battlefield: %+v", o)
	}

	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepEnd)
	e.G.Players[0].Pool = state.Mana{state.MR: 3, state.MW: 1}
	e.G.Players[0].RestrictedMana = []state.ManaRestriction{
		{Color: "TreasureR", Amount: 1, Valid: "Spell"},
		{Color: "SR", Amount: 1, Valid: "Spell"},
		{Color: "CaveW", Amount: 1, Valid: "Spell"},
	}
	if e.G.Players[0].Pool.Total() == 0 || len(e.G.Players[0].RestrictedMana) != 3 {
		t.Fatalf("test precondition: seeded pool/batches missing: pool %+v batches %+v",
			e.G.Players[0].Pool, e.G.Players[0].RestrictedMana)
	}
	replayed := e.G.Clone()
	start := len(e.L.Events)
	pool := bankAtEndStep(t, e, e.G.Players[0].Pool)

	if pool[state.MR] != 3 {
		t.Fatalf("after the boundary red pool = %d, want 3 (the keep protects the red slot whole)", pool[state.MR])
	}
	if pool[state.MW] != 0 {
		t.Fatalf("after the boundary white pool = %d, want 0 (only red is kept)", pool[state.MW])
	}
	kept := e.G.Players[0].RestrictedMana
	if len(kept) != 2 || kept[0].Color != "TreasureR" || kept[1].Color != "SR" {
		t.Fatalf("after the boundary batches = %+v, want the tagged red batches (TreasureR, SR) kept and CaveW dropped", kept)
	}
	replaySince(t, e, replayed, start)
}

// TestUnspentManaStopsWhenTheSourceLeaves pins the lifetime: the banked red
// survives boundaries only while the static's source is on the battlefield;
// once it leaves, the next boundary empties the bank like any other mana.
func TestUnspentManaStopsWhenTheSourceLeaves(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	tyrant := onBoardCard(t, e, 0, corpusAlternativeCard(t, "Leyline Tyrant"))
	pool := bankAtEndStep(t, e, state.Mana{state.MR: 2})
	if pool[state.MR] != 2 {
		t.Fatalf("test precondition: the static did not bank the red: %d", pool[state.MR])
	}
	// The source leaves to the hand (no death trigger to settle); the next
	// boundary's walk sees no live UnspentMana static and empties the slot.
	e.emit(events.Event{Kind: events.MoveZone, Obj: tyrant, From: state.ZBattlefield, To: state.ZHand})
	pool = bankAtEndStep(t, e, pool)
	if pool[state.MR] != 0 {
		t.Fatalf("after the source left, red pool = %d, want 0 (the keep ends with the source)", pool[state.MR])
	}
}

// TestUpwellingProtectsEverySeatAndColour pins the parameterless spelling:
// no ValidPlayer$ and no ManaType$ — every seat's whole pool survives.
func TestUpwellingProtectsEverySeatAndColour(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	upwelling := onBoardCard(t, e, 0, corpusAlternativeCard(t, "Upwelling"))
	if o := e.G.Obj(upwelling); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("test precondition: Upwelling not on the battlefield: %+v", o)
	}
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepEnd)
	e.G.Players[0].Pool = state.Mana{state.MR: 1, state.MG: 2, state.MC: 1}
	e.G.Players[1].Pool = state.Mana{state.MB: 1}
	passOneBoundary(t, e)
	if p := e.G.Players[0].Pool; p[state.MR] != 1 || p[state.MG] != 2 || p[state.MC] != 1 {
		t.Fatalf("seat 0 pool after the boundary = %+v, want every slot intact", p)
	}
	if p := e.G.Players[1].Pool; p[state.MB] != 1 {
		t.Fatalf("seat 1 pool after the boundary = %+v, want the black unit intact", p)
	}
}

// unspentGrantFixture is The Last Agni Kai's grant shape, reduced to a
// one-target vehicle: the spell deals its damage, then its SubAbility$
// registers the Effect-delivered `Mode$ UnspentMana` static (SVar:Unspent,
// the DB$ Effect | StaticAbilities$ spelling). No Duration$: an
// instant/sorcery source's grant is the UntilEOT lifetime
// effectUntilEOT converts it to, which is the oracle's "until end of turn".
const unspentGrantFixture = "Name:Unspent Grant Fixture\n" +
	"ManaCost:2\nTypes:Instant\n" +
	"A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1 | SubAbility$ DBEffect\n" +
	"SVar:DBEffect:DB$ Effect | StaticAbilities$ Unspent\n" +
	"SVar:Unspent:Mode$ UnspentMana | ValidPlayer$ You | ManaType$ Red\n" +
	"Oracle:x\n"

// TestEffectDeliveredUnspentManaBanksUntilEndOfTurn pins the second delivery
// route: the resolving spell's DB$ Effect registers the UnspentMana static
// (effEffect's registration arm), the controller's red mana survives the
// end-step boundary while the grant is live, and it empties again at the
// next boundary once the UntilEOT grant expired at the turn's cleanup.
func TestEffectDeliveredUnspentManaBanksUntilEndOfTurn(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	fixture := e.G.AddObject(card(t, unspentGrantFixture), 0)
	fixture.Zone = state.ZHand
	ids := append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...)
	e.G.SetZone(state.ZHand, 0, append(ids, fixture.ID))
	e.G.Players[0].Pool = state.Mana{state.MR: 2, state.MC: 2}

	castMode(t, e, fixture.ID, "")
	d := e.Pending()
	if d == nil || !(d.Kind == decision.KTarget || (d.Kind == decision.KChoose && d.ResumeKind == "tgts")) {
		t.Fatalf("test precondition: the fixture's damage target ask did not pose: %+v", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("test precondition: opponent not offered as the fixture's target: %+v", d.Options)
	}
	submitChoices(t, e, tgt)
	if top := e.G.Obj(e.G.Stack[len(e.G.Stack)-1]); top != nil && top.Zone == state.ZStack {
		e.resolveTop()
	}
	if life := e.G.Players[1].Life; life != 19 {
		t.Fatalf("test precondition: the fixture's damage never landed (opponent life %d, want 19)", life)
	}

	// The grant is registered and live at the end-step boundary: the
	// boundary's ManaClear carries the keep letters (the event-level pin of
	// the keep decision), and the FOLD retains the banked red at that
	// boundary — asserted below against a clone replayed exactly through the
	// kept ManaClear, because bankAtEndStep's one submit can fire several
	// boundaries and its returned pool is the post-expiry state. Only then
	// does the pool empty at a LATER boundary, because the UntilEOT grant
	// expired at the turn's cleanup (the instant-source conversion) while the
	// seat's own turn-change cascade ran on.
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepEnd)
	e.G.Players[0].Pool = state.Mana{state.MR: 2}
	if e.G.Players[0].Pool.Total() == 0 {
		t.Fatal("test precondition: pool to bank is empty")
	}
	replayed := e.G.Clone()
	mark := len(e.L.Events)
	passOneBoundary(t, e)
	keptIdx := -1
	for i, ev := range e.L.Events[mark:] {
		if ev.Kind == events.ManaClear && ev.Player == 0 && ev.Text != "" {
			keptIdx = i
			break
		}
	}
	if keptIdx < 0 {
		t.Fatalf("no boundary ManaClear carried keep letters while the grant was live (pool now %+v)", e.G.Players[0].Pool)
	}
	for _, ev := range e.L.Events[mark : mark+keptIdx+1] {
		events.Apply(replayed, ev)
	}
	if got := replayed.Players[0].Pool[state.MR]; got != 2 {
		t.Fatalf("the kept boundary did not retain the banked red while the grant was live: red pool = %d, want 2", got)
	}
	if got := e.G.Players[0].Pool[state.MR]; got != 0 {
		t.Fatalf("after the grant expired red pool = %d, want 0", got)
	}
}
