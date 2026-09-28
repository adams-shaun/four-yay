package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func corpusCard(t *testing.T, name string) *cards.Card {
	t.Helper()
	c, ok := testutil.CorpusRegistry(t).Lookup(name)
	if !ok {
		t.Fatalf("corpus fixture %q is missing", name)
	}
	return c
}

func TestSheoldredDrawnTriggerUsesDrawEventPlayer(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	sheoldred := corpusCard(t, "Sheoldred, the Apocalypse")
	source := onBoardCard(t, e, 0, sheoldred)
	if got := e.G.Obj(source).Face().Triggers[0].Mode; got != "Drawn" {
		t.Fatalf("Sheoldred trigger mode = %q, want Drawn", got)
	}

	drawn := e.G.Zone(state.ZLibrary, 0)[0]
	e.emit(events.Event{Kind: events.Draw, Player: 0, Obj: drawn, From: state.ZLibrary, To: state.ZHand, Secret: true})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("Sheoldred draw trigger stack = %v, want one trigger", e.G.Stack)
	}
	e.resolveTop()
	if got := e.G.Players[0].Life; got != 22 {
		t.Fatalf("life after Sheoldred's draw trigger = %d, want 22", got)
	}
}

func TestFateUnravelerDamagesTheDrawingOpponent(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	source := onBoardCard(t, e, 0, corpusCard(t, "Fate Unraveler"))
	drawn := e.G.Zone(state.ZLibrary, 1)[0]
	e.emit(events.Event{Kind: events.Draw, Player: 1, Obj: drawn, From: state.ZLibrary, To: state.ZHand, Secret: true})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Source != source {
		t.Fatalf("Fate Unraveler trigger stack = %v, want its one trigger", e.G.Stack)
	}
	e.resolveTop()
	if got := e.G.Players[1].Life; got != 19 {
		t.Fatalf("drawing opponent life = %d, want 19", got)
	}
	if got := e.G.Players[0].Life; got != 20 {
		t.Fatalf("Fate Unraveler controller life = %d, want 20", got)
	}
}

func TestBlackWidowDrawnNumberAndValidPlayer(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	source := onBoardCard(t, e, 0, corpusCard(t, "Black Widow, Agile Avenger"))
	// The unmodified card says an OPPONENT's SECOND draw. Neither controller
	// draw nor the opponent's first draw may queue it.
	for _, p := range []state.PlayerID{0, 1} {
		drawn := e.G.Zone(state.ZLibrary, p)[0]
		e.emit(events.Event{Kind: events.Draw, Player: p, Obj: drawn, From: state.ZLibrary, To: state.ZHand, Secret: true})
		if len(e.pendingTriggers) != 0 {
			t.Fatalf("draw %d queued %#v, want no Black Widow trigger", p, e.pendingTriggers)
		}
	}
	drawn := e.G.Zone(state.ZLibrary, 1)[0]
	e.emit(events.Event{Kind: events.Draw, Player: 1, Obj: drawn, From: state.ZLibrary, To: state.ZHand, Secret: true})
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Source != source {
		t.Fatalf("opponent second draw queued %#v, want Black Widow", e.pendingTriggers)
	}
}

func TestKeranosDrawnOnlyTriggersOnControllerTurn(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	source := onBoardCard(t, e, 0, corpusCard(t, "Keranos, God of Storms"))
	if got := e.G.Obj(source).Face().Triggers[0].Params["PlayerTurn"]; got != "True" {
		t.Fatalf("Keranos PlayerTurn = %q, want True", got)
	}

	// Keranos owns the source but the opponent owns this first drawn card.
	// Number$ alone would admit it, so this pins the controller-turn gate.
	e.G.Active = 1
	opponentDraw := e.G.Zone(state.ZLibrary, 1)[0]
	e.emit(events.Event{Kind: events.Draw, Player: 1, Obj: opponentDraw, From: state.ZLibrary, To: state.ZHand, Secret: true})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("opponent-turn first draw queued %#v, want no Keranos trigger", e.pendingTriggers)
	}

	e.G.Active = 0
	controllerDraw := e.G.Zone(state.ZLibrary, 0)[0]
	e.emit(events.Event{Kind: events.Draw, Player: 0, Obj: controllerDraw, From: state.ZLibrary, To: state.ZHand, Secret: true})
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Source != source {
		t.Fatalf("controller-turn first draw queued %#v, want Keranos", e.pendingTriggers)
	}
}

func TestOrcishBowmastersDrawnSkipsFirstDrawStepCard(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	bowmasters := corpusCard(t, "Orcish Bowmasters")
	source := onBoardCard(t, e, 0, bowmasters)
	if got := e.G.Obj(source).Face().Triggers[1].Params["FirstCardInDrawStep"]; got != "False" {
		t.Fatalf("Bowmasters FirstCardInDrawStep = %q, want False", got)
	}
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDraw})
	first := e.G.Zone(state.ZLibrary, 1)[0]
	e.emit(events.Event{Kind: events.Draw, Player: 1, Obj: first, From: state.ZLibrary, To: state.ZHand, Secret: true})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("first draw step card queued %#v, want no Bowmasters trigger", e.pendingTriggers)
	}
	second := e.G.Zone(state.ZLibrary, 1)[0]
	e.emit(events.Event{Kind: events.Draw, Player: 1, Obj: second, From: state.ZLibrary, To: state.ZHand, Secret: true})
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Source != source {
		t.Fatalf("second draw step card queued %#v, want Bowmasters trigger", e.pendingTriggers)
	}
}

func TestLifeLostAllObNixilisQueuesOnceForDamageAllPlayers(t *testing.T) {
	t.Parallel()
	// DamageAll must batch all serialized player hits. Ogre Painbringer is the
	// broad real-card regression (every player); End the Festivities supplies
	// Ob Nixilis's exact-one-life condition for the one-trigger assertion.
	e := New(seatZeroStart(Config{Seed: 1, Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40)}}))
	ob := corpusCard(t, "Ob Nixilis, Captive Kingpin")
	source := onBoardCard(t, e, 0, ob)
	if got := e.G.Obj(source).Face().Triggers[0].Mode; got != "LifeLostAll" {
		t.Fatalf("Ob Nixilis trigger mode = %q, want LifeLostAll", got)
	}
	ogre := onBoardCard(t, e, 0, corpusCard(t, "Ogre Painbringer"))
	ogreFace := e.G.Obj(ogre).Face()
	ogreDamage := cards.ResolveSVar(ogreFace.SVars, "TrigDmg")
	if ogreDamage == nil || ogreDamage.API != "DamageAll" || ogreDamage.Params["ValidPlayers"] != "Player" {
		t.Fatalf("Ogre Painbringer DamageAll fixture changed: %+v", ogreDamage)
	}
	e.resolveAbility(ogre, 0, nil, ogreDamage, ogreFace.SVars)
	for _, id := range []state.ObjID{source, ogre} {
		if got := e.G.Obj(id).Damage; got != 0 {
			t.Fatalf("player-only Ogre Painbringer damaged permanent %d for %d", id, got)
		}
	}
	for _, p := range []state.PlayerID{0, 1, 2} {
		if got := e.G.Players[p].Life; got != 17 {
			t.Fatalf("player %d life after Ogre Painbringer = %d, want 17", p, got)
		}
	}
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("Ob Nixilis must not trigger for 3-life losses: %#v", e.pendingTriggers)
	}

	festivities := onBoardCard(t, e, 0, corpusCard(t, "End the Festivities"))
	festivitiesFace := e.G.Obj(festivities).Face()
	festivitiesDamage := festivitiesFace.Abilities[0]
	if festivitiesDamage.API != "DamageAll" || festivitiesDamage.Params["ValidPlayers"] != "Player.Opponent" || festivitiesDamage.Params["NumDmg"] != "1" {
		t.Fatalf("End the Festivities DamageAll fixture changed: %+v", festivitiesDamage)
	}
	e.resolveAbility(festivities, 0, nil, festivitiesDamage, festivitiesFace.SVars)
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Source != source {
		t.Fatalf("simultaneous opponent losses queued %#v, want one Ob Nixilis trigger", e.pendingTriggers)
	}
}

func TestLifeLostAllObNixilisAggregatesEachPlayersBatchLoss(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	source := onBoardCard(t, e, 0, corpusCard(t, "Ob Nixilis, Captive Kingpin"))
	trigger := e.G.Obj(source).Face().Triggers[0]
	if trigger.Mode != "LifeLostAll" || trigger.Params["ValidAmountEach"] != "EQ1" {
		t.Fatalf("Ob Nixilis trigger fixture changed: %+v", trigger)
	}

	// Two serialized combat assignments can hit the same player during one
	// simultaneous damage event. Ob Nixilis sees that player lose two life,
	// not two independent one-life losses.
	e.BeginLifeLossBatch()
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
	e.EndLifeLossBatch()
	if got := e.G.Players[1].Life; got != 18 {
		t.Fatalf("opponent life after two simultaneous one-damage hits = %d, want 18", got)
	}
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("two-life batch queued %#v for Ob Nixilis %d, want no trigger", e.pendingTriggers, source)
	}
}

func TestValgavothLifeLostFirstTimeGate(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	valgavoth := corpusCard(t, "Valgavoth, Harrower of Souls")
	source := onBoardCard(t, e, 0, valgavoth)
	trig := e.G.Obj(source).Face().Triggers[0]
	if trig.Mode != "LifeLost" || trig.Params["FirstTime"] != "True" {
		t.Fatalf("Valgavoth trigger fixture changed: %+v", trig)
	}

	// ValidPlayer$ Opponent.Active is the unmodified corpus condition: the
	// opponent must be the active player, and FirstTime permits only its first
	// life-loss event this turn.
	e.G.Active = 1
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -1})
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Source != source {
		t.Fatalf("first active-opponent loss queued %#v, want Valgavoth", e.pendingTriggers)
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -1})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("second active-opponent loss queued %#v, want no second Valgavoth", e.pendingTriggers)
	}
}

func TestLoseLifeAllBatchesOpponentsForObNixilis(t *testing.T) {
	t.Parallel()
	e := New(seatZeroStart(Config{Seed: 1, Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40)}}))
	source := onBoardCard(t, e, 0, corpusCard(t, "Ob Nixilis, Captive Kingpin"))
	// The real Meathook Massacre sub-ability is the common "each opponent
	// loses one life" shape; it must produce a single LifeLostAll group.
	meathook := corpusCard(t, "The Meathook Massacre").Faces[0]
	loss := cards.ResolveSVar(meathook.SVars, "TrigLoseLife")
	if loss == nil || loss.API != "LoseLife" || loss.Params["Defined"] != "Opponent" {
		t.Fatalf("Meathook loss fixture changed: %+v", loss)
	}
	e.resolveAbility(source, 0, nil, loss, meathook.SVars)
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Source != source {
		t.Fatalf("LoseLife opponent group queued %#v, want one Ob Nixilis trigger", e.pendingTriggers)
	}
}

func TestKefkaLifeLostPlayerTurnGate(t *testing.T) {
	e := layerEngine(t)
	kefka := *corpusCard(t, "Kefka, Ruler of Ruin")
	kefka.Faces = kefka.Faces[1:2] // unmodified Ruler face carries this trigger.
	source := onBoardCard(t, e, 0, &kefka)
	e.G.Active = 1
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -1})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("opponent loss outside Kefka controller turn queued %#v", e.pendingTriggers)
	}
	e.G.Active = 0
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -1})
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Source != source {
		t.Fatalf("opponent loss during Kefka controller turn queued %#v", e.pendingTriggers)
	}
}

func TestSahirLifeLostCauseAndAmountGates(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	source := onBoardCard(t, e, 0, corpusCard(t, "Sahir, Visitor in Darkness"))
	// Sahir's unmodified secondary trigger requires exactly one life lost to
	// a spell or ability its controller controls. A causeless event and a
	// two-life event must not satisfy either half of that condition.
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -1})
	e.damaging = source
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -2})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("invalid Sahir losses queued %#v", e.pendingTriggers)
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -1})
	e.damaging = 0
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Source != source {
		t.Fatalf("Sahir one-life controlled cause queued %#v", e.pendingTriggers)
	}
}

func TestArchfiendCantGainLifePreventsOpponentOnly(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	archfiend := corpusCard(t, "Archfiend of Despair")
	source := onBoardCard(t, e, 0, archfiend)
	if got := e.G.Obj(source).Face().Statics[0].Mode; got != "CantGainLife" {
		t.Fatalf("Archfiend static mode = %q, want CantGainLife", got)
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: 3})
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 3})
	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("opponent life = %d, want prevented gain at 20", got)
	}
	if got := e.G.Players[0].Life; got != 23 {
		t.Fatalf("Archfiend controller life = %d, want 23", got)
	}
}

func TestSulfuricVortexGainLifeReplacementPreventsGain(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	vortex := corpusCard(t, "Sulfuric Vortex")
	source := onBoardCard(t, e, 0, vortex)
	if got := e.G.Obj(source).Face().Repls[0].Event; got != "GainLife" {
		t.Fatalf("Sulfuric Vortex replacement event = %q, want GainLife", got)
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: 4})
	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("life after Sulfuric Vortex replacement = %d, want 20", got)
	}
}

func TestBloodletterLifeReducedDoublesOpponentLossOnYourTurn(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	bloodletter := corpusCard(t, "Bloodletter of Aclazotz")
	source := onBoardCard(t, e, 0, bloodletter)
	if got := e.G.Obj(source).Face().Repls[0].Event; got != "LifeReduced" {
		t.Fatalf("Bloodletter replacement event = %q, want LifeReduced", got)
	}
	e.G.Active = 0
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -3})
	if got := e.G.Players[1].Life; got != 14 {
		t.Fatalf("life after Bloodletter loss replacement = %d, want 14", got)
	}
}

func TestAlhammarretsArchiveDoublesGain(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	archive := corpusCard(t, "Alhammarret's Archive")
	onBoardCard(t, e, 0, archive)
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 3})
	if got := e.G.Players[0].Life; got != 26 {
		t.Fatalf("life after Archive +3 gain = %d, want 26", got)
	}
}

// lifeReplacementOrder emits a life change that several non-commuting
// replacements would modify, asserts that the affected player (and nobody
// else) is offered the CR 616.1 order choice before any life moves, answers
// it with the replacement owned by first, and returns the settled life total.
func lifeReplacementOrder(t *testing.T, e *Engine, p state.PlayerID, amount int32,
	sources []state.ObjID, first state.ObjID) int32 {
	t.Helper()
	before := e.G.Players[p].Life
	e.emit(events.Event{Kind: events.LifeChange, Player: p, Amount: amount})
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || d.Player != p {
		t.Fatalf("pending = %+v, want a replacement-order choice for seat %d", d, p)
	}
	if got := e.G.Players[p].Life; got != before {
		t.Fatalf("life moved to %d before the order was chosen, want %d", got, before)
	}
	if len(d.Options) != len(sources) {
		t.Fatalf("order options = %+v, want one per replacement %v", d.Options, sources)
	}
	pick := -1
	for i, o := range d.Options {
		if o.Obj != sources[i] {
			t.Fatalf("option %d names %d, want %d in scan order", i, o.Obj, sources[i])
		}
		if o.Obj == first {
			pick = o.Index
		}
	}
	submitChoices(t, e, pick)
	if d := e.Pending(); d != nil && d.Kind == decision.KReplacement {
		t.Fatalf("a second order choice was posed with one replacement left: %+v", d)
	}
	return e.G.Players[p].Life
}

// CR 616.1: Tainted Remedy (seat 0) and the gaining player's Alhammarret's
// Archive both apply to seat 1's +3. The gaining player chooses. Remedy first
// turns the gain into a loss, so Archive no longer applies (lose 3). Archive
// first doubles the gain, and Remedy then turns the +6 into a loss of 6.
func TestTaintedRemedyAndArchiveGainingPlayerChoosesOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		first int // index into {remedy, archive}
		want  int32
	}{
		{"remedy first", 0, 17},
		{"archive first", 1, 14},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := layerEngine(t)
			remedy := onBoardCard(t, e, 0, corpusCard(t, "Tainted Remedy"))
			archive := onBoardCard(t, e, 1, corpusCard(t, "Alhammarret's Archive"))
			sources := []state.ObjID{remedy, archive}
			if got := lifeReplacementOrder(t, e, 1, 3, sources, sources[tc.first]); got != tc.want {
				t.Fatalf("seat 1 life = %d, want %d", got, tc.want)
			}
		})
	}
}

// CR 616.1: a player controlling Alhammarret's Archive and Cleric Class who
// would gain 3 chooses between double-then-plus-one (7) and
// plus-one-then-double (8).
func TestArchiveAndClericClassControllerChoosesOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		first int // index into {archive, cleric}
		want  int32
	}{
		{"archive first", 0, 27},
		{"cleric class first", 1, 28},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := layerEngine(t)
			archive := onBoardCard(t, e, 0, corpusCard(t, "Alhammarret's Archive"))
			cleric := onBoardCard(t, e, 0, corpusCard(t, "Cleric Class"))
			sources := []state.ObjID{archive, cleric}
			if got := lifeReplacementOrder(t, e, 0, 3, sources, sources[tc.first]); got != tc.want {
				t.Fatalf("seat 0 life = %d, want %d", got, tc.want)
			}
		})
	}
}

// Two doublers commute, so no order choice is posed and each applies once.
func TestTwoArchivesCommuteWithoutOrderChoice(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	archive := corpusCard(t, "Alhammarret's Archive")
	onBoardCard(t, e, 0, archive)
	onBoardCard(t, e, 0, archive)
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 3})
	if d := e.Pending(); d != nil && d.Kind == decision.KReplacement {
		t.Fatalf("commuting doublers posed an order choice: %+v", d)
	}
	if got := e.G.Players[0].Life; got != 32 {
		t.Fatalf("life after two Archives +3 = %d, want 32", got)
	}
}

func TestTwoBloodlettersEachApplyOnce(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	bloodletter := corpusCard(t, "Bloodletter of Aclazotz")
	onBoardCard(t, e, 0, bloodletter)
	onBoardCard(t, e, 0, bloodletter)
	e.G.Active = 0
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -3})
	if got := e.G.Players[1].Life; got != 8 {
		t.Fatalf("life after two Bloodletters -3 = %d, want 8", got)
	}
}
