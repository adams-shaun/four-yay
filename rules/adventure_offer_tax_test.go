package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// adventure_offer_tax_test.go pins the botbench flip_face livelock (4-seat
// constructed, Order of Midnight // Alter Fate under Thalia, Guardian of
// Thraben): the offer priced the Adventure face while the object still showed
// its Creature front, so a "noncreature spells cost {1} more" tax never
// applied at the offer, the cast (reading the flipped Sorcery face) found the
// taxed cost unpayable, reversed, and the same offer came back forever.
// Authored fixtures only -- never Forge text.

// taxedAdventureSrc: a {1}{B} Creature front over a {1}{B} Sorcery Adventure
// face with a mandatory target (the targetAsk abort site is where the
// livelock reversed).
const taxedAdventureSrc = "Name:Dusk Squire\nManaCost:1 B\nTypes:Creature Human Knight\nPT:2/2\n" +
	"AlternateMode:Adventure\nOracle:front\nALTERNATE\nName:Grim Errand\nManaCost:1 B\n" +
	"Types:Sorcery Adventure\nA:SP$ Destroy | ValidTgts$ Creature | SpellDescription$ Destroy target creature.\nOracle:back\n"

// taxWardenSrc: Thalia's shape -- noncreature spells cost {1} more.
const taxWardenSrc = "Name:Tax Warden\nManaCost:1 W\nTypes:Creature Human Soldier\nPT:2/1\n" +
	"S:Mode$ RaiseCost | ValidCard$ Card.nonCreature | Type$ Spell | Amount$ 1 | Description$ Noncreature spells cost {1} more to cast.\nOracle:x\n"

// TestAdventureFaceOfferPricesTheAdventureFace: with exactly {B}{B} floating
// under the tax, the {1}{B} Creature front is castable and offered, but the
// Sorcery Adventure face costs {2}{B} and must NOT be offered -- the offer and
// the cast flow price the same (flipped) face.
func TestAdventureFaceOfferPricesTheAdventureFace(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 7411, taxedAdventureSrc, taxWardenSrc)
	putCreature(t, e, 0, taxWardenSrc)
	addMana(t, e, 0, "BB")
	if adventureOption(t, e, id, "") == nil {
		t.Fatalf("the untaxed Creature front must be offered: %+v", castOptions(t, e))
	}
	if got := adventureOption(t, e, id, "adventure_alt"); got != nil {
		t.Fatalf("the taxed Adventure face ({2}{B}) was offered with only {B}{B}: %+v", got)
	}
	// With the tax covered the Adventure face is offered and casts for real.
	addMana(t, e, 0, "B")
	alt := adventureOption(t, e, id, "adventure_alt")
	if alt == nil {
		t.Fatalf("the Adventure face must be offered once {2}{B} is available: %+v", castOptions(t, e))
	}
	submitChoices(t, e, alt.Index)
	if d := e.Pending(); d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Grim Errand target ask: %+v", d)
	}
	if hasNote(e, "cast aborted") {
		t.Fatal("the offered Adventure cast aborted")
	}
	replayCheck(t, e, cfg)
}

// TestAdventureFaceNoProgressAbortIsHeldOut pins the defence in depth: when
// an alternate-face cast IS reversed with no progress, the F05-2 count must
// survive the proposal's own FlipFace, so the SECOND identical abort holds
// the card out of the window (the flip used to clear the count, restarting it
// at zero on every attempt). The cast is begun directly -- the offer no
// longer produces it.
func TestAdventureFaceNoProgressAbortIsHeldOut(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 7412, taxedAdventureSrc, taxWardenSrc)
	putCreature(t, e, 0, taxWardenSrc)
	addMana(t, e, 0, "BB")
	opt := decision.Option{Kind: "cast", Obj: id, Mode: "adventure_alt"}
	e.beginCast(0, opt)
	if e.cast != nil || !hasNote(e, "cast aborted: cost no longer payable") {
		t.Fatalf("the taxed Adventure cast must reverse: cast=%+v", e.cast)
	}
	if o := e.G.Obj(id); o.Zone != state.ZHand || o.FaceIdx != 0 {
		t.Fatalf("reversal must restore the card: zone=%s face=%d", o.Zone, o.FaceIdx)
	}
	if got := e.castAborts[id]; got != 1 {
		t.Fatalf("first no-progress abort count = %d, want 1 (the proposal's FlipFace cleared it)", got)
	}
	if e.castSuppressed(0, id) {
		t.Fatal("the FIRST no-progress abort must leave the option offered (CR 733.2)")
	}
	e.beginCast(0, opt)
	if !e.castSuppressed(0, id) {
		t.Fatalf("the SECOND identical no-progress abort must hold the card out (count %d)", e.castAborts[id])
	}
	// Real progress clears the hold, exactly as for a front-face abort.
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "B", Amount: 1})
	if e.castSuppressed(0, id) {
		t.Fatal("a state-changing event must clear the hold")
	}
	e.pending = nil
	e.priorityRound()
	replayCheck(t, e, cfg)
}
