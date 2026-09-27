package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// These tests drive REAL cast and activation flows through the CR 601.2c
// target ask (rules/cast.go targetAsk) for cards whose ability carries
// `TargetingPlayer$ Player.Opponent`: the answering seat must be the
// opponent, not the caster, while target legality stays referenced to the
// caster. The askTarget-level chooser contract itself is pinned in
// targetingplayer_nontrigger_test.go.

// compiledAbilityNamed returns the ability with the given kind whose
// TargetingPlayer$ parameter equals spec, failing when the corpus card no
// longer carries it (the fixture premise of the end-to-end tests below).
func compiledAbilityNamed(t *testing.T, card *cards.Card, kind, spec string) *cards.SA {
	t.Helper()
	for _, f := range card.Faces {
		for _, cand := range f.Abilities {
			if cand.Kind == kind && cand.Params["TargetingPlayer"] == spec {
				return cand
			}
		}
	}
	t.Fatalf("corpus card %q lost its %s ability with TargetingPlayer$ %s -- fixture premise broken", card.Faces[0].Name, kind, spec)
	return nil
}

// moveSeatCardToBattlefield moves the first card named `name` in seat's
// hand/library to the battlefield (logged MoveZone), like searchMoveByName.
func moveSeatCardToBattlefield(t *testing.T, e *Engine, seat state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, seat) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == name {
				if z != state.ZBattlefield {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZBattlefield})
				}
				e.pending = nil
				e.priorityRound()
				return id
			}
		}
	}
	t.Fatalf("seat %d has no %q in hand/library", seat, name)
	return 0
}

// opponentChooserBoard deals seat 0 a deck whose first card is the given
// corpus carrier plus forests, and seat 1 a deck of Grizzly Bears.
func opponentChooserBoard(t *testing.T, reg *cards.Registry, carrier *cards.Card) (*Engine, Config) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck0 := []*cards.Card{carrier}
	for i := 0; i < 6; i++ {
		deck0 = append(deck0, forest)
	}
	for len(deck0) < 40 {
		deck0 = append(deck0, bear)
	}
	deck1 := make([]*cards.Card, 0, 40)
	for len(deck1) < 40 {
		deck1 = append(deck1, bear)
	}
	cfg := Config{Seed: 4711, Names: []string{"caster", "chooser"},
		Decks: [][]*cards.Card{deck0, deck1}, Tokens: reg.Tokens, NameUniverse: reg.Cards}
	e := New(cfg)
	e.Advance()
	// Seat 1 may win the toss and take the first turn: drive to seat 0's next
	// main phase either way (seat 0 is the caster whose target ask we test).
	if e.G.Active != 0 {
		driveToStep(t, e, e.G.Turn+1, 0, state.StepMain1)
	}
	toMain1(t, e)
	return e, cfg
}

// TestEvangelizeCastTargetAskGoesToOpponent casts the real corpus spell
// Evangelize through the full cast flow: after paying {4}{W} the pending
// KTarget ask must be posed to seat 1 (the opponent Evangelize names as
// chooser), not seat 0. The chooser picks its own bear and the spell
// resolves, moving that bear under seat 0's control; the whole game replays.
func TestEvangelizeCastTargetAskGoesToOpponent(t *testing.T) {
	reg := searchTestRegistry(t)
	evangelize := searchCorpusCard(t, reg, "Evangelize")
	// Precondition: the compiled spell still carries the parameter under test.
	compiledAbilityNamed(t, evangelize, "SP", "Player.Opponent")
	e, cfg := opponentChooserBoard(t, reg, evangelize)
	evangelizeID := searchMoveByName(t, e, "Evangelize", state.ZHand)
	bearID := moveSeatCardToBattlefield(t, e, 1, "Grizzly Bears")
	// Precondition: the chooser's candidate is a battlefield creature under
	// seat 1 -- different controllers, so the ask and the legality reference
	// genuinely differ.
	bo := e.G.Obj(bearID)
	if bo == nil || bo.Zone != state.ZBattlefield || bo.Controller != 1 {
		t.Fatalf("bear precondition: %+v (want battlefield creature under seat 1)", bo)
	}
	addMana(t, e, 0, "WWWCC") // {4}{W}

	submitChoices(t, e, castCardOption(t, e, evangelizeID).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending ask after cast = %+v, want the Evangelize target ask", d)
	}
	if d.Player != 1 {
		t.Fatalf("target ask posed to seat %d, want the opponent seat 1 (TargetingPlayer$ Player.Opponent)", d.Player)
	}
	if !targetOptionContains(d.Options, bearID) {
		t.Fatalf("seat 1's bear %d missing from options %+v", bearID, d.Options)
	}
	// The answering seat cannot submit a target outside the offered set.
	bad := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{len(d.Options) + 10}}
	if err := d.Validate(bad); err == nil {
		t.Fatal("chooser submitted an out-of-set target and the decision accepted it")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == bearID {
			idx = o.Index
		}
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit the opponent's target choice: %v", err)
	}
	passUntilStackEmpty(t, e, 20)
	after := e.G.Obj(bearID)
	if after == nil || after.Zone != state.ZBattlefield || after.Controller != 0 {
		t.Fatalf("after Evangelize resolved the bear is %+v, want on the battlefield under the caster seat 0", after)
	}
	replayCheck(t, e, cfg)
}

// TestEchoChamberActivationTargetAskGoesToOpponent activates the real corpus
// artifact Echo Chamber ({4}, {T}) through the full activation flow: the
// KTarget ask for its CopyPermanent body must be posed to seat 1, whose
// chosen creature is copied into a haste token under seat 0's control; the
// whole game replays.
func TestEchoChamberActivationTargetAskGoesToOpponent(t *testing.T) {
	reg := searchTestRegistry(t)
	echoChamber := searchCorpusCard(t, reg, "Echo Chamber")
	// Precondition: the compiled activation still carries the parameter.
	compiledAbilityNamed(t, echoChamber, "AB", "Player.Opponent")
	e, cfg := opponentChooserBoard(t, reg, echoChamber)
	chamberID := moveSeatCardToBattlefield(t, e, 0, "Echo Chamber")
	co := e.G.Obj(chamberID)
	if co == nil || co.Zone != state.ZBattlefield || co.Controller != 0 || co.Tapped {
		t.Fatalf("Echo Chamber precondition: %+v (want untapped artifact under seat 0)", co)
	}
	bearID := moveSeatCardToBattlefield(t, e, 1, "Grizzly Bears")
	bo := e.G.Obj(bearID)
	if bo == nil || bo.Zone != state.ZBattlefield || bo.Controller != 1 {
		t.Fatalf("bear precondition: %+v (want battlefield creature under seat 1)", bo)
	}
	addMana(t, e, 0, "CCCC") // {4}

	submitChoices(t, e, abilityOption(t, e, chamberID, 0).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending ask after activation = %+v, want the Echo Chamber target ask", d)
	}
	if d.Player != 1 {
		t.Fatalf("target ask posed to seat %d, want the opponent seat 1 (TargetingPlayer$ Player.Opponent)", d.Player)
	}
	if !targetOptionContains(d.Options, bearID) {
		t.Fatalf("seat 1's bear %d missing from options %+v", bearID, d.Options)
	}
	// The answering seat cannot submit a target outside the offered set.
	bad := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{len(d.Options) + 10}}
	if err := d.Validate(bad); err == nil {
		t.Fatal("chooser submitted an out-of-set target and the decision accepted it")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == bearID {
			idx = o.Index
		}
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit the opponent's target choice: %v", err)
	}
	passUntilStackEmpty(t, e, 20)
	// The copy is a token under the ability's controller.
	tokens := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o != nil && o.IsToken && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
			tokens++
		}
	}
	if tokens != 1 {
		t.Fatalf("Echo Chamber created %d Grizzly Bears token(s) under seat 0, want 1", tokens)
	}
	replayCheck(t, e, cfg)
}
