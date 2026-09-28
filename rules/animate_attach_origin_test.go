package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestAnimateAttachExplicitOriginOutranksInZone checks the Attach census
// precedence when a scripted Origin$ and ValidTgts$ disagree. An absent
// Origin$ still permits the graveyard-enchant inference.
func TestAnimateAttachExplicitOriginOutranksInZone(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, origin, valid, tgtZone string
		want                         state.Zone
	}{
		{"graveyard origin beats exile filter", "Graveyard", "Creature.inZoneExile", "", state.ZGraveyard},
		{"exile origin beats graveyard filter", "Exile", "Creature.inZoneGraveyard", "", state.ZExile},
		{"no origin infers graveyard", "", "Creature.inZoneGraveyard", "", state.ZGraveyard},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sa := &cards.SA{API: "Attach", Params: map[string]string{
				"Origin": tc.origin, "ValidTgts": tc.valid, "TgtZone": tc.tgtZone,
			}}
			if sa.API != "Attach" || sa.Params["ValidTgts"] == "" {
				t.Fatalf("fixture must exercise an Attach with a ValidTgts$ filter: %+v", sa)
			}
			got := targetZones(sa)
			if len(got) != 1 || got[0] != tc.want {
				t.Fatalf("targetZones(Origin=%q, ValidTgts=%q, TgtZone=%q) = %v, want [%s]",
					tc.origin, tc.valid, tc.tgtZone, got, tc.want)
			}
		})
	}
}
