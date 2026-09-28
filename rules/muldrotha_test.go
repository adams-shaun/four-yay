package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// muldrothaSrc is Muldrotha, the Gravetide's real compiled script: six
// S:Mode$ Continuous statics, one per permanent type, each carrying
// MayPlay$ True | MayPlayLimit$ 1 | MayPlayText$ <type> | AffectedZone$
// Graveyard. Each static is its OWN once-per-turn permission.
const muldrothaSrc = "Name:Muldrotha, the Gravetide\nManaCost:3 B G U\nTypes:Legendary Creature Elemental Avatar\nPT:6/6\n" +
	"S:Mode$ Continuous | Affected$ Land.YouOwn | Condition$ PlayerTurn | MayPlay$ True | MayPlayLimit$ 1 | MayPlayText$ Land | EffectZone$ Battlefield | AffectedZone$ Graveyard | Description$ x\n" +
	"S:Mode$ Continuous | Affected$ Creature.YouOwn+nonLand | Condition$ PlayerTurn | MayPlay$ True | MayPlayLimit$ 1 | MayPlayText$ Creature | EffectZone$ Battlefield | AffectedZone$ Graveyard\n" +
	"S:Mode$ Continuous | Affected$ Planeswalker.YouOwn+nonLand | Condition$ PlayerTurn | MayPlay$ True | MayPlayLimit$ 1 | MayPlayText$ Planeswalker | EffectZone$ Battlefield | AffectedZone$ Graveyard\n" +
	"S:Mode$ Continuous | Affected$ Artifact.YouOwn+nonLand | Condition$ PlayerTurn | MayPlay$ True | MayPlayLimit$ 1 | MayPlayText$ Artifact | EffectZone$ Battlefield | AffectedZone$ Graveyard\n" +
	"S:Mode$ Continuous | Affected$ Enchantment.YouOwn+nonLand | Condition$ PlayerTurn | MayPlay$ True | MayPlayLimit$ 1 | MayPlayText$ Enchantment | EffectZone$ Battlefield | AffectedZone$ Graveyard\n" +
	"S:Mode$ Continuous | Affected$ Battle.YouOwn+nonLand | Condition$ PlayerTurn | MayPlay$ True | MayPlayLimit$ 1 | MayPlayText$ Battle | EffectZone$ Battlefield | AffectedZone$ Graveyard\n" +
	"Oracle:x\n"

func artifactSrc(name, cost string) string {
	return "Name:" + name + "\nManaCost:" + cost + "\nTypes:Artifact\nOracle:x\n"
}

func artifactCreatureSrc(name, cost string) string {
	return "Name:" + name + "\nManaCost:" + cost + "\nTypes:Artifact Creature Golem\nPT:2/2\nOracle:x\n"
}

// mayPlayOffersFor returns the cast options whose Obj is id, in list order,
// so a test can assert which named permissions were offered.
func (e *Engine) mayPlayOffersFor(id state.ObjID) []decision.Option {
	var out []decision.Option
	for _, o := range e.legalActions(0) {
		if o.Kind == "cast" && o.Obj == id && o.Mode == "mayplay" {
			out = append(out, o)
		}
	}
	return out
}

// muldrothaBase builds the may-play fixture with Muldrotha on seat 0's
// battlefield and the graveyards emptied, at seat 0's first main phase.
func muldrothaBase(t *testing.T) *Engine {
	t.Helper()
	e := mayPlayBase(t)
	onBoardGrant(t, e, 0, muldrothaSrc)
	e.G.Players[0].Pool[state.MG] = 6
	return e
}

// TestMuldrothaTypedPermissionsArePerStatic pins the MayPlayText$ permission
// semantics on Muldrotha's real script: on the controller's turn each
// permanent type is its own once-per-turn permission, so one creature AND one
// artifact may be cast from the graveyard in the same turn but not a second
// creature, and an artifact creature offers one named option per matching
// unused permission so the cast can choose which limit to consume.
func TestMuldrothaTypedPermissionsArePerStatic(t *testing.T) {
	t.Parallel()

	t.Run("one creature and one artifact, not a second creature", func(t *testing.T) {
		e := muldrothaBase(t)
		bear := graveCard(e, card(t, creatureSrc("Bear A")), 0, 0)
		clue := graveCard(e, card(t, artifactSrc("Clue A", "1")), 0, 0)

		// Precondition: both are in the graveyard with Muldrotha on the
		// battlefield, so the only reason either could be unoffered is the
		// engine's permission logic.
		if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("precondition: bear not in graveyard: %+v", o)
		}
		if len(e.mayPlayOffersFor(bear)) == 0 {
			t.Fatalf("bear not offered through the Creature permission: %+v", e.legalActions(0))
		}
		if len(e.mayPlayOffersFor(clue)) == 0 {
			t.Fatalf("artifact not offered through the Artifact permission: %+v", e.legalActions(0))
		}

		castMayPlay(t, e, bear, "Creature")

		// The Creature permission is spent: the bear (now on the stack) is
		// gone, and a SECOND creature must not be offered.
		bear2 := graveCard(e, card(t, creatureSrc("Bear B")), 0, 0)
		if offers := e.mayPlayOffersFor(bear2); len(offers) != 0 {
			t.Fatalf("second creature offered after the Creature permission was spent: %+v", offers)
		}
		// The Artifact permission is untouched.
		if offers := e.mayPlayOffersFor(clue); len(offers) == 0 {
			t.Fatalf("artifact no longer offered although only Creature was spent: %+v", e.legalActions(0))
		}
		castMayPlay(t, e, clue, "Artifact")
	})

	t.Run("artifact creature offers one option per matching permission", func(t *testing.T) {
		e := muldrothaBase(t)
		golem := graveCard(e, card(t, artifactCreatureSrc("Golem", "2")), 0, 0)

		offers := e.mayPlayOffersFor(golem)
		if len(offers) != 2 {
			t.Fatalf("artifact creature must offer one option per matching permission, got %d: %+v", len(offers), offers)
		}
		var sawArtifact, sawCreature bool
		for _, o := range offers {
			if strings.Contains(o.Label, "(Artifact)") {
				sawArtifact = true
			}
			if strings.Contains(o.Label, "(Creature)") {
				sawCreature = true
			}
		}
		if !sawArtifact || !sawCreature {
			t.Fatalf("artifact creature options not labelled per permission: %+v", offers)
		}

		// Cast it through the ARTIFACT permission only. The Creature
		// permission must therefore still be available for another creature.
		castMayPlay(t, e, golem, "Artifact")
		bear := graveCard(e, card(t, creatureSrc("Bear C")), 0, 0)
		if offers := e.mayPlayOffersFor(bear); len(offers) == 0 {
			t.Fatalf("Creature permission consumed by the artifact-creature cast: %+v", e.legalActions(0))
		}
	})

	t.Run("nothing offered on the opponent's turn", func(t *testing.T) {
		e := mayPlayBase(t)
		onBoardGrant(t, e, 0, muldrothaSrc)
		e.G.Players[0].Pool[state.MG] = 6
		bear := graveCard(e, card(t, creatureSrc("Bear D")), 0, 0)
		// Precondition: the same board DOES offer the bear on seat 0's turn.
		if len(e.mayPlayOffersFor(bear)) == 0 {
			t.Fatalf("precondition: bear not offered on seat 0's own turn")
		}
		// Now it is seat 1's turn; seat 0 holds priority.
		e.G.Active, e.G.Priority = 1, 0
		e.pending = nil
		e.Advance()
		if offers := e.mayPlayOffersFor(bear); len(offers) != 0 {
			t.Fatalf("Muldrotha granted a cast on the opponent's turn: %+v", offers)
		}
	})
}

// castMayPlay submits the may-play cast option for id whose label names perm
// (the MayPlayText$ type), asserting exactly one such option exists and that
// the spell reaches the stack.
func castMayPlay(t *testing.T, e *Engine, id state.ObjID, perm string) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		e.priorityRound()
		d = e.Pending()
	}
	if d == nil {
		t.Fatalf("no priority decision to cast %s through %s", e.G.Obj(id).Face().Name, perm)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id && o.Mode == "mayplay" && strings.Contains(o.Label, "("+perm+")") {
			if idx >= 0 {
				t.Fatalf("duplicate may-play option for %s (%s): %+v", e.G.Obj(id).Face().Name, perm, d.Options)
			}
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("%s cast through %s not offered: %+v", e.G.Obj(id).Face().Name, perm, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit may-play cast of %s: %v", e.G.Obj(id).Face().Name, err)
	}
	if o := e.G.Obj(id); o.Zone != state.ZStack {
		t.Fatalf("%s zone after cast = %s, want stack", e.G.Obj(id).Face().Name, o.Zone)
	}
	passUntilStackEmpty(t, e, 40)
}

// TestZulAshurGraveyardCast pins Zul Ashur, Lich Lord's activated ability end
// to end: {T} targeting a Zombie creature card in the graveyard registers the
// Effect's StaticAbilities$ Play may-play permission for that card, the card
// is offered and casts, and ExileOnMoved$ Graveyard ends the permission the
// moment the card leaves the graveyard (CR 700.4: it becomes a new object on
// the stack). PumpZone$ Graveyard is inert for api:Effect (Forge's
// EffectEffect never reads it; only PumpEffect does), so its presence must not
// withhold the permission.
func TestZulAshurGraveyardCast(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	zul, ok := reg.Lookup("Zul Ashur, Lich Lord")
	if !ok {
		t.Fatal("Zul Ashur, Lich Lord missing from corpus")
	}
	if d := zul.Link(); len(d) != 0 {
		t.Fatalf("link Zul Ashur: %v", d)
	}
	e := mayPlayBase(t)
	zo := e.G.AddObject(zul, 0)
	zo.Zone = state.ZLibrary
	ids := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	ids = append(ids, zo.ID)
	e.G.SetZone(state.ZLibrary, 0, ids)
	e.emit(events.Event{Kind: events.MoveZone, Obj: zo.ID, From: state.ZLibrary, To: state.ZBattlefield})
	// The {T} activated ability (CR 302.6) cannot be activated while the
	// creature is summoning-sick; a test that wants to observe the ability
	// must model a permanent that has been under its controller since before
	// this turn.
	zo.SummonSick = false
	// The Zombie costs {1}{B}; a payable pool is a precondition of the cast
	// option the test asserts below.
	e.G.Players[0].Pool[state.MB] = 2
	zombie := graveCard(e, card(t, "Name:Grave Zombie\nManaCost:1 B\nTypes:Creature Zombie\nPT:2/2\nOracle:x\n"), 0, 0)
	// Precondition: the Zombie is a LEGAL target (Zombie creature card the
	// activator owns) in the graveyard, and Zul Ashur is untapped on the
	// battlefield. Without both, the activation below could not be offered.
	if o := e.G.Obj(zombie); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: zombie not in graveyard: %+v", o)
	}
	if zo.Zone != state.ZBattlefield || zo.Tapped {
		t.Fatalf("precondition: Zul Ashur not an untapped battlefield permanent: zone=%s tapped=%v", zo.Zone, zo.Tapped)
	}

	e.pending = nil
	e.Advance()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("expected seat 0 priority, got %+v", d)
	}
	var abilityIdx int = -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == zo.ID {
			abilityIdx = o.Index
		}
	}
	if abilityIdx < 0 {
		t.Fatalf("Zul Ashur's {T} ability not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{abilityIdx}}); err != nil {
		t.Fatalf("activate Zul Ashur: %v", err)
	}
	// CR 601.2c: the ability targets; answer the target ask with the Zombie.
	if d = e.Pending(); d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected target decision after activation, got %+v", d)
	}
	targetIdx := -1
	for _, o := range d.Options {
		if o.Obj == zombie {
			targetIdx = o.Index
		}
	}
	if targetIdx < 0 {
		t.Fatalf("zombie not offered as a target: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{targetIdx}}); err != nil {
		t.Fatalf("submit target: %v", err)
	}
	passUntilStackEmpty(t, e, 40)

	// The handler ran: the Effect's may-play permission covers the Zombie.
	if !e.mayPlayEffectGrantsCast(0, e.G.Obj(zombie)) {
		t.Fatal("Zul Ashur's Effect registered no may-play permission for the Zombie")
	}
	offers := e.mayPlayOffersFor(zombie)
	if len(offers) == 0 {
		t.Fatalf("zombie not offered after Zul Ashur's activation: %+v", e.legalActions(0))
	}

	// The live effect must carry the exact move-lifetime rider before the
	// cast; this precondition makes the following removal assertion meaningful.
	var lifetimeSource state.ObjID
	for _, ce := range e.continuous {
		if ce.FromEffect && ce.ExileOnMoved == "Graveyard" {
			lifetimeSource = ce.Source
			break
		}
	}
	if lifetimeSource == 0 {
		t.Fatalf("precondition: Zul Ashur effect with ExileOnMoved=Graveyard absent: %+v", e.continuous)
	}

	// Cast it: the card moves graveyard -> stack, so ExileOnMoved$ Graveyard
	// ends the permission.
	d = e.Pending()
	if d == nil {
		e.priorityRound()
		d = e.Pending()
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == zombie && o.Mode == "mayplay" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("zombie not castable through Zul Ashur: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
		t.Fatalf("cast zombie: %v", err)
	}
	if o := e.G.Obj(zombie); o.Zone != state.ZStack {
		t.Fatalf("zombie zone after cast = %s, want stack", o.Zone)
	}
	// The actual ExileOnMoved effect, not merely the object's new zone, must
	// have ended. (A fresh stack object cannot satisfy the graveyard matcher
	// even if the lifetime handler were missing.)
	for _, ce := range e.continuous {
		if ce.FromEffect && ce.Source == lifetimeSource && ce.ExileOnMoved == "Graveyard" {
			t.Fatalf("ExileOnMoved$ Graveyard left Zul Ashur effect live: %+v", ce)
		}
	}
}
