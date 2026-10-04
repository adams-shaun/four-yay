package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// CR 603.3d says a triggered ability's targets -- its SubAbility$ chain
// links' included -- are chosen as the controller puts it on the stack, not as
// each link resolves. rules/trigger_subtargets.go asks that whole chain at
// placement for every non-modal trigger, whichever push minted the ability
// object: a printed face trigger, a GRANTED trigger (a static's AddTrigger$),
// a DELAYED trigger (a Mode$ Phase or event registration) and a MERGED pile's
// under-card trigger (CR 702.140d). These tests drive one ability object per
// push kind and assert the chain's second target is asked BEFORE any player
// receives priority (a KTarget with ResumeKind "trig_sub" is pending where a
// KPriority would be), and that a mandatory link with no legal target removes
// the ability (the same CR 603.3d exit the root ask uses).

// trigChainHostSrc carries a two-link targeting chain as SVars, the shape a
// granted, delayed or printed trigger's Execute$ names. The root and the link
// each target a creature you control, so the second ask is always reachable
// with two of your own creatures on the battlefield.
const trigChainHostSrc = "Name:Chain Host\nManaCost:1\nTypes:Creature\nPT:1/1\n" +
	"SVar:TrigX:DB$ Pump | ValidTgts$ Creature.YouCtrl | SubAbility$ TrigY\n" +
	"SVar:TrigY:DB$ Pump | ValidTgts$ Creature.YouCtrl\nOracle:x\n"

// trigChainFizzleSrc is the mandatory-link-with-no-legal-target shape: the
// root targets a creature you control, and the link mandates a creature an
// opponent controls (TargetMin$ 1). With no opponent creature the link has
// zero legal candidates, so CR 603.3d removes the ability from the stack.
const trigChainFizzleSrc = "Name:Fizzle Host\nManaCost:1\nTypes:Creature\nPT:1/1\n" +
	"SVar:TrigX:DB$ Pump | ValidTgts$ Creature.YouCtrl | SubAbility$ TrigY\n" +
	"SVar:TrigY:DB$ Pump | ValidTgts$ Creature.OppCtrl | TargetMin$ 1\nOracle:x\n"

// trigChainBody resolves the named Execute$ SVar off id's active face, the
// same body events.Apply mints the ability from.
func trigChainBody(t *testing.T, e *Engine, id state.ObjID, name string) *cards.SA {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		t.Fatalf("no face for source %d", id)
	}
	sa := cards.ResolveSVar(o.Face().SVars, name)
	if sa == nil {
		t.Fatalf("face %q has no SVar %q", o.Face().Name, name)
	}
	return sa
}

// answerRootThenExpectChainAsk answers the pending root target ask with want
// and asserts the very next pending decision is the chain link's placement ask
// (KTarget, ResumeKind "trig_sub") for the same ability object -- never a
// priority decision. It returns the ability object id.
func answerRootThenExpectChainAsk(t *testing.T, e *Engine, want state.ObjID) state.ObjID {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("root ask = %+v, want a KTarget placement ask", d)
	}
	id := d.Source
	submitTarget(t, e, want)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "trig_sub" || d.Source != id {
		t.Fatalf("after the root answer = %+v, want the chain link asked at placement (trig_sub) before priority", d)
	}
	return id
}

// TestMentorKeywordChainAskedOnPlacement drives the KEYWORD push
// (pushTrigger's pt.Mentor arm): a granted keyword's synthesized trigger
// mints an ability object whose chain links must be asked as it is put on the
// stack. Mentor's own synthesized body has no SubAbility$ chain today, so the
// test supplies a chain-bearing SA to the same arm -- the point is that the
// keyword arm runs the chain announcement at all, which it did not on main.
func TestMentorKeywordChainAskedOnPlacement(t *testing.T) {
	e := layerEngine(t)
	src := onBoard(t, e, 0, trigChainHostSrc)
	a := onBoard(t, e, 0, "Name:A\nTypes:Creature\nPT:2/2\nOracle:x\n")
	b := onBoard(t, e, 0, "Name:B\nTypes:Creature\nPT:2/2\nOracle:x\n")
	body := trigChainBody(t, e, src, "TrigX")
	e.pushTrigger(pendingTrigger{Source: src, Controller: 0, Mentor: true,
		Execute: "TrigX", SA: body})
	answerRootThenExpectChainAsk(t, e, a)
	_ = b
}

// TestPrintedTriggerChainAskedOnPlacement drives the PRINTED push
// (pushTrigger's main path): an ordinary face trigger's chain link -- one
// whose chain has no Defined$ Targeted tail, the shape the first pass left
// out -- must be asked as the ability is put on the stack. On main this
// trigger posed no placement link ask at all.
func TestPrintedTriggerChainAskedOnPlacement(t *testing.T) {
	e := layerEngine(t)
	src := onBoard(t, e, 0,
		"Name:Printed Host\nTypes:Creature\nPT:1/1\n"+
			"T:Mode$ ChangesZone | ValidCard$ Card.Self | Destination$ Battlefield | Execute$ TrigX | TriggerDescription$ x\n"+
			"SVar:TrigX:DB$ Pump | ValidTgts$ Creature.YouCtrl | SubAbility$ TrigY\n"+
			"SVar:TrigY:DB$ Pump | ValidTgts$ Creature.YouCtrl\nOracle:x\n")
	a := onBoard(t, e, 0, "Name:A\nTypes:Creature\nPT:2/2\nOracle:x\n")
	b := onBoard(t, e, 0, "Name:B\nTypes:Creature\nPT:2/2\nOracle:x\n")
	o := e.G.Obj(src)
	if o == nil || o.Face() == nil || len(o.Face().Triggers) != 1 {
		t.Fatalf("printed trigger not compiled: %+v", o)
	}
	tr := o.Face().Triggers[0]
	if tr.Effect == nil || tr.Effect.Params["SubAbility"] != "TrigY" {
		t.Fatalf("printed trigger precondition failed: %+v", tr)
	}
	e.pushTrigger(pendingTrigger{Source: src, Controller: 0, Idx: 0, SA: tr.Effect})
	answerRootThenExpectChainAsk(t, e, a)
	_ = b
}

// TestGrantedTriggerChainAskedOnPlacement drives the GRANTED push
// (pushTrigger's pt.Granted arm): a static's AddTrigger$ mints an ability
// object whose chain link must be asked as it is put on the stack.
func TestGrantedTriggerChainAskedOnPlacement(t *testing.T) {
	e := layerEngine(t)
	src := onBoard(t, e, 0, trigChainHostSrc)
	a := onBoard(t, e, 0, "Name:A\nTypes:Creature\nPT:2/2\nOracle:x\n")
	b := onBoard(t, e, 0, "Name:B\nTypes:Creature\nPT:2/2\nOracle:x\n")
	body := trigChainBody(t, e, src, "TrigX")
	e.pushTrigger(pendingTrigger{Source: src, Controller: 0, Granted: true,
		Execute: "TrigX", SA: body})
	id := answerRootThenExpectChainAsk(t, e, a)
	submitTarget(t, e, b)
	if o := e.G.Obj(id); o == nil {
		t.Fatal("the ability object vanished")
	}
}

// TestDelayedTriggerChainAskedOnPlacement drives the DELAYED push
// (pushTrigger's pt.Delayed arm): a Mode$ Phase or event registration mints an
// ability object whose chain link must be asked as it is put on the stack.
func TestDelayedTriggerChainAskedOnPlacement(t *testing.T) {
	e := layerEngine(t)
	src := onBoard(t, e, 0, trigChainHostSrc)
	a := onBoard(t, e, 0, "Name:A\nTypes:Creature\nPT:2/2\nOracle:x\n")
	b := onBoard(t, e, 0, "Name:B\nTypes:Creature\nPT:2/2\nOracle:x\n")
	body := trigChainBody(t, e, src, "TrigX")
	e.pushTrigger(pendingTrigger{Source: src, Controller: 0, Delayed: true,
		Execute: "TrigX", SA: body})
	answerRootThenExpectChainAsk(t, e, a)
	_ = b
}

// TestGainedTriggerChainAskedOnPlacement drives the GAINED push
// (pushTrigger's pt.Gained arm): a permanent that has gained a foreign
// card's triggered ability (Forge's GainsTriggerAbsOf$) mints an ability
// object whose chain link must be asked as it is put on the stack.
func TestGainedTriggerChainAskedOnPlacement(t *testing.T) {
	e := layerEngine(t)
	recipient := onBoard(t, e, 0, "Name:Recipient\nTypes:Creature\nPT:2/2\nOracle:x\n")
	a := onBoard(t, e, 0, "Name:A\nTypes:Creature\nPT:2/2\nOracle:x\n")
	b := onBoard(t, e, 0, "Name:B\nTypes:Creature\nPT:2/2\nOracle:x\n")
	foreign := onBoard(t, e, 0,
		"Name:Foreign Card\nTypes:Creature\nPT:1/1\n"+
			"T:Mode$ ChangesZone | ValidCard$ Card.Self | Destination$ Battlefield | Execute$ TrigX | TriggerDescription$ x\n"+
			"SVar:TrigX:DB$ Pump | ValidTgts$ Creature.YouCtrl | SubAbility$ TrigY\n"+
			"SVar:TrigY:DB$ Pump | ValidTgts$ Creature.YouCtrl\nOracle:x\n")
	fo := e.G.Obj(foreign)
	face := fo.Face()
	if face == nil || len(face.Triggers) != 1 || face.Triggers[0].Effect == nil {
		t.Fatalf("foreign trigger not compiled: %+v", face)
	}
	body := face.Triggers[0].Effect
	e.pushTrigger(pendingTrigger{Source: recipient, Controller: 0, Gained: true,
		GainedFrom: foreign, Idx: 0, Execute: "TrigX", SA: body})
	answerRootThenExpectChainAsk(t, e, a)
	_ = b
}

// TestMergedTriggerChainAskedOnPlacement drives the MERGED push (CR 702.140d;
// pushTrigger's pt.Merged arm): a mutated pile's under-card trigger mints an
// ability object from the under-card's own compiled trigger, and its chain
// link must be asked as it is put on the stack.
func TestMergedTriggerChainAskedOnPlacement(t *testing.T) {
	e := layerEngine(t)
	top := onBoard(t, e, 0, "Name:Top Card\nTypes:Creature\nPT:2/2\nOracle:x\n")
	a := onBoard(t, e, 0, "Name:A\nTypes:Creature\nPT:2/2\nOracle:x\n")
	b := onBoard(t, e, 0, "Name:B\nTypes:Creature\nPT:2/2\nOracle:x\n")
	underCard := card(t,
		"Name:Under Card\nTypes:Creature\nPT:1/1\n"+
			"T:Mode$ ChangesZone | ValidCard$ Card.Self | Destination$ Battlefield | Execute$ TrigX | TriggerDescription$ x\n"+
			"SVar:TrigX:DB$ Pump | ValidTgts$ Creature.YouCtrl | SubAbility$ TrigY\n"+
			"SVar:TrigY:DB$ Pump | ValidTgts$ Creature.YouCtrl\nOracle:x\n")
	under := e.G.Obj(top)
	if under == nil {
		t.Fatal("no top card")
	}
	under.MergedCards = []state.MergedCard{{Card: underCard, FaceIdx: 0}}
	face := under.MergedFaceAt(0)
	if face == nil || len(face.Triggers) != 1 || face.Triggers[0].Effect == nil {
		t.Fatalf("under-card trigger not compiled: %+v", face)
	}
	if got := face.Triggers[0].Params["Execute"]; got != "TrigX" {
		t.Fatalf("under-card Execute = %q, want TrigX", got)
	}
	body := face.Triggers[0].Effect
	e.pushTrigger(pendingTrigger{Source: top, Controller: 0, Merged: 1, Idx: 0,
		Execute: "TrigX", SA: body})
	answerRootThenExpectChainAsk(t, e, a)
	_ = b
}

// TestGrantedChainMandatoryLinkNoLegalTargetRemovesAbility pins CR 603.3d's
// exit for a granted trigger: the chain link mandates a target with no legal
// candidate, so the whole ability is removed from the stack rather than the
// link being asked at resolution.
func TestGrantedChainMandatoryLinkNoLegalTargetRemovesAbility(t *testing.T) {
	e := layerEngine(t)
	src := onBoard(t, e, 0, trigChainFizzleSrc)
	a := onBoard(t, e, 0, "Name:A\nTypes:Creature\nPT:2/2\nOracle:x\n")
	body := trigChainBody(t, e, src, "TrigX")
	e.pushTrigger(pendingTrigger{Source: src, Controller: 0, Granted: true,
		Execute: "TrigX", SA: body})
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("root ask = %+v, want a KTarget placement ask", d)
	}
	id := d.Source
	submitTarget(t, e, a)
	o := e.G.Obj(id)
	if o == nil {
		t.Fatal("precondition: the ability object exists")
	}
	if o.Zone != state.ZExile {
		t.Fatalf("ability zone after a mandatory link with no legal target = %s, want exile", o.Zone)
	}
	// The exit is the root ask's own: the ability is removed, not re-asked.
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatalf("a target ask was re-posed after the fizzle: %+v", d)
	}
}
