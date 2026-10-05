package rules

// stat:IgnoreHexproof (CR 702.11) — "Creatures your opponents control with
// hexproof can be the targets of spells and abilities you control as though
// they didn't have hexproof." Nowhere to Run (DSK) is the Standard carrier;
// Detection Tower, Glaring Spotlight and Kaya, Bane of the Dead are the
// corpus-wide rest.
//
// Before this change the static was unregistered, so the coverage gate skipped
// every carrier and hexproof withheld unconditionally. The static's
// ValidEntity$ names the affected entity, Activator$ scopes whose spells and
// abilities benefit.

import (
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

const ignoreHexproofSrc = "Name:Test Nowhere\nManaCost:2 B\nTypes:Enchantment\n" +
	"S:Mode$ IgnoreHexproof | ValidEntity$ Creature.OppCtrl | Description$ Creatures your opponents control can be the targets of spells and abilities as though they didn't have hexproof.\n" +
	"Oracle:x\n"

const ignoreHexproofSelfSrc = "Name:Test Tower\nTypes:Land\n" +
	"S:Mode$ IgnoreHexproof | Activator$ You | ValidEntity$ Creature.OppCtrl | Description$ Creatures your opponents control with hexproof can be the targets of spells and abilities you control.\n" +
	"Oracle:x\n"

// TestIgnoreHexproofLiftsHexproofForScopedController is the behavioural leaf:
// a hexproof creature on seat 1's board withholds seat 0's targeting first
// (precondition asserted), then a seat-0 IgnoreHexproof static whose
// ValidEntity$ names it lifts the withholding, while a static controlled by
// seat 1 does NOT (its Activator$ You scopes the exemption to its own
// controller).
func TestIgnoreHexproofLiftsHexproofForScopedController(t *testing.T) {
	t.Parallel()
	hexed := "Name:Hexed Elf\nManaCost:G\nTypes:Creature Elf\nK:Hexproof\nPT:2/2\nOracle:x\n"
	e := handEngine(t)
	hexID := onBoard(t, e, 1, hexed)

	// Precondition: hexproof really withholds seat 0's targeting, and the
	// carrier really carries it.
	if !e.HasKeyword(hexID, "Hexproof") {
		t.Fatalf("precondition: fixture lacks Hexproof: %v", e.Keywords(hexID))
	}
	if !e.hexproofBlocksTarget(hexID, 0, 0) {
		t.Fatal("precondition: hexproof did not withhold seat 0's targeting before the static")
	}

	// A seat-1 static must NOT help seat 0 (Activator$ You is seat 1).
	onBoard(t, e, 1, ignoreHexproofSrc)
	if !e.hexproofBlocksTarget(hexID, 0, 0) {
		t.Fatal("opponent-controlled IgnoreHexproof lifted seat 0's hexproof gate (Activator$ scope broken)")
	}

	// A seat-0 static whose ValidEntity$ names the opponent's creature lifts
	// it for seat 0's targeting.
	onBoard(t, e, 0, ignoreHexproofSelfSrc)
	if len(e.activeStatics("IgnoreHexproof")) < 2 {
		t.Fatalf("precondition: expected both IgnoreHexproof statics live, got %d", len(e.activeStatics("IgnoreHexproof")))
	}
	if e.hexproofBlocksTarget(hexID, 0, 0) {
		t.Fatal("IgnoreHexproof did not lift hexproof for the static's controller (CR 702.11)")
	}
	// Its own controller's targeting is unaffected either way (hexproof is
	// asymmetric, and ignore-hexproof never ADDS a restriction).
	if e.hexproofBlocksTarget(hexID, 1, 0) {
		t.Fatal("IgnoreHexproof wrongly withheld the hexproof creature's own controller")
	}
}

// TestIgnoreHexproofScopeIsEntityExact pins that the static only lifts the
// withholding for entities its ValidEntity$ selects: a creature controlled by
// the static's own controller (not Creature.OppCtrl) stays protected.
func TestIgnoreHexproofScopeIsEntityExact(t *testing.T) {
	t.Parallel()
	hexed := "Name:Hexed Bear\nManaCost:G\nTypes:Creature Bear\nK:Hexproof\nPT:2/2\nOracle:x\n"
	e := handEngine(t)
	ownHex := onBoard(t, e, 0, hexed)
	onBoard(t, e, 0, ignoreHexproofSelfSrc)

	// Creature.OppCtrl from seat 0 selects seat 1's creatures only, so seat
	// 1's targeting of seat 0's creature is untouched.
	if !e.hexproofBlocksTarget(ownHex, 1, 0) {
		t.Fatal("IgnoreHexproof (Creature.OppCtrl) wrongly lifted a same-controller creature")
	}
}

// TestIgnoreHexproofPrimitiveIsRegistered pins the support declaration the
// skip gate reads.
func TestIgnoreHexproofPrimitiveIsRegistered(t *testing.T) {
	if !effects.Supported()["stat:IgnoreHexproof"] {
		t.Fatal(`effects.Supported() is missing "stat:IgnoreHexproof"`)
	}
}

// ignoreHexproofCarriers is the corpus-wide printed/Effect-delivered class.
var ignoreHexproofCarriers = []string{
	"Detection Tower",
	"Glaring Spotlight",
	"Kaya, Bane of the Dead",
	"Nowhere to Run",
}

// TestIgnoreHexproofClassCensus pins the carrier set so a corpus-pin bump that
// adds a new carrier fails loudly. It uses the same two-route walk the
// NoCleanupDamage ratchet does: printed S: statics and Effect-delivered
// bodies (Detection Tower's `AB$ Effect | StaticAbilities$ STLoseAB`).
func TestIgnoreHexproofClassCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	found := ignoreHexproofCorpusCarriers(reg)
	list := make([]string, 0, len(found))
	for n := range found {
		list = append(list, n)
	}
	sort.Strings(list)
	want := append([]string(nil), ignoreHexproofCarriers...)
	sort.Strings(want)
	if len(list) != len(want) {
		t.Fatalf("IgnoreHexproof carriers: got %d, want %d\ngot:  %s\nwant: %s", len(list), len(want), joinQuoted(list), joinQuoted(want))
	}
	for i := range want {
		if list[i] != want[i] {
			t.Fatalf("IgnoreHexproof carrier %d = %q, want %q", i, list[i], want[i])
		}
	}
}

// ignoreHexproofCorpusCarriers collects every card that spells
// Mode$ IgnoreHexproof through either delivery route.
func ignoreHexproofCorpusCarriers(reg *cards.Registry) map[string]bool {
	got := map[string]bool{}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			for _, st := range f.Statics {
				if st.Mode == "IgnoreHexproof" {
					got[f.Name] = true
				}
			}
			f.EachRawEffectChild(func(ch cards.EffectChild) {
				if ch.Static != nil && ch.Static.Mode == "IgnoreHexproof" {
					got[f.Name] = true
				}
			})
		}
	}
	return got
}

// TestIgnoreHexproofCarriersCarryTheMode keeps the corpus-shape coupling
// honest: every pinned carrier must still spell the mode, so a corpus pin that
// dropped it fails rather than making the census vacuous.
func TestIgnoreHexproofCarriersCarryTheMode(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	found := ignoreHexproofCorpusCarriers(reg)
	for _, name := range ignoreHexproofCarriers {
		if !found[name] {
			t.Fatalf("pinned carrier %q no longer carries Mode$ IgnoreHexproof", name)
		}
	}
}

// TestIgnoreHexproofFalseGateDoesNotLift is the static-gate regression: an
// IgnoreHexproof static whose Condition$ PlayerTurn gate is FALSE (its
// controller is not the active player) must NOT lift hexproof. Before the fix
// the read matched ValidEntity$ without evaluating the gate, so a false-gated
// static granted the exception anyway.
func TestIgnoreHexproofFalseGateDoesNotLift(t *testing.T) {
	t.Parallel()
	hexed := "Name:Hexed Elf\nManaCost:G\nTypes:Creature Elf\nK:Hexproof\nPT:2/2\nOracle:x\n"
	gated := "Name:Test Gated Tower\nTypes:Land\n" +
		"S:Mode$ IgnoreHexproof | Activator$ You | ValidEntity$ Creature.OppCtrl | Condition$ PlayerTurn | Description$ Creatures your opponents control with hexproof can be targeted as though they didn't have hexproof.\n" +
		"Oracle:x\n"
	e := handEngine(t)
	hexID := onBoard(t, e, 1, hexed)
	onBoard(t, e, 0, gated)

	// Precondition: the carrier carries hexproof, the static is live and its
	// gate is false (seat 1 is active, seat 0 controls the static).
	if !e.HasKeyword(hexID, "Hexproof") {
		t.Fatalf("precondition: fixture lacks Hexproof: %v", e.Keywords(hexID))
	}
	svs := e.activeStatics("IgnoreHexproof")
	if len(svs) != 1 {
		t.Fatalf("precondition: activeStatics(IgnoreHexproof) = %d entries, want 1", len(svs))
	}
	e.G.Active = 1
	if e.staticGateHolds(svs[0]) {
		t.Fatal("precondition: the fixture's Condition$ PlayerTurn gate must be false with seat 1 active")
	}

	if !e.hexproofBlocksTarget(hexID, 0, 0) {
		t.Fatal("false-gated IgnoreHexproof lifted hexproof (CR 702.11 gate not evaluated)")
	}
}

// TestIgnoreHexproofTrueGateLifts asserts the other direction: with the
// static's controller active the gate holds and hexproof is lifted.
func TestIgnoreHexproofTrueGateLifts(t *testing.T) {
	t.Parallel()
	hexed := "Name:Hexed Elf\nManaCost:G\nTypes:Creature Elf\nK:Hexproof\nPT:2/2\nOracle:x\n"
	gated := "Name:Test Gated Tower\nTypes:Land\n" +
		"S:Mode$ IgnoreHexproof | Activator$ You | ValidEntity$ Creature.OppCtrl | Condition$ PlayerTurn | Description$ Creatures your opponents control with hexproof can be targeted as though they didn't have hexproof.\n" +
		"Oracle:x\n"
	e := handEngine(t)
	hexID := onBoard(t, e, 1, hexed)
	onBoard(t, e, 0, gated)

	if !e.HasKeyword(hexID, "Hexproof") {
		t.Fatalf("precondition: fixture lacks Hexproof: %v", e.Keywords(hexID))
	}
	svs := e.activeStatics("IgnoreHexproof")
	if len(svs) != 1 {
		t.Fatalf("precondition: activeStatics(IgnoreHexproof) = %d entries, want 1", len(svs))
	}
	e.G.Active = 0
	if !e.staticGateHolds(svs[0]) {
		t.Fatal("precondition: the fixture's Condition$ PlayerTurn gate must hold with seat 0 active")
	}

	if e.hexproofBlocksTarget(hexID, 0, 0) {
		t.Fatal("true-gated IgnoreHexproof failed to lift hexproof")
	}
}
