package rules

// The AddStaticAbility$ grant read for the mode-scoped scans that never see a
// registered Continuous effect: the Panharmonicon echo scan
// (rules/statics_echo.go) and the combat board's Statics
// (rules/combat_board.go). Each test uses the REAL corpus granting card
// (The Masamune, Windcrag Siege, Tomik, Orzhov Lawmage); the recipients and
// probes are freely-authored fixtures per the licensing rule.

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// deathWatcherSrc is a freely-authored creature with a "whenever a creature
// dies" trigger: the cause is the dying creature (a creature Bf->GY move),
// while the trigger's source -- the permanent The Masamune's grant lands on
// -- stays on the battlefield, which is exactly the shape the granted
// Panharmonicon's ValidCard$ Card.Self / ValidZone$ Battlefield pair needs.
const deathWatcherSrc = "Name:Death Watcher\nManaCost:1 G\nTypes:Creature Human Cleric\nPT:1/1\n" +
	"T:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Creature | Execute$ DbCount | TriggerDescription$ Whenever a creature dies, put a +1/+1 counter on CARDNAME.\n" +
	"SVar:DbCount:DB$ PutCounter | Defined$ Self | Counter$ P1P1\n"

// doomedBearSrc is the freely-authored creature that dies to fire the
// watcher's trigger.
const doomedBearSrc = "Name:Doomed Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// attackWatcherSrc is a freely-authored attacker whose attack trigger is the
// Windcrag Siege grant's ValidCause$ Creature / ValidCard$ Permanent.YouCtrl
// pair: the single declared attacker is both the cause and the trigger's
// source.
const attackWatcherSrc = "Name:Attack Watcher\nManaCost:1 R\nTypes:Creature Human Warrior\nPT:1/1\n" +
	"T:Mode$ Attacks | ValidCard$ Card.Self | Execute$ DbCount | TriggerDescription$ Whenever CARDNAME attacks, put a +1/+1 counter on it.\n" +
	"SVar:DbCount:DB$ PutCounter | Defined$ Self | Counter$ P1P1\n"

// TestMasamuneGrantedPanharmoniconDoublesEquippedCreaturesDeathTrigger is the
// granted-Panharmonicon leaf (real corpus The Masamune): the Equipment grants
// the equipped creature "If a creature dying causes a triggered ability of
// this creature ... to trigger, that ability triggers an additional time."
// The equipped Death Watcher's dies trigger fires once without the Equipment
// and twice with it attached.
func TestMasamuneGrantedPanharmoniconDoublesEquippedCreaturesDeathTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	masamune := lookup(t, reg, "The Masamune")
	watcher := card(t, deathWatcherSrc)
	victim := card(t, doomedBearSrc)
	e := corpusEngine(t, reg, []*cards.Card{masamune, watcher, victim, victim}, nil)
	wid := moveByName(t, e, 0, "Death Watcher", state.ZBattlefield)
	v1 := moveByName(t, e, 0, "Doomed Bear", state.ZBattlefield)
	// PRECONDITION: the watcher is the battlefield trigger source and the
	// victim is a battlefield creature; the counts below are otherwise
	// measuring a missing object.
	if o := e.G.Obj(wid); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Death Watcher is not on the battlefield: %+v", o)
	}
	if o := e.G.Obj(v1); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: first victim is not on the battlefield: %+v", o)
	}
	// Control: no Equipment, the death fires the watcher's trigger once.
	e.emit(events.Event{Kind: events.MoveZone, Obj: v1, From: state.ZBattlefield, To: state.ZGraveyard})
	answerQuiet(t, e, 60)
	if c := e.G.Obj(wid).Counter("P1P1"); c != 1 {
		t.Fatalf("the lone watcher has %d +1/+1 counters after one death, want 1", c)
	}
	// With The Masamune attached to the watcher: the next death doubles the
	// trigger (two more counters).
	mid := moveByName(t, e, 0, "The Masamune", state.ZBattlefield)
	e.emit(events.Event{Kind: events.Attach, Obj: mid, IDs: []state.ObjID{wid}})
	// PRECONDITION: the grant's host relation really holds (the granted
	// static's Source is the equipped creature, not the Equipment).
	if o := e.G.Obj(mid); o == nil || o.AttachedTo != wid {
		t.Fatalf("precondition: The Masamune is not attached to Death Watcher: %+v", o)
	}
	v2 := moveByName(t, e, 0, "Doomed Bear", state.ZBattlefield)
	if v2 == v1 || v2 == 0 {
		t.Fatalf("precondition: second victim id %d duplicates the first %d", v2, v1)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: v2, From: state.ZBattlefield, To: state.ZGraveyard})
	answerQuiet(t, e, 60)
	if c := e.G.Obj(wid).Counter("P1P1"); c != 3 {
		t.Fatalf("the equipped watcher has %d +1/+1 counters, want 3 (1 + the doubled trigger's 2)", c)
	}
}

// TestWindcragSiegeGrantedPanharmoniconDoublesAnAttackTrigger is the same
// grant read on the self-hosted siege shape (real corpus Windcrag Siege): its
// Mardu mode grants the Siege itself a Panharmonicon static over permanents
// you control, so the single declared attacker's attack trigger fires twice
// with the Mardu mode chosen and once without the Siege.
func TestWindcragSiegeGrantedPanharmoniconDoublesAnAttackTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	attackOnce := func(withSiege bool) int32 {
		t.Helper()
		e := corpusEngine(t, reg, []*cards.Card{card(t, attackWatcherSrc), card(t, attackWatcherSrc)}, nil)
		if withSiege {
			siege := onBoardCard(t, e, 0, lookup(t, reg, "Windcrag Siege"))
			e.G.Obj(siege).ChosenModes = []string{"Mardu"}
			// PRECONDITION: the Siege is the battlefield source the grant's
			// outer Card.Self+ChosenModeMardu gate reads, with Mardu recorded.
			if o := e.G.Obj(siege); o == nil || o.Zone != state.ZBattlefield || !slices.Contains(o.ChosenModes, "Mardu") {
				t.Fatalf("precondition: Windcrag Siege is not a battlefield Mardu permanent: %+v", o)
			}
		}
		atk := moveByName(t, e, 0, "Attack Watcher", state.ZBattlefield)
		e.G.Obj(atk).SummonSick = false
		driveToStep(t, e, 1, 0, state.StepDeclareAttackers)
		e.askAttackers()
		submitAttackersOnly(t, e, atk)
		answerQuiet(t, e, 60)
		return e.G.Obj(atk).Counter("P1P1")
	}
	if got := attackOnce(true); got != 2 {
		t.Fatalf("the attacker has %d +1/+1 counters with the Mardu Siege, want 2", got)
	}
	if got := attackOnce(false); got != 1 {
		t.Fatalf("the attacker has %d +1/+1 counters without the Siege, want 1", got)
	}
}

// TestTomikGrantedAttackRestrictCapsAttacksAtThePlaneswalker is the granted
// AttackRestrict leaf (real corpus Tomik, Orzhov Lawmage): Tomik grants each
// planeswalker you control "No more than one creature can attack this
// planeswalker each combat", whose ValidDefender$ Card.Self resolves against
// the granted static's source -- the planeswalker. Two attackers declared at
// the planeswalker are rejected; one is legal, and the same two at the
// player (which the restriction does not name) stay legal.
func TestTomikGrantedAttackRestrictCapsAttacksAtThePlaneswalker(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := combatEngine(t)
	tomik := onBoardCard(t, e, 1, lookup(t, reg, "Tomik, Orzhov Lawmage"))
	walker := onBoard(t, e, 1, targetWalkerFixture)
	a1 := onBoardReady(t, e, 0, "Name:Attack Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	a2 := onBoardReady(t, e, 0, "Name:Attack Wolf\nTypes:Creature Wolf\nPT:2/2\nOracle:x\n")
	// PRECONDITION: Tomik and the walker are seat 1's battlefield permanents
	// (the grant host) and the two attackers are seat 0's.
	if o := e.G.Obj(tomik); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: Tomik is not seat 1's battlefield permanent: %+v", o)
	}
	if o := e.G.Obj(walker); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: the walker is not seat 1's battlefield planeswalker: %+v", o)
	}
	// The one read the group cap, the validator and the wire all derive from.
	if group, limit := e.attackRestrictGroup(1, walker); group == "" || limit != 1 {
		t.Fatalf("attackRestrictGroup(1, walker) = (%q,%d), want a group with limit 1", group, limit)
	}
	// The restriction does not name the player: the player pair stays
	// uncapped, so the battle-scoped group cannot leak onto player attacks.
	if group, _ := e.attackRestrictGroup(1, 0); group != "" {
		t.Fatalf("attackRestrictGroup(1, player) = %q, want no group (the grant names the planeswalker only)", group)
	}
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	idx := func(id state.ObjID) int {
		for _, o := range d.Options {
			if o.Obj == id && o.Battle == walker {
				return o.Index
			}
		}
		return -1
	}
	i1, i2 := idx(a1), idx(a2)
	if i1 < 0 || i2 < 0 {
		t.Fatalf("the walker pair is not offered for both attackers: %+v", d.Options)
	}
	group := d.Options[i1].Group
	if group == "" || group != d.Options[i2].Group {
		t.Fatalf("walker options carry groups %q / %q, want one non-empty shared group", group, d.Options[i2].Group)
	}
	if cap := d.GroupCapFor(group); cap != 1 {
		t.Fatalf("the walker group cap is %d, want 1", cap)
	}
	// Two attackers at the planeswalker: rejected by the shared rule, and the
	// pending decision survives for a legal answer.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i1, i2}}); err == nil {
		t.Fatal("two attackers declared at the capped planeswalker were accepted")
	}
	if e.Pending() == nil {
		t.Fatal("the rejected intent consumed the pending decision")
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i1}}); err != nil {
		t.Fatalf("one attacker at the planeswalker was rejected: %v", err)
	}
	drainCombatPriority(t, e)
	if !e.G.Obj(a1).IsAttacking && !e.G.Obj(a2).IsAttacking {
		t.Fatal("neither attacker was declared after the legal answer")
	}
}
