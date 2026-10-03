package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The CastSa cast-provenance predicate family (task castsa-provenance):
// Forge's "Card.CastSa Spell.<X>" card-level spec — the cast that put this
// object on the stack had property X — was read nowhere outside the
// ValidLKI$ replacement gate (where it fails closed, the documented row),
// so every AffectedZone$ Stack static and Count$ThisTurnCast_ head carrying
// it matched nothing and Rain of Riches' cascade grant was inert. The four
// mana-spend spellings are implemented against the payment path's own
// encodings: a Treasure/Cave/Desert unit's spend is a tagged ManaAdd event
// (the castfilter2 encoding) and the total spend is the plain negative
// ManaAdd delta (trigmatch.ManaSpentForCast's read). The "first spell" gates ride the
// stackGrantCast scratch: the in-flight cast's own grant walk counts PRIOR
// casts, or the EQ0 idiom would fail for the very cast the grant is for.

// rainOfRichesEngine deals seat 0 Rain of Riches (protagonist, to hand) with
// Jackal Pup arranged on top of the library beneath a Forest (the filler is
// Grizzly Bears), so a Bears cast's cascade finds a legal free cast. The
// window order is the cascade_test.go arrangement.
func rainOfRichesEngine(t *testing.T, seed uint64) (*Engine, Config) {
	t.Helper()
	return cascadeTestEngineFiller(t, seed, "Rain of Riches", []string{"Forest", "Jackal Pup"}, nil, "Grizzly Bears")
}

// submitCastOption submits the cast option for id through the pending
// priority decision (the castFixture shape): the option path re-grants
// priority after the commit, which is what drains the cast's queued cascade
// trigger through its resolution.
func submitCastOption(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %d in %+v", id, d.Options)
	}
	submitChoices(t, e, idx)
}

// cascadeQueuedFor reports whether a KeywordTriggerPush cascade trigger was
// queued for the spell object.
func cascadeQueuedFor(e *Engine, id state.ObjID) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == events.KeywordTriggerPush && ev.Obj == id {
			return true
		}
	}
	return false
}

// rainOnBattlefield casts the protagonist Rain of Riches from hand with a
// plain pool, lets it resolve, and asserts its ETB made at least two
// Treasure tokens — the precondition every later assertion leans on.
func rainOnBattlefield(t *testing.T, e *Engine, rainID state.ObjID) {
	t.Helper()
	addMana(t, e, 0, "RRRRR")
	submitCastOption(t, e, rainID)
	passUntilStackEmpty(t, e, 60)
	if o := e.G.Obj(rainID); o.Zone != state.ZBattlefield {
		t.Fatalf("Rain of Riches resolved to %s, want the battlefield", o.Zone)
	}
	if got := tokensNamed(e, 0, "Treasure"); got < 2 {
		t.Fatalf("Rain's ETB created %d Treasure tokens, want 2", got)
	}
}

// treasureUnit seeds one Treasure-produced mana unit into seat 0's pool
// (the castfilter_test.go pattern; the produced tokens enter tapped, so the
// test rides the pool directly), then funds one plain green unit and
// re-asks priority.
func treasureUnit(t *testing.T, e *Engine) {
	t.Helper()
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "TreasureC", Amount: 1})
	addMana(t, e, 0, "G")
}

func TestRainOfRichesTreasureManaCastCascades(t *testing.T) {
	t.Parallel()
	e, cfg := rainOfRichesEngine(t, 9311)
	rainID := searchMoveByName(t, e, "Rain of Riches", state.ZHand)
	rainOnBattlefield(t, e, rainID)
	// The followed-up cast: Grizzly Bears {1}{G}. The coloured pip takes the
	// plain green unit (coloured pips are paid first) and the generic
	// requirement — plain-before-typed, nothing plain left in the
	// colourless slot — consumes the Treasure unit, so the cast's spend
	// window carries one Treasure-tagged unit.
	bears := searchMoveByName(t, e, "Grizzly Bears", state.ZHand)
	treasureUnit(t, e)
	if got := e.SpellsCastThisTurnMatching(0, "Card.YouCtrl+CastSa Spell.ManaFromTreasure"); got != 0 {
		t.Fatalf("pre-cast treasure-cast count = %d, want 0", got)
	}
	submitCastOption(t, e, bears)
	// The count head reads the cast's own spend window: one Treasure unit
	// was spent on THIS cast.
	if got := e.SpellsCastThisTurnMatching(0, "Card.YouCtrl+CastSa Spell.ManaFromTreasure"); got != 1 {
		t.Fatalf("treasure-cast count = %d, want 1", got)
	}
	if !cascadeQueuedFor(e, bears) {
		t.Fatal("the treasure-mana cast did not queue a cascade trigger")
	}
	// The cascade resolves for real: pass priority until the trigger has
	// resolved into its ask, then the election over the arranged Jackal Pup
	// appears and the decline bottoms it.
	passUntilNonPriority(t, e, 20)
	cascadeElection(t, e, "Jackal Pup")
	submitChoices(t, e) // decline the free cast
	passUntilStackEmpty(t, e, 60)
	replayCheck(t, e, cfg)
}

func TestRainOfRichesPlainCastDoesNotCascade(t *testing.T) {
	t.Parallel()
	e, cfg := rainOfRichesEngine(t, 9312)
	rainID := searchMoveByName(t, e, "Rain of Riches", state.ZHand)
	rainOnBattlefield(t, e, rainID)
	// First follow-up cast paid entirely from the plain pool: no Treasure
	// unit in the spend window, so the Affected$ spec never matches and no
	// cascade trigger is queued.
	bears := searchMoveByName(t, e, "Grizzly Bears", state.ZHand)
	addMana(t, e, 0, "GG")
	submitCastOption(t, e, bears)
	passUntilStackEmpty(t, e, 60)
	if got := e.SpellsCastThisTurnMatching(0, "Card.YouCtrl+CastSa Spell.ManaFromTreasure"); got != 0 {
		t.Fatalf("plain-cast treasure-cast count = %d, want 0", got)
	}
	if cascadeQueuedFor(e, bears) {
		t.Fatal("a plain-mana cast must not cascade")
	}
	// The plain cast did not consume the "first": the NEXT cast, paid with a
	// Treasure unit, cascades.
	bears2 := searchMoveByName(t, e, "Grizzly Bears", state.ZHand)
	treasureUnit(t, e)
	submitCastOption(t, e, bears2)
	if !cascadeQueuedFor(e, bears2) {
		t.Fatal("the first treasure-mana cast must cascade even after a plain cast")
	}
	passUntilNonPriority(t, e, 20)
	cascadeElection(t, e, "Jackal Pup")
	submitChoices(t, e)
	passUntilStackEmpty(t, e, 60)
	replayCheck(t, e, cfg)
}

func TestRainOfRichesSecondTreasureCastDoesNotCascade(t *testing.T) {
	t.Parallel()
	e, cfg := rainOfRichesEngine(t, 9313)
	rainID := searchMoveByName(t, e, "Rain of Riches", state.ZHand)
	rainOnBattlefield(t, e, rainID)
	// First treasure-mana cast cascades; decline its election and drain.
	bears := searchMoveByName(t, e, "Grizzly Bears", state.ZHand)
	treasureUnit(t, e)
	submitCastOption(t, e, bears)
	if !cascadeQueuedFor(e, bears) {
		t.Fatal("the first treasure-mana cast must cascade")
	}
	passUntilNonPriority(t, e, 20)
	cascadeElection(t, e, "Jackal Pup")
	submitChoices(t, e)
	passUntilStackEmpty(t, e, 60)
	// Second treasure-mana cast: the gate's count now reads one prior
	// qualifying cast, EQ0 fails, and nothing cascades.
	bears2 := searchMoveByName(t, e, "Grizzly Bears", state.ZHand)
	treasureUnit(t, e)
	submitCastOption(t, e, bears2)
	if got := e.SpellsCastThisTurnMatching(0, "Card.YouCtrl+CastSa Spell.ManaFromTreasure"); got != 2 {
		t.Fatalf("treasure-cast count = %d, want 2", got)
	}
	if cascadeQueuedFor(e, bears2) {
		t.Fatal("the second treasure-mana cast must not cascade")
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("the second cast left %+v pending, want plain priority", d)
	}
	passUntilStackEmpty(t, e, 60)
	replayCheck(t, e, cfg)
}

// TestWildMagicSorcererFirstExileCastCascades pins the whole first-cast
// family on the non-CastSa sibling (Wild-Magic Sorcerer's wasCastFromExile
// gate): the in-flight cast's own grant walk counts prior casts, so the
// first exile-origin cast of the turn cascades. The exile-origin cast is a
// foretell cast (the was_cast_from_zone_test.go flow).
func TestWildMagicSorcererFirstExileCastCascades(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Delayed Blast Fireball"),
		corpusAlternativeCard(t, "Wild-Magic Sorcerer"))
	dbf := e.G.Zone(state.ZHand, 0)[0]
	wm := e.G.Zone(state.ZHand, 0)[1]
	placeOnBattlefield(t, e, wm)
	if o := e.G.Obj(wm); o.Zone != state.ZBattlefield {
		t.Fatalf("Wild-Magic Sorcerer on %s, want the battlefield", o.Zone)
	}
	e.G.Players[0].Pool[state.MC] = 2
	foretellIt(t, e, dbf)
	driveToTurn3Main(t, e)
	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MR] = 4, 2
	submitOption(t, e, "foretell_cast", "Cast Delayed Blast Fireball (foretold)")
	finishCast(t, e, dbf)
	if !e.WasCastFromExile(dbf) {
		t.Fatal("the foretell cast's provenance is not exile")
	}
	if got := e.SpellsCastThisTurnMatching(0, "Card.YouCtrl+wasCastFromExile"); got != 1 {
		t.Fatalf("exile-cast count = %d, want 1", got)
	}
	if !cascadeQueuedFor(e, dbf) {
		t.Fatal("the first exile-origin cast did not queue a cascade trigger")
	}
	// The mountain library has no castable candidate, so the cascade
	// resolves with no election; drain it.
	drainEtb(t, e)
}

// TestSatoruEntryReadsNoManaSpentCastSa pins the ManaSpent EQ0 spelling at
// its entry-provenance site (Satoru, the Infiltrator's ETB batch trigger): a
// creature cast with no mana spent (Ornithopter, cost {0}) draws the card, a
// paid cast does not.
func TestSatoruEntryReadsNoManaSpentCastSa(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Satoru, the Infiltrator"),
		corpusAlternativeCard(t, "Ornithopter"), corpusAlternativeCard(t, "Grizzly Bears"))
	satoru := e.G.Zone(state.ZHand, 0)[0]
	placeOnBattlefield(t, e, satoru)
	// Satoru's own entry was never cast, so the !wasCast alternative made
	// the trigger fire and draw one — the settled baseline.
	baseline := len(e.G.Zone(state.ZHand, 0))
	if baseline != 3 {
		t.Fatalf("setup hand = %d (Ornithopter, Grizzly Bears, Satoru's own-entry draw), want 3", baseline)
	}
	orn := e.G.Zone(state.ZHand, 0)[0]
	castMode(t, e, orn, "")
	finishCast(t, e, orn)
	drainEtb(t, e)
	// The Ornithopter entry: cast with no mana spent, so the CastSa
	// alternative matches and the trigger draws — the hand nets level (the
	// cast took one card, the draw put one back).
	if got := len(e.G.Zone(state.ZHand, 0)); got != baseline {
		t.Fatalf("hand after the {0} cast = %d, want %d (the no-mana-spent entry draws)", got, baseline)
	}
	if got := e.SpellsCastThisTurnMatching(0, "Card.CastSa Spell.ManaSpent EQ0"); got != 1 {
		t.Fatalf("no-mana-spent count = %d, want 1", got)
	}
	// A paid cast matches neither alternative: no trigger, no draw.
	bear := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MG] = 2
	castMode(t, e, bear, "")
	finishCast(t, e, bear)
	drainEtb(t, e)
	if got := len(e.G.Zone(state.ZHand, 0)); got != baseline-1 {
		t.Fatalf("hand after the paid cast = %d, want %d (a paid entry draws nothing)", got, baseline-1)
	}
	if got := e.SpellsCastThisTurnMatching(0, "Card.CastSa Spell.ManaSpent EQ0"); got != 1 {
		t.Fatalf("no-mana-spent count after a paid cast = %d, want 1", got)
	}
}

// TestCountStaysInclusiveOutsideTheGrantWalk guards the skip's scope: a
// count evaluated outside the in-flight cast's own grant walk still counts
// the cast (Vengevine's EQ2 "second creature spell" gate is the inclusive
// convention).
func TestCountStaysInclusiveOutsideTheGrantWalk(t *testing.T) {
	t.Parallel()
	e := handEngine(t, card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	bear := e.G.Zone(state.ZHand, 0)[0]
	addMana(t, e, 0, "GG")
	castMode(t, e, bear, "")
	finishCast(t, e, bear)
	if got := e.SpellsCastThisTurnMatching(0, "Card.YouCtrl"); got != 1 {
		t.Fatalf("post-cast count = %d, want 1 (the cast itself counts outside the grant walk)", got)
	}
	drainEtb(t, e)
}
