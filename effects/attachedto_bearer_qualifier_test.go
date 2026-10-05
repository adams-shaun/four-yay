package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestAttachedToBearerEnchantedBy is the effects leaf for the dotted
// "<class>.<qual>" form of the two-token AttachedTo grammar, where the
// qualifier names a property of the ATTACHED BEARER. With Great Power . . .
// ("Enchanted creature gets +2/+2 for each Aura and Equipment attached to
// it") is the headline carrier:
//
//	SVar:X:Count$Valid Aura.AttachedTo Creature.EnchantedBy,Equipment.AttachedTo Creature.EnchantedBy/Times.2
//
// Before this change attachedToArg accepted only YouCtrl as a dotted
// qualifier, so the token failed closed as an unknown predicate and the
// count was 0. The qualifier bodies (EnchantedBy/EquippedBy) read sc.Source
// through attachedBy, so the SpecContext must name the Aura as the resolving
// source. Every comparison below names a value that DIFFERS from the wrong
// answer, so a fail-closed zero cannot pass silently.
func TestAttachedToBearerEnchantedBy(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	g := state.NewGame([]string{"you", "them"})
	bear := corpusObject(t, reg, g, "Grizzly Bears")
	other := corpusObject(t, reg, g, "Hill Giant")

	auraOnBear := corpusObject(t, reg, g, "Unholy Strength")
	auraOnBear.AttachedTo = bear.ID
	auraOnOther := corpusObject(t, reg, g, "Unholy Strength")
	auraOnOther.AttachedTo = other.ID
	auraFree := corpusObject(t, reg, g, "Unholy Strength")

	// Preconditions: the bearers are battlefield permanents and exactly one
	// Aura names the bear.
	if bear.Zone != state.ZBattlefield || auraOnBear.Zone != state.ZBattlefield {
		t.Fatalf("precondition: bear=%v aura=%v must be on the battlefield", bear.Zone, auraOnBear.Zone)
	}
	if auraOnBear.AttachedTo != bear.ID {
		t.Fatalf("precondition: the Aura must name the bear, got %v", auraOnBear.AttachedTo)
	}
	if auraOnOther.AttachedTo == bear.ID {
		t.Fatalf("precondition: the second Aura must name a different creature")
	}

	sc := SpecContext{You: 0, Source: auraOnBear.ID}
	const spec = "Aura.AttachedTo Creature.EnchantedBy"

	// The recognition path and the census must agree: the token is no longer
	// unknown.
	if got := UnknownPredicates(spec); len(got) != 0 {
		t.Fatalf("UnknownPredicates(%q) = %v, want empty", spec, got)
	}
	if _, ok := attachedToArg("AttachedTo Creature.EnchantedBy"); !ok {
		t.Fatalf("attachedToArg REJECTS %q", "AttachedTo Creature.EnchantedBy")
	}

	if !MatchesObjectCtx(g, spec, auraOnBear, sc) {
		t.Errorf("%s must match the Aura the resolving source is attached to", spec)
	}
	if MatchesObjectCtx(g, spec, auraOnOther, sc) {
		t.Errorf("%s must not match an Aura attached to a different creature", spec)
	}
	if MatchesObjectCtx(g, spec, auraFree, sc) {
		t.Errorf("%s must not match an unattached Aura", spec)
	}
}

// TestAttachedToBearerEquippedBy is the Equipment half: Bonesplitter attached
// to the bear satisfies "Equipment.AttachedTo Creature.EquippedBy" when the
// Equipment is the resolving source.
func TestAttachedToBearerEquippedBy(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	g := state.NewGame([]string{"you", "them"})
	bear := corpusObject(t, reg, g, "Grizzly Bears")

	equip := corpusObject(t, reg, g, "Bonesplitter")
	equip.AttachedTo = bear.ID
	equipFree := corpusObject(t, reg, g, "Bonesplitter")

	if equip.Zone != state.ZBattlefield || equip.AttachedTo != bear.ID {
		t.Fatalf("precondition: Bonesplitter must be a battlefield permanent attached to the bear")
	}

	sc := SpecContext{You: 0, Source: equip.ID}
	const spec = "Equipment.AttachedTo Creature.EquippedBy"

	if got := UnknownPredicates(spec); len(got) != 0 {
		t.Fatalf("UnknownPredicates(%q) = %v, want empty", spec, got)
	}
	if !MatchesObjectCtx(g, spec, equip, sc) {
		t.Errorf("%s must match the Equipment attached to the resolving source's bearer", spec)
	}
	if MatchesObjectCtx(g, spec, equipFree, sc) {
		t.Errorf("%s must not match an unattached Equipment", spec)
	}
}

// TestAttachedToBearerIsRemembered pins the qualifier that is NOT in the
// legacy `predicates` function map: Baki's Curse's
// "Aura.AttachedTo Creature.IsRemembered" needs sc.Remembered, so a
// predicates[qual] lookup could never cover it. The object-filter grammar
// reads the word-kind IsRemembered predicate directly.
func TestAttachedToBearerIsRemembered(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	g := state.NewGame([]string{"you", "them"})
	bear := corpusObject(t, reg, g, "Grizzly Bears")
	other := corpusObject(t, reg, g, "Hill Giant")

	auraOnBear := corpusObject(t, reg, g, "Unholy Strength")
	auraOnBear.AttachedTo = bear.ID

	if auraOnBear.Zone != state.ZBattlefield || auraOnBear.AttachedTo != bear.ID {
		t.Fatalf("precondition: the Aura must be a battlefield permanent attached to the bear")
	}

	const spec = "Aura.AttachedTo Creature.IsRemembered"

	// The bear is remembered: the Aura attached to it matches.
	scBear := SpecContext{You: 0, Resolving: true, Remembered: []state.Target{{Obj: bear.ID}}}
	if len(scBear.Remembered) == 0 || scBear.Remembered[0].Obj != bear.ID {
		t.Fatalf("precondition: the remembered set must hold the bear")
	}
	if got := UnknownPredicates(spec); len(got) != 0 {
		t.Fatalf("UnknownPredicates(%q) = %v, want empty", spec, got)
	}
	if !MatchesObjectCtx(g, spec, auraOnBear, scBear) {
		t.Errorf("%s must match an Aura attached to a remembered creature", spec)
	}

	// A remembered DIFFERENT creature must not admit the Aura.
	scOther := SpecContext{You: 0, Resolving: true, Remembered: []state.Target{{Obj: other.ID}}}
	if MatchesObjectCtx(g, spec, auraOnBear, scOther) {
		t.Errorf("%s must not match an Aura attached to a creature outside the remembered set", spec)
	}
}

// TestAttachedToBearerQualifierNegation pins the '!' form: an unattached Aura
// satisfies "Aura.!AttachedTo Creature.EnchantedBy" and an attached one does
// not, so the negation rides the bearer qualifier rather than inverting an
// always-false token.
func TestAttachedToBearerQualifierNegation(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	g := state.NewGame([]string{"you", "them"})
	bear := corpusObject(t, reg, g, "Grizzly Bears")

	auraOnBear := corpusObject(t, reg, g, "Unholy Strength")
	auraOnBear.AttachedTo = bear.ID
	auraFree := corpusObject(t, reg, g, "Unholy Strength")

	if auraOnBear.AttachedTo != bear.ID || auraFree.AttachedTo != 0 {
		t.Fatalf("precondition: one attached and one free Aura")
	}

	// A nested predicate (a second '.') stays fail-closed: the qualifier must
	// be exactly one predicate token, so a whole dotted sub-spec is refused by
	// both the matcher and the census.
	for _, nested := range []string{
		"AttachedTo Creature.Permanent.YouCtrl",
		"AttachedTo Creature.EnchantedBy.Weird",
	} {
		if _, ok := attachedToArg(nested); ok {
			t.Errorf("attachedToArg must reject the nested predicate %q", nested)
		}
		if got := UnknownPredicates("Aura." + nested); len(got) == 0 {
			t.Errorf("UnknownPredicates must report the nested predicate %q", nested)
		}
	}

	sc := SpecContext{You: 0, Source: auraOnBear.ID}
	const spec = "Aura.!AttachedTo Creature.EnchantedBy"

	if got := UnknownPredicates(spec); len(got) != 0 {
		t.Fatalf("UnknownPredicates(%q) = %v, want empty", spec, got)
	}
	if !MatchesObjectCtx(g, spec, auraFree, sc) {
		t.Errorf("%s must match an unattached Aura", spec)
	}
	if MatchesObjectCtx(g, spec, auraOnBear, sc) {
		t.Errorf("%s must not match an Aura attached to the resolving source's bearer", spec)
	}
}

// TestEvalCountValidAttachedToEnchantedBy is the count-level assertion of the
// real pump value With Great Power . . . computes. The X SVar is
// "Count$Valid Aura.AttachedTo Creature.EnchantedBy,Equipment.AttachedTo
// Creature.EnchantedBy/Times.2". The qualifier describes the ATTACHED
// BEARER (the enchanted creature), so both alternatives admit an object
// attached to that creature -- an Aura or an Equipment. With one attached
// Aura the count is 1 and Times.2 makes 2; an attached Equipment raises it
// to 4; a second Aura raises it to 6. Every asserted value differs from the
// pre-fix zero, so a fail-closed token cannot pass by coincidence.
func TestEvalCountValidAttachedToEnchantedBy(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	g := state.NewGame([]string{"you", "them"})
	h := &fakeHost{g: g}
	bear := corpusObject(t, reg, g, "Grizzly Bears")

	aura := corpusObject(t, reg, g, "Unholy Strength")
	aura.AttachedTo = bear.ID

	// corpusObject sets the Zone FIELD; the Count$Valid battlefield scan walks
	// the zone LIST, so the permanents must be registered there too, exactly
	// as TestEvalCountValidCountsTheBattlefield's board fixture does.
	battle := g.Zone(state.ZBattlefield, 0)
	battle = append(battle, bear.ID, aura.ID)
	g.SetZone(state.ZBattlefield, 0, battle)

	if bear.Zone != state.ZBattlefield || aura.Zone != state.ZBattlefield || aura.AttachedTo != bear.ID {
		t.Fatalf("precondition: the bear and its Aura must be battlefield permanents, Aura attached to the bear")
	}
	if len(g.Zone(state.ZBattlefield, 0)) != 2 {
		t.Fatalf("precondition: the battlefield zone must carry exactly the two permanents, got %v", g.Zone(state.ZBattlefield, 0))
	}

	c := &Ctx{Controller: 0, Source: aura.ID}

	// With Great Power's exact X SVar body: one Aura counts 1, Times.2 = 2.
	if got := EvalCount(h, c, "Count$Valid Aura.AttachedTo Creature.EnchantedBy/Times.2"); got != 2 {
		t.Errorf("Count$Valid Aura.AttachedTo Creature.EnchantedBy/Times.2 = %d, want 2", got)
	}
	// The two-alternative spelling: still one Aura attached, so 2 -- the
	// same as the single spelling and NOT 0, which is the pre-fix value.
	if got := EvalCount(h, c, "Count$Valid Aura.AttachedTo Creature.EnchantedBy,Equipment.AttachedTo Creature.EnchantedBy/Times.2"); got != 2 {
		t.Errorf("the two-alternative spelling with one attached Aura = %d, want 2", got)
	}

	// A real Equipment attached to the same creature: both alternatives now
	// find a permanent, 2 * 2 = 4.
	equip := corpusObject(t, reg, g, "Bonesplitter")
	equip.AttachedTo = bear.ID
	g.SetZone(state.ZBattlefield, 0, append(g.Zone(state.ZBattlefield, 0), equip.ID))
	if got := EvalCount(h, c, "Count$Valid Aura.AttachedTo Creature.EnchantedBy,Equipment.AttachedTo Creature.EnchantedBy/Times.2"); got != 4 {
		t.Errorf("one Aura plus one Equipment attached, Times.2 = %d, want 4", got)
	}

	// A second Aura (Strong Back's / Auramancer's Guise's shape) makes 2
	// attached Auras: 2 * 2 = 4 on the single-alternative spelling.
	aura2 := corpusObject(t, reg, g, "Unholy Strength")
	aura2.AttachedTo = bear.ID
	g.SetZone(state.ZBattlefield, 0, append(g.Zone(state.ZBattlefield, 0), aura2.ID))
	if got := EvalCount(h, c, "Count$Valid Aura.AttachedTo Creature.EnchantedBy/Times.2"); got != 4 {
		t.Errorf("two attached Auras with Times.2 = %d, want 4", got)
	}
}
