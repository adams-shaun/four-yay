package rules

// Increment (CR 702.XX, task kw:Increment): "Whenever you cast a spell, if
// the amount of mana you spent is greater than this creature's power or
// toughness, put a +1/+1 counter on this creature."
//
// cards/kw_increment.go expands the bare `K:Increment` line into an ordinary
// Mode$ SpellCast trigger on the source, and rules/trigmatch_cast.go's
// incrementAdmits evaluates the event-relative spend-vs-power/toughness
// condition. These tests pin the census over the corpus carriers and the
// mechanism's two halves (the spend must EXCEED, and either power or
// toughness admitting is enough).
//
// Each mechanism subtest drives ONE Increment creature, so its trigger never
// competes with a sibling's and no KTriggerOrder ask interleaves the casts.

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// incrementCorpusCarriers is every corpus card whose script carries a bare
// `K:Increment` line at the pin. Measured with
// `/usr/bin/grep -rlE '^K:Increment' .cards/cardsfolder` -> 10 files. Nine are
// Secrets of Strixhaven (sos) carriers; the tenth, Scalar Scholar, is not in
// the audit's 271-card sos list (its oracle's "perpetually gains" is the
// Alchemy rider), which matches the brief's "10 corpus files (9 sos)".
var incrementCorpusCarriers = []string{
	"Ambitious Augmenter",
	"Berta, Wise Extrapolator",
	"Cuboid Colony",
	"Fractal Tender",
	"Hungry Graffalon",
	"Pensive Professor",
	"Scalar Scholar",
	"Tester of the Tangential",
	"Textbook Tabulator",
	"Topiary Lecturer",
}

// TestIncrementCensusEveryCorpusCarrierIsSupported names the other affected
// cards the ticket calls for: every corpus carrier of K:Increment must report
// no missing primitives once the keyword expands and its marker is registered.
// It is the ratchet direction (acceptance_test.go's knownUnsupported is empty
// for these; a regression that drops the expander or the registration makes
// this fail with the exact card and primitive named).
func TestIncrementCensusEveryCorpusCarrierIsSupported(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	for _, name := range incrementCorpusCarriers {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus missing Increment carrier %q -- the census cannot be measured", name)
		}
		if m := reg.Unsupported(c, supported); len(m) > 0 {
			t.Errorf("%s still needs %v", name, m)
		}
	}
}

// incrementCast drives one cast of the named spell and returns the source's
// +1/+1 counter count afterwards. It asserts the source is still on the
// battlefield before and after, so a setup whose creature never arrived (or
// whose spell never resolved) fails loudly rather than reading a zero from an
// object the trigger never saw.
func incrementCast(t *testing.T, e *Engine, srcID state.ObjID, spellSrc string) int32 {
	t.Helper()
	if o := e.G.Obj(srcID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: increment source not on battlefield (zone %v)", o)
	}
	spell := addToHand(t, e, 0, spellSrc)
	e.Advance()
	addMana(t, e, 0, "CCCCC")
	castObj(t, e, spell)
	if o := e.G.Obj(srcID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("after the cast: increment source left the battlefield (zone %v)", o)
	}
	return e.G.Obj(srcID).Counter("P1P1")
}

// TestIncrementSpendMustExceedPowerAndToughness pins the negative direction:
// a 2/2's power and toughness are 2, so a 2-mana spend (2 > 2 is false) adds
// nothing, while a 3-mana spend exceeds both and adds one. The assertions run
// in order on one creature, so the counter moving from 0 to 1 is attributable
// to the expensive cast rather than to setup.
func TestIncrementSpendMustExceedPowerAndToughness(t *testing.T) {
	t.Parallel()
	bear := "Name:Inc Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nK:Increment\nOracle:x\n"
	one := "Name:One Spell\nManaCost:1\nTypes:Instant\nA:SP$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"
	two := "Name:Two Spell\nManaCost:2\nTypes:Instant\nA:SP$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"
	three := "Name:Three Spell\nManaCost:3\nTypes:Instant\nA:SP$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"

	e, cfg, bearID := newFixtureDeck(t, 922, bear, one, two, three)
	e.emit(events.Event{Kind: events.MoveZone, Obj: bearID, From: state.ZHand, To: state.ZBattlefield})
	e.priorityRound()
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Inc Bear not on battlefield (zone %v)", o)
	}
	if p, tt := e.Power(bearID), e.Toughness(bearID); p != 2 || tt != 2 {
		t.Fatalf("precondition: Inc Bear = %d/%d, want 2/2", p, tt)
	}

	if got := incrementCast(t, e, bearID, one); got != 0 {
		t.Errorf("spent 1 vs 2/2: counters = %d, want 0 (1 is not greater than power or toughness)", got)
	}
	if got := incrementCast(t, e, bearID, two); got != 0 {
		t.Errorf("spent 2 vs 2/2: counters = %d, want 0 (2 is not greater than 2)", got)
	}
	if got := incrementCast(t, e, bearID, three); got != 1 {
		t.Errorf("spent 3 vs 2/2: counters = %d, want 1 (3 exceeds both)", got)
	}
	replayCheck(t, e, cfg)
}

// TestIncrementToughnessHalfAdmits proves the oracle's "power or toughness"
// disjunction: a 4/1 whose power (4) a 2-mana spend cannot exceed still gets
// the counter, because 2 > toughness 1. If the comparison read only power,
// this counter would be 0; if it read only toughness, the 2/2 case above
// (2 > 2) would be unaffected, so the pair pins both halves.
func TestIncrementToughnessHalfAdmits(t *testing.T) {
	t.Parallel()
	glass := "Name:Inc Glass\nManaCost:1 G\nTypes:Creature Bear\nPT:4/1\nK:Increment\nOracle:x\n"
	two := "Name:Two Spell\nManaCost:2\nTypes:Instant\nA:SP$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"

	e, cfg, glassID := newFixtureDeck(t, 923, glass, two)
	e.emit(events.Event{Kind: events.MoveZone, Obj: glassID, From: state.ZHand, To: state.ZBattlefield})
	e.priorityRound()
	if o := e.G.Obj(glassID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Inc Glass not on battlefield (zone %v)", o)
	}
	if p, tt := e.Power(glassID), e.Toughness(glassID); p != 4 || tt != 1 {
		t.Fatalf("precondition: Inc Glass = %d/%d, want 4/1", p, tt)
	}
	if got := e.G.Obj(glassID).Counter("P1P1"); got != 0 {
		t.Fatalf("precondition: Inc Glass already carries %d counters, want 0", got)
	}

	if got := incrementCast(t, e, glassID, two); got != 1 {
		t.Errorf("spent 2 vs 4/1: counters = %d, want 1 (2 > toughness 1 admits despite 2 < power 4)", got)
	}
	replayCheck(t, e, cfg)
}
