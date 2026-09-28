package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Corpus pins for Produced$ Special EachColorAmong_Valid <spec>
// (effects/misc.go's effMana): the deterministic "for each color among
// <matching> permanents you control" batch. Faeburrow Elder is the reported
// carrier; Tarnation Vista's second ability carries the +MonoColor variant,
// which is what put the MonoColor predicate into effects/filter.go. Every
// card here is a real corpus card via the registry; the coloured board
// occupants are inline synthetic fixtures (never corpus .txt, per the
// licensing rule).

// monoFixtureSrc is an inline mono-<colour> creature used as a plain
// permanent that contributes exactly one colour to an EachColorAmong set.
func monoFixtureSrc(colour string) string {
	return "Name:Mono" + colour + " Testee\nManaCost:2 " + colour +
		"\nTypes:Creature Bird\nPT:1/1\nOracle:x\n"
}

// multicolourFixtureSrc is an inline two-colour creature: a permanent whose
// colours must NOT reach a +MonoColor EachColorAmong set.
func multicolourFixtureSrc(a, b string) string {
	return "Name:Dual" + a + b + " Testee\nManaCost:1 " + a + " " + b +
		"\nTypes:Creature Bird\nPT:1/1\nOracle:x\n"
}

// colorlessFixture is an inline colourless artifact creature with the
// EachColorAmong ability itself: the zero-colour silent no-op pin (its own
// controller's permanents are all colourless, so the batch adds nothing and
// emits no Note) and, with its Produced$ rewritten, the unknown-selector
// loud-Note boundary.
func colorlessFixture(produced string) string {
	return "Name:Colorless Sifter\nManaCost:3\nTypes:Artifact Creature\n" +
		"A:AB$ Mana | Cost$ T | Produced$ " + produced + "\nOracle:x\n"
}

// tapForManaAfterTurnClears places name onto seat 0's battlefield like
// tapForMana (commander_colour_identity_test.go), but the source here is a
// CREATURE mana ability: CR 302.6 blocks a same-turn {T} activation, and a
// raw SummonSick field write would clear it OFF the log a replayCheck below
// would then miss. A real logged TurnChange for seat 0 gives it a legitimate
// "been under control since this [new] turn began" the same way the repo's
// existing driveToStep-based fixtures do (e.g. the CumulativeUpkeep corpus
// pin in restrict_valid_dotless_test.go), so the activation this drives
// stays byte-for-byte replayable.
func tapForManaAfterTurnClears(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	id := moveToBattlefieldByName(t, e, 0, name)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
	e.priorityRound()
	activateMana(t, e, id)
	return id
}

// eachColorGame builds a two-seat game whose seat 0 is seeded with cards
// plus mountains, at the shape newFixtureDeck uses (no commanders, the CR
// 103.1 toss advanced until seat 0 starts).
func eachColorGame(t *testing.T, seed uint64, seat0 []*cards.Card) (*Engine, Config) {
	t.Helper()
	deck0 := append(append([]*cards.Card{}, seat0...), mountainDeck(t, 40-len(seat0))...)
	cfg := Config{Seed: seed, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{deck0, mountainDeck(t, 40)},
		Tokens: map[string]*cards.Card{}}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	e.Advance()
	return e, cfg
}

// poolIs asserts seat 0's pool exactly.
func poolIs(t *testing.T, e *Engine, want [state.MC + 1]int32) {
	t.Helper()
	pool := e.G.Players[0].Pool
	for i, w := range want {
		if pool[i] != w {
			t.Fatalf("pool slot %d = %d, want %d (pool %v)", i, pool[i], w, pool)
		}
	}
}

// notes reports the engine's Note event texts.
func notes(t *testing.T, e *Engine) []string {
	t.Helper()
	var out []string
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note {
			out = append(out, ev.Text)
		}
	}
	return out
}

// noteContaining reports whether any Note mentions the substring.
func noteContaining(t *testing.T, e *Engine, sub string) bool {
	t.Helper()
	for _, n := range notes(t, e) {
		if strings.Contains(n, sub) {
			return true
		}
	}
	return false
}

// TestFaeburrowElderEachColorAmong is the reported defect: the Elder's
// "{T}: For each color among permanents you control, add one mana of that
// color" emitted a loud Note and NO mana. Alone on the battlefield it owes
// {G}{W} (its own face cost is what ColorsOf reads), so the tap must add
// exactly one G and one W; with an extra mono-blue permanent in play the
// same tap must add a U as well.
func TestFaeburrowElderEachColorAmong(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	elder := corpusCommander(t, reg, "Faeburrow Elder")

	e, cfg := eachColorGame(t, 91, []*cards.Card{elder})
	tapForManaAfterTurnClears(t, e, "Faeburrow Elder")
	poolIs(t, e, [state.MC + 1]int32{state.MW: 1, state.MG: 1})
	replayCheck(t, e, cfg)

	// A mono-blue permanent joins the board: the batch is one unit per
	// DISTINCT colour, so U joins without doubling G or W.
	e2, cfg2 := eachColorGame(t, 91, []*cards.Card{elder,
		card(t, monoFixtureSrc("U"))})
	toMain1(t, e2)
	moveToBattlefieldByName(t, e2, 0, "MonoU Testee")
	tapForManaAfterTurnClears(t, e2, "Faeburrow Elder")
	poolIs(t, e2, [state.MC + 1]int32{state.MW: 1, state.MU: 1, state.MG: 1})
	replayCheck(t, e2, cfg2)

	// Shared-colour dedupe: a mono-G permanent joins the board. The set is a
	// UNION of the matching permanents' colours, so a colour the Elder
	// already contributes is not doubled — G=1 W=1, not G=2 W=1.
	e3, cfg3 := eachColorGame(t, 91, []*cards.Card{elder,
		card(t, monoFixtureSrc("G"))})
	toMain1(t, e3)
	moveToBattlefieldByName(t, e3, 0, "MonoG Testee")
	tapForManaAfterTurnClears(t, e3, "Faeburrow Elder")
	poolIs(t, e3, [state.MC + 1]int32{state.MW: 1, state.MG: 1})
	replayCheck(t, e3, cfg3)
}

// TestTarnationVistaEachColorAmongMonoColor pins the +MonoColor variant: the
// Vista's {1},{T} adds one mana per MONOCOLOURED permanent's colour only --
// a multicoloured permanent contributes nothing even when its colours are
// not otherwise present, and the colourless Vista and mountains contribute
// nothing either (colourless is not a colour and not monocolored).
func TestTarnationVistaEachColorAmongMonoColor(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	vista := corpusCommander(t, reg, "Tarnation Vista")
	monoG := card(t, monoFixtureSrc("G"))
	monoW := card(t, monoFixtureSrc("W"))
	dualUR := card(t, multicolourFixtureSrc("U", "R"))

	e, cfg := eachColorGame(t, 92, []*cards.Card{vista, monoG, monoW, dualUR})
	toMain1(t, e)
	vid := moveToBattlefieldByName(t, e, 0, "Tarnation Vista")
	// CR 305.7 in reverse: the Vista enters tapped via its own ETB
	// replacement, so the test clears the tap with a logged Untap before it
	// activates the {1},{T} ability.
	e.emit(events.Event{Kind: events.Untap, Obj: vid})
	moveToBattlefieldByName(t, e, 0, "MonoG Testee")
	moveToBattlefieldByName(t, e, 0, "MonoW Testee")
	moveToBattlefieldByName(t, e, 0, "DualUR Testee")
	// Fund the {1} generic; the tap is the ability's own cost. Mana
	// abilities surface as "activate" options on the pending priority
	// decision; a permanent with two mana abilities then poses a KChoose
	// naming which one to activate — answer with the EachColorAmong one
	// (option Ability 1, the {1},{T} ability; option 0 is the out-of-scope
	// Produced$ Chosen tap).
	addMana(t, e, 0, "C")
	for _, o := range e.Pending().Options {
		if o.Kind == "activate" && o.Obj == vid {
			submitChoices(t, e, o.Index)
		}
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("after activating the Vista: %+v, want a mana-ability choose", d)
	}
	var opt decision.Option
	found := false
	for _, o := range d.Options {
		if o.Kind == "mana" && strings.Contains(o.Label, "EachColorAmong") {
			opt, found = o, true
		}
	}
	if !found {
		t.Fatalf("no EachColorAmong mana option: %+v", d.Options)
	}
	submitChoices(t, e, opt.Index)
	poolIs(t, e, [state.MC + 1]int32{state.MW: 1, state.MG: 1})
	replayCheck(t, e, cfg)
}

// TestMonoColorPredicate pins the filter predicate itself: exactly one
// colour is MonoColor; zero colours (colourless) and two colours are not.
func TestMonoColorPredicate(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	elder := corpusCommander(t, reg, "Faeburrow Elder")
	monoG := card(t, monoFixtureSrc("G"))
	colorless := card(t, colorlessFixture("C"))

	e, _ := eachColorGame(t, 93, []*cards.Card{elder, monoG, colorless})
	// The predicate is read off the face wherever the object is, so the
	// spec is Card.MonoColor rather than Permanent.MonoColor (a Permanent
	// base additionally requires the battlefield).
	m := func(c *cards.Card) state.ObjID {
		for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
			for _, id := range e.G.Zone(z, 0) {
				if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == c.Faces[0].Name {
					return id
				}
			}
		}
		t.Fatalf("card %q not seeded", c.Faces[0].Name)
		return 0
	}
	if !effects.MatchesSpec(e.G, "Card.MonoColor", m(monoG), 0) {
		t.Fatalf("mono-green fixture does not match MonoColor")
	}
	if effects.MatchesSpec(e.G, "Card.MonoColor", m(colorless), 0) {
		t.Fatalf("colourless fixture wrongly matches MonoColor")
	}
	if effects.MatchesSpec(e.G, "Card.MonoColor", m(elder), 0) {
		t.Fatalf("two-coloured Elder wrongly matches MonoColor")
	}
	if !effects.MatchesSpec(e.G, "Card.MultiColor", m(elder), 0) {
		t.Fatalf("two-coloured Elder does not match MultiColor")
	}
}

// TestMonoColorWidensRealCorpusSpecs pins the generic predicate against a
// REAL corpus card whose spec previously matched NOTHING (unknown predicate
// failed closed): Ultimate Price's ValidTgts$ Creature.MonoColor read from
// the compiled registry card itself — it now matches a monocoloured creature
// and rejects a multicoloured one, and UnknownPredicates no longer reports
// the word. Tarnation Vista is the carrier the ticket's fix needed; Ultimate
// Price is one of the ~16 other corpus files the same predicate widened.
func TestMonoColorWidensRealCorpusSpecs(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	price, ok := reg.Lookup("Ultimate Price")
	if !ok {
		t.Fatalf("Ultimate Price not in the compiled corpus")
	}
	var valid string
	for _, ab := range price.Faces[0].Abilities {
		if v, ok := ab.Params["ValidTgts"]; ok && strings.Contains(v, "MonoColor") {
			valid = v
		}
	}
	if valid == "" {
		t.Fatalf("Ultimate Price carries no MonoColor ValidTgts in the compiled corpus")
	}

	monoG := card(t, monoFixtureSrc("G"))
	dualUR := card(t, multicolourFixtureSrc("U", "R"))
	e, _ := eachColorGame(t, 96, []*cards.Card{monoG, dualUR})
	idOf := func(c *cards.Card) state.ObjID {
		for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
			for _, id := range e.G.Zone(z, 0) {
				if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == c.Faces[0].Name {
					return id
				}
			}
		}
		t.Fatalf("card %q not seeded", c.Faces[0].Name)
		return 0
	}
	if !effects.MatchesSpec(e.G, valid, idOf(monoG), 0) {
		t.Fatalf("Ultimate Price's %q does not match a monocoloured creature", valid)
	}
	if effects.MatchesSpec(e.G, valid, idOf(dualUR), 0) {
		t.Fatalf("Ultimate Price's %q matches a multicoloured creature", valid)
	}
	for _, p := range effects.UnknownPredicates(valid) {
		if p == "MonoColor" {
			t.Fatalf("MonoColor still reported unknown for %q", valid)
		}
	}
}

// TestEachColorAmongEmptySetIsSilentNoOp pins the empty-set shape: a
// controller whose permanents are ALL colourless adds nothing and emits NO
// Note -- "for each color" over none is a deterministic no-op, not a
// failure (the ChangeNum$ 0 Dig convention).
func TestEachColorAmongEmptySetIsSilentNoOp(t *testing.T) {
	t.Parallel()
	e, cfg := eachColorGame(t, 94, []*cards.Card{card(t, colorlessFixture("Special EachColorAmong_Valid Permanent.YouCtrl"))})
	id := tapForManaAfterTurnClears(t, e, "Colorless Sifter")
	poolIs(t, e, [state.MC + 1]int32{})
	if noteContaining(t, e, "unhandled Produced$") {
		t.Fatalf("empty colour set emitted a loud Note: %v", notes(t, e))
	}
	replayCheck(t, e, cfg)
	_ = id
}

// TestEachColorAmongUnknownSelectorStaysLoud pins the fail-closed boundary:
// a Special selector the executor does not resolve (here ExiledWith, the
// sunbird_effigy shape) keeps the loud Note and adds no mana.
func TestEachColorAmongUnknownSelectorStaysLoud(t *testing.T) {
	t.Parallel()
	e, _ := eachColorGame(t, 95, []*cards.Card{card(t, colorlessFixture("Special EachColorAmong_ExiledWith"))})
	tapForManaAfterTurnClears(t, e, "Colorless Sifter")
	if !noteContaining(t, e, "unhandled Produced$") {
		t.Fatalf("unknown Special selector did not emit the loud Note: %v", notes(t, e))
	}
	poolIs(t, e, [state.MC + 1]int32{})
}
