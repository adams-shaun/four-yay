package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The has-all-abilities-of static class (Forge's GainsAbilitiesOf$ /
// GainsTriggerAbsOf$ on a Mode$ Continuous static) pinned on the real corpus
// carrier Idris, Soul of the TARDIS, whose static is
//
//	S:Mode$ Continuous | Affected$ Card.Self | EffectZone$ Battlefield |
//	  GainsAbilitiesOf$ Card.ExiledWithSource |
//	  GainsTriggerAbsOf$ Card.ExiledWithSource | GainsAbilitiesOfZones$ Exile |
//	  AddPower$ X | AddToughness$ X
//	SVar:X:Count$ValidExile Card.ExiledWithSource$CardManaCost
//
// The exiled card is an AUTHORED fixture artifact (never a corpus .txt, per
// the licensing rule) carrying one activated ability and one triggered
// ability, so every assertion runs through the engine's own offer, activation
// and trigger paths against Idris's compiled static.

// gainsArtifactSrc is the exiled artifact: "{T}: Draw a card." plus "At the
// beginning of your upkeep, you lose 2 life." -- one activated and one
// triggered ability, each independently observable.
func gainsArtifactSrc(t testing.TB) *cards.Card {
	t.Helper()
	return card(t, "Name:Gains Artifact\nManaCost:2\nTypes:Artifact\n"+
		"A:AB$ Draw | Cost$ T | NumCards$ 1 | SpellDescription$ Draw a card.\n"+
		"T:Mode$ Phase | Phase$ Upkeep | ValidPlayer$ You | Execute$ TrigBleed | TriggerDescription$ At the beginning of your upkeep, you lose 2 life.\n"+
		"SVar:TrigBleed:DB$ LoseLife | LifeAmount$ 2 | Defined$ You\n"+
		"Oracle:x\n")
}

// gainsBoard seeds Idris (the real corpus carrier) and the authored artifact
// onto seat 0's battlefield, then exiles the artifact with Idris as the
// exiling source through a logged MoveZone carrying the provenance in IDs
// (events' moveZoneEvent shape), and returns (engine, cfg, idris, artifact).
func gainsBoard(t *testing.T) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	idris := tokenReplCorpusCard(t, "Idris, Soul of the TARDIS")
	artifact := gainsArtifactSrc(t)
	e, cfg := tokenReplGame(t, 9107, idris, artifact)
	idrisID := moveSeededCard(t, e, 0, idris, state.ZBattlefield)
	artifactID := moveSeededCard(t, e, 0, artifact, state.ZBattlefield)
	// Exile the artifact with Idris as the exiling source: the MoveZone IDs
	// payload is the exact provenance carrier effects' moveZoneEvent uses,
	// so ExiledWith == Idris afterwards and Card.ExiledWithSource matches.
	e.emit(events.Event{Kind: events.MoveZone, Obj: artifactID, From: state.ZBattlefield,
		To: state.ZExile, IDs: []state.ObjID{idrisID}})
	e.pending = nil
	// This harness supplies Idris's imprinted artifact explicitly. Do not
	// also run its printed ETB exile trigger: that would start a second,
	// unrelated artifact search at the next priority window.
	e.pendingTriggers = nil
	if o := e.G.Obj(artifactID); o == nil || o.Zone != state.ZExile || o.ExiledWith != idrisID {
		t.Fatalf("artifact = %+v, want exiled with %d", o, idrisID)
	}
	e.priorityRound()
	return e, cfg, idrisID, artifactID
}

// TestIdrisGainsTheExiledArtifactAbilities is the filing card end to end: the
// exiled artifact's triggered ability fires for Idris's controller at the
// next upkeep, and its activated ability is offered on Idris (once Idris is
// no longer summoning-sick) and draws a card when activated.
func TestIdrisGainsTheExiledArtifactAbilities(t *testing.T) {
	t.Parallel()
	e, cfg, idrisID, _ := gainsBoard(t)

	// Precondition: Idris is on the battlefield and owns the gains static.
	if o := e.G.Obj(idrisID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Idris = %+v, want on the battlefield", o)
	}

	// The granted TRIGGERED ability fires for Idris's controller at seat 0's
	// next own upkeep (turn 3; Idris entered on turn 1, so this is not a
	// summoning-sickness-dependent step).
	lifeBefore := e.G.Players[0].Life
	gainsDriveToStep(t, e, 3, 0, state.StepUpkeep)
	// The granted upkeep ability and Idris's own Vanishing tick trigger
	// simultaneously. Choose their stack order before draining either.
	gainsAnswerTriggerOrder(t, e)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != lifeBefore-2 {
		t.Fatalf("gained upkeep trigger: life %d -> %d, want -2", lifeBefore, got)
	}
	if n := countKind(e.L.Events, events.GainedTriggerPush, idrisID); n != 1 {
		t.Fatalf("GainedTriggerPush count = %d, want 1", n)
	}

	// The granted activated ability is offered on Idris once Idris can tap
	// (turn 3's main phase), anchored on the foreign card. Idris's own
	// AddPower$/AddToughness$ static (X = the exiled card's mana value) is the
	// same Card.ExiledWithSource read the grant uses, so assert it too.
	gainsDriveToStep(t, e, 3, 0, state.StepMain1)
	if p, tg := e.Power(idrisID), e.Toughness(idrisID); p != 5 || tg != 5 {
		t.Fatalf("Idris P/T = %d/%d, want 5/5 (3/3 base +2/+2 from the exiled mana value)", p, tg)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %+v, want priority", d)
	}
	var opt decision.Option
	found := false
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == idrisID && o.GainedSource != 0 {
			opt, found = o, true
			break
		}
	}
	if !found {
		t.Fatalf("Idris offers no gained ability: %+v", d.Options)
	}
	if o := e.G.Obj(opt.GainedSource); o == nil || o.Zone != state.ZExile {
		t.Fatalf("gained anchor = %d, want the exiled artifact", opt.GainedSource)
	}

	// Activate it: the hand size grows by one from the granted draw.
	handBefore := len(e.G.Zone(state.ZHand, 0))
	libBefore := len(e.G.Zone(state.ZLibrary, 0))
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	handAfter := len(e.G.Zone(state.ZHand, 0))
	if handAfter != handBefore+1 || len(e.G.Zone(state.ZLibrary, 0)) != libBefore-1 {
		t.Fatalf("granted draw: hand %d->%d library %d->%d, want +1/-1",
			handBefore, handAfter, libBefore, len(e.G.Zone(state.ZLibrary, 0)))
	}
	if n := countKind(e.L.Events, events.GainedAbilityPush, idrisID); n != 1 {
		t.Fatalf("GainedAbilityPush count = %d, want 1", n)
	}
	replayCheck(t, e, cfg)
}

// TestIdrisGainedAbilitiesEndWhenTheExiledCardLeaves pins the fail-closed
// liveness: once the exiled card leaves the scoped zone (Exile), Idris stops
// offering its abilities, because the static scan re-derives the named set on
// every event.
func TestIdrisGainedAbilitiesEndWhenTheExiledCardLeaves(t *testing.T) {
	t.Parallel()
	e, cfg, idrisID, artifactID := gainsBoard(t)
	// Advance past summoning sickness so the granted tap ability is offerable
	// (otherwise this precondition would pass vacuously via the tap gate).
	gainsDriveToStep(t, e, 3, 0, state.StepMain1)
	if !hasGainedAbility(e, idrisID) {
		t.Fatal("precondition: Idris does not offer the gained ability before the move")
	}
	// Move the artifact out of exile (to the graveyard): no provenance there.
	e.emit(events.Event{Kind: events.MoveZone, Obj: artifactID, From: state.ZExile, To: state.ZGraveyard})
	e.pending = nil
	e.priorityRound()
	if hasGainedAbility(e, idrisID) {
		t.Fatalf("Idris still offers a gained ability after the exiled card left: %+v", e.Pending().Options)
	}
	replayCheck(t, e, cfg)
}

// TestIdrisDoesNotGainUnrelatedExiledCards pins the Card.ExiledWithSource
// scoping: a card exiled by something ELSE is not gained.
func TestIdrisDoesNotGainUnrelatedExiledCards(t *testing.T) {
	t.Parallel()
	e, cfg, idrisID, artifactID := gainsBoard(t)
	gainsDriveToStep(t, e, 3, 0, state.StepMain1)
	if !hasGainedAbility(e, idrisID) {
		t.Fatal("precondition: Idris does not offer the gained ability before the re-exile")
	}
	// Re-exile the artifact with no exiling source so ExiledWith no longer
	// names Idris.
	e.emit(events.Event{Kind: events.MoveZone, Obj: artifactID, From: state.ZExile, To: state.ZHand})
	e.emit(events.Event{Kind: events.MoveZone, Obj: artifactID, From: state.ZHand, To: state.ZExile})
	e.pending = nil
	e.priorityRound()
	if o := e.G.Obj(artifactID); o == nil || o.ExiledWith != 0 {
		t.Fatalf("artifact ExiledWith = %v, want 0 after a fresh exile with no source", o)
	}
	if hasGainedAbility(e, idrisID) {
		t.Fatal("Idris gained an ability from a card it did not exile")
	}
	replayCheck(t, e, cfg)
}

// gainsManaArtifactSrc is an exiled artifact carrying only a mana ability:
// "{T}: Add {B}."
func gainsManaArtifactSrc(t testing.TB) *cards.Card {
	t.Helper()
	return card(t, "Name:Gains Mana Artifact\nTypes:Artifact\n"+
		"A:AB$ Mana | Cost$ T | Produced$ B | SpellDescription$ Add {B}.\n"+
		"Oracle:x\n")
}

// TestIdrisGainsTheExiledArtifactManaAbility pins the class's mana-ability
// route (a distinct activation path from beginActivation): the exiled
// artifact's mana ability is offered on Idris as an "activate" option and
// produces mana when activated.
func TestIdrisGainsTheExiledArtifactManaAbility(t *testing.T) {
	t.Parallel()
	idris := tokenReplCorpusCard(t, "Idris, Soul of the TARDIS")
	artifact := gainsManaArtifactSrc(t)
	e, cfg := tokenReplGame(t, 9108, idris, artifact)
	idrisID := moveSeededCard(t, e, 0, idris, state.ZBattlefield)
	artifactID := moveSeededCard(t, e, 0, artifact, state.ZBattlefield)
	e.emit(events.Event{Kind: events.MoveZone, Obj: artifactID, From: state.ZBattlefield,
		To: state.ZExile, IDs: []state.ObjID{idrisID}})
	e.pending = nil
	e.priorityRound()

	driveToStep(t, e, 3, 0, state.StepMain1)
	d := e.Pending()
	if d == nil {
		t.Fatal("no priority decision")
	}
	var opt decision.Option
	found := false
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == idrisID {
			opt, found = o, true
			break
		}
	}
	if !found {
		t.Fatalf("Idris offers no gained mana ability: %+v", d.Options)
	}
	submitChoices(t, e, opt.Index)
	if got := e.G.Players[0].Pool[state.MB]; got != 1 {
		t.Fatalf("gained mana ability produced %d black, want 1", got)
	}
	if o := e.G.Obj(idrisID); o == nil || !o.Tapped {
		t.Fatalf("Idris = %+v, want tapped by the gained mana ability", o)
	}
	replayCheck(t, e, cfg)
}

// countersOf reads one counter kind's total off an object.
func countersOf(o *state.Object, kind string) int32 {
	for _, c := range o.Counters {
		if c.Kind == kind {
			return c.N
		}
	}
	return 0
}

// hasGainedAbility reports whether the pending priority decision offers a
// gained ability on obj.
func hasGainedAbility(e *Engine, obj state.ObjID) bool {
	d := e.Pending()
	if d == nil {
		return false
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == obj && o.GainedSource != 0 {
			return true
		}
	}
	return false
}

// gainedAbilitiesOn returns the pending priority decision's gained-ability
// options on obj (the whole options, so a caller can count them and read
// labels).
func gainedAbilitiesOn(e *Engine, obj state.ObjID) []decision.Option {
	d := e.Pending()
	if d == nil {
		return nil
	}
	var out []decision.Option
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == obj && o.GainedSource != 0 {
			out = append(out, o)
		}
	}
	return out
}

// gainsAbilitiesOnlySrc is the REAL corpus carrier Territory Forge, whose
// static names GainsAbilitiesOf$ and NO GainsTriggerAbsOf$ -- the
// Mairsil-the-Pretender class shape the round-2 review broke (a
// GainsAbilitiesOf-only carrier must never fire the foreign card's phase
// triggers, because Forge's parameter grants activated abilities only).
func gainsForgeSrc(t *testing.T) *cards.Card {
	t.Helper()
	return tokenReplCorpusCard(t, "Territory Forge")
}

// TestGainsAbilitiesOnlyNeverFiresForeignTriggers pins the parameter split in
// the TRIGGER direction: Territory Forge gains the exiled card's ACTIVATED
// ability (its parameter means exactly that) but never its upkeep trigger --
// without the GainedTriggerFaces split the foreign trigger fired from the
// gains static alone.
func TestGainsAbilitiesOnlyNeverFiresForeignTriggers(t *testing.T) {
	t.Parallel()
	forge := gainsForgeSrc(t)
	artifact := gainsArtifactSrc(t)
	e, cfg := tokenReplGame(t, 9109, forge, artifact)
	forgeID := moveSeededCard(t, e, 0, forge, state.ZBattlefield)
	artifactID := moveSeededCard(t, e, 0, artifact, state.ZBattlefield)
	e.emit(events.Event{Kind: events.MoveZone, Obj: artifactID, From: state.ZBattlefield,
		To: state.ZExile, IDs: []state.ObjID{forgeID}})
	e.pending = nil
	if o := e.G.Obj(artifactID); o == nil || o.Zone != state.ZExile || o.ExiledWith != forgeID {
		t.Fatalf("artifact = %+v, want exiled with %d", o, forgeID)
	}
	e.priorityRound()

	// Precondition: the grant EXISTS in the activated half -- Territory Forge
	// is an artifact, so the {T} draw is offerable on turn 1 with no
	// summoning-sickness gate in the way. Without this assertion the negative
	// below could pass on a grant that never resolved at all.
	driveToStep(t, e, 1, 0, state.StepMain1)
	if !hasGainedAbility(e, forgeID) {
		t.Fatal("precondition: Territory Forge does not offer the gained activated ability")
	}

	// The foreign card's upkeep trigger never fires: life is unchanged and no
	// GainedTriggerPush was minted.
	lifeBefore := e.G.Players[0].Life
	driveToStep(t, e, 3, 0, state.StepUpkeep)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != lifeBefore {
		t.Fatalf("GainsAbilitiesOf-only fired the foreign upkeep trigger: life %d -> %d, want unchanged", lifeBefore, got)
	}
	if n := countKind(e.L.Events, events.GainedTriggerPush, forgeID); n != 0 {
		t.Fatalf("GainedTriggerPush count = %d, want 0 on a GainsAbilitiesOf-only static", n)
	}
	replayCheck(t, e, cfg)
}

// gainsTriggerOnlySrc is an AUTHORED carrier (never a corpus .txt, per the
// licensing rule) whose static names GainsTriggerAbsOf$ and NO
// GainsAbilitiesOf$ -- the mirror-image parameter split.
func gainsTriggerOnlySrc(t testing.TB) *cards.Card {
	t.Helper()
	return card(t, "Name:Gains Triggers Only\nManaCost:3\nTypes:Artifact\n"+
		"S:Mode$ Continuous | Affected$ Card.Self | EffectZone$ Battlefield | GainsTriggerAbsOf$ Card.ExiledWithSource | GainsAbilitiesOfZones$ Exile\n"+
		"Oracle:x\n")
}

// TestGainsTriggerAbsOnlyNeverGrantsForeignActivatedAbilities pins the
// parameter split in the ACTIVATED direction: a GainsTriggerAbsOf-only static
// fires the foreign card's upkeep trigger (precondition, so the grant is
// live) but never offers its activated ability.
func TestGainsTriggerAbsOnlyNeverGrantsForeignActivatedAbilities(t *testing.T) {
	t.Parallel()
	carrier := gainsTriggerOnlySrc(t)
	artifact := gainsArtifactSrc(t)
	e, cfg := tokenReplGame(t, 9110, carrier, artifact)
	carrierID := moveSeededCard(t, e, 0, carrier, state.ZBattlefield)
	artifactID := moveSeededCard(t, e, 0, artifact, state.ZBattlefield)
	e.emit(events.Event{Kind: events.MoveZone, Obj: artifactID, From: state.ZBattlefield,
		To: state.ZExile, IDs: []state.ObjID{carrierID}})
	e.pending = nil
	e.priorityRound()

	// Precondition: the TRIGGERED grant is live -- the foreign upkeep trigger
	// fires for seat 0 at its turn-3 upkeep.
	lifeBefore := e.G.Players[0].Life
	driveToStep(t, e, 3, 0, state.StepUpkeep)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != lifeBefore-2 {
		t.Fatalf("gained upkeep trigger: life %d -> %d, want -2 (the grant itself must be live)", lifeBefore, got)
	}
	if n := countKind(e.L.Events, events.GainedTriggerPush, carrierID); n != 1 {
		t.Fatalf("GainedTriggerPush count = %d, want 1", n)
	}

	// The negative: the foreign card's ACTIVATED ability is never offered.
	driveToStep(t, e, 3, 0, state.StepMain1)
	if hasGainedAbility(e, carrierID) {
		t.Fatal("GainsTriggerAbsOf-only offered the foreign card's activated ability")
	}
	replayCheck(t, e, cfg)
}

// gainsRivalLandSrc is the AUTHORED opponent land the Sharkey test gains
// from: one mana ability ("{T}: Add {G}.") and one non-mana activated
// ability ("{T}: Draw a card.") -- Sharkey's GainsValidAbilities$
// Activated.!ManaAbility must admit exactly the second.
func gainsRivalLandSrc(t testing.TB) *cards.Card {
	t.Helper()
	return card(t, "Name:Rival Land\nTypes:Land\n"+
		"A:AB$ Mana | Cost$ T | Produced$ G | SpellDescription$ Add {G}.\n"+
		"A:AB$ Draw | Cost$ T | NumCards$ 1 | SpellDescription$ Draw a card.\n"+
		"Oracle:x\n")
}

// TestGainsValidAbilitiesExcludesForeignManaAbilities pins the grant's
// GainsValidAbilities$ filter on the real corpus carrier Sharkey, Tyrant of
// the Shire (`GainsAbilitiesOf$ Land.OppCtrl |
// GainsValidAbilities$ Activated.!ManaAbility`): the opponent land's
// non-mana activated ability IS gained and offered, its mana ability is NOT --
// neither on the ability offer nor in the mana-ability collector the payment
// window reads.
func TestGainsValidAbilitiesExcludesForeignManaAbilities(t *testing.T) {
	t.Parallel()
	sharkey := tokenReplCorpusCard(t, "Sharkey, Tyrant of the Shire")
	land := gainsRivalLandSrc(t)
	e, cfg := tokenReplGameSeats(t, 9111, []*cards.Card{sharkey}, []*cards.Card{land})
	sharkeyID := moveSeededCard(t, e, 0, sharkey, state.ZBattlefield)
	landID := moveSeededCard(t, e, 1, land, state.ZBattlefield)
	e.priorityRound()

	// Sharkey is a creature: drive past summoning sickness so the {T} gained
	// ability is offerable at all.
	driveToStep(t, e, 3, 0, state.StepMain1)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %+v, want priority", d)
	}
	gained := gainedAbilitiesOn(e, sharkeyID)
	if len(gained) != 1 {
		t.Fatalf("Sharkey gained options = %d (%+v), want exactly the non-mana draw", len(gained), gained)
	}
	if o := e.G.Obj(gained[0].GainedSource); o == nil || o.ID != landID || o.Zone != state.ZBattlefield {
		t.Fatalf("gained anchor = %d, want the opponent's land on the battlefield", gained[0].GainedSource)
	}
	if got := e.availableManaAbilities(0, sharkeyID); len(got) != 0 {
		t.Fatalf("Sharkey's mana abilities include the foreign land's: %v, want none", got)
	}
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == sharkeyID {
			t.Fatalf("Sharkey offered a mana activation of the foreign land's ability: %+v", o)
		}
	}
	replayCheck(t, e, cfg)
}

// gainsRivalWalkerSrc is the AUTHORED other planeswalker the Nicol Bolas test
// gains from: one loyalty ability (+1: draw) and one NON-loyalty activated
// ability ({2}: draw two) -- Nicol Bolas Dragon-God's
// GainsValidAbilities$ Activated.Loyalty must admit only the first.
func gainsRivalWalkerSrc(t testing.TB) *cards.Card {
	t.Helper()
	return card(t, "Name:Rival Walker\nManaCost:3\nTypes:Planeswalker\nLoyalty:4\n"+
		"A:AB$ Draw | Cost$ AddCounter<1/LOYALTY> | NumCards$ 1 | Planeswalker$ True | SpellDescription$ +1: Draw a card.\n"+
		"A:AB$ Draw | Cost$ T | NumCards$ 2 | SpellDescription$ {T}: Draw two cards.\n"+
		"Oracle:x\n")
}

// TestGainsValidAbilitiesLoyaltyOnlyAdmitsOnlyLoyalty pins the other filter
// direction on the real corpus carrier Nicol Bolas, Dragon-God
// (`GainsAbilitiesOf$ Planeswalker.Other |
// GainsValidAbilities$ Activated.Loyalty`): of the foreign walker's two
// activated abilities exactly the loyalty one is gained.
func TestGainsValidAbilitiesLoyaltyOnlyAdmitsOnlyLoyalty(t *testing.T) {
	t.Parallel()
	bolas := tokenReplCorpusCard(t, "Nicol Bolas, Dragon-God")
	walker := gainsRivalWalkerSrc(t)
	e, cfg := tokenReplGame(t, 9112, bolas, walker)
	bolasID := moveSeededCard(t, e, 0, bolas, state.ZBattlefield)
	walkerID := moveSeededCard(t, e, 0, walker, state.ZBattlefield)
	e.priorityRound()

	// Precondition: both permanents are on the battlefield and the foreign
	// walker entered with its printed loyalty (so it is a live planeswalker,
	// not a 0-loyalty shell).
	if o := e.G.Obj(walkerID); o == nil || o.Zone != state.ZBattlefield || countersOf(o, "LOYALTY") != 4 {
		t.Fatalf("Rival Walker = %+v, want on the battlefield with 4 loyalty", o)
	}
	driveToStep(t, e, 1, 0, state.StepMain1)
	gained := gainedAbilitiesOn(e, bolasID)
	if len(gained) != 1 {
		t.Fatalf("Bolas gained options = %d (%+v), want exactly the foreign loyalty ability", len(gained), gained)
	}
	if gained[0].GainedSource != walkerID {
		t.Fatalf("gained anchor = %d, want the rival walker", gained[0].GainedSource)
	}
	// The admitted ability is the +1 loyalty one, not the {T} draw-two: the
	// option label carries the foreign ability's SpellDescription.
	if !strings.Contains(gained[0].Label, "+1: Draw a card.") {
		t.Fatalf("gained option label = %q, want the loyalty ability's text", gained[0].Label)
	}

	// Activate the gained [+1]: Bolas's loyalty rises by one (the
	// AddCounter<1/LOYALTY> cost) and the controller draws (the body).
	handBefore := len(e.G.Zone(state.ZHand, 0))
	submitChoices(t, e, gained[0].Index)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(bolasID); o == nil || countersOf(o, "LOYALTY") != 5 {
		t.Fatalf("after the gained [+1], Bolas = %+v, want 5 loyalty", o)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("gained [+1] body: hand %d -> %d, want +1", handBefore, got)
	}

	// CR 606.3 is per PERMANENT and counts gained activations beside printed
	// ones: after the gained [+1], Bolas's own printed [+1]/[-3]/[-8] are
	// withheld too, and the gained [+1] itself is not re-offered.
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %+v, want priority", d)
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == bolasID {
			t.Fatalf("a loyalty ability of Bolas still offered after the gained [+1] (gained=%v): %+v", o.GainedSource != 0, o)
		}
	}

	// Next turn: the per-permanent window reset and the gained [+1] is back.
	gainsDriveToStep(t, e, 3, 0, state.StepMain1)
	if len(gainedAbilitiesOn(e, bolasID)) != 1 {
		t.Fatal("the gained [+1] was not re-offered on the next turn")
	}
	replayCheck(t, e, cfg)
}

// gainsMairsilFixtureSrc is an AUTHORED carrier carrying Mairsil the
// Pretender's exact static line (GainsAbilitiesOf$ Card.YouOwn+counters_GE1_CAGE
// | GainsAbilitiesOfZones$ Exile | GainsAbilitiesLimitPerTurn$ 1) and nothing
// else -- no ETB trigger, no own abilities, so the per-turn cap is the only
// thing under test.
func gainsMairsilFixtureSrc(t testing.TB) *cards.Card {
	t.Helper()
	return card(t, "Name:Cage Warden\nManaCost:2\nTypes:Creature\nPT:2/2\n"+
		"S:Mode$ Continuous | Affected$ Card.Self | EffectZone$ Battlefield | GainsAbilitiesOf$ Card.YouOwn+counters_GE1_CAGE | GainsAbilitiesOfZones$ Exile | GainsAbilitiesLimitPerTurn$ 1\n"+
		"Oracle:x\n")
}

// gainsCagedCardSrc is the AUTHORED caged card: one activated ability, "{2}:
// Draw a card." -- a NON-tap cost, so the tap gate cannot mask the per-turn
// limit after the first activation taps the recipient; the test floats {2}
// before every offer check instead.
func gainsCagedCardSrc(t testing.TB) *cards.Card {
	t.Helper()
	return card(t, "Name:Caged Grimoire\nManaCost:2\nTypes:Artifact\n"+
		"A:AB$ Draw | Cost$ 2 | NumCards$ 1 | SpellDescription$ {2}: Draw a card.\n"+
		"Oracle:x\n")
}

// gainsAnswerTriggerOrder settles a simultaneous-trigger order in the
// offered order. The tests here do not inspect the relative resolution order.
func gainsAnswerTriggerOrder(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTriggerOrder {
		return
	}
	choices := make([]int, 0, len(d.Options))
	for _, o := range d.Options {
		choices = append(choices, o.Index)
	}
	submitChoices(t, e, choices...)
}

// gainsDriveToStep is driveToStep with the extra decision kinds this file's
// longer drives cross: attackers (declined), cleanup discard (first-Max),
// and simultaneous trigger ordering (offered order).
func gainsDriveToStep(t *testing.T, e *Engine, turn int32, active state.PlayerID, step state.Step) {
	t.Helper()
	for i := 0; i < 4000; i++ {
		if e.G.Turn == turn && e.G.Active == active && e.G.Step == step {
			return
		}
		if e.G.Over {
			t.Fatalf("game ended before reaching turn %d seat %d step %s", turn, active, step)
		}
		if answerIfDiscard(t, e) {
			continue
		}
		gainsAnswerTriggerOrder(t, e)
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority && d.Kind != decision.KAttackers {
			t.Fatalf("non-priority decision %+v encountered while driving to turn %d seat %d step %s",
				d, turn, active, step)
		}
		if d.Kind == decision.KAttackers {
			submitAttackersOnly(t, e)
			continue
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d.Options)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	t.Fatalf("did not reach turn %d seat %d step %s", turn, active, step)
}

// TestGainsLimitPerTurnCapsEachForeignAbilityPerTurn pins the grant's
// GainsAbilitiesLimitPerTurn$ 1: the caged card's ability is offered, offered
// again the NEXT turn after the reset, and withheld for the rest of the turn
// it was activated in.
func TestGainsLimitPerTurnCapsEachForeignAbilityPerTurn(t *testing.T) {
	t.Parallel()
	warden := gainsMairsilFixtureSrc(t)
	caged := gainsCagedCardSrc(t)
	e, cfg := tokenReplGame(t, 9113, warden, caged)
	wardenID := moveSeededCard(t, e, 0, warden, state.ZBattlefield)
	cagedID := moveSeededCard(t, e, 0, caged, state.ZBattlefield)
	e.emit(events.Event{Kind: events.MoveZone, Obj: cagedID, From: state.ZBattlefield,
		To: state.ZExile, IDs: []state.ObjID{wardenID}})
	e.emit(events.Event{Kind: events.CounterChange, Obj: cagedID, Counter: "CAGE", Amount: 1})
	e.pending = nil
	if o := e.G.Obj(cagedID); o == nil || o.Zone != state.ZExile || o.ExiledWith != wardenID || countersOf(o, "CAGE") != 1 {
		t.Fatalf("caged card = %+v, want in exile with provenance and 1 CAGE counter", o)
	}
	e.priorityRound()

	// The warden is a creature: drive past summoning sickness so the gained
	// ability is offerable at all, then float the {2} its cost needs.
	driveToStep(t, e, 3, 0, state.StepMain1)
	addMana(t, e, 0, "CC")
	gained := gainedAbilitiesOn(e, wardenID)
	if len(gained) != 1 {
		t.Fatalf("precondition: Warden gained options = %d, want the caged draw offered", len(gained))
	}
	submitChoices(t, e, gained[0].Index)
	passUntilStackEmpty(t, e, 20)
	if n := countKind(e.L.Events, events.GainedAbilityPush, wardenID); n != 1 {
		t.Fatalf("GainedAbilityPush count = %d, want 1 (the cap counts the activation itself)", n)
	}

	// Same turn: the cap binds and the ability is withheld. Float {2} again
	// so an affordability gate cannot fake the withholding.
	addMana(t, e, 0, "CC")
	e.priorityRound()
	if hasGainedAbility(e, wardenID) {
		t.Fatal("GainsAbilitiesLimitPerTurn$ 1 did not withhold the ability after its one activation")
	}

	// Seat 0's next turn: the per-turn window reset and the ability is back.
	// The drive crosses two combats (turn 3 seat 0 and turn 4 seat 1), so it
	// needs a drive that can decline an attackers declaration.
	gainsDriveToStep(t, e, 5, 0, state.StepMain1)
	addMana(t, e, 0, "CC")
	if !hasGainedAbility(e, wardenID) {
		t.Fatal("the per-turn cap did not reset at TurnChange -- the ability is gone for good")
	}
	replayCheck(t, e, cfg)
}

// gainsCarrierSrc is an AUTHORED has-all-abilities-of carrier (never a corpus
// .txt, per the licensing rule) for the r2/r3 regression boards: Idris's
// static shape -- both GainsAbilitiesOf$ and GainsTriggerAbsOf$ over
// Card.ExiledWithSource, scoped to Exile -- without Idris's own enters-trigger
// and Vanishing, whose mandatory hidden artifact search and turn clock would
// interleave with the tests' own machinery. The carrier has no SVar table, so
// a body resolving an SVar off the WRONG (recipient) face reads zero -- which
// is what makes the r3 assertions discriminating rather than vacuous.
func gainsCarrierSrc(t testing.TB) *cards.Card {
	t.Helper()
	return card(t, "Name:Gains Carrier\nManaCost:3\nTypes:Artifact\n"+
		"S:Mode$ Continuous | Affected$ Card.Self | EffectZone$ Battlefield | GainsAbilitiesOf$ Card.ExiledWithSource | GainsTriggerAbsOf$ Card.ExiledWithSource | GainsAbilitiesOfZones$ Exile\n"+
		"Oracle:x\n")
}

// gainsReturnerSrc is the r2-finding foreign fixture: an artifact whose
// trigger fires when a creature you control dies and whose body returns the
// remembered card (the Custodi Squire shape, `DB$ ChangeZone | Defined$
// Remembered | Origin$ Graveyard | Destination$ Hand`). The body reads NO
// SVar -- this test isolates the remembered serialization, not the SVar
// provenance.
func gainsReturnerSrc(t testing.TB) *cards.Card {
	t.Helper()
	return card(t, "Name:Gains Returner\nManaCost:2\nTypes:Artifact\n"+
		"T:Mode$ ChangesZone | ValidCard$ Creature.YouCtrl | Origin$ Battlefield | Destination$ Graveyard | Execute$ TrigReturn | TriggerDescription$ Whenever a creature you control dies, return it to its owner's hand.\n"+
		"SVar:TrigReturn:DB$ ChangeZone | Defined$ Remembered | Origin$ Graveyard | Destination$ Hand\n"+
		"Oracle:x\n")
}

// gainsReaperSrc is the r3-finding foreign fixture for the triggered arm: an
// artifact whose death trigger pumps its recipient by X, where X is an SVar
// on THIS card's face -- so a live-grant-only recovery after the grant ends
// falls back to the recipient's (empty) table and pumps by zero.
func gainsReaperSrc(t testing.TB) *cards.Card {
	t.Helper()
	return card(t, "Name:Gains Reaper\nManaCost:2\nTypes:Artifact\n"+
		"T:Mode$ ChangesZone | ValidCard$ Creature.YouCtrl | Origin$ Battlefield | Destination$ Graveyard | Execute$ TrigPump | TriggerDescription$ Whenever a creature you control dies, it gets +X/+X.\n"+
		"SVar:TrigPump:DB$ Pump | Defined$ Self | NumAtt$ +X | NumDef$ +X\n"+
		"SVar:X:Count$Valid Artifact.YouCtrl\n"+
		"Oracle:x\n")
}

// gainsBleederSrc is the r3-finding foreign fixture for the activated arm:
// "{T}: You lose X life." where X is an SVar on THIS card's face (the Blight
// Pile shape).
func gainsBleederSrc(t testing.TB) *cards.Card {
	t.Helper()
	return card(t, "Name:Gains Bleeder\nManaCost:2\nTypes:Artifact\n"+
		"A:AB$ LoseLife | Cost$ T | Defined$ You | LifeAmount$ X | SpellDescription$ You lose X life.\n"+
		"SVar:X:Count$Valid Artifact.YouCtrl\n"+
		"Oracle:x\n")
}

// gainsWiperSrc is the removal vehicle the r3 tests use to END the grant
// while the gained ability sits on the stack: an artifact whose {T} ability
// puts a card from Exile into its owner's graveyard (the Relic of
// Progenitus shape).
func gainsWiperSrc(t testing.TB) *cards.Card {
	t.Helper()
	return card(t, "Name:Gains Wiper\nManaCost:2\nTypes:Artifact\n"+
		"A:AB$ ChangeZone | Cost$ T | ValidTgts$ Card.inZoneExile | TgtZone$ Exile | Origin$ Exile | Destination$ Graveyard | SpellDescription$ Put target card from exile into its owner's graveyard.\n"+
		"Oracle:x\n")
}

// gainsVictimSrc is the one-shot 1/1 whose death fires the gained triggers.
func gainsVictimSrc(t testing.TB) *cards.Card {
	t.Helper()
	return card(t, "Name:Gains Victim\nManaCost:1\nTypes:Creature\nPT:1/1\nOracle:x\n")
}

// gainsBoardWith seeds the authored carrier and a DIFFERENT authored foreign
// artifact for seat 0, exiles the artifact with the carrier as the exiling
// source (the provenance the static reads), seeds any extra cards onto seat
// 0's battlefield and returns their object ids by card name.
func gainsBoardWith(t *testing.T, foreign *cards.Card, extra ...*cards.Card) (*Engine, Config, state.ObjID, state.ObjID, map[string]state.ObjID) {
	t.Helper()
	carrier := gainsCarrierSrc(t)
	e, cfg := tokenReplGame(t, 9107, append([]*cards.Card{carrier, foreign}, extra...)...)
	carrierID := moveSeededCard(t, e, 0, carrier, state.ZBattlefield)
	foreignID := moveSeededCard(t, e, 0, foreign, state.ZBattlefield)
	e.emit(events.Event{Kind: events.MoveZone, Obj: foreignID, From: state.ZBattlefield,
		To: state.ZExile, IDs: []state.ObjID{carrierID}})
	e.pending = nil
	if o := e.G.Obj(foreignID); o == nil || o.Zone != state.ZExile || o.ExiledWith != carrierID {
		t.Fatalf("foreign card = %+v, want exiled with %d", o, carrierID)
	}
	e.priorityRound()
	ids := map[string]state.ObjID{}
	for _, c := range extra {
		ids[c.Faces[0].Name] = moveSeededCard(t, e, 0, c, state.ZBattlefield)
	}
	e.priorityRound()
	return e, cfg, carrierID, foreignID, ids
}

// gainedTriggerPushEvents returns the GainedTriggerPush events a log carries
// for one recipient, so a test can read the serialized IDs payload.
func gainedTriggerPushEvents(log []events.Event, recipient state.ObjID) []events.Event {
	var out []events.Event
	for _, ev := range log {
		if ev.Kind == events.GainedTriggerPush && ev.Obj == recipient {
			out = append(out, ev)
		}
	}
	return out
}

// gainedStackWrapper finds the resolving gained-ability wrapper on the stack
// (Source = the recipient, Ability = the foreign SA) -- the precondition
// both r3 resolution assertions hang off.
func gainedStackWrapper(t *testing.T, e *Engine, recipient state.ObjID) *state.Object {
	t.Helper()
	for _, id := range e.G.Stack {
		o := e.G.Obj(id)
		if o != nil && o.Zone == state.ZStack && o.Source == recipient && o.Ability != nil {
			return o
		}
	}
	t.Fatalf("no gained-ability wrapper on the stack for recipient %d", recipient)
	return nil
}

// activateGainedAbility activates the one gained ability offered on carrier
// anchored on foreign (the offer precondition rides along).
func activateGainedAbility(t *testing.T, e *Engine, carrierID, foreignID state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %+v, want priority before the activation", d)
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == carrierID && o.GainedSource == foreignID {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("carrier offers no gained ability anchored on %d: %+v", foreignID, d.Options)
}

// submitTargetOn picks the pending target decision's option naming obj.
func submitTargetOn(t *testing.T, e *Engine, obj state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no target decision pending")
	}
	for _, o := range d.Options {
		if o.Obj == obj {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("target decision offers no option on %d: %+v", obj, d.Options)
}

// activateWiperOn activates the wiper's ability and targets obj, ending the
// grant while a gained ability sits on the stack below.
func activateWiperOn(t *testing.T, e *Engine, wiperID, foreignID state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %+v, want priority to activate the wiper", d)
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == wiperID {
			submitChoices(t, e, o.Index)
			submitTargetOn(t, e, foreignID)
			return
		}
	}
	t.Fatalf("the wiper offers no ability: %+v", d.Options)
}

// TestGainedTriggerCarriesItsRememberedReferent is the r2 review's MAJOR:
// pushTrigger serializes the queue-time ctx Remembered after the foreign
// card's provenance slot (IDs[0]), and events.Apply's GainedTriggerPush mint
// must restore it onto the wrapper -- a gained trigger whose body reads
// `Defined$ Remembered` (the Custodi Squire return shape) resolves the
// remembered referent, not an empty set that acts on nobody.
func TestGainedTriggerCarriesItsRememberedReferent(t *testing.T) {
	t.Parallel()
	returner := gainsReturnerSrc(t)
	victim := gainsVictimSrc(t)
	e, cfg, carrierID, foreignID, ids := gainsBoardWith(t, returner, victim)
	victimID := ids["Gains Victim"]
	if o := e.G.Obj(victimID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("victim = %+v, want on the battlefield", o)
	}

	// Kill the victim: the gained trigger fires for the carrier and remembers
	// the dying card (triggerRemembered's ChangesZone capture).
	e.emit(events.Event{Kind: events.Damage, Obj: victimID, Amount: 99})
	e.checkStateBased()
	e.priorityRound()

	pushes := gainedTriggerPushEvents(e.L.Events, carrierID)
	if len(pushes) != 1 {
		t.Fatalf("GainedTriggerPush count = %d, want 1", len(pushes))
	}
	if len(pushes[0].IDs) != 2 || pushes[0].IDs[0] != foreignID || pushes[0].IDs[1] != victimID {
		t.Fatalf("GainedTriggerPush IDs = %v, want [%d %d] (foreign card, remembered victim)",
			pushes[0].IDs, foreignID, victimID)
	}
	// Pre-resolution: the wrapper carries the remembered referent.
	if wr := gainedStackWrapper(t, e, carrierID); len(wr.Remembered) != 1 || wr.Remembered[0].Obj != victimID {
		t.Fatalf("wrapper Remembered = %+v, want the victim %d", wr.Remembered, victimID)
	}

	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(victimID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("victim = %+v, want back in its owner's hand (Defined$ Remembered resolved)", o)
	}
	replayCheck(t, e, cfg)
}

// TestGainedTriggerKeepsItsOwnSVarsAfterTheGrantEnds is the r3 review's
// triggered arm: the exiled card is removed from Exile while the gained
// trigger sits on the stack and the grant ends -- and the trigger's body
// must still resolve X from ITS OWN face's table (+1/+1 on the carrier), not
// from the recipient's (empty) table, which pumps by zero.
func TestGainedTriggerKeepsItsOwnSVarsAfterTheGrantEnds(t *testing.T) {
	t.Parallel()
	reaper := gainsReaperSrc(t)
	wiper := gainsWiperSrc(t)
	victim := gainsVictimSrc(t)
	e, cfg, carrierID, foreignID, ids := gainsBoardWith(t, reaper, wiper, victim)
	wiperID := ids["Gains Wiper"]
	victimID := ids["Gains Victim"]

	// Kill the victim; the gained trigger is pushed and waits on the stack.
	e.emit(events.Event{Kind: events.Damage, Obj: victimID, Amount: 99})
	e.checkStateBased()
	e.priorityRound()
	gainedStackWrapper(t, e, carrierID)

	// While the trigger waits, activate the wiper and put the exiled card
	// into the graveyard: the grant ends with the static's named set.
	activateWiperOn(t, e, wiperID, foreignID)
	passUntilStackEmpty(t, e, 20)

	// The grant is dead: the foreign card is in the graveyard and the
	// carrier no longer offers the foreign card's abilities. Without this
	// the pump assertion below could pass on a grant that never ended.
	if o := e.G.Obj(foreignID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("foreign card = %+v, want in the graveyard", o)
	}
	d := e.Pending()
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == carrierID && o.GainedSource != 0 {
			t.Fatalf("the grant is still live after the exiled card left: %+v", o)
		}
	}

	// The trigger's body resolved X from the FOREIGN face: +2/+2 (X = the
	// two artifacts you control -- the carrier and the wiper), not the
	// recipient's (empty) table's zero.
	if p, tg := e.Power(carrierID), e.Toughness(carrierID); p != 2 || tg != 2 {
		t.Fatalf("gained trigger pumped the carrier to %d/%d, want 2/2 (X = 2 from the foreign face, not the recipient's zero)", p, tg)
	}
	replayCheck(t, e, cfg)
}

// TestGainedActivationKeepsItsOwnSVarsAfterTheGrantEnds is the r3 review's
// activated arm: the exiled card is removed from Exile while the gained
// activation sits on the stack, and the ability's `LifeAmount$ X` still
// resolves from ITS OWN face's SVar table (artifacts you control = 2), not
// from the recipient's (empty) table, which loses zero life.
func TestGainedActivationKeepsItsOwnSVarsAfterTheGrantEnds(t *testing.T) {
	t.Parallel()
	bleeder := gainsBleederSrc(t)
	wiper := gainsWiperSrc(t)
	e, cfg, carrierID, foreignID, ids := gainsBoardWith(t, bleeder, wiper)
	wiperID := ids["Gains Wiper"]

	// Precondition: the carrier offers the gained ability; seat 0's life is
	// at its start-of-turn value.
	lifeBefore := e.G.Players[0].Life
	activateGainedAbility(t, e, carrierID, foreignID)
	gainedStackWrapper(t, e, carrierID)

	// End the grant while the ability waits: the wiper puts the exiled card
	// into the graveyard.
	activateWiperOn(t, e, wiperID, foreignID)
	passUntilStackEmpty(t, e, 20)

	// The grant is dead: the foreign card is in the graveyard and the
	// carrier no longer offers the foreign card's abilities.
	if o := e.G.Obj(foreignID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("foreign card = %+v, want in the graveyard", o)
	}
	d := e.Pending()
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == carrierID && o.GainedSource != 0 {
			t.Fatalf("the grant is still live after the exiled card left: %+v", o)
		}
	}

	// The activation's LifeAmount$ X resolved from the FOREIGN face (X = 2:
	// the carrier and the wiper), not the recipient's zero.
	if got := e.G.Players[0].Life; got != lifeBefore-2 {
		t.Fatalf("seat 0 life %d -> %d, want -2 (X = 2 from the foreign face, not the recipient's zero)", lifeBefore, got)
	}
	replayCheck(t, e, cfg)
}

// gainsSVarManaSrc is the foreign fixture for the gained-mana SVar finding:
// "{T}: Add X {B}, where X is the number of artifacts you control" -- the X
// lives on THIS card's face, so resolving the gained ability against the
// recipient's (empty) table adds nothing.
func gainsSVarManaSrc(t testing.TB) *cards.Card {
	t.Helper()
	return card(t, "Name:Gains Counting Font\nManaCost:2\nTypes:Artifact\n"+
		"A:AB$ Mana | Cost$ T | Produced$ B | Amount$ X | SpellDescription$ Add X {B}.\n"+
		"SVar:X:Count$Valid Artifact.YouCtrl\n"+
		"Oracle:x\n")
}

// gainsSpareArtifactSrc is a vanilla artifact that only raises the
// artifact count the SVar reads.
func gainsSpareArtifactSrc(t testing.TB) *cards.Card {
	t.Helper()
	return card(t, "Name:Gains Spare Cog\nManaCost:1\nTypes:Artifact\nOracle:x\n")
}

// gainedManaOption returns the priority "activate" option on obj, if any.
func gainedManaOption(e *Engine, obj state.ObjID) (decision.Option, bool) {
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		return decision.Option{}, false
	}
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == obj {
			return o, true
		}
	}
	return decision.Option{}, false
}

// TestGainedManaAbilityResolvesItsOwnSVars pins the sol1 finding's SVar half:
// a gained mana ability whose Amount$ X is defined only on the FOREIGN face
// resolves X there (two artifacts: the carrier and a spare cog -> {B}{B}),
// not against the recipient's SVar-less face (which would add nothing).
func TestGainedManaAbilityResolvesItsOwnSVars(t *testing.T) {
	t.Parallel()
	e, cfg, carrierID, _, _ := gainsBoardWith(t, gainsSVarManaSrc(t), gainsSpareArtifactSrc(t))
	if n := len(e.G.Zone(state.ZBattlefield, 0)); n != 2 {
		t.Fatalf("precondition: seat 0 battlefield holds %d objects, want the carrier and the cog", n)
	}
	opt, ok := gainedManaOption(e, carrierID)
	if !ok {
		t.Fatalf("carrier offers no gained mana ability: %+v", e.Pending())
	}
	before := e.G.Players[0].Pool[state.MB]
	submitChoices(t, e, opt.Index)
	if got := e.G.Players[0].Pool[state.MB] - before; got != 2 {
		t.Fatalf("gained mana ability added %d black, want 2 (X from the foreign face's artifact count)", got)
	}
	if o := e.G.Obj(carrierID); o == nil || !o.Tapped {
		t.Fatalf("carrier = %+v, want tapped by the gained mana ability", o)
	}
	replayCheck(t, e, cfg)
}

// gainsCagedManaSrc is the caged card for the capped-mana finding: a NON-tap
// mana ability, "{1}: Add {B}.", so the tap gate cannot mask the per-turn cap
// -- only GainsAbilitiesLimitPerTurn$ can withhold a second activation.
func gainsCagedManaSrc(t testing.TB) *cards.Card {
	t.Helper()
	return card(t, "Name:Caged Filter\nManaCost:2\nTypes:Artifact\n"+
		"A:AB$ Mana | Cost$ 1 | Produced$ B | SpellDescription$ Add {B}.\n"+
		"Oracle:x\n")
}

// TestGainsLimitPerTurnCapsAGainedManaAbility pins the sol1 finding's cap
// half: a GainsAbilitiesLimitPerTurn$ 1 carrier may activate a gained
// non-tap mana ability once per turn. The activation records the replayable
// ManaActivate identity marker (IDs[0] = foreign card), and with {1} floating
// again the ability is withheld for the rest of the turn.
func TestGainsLimitPerTurnCapsAGainedManaAbility(t *testing.T) {
	t.Parallel()
	warden := gainsMairsilFixtureSrc(t)
	caged := gainsCagedManaSrc(t)
	e, cfg := tokenReplGame(t, 9114, warden, caged)
	wardenID := moveSeededCard(t, e, 0, warden, state.ZBattlefield)
	cagedID := moveSeededCard(t, e, 0, caged, state.ZBattlefield)
	e.emit(events.Event{Kind: events.MoveZone, Obj: cagedID, From: state.ZBattlefield,
		To: state.ZExile, IDs: []state.ObjID{wardenID}})
	e.emit(events.Event{Kind: events.CounterChange, Obj: cagedID, Counter: "CAGE", Amount: 1})
	e.pending = nil
	if o := e.G.Obj(cagedID); o == nil || o.Zone != state.ZExile || o.ExiledWith != wardenID || countersOf(o, "CAGE") != 1 {
		t.Fatalf("caged card = %+v, want in exile with provenance and 1 CAGE counter", o)
	}
	addMana(t, e, 0, "C")
	e.priorityRound()
	opt, ok := gainedManaOption(e, wardenID)
	if !ok {
		t.Fatalf("precondition: Warden offers no gained mana ability: %+v", e.Pending())
	}
	submitChoices(t, e, opt.Index)
	if got := e.G.Players[0].Pool[state.MB]; got != 1 {
		t.Fatalf("gained mana ability produced %d black, want 1", got)
	}
	markers := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.ManaActivate && ev.Obj == wardenID {
			if len(ev.IDs) != 1 || ev.IDs[0] != cagedID || ev.Amount != 0 {
				t.Fatalf("gained ManaActivate marker = %+v, want IDs [%d] Amount 0", ev, cagedID)
			}
			markers++
		}
	}
	if markers != 1 {
		t.Fatalf("gained ManaActivate markers = %d, want 1", markers)
	}

	// Same turn, {1} floating again: the cap binds.
	addMana(t, e, 0, "C")
	e.priorityRound()
	if o, ok := gainedManaOption(e, wardenID); ok {
		t.Fatalf("GainsAbilitiesLimitPerTurn$ 1 did not withhold the gained mana ability: %+v", o)
	}
	replayCheck(t, e, cfg)
}
