package templates

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestConditionSVarComparePreludes(t *testing.T) {
	for _, value := range []string{"GE2", "GE3"} {
		params := map[string]string{"SVarCompare": value}
		text := conditionText(params, nil)
		if len(text) != 1 || text[0] != "SVarCompare "+value {
			t.Fatalf("precondition: conditionText = %v", text)
		}
		if needle := conditionParamText("SVarCompare", value); !strings.Contains(strings.ToLower(text[0]), strings.ToLower(needle)) {
			t.Errorf("needle %q does not match emitted text %q", needle, text[0])
		}
	}
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name, body, compare string
		want                []string
	}{
		// Neither GE2 case contains an alternative two-spell needle.
		{"entered artifacts", "Count$ThisTurnEntered_Battlefield_Artifact.YouCtrl", "GE2", []string{"Sol Ring", "Arcane Signet"}},
		{"cast cards", "Count$ThisTurnCast_Card.YouCtrl", "GE2", []string{"Shock", "Shock"}},
		{"drawn cards", "Count$YouDrewThisTurn", "GE3", []string{"Divination", "Concentrate"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range tc.want {
				if _, ok := reg.Lookup(name); !ok {
					t.Fatalf("precondition: %s missing from corpus", name)
				}
			}
			params := map[string]string{"CheckSVar": "History", "SVarCompare": tc.compare}
			preludes := conditionPreludes(reg, params, map[string]string{"History": tc.body})
			if len(preludes) == 0 {
				t.Fatal("precondition: history handler offered no prelude")
			}
			for _, p := range preludes {
				if !reflect.DeepEqual(p.hand, tc.want) {
					continue
				}
				if len(p.steps) != 4 || p.steps[0].Op != "cast" || p.steps[1].Op != "resolve" || p.steps[2].Op != "cast" || p.steps[3].Op != "resolve" {
					t.Fatalf("two-spell prelude steps = %+v", p.steps)
				}
				for i, name := range tc.want {
					if p.steps[i*2].Card != "p0:"+name {
						t.Fatalf("cast %d = %+v, want %s", i, p.steps[i*2], name)
					}
				}
				return
			}
			t.Fatalf("no two-spell prelude with hand %v: %+v", tc.want, preludes)
		})
	}
}
