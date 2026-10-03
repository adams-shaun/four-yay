package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestPsychicPaperRenameAndType is the brief's mandated end-to-end regression.
//
// It equips the real corpus Psychic Paper and drives BOTH halves of the card:
//
//  1. the R:Event$ Attached | ReplaceWith$ ChooseName election (Forge's
//     NameCard body plus its ChooseType SubAbility), which must pose a real
//     two-part ask -- a creature-card name, then a creature type -- as the
//     Equipment becomes attached; and
//  2. the S:Mode$ Continuous | SetName$ ChosenName | AddType$ ChosenType |
//     RemoveCreatureTypes$ True static, which must then make the equipped
//     creature's layer-3 name the answered name and its derived creature type
//     the answered type (dropping the printed ones).
//
// The name filter read is asserted through effects.MatchesSpecCtx over a
// rules-built SpecContext -- the filter tier's read must agree with the layer
// walk (a rename the engine renders but a filter cannot see is the exact
// defect this pins). The context is where the layer-3 names come from; see
// rules/setname.go.
func TestPsychicPaperRenameAndType(t *testing.T) {
	t.Parallel()
	paper := corpusAlternativeCard(t, "Psychic Paper")
	bear := card(t, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	elf := card(t, "Name:Elvish Mystic\nManaCost:G\nTypes:Creature Elf Druid\nPT:1/1\nOracle:x\n")
	e, cfg, _ := corpusDeckEngine(t, nil, []*cards.Card{paper, bear, elf})

	var paperID, bearID state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone != state.ZBattlefield {
			continue
		}
		switch o.Card {
		case paper:
			paperID = o.ID
		case bear:
			bearID = o.ID
		}
	}
	// PRECONDITION: the Equipment and its intended bearer are really on the
	// battlefield. A vacuous setup must fail loudly, not drive an empty ask.
	if paperID == 0 || bearID == 0 {
		t.Fatalf("setup: paper=%d bear=%d (both must be on the battlefield)", paperID, bearID)
	}
	if got := e.Name(bearID); got != "Grizzly Bears" {
		t.Fatalf("precondition: unequipped bear name = %q, want printed Grizzly Bears", got)
	}

	addMana(t, e, 0, "GG")
	opt := abilityOption(t, e, paperID, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("equip target decision: %+v", d)
	}
	idx := indexOfObjOption(d, bearID)
	if idx < 0 {
		t.Fatalf("equip target ask does not offer the bear: %+v", d.Options)
	}
	submitChoices(t, e, idx)

	// Part one: the name ask, posed as the equip resolves (drive priority
	// passes until the mid-resolution KChoose appears). Answer "Elvish
	// Mystic" for the Grizzly Bears -- the choice must GOVERN, so the result
	// can never be read off the printed face.
	d = passUntilAskKind(t, e, decision.KChoose, 30)
	if d.Prompt != "Choose a creature card name" {
		t.Fatalf("expected the attached name ask, got %+v", d)
	}
	nameIdx := optionByLabel(d.Options, "Elvish Mystic")
	if nameIdx < 0 {
		t.Fatalf("name ask does not offer Elvish Mystic: %+v", d.Options)
	}
	submitChoices(t, e, nameIdx)

	// Part two: the paired creature-type ask (the body's ChooseCT SubAbility).
	d = passUntilAskKind(t, e, decision.KChoose, 30)
	if d.Prompt != "Choose a creature type" {
		t.Fatalf("expected the attached type ask after the name, got %+v", d)
	}
	typeIdx := optionByLabel(d.Options, "Elf")
	if typeIdx < 0 {
		t.Fatalf("type ask does not offer Elf: %+v", d.Options)
	}
	submitChoices(t, e, typeIdx)
	passUntilStackEmpty(t, e, 20)

	// The answers are recorded on the Equipment (the static's source).
	po := e.G.Obj(paperID)
	if po.AttachedTo != bearID {
		t.Fatalf("paper attached to %d, want bear %d", po.AttachedTo, bearID)
	}
	if po.ChosenName != "Elvish Mystic" || po.ChosenType != "Elf" {
		t.Fatalf("recorded choice name=%q type=%q, want Elvish Mystic / Elf", po.ChosenName, po.ChosenType)
	}

	// The layer-3 name and the derived type are the answered ones, and the
	// printed Bear subtype is gone (RemoveCreatureTypes$ True).
	if got := e.Name(bearID); got != "Elvish Mystic" {
		t.Fatalf("equipped bear effective name = %q, want the chosen Elvish Mystic", got)
	}
	types := e.Derived(bearID).Types
	if !containsStr(types, "Elf") {
		t.Fatalf("equipped bear derived types = %v, want the chosen Elf", types)
	}
	if containsStr(types, "Bear") {
		t.Fatalf("equipped bear still has printed Bear type %v (RemoveCreatureTypes$ True must drop it)", types)
	}

	// The FILTER tier must see the same effective name and types -- the
	// centralization this fix introduces.
	if !effects.MatchesSpecCtx(e.G, "Card.namedElvish_Mystic", bearID, e.specCtx(0, 0)) {
		t.Fatal("filter: the renamed bear must match Card.namedElvish_Mystic")
	}
	if effects.MatchesSpecCtx(e.G, "Card.namedGrizzly_Bears", bearID, e.specCtx(0, 0)) {
		t.Fatal("filter: the renamed bear must NOT match its printed name Grizzly Bears")
	}
	replayCheck(t, e, cfg)
}

// TestSetNameLiteralStaticIsVisibleToNameFilters pins the literal-arm of the
// SetName$ static on a real corpus carrier: Witness Protection's
// `S:Mode$ Continuous | Affected$ Creature.EnchantedBy | ... | SetName$
// Legitimate Businessperson | ...`. The printed name must be replaced for
// BOTH the layer walk and the filter tier (named/sameName), which is what the
// centralized effective-name read fixes -- the old filter path read the
// printed face and would still match the old name.
func TestSetNameLiteralStaticIsVisibleToNameFilters(t *testing.T) {
	t.Parallel()
	witness := corpusAlternativeCard(t, "Witness Protection")
	bear := card(t, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, cfg, witnessID := corpusDeckEngine(t, []*cards.Card{witness}, []*cards.Card{bear})

	var bearID state.ObjID
	for i := range e.G.Objs {
		if e.G.Objs[i].Card == bear && e.G.Objs[i].Zone == state.ZBattlefield {
			bearID = e.G.Objs[i].ID
		}
	}
	if witnessID == 0 || bearID == 0 {
		t.Fatalf("setup: witness=%d bear=%d", witnessID, bearID)
	}
	toMain1(t, e)
	addMana(t, e, 0, "U")
	submitChoices(t, e, castCardOption(t, e, witnessID).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("aura target decision: %+v", d)
	}
	ti := indexOfObjOption(d, bearID)
	if ti < 0 {
		t.Fatalf("aura target ask does not offer the bear: %+v", d.Options)
	}
	submitChoices(t, e, ti)
	passUntilStackEmpty(t, e, 20)

	if e.G.Obj(witnessID).AttachedTo != bearID {
		t.Fatalf("Witness Protection attached to %d, want %d", e.G.Obj(witnessID).AttachedTo, bearID)
	}
	if got := e.Name(bearID); got != "Legitimate Businessperson" {
		t.Fatalf("enchanted bear effective name = %q, want Legitimate Businessperson", got)
	}
	if !effects.MatchesSpecCtx(e.G, "Card.namedLegitimate_Businessperson", bearID, e.specCtx(0, 0)) {
		t.Fatal("filter: the renamed bear must match Card.namedLegitimate_Businessperson")
	}
	if effects.MatchesSpecCtx(e.G, "Card.namedGrizzly_Bears", bearID, e.specCtx(0, 0)) {
		t.Fatal("filter: the renamed bear must NOT match its printed name")
	}
	replayCheck(t, e, cfg)
}

// TestCompetingSetNameStaticsAgreeWithTheLayerWalk pins the property that
// closed the reviewer's scan-order finding: with two SetName$ static effects
// on one bearer, the FILTER tier and the LAYER WALK must name the same effect
// the winner. The old filter read scanned the battlefield in zone order and
// took the last attachment, which need not be the highest-timestamp layer
// effect; the fix makes the filter consult the layer walk itself. The test
// does not hardcode which blade wins (object timestamps decide that) -- it
// asserts that the filter's answer IS the layer walk's.
func TestCompetingSetNameStaticsAgreeWithTheLayerWalk(t *testing.T) {
	t.Parallel()
	first := card(t, "Name:First Blade\nManaCost:1\nTypes:Artifact Equipment\nK:Equip:1\n"+
		"S:Mode$ Continuous | Affected$ Creature.EquippedBy | SetName$ First Name | Description$ x\nOracle:x\n")
	second := card(t, "Name:Second Blade\nManaCost:1\nTypes:Artifact Equipment\nK:Equip:1\n"+
		"S:Mode$ Continuous | Affected$ Creature.EquippedBy | SetName$ Second Name | Description$ x\nOracle:x\n")
	bear := card(t, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, _, _ := corpusDeckEngine(t, nil, []*cards.Card{first, second, bear})

	var firstID, secondID, bearID state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone != state.ZBattlefield {
			continue
		}
		switch o.Card {
		case first:
			firstID = o.ID
		case second:
			secondID = o.ID
		case bear:
			bearID = o.ID
		}
	}
	if firstID == 0 || secondID == 0 || bearID == 0 {
		t.Fatalf("setup: first=%d second=%d bear=%d", firstID, secondID, bearID)
	}
	// Attach BOTH blades, deliberately in the reverse of the order the
	// battlefield scan would reach them, so a scan-order read and the layer
	// walk can disagree.
	e.emit(events.Event{Kind: events.Attach, Obj: secondID, IDs: []state.ObjID{bearID}})
	e.emit(events.Event{Kind: events.Attach, Obj: firstID, IDs: []state.ObjID{bearID}})
	if e.G.Obj(firstID).AttachedTo != bearID || e.G.Obj(secondID).AttachedTo != bearID {
		t.Fatal("precondition: both blades must be attached to the bear")
	}

	want := e.Name(bearID)
	if want != "First Name" && want != "Second Name" {
		t.Fatalf("layer walk named the bearer %q, want one of the two SetName$ effects", want)
	}
	other := "First Name"
	if want == "First Name" {
		other = "Second Name"
	}
	if !effects.MatchesSpecCtx(e.G, "Card.named"+strings.ReplaceAll(want, " ", "_"), bearID, e.specCtx(0, 0)) {
		t.Fatalf("filter does not see the layer walk's name %q", want)
	}
	if effects.MatchesSpecCtx(e.G, "Card.named"+strings.ReplaceAll(other, " ", "_"), bearID, e.specCtx(0, 0)) {
		t.Fatalf("filter sees the losing SetName$ name %q", other)
	}
}

func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// TestPsychicPaperIgnoresAnotherAttach pins the Attached replacement's
// ValidCard$ frame: Psychic Paper's `ValidCard$ Card.Self` names the
// ATTACHING object, so another Equipment becoming attached never poses the
// paper's name ask (the matcher used to test the replacement source against
// Card.Self, which always matched, so every Attach to a creature asked).
func TestPsychicPaperIgnoresAnotherAttach(t *testing.T) {
	t.Parallel()
	paper := corpusAlternativeCard(t, "Psychic Paper")
	blade := card(t, "Name:Tape Blade\nManaCost:1\nTypes:Artifact Equipment\nK:Equip:1\nOracle:x\n")
	bear := card(t, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, cfg, _ := corpusDeckEngine(t, nil, []*cards.Card{paper, blade, bear})
	var bladeID, bearID state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone != state.ZBattlefield {
			continue
		}
		switch o.Card {
		case blade:
			bladeID = o.ID
		case bear:
			bearID = o.ID
		}
	}
	if bladeID == 0 || bearID == 0 {
		t.Fatalf("setup: blade=%d bear=%d", bladeID, bearID)
	}
	addMana(t, e, 0, "G")
	submitChoices(t, e, abilityOption(t, e, bladeID, 0).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("equip target decision: %+v", d)
	}
	submitChoices(t, e, indexOfObjOption(d, bearID))
	for i := 0; i < 30 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d.Kind != decision.KPriority {
			t.Fatalf("another Equipment attaching posed %s %q", d.Kind, d.Prompt)
		}
		submitChoices(t, e, passIndex(t, d))
	}
	if bo := e.G.Obj(bladeID); bo.AttachedTo != bearID {
		t.Fatalf("blade attached to %d, want bear %d", bo.AttachedTo, bearID)
	}
	replayCheck(t, e, cfg)
}
