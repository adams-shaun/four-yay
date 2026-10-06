package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
)

func TestZoneChangeETBHistoryNamedSkips(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, name := range []string{"Twilight Diviner", "Extraordinary Journey"} {
		t.Run(name, func(t *testing.T) {
			card, ok := reg.Lookup(name)
			if !ok {
				t.Fatalf("precondition: %s missing from corpus", name)
			}
			for _, r := range levelb.Requirements(card) {
				if r.Key != "trigger#0.1" {
					continue
				}
				if r.Sub != "trigger.etb-other" {
					t.Fatalf("precondition: history trigger classified %s", r.Sub)
				}
				it, skip := GenerateB(reg, name, r)
				if skip == nil || !strings.HasPrefix(skip.Reason, "trigger no recipe: etb filter zone history (") {
					t.Fatalf("item=%s skip=%v, want named zone-history skip", it.ID, skip)
				}
				return
			}
			t.Fatal("precondition: trigger#0.1 absent")
		})
	}
	// A history-constrained branch must not block an unconstrained OR branch.
	tr := &cards.Trigger{Mode: "ChangesZoneAll"}
	if got := zoneETBHistorySkip(tr, "Creature.wasCastFromGraveyard,Creature.YouCtrl"); got != "" {
		t.Fatalf("unconstrained alternative was skipped: %s", got)
	}
}
