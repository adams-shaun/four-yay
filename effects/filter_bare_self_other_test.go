package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestSpellIsTargetingBareSelfOther pins the bare `Self`/`Other` arguments of
// Forge's base-qualified `Spell.IsTargeting <target-spec>` form (and its
// SpellAbility spelling). `Self` and `Other` are entries in the `predicates`
// map -- source-relative PREDICATE words, not filter bases -- so as a
// target-spec they must mean the same thing as the base-qualified
// `Card.Self`/`Card.Other`: the spell targets the perspective source itself /
// an object other than it. Before the normalisation the bare word was read as
// a base, matched nothing, and `!`-negating that false result answered a
// question the build could not actually read.
//
// The literal Bronze Horse filter argument is included (not the GPL card
// text): its `ValidCause$ Spell.IsTargeting Self` must recognise and evaluate.
func TestSpellIsTargetingBareSelfOther(t *testing.T) {
	g, id, spell := isTargetingFixture(t)
	sc := SpecContext{You: 0, Source: id["sourceSpell"]}
	// Preconditions the cases below lean on: the two candidate targets are
	// distinct objects and exactly one of them IS the perspective source.
	if id["sourceSpell"] == id["myBear"] {
		t.Fatalf("precondition: source and myBear must be different objects")
	}
	if g.Obj(id["sourceSpell"]).Zone != state.ZBattlefield || g.Obj(id["myBear"]).Zone != state.ZBattlefield {
		t.Fatalf("precondition: both targets must be on the battlefield, got %v / %v",
			g.Obj(id["sourceSpell"]).Zone, g.Obj(id["myBear"]).Zone)
	}
	for _, tc := range []struct {
		name string
		obj  state.ObjID
		spec string
		want bool
	}{
		// `Self` is true only on the target that IS the source.
		{"bare Self on the source target", id["sourceSpell"], "Spell.IsTargeting Self", true},
		{"bare Self on a different target", id["myBear"], "Spell.IsTargeting Self", false},
		{"Valid Self on the source target", id["sourceSpell"], "Spell.IsTargeting Valid Self", true},
		{"Valid Self on a different target", id["myBear"], "Spell.IsTargeting Valid Self", false},
		// `Other` is the exact inverse across the same two targets.
		{"bare Other on a different target", id["myBear"], "Spell.IsTargeting Other", true},
		{"bare Other on the source target", id["sourceSpell"], "Spell.IsTargeting Other", false},
		{"Valid Other on a different target", id["myBear"], "Spell.IsTargeting Valid Other", true},
		{"Valid Other on the source target", id["sourceSpell"], "Spell.IsTargeting Valid Other", false},
		// The SpellAbility spelling goes through the same normaliser.
		{"SpellAbility bare Self on the source target", id["sourceSpell"], "SpellAbility.IsTargeting Self", true},
		{"SpellAbility bare Other on a different target", id["myBear"], "SpellAbility.IsTargeting Other", true},
		// Negation must invert an ACTUAL match, not an always-false spec.
		{"negated bare Self on a different target", id["myBear"], "Spell.!IsTargeting Self", true},
		{"negated bare Self on the source target", id["sourceSpell"], "Spell.!IsTargeting Self", false},
		{"negated bare Other on the source target", id["sourceSpell"], "Spell.!IsTargeting Other", true},
		{"negated bare Other on a different target", id["myBear"], "Spell.!IsTargeting Other", false},
		// The base-qualified spelling the normaliser rewrites TO must agree.
		{"Card.Self on the source target", id["sourceSpell"], "Spell.IsTargeting Card.Self", true},
		{"Card.Other on a different target", id["myBear"], "Spell.IsTargeting Card.Other", true},
		// The Bronze Horse shape: the argument is `Self` after `ValidCause$`.
		{"Bronze Horse ValidCause shape", id["sourceSpell"], "Spell.IsTargeting Self", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Each case's own precondition: the target under test is bound
			// and its identity against the perspective source is what the
			// bare word reads.
			if tc.obj == 0 || g.Obj(tc.obj) == nil {
				t.Fatalf("case binds no target")
			}
			spell.Targets = []state.Target{{Obj: tc.obj}}
			isSource := tc.obj == sc.Source
			// The bare-word cases' own precondition: `Self` must only be
			// expected true on the source and `Other` only false on it, so a
			// case that quietly bound the wrong target fails loudly.
			taggedSelf := tc.spec == "Spell.IsTargeting Self" || tc.spec == "Spell.IsTargeting Valid Self"
			taggedOther := tc.spec == "Spell.IsTargeting Other" || tc.spec == "Spell.IsTargeting Valid Other"
			if taggedSelf && tc.want != isSource {
				t.Fatalf("precondition: Self case wants %v but target isSource=%v", tc.want, isSource)
			}
			if taggedOther && tc.want == isSource {
				t.Fatalf("precondition: Other case wants %v but target isSource=%v", tc.want, isSource)
			}
			if got := MatchesSpecCtx(g, tc.spec, spell.ID, sc); got != tc.want {
				t.Fatalf("MatchesSpecCtx(%q) on target %d (isSource=%v) = %v, want %v",
					tc.spec, tc.obj, isSource, got, tc.want)
			}
			// The textual oracle must agree with the compiled path; both go
			// through the shared normaliser.
			if got := matchesObjectText(g, tc.spec, spell, sc); got != tc.want {
				t.Fatalf("matchesObjectText(%q) on target %d = %v, want %v",
					tc.spec, tc.obj, got, tc.want)
			}
		})
	}
	// Every spelling above is a COMPLETE form the census must recognise, so a
	// ConditionPresent$/Count$ caller reaches the evaluator rather than
	// failing the form closed.
	for _, spec := range []string{
		"Spell.IsTargeting Self",
		"Spell.IsTargeting Other",
		"Spell.IsTargeting Valid Self",
		"Spell.IsTargeting Valid Other",
		"Spell.!IsTargeting Self",
		"SpellAbility.IsTargeting Other",
	} {
		if un := UnknownPredicates(spec); len(un) != 0 {
			t.Errorf("UnknownPredicates(%q) = %v, want empty", spec, un)
		}
	}
}
