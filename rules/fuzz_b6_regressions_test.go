package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Regressions for the cardfuzz batch6 livelocks, hang and panic (all real
// corpus cards).

// handLands counts the land cards in seat p's hand.
func handLands(e *Engine, p state.PlayerID) int {
	n := 0
	for _, id := range e.G.Zone(state.ZHand, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().IsLand() {
			n++
		}
	}
	return n
}

// TestGrantedRetraceChargesTheLandDiscard (cardfuzz batch6 line 1): Six
// grants retrace to the nonland permanent cards in its controller's
// graveyard during their turn. The offer gate read the DERIVED keyword and
// offered the graveyard cast, but beginCast's "retrace" cost fold read the
// printed face, found no Retrace and charged no discard -- so a {0} Jeweled
// Lotus was cast from the graveyard, sacrificed for mana and recast for free
// forever. The cast now charges the additional "discard a land card".
func TestGrantedRetraceChargesTheLandDiscard(t *testing.T) {
	t.Parallel()
	e, cfg := b5Engine(t, "Six", "Jeweled Lotus")
	searchMoveByName(t, e, "Six", state.ZBattlefield)
	lotus := searchMoveByName(t, e, "Jeweled Lotus", state.ZGraveyard)
	lands := handLands(e, 0)
	if lands == 0 {
		t.Fatal("precondition: no land in seat 0's opening hand to discard")
	}
	idx := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == lotus && o.Mode == "retrace" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("granted retrace cast of Jeweled Lotus not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, idx)
	for i := 0; i < 20 && (e.Pending().Kind != decision.KPriority || len(e.G.Stack) > 0); i++ {
		if e.Pending().Kind == decision.KPriority {
			submitPass(t, e)
			continue
		}
		submitChoices(t, e, 0) // the discard pick (any land)
	}
	if z := e.G.Obj(lotus).Zone; z != state.ZBattlefield {
		t.Fatalf("retraced Jeweled Lotus rests in %v, want battlefield", z)
	}
	if got := handLands(e, 0); got != lands-1 {
		t.Fatalf("hand lands after the retrace cast = %d, want %d (the land discard was not charged)", got, lands-1)
	}
	replayCheck(t, e, cfg)
}

// TestTokenElectionSuspendsTheResolvingAbility (cardfuzz batch6 line 4):
// Mirrormind Crown's "the first time you would create one or more tokens
// each turn, you may instead create copies of equipped creature" is an
// election posed from inside the token event. It was posed with a bare ask,
// so the resolving ability (Vivien, Monsters' Advocate's +1) kept running:
// its PutCounter sub asked its own counter-type question over the election,
// the election's flow consumed that answer, and the ability stayed on the
// stack with nothing to finish it -- resolveTop re-resolved it on every
// priority pass, minting a Beast each time (thousands, a wall-clock hang in
// the O(n^2) static sweeps). The election now suspends the resolution; the
// ability resolves exactly once and leaves the stack.
func TestTokenElectionSuspendsTheResolvingAbility(t *testing.T) {
	t.Parallel()
	vivien := tokenReplCorpusCard(t, "Vivien, Monsters' Advocate")
	crown := tokenReplCorpusCard(t, "Mirrormind Crown")
	bears := tokenReplCorpusCard(t, "Grizzly Bears")
	e, cfg := tokenReplGame(t, 71, vivien, crown, bears)
	viv := moveSeededCard(t, e, 0, vivien, state.ZBattlefield)
	cr := moveSeededCard(t, e, 0, crown, state.ZBattlefield)
	bear := moveSeededCard(t, e, 0, bears, state.ZBattlefield)
	e.emit(events.Event{Kind: events.Attach, Obj: cr, IDs: []state.ObjID{bear}})

	addMana(t, e, 0, "")
	submitChoices(t, e, abilityOption(t, e, viv, 0).Index)
	elections := 0
	for i := 0; i < 40; i++ {
		d := e.Pending()
		if d.Kind == decision.KPriority && len(e.G.Stack) == 0 {
			break
		}
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e)
		case decision.KChoose:
			if d.Source == cr {
				elections++
			}
			submitChoices(t, e, 0) // decline the copy; first counter kind
		default:
			t.Fatalf("unexpected %s decision: %+v", d.Kind, d)
		}
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("Vivien's +1 never left the stack (depth %d)", len(e.G.Stack))
	}
	if elections != 1 {
		t.Fatalf("Mirrormind Crown's election posed %d times, want 1", elections)
	}
	if got := countTokensNamedOnSeat(t, e, 0, "Beast Token"); got != 1 {
		t.Fatalf("Vivien's +1 made %d Beast Tokens, want exactly 1", got)
	}
	resolves := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Resolve && e.G.Obj(ev.Obj) != nil && e.G.Obj(ev.Obj).Source == viv {
			resolves++
		}
	}
	if resolves != 1 {
		t.Fatalf("Vivien's +1 resolved %d times, want 1", resolves)
	}
	replayCheck(t, e, cfg)
}

// TestOpeningEntryChoiceDoesNotOverwriteTheOpeningRound (cardfuzz batch6
// line 8): beginning the game with Leyline of Transformation on the
// battlefield poses its "as this enters, choose a creature type" through
// Engine.Ask. The opening round then posed the NEXT "begin the game with"
// ask on top of it and ask's overwrite guard panicked at intent 0. The round
// now waits for the entry choice and steps on once it is answered.
func TestOpeningEntryChoiceDoesNotOverwriteTheOpeningRound(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	leyline := searchCorpusCard(t, reg, "Leyline of Transformation")
	plains := searchCorpusCard(t, reg, "Plains")
	deck := func(withLeylines bool) []*cards.Card {
		out := make([]*cards.Card, 40)
		for i := range out {
			out[i] = plains
			if withLeylines && i < 20 {
				out[i] = leyline
			}
		}
		return out
	}
	for seed := uint64(1); seed < 200; seed++ {
		cfg := Config{Seed: seed, Names: []string{"a", "b"},
			Decks: [][]*cards.Card{deck(true), deck(false)}, Tokens: reg.Tokens}
		e := New(cfg)
		inHand := 0
		for _, id := range e.G.Zone(state.ZHand, 0) {
			if e.G.Obj(id).Face().Name == "Leyline of Transformation" {
				inHand++
			}
		}
		d := e.Pending()
		if inHand < 2 || d == nil || len(d.Options) == 0 || d.Options[0].Kind != "opening_yes" {
			continue
		}
		entryChoices := 0
		for i := 0; i < 40 && e.G.Turn == 0; i++ {
			d := e.Pending()
			if d == nil {
				t.Fatal("no decision pending during the opening round")
			}
			if len(d.Options) > 0 && d.Options[0].Kind != "opening_yes" && d.Kind == decision.KChoose {
				entryChoices++
			}
			submitChoices(t, e, 0) // yes to every opening offer; first creature type
		}
		if e.G.Turn == 0 {
			t.Fatal("the opening round never finished")
		}
		onBF := 0
		for _, id := range e.G.Zone(state.ZBattlefield, 0) {
			if e.G.Obj(id).Face().Name == "Leyline of Transformation" {
				onBF++
			}
		}
		if onBF != inHand || entryChoices != inHand {
			t.Fatalf("%d Leylines in hand: %d began on the battlefield, %d entry choices answered", inHand, onBF, entryChoices)
		}
		replayCheck(t, e, cfg)
		return
	}
	t.Fatal("could not construct an opening hand with two Leylines of Transformation")
}

// TestCleanupDamageRemovalIsNotDamageDone (cardfuzz batch6 line 9): the
// cleanup step removes marked damage with a negative Damage event
// (CR 514.2). Two Ghosts of the Innocent's DamageDone halving replacements
// matched that removal: each cleanup posed a CR 616.1 order ask per damaged
// permanent, the halved removal left damage marked, and cleanup repeated
// forever. A non-positive Damage event is not damage dealt, so no DamageDone
// replacement applies and the damage wears off.
func TestCleanupDamageRemovalIsNotDamageDone(t *testing.T) {
	t.Parallel()
	e, cfg := b5Engine(t, "Ghosts of the Innocent", "Ghosts of the Innocent", "Grizzly Bears")
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	e.emit(events.Event{Kind: events.Damage, Obj: bear, Amount: 1})
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if o := e.G.Obj(id); o.Face().Name == "Ghosts of the Innocent" {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
		}
	}
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(id); o.Face().Name == "Ghosts of the Innocent" {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
		}
	}
	e.pending = nil
	e.priorityRound()
	if e.G.Obj(bear).Damage != 1 {
		t.Fatalf("precondition: bear marked damage = %d, want 1", e.G.Obj(bear).Damage)
	}
	for i := 0; i < 60 && e.G.Turn == 1; i++ {
		d := e.Pending()
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e)
		case decision.KReplacement:
			t.Fatalf("a DamageDone replacement was asked about the cleanup damage removal: %+v", d)
		default:
			submitChoices(t, e, 0)
		}
	}
	if e.G.Turn == 1 {
		t.Fatal("turn 1 never ended")
	}
	if got := e.G.Obj(bear).Damage; got != 0 {
		t.Fatalf("bear still has %d damage marked after cleanup", got)
	}
	replayCheck(t, e, cfg)
}
