package rules

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// This live-corpus census keeps trigger PresentDefined groups from quietly
// growing beyond the cases exercised by the engine tests. Its failure names
// each newly affected card and exact group, rather than pinning a stale count.
func TestTriggerPresentDefinedCorpusCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	var found []string
	for _, card := range reg.Cards {
		if card == nil {
			continue
		}
		for _, face := range card.Faces {
			if face == nil {
				continue
			}
			for _, trigger := range face.Triggers {
				for _, key := range []string{"PresentDefined", "PresentDefined2"} {
					defined := strings.TrimSpace(trigger.Params[key])
					if defined != "" && defined != "Self" {
						found = append(found, face.Name+" ("+trigger.Mode+": "+key+"="+defined+")")
					}
				}
			}
		}
	}
	sort.Strings(found)
	t.Logf("non-Self trigger PresentDefined census (%d): %s", len(found), strings.Join(found, "; "))
	want := []string{"Planetarium of Wan Shi Tong (Scry: PresentDefined=Remembered)", "Planetarium of Wan Shi Tong (Surveil: PresentDefined=Remembered)"}
	if strings.Join(found, "\n") != strings.Join(want, "\n") {
		t.Fatalf("non-Self trigger PresentDefined corpus census changed; found (%d): %s; want (%d): %s", len(found), strings.Join(found, "; "), len(want), strings.Join(want, "; "))
	}
}
