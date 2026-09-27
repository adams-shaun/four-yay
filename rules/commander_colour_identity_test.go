package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// colourIdentityGame builds a two-seat game in the given format whose seat 0
// carries cmdr0 as its commander (deck index 0, so genesis moves it to the
// command zone and state.Player.Commanders names it) plus extras, and whose
// seat 1 carries cmdr1 (nil -> a mountain-only seat with no commander).
// The commander suites' protagonist is seat 0 (seatZeroStart advances the
// seed until the CR 103.1 toss starts seat 0, so the caller addresses seat 0
// by index); the effective seed travels out in cfg for replayCheck.
func colourIdentityGame(t *testing.T, seed uint64, format Format, cmdr0, cmdr1 *cards.Card, seat0 ...*cards.Card) (*Engine, Config) {
	t.Helper()
	deck0 := []*cards.Card{cmdr0}
	deck0 = append(deck0, seat0...)
	deck0 = append(deck0, mountainDeck(t, 40-len(deck0))...)
	deck1 := mountainDeck(t, 40)
	if cmdr1 != nil {
		deck1 = append([]*cards.Card{cmdr1}, deck1[:39]...)
	}
	cfg := Config{Seed: seed, Names: []string{"a", "b"},
		Decks:        [][]*cards.Card{deck0, deck1},
		Commanders:   [][]int{{0}, {}},
		Format:       format,
		StartingLife: 40}
	if cmdr1 != nil {
		cfg.Commanders[1] = []int{0}
	}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	e.Advance()
	return e, cfg
}

// moveToBattlefieldByName moves the named seeded deck card from hand or
// library to the battlefield through a LOGGED MoveZone (the moveSeeded
// pattern keyed on name instead of a script string, so log-only replay can
// reconstruct the object — onBoardCard's direct setup cannot). Only valid
// for a card colourIdentityGame seeded into the seat's deck.
func moveToBattlefieldByName(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	toMain1(t, e)
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, p) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZBattlefield})
				e.pending = nil
				return id
			}
		}
	}
	t.Fatalf("card %q not found in seat %d's library or hand", name, p)
	return 0
}

// tapForMana drives to seat 0's Main1, moves the named card onto the
// battlefield through a logged move, re-asks priority and submits the
// tap-for-mana activation for it.
func tapForMana(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	id := moveToBattlefieldByName(t, e, 0, name)
	e.priorityRound()
	activateMana(t, e, id)
	return id
}

// corpusCommander returns a real corpus legendary card by name.
func corpusCommander(t *testing.T, reg *cards.Registry, name string) *cards.Card {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("corpus has no %s", name)
	}
	return c
}

// commanderReplayCheck is the commander-suite spelling of the standard
// replay comparison. replayFromLog reconstructs genesis bookkeeping from the
// same Config and folds cast counts from the same event stream for every caller.
func commanderReplayCheck(t *testing.T, e *Engine, cfg Config) {
	t.Helper()
	replayCheck(t, e, cfg)
}

// TestCommandTowerAsksCommanderIdentityColours is the user-reported defect:
// tapping a Command Tower with a B/R commander (Valgavoth, Harrower of
// Souls) added NO mana and never asked. Produced$ Combo ColorIdentity must
// pose ONE KChoose offering exactly the commander's identity colours in
// WUBRG order — "Add B" then "Add R" — and the answered colour must be the
// one mana that lands in the pool.
func TestCommandTowerAsksCommanderIdentityColours(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	tower := corpusCommander(t, reg, "Command Tower")
	valgavoth := corpusCommander(t, reg, "Valgavoth, Harrower of Souls")
	if id := valgavoth.ColourIdentity(); id != cards.ColourBlack|cards.ColourRed {
		t.Fatalf("Valgavoth identity = %d, want B|R (%d)", id, cards.ColourBlack|cards.ColourRed)
	}
	e, cfg := colourIdentityGame(t, 77, FormatCommander, valgavoth, nil, tower)
	id := tapForMana(t, e, "Command Tower")
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 {
		t.Fatalf("Command Tower decision = %+v, want a 1-of colour choice", d)
	}
	if d.Prompt != "Choose a colour in your commander's color identity" {
		t.Fatalf("prompt = %q, want the commander-identity wording", d.Prompt)
	}
	if len(d.Options) != 2 {
		t.Fatalf("options = %+v, want exactly two identity colours", d.Options)
	}
	for i, want := range []string{"Add B", "Add R"} {
		o := d.Options[i]
		if o.Kind != "mana" || o.Obj != id || o.Label != want {
			t.Fatalf("option %d = %+v, want a mana option %q for %d", i, o, want, id)
		}
	}
	submitChoices(t, e, manaOption(t, d, "B"))
	pool := e.G.Players[0].Pool
	if pool.Total() != 1 || pool[state.MB] != 1 || pool[state.MR] != 0 || pool[state.MC] != 0 {
		t.Fatalf("pool = %v, want exactly one black and no red/colourless", pool)
	}
	commanderReplayCheck(t, e, cfg)
}

// TestCommandTowerIdentityAsksWUBRGOrderOnSixColourCommander pins the fixed
// WUBRG offer order (no map-range nondeterminism can reach the option list):
// a full-WUBRG commander offers W U B R G in that order.
func TestCommandTowerIdentityAsksWUBRGOrderOnSixColourCommander(t *testing.T) {
	cmdr := card(t, "Name:Rainbow Commander\nManaCost:W U B R G\nTypes:Legendary Creature Avatar\nPT:5/5\nOracle:x\n")
	reg := testutil.CorpusRegistry(t)
	tower := corpusCommander(t, reg, "Command Tower")
	e, _ := colourIdentityGame(t, 78, FormatCommander, cmdr, nil, tower)
	tapForMana(t, e, "Command Tower")
	d := e.Pending()
	if d == nil || len(d.Options) != 5 {
		t.Fatalf("options = %+v, want five identity colours", d)
	}
	for i, want := range []string{"Add W", "Add U", "Add B", "Add R", "Add G"} {
		if d.Options[i].Label != want {
			t.Fatalf("option %d = %q, want %q (fixed WUBRG order)", i, d.Options[i].Label, want)
		}
	}
}

// TestArcaneSignetIdentityAskIsTheSameShape pins the second corpus shape
// (an artifact rock, not a land) shares the branch: a 1-of ask over the
// identity colours, and the answer adds the chosen colour.
func TestArcaneSignetIdentityAskIsTheSameShape(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	signet := corpusCommander(t, reg, "Arcane Signet")
	valgavoth := corpusCommander(t, reg, "Valgavoth, Harrower of Souls")
	e, cfg := colourIdentityGame(t, 79, FormatCommander, valgavoth, nil, signet)
	tapForMana(t, e, "Arcane Signet")
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("Arcane Signet decision = %+v, want a two-colour identity ask", d)
	}
	submitChoices(t, e, manaOption(t, d, "R"))
	pool := e.G.Players[0].Pool
	if pool.Total() != 1 || pool[state.MR] != 1 {
		t.Fatalf("pool = %v, want exactly one red", pool)
	}
	commanderReplayCheck(t, e, cfg)
}

// TestCommandTowerSingleColourIdentityResolvesWithoutAsk: a
// single-colour commander's identity makes the choice trivial — the mana
// resolves directly (no decision is posed, a decision nobody could answer
// differently must not be asked) and adds one mana of that colour.
func TestCommandTowerSingleColourIdentityResolvesWithoutAsk(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	tower := corpusCommander(t, reg, "Command Tower")
	cmdr := card(t, "Name:Red Commander\nManaCost:1 R\nTypes:Legendary Creature Dragon\nPT:4/4\nOracle:x\n")
	e, _ := colourIdentityGame(t, 80, FormatCommander, cmdr, nil, tower)
	id := tapForMana(t, e, "Command Tower")
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("single-colour identity must not ask: %+v", d)
	}
	pool := e.G.Players[0].Pool
	if pool.Total() != 1 || pool[state.MR] != 1 {
		t.Fatalf("pool = %v, want exactly one red without an ask", pool)
	}
	_ = id
}

// TestCommandTowerColourlessCommanderStaysFailClosed (CR 903.4): a
// colourless commander has an EMPTY colour identity, and "any color" of an
// empty identity is no colour at all — adding colourless would be wrong —
// so the tap keeps today's fail-closed shape: the unhandled-Produced$ Note,
// no mana, no ask.
func TestCommandTowerColourlessCommanderStaysFailClosed(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	tower := corpusCommander(t, reg, "Command Tower")
	cmdr := card(t, "Name:Colourless Commander\nManaCost:7\nTypes:Legendary Creature Eldrazi\nPT:7/7\nOracle:x\n")
	e, _ := colourIdentityGame(t, 81, FormatCommander, cmdr, nil, tower)
	id := tapForMana(t, e, "Command Tower")
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("empty identity must not ask: %+v", d)
	}
	if pool := e.G.Players[0].Pool; pool.Total() != 0 {
		t.Fatalf("pool = %v, want nothing added for an empty identity", pool)
	}
	if !hasNote(e, "unhandled Produced$ ColorIdentity") {
		t.Fatal("empty identity kept no fail-closed unhandled-Produced$ Note")
	}
	_ = id
}

// TestCommandTowerNonCommanderGameStaysFailClosed: outside the Commander
// format (no commanders at all) the tap adds nothing and keeps the
// fail-closed Note — the pre-fix behaviour, unchanged.
func TestCommandTowerNonCommanderGameStaysFailClosed(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	tower := corpusCommander(t, reg, "Command Tower")
	e, _ := colourIdentityGame(t, 82, FormatConstructed, nil, nil, tower)
	id := tapForMana(t, e, "Command Tower")
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("no commander must not ask: %+v", d)
	}
	if pool := e.G.Players[0].Pool; pool.Total() != 0 {
		t.Fatalf("pool = %v, want nothing added with no commander", pool)
	}
	if !hasNote(e, "unhandled Produced$ ColorIdentity") {
		t.Fatal("no-commander game kept no fail-closed unhandled-Produced$ Note")
	}
	_ = id
}
