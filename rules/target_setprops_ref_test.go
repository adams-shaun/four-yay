package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// This file pins Forge's reference-relative card-type target constraint
// (rules/setprops.go): TargetsWithSharedCardType$ names a reference object
// (ParentTarget -- the parent ability's chosen target -- or TriggeredCard,
// the triggering card) and a candidate is legal only when it shares a card
// type with that reference; TargetsWithSharedTypes$ narrows which card types
// count for that intersection. Both the census post-filter
// (filterTargetsWithSharedCardType) and the resolution recheck
// (sharedCardTypeAdmits, called from legalTargets) read the ONE predicate.
//
// Each test asserts its own preconditions -- the fixture really carries the
// parameters, and the compared card-type sets really intersect (or not) --
// so a vacuous setup fails loudly.

// TestTargetSetSharedCardType pins TargetsWithSharedCardType$ at the offer
// and the CR 608.2b recheck: the parent's chosen target is an Artifact, so an
// Artifact candidate shares a card type and is offered, while a Creature
// candidate shares none and is withheld; the recheck drops the non-sharing
// target from a recorded set.
func TestTargetSetSharedCardType(t *testing.T) {
	sa := setPropSA("Permanent", map[string]string{"TargetsWithSharedCardType": "ParentTarget"})
	if sharedCardTypeRef(sa) != "ParentTarget" {
		t.Fatal("precondition: fixture lost TargetsWithSharedCardType$")
	}
	e := newSeats(t, 2)
	// The parent ability (a permanent on the battlefield standing in for the
	// resolving source) chose the Artifact reference target.
	parent := putBattlefield(t, e, 0, "Name:Parent\nTypes:Enchantment\nOracle:x\n")
	refArtifact := putBattlefield(t, e, 0, "Name:Ref Artifact\nTypes:Artifact\nOracle:x\n")
	artifact := putBattlefield(t, e, 1, "Name:Other Artifact\nTypes:Artifact\nOracle:x\n")
	creature := putBattlefield(t, e, 1, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: parent, IDs: []state.ObjID{refArtifact}})
	if len(e.G.Obj(parent).Targets) != 1 || e.G.Obj(parent).Targets[0].Obj != refArtifact {
		t.Fatalf("precondition: parent targets = %+v, want the reference Artifact %d", e.G.Obj(parent).Targets, refArtifact)
	}
	// Preconditions: the compared card-type sets really differ.
	if !e.sharedCardTypeAdmits(artifact, refArtifact, nil) {
		t.Fatal("precondition: two Artifacts must share a card type")
	}
	if e.sharedCardTypeAdmits(creature, refArtifact, nil) {
		t.Fatal("precondition: Creature and Artifact must share no card type")
	}
	e.pending = nil
	e.askTarget(0, parent, sa)
	d := e.Pending()
	if d == nil {
		t.Fatal("no target decision posed")
	}
	if setPropOptionIndex(d, artifact) == -1 {
		t.Fatal("the sharing Artifact was not offered")
	}
	if setPropOptionIndex(d, creature) != -1 {
		t.Fatal("the non-sharing Creature was offered despite TargetsWithSharedCardType$")
	}
	// The resolution recheck applies the same predicate.
	kept := e.legalTargets([]state.Target{{Obj: artifact}, {Obj: creature}}, sa, targetZones(sa), 0, parent, parent)
	if len(kept) != 1 || kept[0].Obj != artifact {
		t.Fatalf("recheck = %+v, want only the sharing Artifact %d", kept, artifact)
	}
}

// TestTargetSetSharedTypes pins TargetsWithSharedTypes$: it narrows the
// intersection to the listed card types. The reference is an Artifact
// Creature; an Artifact candidate shares the whitelisted Artifact type and is
// offered, while a Creature candidate shares only the non-whitelisted
// Creature type and is withheld even though it does share a type with the
// reference.
func TestTargetSetSharedTypes(t *testing.T) {
	sa := setPropSA("Permanent", map[string]string{
		"TargetsWithSharedCardType": "ParentTarget",
		"TargetsWithSharedTypes":    "Artifact,Land",
	})
	if sharedCardTypeRef(sa) != "ParentTarget" || len(sharedTypesWhitelist(sa)) != 2 {
		t.Fatal("precondition: fixture lost TargetsWithSharedCardType$/$TargetsWithSharedTypes$")
	}
	e := newSeats(t, 2)
	parent := putBattlefield(t, e, 0, "Name:Parent\nTypes:Enchantment\nOracle:x\n")
	ref := putBattlefield(t, e, 0, "Name:Ref Golem\nTypes:Artifact Creature\nPT:2/2\nOracle:x\n")
	artifact := putBattlefield(t, e, 1, "Name:Other Artifact\nTypes:Artifact\nOracle:x\n")
	creature := putBattlefield(t, e, 1, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: parent, IDs: []state.ObjID{ref}})
	if len(e.G.Obj(parent).Targets) != 1 {
		t.Fatalf("precondition: parent targets = %+v, want one reference", e.G.Obj(parent).Targets)
	}
	whitelist := sharedTypesWhitelist(sa)
	// Preconditions: both candidates share a card type with the reference,
	// but only the Artifact's shared type is whitelisted.
	if !e.sharedCardTypeAdmits(artifact, ref, whitelist) {
		t.Fatal("precondition: Artifact must share a whitelisted type with Artifact Creature")
	}
	if e.sharedCardTypeAdmits(creature, ref, whitelist) {
		t.Fatal("precondition: Creature must share only the non-whitelisted Creature type")
	}
	// Without the whitelist the Creature DOES share a type, proving the
	// whitelist -- not the intersection -- is what withholds it.
	if !e.sharedCardTypeAdmits(creature, ref, nil) {
		t.Fatal("precondition: Creature must share Creature with Artifact Creature absent the whitelist")
	}
	e.pending = nil
	e.askTarget(0, parent, sa)
	d := e.Pending()
	if d == nil {
		t.Fatal("no target decision posed")
	}
	if setPropOptionIndex(d, artifact) == -1 {
		t.Fatal("the whitelisted Artifact was not offered")
	}
	if setPropOptionIndex(d, creature) != -1 {
		t.Fatal("the non-whitelisted Creature was offered despite TargetsWithSharedTypes$")
	}
}

// TestTargetSetSharedCardTypeRecheck isolates the CR 608.2b resolution
// recheck: with no offer involved, a recorded target set holding a
// non-sharing object is narrowed to the objects that share a card type with
// the parent's chosen target. It fails if only the census filter is wired.
func TestTargetSetSharedCardTypeRecheck(t *testing.T) {
	sa := setPropSA("Permanent", map[string]string{"TargetsWithSharedCardType": "ParentTarget"})
	if sharedCardTypeRef(sa) != "ParentTarget" {
		t.Fatal("precondition: fixture lost TargetsWithSharedCardType$")
	}
	e := newSeats(t, 2)
	parent := putBattlefield(t, e, 0, "Name:Parent\nTypes:Enchantment\nOracle:x\n")
	refArtifact := putBattlefield(t, e, 0, "Name:Ref Artifact\nTypes:Artifact\nOracle:x\n")
	artifact := putBattlefield(t, e, 1, "Name:Other Artifact\nTypes:Artifact\nOracle:x\n")
	creature := putBattlefield(t, e, 1, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: parent, IDs: []state.ObjID{refArtifact}})
	if len(e.G.Obj(parent).Targets) != 1 {
		t.Fatalf("precondition: parent targets = %+v, want the reference", e.G.Obj(parent).Targets)
	}
	if !e.sharedCardTypeAdmits(artifact, refArtifact, nil) || e.sharedCardTypeAdmits(creature, refArtifact, nil) {
		t.Fatal("precondition: Artifact must share a card type with the reference and Creature must not")
	}
	kept := e.legalTargets([]state.Target{{Obj: artifact}, {Obj: creature}}, sa, targetZones(sa), 0, parent, parent)
	if len(kept) != 1 || kept[0].Obj != artifact {
		t.Fatalf("recheck = %+v, want only the sharing Artifact %d", kept, artifact)
	}
}

// TestTargetSetSharedCardTypeTriggered pins the TriggeredCard reference the
// corpus also carries (confusion_in_the_ranks.txt: the sub-ability shares a
// card type with the card its trigger fired on). It drives the resolution
// recheck, which reads the stack object's trigger context -- the same
// TriggerCard binding the census offer reads. A candidate sharing a card type
// with the triggering card is kept; one sharing none is dropped.
func TestTargetSetSharedCardTypeTriggered(t *testing.T) {
	sa := setPropSA("Permanent", map[string]string{"TargetsWithSharedCardType": "TriggeredCard"})
	if sharedCardTypeRef(sa) != "TriggeredCard" {
		t.Fatal("precondition: fixture lost TargetsWithSharedCardType$ TriggeredCard")
	}
	e := newSeats(t, 2)
	parent := putBattlefield(t, e, 0, "Name:Parent\nTypes:Enchantment\nOracle:x\n")
	triggerCard := putBattlefield(t, e, 0, "Name:Triggering Artifact\nTypes:Artifact\nOracle:x\n")
	sharing := putBattlefield(t, e, 1, "Name:Other Artifact\nTypes:Artifact\nOracle:x\n")
	nonSharing := putBattlefield(t, e, 1, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	// The stack object carries the firing event's card as TriggeredCard.
	e.triggerContexts = map[state.ObjID]effects.TriggerContext{
		parent: {TriggerCard: triggerCard},
	}
	if e.G.Obj(parent).Face() == nil {
		t.Fatal("precondition: parent must exist")
	}
	if e.sharedCardTypeReference("TriggeredCard", parent, e.targetSpecContext(parent, parent, 0)) != triggerCard {
		t.Fatal("precondition: TriggeredCard did not resolve to the firing card")
	}
	if !e.sharedCardTypeAdmits(sharing, triggerCard, nil) {
		t.Fatal("precondition: two Artifacts must share a card type")
	}
	if e.sharedCardTypeAdmits(nonSharing, triggerCard, nil) {
		t.Fatal("precondition: Creature and Artifact must share no card type")
	}
	kept := e.legalTargets([]state.Target{{Obj: sharing}, {Obj: nonSharing}}, sa, targetZones(sa), 0, parent, parent)
	if len(kept) != 1 || kept[0].Obj != sharing {
		t.Fatalf("recheck = %+v, want only the sharing Artifact %d", kept, sharing)
	}
}

// TestTargetSetSharedCardTypeRealIR asserts the corpus fixtures the predicates
// serve really carry the parameters, so the unit fixtures above are not
// testing a shape no card has. It compiles the real scripts FRESH (never the
// shared ir.gob.gz cache, whose staleness rule a Go-side param read does not
// touch). The key rides an SVar-referenced sub-ability (DB$ ExchangeControl),
// so each SVar body is resolved and scanned, not just the root Abilities.
func TestTargetSetSharedCardTypeRealIR(t *testing.T) {
	reg := freshCorpusRegistry(t, "d/daring_thief.txt", "l/legerdemain.txt")

	carries := func(cardName string) (ref, sharedTypes string, ok bool) {
		c, found := reg.Lookup(cardName)
		if !found {
			t.Fatalf("corpus fixture %q is missing", cardName)
		}
		face := c.Faces[0]
		for _, ab := range face.Abilities {
			if r := sharedCardTypeRef(ab); r != "" {
				return r, sharedTypesRet(ab), true
			}
		}
		for name := range face.SVars {
			if sub := cards.ResolveSVar(face.SVars, name); sub != nil {
				if r := sharedCardTypeRef(sub); r != "" {
					return r, sharedTypesRet(sub), true
				}
			}
		}
		return "", "", false
	}

	ref, shared, ok := carries("Daring Thief")
	if !ok || ref != "ParentTarget" {
		t.Fatalf("precondition: Daring Thief carries TargetsWithSharedCardType$ = %q (found=%v), want ParentTarget", ref, ok)
	}
	if shared != "" {
		t.Fatalf("precondition: Daring Thief unexpectedly carries SharedTypes %q", shared)
	}

	ref, shared, ok = carries("Legerdemain")
	if !ok || ref != "ParentTarget" {
		t.Fatalf("precondition: Legerdemain carries TargetsWithSharedCardType$ = %q (found=%v), want ParentTarget", ref, ok)
	}
	if shared != "Artifact,Creature" {
		t.Fatalf("precondition: Legerdemain SharedTypes = %q, want Artifact,Creature", shared)
	}
}

// sharedTypesRet returns TargetsWithSharedTypes$ for a resolved SA, or "".
func sharedTypesRet(sa *cards.SA) string {
	if sa == nil {
		return ""
	}
	return sa.Params["TargetsWithSharedTypes"]
}
