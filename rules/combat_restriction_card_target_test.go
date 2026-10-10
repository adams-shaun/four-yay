package rules

// The Aetherspark's `S:Mode$ CantAttack | ValidCard$ Creature |
// Target$ Card.Self+AttachedTo Creature` (CR 506.3 / 508.1b): a planeswalker
// Equipment attached to a creature can't be attacked. combat.
// RestrictionTargetMatches reads the Card.<props> Target$ clause for the
// attacked permanent, so the offer list, the declaration validator and the
// bot all agree through AttackBlocked.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const aethersparkTarget = "Card.Self+AttachedTo Creature"

// aethersparkBoard parks seat 0's turn with seat 1 holding the Aetherspark, a
// bearer creature and a second planeswalker, and seat 0 holding the attacker.
func aethersparkBoard(t *testing.T, seed uint64, attach bool) (e *Engine, spark, bearer, walker, attacker state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("The Aetherspark")
	if !ok {
		t.Fatal("corpus missing The Aetherspark")
	}
	var target string
	for _, s := range c.Faces[0].Statics {
		if s.Mode == "CantAttack" {
			target = s.Params["Target"]
		}
	}
	if target != aethersparkTarget {
		t.Fatalf("precondition: The Aetherspark CantAttack Target$ = %q, want %q", target, aethersparkTarget)
	}
	bear0, bear1, w := card(t, staticBearFixture), card(t, staticBearFixture), card(t, targetWalkerFixture)
	e, _ = restrictionGame(t, seed,
		[][]*cards.Card{nil, nil},
		[][]*cards.Card{{bear0}, {c, bear1, w}})
	spark = bearOnBoard(t, e, 1, c)
	bearer = bearOnBoard(t, e, 1, bear1)
	walker = bearOnBoard(t, e, 1, w)
	attacker = bearOnBoard(t, e, 0, bear0)
	if attach {
		e.emit(events.Event{Kind: events.Attach, Obj: spark, IDs: []state.ObjID{bearer}})
		e.pending = nil
		e.Advance()
	}
	so := e.G.Obj(spark)
	if so == nil || so.Zone != state.ZBattlefield || so.Controller != 1 || !faceHasType(so, "Planeswalker") {
		t.Fatalf("precondition: Aetherspark is not seat 1's battlefield planeswalker: %+v", so)
	}
	if want := state.ObjID(0); !attach && so.AttachedTo != want {
		t.Fatalf("precondition: control Aetherspark attached to %d", so.AttachedTo)
	}
	if attach && so.AttachedTo != bearer {
		t.Fatalf("precondition: Aetherspark attached to %d, want the bearer %d", so.AttachedTo, bearer)
	}
	if !faceHasType(e.G.Obj(bearer), "Creature") || e.G.Obj(walker).Zone != state.ZBattlefield {
		t.Fatal("precondition: bearer is not a creature / second walker is not on the battlefield")
	}
	return
}

func TestCantAttackCardSelfAttachedToCreatureBindsThroughAttackBlocked(t *testing.T) {
	t.Parallel()
	e, spark, _, walker, attacker := aethersparkBoard(t, 6140, false)
	if restrictionPlayerTargetMatches(e.G, aethersparkTarget, 1, 0, spark, nil, spark) || e.attackBlocked(attacker, 1, spark) {
		t.Fatal("unattached Aetherspark was shielded")
	}
	e, spark, _, walker, attacker = aethersparkBoard(t, 6141, true)
	if !restrictionPlayerTargetMatches(e.G, aethersparkTarget, 1, 0, spark, nil, spark) {
		t.Fatal("Card.Self+AttachedTo Creature did not match the attached Aetherspark")
	}
	if !e.attackBlocked(attacker, 1, spark) {
		t.Fatal("attached Aetherspark can be attacked")
	}
	if e.attackBlocked(attacker, 1, 0) {
		t.Fatal("attached Aetherspark shielded its controller")
	}
	if e.attackBlocked(attacker, 1, walker) {
		t.Fatal("attached Aetherspark shielded a different planeswalker")
	}
	if restrictionPlayerTargetMatches(e.G, aethersparkTarget, 1, 0, spark, nil, 0) {
		t.Fatal("Card clause matched a player defender")
	}
}

// TestCantAttackCardSelfAttachedOffersAndValidator declares attackers: the
// attached Aetherspark is not offered and a forged pair is rejected; the
// unattached one is offered and accepted; the bot's own answer validates.
func TestCantAttackCardSelfAttachedOffersAndValidator(t *testing.T) {
	t.Parallel()
	pair := func(d *decision.Decision, atk, battle state.ObjID) (decision.Option, bool) {
		for _, o := range d.Options {
			if o.Obj == atk && o.Battle == battle {
				return o, true
			}
		}
		return decision.Option{}, false
	}
	for _, attach := range []bool{false, true} {
		e, spark, _, walker, attacker := aethersparkBoard(t, 6142, attach)
		driveToStep(t, e, e.G.Turn, e.G.Active, state.StepDeclareAttackers)
		d := e.Pending()
		if d == nil || d.Kind != decision.KAttackers {
			t.Fatalf("attach=%v: pending = %+v, want attackers", attach, d)
		}
		if _, ok := pair(d, attacker, walker); !ok {
			t.Fatalf("attach=%v: other planeswalker not offered: %+v", attach, d.Options)
		}
		if _, ok := pair(d, attacker, 0); !ok {
			t.Fatalf("attach=%v: player attack not offered", attach)
		}
		o, offered := pair(d, attacker, spark)
		if offered == attach {
			t.Fatalf("attach=%v: Aetherspark offered=%v", attach, offered)
		}
		if attach {
			// Forge the blocked pair as the walker option's twin.
			w, _ := pair(d, attacker, walker)
			forged := w
			forged.Battle = spark
			d.Options = append(d.Options, forged)
			forged.Index = len(d.Options) - 1
			d.Options[len(d.Options)-1] = forged
			if err := e.validateAttackers(d, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{forged.Index}}); err == nil {
				t.Fatal("validator accepted an attack on the attached Aetherspark")
			}
			d.Options = d.Options[:len(d.Options)-1]
		} else if err := e.validateAttackers(d, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
			t.Fatalf("unattached: validator rejected the Aetherspark attack: %v", err)
		}
		in := newTestBot(7).answer(e, d)
		if err := e.validateAttackers(d, in); err != nil {
			t.Fatalf("attach=%v: bot answer %v fails the validator: %v", attach, in.Choices, err)
		}
	}
}
