package rules

// kw-reconfigure: cards/kw_reconfigure.go expands K:Reconfigure (CR
// 702.150) into the activated abilities the keyword prints -- per printed
// cost, one "attach to target creature you control" ability and one
// "unattach" ability (the no-IDs Attach event), both sorcery-speed; and
// CR 702.150c's not-a-creature-while-attached switch is derived live state
// (state.Object.ReconfiguredAttached) read by the layer-4 type walk, the
// filter grammar, the combat eligibility reads, the creature SBAs and the
// convoke/harmonize/protection creature scans.
//
// Every leaf drives the REAL corpus card Razorfield Ripper
// (`K:Reconfigure:2:PayEnergy<3>` -- "Pay {2} or {E}{E}{E}"), the Creative
// Energy m3c Commander deck carrier: the attached {2} and the alternative
// {E}{E}{E} both attach, the attached form stops being a creature, the
// unattach restores it, and the unattach half is never offered while
// unattached.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// ripperBoard links a board with Razorfield Ripper, a Grizzly Bears and a
// Bear Cub on seat 0's battlefield (the attached form needs a SECOND
// creature so the re-attach leaf can prove "another" and that the attached
// form itself never enters its own target pool).
func ripperBoard(t *testing.T) (*Engine, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"Razorfield Ripper", "Grizzly Bears", "Bear Cub"}, nil)
	return e,
		findOnBoard(t, e, 0, "Razorfield Ripper"),
		findOnBoard(t, e, 0, "Grizzly Bears"),
		findOnBoard(t, e, 0, "Bear Cub")
}

// addEnergy adds n energy counters to seat 0 and re-asks priority so the
// pending decision reflects them (the PlayerCounterChange fold, the same
// shape addcounter_replacement_test.go uses).
func addEnergy(t *testing.T, e *Engine, n int32) {
	t.Helper()
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "ENERGY", Amount: n})
	e.priorityRound()
}

// TestRazorfieldRipperReconfigureAttachAndUnattach pins the {2} pair end to
// end: the attach half activates for {2}, targets another creature you
// control (never the reconfigurer itself), the attached form stops being a
// creature (CR 702.150c), the unattach half is withheld while unattached
// and offered while attached, and unattaching restores creature-ness.
func TestRazorfieldRipperReconfigureAttachAndUnattach(t *testing.T) {
	t.Parallel()
	e, ripper, bear, _ := ripperBoard(t)

	// Preconditions the assertions below depend on: the unattached form IS a
	// creature, it is unattached, and it could attack.
	if e.G.Obj(ripper).AttachedTo != 0 {
		t.Fatalf("ripper starts attached to %d, want unattached", e.G.Obj(ripper).AttachedTo)
	}
	if !e.IsCreature(ripper) {
		t.Fatal("precondition: the unattached Razorfield Ripper is not a creature")
	}
	if !e.canAttack(ripper) {
		t.Fatal("precondition: the unattached Razorfield Ripper cannot attack")
	}

	// Empty pool: the {2} attach is not offered.
	if _, ok := findAbilityOption(e, ripper, 0); ok {
		t.Fatal("Razorfield Ripper's {2} reconfigure offered from an empty pool")
	}
	addMana(t, e, 0, "CC")
	opt, ok := findAbilityOption(e, ripper, 0)
	if !ok {
		t.Fatalf("Razorfield Ripper's {2} attach not offered: %+v", e.Pending().Options)
	}
	if !strings.Contains(opt.Label, "attach") {
		t.Fatalf("ability 0 label %q, want the attach half", opt.Label)
	}
	// The unattach half is withheld while the source is unattached -- a
	// payable no-op here would be a bot livelock.
	if _, ok := findAbilityOption(e, ripper, 1); ok {
		t.Fatal("unattach offered while unattached")
	}
	submitChoices(t, e, opt.Index)
	targetObject(t, e, bear)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(ripper).AttachedTo != bear {
		t.Fatalf("ripper attached to %d, want the bear %d", e.G.Obj(ripper).AttachedTo, bear)
	}
	// CR 702.150c: while attached, this isn't a creature.
	if e.IsCreature(ripper) {
		t.Fatal("the attached Razorfield Ripper still reads as a creature (CR 702.150c)")
	}
	if e.canAttack(ripper) {
		t.Fatal("the attached Razorfield Ripper can attack (CR 702.150c)")
	}

	// Unattach for the same {2}: offered now, restores creature-ness.
	addMana(t, e, 0, "CC")
	optU, ok := findAbilityOption(e, ripper, 1)
	if !ok {
		t.Fatalf("unattach not offered while attached: %+v", e.Pending().Options)
	}
	if !strings.Contains(optU.Label, "unattach") {
		t.Fatalf("ability 1 label %q, want the unattach half", optU.Label)
	}
	submitChoices(t, e, optU.Index)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(ripper).AttachedTo != 0 {
		t.Fatalf("after unattach ripper still attached to %d", e.G.Obj(ripper).AttachedTo)
	}
	if !e.IsCreature(ripper) {
		t.Fatal("the unattached Razorfield Ripper did not become a creature again (CR 702.150c)")
	}
}

// TestReconfigureReattachToAnotherCreature pins the re-attach half and the
// "another" of CR 702.150a: an attached reconfigurer can attach again to a
// DIFFERENT creature you control, and its own id is never in the target
// pool (it is not a creature while attached, and effAttach excludes itself
// besides).
func TestReconfigureReattachToAnotherCreature(t *testing.T) {
	t.Parallel()
	e, ripper, bear, cub := ripperBoard(t)
	addMana(t, e, 0, "CC")
	opt, ok := findAbilityOption(e, ripper, 0)
	if !ok {
		t.Fatal("Razorfield Ripper's {2} attach not offered")
	}
	submitChoices(t, e, opt.Index)
	targetObject(t, e, bear)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(ripper).AttachedTo != bear {
		t.Fatalf("ripper attached to %d, want the bear %d", e.G.Obj(ripper).AttachedTo, bear)
	}

	addMana(t, e, 0, "CC")
	opt2, ok := findAbilityOption(e, ripper, 0)
	if !ok {
		t.Fatal("re-attach not offered while attached")
	}
	submitChoices(t, e, opt2.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want target decision for the re-attach, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == ripper {
			t.Fatalf("the attached reconfigurer is in its own attach target pool: %+v", d.Options)
		}
	}
	targetObject(t, e, cub)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(ripper).AttachedTo != cub {
		t.Fatalf("re-attach landed on %d, want the cub %d", e.G.Obj(ripper).AttachedTo, cub)
	}
}

// TestRazorfieldRipperEnergyAlternativeCost pins the second colon field:
// `K:Reconfigure:2:PayEnergy<3>` is "Pay {2} or {E}{E}{E}" -- the energy
// alternative attaches with an EMPTY mana pool (and only with all three
// {E}), while the {2} half stays unavailable without mana, so the two are
// alternatives, not a sum.
func TestRazorfieldRipperEnergyAlternativeCost(t *testing.T) {
	t.Parallel()
	e, ripper, bear, _ := ripperBoard(t)

	addEnergy(t, e, 2)
	if _, ok := findAbilityOption(e, ripper, 2); ok {
		t.Fatal("the {E}{E}{E} alternative offered with only 2 energy")
	}
	addEnergy(t, e, 1)
	opt, ok := findAbilityOption(e, ripper, 2)
	if !ok {
		t.Fatalf("the {E}{E}{E} alternative not offered with 3 energy and no mana: %+v", e.Pending().Options)
	}
	if !strings.Contains(opt.Label, "attach") {
		t.Fatalf("ability 2 label %q, want the attach half", opt.Label)
	}
	if _, ok := findAbilityOption(e, ripper, 0); ok {
		t.Fatal("the {2} half offered with no mana in the pool (the costs must be alternatives)")
	}
	if _, ok := findAbilityOption(e, ripper, 3); ok {
		t.Fatal("the {E}{E}{E} unattach offered while unattached")
	}
	submitChoices(t, e, opt.Index)
	targetObject(t, e, bear)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(ripper).AttachedTo != bear {
		t.Fatalf("energy alternative attached to %d, want the bear %d", e.G.Obj(ripper).AttachedTo, bear)
	}
	if got := e.G.Players[0].Counter("ENERGY"); got != 0 {
		t.Fatalf("energy after paying the {E}{E}{E} cost = %d, want 0", got)
	}
}

// TestAttachedReconfigureIsNotACreature pins the CR 702.150c switch at the
// read sites the rules consult: the filter grammar (the target ask's
// ValidTgts$ Creature cannot see the attached form; Equipment still reads
// true), the combat eligibility reads, and the creature SBA (lethal
// damage marked on the attached form does not destroy it -- the
// zero-toughness sweep skips non-creatures).
func TestAttachedReconfigureIsNotACreature(t *testing.T) {
	t.Parallel()
	e, ripper, bear, _ := ripperBoard(t)
	addMana(t, e, 0, "CC")
	opt, ok := findAbilityOption(e, ripper, 0)
	if !ok {
		t.Fatal("Razorfield Ripper's {2} attach not offered")
	}
	submitChoices(t, e, opt.Index)
	targetObject(t, e, bear)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(ripper).AttachedTo != bear {
		t.Fatal("precondition: attach did not complete")
	}

	if effects.MatchesSpecFrom(e.G, "Creature", ripper, 0, 0) {
		t.Fatal("the filter grammar still reads the attached reconfigurer as a Creature")
	}
	if !effects.MatchesSpecFrom(e.G, "Equipment", ripper, 0, 0) {
		t.Fatal("the attached reconfigurer stopped being an Equipment")
	}
	if e.canAttack(ripper) {
		t.Fatal("the attached reconfigurer canAttack")
	}
	// Creature SBA: three marked damage would destroy a 3/3 creature; the
	// attached form is not a creature, so the zero-toughness sweep (and the
	// damage-marked casualty read) must skip it.
	e.emit(events.Event{Kind: events.Damage, Obj: ripper, Amount: 3})
	if e.G.Obj(ripper) == nil || e.G.Obj(ripper).Zone != state.ZBattlefield {
		t.Fatal("lethal damage destroyed the attached (non-creature) reconfigurer")
	}
	if !e.IsCreature(bear) {
		t.Fatal("precondition: the bear bearer is a creature")
	}
}

// TestReconfigureUnattachedSelfNeverInItsOwnTargetPool pins the r2 review
// MAJOR: CR 702.150a's "another target creature you control" must be real
// AT THE ASK, not only at resolution. The unattached reconfigurer IS a
// creature matching the attach half's ValidTgts$ Creature.YouCtrl, so with
// excludeSelf == 0 (the Mother-of-Runes ability convention) it offered
// itself in its own attach pool on every board, and answering it paid the
// {2} and attached nothing -- effAttach's self-refusal came only after the
// cost. rules/cast.go's targetAsk now excludes the source for an attach SA
// (the one home effAttach already used at resolution), so the pool never
// offers it and the lone-creature board aborts BEFORE payment instead of
// consuming the {2} for a refusal.
func TestReconfigureUnattachedSelfNeverInItsOwnTargetPool(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	// General board (reconfigurer + two other creatures): the pool holds the
	// other creatures and never the unattached reconfigurer itself.
	e, ripper, bear, cub := ripperBoard(t)
	if e.G.Obj(ripper).AttachedTo != 0 {
		t.Fatalf("precondition: ripper starts attached to %d", e.G.Obj(ripper).AttachedTo)
	}
	addMana(t, e, 0, "CC")
	opt, ok := findAbilityOption(e, ripper, 0)
	if !ok {
		t.Fatal("Razorfield Ripper's {2} attach not offered")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want the attach half's target decision, got %+v", d)
	}
	sawBear, sawCub := false, false
	for _, o := range d.Options {
		if o.Obj == ripper {
			t.Fatalf("the UNATTACHED reconfigurer is in its own attach target pool: %+v", d.Options)
		}
		if o.Obj == bear {
			sawBear = true
		}
		if o.Obj == cub {
			sawCub = true
		}
	}
	if !sawBear || !sawCub {
		t.Fatalf("precondition: the pool must hold the board's other creatures (bear %v cub %v): %+v", sawBear, sawCub, d.Options)
	}
	targetObject(t, e, bear)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(ripper).AttachedTo != bear {
		t.Fatalf("attach landed on %d, want the bear %d", e.G.Obj(ripper).AttachedTo, bear)
	}

	// The lone-creature board (the review's worst case): with the source
	// excluded from its own pool the attach half has no legal target at all.
	// The offer gate excludes the source exactly as the ask does
	// (abilityTargetsAvailable's attach self-exclusion, cardfuzz batch5
	// line 1), so the activation is never offered -- CR 602.2b: an ability
	// with no legal target cannot be activated -- the {2} stays in the pool
	// and nothing attaches. (Before the offer-gate fix it was offered and
	// then aborted before payment, CR 733.1; before the ask fix the self
	// option filled the pool, the {2} was paid and the attach was refused.)
	e2, _ := linkBoard(t, reg, []string{"Razorfield Ripper"}, nil)
	ripper2 := findOnBoard(t, e2, 0, "Razorfield Ripper")
	addMana(t, e2, 0, "CC")
	if _, ok := findAbilityOption(e2, ripper2, 0); ok {
		t.Fatal("lone board: the {2} attach offered with no legal target")
	}
	if got := e2.G.Obj(ripper2).AttachedTo; got != 0 {
		t.Fatalf("lone board: ripper attached to %d with no legal target, want unattached", got)
	}
	if got := e2.G.Players[0].Pool.Total(); got != 2 {
		t.Fatalf("lone board: pool %d, want the unspent {2}", got)
	}
}

// TestReconfigureOnlyAsASorcery pins CR 702.150a's timing: with the game
// driven past the main phase (beginning of combat) neither half is offered
// -- the minted SAs carry SorcerySpeed$ True and the offer loop's sorcery
// gate reads it.
func TestReconfigureOnlyAsASorcery(t *testing.T) {
	t.Parallel()
	e, ripper, _, _ := ripperBoard(t)
	addMana(t, e, 0, "CC")
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepBeginCombat)
	if _, ok := findAbilityOption(e, ripper, 0); ok {
		t.Fatal("reconfigure attach offered outside a sorcery window")
	}
	if _, ok := findAbilityOption(e, ripper, 1); ok {
		t.Fatal("reconfigure unattach offered outside a sorcery window")
	}
}
