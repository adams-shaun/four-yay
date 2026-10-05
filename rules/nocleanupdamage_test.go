package rules

// stat:NoCleanupDamage (CR 514.2) — "Damage isn't removed from this creature
// during cleanup steps." The cleanup step's damage-removal pass
// (rules/combat.go cleanupBody) removed every permanent's marked damage
// unconditionally; a carrier of the static must keep it. Ancient Adamantoise
// is the Standard carrier, six more exist corpus-wide.
//
// The test marks damage on two creatures -- a carrier and a control -- runs
// the real cleanup body, and asserts only the control is healed. Both
// preconditions are asserted so a vacuous setup (no static, no damage) fails
// loudly.

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

const noCleanupDamageSrc = "Name:Test Adamantoise\nManaCost:5 G G G\n" +
	"Types:Creature Turtle\nPT:8/20\n" +
	"S:Mode$ NoCleanupDamage | ValidCard$ Card.Self | Description$ Damage isn't removed from this creature during cleanup steps.\n" +
	"Oracle:x\n"

func TestNoCleanupDamageStaticKeepsMarkedDamage(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	turtle := onBoardCard(t, e, 0, card(t, noCleanupDamageSrc))
	control := onBoardCard(t, e, 0, card(t, "Name:Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))

	// Preconditions: the static is live and selects the carrier, both are
	// creatures, and both carry marked damage to start.
	if got := e.activeStatics("NoCleanupDamage"); len(got) != 1 {
		t.Fatalf("precondition: activeStatics(NoCleanupDamage) = %d entries, want 1", len(got))
	}
	if !e.IsCreature(turtle) || !e.IsCreature(control) {
		t.Fatal("precondition: both fixtures must be creatures on the battlefield")
	}
	e.G.Obj(turtle).Damage = 3
	e.G.Obj(control).Damage = 2

	e.cleanupBody()

	if got := e.G.Obj(turtle).Damage; got != 3 {
		t.Fatalf("NoCleanupDamage carrier lost marked damage in cleanup: Damage = %d, want 3", got)
	}
	if got := e.G.Obj(control).Damage; got != 0 {
		t.Fatalf("control creature's damage not removed in cleanup: Damage = %d, want 0", got)
	}
}

// TestNoCleanupDamagePrimitiveIsRegistered pins the support declaration the
// skip gate reads.
func TestNoCleanupDamagePrimitiveIsRegistered(t *testing.T) {
	if !effects.Supported()["stat:NoCleanupDamage"] {
		t.Fatal(`effects.Supported() is missing "stat:NoCleanupDamage"`)
	}
}

// TestNoCleanupDamageClassCensus pins the corpus-wide carrier set so a
// corpus-pin bump that adds a new carrier fails loudly.
//
// Includes printed, Effect-delivered and Animate-granted static bodies.
// The Animate path is an SVar referenced through staticAbilities$, not a
// printed Face.Static; inspect the source table so pin bumps cannot hide one.
var noCleanupDamageCarriers = []string{
	"Ancient Adamantoise",
	"Case of the Market Melee",
	"Melt Through",
	"Patient Zero",
	"Switchgrass Grazer",
	"Uthgardt Fury",
	"Victory of the Pyrohammer",
}

func TestNoCleanupDamageClassCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	got := map[string]bool{}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			for _, st := range f.Statics {
				if st.Mode == "NoCleanupDamage" {
					got[f.Name] = true
				}
			}
			for _, body := range f.SVars {
				if strings.Contains(body, "Mode$ NoCleanupDamage") {
					got[f.Name] = true
				}
			}
			// Effect-delivered carriers (a body whose StaticAbilities$ names the
			// mode in an SVar) are reached only through EachRawEffectChild, the
			// same source Face.Primitives reads for the stat: coverage token.
			f.EachRawEffectChild(func(ch cards.EffectChild) {
				if ch.Static != nil && ch.Static.Mode == "NoCleanupDamage" {
					got[f.Name] = true
				}
			})
		}
	}
	list := make([]string, 0, len(got))
	for n := range got {
		list = append(list, n)
	}
	sort.Strings(list)
	want := append([]string(nil), noCleanupDamageCarriers...)
	sort.Strings(want)
	if len(list) != len(want) {
		t.Fatalf("NoCleanupDamage carriers: got %d, want %d\ngot:  %s\nwant: %s", len(list), len(want), joinQuoted(list), joinQuoted(want))
	}
	for i := range want {
		if list[i] != want[i] {
			t.Fatalf("NoCleanupDamage carrier %d = %q, want %q\ngot:  %s\nwant: %s", i, list[i], want[i], joinQuoted(list), joinQuoted(want))
		}
	}
}

// TestNoCleanupDamageFalseGateDoesNotKeepDamage is the static-gate regression:
// a NoCleanupDamage carrier whose Condition$ PlayerTurn gate is FALSE (its
// controller is not the active player) must NOT keep marked damage. Before the
// fix the cleanup read matched ValidCard$ without evaluating the gate, so a
// false-gated static preserved damage anyway.
func TestNoCleanupDamageFalseGateDoesNotKeepDamage(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	gated := "Name:Test Gated Tortoise\nManaCost:5 G G G\n" +
		"Types:Creature Turtle\nPT:8/20\n" +
		"S:Mode$ NoCleanupDamage | ValidCard$ Card.Self | Condition$ PlayerTurn | Description$ Damage isn't removed from this creature during cleanup steps.\n" +
		"Oracle:x\n"
	turtle := onBoardCard(t, e, 1, card(t, gated))

	// Precondition: the static is live, selects the carrier, and its gate is
	// genuinely false (seat 1 is not the active player).
	if got := e.activeStatics("NoCleanupDamage"); len(got) != 1 {
		t.Fatalf("precondition: activeStatics(NoCleanupDamage) = %d entries, want 1", len(got))
	}
	e.G.Active = 0
	if e.staticGateHolds(e.activeStatics("NoCleanupDamage")[0]) {
		t.Fatal("precondition: the fixture's Condition$ PlayerTurn gate must be false with seat 0 active")
	}
	e.G.Obj(turtle).Damage = 3

	e.cleanupBody()

	if got := e.G.Obj(turtle).Damage; got != 0 {
		t.Fatalf("false-gated NoCleanupDamage kept marked damage: Damage = %d, want 0", got)
	}
}

// TestNoCleanupDamageTrueGateKeepsDamage asserts the other direction of the
// same gate: with the carrier's controller active the gate holds and the
// damage must survive. This pins that the gate read did not simply turn the
// static off.
func TestNoCleanupDamageTrueGateKeepsDamage(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	gated := "Name:Test Gated Tortoise\nManaCost:5 G G G\n" +
		"Types:Creature Turtle\nPT:8/20\n" +
		"S:Mode$ NoCleanupDamage | ValidCard$ Card.Self | Condition$ PlayerTurn | Description$ Damage isn't removed from this creature during cleanup steps.\n" +
		"Oracle:x\n"
	turtle := onBoardCard(t, e, 0, card(t, gated))

	if got := e.activeStatics("NoCleanupDamage"); len(got) != 1 {
		t.Fatalf("precondition: activeStatics(NoCleanupDamage) = %d entries, want 1", len(got))
	}
	e.G.Active = 0
	if !e.staticGateHolds(e.activeStatics("NoCleanupDamage")[0]) {
		t.Fatal("precondition: the fixture's Condition$ PlayerTurn gate must hold with seat 0 active")
	}
	e.G.Obj(turtle).Damage = 3

	e.cleanupBody()

	if got := e.G.Obj(turtle).Damage; got != 3 {
		t.Fatalf("true-gated NoCleanupDamage lost marked damage: Damage = %d, want 3", got)
	}
}
