package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func suspendCorpusCard(t *testing.T, name string) *cards.Card {
	t.Helper()
	c, ok := testutil.CorpusRegistry(t).Lookup(name)
	if !ok {
		t.Fatalf("corpus missing %s", name)
	}
	return c
}

// giveSuspendThroughDoctor resolves The Tenth Doctor's REAL compiled
// GiveSuspend body against one remembered exiled card, exactly as the card's
// own DB$ Pump does. It is the one grant path every test here shares, so none
// of them can drift onto a hand-built replacement SA.
func giveSuspendThroughDoctor(t *testing.T, e *Engine, doctorID, targetID state.ObjID) {
	t.Helper()
	f := e.G.Obj(doctorID).Face()
	if f == nil {
		t.Fatal("the Doctor has no face")
	}
	sa := cards.ResolveSVar(f.SVars, "GiveSuspend")
	if sa == nil || sa.API != "Pump" {
		t.Fatalf("the Doctor's GiveSuspend SVar did not compile to a Pump body: %+v", sa)
	}
	if got := sa.Params["Defined"]; got != "Remembered.withoutSuspend" {
		t.Fatalf("the compiled GiveSuspend Defined$ = %q, want the real Remembered.withoutSuspend filter", got)
	}
	effects.Resolve(e, &effects.Ctx{Source: doctorID, Controller: 0, SVars: f.SVars,
		Remembered: []state.Target{{Obj: targetID}}}, sa)
}

// TestTenthDoctorExileGainsSuspendAndCasts drives the real GiveSuspend body
// from The Tenth Doctor's compiled SVar onto a NON-Suspend exiled card,
// reaches the ordinary upkeep tick, accepts the final-counter cast choice and
// asserts the card reaches the stack. A grant that only affected the layer-6
// keyword view would never tick TIME and never reach the suspend cast queue.
func TestTenthDoctorExileGainsSuspendAndCasts(t *testing.T) {
	t.Parallel()
	doctor := suspendCorpusCard(t, "The Tenth Doctor")
	spell := suspendCorpusCard(t, "Grizzly Bears") // no printed Suspend
	e := handEngine(t, doctor, spell)
	doctorID, spellID := e.G.Zone(state.ZHand, 0)[0], e.G.Zone(state.ZHand, 0)[1]
	e.emit(events.Event{Kind: events.MoveZone, Obj: doctorID, From: state.ZHand, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.MoveZone, Obj: spellID, From: state.ZHand, To: state.ZExile})

	// PRECONDITION the grant depends on: the card has no suspend yet, so the
	// clear body really is what grants it (a printed-Suspend card would make
	// the grant vacuous and the filter reject it).
	if e.HasKeyword(spellID, "Suspend") {
		t.Fatal("fixture card already has suspend; the grant would be vacuous")
	}
	if !effects.MatchesSpecFrom(e.G, "Card.withoutSuspend", spellID, 0, spellID) {
		t.Fatal("precondition: the card should be withoutSuspend before the grant")
	}

	giveSuspendThroughDoctor(t, e, doctorID, spellID)

	o := e.G.Obj(spellID)
	if o.Zone != state.ZExile || !o.SuspendGranted {
		t.Fatalf("Tenth Doctor grant = zone %s granted %v, want exile+granted", o.Zone, o.SuspendGranted)
	}
	if !e.HasKeyword(spellID, "Suspend") {
		t.Fatalf("granted suspend was not derived as a keyword: %v", e.Derived(spellID).Keywords)
	}
	if !effects.MatchesSpecFrom(e.G, "Card.withSuspend", spellID, 0, spellID) {
		t.Fatal("Card.withSuspend did not see the granted keyword")
	}

	// The real DBPutCounter body precedes the grant on the card, but
	// effPutCounter only places counters on battlefield objects (reported as
	// an issue), so the three time counters the Doctor's PutCounter body
	// supplies are placed directly here.
	e.emit(events.Event{Kind: events.CounterChange, Obj: spellID, Counter: "TIME", Amount: 3})

	// The first two upkeeps decrement TIME. The third must pose the normal
	// suspend cast choice, proving the granted card is in the same queue.
	e.beginTurn(0)
	e.beginTurn(0)
	if got := o.Counter("TIME"); got != 1 {
		t.Fatalf("granted suspend TIME after two upkeeps = %d, want 1", got)
	}
	e.beginTurn(0)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) < 2 || d.Options[0].Kind != "suspend_cast_yes" {
		t.Fatalf("granted suspend final counter did not ask to cast: %+v", d)
	}
	// Accept the cast: the granted card must reach the stack.
	submitChoices(t, e, 0)
	if !hasEvent(e, events.PutOnStack, spellID) {
		t.Fatalf("accepting the suspend cast did not put %d on the stack", spellID)
	}
	if got := e.G.Obj(spellID).Zone; got != state.ZStack {
		t.Fatalf("granted suspend cast ended in %s, want stack", got)
	}
}

// TestRoseTylerBadWolfCountsSuspendedCards grants suspend to a real exiled
// card through the Doctor's compiled body and then resolves Rose Tyler's REAL
// compiled TrigPutCounter SVar, so Card.suspended is exercised end to end by
// the grant's own consumer.
func TestRoseTylerBadWolfCountsSuspendedCards(t *testing.T) {
	t.Parallel()
	doctor := suspendCorpusCard(t, "The Tenth Doctor")
	rose := suspendCorpusCard(t, "Rose Tyler")
	spell := suspendCorpusCard(t, "Grizzly Bears")
	e := handEngine(t, doctor, rose, spell)
	doctorID, roseID, spellID := e.G.Zone(state.ZHand, 0)[0], e.G.Zone(state.ZHand, 0)[1], e.G.Zone(state.ZHand, 0)[2]
	e.emit(events.Event{Kind: events.MoveZone, Obj: doctorID, From: state.ZHand, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.MoveZone, Obj: roseID, From: state.ZHand, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.MoveZone, Obj: spellID, From: state.ZHand, To: state.ZExile})

	if effects.MatchesSpecFrom(e.G, "Card.suspended", spellID, 0, spellID) {
		t.Fatal("precondition: the card is suspended before it has any time counters or grant")
	}
	giveSuspendThroughDoctor(t, e, doctorID, spellID)
	// TIME > 0 is part of Card.suspended; supplied directly because
	// effPutCounter cannot place on an exiled card (see the Doctor test).
	e.emit(events.Event{Kind: events.CounterChange, Obj: spellID, Counter: "TIME", Amount: 1})
	if !effects.MatchesSpecFrom(e.G, "Card.suspended", spellID, 0, spellID) {
		t.Fatal("precondition: the granted exiled card with TIME should now be suspended")
	}

	f := e.G.Obj(roseID).Face()
	sa := cards.ResolveSVar(f.SVars, "TrigPutCounter")
	if sa == nil || sa.API != "PutCounter" {
		t.Fatalf("Rose's TrigPutCounter SVar did not compile to a PutCounter body: %+v", sa)
	}
	effects.Resolve(e, &effects.Ctx{Source: roseID, Controller: 0, SVars: f.SVars}, sa)
	if got := e.G.Obj(roseID).Counter("TIME"); got != 1 {
		t.Fatalf("Rose Bad Wolf TIME = %d, want 1 from one suspended card", got)
	}
}

// TestFaceOfBoeCastsSuspendedSpellAtSuspendCost activates The Face of Boe's
// REAL compiled AB$ Play through the ordinary ability offer, proving the
// Valid$ Card.withSuspend population (a non-Suspend card in hand is NOT
// offered), the optional selection, and the SuspendCost transaction that
// puts the chosen card on the stack without its printed mana cost.
func TestFaceOfBoeCastsSuspendedSpellAtSuspendCost(t *testing.T) {
	t.Parallel()
	boe := suspendCorpusCard(t, "The Face of Boe")
	spell := suspendCorpusCard(t, "Profane Tutor") // K:Suspend:2:1 B
	nonSuspend := suspendCorpusCard(t, "Grizzly Bears")
	e := handEngine(t, boe, spell, nonSuspend)
	hand := e.G.Zone(state.ZHand, 0)
	boeID, spellID, nonSuspendID := hand[0], hand[1], hand[2]
	e.emit(events.Event{Kind: events.MoveZone, Obj: boeID, From: state.ZHand, To: state.ZBattlefield})
	e.G.Obj(boeID).SummonSick = false

	// PRECONDITION: the chosen spell has printed suspend and the other card
	// does not, so the population's filter is what selects it.
	if !effects.MatchesSpecFrom(e.G, "Card.withSuspend", spellID, 0, spellID) {
		t.Fatal("precondition: Profane Tutor should have suspend")
	}
	if effects.MatchesSpecFrom(e.G, "Card.withSuspend", nonSuspendID, 0, nonSuspendID) {
		t.Fatal("precondition: Grizzly Bears should NOT have suspend")
	}

	var opt *decision.Option
	// {1}{B} is Profane Tutor's suspend cost; an empty pool makes the
	// transaction unpayable and the test unable to distinguish the paths.
	e.G.Players[0].Pool[state.MC] = 1
	e.G.Players[0].Pool[state.MB] = 1
	for _, o := range e.legalActions(0) {
		if o.Kind == "ability" && o.Obj == boeID {
			c := o
			opt = &c
			break
		}
	}
	if opt == nil {
		t.Fatal("The Face of Boe's Play ability was not offered")
	}
	e.beginActivation(0, *opt)

	// Resolve the activated ability onto the stack and drive it to its Play
	// selection. resolveTop asks for the mode, so the loop stops there.
	var d *decision.Decision
	for i := 0; i < 60; i++ {
		if d = e.Pending(); d != nil && d.Kind == decision.KModes && d.ResumeKind == "play" {
			break
		}
		if len(e.pendingTriggers) > 0 {
			e.putTriggersOnStack()
			continue
		}
		if len(e.G.Stack) > 0 {
			e.resolveTop()
			continue
		}
		break
	}
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "play" {
		t.Fatalf("Face of Boe AB did not pose the Play selection: %+v", d)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != spellID {
		t.Fatalf("Play population = %+v, want exactly the one suspended spell (Valid$ Card.withSuspend)", d.Options)
	}
	submitChoices(t, e, 0)

	if !hasEvent(e, events.PutOnStack, spellID) {
		t.Fatalf("the SuspendCost play did not put the chosen spell on the stack")
	}
	if got := e.G.Obj(spellID).Zone; got != state.ZStack {
		t.Fatalf("Face of Boe SuspendCost play ended in %s, want stack", got)
	}
	if pool := e.G.Players[0].Pool.Total(); pool != 0 {
		t.Fatalf("mana pool after the SuspendCost play = %d, want 0 (the printed mana cost was never charged)", pool)
	}
	if !e.G.Obj(boeID).Tapped {
		t.Fatal("activating the {T} ability did not tap The Face of Boe")
	}
}

func TestDynamicSuspendGrantIsDerivedAndFilterable(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	c := card(t, "Name:Grantable\nManaCost:2 U\nTypes:Creature\nPT:2/2\nOracle:x\n")
	o := e.G.AddObject(c, 0)
	o.Zone = state.ZExile
	e.G.SetZone(state.ZExile, 0, []state.ObjID{o.ID})
	e.emit(events.Event{Kind: events.CounterChange, Obj: o.ID, Counter: "TIME", Amount: 2})
	e.emit(events.Event{Kind: events.AlterAttribute, Obj: o.ID, Text: "Suspend", Amount: 1})
	if !o.SuspendGranted || !e.HasKeyword(o.ID, "Suspend") {
		t.Fatalf("granted suspend was not derived: granted=%v keywords=%v", o.SuspendGranted, e.Derived(o.ID).Keywords)
	}
	if !effects.MatchesSpecFrom(e.G, "Card.withSuspend", o.ID, 0, o.ID) || !effects.MatchesSpecFrom(e.G, "Card.suspended", o.ID, 0, o.ID) {
		t.Fatal("suspend predicates did not see the granted keyword")
	}
	if effects.MatchesSpecFrom(e.G, "Card.withoutSuspend", o.ID, 0, o.ID) {
		t.Fatal("Card.withoutSuspend matched a card with granted suspend")
	}
}

func TestFaceOfBoeSuspendCostUsesChosenCardKeyword(t *testing.T) {
	t.Parallel()
	f := card(t, "Name:Suspended\nManaCost:5 R\nTypes:Sorcery\nK:Suspend:3:1 R\nOracle:x\n").Faces[0]
	got, ok := pricePlayCost(f, "SuspendCost")
	if !ok || got.Generic != 1 || got.Colored[state.MR] != 1 {
		t.Fatalf("SuspendCost = %+v, %v; want {1}{R}", got, ok)
	}
}
