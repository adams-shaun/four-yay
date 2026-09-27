package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Long River's Pull ({U}{U}, K:Gift) is the pinned X/Y carrier for the
// Count$PromisedGift bound: X = Count$PromisedGift.0.1 gates the main
// Counter's creature-spell target and Y = Count$PromisedGift.1.0 gates the
// chained DBCounter's any-spell target. The pair below drives both halves end
// to end on the real corpus script.

const lrpRelicSrc = "Name:Test Relic\nManaCost:1\nTypes:Artifact\nOracle:x\n"
const lrpBearsSrc = "Name:Test Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
const lrpZapSrc = "Name:Test Zap\nManaCost:U\nTypes:Instant\nOracle:x\n"

// TestGiftLongRiversPullPromisedCountersAnySpell: with the gift promised,
// the offer gate must accept the alternate X = 0 branch even with no creature
// spell on the stack. The elected X = 0/Y = 1 branches then let the chain
// counter the noncreature spell. The promised opponent draws.
func TestGiftLongRiversPullPromisedCountersAnySpell(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pull := mustCorpusCard(t, reg, "Long River's Pull")
	zap := card(t, lrpZapSrc)
	// Seat 1 holds the non-creature instant; seat 0 passes so it can be cast,
	// then responds with Long River's Pull.
	e, cfg := tokenReplGameSeats(t, 17, []*cards.Card{pull}, []*cards.Card{zap})
	pullID := moveSeededCard(t, e, 0, pull, state.ZHand)
	zapID := moveSeededCard(t, e, 1, zap, state.ZHand)
	// Fund seat 1 through a logged ManaAdd so replay derives the same pool.
	e.emit(events.Event{Kind: events.ManaAdd, Player: 1, Counter: "U", Amount: 1})
	addMana(t, e, 0, "UUUUG")
	mainSA := pull.Faces[0].SpellAbility()
	unpromisedMin, _ := e.resolvedTargetBounds(0, pullID, mainSA, 0)
	promised := true
	promisedMin, _ := e.resolvedTargetBoundsWithGift(0, pullID, mainSA, 0, &promised)
	if unpromisedMin != 1 || promisedMin != 0 || unpromisedMin == promisedMin {
		t.Fatalf("precondition: Long River's Pull bounds = unpromised %d, promised %d; want distinct 1 and 0", unpromisedMin, promisedMin)
	}
	if got := len(e.legalTargetCandidates(0, pullID, pullID, mainSA)); got != 0 {
		t.Fatalf("precondition: creature-spell candidate count = %d, want 0 before response", got)
	}
	passPriorityOnce(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 1 {
		t.Fatalf("expected seat 1 priority, got %+v", d)
	}
	submitChoices(t, e, castCardOption(t, e, zapID).Index)
	// Seat 0 receives priority with only the noncreature spell on the stack.
	if e.G.Obj(zapID).Zone != state.ZStack {
		t.Fatalf("precondition: zap spell zone = %v, want stack", e.G.Obj(zapID).Zone)
	}
	if len(e.G.Stack) != 1 {
		t.Fatalf("precondition: stack has %d objects, want only the noncreature spell", len(e.G.Stack))
	}
	submitChoices(t, e, passToCast(t, e, pullID))
	if e.G.Obj(zapID).Zone != state.ZStack {
		t.Fatalf("precondition: zap spell left stack before Gift response: %v", e.G.Obj(zapID).Zone)
	}
	answerGift(t, e, true)
	// X = 0 skips the main creature-spell ask; the DBCounter sub (Y = 1)
	// asks mid-resolution for any spell, so the noncreature instant is the
	// only target. Its presence proves the elected branch controls the ask.
	drainGiftTargets(t, e, zapID, 20)
	if z := e.G.Obj(zapID).Zone; z != state.ZGraveyard {
		t.Fatalf("zap spell zone = %v, want graveyard (promised DBCounter counters any spell)", z)
	}
	replayCheck(t, e, cfg)
}

// TestGiftLongRiversPullDeclinedCountersOnlyACreatureSpell: with the gift
// declined, X = 1 requires the main counter to target a creature spell and
// Y = 0 leaves the chained counter with no target. The same pull that would
// counter any spell when promised can only answer a creature spell here, and
// the opponent draws nothing.
func TestGiftLongRiversPullDeclinedCountersOnlyACreatureSpell(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pull := mustCorpusCard(t, reg, "Long River's Pull")
	bears := card(t, lrpBearsSrc)
	e, cfg := tokenReplGame(t, 18, pull, bears)
	bearsID := moveSeededCard(t, e, 0, bears, state.ZHand)
	pullID := moveSeededCard(t, e, 0, pull, state.ZHand)
	oppHand := len(e.G.Zone(state.ZHand, 1))
	addMana(t, e, 0, "GGUUUU")
	submitChoices(t, e, castCardOption(t, e, bearsID).Index)
	if e.G.Obj(bearsID).Zone != state.ZStack {
		t.Fatalf("precondition: bears spell zone = %v, want stack", e.G.Obj(bearsID).Zone)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority after casting the bears: %+v", d)
	}
	// Priority round-trips through the opponent before the caster may
	// respond to their own creature spell.
	submitChoices(t, e, passToCast(t, e, pullID))
	answerGift(t, e, false)
	// X = 1: the main counter's declared bound must demand exactly one
	// creature spell, and it must offer the bears.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("declined Long River's Pull should ask for a creature spell, pending = %+v", d)
	}
	if d.Min != 1 || d.Max != 1 {
		t.Fatalf("declined Long River's Pull target bound = %d..%d, want 1..1 (X = Count$PromisedGift.0.1 unread)", d.Min, d.Max)
	}
	i := targetOptionIndex(d, bearsID)
	if i < 0 {
		t.Fatalf("target ask does not offer the creature spell: %+v", d.Options)
	}
	submitChoices(t, e, i)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(bearsID).Zone; z != state.ZGraveyard {
		t.Fatalf("bears spell zone = %v, want graveyard (countered)", z)
	}
	if got := len(e.G.Zone(state.ZHand, 1)); got != oppHand {
		t.Fatalf("declined opponent hand = %d, want %d (no gift, no draw)", got, oppHand)
	}
	replayCheck(t, e, cfg)
}

// TestGiftCopyCarriesNoPromise pins CR 702.168 against CR 707.10: a stack
// COPY of a promised spell was never cast, so it inherits no promise. The
// source object's promise is set through the real election event and then a
// StackCopy is emitted; the minted copy must read PromisedGift false, so
// Card.PromisedGift and Count$PromisedGift cannot act on a copied spell.
func TestGiftCopyCarriesNoPromise(t *testing.T) {
	relic := card(t, lrpRelicSrc)
	e, _ := tokenReplGame(t, 21, relic)
	id := moveSeededCard(t, e, 0, relic, state.ZHand)
	addMana(t, e, 0, "U")
	submitChoices(t, e, castCardOption(t, e, id).Index)
	if z := e.G.Obj(id).Zone; z != state.ZStack {
		t.Fatalf("precondition: relic spell zone = %v, want stack", z)
	}
	// The real election fold: promises seat 1 a gift.
	e.emit(events.Event{Kind: events.GiftPromise, Obj: id, Player: 1, Amount: 1})
	if e.G.Obj(id).CastFlags&state.FlagPromisedGift == 0 || e.G.Obj(id).GiftPromisedTo != 1 {
		t.Fatalf("precondition: promise not folded onto the original: %+v", e.G.Obj(id))
	}
	before := len(e.G.Objs)
	e.emit(events.Event{Kind: events.StackCopy, Obj: id, Player: 0})
	var copyID state.ObjID
	for i := before; i < len(e.G.Objs); i++ {
		if e.G.Objs[i].IsCopy {
			copyID = e.G.Objs[i].ID
		}
	}
	if copyID == 0 {
		t.Fatal("precondition: no copy minted by StackCopy")
	}
	cp := e.G.Obj(copyID)
	if cp.CastFlags&state.FlagPromisedGift != 0 || cp.GiftPromisedTo != 0 {
		t.Fatalf("stack copy inherited the gift promise: flags=%s GiftPromisedTo=%d, want no promisedgift flag / 0",
			events.FlagsString(cp.CastFlags), cp.GiftPromisedTo)
	}
}
