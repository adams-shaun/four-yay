package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// These tests close the AGENTS.md row "Layer-4 type grants reach only the layer
// walk": a filter OUTSIDE the layer walk -- the ordinary target offer, cost
// site, Count$Valid census or CantTarget spec -- must see a type a continuous
// effect granted (CR 613.1d/613.1c), exactly as the layer walk's own Affected$
// match already did. The carrier is the REAL corpus Maskwood Nexus
// (AddAllCreatureTypes$ True) and the REAL corpus Tivadar of Thorn whose entry
// trigger declares `ValidTgts$ Goblin`; the vanilla victims are inline fixtures,
// per the no-Forge-script-text rule (and no repo deck carries either carrier, so
// these tests do not move the golden heads).

// layer4Victim is a vanilla creature carrying NO Goblin subtype of its own --
// only a layer-4 grant can make it a Goblin.
const layer4Victim = "Name:Layer4 Victim\nTypes:Creature Human\nPT:1/1\nOracle:x\n"

// TestLayer4GrantedTypeReachesTheOrdinaryFilterGrammar is the row's core claim:
// a `Goblin` filter evaluated through the SAME rules-built SpecContext the
// target offer / Count$Valid / cost sites use now matches a creature a static
// made a Goblin.
func TestLayer4GrantedTypeReachesTheOrdinaryFilterGrammar(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Maskwood Nexus")}, nil)
	moveByName(t, e, 0, "Maskwood Nexus", state.ZBattlefield)
	human := onBoard(t, e, 0, layer4Victim)

	// Preconditions, each of which fails the test if the setup is vacuous.
	o := e.G.Obj(human)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("victim is not on the battlefield: %+v", o)
	}
	if o.Face() == nil || slices.Contains(o.Face().Types, "Goblin") {
		t.Fatalf("precondition: the victim must not print Goblin, types=%v", o.Face().Types)
	}
	// The derived list (the layer walk) genuinely differs from the printed one.
	if d := e.Derived(human); !slices.Contains(d.Types, "Goblin") {
		t.Fatalf("precondition: the layer walk itself must see Goblin, types=%v", d.Types)
	}

	// The real assertion: the ORDINARY context (the one a target offer builds
	// through e.specCtx) sees the granted type.
	sc := e.specCtx(0, 0)
	if !effects.MatchesSpecCtx(e.G, "Goblin", human, sc) {
		t.Fatal("ordinary filter context does not see the layer-4 granted Goblin type")
	}
	if !effects.MatchesSpecCtx(e.G, "Creature", human, sc) {
		t.Fatal("ordinary filter context lost the printed Creature type")
	}
	// Scope control: a context-free call (no rules-built table) still reads the
	// printed face -- the documented fallback, not a global widening.
	if effects.MatchesSpecFrom(e.G, "Goblin", human, 0, 0) {
		t.Fatal("a context-free filter must not see the layer-4 granted type")
	}
}

// TestLayer4GrantedTypeIsOfferedAsAValidTarget drives the REAL Goblin-killer:
// Tivadar of Thorn's entry trigger declares `ValidTgts$ Goblin`, so the target
// offer is exactly the row's "not targetable by a Goblin-killer" symptom. The
// Maskwood-affected Human must be offered; a Goblin-free artifact must not.
func TestLayer4GrantedTypeIsOfferedAsAValidTarget(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Maskwood Nexus"), lookup(t, reg, "Tivadar of Thorn")}, nil)
	moveByName(t, e, 0, "Maskwood Nexus", state.ZBattlefield)
	tivadar := moveByName(t, e, 0, "Tivadar of Thorn", state.ZBattlefield)
	human := onBoard(t, e, 0, layer4Victim)
	artifact := onBoard(t, e, 1, "Name:Layer4 Relic\nTypes:Artifact\nOracle:x\n")

	// Precondition: the victim's derived list carries the granted Goblin.
	if !slices.Contains(e.Derived(human).Types, "Goblin") {
		t.Fatalf("precondition: Maskwood did not grant Goblin, types=%v", e.Derived(human).Types)
	}
	tiv := lookup(t, reg, "Tivadar of Thorn")
	if len(tiv.Faces) == 0 || len(tiv.Faces[0].Triggers) == 0 || tiv.Faces[0].Triggers[0].Effect == nil {
		t.Fatal("corpus fixture: Tivadar of Thorn's entry trigger did not link")
	}
	sa := tiv.Faces[0].Triggers[0].Effect
	if sa.Params["ValidTgts"] != "Goblin" {
		t.Fatalf("precondition: the Goblin-killer's ValidTgts$ is %q, want Goblin", sa.Params["ValidTgts"])
	}

	e.askTarget(0, tivadar, sa)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want a target decision", d)
	}
	offered := map[state.ObjID]bool{}
	for _, opt := range d.Options {
		if opt.Obj != 0 {
			offered[opt.Obj] = true
		}
	}
	if !offered[human] {
		t.Fatal("the Maskwood-affected Human was NOT offered as a Goblin target")
	}
	if offered[artifact] {
		t.Fatal("a Goblin-free artifact was offered as a Goblin target")
	}
}

// TestLayer4RemovedTypeIsNotResurrectedByThePrintedFace pins the authoritative
// half: a RemoveCardTypes$ effect that strips a creature's card types must make
// the ordinary grammar answer NO for `Creature`, rather than falling back to the
// printed face (which would resurrect the removed word). The granted replacement
// type stays visible.
func TestLayer4RemovedTypeIsNotResurrectedByThePrintedFace(t *testing.T) {
	t.Parallel()
	stripper := "Name:Type Stripper\nTypes:Enchantment\n" +
		"S:Mode$ Continuous | Affected$ Creature.Other+YouCtrl | RemoveCardTypes$ True | AddTypes$ Wall\nOracle:x\n"
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{card(t, stripper)}, nil)
	onBoard(t, e, 0, stripper)
	bear := onBoard(t, e, 0, "Name:Layer4 Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	// Preconditions: the bear prints Creature, and the layer walk removed it.
	if o := e.G.Obj(bear); o == nil || o.Face() == nil || !slices.Contains(o.Face().Types, "Creature") {
		t.Fatalf("precondition: bear must print Creature, types=%v", e.G.Obj(bear).Face().Types)
	}
	d := e.Derived(bear)
	if slices.Contains(d.Types, "Creature") {
		t.Fatalf("precondition: the layer walk must have stripped Creature, types=%v", d.Types)
	}
	if !slices.Contains(d.Types, "Wall") {
		t.Fatalf("precondition: the layer walk must have granted Wall, types=%v", d.Types)
	}

	sc := e.specCtx(0, 0)
	if effects.MatchesSpecCtx(e.G, "Creature", bear, sc) {
		t.Fatal("ordinary grammar resurrected a removed Creature type from the printed face")
	}
	if !effects.MatchesSpecCtx(e.G, "Wall", bear, sc) {
		t.Fatal("ordinary grammar lost the granted Wall type")
	}
}
