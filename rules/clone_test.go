package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestCloneResumeChainIndependence pins the fx34 deep copy of the suspended
// resolution's outer continuation chain (cloneResume, rules/clone.go).
// Engine.Clone is the search primitive: a host clones a snapshot and then
// drives the copy, so two engines can be alive at once. When a Clone is
// taken while a NESTED mid-resolution ask is pending, e.resume is a linked
// chain -- the innermost frame is the pending ask and each frame's `outer`
// is a continuation that must run once everything inside it resolves. Before
// fx34 the resume was one struct and `rp := *e.resume` was self-evidently a
// full copy; now it is a linked list, and a shallow copy would leave the
// clone's top frame sharing the ORIGINAL's outer frames. Because both
// engines then resume outward against the SAME frames, whatever the copy
// did (and whatever the original later did) would be indistinguishable from
// a single engine -- a silent aliasing bug every pre-fx34 clone test still
// passes, since none of them clones mid-resolution.
//
// This test clones exactly there. It drives the depth-3 fixture (the shape
// resumption_after_ask_test.go's TestResumptionRunsEveryContinuationAtDepthThree
// already pins resolves to Inn -1, Mid -2, Out -3) to the point where the
// innermost Discard ask is pending, asserts the outer chain actually exists
// (a test that clones with no chain to alias proves nothing), clones, and
// then answers the SAME pending decision on both engines -- the clone first,
// then the original -- requiring each to run every continuation and both to
// land on the same chain head.
func TestCloneResumeChainIndependence(t *testing.T) {
	charm := "Name:PiNCLONE\nManaCost:R\nTypes:Instant\n" +
		"A:SP$ Charm | Choices$ DoRepeat,DoGain | SubAbility$ Out\n" +
		"SVar:DoRepeat:SP$ Repeat | RepeatSubAbility$ DoDiscard | RepeatNum$ 1 | SubAbility$ Mid\n" +
		"SVar:DoDiscard:DB$ Discard | Defined$ You | Mode$ TgtChoose | NumCards$ 1 | SubAbility$ Inn\n" +
		"SVar:DoGain:DB$ GainLife | Defined$ You | LifeAmount$ 5\n" +
		"SVar:Out:DB$ LoseLife | Defined$ You | LifeAmount$ 3\n" +
		"SVar:Mid:DB$ LoseLife | Defined$ You | LifeAmount$ 2\n" +
		"SVar:Inn:DB$ LoseLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 93, charm)
	addMana(t, e, 0, "R")
	life := e.G.Players[0].Life

	// Pass 1: the Charm asks for a mode. Pick DoRepeat (index 0), the branch
	// that runs a Repeat, whose Repeated sub-ability is the asking Discard.
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected the Charm mode KModes ask, got %+v", d)
	}
	submitChoices(t, e, 0)
	d = passUntilNonPriority(t, e, 20)

	// The nested ask is now pending: the Repeated Discard's mid-resolution
	// KModes ask. This is the exact moment to clone. Assert the resume chain
	// actually exists -- the precondition -- so this test can never silently
	// degrade into cloning a resume with a nil outer chain (which would
	// alias nothing and prove nothing).
	d = e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected the Discard's KModes ask, got %+v", d)
	}
	if e.resume == nil || e.resume.outer == nil {
		t.Fatal("precondition: the nested ask has no outer continuation chain to alias")
	}

	// Clone here. The copy must come back with the SAME suspended resolution
	// -- the pending ask and its outer chain -- but owning its own chain.
	c := e.Clone()
	if c.resume == nil || c.resume.outer == nil {
		t.Fatal("clone lost the suspended resolution's outer chain")
	}
	if c.resume.outer == e.resume.outer {
		t.Fatal("clone aliases the original's resume chain (shallow copy of the outer continuation)")
	}

	// The clone must own the WHOLE chain, not just the head: walk both links
	// and compare frame identity at every depth.
	for cc, ce := c.resume.outer, e.resume.outer; cc != nil || ce != nil; cc, ce = cc.outer, ce.outer {
		if cc == nil || ce == nil {
			t.Fatal("clone and original resume chains differ in length")
		}
		if cc == ce {
			t.Fatal("clone aliases the original's resume chain at a nested link")
		}
	}

	// One answer served to both engines. The clone is driven FIRST, to its
	// end, so that under a shared chain the copy's traversal would consume
	// the frames before the original gets them.
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}
	if err := c.Submit(in); err != nil {
		t.Fatalf("clone rejected the pending nested answer: %v", err)
	}
	passUntilStackEmpty(t, c, 20)

	// The copy ran every continuation, each exactly once, after the answer:
	// Inn -1, Mid -2, Out -3 = three distinct lose-life events, 6 life lost.
	if got := countLoseLife(c, 0); got != 3 {
		t.Fatalf("clone lose-life events = %d, want exactly 3 (Inn -1, Mid -2, Out -3)", got)
	}
	if want := life - 6; c.G.Players[0].Life != want {
		t.Fatalf("clone caster life = %d, want %d (Inn -1, Mid -2, Out -3 each exactly once)", c.G.Players[0].Life, want)
	}

	// NOW drive the original with the very same answer. If the clone had
	// aliased the original's chain, driving the clone first would have left
	// nothing for the original to resume -- it would run zero continuations
	// and its chain head would diverge. It must still run all three.
	if err := e.Submit(in); err != nil {
		t.Fatalf("original rejected the pending nested answer: %v", err)
	}
	passUntilStackEmpty(t, e, 20)
	if got := countLoseLife(e, 0); got != 3 {
		t.Fatalf("original lose-life events = %d, want exactly 3 (Inn -1, Mid -2, Out -3); the clone's traversal consumed the shared chain", got)
	}
	if want := life - 6; e.G.Players[0].Life != want {
		t.Fatalf("original caster life = %d, want %d (Inn -1, Mid -2, Out -3 each exactly once)", e.G.Players[0].Life, want)
	}

	// The property that actually matters for replay: identical event streams
	// mean both engines resolve to the same chain head.
	if got, want := e.L.Head(), c.L.Head(); got != want {
		t.Fatalf("chain heads differ after the same nested answer: %s vs %s", got, want)
	}
}

// TestCloneCarriesDrainAwaitsModes pins the drainAwaitsModes carrier in
// Engine.Clone (jj-trg2 leaf 2). drainAwaitsModes is true only while the
// trigger drain is paused on the CR 603.3c placement modes ask; handleModes
// routes an answered KModes to the placement branch (record ChosenModes,
// resume the drain) only when the flag is set. A Clone taken at that exact
// moment must carry the flag, or the copy's answer to the very modes decision
// the original is parked on falls through to the mid-resolution resume path
// (no suspension), emits a "modes answered with no resolution suspended"
// Note, records nothing, and leaves the drain parked -- an alias-class bug
// invisible to every clone test that clones only between decisions. This
// leaf exists because removing the line failed nothing until now. Removing
// it faults here in two independent ways: c.drainAwaitsModes reads false,
// and answering on the clone records no ChosenModes.
func TestCloneCarriesDrainAwaitsModes(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Knight of Autumn")
	crAbortMove(t, e, 0, "Knight of Autumn", state.ZBattlefield)
	e.pending = nil
	e.priorityRound()
	if d := e.Pending(); d == nil || d.Kind != decision.KModes {
		t.Fatalf("fixture did not park on the placement modes ask: %+v", d)
	}
	if !e.drainAwaitsModes {
		t.Fatal("fixture precondition: drainAwaitsModes is not set while the modes ask is pending")
	}
	top := e.G.Stack[len(e.G.Stack)-1]

	c := e.Clone()
	if !c.drainAwaitsModes {
		t.Fatal("clone lost drainAwaitsModes: the modes answer will not resume through the placement branch")
	}

	// Answer the very same pending decision on both engines and require each
	// to record the chosen modes onto its own stack object and continue the
	// drain. A clone that dropped the flag takes the resume path and records
	// nothing.
	d := c.Pending()
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}
	if err := c.Submit(in); err != nil {
		t.Fatalf("clone rejected the pending modes answer: %v", err)
	}
	if err := e.Submit(in); err != nil {
		t.Fatalf("original rejected the pending modes answer: %v", err)
	}

	if co := c.G.Obj(top); co == nil || len(co.ChosenModes) == 0 {
		t.Fatalf("clone did not record the placement mode choice onto the stack object: ChosenModes=%v", c.G.Obj(top).ChosenModes)
	}
	if eo := e.G.Obj(top); eo == nil || len(eo.ChosenModes) == 0 {
		t.Fatalf("original did not record the placement mode choice: ChosenModes=%v", e.G.Obj(top).ChosenModes)
	}
	if c.G.Obj(top).ChosenModes[0] != e.G.Obj(top).ChosenModes[0] {
		t.Fatalf("clone and original disagree on the chosen mode: %v vs %v", c.G.Obj(top).ChosenModes, e.G.Obj(top).ChosenModes)
	}
	if c.L.Head() != e.L.Head() {
		t.Fatalf("chain heads differ after the same modes answer: %s vs %s", c.L.Head(), e.L.Head())
	}
}

// drive answers n decisions with the package's own testBot and returns the
// intents it submitted, so the same choices can be replayed elsewhere.
func drive(t *testing.T, e *Engine, b *testBot, n int) []decision.Intent {
	t.Helper()
	var out []decision.Intent
	for i := 0; i < n && !e.G.Over && e.Pending() != nil; i++ {
		d := e.Pending()
		in := b.answer(e, d)
		if err := e.Submit(in); err != nil {
			t.Fatalf("intent %d: %v", i, err)
		}
		out = append(out, in)
	}
	return out
}

// seedInternalQueues populates every Engine-internal collection Clone must
// copy -- continuous effects, the pending-trigger queue, and both
// trigger-bookkeeping maps -- so a Clone that aliases, rebinds, or simply
// drops one of them shows up as a real assertion failure instead of two
// tests comparing empty collections to each other (a fixture driven only a
// few dozen decisions has none of these populated on its own).
//
// The pending trigger's Source is a battlefield permanent with no T: lines
// of its own (Idx 0 is therefore always out of range for it), so draining it
// through the real trigger pipeline -- which TestCloneStaysIndependentAndReplaysInLockstep's
// further Submit calls do reach, via putTriggersOnStack -- is a guaranteed,
// side-effect-free no-op (events.Apply's TriggerPush case degrades an
// out-of-range index to nothing) rather than resolving an arbitrary card's
// script against fabricated targets.
func seedInternalQueues(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	var src state.ObjID
	for p := state.PlayerID(0); src == 0 && int(p) < len(e.G.Players); p++ {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			if o := e.G.Obj(id); o != nil {
				if f := o.Face(); f != nil && len(f.Triggers) == 0 {
					src = id
					break
				}
			}
		}
	}
	if src == 0 {
		t.Fatal("fixture has no trigger-free battlefield permanent to seed internal state with")
	}

	e.AddContinuous(ContinuousEffect{Source: src, Timestamp: 1, Layer: LAbilities,
		Affects: "Card.Self", AddKeywords: []string{"Flying"}, AddTypes: []string{"Bird"}, UntilEOT: true})
	e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
		Source: src, Idx: 0,
		Ctx: effects.Ctx{
			Targets:    []state.Target{{Obj: src}},
			Remembered: []state.Target{{Obj: src}},
			SVars:      map[string]string{"k": "v"},
			// LKI: a fabricated snapshot with a nonzero counter, so Clone's
			// own deep copy of it (not just of the live object it describes,
			// which Game.Clone already handles) has something to prove --
			// see the LKI-aliasing assertions in
			// TestCloneSharesNoMutableStateWithTheOriginal.
			LKI: &state.Object{ID: src, Counters: []state.Counter{{Kind: "P1P1", N: 2}}},
		},
	})
	e.triggerFireCount = map[triggerKey]int32{{Source: src, Idx: 0}: 1}
	// An OPEN damage batch with one latched (trigger, referent) entry, whose
	// recorded pendingTriggers index names the entry seeded above (index 0),
	// so Clone's copies of all three batch fields have something to prove.
	onceKey := damageBatchKey{triggerKey: triggerKey{Source: 1, Idx: 0}, dealt: true, obj: src}
	e.damageBatchOpen = true
	e.damageBatchDepth = 1
	e.damageBatchIdx = map[damageBatchKey]int{onceKey: 0}
	e.damageBatchLog = []damageBatchEntry{{key: onceKey, idx: 0, amount: 3}}
	e.sourceLifelinkLKI = map[state.ObjID]bool{src: true}

	// A pending cast (Task 9), with its own non-empty slices -- delve/sacs
	// and the cost's own Sac/SubCounter parts -- so a Clone that omits,
	// aliases, or only shallow-copies e.cast shows up as a real assertion
	// failure the same way an omitted continuous/pendingTriggers would.
	e.cast = &pendingCast{
		player:  0,
		card:    src,
		from:    state.ZHand,
		mode:    "kicked",
		ability: -1,
		cost:    Cost{Generic: 1, Sac: []CostPart{{N: 1, Spec: "Creature"}}},
		x:       1,
		delve:   []state.ObjID{src},
		sacs:    []state.ObjID{src},
	}

	if len(e.continuous) == 0 || len(e.pendingTriggers) == 0 ||
		len(e.triggerFireCount) == 0 || len(e.damageBatchLog) == 0 ||
		len(e.sourceLifelinkLKI) == 0 || e.cast == nil {
		t.Fatal("fixture seeding left an internal collection empty")
	}
	return src
}

func TestCloneStaysIndependentAndReplaysInLockstep(t *testing.T) {
	names, decks := testutil.SampleDecks(t, 4)
	e := New(Config{Seed: 7, Names: names, Decks: decks})
	e.Advance()
	bot := newTestBot(7)
	drive(t, e, bot, 40)
	seedInternalQueues(t, e)

	c := e.Clone()
	// Checked immediately, before either side is driven further: a Clone
	// that omits continuous/pendingTriggers/the trigger maps entirely would
	// leave c's copies at zero while e's (seeded above) are not, catching
	// the omission here rather than after both sides have independently
	// drained the same seeded queue back down to empty.
	if len(c.continuous) != len(e.continuous) || len(c.pendingTriggers) != len(e.pendingTriggers) ||
		len(c.triggerFireCount) != len(e.triggerFireCount) || len(c.damageBatchLog) != len(e.damageBatchLog) ||
		c.damageBatchDepth != e.damageBatchDepth || len(c.sourceLifelinkLKI) != len(e.sourceLifelinkLKI) || c.cast == nil {
		t.Fatalf("clone did not copy the seeded internal state: continuous %d/%d, triggers %d/%d, fireCount %d/%d, batchLog %d/%d, batchDepth %d/%d, sourceLKI %d/%d, cast nil=%v",
			len(c.continuous), len(e.continuous), len(c.pendingTriggers), len(e.pendingTriggers),
			len(c.triggerFireCount), len(e.triggerFireCount), len(c.damageBatchLog), len(e.damageBatchLog),
			c.damageBatchDepth, e.damageBatchDepth, len(c.sourceLifelinkLKI), len(e.sourceLifelinkLKI), c.cast == nil)
	}
	headBefore, drawsBefore, eventsBefore := e.L.Head(), e.RNGDraws(), len(e.L.Events)
	if got := diffGames(e.G, c.G); got != "" {
		t.Fatalf("clone differs from original at the boundary: %s", got)
	}
	// The seeded pendingCast is Clone coverage, not a reachable state (its
	// card is a battlefield permanent): with its copy checked above, both
	// sides drop it so the lockstep drive below plays a reachable game, where
	// no priority is ever granted with a cast proposal open.
	e.cast, c.cast = nil, nil

	// Diverge the clone by 60 more decisions; the original must not move.
	recorded := drive(t, c, bot, 60)
	if len(recorded) == 0 {
		t.Fatal("clone accepted no intents")
	}
	if e.L.Head() != headBefore || e.RNGDraws() != drawsBefore || len(e.L.Events) != eventsBefore {
		t.Fatal("driving the clone changed the original")
	}

	// Feed the original the very same intents: identical events, chain head
	// and RNG position mean the clone copied every piece of engine state.
	for i, in := range recorded {
		if err := e.Submit(in); err != nil {
			t.Fatalf("original rejected recorded intent %d: %v", i, err)
		}
	}
	if e.L.Head() != c.L.Head() {
		t.Fatalf("chain heads differ after lockstep: %s vs %s", e.L.Head(), c.L.Head())
	}
	if e.RNGDraws() != c.RNGDraws() {
		t.Fatalf("RNG draws differ: %d vs %d", e.RNGDraws(), c.RNGDraws())
	}
	// The RNGDraws equality above is vacuous by itself: this fixture's decks
	// never draw from the engine's RNG past the opening shuffle (both sides
	// hold steady at the same count for the whole test), so a clone.rng that
	// re-seeded from scratch instead of copying the PCG's position would
	// still pass it. Drawing one more value from each side's RNG and
	// requiring them to agree only holds if Clone copied the exact position.
	if got, want := c.Rand(100), e.Rand(100); got != want {
		t.Fatalf("RNG positions differ after lockstep: clone drew %d, original drew %d", got, want)
	}
	if got := diffGames(e.G, c.G); got != "" {
		t.Fatalf("games differ after lockstep: %s", got)
	}
	if len(e.pendingTriggers) != len(c.pendingTriggers) || len(e.continuous) != len(c.continuous) {
		t.Fatalf("engine-internal queues differ: triggers %d/%d, continuous %d/%d",
			len(e.pendingTriggers), len(c.pendingTriggers), len(e.continuous), len(c.continuous))
	}
}

func TestCloneSharesNoMutableStateWithTheOriginal(t *testing.T) {
	names, decks := testutil.SampleDecks(t, 2)
	e := New(Config{Seed: 3, Names: names, Decks: decks})
	e.Advance()
	drive(t, e, newTestBot(3), 30)
	src := seedInternalQueues(t, e)
	// Nonzero, non-default values for the two bookkeeping scalars: neither
	// test drives the engine again after this point, so leaving them set
	// (rather than the transient save/restore every production caller uses)
	// has no further effect and gives Clone something real to copy.
	e.orderedTriggers = 1
	e.applyingReplacement = true
	c := e.Clone()

	if c.orderedTriggers != e.orderedTriggers || c.applyingReplacement != e.applyingReplacement {
		t.Fatalf("clone did not copy the engine scalars: orderedTriggers %d/%d, applyingReplacement %v/%v",
			c.orderedTriggers, e.orderedTriggers, c.applyingReplacement, e.applyingReplacement)
	}

	// Mutate every cloned collection IN PLACE (index/key writes, never
	// append past a slice's length): an append that exceeds capacity
	// allocates a fresh backing array regardless of whether the clone
	// aliased the original, silently passing even a Clone that aliased
	// everything. An in-place write only leaves the original untouched if
	// Clone actually copied the backing storage.
	//
	// Log is deliberately NOT in this battery: a stored Event or Intent is
	// append-only, hash-chained history, so since A3 Log.Clone shares the
	// backing arrays (each capped at len, so the first append reallocates)
	// and an in-place write to c.L.Events[i] now writes the ORIGINAL's
	// slot too. That is not a Clone defect -- it is the contract: you do
	// not mutate a stored event, on either log, any more than you mutate
	// one already in the chain. The two logs still diverge cleanly by
	// appending, which events.TestLogCloneAppendsDiverge pins.
	c.G.Players[0].Life = -100
	if c.pending != nil && len(c.pending.Options) > 0 {
		c.pending.Options[0].Label = "mutated"
	}
	c.continuous[0].AddKeywords[0] = "Mutated"
	c.pendingTriggers[0].Ctx.Targets[0].Obj = 9999
	c.pendingTriggers[0].Ctx.SVars["k"] = "mutated"
	if c.pendingTriggers[0].Ctx.LKI == nil {
		t.Fatal("clone dropped a pending trigger's LKI")
	}
	c.pendingTriggers[0].Ctx.LKI.AddCounter("P1P1", 5)
	c.triggerFireCount[triggerKey{Source: src, Idx: 0}] = 99
	// The same referent key seedInternalQueues latched; re-derived here
	// because the two sites sit in different functions.
	batchKey := damageBatchKey{triggerKey: triggerKey{Source: 1, Idx: 0}, dealt: true, obj: src}
	c.damageBatchLog[0].amount = 99
	c.damageBatchIdx[batchKey] = 7
	c.sourceLifelinkLKI[src] = false
	c.cast.delve[0] = 9999
	c.cast.sacs[0] = 9999
	c.cast.cost.Sac[0].N = 99

	if e.G.Players[0].Life == -100 {
		t.Fatal("clone shares Game or Log storage with the original")
	}
	if e.pending != nil && len(e.pending.Options) > 0 && e.pending.Options[0].Label == "mutated" {
		t.Fatal("clone shares the pending decision's Options")
	}
	for _, ce := range e.continuous {
		for _, k := range ce.AddKeywords {
			if k == "Mutated" {
				t.Fatal("clone shares a continuous effect's AddKeywords")
			}
		}
	}
	for _, pt := range e.pendingTriggers {
		for _, tgt := range pt.Ctx.Targets {
			if tgt.Obj == 9999 {
				t.Fatal("clone shares a pending trigger's Targets")
			}
		}
		if pt.Ctx.SVars["k"] == "mutated" {
			t.Fatal("clone shares a pending trigger's SVars map")
		}
		if pt.Ctx.LKI != nil && pt.Ctx.LKI.Counter("P1P1") != 2 {
			t.Fatalf("clone shares a pending trigger's LKI: original counter = %d, want 2 (unchanged)",
				pt.Ctx.LKI.Counter("P1P1"))
		}
	}
	if e.triggerFireCount[triggerKey{Source: src, Idx: 0}] == 99 {
		t.Fatal("clone shares triggerFireCount")
	}
	if e.damageBatchLog[0].amount == 99 || e.damageBatchIdx[damageBatchKey{triggerKey: triggerKey{Source: 1, Idx: 0}, dealt: true, obj: src}] == 7 {
		t.Fatal("clone shares the damage-batch bookkeeping")
	}
	if !e.sourceLifelinkLKI[src] {
		t.Fatal("clone shares sourceLifelinkLKI")
	}
	if e.cast.delve[0] == 9999 {
		t.Fatal("clone shares a pending cast's delve slice")
	}
	if e.cast.sacs[0] == 9999 {
		t.Fatal("clone shares a pending cast's sacs slice")
	}
	if e.cast.cost.Sac[0].N == 99 {
		t.Fatal("clone shares a pending cast's cost.Sac slice")
	}
}

func TestCloneOfAFinishedGameIsFinished(t *testing.T) {
	t.Parallel()
	names, decks := testutil.SampleDecks(t, 2)
	e := New(Config{Seed: 11, Names: names, Decks: decks})
	e.Advance()
	drive(t, e, newTestBot(11), 400000)
	if !e.G.Over {
		t.Fatal("fixture game did not finish")
	}
	c := e.Clone()
	if !c.G.Over || c.L.Head() != e.L.Head() || c.Pending() != nil {
		t.Fatal("clone of a finished game is not finished with the same head")
	}
	if err := c.Submit(decision.Intent{}); err == nil {
		t.Fatal("clone of a finished game accepted an intent")
	}
}

// TestCloneCarriesTheMulliganRound pins the pregame half of Clone's own
// contract. Engine.pregame and Engine.mulligan are both documented as
// "Clone copies it like every other value field" (engine.go) — and neither
// is in Clone's struct literal, so a clone taken while the London mulligan
// round is running comes back with pregame false and a zero mulliganRound.
// Answering the very decision the original was parked on then finds no seat
// in mulligan.seats, leaves the round index at -1 and indexes kept[-1].
//
// That is not a theoretical hazard: host.viewAt reaches an intent boundary
// by cloning a snapshot and RE-SUBMITTING the recorded intents, so every
// GET /api/tables/{t}/matches/{k}/view?seq=N inside a live match's mulligan
// window 500s with "viewAt(seq=N) panicked: index out of range [-1]" — a
// spectator scrubbing the DVR into the opening hand gets a blank board.
func TestCloneCarriesTheMulliganRound(t *testing.T) {
	names, decks := testutil.SampleDecks(t, 4)
	e := New(Config{Seed: 7, Names: names, Decks: decks, Mulligans: 1})
	e.Advance()
	d := e.Pending()
	if d == nil || d.Kind != decision.KMulligan {
		t.Fatalf("fixture did not park on a mulligan decision: %+v", d)
	}

	c := e.Clone()
	if !c.pregame {
		t.Fatal("clone lost pregame: the mulligan round is not running on the copy")
	}
	if len(c.mulligan.seats) != len(e.mulligan.seats) || len(c.mulligan.kept) != len(e.mulligan.kept) ||
		len(c.mulligan.taken) != len(e.mulligan.taken) || c.mulligan.limit != e.mulligan.limit {
		t.Fatalf("clone lost the mulligan round: seats %d/%d kept %d/%d taken %d/%d limit %d/%d",
			len(c.mulligan.seats), len(e.mulligan.seats), len(c.mulligan.kept), len(e.mulligan.kept),
			len(c.mulligan.taken), len(e.mulligan.taken), c.mulligan.limit, e.mulligan.limit)
	}

	// The panic itself: the same intent, answered on both engines, must
	// reach the same chain head instead of indexing kept[-1] on the copy.
	bot := newTestBot(7)
	in := bot.answer(e, d)
	if err := c.Submit(in); err != nil {
		t.Fatalf("clone rejected the pending mulligan answer: %v", err)
	}
	if err := e.Submit(in); err != nil {
		t.Fatalf("original rejected the pending mulligan answer: %v", err)
	}
	if e.L.Head() != c.L.Head() {
		t.Fatalf("chain heads differ after the same mulligan answer: %s vs %s", e.L.Head(), c.L.Head())
	}

	// The round's mutable slices must be re-allocated, not aliased: kept
	// (mulligan.go:173, kept[i] = true) and taken (mulligan.go:179,
	// taken[i]++) are both written in place, so a shallow struct copy would
	// let one engine's answer show up in the other's round. seats is only
	// ever replaced wholesale, but it is copied too and pinned here so the
	// round has no shared backing array at all.
	if len(c.mulligan.kept) > 0 {
		c.mulligan.kept[0] = !c.mulligan.kept[0]
		if e.mulligan.kept[0] == c.mulligan.kept[0] {
			t.Fatal("clone aliases the original's mulligan.kept slice")
		}
	}
	if len(c.mulligan.taken) > 0 {
		c.mulligan.taken[0] += 100
		if e.mulligan.taken[0] == c.mulligan.taken[0] {
			t.Fatal("clone aliases the original's mulligan.taken slice")
		}
	}
	if len(c.mulligan.seats) > 0 {
		c.mulligan.seats[0] = c.mulligan.seats[0] + 1
		if e.mulligan.seats[0] == c.mulligan.seats[0] {
			t.Fatal("clone aliases the original's mulligan.seats slice")
		}
	}
}

// TestCloneCopiesCounterAskCursors pins the deep copy of the two
// decision-derived answer cursors (rules/clone.go, the moveCounterAsk /
// aorAsk discipline): a clone taken while a MoveCounter or
// AddOrRemoveCounter resolution is suspended on its own mid-resolution ask
// must carry the answered entries forward AND own its own storage, or the
// clone re-asks an already-answered kind (the counterchoice1 round-2
// finding) and its decision stream diverges from the original's.
func TestCloneCopiesCounterAskCursors(t *testing.T) {
	names, decks := testutil.SampleDecks(t, 2)
	e := New(Config{Seed: 5, Names: names, Decks: decks})
	e.Advance()
	e.moveCounterAsk = map[state.ObjID]*moveCounterPending{
		7: {targets: []state.Target{{Player: 1, IsPlayer: true}}, kind: "P1P1", kindSet: true, n: 3, nSet: true},
	}
	e.aorAsk = map[state.ObjID]map[string]bool{7: {"P1P1": true}}
	c := e.Clone()
	if p := c.moveCounterAsk[7]; p == nil || !p.kindSet || p.kind != "P1P1" || len(p.targets) != 1 {
		t.Fatalf("clone dropped or mangled the moveCounterAsk cursor: %+v", p)
	}
	if set := c.aorAsk[7]; set == nil || !set["P1P1"] {
		t.Fatalf("clone dropped the aorAsk cursor: %+v", set)
	}
	// In-place mutation of the clone's entries must never reach the
	// original's storage.
	c.moveCounterAsk[7].targets[0] = state.Target{Obj: 99}
	c.moveCounterAsk[7].kind = "TIME"
	c.aorAsk[7]["TIME"] = true
	delete(c.aorAsk[7], "P1P1")
	if o := e.moveCounterAsk[7]; o.kind != "P1P1" || o.targets[0].Obj != 0 || o.targets[0].Player != 1 {
		t.Fatalf("clone shares the moveCounterAsk pending with the original: %+v", o)
	}
	if o := e.aorAsk[7]; !o["P1P1"] || o["TIME"] || len(o) != 1 {
		t.Fatalf("clone shares the aorAsk kind set with the original: %+v", o)
	}
}
