package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The AB$ Animate Triggers$ leaf (Forge's AnimateEffect Triggers$ rider):
// the animated body gains the named trigger for the animation's own
// lifetime. Every test here drives a real corpus carrier: Raging Ravine
// (the self-animation shape -- the animated object's own face carries the
// trigger body) and Dragon-Cursed Halls (the cross-object shape -- the
// target creature gains a trigger whose Execute$ body lives on the
// ANIMATING face's SVar table).

// triggerGrantsOn returns the continuous effects granting a trigger on id
// (the white-box read the grant/expiry assertions use).
func triggerGrantsOn(e *Engine, id state.ObjID) []ContinuousEffect {
	var out []ContinuousEffect
	for _, ce := range e.continuous {
		if ce.AddTrigger != nil && ce.Source == id {
			out = append(out, ce)
		}
	}
	return out
}

// animateTriggersEngine is a seat-zero-start two-seat fixture whose seat 0
// deck leads with extras0 and fills with Mountains, driven to seat 0's
// first main phase. Callers then pass to seat 0's next turn so the fixtures
// on the battlefield are untapped and free of summoning sickness.
func animateTriggersEngine(t *testing.T, reg *cards.Registry, extras0 ...*cards.Card) (*Engine, Config) {
	t.Helper()
	m, ok := reg.Lookup("Mountain")
	if !ok {
		t.Fatal("corpus fixture: Mountain missing")
	}
	deck0 := append([]*cards.Card{}, extras0...)
	for len(deck0) < 40 {
		deck0 = append(deck0, m)
	}
	deck1 := make([]*cards.Card, 0, 40)
	for len(deck1) < 40 {
		deck1 = append(deck1, m)
	}
	cfg := seatZeroStart(Config{Seed: 42, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{deck0, deck1}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// TestRagingRavineAnimateAttackTriggerPutsACounter is the leaf's end-to-end
// pin: the animated ravine attacks and the Triggers$-granted trigger puts a
// +1/+1 counter on it -- "Whenever this creature attacks, put a +1/+1
// counter on it", live exactly while the animation is.
func TestRagingRavineAnimateAttackTriggerPutsACounter(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := animateTriggersEngine(t, reg, lookup(t, reg, "Raging Ravine"))
	ravine := moveByName(t, e, 0, "Raging Ravine", state.ZBattlefield)

	// Precondition: before the animation the land carries no trigger grant
	// and cannot attack.
	if grants := triggerGrantsOn(e, ravine); len(grants) != 0 {
		t.Fatalf("unanimated ravine carries %d trigger grants", len(grants))
	}
	// Turn 3, seat 0: the ravine entered turn 1, so it is untapped and free
	// of summoning sickness here.
	driveToStep(t, e, 3, 0, state.StepMain1)
	addMana(t, e, 0, "2CRG")
	submitChoices(t, e, animateAbilityOption(t, e, ravine).Index)
	settleActivation(t, e)

	grants := triggerGrantsOn(e, ravine)
	if len(grants) != 1 {
		t.Fatalf("animated ravine carries %d trigger grants, want 1", len(grants))
	}
	if !grants[0].UntilEOT {
		t.Fatal("the trigger grant is not UntilEOT -- it must expire with the animation")
	}
	if grants[0].TriggerGrantor != ravine {
		t.Fatalf("self-animation grantor = %d, want the ravine itself (%d)", grants[0].TriggerGrantor, ravine)
	}
	if !e.IsCreature(ravine) {
		t.Fatal("animated ravine is not a creature -- the attack below would be vacuous")
	}

	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, ravine)
	// The granted trigger resolves off the stack: the counter lands.
	passAll(t, e, 200)
	if n := e.G.Obj(ravine).Counter("P1P1"); n != 1 {
		t.Fatalf("ravine P1P1 counters after the attacked animation = %d, want 1", n)
	}
	replayCheck(t, e, cfg)
}

// passUntilTurn answers every decision (pass on priority, first option on
// anything else) until the turn count reaches turn -- the bounded form of
// passAll, so driving past a fixed turn number cannot overshoot into the
// next one (the unbounded passAll overran to turn 15 in TestDbgRavine4).
func passUntilTurn(t *testing.T, e *Engine, turn int32) {
	t.Helper()
	for i := 0; i < 4000 && !e.G.Over && e.G.Turn < turn; i++ {
		if answerIfDiscard(t, e) {
			continue
		}
		d := e.Pending()
		if d == nil {
			continue
		}
		if d.Kind == decision.KPriority {
			passOnce(t, e)
			continue
		}
		if len(d.Options) == 0 {
			t.Fatalf("empty non-priority decision %+v at turn %d", d, e.G.Turn)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
}

// TestRagingRavineTriggerGrantExpiresWithTheAnimation pins the lifetime: the
// UntilEOT grant is dropped at end-of-turn cleanup, the land stops being a
// creature, and the counter the trigger put on it REMAINS.
func TestRagingRavineTriggerGrantExpiresWithTheAnimation(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := animateTriggersEngine(t, reg, lookup(t, reg, "Raging Ravine"))
	ravine := moveByName(t, e, 0, "Raging Ravine", state.ZBattlefield)
	driveToStep(t, e, 3, 0, state.StepMain1)
	addMana(t, e, 0, "2CRG")
	submitChoices(t, e, animateAbilityOption(t, e, ravine).Index)
	settleActivation(t, e)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, ravine)
	passUntilTurn(t, e, 4)
	if n := e.G.Obj(ravine).Counter("P1P1"); n != 1 {
		t.Fatalf("precondition: counter after the attacked animation = %d, want 1", n)
	}

	// Seat 0's next turn (turn 5): cleanup dropped the UntilEOT grant.
	driveToStep(t, e, 5, 0, state.StepMain1)
	if grants := triggerGrantsOn(e, ravine); len(grants) != 0 {
		t.Fatalf("after cleanup the ravine still carries %d trigger grants -- the trigger outlived its animation", len(grants))
	}
	if e.IsCreature(ravine) {
		t.Fatal("after cleanup the ravine is still a creature -- the animation outlived its turn")
	}
	if n := e.G.Obj(ravine).Counter("P1P1"); n != 1 {
		t.Fatalf("the real counter must survive the grant's expiry, got %d", n)
	}
}

// TestCrossObjectAnimateTriggerResolvesTheGrantorsBody pins the
// TriggerGrantor route: Dragon-Cursed Halls animates a TARGET creature, whose
// trigger's Execute$ body lives on the halls' own SVar table, not the
// creature's. Without the grantor override the conservative queue gate
// (resolve the body from the grantor's table or never queue) would silently
// swallow the trigger -- so this test fails without the fix.
func TestCrossObjectAnimateTriggerResolvesTheGrantorsBody(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := animateTriggersEngine(t, reg,
		lookup(t, reg, "Dragon-Cursed Halls"), lookup(t, reg, "Grizzly Bears"))
	halls := moveByName(t, e, 0, "Dragon-Cursed Halls", state.ZBattlefield)
	bears := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	driveToStep(t, e, 3, 0, state.StepMain1)

	addMana(t, e, 0, "1")
	opt := animateAbilityOption(t, e, halls)
	submitChoices(t, e, opt.Index)
	// The target ask: animate the bears.
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target decision after the halls' Animate (pending %+v)", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == bears {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no option naming the bears in the target ask: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	settleActivation(t, e)

	grants := triggerGrantsOn(e, bears)
	if len(grants) != 1 {
		t.Fatalf("animated bears carry %d trigger grants, want 1", len(grants))
	}
	if grants[0].Source != bears || grants[0].TriggerGrantor != halls {
		t.Fatalf("grant Source/TriggerGrantor = %d/%d, want %d/%d (the animated object, the animating halls)",
			grants[0].Source, grants[0].TriggerGrantor, bears, halls)
	}

	life := e.G.Players[1].Life
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, bears)
	passAll(t, e, 200)
	// Precondition held: the bears dealt their combat damage to seat 1.
	if got := e.G.Players[1].Life; got != life-2 {
		t.Fatalf("seat 1 life after the bear attack = %d, want %d", got, life-2)
	}
	// The granted trigger fired off the bears and its body (the halls' SVar)
	// created a Treasure token.
	if n := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.GrantTriggerPush && ev.Obj == bears
	}); n != 1 {
		t.Fatalf("GrantTriggerPush events naming the bears = %d, want 1", n)
	}
	if n := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.TokenCreate }); n != 1 {
		t.Fatalf("TokenCreate events after the bear attack = %d, want 1 (the trigger's Treasure)", n)
	}
	replayCheck(t, e, cfg)
}
