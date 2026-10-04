package rules

// Layer-5 consumption of a "choose a color" / CR 903.4b pregame choice
// (SetColor$ ChosenColor / AddColor$ ChosenColor). All seven carriers are REAL
// corpus cards; the statics they carry are exercised through the ordinary
// cast + as-enters colour ask, never an inline script.
//
// Part A: the four K:ETBReplacement:Other:ChooseColor carriers (Alloy Golem,
// Painter's Servant, Shifting Sky, Shimmerwilds Growth) -- the ask already
// worked; only the layer-5 read was missing.
// Part B: the three CharacteristicDefining$ "if CARDNAME is your commander"
// carriers (Faceless One, The Prismatic Piper, Clara Oswald) -- the CR 903.4b
// pregame ask and the commander identity union.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// castCorpusCardToBattlefield casts the named card out of seat p's hand and
// answers the as-enters colour ask with the WUBRG letter choice, driving the
// spell to resolution. It returns the battlefield object id. colour is the
// 0-based WUBRG option index (0=White .. 4=Green).
func castCorpusCardToBattlefield(t *testing.T, e *Engine, p state.PlayerID, name string, colour int) state.ObjID {
	t.Helper()
	id := moveCorpusCard(t, e, name, p, state.ZHand)
	e.pending = nil
	e.Advance()
	castFirst(t, e, "cast")
	d := passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "etb" || len(d.Options) != 5 || d.Options[0].Kind != "color" {
		t.Fatalf("%s: expected the entry colour ask, got %+v", name, d)
	}
	submitChoices(t, e, colour)
	passUntilStackEmpty(t, e, 40)
	return id
}

// TestAlloyGolemBecomesTheChosenColor pins SetColor$ ChosenColor's OVERWRITE
// shape on a real corpus card: as Alloy Golem enters the chosen colour is
// both asked and recorded, and the derived layer-5 colours are exactly that
// colour -- the printed colourless artifact-creature becomes Red, and stays
// an artifact (the setter only replaces colours).
func TestAlloyGolemBecomesTheChosenColor(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Alloy Golem")}, nil)
	addMana(t, e, 0, "CCCCCC")
	id := castCorpusCardToBattlefield(t, e, 0, "Alloy Golem", 3) // Red
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield || o.ChosenColor != "R" {
		t.Fatalf("Alloy Golem: zone %s chosen %q", o.Zone, o.ChosenColor)
	}
	if got := e.Colors(id); got != "R" {
		t.Fatalf("Alloy Golem derived colours = %q, want R (SetColor overwrite)", got)
	}
	d := e.Derived(id)
	if !containsWord(d.Types, "Artifact") || !containsWord(d.Types, "Creature") {
		t.Fatalf("Alloy Golem lost its artifact/creature types: %v", d.Types)
	}
	replayCheck(t, e, cfg)
}

// TestPaintersServantAddsTheChosenColorEverywhere pins AddColor$
// ChosenColor's EXTEND shape with AffectedZone$ All on a real corpus card: a
// battlefield permanent AND a non-battlefield card both gain the chosen
// colour in addition to their printed colours.
func TestPaintersServantAddsTheChosenColorEverywhere(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Painter's Servant")}, nil)
	// A battlefield creature (a second corpus creature) and a hand card.
	hand := moveCorpusCard(t, e, "Mountain", 0, state.ZHand)
	e.pending = nil
	e.Advance()
	addMana(t, e, 0, "CC")
	id := castCorpusCardToBattlefield(t, e, 0, "Painter's Servant", 1) // Blue
	if got := e.Colors(id); got != "U" {
		t.Fatalf("Painter's Servant derived colours = %q, want U", got)
	}
	// The hand card gains Blue in addition to its printed colourless.
	if got := e.Colors(hand); got != "U" {
		t.Fatalf("hand Mountain derived colours = %q, want U (AddColor AffectedZone All)", got)
	}
	replayCheck(t, e, cfg)
}

// TestShiftingSkyRecoloursNonlandPermanents pins SetColor$ ChosenColor with
// Affected$ Permanent.nonLand on a real corpus card: nonland permanents are
// overwritten to the chosen colour while a land keeps its (colourless)
// printed colours.
func TestShiftingSkyRecoloursNonlandPermanents(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Shifting Sky")}, nil)
	land := moveCorpusCard(t, e, "Mountain", 0, state.ZBattlefield)
	e.pending = nil
	e.Advance()
	addMana(t, e, 0, "CCU")
	sky := castCorpusCardToBattlefield(t, e, 0, "Shifting Sky", 4) // Green
	// The Sky itself is a nonland permanent, so it picks up the grant too.
	if got := e.Colors(sky); got != "G" {
		t.Fatalf("Shifting Sky derived colours = %q, want G", got)
	}
	if got := e.Colors(land); got != "" {
		t.Fatalf("Mountain derived colours = %q, want colourless (nonland only)", got)
	}
	replayCheck(t, e, cfg)
}

// TestShimmerwildsGrowthRecoloursTheEnchantedLand pins SetColor$
// ChosenColor with Affected$ Card.AttachedBy on a real corpus Aura: the
// enchanted land gains the chosen colour (an overwrite, and the land's own
// printed colour is empty so it is simply the chosen colour).
func TestShimmerwildsGrowthRecoloursTheEnchantedLand(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Shimmerwilds Growth"), lookup(t, reg, "Forest")}, nil)
	forest := moveByName(t, e, 0, "Forest", state.ZBattlefield)
	e.pending = nil
	e.Advance()
	addMana(t, e, 0, "CG")
	id := moveCorpusCard(t, e, "Shimmerwilds Growth", 0, state.ZHand)
	e.pending = nil
	e.Advance()
	castFirst(t, e, "cast")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the aura's target ask before entry, got %+v", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == forest {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("Forest not offered as the aura target: %+v", d.Options)
	}
	submitChoices(t, e, tgt)
	d = passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "etb" || len(d.Options) != 5 || d.Options[0].Kind != "color" {
		t.Fatalf("expected the aura's entry colour ask, got %+v", d)
	}
	submitChoices(t, e, 0) // White
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield || o.AttachedTo != forest || o.ChosenColor != "W" {
		t.Fatalf("Shimmerwilds Growth: zone %s attached %d chosen %q", o.Zone, o.AttachedTo, o.ChosenColor)
	}
	if got := e.Colors(forest); got != "W" {
		t.Fatalf("enchanted Forest derived colours = %q, want W", got)
	}
	replayCheck(t, e, cfg)
}

// TestChosenColorStaticWithNoChoiceFailsClosed pins the fail-closed guard: a
// characteristic-defining carrier (Faceless One) that is NOT a commander has
// no CR 903.4b pregame ask and therefore no recorded choice, so its layer-5
// SetColor$ ChosenColor static emits nothing and the object stays colourless
// rather than guessing.
func TestChosenColorStaticWithNoChoiceFailsClosed(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Faceless One")}, nil)
	id := moveByName(t, e, 0, "Faceless One", state.ZBattlefield)
	if o := e.G.Obj(id); o.ChosenColor != "" {
		t.Fatalf("precondition: ChosenColor = %q, want empty", o.ChosenColor)
	}
	if got := e.Colors(id); got != "" {
		t.Fatalf("colourless non-commander Faceless One derived colours = %q, want empty (fail closed)", got)
	}
}

// containsWord reports whether the flat type list carries the word.
func containsWord(list []string, w string) bool {
	for _, x := range list {
		if strings.EqualFold(x, w) {
			return true
		}
	}
	return false
}

// --- Part B: the CR 903.4b pregame commander colour choice ---------------

// commanderColourGame builds a two-seat Commander game whose seat 0 and
// (optionally) seat 1 carry the named corpus cards as their commanders, plus
// mountains, with the toss pinned to seat 0. The commander's own colour
// choice round is the first pending decision.
//
// The seed is advanced until e.G.StartingPlayer is 0. seatZeroStart cannot be
// used here: it probes e.G.Active after New, and the colour round now runs
// before beginTurn, so Active is still the zero value and the probe is fooled.
// The folded e.G.StartingPlayer (CR 103.1's resolution) is the reliable read.
func commanderColourGame(t *testing.T, seed uint64, cmdr0, cmdr1 *cards.Card) (*Engine, Config) {
	t.Helper()
	cmds := [][]int{{0}, {}}
	rebuild := func(s uint64) (Config, *Engine) {
		deck0 := append([]*cards.Card{cmdr0}, mountainDeck(t, 39)...)
		deck1 := mountainDeck(t, 40)
		if cmdr1 != nil {
			deck1 = append([]*cards.Card{cmdr1}, mountainDeck(t, 39)...)
			cmds[1] = []int{0}
		}
		cfg := Config{Seed: s, Names: []string{"a", "b"},
			Decks:        [][]*cards.Card{deck0, deck1},
			Commanders:   cmds,
			Format:       FormatCommander,
			StartingLife: 40,
			Tokens:       testutil.CorpusRegistry(t).Tokens}
		return cfg, New(cfg)
	}
	for {
		cfg, e := rebuild(seed)
		if e.G.StartingPlayer == 0 {
			e.Advance()
			return e, cfg
		}
		seed++
	}
}

// answerPregameColour asserts seat p holds a five-option WUBRG colour ask,
// answers it with the given 0-based WUBRG index, and returns the recorded
// letter.
func answerPregameColour(t *testing.T, e *Engine, p state.PlayerID, idx int) string {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Player != p {
		t.Fatalf("expected a pregame colour ask for seat %d, got %+v", p, d)
	}
	if d.Min != 1 || d.Max != 1 || len(d.Options) != 5 {
		t.Fatalf("pregame colour ask shape: %+v", d)
	}
	for i, want := range []string{"White", "Blue", "Black", "Red", "Green"} {
		if d.Options[i].Kind != "color" || d.Options[i].Label != want {
			t.Fatalf("option %d %+v, want color %q in WUBRG order", i, d.Options[i], want)
		}
	}
	submitChoices(t, e, idx)
	return "WUBRG"[idx : idx+1]
}

// TestCommanderPregameColourChoices pins the CR 903.4b ask and its
// consumption on the real corpus carriers: the choice is posed at game start,
// recorded on the commander object as the ETB ask's own Choose "color" event,
// read into the commander's colour identity (the mana-ask path's source), and
// -- once the commander is cast -- into its derived layer-5 colours.
func TestCommanderPregameColourChoices(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	cases := []struct {
		name string
		card string
		idx  int // 0=W..4=G
		want string
	}{
		{"FacelessOne", "Faceless One", 2, "B"},
		{"PrismaticPiper", "The Prismatic Piper", 3, "R"},
		{"ClaraOswald", "Clara Oswald", 0, "W"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card := lookup(t, reg, tc.card)
			e, cfg := commanderColourGame(t, 101, card, nil)
			cmd := e.G.Players[0].Commanders[0]
			got := answerPregameColour(t, e, 0, tc.idx)
			if got != tc.want {
				t.Fatalf("answered letter %q", got)
			}
			if o := e.G.Obj(cmd); o.ChosenColor != tc.want {
				t.Fatalf("%s ChosenColor = %q, want %q", tc.card, o.ChosenColor, tc.want)
			}
			// The identity union reads the recorded choice even in the command
			// zone (the mana-ask path's "any color in your commander's color
			// identity" read).
			if cols := pay.CommanderIdentityColours(asPayer(e), 0); !containsWord(cols, tc.want) {
				t.Fatalf("%s identity colours %v missing %q", tc.card, cols, tc.want)
			}
			// The whole pregame round (the recorded choice, the identity union)
			// replays from the log. The check reads Commanders off the command
			// zone, so it runs before the commander leaves it.
			commanderReplayCheck(t, e, cfg)
			// Drive to Main 1 and cast the commander: once on the battlefield
			// its layer-5 SetColor$ ChosenColor static applies.
			toMain1(t, e)
			addMana(t, e, 0, "CCCCCC")
			submitChoices(t, e, castModeOption(t, e, cmd, ""))
			passUntilStackEmpty(t, e, 40)
			if o := e.G.Obj(cmd); o.Zone != state.ZBattlefield {
				t.Fatalf("%s zone %s, want battlefield", tc.card, o.Zone)
			}
			if got := e.Colors(cmd); got != tc.want {
				t.Fatalf("%s derived colours = %q, want %q", tc.card, got, tc.want)
			}
		})
	}
}

// TestPregameCommanderColourOneAskPerSeatInTurnOrder pins that BOTH seats are
// asked, once each, in AliveFrom(StartingPlayer) order (the toss is pinned to
// seat 0, so seat 0 is asked first), and that each answer lands on that seat's
// own commander.
func TestPregameCommanderColourOneAskPerSeatInTurnOrder(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	a := lookup(t, reg, "Faceless One")
	b := lookup(t, reg, "The Prismatic Piper")
	e, _ := commanderColourGame(t, 102, a, b)
	if d := e.Pending(); d == nil || d.Player != 0 {
		t.Fatalf("first ask should be seat 0, got %+v", d)
	}
	answerPregameColour(t, e, 0, 1) // Blue for seat 0
	if d := e.Pending(); d == nil || d.Player != 1 || d.Kind != decision.KChoose {
		t.Fatalf("second ask should be seat 1's colour ask, got %+v", d)
	}
	answerPregameColour(t, e, 1, 4) // Green for seat 1
	if got := e.G.Obj(e.G.Players[0].Commanders[0]).ChosenColor; got != "U" {
		t.Fatalf("seat 0 commander ChosenColor = %q, want U", got)
	}
	if got := e.G.Obj(e.G.Players[1].Commanders[0]).ChosenColor; got != "G" {
		t.Fatalf("seat 1 commander ChosenColor = %q, want G", got)
	}
	// Exactly one Choose "color" event per commander.
	for _, p := range []state.PlayerID{0, 1} {
		cmd := e.G.Players[p].Commanders[0]
		if n := countEvents(e, func(ev events.Event) bool {
			return ev.Kind == events.Choose && ev.Obj == cmd && ev.Counter == "color"
		}); n != 1 {
			t.Fatalf("seat %d commander got %d colour events, want 1", p, n)
		}
	}
}

// TestNonCommanderCDANoPregameAsk pins that the round opens ONLY for a
// configured commander: Faceless One in an ordinary deck is never asked and
// stays colourless (the fail-closed guard for the layer-5 static once it is
// on the battlefield with no recorded choice).
func TestNonCommanderCDANoPregameAsk(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Faceless One")}, nil)
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("a non-commander Faceless One must pose no colour ask, got %+v", d)
	}
	id := moveByName(t, e, 0, "Faceless One", state.ZBattlefield)
	if o := e.G.Obj(id); o.ChosenColor != "" {
		t.Fatalf("ChosenColor = %q, want empty", o.ChosenColor)
	}
	if got := e.Colors(id); got != "" {
		t.Fatalf("non-commander Faceless One derived colours = %q, want colourless", got)
	}
}
