package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// CR 702.70 Training: "Whenever this creature attacks with another creature
// with greater power, put a +1/+1 counter on this creature." cards/kw_training.go
// expands a printed K:Training into an Attacks trigger carrying Training$ True,
// and rules/trigmatch/combat.go's AttacksMatches reads the event-relative
// condition. The granted (layer-6 AddKeyword$) form is synthesized by
// checkGrantedTrainingTriggers, the Dethrone precedent.

// trainingDeck builds a two-seat game whose seat 0 deck is the given real
// corpus cards (padded with Mountains) and whose seat 1 deck is Mountains.
// cfg.Tokens is returned non-nil and shared with e.G.Tokens, so a token
// minted after New is visible to replayCheck's log-only rebuild.
func trainingDeck(t *testing.T, seed uint64, deck ...*cards.Card) (*Engine, Config) {
	t.Helper()
	if len(deck) > 40 {
		t.Fatalf("trainingDeck: %d cards exceeds the 40-card deck size", len(deck))
	}
	seat0 := append([]*cards.Card{}, deck...)
	seat0 = append(seat0, mountainDeck(t, 40-len(seat0))...)
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"trainee", "other"},
		Decks:  [][]*cards.Card{seat0, mountainDeck(t, 40)},
		Tokens: map[string]*cards.Card{},
	})
	return New(cfg), cfg
}

// battlefieldID finds seat 0's object with the given face name.
func battlefieldID(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == 0 && o.Face() != nil && o.Face().Name == name {
			return o.ID
		}
	}
	t.Fatalf("seat 0 has no object named %q", name)
	return 0
}

// TestTrainingCounterOnAttackWithABiggerCreature drives a real corpus Training
// creature (Gryff Rider, a 2/1 with Training) attacking alongside a strictly
// bigger creature (Craw Wurm, 6/4): the trainee accrues exactly one +1/+1
// counter. It also pins the two negative shapes: attacking alone, and
// attacking with an EQUAL-power creature, both put no counter on it.
func TestTrainingCounterOnAttackWithABiggerCreature(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	rider := mustCorpusCard(t, reg, "Gryff Rider")
	wurm := mustCorpusCard(t, reg, "Craw Wurm")
	if d := rider.Link(); len(d) != 0 {
		t.Fatalf("link Gryff Rider: %v", d)
	}
	if d := wurm.Link(); len(d) != 0 {
		t.Fatalf("link Craw Wurm: %v", d)
	}
	if !rider.Faces[0].HasKeyword("Training") {
		t.Fatal("Gryff Rider does not print Training in the corpus")
	}
	e, cfg := trainingDeck(t, 201, rider, wurm)
	riderID := battlefieldID(t, e, "Gryff Rider")
	wurmID := battlefieldID(t, e, "Craw Wurm")
	e.emit(events.Event{Kind: events.MoveZone, Obj: riderID, From: state.ZHand, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.MoveZone, Obj: wurmID, From: state.ZHand, To: state.ZBattlefield})

	// Attacks alone: no other attacker, so no counter.
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{riderID}})
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(riderID).Counter("P1P1"); got != 0 {
		t.Fatalf("training alone gave %d counters, want 0", got)
	}

	// Attacks with a bigger creature: exactly one counter.
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{riderID, wurmID}})
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(riderID).Counter("P1P1"); got != 1 {
		t.Fatalf("training with a bigger creature gave %d counters, want 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestTrainingIgnoresAnEqualPowerAttacker pins CR 702.70's STRICTLY greater
// requirement, and that the counter goes on the trainee, not the bigger
// creature: two 2/1 Gryff Riders attack together and neither trains.
func TestTrainingIgnoresAnEqualPowerAttacker(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	rider := mustCorpusCard(t, reg, "Gryff Rider")
	e, cfg := trainingDeck(t, 202, rider, rider)
	ids := []state.ObjID{battlefieldID(t, e, "Gryff Rider")}
	// The second copy is the same face; find it by excluding the first.
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == 0 && o.Face() != nil && o.Face().Name == "Gryff Rider" && o.ID != ids[0] {
			ids = append(ids, o.ID)
			break
		}
	}
	if len(ids) != 2 {
		t.Fatalf("found %d Gryff Riders, want 2", len(ids))
	}
	for _, id := range ids {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: ids})
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	for _, id := range ids {
		if got := e.G.Obj(id).Counter("P1P1"); got != 0 {
			t.Fatalf("equal-power attacker trained: %d counters, want 0", got)
		}
	}
	replayCheck(t, e, cfg)
}

// TestGrantedTrainingOnATokenUsesElderArthurMaxson drives Elder Arthur
// Maxson's real static script ("Creature tokens you control have training").
// The minted Cat Token prints no Training, so the counter can only come from
// the layer-6 grant and its synthesized trigger
// (checkGrantedTrainingTriggers).
func TestGrantedTrainingOnATokenUsesElderArthurMaxson(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	maxson := mustCorpusCard(t, reg, "Elder Arthur Maxson")
	wurm := mustCorpusCard(t, reg, "Craw Wurm")
	if d := maxson.Link(); len(d) != 0 {
		t.Fatalf("link Elder Arthur Maxson: %v", d)
	}
	if d := wurm.Link(); len(d) != 0 {
		t.Fatalf("link Craw Wurm: %v", d)
	}
	cat := reg.Tokens["w_2_2_cat"]
	if cat == nil {
		t.Fatal("corpus has no w_2_2_cat token")
	}
	if cat.Faces[0].HasKeyword("Training") {
		t.Fatal("the Cat Token must not print Training for this grant test")
	}
	e, cfg := trainingDeck(t, 203, maxson, wurm)
	maxsonID := battlefieldID(t, e, "Elder Arthur Maxson")
	wurmID := battlefieldID(t, e, "Craw Wurm")
	e.emit(events.Event{Kind: events.MoveZone, Obj: maxsonID, From: state.ZHand, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.MoveZone, Obj: wurmID, From: state.ZHand, To: state.ZBattlefield})

	// Mint the Cat Token onto seat 0's battlefield under Maxson. Runtime token
	// registration belongs to the engine; the replay Config needs the same
	// definition independently because New isolates its token map.
	setFixtureToken(e, "w_2_2_cat", cat)
	cfg.Tokens["w_2_2_cat"] = cat
	e.emit(events.Event{Kind: events.TokenCreate, Player: 0, Text: "w_2_2_cat"})
	catID := e.G.NextID - 1
	if e.G.Obj(catID) == nil || e.G.Obj(catID).Zone != state.ZBattlefield {
		t.Fatalf("Cat Token not on the battlefield: %+v", e.G.Obj(catID))
	}
	if !e.G.Obj(catID).IsToken {
		t.Fatal("Cat Token is not a token object")
	}
	if !e.HasKeyword(catID, "Training") {
		t.Fatal("Elder Arthur Maxson did not grant the token Training")
	}

	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{catID, wurmID}})
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(catID).Counter("P1P1"); got != 1 {
		t.Fatalf("granted Training on the token gave %d counters, want 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestTrainingGrantAndPrintedKeywordDoNotStack pins the dedup: a token that
// PRINTS Training (the Human Soldier token Torens/Rural Recruit create) and is
// also granted it by Elder Arthur Maxson fires exactly ONE trigger. Without
// the printed-keyword skip in checkGrantedTrainingTriggers the token would
// carry the printed expansion AND the synthesized one and train twice.
func TestTrainingGrantAndPrintedKeywordDoNotStack(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	maxson := mustCorpusCard(t, reg, "Elder Arthur Maxson")
	wurm := mustCorpusCard(t, reg, "Craw Wurm")
	token := reg.Tokens["gw_1_1_human_soldier_training"]
	if token == nil {
		t.Fatal("corpus has no gw_1_1_human_soldier_training token")
	}
	if !token.Faces[0].HasKeyword("Training") {
		t.Fatal("the Human Soldier token must PRINT Training for this dedup test")
	}
	e, cfg := trainingDeck(t, 205, maxson, wurm)
	maxsonID := battlefieldID(t, e, "Elder Arthur Maxson")
	wurmID := battlefieldID(t, e, "Craw Wurm")
	e.emit(events.Event{Kind: events.MoveZone, Obj: maxsonID, From: state.ZHand, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.MoveZone, Obj: wurmID, From: state.ZHand, To: state.ZBattlefield})
	// Replay Config needs the runtime token definition independently because
	// New isolates the engine's token map.
	setFixtureToken(e, "gw_1_1_human_soldier_training", token)
	cfg.Tokens["gw_1_1_human_soldier_training"] = token
	e.emit(events.Event{Kind: events.TokenCreate, Player: 0, Text: "gw_1_1_human_soldier_training"})
	tokID := e.G.NextID - 1
	if !e.HasKeyword(tokID, "Training") {
		t.Fatal("the token does not have Training")
	}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{tokID, wurmID}})
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(tokID).Counter("P1P1"); got != 1 {
		t.Fatalf("printed + granted Training stacked: %d counters, want 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestTrainingSpansDefendersInOneDeclaration pins the declaration-wide
// attacker set (Engine.declaredAttackers): a Training creature attacking one
// opponent still trains when its bigger companion attacks a DIFFERENT
// opponent in the same declare-attackers step. handleAttackers emits one
// DeclareAttackers event per defender, so ev.IDs alone cannot answer it; the
// test drives the REAL KAttackers intent (both attackers, one submission) so
// it exercises the production population of the scratch, not a hand-set field.
func TestTrainingSpansDefendersInOneDeclaration(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	rider := mustCorpusCard(t, reg, "Gryff Rider")
	wurm := mustCorpusCard(t, reg, "Craw Wurm")
	e := threeSeatEngine(t)
	riderID := onBoardCard(t, e, 0, rider)
	wurmID := onBoardCard(t, e, 0, wurm)
	e.G.Obj(riderID).SummonSick = false
	e.G.Obj(wurmID).SummonSick = false
	if !e.G.Obj(riderID).Face().HasKeyword("Training") {
		t.Fatal("the real Gryff Rider face does not print Training")
	}
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected a KAttackers decision, got %+v", d)
	}
	choices := make([]int, 0, 2)
	want := map[state.ObjID]state.PlayerID{riderID: 1, wurmID: 2}
	for _, o := range d.Options {
		if want[o.Obj] == o.Player {
			choices = append(choices, o.Index)
		}
	}
	if len(choices) != 2 {
		t.Fatalf("wanted one attacker option per defender; got %v from %+v", choices, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		t.Fatalf("submit split attack: %v", err)
	}
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(riderID).Counter("P1P1"); got != 1 {
		t.Fatalf("training across defenders gave %d counters, want 1", got)
	}
}
