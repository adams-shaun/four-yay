package rules

// T:Mode$ BecomesTargetOnce -- Forge's TriggerBecomesTargetOnce, the BATCH
// sibling of Mode$ BecomesTarget ("whenever one or more creatures you control
// become the target of ...").
//
// These leaves pin the mode end to end on REAL corpus cards:
//
//   - Professor Hojo (`.cards/cardsfolder/p/professor_hojo.txt`): an ACTIVATED
//     ability targeting a creature you control draws a card, and its
//     ActivationLimit$ 1 stops a second activation the same turn. This is the
//     ValidSource$ Activated gate and the queue-time limit together.
//   - Leyline of Combustion (`.cards/cardsfolder/l/leyline_of_combustion.txt`):
//     an OPPONENT's activated ability targeting TWO of your permanents deals 2
//     damage exactly ONCE (the batch reading), and TriggeredSourceSAController
//     resolves the ability's controller rather than the Leyline's.
//   - Lightning Strike targeting Professor Hojo must NOT fire Hojo's trigger:
//     ValidSource$ Activated rejects a spell.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// corpusTriggerMode returns the named Mode$ trigger line of a real corpus card,
// failing loudly if the card no longer carries it -- the precondition every
// assertion below depends on, so a corpus change is a named failure rather than
// a vacuous pass.
func corpusTriggerMode(t *testing.T, c *cards.Card, mode string) *cards.Trigger {
	t.Helper()
	for _, f := range c.Faces {
		for i := range f.Triggers {
			if f.Triggers[i].Mode == mode {
				return &f.Triggers[i]
			}
		}
	}
	t.Fatalf("corpus fixture: %s carries no Mode$ %s", c.Faces[0].Name, mode)
	return nil
}

// becomesTargetOnceBoard deals a two-seat engine with the given REAL corpus
// cards on each seat's battlefield. The placements mutate setup state only
// (the targetingBoard shape), and every placed permanent is marked
// non-summoning-sick so a {T} activated ability is legal without driving a
// whole turn.
func becomesTargetOnceBoard(t *testing.T, reg *cards.Registry, board0, board1 []string) (*Engine, map[string]state.ObjID) {
	t.Helper()
	e := New(Config{Seed: 7, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	ids := map[string]state.ObjID{}
	place := func(p state.PlayerID, names []string) {
		zone := e.G.Zone(state.ZBattlefield, p)
		for _, name := range names {
			id := onBoardCard(t, e, p, mustCorpusCard(t, reg, name))
			// Setup-only untap: the harness places permanents directly, so
			// clear the summoning sickness onBoardCard sets for a {T} ability.
			e.G.Obj(id).SummonSick = false
			zone = append(zone, id)
			ids[name] = id
		}
		e.G.SetZone(state.ZBattlefield, p, zone)
	}
	place(0, board0)
	place(1, board1)
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 1, 1
	return e, ids
}

// targetOptionIdx returns the pending KTarget decision's option index naming
// obj, failing if the decision is not a target ask or does not offer obj.
func targetOptionIdx(t *testing.T, e *Engine, obj state.ObjID) int {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == obj {
			return o.Index
		}
	}
	t.Fatalf("target %d not offered: %+v", obj, d.Options)
	return -1
}

// TestBecomesTargetOnceIsRegistered pins the registration half: the mode is a
// dispatchable matcher and effects.Supported reports the primitive, so the
// coverage census can stop counting it a gap.
func TestBecomesTargetOnceIsRegistered(t *testing.T) {
	t.Parallel()
	if trigMatchers["BecomesTargetOnce"] == nil {
		t.Fatal("Mode$ BecomesTargetOnce has no registered matcher")
	}
	if !effects.Supported()["trig:BecomesTargetOnce"] {
		t.Fatal("effects.Supported does not report trig:BecomesTargetOnce")
	}
}

// TestProfessorHojoBecomesTargetOnceDrawsOncePerTurn drives the REAL corpus
// card: the first activated ability targeting a creature the Hojo controller
// controls draws a card, and the second activation the same turn draws nothing
// because the line's ActivationLimit$ 1 was consumed.
func TestProfessorHojoBecomesTargetOnceDrawsOncePerTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	hojoCard := mustCorpusCard(t, reg, "Professor Hojo")
	trig := corpusTriggerMode(t, hojoCard, "BecomesTargetOnce")
	if trig.Params["ValidSource"] != "Activated" {
		t.Fatalf("corpus fixture drift: Hojo's ValidSource$ = %q, want Activated", trig.Params["ValidSource"])
	}
	if trig.Params["ActivationLimit"] != "1" {
		t.Fatalf("corpus fixture drift: Hojo's ActivationLimit$ = %q, want 1", trig.Params["ActivationLimit"])
	}

	e, ids := becomesTargetOnceBoard(t, reg,
		[]string{"Professor Hojo", "Prodigal Pyromancer", "Prodigal Pyromancer"},
		nil)
	hojo := ids["Professor Hojo"]
	// The two Pyromancers carry the activated {T} ability; hojo itself is the
	// target both of them name (a creature its controller controls).
	pyros := e.G.Zone(state.ZBattlefield, 0)
	var pyroIDs []state.ObjID
	for _, id := range pyros {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Prodigal Pyromancer" {
			pyroIDs = append(pyroIDs, id)
		}
	}
	if len(pyroIDs) != 2 {
		t.Fatalf("precondition: %d Prodigal Pyromancers on the battlefield, want 2", len(pyroIDs))
	}
	if o := e.G.Obj(hojo); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Professor Hojo is not on the battlefield: %+v", o)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("precondition: seat 0 pool = %d, want 0", e.G.Players[0].Pool.Total())
	}

	activatePyroAtHojo := func(pyro state.ObjID) {
		t.Helper()
		if o := e.G.Obj(pyro); o == nil || o.Tapped || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: source %d is not an untapped battlefield permanent: %+v", pyro, o)
		}
		e.askPriority(0)
		opt := abilityOption(t, e, pyro, 0)
		submitChoices(t, e, opt.Index)
		submitChoices(t, e, targetOptionIdx(t, e, hojo))
		passUntilStackEmpty(t, e, 40)
	}

	hand := len(e.G.Zone(state.ZHand, 0))
	activatePyroAtHojo(pyroIDs[0])
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand+1 {
		t.Fatalf("first activation targeting Hojo drew %d cards, want 1 (Hojo's BecomesTargetOnce)", got-hand)
	}
	if e.G.Obj(hojo) == nil || e.G.Obj(hojo).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Hojo left the battlefield before the limit probe (the 1 damage should not kill a 2/2)")
	}
	activatePyroAtHojo(pyroIDs[1])
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand+1 {
		t.Fatalf("second activation targeting Hojo drew to %d, want %d: ActivationLimit$ 1 must stop the second trigger", got, hand+1)
	}
}

// TestProfessorHojoBecomesTargetOnceIgnoresASpell proves the
// ValidSource$ Activated gate on the real card: an OPPONENT's spell that
// targets Hojo is not "an activated ability", so the trigger must not fire
// even though the target matches.
func TestProfessorHojoBecomesTargetOnceIgnoresASpell(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, ids := targetingBoard(t, reg,
		[]string{"Professor Hojo"},
		[]string{"Lightning Strike"})
	hojo, strike := ids["Professor Hojo"], ids["Lightning Strike"]
	// The spell's target is a sturdy OTHER creature seat 0 controls, so the
	// spell provably resolved (the wall took 3) while Hojo survives to be the
	// watcher; ValidTarget$ would match either.
	wall := onBoard(t, e, 0, "Name:TestWall\nManaCost:1\nTypes:Creature Wall\nPT:0/6\nOracle:x\n")
	e.G.Players[1].Pool[state.MR] = 3
	castStrikeAt(t, e, strike, wall, 1)
	passUntilStackEmpty(t, e, 40)

	// The spell really resolved onto the wall (the feature's handler ran): it
	// took 3, Hojo is still on the battlefield, and no card was drawn by the
	// BecomesTargetOnce line (a spell is not an activated ability).
	if got := e.G.Obj(wall).Damage; got != 3 {
		t.Fatalf("Lightning Strike did not resolve onto the wall: damage = %d, want 3", got)
	}
	if o := e.G.Obj(hojo); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Hojo left the battlefield: %+v", o)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != 0 {
		t.Fatalf("a spell targeting a creature Hojo's controller controls fired his Activated-only trigger: seat 0 hand = %d, want 0", got)
	}
}

// TestLeylineOfCombustionBecomesTargetOnceIsOnePerAction drives the batch
// reading on the REAL corpus Leyline of Combustion: an opponent's activated
// ability that names TWO of the Leyline controller's permanents in one target
// answer fires the line exactly ONCE, not once per target. The trigger is
// counted through its TriggerPush, and the payout is asserted on the seats'
// life totals: Leyline's own body resolves Defined$ TriggeredSourceSAController
// (the targeting ability's controller), so the whole batch costs the OPPONENT
// exactly 2 life, and Leyline's controller none.
func TestLeylineOfCombustionBecomesTargetOnceIsOnePerAction(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	lcard := mustCorpusCard(t, reg, "Leyline of Combustion")
	trig := corpusTriggerMode(t, lcard, "BecomesTargetOnce")
	if trig.Params["ValidSource"] != "SpellAbility.OppCtrl" {
		t.Fatalf("corpus fixture drift: Leyline's ValidSource$ = %q, want SpellAbility.OppCtrl", trig.Params["ValidSource"])
	}

	// Seat 1 gets a synthetic {T} ability naming two target lands; the two
	// lands it targets are seat 0's, so ValidTarget$ You,Permanent.YouCtrl
	// matches both. SpellAbility.OppCtrl is satisfied because seat 1 controls
	// the ability.
	const tapper = "Name:TwoTap\nManaCost:1 G\nTypes:Creature Elf\nPT:1/1\n" +
		"A:AB$ Untap | Cost$ T | ValidTgts$ Land | TargetMin$ 2 | TargetMax$ 2 | SpellDescription$ Untap two target lands.\n" +
		"Oracle:x\n"
	e, ids := becomesTargetOnceBoard(t, reg,
		[]string{"Leyline of Combustion", "Mountain", "Mountain"},
		nil)
	leyline := ids["Leyline of Combustion"]
	tapperID := onBoard(t, e, 1, tapper)
	e.G.Obj(tapperID).SummonSick = false
	var lands []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Mountain" {
			lands = append(lands, id)
		}
	}
	if len(lands) != 2 {
		t.Fatalf("precondition: %d Mountains on seat 0, want 2", len(lands))
	}
	if o := e.G.Obj(leyline); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Leyline of Combustion is not on the battlefield: %+v", o)
	}
	// Precondition for the payout assertions: both seats start at the same
	// life, so the damage is attributable to the trigger's resolution alone.
	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
	if life0 == life1-2 {
		t.Fatalf("precondition: seat 0 life %d equals the expected post-damage seat 1 life", life0)
	}

	// Seat 1 activates the ability, naming BOTH of seat 0's lands in one
	// target answer.
	e.G.Active, e.G.Priority = 1, 1
	e.askPriority(1)
	opt := abilityOption(t, e, tapperID, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Max < 2 {
		t.Fatalf("precondition: expected a two-target ask, got %+v", d)
	}
	var picks []int
	for _, o := range d.Options {
		for _, l := range lands {
			if o.Obj == l {
				picks = append(picks, o.Index)
			}
		}
	}
	if len(picks) != 2 {
		t.Fatalf("precondition: the ask offers %d of seat 0's two lands: %+v", len(picks), d.Options)
	}
	submitChoices(t, e, picks[0], picks[1])
	if len(e.G.Stack) == 0 {
		t.Fatal("the two-target ability never reached the stack")
	}
	passUntilStackEmpty(t, e, 40)

	// The ability really resolved (its controller chose two targets; the
	// ability left the stack), and Leyline's line fired exactly once.
	pushes := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == leyline {
			pushes++
		}
	}
	if pushes != 1 {
		t.Fatalf("Leyline of Combustion fired %d times for one two-target ability, want 1 (the batch itself)", pushes)
	}

	// And the body paid the RIGHT seat: Leyline's own TrigDmg resolves
	// Defined$ TriggeredSourceSAController, so the ABILITY's controller (seat
	// 1, who cast the two-target activation) takes exactly 2 damage for the
	// whole batch -- not 4, and never Leyline's controller (seat 0).
	if got := e.G.Players[1].Life; got != life1-2 {
		t.Fatalf("seat 1 life = %d, want %d (the two-target batch deals 2 once)", got, life1-2)
	}
	if got := e.G.Players[0].Life; got != life0 {
		t.Fatalf("seat 0 (Leyline's controller) life = %d, want unchanged %d", got, life0)
	}
}
