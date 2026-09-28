package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Gift (CR 702.168, Bloomburrow). The keyword is a free cast-time election to
// promise an opponent a gift; the promise is folded by events.GiftPromise,
// the gift body (the face's GiftAbility SVar) resolves before the spell's
// other effects, and a kept promise emits events.GiveGift for trig:GiveGift.
// These tests drive the real corpus scripts (Wear Down, Valley Rally,
// Octomancer, Jolly Gerbils) end to end.

// answerGift answers the pending CR 702.168 gift election. promise selects
// the opponent-promise option (the one non-decline option in a 2-seat game);
// it fails loudly if the ask never happened, so a test can never pass with
// the keyword unregistered.
func answerGift(t *testing.T, e *Engine, promise bool) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("gift ask pending = %+v, want a KChoose", d)
	}
	want := "gift_decline"
	if promise {
		want = "gift_promise"
	}
	for _, o := range d.Options {
		if o.Kind == want {
			// Precondition: a promised option must name the opponent.
			if promise && int(o.Player) != 1 {
				t.Fatalf("gift promise option names seat %d, want the sole opponent seat 1", o.Player)
			}
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("gift ask has no %s option: %+v", want, d.Options)
}

// targetOptionIndex returns the option index for object id in a pending
// decision, or -1.
func targetOptionIndex(d *decision.Decision, id state.ObjID) int {
	for _, o := range d.Options {
		if o.Obj == id {
			return o.Index
		}
	}
	return -1
}

// deityArtifacts is a pair of cheap real artifacts used as Wear Down targets.
func wearDownGame(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	wear := mustCorpusCard(t, reg, "Wear Down")
	a1 := mustCorpusCard(t, reg, "Ornithopter")
	a2 := mustCorpusCard(t, reg, "Sol Ring")
	e, cfg := tokenReplGameSeats(t, seed, []*cards.Card{wear}, []*cards.Card{a1, a2})
	wearID := moveSeededCard(t, e, 0, wear, state.ZHand)
	id1 := moveSeededCard(t, e, 1, a1, state.ZBattlefield)
	id2 := moveSeededCard(t, e, 1, a2, state.ZBattlefield)
	return e, cfg, wearID, id1, id2
}

// TestGiftWearDownPromisedDestroysTwoAndDrawsForPromised is the promised half:
// X = Count$PromisedGift.2.1 raises the target requirement to two artifacts,
// both are destroyed, and the gift (DB$ Draw | Defined$ Promised) draws the
// promised opponent a card before the spell's other effects.
func TestGiftWearDownPromisedDestroysTwoAndDrawsForPromised(t *testing.T) {
	t.Parallel()
	e, cfg, wearID, a1, a2 := wearDownGame(t, 11)
	oppHand := len(e.G.Zone(state.ZHand, 1))
	// Precondition: both targets are really on the opponent's battlefield.
	if e.G.Obj(a1).Zone != state.ZBattlefield || e.G.Obj(a2).Zone != state.ZBattlefield {
		t.Fatal("precondition: both Wear Down targets must be on the battlefield")
	}
	addMana(t, e, 0, "1G")
	submitChoices(t, e, castCardOption(t, e, wearID).Index)
	answerGift(t, e, true)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target ask after promising = %+v", d)
	}
	// Precondition the real assertion depends on: the promised bound must
	// actually demand TWO targets (X = 2), not the declined one.
	if d.Min != 2 || d.Max != 2 {
		t.Fatalf("promised Wear Down target bound = %d..%d, want 2..2 (Count$PromisedGift.2.1 unread)", d.Min, d.Max)
	}
	i1, i2 := targetOptionIndex(d, a1), targetOptionIndex(d, a2)
	if i1 < 0 || i2 < 0 {
		t.Fatalf("target ask does not offer both artifacts: %+v", d.Options)
	}
	submitChoices(t, e, i1, i2)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(a1).Zone; z == state.ZBattlefield {
		t.Fatalf("promised Wear Down left %d on the battlefield", a1)
	}
	if z := e.G.Obj(a2).Zone; z == state.ZBattlefield {
		t.Fatalf("promised Wear Down left %d on the battlefield", a2)
	}
	if got := len(e.G.Zone(state.ZHand, 1)); got != oppHand+1 {
		t.Fatalf("promised opponent hand = %d, want %d (they draw the promised gift)", got, oppHand+1)
	}
	replayCheck(t, e, cfg)
}

// TestGiftWearDownDeclinedDestroysOneAndNoDraw is the negative half: a
// declined promise keeps X = 1 (destroy one), and no card is drawn.
func TestGiftWearDownDeclinedDestroysOneAndNoDraw(t *testing.T) {
	t.Parallel()
	e, cfg, wearID, a1, a2 := wearDownGame(t, 12)
	oppHand := len(e.G.Zone(state.ZHand, 1))
	addMana(t, e, 0, "1G")
	submitChoices(t, e, castCardOption(t, e, wearID).Index)
	answerGift(t, e, false)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target ask after declining = %+v", d)
	}
	if d.Min != 1 || d.Max != 1 {
		t.Fatalf("declined Wear Down target bound = %d..%d, want 1..1", d.Min, d.Max)
	}
	i1 := targetOptionIndex(d, a1)
	if i1 < 0 {
		t.Fatalf("target ask does not offer %d: %+v", a1, d.Options)
	}
	submitChoices(t, e, i1)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(a1).Zone; z == state.ZBattlefield {
		t.Fatalf("declined Wear Down should have destroyed %d", a1)
	}
	if z := e.G.Obj(a2).Zone; z != state.ZBattlefield {
		t.Fatalf("declined Wear Down destroyed the second artifact %d", a2)
	}
	if got := len(e.G.Zone(state.ZHand, 1)); got != oppHand {
		t.Fatalf("declined opponent hand = %d, want %d (no gift, no draw)", got, oppHand)
	}
	if n := countGiveGift(e); n != 0 {
		t.Fatalf("declined promise emitted %d GiveGift markers, want 0", n)
	}
	replayCheck(t, e, cfg)
}

func countGiveGift(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.GiveGift {
			n++
		}
	}
	return n
}

// giftBearSrc is an inline-authored fixture creature (never a corpus .txt)
// used as the "creature you control" a promised Valley Rally may target for
// first strike.
const giftBearSrc = "Name:Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// drainGiftTargets drains the stack, answering a mid-resolution KTarget
// (Valley Rally's promised DBPump first-strike ask, which the chain pre-ask
// gate does not cover for a cost that never reads AllTargeted$) by naming
// target, and passing priority otherwise.
func drainGiftTargets(t *testing.T, e *Engine, target state.ObjID, limit int) {
	t.Helper()
	for n := 0; n < limit && !e.G.Over && len(e.G.Stack) > 0; n++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining (stack depth %d)", len(e.G.Stack))
		}
		switch d.Kind {
		case decision.KTarget:
			i := targetOptionIndex(d, target)
			if i < 0 {
				t.Fatalf("mid-resolution gift target ask does not offer %d: %+v", target, d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}}); err != nil {
				t.Fatalf("submit gift target: %v", err)
			}
		case decision.KChoose:
			// The mid-resolution target ask is posed as a KChoose with
			// ResumeKind "tgts" (effects' AskTarget path), not a KTarget.
			i := targetOptionIndex(d, target)
			if i < 0 {
				t.Fatalf("mid-resolution gift target choose does not offer %d: %+v", target, d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}}); err != nil {
				t.Fatalf("submit gift target choose: %v", err)
			}
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("drain pass: %v", err)
			}
		default:
			t.Fatalf("unexpected %s while draining: %+v", d.Kind, d)
		}
	}
}

// TestGiftValleyRallyCreatesFoodForPromisedOpponent drives the instant
// TokenOwner$ Promised path: the promise makes the gift (a Food token) and
// the receiver is the promised opponent, not the caster.
func TestGiftValleyRallyCreatesFoodForPromisedOpponent(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	rally := mustCorpusCard(t, reg, "Valley Rally")
	e, cfg := tokenReplGame(t, 13, rally)
	rallyID := moveSeededCard(t, e, 0, rally, state.ZHand)
	bear := putToken(t, e, 0, giftBearSrc, state.ZBattlefield)
	addMana(t, e, 0, "RRR")
	submitChoices(t, e, castCardOption(t, e, rallyID).Index)
	answerGift(t, e, true)
	// The promised X = 1 (Count$PromisedGift.1.0) turns Valley Rally's
	// chained DBPump into a one-creature first-strike ask, posed
	// mid-resolution.
	drainGiftTargets(t, e, bear, 20)
	if n := countTokensNamedOnSeat(t, e, 1, "Food Token"); n != 1 {
		t.Fatalf("promised opponent Food tokens = %d, want 1 (TokenOwner$ Promised)", n)
	}
	if n := countTokensNamedOnSeat(t, e, 0, "Food Token"); n != 0 {
		t.Fatalf("caster Food tokens = %d, want 0 (the gift is not theirs)", n)
	}
	replayCheck(t, e, cfg)
}

// TestGiftOctomancerCreatesTokenForPromisedOpponent drives a PERMANENT's
// gift: Octomancer's DB$ Token | TokenOwner$ Promised creates an 8/8 for the
// promised opponent as the permanent's gift, and the permanent itself
// resolves onto the battlefield. The same cast's promise must survive the
// stack->battlefield move (it is what the ETB half of the mechanic reads).
func TestGiftOctomancerCreatesTokenForPromisedOpponent(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	octo := mustCorpusCard(t, reg, "Octomancer")
	e, cfg := tokenReplGame(t, 14, octo)
	octoID := moveSeededCard(t, e, 0, octo, state.ZHand)
	addMana(t, e, 0, "GGGGU")
	submitChoices(t, e, castCardOption(t, e, octoID).Index)
	answerGift(t, e, true)
	passUntilStackEmpty(t, e, 20)
	// Precondition: the permanent really resolved onto the battlefield.
	if z := e.G.Obj(octoID).Zone; z != state.ZBattlefield {
		t.Fatalf("Octomancer zone = %v, want battlefield", z)
	}
	if e.G.Obj(octoID).CastFlags&state.FlagPromisedGift == 0 {
		t.Fatalf("Octomancer did not retain its promise across the stack->battlefield move")
	}
	if n := countTokensNamedOnSeat(t, e, 1, "Octopus Token"); n != 1 {
		t.Fatalf("promised opponent Octopus tokens = %d, want 1 (TokenOwner$ Promised)", n)
	}
	replayCheck(t, e, cfg)
}

// TestGiftJollyGerbilsTriggersOnAGivenGift: Jolly Gerbils on the caster's
// battlefield draws a card whenever the caster gives a gift, and stays silent
// on a declined promise. Valley Rally is the giver (an instant with a Food
// gift).
func TestGiftJollyGerbilsTriggersOnAGivenGift(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	gerbils := mustCorpusCard(t, reg, "Jolly Gerbils")
	rally := mustCorpusCard(t, reg, "Valley Rally")
	e, cfg := tokenReplGame(t, 15, gerbils, rally)
	moveSeededCard(t, e, 0, gerbils, state.ZBattlefield)
	rallyID := moveSeededCard(t, e, 0, rally, state.ZHand)
	// Precondition: the trigger's source is a battlefield permanent.
	if z := e.G.Obj(e.G.Zone(state.ZBattlefield, 0)[0]).Zone; z != state.ZBattlefield {
		t.Fatal("precondition: Jolly Gerbils must be on the battlefield")
	}
	handBefore := len(e.G.Zone(state.ZHand, 0))
	bear := putToken(t, e, 0, giftBearSrc, state.ZBattlefield)
	addMana(t, e, 0, "RRR")
	submitChoices(t, e, castCardOption(t, e, rallyID).Index)
	answerGift(t, e, true)
	drainGiftTargets(t, e, bear, 20)
	// One card for the promised opponent's Food (not this seat) plus one for
	// Jolly Gerbils' trigger, minus the Valley Rally we cast.
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore {
		t.Fatalf("Jolly Gerbils hand = %d, want %d (cast rally -1, gerbils draw +1)", got, handBefore)
	}
	if n := countGiveGift(e); n != 1 {
		t.Fatalf("GiveGift markers = %d, want 1", n)
	}
	if n := countTokensNamedOnSeat(t, e, 1, "Food Token"); n != 1 {
		t.Fatalf("promised opponent Food = %d, want 1", n)
	}
	replayCheck(t, e, cfg)
}

// TestGiftDeclinedDoesNotFireJollyGerbils is the negative half: a declined
// promise gives no gift, so trig:GiveGift never matches and the Gerbils draw
// nothing. The test also proves the handler ran (no unimplemented Note for
// the keyword) so it cannot pass with Gift unregistered.
func TestGiftDeclinedDoesNotFireJollyGerbils(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	gerbils := mustCorpusCard(t, reg, "Jolly Gerbils")
	rally := mustCorpusCard(t, reg, "Valley Rally")
	e, cfg := tokenReplGame(t, 16, gerbils, rally)
	moveSeededCard(t, e, 0, gerbils, state.ZBattlefield)
	rallyID := moveSeededCard(t, e, 0, rally, state.ZHand)
	handBefore := len(e.G.Zone(state.ZHand, 0))
	putToken(t, e, 0, giftBearSrc, state.ZBattlefield)
	addMana(t, e, 0, "RRR")
	submitChoices(t, e, castCardOption(t, e, rallyID).Index)
	answerGift(t, e, false)
	passUntilStackEmpty(t, e, 20)
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore-1 {
		t.Fatalf("declined gift hand = %d, want %d (only the cast consumed a card)", got, handBefore-1)
	}
	if n := countGiveGift(e); n != 0 {
		t.Fatalf("declined promise emitted %d GiveGift markers, want 0", n)
	}
	if n := countTokensNamedOnSeat(t, e, 1, "Food Token"); n != 0 {
		t.Fatalf("declined promise created %d Food for the opponent, want 0", n)
	}
	assertNoUnimplementedGiftNote(t, e)
	replayCheck(t, e, cfg)
}

// assertNoUnimplementedGiftNote fails if the Gift family ever degraded to an
// "unimplemented API" Note, so a "nothing happens" test cannot pass with the
// feature unregistered.
func assertNoUnimplementedGiftNote(t *testing.T, e *Engine) {
	t.Helper()
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && (strings.Contains(ev.Text, "Gift") || strings.Contains(ev.Text, "GiveGift")) {
			t.Fatalf("gift degraded to a Note: %q", ev.Text)
		}
	}
}

// TestGiftKitnapPromisedDrawsAndSkipsStunCounters drives a PERMANENT Aura's
// promise through its own ETB trigger: Kitnap's "if the gift wasn't promised,
// put three stun counters on it" reads ConditionPresent$ Card.PromisedGift on
// the entering permanent, so the promise must survive the stack->battlefield
// move. Promised: the opponent draws and the enchanted creature is unstunned.
func TestGiftKitnapPromisedDrawsAndSkipsStunCounters(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	kitnap := mustCorpusCard(t, reg, "Kitnap")
	e, cfg := tokenReplGame(t, 31, kitnap)
	bear := putToken(t, e, 0, giftBearSrc, state.ZBattlefield)
	kitnapID := moveSeededCard(t, e, 0, kitnap, state.ZHand)
	oppHand := len(e.G.Zone(state.ZHand, 1))
	addMana(t, e, 0, "UUUU")
	submitChoices(t, e, castCardOption(t, e, kitnapID).Index)
	answerGift(t, e, true)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Kitnap aura target ask = %+v", d)
	}
	i := targetOptionIndex(d, bear)
	if i < 0 {
		t.Fatalf("Kitnap target ask does not offer the bear: %+v", d.Options)
	}
	submitChoices(t, e, i)
	// CR 702.168c: the promised gift is now a real ETB trigger queued beside
	// Kitnap's printed "when it enters, tap enchanted creature" trigger, so
	// the controller orders the two before either resolves.
	drainEntryTriggers(t, e, bear)
	// Precondition: Kitnap really entered attached to the bear.
	if z := e.G.Obj(kitnapID).Zone; z != state.ZBattlefield {
		t.Fatalf("Kitnap zone = %v, want battlefield", z)
	}
	if got := stunCountersOn(e, bear); got != 0 {
		t.Fatalf("promised Kitnap put %d stun counters on the bear, want 0 (Card.PromisedGift read true)", got)
	}
	if got := len(e.G.Zone(state.ZHand, 1)); got != oppHand+1 {
		t.Fatalf("promised opponent hand = %d, want %d (they draw the gift)", got, oppHand+1)
	}
	replayCheck(t, e, cfg)
}

// TestGiftKitnapDeclinedPutsThreeStunCounters is the negative half: a declined
// promise gives nothing and Kitnap's ETB gate "if the gift wasn't promised"
// fires, stunning the enchanted creature.
func TestGiftKitnapDeclinedPutsThreeStunCounters(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	kitnap := mustCorpusCard(t, reg, "Kitnap")
	e, cfg := tokenReplGame(t, 32, kitnap)
	bear := putToken(t, e, 0, giftBearSrc, state.ZBattlefield)
	kitnapID := moveSeededCard(t, e, 0, kitnap, state.ZHand)
	oppHand := len(e.G.Zone(state.ZHand, 1))
	addMana(t, e, 0, "UUUU")
	submitChoices(t, e, castCardOption(t, e, kitnapID).Index)
	answerGift(t, e, false)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Kitnap aura target ask = %+v", d)
	}
	i := targetOptionIndex(d, bear)
	if i < 0 {
		t.Fatalf("Kitnap target ask does not offer the bear: %+v", d.Options)
	}
	submitChoices(t, e, i)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(kitnapID).Zone; z != state.ZBattlefield {
		t.Fatalf("Kitnap zone = %v, want battlefield", z)
	}
	if got := stunCountersOn(e, bear); got != 3 {
		t.Fatalf("declined Kitnap put %d stun counters on the bear, want 3", got)
	}
	if got := len(e.G.Zone(state.ZHand, 1)); got != oppHand {
		t.Fatalf("declined opponent hand = %d, want %d (no gift, no draw)", got, oppHand)
	}
	replayCheck(t, e, cfg)
}

// drainEntryTriggers drives a promised permanent's entry interaction to
// completion: it answers every controller trigger-order ask over the CR
// 702.168c gift trigger and the card's printed ETB, passes priority, and
// answers any mid-resolution target ask the printed ETB poses. It stops once
// the gift trigger has been placed and has left the stack (countGiveGift > 0
// and an empty stack), before a priority pass could roll the turn into
// another player's draw step.
func drainEntryTriggers(t *testing.T, e *Engine, wants ...state.ObjID) {
	t.Helper()
	for n := 0; n < 30; n++ {
		if len(e.G.Stack) == 0 && countGiveGift(e) > 0 {
			return
		}
		d := e.Pending()
		if d == nil {
			return
		}
		if d.Kind == decision.KTriggerOrder {
			choices := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				choices = append(choices, o.Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
				t.Fatalf("submit entry trigger order: %v", err)
			}
			continue
		}
		if err := answerPriorityOrTarget(t, e, wants...); err != nil {
			t.Fatalf("drain entry triggers: %v", err)
		}
	}
	t.Fatalf("entry triggers did not settle (stack=%v, GiveGift=%d)", e.G.Stack, countGiveGift(e))
}

// stunCountersOn is the named-counter read the stun assertions use. The
// kind is "STUN" (the canonical spelling): main's counter-kind fold
// (961484894, trig:FullyUnlock task) resolves Forge's case-insensitive
// `CounterType$ Stun` onto STUN, which the untap replacement and
// ValidCounterType$ STUN read.
func stunCountersOn(e *Engine, id state.ObjID) int32 {
	if o := e.G.Obj(id); o != nil {
		return o.Counter("STUN")
	}
	return 0
}

// TestGiftPrimitivesRegistered pins that kw:Gift and trig:GiveGift are
// registered with real behaviour, so the coverage census (make report) counts
// every carrier as playable. Perch Protection is the one carrier whose main
// effect still needs stat:CantChangeLife (its phase-out rider's api:Phases
// was registered by main's agent-20260919T181525Z-3cba0676 merge, b81d636ef,
// after this branch cut); its gift half works.
func TestGiftPrimitivesRegistered(t *testing.T) {
	t.Parallel()
	supported := effects.Supported()
	for _, p := range []string{"kw:Gift", "trig:GiveGift"} {
		if !supported[p] {
			t.Fatalf("effects.Supported() is missing %s", p)
		}
	}
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{"Wear Down", "Long River's Pull", "Peerless Recycling",
		"Kitnap", "Valley Rally", "Octomancer", "Jolly Gerbils"} {
		c := mustCorpusCard(t, reg, name)
		if m := reg.Unsupported(c, supported); len(m) != 0 {
			t.Fatalf("%s still measures unsupported: %v", name, m)
		}
	}
	pp := mustCorpusCard(t, reg, "Perch Protection")
	missing := reg.Unsupported(pp, supported)
	// Perch Protection's chained `DB$ Effect | StaticAbilities$ STCantChange`
	// life-total static is the pre-existing Effect-static gap (AGENTS.md's
	// "Effect registers real continuous effects only for ..." row), not a
	// Gift gap; the api:Phases half of the original measurement closed when
	// main registered it. Assert the measured set exactly so a future
	// registration or regression is named.
	want := []string{"stat:CantChangeLife"}
	if len(missing) != len(want) {
		t.Fatalf("Perch Protection unsupported = %v, want %v", missing, want)
	}
	for i := range want {
		if missing[i] != want[i] {
			t.Fatalf("Perch Protection unsupported = %v, want %v", missing, want)
		}
	}
}
