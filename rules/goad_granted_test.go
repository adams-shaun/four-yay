package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// staticgoad1: a Goad$ True static DELIVERED by an Effect (DB$ Effect |
// StaticAbilities$) or by a DB$ Clone AddStaticAbilities$ rider must goad
// exactly like the printed S: route (rules/static_goad_test.go pins that
// half), and the Creature.IsGoaded filter predicate must read the static
// route too. The cards below are the real corpus carriers the AGENTS.md row
// named: Hot Pursuit, Immortal Obligation and Mocking Doppelganger.

// goadGrantEngine is staticGoadEngine's 3-seat toss-pinned board with the
// goadGrant hand convention on top: hand cards for seat 0 only, every hand
// zone cleared, Main1, seat 0 active and on priority -- the shape the cast
// flows below need (a granted goad's goader is the granting spell's
// controller, so the victim must have a second possible defender for the
// CR 701.38b "attack a player other than the goader" property to bite).
func goadGrantEngine(t *testing.T, hand ...*cards.Card) *Engine {
	t.Helper()
	e := New(seatZeroStart(Config{Seed: 1, Names: []string{"a", "b", "c"}, Decks: [][]*cards.Card{
		mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40),
	}}))
	for p := state.PlayerID(0); p < 3; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	var ids []state.ObjID
	for _, c := range hand {
		o := e.G.AddObject(c, 0)
		o.Zone = state.ZHand
		ids = append(ids, o.ID)
	}
	e.G.SetZone(state.ZHand, 0, ids)
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1
	return e
}

// grantGoadTargetAsk drains the stack until the pending decision is the
// suspect/return target ask, returning it. Every resolveTop either completes
// a stack entry or parks on the ask; the loop bounds the drain so a registry
// change that leaves the ask unposed fails the test instead of hanging it.
func grantGoadTargetAsk(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	for i := 0; i < 6; i++ {
		if d := e.Pending(); d != nil {
			if d.Kind != decision.KTarget {
				t.Fatalf("expected the target ask, got %+v", d)
			}
			return d
		}
		if len(e.G.Stack) == 0 {
			t.Fatal("stack drained without a target ask")
		}
		e.resolveTop()
	}
	t.Fatal("target ask never parked")
	return nil
}

// assertStaticGoadBite is the shared CR 701.38b/508.1d tail: the goaded
// creature's only offered attack is the non-goader defender, Required, and
// the engine enforces both halves.
func assertStaticGoadBite(t *testing.T, e *Engine, victim state.ObjID, goader state.PlayerID) {
	t.Helper()
	other := state.PlayerID(0)
	for _, p := range e.G.AliveFrom(0) {
		if p != victim2controller(e, victim) && p != goader {
			other = p
			break
		}
	}
	// The declare-attackers window the requirement read lives in: the
	// victim's controller active, no stale pending decision.
	e.pending = nil
	e.G.Step = state.StepDeclareAttackers
	e.G.Active = victim2controller(e, victim)
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected a KAttackers decision, got %+v", d)
	}
	mine := -1
	for _, o := range d.Options {
		if o.Obj == victim {
			mine = o.Index
			if o.Player != other || !o.Required {
				t.Fatalf("statically goaded creature's option = %+v, want Required attack at player %d", o, other)
			}
		}
	}
	if mine < 0 {
		t.Fatalf("statically goaded creature offered no attack option: %+v", d.Options)
	}
	// CR 508.1d: the declaration must include every required attacker.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: victim2controller(e, victim)}); err == nil {
		t.Fatal("statically goaded creature was allowed to skip its required attack")
	}
	var choices []int
	for _, o := range d.Options {
		if o.Required {
			choices = append(choices, o.Index)
		}
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: victim2controller(e, victim), Choices: choices}); err != nil {
		t.Fatalf("legal non-goader attack rejected: %v", err)
	}
	o := e.G.Obj(victim)
	if !o.IsAttacking || o.Attacking != other {
		t.Fatalf("statically goaded creature attacked %d, want %d", o.Attacking, other)
	}
}

// victim2controller reads the victim's controller -- the player whose
// declare-attackers decision the assertions drive.
func victim2controller(e *Engine, victim state.ObjID) state.PlayerID {
	return e.G.Obj(victim).Controller
}

// TestEffectGrantedGoadHotPursuitForcesAttackAway casts the real Hot Pursuit,
// answers its entering suspect trigger against an opponent's creature, and
// proves the granted `Mode$ Continuous | Affected$ Creature.IsRemembered |
// Goad$ True` static goads that creature for combat exactly like a printed
// static: it must attack the non-goader defender, Required, and cannot skip.
func TestEffectGrantedGoadHotPursuitForcesAttackAway(t *testing.T) {
	t.Parallel()
	e := goadGrantEngine(t, corpusAlternativeCard(t, "Hot Pursuit"))
	victim := onBoardReady(t, e, 1, "Name:Victim\nTypes:Creature\nPT:1/1\nOracle:x\n")
	e.G.Players[0].Pool[state.MC] = 1
	e.G.Players[0].Pool[state.MR] = 1
	hp := e.G.Zone(state.ZHand, 0)[0]

	// Precondition: the IsGoaded predicate does not see the victim before
	// the grant -- the compared values differ.
	const spec = "Creature.IsGoaded"
	if e.matchesSpec(spec, victim, e.specCtx(hp, 0)) {
		t.Fatal("precondition: victim already reads IsGoaded before the grant")
	}
	castMode(t, e, hp, "")
	e.resolveTop() // the enchantment enters; its ETB trigger is queued
	e.pending = nil
	e.Advance()
	d := passPriorityUntil(t, e, decision.KTarget)
	idx := -1
	for _, o := range d.Options {
		if o.Obj == victim {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("victim not offered as the suspect target: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 8)

	// The static route's own precondition: the event-backed goad list stays
	// EMPTY -- this is a granted STATIC, not a goad event -- and the bearer
	// is on the battlefield where the requirement read looks.
	v := e.G.Obj(victim)
	if len(v.Goads) != 0 {
		t.Fatalf("event-backed goads = %v, want none (the grant is a static)", v.Goads)
	}
	if v.Zone != state.ZBattlefield {
		t.Fatalf("victim in %v, want the battlefield", v.Zone)
	}
	if !e.goadedBy(v, 0) {
		t.Fatal("the Effect-granted Goad$ static did not goad the remembered creature")
	}

	// The IsGoaded predicate now reads the static route: the exact corpus
	// spec texts (Vengeful Ancestor's trigger ValidCard$, Hot Pursuit's own
	// GainControl AllValid$) match the statically goaded creature.
	if !e.matchesSpec(spec, victim, e.specCtx(hp, 0)) {
		t.Fatal("Creature.IsGoaded still misses the granted static route")
	}
	if !e.matchesSpec("Creature.IsGoaded,Creature.IsSuspected", victim, e.specCtx(hp, 0)) {
		t.Fatal("Hot Pursuit's AllValid$ spec does not match its own goaded creature")
	}
	assertStaticGoadBite(t, e, victim, 0)
}

// TestEffectGrantedGoadEndsWithTheHost proves the granted goad's lifetime is
// the registration's own: Hot Pursuit leaving the battlefield ends the
// requirement (active() drops the unit on the source-leaves rule) with no
// lifetime bookkeeping on the static reader's side.
func TestEffectGrantedGoadEndsWithTheHost(t *testing.T) {
	t.Parallel()
	e := goadGrantEngine(t, corpusAlternativeCard(t, "Hot Pursuit"))
	victim := onBoardReady(t, e, 1, "Name:Victim\nTypes:Creature\nPT:1/1\nOracle:x\n")
	e.G.Players[0].Pool[state.MC] = 1
	e.G.Players[0].Pool[state.MR] = 1
	hp := e.G.Zone(state.ZHand, 0)[0]
	castMode(t, e, hp, "")
	e.resolveTop()
	e.pending = nil
	e.Advance()
	d := passPriorityUntil(t, e, decision.KTarget)
	idx := -1
	for _, o := range d.Options {
		if o.Obj == victim {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("victim not offered as the suspect target: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 8)
	if !e.goadedBy(e.G.Obj(victim), 0) {
		t.Fatal("precondition: the granted goad is not live")
	}
	// The host leaves through the logged move, the same path any destroy
	// takes.
	e.emit(events.Event{Kind: events.MoveZone, Obj: hp, From: state.ZBattlefield,
		To: state.ZGraveyard, Player: 0})
	if e.goadedBy(e.G.Obj(victim), 0) {
		t.Fatal("the granted goad survived Hot Pursuit leaving the battlefield")
	}
	if e.matchesSpec("Creature.IsGoaded", victim, e.specCtx(hp, 0)) {
		t.Fatal("IsGoaded still reads the ended granted static")
	}
}

// TestEffectGrantedGoadImmortalObligation casts the real Immortal Obligation
// at a creature card in an opponent's graveyard: it returns under THEIR
// control with a duty counter (precondition) and the Effect's granted
// `Mode$ Continuous | Affected$ Creature.IsRemembered | Goad$ True` static
// goads it, so the returned creature must attack a player other than the
// caster (CR 701.38b), Required (CR 508.1d).
func TestEffectGrantedGoadImmortalObligation(t *testing.T) {
	t.Parallel()
	e := goadGrantEngine(t, corpusAlternativeCard(t, "Immortal Obligation"))
	e.G.Players[0].Pool[state.MC] = 1
	e.G.Players[0].Pool[state.MW] = 1
	dread := corpusAlternativeCard(t, "Colossal Dreadmaw")
	grave := e.G.AddObject(dread, 1)
	grave.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 1, []state.ObjID{grave.ID})
	_ = e.G.Zone(state.ZHand, 0)

	e.askPriority(0)
	castFirst(t, e, "cast")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the return target ask, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == grave.ID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("graveyard creature not offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 8)

	// Preconditions: the returned creature is on the battlefield under ITS
	// OWNER-seat's control (the opponent's), carries the duty counter the
	// grant rides, and carries NO event-backed goad -- the grant is static.
	v := e.G.Obj(grave.ID)
	if v == nil || v.Zone != state.ZBattlefield || v.Controller != 1 {
		t.Fatalf("returned creature = zone %v controller %d, want battlefield under player 1", v.Zone, v.Controller)
	}
	if v.Counter("DUTY") != 1 {
		t.Fatalf("duty counter = %d, want 1", v.Counter("DUTY"))
	}
	if len(v.Goads) != 0 {
		t.Fatalf("event-backed goads = %v, want none (the grant is a static)", v.Goads)
	}
	if !e.goadedBy(v, 0) {
		t.Fatal("Immortal Obligation's granted static did not goad the returned creature")
	}
	// The returned creature enters summoning sick; the requirement bites from
	// its next combat, so unsick it the way the fixture helpers do.
	v.SummonSick = false
	assertStaticGoadBite(t, e, grave.ID, 0)
}

// TestCloneGrantedGoadMockingDoppelganger casts the real Mocking
// Doppelganger and has it enter as a copy of one of two same-named creatures
// an opponent controls: the AddStaticAbilities$ FamilyTease grant
// (`Affected$ Creature.sameName+Other | Goad$ True`) goads every OTHER
// creature with the copy's name -- including the copied template -- for the
// clone's controller, and never the clone itself.
func TestCloneGrantedGoadMockingDoppelganger(t *testing.T) {
	t.Parallel()
	bear := corpusAlternativeCard(t, "Grizzly Bears")
	e := goadGrantEngine(t, corpusAlternativeCard(t, "Mocking Doppelganger"))
	bear1 := onBoardReadyCard(t, e, 1, bear)
	bear2 := onBoardReadyCard(t, e, 1, bear)
	e.G.Players[0].Pool[state.MC] = 3
	e.G.Players[0].Pool[state.MU] = 1
	md := e.G.Zone(state.ZHand, 0)[0]

	castMode(t, e, md, "")
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "etb" {
		t.Fatalf("expected the ETB copy election, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == bear1 {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("copy target not offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 8)

	// Preconditions: the clone IS a copy of the chosen bear (so the name the
	// Affected$ spec shares is real), both bears are on the battlefield, and
	// no goad is event-backed.
	clone := e.G.Obj(md)
	if clone == nil || clone.Zone != state.ZBattlefield || clone.Face().Name != "Grizzly Bears" {
		t.Fatalf("clone = %+v, want a Grizzly Bears copy on the battlefield", clone)
	}
	for _, id := range []state.ObjID{bear1, bear2} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("bear precondition failed: %+v", o)
		}
		if len(o.Goads) != 0 {
			t.Fatalf("event-backed goads on bear %d = %v, want none (the grant is a static)", id, o.Goads)
		}
	}
	// The copied template shares the clone's name and is "other" than it, so
	// it is goaded too; so is the second bear; the clone itself is not.
	if !e.goadedBy(e.G.Obj(bear1), 0) {
		t.Fatal("the copied template (same name, other than the clone) is not goaded")
	}
	if !e.goadedBy(e.G.Obj(bear2), 0) {
		t.Fatal("the second same-named creature is not goaded")
	}
	if e.goadedBy(clone, 0) {
		t.Fatal("the clone itself is goaded by its own granted static")
	}
	if !e.matchesSpec("Creature.IsGoaded", bear2, e.specCtx(md, 0)) {
		t.Fatal("Creature.IsGoaded misses the clone-granted static route")
	}
	assertStaticGoadBite(t, e, bear2, 0)
}

// onBoardReadyCard places a real corpus card on the battlefield ready to
// attack (the onBoardReady shape for an existing *cards.Card).
func onBoardReadyCard(t *testing.T, e *Engine, p state.PlayerID, c *cards.Card) state.ObjID {
	t.Helper()
	id := onBoardCard(t, e, p, c)
	e.G.Obj(id).SummonSick = false
	return id
}
