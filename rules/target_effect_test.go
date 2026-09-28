package rules

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// Decode the wire rather than referencing new Go fields, so the regression
// tests also run (and fail behaviorally) before the channel exists.
func targetEffectWire(t *testing.T, d *decision.Decision) map[string]any {
	t.Helper()
	if d == nil {
		t.Fatal("no target decision")
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	effect, ok := wire["target_effect"].(map[string]any)
	if !ok {
		t.Fatal("missing target_effect description")
	}
	return effect
}

func TestTargetEffectCorpusDamage(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name   string
		amount any
	}{
		{"Lightning Bolt", float64(3)}, {"Shock", float64(2)},
		{"Incinerate", float64(3)}, {"Fireball", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatal("missing corpus card")
			}
			sa := c.Faces[0].SpellAbility()
			if sa == nil || sa.API != "DealDamage" {
				t.Fatalf("unexpected compiled SA: %+v", sa)
			}
			e := newSeats(t, 2)
			e.askTarget(0, 0, sa)
			effect := targetEffectWire(t, e.Pending())
			if effect["api"] != "DealDamage" {
				t.Fatalf("effect = %v", effect)
			}
			damage, ok := effect["damage"].(map[string]any)
			if !ok {
				t.Fatal("damage not described")
			}
			amount, present := damage["amount"]
			if !present || amount != tc.amount {
				t.Fatalf("amount = %v (present %v), want %v", amount, present, tc.amount)
			}
		})
	}
}

func TestTargetEffectUnknownIsNotZero(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "X", "UnresolvableSVar", "Count$Valid Creature", "999999999999999999999999", "-1", "0"} {
		t.Run(raw, func(t *testing.T) {
			sa := &cards.SA{API: "DealDamage", Params: map[string]string{"ValidTgts": "Player", "NumDmg": raw}}
			e := newSeats(t, 2)
			e.askTarget(0, 0, sa)
			effect := targetEffectWire(t, e.Pending())
			damage, ok := effect["damage"].(map[string]any)
			if !ok {
				t.Fatal("missing damage")
			}
			amount, present := damage["amount"]
			if !present {
				t.Fatal("unknown must be explicit null")
			}
			if raw == "0" {
				if amount != float64(0) {
					t.Fatalf("literal zero = %v", amount)
				}
			} else if amount != nil {
				t.Fatalf("unknown became numeric: %v", amount)
			}
			// A conservative consumer cannot infer even potential lethal damage
			// without a numeric amount AND independently known positive life.
			n, known := amount.(float64)
			if known && n >= 1 {
				t.Fatal("ignorance became lethal")
			}
		})
	}
}

func TestTargetEffectNonDamage(t *testing.T) {
	t.Parallel()
	for _, api := range []string{"Draw", "Counter", "Destroy", "ChangeZone", "Unrecognised"} {
		t.Run(api, func(t *testing.T) {
			e := newSeats(t, 2)
			e.askTarget(0, 0, &cards.SA{API: api, Params: map[string]string{"ValidTgts": "Player", "NumDmg": "3"}})
			effect := targetEffectWire(t, e.Pending())
			if effect["api"] != api || effect["damage"] != nil {
				t.Fatalf("effect = %v", effect)
			}
		})
	}
}

func TestTargetEffectHostIndependent(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Lightning Bolt")
	if !ok {
		t.Fatal("missing Bolt")
	}
	var want map[string]any
	for _, seats := range []int{2, 4} {
		e := newSeats(t, seats)
		e.askTarget(0, 0, c.Faces[0].SpellAbility())
		direct := targetEffectWire(t, e.Pending())
		b, err := json.Marshal(e.Pending())
		if err != nil {
			t.Fatal(err)
		}
		var remote decision.Decision
		if err := json.Unmarshal(b, &remote); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(direct, targetEffectWire(t, &remote)) {
			t.Fatal("remote host lost metadata")
		}
		if want != nil && !reflect.DeepEqual(want, direct) {
			t.Fatal("host configuration changed metadata")
		}
		want = direct
	}
}
