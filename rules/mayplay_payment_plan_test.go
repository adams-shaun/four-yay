package rules

// A card a may-play permission covers in the graveyard or exile gets the same
// one-click (announce) cast from an empty pool as a hand card (ticket
// fb-20261006T100041Z-ec09a3af): paymentActionsForPriority admits the untyped
// "mayplay" option with Origin "graveyard"/"exile", the planner prices it as
// the offer walk and beginCast do, and the announced cast begins as Mode
// "mayplay". A typed MayPlayText$ permission (Muldrotha) stays withheld.

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const (
	mpGrantSrc = "Name:Grave Grant\nManaCost:1\nTypes:Artifact\n" +
		"S:Mode$ Continuous | Affected$ Creature.YouOwn | MayPlay$ True | EffectZone$ Battlefield | AffectedZone$ Graveyard | Description$ x\nOracle:x\n"
	mpRaiseGrantSrc = "Name:Taxing Grant\nManaCost:1\nTypes:Artifact\n" +
		"S:Mode$ Continuous | Affected$ Creature.YouOwn | MayPlay$ True | RaiseCost$ 1 | EffectZone$ Battlefield | AffectedZone$ Graveyard | Description$ x\nOracle:x\n"
	mpFreeGrantSrc = "Name:Free Grant\nManaCost:1\nTypes:Artifact\n" +
		"S:Mode$ Continuous | Affected$ Creature.YouOwn | MayPlay$ True | MayPlayWithoutManaCost$ True | EffectZone$ Battlefield | AffectedZone$ Graveyard | Description$ x\nOracle:x\n"
	mpBearsSrc = "Name:Grave Bears\nManaCost:1 R\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
)

// mayPlayPlanBoard builds seat 0's board for a graveyard may-play: the grant
// source and `lands` Mountains on the battlefield, Grave Bears in the
// graveyard, an empty pool, and a fresh priority ask.
func mayPlayPlanBoard(t *testing.T, grant string, lands int) (*Engine, state.ObjID) {
	t.Helper()
	e := mayPlayBase(t)
	onBoardGrant(t, e, 0, grant)
	for i := 0; i < lands; i++ {
		onBoard(t, e, 0, apMountain)
	}
	bears := graveyardCard(t, e, mpBearsSrc)
	e.pending = nil
	e.askPriority(0)
	if o := e.G.Obj(bears); o.Zone != state.ZGraveyard {
		t.Fatalf("bears in zone %v, want the graveyard", o.Zone)
	}
	if e.G.Players[0].Pool != (state.Mana{}) {
		t.Fatalf("pool = %v, want empty", e.G.Players[0].Pool)
	}
	return e, bears
}

// mayPlayAction is the payment action the priority decision publishes for id.
func mayPlayAction(e *Engine, id state.ObjID) (decision.PaymentAction, bool) {
	for _, a := range e.EnsurePaymentActions() {
		if a.Cast.Object == id {
			return a, true
		}
	}
	return decision.PaymentAction{}, false
}

// mayPlayOffered reports whether PotentialActions lists a may-play cast of id
// (the offer the plan is a one-click form of) and whether it is typed.
func mayPlayOffered(e *Engine, id state.ObjID) (offered, typed bool) {
	for _, o := range e.PotentialActions(0) {
		if o.Kind == "cast" && o.Obj == id && o.Mode == "mayplay" {
			offered = true
			// A typed permission names itself in the label ("Cast X (Creature)").
			typed = typed || strings.Contains(o.Label, "(")
		}
	}
	return offered, typed
}

// announceAndFill announces a (the planned cast) and answers the window with
// its auto-fill, returning the mana window it opened.
func announceAndFill(t *testing.T, e *Engine, a decision.PaymentAction) {
	t.Helper()
	d := e.Pending()
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Announce: &decision.AnnounceSelection{ActionID: a.ID}}); err != nil {
		t.Fatalf("Submit announce: %v", err)
	}
	w := e.Pending()
	if w == nil || w.ManaPayment == nil {
		t.Fatalf("after announce pending = %#v, want the mana window", w)
	}
	fill := apOption(t, w, decision.OptAutoFill, 0, "")
	apAnswer(t, e, fill)
}

func TestMayPlayGraveyardCastPublishesAPaymentAction(t *testing.T) {
	e, bears := mayPlayPlanBoard(t, mpGrantSrc, 2)
	if offered, typed := mayPlayOffered(e, bears); !offered || typed {
		t.Fatalf("may-play offer: offered=%v typed=%v, want an untyped offer", offered, typed)
	}
	a, ok := mayPlayAction(e, bears)
	if !ok {
		t.Fatalf("no payment action for the graveyard may-play cast of %d", bears)
	}
	if a.Cast.Origin != "graveyard" || len(a.Plans) != 1 || len(a.Plans[0].Activations) != 2 {
		t.Fatalf("action = %#v, want Origin graveyard with a two-Mountain plan", a)
	}
	// The pool is empty, so the legacy option list holds no may-play cast to
	// attach the action to.
	if a.BaseOptionIndex != nil {
		t.Fatalf("BaseOptionIndex = %d with an empty pool, want none", *a.BaseOptionIndex)
	}
	announceAndFill(t, e, a)
	if !slices.Contains(e.G.Stack, bears) || e.G.Obj(bears).Zone != state.ZStack {
		t.Fatalf("bears not on the stack after paying (zone %v, stack %v)", e.G.Obj(bears).Zone, e.G.Stack)
	}
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o.Face().Name == "Mountain" && !o.Tapped {
			t.Fatalf("Mountain %d is untapped: the plan's two sources were not charged", id)
		}
	}
	if e.G.Players[0].Pool != (state.Mana{}) {
		t.Fatalf("pool = %v after the cast, want empty (the cost was exactly {1}{R})", e.G.Players[0].Pool)
	}
}

// The RaiseCost$ surcharge is part of the planned price exactly as beginCast
// charges it: {1}{R} + {1} is three Mountains, and two are not enough.
func TestMayPlayGraveyardPlanIncludesTheRaiseCost(t *testing.T) {
	short, bears := mayPlayPlanBoard(t, mpRaiseGrantSrc, 2)
	if _, ok := mayPlayAction(short, bears); ok {
		t.Fatal("two Mountains published a plan for a {1}{R}+{1} may-play cast")
	}
	e, bears := mayPlayPlanBoard(t, mpRaiseGrantSrc, 3)
	a, ok := mayPlayAction(e, bears)
	if !ok {
		t.Fatalf("three Mountains published no plan for the {1}{R}+{1} cast")
	}
	if got := len(a.Plans[0].Activations); got != 3 {
		t.Fatalf("plan taps %d sources, want 3", got)
	}
	announceAndFill(t, e, a)
	if e.G.Obj(bears).Zone != state.ZStack {
		t.Fatalf("bears in zone %v, want the stack", e.G.Obj(bears).Zone)
	}
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o.Face().Name == "Mountain" && !o.Tapped {
			t.Fatalf("Mountain %d untapped: beginCast charged less than the plan", id)
		}
	}
}

// A typed (MayPlayText$) permission carries its own per-permission riders and
// is withheld: Muldrotha's creature grant publishes no payment action even
// though the may-play cast is offered with its permission key.
func TestMayPlayTypedPermissionPublishesNoPaymentAction(t *testing.T) {
	e := mayPlayBase(t)
	onBoardGrant(t, e, 0, muldrothaSrc)
	for i := 0; i < 2; i++ {
		onBoard(t, e, 0, apMountain)
	}
	bears := graveyardCard(t, e, mpBearsSrc)
	e.pending = nil
	e.askPriority(0)
	if offered, typed := mayPlayOffered(e, bears); !offered || !typed {
		t.Fatalf("may-play offer: offered=%v typed=%v, want a typed offer", offered, typed)
	}
	if a, ok := mayPlayAction(e, bears); ok {
		t.Fatalf("typed permission published %#v, want none", a)
	}
}

// The planner refuses an exile/graveyard origin that does not match where the
// card sits, so a mislabeled origin cannot compose another zone's price.
func TestMayPlayPlanRejectsAMislabeledOrigin(t *testing.T) {
	e, bears := mayPlayPlanBoard(t, mpGrantSrc, 2)
	if got := e.PlanCastPayment(0, decision.PlannedCast{Object: bears, Origin: "graveyard"}); got.Plan == nil {
		t.Fatalf("graveyard origin refused for a graveyard card: %+v", got)
	}
	for _, origin := range []string{"exile", "hand", "command_zone"} {
		if got := e.PlanCastPayment(0, decision.PlannedCast{Object: bears, Origin: origin}); got.Plan != nil {
			t.Fatalf("origin %q planned a card sitting in the graveyard", origin)
		}
	}
}

// Intellect Devourer shape: the permission covers a card the opponent owns, so
// the planner's owner check is relaxed for the may-play origins. The offer's
// own controller check is what gates the cast.
func TestMayPlayPlanAdmitsAnOpponentOwnedCard(t *testing.T) {
	e := mayPlayBase(t)
	onBoardGrant(t, e, 0, "Name:Opp Grant\nManaCost:1\nTypes:Artifact\n"+
		"S:Mode$ Continuous | Affected$ Creature.OppOwn | MayPlay$ True | EffectZone$ Battlefield | AffectedZone$ Graveyard | Description$ x\nOracle:x\n")
	onBoard(t, e, 0, apMountain)
	onBoard(t, e, 0, apMountain)
	o := e.G.AddObject(card(t, mpBearsSrc), 1)
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZGraveyard})
	e.pending = nil
	e.askPriority(0)
	if got := e.G.Obj(o.ID); got.Owner != 1 || got.Zone != state.ZGraveyard {
		t.Fatalf("card owner=%v zone=%v, want seat 1's graveyard", got.Owner, got.Zone)
	}
	if offered, _ := mayPlayOffered(e, o.ID); !offered {
		t.Fatal("the opponent-owned graveyard card is not offered as a may-play cast")
	}
	a, ok := mayPlayAction(e, o.ID)
	if !ok || a.Cast.Origin != "graveyard" {
		t.Fatalf("action = %#v ok=%v, want Origin graveyard", a, ok)
	}
	announceAndFill(t, e, a)
	if got := e.G.Obj(o.ID).Zone; got != state.ZStack {
		t.Fatalf("opponent's card in zone %v, want the stack", got)
	}
}

// A free grant (MayPlayWithoutManaCost$) plans an empty-mana witness, as a
// zero-cost hand card does, and the action points at the offered may-play
// option because that option is affordable from the empty pool.
func TestMayPlayFreeGraveyardCastPlansNoMana(t *testing.T) {
	e, bears := mayPlayPlanBoard(t, mpFreeGrantSrc, 0)
	a, ok := mayPlayAction(e, bears)
	if !ok || a.Cast.Origin != "graveyard" || len(a.Plans[0].Activations) != 0 {
		t.Fatalf("action = %#v ok=%v, want a graveyard plan with no activations", a, ok)
	}
	if a.BaseOptionIndex == nil {
		t.Fatal("free may-play action has no BaseOptionIndex")
	}
	if o := e.Pending().Options[*a.BaseOptionIndex]; o.Kind != "cast" || o.Obj != bears || o.Mode != "mayplay" {
		t.Fatalf("BaseOptionIndex names %#v, want the may-play cast", o)
	}
}

// The planned (Intent.Payment) route begins the same cast as Mode "mayplay"
// and settles it with the witnessed taps.
func TestMayPlayGraveyardPlannedPaymentCompletesTheCast(t *testing.T) {
	e, bears := mayPlayPlanBoard(t, mpGrantSrc, 2)
	a, ok := mayPlayAction(e, bears)
	if !ok {
		t.Fatalf("no payment action for %d", bears)
	}
	d := e.Pending()
	sel := &decision.PaymentSelection{ActionID: a.ID, Plan: a.Plans[0]}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Payment: sel}); err != nil {
		t.Fatalf("Submit planned payment: %v", err)
	}
	if got := e.G.Obj(bears).Zone; got != state.ZStack {
		t.Fatalf("bears in zone %v, want the stack", got)
	}
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o.Face().Name == "Mountain" && !o.Tapped {
			t.Fatalf("Mountain %d untapped after the planned payment", id)
		}
	}
}
