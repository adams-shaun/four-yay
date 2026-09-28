package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The token-creation replacement class (repl:CreateToken / DB$ ReplaceToken)
// pinned end to end on real corpus cards: Divine Visitation (the filing
// card), Doubling Season, Parallel Lives, Academy Manufactor and Xorn. The
// token creator is an authored fixture artifact with an AB$ Token ability
// (never a corpus .txt, per the licensing rule) whose TokenScript$ names a
// real corpus token script, so every mint rides the engine's own effToken
// emit path and the replacements intercept exactly where the live game's
// would.

// tokenReplCorpusCard looks a real corpus card up by NAME.
func tokenReplCorpusCard(t *testing.T, name string) *cards.Card {
	t.Helper()
	c, ok := testutil.CorpusRegistry(t).Lookup(name)
	if !ok {
		t.Fatalf("corpus missing %s", name)
	}
	return c
}

// tokenReplGame builds a 2-seat engine whose seat 0 deck carries the given
// cards (so they can be moved to the battlefield through real logged
// MoveZone events) and whose token registry is the real corpus one. The
// returned cfg travels to replayCheck.
func tokenReplGame(t *testing.T, seed uint64, seat0 ...*cards.Card) (*Engine, Config) {
	t.Helper()
	return tokenReplGameSeats(t, seed, seat0, nil)
}

// tokenReplGameSeats is tokenReplGame with a seeded seat 1 deck too, for
// the opponent-side shapes (halving_season's ValidToken$ Card.OppCtrl).
func tokenReplGameSeats(t *testing.T, seed uint64, seat0, seat1 []*cards.Card) (*Engine, Config) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	if seat1 == nil {
		seat1 = mountainDeck(t, 40)
	}
	build := func(s uint64) Config {
		return Config{Seed: s, Names: []string{"a", "b"},
			Decks: [][]*cards.Card{
				append(append([]*cards.Card{}, seat0...), mountainDeck(t, 40-len(seat0))...),
				append(append([]*cards.Card{}, seat1...), mountainDeck(t, 40-len(seat1))...),
			},
			Tokens: reg.Tokens,
		}
	}
	cfg := seatZeroStart(build(seed))
	e := New(cfg)
	e.Advance()
	return e, cfg
}

// passPriorityOnce submits the pass option of the pending priority
// decision, whoever holds it — the APNAP hand-off a second seat's
// activation needs before its own ability option is pending.
func passPriorityOnce(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending to pass: %+v", d)
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
		t.Fatalf("submit pass: %v", err)
	}
}

// moveSeededCard moves one specific *cards.Card from seat p's library or
// hand to `to` through a real logged MoveZone (the moveSeeded harness, for
// an already-parsed card).
func moveSeededCard(t *testing.T, e *Engine, p state.PlayerID, c *cards.Card, to state.Zone) state.ObjID {
	t.Helper()
	toMain1(t, e)
	name := c.Faces[0].Name
	for _, z := range []state.Zone{state.ZLibrary, state.ZHand} {
		for _, id := range e.G.Zone(z, p) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
				e.pending = nil
				return id
			}
		}
	}
	t.Fatalf("seeded card %q not found in seat %d's library or hand", name, p)
	return 0
}

// tokenForgeSrc is the authored token-creating artifact: activate it and it
// creates one token of the named script, through the engine's own effToken
// path.
func tokenForgeSrc(script string) string {
	return "Name:Token Forge\nTypes:Artifact\n" +
		"A:AB$ Token | Cost$ T | TokenScript$ " + script + " | TokenOwner$ You | SpellDescription$ Create a token.\n" +
		"Oracle:x\n"
}

func cardByName(t *testing.T, src string) *cards.Card { return card(t, src) }

// activateTokenForge funds nothing (Cost$ T on an artifact is paid by the
// activation itself), activates the maker's ability and drains the stack.
func activateTokenForge(t *testing.T, e *Engine, maker state.ObjID) {
	t.Helper()
	addMana(t, e, 0, "") // toMain1 + a fresh priority decision for seat 0
	submitChoices(t, e, abilityOption(t, e, maker, 0).Index)
	passUntilStackEmpty(t, e, 20)
}

// countTokensNamedOnSeat counts seat p's battlefield objects whose face
// name is exactly name.
func countTokensNamedOnSeat(t *testing.T, e *Engine, p state.PlayerID, name string) int {
	t.Helper()
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if o != nil && o.Face() != nil && o.Face().Name == name {
			n++
		}
	}
	return n
}

// TestDivineVisitationReplacesCreatureTokens is the filing card, end to
// end: a creature-token creation under DV's controller becomes the
// 4/4 white Angel with flying and vigilance (the real
// w_4_4_angel_flying_vigilance script), one per would-be token; a
// NON-creature token (a Treasure) is untouched.
func TestDivineVisitationReplacesCreatureTokens(t *testing.T) {
	t.Parallel()
	dv := tokenReplCorpusCard(t, "Divine Visitation")
	squirrelMaker := cardByName(t, tokenForgeSrc("g_1_1_squirrel"))
	treasureMaker := cardByName(t, tokenForgeSrc("c_a_treasure_sac"))

	t.Run("creature_token_becomes_angel", func(t *testing.T) {
		e, cfg := tokenReplGame(t, 41, dv, squirrelMaker)
		moveSeededCard(t, e, 0, dv, state.ZBattlefield)
		maker := moveSeededCard(t, e, 0, squirrelMaker, state.ZBattlefield)
		activateTokenForge(t, e, maker)
		if got := countTokensNamedOnSeat(t, e, 0, "Angel Token"); got != 1 {
			t.Fatalf("Divine Visitation made %d Angel Tokens, want 1", got)
		}
		if got := countTokensNamedOnSeat(t, e, 0, "Squirrel Token"); got != 0 {
			t.Fatalf("the scripted token leaked: %d Squirrel Tokens", got)
		}
		// The angel is the real script: 4/4 white Creature Angel with
		// flying and vigilance.
		var angel *state.Object
		for _, id := range e.G.Zone(state.ZBattlefield, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Angel Token" {
				angel = o
			}
		}
		if angel == nil {
			t.Fatal("no Angel Token object on the battlefield")
		}
		f := angel.Face()
		if f.Power() != 4 || f.Toughness() != 4 {
			t.Fatalf("angel P/T = %d/%d, want 4/4", f.Power(), f.Toughness())
		}
		if !slices.Contains(f.Types, "Creature") || !slices.Contains(f.Types, "Angel") {
			t.Fatalf("angel types = %v, want Creature Angel", f.Types)
		}
		if f.Colors != "white" {
			t.Fatalf("angel colours = %q, want white", f.Colors)
		}
		if !f.HasKeyword("Flying") || !f.HasKeyword("Vigilance") {
			t.Fatalf("angel keywords missing flying/vigilance: %v", f.Keywords)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("non_creature_token_untouched", func(t *testing.T) {
		e, cfg := tokenReplGame(t, 43, dv, treasureMaker)
		moveSeededCard(t, e, 0, dv, state.ZBattlefield)
		maker := moveSeededCard(t, e, 0, treasureMaker, state.ZBattlefield)
		activateTokenForge(t, e, maker)
		if got := countTokensNamedOnSeat(t, e, 0, "Treasure Token"); got != 1 {
			t.Fatalf("made %d Treasure Tokens, want 1 (DV must not touch non-creature tokens)", got)
		}
		if got := countTokensNamedOnSeat(t, e, 0, "Angel Token"); got != 0 {
			t.Fatalf("DV reached a non-creature token: %d Angel Tokens", got)
		}
		replayCheck(t, e, cfg)
	})
}

// TestDoublingSeasonDoublesTokens pins the Amount default: one would-be
// token becomes two identical mints.
func TestDoublingSeasonDoublesTokens(t *testing.T) {
	t.Parallel()
	ds := tokenReplCorpusCard(t, "Doubling Season")
	maker := cardByName(t, tokenForgeSrc("g_1_1_squirrel"))
	e, cfg := tokenReplGame(t, 47, ds, maker)
	moveSeededCard(t, e, 0, ds, state.ZBattlefield)
	m := moveSeededCard(t, e, 0, maker, state.ZBattlefield)
	activateTokenForge(t, e, m)
	if got := countTokensNamedOnSeat(t, e, 0, "Squirrel Token"); got != 2 {
		t.Fatalf("Doubling Season made %d Squirrel Tokens, want 2", got)
	}
	replayCheck(t, e, cfg)
}

// TestAcademyManufactorClueFoodTreasure pins the CSV / 1-to-N shape: one
// Clue becomes one Clue, one Food and one Treasure.
func TestAcademyManufactorClueFoodTreasure(t *testing.T) {
	t.Parallel()
	am := tokenReplCorpusCard(t, "Academy Manufactor")
	maker := cardByName(t, tokenForgeSrc("c_a_clue_draw"))
	e, cfg := tokenReplGame(t, 53, am, maker)
	moveSeededCard(t, e, 0, am, state.ZBattlefield)
	m := moveSeededCard(t, e, 0, maker, state.ZBattlefield)
	activateTokenForge(t, e, m)
	for name, want := range map[string]int{
		"Clue Token": 1, "Food Token": 1, "Treasure Token": 1,
	} {
		if got := countTokensNamedOnSeat(t, e, 0, name); got != want {
			t.Fatalf("Academy Manufactor made %d %s, want %d", got, name, want)
		}
	}
	replayCheck(t, e, cfg)
}

// TestXornAddsTreasure pins the AddToken shape: a Treasure creation becomes
// that Treasure PLUS one more Treasure.
func TestXornAddsTreasure(t *testing.T) {
	t.Parallel()
	xorn := tokenReplCorpusCard(t, "Xorn")
	maker := cardByName(t, tokenForgeSrc("c_a_treasure_sac"))
	e, cfg := tokenReplGame(t, 59, xorn, maker)
	moveSeededCard(t, e, 0, xorn, state.ZBattlefield)
	m := moveSeededCard(t, e, 0, maker, state.ZBattlefield)
	activateTokenForge(t, e, m)
	if got := countTokensNamedOnSeat(t, e, 0, "Treasure Token"); got != 2 {
		t.Fatalf("Xorn made %d Treasure Tokens, want 2 (the original plus one)", got)
	}
	replayCheck(t, e, cfg)
}

// TestTokenReplacementsComposeScanOrder pins composition: Divine
// Visitation and Doubling Season together turn each would-be creature
// token into TWO angels — scan order over the plan, each match applying
// once, no infinite loop.
func TestTokenReplacementsComposeScanOrder(t *testing.T) {
	t.Parallel()
	dv := tokenReplCorpusCard(t, "Divine Visitation")
	ds := tokenReplCorpusCard(t, "Doubling Season")
	maker := cardByName(t, tokenForgeSrc("g_1_1_squirrel"))
	e, cfg := tokenReplGame(t, 61, ds, dv, maker)
	moveSeededCard(t, e, 0, ds, state.ZBattlefield)
	moveSeededCard(t, e, 0, dv, state.ZBattlefield)
	m := moveSeededCard(t, e, 0, maker, state.ZBattlefield)
	activateTokenForge(t, e, m)
	if got := countTokensNamedOnSeat(t, e, 0, "Angel Token"); got != 2 {
		t.Fatalf("DV + Doubling Season made %d Angel Tokens, want exactly 2", got)
	}
	if got := countTokensNamedOnSeat(t, e, 0, "Squirrel Token"); got != 0 {
		t.Fatalf("the scripted token leaked through composition: %d", got)
	}
	replayCheck(t, e, cfg)
}

// TestTokenReplacementExtrasNeverRematch is the loop guard: two Amount
// doublers compose to exactly 4 mints (each applies ONCE per plan mint),
// never 8 and never a livelock — the extras are emitted through the direct
// events.Emit path and can never re-match.
func TestTokenReplacementExtrasNeverRematch(t *testing.T) {
	t.Parallel()
	ds := tokenReplCorpusCard(t, "Doubling Season")
	pl := tokenReplCorpusCard(t, "Parallel Lives")
	maker := cardByName(t, tokenForgeSrc("g_1_1_squirrel"))
	e, cfg := tokenReplGame(t, 67, ds, pl, maker)
	moveSeededCard(t, e, 0, ds, state.ZBattlefield)
	moveSeededCard(t, e, 0, pl, state.ZBattlefield)
	m := moveSeededCard(t, e, 0, maker, state.ZBattlefield)
	activateTokenForge(t, e, m)
	if got := countTokensNamedOnSeat(t, e, 0, "Squirrel Token"); got != 4 {
		t.Fatalf("two doublers made %d Squirrel Tokens, want exactly 4 (extras re-matching would give 8+)", got)
	}
	replayCheck(t, e, cfg)
}

// TestHalvingSeasonRoundsOpponentTokensDownToZero pins the empty-plan
// arm: Halving Season (ValidToken$ Card.OppCtrl, Amount$ HalfDown) under
// seat 0 while seat 1, the opponent, creates ONE token — HalfDown floors
// to zero, the plan empties, the Note witnesses the nothing-creation in
// the log, and the handled=true return discards the original event (no
// mint ever lands, so effToken's want-id rider bookkeeping reads a nil
// g.Obj(want) and skips).
func TestHalvingSeasonRoundsOpponentTokensDownToZero(t *testing.T) {
	t.Parallel()
	hs := tokenReplCorpusCard(t, "Halving Season")
	maker := cardByName(t, tokenForgeSrc("g_1_1_squirrel"))
	e, cfg := tokenReplGameSeats(t, 73, []*cards.Card{hs}, []*cards.Card{maker})
	moveSeededCard(t, e, 0, hs, state.ZBattlefield)
	m := moveSeededCard(t, e, 1, maker, state.ZBattlefield)
	// seat 0 holds the APNAP first slot (seatZeroStart): get its priority
	// pending, pass it, and seat 1's own priority then offers its maker's
	// ability (activateTokenForge's addMana re-runs the round, granting to
	// the holder the pass just handed it to).
	addMana(t, e, 0, "")
	passPriorityOnce(t, e)
	activateTokenForge(t, e, m)
	if got := countTokensNamedOnSeat(t, e, 1, "Squirrel Token"); got != 0 {
		t.Fatalf("Halving Season let %d Squirrel Tokens through, want 0 (HalfDown of 1 is 0)", got)
	}
	if got := countTokensNamedOnSeat(t, e, 0, "Squirrel Token"); got != 0 {
		t.Fatalf("the token landed under the wrong seat: %d", got)
	}
	notes := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "no tokens created (replacement effect rounded the creation down to zero)" {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("empty-plan witness Note count = %d, want exactly 1", notes)
	}
	// And the discarded original left no TokenCreate of its own in the log.
	for _, ev := range e.L.Events {
		if ev.Kind == events.TokenCreate && ev.Text == "g_1_1_squirrel" {
			t.Fatal("the halved-to-zero TokenCreate was still emitted")
		}
	}
	replayCheck(t, e, cfg)
}

// TestTokenReplacementOptionalDeclines pins the DECLINE answer of the
// Optional$ True election Flitwing Lyev poses (task tokrepl1 replaced the
// old silent decline with a real KChoose): the decline option leaves the
// token created verbatim. The accept arm and the ask itself are pinned in
// token_replacement_standins_test.go.
func TestTokenReplacementOptionalDeclines(t *testing.T) {
	t.Parallel()
	lyev := tokenReplCorpusCard(t, "Flitwing, Lyev Detective")
	maker := cardByName(t, tokenForgeSrc("c_a_treasure_sac"))
	e, cfg := tokenReplGame(t, 71, lyev, maker)
	moveSeededCard(t, e, 0, lyev, state.ZBattlefield)
	m := moveSeededCard(t, e, 0, maker, state.ZBattlefield)
	addMana(t, e, 0, "")
	submitChoices(t, e, abilityOption(t, e, m, 0).Index)
	d := drainToChooseOrEmpty(t, e)
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 || d.Options[0].Kind != "decline" {
		t.Fatalf("no parked Optional election with a decline option: %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	if got := countTokensNamedOnSeat(t, e, 0, "Treasure Token"); got != 1 {
		t.Fatalf("a declined Optional$ True replacement must leave the token: got %d Treasure Tokens, want 1", got)
	}
	if got := countTokensNamedOnSeat(t, e, 0, "Clue Token"); got != 0 {
		t.Fatalf("the declined replacement applied anyway: %d Clue Tokens", got)
	}
	replayCheck(t, e, cfg)
}
