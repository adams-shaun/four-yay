// Task 29: rules/trigger.go's applyReplacements used to treat every
// matching R:Event$ Moved replacement as Forge's ReplacementResult$
// Replaced -- resolve ReplaceWith$, discard the original event. That is
// wrong for the corpus's dominant shape, ReplacementResult$ Updated ("the
// event still happens, augmented"): a permanent's own MoveZone onto the
// battlefield got swallowed, so it never left the stack and resolveTop
// (rules/stack.go) re-resolved the same object forever. See
// .superpowers/sdd/2026-09-03-mtgcore-m0-m1/task-29-brief.md and Task 26's
// report (grep ReplacementResult) for the corpus measurements and the
// Geralf's Messenger / Hallowed Fountain / Celestial Colonnade repro.
//
// Card text below is authored for this test, in the same R:/SVar$ shape as
// the corpus cards that found the bug -- never copied from Forge's
// .cards/cardsfolder.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// tappedLandSrc is the Hallowed Fountain shape the brief names: a land whose
// only R: line says "you may pay 2 life... enters tapped" -- simplified here
// to the unconditional "enters tapped" the brief's own excerpt shows, since
// the UnlessCost$ branch is not what Task 29 is about.
const tappedLandSrc = `Name:Tide Gate
Types:Land
R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ DBTap | ReplacementResult$ Updated | Description$ enters tapped
SVar:DBTap:DB$ Tap | Defined$ Self | ETB$ True
Oracle:x
`

// tappedCreatureSrc is the Geralf's Messenger shape: the identical R: line
// on a creature instead of a land, cast and resolved off the stack rather
// than played directly.
const tappedCreatureSrc = `Name:Grave Walker
ManaCost:B
Types:Creature Zombie
PT:2/2
R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ DBTap | ReplacementResult$ Updated | Description$ enters tapped
SVar:DBTap:DB$ Tap | Defined$ Self | ETB$ True
Oracle:x
`

// fullyReplacedEntrySrc is a permanent spell whose ETB replacement is an
// explicit ReplacementResult$ Replaced -- unlike tappedCreatureSrc, its
// ReplaceWith$ (RepLife) never moves the object anywhere, which is exactly
// the resolveTop totality hazard: nothing else ever takes it off the stack.
const fullyReplacedEntrySrc = `Name:Hollow Construct
ManaCost:2
Types:Artifact Creature Golem
PT:2/2
R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ RepLife | ReplacementResult$ Replaced | Description$ x
SVar:RepLife:DB$ GainLife | Defined$ You | LifeAmount$ 1
Oracle:x
`

// watcherSrc is an unrelated permanent with a plain "whenever a permanent
// enters the battlefield" trigger, for test 6: proof that checkTriggers
// still sees an Updated Move.
const watcherSrc = `Name:Sentinel
ManaCost:1
Types:Artifact
T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card | Execute$ TrigGain | TriggerDescription$ x
SVar:TrigGain:DB$ GainLife | Defined$ You | LifeAmount$ 1
Oracle:x
`

// graveyardBlockingReplacementSrc is the shipped corpus's Rest in Peace
// shape (review finding I-1): a broad "if a card would be put into a
// graveyard from anywhere, [do something else] instead" replacement, with
// no ValidCard$ narrowing beyond "any card" and no ReplacementResult$ (so
// today's ReplacementResult$-absent code path, unchanged by this task,
// still discards the original Move). Its ReplaceWith$ uses Defined$
// ReplacedCard, which effects/context.go:51 DOES model, and since the
// Origin$ All fix (fx28) effects/zone.go's ParseZones treats All as a real
// wildcard, so this replacement genuinely relocates the intercepted card to
// exile. It remains a totality hazard for the case this test covers,
// because it also matches the GUARD'S OWN cleanup move -- the I-1 fix runs
// that move with applyingReplacement held true so the guard cannot be
// intercepted by the very replacement it is rescuing an object from.
//
// This comment previously claimed Defined$ ReplacedCard was unmodelled and
// that the replacement "relocates NOTHING". That was false in both halves:
// the shape was inert only because Origin$ All mis-parsed to Graveyard, and
// the six stack_totality_test.go oracles written on that premise had to be
// rebuilt when it was fixed.
const graveyardBlockingReplacementSrc = `Name:Bone Vault
ManaCost:1 W
Types:Enchantment
R:Event$ Moved | Destination$ Graveyard | ValidCard$ Card | ReplaceWith$ ExileInstead | Description$ if a card would go to a graveyard from anywhere, exile it instead
SVar:ExileInstead:DB$ ChangeZone | Origin$ All | Destination$ Exile | Defined$ ReplacedCard
Oracle:x
`

// newFixtureDeck builds a 2-seat engine the same way newSeats does, except
// seat 0's deck leads with fixture (followed by each extra, in order, then
// mountains), so cfg.Decks -- and therefore replayFromLog's own genesis
// reconstruction -- actually contains them (Ruling: an object added to a
// live Engine after New via a direct AddObject call, the way several
// existing tests in this package do it, is invisible to replayFromLog,
// which only rebuilds from cfg.Decks; test 5 below needs the object to line
// up by ID in both). The extras are how cast_test.go's fixture helpers seed
// a real (non-token) card that must start in a graveyard or hand: genesis
// creates it with IsToken=false at an ID the replay reproduces, and the
// helper moves it with a logged MoveZone.
//
// Genesis's shuffle (seeded, but not something this test controls the
// outcome of) may land fixture in the opening hand or leave it in the
// library; either way it ends up in seat 0's hand before returning, via a
// logged MoveZone bridge in the library case. That bridge is deliberately
// NOT counted by any assertion below that cares how many times the fixture
// entered the BATTLEFIELD -- it is unrelated setup, not part of what Task
// 29 changed.
//
// This deliberately stops at turn 1's upkeep (Advance's own first priority
// ask) rather than driving all the way to main1 itself: a pending
// decision's Options is a snapshot legalActions computed once, when ask()
// built it, so any further raw e.emit setup a caller still needs (funding
// mana for a cast, say) must happen BEFORE the main1 priority ask is built,
// not after -- callers that need such setup must do it here, at upkeep,
// then call driveToStep(t, e, 1, 0, state.StepMain1) themselves once done.
func newFixtureDeck(t *testing.T, seed uint64, fixtureSrc string, extras ...string) (*Engine, Config, state.ObjID) {
	t.Helper()
	fixture := card(t, fixtureSrc)
	name := fixture.Faces[0].Name
	deck := []*cards.Card{fixture}
	for _, extra := range extras {
		deck = append(deck, card(t, extra))
	}
	var e *Engine
	var cfg Config
	build := func(s uint64) Config {
		return Config{Seed: s, Names: []string{"a", "b"},
			Decks: [][]*cards.Card{
				append(append([]*cards.Card{fixture}, deck[1:]...), mountainDeck(t, 40-len(deck))...),
				mountainDeck(t, 40),
			},
			// Non-nil (not just the zero value) so a caller that mints a test
			// fixture into the live game via a logged TokenCreate event (Task
			// 9's cast_test.go putToken, for a second card replayFromLog could
			// otherwise never reconstruct -- see this function's own doc above)
			// shares the same map between e.G.Tokens and the cfg this function
			// returns: both are copies of this one Config value, and a map is a
			// reference type, so a key added to e.G.Tokens after New() is also
			// visible through cfg.Tokens at replayCheck time. Every caller that
			// never touches Tokens sees no behaviour change: an empty map reads
			// exactly like a nil one everywhere TokenCreate's Apply case reads it.
			Tokens: map[string]*cards.Card{},
		}
	}
	// Under the CR 103.1 toss (rules.New draws the starting seat) these
	// scenario fixtures' protagonist is seat 0 -- before the toss it was
	// always the starting player, and every caller funds and moves seat 0's
	// zones by index. seatZeroStart advances the seed until the toss starts
	// seat 0 (see its own doc); the caller's seed still pins deck content
	// whenever the toss already starts seat 0, and the effective seed
	// travels out in the returned cfg for replayCheck.
	cfg = seatZeroStart(build(seed))
	e = New(cfg)
	e.Advance()

	var id state.ObjID
	for _, cand := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(cand).Face().Name == name {
			id = cand
		}
	}
	if id == 0 {
		for _, cand := range e.G.Zone(state.ZLibrary, 0) {
			if e.G.Obj(cand).Face().Name == name {
				id = cand
			}
		}
		if id == 0 {
			t.Fatalf("fixture %q not found in seat 0's hand or library", name)
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
		// The bridge ran after Advance built the upkeep priority snapshot, so
		// that snapshot does not offer the bridged card and a caller casting
		// from the pending decision would fatal. Clearing the stale pending
		// and re-driving is the same net state the live Submit loop reaches
		// (cast_test.go and friends do the same e.pending = nil) -- the fresh
		// ask is seat 0's priority with the fixture in hand.
		e.pending = nil
		e.Advance()
	}
	return e, cfg, id
}

// countMoves reports how many times id was moved to exactly `to` anywhere in
// the log -- the metric applyReplacements' Updated fix is actually about,
// independent of whatever unrelated setup (a library->hand bridge, mana
// funding) also touched the log.
func countMoves(log []events.Event, id state.ObjID, to state.Zone) int {
	n := 0
	for _, ev := range log {
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.To == to {
			n++
		}
	}
	return n
}

func countKind(log []events.Event, k events.Kind, id state.ObjID) int {
	n := 0
	for _, ev := range log {
		if ev.Kind == k && ev.Obj == id {
			n++
		}
	}
	return n
}

// passUntilStackEmpty answers "pass" priority decisions until the stack is
// empty, stopping the instant it is -- unlike passAll's fixed budget, which
// would happily keep passing turn after turn once the spell under test has
// long since resolved, running the very next turn's untap step and undoing
// the "entered tapped" state this whole test file exists to check. Bounded
// the same way passThroughStep is, so a real non-termination regression
// (this file's whole point) fails an assertion instead of hanging the test
// binary.
func passUntilStackEmpty(t *testing.T, e *Engine, limit int) int {
	t.Helper()
	n := 0
	for ; n < limit && !e.G.Over && len(e.G.Stack) > 0; n++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining the stack (stack depth %d)", len(e.G.Stack))
		}
		if d.Kind == decision.KChoose && d.ResumeKind == "dig" {
			// A Dig look-and-take ask suspended mid-resolution (Jace, the
			// Mind Sculptor's [+2] is the live shape: ChangeNum$ Any makes
			// the take optional, so the engine now poses the ask it always
			// should have). These drains were written around the pre-ask
			// engine, whose dig was a silent no-op, so decline -- the empty
			// answer Min 0 makes legal -- and keep exactly the board those
			// assertions were written against. A test that wants the take
			// answers the ask itself before draining (dig_variants_test.go).
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
				t.Fatalf("submit dig decline: %v", err)
			}
			continue
		}
		if d.Kind == decision.KArrange && d.ResumeKind == "dig_arrange" {
			// A Dig's ordered-bottom ask suspended mid-resolution: the untaken
			// window cards go to the bottom in the OFFERED order -- the same
			// deterministic answer botpolicy's clamp top-up gives, so a drain
			// written around the pre-arrange engine keeps its board. Min ==
			// Max == len(Options), so the full permutation is the only legal
			// shape and the offered order is one.
			choices := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				choices = append(choices, o.Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
				t.Fatalf("submit dig bottom order: %v", err)
			}
			continue
		}
		if d.Kind == decision.KChoose && d.ResumeKind == "vote" {
			// A per-voter ballot ask (task vote_card_self1): the card and
			// fixed-list Votes now pose a private ask mid-resolution where
			// the pre-ask engine silently took the ballot's first option.
			// These drains were written around that stand-in, so answer
			// option 0 -- the deterministic first ballot entry, exactly the
			// behaviour the assertions were written against. A test that
			// wants to inspect or steer the vote answers it itself before
			// draining (vote_card_bot_test.go, vote_card_self_test.go).
			if len(d.Options) == 0 {
				t.Fatalf("vote ask with no option to take: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit vote first option: %v", err)
			}
			continue
		}
		if d.Kind == decision.KChoose && d.ResumeKind == "damage_split" {
			// A DealDamage DividedAsYouChoose$ allocation now suspends
			// mid-resolution where it used to round-robin silently. Drains
			// written around that stand-in reproduce the round-robin split:
			// the ask is Min == Max == the scripted total over one option per
			// chosen target in order, so cycling the option indices assigns
			// one damage at a time in that same order and leaves these tests
			// the board their assertions were written against. A test that
			// wants a different allocation answers the ask itself first
			// (fury_damage_split_test.go).
			choices := make([]int, 0, d.Min)
			for i := 0; i < d.Min && len(d.Options) > 0; i++ {
				choices = append(choices, d.Options[i%len(d.Options)].Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
				t.Fatalf("submit damage split round-robin: %v", err)
			}
			continue
		}
		if d.Kind == decision.KChoose && d.ResumeKind == "search_confirm" {
			// An explicit Optional$ hidden-origin search now poses Forge's
			// confirmAction gate before the fetch list (task
			// optional-library-confirm). These drains were written around the
			// pre-confirmation engine, whose search proceeded straight to the
			// pick -- or resolved silently on an empty pool -- so accept, the
			// deterministic "do the may" answer, and keep exactly the board
			// the assertions were written against. A test that wants to steer
			// or decline the confirmation answers it itself before draining
			// (optional_library_search_confirm_test.go).
			if len(d.Options) == 0 {
				t.Fatalf("search confirmation with no option to take: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit search confirmation: %v", err)
			}
			continue
		}
		if d.Kind != decision.KPriority {
			if d.Kind == decision.KTarget && d.ResumeKind == "copy_targets" {
				// CR 707.10c: a copy with MayChooseTarget$ asks its
				// controller for a new target. AskCopyTargets places the
				// inherited targets first, in order, so the leading d.Min
				// options are the deterministic keep-current answer -- a
				// single-target copy takes one, a multi-target copy takes
				// all of its inherited targets. Every drain that was
				// written around the pre-election engine keeps exactly the
				// board its assertions expect by taking them; a test that
				// wants a different target answers the ask itself first.
				choices := make([]int, 0, d.Min)
				keep := d.Min
				if keep < 1 {
					// A may/up-to copy (Min 0) still keeps its first inherited
					// target: the pre-election drain took option 0, so this
					// preserves that deterministic answer.
					keep = 1
				}
				for i := 0; i < keep && i < len(d.Options); i++ {
					choices = append(choices, d.Options[i].Index)
				}
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
					t.Fatalf("submit copy target: %v", err)
				}
				continue
			}
			if d.Kind == decision.KReplacement {
				// A CR 616.1 order competition over a non-commuting replacement
				// pair parked mid-drain (the AddCounter Plus/Twice and the
				// CreateToken multiplier/rewriter compositions): these drains were
				// written around the pre-choice engine, whose scan order applied
				// every candidate without asking, so answer option 0 -- the scan
				// order's own first candidate, exactly the board the assertions
				// were written against. A test that wants a different order
				// answers the ask itself before draining
				// (replacement_order_test.go).
				if len(d.Options) == 0 {
					t.Fatalf("replacement order ask with no option to take: %+v", d)
				}
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
					t.Fatalf("submit replacement order first option: %v", err)
				}
				continue
			}
			t.Fatalf("non-priority decision %+v while draining the stack", d)
		}
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
			t.Fatalf("submit: %v", err)
		}
	}
	return n
}

// playTappedLand drives newFixtureDeck's land through the play_land option.
// Shared by test 1 (direct assertions) and test 5 (replay fidelity) so both
// exercise the exact same path.
func playTappedLand(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg, id := newFixtureDeck(t, seed, tappedLandSrc)
	driveToStep(t, e, 1, 0, state.StepMain1)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("expected seat 0's priority after genesis, got %+v", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "play_land" && opt.Obj == id {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no play_land option for the fixture land: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit play_land: %v", err)
	}
	return e, cfg, id
}

// castAndResolveTappedCreature drives newFixtureDeck's creature through the
// cast option, funds its cost, and then drains priority -- bounded, per the
// brief's instruction not to let a real regression hang the test process --
// until it resolves off the stack. Shared by test 2 and test 5.
//
// Funding happens AFTER driving to main1, not before: setStep clears every
// player's mana pool on every step transition (CR 500.4, turn.go's own
// setStep), so mana funded back at upkeep (before Draw's and Main1's own
// transitions) would already be gone by the time main1's priority is asked.
// But a pending decision's Options is a snapshot legalActions computed once
// at ask() time (the same fact newFixtureDeck's doc comment calls out), so
// funding mana after driveToStep returns would land in the pool too late for
// the "cast" option to already be in the Options this test then needs to
// search -- e.priorityRound() is called again directly (the unexported
// method this package's own priority machinery uses, safe to call twice:
// grantPriority re-asks the same holder, it does not advance anything) so
// the decision this test actually inspects reflects the funded pool.
func castAndResolveTappedCreature(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg, id := newFixtureDeck(t, seed, tappedCreatureSrc)
	driveToStep(t, e, 1, 0, state.StepMain1)
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "B", Amount: 1})
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
		t.Fatalf("no cast option for the fixture creature: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit cast: %v", err)
	}
	if len(e.G.Stack) != 1 || e.G.Stack[0] != id {
		t.Fatalf("stack = %v, want [%d] right after casting", e.G.Stack, id)
	}

	const bound = 60
	passUntilStackEmpty(t, e, bound)
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v after %d bounded priority passes -- did not resolve "+
			"(possible infinite re-resolution, Task 29's defect)", e.G.Stack, bound)
	}
	return e, cfg, id
}

// TestUpdatedReplacementAppliesTheOriginalMove is brief test 1. At c19097f
// this fails: applyReplacements discards the land's hand->battlefield Move
// unconditionally, DBTap's own Tap primitive is a no-op for an object not
// already on the battlefield (effects/combatfx.go effTap), and the land is
// simply left in hand, untapped, forever.
func TestUpdatedReplacementAppliesTheOriginalMove(t *testing.T) {
	e, _, id := playTappedLand(t, 100)

	if got := e.G.Obj(id).Zone; got != state.ZBattlefield {
		t.Fatalf("zone = %s, want battlefield -- an Updated replacement must not discard the "+
			"original Move", got)
	}
	if !e.G.Obj(id).Tapped {
		t.Fatal("Tide Gate entered untapped -- ReplaceWith$ DBTap did not run, or ran before " +
			"the Move and found nothing on the battlefield to tap")
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want empty (a land never touches the stack)", e.G.Stack)
	}
	if n := countMoves(e.L.Events, id, state.ZBattlefield); n != 1 {
		t.Fatalf("logged %d MoveZone(->battlefield) for the land, want exactly 1: an Updated "+
			"replacement augments the original event once, it does not re-fire it", n)
	}
	if n := countKind(e.L.Events, events.Tap, id); n != 1 {
		t.Fatalf("logged %d Tap events for the land, want exactly 1 -- the ReplaceWith ran once", n)
	}
}

// TestUpdatedReplacementOnACreatureSpellResolvesOnce is brief test 2. At
// c19097f this hangs (bounded here to 60 priority passes rather than the
// engine's own 400000-intent budget): applyReplacements swallows the
// permanent's stack->battlefield Move every time, so resolveTop finds the
// same object on top again on the next pass and resolves it again, forever.
func TestUpdatedReplacementOnACreatureSpellResolvesOnce(t *testing.T) {
	e, _, id := castAndResolveTappedCreature(t, 101)

	if got := e.G.Obj(id).Zone; got != state.ZBattlefield {
		t.Fatalf("zone = %s, want battlefield", got)
	}
	if !e.G.Obj(id).Tapped {
		t.Fatal("Grave Walker resolved untapped -- ReplaceWith$ DBTap did not take effect")
	}
	if n := countKind(e.L.Events, events.Resolve, id); n != 1 {
		t.Fatalf("logged %d Resolve events, want exactly 1 -- no re-resolution", n)
	}
	if n := countMoves(e.L.Events, id, state.ZBattlefield); n != 1 {
		t.Fatalf("logged %d MoveZone(->battlefield) for the creature, want exactly 1", n)
	}
	resolveAt := -1
	for i, ev := range e.L.Events {
		if ev.Kind == events.Resolve && ev.Obj == id {
			resolveAt = i
			break
		}
	}
	if resolveAt < 0 {
		t.Fatal("no Resolve event found for the creature")
	}

	// The game must actually proceed, not sit resolving the same object:
	// some StepChange must follow the (sole) Resolve event. Driven further
	// AFTER the Tapped/Zone/count assertions above, deliberately: the next
	// turn's untap step would otherwise legitimately untap the creature
	// again before this got to look, which is correct game behaviour but
	// would make this test flaky about the wrong thing.
	passAll(t, e, 20)
	advanced := false
	for _, ev := range e.L.Events[resolveAt+1:] {
		if ev.Kind == events.StepChange {
			advanced = true
			break
		}
	}
	if !advanced {
		t.Fatal("no StepChange logged after the creature resolved -- the game did not proceed " +
			"to the next step")
	}
}

// TestReplacedReplacementStillDiscardsTheMove is brief test 3: the Task 22
// fixture shape (no ReplacementResult$ at all, i.e. absent == Replaced) on a
// permanent being destroyed must keep discarding the Move exactly as before
// -- unchanged behaviour, pinned so the Updated branch above cannot regress
// it.
func TestReplacedReplacementStillDiscardsTheMove(t *testing.T) {
	e := newSeats(t, 2)
	guardian := onBoard(t, e, 0, `Name:Relic Guardian
ManaCost:1 G
Types:Creature Wall
PT:0/4
R:Event$ Moved | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Card.Self | ReplaceWith$ RepLife
SVar:RepLife:DB$ GainLife | Defined$ You | LifeAmount$ 1
Oracle:x
`)
	before := e.G.Players[0].Life

	e.emit(events.Event{Kind: events.MoveZone, Obj: guardian, From: state.ZBattlefield, To: state.ZGraveyard})

	if got := e.G.Obj(guardian).Zone; got != state.ZBattlefield {
		t.Fatalf("zone = %s, want battlefield -- absent ReplacementResult$ must still fully "+
			"replace (discard) the Move", got)
	}
	if got := e.G.Players[0].Life - before; got != 1 {
		t.Fatalf("life gained = %d, want 1", got)
	}
}

// TestPermanentSpellWhoseEntryIsFullyReplacedDoesNotStickOnTheStack is brief
// test 4: an explicit ReplacementResult$ Replaced ETB replacement whose
// ReplaceWith$ never moves the object is exactly the totality hazard
// resolveTop's new guard exists for. At c19097f this hangs the same way
// test 2 does (bounded here for the same reason).
func TestPermanentSpellWhoseEntryIsFullyReplacedDoesNotStickOnTheStack(t *testing.T) {
	e, _, id := newFixtureDeck(t, 102, fullyReplacedEntrySrc)
	driveToStep(t, e, 1, 0, state.StepMain1)
	// Fund and re-ask, not fund-then-drive: see castAndResolveTappedCreature's
	// doc comment (setStep clears pools every step transition, and a pending
	// decision's Options is a one-time snapshot) for why.
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 2})
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
		t.Fatalf("no cast option for the fixture creature: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit cast: %v", err)
	}

	const bound = 60
	passAll(t, e, bound)
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v after %d bounded priority passes -- the fully-replaced entry stuck "+
			"on the stack instead of being swept off it (Task 29's resolveTop guard)", e.G.Stack, bound)
	}
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("zone = %s, want graveyard", got)
	}
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Obj == id {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no Note explaining the graveyard sweep was logged")
	}
	if e.Pending() == nil {
		t.Fatal("no further decision pending -- the engine did not reach the next decision")
	}
}

// TestTotalityGuardSurvivesABroadGraveyardReplacement is review finding I-1
// (Task 29 fix round 1): the totality guard's own escape hatch -- the Note
// and the stack->graveyard MoveZone in resolveTop -- used to go through
// e.emit with applyingReplacement left false, so a broad "any card that
// would go to a graveyard from anywhere" replacement already in play (the
// shipped corpus's Rest in Peace shape, graveyardBlockingReplacementSrc
// above) matched the GUARD'S OWN cleanup move too and discarded it, the
// exact same way the original ETB Move was discarded -- so the object never
// actually left the stack and the guard's Note fired again on every single
// re-resolution (measured pre-fix: passes=120, stack=[1], notes=60). The fix
// runs those two emits with applyingReplacement held true, since they are
// CR 608.2m engine housekeeping, not a further game event a card's own
// replacement should get to intercept.
func TestTotalityGuardSurvivesABroadGraveyardReplacement(t *testing.T) {
	e, _, id := newFixtureDeck(t, 104, fullyReplacedEntrySrc)
	onBoard(t, e, 1, graveyardBlockingReplacementSrc)
	driveToStep(t, e, 1, 0, state.StepMain1)
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 2})
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
		t.Fatalf("no cast option for the fixture creature: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit cast: %v", err)
	}

	const bound = 60
	n := passUntilStackEmpty(t, e, bound)
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v after %d bounded priority passes with a broad graveyard "+
			"replacement in play -- the totality guard's own escape hatch got blocked "+
			"the same way the original Move was (Task 29 review finding I-1)", e.G.Stack, bound)
	}
	if n != 2 {
		t.Fatalf("passes to drain the stack = %d, want exactly 2 -- the same as with no "+
			"blocking replacement in play (TestPermanentSpellWhoseEntryIsFullyReplacedDoesNotStickOnTheStack)", n)
	}
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("zone = %s, want graveyard -- the entry is fully replaced, so the only move "+
			"that reaches this object is the guard's own, which runs with applyingReplacement "+
			"held true and is therefore not itself redirected to exile by Bone Vault", got)
	}
	if n := countKind(e.L.Events, events.Note, id); n != 1 {
		t.Fatalf("logged %d Note events for the guard's escape hatch, want exactly 1 -- "+
			"more than one means the guard re-fired instead of terminating", n)
	}
	if e.Pending() == nil {
		t.Fatal("no further decision pending -- the engine did not reach the next decision")
	}
}

// TestUpdatedReplacementReplaysFaithfully is brief test 5: fold the logs of
// test 1 and test 2's scenarios alone into fresh state.Games (replayFromLog,
// this package's own log-alone reconstruction harness) and diff them
// against the live games. Separate seeds from tests 1/2 -- deliberately not
// sharing state with them -- but the exact same helper functions, so this
// exercises precisely what those tests exercised.
//
// Review finding M-4 (Task 29 fix round 1): this pins REPLAY fidelity of the
// Updated path's log, not the Updated semantics themselves -- it would still
// pass with applyReplacements' Updated branch reverted, as long as the
// resolveTop guard is present (the shared helpers' own bounded drive is what
// would then fail first, not this test's diff). Tests 1, 2 and 6 are what
// pin Updated's actual behaviour; this one only pins that whatever it does
// is faithfully reconstructible from the log alone.
func TestUpdatedReplacementReplaysFaithfully(t *testing.T) {
	t.Run("land", func(t *testing.T) {
		e, cfg, _ := playTappedLand(t, 200)
		fresh := replayFromLog(t, cfg, e.L.Events)
		if diff := diffGames(e.G, fresh); diff != "" {
			t.Fatalf("replay from the log alone diverged from the live game:\n%s", diff)
		}
	})
	t.Run("creature", func(t *testing.T) {
		e, cfg, _ := castAndResolveTappedCreature(t, 201)
		fresh := replayFromLog(t, cfg, e.L.Events)
		if diff := diffGames(e.G, fresh); diff != "" {
			t.Fatalf("replay from the log alone diverged from the live game:\n%s", diff)
		}
	})
}

// TestUpdatedReplacementStillFiresETBTriggers is brief test 6: a "whenever a
// permanent enters" trigger must still see an Updated entry -- proof that
// routing the original event through events.Emit + checkTriggers directly
// (rather than through e.emit, which would re-run replacement matching on
// it) still reaches checkTriggers, exactly as an ordinary unreplaced Move
// would.
func TestUpdatedReplacementStillFiresETBTriggers(t *testing.T) {
	e, _, landID := newFixtureDeck(t, 202, tappedLandSrc)
	onBoard(t, e, 0, watcherSrc)
	driveToStep(t, e, 1, 0, state.StepMain1)
	before := e.G.Players[0].Life

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
		t.Fatalf("stack = %v, want the Sentinel's ETB trigger queued -- the Updated replacement "+
			"must not hide the Move from checkTriggers", e.G.Stack)
	}
	e.resolveTop()
	if got := e.G.Players[0].Life - before; got != 1 {
		t.Fatalf("life gained = %d, want 1 from the ETB trigger firing once for the land's entry", got)
	}
}
