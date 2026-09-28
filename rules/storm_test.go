package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const boltSrc = "Name:Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"

// castTarget casts id from the pending priority decision, answers the target
// decision by choosing the offered option whose Player is want, then empties
// the stack. The bare castObj helper always picks options[0] (the first
// living seat), which for a ValidTgts$ Player spell is the caster themselves;
// Storm's copies keep that choice, so the whole burst lands on one player and
// the life assertion here wants it routed to the opponent (seat 1).
func castTarget(t *testing.T, e *Engine, id state.ObjID, want state.PlayerID) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %d: %+v", id, d.Options)
	}
	submitChoices(t, e, idx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	tIdx := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == want {
			tIdx = o.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("seat %d not offered as a target: %+v", want, d.Options)
	}
	submitChoices(t, e, tIdx)
	passUntilStackEmpty(t, e, 20)
}

// TestStormCopiesTheSpellOncePerSpellCastBefore is Task 17's end-to-end
// Storm carrier: two Instants are cast before a Storm sorcery, so Storm's
// amount is two copies on top of the original. Each copy (and the original)
// lives/dies on its own, and every resolved copy sits in exile (CR 707.10a:
// a copy is a different object; CR 608.2m/111.7 settle a resolves-to-copy
// spell there), while the original goes to the graveyard as a plain sorcery.
func TestStormCopiesTheSpellOncePerSpellCastBefore(t *testing.T) {
	t.Parallel()
	tendrils := "Name:Tendrils\nManaCost:2 B B\nTypes:Sorcery\nK:Storm\nA:SP$ LoseLife | ValidTgts$ Player | LifeAmount$ 2 | SubAbility$ DBGainLife\nSVar:DBGainLife:DB$ GainLife | Defined$ You | LifeAmount$ 2\nOracle:x\n"
	e, cfg, td := newFixtureDeck(t, 91, tendrils, boltSrc, boltSrc)
	for i := 0; i < 2; i++ {
		b := addToHand(t, e, 0, boltSrc)
		addMana(t, e, 0, "R")
		castObj(t, e, b)
		passUntilStackEmpty(t, e, 20)
	}
	if e.CastThisTurn() != 2 {
		t.Fatalf("cast this turn %d", e.CastThisTurn())
	}
	addMana(t, e, 0, "BBGG")
	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
	castTarget(t, e, td, 1) // target the opponent explicitly
	e.Advance()
	passUntilStackEmpty(t, e, 40)
	if e.G.Players[1].Life != life1-6 || e.G.Players[0].Life != life0+6 {
		t.Fatalf("life %d/%d: want original + two copies", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	copies := 0
	for _, o := range e.G.Objs {
		if o.IsCopy {
			copies++
			if o.Zone != state.ZExile {
				t.Fatalf("a resolved copy sits in %s", o.Zone)
			}
		}
	}
	if copies != 2 {
		t.Fatalf("%d copies", copies)
	}
	replayCheck(t, e, cfg)
}

// TestChainLightningAsksForThePayAndMakesTheCopyWhenPaid is the M2d-2
// carrier for the mid-resolution ask closing R-8: Chain Lightning's
// CopySpellAbility has an UnlessCost (RR), so on the first pass the target's
// controller is asked a KModes pay/decline decision mid-resolution — the
// engine must present it with the spell STILL on the stack — and on "pay"
// (the payer has RR in the pool) the copy is made, the original resolves to
// the graveyard, and the copy deals its own damage. The full log replays
// byte-for-byte (replayCheck), so the suspended resolution is event-sourced
// like everything else.
func TestChainLightningAsksForThePayAndMakesTheCopyWhenPaid(t *testing.T) {
	t.Parallel()
	chain := "Name:Chain\nManaCost:R\nTypes:Sorcery\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3 | SubAbility$ DBCopy1\n" +
		"SVar:DBCopy1:DB$ CopySpellAbility | Defined$ Parent | Controller$ TargetedOrController | UnlessPayer$ TargetedOrController | UnlessCost$ R R | UnlessSwitched$ True | MayChooseTarget$ True\nOracle:x\n"
	e, cfg, ch := newFixtureDeck(t, 92, chain)
	addMana(t, e, 0, "R")
	addMana(t, e, 1, "RR")
	life := e.G.Players[1].Life
	// Cast at seat 1 (the opponent, whose controller will be asked), then
	// pass priority until the engine poses the mid-resolution pay decision.
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == ch {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %d: %+v", ch, d.Options)
	}
	submitChoices(t, e, idx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	tIdx := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			tIdx = o.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("seat 1 not offered as a target: %+v", d.Options)
	}
	submitChoices(t, e, tIdx)
	d = passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected a mid-resolution pay decision, got %+v", d)
	}
	// The resolution must have suspended WITH the spell on the stack: the
	// asked object is not popped just because it is pending.
	if o := e.G.Obj(ch); o.Zone != state.ZStack {
		t.Fatalf("resolution must suspend WITH the spell on the stack, zone %s", o.Zone)
	}
	submitChoices(t, e, 0) // "Pay R R — make a copy"
	// The paid copy is itself a Chain Lightning (copies copy all text), so
	// CR 707.10c gives its controller a new-target election before it
	// resolves; drainToEnd takes the deterministic keep-current option for
	// it. The copy's own copy clause then re-asks mid-resolution, but the
	// payer's pool is now empty, so the engine declines that second ask and
	// no second copy is made.
	drainToEnd(t, e, 30)
	if e.G.Players[1].Life != life-3-3 {
		t.Fatalf("life = %d, want %d (original + paid copy each deal 3)", e.G.Players[1].Life, life-6)
	}
	if z := e.G.Obj(ch).Zone; z != state.ZGraveyard {
		t.Errorf("original went to %s, want Graveyard", z)
	}
	copies := 0
	for _, o := range e.G.Objs {
		if o.IsCopy {
			copies++
			if o.Zone != state.ZExile {
				t.Errorf("a resolved copy sits in %s", o.Zone)
			}
		}
	}
	if copies != 1 {
		t.Errorf("%d copies, want exactly 1 (the paid one)", copies)
	}
	if !hasEventKind(e, events.ModeChosen) {
		t.Error("no ModeChosen event recorded the pay decision")
	}
	replayCheck(t, e, cfg)
}

// TestChainLightningDeclinesThePayWhenThePayerCannotAffordIt drives the same
// shape with NO red in the payer's pool: the bot-shaped answer is "pay"
// (option 0), but the engine cannot make the payment, so it declines and no
// copy is made — deterministic, and the log still replays. This is the
// acceptance games' ordinary Chain Lightning (a tapped-out opponent) and the
// keep of the old R-8 decline behaviour, now via a real ask instead of a
// blanket Note.
func TestChainLightningDeclinesThePayWhenThePayerCannotAffordIt(t *testing.T) {
	t.Parallel()
	chain := "Name:Chain\nManaCost:R\nTypes:Sorcery\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3 | SubAbility$ DBCopy1\n" +
		"SVar:DBCopy1:DB$ CopySpellAbility | Defined$ Parent | Controller$ TargetedOrController | UnlessPayer$ TargetedOrController | UnlessCost$ R R | UnlessSwitched$ True | MayChooseTarget$ True\nOracle:x\n"
	e, cfg, ch := newFixtureDeck(t, 93, chain)
	addMana(t, e, 0, "R")
	life := e.G.Players[1].Life
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == ch {
			idx = o.Index
		}
	}
	submitChoices(t, e, idx)
	d = e.Pending()
	tIdx := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			tIdx = o.Index
		}
	}
	submitChoices(t, e, tIdx)
	d = passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected a mid-resolution pay decision, got %+v", d)
	}
	submitChoices(t, e, 0) // "Pay" — unaffordable
	passUntilStackEmpty(t, e, 20)
	if e.G.Players[1].Life != life-3 {
		t.Fatalf("life = %d, want %d (no copy without payment)", e.G.Players[1].Life, life-3)
	}
	for _, o := range e.G.Objs {
		if o.IsCopy {
			t.Fatal("a copy was made despite the payer not covering the cost")
		}
	}
	replayCheck(t, e, cfg)
}
