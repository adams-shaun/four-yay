package seat

import (
	"context"
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// allSteps is every state.Step, in engine order, used only to print the
// per-step activation histogram below in a fixed, deterministic order --
// never to decide anything about a live game.
var allSteps = []state.Step{
	state.StepUntap, state.StepUpkeep, state.StepDraw, state.StepMain1,
	state.StepBeginCombat, state.StepDeclareAttackers, state.StepDeclareBlockers,
	state.StepCombatDamage, state.StepEndCombat, state.StepMain2, state.StepEnd, state.StepCleanup,
}

// TestBotOnlyActivatesInAMainPhase is Ruling T25-g's regression test against
// a real, whole game: fix round 1's own synthetic "priority (combat)" case
// (bot_test.go) carried a play_land option, which the switch's un-gated
// play_land/cast loop matches regardless of phase -- a shape the live
// engine never actually offers outside a main phase (play_land requires
// sorcery speed), so that test never reached the clamp-top-up fallback path
// that reintroduced I-1(b) in real games. This drives one whole
// SampleDecks(t,4) game with the real seat.Bot against the real engine
// (seat is allowed to import rules for its own tests: rules does not import
// seat, so there is no cycle -- the same pattern view_test.go already uses)
// and tallies every "activate" choice by the step it was made in, reading
// e.G.Step before each Submit.
func TestBotOnlyActivatesInAMainPhase(t *testing.T) {
	names, decks := testutil.SampleDecks(t, 4)
	e := rules.New(rules.Config{Seed: 0, Names: names, Decks: decks})
	e.Advance()
	b := NewBot(0)

	hist := map[state.Step]int{}
	n := 0
	for !e.G.Over && e.Pending() != nil && n < 200000 {
		d := e.Pending()
		step := e.G.Step
		v := view.Project(e.G, e, d.Player, d)
		in, err := b.Decide(context.Background(), v, *d)
		if err != nil {
			t.Fatalf("intent %d: Decide returned an error: %v", n, err)
		}
		if d.Kind == decision.KPriority {
			if chosen := d.Chosen(in); len(chosen) == 1 && chosen[0].Kind == "activate" {
				hist[step]++
			}
		}
		if err := e.Submit(in); err != nil {
			t.Fatalf("intent %d: %v", n, err)
		}
		n++
	}
	if !e.G.Over {
		t.Fatalf("game did not terminate after %d intents (turn %d)", n, e.G.Turn)
	}

	t.Log("seat.Bot activate choices by step:")
	for _, s := range allSteps {
		t.Logf("  %-18s %d", s, hist[s])
	}
	for _, s := range allSteps {
		if s == state.StepMain1 || s == state.StepMain2 {
			continue
		}
		if hist[s] > 0 {
			t.Errorf("seat.Bot activated %d times during %s, outside any main phase", hist[s], s)
		}
	}
	if hist[state.StepMain1]+hist[state.StepMain2] == 0 {
		t.Error("seat.Bot never activated during a main phase across the whole game")
	}
}

// TestBotAdaptersAgreePerStep pins Ruling F7's "keep the two in step" as a
// measured property, per step: for every step the engine can be in, the two
// adapter halves must build the same botpolicy.Board from the same facts --
// the view-shaped half (BoardFromView, fed the Phase string view.PhaseOf
// projects for that step, which is exactly what a real seat receives) and
// the game-shaped half (the rules test host's g.Step.IsMain()). A bare
// step has no game behind it, so the only fact either half can report is
// IsMain; the creature/life census agreement is TestBotAdaptersAgreeOverWholeGame's
// territory instead. The policy must then answer the same; this uses the
// priority decision where IsMain changes the choice (in a main phase the
// policy taps mana, outside it the same options are passed on), with the
// two sides' rngs seeded identically. This is the test that dies on
// mutation M1: invert BoardFromView's IsMain and the halves disagree
// precisely on StepMain1/StepMain2.
func TestBotAdaptersAgreePerStep(t *testing.T) {
	prio := decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "activate", Obj: 100},
			{Index: 1, Kind: "pass"},
		}}
	for _, tc := range []struct {
		name string
		new  func(uint64) *Bot
	}{
		{name: "bot", new: NewBot},
		{name: "lethal-pressure", new: NewLethalPressureBot},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, s := range allSteps {
				boardView := BoardFromView(view.View{Phase: view.PhaseOf(s), Step: s.String()})
				boardGame := botpolicy.Board{IsMain: s.IsMain(), Step: s} // the rules host's expression
				if boardView.IsMain != boardGame.IsMain {
					t.Errorf("step %s: view-shaped IsMain %v, game-shaped IsMain %v", s, boardView.IsMain, boardGame.IsMain)
				}
				// The step fact itself (the cast scorer's timing features): the
				// view half parses the projected View.Step string, so it must
				// name exactly the engine step the game half carries.
				if boardView.Step != boardGame.Step {
					t.Errorf("step %s: view-shaped Step %v, game-shaped Step %v", s, boardView.Step, boardGame.Step)
				}
				inView, err := tc.new(1).Decide(context.Background(), view.View{Phase: view.PhaseOf(s), Step: s.String()}, prio)
				if err != nil {
					t.Fatalf("step %s: view-shaped Decide: %v", s, err)
				}
				inGame, err := tc.new(1).DecideBoard(context.Background(), boardGame, prio)
				if err != nil {
					t.Fatalf("step %s: game-shaped DecideBoard: %v", s, err)
				}
				if !slices.Equal(inView.Choices, inGame.Choices) {
					t.Errorf("step %s: view-shaped choices %v, game-shaped choices %v", s, inView.Choices, inGame.Choices)
				}
			}
		})
	}
}

// TestBotAdaptersAgreeOverCommanderGame is TestBotAdaptersAgreeOverWholeGame's
// commander-format twin: the two adapter halves must derive IDENTICAL
// commander facts from the same game — the Board.Commanders map (roster,
// CR 903.8 cast counts, InCommandZone, and the CR 903.10 per-commander
// damage clock) is pinned with reflect.DeepEqual on every decision, the
// way the casting Card census is pinned over the Constructed game above,
// because those facts drive the commander cast rule and the combat clock.
// The game is a two-seat Commander-format sample: each seat's first
// vanilla creature is its commander, so the cast, the tax, and the clock
// all genuinely happen over a whole game. The View additions this task
// made (the projected command zone, roster + cast counts, and CmdDamage)
// are exactly the wire facts this test's view-shaped half reads.
func TestBotAdaptersAgreeOverCommanderGame(t *testing.T) {
	for _, tc := range []struct {
		name string
		new  func(uint64) *Bot
	}{
		{name: "bot", new: NewBot},
		{name: "lethal-pressure", new: NewLethalPressureBot},
	} {
		t.Run(tc.name, func(t *testing.T) { agreeOverCommanderGame(t, tc.new) })
	}
}

func agreeOverCommanderGame(t testing.TB, newBot func(uint64) *Bot) {
	t.Helper()
	names, decks := testutil.SampleDecks(t, 2)
	cmds := [][]int{{17}, {17}} // each seat's first vanilla creature leaves the deck for the command zone
	cfg := rules.Config{Seed: 0, Names: names, Decks: decks,
		Commanders: cmds, StartingLife: 20, Format: rules.FormatCommander}
	eView := rules.New(cfg)
	eGame := rules.New(cfg)
	eView.Advance()
	eGame.Advance()
	botView := newBot(7)
	botGame := newBot(7)
	cmdPinned := 0
	poolN := 0
	stackN := 0
	stepN := 0
	n := 0
	for !eView.G.Over && !eGame.G.Over && eView.Pending() != nil && eGame.Pending() != nil && n < 200000 {
		d := eView.Pending()
		// One projection per decision, shared by both consumers: the same
		// projected View a real client would receive feeds Decide AND the
		// view-shaped Board adapter — projecting twice is what the original
		// whole-game test does, but a commander game's projections carry real
		// roster/clock content, and the suite's allocation is budgeted.
		v := view.Project(eView.G, eView, d.Player, d)
		inView, err := botView.Decide(context.Background(), v, *d)
		if err != nil {
			t.Fatalf("intent %d: view-shaped Decide: %v", n, err)
		}
		boardGame := botpolicy.BoardFromGame(eGame.G, eGame, d.Player)
		boardView := BoardFromView(v)
		if !reflect.DeepEqual(boardView.OwnDeck, boardGame.OwnDeck) {
			t.Fatalf("intent %d: own deck manifest diverged: view=%#v game=%#v", n, boardView.OwnDeck, boardGame.OwnDeck)
		}
		if !maps.Equal(boardView.Cards, boardGame.Cards) {
			t.Fatalf("intent %d: casting Card census diverged (step %s)", n, eGame.G.Step)
		}
		// The exact step fact, the commander twin of the whole-game pin.
		if boardView.Step != boardGame.Step {
			t.Fatalf("intent %d: step diverged: view %v vs game %v", n, boardView.Step, boardGame.Step)
		}
		if !boardGame.Step.IsMain() {
			stepN++
		}
		// The commander facts: identical maps on both halves, pinned on
		// every decision (not only when a ranking flips a choice).
		if !reflect.DeepEqual(boardView.Commanders, boardGame.Commanders) {
			t.Fatalf("intent %d: commander facts diverged:\nview: %+v\ngame: %+v (step %s)", n, boardView.Commanders, boardGame.Commanders, eGame.G.Step)
		}
		// op6 (the tap gate's pool), the commander twin of the whole-game
		// test's own pool agreement.
		// The RepeatOptional$ stop gate's zone counts (the deciding seat's
		// own library and hand sizes) must agree too.
		if boardView.LibrarySize != boardGame.LibrarySize || boardView.HandSize != boardGame.HandSize {
			t.Fatalf("intent %d: zone counts diverged: view lib %d hand %d vs game lib %d hand %d", n, boardView.LibrarySize, boardView.HandSize, boardGame.LibrarySize, boardGame.HandSize)
		}
		if boardView.Pool != boardGame.Pool {
			t.Fatalf("intent %d: pool diverged: view %v vs game %v (step %s)", n, boardView.Pool, boardGame.Pool, eGame.G.Step)
		}
		if boardView.PoolRestricted != boardGame.PoolRestricted {
			t.Fatalf("intent %d: restricted pool diverged: view %v vs game %v (step %s)", n, boardView.PoolRestricted, boardGame.PoolRestricted, eGame.G.Step)
		}
		if boardView.Pool.Total() > 0 {
			poolN++
		}
		// The public stack census (C8's facts, task botcounter1), the
		// commander twin of the whole-game test's own agreement.
		if !slices.Equal(boardView.Stack, boardGame.Stack) {
			t.Fatalf("intent %d: stack census diverged: view %v vs game %v (step %s)", n, boardView.Stack, boardGame.Stack, eGame.G.Step)
		}
		if len(boardGame.Stack) > 0 {
			stackN++
		}
		cmdPinned++
		inGame, err := botGame.DecideBoard(context.Background(), boardGame, *eGame.Pending())
		if err != nil {
			t.Fatalf("intent %d: game-shaped DecideBoard: %v", n, err)
		}
		if inView.Seq != inGame.Seq || inView.Player != inGame.Player || !slices.Equal(inView.Choices, inGame.Choices) {
			t.Fatalf("intent %d: adapters diverged: view %+v vs game %+v (step %s)", n, inView, inGame, eGame.G.Step)
		}
		if err := eView.Submit(inView); err != nil {
			t.Fatalf("intent %d: view-shaped Submit: %v", n, err)
		}
		if err := eGame.Submit(inGame); err != nil {
			t.Fatalf("intent %d: game-shaped Submit: %v", n, err)
		}
		n++
	}
	if !eView.G.Over || !eGame.G.Over {
		t.Fatalf("game did not terminate after %d intents (view over=%v, game over=%v)", n, eView.G.Over, eGame.G.Over)
	}
	if cmdPinned == 0 {
		t.Fatal("no decision ever carried a commander fact to pin — the game never projected one")
	}
	if poolN == 0 {
		t.Fatal("no decision ever carried a non-zero Pool -- the tap gate's pool fact was never exercised over the commander game")
	}
	if stackN == 0 {
		t.Fatal("no decision ever carried a non-empty stack census -- the stack fact was never exercised over the commander game")
	}
	if stepN == 0 {
		t.Fatal("no decision ever landed outside a main phase -- the step fact was never exercised over the commander game")
	}
	if h1, h2 := eView.L.Head(), eGame.L.Head(); h1 != h2 {
		t.Fatalf("chains diverged: view %s, game %s", h1, h2)
	}
}

// TestBotAdaptersAgreeOverWholeGame drives two byte-identical acceptance
// games from the same (engine seed, bot seed) -- one through this package's
// view-shaped adapter (projecting a View every decision, exactly like a real
// client), one through the rules host's game-shaped adapter (e.G.Step.IsMain
// on the engine's own step). Every intent the two produce must be identical
// -- same Seq/Player/Choices -- so the two chains reach the same head. This
// is the copy-paste mirror's guarantee (Ruling F7) turned into a measured
// property of the two real adapter halves over a whole game.
func TestBotAdaptersAgreeOverWholeGame(t *testing.T) {
	for _, tc := range []struct {
		name string
		new  func(uint64) *Bot
	}{
		{name: "bot", new: NewBot},
		{name: "lethal-pressure", new: NewLethalPressureBot},
		// cast-profile rides the same adapter pair: with the embedded default
		// profile its decisions must agree across both halves exactly like
		// the other policies' do.
		{name: "cast-profile", new: func(seed uint64) *Bot {
			b, err := NewCastProfileBot(seed)
			if err != nil {
				t.Fatalf("NewCastProfileBot: %v", err)
			}
			return b
		}},
	} {
		t.Run(tc.name, func(t *testing.T) { agreeOverWholeGame(t, tc.new) })
	}
}

func agreeOverWholeGame(t testing.TB, newBot func(uint64) *Bot) {
	t.Helper()
	// Scenario 1: the historical SampleDecks(4) whole game -- every decision
	// of a full four-seat Constructed game. Its decks carry no Aura/Equipment,
	// so (op3) a second scenario below is what actually exercises Card.
	// AttachedTo non-zero; this one pins the general two-adapter agreement
	// over the whole game, including an AttachedTo that stays at 0 on both
	// halves (the agreement must still hold when the fact is "unattached").
	names, decks := testutil.SampleDecks(t, 4)
	agreeOverGame(t, names, decks, 0, false, false, newBot)

	// Scenario 1b (botcounter1, the counter identity): a two-seat mirror
	// whose seat-1 list carries a real SP$ Counter (authored inline, no
	// corpus fixture) opposite seat 0's vanilla creatures, so the game
	// genuinely produces foreign stack spells the seat-1 bot counters —
	// the C8 rule's preserving half — while Card.Counter and the stack
	// census go non-zero on BOTH adapter halves and are pinned equal on
	// every intent.
	namesC, decksC := counterMirrorDecks(t)
	agreeOverGame(t, namesC, decksC, 5, false, true, newBot)

	// Scenario 2 (op3, the attachment fact): a controlled two-seat game
	// whose seat-0 deck includes two free Equip:0 Equipments (bareGreavesSrc,
	// the Lightning Greaves shape) beside lands and creatures, so across the
	// game the bot casts and equips them onto its own creatures and both
	// adapter halves genuinely read a non-zero Card.AttachedTo. The plain
	// SampleDecks above cannot do this (no Aura/Equipment in the lists), so
	// without this scenario the whole-game agreement on AttachedTo would be
	// vacuously all-zero and the gate "never fills AttachedTo" would pass for
	// the wrong reason. agreeOverGame fails on any divergence between the two
	// halves AND asserts the field actually went non-zero (wantAttached).
	names2, decks2 := equippingDeck(t)
	agreeOverGame(t, names2, decks2, 3, true, false, newBot)
}

// TestOp3Seed13GreavesLoopTerminates is the measured op3 regression test: a
// whole two-seat game of foundations-keen-engineering (seat 0) vs
// mono-red-goblins (seat 1) at engine seed 13 must TERMINATE. Before the
// fix-round-2 A1 (botpolicy/ability.go) this exact game froze: from turn 25,
// a free Lightning Greaves Equip re-attached the equipment onto the creature
// it already carried at every main-phase priority — byte-stable, to the
// bench's -max-intents cap. The fix reads the attachment state (AttachedTo)
// instead of predicting the target, so the re-attach is declined and the
// game ends (measured: over=true turn=35). Names, deck order, bot seeds and
// the intent cap reproduce the cmd/botbench pair exactly: seat k's bot is
// seeded seed^(k+1) (cmd/botbench's playMatch wiring),
// foundations-keen-engineering is dealt as a constructed pile (the bench's
// default -format constructed), and 20000 is the bench's default
// -max-intents — the cap the broken build reached before recording the
// stall.
func TestOp3Seed13GreavesLoopTerminates(t *testing.T) {
	reg := testutil.CorpusRegistry(t) // skips without a corpus, like the repo-deck tests in rules/
	names := []string{"foundations-keen-engineering", "mono-red-goblins"}
	e := rules.New(rules.Config{Seed: 13, Names: names,
		Decks:  [][]*cards.Card{testutil.RepoDeck(t, reg, names[0]), testutil.RepoDeck(t, reg, names[1])},
		Tokens: reg.Tokens})
	e.Advance()
	bots := []*Bot{NewBot(13 ^ 1), NewBot(13 ^ 2)}
	n := 0
	for !e.G.Over && e.Pending() != nil && n < 20000 {
		d := e.Pending()
		in, err := bots[d.Player].Decide(context.Background(), view.Project(e.G, e, d.Player, d), *d)
		if err != nil {
			t.Fatalf("intent %d: Decide: %v", n, err)
		}
		if err := e.Submit(in); err != nil {
			t.Fatalf("intent %d: Submit: %v", n, err)
		}
		n++
	}
	if !e.G.Over {
		t.Fatalf("seed 13 did not terminate: %d intents at turn %d — the Lightning Greaves re-attach loop is back", n, e.G.Turn)
	}
	t.Logf("seed 13: over=true turn=%d intents=%d", e.G.Turn, n)
}

// equippingDeck returns the (names, decks) of scenario 2, whose seat-0 list
// forces an Equipment attach across a whole game so the attachment fact is
// genuinely exercised (see TestBotAdaptersAgreeOverWholeGame). Seat 1 is
// pure lands and never threatening, so seat 0's equip actually resolves
// instead of the game ending first.
func equippingDeck(t testing.TB) ([]string, [][]*cards.Card) {
	greaves := parseTestCard(t, bareGreavesSrc)
	bear := parseTestCard(t, "Name:Bear\nManaCost:1\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	island := parseTestCard(t, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	mountain := parseTestCard(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	var d0, d1 []*cards.Card
	for i := 0; i < 30; i++ {
		d0 = append(d0, island)
		d1 = append(d1, mountain)
	}
	for i := 0; i < 4; i++ {
		d0 = append(d0, bear)
	}
	d0 = append(d0, greaves, greaves)
	return []string{"a", "b"}, [][]*cards.Card{d0, d1}
}

// agreeOverGame drives one whole game through BOTH adapter halves -- the
// view-shaped half (seat/Bot.Decide off a projected View, exactly like a real
// client) and the game-shaped half (botpolicy.BoardFromGame + Decide, like
// the rules test host) -- and demands they agree on every intent, every
// Card in the casting census (which since op3 includes AttachedTo), and the
// final chain head. wantAttached additionally requires that at least one
// decision carried a non-zero Card.AttachedTo, so a deck-set that never
// attaches the two halves can not pass the AttachedTo agreement vacuously;
// wantCounter does the same for the botcounter1 facts — a Card.Counter true
// and a foreign stack spell for the decider — so a deck-set without a
// counter cannot pass the C8-fact agreement vacuously.
func agreeOverGame(t testing.TB, names []string, decks [][]*cards.Card, seed uint64, wantAttached, wantCounter bool, newBot func(uint64) *Bot) {
	t.Helper()
	stackN, counterN, foreignSpellN := 0, 0, 0
	stepN := 0
	cfg := rules.Config{Seed: seed, Names: names, Decks: decks}
	eView := rules.New(cfg)
	eGame := rules.New(cfg)
	eView.Advance()
	eGame.Advance()
	botView := newBot(7)
	botGame := newBot(7)
	attachedN := 0
	poolN := 0
	sawUnequalLife := false
	n := 0
	for !eView.G.Over && !eGame.G.Over && eView.Pending() != nil && eGame.Pending() != nil && n < 200000 {
		d := eView.Pending()
		inView, err := botView.Decide(context.Background(), view.Project(eView.G, eView, d.Player, d), *d)
		if err != nil {
			t.Fatalf("intent %d: view-shaped Decide: %v", n, err)
		}
		// The game-shaped half builds the same Board straight off the engine
		// (botpolicy.BoardFromGame: IsMain from eGame.G.Step, the creature
		// census and life from eGame.G) -- the exact expression the rules
		// test host's answer uses, so a divergence between the two adapter
		// halves fails here on the intent where it first appears.
		boardGame := botpolicy.BoardFromGame(eGame.G, eGame, d.Player)
		// The casting Card census (B4): the view-shaped half fills the same
		// map for the same deciding player off the projected Hand/Graveyard/
		// Battlefield CardViews. It is the new field this task's widening adds
		// to Board, so it is pinned here explicitly -- not only through the
		// intents below (which would catch a divergence only when a ranking
		// actually flips a choice): a Cards map one half fills and the other
		// leaves zero is a bot that casts differently depending on who asked.
		// Since op3 the comparison also covers Card.AttachedTo (the A1
		// attachment fact): BoardFromView fills it off CardView.AttachedTo,
		// BoardFromGame off state.Object.AttachedTo, so a divergence here is
		// the two adapters reading different attachment facts.
		boardView := BoardFromView(view.Project(eView.G, eView, d.Player, d))
		if boardGame.OwnDeck == nil || boardView.OwnDeck == nil || !reflect.DeepEqual(boardView.OwnDeck, boardGame.OwnDeck) {
			t.Fatalf("intent %d: own deck manifest diverged: view=%#v game=%#v", n, boardView.OwnDeck, boardGame.OwnDeck)
		}
		// AR6 also reads defender life to break equal combat tiers. Compare
		// the fact itself, not just choices that may never need a tiebreak.
		if !maps.Equal(boardView.Life, boardGame.Life) {
			t.Fatalf("intent %d: life census diverged: view %v vs game %v", n, boardView.Life, boardGame.Life)
		}
		for _, p := range eView.G.Players {
			if boardView.Life[p.ID] != boardView.Life[d.Player] {
				sawUnequalLife = true
			}
		}
		if !maps.Equal(boardView.Cards, boardGame.Cards) {
			t.Fatalf("intent %d: casting Card census diverged: view %v vs game %v (step %s)", n, boardView.Cards, boardGame.Cards, eGame.G.Step)
		}
		// The exact step fact (the cast scorer's timing features): the view
		// half parses the projected View.Step string, the game half reads
		// eGame.G.Step directly, so a divergence here is the two adapters
		// naming different windows. stepN counts the steps a whole game runs
		// OUTSIDE a main phase, so the pin is not vacuous on a game whose
		// decisions all land in main1/main2.
		if boardView.Step != boardGame.Step {
			t.Fatalf("intent %d: step diverged: view %v vs game %v", n, boardView.Step, boardGame.Step)
		}
		if !boardGame.Step.IsMain() {
			stepN++
		}
		// op6 (the tap gate's pool): the deciding seat's own mana pool must
		// be the same numbers on both halves -- the projected poolView map
		// and state.Game's Pool field -- and it must actually go non-zero
		// over the game (poolN), so the T1 need gate is exercised with a
		// real pool and the agreement is not vacuous.
		if boardView.Pool != boardGame.Pool {
			t.Fatalf("intent %d: pool diverged: view %v vs game %v (step %s)", n, boardView.Pool, boardGame.Pool, eGame.G.Step)
		}
		if boardView.PoolRestricted != boardGame.PoolRestricted {
			t.Fatalf("intent %d: restricted pool diverged: view %v vs game %v (step %s)", n, boardView.PoolRestricted, boardGame.PoolRestricted, eGame.G.Step)
		}
		if boardView.Pool.Total() > 0 {
			poolN++
		}
		// The public stack census (C8's facts, task botcounter1): bottom-to-top
		// order on both halves, pinned on every intent — and counted when
		// non-empty so the pin is not vacuous (a whole game always resolves
		// something through the stack).
		if !slices.Equal(boardView.Stack, boardGame.Stack) {
			t.Fatalf("intent %d: stack census diverged: view %v vs game %v (step %s)", n, boardView.Stack, boardGame.Stack, eGame.G.Step)
		}
		if len(boardGame.Stack) > 0 {
			stackN++
		}
		// The counter identity (C8): a card whose SP$ ability is a Counter
		// reads true on both halves — the view half off CardView.SpellAPI,
		// the game half off cards.Face.SpellAbility().API — and a foreign
		// stack spell is what makes the census worth reading at all.
		for _, c := range boardGame.Cards {
			if c.Counter {
				counterN++
				break
			}
		}
		for _, s := range boardGame.Stack {
			if s.IsSpell && s.Controller != d.Player {
				foreignSpellN++
				break
			}
		}
		// op3 (A1, the attachment fact): an AttachedTo divergence is the
		// adapters reading different facts, and the non-zero count besides
		// proves the field actually BOTHERS to run rather than staying at 0
		// on both halves by luck (the wantAttached assertion below).
		for id, cv := range boardView.Cards {
			if cv.AttachedTo != 0 {
				if boardGame.Cards[id].AttachedTo != cv.AttachedTo {
					t.Fatalf("intent %d: AttachedTo diverged for %d: view %d, game %d (step %s)", n, id, cv.AttachedTo, boardGame.Cards[id].AttachedTo, eGame.G.Step)
				}
				attachedN++
			}
		}
		inGame, err := botGame.DecideBoard(context.Background(), boardGame, *eGame.Pending())
		if err != nil {
			t.Fatalf("intent %d: game-shaped DecideBoard: %v", n, err)
		}
		if inView.Seq != inGame.Seq || inView.Player != inGame.Player || !slices.Equal(inView.Choices, inGame.Choices) {
			t.Fatalf("intent %d: adapters diverged: view %+v vs game %+v (step %s)", n, inView, inGame, eGame.G.Step)
		}
		if err := eView.Submit(inView); err != nil {
			t.Fatalf("intent %d: view-shaped Submit: %v", n, err)
		}
		if err := eGame.Submit(inGame); err != nil {
			t.Fatalf("intent %d: game-shaped Submit: %v", n, err)
		}
		n++
	}
	if !eView.G.Over || !eGame.G.Over {
		t.Fatalf("game did not terminate after %d intents (view over=%v, game over=%v)", n, eView.G.Over, eGame.G.Over)
	}
	if !sawUnequalLife {
		t.Fatal("no unequal life totals observed -- the life comparison was vacuous")
	}
	if wantAttached && attachedN == 0 {
		t.Fatal("no decision ever carried a non-zero AttachedTo on Card -- the attachment fact was never exercised over the whole game")
	}
	if poolN == 0 {
		t.Fatal("no decision ever carried a non-zero Pool -- the tap gate's pool fact was never exercised over the whole game")
	}
	if stackN == 0 {
		t.Fatal("no decision ever carried a non-empty stack census -- the stack fact was never exercised over the whole game")
	}
	if wantCounter && counterN == 0 {
		t.Fatal("no decision ever carried a Counter card fact -- the counter identity was never exercised over the whole game")
	}
	if wantCounter && foreignSpellN == 0 {
		t.Fatal("no decision ever saw a foreign spell on the stack -- the C8 census was never exercised over the whole game")
	}
	if stepN == 0 {
		t.Fatal("no decision ever landed outside a main phase -- the step fact was never exercised over the whole game")
	}
	if h1, h2 := eView.L.Head(), eGame.L.Head(); h1 != h2 {
		t.Fatalf("chains diverged: view %s, game %s", h1, h2)
	}
}

// counterSpellSrc is a plain unconditional counter (the Counterspell
// shape: SP$ Counter at a spell, no TargetMin/Max/Choices), authored inline
// so the counter-identity parity and the C8 mirror run without a corpus
// fixture. Its ManaCost makes it genuinely castable at instant speed.
const counterSpellSrc = `Name:Test Counterspell
ManaCost:U U
Types:Instant
A:SP$ Counter | TargetType$ Spell | ValidTgts$ Card | SpellDescription:Counter target spell.
Oracle:Counter target spell.
`

// counterMirrorDecks returns the (names, decks) of scenario 1b: seat 0
// plays islands and a vanilla creature (a foreign spell seat 1 can
// counter), seat 1 plays islands and counterspells (Card.Counter true on
// both adapter halves). Short lists keep the mirror's games short (decking
// ends them) so the seat package's test-time budget still covers it.
func counterMirrorDecks(t testing.TB) ([]string, [][]*cards.Card) {
	counter := parseTestCard(t, counterSpellSrc)
	bear := parseTestCard(t, "Name:Bear\nManaCost:1\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	island := parseTestCard(t, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	var d0, d1 []*cards.Card
	for i := 0; i < 19; i++ {
		d0 = append(d0, island)
		d1 = append(d1, island)
	}
	for i := 0; i < 5; i++ {
		d0 = append(d0, bear)
		d1 = append(d1, counter)
	}
	return []string{"a", "b"}, [][]*cards.Card{d0, d1}
}

// bareGreavesSrc is a free Equip:0 Equipment, the op3 hang's own shape
// (Lightning Greaves), authored inline so the adapter-agreement test drives
// a real non-zero AttachedTo without a corpus fixture.
const bareGreavesSrc = `Name:Bare Greaves
ManaCost:0
Types:Artifact Equipment
K:Equip:0
Oracle:x
`

// parseTestCard is the seat package's re-authoring of testutil.parseCard
// (which is unexported there): parse the inline card source, link its
// SVar/trigger chain, and apply the intrinsics the corpus assumes (basic
// land mana etc.), so a test can hand the engine a bespoke Equipment.
func parseTestCard(t testing.TB, src string) *cards.Card {
	t.Helper()
	c, diags := cards.ParseBytes("integration_test.txt", []byte(src))
	if len(diags) != 0 {
		t.Fatalf("parseTestCard: %v", diags)
	}
	c.Link()
	for _, f := range c.Faces {
		f.ApplyIntrinsics()
	}
	return c
}
