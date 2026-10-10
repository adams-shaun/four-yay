package rules

// The AddStaticAbility$ grant read for the mode-scoped scans that never see a
// registered Continuous effect: the Panharmonicon echo scan
// (rules/statics_echo.go) and the combat board's Statics
// (rules/combat_board.go). Each test uses the REAL corpus granting card
// (The Masamune, Windcrag Siege, Tomik, Orzhov Lawmage); the recipients and
// probes are freely-authored fixtures per the licensing rule.

import (
	"fmt"
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

// playerCapSrc is a freely-authored enchantment with a PRINTED player-scoped
// AttackRestrict (the Crawlspace shape at a lower ceiling): it coexists with
// Tomik's granted battle-scoped one and must not relax it.
const playerCapSrc = "Name:Player Cap\nTypes:Enchantment\nOracle:x\n" +
	"S:Mode$ AttackRestrict | MaxAttackers$ 2 | ValidDefender$ You\n"

// TestTomikGrantedAttackRestrictAndPlayerCapBindTogether is the CR 508.1c
// smallest-ceiling case: the walker carries BOTH Tomik's granted
// battle-scoped cap (ValidDefender$ Card.Self, MaxAttackers$ 1) and a printed
// player-scoped cap (ValidDefender$ You, MaxAttackers$ 2). The walker option
// joins the battle-scoped group at the smaller cap -- the player cap never
// relaxes the walker cap to 2 -- while the player attacks keep their own
// player-scoped group untouched by the walker cap.
func TestTomikGrantedAttackRestrictAndPlayerCapBindTogether(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := combatEngine(t)
	tomik := onBoardCard(t, e, 1, lookup(t, reg, "Tomik, Orzhov Lawmage"))
	walker := onBoard(t, e, 1, targetWalkerFixture)
	capEnchant := onBoard(t, e, 1, playerCapSrc)
	a1 := onBoardReady(t, e, 0, "Name:Attack Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	a2 := onBoardReady(t, e, 0, "Name:Attack Wolf\nTypes:Creature Wolf\nPT:2/2\nOracle:x\n")
	a3 := onBoardReady(t, e, 0, "Name:Attack Elk\nTypes:Creature Elk\nPT:2/2\nOracle:x\n")
	a4 := onBoardReady(t, e, 0, "Name:Attack Ox\nTypes:Creature Ox\nPT:2/2\nOracle:x\n")
	// PRECONDITION: all three seat-1 permanents are on the battlefield under
	// seat 1, and the printed static's source is the enchantment, so the
	// player spec below really is a second, independent cap.
	for _, id := range []state.ObjID{tomik, walker, capEnchant} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
			t.Fatalf("precondition: seat-1 permanent %d is not on seat 1's battlefield: %+v", id, o)
		}
	}
	// The walker pair: BOTH caps fire, the battle-scoped group carries the
	// smaller ceiling (min(2,1) = 1), not the player cap's 2.
	group, limit := e.attackRestrictGroup(1, walker)
	if want := fmt.Sprintf("attack-restrict:%d:%d", 1, walker); group != want || limit != 1 {
		t.Fatalf("attackRestrictGroup(1, walker) = (%q,%d), want (%q,1): the player cap must not relax the walker cap", group, limit, want)
	}
	// The player pair: only the player cap fires, at its own ceiling of 2.
	if group, limit := e.attackRestrictGroup(1, 0); group != fmt.Sprintf("attack-restrict:%d", 1) || limit != 2 {
		t.Fatalf("attackRestrictGroup(1, player) = (%q,%d), want (%q,2)", group, limit, fmt.Sprintf("attack-restrict:%d", 1))
	}
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	idx := func(id state.ObjID, battle state.ObjID) int {
		for _, o := range d.Options {
			if o.Obj == id && o.Battle == battle {
				return o.Index
			}
		}
		return -1
	}
	iw1, iw2 := idx(a1, walker), idx(a2, walker)
	ip1, ip2 := idx(a3, 0), idx(a4, 0)
	if iw1 < 0 || iw2 < 0 || ip1 < 0 || ip2 < 0 {
		t.Fatalf("the walker and player pairs are not offered: %+v", d.Options)
	}
	battleGroup := d.Options[iw1].Group
	if want := fmt.Sprintf("attack-restrict:%d:%d", 1, walker); battleGroup != want || battleGroup != d.Options[iw2].Group {
		t.Fatalf("walker options carry groups %q / %q, want one shared %q", battleGroup, d.Options[iw2].Group, want)
	}
	playerGroup := d.Options[ip1].Group
	if want := fmt.Sprintf("attack-restrict:%d", 1); playerGroup != want || playerGroup != d.Options[ip2].Group {
		t.Fatalf("player options carry groups %q / %q, want one shared %q", playerGroup, d.Options[ip2].Group, want)
	}
	if cap := d.GroupCapFor(battleGroup); cap != 1 {
		t.Fatalf("the walker group cap is %d, want 1 (the smaller ceiling binds)", cap)
	}
	if cap := d.GroupCapFor(playerGroup); cap != 2 {
		t.Fatalf("the player group cap is %d, want 2", cap)
	}
	// Four attackers split 2 walker + 2 player: the walker group's cap of 1
	// rejects the pair even though the player cap alone (2) would allow it...
	reject := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{iw1, iw2, ip1, ip2}}
	if err := e.Submit(reject); err == nil {
		t.Fatal("two attackers at the walker were accepted alongside the player cap")
	}
	if e.Pending() == nil {
		t.Fatal("the rejected intent consumed the pending decision")
	}
	// ...and the mixed split 1 walker + 2 player is legal: the walker attack
	// does not consume the player group's count.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{iw1, ip1, ip2}}); err != nil {
		t.Fatalf("one walker attack plus two player attacks was rejected: %v", err)
	}
	drainCombatPriority(t, e)
	if !e.G.Obj(a1).IsAttacking || !e.G.Obj(a3).IsAttacking || !e.G.Obj(a4).IsAttacking {
		t.Fatal("the legal mixed declaration was not recorded")
	}
	if e.G.Obj(a2).IsAttacking {
		t.Fatal("the second walker attacker was declared despite the cap")
	}
}
