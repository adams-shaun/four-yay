package rules

// Task rpteachopt1: RepeatEach's RepeatOptionalForEachPlayer$ per-subject
// election. Each subject of the loop gets its own yes/no offer (the line's
// RepeatOptionalMessage$ is the prompt); a yes runs that subject's body once,
// a no skips only that subject and the loop continues with the next.
//
// Tempt with Vengeance is the real corpus carrier: its DBRepeat is
// `RepeatEach | RepeatPlayers$ Player.Opponent | RepeatOptionalForEachPlayer$
// True | RepeatOptionalMessage$ Do you want to create X 1/1 red Elemental
// creature tokens with haste?`, and its per-subject body creates X Elemental
// tokens owned by `Player.IsRemembered` -- exactly one per accepting opponent
// at X=1, so the election's effect and the loop order are observable without
// an accumulation primitive (the post-loop DBToken depends on an SVar
// accumulation outside this scope and is asserted only for absence here).

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const temptMessage = "Do you want to create X 1/1 red Elemental creature tokens with haste?"

// repeatEachCarrierFixture seats the real corpus Tempt with Vengeance in seat
// 0's deck over a 3-seat game (two opponents, so a mixed yes/no answer is
// possible), pins turn 1 to seat 0 and bridges the card into seat 0's hand at
// Main 1 with {1}{R} funded for X=1.
func repeatEachCarrierFixture(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	carrier := mustCorpusCard(t, reg, "Tempt with Vengeance")
	seat0 := append([]*cards.Card{carrier}, mountainDeck(t, 39)...)
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b", "c"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{seat0, mountainDeck(t, 40), mountainDeck(t, 40)}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	id := findByName(e, "Tempt with Vengeance", 0)
	if id == 0 {
		t.Fatal("Tempt with Vengeance not found for seat 0 -- corpus missing?")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: state.ZHand})
	e.pending = nil
	e.priorityRound()
	return e, cfg, id
}

// castTempt casts Tempt with Vengeance at X=1 and returns the first pending
// mid-resolution decision (the first opponent's election).
func castTempt(t *testing.T, e *Engine, id state.ObjID) *decision.Decision {
	t.Helper()
	addMana(t, e, 0, "RRCC") // {X}{R} with X=1 -> {1}{R}
	submitChoices(t, e, castOptionFor(t, e, id).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
		t.Fatalf("precondition failed: X decision after casting, got %+v", d)
	}
	submitChoices(t, e, 1) // X = 1
	return passUntilNonPriority(t, e, 20)
}

// requireElection asserts d is a RepeatEach per-subject election for the
// given player with the carrier's RepeatOptionalMessage$ as its prompt, and
// returns the option index for the wanted yes/no answer.
func requireElection(t *testing.T, d *decision.Decision, player state.PlayerID, accept bool) int {
	t.Helper()
	return requireElectionFor(t, d, player, accept, temptMessage)
}

// requireElectionFor is requireElection with an explicit prompt, for the
// inline suspending-body fixture whose message differs.
func requireElectionFor(t *testing.T, d *decision.Decision, player state.PlayerID, accept bool, prompt string) int {
	t.Helper()
	if d == nil {
		t.Fatalf("no decision pending, want the election for player %d", player)
	}
	if d.Kind != decision.KChoose || d.ResumeKind != "repeat_each_optional" {
		t.Fatalf("decision = kind %s resume %q, want a repeat_each_optional KChoose for player %d: %+v",
			d.Kind, d.ResumeKind, player, d)
	}
	if d.Player != player {
		t.Fatalf("election player = %d, want %d (each subject is offered its own)", d.Player, player)
	}
	if d.Prompt != prompt {
		t.Fatalf("election prompt = %q, want the script's RepeatOptionalMessage$ %q", d.Prompt, prompt)
	}
	want := "no"
	if accept {
		want = "yes"
	}
	for _, o := range d.Options {
		if o.Kind == want {
			return o.Index
		}
	}
	t.Fatalf("election has no %q option: %+v", want, d.Options)
	return -1
}

// countElementals counts the Elemental Tokens on the battlefield controlled
// by p (the per-subject body's observable output at X=1).
func countElementals(e *Engine, p state.PlayerID) int {
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Elemental Token" {
			n++
		}
	}
	return n
}

// TestRepeatEachOptionalForEachPlayerMixedAnswers is the core end-to-end
// leaf: the first opponent declines its own offer and creates nothing, the
// second accepts and creates one Elemental Token. The two elections are
// posed in loop order (opponent 1 then opponent 2), and the enclosing spell
// still resolves off the stack.
func TestRepeatEachOptionalForEachPlayerMixedAnswers(t *testing.T) {
	t.Parallel()
	e, cfg, id := repeatEachCarrierFixture(t, 6301)
	d := castTempt(t, e, id)

	// Precondition: no Elemental tokens exist before either election, so the
	// per-player counts below cannot be satisfied by a pre-existing board.
	if n := countElementals(e, 1) + countElementals(e, 2); n != 0 {
		t.Fatalf("precondition failed: %d Elemental Tokens before any answer, want 0", n)
	}

	// Opponent 1 declines: its body is skipped and its count stays 0.
	submitChoices(t, e, requireElection(t, d, 1, false))
	if n := countElementals(e, 1); n != 0 {
		t.Fatalf("after opponent 1 declined, %d Elementals on seat 1, want 0", n)
	}

	// Opponent 2 is asked next, in loop order, and accepts.
	d = e.Pending()
	submitChoices(t, e, requireElection(t, d, 2, true))
	if n := countElementals(e, 2); n != 1 {
		t.Fatalf("after opponent 2 accepted, %d Elementals on seat 2, want 1", n)
	}
	// The decline's skip must not have leaked a body draw for seat 1.
	if n := countElementals(e, 1); n != 0 {
		t.Fatalf("opponent 1's decline still created %d Elementals, want 0", n)
	}

	passUntilStackEmpty(t, e, 30)
	if o := e.G.Obj(id); o != nil && o.Zone == state.ZStack {
		t.Fatalf("Tempt with Vengeance is still on the stack after the loop")
	}
	// The accepting opponent's token survives the spell leaving the stack.
	if n := countElementals(e, 2); n != 1 {
		t.Fatalf("after the spell resolved, %d Elementals on seat 2, want 1", n)
	}
	replayCheck(t, e, cfg)
}

// TestRepeatEachOptionalForEachPlayerDeclinesEverySubject proves the "no"
// path is a real skip for every subject and not "run the body anyway": both
// opponents decline and neither creates an Elemental.
func TestRepeatEachOptionalForEachPlayerDeclinesEverySubject(t *testing.T) {
	t.Parallel()
	e, cfg, id := repeatEachCarrierFixture(t, 6302)
	d := castTempt(t, e, id)

	submitChoices(t, e, requireElection(t, d, 1, false))
	d = e.Pending()
	submitChoices(t, e, requireElection(t, d, 2, false))

	passUntilStackEmpty(t, e, 30)
	// Precondition: the loop's handler ran (the two elections above) rather
	// than the whole feature being unregistered; and no unimplemented-API
	// Note for RepeatEach was emitted.
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "RepeatEach selector unimplemented") {
			t.Fatalf("RepeatEach reported unimplemented: %q", ev.Text)
		}
	}
	if n := countElementals(e, 1) + countElementals(e, 2); n != 0 {
		t.Fatalf("both opponents declined but %d Elementals were created, want 0", n)
	}
	replayCheck(t, e, cfg)
}

// TestRepeatEachOptionalForEachPlayerAcceptsBoth keeps the yes path honest
// for every subject: both opponents accept, so each creates its own Elemental
// (and each election's effect is attributed to its own player).
func TestRepeatEachOptionalForEachPlayerAcceptsBoth(t *testing.T) {
	t.Parallel()
	e, cfg, id := repeatEachCarrierFixture(t, 6303)
	d := castTempt(t, e, id)

	submitChoices(t, e, requireElection(t, d, 1, true))
	d = e.Pending()
	submitChoices(t, e, requireElection(t, d, 2, true))

	passUntilStackEmpty(t, e, 30)
	if n := countElementals(e, 1); n != 1 {
		t.Fatalf("seat 1 accepted but has %d Elementals, want 1", n)
	}
	if n := countElementals(e, 2); n != 1 {
		t.Fatalf("seat 2 accepted but has %d Elementals, want 1", n)
	}
	replayCheck(t, e, cfg)
}

// suspendingRepeatEachScript is an inline RepeatEach carrier whose per-subject
// body itself asks: a Scry poses a mid-resolution KArrange for the resolving
// controller. It is the regression for the continuation transport -- an
// election answer must re-enter the loop at the offered subject's body, and a
// body that suspends must resume into the NEXT subject's election (never
// re-run the answered subject or skip the next).
func suspendingRepeatEachScript() string {
	return "Name:Suspending RepeatEach\nManaCost:R\nTypes:Sorcery\n" +
		"A:SP$ RepeatEach | RepeatSubAbility$ Body | RepeatPlayers$ Player.Opponent | " +
		"RepeatOptionalForEachPlayer$ True | RepeatOptionalMessage$ Do you want to scry?\n" +
		"SVar:Body:DB$ Scry | ScryNum$ 2\n" +
		"Oracle:x\n"
}

// repeatEachInlineFixture builds a 3-seat game around an inline fixture card
// in seat 0's hand at Main 1 (two opponents, so a mixed answer and a
// next-subject election are both observable).
func repeatEachInlineFixture(t *testing.T, seed uint64, src string) (*Engine, Config, state.ObjID) {
	t.Helper()
	fixture := card(t, src)
	name := fixture.Faces[0].Name
	build := func(s uint64) Config {
		return Config{Seed: s, Names: []string{"a", "b", "c"}, Tokens: map[string]*cards.Card{},
			Decks: [][]*cards.Card{
				append([]*cards.Card{fixture}, mountainDeck(t, 39)...),
				mountainDeck(t, 40),
				mountainDeck(t, 40),
			}}
	}
	cfg := seatZeroStart(build(seed))
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	id := findByName(e, name, 0)
	if id == 0 {
		t.Fatalf("fixture %q not found for seat 0", name)
	}
	if e.G.Obj(id).Zone != state.ZHand {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: state.ZHand})
	}
	e.pending = nil
	e.priorityRound()
	return e, cfg, id
}

// TestRepeatEachOptionalForEachPlayerSuspendedBody is the continuation-transport
// regression: seat 1 accepts and its per-subject body suspends on its own Scry
// KArrange; answering that arrange must resume into seat 2's election (with
// seat 2 free to decline, running no body of its own).
func TestRepeatEachOptionalForEachPlayerSuspendedBody(t *testing.T) {
	t.Parallel()
	e, cfg, id := repeatEachInlineFixture(t, 6310, suspendingRepeatEachScript())
	addMana(t, e, 0, "R")
	castFirst(t, e, "cast")

	d := passPriorityUntilNonPriority(t, e)
	submitChoices(t, e, requireElectionFor(t, d, 1, true, "Do you want to scry?"))

	// The accepted subject's body suspended on its own Scry arrange. This is
	// the precondition the rest of the test depends on: the body really did
	// ask (a silent body would make the transport assertion vacuous).
	arrange := e.Pending()
	if arrange == nil || arrange.Kind != decision.KArrange {
		t.Fatalf("after seat 1 accepted, want its Scry KArrange, got %+v", arrange)
	}
	submitArrange(t, e, arrange, nil)

	// The body resumed and completed: the loop must now pose seat 2's
	// election -- not re-offer seat 1's nor run seat 1's body again.
	d = e.Pending()
	submitChoices(t, e, requireElectionFor(t, d, 2, false, "Do you want to scry?"))

	passUntilStackEmpty(t, e, 30)
	if o := e.G.Obj(id); o != nil && o.Zone == state.ZStack {
		t.Fatalf("the spell is still on the stack after the loop")
	}
	replayCheck(t, e, cfg)
}
