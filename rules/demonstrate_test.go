package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// demonstrateMove moves one corpus card from seat p's hand or library to
// zone `to` through a logged MoveZone (mobVerdictVoteMove's shape,
// generalised off its seat-0-only hardcoding), then re-asks priority.
func demonstrateMove(t *testing.T, e *Engine, p state.PlayerID, name string, to state.Zone) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, p) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == name {
				if z != to {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
				}
				e.pending = nil
				e.priorityRound()
				return id
			}
		}
	}
	t.Fatalf("corpus card %q absent from seat %d hand/library", name, p)
	return 0
}

// demonstrateEngine builds a table whose seat 0 opens with the fixture deck
// seat0 (padded with Mountains) and each other seat the given fixture deck
// padded with Mountains. Seat 0 is the CR 103.1 toss winner (seatZeroStart),
// so the asks' players are fixed.
func demonstrateEngine(t *testing.T, reg *cards.Registry, seat0 []*cards.Card, others ...[]*cards.Card) (*Engine, Config) {
	t.Helper()
	mountain := searchCorpusCard(t, reg, "Mountain")
	deck0 := append([]*cards.Card(nil), seat0...)
	for len(deck0) < 40 {
		deck0 = append(deck0, mountain)
	}
	decks := [][]*cards.Card{deck0}
	for _, other := range others {
		opp := append([]*cards.Card(nil), other...)
		for len(opp) < 40 {
			opp = append(opp, mountain)
		}
		decks = append(decks, opp)
	}
	names := make([]string, len(decks))
	for i := range names {
		names[i] = string(rune('a' + i))
	}
	cfg := seatZeroStart(Config{Seed: 7717, Names: names, Decks: decks, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// demonstrateStackCopies counts the StackCopy events the log carries so far.
func demonstrateStackCopies(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.StackCopy {
			n++
		}
	}
	return n
}

// demonstrateBearsOnBattlefield counts seat p's battlefield permanents named
// name, split into tokens and real cards.
func demonstrateBearsOnBattlefield(e *Engine, p state.PlayerID, name string) (tokens, real int) {
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil || o.Face().Name != name {
			continue
		}
		if o.IsToken {
			tokens++
		} else {
			real++
		}
	}
	return tokens, real
}

// TestSilverquillLecturerDemonstrateCopyAndOpponentToken is the brief's pin
// (kw-demonstrate): casting a creature spell with Silverquill Lecturer on the
// battlefield poses the may-copy election, then the opponent choice, and both
// copies materialise as creature tokens when they resolve (the caster's copy
// and the chosen opponent's copy, each under its own controller). The trigger
// comes from the granted-keyword synthesis (rules/trigger_granted.go's
// checkGrantedDemonstrateTriggers) -- Silverquill Lecturer's entire printed
// text is the AddKeyword$ Demonstrate static granting demonstrate to creature
// spells you cast.
func TestSilverquillLecturerDemonstrateCopyAndOpponentToken(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	lecturer := searchCorpusCard(t, reg, "Silverquill Lecturer")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	e, cfg := demonstrateEngine(t, reg, []*cards.Card{lecturer, bear}, nil, nil, nil)

	lect := demonstrateMove(t, e, 0, "Silverquill Lecturer", state.ZBattlefield)
	if o := e.G.Obj(lect); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Silverquill Lecturer is not on the battlefield: %+v", o)
	}
	bearID := demonstrateMove(t, e, 0, "Grizzly Bears", state.ZHand)
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("Grizzly Bears is not in hand: %+v", o)
	}

	addMana(t, e, 0, "GG")
	castFixture(t, e, bearID, -1)

	// The demonstrate trigger fired on the paid cast: the may-copy election
	// (stage 0) is pending to the caster.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "demonstrate" {
		t.Fatalf("pending after the cast = %+v, want a demonstrate election KChoose", d)
	}
	if d.Player != 0 || d.ResumeTarget != 0 || d.Min != 1 || d.Max != 1 {
		t.Fatalf("election = %+v, want seat 0, stage 0, 1..1", d)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("election options = %+v, want exactly one yes and one no", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index) // yes

	// The opponent choice (stage 1): three living opponents offered, never
	// the caster.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "demonstrate" || d.ResumeTarget != 1 {
		t.Fatalf("pending after the election = %+v, want the demonstrate opponent KChoose", d)
	}
	if d.Player != 0 || d.Min != 1 || d.Max != 1 {
		t.Fatalf("opponent ask = %+v, want seat 0, 1..1", d)
	}
	if len(d.Options) != 3 {
		t.Fatalf("opponent ask offered %d options, want the 3 other seats", len(d.Options))
	}
	for _, o := range d.Options {
		if o.Kind != "player" || o.Player == 0 {
			t.Fatalf("opponent option %+v: want a player option that is not seat 0", o)
		}
	}
	// No copy is on the stack yet: both copies wait for the opponent clause
	// to settle (an emission between the two asks would put a stack spell on
	// top of the demonstrate wrapper and misdirect the opponent ask's resume
	// point).
	if got := demonstrateStackCopies(e); got != 0 {
		t.Fatalf("after the election: %d StackCopy events, want 0 (the copies wait for the opponent clause)", got)
	}
	submitChoices(t, e, d.Options[1].Index) // seat 2
	if got := demonstrateStackCopies(e); got != 2 {
		t.Fatalf("after the opponent choice: %d StackCopy events, want 2", got)
	}
	drainStack(t, e, 200)

	// Both copies materialised as creature tokens: seat 2 holds one token
	// copy of Grizzly Bears, seat 0 the original card plus its own token
	// copy -- and the original is a real card, not a token.
	toks2, real2 := demonstrateBearsOnBattlefield(e, 2, "Grizzly Bears")
	if toks2 != 1 || real2 != 0 {
		t.Fatalf("seat 2's battlefield: %d token bears, %d real, want 1 token copy and no card", toks2, real2)
	}
	toks0, real0 := demonstrateBearsOnBattlefield(e, 0, "Grizzly Bears")
	if toks0 != 1 || real0 != 1 {
		t.Fatalf("seat 0's battlefield: %d token bears, %d real, want 1 token copy and the original card", toks0, real0)
	}
	replayCheck(t, e, cfg)
}

// TestDemonstrateElectionDeclineCopiesNothing pins the may-copy election's
// decline arm: answering no copies nothing and the original resolves alone.
func TestDemonstrateElectionDeclineCopiesNothing(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	lecturer := searchCorpusCard(t, reg, "Silverquill Lecturer")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	e, _ := demonstrateEngine(t, reg, []*cards.Card{lecturer, bear}, nil)

	demonstrateMove(t, e, 0, "Silverquill Lecturer", state.ZBattlefield)
	bearID := demonstrateMove(t, e, 0, "Grizzly Bears", state.ZHand)
	addMana(t, e, 0, "GG")
	castFixture(t, e, bearID, -1)

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "demonstrate" {
		t.Fatalf("pending after the cast = %+v, want a demonstrate election KChoose", d)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("election options = %+v, want exactly one yes and one no", d.Options)
	}
	submitChoices(t, e, d.Options[1].Index) // no
	if got := demonstrateStackCopies(e); got != 0 {
		t.Fatalf("a declined demonstrate emitted %d StackCopy events, want 0", got)
	}
	drainStack(t, e, 200)
	toks, real := demonstrateBearsOnBattlefield(e, 0, "Grizzly Bears")
	if toks != 0 || real != 1 {
		t.Fatalf("after the decline: %d token bears, %d real, want only the original", toks, real)
	}
}

// TestDemonstrateSoleOpponentAsksNothing pins the strict-supersets half of
// the opponent choice: at two seats there is exactly one admissible answer,
// so both copies are recorded without a second ask (the battle-protector
// precedent).
func TestDemonstrateSoleOpponentAsksNothing(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	lecturer := searchCorpusCard(t, reg, "Silverquill Lecturer")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	e, _ := demonstrateEngine(t, reg, []*cards.Card{lecturer, bear}, nil)

	demonstrateMove(t, e, 0, "Silverquill Lecturer", state.ZBattlefield)
	bearID := demonstrateMove(t, e, 0, "Grizzly Bears", state.ZHand)
	addMana(t, e, 0, "GG")
	castFixture(t, e, bearID, -1)

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "demonstrate" {
		t.Fatalf("pending after the cast = %+v, want a demonstrate election KChoose", d)
	}
	submitChoices(t, e, d.Options[0].Index) // yes
	if got := demonstrateStackCopies(e); got != 2 {
		t.Fatalf("one-opponent demonstrate emitted %d StackCopy events, want 2 with no second ask", got)
	}
	drainStack(t, e, 200)
	toks2, _ := demonstrateBearsOnBattlefield(e, 1, "Grizzly Bears")
	toks0, real0 := demonstrateBearsOnBattlefield(e, 0, "Grizzly Bears")
	if toks2 != 1 || toks0 != 1 || real0 != 1 {
		t.Fatalf("seat 0: %d tokens %d real, seat 1: %d tokens -- want one token copy per seat and the original",
			toks0, real0, toks2)
	}
}

// TestPrintedDemonstrateCarrierOffersTheElection pins the printed expansion
// (cards/kw_demonstrate.go): Excavation Technique carries K:Demonstrate on
// its face, the compiled face grows the SpellCast trigger, and casting it
// poses the same may-copy election -- a decline copies nothing and the spell
// itself resolves.
func TestPrintedDemonstrateCarrierOffersTheElection(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	et := searchCorpusCard(t, reg, "Excavation Technique")
	found := false
	for _, f := range et.Faces {
		for _, tr := range f.Triggers {
			if tr.Mode == "SpellCast" && tr.Params["ValidCard"] == "Card.Self" && tr.Effect != nil &&
				tr.Effect.API == "Demonstrate" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("Excavation Technique's compiled face carries no expanded demonstrate SpellCast trigger")
	}

	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	e, cfg := demonstrateEngine(t, reg, []*cards.Card{et}, []*cards.Card{bear})
	// Seat 1 gets a nonland permanent to target (the same logged-MoveZone
	// pattern demonstrateMove uses, via the seat-1 deck's own opening).
	bearID := demonstrateMove(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("seat 1's Grizzly Bears is not on the battlefield: %+v", o)
	}
	etID := demonstrateMove(t, e, 0, "Excavation Technique", state.ZHand)
	addMana(t, e, 0, "WWWW")

	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == etID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Excavation Technique: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	// The target ask: seat 1's bear.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending after the cast option = %+v, want the target ask", d)
	}
	tidx := -1
	for _, o := range d.Options {
		if o.Obj == bearID {
			tidx = o.Index
		}
	}
	if tidx < 0 {
		t.Fatalf("seat 1's bear not offered as a target: %+v", d.Options)
	}
	submitChoices(t, e, tidx)

	// The demonstrate trigger resolves on the following priority passes (a
	// trigger resolves like any stack object, not inside the cast flow).
	d = passUntilNonPriority(t, e, 20)
	if d.Kind != decision.KChoose || d.ResumeKind != "demonstrate" {
		t.Fatalf("pending after the target = %+v, want a demonstrate election KChoose", d)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("election options = %+v, want exactly one yes and one no", d.Options)
	}
	submitChoices(t, e, d.Options[1].Index) // no
	if got := demonstrateStackCopies(e); got != 0 {
		t.Fatalf("a declined printed demonstrate emitted %d StackCopy events, want 0", got)
	}
	drainStack(t, e, 200)
	if o := e.G.Obj(bearID); o != nil && o.Zone == state.ZBattlefield {
		t.Fatalf("seat 1's bear survived a resolved Excavation Technique: %+v", o)
	}
	replayCheck(t, e, cfg)
}
