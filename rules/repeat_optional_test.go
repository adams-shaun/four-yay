package rules

// Task repeatopt1: the RepeatOptional$ do/while election. Ad Nauseam is the
// named corpus carrier (its brief source), and Forbidden Ritual is the
// corpus carrier whose BODY itself asks (a sacrifice resolving into a
// GenericChoice modal ask), which is the shape a resumed body must return
// to the election for -- never fall straight into the next iteration.
//
// The two carriers pin the two distinct resume states:
//   - Ad Nauseam: the body does not ask, so the loop reaches the election
//     directly. Guards the base fix (the election is posed at all, and "no"
//     stops after exactly one iteration).
//   - Forbidden Ritual: the body SUSPENDS on its own ask. When that body
//     completes after the answer, the next decision must be the repeat
//     election for the NEXT iteration, not that iteration's body. This is
//     the state RepeatOptionalContinuation.AskElection names.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// countSacrifices counts the real sacrifice moves in the log (CR 701.21a),
// the observable one RepeatOptional iteration of Forbidden Ritual adds.
func countSacrifices(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if events.IsSacrifice(ev) {
			n++
		}
	}
	return n
}

// libraryToHandMoves counts the library->hand moves on the log -- Forbidden
// Ritual's sacrifice fodder and Ad Nauseam's dig both ride MoveZone, so the
// helpers below take a baseline and compare.
func libraryToHandMoves(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.From == state.ZLibrary && ev.To == state.ZHand {
			n++
		}
	}
	return n
}

// castAndReachElection casts a resolved RepeatOptional carrier at seat 0 and
// answers every mid-resolution ask on the way until the do/while election is
// pending, returning it. bodyAnswers are submitted for each KModes/KChoose
// body or nested unless-pay ask before the election, in order. A nil talker
// (no ask pending) fatals -- the tests assert the exact ask sequence, not a
// tolerant drain.
func castAndReachElection(t *testing.T, e *Engine, id state.ObjID, want int, answers ...int) *decision.Decision {
	t.Helper()
	d := castFixture(t, e, id, want)
	for i, a := range answers {
		if d == nil {
			t.Fatalf("answer %d: no decision pending before the repeat election", i)
		}
		if d.ResumeKind == "repeat_optional" {
			t.Fatalf("answer %d: the election was reached after only %d body answers", i, i)
		}
		submitChoices(t, e, a)
		d = e.Pending()
	}
	if d == nil || d.ResumeKind != "repeat_optional" {
		t.Fatalf("expected the repeat_optional election, got %+v", d)
	}
	if d.Kind != decision.KChoose || d.Prompt != "Repeat this process?" {
		t.Fatalf("election shape = kind %s prompt %q, want KChoose %q", d.Kind, d.Prompt, "Repeat this process?")
	}
	return d
}

// submitStop answers the pending repeat election with the "Stop" option.
func submitStop(t *testing.T, e *Engine, d *decision.Decision) {
	t.Helper()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "no" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("repeat election has no stop option: %+v", d.Options)
	}
	submitChoices(t, e, idx)
}

// TestAdNauseamOptionalRepeatElectionStopsOnNo is the brief's core leaf on
// the real corpus card: Ad Nauseam's optional do/while poses the "Repeat
// this process?" election after its first reveal, and answering no resolves
// after exactly that one iteration (one card taken, no second).
func TestAdNauseamOptionalRepeatElectionStopsOnNo(t *testing.T) {
	t.Parallel()
	e, cfg, id, caster := corpusCardConfig(t, 6101, "Ad Nauseam")
	addMana(t, e, caster, "BBBCC") // {3}{B}{B}

	movesBefore := libraryToHandMoves(e)
	libBefore := len(e.G.Zone(state.ZLibrary, caster))
	handBefore := len(e.G.Zone(state.ZHand, caster))

	d := castAndReachElection(t, e, id, -1) // Ad Nauseam has no targets: the election is the first ask
	if d.Player != caster {
		t.Fatalf("election player = %d, want the caster %d", d.Player, caster)
	}
	// Precondition: the body actually did something (one reveal->hand) before
	// the election, so a vacuously-empty body could not pass this test.
	if got := libraryToHandMoves(e) - movesBefore; got != 1 {
		t.Fatalf("precondition failed: body took %d cards before the election, want 1", got)
	}
	if got := libBefore - len(e.G.Zone(state.ZLibrary, caster)); got != 1 {
		t.Fatalf("precondition failed: library shrank by %d, want 1", got)
	}

	submitStop(t, e, d)
	passUntilStackEmpty(t, e, 30)

	// "No" stops: exactly one card was taken, one left the library, and the
	// spell resolved off the stack.
	if got := libraryToHandMoves(e) - movesBefore; got != 1 {
		t.Fatalf("after stopping, %d cards were taken, want exactly 1", got)
	}
	if got := libBefore - len(e.G.Zone(state.ZLibrary, caster)); got != 1 {
		t.Fatalf("after stopping, the library shrank by %d, want 1", got)
	}
	// The taken card replaced the cast spell in hand.
	if got := len(e.G.Zone(state.ZHand, caster)); got != handBefore {
		t.Fatalf("hand size = %d, want %d (one card taken, the spell left)", got, handBefore)
	}
	if o := e.G.Obj(id); o != nil && o.Zone == state.ZStack {
		t.Fatalf("Ad Nauseam is still on the stack after the stop answer")
	}
	replayCheck(t, e, cfg)
}

// TestAdNauseamOptionalRepeatYesIterates proves the other side of the
// election: answering yes runs the body a second time (a second card is
// taken), so the loop is a real do/while and the "no" test above is not
// passing because iteration is impossible.
func TestAdNauseamOptionalRepeatYesIterates(t *testing.T) {
	t.Parallel()
	e, cfg, id, caster := corpusCardConfig(t, 6102, "Ad Nauseam")
	addMana(t, e, caster, "BBBCC")

	movesBefore := libraryToHandMoves(e)
	d := castAndReachElection(t, e, id, -1)
	if got := libraryToHandMoves(e) - movesBefore; got != 1 {
		t.Fatalf("precondition failed: first body took %d cards, want 1", got)
	}
	// Answer yes: the resume must run the body of the next iteration.
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "yes" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("election has no repeat option: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	d = e.Pending()
	if d == nil || d.ResumeKind != "repeat_optional" {
		t.Fatalf("after a yes, expected the next iteration's election, got %+v", d)
	}
	if got := libraryToHandMoves(e) - movesBefore; got != 2 {
		t.Fatalf("after a yes, %d cards were taken, want 2 (the second iteration ran)", got)
	}
	submitStop(t, e, d)
	passUntilStackEmpty(t, e, 30)
	if got := libraryToHandMoves(e) - movesBefore; got != 2 {
		t.Fatalf("after the stop, %d cards were taken, want exactly 2", got)
	}
	replayCheck(t, e, cfg)
}

// forbiddenRitualFixture seats the real corpus Forbidden Ritual plus three
// real Grizzly Bears on seat 0's battlefield (nontoken sacrifice fodder),
// pins the turn-1 active seat to 0, bridges the Ritual into hand and funds
// {2}{B}{B}.
func forbiddenRitualFixture(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	ritual := mustCorpusCard(t, reg, "Forbidden Ritual")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	seat0 := []*cards.Card{ritual, bear, bear, bear}
	seat0 = append(seat0, mountainDeck(t, 36)...)
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{seat0, mountainDeck(t, 40)}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	id := findByName(e, "Forbidden Ritual", 0)
	if id == 0 {
		t.Fatal("Forbidden Ritual not found for seat 0 -- corpus missing?")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: state.ZHand})
	// Park the three bears on the battlefield through the event path. They
	// must be real permanents (CR 701.21a nontoken fodder), not tokens. The
	// seeded shuffle may have drawn one into the opening hand, so scan both
	// the hand and the library.
	parked := 0
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, oid := range e.G.Zone(z, 0) {
			o := e.G.Obj(oid)
			if o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
				e.emit(events.Event{Kind: events.MoveZone, Obj: oid, From: z, To: state.ZBattlefield})
				parked++
				if parked == 3 {
					break
				}
			}
		}
		if parked == 3 {
			break
		}
	}
	if parked < 2 {
		t.Fatalf("precondition failed: only %d Grizzly Bears parked, want at least 2", parked)
	}
	e.pending = nil
	e.priorityRound()
	return e, cfg, id
}

// TestForbiddenRitualBodyAskResumesToRepeatElection is the regression the
// findings require. Forbidden Ritual's body sacrifices a permanent and then
// resolves a GenericChoice KModes ask, so the loop suspends INSIDE its body.
// Answering that body ask (and the nested unless-pay) must resume to the
// repeat election -- not run a second body iteration. The test asserts the
// very next decision is the election, and that exactly one iteration's
// sacrifice happened before it.
func TestForbiddenRitualBodyAskResumesToRepeatElection(t *testing.T) {
	t.Parallel()
	e, cfg, id := forbiddenRitualFixture(t, 6201)
	addMana(t, e, 0, "BBCC") // {2}{B}{B}

	d := castFixture(t, e, id, 1) // target the opponent (seat 1)
	// The body's first ask: which nontoken permanent to sacrifice (three
	// Grizzly Bears are eligible, so the engine poses a real choice).
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "sacrifice" {
		t.Fatalf("expected the body's sacrifice pick, got %+v", d)
	}
	if got := countSacrifices(e); got != 0 {
		t.Fatalf("precondition failed: %d sacrifices before the pick was answered, want 0", got)
	}
	submitChoices(t, e, 0)
	d = e.Pending()

	// The body's second ask: the GenericChoice modal election.
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected the body's GenericChoice KModes ask, got %+v", d)
	}
	if got := countSacrifices(e); got != 1 {
		t.Fatalf("precondition failed: body sacrificed %d permanents before its mode ask, want 1", got)
	}

	// Answer the body's mode (option 0 = PaySac, the sacrifice branch),
	// which chains into the targeted player's unless-pay ask.
	submitChoices(t, e, 0)
	d = e.Pending()
	if d == nil || d.ResumeKind != "unless_pay" {
		t.Fatalf("expected the nested unless_pay ask, got %+v", d)
	}
	// Decline to pay: the opponent loses 2 life and the body completes.
	decline := -1
	for _, o := range d.Options {
		if o.Label == "Don't pay" {
			decline = o.Index
		}
	}
	if decline < 0 {
		t.Fatalf("unless-pay ask has no decline option: %+v", d.Options)
	}
	submitChoices(t, e, decline)

	// THE REGRESSION: after the body completed, the engine must pose the
	// repeat election for the next iteration -- not run that iteration's
	// body (which would appear here as a second KModes ask).
	d = e.Pending()
	if d == nil || d.ResumeKind != "repeat_optional" {
		t.Fatalf("after the body completed, expected the repeat_optional election, got kind=%v resume=%q -- "+
			"the body-suspension resume ran the next iteration instead of asking", d.Kind, d.ResumeKind)
	}
	if sacAfter := countSacrifices(e); sacAfter != 1 {
		t.Fatalf("before the election %d permanents were sacrificed, want exactly 1 (no second iteration ran)", sacAfter)
	}

	// Stop: exactly one iteration's effects stand.
	submitStop(t, e, d)
	passUntilStackEmpty(t, e, 30)
	if got := countSacrifices(e); got != 1 {
		t.Fatalf("after stopping, %d permanents were sacrificed, want exactly 1", got)
	}
	if got := e.G.Players[1].Life; got != 18 {
		t.Fatalf("opponent life = %d, want 18 (one 2-life loss)", got)
	}
	if o := e.G.Obj(id); o != nil && o.Zone == state.ZStack {
		t.Fatalf("Forbidden Ritual is still on the stack after the stop answer")
	}
	replayCheck(t, e, cfg)
}

// TestForbiddenRitualNoProgressIterationEndsTheLoop pins the cardfuzz
// livelock (decision_made "choose:[0]" -> Note "clears remembered" ->
// decision_ask "choose", forever): once the caster has no nontoken permanent
// left, an iteration sacrifices nothing, its GenericChoice is gated off and
// its Cleanup only Notes -- the body posed no decision and changed nothing,
// so every further "repeat" is the identical no-op. The engine must end the
// do/while there instead of re-offering the election. It walks the real
// shape: one iteration that DOES sacrifice (so the election is offered and
// answered yes), then the no-op iteration, after which no election follows.
func TestForbiddenRitualNoProgressIterationEndsTheLoop(t *testing.T) {
	t.Parallel()
	e, cfg, id := forbiddenRitualFixture(t, 6203)
	// Leave exactly one nontoken permanent on seat 0's battlefield.
	bf := append([]state.ObjID(nil), e.G.Zone(state.ZBattlefield, 0)...)
	for _, oid := range bf[1:] {
		e.emit(events.Event{Kind: events.MoveZone, Obj: oid, From: state.ZBattlefield, To: state.ZGraveyard})
	}
	if got := len(e.G.Zone(state.ZBattlefield, 0)); got != 1 {
		t.Fatalf("precondition failed: seat 0 controls %d permanents, want 1", got)
	}
	e.pending = nil
	e.priorityRound()
	addMana(t, e, 0, "BBCC")

	d := castFixture(t, e, id, 1)
	for i := 0; d != nil && d.ResumeKind != "repeat_optional"; i++ {
		if i > 8 {
			t.Fatalf("no repeat election after %d body answers; last %+v", i, d)
		}
		pick := 0
		for _, o := range d.Options {
			if o.Label == "Don't pay" {
				pick = o.Index
			}
		}
		submitChoices(t, e, pick)
		d = e.Pending()
	}
	if d == nil {
		t.Fatal("the first iteration ended without offering the repeat election")
	}
	if got := countSacrifices(e); got != 1 {
		t.Fatalf("precondition failed: first iteration sacrificed %d permanents, want 1", got)
	}
	yes := -1
	for _, o := range d.Options {
		if o.Kind == "yes" {
			yes = o.Index
		}
	}
	submitChoices(t, e, yes)

	// The second iteration had nothing to sacrifice: no further election.
	if d := e.Pending(); d != nil && d.ResumeKind == "repeat_optional" {
		t.Fatalf("a no-progress iteration re-offered the repeat election: %+v", d)
	}
	if got := countSacrifices(e); got != 1 {
		t.Fatalf("%d permanents sacrificed, want 1", got)
	}
	passUntilStackEmpty(t, e, 30)
	if o := e.G.Obj(id); o != nil && o.Zone == state.ZStack {
		t.Fatal("Forbidden Ritual is still on the stack")
	}
	replayCheck(t, e, cfg)
}
