package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const rememberedChangeZoneSpell = "Name:Remembered Return\nManaCost:0\nTypes:Sorcery\n" +
	"A:SP$ Effect | RememberObjects$ Targeted | ReplacementEffects$ ETBCreat | ExileOnMoved$ Graveyard | SubAbility$ Return\n" +
	"SVar:Return:DB$ ChangeZone | ValidTgts$ Creature.YouOwn | TargetingPlayer$ Opponent | Origin$ Graveyard | Destination$ Battlefield\n" +
	"SVar:ETBCreat:Event$ Moved | ValidCard$ Hero.IsRemembered | Destination$ Battlefield | ReplaceWith$ AddEntryCounter | ReplacementResult$ Updated | Description$ enters with a counter\n" +
	"SVar:AddEntryCounter:DB$ PutCounter | Defined$ ReplacedCard | CounterType$ P1P1 | ETB$ True | CounterNum$ 1\nOracle:x\n"

const rememberedChangeZoneHero = "Name:Remembered Hero\nManaCost:1 W\nTypes:Creature Hero\nPT:3/4\nOracle:x\n"

// The sub's graveyard target must be captured by the root Effect before its
// entry replacement is registered. This drives the actual ask, suspension,
// resume and move rather than testing the prefetch helper in isolation.
func TestEffectRememberedTargetFromChangeZoneSubScopesEntryReplacement(t *testing.T) {
	fixtures := []*cards.Card{card(t, rememberedChangeZoneSpell), card(t, rememberedChangeZoneHero)}
	deck0 := append([]*cards.Card(nil), fixtures...)
	deck0 = append(deck0, mountainDeck(t, 40-len(deck0))...)
	cfg := Config{Seed: 9021, Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{deck0, mountainDeck(t, 40), mountainDeck(t, 40)}, Tokens: map[string]*cards.Card{}}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	e.Advance()
	driveToStep(t, e, 1, 0, state.StepMain1)
	for _, fixture := range fixtures {
		id := findByName(e, fixture.Faces[0].Name, 0)
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: state.ZHand})
	}
	e.pending = nil
	e.Advance()
	find := func(name string) state.ObjID { return findByName(e, name, 0) }
	hero := find("Remembered Hero")
	if hero == 0 {
		t.Fatal("precondition: hero fixture missing")
	}
	if o := e.G.Obj(hero); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: hero starts in %v, want hand", o)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: hero, From: state.ZHand, To: state.ZGraveyard})
	if o := e.G.Obj(hero); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: hero is not in graveyard: %+v", o)
	}
	spell := find("Remembered Return")
	if spell == 0 {
		t.Fatal("precondition: return spell fixture missing")
	}
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: return spell starts outside hand: %+v", o)
	}

	// Cast the zero-cost spell without the convenience helper, which would
	// answer any ensuing choice before this test can inspect its ownership.
	d := e.Pending()
	if d == nil {
		t.Fatal("no priority decision")
	}
	cast := -1
	for _, option := range d.Options {
		if option.Kind == "cast" && option.Obj == spell {
			cast = option.Index
		}
	}
	if cast < 0 {
		t.Fatalf("return spell not offered: %+v", d.Options)
	}
	submitChoices(t, e, cast)

	// Resolve to the ChangeZone sub's mid-resolution target ask.
	for i := 0; i < 12; i++ {
		d = e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KChoose && d.ResumeKind == "opp_pick" {
			opponentIdx := -1
			for _, option := range d.Options {
				if option.Kind == "player" && option.Player == 2 {
					opponentIdx = option.Index
				}
			}
			if opponentIdx < 0 {
				t.Fatalf("seat 2 not offered as target chooser: %+v", d.Options)
			}
			submitChoices(t, e, opponentIdx)
			continue
		}
		if d.Kind == decision.KChoose && d.ResumeKind == "choice" {
			if d.Player != 2 {
				t.Fatalf("target chooser = %d, want selected opponent 2", d.Player)
			}
			break
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision before ChangeZone target: %+v", d)
		}
		passPriorityOnce(t, e)
	}
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choice" {
		t.Fatalf("ChangeZone target ask missing: %+v", d)
	}
	idx := -1
	for _, option := range d.Options {
		if option.Obj == hero {
			idx = option.Index
		}
	}
	if idx < 0 {
		t.Fatalf("graveyard Hero not offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)

	entered := e.G.Obj(hero)
	if entered == nil || entered.Zone != state.ZBattlefield {
		t.Fatalf("precondition: selected Hero did not return: %+v", entered)
	}
	if got := entered.Counter("P1P1"); got != 1 {
		t.Fatalf("remembered Hero counters = %d, want 1", got)
	}
	if len(activeMovedReplacements(e)) != 0 {
		t.Fatalf("entry replacement survived its source spell: %+v", activeMovedReplacements(e))
	}
	assertNoUnimplementedNote(t, e)
	replayCheck(t, e, cfg)
}

// The same shape's second corpus carrier: What Must Be Done's Release Juno
// mode is an Effect with "RememberObjects$ Targeted & Self" whose target is
// chosen by its following ChangeZone sub. Without the prefetch the
// remembered set is empty and the "two additional +1/+1 counters on it if
// it's a creature" replacement never fires; the returned legendary Hero would
// enter bare. This pins the general arm against the REAL compiled card (no
// Forge text is committed here).
func TestWhatMustBeDoneReturnsHistoricCreatureWithTwoCounters(t *testing.T) {
	reg := searchTestRegistry(t)
	ncmd := searchCorpusCard(t, reg, "What Must Be Done")
	hero := searchCorpusCard(t, reg, "Captain America, Living Legend")
	forest := searchCorpusCard(t, reg, "Forest")
	bears := searchCorpusCard(t, reg, "Grizzly Bears")
	legendary := false
	for _, typ := range hero.Faces[0].Types {
		if typ == "Legendary" {
			legendary = true
		}
	}
	if hero.Faces[0].Cmc() != 3 || !legendary {
		t.Fatalf("precondition: Captain America must be a historic 3-MV creature, got mv=%d types=%v", hero.Faces[0].Cmc(), hero.Faces[0].Types)
	}
	// Seed the deck with the spell; the opening deal may or may not put it in
	// hand (the deal follows the shuffle), so a fallback logged move below
	// guarantees it starts in hand.
	deck := make([]*cards.Card, 0, 40)
	deck = append(deck, ncmd)
	deck = append(deck, forest, forest, forest, hero)
	for len(deck) < 40 {
		deck = append(deck, bears)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = forest
	}
	cfg := seatZeroStart(Config{Seed: 9157, Names: []string{"done", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens, NameUniverse: reg.Cards})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	spellID := findByName(e, "What Must Be Done", 0)
	if spellID == 0 {
		t.Fatal("precondition: What Must Be Done fixture missing")
	}
	if z := e.G.Obj(spellID).Zone; z != state.ZHand {
		e.emit(events.Event{Kind: events.MoveZone, Obj: spellID, From: z, To: state.ZHand})
		e.pending = nil
		e.priorityRound()
	}
	if o := e.G.Obj(spellID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: What Must Be Done not in seat 0 hand: %+v", e.G.Obj(spellID))
	}
	heroID := findByName(e, "Captain America, Living Legend", 0)
	if heroID == 0 {
		t.Fatal("precondition: Captain America fixture missing")
	}
	// Seed the graveyard with a logged move from wherever it was dealt.
	from := e.G.Obj(heroID).Zone
	if from != state.ZGraveyard {
		e.emit(events.Event{Kind: events.MoveZone, Obj: heroID, From: from, To: state.ZGraveyard})
	}
	if o := e.G.Obj(heroID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: hero is not in graveyard: %+v", e.G.Obj(heroID))
	}
	// Fund 3WW and re-ask priority so the cast is offered.
	for _, r := range "WW" {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: string(r), Amount: 1})
	}
	for _, r := range "GGG" {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: string(r), Amount: 1})
	}
	e.priorityRound()

	d := e.Pending()
	if d == nil {
		t.Fatal("no priority decision")
	}
	cast := -1
	for _, option := range d.Options {
		if option.Kind == "cast" && option.Obj == spellID {
			cast = option.Index
		}
	}
	if cast < 0 {
		t.Fatalf("What Must Be Done not offered: %+v", d.Options)
	}
	submitChoices(t, e, cast)

	// The Charm asks its two modes; pick Release Juno (the DBConditionEffect
	// ChangeZone mode, labelled by the SpellDescription$ that rides its
	// DBChangeZone sub, one hop down -- the printed "Release Juno —" bullet).
	d = e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("pending = %+v, want the Charm mode ask", d)
	}
	juno := -1
	for _, option := range d.Options {
		if strings.HasPrefix(option.Label, "Release Juno") {
			juno = option.Index
		}
	}
	if juno < 0 {
		t.Fatalf("Release Juno mode not offered: %+v", d.Options)
	}
	submitChoices(t, e, juno)

	// Resolve to the sub's target ask.
	for i := 0; i < 12; i++ {
		d = e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KPriority {
			passPriorityOnce(t, e)
			continue
		}
		break
	}
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("ChangeZone target ask missing: %+v", d)
	}
	idx := -1
	for _, option := range d.Options {
		if option.Obj == heroID {
			idx = option.Index
		}
	}
	if idx < 0 {
		t.Fatalf("graveyard Hero not offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)

	entered := e.G.Obj(heroID)
	if entered == nil || entered.Zone != state.ZBattlefield {
		t.Fatalf("precondition: selected Hero did not return: %+v", entered)
	}
	if got := entered.Counter("P1P1"); got != 2 {
		t.Fatalf("returned historic creature counters = %d, want 2", got)
	}
	assertNoUnimplementedNote(t, e)
	replayCheck(t, e, cfg)
}
