package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func corpusAlternativeCard(t *testing.T, name string) *cards.Card {
	t.Helper()
	c, ok := testutil.CorpusRegistry(t).Lookup(name)
	if !ok {
		t.Fatalf("corpus missing %s", name)
	}
	return c
}

func castMode(t *testing.T, e *Engine, id state.ObjID, mode string) {
	t.Helper()
	e.beginCast(0, decision.Option{Kind: "cast", Obj: id, Mode: mode})
}

func TestBuybackConstantMistsReturnsToHand(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Constant Mists"))
	mtn := card(t, "Name:Land\nTypes:Basic Land Mountain\nA:AB$ Mana | Cost$ T | Produced$ R\nOracle:x\n")
	land := e.G.AddObject(mtn, 0)
	land.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{land.ID})
	mist := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MG] = 1, 1
	castMode(t, e, mist, "buyback")
	if d := e.Pending(); d == nil || d.Options[0].Kind != "sacrifice" {
		t.Fatalf("buyback did not ask to sacrifice its real Land cost: %+v", d)
	}
	submitChoices(t, e, 0)
	if e.G.Obj(land.ID).Zone != state.ZGraveyard || e.G.Obj(mist).Zone != state.ZStack {
		t.Fatalf("buyback payment zones land=%s spell=%s", e.G.Obj(land.ID).Zone, e.G.Obj(mist).Zone)
	}
	e.resolveTop()
	if e.G.Obj(mist).Zone != state.ZHand {
		t.Fatalf("Constant Mists after buyback = %s, want hand", e.G.Obj(mist).Zone)
	}
}

func TestBuybackSearingTouchFizzleGoesToGraveyard(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Searing Touch"))
	target := e.G.AddObject(card(t, "Name:Target\nTypes:Creature Human\nPT:1/1\nOracle:x\n"), 1)
	target.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{target.ID})
	spell := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MR], e.G.Players[0].Pool[state.MC] = 1, 4
	castMode(t, e, spell, "buyback")
	d := e.Pending()
	targetChoice := -1
	for i, o := range d.Options {
		if o.Obj == target.ID {
			targetChoice = i
			break
		}
	}
	if targetChoice < 0 {
		t.Fatalf("Searing Touch target missing: %+v", d)
	}
	submitChoices(t, e, targetChoice)
	e.emit(events.Event{Kind: events.MoveZone, Obj: target.ID, From: state.ZBattlefield, To: state.ZGraveyard})
	e.resolveTop()
	if got := e.G.Obj(spell).Zone; got != state.ZGraveyard {
		t.Fatalf("fizzled Buyback spell went to %s, want graveyard", got)
	}
}

// suspendToFinalCounter drives a suspended Profane Tutor to the upkeep whose
// TIME counter hits 0 and returns the may-cast ask the engine poses there.
func suspendToFinalCounter(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	e := handEngine(t, corpusAlternativeCard(t, "Profane Tutor"))
	profane := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MB] = 1
	e.G.Players[0].Pool[state.MC] = 1
	castMode(t, e, profane, "suspend")
	if o := e.G.Obj(profane); o.Zone != state.ZExile || o.Counter("TIME") != 2 || o.CastFlags&state.FlagSuspend == 0 {
		t.Fatalf("Suspend did not create marked exile card: %+v", o)
	}
	e.beginTurn(0)
	if got := e.G.Obj(profane).Counter("TIME"); got != 1 {
		t.Fatalf("first suspend upkeep TIME=%d, want 1", got)
	}
	e.beginTurn(0)
	d := e.Pending()
	if d == nil || len(d.Options) != 2 || d.Options[0].Kind != "suspend_cast_yes" || d.Options[1].Kind != "suspend_cast_no" {
		t.Fatalf("final suspend counter did not offer the CR 702.62a may-cast ask: %+v", d)
	}
	return e, profane
}

func TestSuspendProfaneTutorHasProvenanceAndOptionalCast(t *testing.T) {
	t.Parallel()
	e, profane := suspendToFinalCounter(t)
	// CR 702.62a: "you may play it without paying its mana cost if able" --
	// answering the offer casts it; option 0 is Cast, option 1 is Leave.
	submitChoices(t, e, 0)
	if e.G.Obj(profane).Zone != state.ZStack {
		t.Fatalf("accepted suspend cast left the card in %s, want stack", e.G.Obj(profane).Zone)
	}
}

func TestSuspendDeclinedCastStaysInExileAndTurnContinues(t *testing.T) {
	t.Parallel()
	e, profane := suspendToFinalCounter(t)
	// A decline (or a stale offer) leaves the card in exile with its Suspend
	// provenance, and the upkeep completes: the next step is the draw step.
	submitChoices(t, e, 1)
	o := e.G.Obj(profane)
	if o.Zone != state.ZExile || o.CastFlags&state.FlagSuspend == 0 || o.Counter("TIME") != 0 {
		t.Fatalf("declined suspend cast moved the card: %+v", o)
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("decline left an unexpected decision pending: %+v", d)
	}
	// The decline resumes the ordinary flow: the same upkeep priority the
	// no-suspend path grants (step()'s default branch). Passing it reaches
	// the draw step, so nothing wedged.
	driveToStep(t, e, 3, 0, state.StepDraw)
	if e.G.Step != state.StepDraw {
		t.Fatalf("declined suspend cast wedged the turn at %s", e.G.Step)
	}
}

func TestSuspendOrdinaryExiledCardNeverGetsTheOffer(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Profane Tutor"))
	// A different exiled copy with zero TIME is not a suspended card.
	other := e.G.AddObject(corpusAlternativeCard(t, "Profane Tutor"), 0)
	other.Zone = state.ZExile
	e.G.SetZone(state.ZExile, 0, append(e.G.Zone(state.ZExile, 0), other.ID))
	e.suspendedCasts = nil
	e.beginTurn(0)
	if other.Zone != state.ZExile {
		t.Fatalf("ordinary exiled Suspend card was cast for free: %s", other.Zone)
	}
	if d := e.Pending(); d != nil {
		t.Fatalf("ordinary exiled Suspend card was offered a cast: %+v", d)
	}
}

func TestSuspendDoesNotCastThroughCantBeCast(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Profane Tutor"))
	profane := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MB], e.G.Players[0].Pool[state.MC] = 1, 1
	castMode(t, e, profane, "suspend")

	// A mandatory Suspend cast is still a cast. This live CantBeCast static
	// makes it illegal rather than merely unaffordable, so "if able" leaves
	// the zero-counter Profane Tutor in exile and never starts a cast flow.
	restrictor := e.G.AddObject(card(t, "Name:Restrictor\nTypes:Artifact\nS:Mode$ CantBeCast | ValidCard$ Card\nOracle:x\n"), 1)
	restrictor.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{restrictor.ID})
	e.beginTurn(0)
	e.beginTurn(0)
	if o := e.G.Obj(profane); o.Zone != state.ZExile || o.Counter("TIME") != 0 || e.cast != nil {
		t.Fatalf("restricted suspended spell was cast: %+v pending=%+v", o, e.cast)
	}
}

func TestSuspendXBenalishCommanderAnnouncesTimeAndCost(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Benalish Commander"))
	commander := e.G.Zone(state.ZHand, 0)[0]
	if info, ok := suspendCost(e.G.Obj(commander).Face()); !ok || !info.timeX || info.minTime != 1 || info.cost.X != 1 {
		t.Fatalf("Benalish Suspend parse = %+v, %v", info, ok)
	}
	e.G.Players[0].Pool[state.MC] = 1
	e.G.Players[0].Pool[state.MW] = 2
	castMode(t, e, commander, "suspend")
	if e.cast == nil || !e.cast.suspendTimeX || e.cast.suspendMinX != 1 {
		t.Fatalf("Suspend X pending cast = %+v", e.cast)
	}
	d := e.Pending()
	if d == nil || len(d.Options) != 1 || d.Options[0].Kind != "x" || d.Options[0].Amount != 1 {
		t.Fatalf("Suspend X did not offer only its payable XMin1 choice: %+v", d)
	}
	submitChoices(t, e, 0)
	o := e.G.Obj(commander)
	if o.Zone != state.ZExile || o.Counter("TIME") != 1 || o.X != 1 || o.CastFlags&state.FlagSuspend == 0 {
		t.Fatalf("Suspend X did not share announced X between cost and time: %+v", o)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("Suspend X cost left mana behind: %v", e.G.Players[0].Pool)
	}
}

func TestConvokeCrowdsFavorCommitsChosenCreature(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Crowd's Favor"))
	creature := card(t, "Name:Red Druid\nManaCost:R\nTypes:Creature Elf\nPT:1/1\nA:AB$ Mana | Cost$ T | Produced$ R\nOracle:x\n")
	id := e.G.AddObject(creature, 0)
	id.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{id.ID})
	spell := e.G.Zone(state.ZHand, 0)[0]
	if !e.HasKeyword(spell, "Convoke") {
		t.Fatal("corpus Crowd's Favor lost Convoke before cast")
	}
	castMode(t, e, spell, "")
	d := e.Pending()
	if d == nil || len(d.Options) == 0 || d.Options[0].Kind != "convoke_R" {
		t.Fatalf("Convoke did not announce a coloured creature payment: %+v", d)
	}
	submitChoices(t, e, 0)
	if e.cast == nil || !e.convokeCommitted(e.cast, id.ID) {
		t.Fatal("chosen Convoke creature was not reserved before payment")
	}
	// The mandatory target is the same creature. Once target selection is
	// answered, its Tap is the payment and it never supplied mana as well.
	submitChoices(t, e, 0)
	if !e.G.Obj(id.ID).Tapped || e.G.Players[0].Pool[state.MR] != 0 {
		t.Fatalf("Convoke creature was not exclusively tapped as payment: tapped=%v pool=%v", e.G.Obj(id.ID).Tapped, e.G.Players[0].Pool)
	}
}

func TestConvokeMarchOfMultitudesAnnouncesEveryCreatureAndFundsX(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "March of the Multitudes"))
	for i := 0; i < 3; i++ {
		c := card(t, "Name:White Helper\nManaCost:W\nTypes:Creature Human\nPT:1/1\nOracle:x\n")
		o := e.G.AddObject(c, 0)
		o.Zone = state.ZBattlefield
		e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), o.ID))
	}
	e.G.Players[0].Pool[state.MG] = 1
	spell := e.G.Zone(state.ZHand, 0)[0]
	castMode(t, e, spell, "")
	d := e.Pending()
	if d == nil || d.Options == nil {
		t.Fatalf("March did not ask for Convoke: %+v", d)
	}
	// Choose two white contributions and one generic contribution in ONE
	// legal multi-select answer. The generic helper is what pays X=1.
	var whites, generic []int
	for i, o := range d.Options {
		switch o.Kind {
		case "convoke_W":
			whites = append(whites, i)
		case "convoke_generic":
			generic = append(generic, i)
		}
	}
	if len(whites) < 2 || len(generic) < 1 {
		t.Fatalf("March Convoke options whites=%v generic=%v all=%+v", whites, generic, d.Options)
	}
	submitChoices(t, e, whites[0], whites[1], generic[2])
	d = e.Pending()
	// X=0 would leave the announced generic helper reducing nothing, so the
	// X ask offers exactly the one absorbing value: X=1.
	if d == nil || len(d.Options) != 1 || d.Options[0].Kind != "x" || d.Options[0].Amount != 1 {
		t.Fatalf("Convoke-funded X=1 was not offered: %+v", d)
	}
	submitChoices(t, e, 0)
	if e.cast != nil || e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("March did not complete its Convoke/X payment: cast=%+v zone=%s", e.cast, e.G.Obj(spell).Zone)
	}
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if !e.G.Obj(id).Tapped {
			t.Fatalf("selected Convoke creature %d was not tapped", id)
		}
	}
}

func TestHarmonizeZenithFestivalFundsX(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	spell := e.G.AddObject(corpusAlternativeCard(t, "Zenith Festival"), 0)
	spell.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 0, []state.ObjID{spell.ID})
	helper := e.G.AddObject(card(t, "Name:Three Power Helper\nTypes:Creature Human\nPT:3/3\nOracle:x\n"), 0)
	helper.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{helper.ID})
	e.G.Players[0].Pool[state.MR] = 2
	castMode(t, e, spell.ID, "harmonize")
	d := e.Pending()
	if d == nil || len(d.Options) != 1 || d.Options[0].Kind != "harmonize" {
		t.Fatalf("Harmonize X contribution was not offered: %+v", d)
	}
	submitChoices(t, e, 0)
	d = e.Pending()
	// X=0 would leave the announced 3-power creature reducing nothing (the
	// {0}{R}{R} rest is already payable from the pool), so the X ask offers
	// only the values that absorb the whole announcement: 1, 2, 3.
	if d == nil || len(d.Options) != 3 || d.Options[2].Amount != 3 {
		t.Fatalf("Harmonize-funded X options are not the absorbing 1..3: %+v", d)
	}
	submitChoices(t, e, 2)
	if e.G.Obj(spell.ID).Zone != state.ZStack || !e.G.Obj(helper.ID).Tapped {
		t.Fatalf("Harmonize X cast did not pay and tap: spell=%s helper tapped=%v", e.G.Obj(spell.ID).Zone, e.G.Obj(helper.ID).Tapped)
	}
}

func TestHarmonizeWildRideUsesAnnouncedPower(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Wild Ride"))
	creature := card(t, "Name:Four Power Druid\nTypes:Creature Elf\nPT:4/4\nA:AB$ Mana | Cost$ T | Produced$ R\nOracle:x\n")
	id := e.G.AddObject(creature, 0)
	id.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{id.ID})
	e.G.Players[0].Pool[state.MR] = 1
	spell := e.G.Zone(state.ZHand, 0)[0]
	castMode(t, e, spell, "harmonize")
	d := e.Pending()
	if d == nil || d.Options[0].Kind != "harmonize" || d.Options[0].Amount != 4 {
		t.Fatalf("Harmonize did not offer the creature's chosen power: %+v", d)
	}
	submitChoices(t, e, 0)
	submitChoices(t, e, 0) // target the creature
	if !e.G.Obj(id.ID).Tapped || e.G.Players[0].Pool[state.MR] != 0 {
		t.Fatalf("Harmonize did not pay after announced reduction: tapped=%v pool=%v", e.G.Obj(id.ID).Tapped, e.G.Players[0].Pool)
	}
}

// TestHarmonizeAnnouncementSkipsNonCreaturePower pins the creature gate on the
// CR 601.2b announcement (convokeAsk): Harmonize's payment is creatures only
// (CR 702.46a, harmonizePayment's own filter), so a non-creature permanent that
// still carries a P/T -- an uncrewed Vehicle -- must be neither offered as a
// harmonize payment nor credited by the offer gate (harmonizePayment), which
// reads the same filter. A 5/3 Vehicle beside the 4/4 creature the existing
// Wild Ride pin uses must be absent from the announcement and untapped after
// payment.
func TestHarmonizeAnnouncementSkipsNonCreaturePower(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Wild Ride"))
	vehicle := card(t, "Name:Test Vehicle\nTypes:Artifact Vehicle\nPT:5/3\nOracle:x\n")
	vid := e.G.AddObject(vehicle, 0)
	vid.Zone = state.ZBattlefield
	creature := card(t, "Name:Four Power Druid\nTypes:Creature Elf\nPT:4/4\nA:AB$ Mana | Cost$ T | Produced$ R\nOracle:x\n")
	cid := e.G.AddObject(creature, 0)
	cid.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{vid.ID, cid.ID})
	e.G.Players[0].Pool[state.MR] = 1
	spell := e.G.Zone(state.ZHand, 0)[0]
	castMode(t, e, spell, "harmonize")
	d := e.Pending()
	if d == nil || d.Options[0].Kind != "harmonize" {
		t.Fatalf("Harmonize did not announce its creature payment: %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == vid.ID {
			t.Fatalf("Harmonize announcement offered the non-creature Vehicle: %+v", o)
		}
		if o.Obj == cid.ID && o.Amount != 4 {
			t.Fatalf("Harmonize offered the creature for the wrong power: %+v", o)
		}
	}
	submitChoices(t, e, 0)
	submitChoices(t, e, 0) // target the creature
	if !e.G.Obj(cid.ID).Tapped || e.G.Obj(vid.ID).Tapped || e.G.Players[0].Pool[state.MR] != 0 {
		t.Fatalf("Harmonize payment tapped the wrong permanents: creature=%v vehicle=%v pool=%v",
			e.G.Obj(cid.ID).Tapped, e.G.Obj(vid.ID).Tapped, e.G.Players[0].Pool)
	}
}

func TestConvokeOverSelectionIsRejectedAndResubmitted(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Crowd's Favor"))
	for i := 0; i < 2; i++ {
		creature := card(t, "Name:Red Druid\nManaCost:R\nTypes:Creature Elf\nPT:1/1\nA:AB$ Mana | Cost$ T | Produced$ R\nOracle:x\n")
		id := e.G.AddObject(creature, 0)
		id.Zone = state.ZBattlefield
		e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), id.ID))
	}
	spell := e.G.Zone(state.ZHand, 0)[0]
	castMode(t, e, spell, "")
	d := e.Pending()
	if d == nil || len(d.Options) != 2 || d.Options[0].Kind != "convoke_R" || d.Options[1].Kind != "convoke_R" {
		t.Fatalf("Convoke did not offer both red creatures: %+v", d)
	}
	if d.Max != 1 {
		t.Fatalf("Convoke offer Max=%d, want 1: the {R} cost absorbs exactly one contribution", d.Max)
	}
	// The over-selection the finding traces: two distinct groups pass the
	// decision's static Validate, but the {R} cost absorbs only one
	// contribution, so the second creature would be tapped for nothing.
	// Submit must reject the answer and keep THIS decision pending for a
	// legal resubmission, exactly like validateAttackers.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}); err == nil {
		t.Fatal("over-selection of two colour contributions for one {R} was accepted")
	}
	if p := e.Pending(); p == nil || p.Seq != d.Seq {
		t.Fatalf("rejected over-selection did not preserve the pending decision: %+v", p)
	}
	submitChoices(t, e, 0)
	submitChoices(t, e, 0) // the target ask
	tapped := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if e.G.Obj(id).Tapped {
			tapped++
		}
	}
	if tapped != 1 {
		t.Fatalf("exactly one convoke creature may be tapped as payment, got %d", tapped)
	}
	if e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("legal single-contribution resubmission did not cast: spell in %s", e.G.Obj(spell).Zone)
	}
}

func TestConvokeXAnnouncementPricesEveryCreature(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "March of the Multitudes"))
	for i := 0; i < 2; i++ {
		c := card(t, "Name:White Helper\nManaCost:W\nTypes:Creature Human\nPT:1/1\nOracle:x\n")
		o := e.G.AddObject(c, 0)
		o.Zone = state.ZBattlefield
		e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), o.ID))
	}
	e.G.Players[0].Pool[state.MG] = 1
	e.G.Players[0].Pool[state.MW] = 2
	spell := e.G.Zone(state.ZHand, 0)[0]
	castMode(t, e, spell, "")
	d := e.Pending()
	var generic []int
	for i, o := range d.Options {
		if o.Kind == "convoke_generic" {
			generic = append(generic, i)
		}
	}
	if len(generic) != 2 {
		t.Fatalf("both creatures were not offered as generic contributions: %+v", d.Options)
	}
	// With {X} unfixed the generic requirement is open, so the announcement
	// of two generic contributions is accepted at answer time (CR 601.2b
	// announces Convoke before X) and xAsk prices it: only the X values
	// whose cost absorbs BOTH creatures are offered.
	submitChoices(t, e, generic[0], generic[1])
	d = e.Pending()
	if d == nil || len(d.Options) != 1 || d.Options[0].Kind != "x" || d.Options[0].Amount != 2 {
		t.Fatalf("X ask did not narrow to the only absorbing value X=2: %+v", d)
	}
	submitChoices(t, e, 0)
	if e.cast != nil || e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("convoke-funded X=2 cast did not complete: cast=%+v zone=%s", e.cast, e.G.Obj(spell).Zone)
	}
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if !e.G.Obj(id).Tapped {
			t.Fatalf("announced convoke creature %d was not tapped", id)
		}
	}
}

func TestHarmonizePaysDerivedPowerNotPrinted(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Wild Ride"))
	creature := card(t, "Name:Boosted Druid\nTypes:Creature Elf\nPT:1/1\nA:AB$ Mana | Cost$ T | Produced$ R\nOracle:x\n")
	id := e.G.AddObject(creature, 0)
	id.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{id.ID})
	// +3/+1 in +1/+1 counters: the CR 613.4-derived power is 4, the printed
	// power is 1. Harmonize {4}{R} must be payable from the {R} pool alone.
	e.emit(events.Event{Kind: events.CounterChange, Obj: id.ID, Counter: "P1P1", Amount: 3})
	e.G.Players[0].Pool[state.MR] = 1
	spell := e.G.Zone(state.ZHand, 0)[0]
	castMode(t, e, spell, "harmonize")
	d := e.Pending()
	if d == nil || len(d.Options) != 1 || d.Options[0].Kind != "harmonize" {
		t.Fatalf("Harmonize was not offered over the boosted creature: %+v", d)
	}
	if d.Options[0].Amount != 4 {
		t.Fatalf("Harmonize credited printed power %d, want the derived 4", d.Options[0].Amount)
	}
	submitChoices(t, e, 0)
	submitChoices(t, e, 0) // target the creature
	if !e.G.Obj(id.ID).Tapped || e.G.Players[0].Pool[state.MR] != 0 {
		t.Fatalf("Harmonize did not pay by derived power: tapped=%v pool=%v", e.G.Obj(id.ID).Tapped, e.G.Players[0].Pool)
	}
}

func TestHarmonizeReducedDerivedPowerPaysOnlyItsPower(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Wild Ride"))
	creature := card(t, "Name:Diminished Druid\nTypes:Creature Elf\nPT:4/4\nA:AB$ Mana | Cost$ T | Produced$ R\nOracle:x\n")
	id := e.G.AddObject(creature, 0)
	id.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{id.ID})
	// Three -1/-1 counters: the CR 613.4-derived power is 1, the printed 4.
	// Harmonize {4}{R} then reduces by 1, leaving {3}{R} for the pool.
	e.emit(events.Event{Kind: events.CounterChange, Obj: id.ID, Counter: "M1M1", Amount: 3})
	e.G.Players[0].Pool[state.MR] = 1
	e.G.Players[0].Pool[state.MC] = 3
	spell := e.G.Zone(state.ZHand, 0)[0]
	castMode(t, e, spell, "harmonize")
	d := e.Pending()
	if d == nil || len(d.Options) != 1 || d.Options[0].Kind != "harmonize" {
		t.Fatalf("Harmonize was not offered over the diminished creature: %+v", d)
	}
	if d.Options[0].Amount != 1 {
		t.Fatalf("Harmonize credited printed power %d, want the derived 1", d.Options[0].Amount)
	}
	submitChoices(t, e, 0)
	submitChoices(t, e, 0) // target the creature
	if !e.G.Obj(id.ID).Tapped || e.G.Players[0].Pool[state.MR] != 0 || e.G.Players[0].Pool[state.MC] != 0 {
		t.Fatalf("Harmonize did not pay by derived power: tapped=%v pool=%v", e.G.Obj(id.ID).Tapped, e.G.Players[0].Pool)
	}
}

func TestTransmuteAndCyclingRealHandActivations(t *testing.T) {
	t.Parallel()
	t.Run("Dizzy Spell searches matching mana value", func(t *testing.T) {
		e := handEngine(t, corpusAlternativeCard(t, "Dizzy Spell"))
		// Transmute searches for a card with the source's PRINTED mana value
		// (Dizzy Spell's is one). handEngine's library is all Mountains, and a
		// land has no mana cost, so its mana value is zero: the unmodified
		// library holds no legal search result and the search would correctly
		// fail to find. Seed one mana-value-one card for the search to find.
		seed := card(t, "Name:Ember Study\nManaCost:U\nTypes:Instant\nOracle:x\n")
		sObj := e.G.AddObject(seed, 0)
		e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{sObj.ID}, e.G.Zone(state.ZLibrary, 0)...))
		wanted := sObj.ID
		id := e.G.Zone(state.ZHand, 0)[0]
		e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MU] = 1, 2
		var opt decision.Option
		for _, o := range e.legalActions(0) {
			if o.Kind == "ability" && o.Obj == id {
				opt = o
				break
			}
		}
		if opt.Kind != "ability" {
			t.Fatal("Dizzy Spell Transmute activation not offered")
		}
		e.beginActivation(0, opt)
		submitChoices(t, e, 0) // discard Dizzy Spell
		if e.G.Obj(id).Zone != state.ZGraveyard {
			t.Fatalf("Transmute discard zone=%s", e.G.Obj(id).Zone)
		}
		e.resolveTop()
		d := e.Pending()
		choice := -1
		if d != nil {
			for i, o := range d.Options {
				if o.Obj == wanted {
					choice = i
					break
				}
			}
		}
		if choice < 0 {
			t.Fatalf("Transmute did not offer matching mana-value library card: %+v", d)
		}
		submitChoices(t, e, choice)
		if e.G.Obj(wanted).Zone != state.ZHand {
			t.Fatalf("Transmute target zone=%s, want hand", e.G.Obj(wanted).Zone)
		}
	})
	t.Run("Ziatora's Proving Ground cycling draws", func(t *testing.T) {
		e := handEngine(t, corpusAlternativeCard(t, "Ziatora's Proving Ground"))
		drawn := e.G.Zone(state.ZLibrary, 0)[0]
		id := e.G.Zone(state.ZHand, 0)[0]
		e.G.Players[0].Pool[state.MC] = 3
		var opt decision.Option
		for _, o := range e.legalActions(0) {
			if o.Kind == "ability" && o.Obj == id {
				opt = o
				break
			}
		}
		if opt.Kind != "ability" {
			t.Fatal("Ziatora cycling activation not offered")
		}
		e.beginActivation(0, opt)
		submitChoices(t, e, 0) // discard the cycling card
		if e.G.Obj(id).Zone != state.ZGraveyard {
			t.Fatalf("Cycling discard zone=%s", e.G.Obj(id).Zone)
		}
		e.resolveTop()
		if e.G.Obj(drawn).Zone != state.ZHand {
			t.Fatalf("Cycling did not draw: zone=%s", e.G.Obj(drawn).Zone)
		}
	})
}

// TestTypeCyclingSearchesTheNamedType is the CR 702.28d headline: typed
// cycling is a LIBRARY SEARCH for a card of the named type, revealed and put
// into hand -- NOT a draw. Monstrosity of the Lake prints "Islandcycling {2}"
// (K:TypeCycling:Island:2), so it must find the seeded Island and not a
// Mountain, and the stated-quality ChangeType$ must make the search reveal
// the found card by default.
func TestTypeCyclingSearchesTheNamedType(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Monstrosity of the Lake"))
	island := card(t, "Name:Island\nTypes:Basic Land Island\nA:AB$ Mana | Cost$ T | Produced$ U\nOracle:x\n")
	sObj := e.G.AddObject(island, 0)
	e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{sObj.ID}, e.G.Zone(state.ZLibrary, 0)...))
	wanted := sObj.ID
	id := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 2
	var opt decision.Option
	for _, o := range e.legalActions(0) {
		if o.Kind == "ability" && o.Obj == id {
			opt = o
			break
		}
	}
	if opt.Kind != "ability" {
		t.Fatal("Monstrosity of the Lake Islandcycling activation not offered")
	}
	e.beginActivation(0, opt)
	submitChoices(t, e, 0) // discard Monstrosity of the Lake
	if e.G.Obj(id).Zone != state.ZGraveyard {
		t.Fatalf("TypeCycling discard zone=%s, want graveyard", e.G.Obj(id).Zone)
	}
	e.resolveTop()
	d := e.Pending()
	choice := -1
	for i, o := range d.Options {
		if o.Obj == wanted {
			choice = i
		}
		// An Islandcycling search must not find a Mountain (a different land
		// type). The library is otherwise all Mountains.
		if o.Obj != wanted && e.G.Obj(o.Obj).Face().Name == "Mountain" {
			t.Fatalf("Islandcycling offered a non-Island: %+v", o)
		}
	}
	if choice < 0 {
		t.Fatalf("TypeCycling did not offer the seeded Island: %+v", d)
	}
	submitChoices(t, e, choice)
	if e.G.Obj(wanted).Zone != state.ZHand {
		t.Fatalf("TypeCycling searched card zone=%s, want hand", e.G.Obj(wanted).Zone)
	}
	revealed := false
	for _, ev := range revealNotes(e) {
		for _, rid := range ev.IDs {
			if rid == wanted {
				revealed = true
			}
		}
	}
	if !revealed {
		t.Fatal("TypeCycling search did not reveal the found card")
	}
}

// TestTypeCyclingBasicLandSearchesAnyBasic covers the Basic base (CR 702.28d's
// "Basic landcycling"): the filter must match ANY basic land card, not one
// named type. Kree Sentinel prints K:TypeCycling:Basic:2.
func TestTypeCyclingBasicLandSearchesAnyBasic(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Kree Sentinel"))
	forest := card(t, "Name:Forest\nTypes:Basic Land Forest\nA:AB$ Mana | Cost$ T | Produced$ G\nOracle:x\n")
	sObj := e.G.AddObject(forest, 0)
	e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{sObj.ID}, e.G.Zone(state.ZLibrary, 0)...))
	wanted := sObj.ID
	id := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 2
	var opt decision.Option
	for _, o := range e.legalActions(0) {
		if o.Kind == "ability" && o.Obj == id {
			opt = o
			break
		}
	}
	if opt.Kind != "ability" {
		t.Fatal("Kree Sentinel basic landcycling activation not offered")
	}
	e.beginActivation(0, opt)
	submitChoices(t, e, 0) // discard Kree Sentinel
	e.resolveTop()
	d := e.Pending()
	choice := -1
	for i, o := range d.Options {
		if o.Obj == wanted {
			choice = i
		}
	}
	if choice < 0 {
		t.Fatalf("basic landcycling did not offer the seeded Forest: %+v", d)
	}
	submitChoices(t, e, choice)
	if e.G.Obj(wanted).Zone != state.ZHand {
		t.Fatalf("basic landcycling searched card zone=%s, want hand", e.G.Obj(wanted).Zone)
	}
}

func TestCastWithFlashHonorsScriptGates(t *testing.T) {
	e := handEngine(t, card(t, "Name:Slow Spell\nManaCost:0\nTypes:Sorcery\nA:SP$ Draw | Defined$ You\nOracle:x\n"))
	source := e.G.AddObject(card(t, "Name:Gated Flash\nTypes:Artifact\nS:Mode$ CastWithFlash | ValidCard$ Card | ValidSA$ Spell | Caster$ You | IsPresent$ Creature.YouCtrl\nOracle:x\n"), 0)
	source.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{source.ID})
	e.G.Active = 1
	spell := e.G.Zone(state.ZHand, 0)[0]
	if hasCastOption(e.legalActions(0), spell) {
		t.Fatal("unmet IsPresent$ incorrectly granted flash")
	}
	creature := e.G.AddObject(card(t, "Name:Gate Creature\nTypes:Creature Human\nPT:1/1\nOracle:x\n"), 0)
	creature.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{source.ID, creature.ID})
	if !hasCastOption(e.legalActions(0), spell) {
		t.Fatal("met IsPresent$ did not grant flash")
	}
	// The same static scoped to an activation only. A compiled node's Params
	// is immutable once its ParamSet is bound (cards/params.go), so the
	// variant is its own card rather than an in-place edit of the first.
	source.Zone = state.ZGraveyard
	equipOnly := e.G.AddObject(card(t, "Name:Equip Flash\nTypes:Artifact\nS:Mode$ CastWithFlash | ValidCard$ Card | ValidSA$ Activated.Equip | Caster$ You | IsPresent$ Creature.YouCtrl\nOracle:x\n"), 0)
	equipOnly.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{equipOnly.ID, creature.ID})
	e.G.SetZone(state.ZGraveyard, 0, []state.ObjID{source.ID})
	if hasCastOption(e.legalActions(0), spell) {
		t.Fatal("activation-only ValidSA$ incorrectly granted spell flash")
	}
}

func TestVedalkenOrreryAppliesOutsideHand(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	orrery := e.G.AddObject(corpusAlternativeCard(t, "Vedalken Orrery"), 0)
	orrery.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{orrery.ID})
	e.G.Active = 1
	flashback := card(t, "Name:Slow Recall\nManaCost:0\nTypes:Sorcery\nK:Flashback:0\nA:SP$ Draw | Defined$ You | NumCards$ 1\nOracle:x\n")
	gy := e.G.AddObject(flashback, 0)
	gy.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 0, []state.ObjID{gy.ID})
	if !hasCastOption(e.legalActions(0), gy.ID) {
		t.Fatal("Vedalken Orrery did not give a Flashback sorcery instant timing")
	}
	e.format = FormatCommander
	cmd := e.G.AddObject(card(t, "Name:Slow Commander\nManaCost:0\nTypes:Creature Human\nPT:1/1\nOracle:x\n"), 0)
	cmd.Zone = state.ZCommand
	e.G.Players[0].Commanders = []state.ObjID{cmd.ID}
	e.G.Players[0].CmdCasts = []int32{0}
	e.G.SetZone(state.ZCommand, 0, []state.ObjID{cmd.ID})
	if !hasCastOption(e.legalActions(0), cmd.ID) {
		t.Fatal("Vedalken Orrery did not give a command-zone spell instant timing")
	}
}

func TestGemstoneCavernsOpeningHandEffect(t *testing.T) {
	t.Parallel()
	gem := corpusAlternativeCard(t, "Gemstone Caverns")
	fill := card(t, "Name:Filler\nTypes:Basic Land\nOracle:x\n")
	for seed := uint64(1); seed < 200; seed++ {
		deck := make([]*cards.Card, 40)
		deck[0] = gem
		for i := 1; i < len(deck); i++ {
			deck[i] = fill
		}
		e := New(Config{Seed: seed, Names: []string{"a", "b"}, Decks: [][]*cards.Card{deck, deck}})
		if e.Pending() == nil || e.Pending().Kind != decision.KChoose || e.Pending().Player == e.opening.start {
			continue
		}
		d := e.Pending()
		if d.Options[0].Kind != "opening_yes" {
			continue
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatal(err)
		}
		d = e.Pending()
		if d == nil || d.Options[0].Kind != "opening_exile" {
			t.Fatalf("Gemstone did not continue to mandatory exile: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatal(err)
		}
		var caverns *state.Object
		for _, id := range e.G.Zone(state.ZBattlefield, 0) {
			if e.G.Obj(id).Face().Name == "Gemstone Caverns" {
				caverns = e.G.Obj(id)
			}
		}
		if caverns == nil || caverns.Counter("LUCK") != 1 || len(e.G.Zone(state.ZExile, 0)) != 1 {
			t.Fatalf("Gemstone result=%+v exile=%v", caverns, e.G.Zone(state.ZExile, 0))
		}
		return
	}
	t.Fatal("could not construct non-starting Gemstone opening hand")
}

func TestGemstoneCavernsIsOfferedOnlyAfterMulligans(t *testing.T) {
	t.Parallel()
	gem := corpusAlternativeCard(t, "Gemstone Caverns")
	fill := card(t, "Name:Filler\nTypes:Basic Land\nOracle:x\n")
	deck := func() []*cards.Card {
		out := make([]*cards.Card, 40)
		out[0] = gem
		for i := 1; i < len(out); i++ {
			out[i] = fill
		}
		return out
	}
	for seed := uint64(1); seed < 1000; seed++ {
		e := New(Config{Seed: seed, Names: []string{"a", "b"}, Decks: [][]*cards.Card{deck(), deck()}, Mulligans: 1})
		e.Advance()
		// The first pregame decision is always London, never an opening effect.
		if d := e.Pending(); d == nil || d.Kind != decision.KMulligan {
			t.Fatalf("opening effect preceded mulligan: %+v", d)
		}
		mulliganed := false
		for e.Pending() != nil && e.Pending().Kind == decision.KMulligan {
			d := e.Pending()
			choose := 0
			if !mulliganed && d.Player != e.mulligan.seats[0] {
				for _, id := range e.G.Zone(state.ZHand, d.Player) {
					if e.G.Obj(id).Face().Name == "Gemstone Caverns" {
						choose, mulliganed = 1, true
						break
					}
				}
			}
			submitChoices(t, e, choose)
			if mulliganed && choose == 1 {
				// Keep the redrawn hand only when the Caverns did not return.
				still := false
				for _, id := range e.G.Zone(state.ZHand, d.Player) {
					still = still || e.G.Obj(id).Face().Name == "Gemstone Caverns"
				}
				if still {
					break
				}
			}
		}
		if !mulliganed || e.Pending() == nil || e.Pending().Kind == decision.KMulligan {
			continue
		}
		if d := e.Pending(); d != nil && len(d.Options) > 0 && d.Options[0].Kind == "opening_yes" {
			t.Fatalf("mulliganed-away Gemstone was offered from a final hand without it: %+v", d)
		}
		return
	}
	t.Fatal("could not construct a mulliganed-away non-starting Gemstone hand")
}

func TestChancellorOpeningEffectRegistersAndRunsItsPhaseTrigger(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Chancellor of the Tangle"))
	id := e.G.Zone(state.ZHand, 0)[0]
	e.applyOpeningEffect(openingEffect{player: 0, card: id, svar: "RevealCard"})
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].Execute != "EffMana" || e.G.Delayed[0].Phase != state.StepMain1 {
		t.Fatalf("Chancellor opening Effect did not register its real phase child: %+v", e.G.Delayed)
	}
	// handEngine starts from a genesis priority decision; this direct
	// pregame setup owns no live decision before its new first turn.
	e.pending = nil
	e.beginTurn(0)
	turn := e.G.Turn
	e.Advance()
	driveToStep(t, e, turn, 0, state.StepMain1)
	if len(e.G.Stack) == 0 {
		t.Fatal("Chancellor opening trigger did not reach the stack")
	}
	e.resolveTop()
	if e.G.Players[0].Pool[state.MG] != 1 {
		t.Fatalf("Chancellor trigger mana=%v, want one green", e.G.Players[0].Pool)
	}
}

func TestImpatientIguanaBecomesStartingPlayer(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Impatient Iguana"))
	id := e.G.Zone(state.ZHand, 0)[0]
	e.pending = nil
	e.opening = openingRound{start: 1, effects: []openingEffect{{player: 0, card: id, svar: "RevealCard"}}}
	e.stepOpening()
	submitChoices(t, e, 0)
	if e.G.Active != 0 {
		t.Fatalf("Impatient Iguana did not become starting player: active=%d", e.G.Active)
	}
}

func TestOpeningHandRevealActionAndPlayFirstGate(t *testing.T) {
	t.Parallel()
	chancellor := corpusAlternativeCard(t, "Chancellor of the Tangle")
	iguana := corpusAlternativeCard(t, "Impatient Iguana")
	fill := card(t, "Name:Filler\nTypes:Basic Land\nOracle:x\n")
	deck := func(c *cards.Card) []*cards.Card {
		out := make([]*cards.Card, 40)
		out[0] = c
		for i := 1; i < len(out); i++ {
			out[i] = fill
		}
		return out
	}
	foundChancellor := false
	for seed := uint64(1); seed < 200; seed++ {
		e := New(Config{Seed: seed, Names: []string{"a", "b"}, Decks: [][]*cards.Card{deck(chancellor), deck(chancellor)}})
		d := e.Pending()
		if d == nil || d.Options[0].Kind != "opening_yes" {
			continue
		}
		id := d.Options[0].Obj
		if e.G.Obj(id).Face().Name != "Chancellor of the Tangle" || e.opening.effects[e.opening.index].svar != "RevealCard" {
			t.Fatalf("Chancellor opening action = %+v, want RevealCard", e.opening.effects)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatal(err)
		}
		revealed := false
		for _, ev := range e.L.Events {
			if ev.Kind == events.Note {
				for _, got := range ev.IDs {
					revealed = revealed || got == id
				}
			}
		}
		if !revealed || e.G.Obj(id).Zone != state.ZHand {
			t.Fatalf("Chancellor did not reveal from opening hand: revealed=%v zone=%s", revealed, e.G.Obj(id).Zone)
		}
		foundChancellor = true
		break
	}
	if !foundChancellor {
		t.Fatal("could not construct Chancellor opening hand")
	}
	for seed := uint64(1); seed < 200; seed++ {
		e := New(Config{Seed: seed, Names: []string{"a", "b"}, Decks: [][]*cards.Card{deck(iguana), deck(iguana)}})
		d := e.Pending()
		if d == nil || d.Options[0].Kind != "opening_yes" {
			continue
		}
		if d.Player == e.opening.start {
			t.Fatalf("!PlayFirst offered Impatient Iguana to starting player %d", d.Player)
		}
		return
	}
	t.Fatal("could not construct opening hands for real reveal scripts")
}

// Keep events imported above in this file's behavioural setup, rather than
// hand-writing state mutation; this compile-time assertion documents that all
// alternative-cost tests use the event boundary for game changes.
var _ = events.Event{}

// altCastOptions returns the alternative-cost cast options legalActions
// offered for id (AltCostIndex > 0; the paid cast carries the zero value).
func altCastOptions(opts []decision.Option, id state.ObjID) []decision.Option {
	var out []decision.Option
	for _, o := range opts {
		if o.Kind == "cast" && o.Obj == id && o.AltCostIndex > 0 {
			out = append(out, o)
		}
	}
	return out
}

// TestDazeAltCostGatedOnAnIslandToReturn pins the two-param shape: Daze's
// EffectZone$ All + ValidSA$ Spell.Self self-carried static reaches the
// hand, but the Cost$ Return<1/Island> candidate gate keeps the alternative
// unoffered until the payer controls an Island. Taking the alternative
// returns the Island at cast commit, beside the other payments.
func TestDazeAltCostGatedOnAnIslandToReturn(t *testing.T) {
	t.Parallel()
	daze := corpusAlternativeCard(t, "Daze")
	e := handEngine(t, daze)
	dazeID := e.G.Zone(state.ZHand, 0)[0]

	// A spell on the stack so the counterspell has a legal target either way.
	bolt := e.G.AddObject(card(t, "Name:Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"), 1)
	bolt.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 1, []state.ObjID{bolt.ID})

	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MU] = 1, 1

	if alts := altCastOptions(e.legalActions(0), dazeID); len(alts) != 0 {
		t.Fatalf("Daze alt-cost offered with no Island on the battlefield: %+v", alts)
	}
	if !hasCastOption(e.legalActions(0), dazeID) {
		t.Fatal("the paid cast is still offered without an Island")
	}

	// EffectZone$ is a real gate: a Daze-shaped static pointed at the
	// battlefield (Daze names All) withdraws the grant from the hand.
	// Synthetic, not the corpus card: registry lookups are shared pointers.
	bfDaze := card(t, "Name:Battlefield Daze\nManaCost:1 U\nTypes:Instant\n"+
		"A:SP$ Counter | TargetType$ Spell | ValidTgts$ Card\n"+
		"S:Mode$ AlternativeCost | ValidSA$ Spell.Self | EffectZone$ Battlefield | Cost$ Return<1/Island>\nOracle:x\n")
	e2 := handEngine(t, bfDaze)
	bfID := e2.G.Zone(state.ZHand, 0)[0]
	stack := e2.G.AddObject(card(t, "Name:Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"), 1)
	stack.Zone = state.ZStack
	e2.G.SetZone(state.ZStack, 1, []state.ObjID{stack.ID})
	isl := e2.G.AddObject(card(t, "Name:Island\nTypes:Basic Land Island\nOracle:x\n"), 0)
	isl.Zone = state.ZBattlefield
	e2.G.SetZone(state.ZBattlefield, 0, []state.ObjID{isl.ID})
	if alts := altCastOptions(e2.legalActions(0), bfID); len(alts) != 0 {
		t.Fatalf("EffectZone$ Battlefield grant still reached the hand: %+v", alts)
	}

	land := e.G.AddObject(card(t, "Name:Island\nTypes:Basic Land Island\nOracle:x\n"), 0)
	land.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{land.ID})

	alts := altCastOptions(e.legalActions(0), dazeID)
	if len(alts) != 1 {
		t.Fatalf("Daze with an Island in play: alt-cost options = %+v, want exactly one", alts)
	}
	e.beginCast(0, alts[0])
	// The Return<1/Island> payment asks over the payer's battlefield...
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "returncost" {
		t.Fatalf("Daze alt-cost did not ask to return the Island: %+v", e.Pending())
	}
	submitChoices(t, e, 0)
	// ...then the counter's target ask...
	d := e.Pending()
	targetChoice := -1
	for i, o := range d.Options {
		if o.Obj == bolt.ID {
			targetChoice = i
			break
		}
	}
	if targetChoice < 0 {
		t.Fatalf("Daze target ask missing the stack spell: %+v", d)
	}
	submitChoices(t, e, targetChoice)
	// ...and the cast commits with the Island back in the payer's hand.
	if got := e.G.Obj(land.ID).Zone; got != state.ZHand {
		t.Fatalf("Daze alt-cost left the Island in %s, want hand", got)
	}
	if got := e.G.Obj(dazeID).Zone; got != state.ZStack {
		t.Fatalf("free-cast Daze sits in %s, want stack", got)
	}
}

// TestDeadlyRollickAltCostGatedOnCommanderPresence pins the four-param
// shape: ValidPlayer$ You (the caster is the static's controller),
// EffectZone$ All, ValidSA$ Spell and -- the one that actually gates --
// IsPresent$ Card.IsCommander+YouCtrl, which counts battlefield permanents
// only, so a commander in the command zone does not unlock the free cast.
func TestDeadlyRollickAltCostGatedOnCommanderPresence(t *testing.T) {
	t.Parallel()
	rollick := corpusAlternativeCard(t, "Deadly Rollick")
	e := handEngine(t, rollick)
	rollickID := e.G.Zone(state.ZHand, 0)[0]

	prey := e.G.AddObject(card(t, "Name:Prey\nTypes:Creature Human\nPT:2/2\nOracle:x\n"), 1)
	prey.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{prey.ID})
	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MB] = 3, 1

	// No commander anywhere: IsPresent$ blocks the free cast; only the
	// paid 3 B cast is on the table.
	if alts := altCastOptions(e.legalActions(0), rollickID); len(alts) != 0 {
		t.Fatalf("Deadly Rollick free-cast offered with no commander: %+v", alts)
	}
	if !hasCastOption(e.legalActions(0), rollickID) {
		t.Fatal("the paid cast is still offered without a commander")
	}

	// A commander in the command zone is not a permanent you control:
	// IsPresent$ scans the battlefield only.
	cmd := e.G.AddObject(card(t, "Name:Commander\nManaCost:2 G\nTypes:Creature Human\nPT:2/2\nOracle:x\n"), 0)
	cmd.Zone = state.ZCommand
	e.G.SetZone(state.ZCommand, 0, []state.ObjID{cmd.ID})
	e.G.Players[0].Commanders = []state.ObjID{cmd.ID}
	if alts := altCastOptions(e.legalActions(0), rollickID); len(alts) != 0 {
		t.Fatal("a command-zone commander incorrectly satisfied IsPresent$")
	}

	// The commander enters the battlefield: the free cast is offered.
	e.emit(events.Event{Kind: events.MoveZone, Obj: cmd.ID, From: state.ZCommand, To: state.ZBattlefield, Text: "commander entered the battlefield"})
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{cmd.ID})
	e.G.SetZone(state.ZCommand, 0, nil)

	alts := altCastOptions(e.legalActions(0), rollickID)
	if len(alts) != 1 {
		t.Fatalf("Deadly Rollick with a commander in play: alt-cost options = %+v, want exactly one", alts)
	}
	e.beginCast(0, alts[0])
	// Cost$ 0 pays nothing: the only ask is the exile target.
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("free-cast Deadly Rollick did not go straight to targets: %+v", d)
	}
	targetChoice := -1
	for i, o := range d.Options {
		if o.Obj == prey.ID {
			targetChoice = i
			break
		}
	}
	if targetChoice < 0 {
		t.Fatalf("Deadly Rollick target ask missing the creature: %+v", d)
	}
	submitChoices(t, e, targetChoice)
	if got := e.G.Obj(rollickID).Zone; got != state.ZStack {
		t.Fatalf("free-cast Deadly Rollick sits in %s, want stack", got)
	}
	if got := e.G.Obj(prey.ID).Zone; got != state.ZBattlefield {
		t.Fatalf("target exiled at cast time? prey in %s", got)
	}
}

// TestAlternativeCostValidPlayerScopesTheOffer pins ValidPlayer$ being read
// (You = the static's controller: seat 0 casting its own card matches) and
// the fail-closed direction for an unevaluable ValidSA$ Spell constraint:
// the free cast is denied, never silently widened.
func TestAlternativeCostValidPlayerScopesTheOffer(t *testing.T) {
	t.Parallel()
	freebie := card(t, "Name:Freebie\nManaCost:1 U\nTypes:Instant\n"+
		"A:SP$ Draw | Defined$ You | NumCards$ 1\n"+
		"S:Mode$ AlternativeCost | ValidSA$ Spell | ValidPlayer$ You | Cost$ 0\nOracle:x\n")
	e := handEngine(t, freebie)
	freeID := e.G.Zone(state.ZHand, 0)[0]

	if alts := altCastOptions(e.legalActions(0), freeID); len(alts) != 1 {
		t.Fatalf("controller's own free cast missing: %+v", alts)
	}
	if !hasCastOption(e.legalActions(0), freeID) {
		t.Fatal("the paid cast is still offered")
	}

	// An unevaluable ValidSA$ Spell constraint fails closed (the grant is
	// not withdrawn silently into a paid cast): the alternative disappears.
	scoped := card(t, "Name:Scoped\nManaCost:1 U\nTypes:Instant\n"+
		"A:SP$ Draw | Defined$ You | NumCards$ 1\n"+
		"S:Mode$ AlternativeCost | ValidSA$ Spell.Samurai | Cost$ 0\nOracle:x\n")
	e2 := handEngine(t, scoped)
	scopedID := e2.G.Zone(state.ZHand, 0)[0]
	if alts := altCastOptions(e2.legalActions(0), scopedID); len(alts) != 0 {
		t.Fatalf("unevaluable ValidSA$ constraint still granted the free cast: %+v", alts)
	}
}
