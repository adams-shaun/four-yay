// Final whole-branch review, Critical C1: Task 29 added a totality guard
// (ensureLeftTheStack, rules/stack.go) for exactly one of resolveTop's five
// stack exits -- a permanent spell's ETB Move onto the battlefield. The
// other four (askTarget's cast-time "countered: no legal targets", an
// ability object's fizzle and its normal ceases-to-exist move, a spell's
// resolution-time fizzle, and -- the two cases this file covers -- an
// instant/sorcery's normal resolution and the ability-object exile move
// once more, from a different trigger source) carried the identical hazard,
// unguarded, since c19097f: a ReplacementResult$-absent R:Event$ Moved
// replacement that matches one of those moves and does not itself relocate
// the card leaves the object on the stack forever. The reviewer measured
// this as a genuine non-terminating match (100 000+ intents, still turn 1)
// reachable from 24 corpus cards this build's own coverage gate calls fully
// playable, including Rest in Peace and Dryad Militant.
//
// Card text below is authored for this test, in the same R:/SVar$ shape as
// the corpus cards that found the bug -- never copied from Forge's
// .cards/cardsfolder.
package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// plainLandSrc is a land with no replacement of its own, used only to give
// TestAbilityObjectResolvingUnderAnExileReplacementDoesNotStickOnTheStack a
// legal play_land action that fires watcherSrc's ETB trigger.
const plainLandSrc = `Name:Open Field
Types:Land
Oracle:x
`

// harmlessInstantSrc is a targetless instant (Defined$ You, not a chosen
// target), so casting and resolving it never needs a target decision --
// keeping this test about the totality guard, not target selection.
const harmlessInstantSrc = `Name:Minor Boon
ManaCost:R
Types:Instant
A:SP$ GainLife | Defined$ You | LifeAmount$ 1
Oracle:x
`

// graveyardBlockingSpellsReplacementSrc is a NON-relocating "would go to a
// graveyard" replacement narrowed to ValidCard$ Instant,Sorcery -- the shape
// the stack-totality guard exists to protect. The controller ruling (fx28fix1)
// established that the earlier fixture (Militant Ward / Dryad Militant's
// printed shape, ReplaceWith$ ChangeZone Origin$ All ... Defined$ ReplacedCard)
// was only ever a no-op because of the Origin$ All defect, and that with that
// defect fixed it now genuinely relocates the object to exile. A relocating
// replacement removes the object from the stack itself, so the guard never
// fires and such a fixture cannot discriminate on the guard. This one is
// therefore deliberately non-relocating -- ReplaceWith$ names an effect that
// does not move the intercepted card -- so the object really is stranded on
// the stack and only the guard's rescue can land it in the graveyard.
const graveyardBlockingSpellsReplacementSrc = `Name:Grave Toll
ManaCost:1 W
Types:Creature Human Soldier
PT:2/1
R:Event$ Moved | Destination$ Graveyard | ValidCard$ Instant,Sorcery | ReplaceWith$ Levy | Description$ if an instant or sorcery card would be put into a graveyard from anywhere, its controller loses 1 life instead
SVar:Levy:DB$ LoseLife | LifeAmount$ 1
Oracle:x
`

// exileBlockingReplacementSrc mirrors graveyardBlockingSpellsReplacementSrc
// (the review's I-2 shape: an ability object's own ceases-to-exist move,
// normally parked in exile per CR 608.2m, discarded by an unrelated
// replacement) but for Destination$ Exile instead of Graveyard, and for the
// same reason as above it is deliberately NON-relocating: the earlier "Void
// Ward" fixture used the ReplaceWith$ ChangeZone Origin$ All ...
// Defined$ ReplacedCard shape, which with the Origin$ All defect fixed now
// relocates the intercepted card and so could no longer serve as the
// guard-discriminating scenery. This one intercepts a would-be exile move
// and does not move the card anywhere, so the object stays on the stack and
// only ensureLeftTheStack can take it off.
const exileBlockingReplacementSrc = `Name:Void Toll
ManaCost:1 U
Types:Enchantment
R:Event$ Moved | Destination$ Exile | ValidCard$ Card | ReplaceWith$ Bounty | Description$ if a card or ability would be exiled from anywhere, its controller gains 1 life instead
SVar:Bounty:DB$ GainLife | LifeAmount$ 1
Oracle:x
`

// TestInstantResolvingUnderAGraveyardReplacementDoesNotStickOnTheStack is
// C1's regression test (a): an ordinary instant, cast and resolved while a
// graveyard-blocking replacement is on the battlefield. Before the guard
// (resolveTop's else branch had no ensureLeftTheStack call), this hangs: the
// instant's own Stack->Graveyard Move is discarded by the replacement, which
// does NOT itself relocate the card (its ReplaceWith$ is deliberately
// non-relocating, see the fixture's doc above), nothing else removes it from
// the stack, and the bounded drive below exhausts its budget with the spell
// still on top, re-resolving every single pass. The guard's rescue is what
// lands it in the graveyard -- exactly the single Note and single Resolve
// asserted below.
func TestInstantResolvingUnderAGraveyardReplacementDoesNotStickOnTheStack(t *testing.T) {
	t.Parallel()
	e, _, id := newFixtureDeck(t, 110, harmlessInstantSrc)
	onBoard(t, e, 1, graveyardBlockingSpellsReplacementSrc)
	driveToStep(t, e, 1, 0, state.StepMain1)
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "R", Amount: 1})
	e.priorityRound()

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("expected seat 0's priority after genesis, got %+v", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "cast" && opt.Obj == id {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for the fixture instant: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit cast: %v", err)
	}
	if len(e.G.Stack) != 1 || e.G.Stack[0] != id {
		t.Fatalf("stack = %v, want [%d] right after casting", e.G.Stack, id)
	}

	const bound = 60
	n := passUntilStackEmpty(t, e, bound)
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v after %d bounded priority passes with a graveyard-blocking "+
			"replacement in play -- the instant/sorcery resolve exit is unguarded (final "+
			"review Critical C1)", e.G.Stack, bound)
	}
	if n != 2 {
		t.Fatalf("passes to drain the stack = %d, want exactly 2", n)
	}
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("zone = %s, want graveyard -- the non-relocating replacement discarded "+
			"the object's own Stack->Graveyard move without moving it anywhere, so the guard's "+
			"own graveyard move must be what lands it there", got)
	}
	if n := countKind(e.L.Events, events.Note, id); n != 1 {
		t.Fatalf("logged %d Note events for the guard's escape hatch, want exactly 1", n)
	}
	if n := countKind(e.L.Events, events.Resolve, id); n != 1 {
		t.Fatalf("logged %d Resolve events for the instant, want exactly 1 -- more means it "+
			"re-resolved instead of terminating", n)
	}
	if e.Pending() == nil {
		t.Fatal("no further decision pending -- the engine did not reach the next decision")
	}
}

// TestAbilityObjectResolvingUnderAnExileReplacementDoesNotStickOnTheStack is
// C1's regression test (b), and independently closes Task 29's own I-2
// ("the ability-object branch has the identical hazard... not fixed here"):
// a triggered ability (watcherSrc's plain ETB trigger, fired here by playing
// a harmless land) resolving normally while an exile-blocking replacement is
// on the battlefield. Before the guard, the ability object's own
// Stack->Exile "ceases to exist" Move is discarded by the replacement,
// which (deliberately, see the fixture's doc) does not relocate it, so it
// re-resolves forever (final review measured intents=300000, never
// terminating, on an equivalent board). The guard's rescue is the single
// exile move that finally lands it there.
func TestAbilityObjectResolvingUnderAnExileReplacementDoesNotStickOnTheStack(t *testing.T) {
	t.Parallel()
	e, _, landID := newFixtureDeck(t, 111, plainLandSrc)
	onBoard(t, e, 0, watcherSrc)
	onBoard(t, e, 1, exileBlockingReplacementSrc)
	driveToStep(t, e, 1, 0, state.StepMain1)

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("expected seat 0's priority after genesis, got %+v", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "play_land" && opt.Obj == landID {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no play_land option for the fixture land: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit play_land: %v", err)
	}
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the Sentinel's ETB trigger queued", e.G.Stack)
	}
	abilityID := e.G.Stack[0]

	const bound = 60
	n := passUntilStackEmpty(t, e, bound)
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v after %d bounded priority passes with an exile-blocking "+
			"replacement in play -- the ability-object resolve exit is unguarded (final "+
			"review Critical C1 / Task 29 I-2)", e.G.Stack, bound)
	}
	if n != 2 {
		t.Fatalf("passes to drain the stack = %d, want exactly 2", n)
	}
	if got := e.G.Obj(abilityID).Zone; got != state.ZExile {
		t.Fatalf("zone = %s, want exile -- the non-relocating replacement discarded the "+
			"object's own Stack->Exile move without moving it anywhere, so the guard's own "+
			"exile move must be what lands it there", got)
	}
	if n := countKind(e.L.Events, events.Note, abilityID); n != 1 {
		t.Fatalf("logged %d Note events for the guard's escape hatch, want exactly 1", n)
	}
	if n := countKind(e.L.Events, events.Resolve, abilityID); n != 1 {
		t.Fatalf("logged %d Resolve events for the ability, want exactly 1 -- more means it "+
			"re-resolved instead of terminating", n)
	}
	if e.Pending() == nil {
		t.Fatal("no further decision pending -- the engine did not reach the next decision")
	}
}

// damageWatcherSrc is watcherSrc's shape (final review R1(a)) but with a
// TARGETED Execute$ SVar (DealDamage | ValidTgts$ Creature) instead of
// watcherSrc's untargeted GainLife -- the shape the review names for
// stack.go:216, the ability branch's own CR 608.2b recheck.
//
// No board state is needed to make the target illegal: under Task 7 a
// triggered ability with ValidTgts$ pops askTarget the instant it is placed
// (pushTrigger, immediately after its TriggerPush), so with no matching
// creature anywhere this trigger is miscast at ASK time -- askTarget's CR
// 608.2b fizzle moves the just-created ability object straight to exile,
// before it ever sits on the stack to be rechecked at resolution. That means
// the totality guard tested below (ensureLeftTheStack under an exile-blocking
// replacement) is now exercised at ask time, not at resolution -- the C1
// hang this file exists to forbid cannot occur either way.
const damageWatcherSrc = `Name:Reflex Sentinel
ManaCost:1
Types:Artifact
T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card | Execute$ TrigZap | TriggerDescription$ x
SVar:TrigZap:DB$ DealDamage | ValidTgts$ Creature | NumDmg$ 1
Oracle:x
`

// TestAbilityFizzlingAtResolutionUnderAnExileReplacementDoesNotStickOnTheStack
// is final review R1(a): the ability branch's own CR 608.2b fizzle. Task 7
// moved WHEN that fizzle runs for a ValidTgts$ trigger from resolution-time
// (resolveTop's recheck) to ask-time: pushTrigger now asks targets the
// instant the trigger is placed, and askTarget with no legal target fizzles
// the just-created ability straight to exile rather than letting it sit on
// the stack. The totality property this file exists to pin -- a fizzling
// ability whose own Stack->Exile Move is discarded by
// exileBlockingReplacementSrc (which deliberately does not relocate it, so
// the object genuinely stays on the stack) must not hang or re-resolve
// forever -- is the same, but it is now exercised at ask time: the ability
// is created in the same Submit as the land entry and immediately
// fizzled, the match terminates, and no Resolve ever runs. Before the
// guard, deleting askTarget's ensureLeftTheStack call leaves the ability
// stuck on the stack forever; this asserts the terminal state that guard
// delivers. The ability id below is read by the ability object's OWN
// identity -- its o.Source is the Sentinel permanent -- not by the
// destination of a logged move, so the discovery holds whatever zone the
// guard or the replacement lands the object in.
func TestAbilityFizzlingAtResolutionUnderAnExileReplacementDoesNotStickOnTheStack(t *testing.T) {
	t.Parallel()
	e, _, landID := newFixtureDeck(t, 112, plainLandSrc)
	sentinel := onBoard(t, e, 0, damageWatcherSrc)
	onBoard(t, e, 1, exileBlockingReplacementSrc)
	driveToStep(t, e, 1, 0, state.StepMain1)

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("expected seat 0's priority after genesis, got %+v", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "play_land" && opt.Obj == landID {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no play_land option for the fixture land: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit play_land: %v", err)
	}
	// Task 7: the Sentinel's ValidTgts ETB trigger fizzles AT ASK TIME (no
	// creature to target), so the ability object never finishes its turn on
	// the stack -- it is created by its own TriggerPush and immediately
	// fizzled by askTarget's CR 608.2b exit. Find the ability object by its
	// own identity, not by where a move landed: it is the one stack object
	// whose o.Source is the Sentinel permanent (the trigger's ability object
	// id), which is stable whatever zone the guard or the replacement ends
	// up landing it in.
	var abilityID state.ObjID
	for i := range e.G.Objs {
		if o := &e.G.Objs[i]; o.Ability != nil && o.Source == sentinel {
			abilityID = o.ID
			break
		}
	}
	if abilityID == 0 {
		t.Fatal("no ability object with Source == the Sentinel found after the ask-time fizzle")
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want empty -- the trigger's ability never sat on the stack "+
			"(ask-time fizzle)", e.G.Stack)
	}
	if got := e.G.Obj(abilityID).Zone; got != state.ZExile {
		t.Fatalf("zone = %s, want exile -- the non-relocating replacement discarded the "+
			"object's own Stack->Exile move without moving it anywhere, so the guard's own "+
			"exile move must be what lands it there", got)
	}
	noteCount := 0
	noneLeft := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note {
			noteCount++
			if strings.Contains(ev.Text, "no legal targets") {
				noneLeft = true
			}
		}
	}
	if noteCount < 1 {
		t.Fatal("no guard Note logged for the fizzled ability")
	}
	if !noneLeft {
		t.Fatal(`no guard Note mentions "no legal targets"`)
	}
	if n := countKind(e.L.Events, events.Resolve, abilityID); n != 0 {
		t.Fatalf("logged %d Resolve events for the fizzled ability, want 0 -- a fizzle "+
			"returns before the Resolve emit and never runs the script", n)
	}
	if e.Pending() == nil {
		t.Fatal("no further decision pending -- the engine did not reach the next decision")
	}
}

const creatureTargetInstantSrc = `Name:Sudden Spark
ManaCost:R
Types:Instant
A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 1
Oracle:x
`

// TestSpellCounteredForNoTargetsAtCastUnderAGraveyardReplacementDoesNotStickOnTheStack
// keeps its historical name while reaching CR 608.2b through a legal cast:
// the spell chooses a creature, then that creature leaves before resolution.
// The graveyard replacement discards the spell's fizzle move, so the totality
// guard must still remove the spell from the stack.
func TestSpellCounteredForNoTargetsAtCastUnderAGraveyardReplacementDoesNotStickOnTheStack(t *testing.T) {
	t.Parallel()
	e, _, id := newFixtureDeck(t, 113, creatureTargetInstantSrc)
	target := onBoard(t, e, 1, fieldRatSrc)
	onBoard(t, e, 1, graveyardBlockingSpellsReplacementSrc)
	driveToStep(t, e, 1, 0, state.StepMain1)
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "R", Amount: 1})
	e.priorityRound()

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("expected seat 0's priority after genesis, got %+v", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "cast" && opt.Obj == id {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for the fixture instant: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit cast: %v", err)
	}

	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected target decision, got %+v", d)
	}
	idx = -1
	for _, opt := range d.Options {
		if opt.Obj == target {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("creature not offered as target: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit target: %v", err)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: target, From: state.ZBattlefield, To: state.ZGraveyard})
	passUntilStackEmpty(t, e, 8)

	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v after resolution-time fizzle, want empty", e.G.Stack)
	}
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("zone = %s, want graveyard -- the non-relocating replacement discarded "+
			"the object's own Stack->Graveyard move without moving it anywhere, so the guard's "+
			"own graveyard move must be what lands it there", got)
	}
	noteCount := 0
	noteMentionsFizzle := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Obj == id {
			noteCount++
			if strings.Contains(ev.Text, "fizzled: no legal targets") {
				noteMentionsFizzle = true
			}
		}
	}
	if noteCount != 1 {
		t.Fatalf("logged %d Note events for the guard's escape hatch, want exactly 1 per cast", noteCount)
	}
	if !noteMentionsFizzle {
		t.Fatal(`no guard Note mentions "fizzled: no legal targets"`)
	}
	if e.Pending() == nil {
		t.Fatal("no further decision pending -- the engine did not reach the next decision")
	}
}

// twinDamageInstantSrc is a minimal creature-removal instant. Two copies of
// it, both aimed at the same 1/1 (fieldRatSrc below), are the review's shape
// for stack.go:291: the top copy resolves first and kills the shared
// target, so the bottom copy finds zero legal targets when ITS turn to
// resolve comes -- CR 608.2b's resolution-time recheck, not the cast-time
// one R1(b) above covers.
const twinDamageInstantSrc = `Name:Twin Spark
ManaCost:R
Types:Instant
A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 1
Oracle:x
`

// fieldRatSrc is a vanilla 1/1 -- twinDamageInstantSrc's shared target, with
// exactly enough toughness that one copy's damage is lethal.
const fieldRatSrc = `Name:Field Rat
ManaCost:B
Types:Creature Rat
PT:1/1
Oracle:x
`

// removalSparkSrc is a creature-killing instant, cast AFTER Reflex Sentinel's
// triggered ability has already taken a legal target, to make that target stop
// being legal before the ability resolves. Five damage is lethal to a 1/1.
const removalSparkSrc = `Name:Murder Spark
ManaCost:B
Types:Instant
A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 5
Oracle:x
`

// TestSpellFizzlingAtResolutionUnderAGraveyardReplacementDoesNotStickOnTheStack
// is final review R1(c): stack.go:291, the spell branch's own CR 608.2b
// resolution-time recheck -- a target legal when chosen (fieldRatSrc, alive
// on the battlefield when both copies were cast and targeted) stops being
// legal before the bottom copy's own turn to resolve, because the top copy
// resolved first and killed it. Before the guard, deleting the :291 call
// alone leaves a reachable game hung: the bottom copy's "fizzled: no legal
// targets remain" Move is discarded by graveyardBlockingSpellsReplacementSrc
// (which deliberately does not relocate it, see the fixture's doc above),
// and nothing else ever takes it off the stack.
//
// The graveyard-blocking replacement's own ValidCard$ is "Instant,Sorcery",
// so it does not intercept fieldRatSrc's own (creature) death move -- only
// the two spells' exits are affected, keeping this test about the spell
// branch's guard, not the creature's.
func TestSpellFizzlingAtResolutionUnderAGraveyardReplacementDoesNotStickOnTheStack(t *testing.T) {
	t.Parallel()
	spell := card(t, twinDamageInstantSrc)
	e := handEngine(t, spell, spell)
	target := onBoard(t, e, 1, fieldRatSrc)
	onBoard(t, e, 1, graveyardBlockingSpellsReplacementSrc)
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "R", Amount: 2})
	e.askPriority(0)

	chooseTarget := func() {
		t.Helper()
		d := e.Pending()
		if d == nil || d.Kind != decision.KTarget {
			t.Fatalf("expected a target decision, got %+v", d)
		}
		idx := -1
		for _, opt := range d.Options {
			if opt.Kind == "permanent" && opt.Obj == target {
				idx = opt.Index
			}
		}
		if idx < 0 {
			t.Fatalf("no target option for the fixture rat: %+v", d.Options)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit target: %v", err)
		}
	}

	castFirst(t, e, "cast")
	chooseTarget()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the first Twin Spark", e.G.Stack)
	}
	bottom := e.G.Stack[0]
	castFirst(t, e, "cast")
	chooseTarget()
	if len(e.G.Stack) != 2 {
		t.Fatalf("stack = %v, want both copies of Twin Spark", e.G.Stack)
	}

	const bound = 60
	n := passUntilStackEmpty(t, e, bound)
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v after %d bounded priority passes with a graveyard-blocking "+
			"replacement in play -- the spell resolution-time fizzle exit is unguarded "+
			"(final review R1(c), stack.go:291)", e.G.Stack, bound)
	}
	if n != 4 {
		t.Fatalf("passes to drain the stack = %d, want exactly 4 -- two per resolution, "+
			"two resolutions", n)
	}
	if got := e.G.Obj(target).Zone; got != state.ZGraveyard {
		t.Fatalf("Field Rat zone = %s, want graveyard -- the top copy's damage should have "+
			"killed it", got)
	}
	if got := e.G.Obj(bottom).Zone; got != state.ZGraveyard {
		t.Fatalf("zone = %s, want graveyard -- the non-relocating replacement discarded "+
			"the object's own Stack->Graveyard move without moving it anywhere, so the guard's "+
			"own graveyard move must be what lands it there", got)
	}
	noteCount := 0
	noteMentionsFizzled := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Obj == bottom {
			noteCount++
			if strings.Contains(ev.Text, "fizzled: no legal targets") {
				noteMentionsFizzled = true
			}
		}
	}
	if noteCount < 1 {
		t.Fatal("no guard Note logged for the fizzled (bottom) spell")
	}
	if !noteMentionsFizzled {
		t.Fatal(`no guard Note mentions "fizzled: no legal targets"`)
	}
	if e.Pending() == nil {
		t.Fatal("no further decision pending -- the engine did not reach the next decision")
	}
}

// TestAbilityFizzlesAtResolutionWhenItsTargetDiesBeforeResolving closes the
// resolution-time gap the reworked ask-time test above deliberately leaves:
// Task 7 moved the WHEN of a ValidTgts$ trigger's fizzle for a trigger that
// finds no legal target AT ASK TIME to askTarget, so the legacy fixture no
// longer drives resolveTop's ability-branch recheck. That recheck (CR 608.2b)
// is still reachable for a target that WAS legal when chosen and stops being
// legal by the time the ability resolves -- and it is now untested. Here a
// target is chosen legally and then undergoes exactly that transition: play a
// Mountain, Reflex Sentinel's ETB trigger (damageWatcherSrc) asks for and is
// given the alive 1/1 rat as its target, then seat 0 casts removal at the rat
// before the trigger resolves. The removal is an instant, so it resolves
// first and kills the rat; when the sentinel ability's turn to resolve comes,
// its only target is dead, resolveTop's recheck finds zero legal targets, and
// it fizzles -- moved to exile, never resolving, never running its script.
//
// The exile-blocking replacement (exileBlockingReplacementSrc) is left in
// play so this also re-pins the totality guard at the resolution-time exit:
// it intercepts the fizzle's own Stack->Exile MoveZone (and, being
// deliberately non-relocating, leaves the object sitting on the stack), so
// the logged to-exile move is ensureLeftTheStack's re-emit -- the same
// guarantee the ask-time test above asserts, reached here through the public
// cast->resolve path rather than a direct ability-object fizzle.
func TestAbilityFizzlesAtResolutionWhenItsTargetDiesBeforeResolving(t *testing.T) {
	t.Parallel()
	mountain := card(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	removal := card(t, removalSparkSrc)
	e := handEngine(t, mountain, removal)
	sentinel := onBoard(t, e, 0, damageWatcherSrc)
	if got := e.G.Obj(sentinel).Zone; got != state.ZBattlefield {
		t.Fatalf("sentinel zone = %s, want battlefield", got)
	}
	rat := onBoard(t, e, 1, fieldRatSrc)
	onBoard(t, e, 1, exileBlockingReplacementSrc)
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "B", Amount: 1})
	e.askPriority(0)

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected priority before playing the land, got %+v", d)
	}
	playLand := -1
	for _, opt := range d.Options {
		if opt.Kind == "play_land" {
			playLand = opt.Index
		}
	}
	if playLand < 0 {
		t.Fatalf("no play_land option: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{playLand}}); err != nil {
		t.Fatalf("submit play_land: %v", err)
	}

	// The sentinel's ValidTgts trigger is now the target of its own drain,
	// asking seat 0 for a target -- the alive rat -- before any player acts.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the sentinel trigger's target decision, got %+v", d)
	}
	ratChoice := -1
	for _, opt := range d.Options {
		if opt.Kind == "permanent" && opt.Obj == rat {
			ratChoice = opt.Index
		}
	}
	if ratChoice < 0 {
		t.Fatalf("no target option for the rat at ask time: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{ratChoice}}); err != nil {
		t.Fatalf("submit sentinel target: %v", err)
	}
	abilityID := e.G.Stack[len(e.G.Stack)-1]
	if len(e.G.Stack) != 1 || e.G.Obj(abilityID).Ability == nil {
		t.Fatalf("stack = %v, want the sentinel's targeted ability on it", e.G.Stack)
	}

	// The drain finished and granted priority while the trigger sat on the
	// stack. Seat 0 now casts removal at the SAME rat -- an instant, so it
	// enters the stack above the trigger and resolves first.
	castFirst(t, e, "cast")
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the removal's target decision, got %+v", d)
	}
	ratChoice = -1
	for _, opt := range d.Options {
		if opt.Kind == "permanent" && opt.Obj == rat {
			ratChoice = opt.Index
		}
	}
	if ratChoice < 0 {
		t.Fatalf("no target option for the removal's rat: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{ratChoice}}); err != nil {
		t.Fatalf("submit removal target: %v", err)
	}

	passUntilStackEmpty(t, e, 60)
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want empty -- the sentinel ability did not leave it", e.G.Stack)
	}
	if got := e.G.Obj(rat).Zone; got != state.ZGraveyard {
		t.Fatalf("rat zone = %s, want graveyard -- the removal should have killed it", got)
	}
	// The sentinel ability fizzled at RESOLUTION time (no legal targets
	// remain): its target ceased to exist before its turn to resolve. The
	// exile-blocking replacement discarded its fizzle MoveZone (it does not
	// relocate the object), so ensureLeftTheStack re-emitted it, landing it
	// in exile.
	if got := e.G.Obj(abilityID).Zone; got != state.ZExile {
		t.Fatalf("ability zone = %s, want exile -- the resolution-time fizzle must land "+
			"it there (via the guard, under the exile-blocking replacement)", got)
	}
	fizzleNote := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Obj == abilityID && strings.Contains(ev.Text, "fizzled: no legal targets") {
			fizzleNote = true
		}
	}
	if !fizzleNote {
		t.Fatal("no guard Note records the sentinel ability fizzling for no legal targets")
	}
	if n := countKind(e.L.Events, events.Resolve, abilityID); n != 0 {
		t.Fatalf("logged %d Resolve events for the fizzled ability, want 0 -- a fizzle "+
			"returns before the Resolve emit and never runs the script", n)
	}
	if e.Pending() == nil {
		t.Fatal("no further decision pending -- the engine did not reach the next decision")
	}
}
