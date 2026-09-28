package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func addStackTargetCard(t *testing.T, e *Engine, c *cards.Card, p state.PlayerID) state.ObjID {
	t.Helper()
	o := e.G.AddObject(c, p)
	o.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 0, append(e.G.Zone(state.ZStack, 0), o.ID))
	return o.ID
}

func corpusSA(t *testing.T, reg *cards.Registry, name, svar string) *cards.SA {
	t.Helper()
	c := searchCorpusCard(t, reg, name)
	if len(c.Faces) == 0 {
		t.Fatalf("corpus card %q has no faces", name)
	}
	if svar == "" {
		return c.Faces[0].SpellAbility()
	}
	sa := cards.ResolveSVar(c.Faces[0].SVars, svar)
	if sa == nil {
		t.Fatalf("corpus card %q has no SVar %q", name, svar)
	}
	return sa
}

// TestTargetTypeQualifiersReadAllNamedQualifiers uses the corpus carriers for
// each remaining TargetType$ qualifier. The compared cards deliberately differ
// in the characteristic the qualifier reads, so a widened census cannot pass.
func TestTargetTypeQualifiersReadAllNamedQualifiers(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, ids := stackTargetsFixture(t)

	// These are real corpus cards, not copies of their scripts. Their faces are
	// the stack objects whose current characteristics TargetType$ must inspect.
	coloredNoncreature := addStackTargetCard(t, e, searchCorpusCard(t, reg, "Cancel"), 1)
	colorlessCreature := addStackTargetCard(t, e, searchCorpusCard(t, reg, "Endless One"), 1)
	legendaryCreature := addStackTargetCard(t, e, searchCorpusCard(t, reg, "Atraxa, Praetors' Voice"), 1)
	for _, id := range []state.ObjID{coloredNoncreature, colorlessCreature, legendaryCreature} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZStack || o.Face() == nil {
			t.Fatalf("target precondition: object %d is not a face-bearing stack object", id)
		}
	}
	if e.IsCreature(coloredNoncreature) || !e.IsCreature(colorlessCreature) ||
		e.Colors(coloredNoncreature) == "" || e.Colors(colorlessCreature) != "" ||
		!stackHasType(e.Derived(legendaryCreature).Types, "Legendary") {
		t.Fatalf("target precondition characteristics did not differ: colored noncreature=%v/%q, colorless creature=%v/%q, legendary=%v",
			e.IsCreature(coloredNoncreature), e.Colors(coloredNoncreature),
			e.IsCreature(colorlessCreature), e.Colors(colorlessCreature),
			e.Derived(legendaryCreature).Types)
	}

	louisoix := corpusSA(t, reg, "Louisoix's Sacrifice", "")
	got := censusOf(t, e, 0, louisoix)
	if !containsID(got, coloredNoncreature) || containsID(got, colorlessCreature) || containsID(got, legendaryCreature) {
		t.Fatalf("Spell.nonCreature census = %v, want colored noncreature only among compared spells", got)
	}
	if !containsID(got, ids.activated) || !containsID(got, ids.triggered) {
		t.Fatalf("Louisoix's Activated/Triggered alternatives dropped abilities: %v", got)
	}

	consign := corpusSA(t, reg, "Consign to Memory", "")
	got = censusOf(t, e, 0, consign)
	if !containsID(got, colorlessCreature) || containsID(got, coloredNoncreature) || containsID(got, legendaryCreature) {
		t.Fatalf("Spell.Colorless census = %v, want only the colorless compared spell", got)
	}

	tales := corpusSA(t, reg, "Tales End", "")
	got = censusOf(t, e, 0, tales)
	if !containsID(got, legendaryCreature) || containsID(got, coloredNoncreature) || containsID(got, colorlessCreature) {
		t.Fatalf("Spell.Legendary census = %v, want only the legendary compared spell", got)
	}

	// Willbender's real ChangeTargets body requires exactly one chosen target.
	willbender := corpusSA(t, reg, "Willbender", "TrigChange")
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: ids.spell, IDs: []state.ObjID{ids.perm}})
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: ids.activated, IDs: []state.ObjID{ids.perm}})
	got = censusOf(t, e, 0, willbender)
	if !containsID(got, ids.spell) || !containsID(got, ids.activated) || containsID(got, ids.triggered) {
		t.Fatalf("singleTarget census = %v, want only stack objects with one target", got)
	}

	// Wylls Reversal's real body asks for a stack object with one or more
	// targets. The un-targeted trigger is the precondition proving GE1 binds.
	wylls := corpusSA(t, reg, "Wyll's Reversal", "")
	got = censusOf(t, e, 0, wylls)
	if !containsID(got, ids.spell) || !containsID(got, ids.activated) || containsID(got, ids.triggered) {
		t.Fatalf("numTargets GE1 census = %v, want only objects with targets", got)
	}

	// Nimble Obstructionist's real delayed body has YouDontCtrl on both
	// ability kinds. From seat 0, only seat 1's abilities are legal.
	nimble := corpusSA(t, reg, "Nimble Obstructionist", "TrigCounter")
	got = censusOf(t, e, 0, nimble)
	if !containsID(got, ids.activated) || !containsID(got, ids.triggered) || containsID(got, ids.mine) {
		t.Fatalf("YouDontCtrl census from seat 0 = %v, want opponent abilities only", got)
	}
}

// TestTriggeredTargetTypeKeepsKindAfterSourceLeaves proves CR 113.7a's
// last-known-information half: the triggered/activated classification comes
// from the minted stack object, not a source-face lookup at targeting time.
func TestTriggeredTargetTypeKeepsKindAfterSourceLeaves(t *testing.T) {
	t.Parallel()
	e := counterHands(t, nil, nil, nil, []*cards.Card{card(t, heraldSrc)})
	source := e.G.Zone(state.ZBattlefield, 1)[0]
	e.emit(events.Event{Kind: events.TriggerPush, Obj: source, Player: 1, Amount: 0})
	if len(e.G.Stack) != 1 {
		t.Fatalf("trigger setup: stack = %v, want one triggered ability", e.G.Stack)
	}
	trigger := e.G.Stack[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: state.ZBattlefield, To: state.ZGraveyard})
	to := e.G.Obj(trigger)
	if to == nil || to.Zone != state.ZStack {
		t.Fatalf("trigger precondition after source leaves: object = %+v, want stack object", to)
	}
	// A source that has left the game is not available to the classifier. The
	// event-minted kind is the surviving LKI needed by CR 113.7a.
	to.Source = state.ObjID(999999)
	if !to.StackKindKnown || to.StackKind != state.StackKindTriggered {
		t.Fatalf("trigger precondition after source leaves: object = %+v, want known triggered stack object", to)
	}
	triggeredOnly := card(t, spiderSenseSrc).Faces[0].SpellAbility()
	got := censusOf(t, e, 0, triggeredOnly)
	if !containsID(got, trigger) {
		t.Fatalf("Triggered-only census after source left = %v, want trigger %d", got, trigger)
	}
}
