package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Mutate (CR 702.140) pinned end to end on the filing card, Everquill
// Phoenix (K:Mutate:3 R, T:Mode$ Mutates ... create a Feather token): the
// mutate cast is offered beside the plain creature cast, pays the mutate cost
// instead of the mana cost, asks the over/under placement and a non-Human
// creature you own as its target, merges the cards into one permanent (the
// top card carries its characteristics, the cards beneath it are recorded),
// and fires the "whenever this creature mutates" trigger exactly once per
// mutation with Count$TimesMutated tracking the count.

const mutateBearSrc = "Name:Mutate Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// mutatedCastOption returns the "cast" option with Mode "mutated" for the id.
func mutatedCastOption(t *testing.T, e *Engine, id state.ObjID) decision.Option {
	t.Helper()
	for _, o := range castOptions(t, e) {
		if o.Obj == id && o.Mode == "mutated" {
			return o
		}
	}
	t.Fatalf("no mutated cast option for card %d in %+v", id, castOptions(t, e))
	return decision.Option{}
}

// mutateCastOnto submits the mutated cast, answers CR 702.140b's placement
// ask (onTop selects the first "On top" option; otherwise "Under"), answers
// the non-Human-creature target ask with bearer, and drains the stack.
func mutateCastOnto(t *testing.T, e *Engine, opt decision.Option, bearer state.ObjID, onTop bool) {
	t.Helper()
	mutateCastOntoUndrained(t, e, opt, bearer, onTop)
	passUntilStackEmpty(t, e, 40)
}

// mutateCastOntoUndrained is mutateCastOnto without the stack drain: the cast
// is announced, placed and targeted, and the caller drives the decisions the
// mutation's own triggers pose (an OptionalDecider$ trigger asks a
// KTriggerOptional as it resolves, which passUntilStackEmpty cannot answer).
func mutateCastOntoUndrained(t *testing.T, e *Engine, opt decision.Option, bearer state.ObjID, onTop bool) {
	t.Helper()
	submitChoices(t, e, opt.Index)
	// CR 702.140b: the over/under placement is announced at cast time.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("mutate placement ask: %+v", d)
	}
	place := -1
	for _, o := range d.Options {
		if o.Kind != "mutate_place" {
			continue
		}
		if onTop && o.Label == "On top" {
			place = o.Index
		}
		if !onTop && o.Label == "Under" {
			place = o.Index
		}
	}
	if place < 0 {
		t.Fatalf("no %s placement option: %+v", map[bool]string{true: "on-top", false: "under"}[onTop], d.Options)
	}
	submitChoices(t, e, place)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("mutate target ask: %+v", d)
	}
	idx := indexOfObjOption(d, bearer)
	if idx < 0 {
		t.Fatalf("mutate target ask does not offer %d: %+v", bearer, d.Options)
	}
	submitChoices(t, e, idx)
}

// battlefieldNamedCount is a thin alias for the shared foretell_test helper
// (which counts battlefield permanents named name under seat 0).
func battlefieldNamedCount(t *testing.T, e *Engine, name string) int {
	return battlefieldNamed(t, e, name)
}

// TestEverquillPhoenixMutatesOntoTopAndCreatesFeather is the filing card: the
// priority decision offers both the plain cast ({2}{R}{R}) and the mutate
// cast ({3}{R}), the mutate cast asks placement and a non-Human creature you
// own, and after resolution the surviving permanent is Everquill Phoenix on
// top of the bear with TimesMutated 1 -- and its "whenever this creature
// mutates" trigger has made the Feather token.
func TestEverquillPhoenixMutatesOntoTopAndCreatesFeather(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	phoenix := mustCorpusCard(t, reg, "Everquill Phoenix")
	e, cfg := tokenReplGame(t, 301, phoenix)
	phoenixID := moveSeededCard(t, e, 0, phoenix, state.ZHand)
	bear := putToken(t, e, 0, mutateBearSrc, state.ZBattlefield)
	addMana(t, e, 0, "RRRR") // the mutate cost {3}{R}

	if o := plainCastOption(t, e, phoenixID); o.Label != "Cast Everquill Phoenix" {
		t.Fatalf("plain cast label %q", o.Label)
	}
	if o := mutatedCastOption(t, e, phoenixID); o.Label != "Cast Everquill Phoenix (mutated)" {
		t.Fatalf("mutated cast label %q", o.Label)
	}

	mutateCastOnto(t, e, mutatedCastOption(t, e, phoenixID), bear, true)

	// The bear's object survived as the pile; its top card is now the Phoenix.
	pile := e.G.Obj(bear)
	if pile == nil || pile.Zone != state.ZBattlefield {
		t.Fatalf("mutated pile zone: %+v", pile)
	}
	if pile.Face() == nil || pile.Face().Name != "Everquill Phoenix" {
		t.Fatalf("mutated pile top card = %v, want Everquill Phoenix", pile.Face())
	}
	if pile.TimesMutated != 1 {
		t.Fatalf("TimesMutated = %d, want 1", pile.TimesMutated)
	}
	if len(pile.MergedCards) != 1 || pile.MergedCards[0].Card == nil ||
		pile.MergedCards[0].Card.Faces[0].Name != "Mutate Bear" {
		t.Fatalf("merged under-cards = %+v, want the bear beneath the Phoenix", pile.MergedCards)
	}
	if battlefieldNamedCount(t, e, "Feather") == 0 {
		t.Fatal("the Mutates trigger did not create the Feather token")
	} // The phoenix's own card object must NOT still be an independent
	// permanent: it merged into the pile.
	if o := e.G.Obj(phoenixID); o != nil && o.Zone == state.ZBattlefield {
		t.Fatal("the mutating card is still a separate battlefield permanent")
	}
	replayCheck(t, e, cfg)
}

// TestEverquillPhoenixMutatesUnderKeepsTargetOnTop: CR 702.140b's other
// choice -- the mutating card goes UNDER the target, so the surviving pile's
// top card is still the bear (its 2/2 characteristics) and the Phoenix is the
// recorded under-card.
func TestEverquillPhoenixMutatesUnderKeepsTargetOnTop(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	phoenix := mustCorpusCard(t, reg, "Everquill Phoenix")
	e, cfg := tokenReplGame(t, 302, phoenix)
	phoenixID := moveSeededCard(t, e, 0, phoenix, state.ZHand)
	bear := putToken(t, e, 0, mutateBearSrc, state.ZBattlefield)
	addMana(t, e, 0, "RRRR")

	mutateCastOnto(t, e, mutatedCastOption(t, e, phoenixID), bear, false)

	pile := e.G.Obj(bear)
	if pile.Face() == nil || pile.Face().Name != "Mutate Bear" {
		t.Fatalf("under-placement top card = %v, want the bear", pile.Face())
	}
	if p, tt := e.Power(bear), e.Toughness(bear); p != 2 || tt != 2 {
		t.Fatalf("under-placement P/T = %d/%d, want the bear's 2/2", p, tt)
	}
	if pile.TimesMutated != 1 {
		t.Fatalf("TimesMutated = %d, want 1", pile.TimesMutated)
	}
	if len(pile.MergedCards) != 1 || pile.MergedCards[0].Card == nil ||
		pile.MergedCards[0].Card.Faces[0].Name != "Everquill Phoenix" {
		t.Fatalf("merged under-cards = %+v, want the Phoenix beneath the bear", pile.MergedCards)
	}
	// CR 702.140d: the permanent has all abilities of the cards beneath it, so
	// the under-card Phoenix's "whenever this creature mutates" trigger fires
	// even though the bear is the top card.
	if battlefieldNamedCount(t, e, "Feather") == 0 {
		t.Fatal("the under-card Mutates trigger did not create the Feather token")
	}
	replayCheck(t, e, cfg)
}

// TestMutatesCountAccumulatesAcrossTwoMutations: Everquill Phoenix mutates
// twice onto the same bear. The second cast re-offers the mutate mode on the
// already-mutated pile and TimesMutated becomes 2.
func TestMutatesCountAccumulatesAcrossTwoMutations(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	phoenix1 := mustCorpusCard(t, reg, "Everquill Phoenix")
	phoenix2 := mustCorpusCard(t, reg, "Everquill Phoenix")
	e, cfg := tokenReplGame(t, 303, phoenix1, phoenix2)
	first := moveSeededCard(t, e, 0, phoenix1, state.ZHand)
	bear := putToken(t, e, 0, mutateBearSrc, state.ZBattlefield)
	addMana(t, e, 0, "RRRR")
	mutateCastOnto(t, e, mutatedCastOption(t, e, first), bear, true)
	if got := e.G.Obj(bear).TimesMutated; got != 1 {
		t.Fatalf("after first mutate TimesMutated = %d, want 1", got)
	}

	second := moveSeededCard(t, e, 0, phoenix2, state.ZHand)
	addMana(t, e, 0, "RRRR")
	mutateCastOnto(t, e, mutatedCastOption(t, e, second), bear, true)

	pile := e.G.Obj(bear)
	if pile.TimesMutated != 2 {
		t.Fatalf("after second mutate TimesMutated = %d, want 2", pile.TimesMutated)
	}
	if len(pile.MergedCards) != 2 {
		t.Fatalf("merged under-cards = %d, want 2", len(pile.MergedCards))
	}
	if battlefieldNamedCount(t, e, "Feather") == 0 {
		t.Fatal("the second mutation did not fire the Mutates trigger")
	}
	replayCheck(t, e, cfg)
}

// TestMutateMergedCardsLeaveWithThePile: CR 702.140e -- when the mutated pile
// dies, EVERY card in it (the top card and each under-card) moves to the
// graveyard, and the pile marker is cleared.
func TestMutateMergedCardsLeaveWithThePile(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	phoenix := mustCorpusCard(t, reg, "Everquill Phoenix")
	bears := mustCorpusCard(t, reg, "Grizzly Bears")
	e, cfg := tokenReplGame(t, 304, phoenix, bears)
	phoenixID := moveSeededCard(t, e, 0, phoenix, state.ZHand)
	bear := moveSeededCard(t, e, 0, bears, state.ZBattlefield)
	addMana(t, e, 0, "RRRR")
	mutateCastOnto(t, e, mutatedCastOption(t, e, phoenixID), bear, true)

	// On-top placement: the survivor object's top card is now the Phoenix, and
	// the demoted bear card is the single merged under-card with its own
	// parked object. Capture that under-card object before the pile leaves.
	under := e.G.Obj(bear).MergedCards[0].Obj
	e.emit(events.Sacrifice(bear))
	e.checkStateBased()
	for _, id := range []state.ObjID{bear, under} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("card %d zone = %v, want graveyard (CR 702.140e)", id, func() string {
				if o == nil {
					return "nil"
				}
				return o.Zone.String()
			}())
		}
	}
	// The phoenix's card went to the graveyard as the pile's top card, so it
	// is present exactly once and never as an independent permanent.
	if battlefieldNamedCount(t, e, "Everquill Phoenix") != 0 {
		t.Fatal("the mutated Phoenix is still on the battlefield after the pile died")
	}
	if got := e.G.Obj(bear).TimesMutated; got != 0 {
		t.Fatalf("a departed pile still carries TimesMutated %d, want 0", got)
	}
	replayCheck(t, e, cfg)
}

// TestMutateCostAndCoverage: the keyword is parsed to the printed cost (the
// mutate cost is paid instead of the mana cost) and the keyword/trigger pair
// no longer reports as unsupported.
func TestMutateCostAndCoverage(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	phoenix := mustCorpusCard(t, reg, "Everquill Phoenix")
	cost, ok := mutateCost(phoenix.Faces[0])
	if !ok {
		t.Fatal("Everquill Phoenix's K:Mutate:3 R did not parse")
	}
	if cost.Generic != 3 || cost.Colored[state.MR] != 1 {
		t.Fatalf("mutate cost = %+v, want 3 generic + 1 red", cost)
	}
	unsupported := reg.Unsupported(phoenix, effects.Supported())
	for _, u := range unsupported {
		if u == "kw:Mutate" || u == "trig:Mutates" {
			t.Fatalf("Everquill Phoenix still reports %q unsupported: %v", u, unsupported)
		}
	}
}

// TestMutateOfferRequiresNonHumanYouOwn: CR 702.140a's target restriction --
// a Human creature and an opponent's creature are both illegal, so with only
// those on the battlefield the mutate cast is withheld while the plain cast
// stays offered.
func TestMutateOfferRequiresNonHumanYouOwn(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	phoenix := mustCorpusCard(t, reg, "Everquill Phoenix")
	e, _ := tokenReplGame(t, 305, phoenix)
	phoenixID := moveSeededCard(t, e, 0, phoenix, state.ZHand)
	putToken(t, e, 0, "Name:Test Human\nManaCost:1 W\nTypes:Creature Human Soldier\nPT:1/1\nOracle:x\n", state.ZBattlefield)
	putToken(t, e, 1, mutateBearSrc, state.ZBattlefield) // an OPPONENT's non-Human creature
	addMana(t, e, 0, "RRRR")

	for _, o := range castOptions(t, e) {
		if o.Obj == phoenixID && o.Mode == "mutated" {
			t.Fatalf("mutate offered with no legal (non-Human, you-own) target: %+v", o)
		}
	}
	if o := plainCastOption(t, e, phoenixID); o.Obj == 0 {
		t.Fatal("the plain cast must remain offered")
	}
}

// --- CR 702.140d: the under-card's trigger runs ITS OWN body ---

// mutateDrain answers the trigger-drain decisions a pile with several Mutates
// triggers poses: a CR 603.3b trigger_order ask is decided BEFORE placement,
// while the stack is still empty, so passUntilStackEmpty's stack-depth gate
// exits before it is ever answered. The ordering answer is the full offered
// permutation in ascending order -- the recorded mirror of the engine-side
// first-order stand-in, the same answer drainTriggerAsks gives. Priority is
// passed; any other single-choice ask takes the first option.
func mutateDrain(t *testing.T, e *Engine, limit int) {
	t.Helper()
	startTurn := e.G.Turn
	for i := 0; i < limit && !e.G.Over && e.G.Turn == startTurn; i++ {
		d := e.Pending()
		if d == nil {
			if len(e.G.Stack) == 0 {
				return
			}
			t.Fatalf("no decision while draining the stack (depth %d)", len(e.G.Stack))
		}
		switch d.Kind {
		case decision.KTriggerOrder:
			idx := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				idx = append(idx, o.Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: idx}); err != nil {
				t.Fatalf("submit trigger order: %v", err)
			}
		case decision.KPriority:
			if len(e.G.Stack) == 0 {
				// The mutation and every trigger it fired have fully resolved:
				// priority is back with an empty stack. Stop here -- running on
				// would walk into the end step and expire the very "until end
				// of turn" pumps these assertions read.
				return
			}
			passed := false
			for _, o := range d.Options {
				if o.Kind == "pass" {
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
						t.Fatalf("submit pass: %v", err)
					}
					passed = true
					break
				}
			}
			if !passed {
				t.Fatalf("priority decision with no pass option: %+v", d.Options)
			}
		default:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
				t.Fatalf("submit %v: %v", d.Kind, err)
			}
		}
	}
	if e.G.Over {
		return
	}
	if d := e.Pending(); d != nil && !(d.Kind == decision.KPriority && len(e.G.Stack) == 0) {
		t.Fatalf("drain never cleared (pending %+v, stack depth %d)", d, len(e.G.Stack))
	}
}

// TestMergedTriggerResolvesTheUnderCardsOwnBody is the finding-r2 regression:
// the pile's top card and an under-card can both define an SVar of the SAME
// name -- Forge's canonical token body name TrigToken -- and the under-card's
// "whenever this creature mutates" trigger must run the UNDER-CARD's body,
// never the top face's. Cubwarden (SVar:TrigToken = two 1/1 white Cat tokens
// with lifelink) mutated under Everquill Phoenix (SVar:TrigToken = one Feather
// artifact token) is the real collision pair: both placements must produce
// exactly the bodies' own tokens and never a cross-body steal.
//
//   - Phoenix mutated ON TOP of Cubwarden: the top Phoenix's own trigger makes
//     1 Feather, the under Cubwarden's makes 2 Cats (pre-fix: 0 Cats, 2
//     Feathers -- the under-card's Execute$ resolved against the TOP face's
//     TrigToken).
//   - Cubwarden mutated ON TOP of the Phoenix: the top Cubwarden's own trigger
//     makes 2 Cats, the under Phoenix's makes 1 Feather (pre-fix: 4 Cats, 0
//     Feathers -- the under Phoenix ran the top Cubwarden's body twice).
func TestMergedTriggerResolvesTheUnderCardsOwnBody(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	phoenix := mustCorpusCard(t, reg, "Everquill Phoenix")
	cubwarden := mustCorpusCard(t, reg, "Cubwarden")

	// Phoenix on top, Cubwarden under.
	e, cfg := tokenReplGame(t, 306, phoenix, cubwarden)
	cub := moveSeededCard(t, e, 0, cubwarden, state.ZBattlefield)
	phoenixID := moveSeededCard(t, e, 0, phoenix, state.ZHand)
	addMana(t, e, 0, "RRRR") // the mutate cost {3}{R}
	mutateCastOnto(t, e, mutatedCastOption(t, e, phoenixID), cub, true)
	mutateDrain(t, e, 40)
	if got := battlefieldNamedCount(t, e, "Feather"); got != 1 {
		t.Fatalf("top Phoenix's own trigger made %d Feather, want 1", got)
	}
	if got := battlefieldNamedCount(t, e, "Cat Token"); got != 2 {
		t.Fatalf("under Cubwarden's trigger made %d Cat Token, want 2 (its own TrigToken body, not the top face's)", got)
	}
	replayCheck(t, e, cfg)

	// Cubwarden on top, Phoenix under.
	e2, cfg2 := tokenReplGame(t, 307, cubwarden, phoenix)
	phx := moveSeededCard(t, e2, 0, phoenix, state.ZBattlefield)
	cubID := moveSeededCard(t, e2, 0, cubwarden, state.ZHand)
	addMana(t, e2, 0, "WWWW") // the mutate cost {2}{W}{W}
	mutateCastOnto(t, e2, mutatedCastOption(t, e2, cubID), phx, true)
	mutateDrain(t, e2, 40)
	if got := battlefieldNamedCount(t, e2, "Cat Token"); got != 2 {
		t.Fatalf("top Cubwarden's own trigger made %d Cat Token, want 2", got)
	}
	if got := battlefieldNamedCount(t, e2, "Feather"); got != 1 {
		t.Fatalf("under Phoenix's trigger made %d Feather, want 1 (its own TrigToken body, not the top face's)", got)
	}
	replayCheck(t, e2, cfg2)
}

// TestCountTimesMutatedDrivesTheRealReader pins Count$TimesMutated end to end
// on its real carrier Huntmaster Liger: "Whenever this creature mutates,
// other creatures you control get +X/+X until end of turn, where X is the
// number of times this creature has mutated." After the first mutation the
// other bear is +1/+1 (X=1, not 0); after the second it has taken the second
// mutation's +2/+2 from BOTH Mutates triggers the pile now carries (the top
// Liger's and the newly merged one's, each reading X=2).
func TestCountTimesMutatedDrivesTheRealReader(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	liger1 := mustCorpusCard(t, reg, "Huntmaster Liger")
	liger2 := mustCorpusCard(t, reg, "Huntmaster Liger")
	bears := mustCorpusCard(t, reg, "Grizzly Bears")
	e, cfg := tokenReplGame(t, 308, liger1, liger2, bears)
	first := moveSeededCard(t, e, 0, liger1, state.ZHand)
	target := moveSeededCard(t, e, 0, bears, state.ZBattlefield)
	other := putToken(t, e, 0, mutateBearSrc, state.ZBattlefield)
	addMana(t, e, 0, "WWW") // the mutate cost {2}{W}
	mutateCastOnto(t, e, mutatedCastOption(t, e, first), target, true)
	mutateDrain(t, e, 40)

	if got := e.G.Obj(target).TimesMutated; got != 1 {
		t.Fatalf("after first mutate TimesMutated = %d, want 1", got)
	}
	if p, tt := e.Power(other), e.Toughness(other); p != 3 || tt != 3 {
		t.Fatalf("other creature after first mutation = %d/%d, want 3/3 (X=1)", p, tt)
	}

	second := moveSeededCard(t, e, 0, liger2, state.ZHand)
	addMana(t, e, 0, "WWW")
	mutateCastOnto(t, e, mutatedCastOption(t, e, second), target, false)
	mutateDrain(t, e, 40)

	if got := e.G.Obj(target).TimesMutated; got != 2 {
		t.Fatalf("after second mutate TimesMutated = %d, want 2", got)
	}
	// Both of the pile's Liger triggers fire on the second mutation and each
	// reads X=2, so the other bear takes +2/+2 twice on top of the first +1/+1.
	if p, tt := e.Power(other), e.Toughness(other); p != 7 || tt != 7 {
		t.Fatalf("other creature after second mutation = %d/%d, want 7/7 (X=2 from both triggers)", p, tt)
	}
	replayCheck(t, e, cfg)
}

// TestEssenceSymbioteWatchesATeamMutation is the round-3 regression for the
// ONLY non-Card.Self Mutates carrier in the corpus: Essence Symbiote's
// "Whenever a creature you control mutates, put a +1/+1 counter on that
// creature and you gain 2 life." (ValidCard$ Creature.YouCtrl). The scanning
// source for such a trigger is the Symbiote, while the events.Mutate event's
// Obj is the mutated PILE, so a matcher requiring ev.Obj == source silently
// dropped the trigger. This test mutates Everquill Phoenix onto a Bear with
// the Symbiote on the battlefield and asserts BOTH halves of the Symbiote's
// body ran on the pile: the +1/+1 counter (via Defined$ TriggeredCardLKICopy,
// which triggerRemembered binds to the event's Obj) and the 2 life.
func TestEssenceSymbioteWatchesATeamMutation(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	phoenix := mustCorpusCard(t, reg, "Everquill Phoenix")
	symbiote := mustCorpusCard(t, reg, "Essence Symbiote")
	e, cfg := tokenReplGame(t, 310, phoenix, symbiote)
	phoenixID := moveSeededCard(t, e, 0, phoenix, state.ZHand)
	moveSeededCard(t, e, 0, symbiote, state.ZBattlefield)
	bear := putToken(t, e, 0, mutateBearSrc, state.ZBattlefield)
	addMana(t, e, 0, "RRRR")

	lifeBefore := e.G.Players[0].Life
	mutateCastOnto(t, e, mutatedCastOption(t, e, phoenixID), bear, true)
	mutateDrain(t, e, 40)

	pile := e.G.Obj(bear)
	if pile == nil || pile.Zone != state.ZBattlefield {
		t.Fatalf("mutated pile = %+v, want it on the battlefield", pile)
	}
	if got := pile.Counter("P1P1"); got != 1 {
		t.Fatalf("pile P1P1 counters = %d, want 1 (the Symbiote's counter, on the mutated creature)", got)
	}
	if got := e.G.Players[0].Life - lifeBefore; got != 2 {
		t.Fatalf("life gain = %d, want 2 (the Symbiote's team-watcher trigger did not fire)", got)
	}
	replayCheck(t, e, cfg)
}

// TestMutateCostIsPaidInsteadOfTheManaCost pins CR 702.140a's cost
// substitution on a card where the two costs are distinguishable: Huntmaster
// Liger's mutate cost is {2}{W} but its printed mana cost is {3}{W}, so a
// WWW pool pays the mutate cast and can NOT pay the plain cast. The mutated
// cast must be offered while the plain cast is withheld, the cast must
// complete, and the mana actually spent must be the mutate cost's 3 (not the
// printed cost's 4 -- the pre-fix engine charged the plain cost here and
// aborted the cast as "cost no longer payable").
func TestMutateCostIsPaidInsteadOfTheManaCost(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	liger := mustCorpusCard(t, reg, "Huntmaster Liger")
	e, cfg := tokenReplGame(t, 309, liger)
	ligerID := moveSeededCard(t, e, 0, liger, state.ZHand)
	bear := putToken(t, e, 0, mutateBearSrc, state.ZBattlefield)
	addMana(t, e, 0, "WWW") // pays the mutate cost {2}{W}; the plain {3}{W} is not payable

	for _, o := range castOptions(t, e) {
		if o.Obj == ligerID && o.Mode == "" {
			t.Fatalf("plain cast offered from a pool that cannot pay the printed mana cost {3}{W}: %+v", o)
		}
	}
	n0 := len(e.L.Events)
	mutateCastOnto(t, e, mutatedCastOption(t, e, ligerID), bear, true)
	mutateDrain(t, e, 40)

	pile := e.G.Obj(bear)
	if pile == nil || pile.Zone != state.ZBattlefield || pile.Face() == nil ||
		pile.Face().Name != "Huntmaster Liger" {
		t.Fatalf("mutated pile = %+v, want the Liger on top of the bear", pile)
	}
	spent := int32(0)
	for _, ev := range e.L.Events[n0:] {
		if ev.Kind == events.ManaAdd && ev.Amount < 0 {
			spent += -ev.Amount
		}
	}
	if spent != 3 {
		t.Fatalf("mutate cast spent %d mana, want the mutate cost {2}{W}'s 3 (a plain-cost charge would be 4)", spent)
	}
	replayCheck(t, e, cfg)
}

// --- CR 702.140d: an under-card's ability is resolved AS the under-card's ---

// drainUntilOptional passes priority until an optional-trigger ask is posed,
// and returns it. The pile's own mutation must have resolved first, so this
// is the mutateDrain loop with the KTriggerOptional stop: an empty stack with
// no ask means the optional trigger never asked at all, which is exactly the
// defect the callers pin.
func drainUntilOptional(t *testing.T, e *Engine, limit int) *decision.Decision {
	t.Helper()
	for i := 0; i < limit && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision and no optional ask (stack depth %d)", len(e.G.Stack))
		}
		switch d.Kind {
		case decision.KTriggerOptional:
			return d
		case decision.KPriority:
			if len(e.G.Stack) == 0 {
				t.Fatal("the stack emptied without the optional trigger ever asking")
			}
			passed := false
			for _, o := range d.Options {
				if o.Kind == "pass" {
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
						t.Fatalf("submit pass: %v", err)
					}
					passed = true
					break
				}
			}
			if !passed {
				t.Fatalf("priority decision with no pass option: %+v", d.Options)
			}
		default:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
				t.Fatalf("submit %v: %v", d.Kind, err)
			}
		}
	}
	t.Fatal("drain never reached the optional ask")
	return nil
}

// TestHuntmasterLigerUnderAForeignTopPumpsWithItsOwnSVar is the verdict-sol1
// MAJOR-1 regression: a mutated pile's UNDER-CARD triggered ability used to
// resolve its SVar-indirect parameters against the pile's TOP face.
//
// Huntmaster Liger's trigger body is "DB$ PumpAll | NumAtt$ +X | NumDef$ +X"
// with "SVar:X:Count$TimesMutated" -- both names live on the Liger's own
// face. Mutated UNDER a Grizzly Bears (a vanilla corpus card with no SVars at
// all), the pile's Face() is the Bears, so resolveTop handed the resolution
// the BEARS' SVar table, X degraded to 0 per Num's convention and the pump
// was +0/+0: the other creature stayed 2/2 where CR 702.140d requires 3/3.
// The engine-side cause was pointer identity, not the lookup: the ability was
// minted from a fresh cards.ResolveSVar parse, so no consumer could recover
// the merged face that owned it. It is now minted from the under-card face's
// COMPILED trigger, exactly as an ordinary TriggerPush mints from the top
// face's, and the owning face is what the SVar table is read from.
//
// The mirror case (Liger on TOP) already worked and is asserted alongside, so
// a regression that simply broke the top-face table cannot pass this test.
func TestHuntmasterLigerUnderAForeignTopPumpsWithItsOwnSVar(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name  string
		onTop bool
		seed  uint64
	}{
		{"under a foreign top card", false, 311},
		{"on top itself", true, 312},
	} {
		t.Run(tc.name, func(t *testing.T) {
			liger := mustCorpusCard(t, reg, "Huntmaster Liger")
			bears := mustCorpusCard(t, reg, "Grizzly Bears")
			e, cfg := tokenReplGame(t, tc.seed, liger, bears)
			ligerID := moveSeededCard(t, e, 0, liger, state.ZHand)
			target := moveSeededCard(t, e, 0, bears, state.ZBattlefield)
			other := putToken(t, e, 0, mutateBearSrc, state.ZBattlefield)
			addMana(t, e, 0, "WWW") // the mutate cost {2}{W}

			mutateCastOnto(t, e, mutatedCastOption(t, e, ligerID), target, tc.onTop)
			mutateDrain(t, e, 40)

			pile := e.G.Obj(target)
			if pile == nil || pile.TimesMutated != 1 {
				t.Fatalf("pile = %+v, want TimesMutated 1", pile)
			}
			// The under-card's own X (Count$TimesMutated) is 1, so the OTHER
			// creature you control is +1/+1: 3/3, whichever card is on top.
			if p, tt := e.Power(other), e.Toughness(other); p != 3 || tt != 3 {
				t.Fatalf("other creature = %d/%d, want 3/3 (the Liger's own SVar X = TimesMutated 1, "+
					"not the top face's absent X)", p, tt)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestPouncingShoresharkUnderAForeignTopAsksItsOptionalDecider is the
// verdict-sol1 MAJOR-2 regression: an under-card trigger's OptionalDecider$
// gate was skipped entirely, so "you may return target creature an opponent
// controls" silently became "you must".
//
// resolveTop recovers a resolving ability's owning cards.Trigger by comparing
// the ability against the compiled t.Effect POINTER; the merged half of that
// scan was dead because the merged ability had been minted from a fresh
// cards.ResolveSVar parse. With the mint taking the compiled pointer, the
// gate fires: the decider is asked, a "No" leaves the opponent's creature
// alone, and the label names the UNDER-CARD, not the pile's top card.
func TestPouncingShoresharkUnderAForeignTopAsksItsOptionalDecider(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	shark := mustCorpusCard(t, reg, "Pouncing Shoreshark")
	e, cfg := tokenReplGame(t, 313, shark)
	sharkID := moveSeededCard(t, e, 0, shark, state.ZHand)
	bear := putToken(t, e, 0, mutateBearSrc, state.ZBattlefield)
	victim := putToken(t, e, 1, mutateBearSrc, state.ZBattlefield)
	addMana(t, e, 0, "UUUU") // the mutate cost {3}{U}

	mutateCastOntoUndrained(t, e, mutatedCastOption(t, e, sharkID), bear, false)
	d := drainUntilOptional(t, e, 40)
	if d.Player != 0 {
		t.Fatalf("optional decider = player %d, want the controller (OptionalDecider$ You)", d.Player)
	}
	if !strings.Contains(d.Prompt, "Pouncing Shoreshark") {
		t.Fatalf("optional ask prompt %q does not name the under-card", d.Prompt)
	}
	// Answer "No": CR 603.5's may-clause declined, so the opponent's creature
	// must still be on the battlefield.
	no := -1
	for _, o := range d.Options {
		if o.Kind == "no" {
			no = o.Index
		}
	}
	if no < 0 {
		t.Fatalf("optional ask has no decline option: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{no}}); err != nil {
		t.Fatalf("submit no: %v", err)
	}
	mutateDrain(t, e, 40)

	if o := e.G.Obj(victim); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the declined 'you may return' bounced the opponent's creature anyway: %+v", o)
	}
	replayCheck(t, e, cfg)
}
