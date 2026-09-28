package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// These tests close the remainder the layer-4 row's commit named: the
// Player.controlsCreature.<spec> / Player.controlsPermanent.<spec> family
// built its own nested SpecContext with no layer-3/layer-4 tables, so a
// creature a continuous effect made a Goblin was not counted by a
// controlsCreature.Goblin question even though every ordinary filter site
// now sees it. The fix threads the rules-computed tables through PlayerSpecCtx.
//
// The layer-4 carrier is the REAL corpus Maskwood Nexus (AddAllCreatureTypes$
// True); the victim is an inline fixture, per the no-Forge-script-text rule.
// The layer-3 carrier is an inline SetName$ Equipment.

// controlsVictim is a vanilla creature printing NO Goblin subtype: only a
// layer-4 grant can make it count for Player.controlsCreature.Goblin.
const controlsVictim = "Name:Controls Victim\nTypes:Creature Human\nPT:1/1\nOracle:x\n"

// TestPlayerControlsCreatureSeesLayer4DerivedTypes drives the PRODUCTION
// caller -- phaseMatches, the Mode$ Phase trigger's ValidPlayer$ gate, which
// builds its context through e.playerSpecCtx -- so the assertion is not just
// the leaf grammar. Maskwood Nexus makes every creature every creature type;
// a controlsCreature.Goblin trigger must therefore fire for the seat whose
// otherwise-Human creature it affects.
func TestPlayerControlsCreatureSeesLayer4DerivedTypes(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Maskwood Nexus")}, nil)
	source := moveByName(t, e, 0, "Maskwood Nexus", state.ZBattlefield)
	victim := onBoard(t, e, 0, controlsVictim)

	// Preconditions: the victim is on the battlefield, does NOT print Goblin,
	// and the layer walk genuinely derives Goblin for it.
	o := e.G.Obj(victim)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: victim is not on the battlefield: %+v", o)
	}
	if o.Face() == nil || slices.Contains(o.Face().Types, "Goblin") {
		t.Fatalf("precondition: victim must not print Goblin, types=%v", o.Face().Types)
	}
	if d := e.Derived(victim); !slices.Contains(d.Types, "Goblin") {
		t.Fatalf("precondition: the layer walk must derive Goblin, types=%v", d.Types)
	}

	// The production caller: a Mode$ Phase trigger gated on
	// ValidPlayer$ Player.controlsCreature.Goblin_GE1.
	trig := cards.Trigger{Mode: "Phase", Params: map[string]string{
		"Phase":       "Main1",
		"ValidPlayer": "Player.controlsCreature.Goblin_GE1",
	}}
	ev := events.Event{Kind: events.StepChange, Step: state.StepMain1}
	if !e.phaseMatches(trig, source, ev) {
		t.Fatal("phaseMatches ValidPlayer$ controlsCreature.Goblin did not see the layer-4 granted Goblin")
	}
	// Non-vacuity: the same gate answers NO for a seat with no Goblin
	// creature. phaseMatches always reads the active player, so assert the
	// seat scoping through the same context carrier it builds.
	if effects.MatchesPlayerSpecCtx(e.G, "Player.controlsCreature.Goblin_GE1", 1, 0, e.playerSpecCtx(source)) {
		t.Fatal("seat 1 controls no Goblin, yet controlsCreature matched")
	}
}

// TestPlayerControlsCreatureSeesLayer4CountComparison pins the count half:
// the nested filter still counts, so a _GE2 token over a two-creature board
// grants by layer 4 must be satisfied (and _GE3 not).
func TestPlayerControlsCreatureSeesLayer4CountComparison(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Maskwood Nexus")}, nil)
	moveByName(t, e, 0, "Maskwood Nexus", state.ZBattlefield)
	onBoard(t, e, 0, controlsVictim)
	onBoard(t, e, 0, "Name:Controls Victim B\nTypes:Creature Human\nPT:1/1\nOracle:x\n")

	pc := e.playerSpecCtx(0)
	if !effects.MatchesPlayerSpecCtx(e.G, "Player.controlsCreature.Goblin_GE2", 0, 0, pc) {
		t.Fatal("controlsCreature.Goblin_GE2 did not count both layer-4 granted Goblins")
	}
	if effects.MatchesPlayerSpecCtx(e.G, "Player.controlsCreature.Goblin_GE3", 0, 0, pc) {
		t.Fatal("controlsCreature.Goblin_GE3 matched with only two Goblins")
	}
}

// TestPlayerControlsPermanentSeesLayer3Name pins the layer-3 sibling: a
// SetName$ rename must satisfy Player.controlsPermanent.named<X>, exactly as
// it already satisfies an ordinary named<X> object filter.
func TestPlayerControlsPermanentSeesLayer3Name(t *testing.T) {
	t.Parallel()
	blade := card(t, "Name:Renaming Blade\nManaCost:1\nTypes:Artifact Equipment\nK:Equip:1\n"+
		"S:Mode$ Continuous | Affected$ Creature.EquippedBy | SetName$ First Name | Description$ x\nOracle:x\n")
	bear := card(t, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, _, _ := corpusDeckEngine(t, nil, []*cards.Card{blade, bear})
	var bladeID, bearID state.ObjID
	for i := range e.G.Objs {
		ob := &e.G.Objs[i]
		if ob.Zone != state.ZBattlefield {
			continue
		}
		switch ob.Card {
		case blade:
			bladeID = ob.ID
		case bear:
			bearID = ob.ID
		}
	}
	if bladeID == 0 || bearID == 0 {
		t.Fatalf("setup: blade=%d bear=%d", bladeID, bearID)
	}

	// Precondition: before the attach the seat controls no "First Name".
	pcBefore := e.playerSpecCtx(0)
	if effects.MatchesPlayerSpecCtx(e.G, "Player.controlsPermanent.namedFirst_Name", 0, 0, pcBefore) {
		t.Fatal("precondition: the rename must not apply before the Equipment attaches")
	}
	// The layer-3 rename must also genuinely differ from the printed face.
	if o := e.G.Obj(bearID); o == nil || o.Face() == nil || o.Face().Name != "Grizzly Bears" {
		t.Fatalf("precondition: bear must print its own name: %+v", o)
	}

	e.emit(events.Event{Kind: events.Attach, Obj: bladeID, IDs: []state.ObjID{bearID}})
	if e.G.Obj(bladeID).AttachedTo != bearID {
		t.Fatalf("attach did not take: blade on %d, want %d", e.G.Obj(bladeID).AttachedTo, bearID)
	}
	if e.Derived(bearID).Name == "Grizzly Bears" {
		t.Fatal("precondition: the layer-3 rename did not change the derived name")
	}
	// Precondition: the ordinary named<X> object filter already sees the
	// rename (this is the layer-3 half's own control).
	if !effects.MatchesSpecCtx(e.G, "Card.namedFirst_Name", bearID, e.specCtx(0, 0)) {
		t.Fatal("precondition: the ordinary filter must see the SetName$ rename")
	}

	pc := e.playerSpecCtx(0)
	if !effects.MatchesPlayerSpecCtx(e.G, "Player.controlsPermanent.namedFirst_Name", 0, 0, pc) {
		t.Fatal("controlsPermanent.named<X> did not see the SetName$ rename")
	}
	// Scope control: seat 1 controls nothing renamed.
	if effects.MatchesPlayerSpecCtx(e.G, "Player.controlsPermanent.namedFirst_Name", 1, 0, pc) {
		t.Fatal("seat 1 controls no renamed permanent, yet controlsPermanent matched")
	}
}

// TestPlayerControlsPrintedFaceFallback pins the documented fallback: the
// context-free player-spec entry points (MatchesPlayerSpec / From, which own
// no engine handle and therefore no layer tables) still read the printed
// face. This is a scope control, not a widening: the fix threads the tables
// only through contexts a rules caller builds.
func TestPlayerControlsPrintedFaceFallback(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Maskwood Nexus")}, nil)
	moveByName(t, e, 0, "Maskwood Nexus", state.ZBattlefield)
	victim := onBoard(t, e, 0, controlsVictim)

	// Preconditions: the rules-built context DOES see the granted type while
	// the context-free one does not -- so the compared values differ.
	if !slices.Contains(e.Derived(victim).Types, "Goblin") {
		t.Fatalf("precondition: the layer walk must derive Goblin, types=%v", e.Derived(victim).Types)
	}
	if !effects.MatchesPlayerSpecCtx(e.G, "Player.controlsCreature.Goblin", 0, 0, e.playerSpecCtx(0)) {
		t.Fatal("precondition: the rules-built context must see the granted Goblin")
	}
	if effects.MatchesPlayerSpec(e.G, "Player.controlsCreature.Goblin", 0, 0) {
		t.Fatal("a context-free controlsCreature read must not see the layer-4 granted type")
	}
	if effects.MatchesPlayerSpecFrom(e.G, "Player.controlsCreature.Goblin", 0, 0, 0) {
		t.Fatal("MatchesPlayerSpecFrom must not see the layer-4 granted type either")
	}
}
