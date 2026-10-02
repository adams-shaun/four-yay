package rules

// TargetUnique$ ("every target this resolution chooses must be different")
// is read at the shared mid-resolution target pre-ask, so a root/SubAbility
// pair cannot aim "another target player/creature" at the parent's own target
// and a chain of TargetUnique asks accumulates its picks. The charms' cross-
// mode family (Shadrix Silverquill and the duo cycle) is a separate combined
// ask covered by charm_cross_mode_test.go.
//
// The carriers here inline the REAL corpus scripts (never a .cards file, per
// the licensing rule): biomantic_mastery.txt for the root/sub "another target
// player" shape, and know_evil.txt's three-rider shape for intra-chain
// accumulation. Only ManaCost/Types are simplified so the fixtures are cheap
// to cast; every target-bearing SA line and every parameter spelling is the
// corpus card's.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// biomanticScript is biomantic_mastery.txt's script, verbatim except the
// Oracle line and the ManaCost (7 generic instead of the hybrid 4 GU GU GU,
// so the fixture funds with one colour). The root Draw declares "target
// player" and the SubAbility carries TargetUnique$ True — the corpus's own
// encoding of the Oracle's "another target player".
const biomanticScript = "Name:Biomantic Mastery\nManaCost:7\nTypes:Sorcery\n" +
	"A:SP$ Draw | Defined$ You | ValidTgts$ Player | NumCards$ X | SubAbility$ DBDraw | SpellDescription$ Draw a card for each creature target player controls, then draw a card for each creature another target player controls.\n" +
	"SVar:DBDraw:DB$ Draw | Defined$ You | ValidTgts$ Player | TargetUnique$ True | NumCards$ X\n" +
	"SVar:X:ThisTargetedPlayer$Valid Creature.YouCtrl\n" +
	"Oracle:x\n"

// pendingPlayerIDs returns the pending decision's player-kind option ids as a
// set, so a test can assert which players an ask offers.
func pendingPlayerIDs(t *testing.T, e *Engine) map[int]bool {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	out := map[int]bool{}
	for _, o := range d.Options {
		if o.Kind == "player" {
			out[int(o.Player)] = true
		}
	}
	return out
}

// TestBiomanticMasterySubTargetExcludesParentTarget pins the root/sub
// TargetUnique$ exclusion on the real corpus card: Biomantic Mastery's root
// targets a player, and the SubAbility's "another target player" ask must not
// offer that same player again. Before the shared constraint the sub pre-ask
// offered BOTH players and the deterministic first-option answer re-picked the
// parent's own target, so both draws counted the same player's creatures.
func TestBiomanticMasterySubTargetExcludesParentTarget(t *testing.T) {
	bear := "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, _ := newFixtureDeck(t, 7001, biomanticScript, bear)
	// A creature for each player, so each Draw has a non-zero X and the two
	// halves are observable.
	putCreature(t, e, 0, bear)
	putToken(t, e, 1, bear, state.ZBattlefield) // seat 1's deck is mountains only

	addMana(t, e, 0, "GGGGGGG") // 7 generic (the simplified ManaCost)
	castFirst(t, e, "cast")

	// CR 601.2c: the root's own target ask. Choose the OPPONENT (seat 1).
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("root target ask = %+v, want KTarget", d)
	}
	if !pendingPlayerIDs(t, e)[1] {
		t.Fatalf("root target ask does not offer seat 1: %+v", d.Options)
	}
	rootChoice := -1
	for _, o := range d.Options {
		if o.Kind == "player" && int(o.Player) == 1 {
			rootChoice = o.Index
		}
	}
	if rootChoice < 0 {
		t.Fatalf("no seat-1 option in root ask: %+v", d.Options)
	}
	submitChoices(t, e, rootChoice)

	// CR 601.2c: "another target player" is announced with the first, as the
	// spell is cast. TargetUnique$ must exclude seat 1, the root's target.
	sub := castSubAsk(t, e)
	offered := pendingPlayerIDs(t, e)
	if offered[1] {
		t.Fatalf("sub TargetUnique$ ask re-offers the parent's own target (seat 1): %+v", sub.Options)
	}
	if !offered[0] {
		t.Fatalf("sub ask offers no legal different player: %+v", sub.Options)
	}
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 30)
	replayCheck(t, e, cfg)
}

// chainUniqueScript is a synthetic carrier of know_evil.txt's three-rider
// shape: a root with no targeting followed by chained `ValidTgts$ Player |
// TargetUnique$ True | TargetMin$ 0 | TargetMax$ 1` riders.
// The target-bearing lines are the corpus card's exact parameter spellings;
// only the Effect riders are replaced by Draw so the fixture needs no
// Scheme/static machinery.
func chainUniqueScript() string {
	return "Name:Chain Unique\nManaCost:1\nTypes:Sorcery\n" +
		"A:SP$ Draw | NumCards$ 1 | SubAbility$ R1\n" +
		"SVar:R1:DB$ Draw | NumCards$ 0 | ValidTgts$ Player | TargetUnique$ True | TargetMin$ 0 | TargetMax$ 1 | SubAbility$ R2\n" +
		"SVar:R2:DB$ Draw | NumCards$ 0 | ValidTgts$ Player | TargetUnique$ True | TargetMin$ 0 | TargetMax$ 1\n" +
		"Oracle:x\n"
}

// TestTargetUniqueAccumulatesAcrossTheChain pins the intra-chain half of the
// shared constraint at its CR 601.2c timing: both riders are announced as the
// spell is cast, and the second TargetUnique$ rider excludes not only the
// root's targets (there are none here — the root is untargeted) but also the
// target an EARLIER TargetUnique$ rider in the same chain chose. Without the
// accumulation the second rider offers both players again.
func TestTargetUniqueAccumulatesAcrossTheChain(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 7002, chainUniqueScript())
	addMana(t, e, 0, "C")
	castFirst(t, e, "cast")

	// CR 601.2c: both riders are targets, so both are announced as the spell
	// is cast -- before any priority is passed and before the spell resolves.
	// First rider's ask: both players.
	d := castSubAsk(t, e)
	first := pendingPlayerIDs(t, e)
	if !first[0] || !first[1] {
		t.Fatalf("first rider should offer both players: %+v", d.Options)
	}
	pick0 := -1
	for _, o := range d.Options {
		if o.Kind == "player" && int(o.Player) == 0 {
			pick0 = o.Index
		}
	}
	if pick0 < 0 {
		t.Fatalf("no seat-0 option in first rider ask: %+v", d.Options)
	}
	submitChoices(t, e, pick0)

	// Second rider's ask must exclude seat 0, chosen by the FIRST rider.
	d2 := castSubAsk(t, e)
	second := pendingPlayerIDs(t, e)
	if second[0] {
		t.Fatalf("second rider re-offers the first rider's target (seat 0): %+v", d2.Options)
	}
	if !second[1] {
		t.Fatalf("second rider offers no legal different player: %+v", d2.Options)
	}
	submitChoices(t, e, 0)
	// Both announced before the spell could resolve: it is still on the stack
	// and the resolution poses no target ask of its own.
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority || len(e.G.Stack) != 1 {
		t.Fatalf("after the announcement: pending %+v, stack %v; want priority with the spell on the stack", d, e.G.Stack)
	}
	for len(e.G.Stack) > 0 {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("the resolution posed %+v: every rider was already announced on cast", d)
		}
		submitChoices(t, e, passIndex(t, d))
	}
	replayCheck(t, e, cfg)
}

// cyberneticaScript is cybernetica_datasmith.txt's ability shape, verbatim
// except the ManaCost/Type lines: the root Draw declares "target player" and
// the SubAbility Token carries TargetUnique$ True — the corpus's encoding of
// the Oracle's "Another target player creates ...". This is the brief's
// api:Token instance (the api:Draw instance is Biomantic Mastery above).
const cyberneticaScript = "Name:Cybernetica Datasmith\nManaCost:1\nTypes:Artifact Creature Human Artificer\nPT:0/1\n" +
	"A:SP$ Draw | NumCards$ 1 | ValidTgts$ Player | SubAbility$ DBToken | SpellDescription$ Target player draws a card. Another target player creates a 4/4 colorless Robot artifact creature token with \"This creature can't block.\"\n" +
	"SVar:DBToken:DB$ Token | TokenScript$ c_4_4_a_robot_noblock | ValidTgts$ Player | TargetUnique$ True | TokenOwner$ ThisTargetedPlayer\n" +
	"Oracle:x\n"

const robotScript = "Name:Robot\nManaCost:\nTypes:Artifact Creature Robot\nPT:4/4\nOracle:x\n"

// TestCyberneticaTokenSubTargetExcludesParentTarget pins the api:Token half of
// the brief: the Token SubAbility's "another target player" must not create the
// token under the root's own target. It asserts the exclusion ask AND that the
// token actually lands under the DIFFERENT player (TokenOwner$
// ThisTargetedPlayer reads the sub's answered target).
func TestCyberneticaTokenSubTargetExcludesParentTarget(t *testing.T) {
	e, cfg, _ := newFixtureDeck(t, 7003, cyberneticaScript)
	e.G.Tokens["c_4_4_a_robot_noblock"] = card(t, robotScript)
	addMana(t, e, 0, "C")
	castFirst(t, e, "cast")

	// Root target ask: choose seat 1.
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("root target ask = %+v, want KTarget", d)
	}
	pick1 := -1
	for _, o := range d.Options {
		if o.Kind == "player" && int(o.Player) == 1 {
			pick1 = o.Index
		}
	}
	if pick1 < 0 {
		t.Fatalf("root ask does not offer seat 1: %+v", d.Options)
	}
	submitChoices(t, e, pick1)

	// CR 601.2c: "another target player" is announced with the first, as the
	// spell is cast.
	sub := castSubAsk(t, e)
	offered := pendingPlayerIDs(t, e)
	if offered[1] {
		t.Fatalf("token TargetUnique$ ask re-offers the parent's target (seat 1): %+v", sub.Options)
	}
	if !offered[0] {
		t.Fatalf("token ask offers no legal different player: %+v", sub.Options)
	}
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 30)

	// The token must exist under seat 0 (the DIFFERENT player), not seat 1.
	found := false
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Face() != nil && o.Face().Name == "Robot" && o.Zone == state.ZBattlefield {
			found = true
			if o.Controller != 0 {
				t.Fatalf("Robot token controller = %d, want seat 0 (the different target)", o.Controller)
			}
		}
	}
	if !found {
		t.Fatal("no Robot token on the battlefield after resolution")
	}
	replayCheck(t, e, cfg)
}

// p1p1On returns the P1P1 counter count an object carries (a test-local read
// of state's []Counter slice).
func p1p1On(t *testing.T, e *Engine, id state.ObjID) int32 {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil {
		return 0
	}
	for _, c := range o.Counters {
		if c.Kind == "P1P1" {
			return c.N
		}
	}
	return 0
}

// venomBlastScript is venom_blast.txt's script, verbatim except ManaCost
// (4 generic instead of 2 G G, so the fixture funds with one colour). The
// root pumps "target creature you control"; the SubAbility deals its power
// "to up to one OTHER target creature" (TargetUnique$ True).
const venomBlastScript = "Name:Venom Blast\nManaCost:4\nTypes:Instant\n" +
	"A:SP$ PutCounter | ValidTgts$ Creature.YouCtrl | TgtPrompt$ Select target creature you control | CounterType$ P1P1 | CounterNum$ 2 | SubAbility$ DBDamage | SpellDescription$ Put two +1/+1 counters on target creature you control. It deals damage equal to its power to up to one other target creature.\n" +
	"SVar:DBDamage:DB$ DealDamage | ValidTgts$ Creature | TargetUnique$ True | TargetMin$ 0 | TargetMax$ 1 | TgtPrompt$ Select up to one other target creature | NumDmg$ X | DamageSource$ ParentTarget | AILogic$ PowerDmg\n" +
	"SVar:X:ParentTargeted$CardPower\n" +
	"Oracle:x\n"

// TestVenomBlastNoOtherTargetDealsNoDamage pins the empty-exclusion outcome
// end to end on the real corpus card: in a two-player game whose ONLY legal
// creature is the root's own target, the TargetUnique$ filter leaves no
// candidate, so the sub must be a NO-OP — never the pre-fix fallthrough,
// where the DealDamage body re-read Ctx.Targets and the pumped creature dealt
// its (now boosted) damage to ITSELF and died. The Bear carries 4 power after
// the pump, so the pre-fix self-hit is lethal and the assertion cannot pass
// vacuously.
func TestVenomBlastNoOtherTargetDealsNoDamage(t *testing.T) {
	t.Parallel()
	bear := "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, _ := newFixtureDeck(t, 7004, venomBlastScript, bear)
	id := putCreature(t, e, 0, bear)
	addMana(t, e, 0, "GGGG")
	castFirst(t, e, "cast")

	// The root's own target ask: the Bear is the only creature offered.
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("root target ask = %+v, want KTarget", d)
	}
	bearOpt := -1
	for _, o := range d.Options {
		if o.Obj == id {
			bearOpt = o.Index
		}
	}
	if bearOpt < 0 {
		t.Fatalf("root ask does not offer the Bear: %+v", d.Options)
	}
	submitChoices(t, e, bearOpt)
	passUntilStackEmpty(t, e, 30)

	// The root's pump is the precondition the no-damage assertion depends on:
	// the resolution reached the sub at all.
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the pumped creature left the battlefield before the sub ran: %+v", o)
	}
	if got := p1p1On(t, e, id); got != 2 {
		t.Fatalf("pump precondition: Bear carries %d P1P1 counters, want 2", got)
	}
	// The assertion itself: no self-damage. Pre-fix the TargetUnique filter
	// emptied the ask's candidates, poseTargetsAsk fell through to Defined's
	// Ctx.Targets read, and the Bear dealt its 4 power to itself.
	if o := e.G.Obj(id); o == nil {
		t.Fatal("the Bear died: the excluded parent target was damaged by its own sub")
	} else if o.Damage != 0 {
		t.Fatalf("the excluded parent target took %d damage from its own sub", o.Damage)
	}
	replayCheck(t, e, cfg)
}

// withdrawScript is withdraw.txt's script verbatim (ManaCost U U kept — the
// fixture funds blue). The root bounces "target creature"; the SubAbility
// (DBBounce verbatim) bounces "another target creature ... unless its
// controller pays {1}".
const withdrawScript = "Name:Withdraw\nManaCost:U U\nTypes:Instant\n" +
	"A:SP$ ChangeZone | ValidTgts$ Creature | Origin$ Battlefield | Destination$ Hand | SubAbility$ DBBounce | SpellDescription$ Return target creature to its owner's hand. Then return another target creature to its owner's hand unless its controller pays {1}.\n" +
	"SVar:DBBounce:DB$ ChangeZone | ValidTgts$ Creature | TgtPrompt$ Select another target creature | TargetUnique$ True | Origin$ Battlefield | Destination$ Hand | UnlessCost$ 1 | UnlessPayer$ TargetedController\n" +
	"Oracle:x\n"

// TestWithdrawChangeZoneSubAsksAnotherTarget pins the ChangeZone half of the
// shared constraint on the real corpus card: the sub's ChangeZone targeting
// was inherited wholesale from the root (changeZoneChosenTargets' parent
// early-return) — with the root's creature already bounced, the Origin$
// precondition skipped it and the "another target creature" clause was dead:
// the second creature never moved. With the shared ask the sub poses its own
// question and the second creature is bounced.
func TestWithdrawChangeZoneSubAsksAnotherTarget(t *testing.T) {
	t.Parallel()
	bear := "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, _ := newFixtureDeck(t, 7005, withdrawScript, bear, bear)
	ida := putCreature(t, e, 0, bear)
	idb := putCreature(t, e, 0, bear)
	addMana(t, e, 0, "UU")
	castFirst(t, e, "cast")

	// The root's target ask: choose the first Bear.
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("root target ask = %+v, want KTarget", d)
	}
	aOpt := -1
	for _, o := range d.Options {
		if o.Obj == ida {
			aOpt = o.Index
		}
	}
	if aOpt < 0 {
		t.Fatalf("root ask does not offer the first Bear: %+v", d.Options)
	}
	submitChoices(t, e, aOpt)

	// The sub carries UnlessCost$ 1 / UnlessPayer$ TargetedController: the
	// gate asks the root target's controller (both Bears are seat 0's)
	// before the body. Decline, so the bounce runs.
	paidAsk := false
	for i := 0; i < 20; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while awaiting the unless pay ask")
		}
		if d.Kind == decision.KModes && d.ResumeKind == "unless_pay" {
			// The payer spent its {U}{U} casting Withdraw and has no source,
			// so the bounded offer is decline-only. Use the trailing decline
			// option to keep this test about the resumed target accumulator.
			submitChoices(t, e, d.Options[len(d.Options)-1].Index)
			paidAsk = true
			break
		}
		if d.Kind != decision.KPriority {
			break
		}
		for _, o := range d.Options {
			if o.Kind == "pass" {
				submitChoices(t, e, o.Index)
				break
			}
		}
	}
	if !paidAsk {
		t.Fatal("the unless gate never posed its pay ask")
	}

	// The sub's own target ask (KChoose, the "choice" resume arm): it must
	// NOT be skipped — pre-fix it never fired and the clause was dead.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choice" {
		t.Fatalf("sub target ask = %+v, want KChoose choice", d)
	}
	bOpt := -1
	for _, o := range d.Options {
		if o.Obj == idb {
			bOpt = o.Index
		}
	}
	if bOpt < 0 {
		t.Fatalf("sub ask does not offer the second Bear: %+v", d.Options)
	}
	if len(d.Options) != 1 {
		t.Fatalf("sub ask offers %d options, want exactly the one remaining creature: %+v", len(d.Options), d.Options)
	}
	submitChoices(t, e, bOpt)
	passUntilStackEmpty(t, e, 30)

	// Both Bears are in seat 0's hand (the root's and the sub's bounces).
	for _, id := range []state.ObjID{ida, idb} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZHand {
			t.Fatalf("Bear %d is %+v, want it bounced to its owner's hand", id, o)
		}
	}
	replayCheck(t, e, cfg)
}

// withdrawPumpRootScript keeps withdraw.txt's DBBounce SubAbility VERBATIM but
// replaces the root with a Pump (the Betrayal at the Vault root shape), so the
// parent target STAYS on the battlefield and the TargetUnique$ filter itself —
// not just the census — is load-bearing: without the exclusion the sub's ask
// would offer the parent again and a deterministic first-option host would
// bounce the PARENT instead of "another target creature".
const withdrawPumpRootScript = "Name:Withdraw Shape\nManaCost:2\nTypes:Instant\n" +
	"A:SP$ Pump | ValidTgts$ Creature.YouCtrl | TgtPrompt$ Select target creature you control | SubAbility$ DBBounce | SpellDescription$ Target creature you control gets +1/+0. Then return another target creature to its owner's hand.\n" +
	"SVar:DBBounce:DB$ ChangeZone | ValidTgts$ Creature | TgtPrompt$ Select another target creature | TargetUnique$ True | Origin$ Battlefield | Destination$ Hand\n" +
	"Oracle:x\n"

// TestWithdrawShapedSubExcludesTheBattlefieldParent pins the exclusion half
// against a parent target that is still a legal candidate: the ChangeZone sub
// must not offer the root's own creature.
func TestWithdrawShapedSubExcludesTheBattlefieldParent(t *testing.T) {
	t.Parallel()
	bear := "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, _ := newFixtureDeck(t, 7006, withdrawPumpRootScript, bear, bear)
	ida := putCreature(t, e, 0, bear)
	idb := putCreature(t, e, 0, bear)
	addMana(t, e, 0, "GG")
	castFirst(t, e, "cast")

	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("root target ask = %+v, want KTarget", d)
	}
	aOpt := -1
	for _, o := range d.Options {
		if o.Obj == ida {
			aOpt = o.Index
		}
	}
	if aOpt < 0 {
		t.Fatalf("root ask does not offer the first Bear: %+v", d.Options)
	}
	submitChoices(t, e, aOpt)

	for i := 0; i < 20; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while awaiting the sub target ask")
		}
		if d.Kind != decision.KPriority {
			break
		}
		for _, o := range d.Options {
			if o.Kind == "pass" {
				submitChoices(t, e, o.Index)
				break
			}
		}
	}

	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choice" {
		t.Fatalf("sub target ask = %+v, want KChoose choice", d)
	}
	for _, o := range d.Options {
		if o.Obj == ida {
			t.Fatalf("sub TargetUnique$ ask re-offers the pumped parent: %+v", d.Options)
		}
	}
	bOpt := -1
	for _, o := range d.Options {
		if o.Obj == idb {
			bOpt = o.Index
		}
	}
	if bOpt < 0 {
		t.Fatalf("sub ask offers no other creature: %+v", d.Options)
	}
	submitChoices(t, e, bOpt)
	passUntilStackEmpty(t, e, 30)

	// The parent was pumped and stayed; the OTHER bear was bounced.
	if o := e.G.Obj(ida); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the parent left the battlefield: %+v", o)
	}
	if o := e.G.Obj(idb); o == nil || o.Zone != state.ZHand {
		t.Fatalf("the other creature was not bounced: %+v", o)
	}
	replayCheck(t, e, cfg)
}

// suspensionRiderScript is chainUniqueScript with an intervening PLAIN (non-
// TargetUnique) target rider between the two unique ones: the third rider
// must exclude the first rider's target but not the second's.
func suspensionRiderScript() string {
	return "Name:Rider Suspension\nManaCost:1\nTypes:Sorcery\n" +
		"A:SP$ Draw | NumCards$ 1 | SubAbility$ R1\n" +
		"SVar:R1:DB$ Draw | NumCards$ 0 | ValidTgts$ Player | TargetUnique$ True | TargetMin$ 0 | TargetMax$ 1 | SubAbility$ R2\n" +
		"SVar:R2:DB$ Draw | NumCards$ 0 | ValidTgts$ Player | TargetMin$ 0 | TargetMax$ 1 | SubAbility$ R3\n" +
		"SVar:R3:DB$ Draw | NumCards$ 0 | ValidTgts$ Player | TargetUnique$ True | TargetMin$ 0 | TargetMax$ 1\n" +
		"Oracle:x\n"
}

// TestTargetUniqueSurvivesASuspensionBetweenRiders pins the exclusion across
// an intervening plain target ask, at the CR 601.2c timing (all three riders
// are announced on cast): the plain ask between two TargetUnique$ riders must
// not erase the earlier rider's pick, and its own answer must not enter the
// exclusion set. The resolution then poses no target ask at all.
func TestTargetUniqueSurvivesASuspensionBetweenRiders(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 7007, suspensionRiderScript())
	addMana(t, e, 0, "C")
	castFirst(t, e, "cast")
	handBefore := len(e.G.Zone(state.ZHand, 0))

	// CR 601.2c: all three riders are announced as the spell is cast.
	// Rider 1's ask: both players; pick seat 0.
	d := castSubAsk(t, e)
	if !pendingPlayerIDs(t, e)[0] || !pendingPlayerIDs(t, e)[1] {
		t.Fatalf("first rider should offer both players: %+v", d.Options)
	}
	submitChoices(t, e, 0)

	// Rider 2's ask (the intervening non-unique one): pick seat 1.
	d = castSubAsk(t, e)
	if !pendingPlayerIDs(t, e)[0] || !pendingPlayerIDs(t, e)[1] {
		t.Fatalf("second rider should offer both players (it is not unique): %+v", d.Options)
	}
	submitChoices(t, e, 1)

	// Rider 3's ask must still exclude seat 0, chosen by rider 1 BEFORE
	// rider 2's suspension.
	d = castSubAsk(t, e)
	if pendingPlayerIDs(t, e)[0] {
		t.Fatalf("third rider re-offers seat 0: the intervening ask dropped the exclusion: %+v", d.Options)
	}
	if !pendingPlayerIDs(t, e)[1] {
		t.Fatalf("third rider offers no legal different player: %+v", d.Options)
	}
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 30)
	// Precondition that the root spell actually resolved: the caster drew one.
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("root draw precondition: hand grew %d -> %d, want +1", handBefore, got)
	}
	replayCheck(t, e, cfg)
}
