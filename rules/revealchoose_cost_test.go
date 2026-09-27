package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// revealchoose_cost_test.go pins the either-or `RevealOrChoose<N/Spec>` cost
// (Forge's CostChoice between revealing a hand card and choosing a permanent
// you control) end to end on the two measured corpus carriers, Monstrous
// Emergence and Dragon's Fire. Before this work the token parsed into the
// ordinary Reveal slice, so only the hand arm was offered: Monstrous
// Emergence was uncastable with a creature on the battlefield and no creature
// card in hand, and Dragon's Fire could not pay its optional cost by choosing
// a Dragon. The choose arm adds the elected permanent to the same paid list
// the `Revealed$<Property>` ref reads (Forge's CostReveal owns both arms), but
// it is announced as a CHOICE, never a reveal of a hand card.
//
// No Forge script text is committed here; every card comes from the compiled
// .cards corpus through searchCorpusCard.

// revealChooseEngine builds a two-seat game from compiled corpus cards: seat0
// is padded with Forests and seat1 with Mountains. It is paidCostEngine's
// sibling, kept separate so this ticket's fixtures read clearly.
func revealChooseEngine(t *testing.T, seat0, seat1 []string) (*Engine, Config) {
	t.Helper()
	return paidCostEngine(t, seat0, seat1)
}

// revealChooseNotes returns the public Notes whose Text names substr.
func revealChooseNotes(e *Engine, substr string) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && !ev.Secret && strings.Contains(ev.Text, substr) {
			out = append(out, ev)
		}
	}
	return out
}

// revealChooseHasType reports whether the card face prints want as one of its
// types (Face.Types is the raw printed type line).
func revealChooseHasType(f *cards.Face, want string) bool {
	if f == nil {
		return false
	}
	for _, ty := range f.Types {
		if ty == want {
			return true
		}
	}
	return false
}

// TestMonstrousEmergenceChooseCreatureSizesDamage pins the CHOOSE arm alone:
// with a creature on the battlefield and NO creature card in hand, Monstrous
// Emergence is legal, elects that creature, deals damage equal to ITS power,
// leaves the creature on the battlefield, announces a choice (not a reveal),
// and never touches a hand card.
func TestMonstrousEmergenceChooseCreatureSizesDamage(t *testing.T) {
	e, cfg := revealChooseEngine(t, []string{"Monstrous Emergence", "Hill Giant"}, []string{"Ancient Brontodon"})
	chosen := paidCostMoveTo(t, e, 0, "Hill Giant", state.ZBattlefield)
	spell := paidCostMoveTo(t, e, 0, "Monstrous Emergence", state.ZHand)
	receiver := paidCostMoveTo(t, e, 1, "Ancient Brontodon", state.ZBattlefield)

	// Precondition: the chosen permanent's power is nonzero and distinct from
	// the spell's own (a sorcery has none) so a zero is caught.
	if got := e.G.Obj(chosen).Face().Power(); got != 3 {
		t.Fatalf("precondition: Hill Giant power = %d, want 3", got)
	}
	// Precondition: no creature card is in seat 0's hand, so the ONLY payable
	// arm is the choose arm (this is the bug's exact shape).
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(id); o != nil && revealChooseHasType(o.Face(), "Creature") {
			t.Fatalf("precondition: creature card %s in hand; the choose arm must be the only payable arm", o.Face().Name)
		}
	}

	paidCostCast(t, e, spell, "G1")
	// The choose arm is the only payable arm: it is settled without an ask.
	// The spell targets; answer the target ask.
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		pick := -1
		for _, o := range d.Options {
			if o.Obj == receiver {
				pick = o.Index
			}
		}
		if pick < 0 {
			t.Fatalf("target ask did not offer the receiver: %+v", d.Options)
		}
		submitChoices(t, e, pick)
	}
	passUntilStackEmpty(t, e, 30)

	if got := e.G.Obj(chosen).Zone; got != state.ZBattlefield {
		t.Fatalf("chosen creature zone = %+v, want battlefield (a choice does not move it)", got)
	}
	if got := e.G.Obj(receiver).Damage; got != 3 {
		t.Fatalf("Ancient Brontodon damage = %d, want 3 (the chosen creature's power)", got)
	}
	// A chosen permanent is announced as a choice; it is NEVER a reveal of a
	// hand card.
	if notes := revealChooseNotes(e, "revealed "); len(notes) != 0 {
		t.Fatalf("choose arm emitted a reveal note: %+v", notes)
	}
	if notes := revealChooseNotes(e, "chose "); len(notes) == 0 {
		t.Fatal("choose arm emitted no choice note (precondition: emitChoiceCosts ran)")
	}
	replayCheck(t, e, cfg)
}

// TestMonstrousEmergenceOffersRevealAndChoose pins the election when BOTH
// arms can pay: the cast poses one KChoose offering a hand card (the REVEAL
// arm, option kind "revealorchoose") and a controlled creature (the CHOOSE
// arm, kind "choosecost"), and electing the choose arm sizes the damage from
// the chosen creature while the hand card stays in hand.
func TestMonstrousEmergenceOffersRevealAndChoose(t *testing.T) {
	e, cfg := revealChooseEngine(t, []string{"Monstrous Emergence", "Grizzly Bears", "Hill Giant"}, []string{"Ancient Brontodon"})
	handCard := paidCostMoveTo(t, e, 0, "Grizzly Bears", state.ZHand)
	chosen := paidCostMoveTo(t, e, 0, "Hill Giant", state.ZBattlefield)
	spell := paidCostMoveTo(t, e, 0, "Monstrous Emergence", state.ZHand)
	receiver := paidCostMoveTo(t, e, 1, "Ancient Brontodon", state.ZBattlefield)

	// Precondition: the two arms' powers DIFFER (2 vs 3), so the assertion
	// distinguishes which arm paid.
	if e.G.Obj(handCard).Face().Power() == e.G.Obj(chosen).Face().Power() {
		t.Fatalf("precondition: arm powers both %d, want distinct values", e.G.Obj(handCard).Face().Power())
	}

	paidCostCast(t, e, spell, "G1")
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no either-or KChoose pending: %+v", d)
	}
	var handIdx, chooseIdx = -1, -1
	for _, o := range d.Options {
		switch {
		case o.Kind == "revealorchoose" && o.Obj == handCard:
			handIdx = o.Index
		case o.Kind == "choosecost" && o.Obj == chosen:
			chooseIdx = o.Index
		}
	}
	if handIdx < 0 {
		t.Fatalf("either-or ask did not offer the hand arm: %+v", d.Options)
	}
	if chooseIdx < 0 {
		t.Fatalf("either-or ask did not offer the choose arm: %+v", d.Options)
	}
	submitChoices(t, e, chooseIdx)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		pick := -1
		for _, o := range d.Options {
			if o.Obj == receiver {
				pick = o.Index
			}
		}
		if pick < 0 {
			t.Fatalf("target ask did not offer the receiver: %+v", d.Options)
		}
		submitChoices(t, e, pick)
	}
	passUntilStackEmpty(t, e, 30)

	if got := e.G.Obj(handCard).Zone; got != state.ZHand {
		t.Fatalf("hand card zone = %+v, want hand (the choose arm does not move or reveal it)", got)
	}
	if got := e.G.Obj(chosen).Zone; got != state.ZBattlefield {
		t.Fatalf("chosen creature zone = %+v, want battlefield", got)
	}
	if got := e.G.Obj(receiver).Damage; got != 3 {
		t.Fatalf("Ancient Brontodon damage = %d, want 3 (the CHOSEN creature's power, not the hand card's 2)", got)
	}
	if notes := revealChooseNotes(e, "revealed "); len(notes) != 0 {
		t.Fatalf("choose arm emitted a reveal note: %+v", notes)
	}
	replayCheck(t, e, cfg)
}

// TestMonstrousEmergenceChooseArmLeavesHandCardRevealDistinct is the mirror:
// electing the REVEAL arm still reveals the hand card (the plain reveal path
// must be preserved), sizes from its power, and leaves the controlled
// creature untouched.
func TestMonstrousEmergenceChooseArmLeavesHandCardRevealDistinct(t *testing.T) {
	e, cfg := revealChooseEngine(t, []string{"Monstrous Emergence", "Grizzly Bears", "Hill Giant"}, []string{"Ancient Brontodon"})
	handCard := paidCostMoveTo(t, e, 0, "Grizzly Bears", state.ZHand)
	chosen := paidCostMoveTo(t, e, 0, "Hill Giant", state.ZBattlefield)
	spell := paidCostMoveTo(t, e, 0, "Monstrous Emergence", state.ZHand)
	receiver := paidCostMoveTo(t, e, 1, "Ancient Brontodon", state.ZBattlefield)

	paidCostCast(t, e, spell, "G1")
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no either-or KChoose pending: %+v", d)
	}
	revealIdx := -1
	for _, o := range d.Options {
		if o.Kind == "revealorchoose" && o.Obj == handCard {
			revealIdx = o.Index
		}
	}
	if revealIdx < 0 {
		t.Fatalf("either-or ask did not offer the hand arm: %+v", d.Options)
	}
	submitChoices(t, e, revealIdx)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		pick := -1
		for _, o := range d.Options {
			if o.Obj == receiver {
				pick = o.Index
			}
		}
		if pick < 0 {
			t.Fatalf("target ask did not offer the receiver: %+v", d.Options)
		}
		submitChoices(t, e, pick)
	}
	passUntilStackEmpty(t, e, 30)

	if got := e.G.Obj(handCard).Zone; got != state.ZHand {
		t.Fatalf("revealed card zone = %+v, want hand (a reveal does not move)", got)
	}
	if got := e.G.Obj(receiver).Damage; got != 2 {
		t.Fatalf("Ancient Brontodon damage = %d, want 2 (the revealed card's power)", got)
	}
	if notes := revealChooseNotes(e, "revealed "); len(notes) == 0 {
		t.Fatal("reveal arm emitted no reveal note (precondition: emitChoiceCosts ran)")
	}
	_ = chosen
	replayCheck(t, e, cfg)
}

// TestDragonsFireChooseDragonSizesDamage pins Dragon's Fire's optional cost
// paid by CHOOSING a Dragon on the battlefield, with no Dragon in hand: the
// optional cast is offered, and Y (Count$OptionalGenericCostPaid.X.3 with
// SVar:X:Revealed$CardPower) sizes from the chosen Dragon's power instead of
// the plain 3.
func TestDragonsFireChooseDragonSizesDamage(t *testing.T) {
	e, cfg := revealChooseEngine(t, []string{"Dragon's Fire", "Shivan Dragon"}, []string{"Ancient Brontodon"})
	spell := paidCostMoveTo(t, e, 0, "Dragon's Fire", state.ZHand)
	dragon := paidCostMoveTo(t, e, 0, "Shivan Dragon", state.ZBattlefield)
	receiver := paidCostMoveTo(t, e, 1, "Ancient Brontodon", state.ZBattlefield)

	if got := e.G.Obj(dragon).Face().Power(); got != 5 {
		t.Fatalf("precondition: Shivan Dragon power = %d, want 5", got)
	}
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(id); o != nil && revealChooseHasType(o.Face(), "Dragon") {
			t.Fatalf("precondition: Dragon card %s in hand; the choose arm must be the only payable arm", o.Face().Name)
		}
	}

	addMana(t, e, 0, "R1")
	paidIdx := castModeOption(t, e, spell, "optionalcost")
	submitChoices(t, e, paidIdx)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		pick := -1
		for _, o := range d.Options {
			if o.Obj == receiver {
				pick = o.Index
			}
		}
		if pick < 0 {
			t.Fatalf("target ask did not offer the receiver: %+v", d.Options)
		}
		submitChoices(t, e, pick)
	}
	passUntilStackEmpty(t, e, 40)

	if !optionalCostCastInfo(e, spell) {
		t.Fatal("no pay-time CastInfo carrying optionalcostpaid")
	}
	if got := e.G.Obj(receiver).Damage; got != 5 {
		t.Fatalf("Ancient Brontodon damage = %d, want 5 (the chosen Dragon's power, not the plain 3)", got)
	}
	if notes := revealChooseNotes(e, "revealed "); len(notes) != 0 {
		t.Fatalf("choose arm emitted a reveal note: %+v", notes)
	}
	replayCheck(t, e, cfg)
}

// TestDragonsFireRevealDragonSizesDamage pins the optional REVEAL arm: with a
// Dragon card in hand (and the plain cast declined for the optional one), the
// revealed Dragon sizes the damage.
func TestDragonsFireRevealDragonSizesDamage(t *testing.T) {
	e, cfg := revealChooseEngine(t, []string{"Dragon's Fire", "Shivan Dragon"}, []string{"Ancient Brontodon"})
	spell := paidCostMoveTo(t, e, 0, "Dragon's Fire", state.ZHand)
	dragon := paidCostMoveTo(t, e, 0, "Shivan Dragon", state.ZHand)
	receiver := paidCostMoveTo(t, e, 1, "Ancient Brontodon", state.ZBattlefield)

	if got := e.G.Obj(dragon).Face().Power(); got != 5 {
		t.Fatalf("precondition: Shivan Dragon power = %d, want 5", got)
	}
	addMana(t, e, 0, "R1")
	paidIdx := castModeOption(t, e, spell, "optionalcost")
	submitChoices(t, e, paidIdx)
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		// Exactly one Dragon in hand: the reveal arm is the only payable arm
		// and is settled without an ask. Guard against an unexpected ask.
		pick := -1
		for _, o := range d.Options {
			if o.Obj == dragon {
				pick = o.Index
			}
		}
		if pick >= 0 {
			submitChoices(t, e, pick)
		}
	}
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		pick := -1
		for _, o := range d.Options {
			if o.Obj == receiver {
				pick = o.Index
			}
		}
		if pick < 0 {
			t.Fatalf("target ask did not offer the receiver: %+v", d.Options)
		}
		submitChoices(t, e, pick)
	}
	passUntilStackEmpty(t, e, 40)

	if got := e.G.Obj(dragon).Zone; got != state.ZHand {
		t.Fatalf("revealed Dragon zone = %+v, want hand (a reveal does not move)", got)
	}
	if got := e.G.Obj(receiver).Damage; got != 5 {
		t.Fatalf("Ancient Brontodon damage = %d, want 5 (the revealed Dragon's power)", got)
	}
	replayCheck(t, e, cfg)
}

// TestDragonsFirePlainDealsThree pins the decline path: Dragon's Fire without
// the optional cost deals exactly 3 (Count$OptionalGenericCostPaid.X.3's
// unpaid branch), and stamps no optionalcostpaid CastInfo.
func TestDragonsFirePlainDealsThree(t *testing.T) {
	e, cfg := revealChooseEngine(t, []string{"Dragon's Fire"}, []string{"Ancient Brontodon"})
	spell := paidCostMoveTo(t, e, 0, "Dragon's Fire", state.ZHand)
	receiver := paidCostMoveTo(t, e, 1, "Ancient Brontodon", state.ZBattlefield)

	addMana(t, e, 0, "R1")
	paidCostCast(t, e, spell, "R1")
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		pick := -1
		for _, o := range d.Options {
			if o.Obj == receiver {
				pick = o.Index
			}
		}
		if pick < 0 {
			t.Fatalf("target ask did not offer the receiver: %+v", d.Options)
		}
		submitChoices(t, e, pick)
	}
	passUntilStackEmpty(t, e, 40)

	if optionalCostCastInfo(e, spell) {
		t.Fatal("plain cast stamped optionalcostpaid")
	}
	if got := e.G.Obj(receiver).Damage; got != 3 {
		t.Fatalf("Ancient Brontodon damage = %d, want 3 (the plain cast)", got)
	}
	replayCheck(t, e, cfg)
}

// TestRevealOrChooseChooseArmIsNotInheritedByAStackCopy pins the copy
// boundary for the new scratch state: the chosen permanent is recorded on the
// cast's own stack object in e.castRevealed, so a StackCopy -- which paid no
// cost -- reads an EMPTY paid list and deals zero, exactly like the reveal
// arm's existing copy test. The original is moved off the stack first so only
// the copy resolves.
func TestRevealOrChooseChooseArmIsNotInheritedByAStackCopy(t *testing.T) {
	e, _ := revealChooseEngine(t, []string{"Monstrous Emergence", "Hill Giant"}, []string{"Ancient Brontodon"})
	chosen := paidCostMoveTo(t, e, 0, "Hill Giant", state.ZBattlefield)
	spell := paidCostMoveTo(t, e, 0, "Monstrous Emergence", state.ZHand)
	receiver := paidCostMoveTo(t, e, 1, "Ancient Brontodon", state.ZBattlefield)

	paidCostCast(t, e, spell, "G1")
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		for _, o := range d.Options {
			if o.Obj == receiver {
				submitChoices(t, e, o.Index)
			}
		}
	}
	// Precondition: the original carries the chosen-arm paid list.
	if len(e.castRevealed[spell]) != 1 || e.castRevealed[spell][0] != chosen {
		t.Fatalf("precondition: original paid list = %v, want [%d]", e.castRevealed[spell], chosen)
	}
	e.emit(events.Event{Kind: events.StackCopy, Obj: spell, Player: 0})
	copyID := e.G.Stack[len(e.G.Stack)-1]
	if copyID == spell {
		t.Fatal("precondition: StackCopy must mint a new object")
	}
	if len(e.castRevealed[copyID]) != 0 {
		t.Fatalf("copy inherited the chosen-arm paid list %v", e.castRevealed[copyID])
	}
	// Counter the original so only the copy's resolution is observed.
	e.emit(events.Event{Kind: events.MoveZone, Obj: spell, From: state.ZStack, To: state.ZGraveyard, Text: "countered"})
	passUntilStackEmpty(t, e, 30)

	if got := e.G.Obj(receiver).Damage; got != 0 {
		t.Fatalf("copy dealt %d damage, want 0 (a copy paid no choose cost)", got)
	}
}

// TestRevealOrChooseAskSurvivesAClone pins the clone boundary for the new
// per-cast scratch: a clone taken while the either-or ask is pending carries
// pc.revealOrChoosePart and pc.revealHandArm, so answering the election on the
// clone resolves the same cast and sizes the damage from the chosen arm.
func TestRevealOrChooseAskSurvivesAClone(t *testing.T) {
	e, _ := revealChooseEngine(t, []string{"Monstrous Emergence", "Grizzly Bears", "Hill Giant"}, []string{"Ancient Brontodon"})
	handCard := paidCostMoveTo(t, e, 0, "Grizzly Bears", state.ZHand)
	chosen := paidCostMoveTo(t, e, 0, "Hill Giant", state.ZBattlefield)
	spell := paidCostMoveTo(t, e, 0, "Monstrous Emergence", state.ZHand)
	receiver := paidCostMoveTo(t, e, 1, "Ancient Brontodon", state.ZBattlefield)

	paidCostCast(t, e, spell, "G1")
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no either-or KChoose pending before clone: %+v", d)
	}
	clone := e.Clone()
	d := clone.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("clone lost the pending either-or ask: %+v", d)
	}
	chooseIdx := -1
	for _, o := range d.Options {
		if o.Kind == "choosecost" && o.Obj == chosen {
			chooseIdx = o.Index
		}
	}
	if chooseIdx < 0 {
		t.Fatalf("clone ask did not offer the choose arm: %+v", d.Options)
	}
	submitChoices(t, clone, chooseIdx)
	if d := clone.Pending(); d != nil && d.Kind == decision.KTarget {
		for _, o := range d.Options {
			if o.Obj == receiver {
				submitChoices(t, clone, o.Index)
			}
		}
	}
	passUntilStackEmpty(t, clone, 30)

	if got := clone.G.Obj(handCard).Zone; got != state.ZHand {
		t.Fatalf("clone hand card zone = %+v, want hand", got)
	}
	if got := clone.G.Obj(receiver).Damage; got != 3 {
		t.Fatalf("clone damage = %d, want 3 (the chosen creature's power)", got)
	}
}
