package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestChainOfSilenceCopyOptionalUnlessComposition pins the REAL corpus Chain
// of Silence copy SA end to end through the engine -- the one combined
// Optional$ + UnlessCost$ carrier whose gate is SWITCHED
// (UnlessSwitched$ True) and whose UnlessCost$ is a non-mana cost
// (Sac<1/Land>), neither of which the synthetic rules/copy_optional_unless_test.go
// fixture covers. Chain of Silence's copy SA ("that creature's controller may
// sacrifice a land. If the player does, they may copy this spell") composes
// the two asks SEQUENTIALLY: the shared unless gate (effects/unless.go's
// unlessProceed) runs first and its orientation decides only whether the copy
// body runs at all -- on this switched shape PAYING runs it -- and only then
// does the body pose the may-copy election, to the copy's CONTROLLER
// (Controller$ TargetedController = the targeted creature's controller, seat
// 1), not to the seat-0 caster.
//
// Real corpus SA (.cards ... /c/chain_of_silence.txt):
//
//	DB$ CopySpellAbility | Defined$ Parent | Controller$ TargetedController |
//	Optional$ True | UnlessPayer$ TargetedController |
//	UnlessCost$ Sac<1/Land> | UnlessSwitched$ True | ...
const chainOfSilenceTargetName = "Silence Target"

const chainOfSilenceTargetFixture = "Name:Silence Target\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

const chainOfSilenceLandName = "Silence Land"

const chainOfSilenceLandFixture = "Name:Silence Land\nTypes:Basic Land Forest\nOracle:x\n"

// chainOfSilenceEngine builds a two-seat game at seat 0's Main 1 with the real
// corpus Chain of Silence in seat 0's hand, {W} in seat 0's pool, and, on seat
// 1's battlefield, the target creature plus TWO lands -- two so the
// Sac<1/Land> payment poses its own intermediate KChoose (advanceUnlessPayment
// auto-records a lone eligible candidate, which would skip the sacrifice
// picker this test also exercises). Returns the engine, its config, the Chain
// of Silence id and the two land ids.
func chainOfSilenceEngine(t *testing.T, reg *cards.Registry, seed uint64) (*Engine, Config, state.ObjID, [2]state.ObjID) {
	t.Helper()
	cos := mustCorpusCard(t, reg, "Chain of Silence")
	targetCard := card(t, chainOfSilenceTargetFixture)
	landCard := card(t, chainOfSilenceLandFixture)
	// Everything goes into the decks (never e.G.AddObject) so a log-only
	// replay can rebuild the exact objects by seed -- replayCheck below.
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{cos}, mountainDeck(t, 39)...),
			append([]*cards.Card{targetCard, landCard, landCard}, mountainDeck(t, 37)...),
		}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	cosID := moveByName(t, e, 0, "Chain of Silence", state.ZHand)
	moveByName(t, e, 1, chainOfSilenceTargetName, state.ZBattlefield)
	// moveByName matches the first hand/library copy each call and the moved
	// object leaves those zones, so a second call moves the second land.
	lands := [2]state.ObjID{
		moveByName(t, e, 1, chainOfSilenceLandName, state.ZBattlefield),
		moveByName(t, e, 1, chainOfSilenceLandName, state.ZBattlefield),
	}
	e.staticEpoch, e.activeEpoch = -1, -1
	addMana(t, e, 0, "W1")
	return e, cfg, cosID, lands
}

// chainOfSilencePreconditions asserts the compiled SA really carries the four
// parameters this composition turns on, and that the board objects the rules
// read are where the rules read them. Fatal on any miss, so no arm of this
// test can pass vacuously.
func chainOfSilencePreconditions(t *testing.T, reg *cards.Registry, e *Engine, target state.ObjID, lands [2]state.ObjID) {
	t.Helper()
	sa := cards.ResolveSVar(mustCorpusCard(t, reg, "Chain of Silence").Faces[0].SVars, "DBCopy")
	if sa == nil {
		t.Fatal("precondition: Chain of Silence has no DBCopy SA")
	}
	if got := sa.Params["Optional"]; got != "True" {
		t.Fatalf("precondition: DBCopy Optional$ = %q, want True", got)
	}
	if got := sa.Params["UnlessSwitched"]; got != "True" {
		t.Fatalf("precondition: DBCopy UnlessSwitched$ = %q, want True", got)
	}
	if got := sa.Params["UnlessCost"]; got != "Sac<1/Land>" {
		t.Fatalf("precondition: DBCopy UnlessCost$ = %q, want Sac<1/Land>", got)
	}
	if got := sa.Params["Controller"]; got != "TargetedController" {
		t.Fatalf("precondition: DBCopy Controller$ = %q, want TargetedController", got)
	}
	if o := e.G.Obj(target); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: target creature zone = %v, want battlefield", o)
	}
	for i, id := range lands {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: land %d zone = %v, want battlefield", i, o)
		}
	}
}

// copiesOnStack counts the IsCopy stack objects -- the engine's own mark of a
// StackCopy-created duplicate (CR 707.10).
func copiesOnStack(e *Engine) int {
	n := 0
	for _, id := range e.G.Stack {
		if o := e.G.Obj(id); o != nil && o.IsCopy {
			n++
		}
	}
	return n
}

// castChainOfSilenceAtSeat1Target casts the real Chain of Silence from seat
// 0's hand at seat 1's creature and drives to the pending unless_pay decision,
// returning it.
func castChainOfSilenceAtSeat1Target(t *testing.T, e *Engine, cos state.ObjID) *decision.Decision {
	t.Helper()
	d := e.Pending()
	cast := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == cos {
			cast = o.Index
		}
	}
	if cast < 0 {
		t.Fatalf("Chain of Silence not castable: %+v", d.Options)
	}
	submitChoices(t, e, cast)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending after cast = %+v, want the target decision", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Kind == "permanent" && o.Player == 1 {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("seat 1's creature not offered as a target: %+v", d.Options)
	}
	submitChoices(t, e, pick)
	pay := passUntilNonPriority(t, e, 20)
	if pay == nil || pay.Kind != decision.KModes || pay.ResumeKind != "unless_pay" {
		t.Fatalf("pending = %+v, want the unless_pay gate", pay)
	}
	if pay.Player != 1 {
		t.Fatalf("unless_pay payer = seat %d, want seat 1 (the targeted creature's controller)", pay.Player)
	}
	if len(pay.Options) < 2 || pay.Options[0].Label != "Pay Sac<1/Land> — make a copy" {
		t.Fatalf("unless_pay options = %+v, want the switched pay/decline pair headed by the copy label", pay.Options)
	}
	return pay
}

// TestChainOfSilenceCopyOptionalUnlessComposition drives the real corpus card
// through its three arms: the switched decline runs no body (no election, no
// copy); the switched pay runs the body, poses the may-copy election to the
// copy's controller (seat 1), and the answered election's no/yes arms make
// 0/1 copies.
func TestChainOfSilenceCopyOptionalUnlessComposition(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	// --- Declined switched gate: no body, so no election and no copy. ---
	{
		e, cfg, cos, lands := chainOfSilenceEngine(t, reg, 917)
		target := e.G.Zone(state.ZBattlefield, 1)[0]
		chainOfSilencePreconditions(t, reg, e, target, lands)
		pay := castChainOfSilenceAtSeat1Target(t, e, cos)
		if copiesOnStack(e) != 0 {
			t.Fatalf("copy before the gate was answered = %d, want 0", copiesOnStack(e))
		}
		decline := -1
		for _, o := range pay.Options {
			if o.Kind == "mode" && o.Label == "Don't pay" {
				decline = o.Index
			}
		}
		if decline < 0 {
			t.Fatalf("no decline option in %+v", pay.Options)
		}
		submitChoices(t, e, decline)
		// Drain: a declined switched gate runs no body, so NOTHING may be
		// asked and no copy may appear. A copy_optional election here would be
		// the exact regression this test exists to catch.
		for i := 0; i < 30 && !e.G.Over; i++ {
			d := e.Pending()
			if d == nil {
				break
			}
			if d.Kind == decision.KChoose && d.ResumeKind == "copy_optional" {
				t.Fatalf("a declined switched gate still posed the may-copy election: %+v", d)
			}
			if d.Kind == decision.KPriority {
				if len(e.G.Stack) == 0 {
					break
				}
				passPriority(t, e)
				continue
			}
			t.Fatalf("a declined switched gate posed an unexpected decision: %+v", d)
		}
		if got := copiesOnStack(e); got != 0 {
			t.Fatalf("declined switched gate made %d copies, want 0", got)
		}
		for i, id := range lands {
			if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("declined gate moved land %d off the battlefield (zone %v)", i, o)
			}
		}
		replayCheck(t, e, cfg)
	}

	// --- Paid switched gate, election declined: body runs, no copy. ---
	{
		e, cfg, cos, lands := chainOfSilenceEngine(t, reg, 918)
		target := e.G.Zone(state.ZBattlefield, 1)[0]
		chainOfSilencePreconditions(t, reg, e, target, lands)
		pay := castChainOfSilenceAtSeat1Target(t, e, cos)
		if copiesOnStack(e) != 0 {
			t.Fatalf("copy before the gate was answered = %d, want 0", copiesOnStack(e))
		}
		submitChoices(t, e, pay.Options[0].Index) // "Pay Sac<1/Land> — make a copy"
		// The two eligible lands force the intermediate sacrifice picker: the
		// payment continuation must ask WHICH land, not auto-record it.
		sac := e.Pending()
		if sac == nil || sac.Kind != decision.KChoose || sac.ResumeKind != "unless_cost" || sac.Player != 1 {
			t.Fatalf("after paying Sac<1/Land>: pending = %+v, want seat 1's unless_cost sacrifice KChoose", sac)
		}
		picked := false
		for _, o := range sac.Options {
			if o.Obj == lands[0] {
				submitChoices(t, e, o.Index)
				picked = true
			}
		}
		if !picked {
			t.Fatalf("the sacrifice ask did not offer land %d: %+v", lands[0], sac.Options)
		}
		elect := e.Pending()
		if elect == nil || elect.Kind != decision.KChoose || elect.ResumeKind != "copy_optional" {
			t.Fatalf("after paying the switched gate: pending = %+v, want the may-copy election", elect)
		}
		if elect.Player != 1 {
			t.Fatalf("election posed to seat %d, want seat 1 (Controller$ TargetedController)", elect.Player)
		}
		if elect.Min != 1 || elect.Max != 1 || len(elect.Options) != 2 ||
			elect.Options[0].Kind != "yes" || elect.Options[1].Kind != "no" {
			t.Fatalf("election shape = %+v, want Min==Max==1 options [yes, no]", elect)
		}
		if !inZone(e, state.ZGraveyard, 1, lands[0]) {
			t.Fatalf("paid Sac<1/Land> did not move land %d to the graveyard", lands[0])
		}
		submitChoices(t, e, elect.Options[1].Index) // no
		for i := 0; i < 30 && !e.G.Over && len(e.G.Stack) > 0; i++ {
			d := e.Pending()
			if d == nil {
				break
			}
			if d.Kind == decision.KPriority {
				passPriority(t, e)
				continue
			}
			t.Fatalf("unexpected decision after declining the election: %+v", d)
		}
		if got := copiesOnStack(e); got != 0 {
			t.Fatalf("declined election after a paid gate made %d copies, want 0", got)
		}
		replayCheck(t, e, cfg)
	}

	// --- Paid switched gate, election accepted: exactly one copy. ---
	{
		e, cfg, cos, lands := chainOfSilenceEngine(t, reg, 919)
		target := e.G.Zone(state.ZBattlefield, 1)[0]
		chainOfSilencePreconditions(t, reg, e, target, lands)
		pay := castChainOfSilenceAtSeat1Target(t, e, cos)
		submitChoices(t, e, pay.Options[0].Index)
		sac := e.Pending()
		if sac == nil || sac.Kind != decision.KChoose || sac.ResumeKind != "unless_cost" {
			t.Fatalf("after paying Sac<1/Land>: pending = %+v, want the sacrifice KChoose", sac)
		}
		for _, o := range sac.Options {
			if o.Obj == lands[0] {
				submitChoices(t, e, o.Index)
			}
		}
		elect := e.Pending()
		if elect == nil || elect.Kind != decision.KChoose || elect.ResumeKind != "copy_optional" || elect.Player != 1 {
			t.Fatalf("pending = %+v, want seat 1's may-copy election", elect)
		}
		submitChoices(t, e, elect.Options[0].Index) // yes
		if got := copiesOnStack(e); got != 1 {
			t.Fatalf("accepted election made %d copies, want exactly 1", got)
		}
		replayCheck(t, e, cfg)
	}
}
