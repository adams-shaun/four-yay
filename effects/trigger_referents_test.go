package effects

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestTriggerReferentFilters(t *testing.T) {
	g, ids := board(t)
	sc := SpecContext{Source: ids["myBear"], TriggerContext: TriggerContext{
		TriggerTarget: state.Target{Obj: ids["theirBig"]}, TriggerSource: ids["myBear"],
		TriggerCard: ids["theirBig"], TriggerPlayer: state.Target{IsPlayer: true, Player: 0},
		DefendingPlayer: state.Target{IsPlayer: true, Player: 1},
	}}
	for _, ref := range []string{"TriggeredTarget", "TriggeredCard", "TriggeredPlayer", "TriggeredDefendingPlayer"} {
		for _, op := range []string{"ControlledBy", "OwnedBy"} {
			spec := "Card." + op + " " + ref
			want := state.PlayerID(1)
			if ref == "TriggeredPlayer" {
				want = 0
			}
			// Deliberately distinct controller and owner on an object value: ownership
			// is not control. Neither the live game nor its objects are mutated here.
			o := &state.Object{Owner: 0, Controller: 1}
			got := MatchesObjectCtx(g, spec, o, sc)
			expected := (op == "OwnedBy" && want == 0) || (op == "ControlledBy" && want == 1)
			if got != expected {
				t.Errorf("%s = %v, want %v", spec, got, expected)
			}
			if unknown := UnknownPredicates(spec); len(unknown) != 0 {
				t.Errorf("recognised %s reported %v", spec, unknown)
			}
		}
	}
	// Both player and object target forms; seat zero is a PRESENT player.
	sc.TriggerTarget = state.Target{IsPlayer: true, Player: 0}
	if !MatchesSpecCtx(g, "Creature.ControlledBy TriggeredTarget", ids["myBear"], sc) {
		t.Fatal("seat zero target lost")
	}
	sc.TriggerTarget = state.Target{Obj: 999999}
	if MatchesSpecCtx(g, "Creature.ControlledBy TriggeredTarget", ids["myBear"], sc) {
		t.Fatal("missing object widened")
	}
}

func TestTriggerReferentWithoutContextFailsClosed(t *testing.T) {
	g, ids := board(t)
	for _, ref := range []string{"TriggeredTarget", "TriggeredCard", "TriggeredPlayer", "TriggeredDefendingPlayer"} {
		for _, op := range []string{"ControlledBy", "OwnedBy"} {
			for _, bang := range []string{"", "!"} {
				spec := "Creature." + bang + op + " " + ref
				for _, id := range []state.ObjID{ids["myBear"], ids["theirBig"]} {
					if MatchesSpecCtx(g, spec, id, SpecContext{You: 0, Source: ids["myBear"]}) {
						t.Errorf("zero context matched %s object %d", spec, id)
					}
				}
			}
		}
	}
}

func TestTriggerReferentGrammarScope(t *testing.T) {
	g, ids := board(t)
	sc := SpecContext{TriggerContext: TriggerContext{TriggerTarget: state.Target{IsPlayer: true, Player: 0}}}
	// A bare Targeted or Spawner> word is not itself a predicate: its
	// recognised use is as the argument to ControlledBy/OwnedBy. Bare, each
	// stays unknown.
	for _, ref := range []string{"Targeted", "Spawner>TriggeredTarget"} {
		spec := "Creature." + ref
		if MatchesSpecCtx(g, spec, ids["myBear"], sc) {
			t.Errorf("out-of-scope %s matched", spec)
		}
		if got := UnknownPredicates(spec); !reflect.DeepEqual(got, []string{ref}) {
			t.Errorf("%s unknown=%v, want [%s]", spec, got, ref)
		}
	}
	// TriggeredTargetController is not an inner ref of any kind: unknown
	// bare and as the ControlledBy/OwnedBy argument, Spawner> chain or not.
	for _, prefix := range []string{"", "ControlledBy ", "OwnedBy ", "ControlledBy Spawner>", "OwnedBy Spawner>"} {
		token := prefix + "TriggeredTargetController"
		spec := "Creature." + token
		if MatchesSpecCtx(g, spec, ids["myBear"], sc) {
			t.Errorf("out-of-scope %s matched", spec)
		}
		if got := UnknownPredicates(spec); !reflect.DeepEqual(got, []string{token}) {
			t.Errorf("%s unknown=%v, want [%s]", spec, got, token)
		}
	}
	// spawnercontrol: a Spawner> chain over a KNOWN inner ref is recognised
	// as the ControlledBy/OwnedBy argument (The Motherlode, Excavator's
	// "ControlledBy Spawner>TriggeredDefendingPlayer" and its seven sibling
	// corpus cards). The resolution itself is pinned by
	// TestSpawnerChainControlReferent below.
	for _, prefix := range []string{"ControlledBy ", "OwnedBy "} {
		spec := "Creature." + prefix + "Spawner>TriggeredTarget"
		if got := UnknownPredicates(spec); len(got) != 0 {
			t.Errorf("recognised %s reported %v", spec, got)
		}
	}
}

// TestNextOpponentToYourLeftReferent: Barroom Brawl's "target creature the
// opponent to your left controls" (Creature.ControlledBy
// NextOpponentToYourLeft) names the next living seat after You. The
// referent used to be unrecognised, so the fight's second target never
// matched anything.
func TestNextOpponentToYourLeftReferent(t *testing.T) {
	g, ids := board(t)
	spec := "Creature.ControlledBy NextOpponentToYourLeft"
	if unknown := UnknownPredicates(spec); len(unknown) != 0 {
		t.Fatalf("%s reported unknown %v", spec, unknown)
	}
	sc := SpecContext{You: 0}
	if !MatchesSpecCtx(g, spec, ids["theirBig"], sc) {
		t.Error("seat 1's creature is not controlled by seat 0's next opponent")
	}
	if MatchesSpecCtx(g, spec, ids["myBear"], sc) {
		t.Error("seat 0's own creature matched its next opponent")
	}
	sc.You = 1
	if !MatchesSpecCtx(g, spec, ids["myBear"], sc) {
		t.Error("from seat 1 the next seat wraps to seat 0")
	}
}
