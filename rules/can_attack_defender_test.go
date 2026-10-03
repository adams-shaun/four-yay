package rules

// canattackdefender1: stat:CanAttackDefender (CR 702.3b's effect half, "can
// attack as though it didn't have defender"). Before this primitive the
// attacker-legality read refused any creature with K:Defender unconditionally,
// so every Defender wall stayed walled even under Arcades the Strategist's
// own static. The read (rules/combat/defender.go AttackAllowedThroughDefender)
// is consulted per (attacker, defender) pair through canAttackPair from the
// offer list, the validator and the encore gate, and it covers both routes
// the corpus spells:
//
//   - printed face statics (`S:Mode$ CanAttackDefender`) — Felothar the
//     Steadfast's Creature.YouCtrl, Drowsing Tyrannodon's IsPresent$ gate,
//     Weathered Sentinels' ValidAttacked$ scoping;
//   - Effect-granted bodies registered as CanAttackDefender continuous
//     effects (effects/misc.go effEffect) — Assault Formation's
//     Creature.IsRemembered grant, Krotiq Nestguard's Card.EffectSource
//     self-grant, Wakestone Gargoyle's ValidCards$ plural spelling.
//
// Fixtures load the REAL compiled corpus card (never a copied Forge script).

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestCanAttackDefenderPrimitiveIsRegistered(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["stat:CanAttackDefender"] {
		t.Fatal(`effects.Supported() is missing "stat:CanAttackDefender"`)
	}
}

// wallFixture is the authored Defender wall every fixture uses (never a
// committed .txt, per the licensing rule).
const wallFixture = "Name:Test Wall\nManaCost:1 W\nTypes:Creature Wall\nPT:0/4\nK:Defender\nOracle:x\n"

// attackerOption reports whether the pending KAttackers decision offers
// (id, def), with the option.
func attackerOption(t *testing.T, e *Engine, id state.ObjID, def state.PlayerID) (decision.Option, bool) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == id && o.Player == def {
			return o, true
		}
	}
	return decision.Option{}, false
}

// TestFelotharLiftsTheWallForYourCreatures pins the printed-static route on
// the real corpus Felothar the Steadfast (`S:Mode$ CanAttackDefender |
// ValidCard$ Creature.YouCtrl`): an authored Defender wall on the same
// battlefield is offered as an attacker, and the declaration commits.
func TestFelotharLiftsTheWallForYourCreatures(t *testing.T) {
	t.Parallel()
	felothar, ok := testutil.CorpusRegistry(t).Lookup("Felothar the Steadfast")
	if !ok {
		t.Fatal("corpus fixture: Felothar the Steadfast missing")
	}
	wall := card(t, wallFixture)
	e, cfg := restrictionGame(t, 7201, [][]*cards.Card{nil, nil},
		[][]*cards.Card{{felothar, wall}, nil})
	felotharID := bearOnBoard(t, e, 0, felothar)
	wallID := bearOnBoard(t, e, 0, wall)

	// Preconditions the assertion depends on: the wall is a battlefield
	// creature of the active player carrying Defender.
	if e.G.Obj(wallID).Zone != state.ZBattlefield || !e.IsCreature(wallID) {
		t.Fatal("precondition: the wall is not a battlefield creature")
	}
	if !e.HasKeyword(wallID, "Defender") {
		t.Fatal("precondition: the wall carries no Defender")
	}

	// Enter the declare-attackers step through the event, so the log-only
	// replay reaches the same step.
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	e.askAttackers()
	if _, ok := attackerOption(t, e, wallID, 1); !ok {
		t.Fatalf("Felothar's CanAttackDefender static did not lift the wall: %+v", e.Pending().Options)
	}
	if _, ok := attackerOption(t, e, felotharID, 1); !ok {
		t.Fatal("Felothar itself (no Defender) is not offered -- the shared read broke ordinary attackers")
	}
	submitAttackerAt(t, e, wallID, 1)
	if !e.G.Obj(wallID).IsAttacking || e.G.Obj(wallID).Attacking != 1 {
		t.Fatalf("wall declaration did not commit: IsAttacking=%v Attacking=%d",
			e.G.Obj(wallID).IsAttacking, e.G.Obj(wallID).Attacking)
	}
	replayCheck(t, e, cfg)
}

// TestDefenderWallStaysWalledWithoutTheStatic is the negative control for the
// route above: the same authored wall, no CanAttackDefender carrier on the
// board, is not offered and both reads say walled.
func TestDefenderWallStaysWalledWithoutTheStatic(t *testing.T) {
	t.Parallel()
	wall := card(t, wallFixture)
	e, cfg := restrictionGame(t, 7202, [][]*cards.Card{nil, nil},
		[][]*cards.Card{{wall}, nil})
	wallID := bearOnBoard(t, e, 0, wall)
	if e.canAttackPair(wallID, 1) || e.canAttack(wallID) {
		t.Fatal("a Defender wall with no CanAttackDefender static became attackable")
	}
	// Enter the declare-attackers step through the event, so the log-only
	// replay reaches the same step.
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	e.askAttackers()
	// The offer list is empty, so askAttackers takes the silent empty path:
	// no attacker decision is ever posed (the stale priority from Advance is
	// still pending), which IS the observable "not offered".
	if d := e.Pending(); d != nil && d.Kind == decision.KAttackers {
		for _, o := range d.Options {
			if o.Obj == wallID {
				t.Fatalf("the wall was offered with no lift in force: %+v", d.Options)
			}
		}
	}
	replayCheck(t, e, cfg)
}

// TestKrotiqNestguardEffectSourceGrant pins the Effect-granted route's
// dominant shape on the real corpus Krotiq Nestguard: its `AB$ Effect |
// StaticAbilities$ CanAttack` body is `ValidCard$ Card.EffectSource`, so the
// registration binds the effect's own source and the activated grant lifts
// the carrier's own wall until end of turn.
func TestKrotiqNestguardEffectSourceGrant(t *testing.T) {
	t.Parallel()
	nestguard, ok := testutil.CorpusRegistry(t).Lookup("Krotiq Nestguard")
	if !ok {
		t.Fatal("corpus fixture: Krotiq Nestguard missing")
	}
	e, cfg := restrictionGame(t, 7203, [][]*cards.Card{nil, nil},
		[][]*cards.Card{{nestguard}, nil})
	ngID := bearOnBoard(t, e, 0, nestguard)

	// Precondition: the wall holds before the grant.
	if e.canAttackPair(ngID, 1) {
		t.Fatal("precondition: Krotiq Nestguard is attackable before its grant")
	}
	addMana(t, e, 0, "GGG") // the {2}{G} grant, three units
	opt, ok := findAbilityOption(e, ngID, 0)
	if !ok {
		t.Fatalf("the {2}{G} grant ability is not offered: %+v", e.Pending())
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 60)
	if z := e.G.Obj(ngID).Zone; z != state.ZBattlefield {
		t.Fatalf("the grant removed the Nestguard: %s", z)
	}
	if !e.canAttackPair(ngID, 1) || !e.canAttack(ngID) {
		t.Fatal("the Card.EffectSource grant did not lift the carrier's own wall")
	}
	// Enter the declare-attackers step through the event, so the log-only
	// replay reaches the same step.
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	e.askAttackers()
	if _, ok := attackerOption(t, e, ngID, 1); !ok {
		t.Fatalf("the granted Nestguard is not offered as an attacker: %+v", e.Pending().Options)
	}
	submitAttackerAt(t, e, ngID, 1)
	if !e.G.Obj(ngID).IsAttacking {
		t.Fatal("the granted Nestguard's attack did not commit")
	}
	replayCheck(t, e, cfg)
}

// TestKrotiqNestguardGrantExpiresAtEndOfTurn is the canattackdefender1-r2
// expiry regression. Krotiq Nestguard's grant body writes no inline
// Duration$, and its card text names THIS TURN only ("{2}{G}: This creature
// can attack this turn as though it didn't have defender"). Before the fix
// effEffect's absent-Duration default ("Permanent") registered the grant as a
// one-shot that survives its source (CR 611.2a), so the permission stayed live
// for the rest of the game -- the wall was still lifted on later turns while
// the Nestguard stayed on the battlefield. The class helper
// effects.absentDurationMeansThisTurn now routes this mode's absent Duration$
// to UntilEOT.
//
// The test drives the source through a real activation and then ends the turn
// with the source STILL ON THE BATTLEFIELD, which is the condition that
// separates an UntilEOT registration from a source-leaves one: if the grant
// only disappeared because the source left, this test could not observe the
// defect.
func TestKrotiqNestguardGrantExpiresAtEndOfTurn(t *testing.T) {
	t.Parallel()
	nestguard, ok := testutil.CorpusRegistry(t).Lookup("Krotiq Nestguard")
	if !ok {
		t.Fatal("corpus fixture: Krotiq Nestguard missing")
	}
	e, cfg := restrictionGame(t, 7210, [][]*cards.Card{nil, nil},
		[][]*cards.Card{{nestguard}, nil})
	ngID := bearOnBoard(t, e, 0, nestguard)

	// Preconditions the assertions depend on: the wall is a battlefield
	// creature of the active player carrying Defender, and it is walled
	// before the grant.
	if e.G.Obj(ngID).Zone != state.ZBattlefield || !e.IsCreature(ngID) {
		t.Fatal("precondition: the Nestguard is not a battlefield creature")
	}
	if !e.HasKeyword(ngID, "Defender") {
		t.Fatal("precondition: the Nestguard carries no Defender")
	}
	if e.canAttackPair(ngID, 1) {
		t.Fatal("precondition: the Nestguard is attackable before its grant")
	}

	addMana(t, e, 0, "GGG") // the {2}{G} grant, three units
	opt, ok := findAbilityOption(e, ngID, 0)
	if !ok {
		t.Fatalf("the {2}{G} grant ability is not offered: %+v", e.Pending())
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 60)

	// The grant is live this turn (the route the sibling test pins) AND the
	// source is still on the battlefield -- the two facts that make the
	// expiry assertion non-vacuous.
	if e.G.Obj(ngID).Zone != state.ZBattlefield {
		t.Fatal("precondition: the grant removed the Nestguard")
	}
	if !e.canAttackPair(ngID, 1) {
		t.Fatal("precondition: the grant did not lift the wall this turn")
	}

	// End the turn. The source has NOT left the battlefield, so only a
	// this-turn (UntilEOT) registration is reclaimed here.
	e.EndOfTurnCleanup()
	if e.G.Obj(ngID).Zone != state.ZBattlefield {
		t.Fatal("precondition: the Nestguard left the battlefield at cleanup")
	}
	if e.canAttackPair(ngID, 1) || e.canAttack(ngID) {
		t.Fatal("the Nestguard's grant survived end of turn -- an absent-Duration " +
			"CanAttackDefender Effect registration was left Permanent")
	}

	// A later turn must stay walled too: the same observation one turn on.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	if e.canAttackPair(ngID, 1) || e.canAttack(ngID) {
		t.Fatal("the Nestguard's grant is still live on a later turn")
	}
	replayCheck(t, e, cfg)
}

// TestAssaultFormationRememberedGrant pins the remembered-target grant on the
// real corpus Assault Formation: its `AB$ Effect | RememberObjects$ Targeted`
// delivers `Mode$ CanAttackDefender | ValidCard$ Creature.IsRemembered`, so
// the TARGETED Defender wall attacks this turn and an untargeted one does not.
func TestAssaultFormationRememberedGrant(t *testing.T) {
	t.Parallel()
	af, ok := testutil.CorpusRegistry(t).Lookup("Assault Formation")
	if !ok {
		t.Fatal("corpus fixture: Assault Formation missing")
	}
	wallA := card(t, "Name:Wall A\nManaCost:1 W\nTypes:Creature Wall\nPT:0/4\nK:Defender\nOracle:x\n")
	wallB := card(t, "Name:Wall B\nManaCost:1 W\nTypes:Creature Wall\nPT:0/4\nK:Defender\nOracle:x\n")
	e, cfg := restrictionGame(t, 7204, [][]*cards.Card{nil, nil},
		[][]*cards.Card{{af, wallA, wallB}, nil})
	afID := bearOnBoard(t, e, 0, af)
	aID := bearOnBoard(t, e, 0, wallA)
	bID := bearOnBoard(t, e, 0, wallB)

	// Precondition: both walls walled, and the grant ability offered.
	if e.canAttackPair(aID, 1) || e.canAttackPair(bID, 1) {
		t.Fatal("precondition: a wall is attackable before the grant")
	}
	addMana(t, e, 0, "G")
	opt, ok := findAbilityOption(e, afID, 0)
	if !ok {
		t.Fatalf("Assault Formation's grant ability is not offered: %+v", e.Pending())
	}
	submitChoices(t, e, opt.Index)
	submitTarget(t, e, aID)
	passUntilStackEmpty(t, e, 60)

	if !e.canAttackPair(aID, 1) || !e.canAttack(aID) {
		t.Fatal("the targeted wall was not lifted (Creature.IsRemembered unread)")
	}
	if e.canAttackPair(bID, 1) || e.canAttack(bID) {
		t.Fatal("the UNTARGETED wall was lifted too -- the remembered grant over-applies")
	}
	// Enter the declare-attackers step through the event, so the log-only
	// replay reaches the same step.
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	e.askAttackers()
	if _, ok := attackerOption(t, e, aID, 1); !ok {
		t.Fatalf("the lifted wall is not offered: %+v", e.Pending().Options)
	}
	if _, ok := attackerOption(t, e, bID, 1); ok {
		t.Fatal("the untargeted wall is offered")
	}
	submitAttackerAt(t, e, aID, 1)
	if !e.G.Obj(aID).IsAttacking || e.G.Obj(aID).Attacking != 1 {
		t.Fatal("the lifted wall's attack did not commit")
	}
	replayCheck(t, e, cfg)
}

// TestDrowsingTyrannodonGateScoping pins the gated face static on the real
// corpus Drowsing Tyrannodon (`IsPresent$ Creature.powerGE4+YouCtrl`): the
// wall holds while no qualifying creature exists and lifts once one does.
func TestDrowsingTyrannodonGateScoping(t *testing.T) {
	t.Parallel()
	drowsing, ok := testutil.CorpusRegistry(t).Lookup("Drowsing Tyrannodon")
	if !ok {
		t.Fatal("corpus fixture: Drowsing Tyrannodon missing")
	}
	big := card(t, "Name:Big Beast\nManaCost:4 G\nTypes:Creature Beast\nPT:4/4\nOracle:x\n")
	e, cfg := restrictionGame(t, 7205, [][]*cards.Card{{big}, nil},
		[][]*cards.Card{{drowsing}, nil})
	dID := bearOnBoard(t, e, 0, drowsing)

	if !e.HasKeyword(dID, "Defender") {
		t.Fatal("precondition: Drowsing Tyrannodon carries no Defender")
	}
	if e.canAttackPair(dID, 1) {
		t.Fatal("precondition: the gate held state already lifts the wall")
	}
	bigID := moveByName(t, e, 0, "Big Beast", state.ZBattlefield)
	if o := e.G.Obj(bigID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: the 4-power creature never reached the battlefield")
	}
	if !e.canAttackPair(dID, 1) || !e.canAttack(dID) {
		t.Fatal("the IsPresent$ powerGE4 gate did not lift the wall with a 4-power creature out")
	}
	replayCheck(t, e, cfg)
}

// sentinelDrive drives a real three-seat game from genesis to seat 0's turn-4
// declare-attackers step. When attackFirst is true, seat 1 attacks seat 0 on
// its turn-2 declaration; seat 2 never attacks. Returns the engine, its
// config and the Weathered Sentinels object.
func sentinelDrive(t *testing.T, seed uint64, attackFirst bool) (*Engine, Config, state.ObjID) {
	t.Helper()
	sent, ok := testutil.CorpusRegistry(t).Lookup("Weathered Sentinels")
	if !ok {
		t.Fatal("corpus fixture: Weathered Sentinels missing")
	}
	bear := card(t, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{
			append(mountainDeck(t, 39), sent),
			append(mountainDeck(t, 39), bear),
			mountainDeck(t, 40),
		}})
	e := New(cfg)
	// Real logged MoveZone placements, so the log-only replay reconstructs
	// the same board (a direct-state placement would diverge it).
	sentID := moveByName(t, e, 0, "Weathered Sentinels", state.ZBattlefield)
	bearID := moveByName(t, e, 1, "Runeclaw Bear", state.ZBattlefield)
	e.Advance()
	driveToStepAll(t, e, 2, 1, state.StepDeclareAttackers)
	if attackFirst {
		if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZBattlefield || o.SummonSick {
			t.Fatal("precondition: the bear is not an unsick battlefield creature on its controller's turn")
		}
		submitAttackerAt(t, e, bearID, 0)
	} else {
		submitEmptyAttackers(t, e)
	}
	driveToStepAll(t, e, 4, 0, state.StepDeclareAttackers)
	return e, cfg, sentID
}

// submitEmptyAttackers answers a pending KAttackers decision with no
// declaration (the legal empty answer).
func submitEmptyAttackers(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
		t.Fatalf("submit empty declaration: %v", err)
	}
}

// TestWeatheredSentinelsAttackedYouTheirLastTurn pins the ValidAttacked$
// scoping on the real corpus Weathered Sentinels
// (`ValidAttacked$ Player.attackedYouTheirLastTurn`): seat 1 attacked seat 0
// during its last turn, so the Sentinels may attack seat 1 on seat 0's next
// turn -- and NOT seat 2, whose last turn never attacked seat 0.
func TestWeatheredSentinelsAttackedYouTheirLastTurn(t *testing.T) {
	t.Parallel()
	e, cfg, sentID := sentinelDrive(t, 7206, true)

	if !e.playerAttackedYouTheirLastTurn(1, 0) {
		t.Fatal("precondition: the log does not record seat 1 attacking seat 0 on its last turn")
	}
	if e.playerAttackedYouTheirLastTurn(2, 0) {
		t.Fatal("precondition: the log falsely records seat 2 attacking seat 0")
	}
	if !e.HasKeyword(sentID, "Defender") {
		t.Fatal("precondition: the Sentinels carry no Defender")
	}
	if !e.canAttackPair(sentID, 1) {
		t.Fatal("the ValidAttacked$-scoped static did not lift the wall against seat 1")
	}
	if e.canAttackPair(sentID, 2) {
		t.Fatal("the static lifted the wall against seat 2, which never attacked you")
	}
	if _, ok := attackerOption(t, e, sentID, 1); !ok {
		t.Fatalf("the Sentinels are not offered against seat 1: %+v", e.Pending().Options)
	}
	if _, ok := attackerOption(t, e, sentID, 2); ok {
		t.Fatal("the Sentinels are offered against seat 2")
	}
	submitAttackerAt(t, e, sentID, 1)
	if !e.G.Obj(sentID).IsAttacking || e.G.Obj(sentID).Attacking != 1 {
		t.Fatalf("the Sentinels' attack did not commit: IsAttacking=%v Attacking=%d",
			e.G.Obj(sentID).IsAttacking, e.G.Obj(sentID).Attacking)
	}
	replayCheck(t, e, cfg)
}

// TestWeatheredSentinelsStaysWalledWithoutTheRecord is the control: the same
// drive with NO attack ever declared leaves the Sentinels walled against
// every defender -- the static's ValidAttacked$ gate is doing the work, not
// the mode alone.
func TestWeatheredSentinelsStaysWalledWithoutTheRecord(t *testing.T) {
	t.Parallel()
	e, _, sentID := sentinelDrive(t, 7207, false)
	if e.playerAttackedYouTheirLastTurn(1, 0) || e.playerAttackedYouTheirLastTurn(2, 0) {
		t.Fatal("precondition: the log records an attack nobody declared")
	}
	if e.canAttackPair(sentID, 1) || e.canAttackPair(sentID, 2) || e.canAttack(sentID) {
		t.Fatal("the Sentinels are attackable with no ValidAttacked$ record")
	}
	// The offer list is empty, so the declare-attackers step takes the silent
	// empty path: no attacker decision is posed. If one IS posed, the
	// Sentinels must not appear in it.
	if d := e.Pending(); d != nil && d.Kind == decision.KAttackers {
		for _, o := range d.Options {
			if o.Obj == sentID {
				t.Fatalf("the Sentinels are offered without the attacked-you record: %+v", d.Options)
			}
		}
	}
}
