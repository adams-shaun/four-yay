package rules

// Real-flow MustBlock carrier tests (task cli-20261006T023257Z-ec7b4d90).
//
// 858182dc8 certified all 27 api:MustBlock carriers at the SHAPE level, but
// several target filters and gates had no end-to-end test: a regression in
// them would still pass every census. Each test below drives the real card
// through the engine -- the ability is activated (or the spell cast), the
// target ask is answered from the offered options, the branch resolves, the
// duty binds, and the declare-blockers decision marks the required pair.
//
// The five filters/gates under test:
//
//   - Lineprancers      ValidTgts$ Creature.YouDontCtrl (the MustBlock sub-ask)
//   - Auriok Siege Sled ValidTgts$ Creature.Artifact
//   - Monstrous Step    TargetUnique$ True on the chained MustBlock
//   - Lurking Arynx     CheckSVar$ FormidableTest / SVarCompare$ GE8 offer gate
//   - Tower Above       the Animate-granted Attacks -> MustBlock trigger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/combat"
	"github.com/adams-shaun/gorge/state"
)

// mbMain parks e at seat 0's first main phase with priority, so an activated
// ability's offer loop runs.
func mbMain(t *testing.T, e *Engine) {
	t.Helper()
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1
	e.priorityRound()
}

// mbFund emits one ManaAdd event per symbol in symbols (each byte WUBRGC)
// into seat p's pool, so an activation's cost is payable and the offer loop
// reaches the ability -- what the test withholds must be the gate, not an
// unpayable cost.
func mbFund(e *Engine, p state.PlayerID, symbols string) {
	for _, c := range symbols {
		e.emit(events.Event{Kind: events.ManaAdd, Player: p, Counter: string(c), Amount: 1})
	}
}

// mbActivate submits the pending priority decision's "ability" option for obj
// and returns true; false when the offer loop withheld it (the gate's
// negative leg reads that).
func mbActivate(t *testing.T, e *Engine, obj state.ObjID) bool {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("not at priority: %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == obj {
			submitChoices(t, e, o.Index)
			return true
		}
	}
	return false
}

// mbDrain passes priority until the stack empties (an activation/trigger has
// finished resolving). It never answers a non-priority ask: the caller must
// answer any target ask before draining.
func mbDrain(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 40; i++ {
		if len(e.G.Stack) == 0 {
			return
		}
		d := e.Pending()
		if d == nil {
			e.priorityRound()
			continue
		}
		if d.Kind != decision.KPriority {
			return
		}
		passPriority(t, e)
	}
}

// mbAttackOn declares attacker (on seat 0) attacking defender and parks the
// game at the declare-blockers step, so the duty's Required flag can be read
// off the real declaration decision.
func mbAttackOn(t *testing.T, e *Engine, defender state.PlayerID, attacker state.ObjID) {
	t.Helper()
	o := e.G.Obj(attacker)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: attacker %d is not on the battlefield", attacker)
	}
	o.SummonSick = false
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: defender, IDs: []state.ObjID{attacker}})
	e.G.Step = state.StepDeclareBlockers
}

// mbTargetAsk asserts a target decision is pending and returns its offered
// object ids.
func mbTargetAsk(t *testing.T, e *Engine) (*decision.Decision, map[state.ObjID]bool) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target ask, got %+v", d)
	}
	offered := map[state.ObjID]bool{}
	for _, o := range d.Options {
		offered[o.Obj] = true
	}
	return d, offered
}

// TestMustBlockLineprancersYouDontCtrlFilter drives Lineprancers' printed
// {3}{G} ability through the engine and pins the MustBlock sub-ability's
// `ValidTgts$ Creature.YouDontCtrl` filter (CR 509.1c / 601.2c): the blocker
// ask offers the opponent's creature and never a creature the activator
// controls, the resolved duty binds the chosen opponent creature to the
// pumped parent, and the declare-blockers decision marks that pair Required.
//
// The printed parent target is `Creature.YouCtrl+stickeredWith PT+Other`.
// Sticker placement (PutSticker) and the `stickeredWith` predicate are not
// implemented in this build, so the real card's ability is un-offerable and
// its sub-ask unreachable. The test therefore parses the card's OWN script
// with ONLY that unimplemented parent predicate removed; the MustBlock body
// (`DBMustBlock`) exercised below is byte-for-byte the card's.
func TestMustBlockLineprancersYouDontCtrlFilter(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", ".cards", "cardsfolder", "l", "lineprancers.txt"))
	if err != nil {
		t.Fatalf("read Lineprancers corpus script: %v", err)
	}
	parentFilter := "Creature.YouCtrl+stickeredWith PT+Other"
	if !strings.Contains(string(raw), parentFilter) {
		t.Fatalf("precondition: Lineprancers parent filter %q changed; the sticker gap may be closed and this test's workaround stale", parentFilter)
	}
	card, errs := cards.ParseBytes("lineprancers.test.txt", []byte(strings.Replace(string(raw), parentFilter, "Creature.YouCtrl+Other", 1)))
	if len(errs) != 0 {
		t.Fatalf("parse Lineprancers: %v", errs)
	}
	if errs = card.Link(); len(errs) != 0 {
		t.Fatalf("link Lineprancers: %v", errs)
	}
	// Precondition: the MustBlock body under test is the card's own and still
	// carries the YouDontCtrl filter.
	body := cards.ResolveSVar(card.Faces[0].SVars, "DBMustBlock")
	if body == nil || body.API != "MustBlock" || body.ParamStr(cards.PKValidTgts) != "Creature.YouDontCtrl" || body.ParamStr(cards.PKDefinedAttacker) != "ParentTarget" {
		t.Fatalf("precondition: Lineprancers DBMustBlock changed: %#v", body)
	}

	e := layerEngine(t)
	src := onBoardCard(t, e, 0, card)
	parent := onBoard(t, e, 0, "Name:My Pumped Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	ownBystander := onBoard(t, e, 0, "Name:My Other Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	theirBlocker := onBoard(t, e, 1, "Name:Their Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if src == parent || parent == ownBystander || ownBystander == theirBlocker {
		t.Fatal("precondition: distinct source, parent, own bystander and opponent creature required")
	}
	mbFund(e, 0, "CCCG")
	mbMain(t, e)
	if !mbActivate(t, e, src) {
		t.Fatalf("precondition: Lineprancers' ability was not offered on a legal board: %+v", e.Pending())
	}
	// Root target: the creature you control that gets pumped and attacked.
	_, offered := mbTargetAsk(t, e)
	if !offered[parent] || offered[theirBlocker] || offered[src] {
		t.Fatalf("precondition: parent-target ask wrong: offered=%v parent=%d source=%d", offered, parent, src)
	}
	submitTarget(t, e, parent)

	// The sub-ask: the MustBlock's ValidTgts$ Creature.YouDontCtrl filter.
	_, sub := mbTargetAsk(t, e)
	if !sub[theirBlocker] {
		t.Fatalf("YouDontCtrl filter dropped the opponent's creature: offered=%v", sub)
	}
	for label, id := range map[string]state.ObjID{"pumped parent": parent, "own bystander": ownBystander, "Lineprancers": src} {
		if sub[id] {
			t.Fatalf("YouDontCtrl filter offered the %s (%d): offered=%v", label, id, sub)
		}
	}
	submitTarget(t, e, theirBlocker)
	mbDrain(t, e)

	b := asBoard(e)
	if !combat.MustBlockPairRequired(b, theirBlocker, parent) || combat.MustBlockPairRequired(b, theirBlocker, ownBystander) {
		t.Fatal("resolved duty is not scoped to the chosen opponent creature and the pumped parent")
	}
	mbAttackOn(t, e, 1, parent)
	bd := askBlockersFresh(t, e)
	if bd == nil || bd.Kind != decision.KBlockers {
		t.Fatalf("precondition: no blockers decision: %+v", bd)
	}
	pair := findBlockOption(bd, theirBlocker, parent)
	if pair == nil || !pair.Required || !pair.BlockMust || bd.RequiredQuota() != 1 {
		t.Fatalf("Lineprancers offered pair incorrect: %+v", bd.Options)
	}
	if err := e.validateBlockers(bd, decision.Intent{Player: bd.Player, Seq: bd.Seq, Choices: []int{pair.Index}}); err != nil {
		t.Fatalf("engine rejected the required block: %v", err)
	}
	if err := e.validateBlockers(bd, decision.Intent{Player: bd.Player, Seq: bd.Seq, Choices: nil}); err == nil {
		t.Fatal("engine accepted an empty declaration omitting the required blocker")
	}
}

// TestMustBlockAuriokSiegeSledArtifactFilter activates the real Auriok Siege
// Sled's {1} MustBlock ability and pins its `ValidTgts$ Creature.Artifact`
// filter: only artifact creatures are offered (the Sled itself and the
// opponent's artifact creature), never a non-artifact creature. The resolved
// duty binds the chosen artifact creature to the Sled and the declaration
// marks that pair Required (CR 509.1c).
func TestMustBlockAuriokSiegeSledArtifactFilter(t *testing.T) {
	e := layerEngine(t)
	sled := enterOnBattlefield(t, e, 0, corpusCard(t, "Auriok Siege Sled"))
	theirArtifact := onBoard(t, e, 1, "Name:Their Golem\nTypes:Artifact Creature Golem\nPT:2/2\nOracle:x\n")
	theirBears := onBoard(t, e, 1, "Name:Their Bears\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if sled == theirArtifact || theirArtifact == theirBears {
		t.Fatal("precondition: distinct Sled, artifact creature and non-artifact creature required")
	}
	mbFund(e, 0, "C")
	mbMain(t, e)
	if !mbActivate(t, e, sled) {
		t.Fatalf("precondition: Auriok Siege Sled's ability was not offered: %+v", e.Pending())
	}
	_, offered := mbTargetAsk(t, e)
	if !offered[sled] || !offered[theirArtifact] {
		t.Fatalf("Artifact filter dropped an artifact creature: offered=%v sled=%d artifact=%d", offered, sled, theirArtifact)
	}
	if offered[theirBears] {
		t.Fatalf("Artifact filter offered the non-artifact creature (%d): offered=%v", theirBears, offered)
	}
	submitTarget(t, e, theirArtifact)
	mbDrain(t, e)

	b := asBoard(e)
	if !combat.MustBlockPairRequired(b, theirArtifact, sled) {
		t.Fatal("resolved duty did not bind the chosen artifact creature to the Sled")
	}
	mbAttackOn(t, e, 1, sled)
	bd := askBlockersFresh(t, e)
	if bd == nil || bd.Kind != decision.KBlockers {
		t.Fatalf("precondition: no blockers decision: %+v", bd)
	}
	pair := findBlockOption(bd, theirArtifact, sled)
	if pair == nil || !pair.Required || !pair.BlockMust || bd.RequiredQuota() != 1 {
		t.Fatalf("Auriok Siege Sled offered pair incorrect: %+v", bd.Options)
	}
	if err := e.validateBlockers(bd, decision.Intent{Player: bd.Player, Seq: bd.Seq, Choices: []int{pair.Index}}); err != nil {
		t.Fatalf("engine rejected the required artifact block: %v", err)
	}
}

// TestMustBlockMonstrousStepTargetUnique casts the real Monstrous Step and
// pins `TargetUnique$ True` on its chained MustBlock (CR 601.2c): after the
// parent target is chosen, the "other target creature" sub-ask must EXCLUDE
// that parent while still offering every other creature. The resolved duty
// binds the chosen blocker to the parent, and the declaration marks it
// Required.
func TestMustBlockMonstrousStepTargetUnique(t *testing.T) {
	reg := freshCorpusRegistry(t, "m/monstrous_step.txt", "g/grizzly_bears.txt", "f/forest.txt")
	e, _ := etbreplEngine(t, reg, "Monstrous Step", "Grizzly Bears", "Grizzly Bears")
	parent := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	ownOther := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	theirBlocker := searchMoveByNameSeat(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	if parent == ownOther || parent == theirBlocker || ownOther == theirBlocker {
		t.Fatal("precondition: distinct parent, own other creature and opponent blocker required")
	}
	spell := searchMoveByName(t, e, "Monstrous Step", state.ZHand)
	addMana(t, e, 0, "GGGGG")
	submitChoices(t, e, castOptionFor(t, e, spell).Index)
	_, offered := mbTargetAsk(t, e)
	if !offered[parent] || !offered[ownOther] || !offered[theirBlocker] {
		t.Fatalf("precondition: Monstrous Step parent ask wrong: offered=%v", offered)
	}
	submitTarget(t, e, parent)

	// The chained MustBlock ask. TargetUnique$ must remove the parent.
	_, sub := mbTargetAsk(t, e)
	if sub[parent] {
		t.Fatalf("TargetUnique$ failed: the parent target %d was offered as the blocker: offered=%v", parent, sub)
	}
	if !sub[ownOther] || !sub[theirBlocker] {
		t.Fatalf("TargetUnique$ over-excluded (non-parent creatures dropped): offered=%v", sub)
	}
	submitTarget(t, e, theirBlocker)
	mbDrain(t, e)

	b := asBoard(e)
	if !combat.MustBlockPairRequired(b, theirBlocker, parent) || combat.MustBlockPairRequired(b, theirBlocker, ownOther) {
		t.Fatal("resolved duty is not scoped to the chosen blocker and the parent target")
	}
	mbAttackOn(t, e, 1, parent)
	bd := askBlockersFresh(t, e)
	if bd == nil || bd.Kind != decision.KBlockers {
		t.Fatalf("precondition: no blockers decision: %+v", bd)
	}
	pair := findBlockOption(bd, theirBlocker, parent)
	if pair == nil || !pair.Required || !pair.BlockMust || bd.RequiredQuota() != 1 {
		t.Fatalf("Monstrous Step offered pair incorrect: %+v", bd.Options)
	}
	if err := e.validateBlockers(bd, decision.Intent{Player: bd.Player, Seq: bd.Seq, Choices: []int{pair.Index}}); err != nil {
		t.Fatalf("engine rejected the required block: %v", err)
	}
	if err := e.validateBlockers(bd, decision.Intent{Player: bd.Player, Seq: bd.Seq, Choices: nil}); err == nil {
		t.Fatal("engine accepted an empty declaration omitting the required blocker")
	}
}

// TestMustBlockLurkingArynxCheckSVarGate pins the Formidable offer gate on
// the real Lurking Arynx: `CheckSVar$ FormidableTest / SVarCompare$ GE8`
// withholds the {2}{G} ability below 8 total power and offers it at 8. The
// high-power leg then activates, targets the opponent's creature, resolves
// the duty and reads the Required declaration; the low-power leg asserts the
// ability is absent while the Sled-shape preconditions (source on battlefield,
// cost funded) still hold, so the absence is the gate and not a bad setup.
func TestMustBlockLurkingArynxCheckSVarGate(t *testing.T) {
	// High-power leg: Arynx (3) + a 5-power filler = 8, the gate holds.
	e := layerEngine(t)
	arynx := enterOnBattlefield(t, e, 0, corpusCard(t, "Lurking Arynx"))
	onBoard(t, e, 0, "Name:Five Power Filler\nTypes:Creature Beast\nPT:5/5\nOracle:x\n")
	theirBlocker := onBoard(t, e, 1, "Name:Their Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	mbFund(e, 0, "CCG")
	mbMain(t, e)
	if !mbActivate(t, e, arynx) {
		t.Fatalf("Formidable gate withheld the ability at 8 total power: %+v", e.Pending())
	}
	_, offered := mbTargetAsk(t, e)
	if !offered[theirBlocker] || !offered[arynx] {
		t.Fatalf("precondition: Arynx target ask wrong: offered=%v", offered)
	}
	submitTarget(t, e, theirBlocker)
	mbDrain(t, e)
	b := asBoard(e)
	if !combat.MustBlockPairRequired(b, theirBlocker, arynx) {
		t.Fatal("resolved duty did not bind the chosen creature to Arynx")
	}
	mbAttackOn(t, e, 1, arynx)
	bd := askBlockersFresh(t, e)
	if bd == nil || bd.Kind != decision.KBlockers {
		t.Fatalf("precondition: no blockers decision: %+v", bd)
	}
	pair := findBlockOption(bd, theirBlocker, arynx)
	if pair == nil || !pair.Required || !pair.BlockMust || bd.RequiredQuota() != 1 {
		t.Fatalf("Lurking Arynx offered pair incorrect: %+v", bd.Options)
	}
	if err := e.validateBlockers(bd, decision.Intent{Player: bd.Player, Seq: bd.Seq, Choices: []int{pair.Index}}); err != nil {
		t.Fatalf("engine rejected the required Arynx block: %v", err)
	}

	// Low-power leg: only Arynx (3 power) -- the gate must withhold it.
	e2 := layerEngine(t)
	arynx2 := enterOnBattlefield(t, e2, 0, corpusCard(t, "Lurking Arynx"))
	onBoard(t, e2, 1, "Name:Their Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	mbFund(e2, 0, "CCG")
	mbMain(t, e2)
	if o := e2.G.Obj(arynx2); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: second Arynx not on battlefield: %+v", o)
	}
	d := e2.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("precondition: second engine not at priority: %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == arynx2 {
			t.Fatal("Formidable gate offered Lurking Arynx's ability at only 3 total power")
		}
	}
}

// TestMustBlockTowerAboveGrantedTrigger casts the real Tower Above and pins
// the Animate-granted "When this creature attacks, target creature blocks it
// this turn if able" trigger (CR 509.1c): before the grant no attack trigger
// exists, and after it a real attack declaration queues the trigger, whose
// target ask resolves the MustBlock duty. The declaration then marks the
// chosen blocker's pair Required.
func TestMustBlockTowerAboveGrantedTrigger(t *testing.T) {
	reg := freshCorpusRegistry(t, "t/tower_above.txt", "g/grizzly_bears.txt", "f/forest.txt")
	e, _ := etbreplEngine(t, reg, "Tower Above", "Grizzly Bears", "Grizzly Bears")
	recipient := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	theirBlocker := searchMoveByNameSeat(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	if recipient == theirBlocker {
		t.Fatal("precondition: distinct recipient and opponent blocker required")
	}
	spell := searchMoveByName(t, e, "Tower Above", state.ZHand)
	addMana(t, e, 0, "GGGGGG")
	submitChoices(t, e, castOptionFor(t, e, spell).Index)
	d := e.Pending()
	for d != nil && d.Kind == decision.KChoose && isPipAsk(d.Options) {
		chooseKindOption(t, e, d, "pay_G")
		d = e.Pending()
	}
	_, offered := mbTargetAsk(t, e)
	if !offered[recipient] {
		t.Fatalf("Tower Above target ask dropped the intended recipient: offered=%v", offered)
	}
	submitTarget(t, e, recipient)
	mbDrain(t, e)

	// Precondition: the recipient's attack must raise the granted trigger.
	// If Tower Above's Animate grant were gone, no trigger would queue and
	// mbTargetAsk below would fail -- so its success is the grant's proof.
	// Drive the REAL declare-attackers flow so the attack trigger queues and
	// resolves above the combat step, exactly as a seat's declaration does.
	e.G.Active, e.G.Priority = 0, 0
	e.G.Step = state.StepDeclareAttackers
	e.G.Obj(recipient).SummonSick = false
	e.pending = nil
	e.askAttackers()
	submitAttackersAt(t, e, [][2]state.ObjID{{recipient, 1}})
	for i := 0; i < 16; i++ {
		td := e.Pending()
		if td == nil || td.Kind != decision.KPriority {
			break
		}
		passPriority(t, e)
	}
	_, trig := mbTargetAsk(t, e)
	if !trig[theirBlocker] || !trig[recipient] {
		t.Fatalf("granted trigger target ask wrong: offered=%v", trig)
	}
	submitTarget(t, e, theirBlocker)
	mbDrain(t, e)

	b := asBoard(e)
	if !combat.MustBlockPairRequired(b, theirBlocker, recipient) {
		t.Fatal("granted trigger's duty did not bind the chosen blocker to the attacking recipient")
	}
	bd := askBlockersFresh(t, e)
	if bd == nil || bd.Kind != decision.KBlockers {
		t.Fatalf("precondition: no blockers decision: %+v", bd)
	}
	pair := findBlockOption(bd, theirBlocker, recipient)
	if pair == nil || !pair.Required || !pair.BlockMust || bd.RequiredQuota() != 1 {
		t.Fatalf("Tower Above offered pair incorrect: %+v", bd.Options)
	}
	if err := e.validateBlockers(bd, decision.Intent{Player: bd.Player, Seq: bd.Seq, Choices: []int{pair.Index}}); err != nil {
		t.Fatalf("engine rejected the required Tower Above block: %v", err)
	}
}
