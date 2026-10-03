package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func optionIndexByLabel(d *decision.Decision, label string) int {
	for _, o := range d.Options {
		if o.Label == label {
			return o.Index
		}
	}
	return -1
}

// TestInvokeTheAncientsCounterTypePerDefinedChoice drives the real spell's
// remembered two-token chain. Every token gets its own choice; the first
// answer must survive while the second ask is pending.
func TestInvokeTheAncientsCounterTypePerDefinedChoice(t *testing.T) {
	t.Parallel()
	invoke := mustCorpusCardT(t, "Invoke the Ancients")
	e, cfg := tokenReplGame(t, 917, invoke)
	id := moveSeededCard(t, e, 0, invoke, state.ZHand)
	addMana(t, e, 0, "GGGGG")
	castFixture(t, e, id, -1)
	d := passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "counter_kind" || d.Min != 1 || d.Max != 1 || len(d.Options) != 3 {
		t.Fatalf("first kind choice = %+v, want one of three individual counter kinds", d)
	}
	vigilance := optionIndexByLabel(d, "Vigilance")
	if vigilance < 0 {
		t.Fatalf("first kind choices = %+v, want Vigilance", d.Options)
	}
	submitChoices(t, e, vigilance)
	d = passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "counter_kind" || len(d.Options) != 3 {
		t.Fatalf("second kind choice = %+v, want a distinct sequential choice", d)
	}
	reach := optionIndexByLabel(d, "Reach")
	if reach < 0 || reach == vigilance {
		t.Fatalf("second choices = %+v, want a distinct Reach option", d.Options)
	}
	submitChoices(t, e, reach)
	passUntilStackEmpty(t, e, 40)
	var spirits []state.ObjID
	for _, oid := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(oid)
		if o != nil && o.IsToken && o.Face() != nil && o.Face().Name == "Spirit Token" {
			spirits = append(spirits, oid)
		}
	}
	if len(spirits) != 2 {
		t.Fatalf("precondition: live Spirit tokens = %d, want 2", len(spirits))
	}
	if got := e.G.Obj(spirits[0]).Counter("Vigilance"); got != 1 || e.G.Obj(spirits[0]).Counter("Reach") != 0 {
		t.Fatalf("first Spirit counters Vigilance/Reach = %d/%d, want 1/0", got, e.G.Obj(spirits[0]).Counter("Reach"))
	}
	if got := e.G.Obj(spirits[1]).Counter("Reach"); got != 1 || e.G.Obj(spirits[1]).Counter("Vigilance") != 0 {
		t.Fatalf("second Spirit counters Reach/Vigilance = %d/%d, want 1/0", got, e.G.Obj(spirits[1]).Counter("Vigilance"))
	}
	replayCheck(t, e, cfg)
}

// TestCrystallineGiantCounterTypeChoice exercises the real begin-combat
// trigger: it uses RNG and creates no player KChoose while avoiding a kind
// the Giant already has.
// TestCounterTypeChoiceDoesNotLeakIntoSubAbility is the fx42 scoping
// regression: one resolved list answer must not suppress a later list ask in
// the same Resolve chain.
func TestCounterTypeChoiceDoesNotLeakIntoSubAbility(t *testing.T) {
	t.Parallel()
	chain := card(t, "Name:Counter Chain\nTypes:Artifact\n"+
		"A:AB$ PutCounter | Cost$ T | Defined$ Self | CounterType$ P1P1,First Strike | SubAbility$ DBSecond | SpellDescription$ x\n"+
		"SVar:DBSecond:DB$ PutCounter | Defined$ Self | CounterType$ Vigilance,Reach | SpellDescription$ x\nOracle:x\n")
	e, cfg := tokenReplGame(t, 920, chain)
	id := moveSeededCard(t, e, 0, chain, state.ZBattlefield)
	addMana(t, e, 0, "")
	submitChoices(t, e, abilityOption(t, e, id, 0).Index)
	d := passUntilNonPriority(t, e, 30)
	if d == nil || d.ResumeKind != "counter_kind" || optionIndexByLabel(d, "First Strike") < 0 {
		t.Fatalf("first chained kind ask = %+v, want First Strike", d)
	}
	submitChoices(t, e, optionIndexByLabel(d, "First Strike"))
	d = passUntilNonPriority(t, e, 30)
	if d == nil || d.ResumeKind != "counter_kind" || optionIndexByLabel(d, "Reach") < 0 {
		t.Fatalf("second chained kind ask = %+v, want independent Reach choice", d)
	}
	submitChoices(t, e, optionIndexByLabel(d, "Reach"))
	passUntilStackEmpty(t, e, 30)
	if got := e.G.Obj(id).Counter("First Strike"); got != 1 || e.G.Obj(id).Counter("Reach") != 1 {
		t.Fatalf("chained counters First Strike/Reach = %d/%d, want 1/1", got, e.G.Obj(id).Counter("Reach"))
	}
	replayCheck(t, e, cfg)
}

func TestCrystallineGiantCounterTypeChoice(t *testing.T) {
	t.Parallel()
	giant := mustCorpusCardT(t, "Crystalline Giant")
	e, cfg := tokenReplGame(t, 921, giant)
	id := moveSeededCard(t, e, 0, giant, state.ZBattlefield)
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "Flying", Amount: 1})
	e.pending = nil
	if got := e.G.Obj(id).Counter("Flying"); got != 1 {
		t.Fatalf("precondition: Giant Flying = %d, want 1", got)
	}
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})
	e.putTriggersOnStack()
	answerTriggerOrders(t, e)
	e.priorityRound()
	passUntilStackEmpty(t, e, 40)
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("random counter kind must not pose KChoose: %+v", d)
	}
	if got := e.G.Obj(id).Counter("Flying"); got != 1 {
		t.Fatalf("random counter duplicated Flying: got %d, want unchanged 1", got)
	}
	newKinds := []string{"First Strike", "Deathtouch", "Hexproof", "Lifelink", "Menace", "Reach", "Trample", "Vigilance", "P1P1"}
	added := int32(0)
	for _, kind := range newKinds {
		added += e.G.Obj(id).Counter(kind)
	}
	if added != 1 {
		t.Fatalf("random counter additions across eligible non-Flying kinds = %d, want exactly 1", added)
	}
	replayCheck(t, e, cfg)
}

// TestDismantleCounterTypeChoiceContinuesAfterDeterministicRecipient proves a
// fixed Choices$ recipient does not suppress its subsequent list-kind choice.
func TestDismantleCounterTypeChoiceContinuesAfterDeterministicRecipient(t *testing.T) {
	t.Parallel()
	dismantle := mustCorpusCardT(t, "Dismantle")
	// Indestructible keeps the target's counters live for Dismantle's
	// ConditionDefined$ Targeted check, while its opponent controller leaves
	// Remaining Relic as the one deterministic Artifact.YouCtrl recipient.
	target := card(t, "Name:Dismantled Relic\nTypes:Artifact\nK:Indestructible\nOracle:x\n")
	recipient := card(t, "Name:Remaining Relic\nTypes:Artifact\nOracle:x\n")
	e, cfg := tokenReplGameSeats(t, 922, []*cards.Card{dismantle, recipient}, []*cards.Card{target})
	dismantleID := moveSeededCard(t, e, 0, dismantle, state.ZHand)
	targetID := moveSeededCard(t, e, 1, target, state.ZBattlefield)
	recipientID := moveSeededCard(t, e, 0, recipient, state.ZBattlefield)
	e.emit(events.Event{Kind: events.CounterChange, Obj: targetID, Counter: "P1P1", Amount: 2})
	e.pending = nil
	if o := e.G.Obj(targetID); o == nil || o.Zone != state.ZBattlefield || o.Counter("P1P1") != 2 {
		t.Fatalf("precondition: Dismantle target = %+v, want battlefield Artifact with two P1P1", o)
	}
	if o := e.G.Obj(recipientID); o == nil || o.Zone != state.ZBattlefield || o.Counter("P1P1") != 0 || o.Counter("CHARGE") != 0 {
		t.Fatalf("precondition: only recipient = %+v, want clean battlefield Artifact", o)
	}
	addMana(t, e, 0, "RRR")
	d := e.Pending()
	cast := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == dismantleID {
			cast = o.Index
		}
	}
	if cast < 0 {
		t.Fatalf("precondition: Dismantle cast option absent: %+v", d.Options)
	}
	submitChoices(t, e, cast)
	targetObject(t, e, targetID)
	d = passUntilNonPriority(t, e, 30)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "counter_kind" || d.Min != 1 || d.Max != 1 || len(d.Options) != 2 {
		t.Fatalf("Dismantle deterministic recipient must continue to kind ask: %+v", d)
	}
	charge := optionIndexByLabel(d, "CHARGE")
	if charge < 0 {
		t.Fatalf("Dismantle kind options = %+v, want CHARGE", d.Options)
	}
	if o := e.G.Obj(targetID); o == nil || o.Zone != state.ZBattlefield || o.Counter("P1P1") != 2 {
		t.Fatalf("precondition: indestructible target = %+v, want live with two P1P1 before kind answer", o)
	}
	submitChoices(t, e, charge)
	passUntilStackEmpty(t, e, 30)
	if got := e.G.Obj(recipientID).Counter("CHARGE"); got != 2 {
		t.Fatalf("recipient CHARGE = %d, want 2 from selected individual kind", got)
	}
	if got := e.G.Obj(recipientID).Counter("P1P1"); got != 0 {
		t.Fatalf("recipient P1P1 = %d, want 0 after choosing CHARGE", got)
	}
	if got := e.G.Obj(recipientID).Counter("P1P1,CHARGE"); got != 0 {
		t.Fatalf("recipient composite counter = %d, want 0", got)
	}
	replayCheck(t, e, cfg)
}
