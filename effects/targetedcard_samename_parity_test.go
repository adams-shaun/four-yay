package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestTargetedCardSameNameParity guards the textual/compiled parity of the
// TargetedCard base for its NON-Self qualifier. TargetedCard.sameName names a
// card sharing a name with the target (Invasive Surgery, Quash, Counterbore,
// Test of Talents, The End); TargetedCard.Self names the target itself. The
// textual oracle must not drop the sameName alternative while the compiled
// form handles it through the shared contextualSameName rewrite, so both
// matchers are held equal for the target, its namesake and a bystander.
func TestTargetedCardSameNameParity(t *testing.T) {
	g, ids := board(t)
	targetID := ids["myBear"]
	namesakeID := addBattlefieldCard(t, g, "Bear", "Creature Bear")
	otherID := ids["myFlier"]
	target, namesake, other := g.Obj(targetID), g.Obj(namesakeID), g.Obj(otherID)
	if target.Zone != state.ZBattlefield || namesake.Zone != state.ZBattlefield || other.Zone != state.ZBattlefield {
		t.Fatal("precondition: all three objects must be on the battlefield")
	}
	if target.Face().Name != namesake.Face().Name || target.Face().Name == other.Face().Name {
		t.Fatalf("precondition: target/namesake names must equal and differ from bystander: %q, %q, %q", target.Face().Name, namesake.Face().Name, other.Face().Name)
	}
	sc := SpecContext{You: 0, ResolutionTargets: []state.Target{{Obj: targetID}}, Resolving: true}
	const sameNameSpec = "TargetedCard.sameName"
	// The precondition the parity assertion rests on: the textual oracle must
	// actually match the namesake, not merely agree with the compiled form on
	// a uniform false.
	if !MatchesObjectTextOracle(g, sameNameSpec, namesake, sc) {
		t.Fatal("precondition: TargetedCard.sameName must match the namesake in the textual oracle")
	}
	if MatchesObjectTextOracle(g, sameNameSpec, other, sc) {
		t.Fatal("precondition: TargetedCard.sameName must not match the unrelated permanent")
	}
	for _, spec := range []string{sameNameSpec, "TargetedCard.Self"} {
		for _, c := range []struct {
			name string
			o    *state.Object
			self bool
		}{{"target", target, true}, {"namesake", namesake, false}, {"bystander", other, false}} {
			text := MatchesObjectTextOracle(g, spec, c.o, sc)
			compiled := MatchesObjectCompiledCached(g, spec, c.o, sc)
			if spec == "TargetedCard.Self" && (text != c.self || compiled != c.self) {
				t.Errorf("%s %s: textual=%v compiled=%v, want both %v", spec, c.name, text, compiled, c.self)
			}
			if text != compiled {
				t.Errorf("%s %s: textual=%v compiled=%v (parity broken)", spec, c.name, text, compiled)
			}
		}
	}
}

// TestSharesNameWithReferentNarrowing holds the sharesNameWith referent set to
// the name-comparison referents the shared resolver actually binds — Targeted,
// Remembered, RememberedCard, TriggeredCard and Self. Self was classified by
// the Marvin, Murderous Mimic donor ticket (it binds the static's own source,
// SpecContext.Source), so it is recognised at the census level; with no
// Source bound it must still fail closed under both polarities.
// sharesTypeArgCodes aliases its ordinals across
// RememberedCard/RememberedLKI/TriggeredCardLKICopy/Self/Commander/Convoked,
// so a code-based switch there would silently recognise all six and evaluate a
// name comparison against a referent no ticket covered (Reflector Mage's
// RememberedLKI). sharesNameWithArg reads the distinct-code
// sharesTypeReferentsCodes table instead, so the referents outside the
// recognised set — RememberedLKI, Commander, Convoked, TriggeredCardLKICopy —
// must stay unrecognised and fail closed under both polarities.
func TestSharesNameWithReferentNarrowing(t *testing.T) {
	g, ids := board(t)
	namesakeID := addBattlefieldCard(t, g, "Bear", "Creature Bear")
	namesake := g.Obj(namesakeID)
	if namesake == nil || namesake.Zone != state.ZBattlefield {
		t.Fatal("precondition: namesake must be a battlefield permanent")
	}
	sc := SpecContext{You: 0, ResolutionTargets: []state.Target{{Obj: ids["myBear"]}}, Resolving: true}
	for _, ref := range []string{"RememberedLKI", "Commander", "Convoked", "TriggeredCardLKICopy"} {
		spec := "Permanent.sharesNameWith " + ref
		if got := UnknownPredicates(spec); len(got) == 0 {
			t.Errorf("UnknownPredicates(%q) = empty; %s must stay an unrecognised referent", spec, ref)
		}
		if MatchesObjectCtx(g, spec, namesake, sc) {
			t.Errorf("%s must not bind referent %s", spec, ref)
		}
		neg := "Permanent.!sharesNameWith " + ref
		if MatchesObjectCtx(g, neg, namesake, sc) {
			t.Errorf("%s must fail closed (negated spelling must not widen)", neg)
		}
	}
	// The five supported referents must be recognised. Self binds only
	// through SpecContext.Source (the Marvin ticket); with the source
	// unbound the referent is empty and must fail closed under both
	// polarities, exactly like an unrecognised referent.
	for _, ref := range []string{"Targeted", "Remembered", "RememberedCard", "TriggeredCard", "Self"} {
		spec := "Permanent.sharesNameWith " + ref
		if got := UnknownPredicates(spec); len(got) != 0 {
			t.Errorf("UnknownPredicates(%q) = %v, want empty", spec, got)
		}
	}
	for _, spec := range []string{"Permanent.sharesNameWith Self", "Permanent.!sharesNameWith Self"} {
		if MatchesObjectCtx(g, spec, namesake, sc) {
			t.Errorf("%s with no bound source must fail closed, not match", spec)
		}
	}
}
